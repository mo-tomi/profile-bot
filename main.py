import discord
from discord import ui
import os
import threading
import logging
from dotenv import load_dotenv
from flask import Flask
import database as db

# --- 初期設定 ---
load_dotenv()
logging.basicConfig(level=logging.INFO)

# --- 環境変数と定数 ---
TOKEN = os.getenv("TOKEN")
INTRODUCTION_CHANNEL_ID = 1300659373227638794
NOTIFICATION_CHANNEL_ID = 1331177944244289598
TARGET_VOICE_CHANNELS = [
    1300291307750559754, 1302151049368571925, 1302151154981011486,
    1306190768431431721, 1306190915483734026
]

# --- Discord Botの準備 ---
intents = discord.Intents.default()
intents.voice_states = True
intents.messages = True
intents.message_content = True
intents.members = True
client = discord.Client(intents=intents)

# --- スリープ対策Webサーバーの準備 ---
app = Flask(__name__)
@app.route('/')
def home():
    return "Self-Introduction Bot v2 is running!"
@app.route('/health')
def health_check():
    return "OK"
def run_flask():
    port = int(os.getenv("PORT", 8080))
    app.run(host='0.0.0.0', port=port)

# --- Botのイベント処理 ---
@client.event
async def on_ready():
    logging.info(f"✅ Botがログインしました: {client.user}")
    try:
        await db.init_intro_bot_db()
        logging.info("✅ データベースを初期化しました。")
        intro_channel = client.get_channel(INTRODUCTION_CHANNEL_ID)
        if intro_channel:
            logging.info(f"📜 過去の自己紹介をスキャン中 (チャンネル: {intro_channel.name})...")
            count = 0
            async for message in intro_channel.history(limit=2000):
                if not message.author.bot:
                    await db.save_intro(message.author.id, message.channel.id, message.id)
                    count += 1
            logging.info(f"📜 スキャン完了。{count}件の自己紹介をDBに保存/更新しました。")
        else:
            logging.error(f"❌ 自己紹介チャンネル(ID: {INTRODUCTION_CHANNEL_ID})が見つかりません。")
    except Exception as e:
        logging.error(f"❌ 起動処理中にエラー: {e}", exc_info=True)

@client.event
async def on_message(message):
    if message.channel.id == INTRODUCTION_CHANNEL_ID and not message.author.bot:
        try:
            await db.save_intro(message.author.id, message.channel.id, message.id)
            logging.info(f"📝 {message.author} の新しい自己紹介をDBに保存しました。")
        except Exception as e:
            logging.error(f"❌ on_messageでのDB保存中にエラー: {e}", exc_info=True)

@client.event
async def on_voice_state_update(member, before, after):
    if before.channel != after.channel and after.channel and after.channel.id in TARGET_VOICE_CHANNELS:
        logging.info(f"🔊 {member} がボイスチャンネル '{after.channel.name}' に参加しました。")
        notify_channel = client.get_channel(NOTIFICATION_CHANNEL_ID)
        if not notify_channel:
            logging.error(f"❌ 通知チャンネル(ID: {NOTIFICATION_CHANNEL_ID})が見つかりません。")
            return
        try:
            intro_ids = await db.get_intro_ids(member.id)
            intro_message = None
            if intro_ids:
                try:
                    intro_channel = client.get_channel(intro_ids['channel_id'])
                    if intro_channel:
                        intro_message = await intro_channel.fetch_message(intro_ids['message_id'])
                except discord.NotFound:
                    logging.warning(f"⚠️ {member.name}さんの自己紹介メッセージが見つかりませんでした(削除されたかも)。")
            if intro_message:
                embed = discord.Embed(description=intro_message.content, color=discord.Color.blue())
                embed.set_author(name=f"{member.display_name}さんの自己紹介", icon_url=member.display_avatar.url)
                view = ui.View()
                button = ui.Button(label="元の自己紹介へ移動", style=discord.ButtonStyle.link, url=intro_message.jump_url)
                view.add_item(button)
                await notify_channel.send(f"**{member.display_name}** さんが`{after.channel.name}` に入室しました！", embed=embed, view=view)
            else:
                msg = f"**{member.display_name}** さんが`{after.channel.name}` に入室しました！\n⚠️ この方の自己紹介はまだ投稿されていないか、見つかりませんでした。"
                await notify_channel.send(msg)
            logging.info(f"✅ {member.display_name} さんの入室通知を送信しました。")
        except Exception as e:
            logging.error(f"❌ 通知メッセージ送信中に予期せぬエラー: {e}", exc_info=True)

# --- 起動処理 ---
def main():
    flask_thread = threading.Thread(target=run_flask)
    flask_thread.start()
    if not TOKEN:
        logging.error("❌ TOKENが設定されていません！")
        return
    client.run(TOKEN)

if __name__ == "__main__":
    main()
