package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/tomim/profile-bot/internal/database"
)

// onMessageCreate はメッセージ投稿時に実行されるハンドラー
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// bot自身のメッセージは無視
	if m.Author.Bot {
		return
	}

	// 自己紹介チャンネルへの投稿かチェック
	if m.ChannelID != b.Config.IntroductionChannelID {
		return
	}

	ctx := context.Background()

	// データベースに保存
	err := b.DB.SaveIntroduction(ctx, m.Author.ID, m.ChannelID, m.ID)
	if err != nil {
		slog.Error("Failed to save introduction", "error", err.Error(), "user_id", m.Author.ID)
		return
	}

	slog.Info("Saved introduction", "user", fmt.Sprintf("%s#%s", m.Author.Username, m.Author.Discriminator), "user_id", m.Author.ID)

	// 「自己紹介済み」ロールを付与
	go b.assignIntroducedRole(s, m.GuildID, m.Author.ID)
}

// onMessageUpdate は自己紹介チャンネルでメッセージが編集された時に実行されるハンドラー
// 本文はDiscord APIから都度取得しているため実質no-opだが、DB上のレコードを最新化しておく
func (b *Bot) onMessageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	if m.Author == nil || m.Author.Bot {
		return
	}

	if m.ChannelID != b.Config.IntroductionChannelID {
		return
	}

	ctx := context.Background()

	if err := b.DB.SaveIntroduction(ctx, m.Author.ID, m.ChannelID, m.ID); err != nil {
		slog.Error("Failed to update introduction on message edit", "error", err.Error(), "user_id", m.Author.ID)
	}
}

// onMessageDelete は自己紹介チャンネルでメッセージが削除された時に実行されるハンドラー
func (b *Bot) onMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	if m.ChannelID != b.Config.IntroductionChannelID {
		return
	}

	b.handleIntroductionMessageDeleted(s, m.GuildID, m.ID)
}

// onMessageDeleteBulk は自己紹介チャンネルでメッセージが一括削除された時に実行されるハンドラー
func (b *Bot) onMessageDeleteBulk(s *discordgo.Session, m *discordgo.MessageDeleteBulk) {
	if m.ChannelID != b.Config.IntroductionChannelID {
		return
	}

	for _, messageID := range m.Messages {
		b.handleIntroductionMessageDeleted(s, m.GuildID, messageID)
	}
}

// handleIntroductionMessageDeleted はDBを message_id で逆引きし、該当レコードを削除する。
// MessageDelete イベントには Author が含まれないことがあるため、DB側から user_id を特定する。
// 削除後、同ユーザーの自己紹介が他に残っていなければ「自己紹介済み」ロールを剥奪する。
func (b *Bot) handleIntroductionMessageDeleted(s *discordgo.Session, guildID, messageID string) {
	ctx := context.Background()

	intro, err := b.DB.GetIntroductionByMessageID(ctx, messageID)
	if err != nil {
		slog.Error("Failed to look up introduction by message id", "error", err.Error(), "message_id", messageID)
		return
	}
	if intro == nil {
		// このメッセージは自己紹介として保存されていない
		return
	}

	if err := b.DB.DeleteIntroductionByMessageID(ctx, messageID); err != nil {
		slog.Error("Failed to delete introduction", "error", err.Error(), "message_id", messageID, "user_id", intro.UserID)
		return
	}

	slog.Info("Deleted introduction due to message delete", "user_id", intro.UserID, "message_id", messageID)

	// 同ユーザーの自己紹介が他に残っていれば剥奪しない
	hasOther, err := b.DB.HasIntroduction(ctx, intro.UserID)
	if err != nil {
		slog.Error("Failed to check remaining introductions", "error", err.Error(), "user_id", intro.UserID)
		return
	}
	if hasOther {
		return
	}

	if guildID == "" {
		slog.Warn("Cannot remove introduced role: guild_id is empty", "user_id", intro.UserID)
		return
	}

	b.removeIntroducedRole(s, guildID, intro.UserID)
}

