package database

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB はデータベース接続プールを管理する構造体
type DB struct {
	Pool *pgxpool.Pool
}

// NewDB は新しいデータベース接続を作成する
func NewDB(ctx context.Context, connString string) (*DB, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// 接続テスト
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	log.Println("✅ Database connection established")

	return &DB{Pool: pool}, nil
}

// Close はデータベース接続を閉じる
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
		log.Println("✅ Database connection closed")
	}
}

// InitTables はデータベーステーブルを初期化する
func (db *DB) InitTables(ctx context.Context) error {
	// introductions テーブル
	_, err := db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS introductions (
			user_id BIGINT PRIMARY KEY,
			channel_id BIGINT NOT NULL,
			message_id BIGINT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create introductions table: %w", err)
	}

	// インデックス作成
	_, err = db.Pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_introductions_user_id ON introductions(user_id);
		CREATE INDEX IF NOT EXISTS idx_introductions_created_at ON introductions(created_at DESC);
	`)
	if err != nil {
		return fmt.Errorf("failed to create indexes on introductions: %w", err)
	}

	// daily_reminder_log テーブル
	_, err = db.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS daily_reminder_log (
			id SERIAL PRIMARY KEY,
			reminder_date DATE NOT NULL UNIQUE,
			notified_users TEXT[],
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create daily_reminder_log table: %w", err)
	}

	// インデックス作成
	_, err = db.Pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_daily_reminder_date ON daily_reminder_log(reminder_date);
	`)
	if err != nil {
		return fmt.Errorf("failed to create indexes on daily_reminder_log: %w", err)
	}

	log.Println("✅ Database tables initialized")
	return nil
}
