package bot

import (
	"context"
	"fmt"
	"sync"

	"log/slog"

	"github.com/bwmarrin/discordgo"
	"github.com/tomim/profile-bot/internal/config"
	"github.com/tomim/profile-bot/internal/database"
)

// Bot はDiscord botの本体
type Bot struct {
	Session     *discordgo.Session
	Config      *config.Config
	RolesConfig *config.RolesConfig
	DB          *database.DB
	Ready       bool
	// readyOnce は onReady の初期化処理を一度だけ行うための同期プリミティブ
	readyOnce sync.Once
}

// NewBot は新しいBotインスタンスを作成する
func NewBot(cfg *config.Config, rolesConfig *config.RolesConfig, db *database.DB) (*Bot, error) {
	// トークンの検証
	if cfg.DiscordToken == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN is empty - please set the DISCORD_TOKEN environment variable")
	}

	// Discord セッション作成
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	// Intentsを設定
	session.Identify.Intents = discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildVoiceStates |
		discordgo.IntentsGuildMembers |
		discordgo.IntentsMessageContent

	bot := &Bot{
		Session:     session,
		Config:      cfg,
		RolesConfig: rolesConfig,
		DB:          db,
		Ready:       false,
	}

	// イベントハンドラー登録
	session.AddHandler(bot.onReady)
	session.AddHandler(bot.onMessageCreate)
	session.AddHandler(bot.onVoiceStateUpdate)

	return bot, nil
}

// Start はBotを起動する
func (b *Bot) Start(ctx context.Context) error {
	slog.Info("Starting Discord bot")

	// Discord接続
	if err := b.Session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}

	slog.Info("Discord bot started successfully")

	// コンテキストのキャンセルを待機
	<-ctx.Done()
	slog.Info("Shutting down Discord bot")

	return nil
}

// Shutdown はBotを正常終了する
func (b *Bot) Shutdown(ctx context.Context) error {
	slog.Info("Closing Discord session")

	if b.Session != nil {
		if err := b.Session.Close(); err != nil {
			return fmt.Errorf("failed to close Discord session: %w", err)
		}
	}

	slog.Info("Discord session closed")
	return nil
}

// IsHealthy はBotが正常に動作しているかチェックする
func (b *Bot) IsHealthy() bool {
	if b.Session == nil {
		return false
	}

	// Discord接続状態をチェック
	if b.Session.DataReady == false {
		return false
	}

	// データベース接続をチェック
	if err := b.DB.Pool.Ping(context.Background()); err != nil {
		return false
	}

	return true
}

// IsReady はBotの起動準備が完了しているかチェックする
func (b *Bot) IsReady() bool {
	return b.Ready
}

// onReady はBot起動時に実行されるハンドラー
func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	slog.Info("Bot logged in", "username", s.State.User.Username, "discriminator", s.State.User.Discriminator)

	// 初回の初期化処理は再接続時に再実行しないよう sync.Once で囲む
	b.readyOnce.Do(func() {
		// 自己紹介チャンネルの存在確認
		introChannel, err := s.Channel(b.Config.IntroductionChannelID)
		if err != nil {
			slog.Error("Failed to get introduction channel", "error", err.Error())
			return
		}
		slog.Info("Introduction channel", "name", introChannel.Name, "id", introChannel.ID)

		// 通知チャンネルの存在確認
		notifyChannel, err := s.Channel(b.Config.NotificationChannelID)
		if err != nil {
			slog.Error("Failed to get notification channel", "error", err.Error())
			return
		}
		slog.Info("Notification channel", "name", notifyChannel.Name, "id", notifyChannel.ID)

		// 自己紹介チャンネルの履歴をスキャン
		go b.scanIntroductionHistory(s, b.Config.IntroductionChannelID)

		// 週次リマインダーを開始
		go b.StartWeeklyReminder()

		// スラッシュコマンドを登録
		go b.registerCommands(s)

		b.Ready = true
		slog.Info("Bot initialization complete")
	})
}

// scanIntroductionHistory は自己紹介チャンネルの履歴をスキャンする
func (b *Bot) scanIntroductionHistory(s *discordgo.Session, channelID string) {
	slog.Info("Scanning introduction channel history")

	ctx := context.Background()
	scanCount := 0
	newCount := 0
	updateCount := 0

	// 過去のメッセージを取得（最大3000件、100件ずつ）
	var lastMessageID string
	targetCount := 3000
	totalFetched := 0

	for totalFetched < targetCount {
		// Discord APIは一度に最大100件まで
		limit := 100
		if remaining := targetCount - totalFetched; remaining < limit {
			limit = remaining
		}

		messages, err := s.ChannelMessages(channelID, limit, lastMessageID, "", "")
		if err != nil {
			slog.Error("Failed to fetch channel messages", "error", err.Error())
			break
		}

		if len(messages) == 0 {
			break // これ以上メッセージがない
		}

		// 取得したメッセージをその場で処理してDBへ保存（メモリ肥大化防止）
		for _, message := range messages {
			if message.Author.Bot {
				continue
			}

			scanCount++

			exists, err := b.DB.HasIntroduction(ctx, message.Author.ID)
			if err != nil {
				slog.Error("Failed to check introduction existence", "error", err.Error())
				continue
			}

			if err := b.DB.SaveIntroduction(ctx, message.Author.ID, message.ChannelID, message.ID); err != nil {
				slog.Error("Failed to save introduction", "error", err.Error())
				continue
			}

			if exists {
				updateCount++
			} else {
				newCount++
				slog.Info("New introduction", "user", message.Author.Username, "user_id", message.Author.ID)
			}

			if scanCount%100 == 0 {
				slog.Info("Scan progress", "processed", scanCount, "new", newCount, "updated", updateCount)
			}
		}

		totalFetched += len(messages)
		lastMessageID = messages[len(messages)-1].ID
	}

	slog.Info("Scan complete", "total_processed", scanCount, "new", newCount, "updated", updateCount)

	// 最終的なDB内の自己紹介件数を確認
	if finalCount, err := b.DB.GetIntroductionCount(ctx); err == nil {
		slog.Info("Total introductions in DB", "count", finalCount)
	}
}
