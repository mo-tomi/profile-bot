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
	textChannelID, err := b.getVoiceChannelTextChat(s, vs.ChannelID, vs.GuildID)
	if err != nil {
		// 失敗時は何もしない（要件定義書 5.2節）
		log.Printf("⚠️  Voice channel text chat not available (VC: %s): %v", vs.ChannelID, err)
		return
	}

	log.Printf("✅ Found text channel %s for VC %s", textChannelID, vs.ChannelID)

	// 自己紹介を取得して投稿
	go b.sendIntroductionToVoiceChat(s, textChannelID, vs.Member, vs.ChannelID, vs.GuildID)
}

// getVoiceChannelTextChat はVCの専用テキストチャットIDを取得する（強化版）
func (b *Bot) getVoiceChannelTextChat(s *discordgo.Session, voiceChannelID, guildID string) (string, error) {
	log.Printf("🔍 Searching for Text-in-Voice channel for VC: %s", voiceChannelID)

	// ギルドの全チャンネルを取得（APIから直接取得）
	channels, err := s.GuildChannels(guildID)
	if err != nil {
		return "", fmt.Errorf("failed to get guild channels: %w", err)
	}

	log.Printf("📋 Total channels in guild: %d", len(channels))

	// VCチャンネル情報を取得
	var vcChannel *discordgo.Channel
	for _, ch := range channels {
		if ch.ID == voiceChannelID {
			vcChannel = ch
			break
		}
	}

	if vcChannel == nil {
		return "", fmt.Errorf("voice channel not found in guild channels list")
	}

	log.Printf("🎤 VC Info - ID: %s, Name: %s, Type: %d, ParentID: %s",
		vcChannel.ID, vcChannel.Name, vcChannel.Type, vcChannel.ParentID)

	// デバッグ: 全チャンネルの情報を出力（Text-in-Voice候補を探す）
	var candidates []string
	for _, ch := range channels {
		// テキストチャンネルで、VCと関連がありそうなもの
		if ch.Type == discordgo.ChannelTypeGuildText {
			// パターン1: ParentIDがVCのID（これがText-in-Voiceの正しい構造）
			if ch.ParentID == voiceChannelID {
				log.Printf("  ✨ Text-in-Voice candidate (ParentID matches VC): %s (ID: %s, ParentID: %s)",
					ch.Name, ch.ID, ch.ParentID)
				candidates = append(candidates, ch.ID)
			}
			// パターン2: VCと同じ親カテゴリで名前が似ている
			if ch.ParentID == vcChannel.ParentID && strings.Contains(ch.Name, vcChannel.Name) {
				log.Printf("  🔍 Potential Text-in-Voice (same parent, similar name): %s (ID: %s)",
					ch.Name, ch.ID)
			}
		}
	}

	// Text-in-Voiceチャンネルを検索
	// Discord API仕様: Text-in-VoiceはParentIDがVCのIDと同じテキストチャンネル
	for _, ch := range channels {
		if ch.Type == discordgo.ChannelTypeGuildText && ch.ParentID == voiceChannelID {
			log.Printf("✅ Found Text-in-Voice channel: %s (ID: %s)", ch.Name, ch.ID)
			return ch.ID, nil
		}
	}

	// 見つからない場合の詳細ログ
	if len(candidates) == 0 {
		log.Printf("❌ No Text-in-Voice channel found. This VC may not have Text-in-Voice enabled.")
		log.Printf("   To enable: Right-click the VC → Edit Channel → Enable 'Text Chat in Voice Channels'")
	}

	return "", fmt.Errorf("voice channel text chat not found (Text-in-Voice may be disabled)")
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
	roleInfo := b.getRoleInfo(s, member)

	// Embed作成（パターンAまたはB）
	embed := b.createIntroductionEmbed(username, member.User.AvatarURL(""), vcName, introContent, intro, roleInfo, guildID)

	// メッセージ送信
	_, err = s.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		log.Printf("❌ Failed to send introduction embed: %v", err)
		return
	}

	log.Printf("✅ Introduction sent to voice chat (channel: %s) for user %s", channelID, username)
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

// createIntroductionEmbed は自己紹介のEmbedを作成する（要件定義書 5.2節準拠）
func (b *Bot) createIntroductionEmbed(username, avatarURL, vcName, introContent string, intro *database.Introduction, roleInfo map[string][]string, guildID string) *discordgo.MessageEmbed {
	var description strings.Builder

	// ヘッダー
	description.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	description.WriteString(fmt.Sprintf("👤 %s さんが入室しました\n", username))

	// ロール情報がある場合（パターンA）
	if len(roleInfo) > 0 {
		description.WriteString("\n📋 プロフィール\n")
		description.WriteString("━━━━━━━━━━━━━━━━━━━\n")

		// RolesConfigの順序でカテゴリを表示
		for _, category := range b.RolesConfig.RoleCategories {
			if roles, exists := roleInfo[category.DisplayName]; exists && len(roles) > 0 {
				description.WriteString(fmt.Sprintf("\n%s\n", category.DisplayName))
				for _, role := range roles {
					description.WriteString(fmt.Sprintf("%s\n", role))
				}
			}
		}

		description.WriteString("\n━━━━━━━━━━━━━━━━━━━\n")
	}

	// 自己紹介本文
	description.WriteString("📝 自己紹介\n\n")
	description.WriteString(introContent)
	description.WriteString("\n\n")

	// 元の自己紹介へのリンク
	if intro != nil {
		introLink := fmt.Sprintf("https://discord.com/channels/%s/%s/%s",
			guildID, intro.ChannelID, intro.MessageID)
		description.WriteString(fmt.Sprintf("[元の自己紹介を見る](%s)\n", introLink))
	}

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
