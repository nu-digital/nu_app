# 引き継ぎ: Issue #124 deploy.shの運用設定読み込み・RUNBOOK

- 2026-11-26 / Claude Code（Sonnet 5.5）。ブランチ `fix/124-deploy-env`。
- 検証（実機）: GHCRログイン（root）・環境`production`（必須レビュアーあり）・Secrets 4件・Discord Webhook（接続テスト204）を確認。**Webhookの値は表示していない**。
- 修正: SSH経由のsudoではsystemdの環境ファイルが効かず通知先がdeploy.shに渡らないため、`/etc/nu_app/backup.env`をdeploy.shが読む（テスト36/36）。
- 文書: `docs/RUNBOOK.md`を新設（構成・VPS・DNS・Nginx/TLS・SSH鍵・GitHub/GHCR/Secrets・Discord・デプロイ/切り戻し・バックアップ/復元・更新作業・障害対応・交代時の引き継ぎ・実機検証・初回公開チェックリスト）。OPERATIONSの重複・古い記述（手順書・実機記録）を整理しRUNBOOKへ集約。
- 未実施（要確認）: `DEPLOY_ENABLED=true`、初回デプロイ、バックアップタイマー有効化、初期Owner、取り込み、`NOPASSWD:ALL`解除。
