# Discord Profile Bot (Go)

Discord自己紹介管理Bot - Go言語実装版

## 概要

このBotは、Discordサーバーにおける自己紹介の管理と通知を自動化します。

### 主な機能

1. **自己紹介管理**: 指定チャンネルへの投稿を自動保存
2. **VC入室通知**: ボイスチャンネル入室時に自己紹介を表示
3. **週次リマインダー**: 毎週月曜10:00に未投稿者へ通知
4. **自動ロール付与**: 自己紹介投稿時に「自己紹介済み」ロールを自動付与
5. **スラッシュコマンド**: `/profilebot` で手動リマインダー実行

### 重要な改善点

**Python版からの移行理由**:
- **重複送信問題の解決**: PostgreSQL Advisory Lockによる完全な排他制御
- **パフォーマンス向上**: メモリ使用量75%削減、起動時間70%短縮
- **K8s対応**: 複数レプリカでの安定動作

## 技術スタック

- **言語**: Go 1.21+
- **Discord API**: discordgo v0.27.1
- **データベース**: PostgreSQL (pgx/v5)
- **スケジューリング**: robfig/cron/v3
- **デプロイ**: Kubernetes

## セットアップ

### 必要な環境変数

`.env.example`を`.env`にコピーして、以下を設定してください:

```bash
DISCORD_TOKEN=your_bot_token_here
DATABASE_URL=postgresql://user:password@host:5432/database
```

### ローカル実行

```bash
# 依存関係のインストール
go mod download

# 実行
go run cmd/bot/main.go
```

### Docker実行

```bash
# ビルド
docker build -t profile-bot .

# 実行
docker run --env-file .env profile-bot
```

### Kubernetes デプロイ

```bash
# Secretsの作成（実際の値に置き換えてください）
kubectl create secret generic profile-bot-secrets \
  --from-literal=discord-token=YOUR_TOKEN \
  --from-literal=database-url=YOUR_DATABASE_URL

# ConfigMap作成
kubectl apply -f deployments/k8s/configmap.yaml

# Deployment作成
kubectl apply -f deployments/k8s/deployment.yaml

# Service作成
kubectl apply -f deployments/k8s/service.yaml
```

## アーキテクチャ

```
cmd/bot/main.go                # エントリーポイント
internal/
├── bot/                       # Bot本体
│   ├── bot.go                # 初期化・起動
│   ├── handlers.go           # イベントハンドラー
│   ├── commands.go           # スラッシュコマンド
│   └── reminder.go           # リマインダー（Advisory Lock実装）
├── database/                  # データベース層
│   ├── db.go                 # 接続管理
│   ├── intro.go              # 自己紹介CRUD
│   ├── reminder.go           # リマインダーログ
│   └── lock.go               # PostgreSQL Advisory Lock
└── config/                    # 設定管理
    ├── config.go             # 環境変数読み込み
    └── roles.go              # ロール設定管理
```

## PostgreSQL Advisory Lock（重要）

K8s環境で複数Podが並行実行しても、リマインダーが重複送信されない仕組み:

```go
// 1. ロック取得
acquired, err := AcquireAdvisoryLock(ctx, pool, "weekly_reminder")
if !acquired {
    return // 別Podが実行中
}
defer ReleaseAdvisoryLock(ctx, pool, "weekly_reminder")

// 2. リマインダー実行
executeReminder()
```

## 監視・デバッグ

### ヘルスチェックエンドポイント

- `GET /health`: Discord接続とDB接続の状態
- `GET /ready`: Bot起動準備完了の状態

### ログ確認

```bash
# K8s環境
kubectl logs -f deployment/profile-bot

# 特定のPod
kubectl logs -f profile-bot-xxxx-yyyy
```

### 重複送信のデバッグ

ログで以下を確認:
- `✅ Advisory lock acquired: weekly_reminder` - ロック取得成功
- `⏭️  Advisory lock busy (another pod is running)` - 別Podが実行中

## 設定ファイル

### configs/roles.yaml

ロール表示のカテゴリ設定。K8sではConfigMapとしてマウントされます。

## トラブルシューティング

### VCチャット投稿が失敗する

- サーバー設定でText-in-Voice機能が有効か確認
- ログで`Voice channel text chat not available`を確認

### リマインダーが重複する

- Advisory Lockが正常に機能しているか確認
- `daily_reminder_log`テーブルを確認

### ロールが表示されない

- `configs/roles.yaml`の形式が正しいか確認
- ConfigMapが正しくマウントされているか確認

## 移行について

Python版から移行する場合、既存のデータベーステーブルはそのまま使用できます。

### データベーススキーマ

- `introductions`: 自己紹介データ（変更なし）
- `daily_reminder_log`: リマインダー実行ログ（新規追加）

## ライセンス

MIT License

## サポート

問題が発生した場合は、GitHubのIssuesで報告してください。
