import discord
from discord import app_commands, ui
import os
import threading
import logging
import datetime
from dotenv import load_dotenv
from flask import Flask
import database as db

# --- 初期設定 ---
load_dotenv()  # 環境変数（.env）からSupabaseデータベース接続情報を読み込み
logging.basicConfig(level=logging.INFO)

# --- 定数 ---
TOKEN = os.getenv("DISCORD_BOT_TOKEN")
COOLDOWN_MINUTES = 5 # クールダウン時間（分）
REPORT_BUTTON_CHANNEL_ID = 1399405974841852116  # ボタン式報告専用チャンネルID（変更したい場合はここを修正）
WARNING_CHANNEL_ID = 1399405974841852116  # 警告発行時の報告先チャンネルID
ADMIN_ONLY_CHANNEL_ID = 1388167902808637580  # 管理者のみ報告時のチャンネルID
RULE_ANNOUNCEMENT_LINK = "https://discord.com/channels/1300291307314610316/1377465336076566578"  # ルールアナウンスチャンネルのリンク

# --- Discord Botの準備 ---
intents = discord.Intents.default()
intents.members = True  # サーバーメンバー情報の取得に必要
intents.guilds = True   # ギルド情報の取得に必要
client = discord.Client(intents=intents)
tree = app_commands.CommandTree(client)

# --- スリープ対策Webサーバー ---
app = Flask(__name__)
@app.route('/')
def home(): return "Shugoshin Bot is watching over you."
@app.route('/health')
def health_check(): return "OK"
def run_flask():
    port = int(os.getenv("PORT", 8080))
    app.run(host='0.0.0.0', port=port)

# --- Botのイベント ---
@client.event
async def on_ready():
    # Supabaseローカル環境で守護神ボット用テーブルを初期化
    await db.init_shugoshin_db()
    
    # 永続ビューを追加（ボット再起動後もボタンが動作するように）
    client.add_view(ReportStartView())
    
    await tree.sync()
    logging.info(f"✅ 守護神ボットが起動しました: {client.user}")
    
    # 報告用ボタンをチャンネルに送信
    await setup_report_button()

async def setup_report_button():
    """報告用ボタンを特定のチャンネルに設置する"""
    try:
        channel = client.get_channel(REPORT_BUTTON_CHANNEL_ID)
        if not channel:
            logging.error(f"チャンネルID {REPORT_BUTTON_CHANNEL_ID} が見つかりません")
            return
            
        logging.info(f"チャンネル '{channel.name}' (ID: {channel.id}) への報告ボタン設置を試行中...")
        
        # ボットの権限チェック
        permissions = channel.permissions_for(channel.guild.me)
        if not permissions.send_messages:
            logging.error(f"チャンネル '{channel.name}' にメッセージ送信権限がありません")
            return
            
        # 既存のボタンメッセージを探す（新しいメッセージを無限に作らないように）
        async for message in channel.history(limit=50):
            if message.author == client.user and message.embeds:
                embed = message.embeds[0]
                if embed.title and "報告システム" in embed.title:
                    # 既存のボタンメッセージがあるので、新しく作らない
                    logging.info(f"既存の報告ボタンが見つかりました (メッセージID: {message.id})")
                    return
        
        # 新しい報告ボタンメッセージを作成
        embed = discord.Embed(
            title="🛡️ 守護神ボット 報告システム",
            description="サーバーのルール違反を匿名で管理者に報告できます。\n下のボタンをクリックして報告を開始してください。",
            color=discord.Color.blue()
        )
        embed.add_field(
            name="📋 報告の流れ", 
            value="① 報告開始ボタンをクリック\n② 対象者を選択\n③ 違反ルールを選択\n④ 緊急度を選択\n⑤ 詳細情報を入力\n⑥ 最終確認・送信", 
            inline=False
        )
        embed.set_footer(text="報告は完全に匿名で処理されます")
        
        view = ReportStartView()
        sent_message = await channel.send(embed=embed, view=view)
        logging.info(f"報告用ボタンを設置しました (メッセージID: {sent_message.id})")
        
    except discord.Forbidden:
        logging.error(f"チャンネルID {REPORT_BUTTON_CHANNEL_ID} にメッセージを送信する権限がありません")
    except Exception as e:
        logging.error(f"報告ボタンの設置に失敗: {e}", exc_info=True)

# --- 確認ボタン付きView ---
class ConfirmWarningView(ui.View):
    def __init__(self, *, interaction: discord.Interaction):
        super().__init__(timeout=60)
        self.original_interaction = interaction
        self.confirmed = None

    async def interaction_check(self, interaction: discord.Interaction) -> bool:
        if interaction.user.id != self.original_interaction.user.id:
            await interaction.response.send_message("これはあなたのためのボタンではありません。", ephemeral=True)
            return False
        return True

    @ui.button(label="はい、警告を発行する", style=discord.ButtonStyle.danger)
    async def confirm(self, interaction: discord.Interaction, button: ui.Button):
        self.confirmed = True
        for item in self.children:
            item.disabled = True
        await interaction.response.edit_message(content="処理中です...", view=self)
        self.stop()

    @ui.button(label="いいえ、やめておく", style=discord.ButtonStyle.secondary)
    async def cancel(self, interaction: discord.Interaction, button: ui.Button):
        self.confirmed = False
        for item in self.children:
            item.disabled = True
        await interaction.response.edit_message(content="警告の発行をキャンセルしました。", view=None)
        self.stop()

