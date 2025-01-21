# main.py
import discord
import os
import json
from keep_alive import keep_alive

# 🌟 ここにBotの設定を書いていきます

# 🔧 権限設定（ボットができることを許可）
intents = discord.Intents.default()
intents.voice_states = True  # ボイスチャンネルの情報を見る
intents.messages = True      # メッセージを見る
intents.message_content = True  # メッセージの中身を見る（重要！）
intents.members = True       # メンバー情報を見る

client = discord.Client(intents=intents)

# 🔧 自己紹介チャンネルのIDを設定（後で変更）
INTRODUCTION_CHANNEL_ID = 1300659373227638794  # ✏️ ここを変更！

# 📂 自己紹介リンクを保存する辞書
introduction_links = {}

# 💾 リンクをファイルに保存する関数
def save_links():
    with open("introduction_links.json", "w") as f:
        json.dump(introduction_links, f)

# 📥 リンクをファイルから読み込む関数
def load_links():
    try:
        with open("introduction_links.json", "r") as f:
            return json.load(f)
    except FileNotFoundError:
        return {}

# 🚀 Bot起動時の処理
@client.event
async def on_ready():
    global introduction_links
    introduction_links = load_links()
    print(f'✅ Botがログインしました: {client.user}')
    print(f"📜 読み込まれたリンク数: {len(introduction_links)}")

# 💬 メッセージ受信時の処理
@client.event
async def on_message(message):
    # 自己紹介チャンネルでのみ反応
    if message.channel.id == INTRODUCTION_CHANNEL_ID:
        # メッセージリンク作成
        message_link = f"https://discord.com/channels/{message.guild.id}/{message.channel.id}/{message.id}"
        
        # ユーザーIDをキーにして保存
        introduction_links[str(message.author.id)] = message_link
        save_links()
        
        print(f"📝 {message.author} のリンクを保存: {message_link}")

# 🎧 ボイスチャンネル入室時の処理
@client.event
async def on_voice_state_update(member, before, after):
    # 入室時のみ反応（退室時は無視）
    if before.channel is None and after.channel is not None:
        # 🔧 通知先チャンネルID（後で変更）
        notify_channel = client.get_channel(1300291307750559754)  # ✏️ ここを変更！
        
        # ユーザーの自己紹介リンクを検索
        user_link = introduction_links.get(str(member.id))
        
        # メッセージ作成
        if user_link:
            msg = (
                f"{member.mention} さんがボイスチャンネルに参加しました！🎉\n"
                f"📌 自己紹介はこちら → {user_link}"
            )
        else:
            msg = (
                f"{member.mention} さんがボイスチャンネルに参加しました！🎉\n"
                "❌ 自己紹介がまだありません"
            )
        
        await notify_channel.send(msg)

# 🌐 サーバーを起動し続ける（Render用）
keep_alive()

# 🔑 TOKENでBotを起動
client.run(os.getenv("TOKEN"))
