package database

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AcquireAdvisoryLock はPostgreSQLのAdvisory Lockを取得する
// lockKeyには "weekly_reminder" などの文字列を渡す
// 戻り値: (取得成功, エラー)
func AcquireAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockKey string) (bool, error) {
	var acquired bool

	// pg_try_advisory_lock: ブロックせずにロック取得を試みる
	// hashtext(): 文字列をハッシュ化してBIGINTに変換
	query := "SELECT pg_try_advisory_lock(hashtext($1))"

	err := pool.QueryRow(ctx, query, lockKey).Scan(&acquired)
	if err != nil {
		return false, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	if acquired {
		slog.Info("Advisory lock acquired", "lock_key", lockKey)
	} else {
		slog.Info("Advisory lock busy (another pod is running)", "lock_key", lockKey)
	}

	return acquired, nil
}

// ReleaseAdvisoryLock はAdvisory Lockを解放する
func ReleaseAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockKey string) error {
	query := "SELECT pg_advisory_unlock(hashtext($1))"

	var released bool
	err := pool.QueryRow(ctx, query, lockKey).Scan(&released)
	if err != nil {
		return fmt.Errorf("failed to release advisory lock: %w", err)
	}

	if released {
		slog.Info("Advisory lock released", "lock_key", lockKey)
	} else {
		slog.Warn("Advisory lock was not held", "lock_key", lockKey)
	}

	return nil
}
