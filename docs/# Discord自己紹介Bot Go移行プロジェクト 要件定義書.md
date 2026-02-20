# Discord自己紹介Bot Go移行プロジェクト 要件定義書

**バージョン:** v5.0 Final  
**作成日:** 2025年11月3日  
**対象システム:** Discord自己紹介管理Bot  
**移行内容:** Python → Go言語  
**運用環境:** Kubernetes (K8s)  

---

## 📑 目次

1. [プロジェクト概要](#1-プロジェクト概要)
2. [現状の課題](#2-現状の課題)
3. [移行の目的と効果](#3-移行の目的と効果)
4. [システム要件](#4-システム要件)
5. [機能仕様](#5-機能仕様)
6. [データベース設計](#6-データベース設計)
7. [技術仕様](#7-技術仕様)
8. [プロジェクト構成](#8-プロジェクト構成)
9. [実装スケジュール](#9-実装スケジュール)
10. [移行手順](#10-移行手順)
11. [運用保守](#11-運用保守)
12. [付録](#12-付録)

---

## 1. プロジェクト概要

### 1.1 プロジェクト名
**Discord自己紹介Bot Go移行プロジェクト**

### 1.2 背景
現在Python（py-cord）で実装されている自己紹介管理botを、Go言語（discordgo）に移行する。

### 1.3 対象範囲
- **移行対象**: 自己紹介bot（profile-bot）のみ
- **対象外**: BUMPbot、守護神bot（別リポジトリで継続運用）

### 1.4 関係者

| 役割 | 担当 | 責任範囲 |
|------|------|----------|
| プロジェクトオーナー | あなた | 要件定義、受け入れテスト |
| 開発者 | Claude（私） | 設計、実装、テスト |
| インフラ管理者 | 友人 | K8s設定、デプロイ |

---

## 2. 現状の課題

### 2.1 技術的課題

#### 問題1: 重複メッセージ送信
**症状**
```
同じリマインダーメッセージが連続で複数回送信される
例：19:00に同じメッセージが3回送信
```

**原因**
- K8s環境で複数Pod（レプリカ）が並行実行
- `daily_reminder_task()`が各Podで独立実行
- データベースのレースコンディション
- 分散ロック機構の不在

**影響**
- ユーザー体験の悪化
- 通知チャンネルの混乱
- 管理負荷の増加

#### 問題2: パフォーマンス
- メモリ使用量：約200MB（Flask + Bot）
- 起動時間：15-20秒
- 並行処理の複雑さ（asyncio）

### 2.2 運用上の課題
- デプロイの複雑さ（Python依存関係）
- K8sとの親和性の低さ
- エラー時のデバッグ困難

---

## 3. 移行の目的と効果

### 3.1 主目的
1. **重複送信問題の解決** - 分散ロック実装
2. **パフォーマンス改善** - メモリ使用量1/4削減
3. **運用性向上** - シングルバイナリデプロイ

### 3.2 期待効果

| 項目 | 現状（Python） | 目標（Go） | 改善率 |
|------|---------------|-----------|--------|
| メモリ使用量 | 200MB | 50MB | 75%削減 |
| 起動時間 | 15-20秒 | 5秒以下 | 70%短縮 |
| デプロイサイズ | 100MB+ | 20MB以下 | 80%削減 |
| 重複送信 | 発生中 | 0件 | 100%解決 |

### 3.3 副次的効果
- K8s管理者（友人）の負担軽減
- 将来的な機能追加の容易化
- モニタリング・ログの改善

---

## 4. システム要件

### 4.1 機能要件

#### FR1: 自己紹介管理機能
- **FR1.1** 指定チャンネルの投稿監視
- **FR1.2** 非botメッセージのDB保存
- **FR1.3** ユーザーごとの最新自己紹介管理

#### FR2: VC入室通知機能
- **FR2.1** 指定VCの入室検知
- **FR2.2** VCチャット（Text-in-Voice）への投稿
- **FR2.3** ロール情報の表示
- **FR2.4** 自己紹介本文の表示

#### FR3: 週次リマインダー機能
- **FR3.1** 毎週月曜10:00実行
- **FR3.2** 自己紹介未投稿者の抽出
- **FR3.3** 通知メッセージ送信
- **FR3.4** 重複送信の完全防止

#### FR4: 自動役職付与機能
- **FR4.1** 自己紹介投稿時の検知
- **FR4.2** 「自己紹介済み」ロール付与
- **FR4.3** 重複付与の防止

#### FR5: 管理コマンド機能
- **FR5.1** `/profilebot` - リマインダー手動実行

### 4.2 非機能要件

#### NFR1: パフォーマンス
- **NFR1.1** メッセージ応答時間：500ms以内
- **NFR1.2** VC入室通知遅延：2秒以内
- **NFR1.3** メモリ使用量：100MB以下
- **NFR1.4** 起動時間：10秒以内

#### NFR2: 可用性
- **NFR2.1** アップタイム：99.9%
- **NFR2.2** Graceful Shutdown対応
- **NFR2.3** 自動再起動対応

#### NFR3: スケーラビリティ
- **NFR3.4** K8s水平スケーリング対応
- **NFR3.5** 複数レプリカ対応
- **NFR3.6** 定時ジョブのLeader Election

#### NFR4: 保守性
- **NFR4.1** 構造化ログ（JSON形式）
- **NFR4.2** 設定の外部化
- **NFR4.3** ヘルスチェックエンドポイント

#### NFR5: セキュリティ
- **NFR5.1** 環境変数による秘密情報管理
- **NFR5.2** K8s Secretsとの統合

---

## 5. 機能仕様

### 5.1 自己紹介チャンネル監視

#### 仕様
```yaml
監視対象:
  チャンネルID: 1300659373227638794
  対象: 非botユーザーの全投稿

動作:
  - メッセージ投稿を検知
  - データベースに保存（UPSERT）
  - 「自己紹介済み」ロール付与
  
保存データ:
  - user_id (BIGINT)
  - channel_id (BIGINT)
  - message_id (BIGINT)
  - created_at (TIMESTAMP)
```

#### 起動時の初期スキャン
```yaml
処理内容:
  - 過去3000件のメッセージをスキャン
  - 既存データと比較
  - 新規データのみ保存
  
ログ出力:
  - スキャン進捗（100件ごと）
  - 新規追加件数
  - 更新件数
  - 最終件数
```

### 5.2 VC入室通知機能

#### 対象VCチャンネル
```yaml
監視対象VC (8チャンネル):
  - 1300291307750559754
  - 1302151049368571925
  - 1302151154981011486
  - 1306190768431431721
  - 1306190915483734026
  - 1403273245360259163
  - 1404396375965433926
  - 1384813451813191752

除外ユーザー:
  - 533698325203910668
  - 916300992612540467
  - 1300226846599675974
```

#### 投稿先の決定ロジック
```
1. ユーザーがVC入室を検知
   ↓
2. VCの専用チャット（Text-in-Voice）を取得
   ↓
3. チャット投稿を試行
   ├─ 成功 → 完了
   └─ 失敗 → 何もしない（通知なし）
```

#### 表示内容

##### パターンA: 自己紹介あり・ロールあり
```
━━━━━━━━━━━━━━━━━━━
👤 田中太郎 さんが入室しました

📋 プロフィール
━━━━━━━━━━━━━━━━━━━

【障害】
🟢 ASD（自閉症スペクトラム障害）
⚡ ADHD（注意欠如・多動性障害）

【性別】
👨 男性

【手帳】
📗 療育手帳

【コミュニケーション】
📞 通話OK
✅ OKフレンド申請OK

━━━━━━━━━━━━━━━━━━━
📝 自己紹介

こんにちは！よろしくお願いします。
ゲームと音楽が好きです🎮🎵

[元の自己紹介を見る]
━━━━━━━━━━━━━━━━━━━
```

##### パターンB: 自己紹介あり・ロールなし
```
━━━━━━━━━━━━━━━━━━━
👤 田中太郎 さんが入室しました

📝 自己紹介

こんにちは！よろしくお願いします。

[元の自己紹介を見る]
━━━━━━━━━━━━━━━━━━━
```

##### パターンC: 自己紹介なし
```
━━━━━━━━━━━━━━━━━━━
👤 田中太郎 さんが入室しました

⚠️ この方の自己紹介はまだ投稿されていません
━━━━━━━━━━━━━━━━━━━
```

### 5.3 ロール表示機能

#### 表示対象ロール（カテゴリ別）

```yaml
障害カテゴリ:
  - { name: "うつ病", emoji: "💙" }
  - { name: "ASD（自閉症スペクトラム障害）", emoji: "🟢" }
  - { name: "ADHD（注意欠如・多動性障害）", emoji: "⚡" }
  - { name: "発達障害", emoji: "🧠" }
  - { name: "グレーゾーン", emoji: "🌀" }
  - { name: "双極性障害", emoji: "💫" }
  - { name: "てんかん", emoji: "⚠️" }
  - { name: "境界性パーソナリティ", emoji: "💗" }
  - { name: "視覚障害", emoji: "👁️" }

性別カテゴリ:
  - { name: "男性", emoji: "👨" }
  - { name: "女性", emoji: "👩" }

手帳カテゴリ:
  - { name: "身体手帳", emoji: "🟦" }
  - { name: "療育手帳", emoji: "📗" }
  - { name: "精神手帳", emoji: "💚" }

コミュニケーションカテゴリ:
  - { name: "通話OK", emoji: "📞" }
  - { name: "OKフレンド申請OK", emoji: "✅" }
  - { name: "NGフレンド申請NG", emoji: "❌" }
  - { name: "フレンド申請(要相談)", emoji: "⚠️" }
```

#### 除外ロール
```yaml
除外条件:
  - ロール名が "Carl-bot"
  - ロール名が "@everyone"
  - Managed（bot管理）フラグがtrue
  - ロール名末尾が "bot" または "Bot"
```

#### 特殊処理
```yaml
「(聴覚者)」の扱い:
  - ロール名から自動削除
  - 例: "女性（聴覚者）" → "女性"
  - 例: "精神手帳（聴覚者）" → "精神手帳"
```

### 5.4 週次リマインダー機能

#### 実行タイミング
```yaml
スケジュール:
  頻度: 週1回
  曜日: 月曜日
  時刻: 10:00 (JST)
  
実行方式:
  - cronジョブとして実装
  - PostgreSQL Advisory Lockで排他制御
  - Leader Electionによる単一実行保証
```

#### 対象ユーザー抽出
```sql
-- 自己紹介未投稿メンバーの抽出
SELECT m.id, m.username, m.discriminator
FROM guild_members m
LEFT JOIN introductions i ON m.user_id = i.user_id
WHERE i.user_id IS NULL
  AND m.bot = false
ORDER BY m.joined_at DESC;
```

#### 送信メッセージ
```yaml
送信先:
  チャンネルID: 1331177944244289598 (通知チャンネル)

メッセージ形式:
  - 対象メンバー10名まで表示（@なし）
  - 10名超の場合: "ほかN名"
  - 自己紹介チャンネルへのリンク
  
例:
  🌟 **自己紹介のお知らせ** 🌟
  
  ken024934, haruccha02751, agasht, kawausot, 
  53kieroww, sick_fox2983_42410, warabi63, 
  takahashihina, takana_2475, seeco999 ほか61名の皆さん
  
  こんにちは！<#1300659373227638794> チャンネルでの
  自己紹介をお待ちしています！
  書ける範囲で構いませんので、あなたのことを教えてください 😊
  趣味、好きなこと、最近気になっていることなど、何でも大丈夫です！
```

#### 重複送信防止機構
```go
// PostgreSQL Advisory Lockによる実装
func executeWeeklyReminder(ctx context.Context) error {
    conn, err := db.Acquire(ctx)
    if err != nil {
        return err
    }
    defer conn.Release()
    
    // ロック取得（キー: "weekly_reminder"のハッシュ値）
    acquired, err := conn.Exec(ctx,
        "SELECT pg_try_advisory_lock(hashtext('weekly_reminder'))")
    
    if !acquired {
        log.Info("別のPodがリマインダーを実行中")
        return nil
    }
    
    defer conn.Exec(ctx,
        "SELECT pg_advisory_unlock(hashtext('weekly_reminder'))")
    
    // 今日既に実行済みかチェック
    today := time.Now().Format("2006-01-02")
    var exists bool
    err = conn.QueryRow(ctx,
        "SELECT EXISTS(SELECT 1 FROM daily_reminder_log WHERE reminder_date = $1)",
        today).Scan(&exists)
    
    if exists {
        log.Info("今日は既に実行済み")
        return nil
    }
    
    // リマインダー実行
    // ...
    
    // ログ記録
    _, err = conn.Exec(ctx,
        "INSERT INTO daily_reminder_log (reminder_date, notified_users) VALUES ($1, $2)",
        today, notifiedUsers)
    
    return err
}
```

### 5.5 自動役職付与機能

#### トリガー
```yaml
検知イベント:
  - 自己紹介チャンネルへの投稿
  - 投稿者が非botユーザー
  
処理内容:
  1. メッセージ投稿を検知
  2. データベースに保存
  3. 「自己紹介済み」ロールを付与
  4. 既に持っている場合はスキップ
```

#### ロール情報
```yaml
ロール名: "自己紹介済み"
動作:
  - サーバーから既存ロールを検索
  - ロールIDを取得
  - ユーザーに付与（AddMemberRole）
  - エラー時はログ記録のみ（処理継続）
```

### 5.6 管理コマンド

#### `/profilebot` コマンド
```yaml
説明: 自己紹介リマインダーをテスト実行

権限: 全メンバー（制限なし）

動作:
  - コマンド実行者のみに結果を表示（ephemeral）
  - 重複送信チェックをスキップ（force=true）
  - 実際に通知チャンネルにメッセージ送信
  - 実行結果をコマンド実行者に返信
  
レスポンス例:
  🔄 **プロフィールリマインダー実行結果**
  ✅ 自己紹介リマインダーを送信しました (71名対象)
```

---

## 6. データベース設計

### 6.1 使用データベース
```yaml
サービス: Supabase
タイプ: PostgreSQL
接続方法: 環境変数 DATABASE_URL
```

### 6.2 テーブル定義

#### 6.2.1 introductions テーブル
```sql
CREATE TABLE IF NOT EXISTS introductions (
    user_id BIGINT PRIMARY KEY,
    channel_id BIGINT NOT NULL,
    message_id BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_introductions_user_id 
ON introductions(user_id);

CREATE INDEX IF NOT EXISTS idx_introductions_created_at 
ON introductions(created_at DESC);
```

**カラム説明**
| カラム | 型 | 説明 |
|--------|-----|------|
| user_id | BIGINT | DiscordユーザーID（主キー） |
| channel_id | BIGINT | 自己紹介チャンネルID |
| message_id | BIGINT | 自己紹介メッセージID |
| created_at | TIMESTAMP | 投稿日時 |

#### 6.2.2 daily_reminder_log テーブル
```sql
CREATE TABLE IF NOT EXISTS daily_reminder_log (
    id SERIAL PRIMARY KEY,
    reminder_date DATE NOT NULL DEFAULT CURRENT_DATE,
    notified_users TEXT[],
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_reminder_date 
ON daily_reminder_log(reminder_date);
```

**カラム説明**
| カラム | 型 | 説明 |
|--------|-----|------|
| id | SERIAL | 連番ID |
| reminder_date | DATE | リマインダー実行日 |
| notified_users | TEXT[] | 通知対象ユーザーIDリスト |
| created_at | TIMESTAMP | 記録日時 |

### 6.3 他システムとの共有

#### 他botが使用するテーブル（参照のみ）
```yaml
BUMPbot用:
  - users (bump_count管理)
  - reminders (2時間リマインダー)
  - settings (システム設定)

守護神bot用:
  - reports (通報記録)
  - guild_settings (サーバー設定)
  - report_cooldowns (クールダウン管理)

原則:
  - 自己紹介botからの変更禁止
  - 読み取り専用でのアクセスのみ許可
```

---

## 7. 技術仕様

### 7.1 開発環境

#### 言語・バージョン
```yaml
言語: Go
バージョン: 1.21以上
モジュールモード: 有効
```

#### 主要ライブラリ
```yaml
Discord:
  - github.com/bwmarrin/discordgo v0.27.1
  
Database:
  - github.com/jackc/pgx/v5 v5.5.0
  
Cron:
  - github.com/robfig/cron/v3 v3.0.1
  
Logging:
  - github.com/rs/zerolog v1.31.0
  
Configuration:
  - github.com/kelseyhightower/envconfig v1.5.0
  - gopkg.in/yaml.v3 v3.0.1
```

### 7.2 アーキテクチャ

#### レイヤー構成
```
┌─────────────────────────────────┐
│      Presentation Layer         │
│  (Discord Event Handlers)       │
├─────────────────────────────────┤
│      Application Layer          │
│   (Business Logic / Use Cases)  │
├─────────────────────────────────┤
│      Domain Layer               │
│   (Entities / Value Objects)    │
├─────────────────────────────────┤
│   Infrastructure Layer          │
│  (Database / External Services) │
└─────────────────────────────────┘
```

#### ディレクトリ構造
```
profile-bot-go/
├─ cmd/
│   └─ bot/
│       └─ main.go                 # エントリーポイント
│
├─ internal/
│   ├─ bot/
│   │   └─ bot.go                  # Bot初期化・起動
│   │
│   ├─ handlers/                   # イベントハンドラ
│   │   ├─ voice.go                # VC入室ハンドラ
│   │   ├─ message.go              # メッセージハンドラ
│   │   ├─ reminder.go             # リマインダー
│   │   └─ command.go              # スラッシュコマンド
│   │
│   ├─ services/                   # ビジネスロジック
│   │   ├─ intro.go                # 自己紹介サービス
│   │   ├─ role.go                 # ロール管理サービス
│   │   └─ notification.go         # 通知サービス
│   │
│   ├─ repository/                 # データアクセス
│   │   ├─ intro.go                # 自己紹介リポジトリ
│   │   └─ reminder.go             # リマインダーリポジトリ
│   │
│   ├─ discord/                    # Discord関連ユーティリティ
│   │   ├─ embed.go                # Embed生成
│   │   ├─ role.go                 # ロール情報取得
│   │   └─ voice.go                # VCチャット操作
│   │
│   ├─ database/                   # DB接続管理
│   │   └─ postgres.go             # PostgreSQL接続プール
│   │
│   └─ config/                     # 設定管理
│       ├─ config.go               # 環境変数読み込み
│       └─ roles.go                # ロール設定読み込み
│
├─ configs/
│   └─ roles.yaml                  # ロール表示設定
│
├─ deployments/
│   └─ k8s/
│       ├─ deployment.yaml         # K8s Deployment
│       ├─ service.yaml            # K8s Service
│       └─ configmap.yaml          # ConfigMap
│
├─ scripts/
│   ├─ migrate.sh                  # データ移行スクリプト
│   └─ test.sh                     # テストスクリプト
│
├─ Dockerfile                      # Dockerイメージビルド
├─ .dockerignore
├─ go.mod                          # Go依存関係
├─ go.sum
├─ .env.example                    # 環境変数テンプレート
├─ .gitignore
└─ README.md                       # ドキュメント
```

### 7.3 設定管理

#### 環境変数
```bash
# .env.example

# Discord
DISCORD_TOKEN=your_bot_token_here
INTRODUCTION_CHANNEL_ID=1300659373227638794
NOTIFICATION_CHANNEL_ID=1331177944244289598

# Database
DATABASE_URL=postgresql://user:password@host:5432/database

# Application
LOG_LEVEL=info
ENVIRONMENT=production
PORT=8080
```

#### ロール設定ファイル
```yaml
# configs/roles.yaml

role_categories:
  障害:
    display_name: "【障害】"
    roles:
      - name: "うつ病"
        emoji: "💙"
      - name: "ASD（自閉症スペクトラム障害）"
        emoji: "🟢"
      - name: "ADHD（注意欠如・多動性障害）"
        emoji: "⚡"
      - name: "発達障害"
        emoji: "🧠"
      - name: "グレーゾーン"
        emoji: "🌀"
      - name: "双極性障害"
        emoji: "💫"
      - name: "てんかん"
        emoji: "⚠️"
      - name: "境界性パーソナリティ"
        emoji: "💗"
      - name: "視覚障害"
        emoji: "👁️"
  
  性別:
    display_name: "【性別】"
    roles:
      - name: "男性"
        emoji: "👨"
      - name: "女性"
        emoji: "👩"
  
  手帳:
    display_name: "【手帳】"
    roles:
      - name: "身体手帳"
        emoji: "🟦"
      - name: "療育手帳"
        emoji: "📗"
      - name: "精神手帳"
        emoji: "💚"
  
  コミュニケーション:
    display_name: "【コミュニケーション】"
    roles:
      - name: "通話OK"
        emoji: "📞"
      - name: "OKフレンド申請OK"
        emoji: "✅"
      - name: "NGフレンド申請NG"
        emoji: "❌"
      - name: "フレンド申請(要相談)"
        emoji: "⚠️"

exclude_roles:
  - "Carl-bot"
  - "@everyone"

exclude_patterns:
  - ".*[Bb]ot$"
```

### 7.4 Docker化

#### Dockerfile
```dockerfile
# Multi-stage build

# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /build

# 依存関係のキャッシュ
COPY go.mod go.sum ./
RUN go mod download

# ソースコードコピー
COPY . .

# ビルド
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s" -o bot ./cmd/bot

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# ビルド成果物をコピー
COPY --from=builder /build/bot .
COPY --from=builder /build/configs ./configs

# 非rootユーザーで実行
RUN adduser -D -u 1000 botuser
USER botuser

# ヘルスチェック
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

EXPOSE 8080

CMD ["./bot"]
```

#### .dockerignore
```
.git
.github
.env
*.md
scripts/
deployments/
.vscode
.idea
```

### 7.5 Kubernetes設定

#### Deployment
```yaml
# deployments/k8s/deployment.yaml

apiVersion: apps/v1
kind: Deployment
metadata:
  name: profile-bot
  labels:
    app: profile-bot
spec:
  replicas: 2
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  selector:
    matchLabels:
      app: profile-bot
  template:
    metadata:
      labels:
        app: profile-bot
    spec:
      containers:
      - name: bot
        image: ghcr.io/your-repo/profile-bot:latest
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: DISCORD_TOKEN
          valueFrom:
            secretKeyRef:
              name: discord-secrets
              key: token
        - name: DATABASE_URL
          valueFrom:
            secretKeyRef:
              name: database-secrets
              key: url
        - name: LOG_LEVEL
          value: "info"
        - name: ENVIRONMENT
          value: "production"
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "128Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        volumeMounts:
        - name: config
          mountPath: /app/configs
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: profile-bot-config
```

#### Service
```yaml
# deployments/k8s/service.yaml

apiVersion: v1
kind: Service
metadata:
  name: profile-bot
spec:
  selector:
    app: profile-bot
  ports:
  - port: 8080
    targetPort: 8080
    name: http
  type: ClusterIP
```

#### ConfigMap
```yaml
# deployments/k8s/configmap.yaml

apiVersion: v1
kind: ConfigMap
metadata:
  name: profile-bot-config
data:
  roles.yaml: |
    # ロール設定をここに記述
    role_categories:
      # ... (省略)
```

---

## 8. プロジェクト構成

### 8.1 リポジトリ管理

#### ブランチ戦略
```
main          # 本番環境
├─ develop    # 開発環境
└─ feature/*  # 機能開発
```

#### Git運用
```bash
# 現在のPythonコードは削除せず、別ディレクトリに移動
mkdir -p archive/python
git mv *.py archive/python/
git mv requirements.txt archive/python/

# Goコードを配置
# ...

git add .
git commit -m "feat: Migrate to Go implementation"
```

### 8.2 バージョン管理

#### セマンティックバージョニング
```
v1.0.0  # 初回リリース（Go版）
v1.1.0  # 機能追加
v1.1.1  # バグ修正
```

---

## 9. 実装スケジュール

### 9.1 全体スケジュール（4週間）

```
Week 1: プロジェクトセットアップ
Week 2: コア機能実装
Week 3: 追加機能・K8s対応
Week 4: テスト・デプロイ
```

### 9.2 詳細スケジュール

#### Week 1: プロジェクトセットアップ（3-4日）
```
Day 1-2: 環境構築
├─ Goプロジェクト初期化
├─ ディレクトリ構造作成
├─ 基本的なBot接続確認
└─ Supabase接続テスト

Day 3-4: 設定・ユーティリティ
├─ roles.yaml作成
├─ 環境変数管理実装
├─ ログ基盤構築
└─ ヘルスチェックエンドポイント
```

#### Week 2: コア機能実装（5-6日）
```
Day 1-2: 自己紹介機能
├─ チャンネル監視実装
├─ DB保存ロジック
├─ 起動時スキャン機能
└─ CRUD操作実装

Day 3-4: VC入室通知
├─ VC入室検知
├─ 自己紹介取得ロジック
├─ VCチャット投稿実装
└─ エラーハンドリング

Day 5-6: ロール表示
├─ ロール情報取得
├─ カテゴリ別整理
├─ Embed生成
└─ 表示テスト
```

#### Week 3: 追加機能・K8s対応（5-6日）
```
Day 1-2: リマインダー
├─ cronジョブ実装
├─ 未投稿者抽出
├─ PostgreSQL Advisory Lock
└─ 重複送信防止テスト

Day 3-4: 自動役職・コマンド
├─ 役職付与実装
├─ /profilebot コマンド
├─ エラーハンドリング
└─ 動作確認

Day 5-6: K8s対応
├─ Dockerfile作成
├─ K8s YAMLファイル作成
├─ Leader Election実装
└─ ローカルK8sテスト（minikube）
```

#### Week 4: テスト・デプロイ（3-4日）
```
Day 1: 統合テスト
├─ テストサーバーデプロイ
├─ 全機能の動作確認
├─ エッジケーステスト
└─ パフォーマンステスト

Day 2: K8s設定調整
├─ 友人と設定確認
├─ Secrets設定
├─ ConfigMap設定
└─ デプロイ手順書作成

Day 3: 本番デプロイ
├─ Blue-Green Deployment
├─ 旧Python bot停止
├─ Go bot起動
└─ 監視設定

Day 4: 最終確認
├─ 動作確認
├─ ログ確認
├─ ドキュメント整備
└─ 引き継ぎ
```

---

## 10. 移行手順

### 10.1 移行の流れ

#### Phase 1: 準備（1日）
```bash
# 1. 現在のbotのバックアップ
kubectl get deployment profile-bot -o yaml > backup-deployment.yaml

# 2. データベースのバックアップ
pg_dump $DATABASE_URL > backup.sql

# 3. 設定確認
# - SECRET名の確認
# - 環境変数の確認
# - レプリカ数の確認
```

#### Phase 2: デプロイ（1日）
```bash
# 1. Docker imageのビルド・プッシュ
docker build -t ghcr.io/your-repo/profile-bot:v1.0.0 .
docker push ghcr.io/your-repo/profile-bot:v1.0.0

# 2. K8s ConfigMap作成
kubectl apply -f deployments/k8s/configmap.yaml

# 3. 新しいDeploymentをテスト（レプリカ1）
kubectl apply -f deployments/k8s/deployment.yaml

# 4. 動作確認
kubectl logs -f deployment/profile-bot

# 5. 問題なければレプリカ増加
kubectl scale deployment profile-bot --replicas=2
```

#### Phase 3: 切り替え（30分）
```bash
# 1. 旧Python botを停止
kubectl scale deployment profile-bot-python --replicas=0

# 2. Go botが正常動作していることを確認
# - VCに入室してテスト
# - 自己紹介投稿テスト
# - /profilebot コマンドテスト

# 3. 問題なければ旧Deploymentを削除
kubectl delete deployment profile-bot-python
```

#### Phase 4: 監視（1週間）
```bash
# ログ監視
kubectl logs -f -l app=profile-bot

# リソース使用量確認
kubectl top pod -l app=profile-bot

# エラー確認
kubectl get events --sort-by='.lastTimestamp'
```

### 10.2 ロールバック手順

問題が発生した場合：
```bash
# 1. Go botを停止
kubectl scale deployment profile-bot --replicas=0

# 2. Python botを再起動
kubectl apply -f backup-deployment.yaml
kubectl scale deployment profile-bot-python --replicas=2

# 3. 動作確認
# ...
```

---

## 11. 運用保守

### 11.1 ログ管理

#### ログレベル
```
ERROR  # エラー（要対応）
WARN   # 警告（監視必要）
INFO   # 情報（通常動作）
DEBUG  # デバッグ（開発時のみ）
```

#### ログ形式（JSON）
```json
{
  "level": "info",
  "time": "2025-11-03T10:00:00+09:00",
  "message": "User joined voice channel",
  "user_id": "123456789",
  "username": "田中太郎",
  "channel_id": "987654321",
  "channel_name": "雑談"
}
```

### 11.2 監視項目

#### ヘルスチェック
```go
// /health エンドポイント
func healthCheck(w http.ResponseWriter, r *http.Request) {
    checks := map[string]bool{
        "discord": session.DataReady,
        "database": db.Ping() == nil,
    }
    
    allHealthy := true
    for _, healthy := range checks {
        if !healthy {
            allHealthy = false
            break
        }
    }
    
    status := http.StatusOK
    if !allHealthy {
        status = http.StatusServiceUnavailable
    }
    
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(checks)
}
```

#### メトリクス
```
# Prometheus形式
bot_events_total{type="voice_join"} 1234
bot_messages_sent_total{type="introduction"} 567
bot_errors_total{type="database"} 2
bot_reminder_executions_total{status="success"} 45
```

### 11.3 トラブルシューティング

#### 問題1: VCチャット投稿失敗
```
症状: 自己紹介が表示されない
原因: VCの専用チャットが無効

対処:
1. ログで "failed to send to voice channel" を確認
2. サーバー設定で専用チャットを有効化
3. または代替チャンネルを設定
```

#### 問題2: リマインダー重複送信
```
症状: 同じメッセージが複数回送信
原因: Advisory Lockが機能していない

対処:
1. データベース接続を確認
2. ログで "acquired lock" を確認
3. daily_reminder_log テーブルを確認
```

#### 問題3: ロールが表示されない
```
症状: プロフィール欄が空
原因: roles.yamlの設定ミス

対処:
1. ConfigMapの内容を確認
2. ロール名の完全一致を確認
3. 除外設定を確認
```

### 11.4 設定変更手順

#### ロール設定の変更
```bash
# 1. roles.yamlを編集
vim configs/roles.yaml

# 2. ConfigMapを更新
kubectl create configmap profile-bot-config \
  --from-file=configs/roles.yaml \
  --dry-run=client -o yaml | kubectl apply -f -

# 3. Podを再起動
kubectl rollout restart deployment/profile-bot
```

#### 監視VCの追加
```bash
# 1. 環境変数を更新（コード修正が必要）
# main.goのTARGET_VOICE_CHANNELSに追加

# 2. 再ビルド・デプロイ
docker build -t ghcr.io/your-repo/profile-bot:v1.1.0 .
docker push ghcr.io/your-repo/profile-bot:v1.1.0
kubectl set image deployment/profile-bot \
  bot=ghcr.io/your-repo/profile-bot:v1.1.0
```

---

## 12. 付録

### 12.1 用語集

| 用語 | 説明 |
|------|------|
| VC | ボイスチャンネル（Voice Channel） |
| Text-in-Voice | VCの専用テキストチャット機能 |
| Embed | Discord埋め込みメッセージ |
| K8s | Kubernetes（コンテナオーケストレーション） |
| Pod | K8sの最小デプロイ単位 |
| Replica | 複製されたPod |
| Advisory Lock | PostgreSQLの排他制御機構 |
| Leader Election | 複数インスタンスから1つを選出する仕組み |
| UPSERT | INSERT + UPDATE（存在すれば更新、なければ挿入） |

### 12.2 参考資料

#### 公式ドキュメント
- [Discord Developer Portal](https://discord.com/developers/docs)
- [discordgo Documentation](https://pkg.go.dev/github.com/bwmarrin/discordgo)
- [PostgreSQL Advisory Locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)
- [Kubernetes Documentation](https://kubernetes.io/docs/)

#### Go言語
- [Effective Go](https://go.dev/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)

### 12.3 連絡先

| 役割 | 連絡方法 | 対応範囲 |
|------|----------|----------|
| あなた | Discord DM | 要件確認、受け入れテスト |
| 友人（K8s管理者） | （後日確認） | デプロイ、インフラ |
| 開発者（Claude） | このチャット | 実装、技術相談 |

### 12.4 承認記録

```
要件定義書 v5.0
承認日: 2025年11月3日
承認者: [あなたの名前]
```

---

## 変更履歴

| バージョン | 日付 | 変更内容 |
|-----------|------|----------|
| v1.0 | 2025-11-03 | 初版作成 |
| v2.0 | 2025-11-03 | VCチャット投稿仕様追加 |
| v3.0 | 2025-11-03 | データベースをSupabaseに変更 |
| v4.0 | 2025-11-03 | ロール表示仕様確定 |
| v5.0 | 2025-11-03 | 最終版（全要件確定） |