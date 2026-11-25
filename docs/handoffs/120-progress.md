# 引き継ぎ: P4 実機（さくらのVPS v5）セットアップと検証

- 2026-11-26 / Claude Code（Sonnet 5.5）。ブランチ `feat/120-rehearsal-knobs`。
- 実機: SSH鍵認証（ホスト鍵は指紋照合）、パスワード認証・root直ログイン無効（別セッションで確認後）、ufw・fail2ban・自動更新、Docker 29.10.1、deployユーザーとforced command、deploy key（読み取り専用）でリポジトリ取得、`.env`（ランダムなDBパスワードをサーバー上で生成。表示していない）・`backup.env`の雛形。
- 試験環境での検証（結果は OPERATIONS「実機での検証の記録」）: 起動・メモリ・負荷・画像アップロード・取り込み・永続化・バックアップ/復元検証・デプロイ予行演習（壊れた版→自動で前版へ）。試験環境は削除済み。
- コード: `deploy.sh`に`COMPOSE_ARGS`・`SKIP_PULL`（予行演習用）を追加。deploy-test 34/34。
- 残り（要確認・要操作）: DNS（Cloudflare）、さくらのパケットフィルターで80/443を開放、Nginx設定・TLS取得、GHCRの読み取りトークン、GitHubの環境`production`・secret・`DEPLOY_ENABLED`、初回デプロイ、`NOPASSWD:ALL`の解除（`/etc/sudoers.d/90-nu-setup`。完了後）。
