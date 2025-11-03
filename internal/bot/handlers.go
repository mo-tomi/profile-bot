package bot

import (
	"context"
	"fmt"
	"log"
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
		log.Printf("❌ Failed to save introduction: %v", err)
		return
	}

	log.Printf("📝 Saved introduction from %s#%s (ID: %s)", m.Author.Username, m.Author.Discriminator, m.Author.ID)

	// 「自己紹介済み」ロールを付与
	go b.assignIntroducedRole(s, m.GuildID, m.Author.ID)
}

// onVoiceStateUpdate はVC入退室時に実行されるハンドラー
func (b *Bot) onVoiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
	// VC入室検知（BeforeUpdateがnilまたはチャンネルが変わった場合）
	if vs.BeforeUpdate != nil && vs.BeforeUpdate.ChannelID == vs.ChannelID {
		return // 退室またはミュート/ミュート解除など
	}

	// 退室の場合はスキップ
	if vs.ChannelID == "" {
		return
	}

	// 対象VCかチェック
	if !contains(b.Config.TargetVoiceChannels, vs.ChannelID) {
		return
	}

	// 除外ユーザーかチェック
	if contains(b.Config.ExcludedUserIDs, vs.UserID) {
		log.Printf("🤖 Excluded user %s joined VC, skipping notification", vs.UserID)
		return
	}

	log.Printf("🔊 User %s joined VC (Channel ID: %s)", vs.UserID, vs.ChannelID)

	// VCの専用テキストチャット（Text-in-Voice）を取得
	textChannelID, err := b.getVoiceChannelTextChat(s, vs.ChannelID)
	if err != nil {
		// 失敗時は何もしない（Python版の動作に合わせる）
		log.Printf("Debug: Voice channel text chat not available (VC: %s): %v", vs.ChannelID, err)
		return
	}

	// 自己紹介を取得して投稿
	go b.sendIntroductionToVoiceChat(s, textChannelID, vs.Member)
}

// getVoiceChannelTextChat はVCの専用テキストチャットIDを取得する
func (b *Bot) getVoiceChannelTextChat(s *discordgo.Session, voiceChannelID string) (string, error) {
	// VCチャンネル情報を取得
	vcChannel, err := s.Channel(voiceChannelID)
	if err != nil {
		return "", fmt.Errorf("failed to get voice channel: %w", err)
	}

	log.Printf("🔍 Debug: VC Channel Info - ID: %s, Name: %s, Type: %d, ParentID: %s",
		vcChannel.ID, vcChannel.Name, vcChannel.Type, vcChannel.ParentID)

	// ギルドの全チャンネルを取得（API経由）
	channels, err := s.GuildChannels(vcChannel.GuildID)
	if err != nil {
		return "", fmt.Errorf("failed to get guild channels: %w", err)
	}

	// デバッグ: 全チャンネルの情報を出力
	log.Printf("🔍 Debug: Searching for Text-in-Voice channel (Total channels: %d)", len(channels))
	for _, ch := range channels {
		// VCの周辺チャンネルのみログ出力
		if ch.ParentID == voiceChannelID || ch.ParentID == vcChannel.ParentID || ch.ID == voiceChannelID {
			log.Printf("  - Channel: ID=%s, Name=%s, Type=%d, ParentID=%s",
				ch.ID, ch.Name, ch.Type, ch.ParentID)
		}
	}

	// 全チャンネルから検索
	// Text-in-Voice機能の専用チャットは、親IDがVCのIDと同じテキストチャンネル
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildText && ch.ParentID == voiceChannelID {
			log.Printf("✅ Found Text-in-Voice channel: %s (ID: %s)", ch.Name, ch.ID)
			return ch.ID, nil
		}
	}

	return "", fmt.Errorf("voice channel text chat not found")
}