// onVoiceStateUpdate はVC入退室時に実行されるハンドラー
func (b *Bot) onVoiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	// ミュート/ミュート解除など、チャンネルが変わらないイベントは無視
	if vs.BeforeUpdate != nil && vs.BeforeUpdate.ChannelID == vs.ChannelID {
		return
	}

	// 退室 or 別チャンネルへの移動を検知したら、元VCへ送った入室通知を削除する
	if b.Config.DeleteOnLeave && vs.BeforeUpdate != nil && vs.BeforeUpdate.ChannelID != "" &&
		vs.BeforeUpdate.ChannelID != vs.ChannelID && contains(b.Config.TargetVoiceChannels, vs.BeforeUpdate.ChannelID) {
		go b.deleteTrackedVCMessages(s, vs.GuildID, vs.UserID)
	}

	// 退室の場合は新規入室先がないためここで終了
	if vs.ChannelID == "" {
		return
	}

	// 対象VCかチェック
	if !contains(b.Config.TargetVoiceChannels, vs.ChannelID) {
		return
	}

	// 除外ユーザーかチェック
	if contains(b.Config.ExcludedUserIDs, vs.UserID) {
		slog.Info("Excluded user joined VC, skipping", "user_id", vs.UserID)
		return
	}

	slog.Info("User joined VC", "user_id", vs.UserID, "channel_id", vs.ChannelID)

	// 直接VCチャットに送信を試み、失敗時のみ通知チャンネルへフォールバックする。
	go b.sendIntroductionToVoiceChat(s, vs.ChannelID, vs.Member, vs.GuildID, b.Config.NotificationChannelID)
}

// vcMessageKey はguildIDとuserIDから追跡マップのキーを生成する
func vcMessageKey(guildID, userID string) string {
	return guildID + ":" + userID
}

// trackVCMessage はVC入室通知として送信したメッセージを追跡マップに記録する
// DELETE_ON_LEAVEが無効な場合は何もしない
func (b *Bot) trackVCMessage(guildID, userID, channelID, messageID string) {
	if !b.Config.DeleteOnLeave {
		return
	}

	b.vcMessagesMu.Lock()
	defer b.vcMessagesMu.Unlock()

	key := vcMessageKey(guildID, userID)
	b.vcMessages[key] = append(b.vcMessages[key], sentMessage{ChannelID: channelID, MessageID: messageID})
}

// deleteTrackedVCMessages はユーザーの退室/移動時に、追跡していた入室通知メッセージを削除する
func (b *Bot) deleteTrackedVCMessages(s *discordgo.Session, guildID, userID string) {
	key := vcMessageKey(guildID, userID)

	b.vcMessagesMu.Lock()
	messages := b.vcMessages[key]
	delete(b.vcMessages, key)
	b.vcMessagesMu.Unlock()

	for _, msg := range messages {
		if err := s.ChannelMessageDelete(msg.ChannelID, msg.MessageID); err != nil {
			slog.Warn("Failed to delete tracked VC message", "error", err.Error(), "channel_id", msg.ChannelID, "message_id", msg.MessageID)
			continue
		}
		slog.Info("Deleted VC introduction message on leave", "user_id", userID, "channel_id", msg.ChannelID, "message_id", msg.MessageID)
	}
}

