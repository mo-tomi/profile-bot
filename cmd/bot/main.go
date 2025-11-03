package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tomim/profile-bot/internal/bot"
	"github.com/tomim/profile-bot/internal/config"
	"github.com/tomim/profile-bot/internal/database"
)

func main() {
	log.Println("🚀 Starting Profile Bot...")

	// 設定読み込み
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("❌ Failed to load config: %v", err)
	}

	log.Printf("✅ Config loaded (Environment: %s, Log Level: %s)", cfg.Environment, cfg.LogLevel)

	// ロール設定読み込み
	rolesConfig, err := config.LoadRolesConfig(cfg.RolesConfigPath)
	if err != nil {
		log.Fatalf("❌ Failed to load roles config: %v", err)
	}

	log.Printf("✅ Roles config loaded (%d categories)", len(rolesConfig.RoleCategories))

	// データベース接続
	ctx := context.Background()
	db, err := database.NewDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	// テーブル初期化
	if err := db.InitTables(ctx); err != nil {
		log.Fatalf("❌ Failed to initialize database tables: %v", err)
	}

	// Bot初期化
	profileBot, err := bot.NewBot(cfg, rolesConfig, db)
	if err != nil {
		log.Fatalf("❌ Failed to create bot: %v", err)
	}

	// コンテキスト作成
	botCtx, botCancel := context.WithCancel(context.Background())
	defer botCancel()

	// ヘルスチェックサーバー起動
	go startHealthCheckServer(profileBot, cfg.Port)

	// Bot起動
	go func() {
		if err := profileBot.Start(botCtx); err != nil {
			log.Fatalf("❌ Bot error: %v", err)
		}
	}()

	// シグナル待機
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	// シグナル受信
	sig := <-sigChan
	log.Printf("🛑 Received signal: %v", sig)

	// Graceful Shutdown開始
	log.Println("🔄 Starting graceful shutdown...")

	// Botコンテキストをキャンセル
	botCancel()

	// Botの終了処理
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := profileBot.Shutdown(shutdownCtx); err != nil {
		log.Printf("⚠️  Error during shutdown: %v", err)
	}

	log.Println("✅ Shutdown complete")
}

// startHealthCheckServer はヘルスチェック用のHTTPサーバーを起動する
func startHealthCheckServer(bot *bot.Bot, port string) {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if bot.IsHealthy() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Unhealthy"))
		}
	})

	http.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if bot.IsReady() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Ready"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("Not ready"))
		}
	})

	log.Printf("🏥 Health check server listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("❌ Failed to start health check server: %v", err)
	}
}
