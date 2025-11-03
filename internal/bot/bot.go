package bot

import (
	"context"
	"fmt"
	"log"

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
}

// NewBot は新しいBotインスタンスを作成する
func NewBot(cfg *config.Config, rolesConfig *config.RolesConfig, db *database.DB) (*Bot, error) {
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
	log.Println("🚀 Starting Discord bot...")

	// Discord接続
	if err := b.Session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}

	log.Println("✅ Discord bot started successfully")

	// コンテキストのキャンセルを待機
	<-ctx.Done()
	log.Println("🛑 Shutting down Discord bot...")

	return nil
}

// Shutdown はBotを正常終了する
func (b *Bot) Shutdown(ctx context.Context) error {
	log.Println("🔄 Closing Discord session...")

	if b.Session != nil {
		if err := b.Session.Close(); err != nil {
			return fmt.Errorf("failed to close Discord session: %w", err)
		}
	}

	log.Println("✅ Discord session closed")
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
	log.Printf("✅ Bot logged in as: %s#%s", s.State.User.Username, s.State.User.Discriminator)

	// 自己紹介チャンネルの存在確認
	introChannel, err := s.Channel(b.Config.IntroductionChannelID)
	if err != nil {
		log.Printf("❌ Failed to get introduction channel: %v", err)
		return
	}
	log.Printf("📜 Introduction channel: %s (ID: %s)", introChannel.Name, introChannel.ID)

	// 通知チャンネルの存在確認
	notifyChannel, err := s.Channel(b.Config.NotificationChannelID)
	if err != nil {
		log.Printf("❌ Failed to get notification channel: %v", err)
		return
	}
	log.Printf("📢 Notification channel: %s (ID: %s)", notifyChannel.Name, notifyChannel.ID)

	// 自己紹介チャンネルの履歴をスキャン
	go b.scanIntroductionHistory(s, b.Config.IntroductionChannelID)

	// 週次リマインダーを開始
	go b.StartWeeklyReminder()

	// スラッシュコマンドを登録
	go b.registerCommands(s)

	b.Ready = true
	log.Println("✅ Bot initialization complete!")
}

// scanIntroductionHistory は自己紹介チャンネルの履歴をスキャンする
func (b *Bot) scanIntroductionHistory(s *discordgo.Session, channelID string) {
	log.Println("🔍 Scanning introduction channel history...")

	ctx := context.Background()
	scanCount := 0
	newCount := 0
	updateCount := 0

	// 過去のメッセージを取得（最大3000件、100件ずつ）
	var allMessages []*discordgo.Message
	var lastMessageID string
	targetCount := 3000

	for len(allMessages) < targetCount {
		// Discord APIは一度に最大100件まで
		limit := 100
		if remaining := targetCount - len(allMessages); remaining < limit {
			limit = remaining
		}

		messages, err := s.ChannelMessages(channelID, limit, lastMessageID, "", "")
		if err != nil {
			log.Printf("❌ Failed to fetch channel messages: %v", err)
			break
		}

		if len(messages) == 0 {
			break // これ以上メッセージがない
		}

		allMessages = append(allMessages, messages...)
		lastMessageID = messages[len(messages)-1].ID
	}

	log.Printf("📊 Fetched %d messages from channel history", len(allMessages))

	for _, message := range allMessages {
		if message.Author.Bot {
			continue
		}

		scanCount++

		// 既存の自己紹介があるかチェック
		exists, err := b.DB.HasIntroduction(ctx, message.Author.ID)
		if err != nil {
			log.Printf("❌ Failed to check introduction existence: %v", err)
			continue
		}

		// データベースに保存
		err = b.DB.SaveIntroduction(ctx, message.Author.ID, message.ChannelID, message.ID)
		if err != nil {
			log.Printf("❌ Failed to save introduction: %v", err)
			continue
		}

		if exists {
			updateCount++
		} else {
			newCount++
			log.Printf("🆕 New introduction: User %s", message.Author.Username)
		}

		// 100件ごとに進捗をログ出力
		if scanCount%100 == 0 {
			log.Printf("📈 Scan progress: %d processed (new: %d, updated: %d)", scanCount, newCount, updateCount)
		}
	}

	log.Println("🎉 Scan complete!")
	log.Printf("  📊 Total processed: %d", scanCount)
	log.Printf("  🆕 New: %d", newCount)
	log.Printf("  🔄 Updated: %d", updateCount)

	// 最終的なDB内の自己紹介件数を確認
	finalCount, err := b.DB.GetIntroductionCount(ctx)
	if err == nil {
		log.Printf("📊 Total introductions in DB: %d", finalCount)
	}
}
