package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/robfig/cron/v3"
	"github.com/tomim/profile-bot/internal/database"
)

// StartWeeklyReminder は週次リマインダーを開始する
func (b *Bot) StartWeeklyReminder() {
	// JSTタイムゾーンを設定
	jst := time.FixedZone("JST", 9*60*60)
	c := cron.New(cron.WithLocation(jst))

	// 毎週月曜10:00に実行
	_, err := c.AddFunc("0 10 * * MON", func() {
		log.Println("⏰ Weekly reminder cron triggered")
		if err := b.ExecuteWeeklyReminder(); err != nil {
			log.Printf("❌ Failed to execute weekly reminder: %v", err)
		}
	})

	if err != nil {
		log.Printf("❌ Failed to schedule weekly reminder: %v", err)
		return
	}

	c.Start()
	log.Println("✅ Weekly reminder scheduler started")
}

// ExecuteWeeklyReminder は週次リマインダーを実行する（重複防止機能付き）
func (b *Bot) ExecuteWeeklyReminder() error {
	ctx := context.Background()

	// 1. PostgreSQL Advisory Lockを取得
	acquired, err := database.AcquireAdvisoryLock(ctx, b.DB.Pool, "weekly_reminder")
	if err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	if !acquired {
		// 別のPodが実行中なのでスキップ
		log.Println("⏭️  Another pod is executing weekly reminder, skipping")
		return nil
	}

	// 2. 必ずロックを解放（defer）
	defer func() {
		if err := database.ReleaseAdvisoryLock(ctx, b.DB.Pool, "weekly_reminder"); err != nil {
			log.Printf("❌ Failed to release advisory lock: %v", err)
		}
	}()

	// 3. 今日既に実行済みかチェック
	today := time.Now().Format("2006-01-02")
	executed, err := b.DB.IsReminderExecutedToday(ctx, today)
	if err != nil {
		return fmt.Errorf("failed to check reminder log: %w", err)
	}

	if executed {
		log.Println("📅 Reminder already executed today")
		return nil
	}

	// 4. リマインダー実行
	log.Println("📢 Executing weekly reminder...")
	notifiedUserIDs, err := b.executeReminderInternal(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute reminder: %w", err)
	}

	// 5. 実行ログを記録
	err = b.DB.LogReminderExecution(ctx, today, notifiedUserIDs)
	if err != nil {
		return fmt.Errorf("failed to log reminder execution: %w", err)
	}

	log.Printf("✅ Weekly reminder completed successfully (%d users notified)", len(notifiedUserIDs))
	return nil
}

// executeReminderInternal はリマインダーの実際の処理を実行する
func (b *Bot) executeReminderInternal(ctx context.Context) ([]string, error) {
	// 通知チャンネルを取得
	notifyChannel, err := b.Session.Channel(b.Config.NotificationChannelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get notification channel: %w", err)
	}

	// 自己紹介チャンネルを取得
	introChannel, err := b.Session.Channel(b.Config.IntroductionChannelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get introduction channel: %w", err)
	}

	// ギルド情報を取得
	guild, err := b.Session.Guild(notifyChannel.GuildID)
	if err != nil {
		return nil, fmt.Errorf("failed to get guild: %w", err)
	}

	// 全メンバーのIDリスト（botを除く）を取得
	var allMemberIDs []string
	for _, member := range guild.Members {
		if !member.User.Bot {
			allMemberIDs = append(allMemberIDs, member.User.ID)
		}
	}

	// 自己紹介未投稿のメンバーIDを取得
	membersWithoutIntro, err := b.DB.GetMembersWithoutIntroduction(ctx, allMemberIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get members without introduction: %w", err)
	}

	// 全員が自己紹介済みの場合
	if len(membersWithoutIntro) == 0 {
		log.Println("🎉 All members have posted their introductions!")
		return []string{}, nil
	}

	// メンバー名を取得（最大10名）
	var memberNames []string
	maxDisplay := 10
	if len(membersWithoutIntro) > maxDisplay {
		memberNames = b.getMemberNames(guild, membersWithoutIntro[:maxDisplay])
	} else {
		memberNames = b.getMemberNames(guild, membersWithoutIntro)
	}

	// メッセージ作成
	var message strings.Builder
	message.WriteString("🌟 **自己紹介のお知らせ** 🌟\n\n")

	if len(membersWithoutIntro) > maxDisplay {
		message.WriteString(fmt.Sprintf("**%s ほか%d名の皆さん**\n\n",
			strings.Join(memberNames, ", "),
			len(membersWithoutIntro)-maxDisplay))
	} else {
		message.WriteString(fmt.Sprintf("**%s の皆さん**\n\n",
			strings.Join(memberNames, ", ")))
	}

	message.WriteString(fmt.Sprintf("こんにちは！<#%s> チャンネルでの自己紹介をお待ちしています！\n", introChannel.ID))
	message.WriteString("書ける範囲で構いませんので、あなたのことを教えてください 😊\n")
	message.WriteString("趣味、好きなこと、最近気になっていることなど、何でも大丈夫です！")

	// メッセージ送信
	_, err = b.Session.ChannelMessageSend(notifyChannel.ID, message.String())
	if err != nil {
		return nil, fmt.Errorf("failed to send reminder message: %w", err)
	}

	log.Printf("✅ Reminder message sent to %d members", len(membersWithoutIntro))
	return membersWithoutIntro, nil
}

// getMemberNames はメンバーIDリストから表示名を取得する
func (b *Bot) getMemberNames(guild *discordgo.Guild, memberIDs []string) []string {
	var names []string

	for _, memberID := range memberIDs {
		// ギルドのメンバーリストから検索
		for _, member := range guild.Members {
			if member.User.ID == memberID {
				// ニックネームがあればそれを使用、なければユーザー名
				name := member.User.Username
				if member.Nick != "" {
					name = member.Nick
				}
				names = append(names, name)
				break
			}
		}
	}

	return names
}

// ExecuteReminderManually は手動でリマインダーを実行する（/profilebot コマンド用）
func (b *Bot) ExecuteReminderManually() (string, error) {
	ctx := context.Background()

	// ロック取得（通常のリマインダーと同じロックを使用）
	acquired, err := database.AcquireAdvisoryLock(ctx, b.DB.Pool, "weekly_reminder")
	if err != nil {
		return "", fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	if !acquired {
		return "⏭️ 別のPodがリマインダーを実行中です。しばらく待ってから再試行してください。", nil
	}

	defer func() {
		if err := database.ReleaseAdvisoryLock(ctx, b.DB.Pool, "weekly_reminder"); err != nil {
			log.Printf("❌ Failed to release advisory lock: %v", err)
		}
	}()

	// リマインダー実行（ログ記録はスキップ）
	notifiedUserIDs, err := b.executeReminderInternal(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to execute reminder: %w", err)
	}

	if len(notifiedUserIDs) == 0 {
		return "🎉 全メンバーが自己紹介済みです！", nil
	}

	return fmt.Sprintf("✅ 自己紹介リマインダーを送信しました (%d名対象)", len(notifiedUserIDs)), nil
}
