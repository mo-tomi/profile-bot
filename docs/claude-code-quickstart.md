# Claude Code クイックスタート: Discord自己紹介Bot Go移行

## 🎯 ミッション
Python製Discord BotをGo言語に移行。**最重要課題**: K8s環境での重複メッセージ送信を完全に防止する。

## 📋 現状確認

### 既存コード（Python）
- `main.py`: Bot本体（py-cord使用）
- `database.py`: PostgreSQL操作（asyncpg使用）
- 問題: K8sで複数Podが同時にリマインダー実行 → 重複送信

### 重要な定数
```python
INTRODUCTION_CHANNEL_ID = 1300659373227638794  # 自己紹介チャンネル
NOTIFICATION_CHANNEL_ID = 1331177944244289598  # 通知チャンネル

TARGET_VOICE_CHANNELS = [  # 監視対象VC（8個）
    1300291307750559754, 1302151049368571925, 1302151154981011486,
    1306190768431431721, 1306190915483734026, 1403273245360259163,
    1404396375965433926, 1384813451813191752
]

EXCLUDED_BOT_IDS = [  # 除外するbot
    533698325203910668, 916300992612540467, 1300226846599675974
]
```

## 🔧 技術スタック

### Go言語での実装
```
discordgo     # Discord API
pgx/v5        # PostgreSQL（asyncpgの代替）
cron/v3       # スケジューリング
yaml.v3       # 設定ファイル
```

## 🚀 実装手順（順番に実行）

### Step 1: プロジェクト初期化
```bash
# アーカイブ作業
mkdir -p archive/python
git mv main.py database.py keep_alive.py requirements.txt Dockerfile .dockerignore archive/python/

# Go プロジェクト作成
go mod init github.com/your-username/profile-bot
mkdir -p cmd/bot internal/{bot,database,config,utils} configs deployments/{docker,k8s}

# 依存関係
go get github.com/bwmarrin/discordgo
go get github.com/jackc/pgx/v5/pgxpool
go get github.com/robfig/cron/v3
go get gopkg.in/yaml.v3
```

### Step 2: データベース層（重要！）

**`internal/database/lock.go`** - 重複送信の根本解決
```go
package database

import (
    "context"
    "github.com/jackc/pgx/v5/pgxpool"
)

// PostgreSQL Advisory Lockで排他制御
func AcquireAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockKey string) (bool, error) {
    var acquired bool
    query := "SELECT pg_try_advisory_lock(hashtext($1))"
    err := pool.QueryRow(ctx, query, lockKey).Scan(&acquired)
    return acquired, err
}

func ReleaseAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockKey string) error {
    query := "SELECT pg_advisory_unlock(hashtext($1))"
    _, err := pool.Exec(ctx, query, lockKey)
    return err
}
```

### Step 3: リマインダー実装（最重要機能）

**`internal/bot/reminder.go`**
```go
func (b *Bot) StartWeeklyReminder() {
    c := cron.New(cron.WithLocation(time.FixedZone("JST", 9*60*60)))
    
    // 毎週月曜10:00
    c.AddFunc("0 10 * * MON", func() {
        ctx := context.Background()
        
        // 1. Advisory Lock取得（重複防止）
        acquired, err := database.AcquireAdvisoryLock(ctx, b.db.Pool, "weekly_reminder")
        if !acquired {
            log.Info("別Podが実行中 - スキップ")
            return
        }
        defer database.ReleaseAdvisoryLock(ctx, b.db.Pool, "weekly_reminder")
        
        // 2. 今日実行済みかチェック
        today := time.Now().Format("2006-01-02")
        if b.db.IsReminderExecutedToday(ctx, today) {
            log.Info("本日実行済み")
            return
        }
        
        // 3. リマインダー実行
        b.executeReminder(ctx)
        
        // 4. ログ記録
        b.db.LogReminderExecution(ctx, today)
    })
    
    c.Start()
}
```

### Step 4: VC入室通知（重要な仕様）

**Python版の動作**:
- VCの専用チャット（Text-in-Voice）に投稿
- 専用チャット取得失敗時は何もしない