// sendIntroductionToVoiceChat はVCのテキストチャットに自己紹介を投稿する
func (b *Bot) sendIntroductionToVoiceChat(s *discordgo.Session, voiceChannelID string, member *discordgo.Member, guildID string, fallbackChannelID string) {
	ctx := context.Background()

	// VCチャンネル名を取得（フォールバックメッセージ用）
	vcName := "VC"
	if vcChannel, err := s.Channel(voiceChannelID); err == nil {
		vcName = vcChannel.Name
	}

	// 完全なメンバー情報をAPIから取得
	fullMember, err := s.GuildMember(guildID, member.User.ID)
	if err != nil {
		slog.Warn("Failed to fetch full member info, using partial info", "error", err.Error(), "user_id", member.User.ID)
		fullMember = member // フォールバック
	}

	// メンバー表示名の解決（優先順位: Nick > GlobalName > Username）
	username := fullMember.User.Username
	if fullMember.User.GlobalName != "" {
		username = fullMember.User.GlobalName
	}
	if fullMember.Nick != "" {
		username = fullMember.Nick
	}

	slog.Info("Preparing to send introduction to VC", "user", username, "voice_channel_id", voiceChannelID)

	// 自己紹介を取得
	intro, err := b.DB.GetIntroduction(ctx, member.User.ID)
	if err != nil {
		// DBエラーが発生した場合は未投稿扱いせず即終了（DB障害の可能性）
		slog.Error("DB error when getting introduction, aborting send", "error", err.Error(), "user_id", member.User.ID)
		return
	}

	// 自己紹介が存在しない場合は「未投稿」メッセージを作成
	if intro == nil {
		message := fmt.Sprintf("━━━━━━━━━━━━━━━━━━━\n👤 %s さんが入室しました\n\n⚠️ この方の自己紹介はまだ投稿されていません\n━━━━━━━━━━━━━━━━━━━", username)

		// まずVCチャットへ送信を試みる
		if sent, err := s.ChannelMessageSend(voiceChannelID, message); err == nil {
			b.trackVCMessage(guildID, member.User.ID, voiceChannelID, sent.ID)
			slog.Info("Sent 'no introduction' message to VC", "voice_channel_id", voiceChannelID, "user", username)
			return
		} else {
			// 失敗した場合のみ通知チャンネルへフォールバック（VC名を先頭に付与）
			fallbackMessage := fmt.Sprintf("🔊 **%s** に入室しました\n\n%s", vcName, message)
			sent, err := s.ChannelMessageSend(fallbackChannelID, fallbackMessage)
			if err != nil {
				slog.Error("Failed to send 'no introduction' fallback message", "fallback_channel", fallbackChannelID, "error", err.Error())
				return
			}
			b.trackVCMessage(guildID, member.User.ID, fallbackChannelID, sent.ID)
			slog.Info("Sent 'no introduction' message to fallback channel", "fallback_channel", fallbackChannelID, "user", username)
			return
		}
	}

	// 自己紹介メッセージ内容を取得
	introMessage, err := s.ChannelMessage(intro.ChannelID, intro.MessageID)
	var introContent string
	if err == nil {
		introContent = introMessage.Content
		if len(introContent) > 1800 {
			introContent = introContent[:1800] + "..."
		}
	} else {
		slog.Warn("Failed to fetch introduction message content, using placeholder", "error", err.Error(), "user", username)
		introContent = "（自己紹介メッセージの取得に失敗しました）"
	}

	// ロール情報を取得
	roleInfo := b.getRoleInfo(s, fullMember, guildID)

	// アバターURLを取得（サイズ256で高品質）
	avatarURL := fullMember.User.AvatarURL("256")
	slog.Debug("Avatar URL", "user", username, "avatar_url", avatarURL)

	// Embed作成
	embed := b.createIntroductionEmbed(username, avatarURL, vcName, introContent, intro, roleInfo, guildID)

	// 元の自己紹介へのリンクボタンを作成
	introLink := fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, intro.ChannelID, intro.MessageID)
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label: "元の自己紹介を見る",
					Style: discordgo.LinkButton,
					URL:   introLink,
					Emoji: &discordgo.ComponentEmoji{
						Name: "📝",
					},
				},
			},
		},
	}

	// まずVCチャットへ送信を試みる
	if sent, err := s.ChannelMessageSendComplex(voiceChannelID, &discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}, Components: components}); err == nil {
		b.trackVCMessage(guildID, member.User.ID, voiceChannelID, sent.ID)
		slog.Info("Sent introduction embed to VC", "voice_channel_id", voiceChannelID, "user", username)
		return
	} else {
		// 失敗した場合のみ通知チャンネルへフォールバック（VC名を先頭に付与）
		fallbackPrefix := fmt.Sprintf("🔊 **%s** に入室しました\n\n", vcName)
		sent, err := s.ChannelMessageSendComplex(fallbackChannelID, &discordgo.MessageSend{Content: fallbackPrefix, Embeds: []*discordgo.MessageEmbed{embed}, Components: components})
		if err != nil {
			slog.Error("Failed to send introduction embed to fallback channel", "fallback_channel", fallbackChannelID, "error", err.Error())
			return
		}
		b.trackVCMessage(guildID, member.User.ID, fallbackChannelID, sent.ID)
		slog.Info("Sent introduction embed to fallback channel", "fallback_channel", fallbackChannelID, "user", username)
		return
	}
}

