import discord
from discord import app_commands
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
TOKEN = os.getenv("DISCORD_BOT_TOKEN")
REPORT_CHANNEL_ID = int(os.getenv("REPORT_CHANNEL_ID", 0))
URGENT_ROLE_ID = int(os.getenv("URGENT_ROLE_ID", 0))

# --- Discord Botの準備 ---
intents = discord.Intents.default()
client = discord.Client(intents=intents)
tree = app_commands.CommandTree(client)

# --- スリープ対策Webサーバーの準備 ---
app = Flask(__name__)
@app.route('/')
def home():
    return "Shugoshin Bot is watching over you."

@app.route('/health')
def health_check():
    return "OK", 200

def run_flask():
    port = int(os.getenv("PORT", 8080))
    app.run(host='0.0.0.0', port=port)

# --- Botのイベント処理 ---
@client.event
async def on_ready():
    # ★★★★★ ここが修正されたポイント ★★★★★
    # 守護神ボット専用のDBテーブルを、正しい関数名で初期化
    await db.init_shugoshin_db() 
    # ★★★★★★★★★★★★★★★★★★★★★★★
    
    await tree.sync()
    logging.info(f"✅ 守護神ボットが起動しました: {client.user}")


# --- 管理コマンドのグループを作成 ---
report_manage_group = app_commands.Group(name="reportmanage", description="報告を管理します。")

# --- サブコマンド: status ---
@report_manage_group.command(name="status", description="報告のステータスを変更します。")
@app_commands.describe(report_id="ステータスを変更したい報告のID", new_status="新しいステータス")
@app_commands.choices(new_status=[
    app_commands.Choice(name="対応中", value="対応中"),
    app_commands.Choice(name="解決済み", value="解決済み"),
    app_commands.Choice(name="却下", value="却下"),
])
async def status(interaction: discord.Interaction, report_id: int, new_status: app_commands.Choice[str]):
    await interaction.response.defer(ephemeral=True)
    
    try:
        report_data = await db.get_report(report_id)
        if not report_data:
            await interaction.followup.send(f"エラー: 報告ID `{report_id}` が見つかりません。", ephemeral=True)
            return

        report_channel = client.get_channel(REPORT_CHANNEL_ID)
        original_message = await report_channel.fetch_message(report_data['message_id'])
        
        original_embed = original_message.embeds[0]
        
        status_colors = {"対応中": discord.Color.yellow(), "解決済み": discord.Color.green(), "却下": discord.Color.greyple()}
        original_embed.color = status_colors.get(new_status.value)
        
        for i, field in enumerate(original_embed.fields):
            if field.name == "📊 ステータス":
                original_embed.set_field_at(i, name="📊 ステータス", value=new_status.value, inline=False)
                break
        
        await original_message.edit(embed=original_embed)
        await db.update_report_status(report_id, new_status.value)
        
        await interaction.followup.send(f"報告ID `{report_id}` のステータスを「{new_status.value}」に変更しました。", ephemeral=True)
        logging.info(f"報告ID {report_id} のステータスが {new_status.value} に変更されました。")

    except discord.NotFound:
        await interaction.followup.send("エラー: 元の報告メッセージが見つかりませんでした。削除された可能性があります。", ephemeral=True)
    except Exception as e:
        await interaction.followup.send(f"ステータス更新中にエラーが発生しました: {e}", ephemeral=True)
        logging.error(f"ステータス更新エラー: {e}", exc_info=True)


# --- サブコマンド: list ---
@report_manage_group.command(name="list", description="報告の一覧を表示します。")
@app_commands.describe(filter="表示するステータスで絞り込みます。")
@app_commands.choices(filter=[
    app_commands.Choice(name="すべて", value="all"),
    app_commands.Choice(name="未対応", value="未対応"),
    app_commands.Choice(name="対応中", value="対応中"),
])
async def list_reports_cmd(interaction: discord.Interaction, filter: app_commands.Choice[str] = None):
    await interaction.response.defer(ephemeral=True)
    
    status_filter = filter.value if filter else None
    reports = await db.list_reports(status_filter)
    
    if not reports:
        await interaction.followup.send("該当する報告はありません。", ephemeral=True)
        return

    embed = discord.Embed(title=f"📜 報告リスト ({filter.name if filter else '最新'})", color=discord.Color.blue())
    
    description = ""
    for report in reports:
        try:
            target_user = await client.fetch_user(report['target_user_id'])
            user_name = target_user.name
        except discord.NotFound:
            user_name = "不明なユーザー"

        description += f"**ID: {report['report_id']}** | 対象: {user_name} | ステータス: `{report['status']}`\n"
        
    embed.description = description
    await interaction.followup.send(embed=embed, ephemeral=True)


# --- サブコマンド: stats ---
@report_manage_group.command(name="stats", description="報告の統計情報を表示します。")
async def stats(interaction: discord.Interaction):
    await interaction.response.defer(ephemeral=True)
    
    stats_data = await db.get_report_stats()
    total = sum(stats_data.values())
    
    embed = discord.Embed(title="📈 報告統計", description=f"総報告数: **{total}** 件", color=discord.Color.purple())
    
    unhandled = stats_data.get('未対応', 0)
    in_progress = stats_data.get('対応中', 0)
    resolved = stats_data.get('解決済み', 0)
    rejected = stats_data.get('却下', 0)
    
    embed.add_field(name="未対応 🔴", value=f"**{unhandled}** 件", inline=True)
    embed.add_field(name="対応中 🟡", value=f"**{in_progress}** 件", inline=True)
    embed.add_field(name="解決済み 🟢", value=f"**{resolved}** 件", inline=True)
    embed.add_field(name="却下 ⚪", value=f"**{rejected}** 件", inline=True)
    
    await interaction.followup.send(embed=embed, ephemeral=True)


