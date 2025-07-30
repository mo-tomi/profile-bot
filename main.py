import discord
from discord import ui
import os
import threading
import logging
import signal
import sys
import asyncio
from datetime import datetime, time, timedelta
from dotenv import load_dotenv
from flask import Flask
import database as db

load_dotenv()
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(name)s: %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S'
)

TOKEN = os.getenv("TOKEN")
INTRODUCTION_CHANNEL_ID = 1300659373227638794
NOTIFICATION_CHANNEL_ID = 1331177944244289598
TARGET_VOICE_CHANNELS = [
    1300291307750559754, 1302151049368571925, 1302151154981011486,
    1306190768431431721, 1306190915483734026
]

intents = discord.Intents.default()
intents.voice_states = True
intents.messages = True
intents.message_content = True
intents.members = True
bot = discord.Bot(intents=intents)

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

async def shutdown():
    logging.info("🔄 Botを終了中...")
    await db.close_pool()
    await bot.close()
    logging.info("✅ 終了処理完了")

def signal_handler(sig, frame):
    logging.info(f"🛑 シグナル {sig} を受信しました")
    try:
        import asyncio
        loop = asyncio.get_event_loop()
        loop.create_task(shutdown())
    except:
        pass
    sys.exit(0)

signal.signal(signal.SIGTERM, signal_handler)
signal.signal(signal.SIGINT, signal_handler)

@bot.event
async def on_ready():
    logging.info(f"✅ Botがログインしました: {bot.user}")
    
    database_url = os.getenv("DATABASE_URL")
    if not database_url:
        logging.error("❌ DATABASE_URL環境変数が設定されていません！")
        return
    
    logging.info(f"🔗 データベース接続中... (URL前半: {database_url[:50]}...)")
    
    try:
        logging.info("🔧 データベースを初期化中...")
        await db.init_intro_bot_db()
        await db.init_daily_reminder_db()
        
        intro_count = await db.get_intro_count()
        logging.info(f"📊 現在の自己紹介データ件数: {intro_count}件")
        
        intro_channel = bot.get_channel(INTRODUCTION_CHANNEL_ID)
        if not intro_channel:
            logging.error(f"❌ 自己紹介チャンネル(ID: {INTRODUCTION_CHANNEL_ID})が見つかりません！")
            return
        
        logging.info(f"📜 自己紹介チャンネル確認: {intro_channel.name} (ID: {intro_channel.id})")
        
        notify_channel = bot.get_channel(NOTIFICATION_CHANNEL_ID)
        if not notify_channel:
            logging.error(f"❌ 通知チャンネル(ID: {NOTIFICATION_CHANNEL_ID})が見つかりません！")
            return
        
        logging.info(f"📢 通知チャンネル確認: {notify_channel.name} (ID: {notify_channel.id})")
        
        logging.info("🔍 過去の自己紹介をスキャン開始...")
        scan_count = 0
        new_count = 0
        update_count = 0
        
        try:
            async for message in intro_channel.history(limit=3000):
                if not message.author.bot:
                    scan_count += 1
                    try:
                        existing_intro = await db.get_intro_ids(message.author.id)
                        if existing_intro:
                            update_count += 1
                            logging.debug(f"🔄 更新: {message.author.name} (ID: {message.author.id})")
                        else:
                            new_count += 1
                            logging.info(f"🆕 新規: {message.author.name} (ID: {message.author.id})")
                        
                        await db.save_intro(message.author.id, message.channel.id, message.id)
                        
                        if scan_count % 100 == 0:
                            logging.info(f"📈 スキャン進捗: {scan_count}件処理完了 (新規: {new_count}, 更新: {update_count})")
                            
                    except Exception as save_error:
                        logging.error(f"❌ メッセージ保存エラー (Message ID: {message.id}): {save_error}")
            
            logging.info(f"🎉 スキャン完了！")
            logging.info(f"  📊 総処理数: {scan_count}件")
            logging.info(f"  🆕 新規追加: {new_count}件")
            logging.info(f"  🔄 更新: {update_count}件")
            
            final_count = await db.get_intro_count()
            logging.info(f"📊 最終DB内自己紹介件数: {final_count}件")
            
            recent_intros = await db.list_recent_intros(5)
            if recent_intros:
                logging.info("📝 最新の自己紹介サンプル:")
                for intro in recent_intros:
                    logging.info(f"  User: {intro['user_id']}, Channel: {intro['channel_id']}, Message: {intro['message_id']}")
            
        except Exception as scan_error:
            logging.error(f"❌ メッセージスキャン中にエラー: {scan_error}", exc_info=True)
        
        # 日次リマインダータスクを開始
        asyncio.create_task(daily_reminder_task())
        
        logging.info("✅ Bot初期化完了！入室監視を開始します。")
        
    except Exception as e:
        logging.error(f"❌ 起動処理中にエラー: {e}", exc_info=True)