# --- ボタンベースの報告システム用View ---
class ReportStartView(ui.View):
    """報告を開始するボタン"""
    def __init__(self):
        super().__init__(timeout=None)  # 永続化

    @ui.button(label="📝 報告を開始する", style=discord.ButtonStyle.primary, emoji="🛡️", custom_id="report_start_button")
    async def start_report(self, interaction: discord.Interaction, button: ui.Button):
        # 最初に即座に応答して、その後でクールダウンチェックを行う
        await interaction.response.defer(ephemeral=True)
        
        try:
            # クールダウンチェック
            remaining_time = await db.check_cooldown(interaction.user.id, COOLDOWN_MINUTES * 60)
            if remaining_time > 0:
                await interaction.followup.send(
                    f"⏰ クールダウン中です。あと `{int(remaining_time // 60)}分 {int(remaining_time % 60)}秒` 待ってください。", 
                    ephemeral=True
                )
                return
            
            # 報告データを初期化
            report_data = ReportData()
            view = TargetUserSelectView(report_data)
            
            embed = discord.Embed(
                title="👤 報告対象者の選択",
                description="報告したい相手を選択してください。\n\n**使い方:**\n• 上のセレクトメニューから直接ユーザーを選択（最近アクティブなユーザーのみ表示）\n• または「🔍 ユーザーを検索」ボタンでユーザー名やIDを入力",
                color=discord.Color.orange()
            )
            embed.add_field(
                name="💡 ヒント",
                value="セレクトメニューに目的のユーザーが表示されない場合は、「🔍 ユーザーを検索」ボタンをご利用ください。",
                inline=False
            )
            embed.set_footer(text="ステップ 1/5 | 5分でタイムアウトします")
            
            await interaction.followup.send(embed=embed, view=view, ephemeral=True)
            
        except Exception as e:
            logging.error(f"報告開始ボタンでエラー: {e}", exc_info=True)
            await interaction.followup.send("❌ 報告システムでエラーが発生しました。しばらく待ってから再試行してください。", ephemeral=True)

class ReportData:
    """報告データを保持するクラス"""
    def __init__(self):
        self.target_user = None
        self.violated_rule = None
        self.urgency = None
        self.issue_warning = False
        self.details = None
        self.message_link = None

class TargetUserSelectView(ui.View):
    """対象ユーザー選択用のView"""
    def __init__(self, report_data: ReportData):
        super().__init__(timeout=300)  # 5分に延長
        self.report_data = report_data

    @ui.select(
        cls=ui.UserSelect,
        placeholder="報告対象のユーザーを選択してください",
        min_values=1,
        max_values=1
    )
    async def select_user(self, interaction: discord.Interaction, select: ui.UserSelect):
        """ユーザー選択時の処理"""
        selected_user = select.values[0]
        self.report_data.target_user = selected_user
        
        # 次のステップへ
        view = RuleSelectView(self.report_data)
        embed = discord.Embed(
            title="📜 違反ルールの選択",
            description=f"**報告対象者:** {selected_user.mention}\n\n違反したルールを選択してください:",
            color=discord.Color.orange()
        )
        embed.set_footer(text="ステップ 2/5")
        
        await interaction.response.edit_message(embed=embed, view=view)

    @ui.button(label="🔍 ユーザーを検索", style=discord.ButtonStyle.secondary)
    async def input_user_manually(self, interaction: discord.Interaction, button: ui.Button):
        """手動でユーザーIDやメンションを入力する場合"""
        modal = UserInputModal(self.report_data)
        await interaction.response.send_modal(modal)

