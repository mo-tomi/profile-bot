# Discord自己紹介Bot Go移行 - 技術リファレンス

## 📚 重要な実装パターン集

### 1. PostgreSQL Advisory Lock（重複送信防止の核心）

#### 概要
- K8s環境で複数Podが並行実行しても、1つのPodだけがリマインダーを実行
- データベースレベルの排他制御なので確実

#### 実装例

```go
package database

import (
    "context"
    "fmt"
    "github.com/jackc/pgx/v5/pgxpool"
    "log"
)

// ロック取得
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
        log.Printf("✅ Advisory lock acquired: %s", lockKey)
    } else {
        log.Printf("⏭️  Advisory lock busy (another pod is running): %s", lockKey)
    }
    
    return acquired, nil
}

// ロック解放
func ReleaseAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockKey string) error {
    query := "SELECT pg_advisory_unlock(hashtext($1))"
    
    var released bool
    err := pool.QueryRow(ctx, query, lockKey).Scan(&released)
    if err != nil {
        return fmt.Errorf("failed to release advisory lock: %w", err)
    }
    
    if released {
        log.Printf("🔓 Advisory lock released: %s", lockKey)
    } else {
        log.Printf("⚠️  Advisory lock was not held: %s", lockKey)
    }
    
    return nil
}

// 使用例
func ExecuteWeeklyReminder(ctx context.Context, pool *pgxpool.Pool) error {
    // 1. ロック取得
    acquired, err := AcquireAdvisoryLock(ctx, pool, "weekly_reminder")
    if err != nil {
        return err
    }
    
    if !acquired {
        // 別のPodが実行中なので何もしない
        return nil
    }
    
    // 2. 必ずロック解放（defer）
    defer func() {
        if err := ReleaseAdvisoryLock(ctx, pool, "weekly_reminder"); err != nil {
            log.Printf("❌ Failed to release lock: %v", err)
        }
    }()
    
    // 3. 今日既に実行済みかチェック
    today := time.Now().Format("2006-01-02")
    var exists bool
    err = pool.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM daily_reminder_log WHERE reminder_date = $1)",
        today).Scan(&exists)
    
    if err != nil {
        return fmt.Errorf("failed to check reminder log: %w", err)
    }
    
    if exists {
        log.Println("📅 Reminder already executed today")
        return nil
    }
    
    // 4. リマインダー実行
    log.Println("📢 Executing weekly reminder...")
    // ... 実際のリマインダー処理 ...
    
    // 5. 実行ログを記録
    _, err = pool.Exec(ctx,
        "INSERT INTO daily_reminder_log (reminder_date, notified_users) VALUES ($1, $2)",
        today, notifiedUserIDs)
    
    if err != nil {
        return fmt.Errorf("failed to log reminder execution: %w", err)
    }
    
    log.Println("✅ Weekly reminder completed successfully")
    return nil
}
```

#### テーブル定義

```sql
CREATE TABLE IF NOT EXISTS daily_reminder_log (
    id SERIAL PRIMARY KEY,
    reminder_date DATE NOT NULL UNIQUE,
    notified_users TEXT[],
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_daily_reminder_date 
ON daily_reminder_log(reminder_date);
```

#### デバッグ方法

```sql
-- 現在のロック状態を確認
SELECT 
    pid,
    locktype,
    database,
    classid,
    objid,
    mode,
    granted
FROM pg_locks
WHERE locktype = 'advisory';

-- ロックを強制解放（緊急時のみ）
SELECT pg_advisory_unlock_all();
```

---

### 2. Discord Text-in-Voice（VC専用チャット）

#### 概要
- ボイスチャンネルに紐づくテキストチャット
- 2023年に追加された機能
- Python版では正常に動作していたが、Goで実装方法を確認する必要あり

#### discordgoでの実装（調査必要）