@bot.event
async def on_message(message):
    if message.channel.id == INTRODUCTION_CHANNEL_ID and not message.author.bot:
        try:
            await db.save_intro(message.author.id, message.channel.id, message.id)
            logging.info(f"📝 {message.author.name} の新しい自己紹介をDBに保存しました")
        except Exception as e:
            logging.error(f"❌ on_messageでのDB保存中にエラー: {e}", exc_info=True)

@bot.event
async def on_voice_state_update(member, before, after):
    # 特定のbotと管理人の自己紹介を除外
    excluded_bot_ids = [533698325203910668, 916300992612540467, 1300226846599675974]
    
    if (before.channel != after.channel and 
        after.channel and 
        after.channel.id in TARGET_VOICE_CHANNELS):
        
        # 除外対象のbotかチェック
        if member.id in excluded_bot_ids:
            logging.info(f"🤖 除外対象bot {member.display_name} (ID: {member.id}) がボイスチャンネル '{after.channel.name}' に参加しましたが、自己紹介通知をスキップします")
            return
        
        logging.info(f"🔊 {member.display_name} (ID: {member.id}) がボイスチャンネル '{after.channel.name}' に参加しました")
        
        notify_channel = bot.get_channel(NOTIFICATION_CHANNEL_ID)
        if not notify_channel:
            logging.error(f"❌ 通知チャンネル(ID: {NOTIFICATION_CHANNEL_ID})が見つかりません")
            return
        
        try:
            logging.info(f"🔍 {member.display_name} の自己紹介を検索中...")
            intro_ids = await db.get_intro_ids(member.id)
            
            if intro_ids:
                logging.info(f"✅ 自己紹介発見: Channel {intro_ids['channel_id']}, Message {intro_ids['message_id']}")
                
                try:
                    intro_channel = bot.get_channel(intro_ids['channel_id'])
                    if not intro_channel:
                        logging.error(f"❌ 自己紹介チャンネル(ID: {intro_ids['channel_id']})が取得できません")
                        raise Exception("チャンネル取得失敗")
                    
                    intro_message = await intro_channel.fetch_message(intro_ids['message_id'])
                    logging.info(f"✅ 自己紹介メッセージ取得成功 (長さ: {len(intro_message.content)}文字)")
                    
                    embed = discord.Embed(
                        description=intro_message.content, 
                        color=discord.Color.blue()
                    )
                    embed.set_author(
                        name=f"{member.display_name}さんの自己紹介", 
                        icon_url=member.display_avatar.url
                    )
                    
                    view = ui.View()
                    button = ui.Button(
                        label="元の自己紹介へ移動", 
                        style=discord.ButtonStyle.link, 
                        url=intro_message.jump_url
                    )
                    view.add_item(button)
                    
                    await notify_channel.send(
                        f"**{member.display_name}** さんが `{after.channel.name}` に入室しました！", 
                        embed=embed, 
                        view=view
                    )
                    logging.info("✅ 自己紹介付き通知を送信しました")
                    
                except discord.NotFound:
                    logging.warning(f"⚠️ {member.display_name} の自己紹介メッセージが見つかりません（削除済み?）")
                    msg = f"**{member.display_name}** さんが `{after.channel.name}` に入室しました！\n⚠️ この方の自己紹介メッセージが削除されているようです。"
                    await notify_channel.send(msg)
                    logging.info("✅ 自己紹介なし通知（削除済み）を送信しました")
                    
                except Exception as fetch_error:
                    logging.error(f"❌ 自己紹介メッセージ取得エラー: {fetch_error}")
                    msg = f"**{member.display_name}** さんが `{after.channel.name}` に入室しました！\n⚠️ 自己紹介の取得中にエラーが発生しました。"
                    await notify_channel.send(msg)
                    logging.info("✅ エラー時代替通知を送信しました")
            else:
                logging.info(f"❌ {member.display_name} の自己紹介がDBに見つかりません")
                msg = f"**{member.display_name}** さんが `{after.channel.name}` に入室しました！\n⚠️ この方の自己紹介はまだ投稿されていないか、見つかりませんでした。"
                await notify_channel.send(msg)
                logging.info("✅ 自己紹介なし通知を送信しました")
                
        except Exception as e:
            logging.error(f"❌ 通知処理中にエラー: {e}", exc_info=True)
            
            try:
                msg = f"**{member.display_name}** さんが `{after.channel.name}` に入室しました！"
                await notify_channel.send(msg)
                logging.info("✅ 最低限の入室通知を送信しました")
            except Exception as fallback_error:
                logging.error(f"❌ 代替通知送信も失敗: {fallback_error}")