class UserInputModal(ui.Modal):
    """ユーザー入力用のモーダル"""
    def __init__(self, report_data: ReportData):
        super().__init__(title="ユーザー検索")
        self.report_data = report_data

    user_input = ui.TextInput(
        label="報告対象者",
        placeholder="ユーザー名、表示名、@メンション、またはユーザーIDを入力してください",
        required=True,
        max_length=200,
        style=discord.TextStyle.short
    )

    async def on_submit(self, interaction: discord.Interaction):
        await interaction.response.defer(ephemeral=True)
        user_input_text = self.user_input.value.strip()
        
        try:
            target_user = None
            
            # 1. メンションからユーザーIDを抽出
            if user_input_text.startswith('<@') and user_input_text.endswith('>'):
                user_id_str = user_input_text[2:-1]
                if user_id_str.startswith('!'):
                    user_id_str = user_id_str[1:]
                try:
                    user_id = int(user_id_str)
                    target_user = await interaction.client.fetch_user(user_id)
                except (ValueError, discord.NotFound):
                    pass
            
            # 2. 数字のみの場合はユーザーIDとして処理
            elif user_input_text.isdigit():
                try:
                    user_id = int(user_input_text)
                    target_user = await interaction.client.fetch_user(user_id)
                except discord.NotFound:
                    pass
            
            # 3. ユーザー名や表示名で検索（改善版）
            if not target_user:
                guild = interaction.guild
                search_term = user_input_text.strip()  # 前後の空白を削除
                search_term_lower = search_term.lower()
                
                # 候補者を格納するリスト
                exact_matches = []      # 完全一致
                startswith_matches = [] # 前方一致
                partial_matches = []    # 部分一致
                
                # サーバーメンバーから検索
                for member in guild.members:
                    # ボット除外
                    if member.bot:
                        continue
                        
                    member_name = member.name.lower()
                    member_display = member.display_name.lower()
                    
                    # 完全一致チェック（最優先）
                    if (member_name == search_term_lower or 
                        member_display == search_term_lower or
                        member.name == search_term or
                        member.display_name == search_term):
                        exact_matches.append(member)
                        continue
                    
                    # 前方一致チェック（2番目の優先度）
                    if (member_name.startswith(search_term_lower) or 
                        member_display.startswith(search_term_lower)):
                        startswith_matches.append(member)
                        continue
                    
                    # 部分一致チェック（3番目の優先度）
                    if (search_term_lower in member_name or 
                        search_term_lower in member_display):
                        partial_matches.append(member)
                
                # 結果の選択（完全一致 > 前方一致 > 部分一致の順）
                if exact_matches:
                    target_user = exact_matches[0]
                elif startswith_matches:
                    target_user = startswith_matches[0]
                elif partial_matches:
                    target_user = partial_matches[0]
                
                # デバッグ情報をログに出力
                logging.info(f"ユーザー検索: '{user_input_text}' -> 完全一致:{len(exact_matches)}件, 前方一致:{len(startswith_matches)}件, 部分一致:{len(partial_matches)}件")
            
            if target_user:
                self.report_data.target_user = target_user
                
                # 次のステップへ
                view = RuleSelectView(self.report_data)
                embed = discord.Embed(
                    title="📜 違反ルールの選択",
                    description=f"**報告対象者:** {target_user.mention}\n\n違反したルールを選択してください:",
                    color=discord.Color.orange()
                )
                embed.set_footer(text="ステップ 2/5")
                
                await interaction.edit_original_response(embed=embed, view=view)
            else:
                # 検索に失敗した場合の詳細診断情報
                guild = interaction.guild
                member_count = guild.member_count  # Discord公式メンバー数
                member_list = [member for member in guild.members]  # 実際に取得できたメンバー
                member_list_count = len(member_list)
                
                # Intent設定の確認
                intents_status = f"members:{client.intents.members}, guilds:{client.intents.guilds}"
                
                # 類似ユーザー名を探す（最大10件、改善版）
                similar_users = []
                search_term_lower = user_input_text.lower().strip()
                
                # 検索候補を作成
                candidates = []
                for member in member_list:
                    if member.bot:  # ボットを除外
                        continue
                        
                    member_name = member.name.lower()
                    member_display = member.display_name.lower()
                    
                    # より柔軟な類似検索
                    similarity_score = 0
                    
                    # 部分一致のスコア計算
                    for term_char in search_term_lower:
                        if term_char in member_name:
                            similarity_score += 1
                        if term_char in member_display:
                            similarity_score += 1
                    
                    # 前方一致ボーナス
                    if member_name.startswith(search_term_lower[:2]) or member_display.startswith(search_term_lower[:2]):
                        similarity_score += 5
                    
                    if similarity_score > 0:
                        candidates.append((similarity_score, member))
                
                # スコア順でソートして上位10件を取得
                candidates.sort(key=lambda x: x[0], reverse=True)
                for score, member in candidates[:10]:
                    similar_users.append(f"• {member.display_name} (@{member.name}) - ID: {member.id}")
                
                error_message = f"❌ 「{user_input_text}」に一致するユーザーが見つかりませんでした。\n\n"
                error_message += f"**サーバー診断:**\n"
                error_message += f"• Discord公式メンバー数: {member_count}人\n"
                error_message += f"• 実際に取得できた数: {member_list_count}人\n"
                error_message += f"• Intent設定: {intents_status}\n\n"
                
                # メンバー数が異常に少ない場合の警告
                if member_list_count == 1:
                    error_message += "⚠️ **メンバー情報取得エラー**\n"
                    error_message += "Discord Developer Portalで以下を確認してください：\n"
                    error_message += "1. SERVER MEMBERS INTENTが有効か\n"
                    error_message += "2. GUILDS INTENTが有効か\n\n"
                elif member_list_count < member_count * 0.5:  # 半分以下の場合
                    error_message += "⚠️ **メンバー情報が不完全**\n"
                    error_message += "一部のメンバー情報が取得できていません。\n\n"
                
                if similar_users:
                    error_message += "**類似するユーザー名:**\n" + "\n".join(similar_users) + "\n\n"
                
                error_message += ("**検索のコツ:**\n"
                                "• 日本語のユーザー名も正しく検索できます\n"
                                "• ユーザー名の一部だけでも検索可能です\n"
                                "• 表示名（ニックネーム）も検索対象です\n"
                                "• ユーザーIDを直接入力することもできます\n"
                                "• @メンションをコピーして貼り付けることもできます\n"
                                "• そのユーザーがこのサーバーのメンバーか確認してください")
                
                await interaction.followup.send(error_message, ephemeral=True)
                
        except Exception as e:
            logging.error(f"ユーザー検索エラー: {e}", exc_info=True)
            await interaction.followup.send(f"❌ ユーザー検索中にエラーが発生しました: {e}", ephemeral=True)

