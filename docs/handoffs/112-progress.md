# 引き継ぎ: Issue #112 P4-01a 本番Compose・Nginx・TLS

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/112-prod-compose-nginx`（base: main 0895c89以降）。VPS未契約でも検証できる範囲を実装。**本番への接続・DNS変更・デプロイは一切していない**。
- 完了: `infrastructure/compose.prod.yaml`（ビルドしない・127.0.0.1のみ・digest固定・メモリ上限・ログ上限）、`.env.production.example`、`infrastructure/nginx/nu_app.conf`、`scripts/nginx-test.sh`（31項目: 使い捨てNginx＋擬似バックエンド）、`cert-check.sh`＋systemd timer＋`scripts/cert-check-test.sh`（9項目）、certbotのdeployフック、`check.mjs`（compose.prod.yamlのビルド禁止・ポート・digest）、OPERATIONSの初回セットアップ手順（手順書のみ）、DECISIONS P4-01-1、CI `ops / nginx`。
- 実測（ローカル）: `scripts/nginx-test.sh` 31/31、`scripts/cert-check-test.sh` 9/9、`docker compose config`（雛形）成功、`check.mjs`の変異（ポート・digest）で失敗を確認。
- 未実施・VPS契約後: 実ドメインでのACME取得、実機のNginx版・挙動、メモリ上限の実測、デプロイ（次のPR）。