```go
package bot

import (
    "fmt"
    "github.com/bwmarrin/discordgo"
)

// VCの専用テキストチャットIDを取得
func (b *Bot) getVoiceChannelTextChat(s *discordgo.Session, voiceChannelID string) (string, error) {
    // 方法1: チャンネル情報から取得
    channel, err := s.Channel(voiceChannelID)
    if err != nil {
        return "", fmt.Errorf("failed to get voice channel: %w", err)
    }
    
    // VCのメタデータに専用チャットIDが含まれている可能性
    // ※ discordgoのバージョンによって実装が異なる可能性あり
    
    // 方法2: 親カテゴリから同名のテキストチャンネルを探す
    // ※ これは旧来の方法で、Text-in-Voice機能とは異なる
    
    // 方法3: Guild全体のチャンネルリストから検索
    guild, err := s.Guild(channel.GuildID)
    if err != nil {
        return "", fmt.Errorf("failed to get guild: %w", err)
    }
    
    // Text-in-Voice専用チャンネルの特徴:
    // - Type: ChannelTypeGuildText (0)
    // - ParentID: VCと同じ親カテゴリ（またはVCそのものが親）
    // - 名前がVCと同じ、または "chat-in-{VC名}"
    
    for _, ch := range guild.Channels {
        // ここで適切な条件で検索
        // ※ Discord APIの仕様を要確認
    }
    
    return "", fmt.Errorf("voice channel text chat not found or disabled")
}

// エラーハンドリング例
func (b *Bot) handleVoiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
    // ... 入室検知など ...
    
    // VCの専用チャット取得
    textChannelID, err := b.getVoiceChannelTextChat(s, vs.ChannelID)
    if err != nil {
        // Python版と同じ動作: 失敗時は何もしない（ログのみ）
        b.logger.Debug("Voice channel text chat not available",
            "vc_id", vs.ChannelID,
            "error", err)
        return
    }
    
    // 自己紹介を投稿
    b.sendIntroductionToVoiceChat(s, textChannelID, vs.Member)
}
```

#### Python版の実装（参考）

```python
# main.py から抜粋（動作確認済み）

@bot.event
async def on_voice_state_update(member, before, after):
    if (before.channel != after.channel and
        after.channel and
        after.channel.id in TARGET_VOICE_CHANNELS):
        
        notify_channel = bot.get_channel(NOTIFICATION_CHANNEL_ID)
        if not notify_channel:
            return
        
        # ここで自己紹介を取得して投稿
        # ※ Python版はNOTIFICATION_CHANNEL_IDに投稿していた
        # Go版ではVCの専用チャットに投稿する必要がある
```

#### 調査項目
1. `discordgo`のバージョン確認（最新版を使用）
2. Discord API仕様書でText-in-Voiceの取得方法を確認
3. テストサーバーで動作確認

---

### 3. Discord Embed（メッセージ装飾）

#### 自己紹介表示のEmbed例