class RuleSelectView(ui.View):
    """ルール選択用のView"""
    def __init__(self, report_data: ReportData):
        super().__init__(timeout=300)  # 5分に延長
        self.report_data = report_data

    @ui.select(
        placeholder="違反したルールを選択してください",
        options=[
            discord.SelectOption(
                label="そのいち：ひとのいやがること・傷つくことはしない",
                description="侮辱・差別・暴言・しつこいDM等",
                emoji="🟥",
                value="そのいち：ひとのいやがること・傷つくことはしない 🟥"
            ),
            discord.SelectOption(
                label="そのに：かってにフレンドにならない",
                description="フレンド申請は相手の同意が必須",
                emoji="🤝",
                value="そのに：かってにフレンドにならない 🤝"
            ),
            discord.SelectOption(
                label="そのさん：くすりのなまえはかきません",
                description="薬の名前を書く・口に出すのは避ける",
                emoji="💊",
                value="そのさん：くすりのなまえはかきません 💊"
            ),
            discord.SelectOption(
                label="その他の違反",
                description="上記以外のルール違反",
                emoji="❓",
                value="その他"
            ),
        ]
    )
    async def rule_select(self, interaction: discord.Interaction, select: ui.Select):
        self.report_data.violated_rule = select.values[0]
        
        # 次のステップへ
        view = UrgencySelectView(self.report_data)
        embed = discord.Embed(
            title="🔥 緊急度の選択",
            description=f"**報告対象者:** {self.report_data.target_user.mention}\n**違反ルール:** {self.report_data.violated_rule}\n\n緊急度を選択してください:",
            color=discord.Color.orange()
        )
        embed.set_footer(text="ステップ 3/5")
        
        await interaction.response.edit_message(embed=embed, view=view)

    @ui.button(label="❌ キャンセル", style=discord.ButtonStyle.danger, row=1)
    async def cancel_report(self, interaction: discord.Interaction, button: ui.Button):
        """報告をキャンセルする"""
        embed = discord.Embed(
            title="❌ 報告をキャンセルしました",
            description="報告はキャンセルされました。",
            color=discord.Color.red()
        )
        await interaction.response.edit_message(embed=embed, view=None)

class UrgencySelectView(ui.View):
    """緊急度選択用のView"""
    def __init__(self, report_data: ReportData):
        super().__init__(timeout=300)  # 5分に延長
        self.report_data = report_data

    @ui.select(
        placeholder="緊急度を選択してください",
        options=[
            discord.SelectOption(
                label="低：通常の違反報告",
                description="通常の処理で問題ありません",
                emoji="🟢",
                value="低"
            ),
            discord.SelectOption(
                label="中：早めの対応が必要",
                description="早めの確認をお願いします",
                emoji="🟡",
                value="中"
            ),
            discord.SelectOption(
                label="高：即座の対応が必要",
                description="緊急で対応が必要です",
                emoji="🔴",
                value="高"
            ),
        ]
    )
    async def urgency_select(self, interaction: discord.Interaction, select: ui.Select):
        self.report_data.urgency = select.values[0]
        
        # 次のステップへ
        view = WarningSelectView(self.report_data)
        embed = discord.Embed(
            title="⚠️ 警告発行の選択",
            description=f"**報告対象者:** {self.report_data.target_user.mention}\n**違反ルール:** {self.report_data.violated_rule}\n**緊急度:** {self.report_data.urgency}\n\n対象者に警告を発行しますか？",
            color=discord.Color.orange()
        )
        embed.add_field(
            name="⚠️ 注意",
            value="警告を発行すると、報告チャンネルで対象者にメンションが送られます。\nタイミングから通報者が特定される可能性があります。",
            inline=False
        )
        embed.set_footer(text="ステップ 4/5")
        
        await interaction.response.edit_message(embed=embed, view=view)

    @ui.button(label="❌ キャンセル", style=discord.ButtonStyle.danger, row=1)
    async def cancel_report(self, interaction: discord.Interaction, button: ui.Button):
        """報告をキャンセルする"""
        embed = discord.Embed(
            title="❌ 報告をキャンセルしました",
            description="報告はキャンセルされました。",
            color=discord.Color.red()
        )
        await interaction.response.edit_message(embed=embed, view=None)

class WarningSelectView(ui.View):
    """警告発行選択用のView"""
    def __init__(self, report_data: ReportData):
        super().__init__(timeout=300)  # 5分に延長
        self.report_data = report_data

    @ui.button(label="はい、警告を発行する", style=discord.ButtonStyle.danger, emoji="⚠️")
    async def issue_warning(self, interaction: discord.Interaction, button: ui.Button):
        self.report_data.issue_warning = True
        await self._proceed_to_details(interaction)

    @ui.button(label="いいえ、管理者にのみ報告", style=discord.ButtonStyle.secondary, emoji="🤐")
    async def no_warning(self, interaction: discord.Interaction, button: ui.Button):
        self.report_data.issue_warning = False
        await self._proceed_to_details(interaction)

    @ui.button(label="❌ キャンセル", style=discord.ButtonStyle.danger, row=1)
    async def cancel_report(self, interaction: discord.Interaction, button: ui.Button):
        """報告をキャンセルする"""
        embed = discord.Embed(
            title="❌ 報告をキャンセルしました",
            description="報告はキャンセルされました。",
            color=discord.Color.red()
        )
        await interaction.response.edit_message(embed=embed, view=None)

    async def _proceed_to_details(self, interaction: discord.Interaction):
        """詳細入力ステップへ進む"""
        modal = DetailsInputModal(self.report_data)
        await interaction.response.send_modal(modal)