async def daily_reminder_task():
    """
    毎日決まった時間（午前10時）に自己紹介未投稿のメンバーにお知らせを送信する。
    """
    while True:
        try:
            now = datetime.now()
            # 毎日午前10時に実行
            target_time = time(10, 0)  # 10:00 AM
            
            # 今日の10時まで待機
            target_datetime = datetime.combine(now.date(), target_time)
            if now.time() > target_time:
                # 既に10時を過ぎている場合は明日の10時に設定
                target_datetime += timedelta(days=1)
            
            # 次の実行時刻まで待機
            sleep_seconds = (target_datetime - now).total_seconds()
            logging.info(f"⏰ 次回自己紹介リマインダー実行: {target_datetime} ({sleep_seconds:.0f}秒後)")
            await asyncio.sleep(sleep_seconds)
            
            # 共通関数を使用してリマインダーを送信
            result = await send_intro_reminder()
            logging.info(result)
            
        except Exception as e:
            logging.error(f"❌ 日次リマインダー処理中にエラー: {e}", exc_info=True)
            # エラーが発生しても1時間後に再試行
            await asyncio.sleep(3600)

async def send_intro_reminder(force=False):
    """
    自己紹介リマインダーを送信する共通関数
    """
    try:
        # forceがTrueでない場合、今日既にリマインダーを送信済みかチェック
        if not force and await db.check_daily_reminder_sent():
            return "📅 今日は既にリマインダーを送信済みです"
        
        # 自己紹介チャンネルとサーバーを取得
        intro_channel = bot.get_channel(INTRODUCTION_CHANNEL_ID)
        if not intro_channel:
            return f"❌ 自己紹介チャンネル(ID: {INTRODUCTION_CHANNEL_ID})が見つかりません"
            
        guild = intro_channel.guild
        
        # 自己紹介未投稿のメンバーを取得
        members_without_intro = await db.get_members_without_intro(guild.members)
        
        if not members_without_intro:
            if not force:
                await db.log_daily_reminder([])
            return "🎉 全メンバーが自己紹介済みです！"
        
        # リマインダーメッセージを作成・送信
        member_mentions = [member.mention for member in members_without_intro[:10]]  # 最大10人まで
        remaining_count = len(members_without_intro) - 10
        
        message_content = "🌟 **自己紹介のお知らせ** 🌟\n\n"
        message_content += f"{' '.join(member_mentions)}\n\n"
        message_content += f"こんにちは！<#{INTRODUCTION_CHANNEL_ID}> チャンネルでの自己紹介をお待ちしています！\n"
        message_content += "あなたのことを教えてください 😊\n\n"
        
        if remaining_count > 0:
            message_content += f"※他にも{remaining_count}名の方が自己紹介をお待ちしています"
        
        await intro_channel.send(message_content)
        
        # ログを記録（forceの場合は記録しない）
        if not force:
            notified_user_ids = [str(member.id) for member in members_without_intro]
            await db.log_daily_reminder(notified_user_ids)
        
        return f"✅ 自己紹介リマインダーを送信しました ({len(members_without_intro)}名対象)"
        
    except Exception as e:
        logging.error(f"❌ リマインダー送信中にエラー: {e}", exc_info=True)
        return f"❌ エラーが発生しました: {str(e)}"

@bot.slash_command(name="profile", description="自己紹介リマインダーをテスト実行します")
async def profile_command(ctx):
    """
    自己紹介リマインダーを手動でテスト実行するスラッシュコマンド
    """
    await ctx.defer()
    
    try:
        result = await send_intro_reminder(force=True)
        await ctx.followup.send(f"🔄 **プロフィールリマインダー実行結果**\n{result}")
        logging.info(f"✅ /profile コマンドが実行されました - 結果: {result}")
    except Exception as e:
        error_msg = f"❌ コマンド実行中にエラー: {str(e)}"
        await ctx.followup.send(error_msg)
        logging.error(f"❌ /profile コマンド実行エラー: {e}", exc_info=True)

def main():
    if not TOKEN:
        logging.error("❌ TOKENが設定されていません！")
        return
    
    if not os.getenv("DATABASE_URL"):
        logging.error("❌ DATABASE_URLが設定されていません！")
        return
    
    flask_thread = threading.Thread(target=run_flask, daemon=True)
    flask_thread.start()
    logging.info("✅ Webサーバーを開始しました")
    
    logging.info("🚀 Botを開始します...")
    try:
        bot.run(TOKEN)
    except Exception as e:
        logging.error(f"❌ Bot実行エラー: {e}")
    finally:
        logging.info("🔚 Bot終了")

if __name__ == "__main__":
    main()