```go
package bot

import (
    "fmt"
    "github.com/bwmarrin/discordgo"
    "strings"
)

// ロール情報を含む自己紹介Embedを生成
func (b *Bot) createIntroductionEmbed(member *discordgo.Member, intro *Introduction, roleInfo *RoleInfo) *discordgo.MessageEmbed {
    embed := &discordgo.MessageEmbed{
        Color: 0x3498db, // 青色
    }
    
    // タイトル部分
    title := fmt.Sprintf("━━━━━━━━━━━━━━━━━━━\n👤 %s さんが入室しました\n", member.User.Username)
    
    // ロール情報がある場合
    if roleInfo != nil && len(roleInfo.Categories) > 0 {
        title += "\n📋 プロフィール\n━━━━━━━━━━━━━━━━━━━\n"
        
        for _, category := range roleInfo.Categories {
            if len(category.Roles) > 0 {
                title += fmt.Sprintf("\n【%s】\n", category.Name)
                for _, role := range category.Roles {
                    title += fmt.Sprintf("%s %s\n", role.Emoji, role.Name)
                }
            }
        }
        
        title += "\n━━━━━━━━━━━━━━━━━━━\n"
    }
    
    // 自己紹介本文
    if intro != nil {
        title += "📝 自己紹介\n\n"
        embed.Description = intro.Content
    } else {
        title += "\n⚠️ この方の自己紹介はまだ投稿されていません\n"
    }
    
    title += "━━━━━━━━━━━━━━━━━━━"
    
    embed.Title = title
    embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
        URL: member.User.AvatarURL(""),
    }
    
    return embed
}

// 元の自己紹介へのリンクボタン
func (b *Bot) createIntroductionButton(channelID, messageID string) []discordgo.MessageComponent {
    return []discordgo.MessageComponent{
        discordgo.ActionsRow{
            Components: []discordgo.MessageComponent{
                discordgo.Button{
                    Label: "元の自己紹介を見る",
                    Style: discordgo.LinkButton,
                    URL:   fmt.Sprintf("https://discord.com/channels/%s/%s/%s", 
                        guildID, channelID, messageID),
                },
            },
        },
    }
}

// 使用例
func (b *Bot) sendIntroductionToVoiceChat(s *discordgo.Session, channelID string, member *discordgo.Member) {
    // 自己紹介を取得
    intro, err := b.db.GetIntroduction(context.Background(), member.User.ID)
    if err != nil {
        b.logger.Error("Failed to get introduction", "error", err)
        return
    }
    
    // ロール情報を取得
    roleInfo := b.getRoleInfo(member)
    
    // Embed生成
    embed := b.createIntroductionEmbed(member, intro, roleInfo)
    
    // ボタン生成
    var components []discordgo.MessageComponent
    if intro != nil {
        components = b.createIntroductionButton(intro.ChannelID, intro.MessageID)
    }
    
    // メッセージ送信
    _, err = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
        Embeds:     []*discordgo.MessageEmbed{embed},
        Components: components,
    })
    
    if err != nil {
        b.logger.Error("Failed to send introduction", "error", err)
        return
    }
    
    b.logger.Info("Introduction sent successfully",
        "user", member.User.Username,
        "channel", channelID)
}
```

---

### 4. ロール設定の読み込みと処理

#### YAML設定ファイル

```yaml
# configs/roles.yaml
role_categories:
  - name: "障害"
    emoji_prefix: true
    roles:
      - { name: "うつ病", emoji: "💙" }
      - { name: "ASD（自閉症スペクトラム障害）", emoji: "🟢" }
      - { name: "ADHD（注意欠如・多動性障害）", emoji: "⚡" }
      - { name: "発達障害", emoji: "🧠" }
      - { name: "グレーゾーン", emoji: "🌀" }
      - { name: "双極性障害", emoji: "💫" }
      - { name: "てんかん", emoji: "⚠️" }
      - { name: "境界性パーソナリティ", emoji: "💗" }
      - { name: "視覚障害", emoji: "👁️" }
  
  - name: "性別"
    emoji_prefix: true
    roles:
      - { name: "男性", emoji: "👨" }
      - { name: "女性", emoji: "👩" }
  
  - name: "手帳"
    emoji_prefix: true
    roles:
      - { name: "身体手帳", emoji: "🟦" }
      - { name: "療育手帳", emoji: "📗" }
      - { name: "精神手帳", emoji: "💚" }
  
  - name: "コミュニケーション"
    emoji_prefix: true
    roles:
      - { name: "通話OK", emoji: "📞" }
      - { name: "OKフレンド申請OK", emoji: "✅" }
      - { name: "NGフレンド申請NG", emoji: "❌" }
      - { name: "フレンド申請(要相談)", emoji: "⚠️" }

excluded_roles:
  - "@everyone"
  - "Carl-bot"

excluded_suffixes:
  - "bot"
  - "Bot"

special_filters:
  - pattern: "（聴覚者）"
    action: "remove"
```

#### Go実装