class DetailsInputModal(ui.Modal):
    """詳細情報入力用のモーダル"""
    def __init__(self, report_data: ReportData):
        super().__init__(title="報告の詳細情報")
        self.report_data = report_data

    details = ui.TextInput(
        label="詳しい状況（任意）",
        placeholder="何があったのか、詳しく教えてください。「その他」を選んだ場合は必須です。",
        style=discord.TextStyle.long,
        required=False,
        max_length=1000
    )

    message_link = ui.TextInput(
        label="証拠となるメッセージのリンク（任意）",
        placeholder="https://discord.com/channels/...",
        required=False,
        max_length=200
    )

    async def on_submit(self, interaction: discord.Interaction):
        self.report_data.details = self.details.value if self.details.value else None
        self.report_data.message_link = self.message_link.value if self.message_link.value else None
        
        # 「その他」を選んだ場合、詳細が必須
        if self.report_data.violated_rule == "その他" and not self.report_data.details:
            await interaction.response.send_message(
                "❌ 「その他」のルール違反を選んだ場合、詳細な状況の入力が必要です。", 
                ephemeral=True
            )
            return
        
        # 最終確認ステップへ
        view = FinalConfirmView(self.report_data)
        embed = discord.Embed(
            title="✅ 最終確認",
            description="以下の内容で報告を送信します。よろしいですか？",
            color=discord.Color.green()
        )
        embed.add_field(name="👤 報告対象者", value=self.report_data.target_user.mention, inline=False)
        embed.add_field(name="📜 違反ルール", value=self.report_data.violated_rule, inline=False)
        embed.add_field(name="🔥 緊急度", value=self.report_data.urgency, inline=False)
        embed.add_field(name="⚠️ 警告発行", value="はい" if self.report_data.issue_warning else "いいえ", inline=False)
        if self.report_data.details:
            embed.add_field(name="📝 詳細", value=self.report_data.details[:500] + ("..." if len(self.report_data.details) > 500 else ""), inline=False)
        if self.report_data.message_link:
            embed.add_field(name="🔗 証拠リンク", value=self.report_data.message_link, inline=False)
        embed.set_footer(text="ステップ 5/5 | この報告は匿名で送信されます")
        
        await interaction.response.edit_message(embed=embed, view=view)

class FinalConfirmView(ui.View):
    """最終確認用のView"""
    def __init__(self, report_data: ReportData):
        super().__init__(timeout=300)  # 5分に延長
        self.report_data = report_data

    @ui.button(label="📤 報告を送信する", style=discord.ButtonStyle.success, emoji="✅")
    async def submit_report(self, interaction: discord.Interaction, button: ui.Button):
        await interaction.response.defer(ephemeral=True)
        
        try:
            # 報告チャンネルを警告発行の有無で分岐
            if self.report_data.issue_warning:
                report_channel = client.get_channel(WARNING_CHANNEL_ID)
            else:
                report_channel = client.get_channel(ADMIN_ONLY_CHANNEL_ID)
            
            if not report_channel:
                await interaction.followup.send("❌ 報告先チャンネルが見つかりません。管理者に連絡してください。", ephemeral=True)
                return

            report_id = await db.create_report(
                interaction.guild.id, 
                self.report_data.target_user.id, 
                self.report_data.violated_rule, 
                self.report_data.details, 
                self.report_data.message_link, 
                self.report_data.urgency
            )
            
            # 埋め込みの色と絵文字を設定
            embed_color = discord.Color.greyple()
            title_prefix = "📝"
            content = None

            if self.report_data.urgency == "中":
                embed_color = discord.Color.orange()
                title_prefix = "⚠️"
            elif self.report_data.urgency == "高":
                embed_color = discord.Color.red()
                title_prefix = "🚨"
                # 緊急時のロールメンションは設定から取得（将来的に設定可能にする場合のため）
                # content = f"@everyone 緊急の報告です！"  # 必要に応じてコメントアウト解除
            
            # 報告種別を表示に追加
            report_type = "警告付き報告" if self.report_data.issue_warning else "管理者のみ報告"
            
            embed = discord.Embed(title=f"{title_prefix} 新規の匿名報告 (ID: {report_id})", color=embed_color)
            embed.add_field(name="👤 報告対象者", value=f"{self.report_data.target_user.mention} ({self.report_data.target_user.id})", inline=False)
            embed.add_field(name="📜 違反したルール", value=self.report_data.violated_rule, inline=False)
            embed.add_field(name="🔥 緊急度", value=self.report_data.urgency, inline=False)
            embed.add_field(name="📋 報告種別", value=report_type, inline=False)
            if self.report_data.details: 
                embed.add_field(name="📝 詳細", value=self.report_data.details, inline=False)
            if self.report_data.message_link: 
                embed.add_field(name="🔗 関連メッセージ", value=self.report_data.message_link, inline=False)
            embed.add_field(name="📊 ステータス", value="未対応", inline=False)
            embed.set_footer(text="この報告は匿名で送信されました（ボタン式報告）")

            sent_message = await report_channel.send(content=content, embed=embed)
            await db.update_report_message_id(report_id, sent_message.id)

            # 警告を発行する場合（警告チャンネルでのみ実行）
            if self.report_data.issue_warning:
                warning_message = (
                    f"{self.report_data.target_user.mention}\n\n"
                    f"⚠️ **サーバー管理者からのお知らせです** ⚠️\n"
                    f"━━━━━━━━━━━━━━━━━━━━━━\n"
                    f"あなたの行動について、サーバーのルールに関する報告が寄せられました。\n\n"
                    f"**該当ルール:** [✅ルール](<{RULE_ANNOUNCEMENT_LINK}>)\n\n"
                    f"みんなが楽しく過ごせるよう、今一度ルールの確認をお願いいたします。\n"
                    f"ご不明な点があれば、このチャンネルで返信するか、管理者にDMを送ってください。\n"
                    f"━━━━━━━━━━━━━━━━━━━━━━"
                )
                await report_channel.send(warning_message)

            final_message = "✅ 報告を送信しました。ご協力ありがとうございます。"
            if self.report_data.issue_warning:
                final_message = "✅ 報告と警告発行を完了しました。ご協力ありがとうございます。"

            await interaction.followup.send(final_message, ephemeral=True)

        except Exception as e:
            logging.error(f"ボタン式報告処理中にエラー: {e}", exc_info=True)
            await interaction.followup.send(f"❌ 報告の送信中にエラーが発生しました: {e}", ephemeral=True)

    @ui.button(label="❌ キャンセル", style=discord.ButtonStyle.danger, row=1)
    async def cancel_report(self, interaction: discord.Interaction, button: ui.Button):
        embed = discord.Embed(
            title="❌ 報告をキャンセルしました",
            description="報告はキャンセルされました。",
            color=discord.Color.red()
        )
        await interaction.response.edit_message(embed=embed, view=None)

