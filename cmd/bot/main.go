package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tomim/profile-bot/internal/bot"
	"github.com/tomim/profile-bot/internal/config"
	"github.com/tomim/profile-bot/internal/database"
)

// initLogger は構造化ロガーを初期化します。
// 対応レベル: debug, info, warn, error
// 環境変数 LOG_FORMAT=json|text でハンドラーを切り替え（デフォルト: json）
func initLogger(logLevel, logFormat string) {
	lvl := slog.LevelInfo
	switch strings.ToLower(strings.TrimSpace(logLevel)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	case "info":
		lvl = slog.LevelInfo
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(logFormat)) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

func main() {
	// まずは環境変数から一時的にログレベル/フォーマットを読んでロガーを初期化し、続けて設定をロードして
	// 最終的に `cfg.LogLevel` / `cfg.LogFormat` で再初期化します。
	initLogger(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))

	// 設定読み込み
	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("Failed to load config", "error", err.Error())
		os.Exit(1)
	}

	// 設定中のログレベル/フォーマットでロガーを再初期化
	initLogger(cfg.LogLevel, cfg.LogFormat)

	slog.Info("🚀 Starting Profile Bot...")
	slog.Info("Config loaded", "environment", cfg.Environment, "log_level", cfg.LogLevel)

	// ロール設定読み込み
	rolesConfig, err := config.LoadRolesConfig(cfg.RolesConfigPath)
	if err != nil {
		slog.Error("Failed to load roles config", "error", err.Error())
		os.Exit(1)
	}

	slog.Info("Roles config loaded", "categories", len(rolesConfig.RoleCategories))

	// データベース接続
	ctx := context.Background()
	db, err := database.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	// テーブル初期化
	if err := db.InitTables(ctx); err != nil {
		slog.Error("Failed to initialize database tables", "error", err.Error())
		os.Exit(1)
	}

	// Bot初期化
	profileBot, err := bot.NewBot(cfg, rolesConfig, db)
	if err != nil {
		slog.Error("Failed to create bot", "error", err.Error())
		os.Exit(1)
	}

	// コンテキスト作成
	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()

	// ヘルスチェックサーバー起動（サーバーを取得してシャットダウン時に閉じる）
	srv := startHealthCheckServer(profileBot, cfg.Port)

	// Bot起動
	go func() {
		if err := profileBot.Start(botCtx); err != nil {
			slog.Error("Bot error", "error", err.Error())
			os.Exit(1)
		}
	}()

	// シグナル待機
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// シグナル受信
	sig := <-sigChan
	slog.Info("Received signal", "signal", sig.String())

	// Graceful Shutdown開始
	slog.Info("🔄 Starting graceful shutdown...")

	// Botコンテキストをキャンセル
	botCancel()

	// Botの終了処理
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := profileBot.Shutdown(shutdownCtx); err != nil {
		slog.Error("Error during bot shutdown", "error", err.Error())
	}

	// HTTP サーバーもシャットダウン
	if srv != nil {
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("Error during HTTP server shutdown", "error", err.Error())
		}
	}

	slog.Info("✅ Shutdown complete")
}

// startHealthCheckServer はヘルスチェック用のHTTPサーバーを起動する
func startHealthCheckServer(bot *bot.Bot, port string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if bot.IsHealthy() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Unhealthy"))
		}
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if bot.IsReady() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Not ready"))
		}
	})

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	slog.Info("Health check server listening", "port", port)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Failed to start health check server", "error", err.Error())
		}
	}()

	return srv
}