# --- 通常コマンド: report ---
@tree.command(name="report", description="サーバーのルール違反を匿名で管理者に報告します。")
@app_commands.describe(
    target_user="報告したい相手",
    violated_rule="違反したと思われるルール",
    urgency="報告の緊急度を選択してください。",
    details="（「その他」を選んだ場合は必須）具体的な状況を教えてください。",
    message_link="証拠となるメッセージのリンク（任意）"
)
@app_commands.choices(
    violated_rule=[
        app_commands.Choice(name="そのいち：ひとをきずつけない 💔", value="そのいち：ひとをきずつけない 💔"),
        app_commands.Choice(name="そのに：ひとのいやがることをしない 🚫", value="そのに：ひとのいやがることをしない 🚫"),
        app_commands.Choice(name="そのさん：かってにフレンドにならない 👥", value="そのさん：かってにフレンドにならない 👥"),
        app_commands.Choice(name="そのよん：くすりのなまえはかきません 💊", value="そのよん：くすりのなまえはかきません 💊"),
        app_commands.Choice(name="そのご：あきらかなせんでんこういはしません 📢", value="そのご：あきらかなせんでんこういはしません 📢"),
        app_commands.Choice(name="その他：上記以外の違反", value="その他"),
    ],
    urgency=[
        app_commands.Choice(name="低：通常の違反報告", value="低"),
        app_commands.Choice(name="中：早めの対応が必要", value="中"),
        app_commands.Choice(name="高：即座の対応が必要", value="高"),
    ]
)
async def report(
    interaction: discord.Interaction,
    target_user: discord.User,
    violated_rule: app_commands.Choice[str],
    urgency: app_commands.Choice[str],
    details: str = None,
    message_link: str = None
):
    if violated_rule.value == "その他" and not details:
        await interaction.response.send_message("「その他」を選んだ場合は、具体的な状況を `details` に入力してください。", ephemeral=True)
        return
        
    if REPORT_CHANNEL_ID == 0:
        await interaction.response.send_message("現在、ボットが設定中のため通報機能を利用できません。", ephemeral=True)
        return

    try:
        report_id = await db.create_report(
            interaction.guild.id, target_user.id, violated_rule.value, details, message_link, urgency.value
        )
        report_channel = client.get_channel(REPORT_CHANNEL_ID)
        if not report_channel:
            await interaction.response.send_message("報告用チャンネルが見つかりません。", ephemeral=True)
            return

        embed_color = discord.Color.greyple()
        title_prefix = "📝"
        content = None

        if urgency.value == "中":
            embed_color = discord.Color.orange()
            title_prefix = "⚠️"
        elif urgency.value == "高":
            embed_color = discord.Color.red()
            title_prefix = "🚨"
            if URGENT_ROLE_ID != 0:
                role = interaction.guild.get_role(URGENT_ROLE_ID)
                if role:
                    content = f"{role.mention} 緊急の報告です！"
                else:
                    logging.warning(f"緊急メンション用のロール(ID: {URGENT_ROLE_ID})が見つかりません。")

        embed = discord.Embed(
            title=f"{title_prefix} 新規の匿名報告 (ID: {report_id})",
            color=embed_color
        )
        embed.add_field(name="👤 報告対象者", value=f"{target_user.mention} ({target_user.id})", inline=False)
        embed.add_field(name="📜 違反したルール", value=violated_rule.value, inline=False)
        embed.add_field(name="🔥 緊急度", value=urgency.value, inline=False)
        if details: embed.add_field(name="📝 詳細", value=details, inline=False)
        if message_link: embed.add_field(name="🔗 関連メッセージ", value=message_link, inline=False)
        embed.add_field(name="📊 ステータス", value="未対応", inline=False)
        embed.set_footer(text="この報告は匿名で送信されました。")

        sent_message = await report_channel.send(content=content, embed=embed)
        await db.update_report_message_id(report_id, sent_message.id)
        await interaction.response.send_message("通報を受け付けました。ご協力ありがとうございます。", ephemeral=True)
        logging.info(f"新規通報(ID:{report_id})を受信。対象: {target_user.name}")

    except discord.Forbidden:
        await interaction.response.send_message("エラー: ボットが報告用チャンネルにメッセージを送信する権限がありません。", ephemeral=True)
    except Exception as e:
        await interaction.response.send_message(f"不明なエラーが発生しました: {e}", ephemeral=True)
        logging.error(f"通報処理中にエラー: {e}", exc_info=True)


# --- 起動処理 ---
def main():
    # 作ったコマンドグループをBotに登録
    tree.add_command(report_manage_group)

    # Webサーバーを起動
    flask_thread = threading.Thread(target=run_flask)
    flask_thread.start()
    
    # Botを起動
    if not TOKEN:
        logging.error("❌ TOKENが設定されていません！")
        return
    client.run(TOKEN)

if __name__ == "__main__":
    main()
