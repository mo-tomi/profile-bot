package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Introduction は自己紹介データを表す構造体
type Introduction struct {
	UserID    string
	ChannelID string
	MessageID string
	CreatedAt time.Time
}

// SaveIntroduction は自己紹介をデータベースに保存する（UPSERT）
func (db *DB) SaveIntroduction(ctx context.Context, userID, channelID, messageID string) error {
	query := `
		INSERT INTO introductions (user_id, channel_id, message_id, created_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (user_id) DO UPDATE SET
			channel_id = EXCLUDED.channel_id,
			message_id = EXCLUDED.message_id,
			created_at = EXCLUDED.created_at
	`

	_, err := db.Pool.Exec(ctx, query, userID, channelID, messageID)
	if err != nil {
		return fmt.Errorf("failed to save introduction: %w", err)
	}

	return nil
}

// GetIntroduction は指定ユーザーの自己紹介を取得する
func (db *DB) GetIntroduction(ctx context.Context, userID string) (*Introduction, error) {
	query := `
		SELECT user_id, channel_id, message_id, created_at
		FROM introductions
		WHERE user_id = $1
	`

	var intro Introduction
	err := db.Pool.QueryRow(ctx, query, userID).Scan(
		&intro.UserID,
		&intro.ChannelID,
		&intro.MessageID,
		&intro.CreatedAt,
	)

	if err != nil {
		// レコードが存在しない場合は「未投稿」を意味するためエラーではなく nil を返す
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get introduction: %w", err)
	}

	return &intro, nil
}

// GetIntroductionCount は自己紹介の総数を取得する
func (db *DB) GetIntroductionCount(ctx context.Context) (int, error) {
	var count int
	query := "SELECT COUNT(*) FROM introductions"

	err := db.Pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get introduction count: %w", err)
	}

	return count, nil
}

// GetRecentIntroductions は最新の自己紹介を取得する
func (db *DB) GetRecentIntroductions(ctx context.Context, limit int) ([]Introduction, error) {
	query := `
		SELECT user_id, channel_id, message_id, created_at
		FROM introductions
		ORDER BY created_at DESC
		LIMIT $1
	`

	rows, err := db.Pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get recent introductions: %w", err)
	}
	defer rows.Close()

	var introductions []Introduction
	for rows.Next() {
		var intro Introduction
		err := rows.Scan(&intro.UserID, &intro.ChannelID, &intro.MessageID, &intro.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan introduction row: %w", err)
		}
		introductions = append(introductions, intro)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating introduction rows: %w", err)
	}

	return introductions, nil
}

// HasIntroduction は指定ユーザーが自己紹介を投稿済みかチェックする
func (db *DB) HasIntroduction(ctx context.Context, userID string) (bool, error) {
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM introductions WHERE user_id = $1)"

	err := db.Pool.QueryRow(ctx, query, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check introduction existence: %w", err)
	}

	return exists, nil
}