```go
package config

import (
    "gopkg.in/yaml.v3"
    "os"
    "strings"
)

type RoleConfig struct {
    Name  string `yaml:"name"`
    Emoji string `yaml:"emoji"`
}

type CategoryConfig struct {
    Name        string       `yaml:"name"`
    EmojiPrefix bool         `yaml:"emoji_prefix"`
    Roles       []RoleConfig `yaml:"roles"`
}

type Config struct {
    RoleCategories   []CategoryConfig `yaml:"role_categories"`
    ExcludedRoles    []string         `yaml:"excluded_roles"`
    ExcludedSuffixes []string         `yaml:"excluded_suffixes"`
    SpecialFilters   []SpecialFilter  `yaml:"special_filters"`
}

type SpecialFilter struct {
    Pattern string `yaml:"pattern"`
    Action  string `yaml:"action"`
}

// 設定ファイル読み込み
func LoadConfig(filepath string) (*Config, error) {
    data, err := os.ReadFile(filepath)
    if err != nil {
        return nil, err
    }
    
    var config Config
    err = yaml.Unmarshal(data, &config)
    if err != nil {
        return nil, err
    }
    
    return &config, nil
}

// ロールをフィルタリング
func (c *Config) FilterRoles(roles []*discordgo.Role) []FilteredRole {
    var filtered []FilteredRole
    
    for _, role := range roles {
        // 除外ロールチェック
        if c.isExcludedRole(role) {
            continue
        }
        
        // 特殊フィルター適用
        name := c.applySpecialFilters(role.Name)
        
        // カテゴリと絵文字を取得
        category, emoji := c.getCategoryAndEmoji(name)
        if category != "" {
            filtered = append(filtered, FilteredRole{
                Name:     name,
                Category: category,
                Emoji:    emoji,
            })
        }
    }
    
    return filtered
}

// 除外ロール判定
func (c *Config) isExcludedRole(role *discordgo.Role) bool {
    // 除外リストチェック
    for _, excluded := range c.ExcludedRoles {
        if role.Name == excluded {
            return true
        }
    }
    
    // bot管理フラグチェック
    if role.Managed {
        return true
    }
    
    // サフィックスチェック
    for _, suffix := range c.ExcludedSuffixes {
        if strings.HasSuffix(role.Name, suffix) {
            return true
        }
    }
    
    return false
}

// 特殊フィルター適用
func (c *Config) applySpecialFilters(name string) string {
    for _, filter := range c.SpecialFilters {
        if filter.Action == "remove" {
            name = strings.ReplaceAll(name, filter.Pattern, "")
        }
    }
    return strings.TrimSpace(name)
}

// カテゴリと絵文字を取得
func (c *Config) getCategoryAndEmoji(roleName string) (string, string) {
    for _, category := range c.RoleCategories {
        for _, role := range category.Roles {
            if role.Name == roleName {
                return category.Name, role.Emoji
            }
        }
    }
    return "", ""
}
```

---

### 5. 構造化ログ（JSON形式）

```go
package utils

import (
    "encoding/json"
    "log"
    "os"
    "time"
)

type Logger struct {
    level string
}

type LogEntry struct {
    Level     string                 `json:"level"`
    Time      string                 `json:"time"`
    Message   string                 `json:"message"`
    Fields    map[string]interface{} `json:"fields,omitempty"`
}

func NewLogger(level string) *Logger {
    return &Logger{level: level}
}

func (l *Logger) log(level, message string, fields map[string]interface{}) {
    entry := LogEntry{
        Level:   level,
        Time:    time.Now().Format(time.RFC3339),
        Message: message,
        Fields:  fields,
    }
    
    jsonData, err := json.Marshal(entry)
    if err != nil {
        log.Printf("Failed to marshal log entry: %v", err)
        return
    }
    
    os.Stdout.Write(jsonData)
    os.Stdout.Write([]byte("\n"))
}

func (l *Logger) Info(message string, fields ...interface{}) {
    l.log("info", message, convertFields(fields))
}

func (l *Logger) Error(message string, fields ...interface{}) {
    l.log("error", message, convertFields(fields))
}

func (l *Logger) Debug(message string, fields ...interface{}) {
    if l.level == "debug" {
        l.log("debug", message, convertFields(fields))
    }
}

func convertFields(fields []interface{}) map[string]interface{} {
    result := make(map[string]interface{})
    for i := 0; i < len(fields); i += 2 {
        if i+1 < len(fields) {
            key := fields[i].(string)
            result[key] = fields[i+1]
        }
    }
    return result
}
```

