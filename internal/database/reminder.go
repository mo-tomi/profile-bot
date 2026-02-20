package database

import (
	"context"
	"fmt"
	"time"
)

// IsReminderExecutedToday は今日既にリマインダーが実行済みかチェックする
func (db *DB) IsReminderExecutedToday(ctx context.Context, date string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM daily_reminder_log WHERE reminder_date = $1)"

	err := db.Pool.QueryRow(ctx, query, date).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check reminder log: %w", err)
	}

	return exists, nil
}

// LogReminderExecution はリマインダーの実行ログを記録する
func (db *DB) LogReminderExecution(ctx context.Context, date string, notifiedUserIDs []string) error {
	query := `
		INSERT INTO daily_reminder_log (reminder_date, notified_users, created_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (reminder_date) DO NOTHING
	`

	_, err := db.Pool.Exec(ctx, query, date, notifiedUserIDs)
	if err != nil {
		return fmt.Errorf("failed to log reminder execution: %w", err)
	}

	return nil
}

// GetMembersWithoutIntroduction は自己紹介未投稿のメンバーIDリストを返す
// guildMemberIDs: サーバー内の全メンバーIDリスト（botを除く）
func (db *DB) GetMembersWithoutIntroduction(ctx context.Context, guildMemberIDs []string) ([]string, error) {
	if len(guildMemberIDs) == 0 {
		return []string{}, nil
	}

	// すべての自己紹介済みユーザーIDを取得
	query := "SELECT user_id FROM introductions"
	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get introduction user IDs: %w", err)
	}
	defer rows.Close()

	// 自己紹介済みユーザーIDをマップに格納
	introducedUsers := make(map[string]bool)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, fmt.Errorf("failed to scan user ID: %w", err)
		}
		introducedUsers[userID] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user ID rows: %w", err)
	}

	// 自己紹介未投稿のメンバーをフィルタリング
	var membersWithoutIntro []string
	for _, memberID := range guildMemberIDs {
		if !introducedUsers[memberID] {
			membersWithoutIntro = append(membersWithoutIntro, memberID)
		}
	}

	return membersWithoutIntro, nil
}

// GetLastReminderDate は最後にリマインダーを実行した日付を取得する
func (db *DB) GetLastReminderDate(ctx context.Context) (time.Time, error) {
	var lastDate time.Time
	query := "SELECT MAX(reminder_date) FROM daily_reminder_log"

	err := db.Pool.QueryRow(ctx, query).Scan(&lastDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get last reminder date: %w", err)
	}

	return lastDate, nil
}
