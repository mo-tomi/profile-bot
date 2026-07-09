package bot

import (
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

// ptrString は文字列のポインタを返すヘルパー関数
func ptrString(s string) *string {
	return &s
}