# --- スラッシュコマンド ---

# ★★★★★★★ 直接報告コマンド ★★★★★★★
@tree.command(name="syugoshin", description="サーバーのルール違反を匿名で管理者に報告します。")
@app_commands.describe(
    user="① 報告したい相手を選んでね",
    rule="② 違反したルールを選んでね",
    speed="③ 緊急度を選んでね",
    info="④ どんなことがあったか、くわしく書いてください（『その他』を選んだときは必ず書いてね）",
    message_link="⑤ 問題のあったメッセージのリンクがあれば貼ってください"
)
@app_commands.choices(
    rule=[
        app_commands.Choice(name="そのいち：ひとのいやがること・傷つくことはしない 🟥", value="そのいち：ひとのいやがること・傷つくことはしない 🟥"),
        app_commands.Choice(name="そのに：かってにフレンドにならない 🤝", value="そのに：かってにフレンドにならない 🤝"),
        app_commands.Choice(name="そのさん：くすりのなまえはかきません 💊", value="そのさん：くすりのなまえはかきません 💊"),
        app_commands.Choice(name="その他：上記以外の違反", value="その他"),
    ],
    speed=[
        app_commands.Choice(name="低：通常の違反報告", value="低"),
        app_commands.Choice(name="中：早めの対応が必要", value="中"),
        app_commands.Choice(name="高：即座の対応が必要", value="高"),
    ],
)
async def report(
    interaction: discord.Interaction,
    user: discord.User,
    rule: app_commands.Choice[str],
    speed: app_commands.Choice[str],
    info: str = None,
    message_link: str = None
):
    await interaction.response.defer(ephemeral=True)

    settings = await db.get_guild_settings(interaction.guild.id)
    if not settings or not settings.get('report_channel_id'):
        await interaction.followup.send("ボットの初期設定が完了していません。管理者が`/setup`で設定してください。", ephemeral=True)
        return

    remaining_time = await db.check_cooldown(interaction.user.id, COOLDOWN_MINUTES * 60)
    if remaining_time > 0:
        await interaction.followup.send(f"クールダウン中です。あと `{int(remaining_time // 60)}分 {int(remaining_time % 60)}秒` 待ってください。", ephemeral=True)
        return

    
    try:
        report_id = await db.create_report(
            interaction.guild.id, user.id, rule.value, info, message_link, speed.value
        )
        
        report_channel = client.get_channel(settings['report_channel_id'])
        
        embed_color = discord.Color.greyple()
        title_prefix = "📝"
        content = None

        if speed.value == "中":
            embed_color = discord.Color.orange()
            title_prefix = "⚠️"
        elif speed.value == "高":
            embed_color = discord.Color.red()
            title_prefix = "🚨"
            if settings.get('urgent_role_id'):
                role = interaction.guild.get_role(settings['urgent_role_id'])
                if role: content = f"{role.mention} 緊急の報告です！"
        
        embed = discord.Embed(title=f"{title_prefix} 新規の匿名報告 (ID: {report_id})", color=embed_color)
        embed.add_field(name="👤 報告対象者", value=f"{user.mention} ({user.id})", inline=False)
        embed.add_field(name="📜 違反したルール", value=rule.value, inline=False)
        embed.add_field(name="🔥 緊急度", value=speed.value, inline=False)
        if info: embed.add_field(name="📝 詳細", value=info, inline=False)
        if message_link: embed.add_field(name="🔗 関連メッセージ", value=message_link, inline=False)
        embed.add_field(name="📊 ステータス", value="未対応", inline=False)
        embed.set_footer(text="この報告は匿名で送信されました。")

        sent_message = await report_channel.send(content=content, embed=embed)
        await db.update_report_message_id(report_id, sent_message.id)

        final_message = "通報を受け付けました。ご協力ありがとうございます。"

        await interaction.followup.send(final_message, ephemeral=True)

    except Exception as e:
        logging.error(f"通報処理中にエラー: {e}", exc_info=True)
        await interaction.followup.send(f"不明なエラーが発生しました: {e}", ephemeral=True)