**`internal/bot/handlers.go`**
```go
func (b *Bot) handleVoiceStateUpdate(s *discordgo.Session, vs *discordgo.VoiceStateUpdate) {
    // 入室検知
    if vs.BeforeUpdate != nil && vs.BeforeUpdate.ChannelID == vs.ChannelID {
        return // 退室または移動
    }
    
    // 対象VCチェック
    if !contains(TARGET_VOICE_CHANNELS, vs.ChannelID) {
        return
    }
    
    // 除外botチェック
    if contains(EXCLUDED_BOT_IDS, vs.UserID) {
        return
    }
    
    // VCの専用チャット取得（Text-in-Voice）
    textChannelID, err := b.getVoiceChannelTextChat(s, vs.ChannelID)
    if err != nil {
        // 失敗時は何もしない（Python版の動作）
        return
    }
    
    // 自己紹介を取得して投稿
    b.sendIntroductionToVoiceChat(s, textChannelID, vs.Member)
}
```

### Step 5: ロール表示機能

**`configs/roles.yaml`** を作成:
```yaml
role_categories:
  - name: "障害"
    roles:
      - { name: "ASD（自閉症スペクトラム障害）", emoji: "🟢" }
      - { name: "ADHD（注意欠如・多動性障害）", emoji: "⚡" }
  - name: "性別"
    roles:
      - { name: "男性", emoji: "👨" }
      - { name: "女性", emoji: "👩" }
  - name: "手帳"
    roles:
      - { name: "療育手帳", emoji: "📗" }
      - { name: "精神手帳", emoji: "💚" }
  - name: "コミュニケーション"
    roles:
      - { name: "通話OK", emoji: "📞" }
      - { name: "OKフレンド申請OK", emoji: "✅" }
```

**除外ロール**: "@everyone", "Carl-bot", bot管理フラグ付き, 末尾"bot"/"Bot"

### Step 6: K8s対応

**Dockerfile**（Multi-stage build）:
```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bot ./cmd/bot

FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /app/bot .
COPY --from=builder /app/configs ./configs
EXPOSE 8080
CMD ["./bot"]
```

**ヘルスチェック** (`cmd/bot/main.go`):
```go
func startHealthCheckServer() {
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        w.Write([]byte("OK"))
    })
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

## ✅ テスト項目

### 重複送信防止テスト（最重要）
```bash
# K8sで2つのPodを起動
kubectl scale deployment profile-bot --replicas=2

# リマインダー手動実行
# → 1つのPodのみがメッセージ送信すること
# → もう1つのPodは "別Podが実行中" とログ出力すること
```

### その他の動作確認
1. 自己紹介投稿 → DB保存 → ロール付与
2. VC入室 → VCの専用チャットに通知
3. `/profilebot` コマンド実行

## 📊 期待される改善

| 項目 | Python | Go | 改善率 |
|------|--------|-----|--------|
| メモリ | 200MB | 50MB | 75%削減 |
| 起動時間 | 15秒 | 5秒 | 70%短縮 |
| 重複送信 | 発生 | 0件 | 100%解決 |

## 🔍 デバッグポイント

### ログで確認すべきこと
```
✅ "Acquired advisory lock: weekly_reminder"
✅ "Reminder executed successfully"
❌ "Another pod is executing reminder" （2台目のPod）
```

### よくある問題
1. **VCチャット投稿失敗**: 専用チャットが無効 → サーバー設定で有効化
2. **ロックが取れない**: DB接続エラー → 接続文字列確認
3. **ロール表示されない**: roles.yamlの形式エラー → YAML構文確認

## 📝 実装の優先順位

### 最優先（Week 1-2）
1. ✅ プロジェクトセットアップ
2. ✅ データベース層（Advisory Lock実装）
3. ✅ 週次リマインダー（重複防止機能）

### 高優先（Week 2-3）
4. ✅ 自己紹介管理
5. ✅ VC入室通知
6. ✅ ロール表示

### 中優先（Week 3-4）
7. ✅ K8s対応
8. ✅ ヘルスチェック
9. ✅ Graceful Shutdown

---

## 🎬 実装開始コマンド

```bash
# アーカイブとプロジェクト初期化を実行
mkdir -p archive/python
git mv main.py database.py keep_alive.py requirements.txt Dockerfile .dockerignore archive/python/
go mod init github.com/your-username/profile-bot
mkdir -p cmd/bot internal/{bot,database,config,utils} configs deployments/{docker,k8s}
go get github.com/bwmarrin/discordgo github.com/jackc/pgx/v5/pgxpool github.com/robfig/cron/v3 gopkg.in/yaml.v3
```

**それでは、Step 1から順番に実装を開始してください！**
