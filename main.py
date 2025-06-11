import discord
import os
import threading
import logging
from dotenv import load_dotenv
from flask import Flask
import database as db  # データベース操作用のファイルをインポート

# --- 初期設定 ---
load_dotenv()
logging.basicConfig(level=logging.INFO)

# --- 環境変数と定数 ---
TOKEN = os.getenv("TOKEN")
INTRODUCTION_CHANNEL_ID = 1300659373227638794  # 🚨実際の自己紹介チャンネルIDに要変更
NOTIFICATION_CHANNEL_ID = 1331177944244289598  # 🚨実際の通知用チャンネルIDに要変更
TARGET_VOICE_CHANNELS = [
    1300291307750559754, 1302151049368571925, 1302151154981011486,
    1306190768431431721, 1306190915483734026
] # 🚨実際のボイスチャンネルIDリストに要変更

# --- Discord Botの準備 ---
intents = discord.Intents.default()
intents.voice_states = True
intents.messages = True
intents.message_content = True
intents.members = True # メンバー情報を取得するために必要
client = discord.Client(intents=intents)

# --- スリープ対策Webサーバーの準備 ---
app = Flask(__name__)
@app.route('/')
def home():
    return "Self-Introduction Bot is running!"
def run_flask():
    port = int(os.getenv("PORT", 8080))
    app.run(host='0.0.0.0', port=port)

# --- Botのイベント処理 ---

@client.event
async def on_ready():
    logging.info(f"✅ Botがログインしました: {client.user}")
    
    try:
        # 1. データベースを初期化（テーブルがなければ作る）
        await db.init_intro_bot_db()
        logging.info("✅ データベースを初期化しました。")

        # 2. 過去の自己紹介メッセージをスキャンしてDBに保存
        intro_channel = client.get_channel(INTRODUCTION_CHANNEL_ID)
        if intro_channel:
            logging.info("📜 過去の自己紹介をスキャン中...")
            count = 0
            async for message in intro_channel.history(limit=2000): # 取得件数を増やすことも可能
                if not message.author.bot:
                    message_link = f"https://discord.com/channels/{message.guild.id}/{message.channel.id}/{message.id}"
                    await db.save_intro_link(message.author.id, message_link)
                    count += 1
            logging.info(f"📜 スキャン完了。{count}件の自己紹介をDBに保存/更新しました。")
        else:
            logging.error(f"❌ 自己紹介チャンネル(ID: {INTRODUCTION_CHANNEL_ID})が見つかりません。")

    except Exception as e:
        logging.error(f"❌ 起動処理中にエラーが発生しました: {e}", exc_info=True)


@client.event
async def on_message(message):
    # 自己紹介チャンネルに投稿された、Bot以外のメッセージを処理
    if message.channel.id == INTRODUCTION_CHANNEL_ID and not message.author.bot:
        try:
            message_link = f"https://discord.com/channels/{message.guild.id}/{message.channel.id}/{message.id}"
            # データベースにリンクを保存
            await db.save_intro_link(message.author.id, message_link)
            logging.info(f"📝 {message.author} の新しい自己紹介をDBに保存しました。")
        except Exception as e:
            logging.error(f"❌ on_messageでのDB保存中にエラー: {e}", exc_info=True)


@client.event
async def on_voice_state_update(member, before, after):
    # ボイスチャンネルに「入室」した時だけ反応
    if before.channel is None and after.channel is not None:
        # 対象のボイスチャンネルか確認
        if after.channel.id in TARGET_VOICE_CHANNELS:
            logging.info(f"🔊 {member} がボイスチャンネル '{after.channel.name}' に参加しました。")
            
            notify_channel = client.get_channel(NOTIFICATION_CHANNEL_ID)
            if not notify_channel:
                logging.error(f"❌ 通知チャンネル(ID: {NOTIFICATION_CHANNEL_ID})が見つかりません。")
                return
            
            try:
                # データベースからユーザーの自己紹介リンクを取得
                user_link = await db.load_intro_link(member.id)
                
                if user_link:
                    msg = (
                        f"{member.display_name} さんが`{after.channel.name}` に入室しました！\n"
                        f"📌 自己紹介はこちら → {user_link}"
                    )
                else:
                    msg = (
                        f"{member.display_name} さんが`{after.channel.name}` に入室しました！\n"
                        "⚠️ この方の自己紹介はまだ投稿されていません。"
                    )
                
                await notify_channel.send(msg)
                logging.info(f"✅ {member.display_name} さんの入室通知を送信しました。")

            except Exception as e:
                logging.error(f"❌ 通知メッセージ送信中にエラー: {e}", exc_info=True)


# --- 起動処理 ---
def main():
    # Webサーバーを別スレッドで起動
    flask_thread = threading.Thread(target=run_flask)
    flask_thread.start()
    
    # Botを起動
    if not TOKEN:
        logging.error("❌ TOKENが設定されていません！.envファイルを確認してください。")
        return
        
    try:
        client.run(TOKEN)
    except discord.errors.LoginFailure:
        logging.error("❌ TOKENが不正です。Discord Developer Portalでトークンを確認してください。")
    except Exception as e:
        logging.error(f"❌ Botの起動に失敗しました: {e}", exc_info=True)

if __name__ == "__main__":
    main()