使用例:
```go
logger := utils.NewLogger("info")
logger.Info("User joined voice channel",
    "user_id", userID,
    "username", username,
    "channel_id", channelID)
```

出力:
```json
{
  "level": "info",
  "time": "2025-11-03T10:00:00+09:00",
  "message": "User joined voice channel",
  "fields": {
    "user_id": "123456789",
    "username": "田中太郎",
    "channel_id": "987654321"
  }
}
```

---

### 6. Graceful Shutdown

```go
package main

import (
    "context"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    // Bot初期化
    bot := bot.NewBot()
    
    // コンテキスト作成
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Bot起動
    go func() {
        if err := bot.Start(ctx); err != nil {
            log.Fatalf("Failed to start bot: %v", err)
        }
    }()
    
    // ヘルスチェックサーバー起動
    go startHealthCheckServer(bot)
    
    // シグナル待機
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
    
    // シグナル受信
    sig := <-sigChan
    log.Printf("Received signal: %v", sig)
    
    // Graceful Shutdown開始
    log.Println("Starting graceful shutdown...")
    
    // コンテキストキャンセル
    cancel()
    
    // Botの終了処理
    shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer shutdownCancel()
    
    if err := bot.Shutdown(shutdownCtx); err != nil {
        log.Printf("Error during shutdown: %v", err)
    }
    
    log.Println("Shutdown complete")
}
```

---

## 🔍 デバッグTips

### 1. PostgreSQL接続確認

```go
func testDatabaseConnection(connString string) error {
    pool, err := pgxpool.New(context.Background(), connString)
    if err != nil {
        return fmt.Errorf("unable to create connection pool: %w", err)
    }
    defer pool.Close()
    
    // Ping
    if err := pool.Ping(context.Background()); err != nil {
        return fmt.Errorf("unable to ping database: %w", err)
    }
    
    log.Println("✅ Database connection successful")
    return nil
}
```

### 2. Discord Bot接続確認

```go
func testBotConnection(token string) error {
    session, err := discordgo.New("Bot " + token)
    if err != nil {
        return fmt.Errorf("error creating Discord session: %w", err)
    }
    
    // Botユーザー情報を取得
    user, err := session.User("@me")
    if err != nil {
        return fmt.Errorf("error fetching bot user: %w", err)
    }
    
    log.Printf("✅ Bot connected as: %s#%s", user.Username, user.Discriminator)
    return nil
}
```

### 3. 環境変数確認

```go
func validateEnvironmentVariables() error {
    required := []string{
        "DISCORD_TOKEN",
        "DATABASE_URL",
    }
    
    for _, key := range required {
        if os.Getenv(key) == "" {
            return fmt.Errorf("required environment variable not set: %s", key)
        }
    }
    
    log.Println("✅ All required environment variables are set")
    return nil
}
```

---

## 📊 パフォーマンス計測

```go
package utils

import (
    "log"
    "time"
)

// 関数実行時間を計測
func MeasureTime(name string) func() {
    start := time.Now()
    return func() {
        duration := time.Since(start)
        log.Printf("⏱️  %s took %v", name, duration)
    }
}

// 使用例
func someFunction() {
    defer MeasureTime("someFunction")()
    
    // 処理内容
}
```

---

## 🚨 エラーハンドリングパターン

```go
// パターン1: ログして続行
if err != nil {
    logger.Warn("Non-critical error occurred", "error", err)
    // 処理続行
}

// パターン2: ログして返す
if err != nil {
    logger.Error("Critical error occurred", "error", err)
    return fmt.Errorf("operation failed: %w", err)
}

// パターン3: リトライ
func retryOperation(ctx context.Context, maxRetries int, operation func() error) error {
    for i := 0; i < maxRetries; i++ {
        err := operation()
        if err == nil {
            return nil
        }
        
        logger.Warn("Operation failed, retrying",
            "attempt", i+1,
            "max_retries", maxRetries,
            "error", err)
        
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(time.Second * time.Duration(i+1)):
            // 指数バックオフ
        }
    }
    
    return fmt.Errorf("operation failed after %d retries", maxRetries)
}
```

---

この技術リファレンスを参照しながら実装を進めてください！
