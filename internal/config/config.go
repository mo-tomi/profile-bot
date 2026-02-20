package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config はアプリケーション設定を保持する構造体
type Config struct {
	// Discord設定
	DiscordToken          string
	IntroductionChannelID string
	NotificationChannelID string

	// 監視対象VCチャンネルID
	TargetVoiceChannels []string

	// 除外ユーザーID
	ExcludedUserIDs []string

	// データベース設定
	DatabaseURL string

	// アプリケーション設定
	LogLevel    string
	Environment string
	Port        string

	// ロール設定ファイルパス
	RolesConfigPath string

	// 自己紹介済みロール名
	IntroducedRoleName string
}

// LoadConfig は環境変数からアプリケーション設定を読み込む
func LoadConfig() (*Config, error) {
	config := &Config{
		DiscordToken:          strings.TrimSpace(os.Getenv("DISCORD_TOKEN")),
		IntroductionChannelID: getEnvOrDefault("INTRODUCTION_CHANNEL_ID", "1300659373227638794"),
		NotificationChannelID: getEnvOrDefault("NOTIFICATION_CHANNEL_ID", "1331177944244289598"),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		LogLevel:              getEnvOrDefault("LOG_LEVEL", "info"),
		Environment:           getEnvOrDefault("ENVIRONMENT", "production"),
		Port:                  getEnvOrDefault("PORT", "8080"),
		RolesConfigPath:       getEnvOrDefault("ROLES_CONFIG_PATH", "configs/roles.yaml"),
		IntroducedRoleName:    getEnvOrDefault("INTRODUCED_ROLE_NAME", "自己紹介済み"),
	}

	// 必須環境変数のチェック
	if config.DiscordToken == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN environment variable is required\n\nPlease set it using:\n  PowerShell: $env:DISCORD_TOKEN = \"your_token_here\"\n  Bash: export DISCORD_TOKEN=\"your_token_here\"")
	}
	if config.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required\n\nPlease set it using:\n  PowerShell: $env:DATABASE_URL = \"your_database_url_here\"\n  Bash: export DATABASE_URL=\"your_database_url_here\"")
	}

	// 監視対象VCチャンネルID（デフォルト値）
	config.TargetVoiceChannels = []string{
		"1397176293119758406",
		"1302151154981011486",
		"1448478010650132583",
		"1302151049368571925",
		"1306190768431431721",
		"1448464956453425423",
		"1448465164671389706",
		"1306190915483734026",
		"1384813451813191752",
		"1404396375965433926",
	}

	// 環境変数で上書き可能
	if vcChannels := os.Getenv("TARGET_VOICE_CHANNELS"); vcChannels != "" {
		config.TargetVoiceChannels = strings.Split(vcChannels, ",")
	}

	// 除外ユーザーID（デフォルト値）
	config.ExcludedUserIDs = []string{
		"533698325203910668",
		"916300992612540467",
		"1300226846599675974",
	}

	// 環境変数で上書き可能
	if excludedIDs := os.Getenv("EXCLUDED_USER_IDS"); excludedIDs != "" {
		config.ExcludedUserIDs = strings.Split(excludedIDs, ",")
	}

	return config, nil
}

// getEnvOrDefault は環境変数を取得し、存在しない場合はデフォルト値を返す
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt は環境変数を整数として取得し、存在しない場合はデフォルト値を返す
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}