# (/kanrinin グループ - 管理者用報告管理コマンド) - 一時的に非表示
# report_manage_group = app_commands.Group(name="kanrinin", description="報告を管理します。")

# @report_manage_group.command(name="status", description="報告のステータスを変更します。")
# @app_commands.describe(report_id="ステータスを変更したい報告のID", new_status="新しいステータス")
# @app_commands.choices(new_status=[app_commands.Choice(name="対応中", value="対応中"), app_commands.Choice(name="解決済み", value="解決済み"), app_commands.Choice(name="却下", value="却下"),])
# async def status(interaction: discord.Interaction, report_id: int, new_status: app_commands.Choice[str]):
#     await interaction.response.defer(ephemeral=True)
#     settings = await db.get_guild_settings(interaction.guild.id)
#     if not settings: return await interaction.followup.send("未設定です。`/setup`を実行してください。", ephemeral=True)
#     try:
#         report_data = await db.get_report(report_id)
#         if not report_data:
#             await interaction.followup.send(f"エラー: 報告ID `{report_id}` が見つかりません。", ephemeral=True)
#             return
#         report_channel = client.get_channel(settings['report_channel_id'])
#         original_message = await report_channel.fetch_message(report_data['message_id'])
#         original_embed = original_message.embeds[0]
#         status_colors = {"対応中": discord.Color.yellow(), "解決済み": discord.Color.green(), "却下": discord.Color.greyple()}
#         original_embed.color = status_colors.get(new_status.value)
#         for i, field in enumerate(original_embed.fields):
#             if field.name == "📊 ステータス":
#                 original_embed.set_field_at(i, name="📊 ステータス", value=new_status.value, inline=False)
#                 break
#         await original_message.edit(embed=original_embed)
#         await db.update_report_status(report_id, new_status.value)
#         await interaction.followup.send(f"報告ID `{report_id}` のステータスを「{new_status.value}」に変更しました。", ephemeral=True)
#     except Exception as e:
#         await interaction.followup.send(f"ステータス更新中にエラー: {e}", ephemeral=True)

# @report_manage_group.command(name="list", description="報告の一覧を表示します。")
# @app_commands.describe(filter="表示するステータスで絞り込みます。")
# @app_commands.choices(filter=[app_commands.Choice(name="すべて", value="all"), app_commands.Choice(name="未対応", value="未対応"), app_commands.Choice(name="対応中", value="対応中"),])
# async def list_reports_cmd(interaction: discord.Interaction, filter: app_commands.Choice[str] = None):
#     await interaction.response.defer(ephemeral=True)
#     status_filter = filter.value if filter else None
#     reports = await db.list_reports(status_filter)
#     if not reports:
#         await interaction.followup.send("該当する報告はありません。", ephemeral=True)
#         return
#     embed = discord.Embed(title=f"📜 報告リスト ({filter.name if filter else '最新'})", color=discord.Color.blue())
#     description = ""
#     for report in reports:
#         try:
#             target_user = await client.fetch_user(report['target_user_id'])
#             user_name = target_user.name
#         except discord.NotFound:
#             user_name = "不明なユーザー"
#         description += f"**ID: {report['report_id']}** | 対象: {user_name} | ステータス: `{report['status']}`\n"
#     embed.description = description
#     await interaction.followup.send(embed=embed, ephemeral=True)

# @report_manage_group.command(name="stats", description="報告の統計情報を表示します。")
# async def stats(interaction: discord.Interaction):
#     await interaction.response.defer(ephemeral=True)
#     stats_data = await db.get_report_stats()
#     total = sum(stats_data.values())
#     embed = discord.Embed(title="📈 報告統計", description=f"総報告数: **{total}** 件", color=discord.Color.purple())
#     unhandled = stats_data.get('未対応', 0)
#     in_progress = stats_data.get('対応中', 0)
#     resolved = stats_data.get('解決済み', 0)
#     rejected = stats_data.get('却下', 0)
#     embed.add_field(name="未対応 🔴", value=f"**{unhandled}** 件", inline=True)
#     embed.add_field(name="対応中 🟡", value=f"**{in_progress}** 件", inline=True)
#     embed.add_field(name="解決済み 🟢", value=f"**{resolved}** 件", inline=True)
#     embed.add_field(name="却下 ⚪", value=f"**{rejected}** 件", inline=True)
#     await interaction.followup.send(embed=embed, ephemeral=True)

# /kanrinin set サブグループを作成
# kanrinin_set_group = app_commands.Group(name="set", description="各種設定を行います。", parent=report_manage_group)

