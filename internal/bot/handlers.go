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

	// Text-in-Voice機能の確認
	// Discord APIでは、Text-in-Voice有効時、VCチャンネル自体にメッセージを送信できる
	vcChannel, err := s.Channel(vs.ChannelID)
	if err != nil {
		log.Printf("❌ Failed to get VC channel: %v", err)
		return
	}

	// VCチャンネルにlast_message_idがある場合、Text-in-Voice有効の可能性が高い
	hasTextInVoice := vcChannel.LastMessageID != ""

	var textChannelID string
	if hasTextInVoice {
		// Text-in-Voice有効: VCチャンネル自体にメッセージを送信
		log.Printf("✅ Sending to VC channel (Text-in-Voice enabled): %s", vs.ChannelID)
		textChannelID = vs.ChannelID
	} else {
		// Text-in-Voice無効: 通知チャンネルにフォールバック
		log.Printf("🔄 Text-in-Voice not enabled, using notification channel fallback")
		textChannelID = b.Config.NotificationChannelID
	}

	// 自己紹介を取得して投稿
	go b.sendIntroductionToVoiceChat(s, textChannelID, vs.Member, vs.ChannelID, vs.GuildID)
}

// sendIntroductionToVoiceChat はVCのテキストチャットに自己紹介を投稿する
func (b *Bot) sendIntroductionToVoiceChat(s *discordgo.Session, channelID string, member *discordgo.Member, voiceChannelID, guildID string) {
	ctx := context.Background()

	// VCチャンネル名を取得
	vcChannel, err := s.Channel(voiceChannelID)
	vcName := "VC"
	if err == nil {
		vcName = vcChannel.Name
	}

	// メンバー情報を取得
	username := member.User.Username
	if member.Nick != "" {
		username = member.Nick
	}

	// 自己紹介を取得
	intro, err := b.DB.GetIntroduction(ctx, member.User.ID)

	if err != nil || intro == nil {
		// パターンC: 自己紹介なし（要件定義書 5.2節）
		message := fmt.Sprintf("━━━━━━━━━━━━━━━━━━━\n👤 %s さんが入室しました\n\n⚠️ この方の自己紹介はまだ投稿されていません\n━━━━━━━━━━━━━━━━━━━", username)
		
		_, err = s.ChannelMessageSend(channelID, message)
		if err != nil {
			log.Printf("❌ Failed to send introduction (no intro): %v", err)
			return
		}
		
		log.Printf("✅ Sent 'no introduction' message for user %s", username)
		return
	}

	// 自己紹介メッセージ内容を取得
	introMessage, err := s.ChannelMessage(intro.ChannelID, intro.MessageID)
	var introContent string
	if err == nil {
		introContent = introMessage.Content
		// 長さ制限（Embedのdescriptionは4096文字まで）
		if len(introContent) > 1800 {
			introContent = introContent[:1800] + "..."
		}
	} else {
		log.Printf("⚠️  Failed to fetch introduction message content: %v", err)
		introContent = "（自己紹介メッセージの取得に失敗しました）"
	}

	// ロール情報を取得
	roleInfo := b.getRoleInfo(s, member, guildID)

	// Embed作成（パターンAまたはB）
	embed := b.createIntroductionEmbed(username, member.User.AvatarURL(""), vcName, introContent, intro, roleInfo, guildID)

	// 元の自己紹介へのリンクボタンを作成
	introLink := fmt.Sprintf("https://discord.com/channels/%s/%s/%s",
		guildID, intro.ChannelID, intro.MessageID)

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

	// メッセージ送信（Embed + ボタン）
	_, err = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	if err != nil {
		log.Printf("❌ Failed to send introduction embed: %v", err)
		return
	}

	log.Printf("✅ Introduction sent to voice chat (channel: %s) for user %s", channelID, username)
}

// getRoleInfo はメンバーのロール情報を取得する
func (b *Bot) getRoleInfo(s *discordgo.Session, member *discordgo.Member, guildID string) map[string][]string {
	roleInfo := make(map[string][]string)

	// ギルド情報を取得
	guild, err := s.Guild(guildID)
	if err != nil {
		log.Printf("❌ Failed to get guild (ID: %s): %v", guildID, err)
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
	description.WriteString(fmt.Sprintf("👤 %s さんが入室しました\n", username))

	// ロール情報がある場合
	if len(roleInfo) > 0 {
		description.WriteString("\n")

		// RolesConfigの順序でカテゴリを表示
		for _, category := range b.RolesConfig.RoleCategories {
			if roles, exists := roleInfo[category.DisplayName]; exists && len(roles) > 0 {
				description.WriteString(fmt.Sprintf("%s\n", category.DisplayName))
				for _, role := range roles {
					description.WriteString(fmt.Sprintf("%s\n", role))
				}
				description.WriteString("\n")
			}
		}

		description.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	}

	// 自己紹介本文（ヘッダーなし）
	description.WriteString("\n")
	description.WriteString(introContent)
	description.WriteString("\n")
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