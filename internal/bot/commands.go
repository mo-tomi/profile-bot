package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// registerCommands はスラッシュコマンドを登録する
func (b *Bot) registerCommands(s *discordgo.Session) {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "profilebot",
			Description: "自己紹介リマインダーをテスト実行します",
		},
		{
			Name:        "profile",
			Description: "指定したユーザーの自己紹介を表示します",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "user",
					Description: "自己紹介を表示するユーザー",
					Required:    true,
				},
			},
		},
	}

	// コマンドハンドラーを登録
	s.AddHandler(b.handleSlashCommand)

	// グローバルコマンドとして登録
	for _, cmd := range commands {
		_, err := s.ApplicationCommandCreate(s.State.User.ID, "", cmd)
		if err != nil {
			slog.Error("Failed to create command", "command", cmd.Name, "error", err.Error())
		} else {
			slog.Info("Registered slash command", "command", cmd.Name)
		}
	}
}

// handleSlashCommand はスラッシュコマンドのハンドラー
func (b *Bot) handleSlashCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	switch i.ApplicationCommandData().Name {
	case "profilebot":
		b.handleProfileBotCommand(s, i)
	case "profile":
		b.handleProfileCommand(s, i)
	}
}

// handleProfileBotCommand は /profilebot コマンドのハンドラー
func (b *Bot) handleProfileBotCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// コマンドを受け付けたことを通知（ephemeral = 実行者のみに表示）
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})

	if err != nil {
		slog.Error("Failed to respond to interaction", "error", err.Error())
		return
	}

	// リマインダーを手動実行
	result, err := b.ExecuteReminderManually()
	if err != nil {
		result = "❌ エラーが発生しました: " + err.Error()
		slog.Error("/profilebot command error", "error", err.Error())
	}

	// 結果を返信
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: ptrString("🔄 **プロフィールリマインダー実行結果**\n" + result),
	})

	if err != nil {
		slog.Error("Failed to edit interaction response", "error", err.Error())
		return
	}

	slog.Info("/profilebot command executed", "user", i.Member.User.Username)
}

// handleProfileCommand は /profile @user コマンドのハンドラー
func (b *Bot) handleProfileCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// 常にephemeral(実行者のみに表示)で応答する
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		slog.Error("Failed to respond to /profile interaction", "error", err.Error())
		return
	}

	options := i.ApplicationCommandData().Options
	if len(options) == 0 {
		b.editProfileResponse(s, i, "❌ ユーザーが指定されていません")
		return
	}

	targetUser := options[0].UserValue(s)
	if targetUser == nil {
		b.editProfileResponse(s, i, "❌ ユーザーが指定されていません")
		return
	}

	ctx := context.Background()
	intro, err := b.DB.GetIntroduction(ctx, targetUser.ID)
	if err != nil {
		slog.Error("Failed to get introduction for /profile", "error", err.Error(), "user_id", targetUser.ID)
		b.editProfileResponse(s, i, "❌ 取得に失敗しました")
		return
	}
	if intro == nil {
		b.editProfileResponse(s, i, "まだ自己紹介が投稿されていません")
		return
	}

	introMessage, err := s.ChannelMessage(intro.ChannelID, intro.MessageID)
	if err != nil {
		slog.Warn("Failed to fetch introduction message for /profile", "error", err.Error(), "user_id", targetUser.ID)
		b.editProfileResponse(s, i, "❌ 取得に失敗しました")
		return
	}

	introContent := introMessage.Content
	if len(introContent) > 1800 {
		introContent = introContent[:1800] + "..."
	}

	username := targetUser.Username
	if targetUser.GlobalName != "" {
		username = targetUser.GlobalName
	}

	var roleInfo map[string][]string
	fullMember, err := s.GuildMember(i.GuildID, targetUser.ID)
	if err != nil {
		slog.Warn("Failed to fetch member info for /profile, showing without roles", "error", err.Error(), "user_id", targetUser.ID)
	} else {
		if fullMember.Nick != "" {
			username = fullMember.Nick
		}
		roleInfo = b.getRoleInfo(s, fullMember, i.GuildID)
	}

	embed := b.createIntroductionEmbed(username, targetUser.AvatarURL("256"), "プロフィール表示", introContent, intro, roleInfo, i.GuildID)

	introLink := fmt.Sprintf("https://discord.com/channels/%s/%s/%s", i.GuildID, intro.ChannelID, intro.MessageID)
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

	embeds := []*discordgo.MessageEmbed{embed}
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Embeds:     &embeds,
		Components: &components,
	})
	if err != nil {
		slog.Error("Failed to edit /profile interaction response", "error", err.Error())
	}
}

// editProfileResponse は /profile コマンドの応答をテキストメッセージに差し替える
func (b *Bot) editProfileResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: ptrString(content),
	})
	if err != nil {
		slog.Error("Failed to edit /profile interaction response", "error", err.Error())
	}
}

// ptrString は文字列のポインタを返すヘルパー関数
func ptrString(s string) *string {
	return &s
}