# @kanrinin_set_group.command(name="channel", description="【管理者用】指定したチャンネルに報告用フォームを設置します。")
# @app_commands.checks.has_permissions(administrator=True)
# @app_commands.describe(channel="報告フォームを設置するチャンネル")
# async def set_channel(interaction: discord.Interaction, channel: discord.TextChannel):
#     """指定したチャンネルに報告用ボタンを設置するコマンド"""
#     await interaction.response.defer(ephemeral=True)
#     
#     # ボットがメッセージを送信する権限があるかチェック
#     if not channel.permissions_for(interaction.guild.me).send_messages:
#         await interaction.followup.send(f"❌ {channel.mention} にメッセージを送信する権限がありません。", ephemeral=True)
#         return
#     
#     try:
#         # 既存のボタンメッセージを探す（新しいメッセージを無限に作らないように）
#         async for message in channel.history(limit=50):
#             if message.author == client.user and message.embeds:
#                 embed = message.embeds[0]
#                 if embed.title and "報告システム" in embed.title:
#                     # 既存のボタンメッセージがあるので、新しく作らない
#                     await interaction.followup.send(
#                         f"⚠️ {channel.mention} には既に報告ボタンが設置されています。\n"
#                         f"**既存メッセージID:** {message.id}",
#                         ephemeral=True
#                     )
#                     return
#         
#         # 新しい報告ボタンメッセージを作成
#         embed = discord.Embed(
#             title="🛡️ 守護神ボット 報告システム",
#             description="サーバーのルール違反を匿名で管理者に報告できます。\n下のボタンをクリックして報告を開始してください。",
#             color=discord.Color.blue()
#         )
#         embed.add_field(
#             name="📋 報告の流れ", 
#             value="① 報告開始ボタンをクリック\n② 対象者を選択\n③ 違反ルールを選択\n④ 緊急度を選択\n⑤ 詳細情報を入力\n⑥ 最終確認・送信", 
#             inline=False
#         )
#         embed.set_footer(text="報告は完全に匿名で処理されます")
#         
#         view = ReportStartView()
#         sent_message = await channel.send(embed=embed, view=view)
#         
#         await interaction.followup.send(
#             f"✅ 報告フォームを {channel.mention} に設置しました。\n"
#             f"**メッセージID:** {sent_message.id}\n"
#             f"**チャンネルID:** {channel.id}", 
#             ephemeral=True
#         )
#         
#         # 設置されたチャンネルIDをログに出力
#         logging.info(f"報告フォームを設置: チャンネル={channel.name}({channel.id})")
#         
#     except discord.Forbidden:
#         await interaction.followup.send(f"❌ {channel.mention} にメッセージを送信する権限がありません。", ephemeral=True)
#     except Exception as e:
#         logging.error(f"フォーム設置エラー: {e}")
#         await interaction.followup.send(f"❌ 報告フォームの設置に失敗しました: {e}", ephemeral=True)

# @set_channel.error
# async def set_channel_error(interaction: discord.Interaction, error: app_commands.AppCommandError):
#     if isinstance(error, app_commands.MissingPermissions):
#         await interaction.response.send_message("このコマンドはサーバーの管理者のみが実行できます。", ephemeral=True)
#     else:
#         await interaction.response.send_message(f"フォーム設置中にエラーが発生しました: {error}", ephemeral=True)

# @kanrinin_set_group.command(name="reportchannel", description="【管理者用】匿名報告の送信先チャンネルを設定します。")
# @app_commands.checks.has_permissions(administrator=True)
# @app_commands.describe(
#     report_channel="匿名報告が送信されるチャンネル",
#     urgent_role="緊急度「高」の際にメンションするロール（任意）"
# )
# async def set_reportchannel(interaction: discord.Interaction, report_channel: discord.TextChannel, urgent_role: discord.Role = None):
#     """匿名報告の送信先チャンネルを設定するコマンド"""
#     await interaction.response.defer(ephemeral=True)
#     
#     # ボットがメッセージを送信する権限があるかチェック
#     if not report_channel.permissions_for(interaction.guild.me).send_messages:
#         await interaction.followup.send(f"❌ {report_channel.mention} にメッセージを送信する権限がありません。", ephemeral=True)
#         return
#     
#     try:
#         role_id = urgent_role.id if urgent_role else None
#         await db.setup_guild(interaction.guild.id, report_channel.id, role_id)
#         role_mention = urgent_role.mention if urgent_role else "未設定"
#         
#         await interaction.followup.send(
#             f"✅ 報告先チャンネルを設定しました。\n"
#             f"**報告先チャンネル:** {report_channel.mention}\n"
#             f"**緊急メンション用ロール:** {role_mention}",
#             ephemeral=True
#         )
#         
#         # 設定をログに出力
#         logging.info(f"報告先チャンネルを設定: チャンネル={report_channel.name}({report_channel.id}), 緊急ロール={urgent_role.name if urgent_role else 'なし'}")
#         
#     except Exception as e:
#         logging.error(f"報告先チャンネル設定エラー: {e}")
#         await interaction.followup.send(f"❌ 報告先チャンネルの設定に失敗しました: {e}", ephemeral=True)

# @set_reportchannel.error
# async def set_reportchannel_error(interaction: discord.Interaction, error: app_commands.AppCommandError):
#     if isinstance(error, app_commands.MissingPermissions):
#         await interaction.response.send_message("このコマンドはサーバーの管理者のみが実行できます。", ephemeral=True)
#     else:
#         await interaction.response.send_message(f"報告先チャンネル設定中にエラーが発生しました: {error}", ephemeral=True)


# --- 起動処理 ---
def main():
    # tree.add_command(report_manage_group)  # 一時的に非表示
    flask_thread = threading.Thread(target=run_flask)
    flask_thread.start()
    client.run(TOKEN)

if __name__ == "__main__":
    main()