// sendIntroductionToVoiceChat はVCのテキストチャットに自己紹介を投稿する
func (b *Bot) sendIntroductionToVoiceChat(s *discordgo.Session, channelID string, member *discordgo.Member) {
	ctx := context.Background()

	// メンバー情報を取得
	username := member.User.Username
	if member.Nick != "" {
		username = member.Nick
	}

	// 自己紹介を取得
	intro, err := b.DB.GetIntroduction(ctx, member.User.ID)

	// メッセージ本文を構築
	var content string
	var embed *discordgo.MessageEmbed

	if err != nil || intro == nil {
		// 自己紹介なし
		content = fmt.Sprintf("━━━━━━━━━━━━━━━━━━━\n👤 **%s** さんが入室しました\n\n⚠️ この方の自己紹介はまだ投稿されていません\n━━━━━━━━━━━━━━━━━━━", username)
	} else {
		// 自己紹介あり
		// ロール情報を取得
		roleInfo := b.getRoleInfo(s, member)

		// Embed作成
		embed = b.createIntroductionEmbed(username, member.User.AvatarURL(""), intro, roleInfo)
	}

	// メッセージ送信
	if embed != nil {
		_, err = s.ChannelMessageSendEmbed(channelID, embed)
	} else {
		_, err = s.ChannelMessageSend(channelID, content)
	}

	if err != nil {
		log.Printf("❌ Failed to send introduction to voice chat: %v", err)
		return
	}

	log.Printf("✅ Introduction sent to voice chat for user %s", username)
}

// getRoleInfo はメンバーのロール情報を取得する
func (b *Bot) getRoleInfo(s *discordgo.Session, member *discordgo.Member) map[string][]string {
	roleInfo := make(map[string][]string)

	// ギルド情報を取得
	guild, err := s.Guild(member.GuildID)
	if err != nil {
		log.Printf("❌ Failed to get guild: %v", err)
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

// createIntroductionEmbed は自己紹介のEmbedを作成する
func (b *Bot) createIntroductionEmbed(username, avatarURL string, intro *database.Introduction, roleInfo map[string][]string) *discordgo.MessageEmbed {
	var description strings.Builder

	description.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	description.WriteString(fmt.Sprintf("👤 **%s** さんが入室しました\n", username))

	// ロール情報がある場合
	if len(roleInfo) > 0 {
		description.WriteString("\n📋 プロフィール\n")
		description.WriteString("━━━━━━━━━━━━━━━━━━━\n")

		for category, roles := range roleInfo {
			description.WriteString(fmt.Sprintf("\n%s\n", category))
			for _, role := range roles {
				description.WriteString(fmt.Sprintf("%s\n", role))
			}
		}

		description.WriteString("\n━━━━━━━━━━━━━━━━━━━\n")
	}

	// 自己紹介本文
	description.WriteString("📝 自己紹介\n\n")
	// TODO: 実際の自己紹介メッセージ内容を取得する必要がある
	// 現在はメッセージIDのみ保存しているため、メッセージ内容を取得するAPIコールが必要

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

// assignIntroducedRole は「自己紹介済み」ロールを付与する
func (b *Bot) assignIntroducedRole(s *discordgo.Session, guildID, userID string) {
	// ギルド情報を取得
	guild, err := s.Guild(guildID)
	if err != nil {
		log.Printf("⚠️  Failed to get guild for role assignment: %v", err)
		return
	}

	// ロールを検索
	var roleID string
	for _, role := range guild.Roles {
		if role.Name == b.Config.IntroducedRoleName {
			roleID = role.ID
			break
		}
	}

	if roleID == "" {
		log.Printf("⚠️  Role '%s' not found in guild", b.Config.IntroducedRoleName)
		return
	}

	// ロールを付与
	err = s.GuildMemberRoleAdd(guildID, userID, roleID)
	if err != nil {
		log.Printf("⚠️  Failed to assign role: %v", err)
		return
	}

	log.Printf("✅ Assigned '%s' role to user %s", b.Config.IntroducedRoleName, userID)
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
