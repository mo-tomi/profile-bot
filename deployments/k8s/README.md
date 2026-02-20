# Kubernetes Deployment Guide

このガイドでは、Profile BotをKubernetesクラスターにデプロイする手順を説明します。

## 📋 前提条件

- Kubernetesクラスターへのアクセス（kubectl設定済み）
- Docker Hubまたはコンテナレジストリへのアクセス
- Discord Bot Token
- PostgreSQL Database URL

## 🚀 デプロイ手順

### 1. Dockerイメージをビルド

```bash
# リポジトリのルートディレクトリで実行
docker build -t ghcr.io/tomim/profile-bot:latest .

# イメージをプッシュ（GitHub Container Registryの例）
docker push ghcr.io/tomim/profile-bot:latest
```

**注意:** `ghcr.io/tomim/profile-bot` は適切なレジストリパスに変更してください。

### 2. Kubernetes Secretを作成

Bot用の機密情報（トークン、データベースURL）をSecretとして登録します。

```bash
# Secretを作成
kubectl create secret generic profile-bot-secrets \
  --from-literal=discord-token='あなたのDiscordBotトークン' \
  --from-literal=database-url='あなたのPostgreSQLデータベースURL'
```

**例:**
```bash
kubectl create secret generic profile-bot-secrets \
  --from-literal=discord-token='YOUR_DISCORD_BOT_TOKEN_HERE' \
  --from-literal=database-url='postgresql://user:pass@host:5432/dbname'
```

### 3. ConfigMapを適用

ロール設定ファイルをConfigMapとして登録します。

```bash
kubectl apply -f deployments/k8s/configmap.yaml
```

### 4. Deploymentを適用

Botをデプロイします（2レプリカで起動）。

```bash
kubectl apply -f deployments/k8s/deployment.yaml
```

### 5. Serviceを適用（オプション）

ヘルスチェック用のServiceを作成します。

```bash
kubectl apply -f deployments/k8s/service.yaml
```

## ✅ デプロイメント確認

### Podの状態を確認

```bash
kubectl get pods -l app=profile-bot
```

**期待される出力:**
```
NAME                           READY   STATUS    RESTARTS   AGE
profile-bot-xxxxxxxxxx-xxxxx   1/1     Running   0          30s
profile-bot-xxxxxxxxxx-xxxxx   1/1     Running   0          30s
```

### ログを確認

```bash
# 特定のPodのログを表示
kubectl logs -f deployment/profile-bot

# すべてのPodのログを表示
kubectl logs -f -l app=profile-bot
```

**期待されるログ:**
```
🚀 Starting Profile Bot...
✅ Config loaded (Environment: production, Log Level: info)
✅ Roles config loaded (4 categories)
✅ Database connection established
✅ Database tables initialized
✅ Discord bot started successfully
✅ Bot logged in as: 自己紹介bot#8868
```

### ヘルスチェックを確認

```bash
# Podにポートフォワード
kubectl port-forward deployment/profile-bot 8080:8080

# 別のターミナルでヘルスチェック
curl http://localhost:8080/health
# 出力: OK

curl http://localhost:8080/ready
# 出力: Ready
```

## 🔧 設定のカスタマイズ

### レプリカ数の変更

`deployment.yaml` の `replicas` を編集:

```yaml
spec:
  replicas: 3  # レプリカ数を変更
```

適用:
```bash
kubectl apply -f deployments/k8s/deployment.yaml
```

### リソース制限の変更

`deployment.yaml` の `resources` セクションを編集:

```yaml
resources:
  requests:
    memory: "128Mi"
    cpu: "200m"
  limits:
    memory: "256Mi"
    cpu: "1000m"
```

### 環境変数の変更

`deployment.yaml` の `env` セクションを編集:

```yaml
env:
- name: LOG_LEVEL
  value: "debug"  # infoからdebugに変更
- name: INTRODUCTION_CHANNEL_ID
  value: "新しいチャンネルID"
```

## 🔄 更新手順

### 新しいバージョンのデプロイ

```bash
# 1. 新しいDockerイメージをビルド＆プッシュ
docker build -t ghcr.io/tomim/profile-bot:v1.1.0 .
docker push ghcr.io/tomim/profile-bot:v1.1.0

# 2. deployment.yamlのイメージタグを更新
# image: ghcr.io/tomim/profile-bot:v1.1.0

# 3. デプロイメントを更新
kubectl apply -f deployments/k8s/deployment.yaml

# 4. ローリングアップデートの進行状況を確認
kubectl rollout status deployment/profile-bot
```

### ロールバック

```bash
# 前のバージョンにロールバック
kubectl rollout undo deployment/profile-bot

# 特定のリビジョンにロールバック
kubectl rollout undo deployment/profile-bot --to-revision=2

# ロールアウト履歴を確認
kubectl rollout history deployment/profile-bot
```

## 🗑️ デプロイメント削除

```bash
# すべてのリソースを削除
kubectl delete -f deployments/k8s/deployment.yaml
kubectl delete -f deployments/k8s/service.yaml
kubectl delete -f deployments/k8s/configmap.yaml
kubectl delete secret profile-bot-secrets
```

## 🐛 トラブルシューティング

### Podが起動しない

```bash
# Podの詳細を確認
kubectl describe pod -l app=profile-bot

# イベントを確認
kubectl get events --sort-by='.lastTimestamp'
```

**よくある問題:**
- `ImagePullBackOff`: Dockerイメージが見つからない → レジストリパスを確認
- `CrashLoopBackOff`: Bot起動時にエラー → ログを確認
- `Pending`: リソース不足 → ノードのリソースを確認

### Botがメッセージを送信しない

```bash
# ログでエラーを確認
kubectl logs -f -l app=profile-bot | grep "❌"

# データベース接続を確認
kubectl logs -f -l app=profile-bot | grep "Database"
```

### 複数レプリカで重複メッセージが送信される

- PostgreSQL Advisory Lockが正しく動作していることを確認
- ログで `Acquiring advisory lock` を確認

```bash
kubectl logs -f -l app=profile-bot | grep "advisory"
```

## 📊 モニタリング

### リソース使用状況

```bash
# CPU/メモリ使用状況を確認
kubectl top pod -l app=profile-bot

# ノード全体のリソースを確認
kubectl top nodes
```

### メトリクス（Prometheusがある場合）

ヘルスチェックエンドポイント（`/health`, `/ready`）をPrometheusでモニタリングできます。

## 🔐 セキュリティ

### Secretの管理

本番環境では、以下のツールを使用してSecretを管理することを推奨します：

- **Sealed Secrets**: Secretを暗号化してGitにコミット
- **External Secrets Operator**: 外部のシークレット管理システムと連携
- **Vault**: HashiCorp Vaultと連携

### RBAC設定

必要に応じて、BotにRole-Based Access Control (RBAC) を設定します。

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: profile-bot
---
# 必要な権限をRoleで定義
```

## 📝 その他の注意事項

### データベース接続

- データベースはKubernetesクラスター外にあることを推奨（Supabase, RDS等）
- Connection poolingが有効であることを確認
- `DATABASE_URL` はpgbouncerを通している場合、`statement_cache_size=0` が必要

### ログレベル

- 本番環境: `LOG_LEVEL=info`（デフォルト）
- デバッグ時: `LOG_LEVEL=debug`

### Discord Bot権限

Botには以下のIntentsが必要です：
- `GUILD_MESSAGES`
- `GUILD_VOICE_STATES`
- `GUILD_MEMBERS`
- `MESSAGE_CONTENT`

Discord Developer Portalで権限を確認してください。