// getRoleInfo はメンバーのロール情報を取得する
func (b *Bot) getRoleInfo(s *discordgo.Session, member *discordgo.Member, guildID string) map[string][]string {
	roleInfo := make(map[string][]string)

	// ギルド情報を取得
	guild, err := s.Guild(guildID)
	if err != nil {
		slog.Error("Failed to get guild", "guild_id", guildID, "error", err.Error())
		return roleInfo
	}

	// メンバーのロール名を収集
	var roleNames []string
	for _, roleID := range member.Roles {
		for _, guildRole := range guild.Roles {
			if guildRole.ID == roleID {
				// 除外ロールチェック
				if !b.RolesConfig.IsExcludedRole(guildRole.Name, guildRole.Managed) {
					roleNames = append(roleNames, guildRole.Name)
				}
				break
			}
		}
	}

	// カテゴリ別に整理
	categorized := b.RolesConfig.CategorizeRoles(roleNames)
	for _, category := range categorized {
		var roles []string
		for _, role := range category.Roles {
			roles = append(roles, fmt.Sprintf("%s %s", role.Emoji, role.Name))
		}
		if len(roles) > 0 {
			roleInfo[category.DisplayName] = roles
		}
	}

	return roleInfo
}

// createIntroductionEmbed は自己紹介のEmbedを作成する（シンプル版）
func (b *Bot) createIntroductionEmbed(username, avatarURL, vcName, introContent string, intro *database.Introduction, roleInfo map[string][]string, guildID string) *discordgo.MessageEmbed {
	var description strings.Builder

	// ヘッダー
	description.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&description, "👤 %s さんが入室しました\n", username)

	// ロール情報がある場合
	if len(roleInfo) > 0 {
		description.WriteString("\n")

		// RolesConfigの順序でカテゴリを表示
		for _, category := range b.RolesConfig.RoleCategories {
			if roles, exists := roleInfo[category.DisplayName]; exists && len(roles) > 0 {
				fmt.Fprintf(&description, "%s\n", category.DisplayName)
				for _, role := range roles {
					fmt.Fprintf(&description, "%s\n", role)
				}
				description.WriteString("\n")
			}
		}

		description.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	}

	// 自己紹介本文（ヘッダーなし）
	description.WriteString("\n")
	description.WriteString(introContent)
	description.WriteString("\n\n")
	description.WriteString("━━━━━━━━━━━━━━━━━━━")

	embed := &discordgo.MessageEmbed{
		Description: description.String(),
		Color:       0x3498db, // 青色
	}

	if avatarURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: avatarURL,
		}
	}

	return embed
}

// findRoleIDByName はギルド内から指定ロール名のロールIDを検索する
func findRoleIDByName(guild *discordgo.Guild, roleName string) string {
	for _, role := range guild.Roles {
		if role.Name == roleName {
			return role.ID
		}
	}
	return ""
}

// assignIntroducedRole は「自己紹介済み」ロールを付与する
func (b *Bot) assignIntroducedRole(s *discordgo.Session, guildID, userID string) {
	// ギルド情報を取得
	guild, err := s.Guild(guildID)
	if err != nil {
		slog.Warn("Failed to get guild for role assignment", "error", err.Error())
		return
	}

	roleID := findRoleIDByName(guild, b.Config.IntroducedRoleName)
	if roleID == "" {
		slog.Warn("Role not found in guild", "role_name", b.Config.IntroducedRoleName)
		return
	}

	// ロールを付与
	err = s.GuildMemberRoleAdd(guildID, userID, roleID)
	if err != nil {
		slog.Warn("Failed to assign role", "error", err.Error())
		return
	}

	slog.Info("Assigned role to user", "role_name", b.Config.IntroducedRoleName, "user_id", userID)
}

// removeIntroducedRole は「自己紹介済み」ロールを剥奪する
func (b *Bot) removeIntroducedRole(s *discordgo.Session, guildID, userID string) {
	// ギルド情報を取得
	guild, err := s.Guild(guildID)
	if err != nil {
		slog.Warn("Failed to get guild for role removal", "error", err.Error())
		return
	}

	roleID := findRoleIDByName(guild, b.Config.IntroducedRoleName)
	if roleID == "" {
		slog.Warn("Role not found in guild", "role_name", b.Config.IntroducedRoleName)
		return
	}

	// ロールを剥奪
	err = s.GuildMemberRoleRemove(guildID, userID, roleID)
	if err != nil {
		slog.Warn("Failed to remove role", "error", err.Error())
		return
	}

	slog.Info("Removed role from user", "role_name", b.Config.IntroducedRoleName, "user_id", userID)
}

// contains は文字列スライスに指定の文字列が含まれているかチェックする
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
