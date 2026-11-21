# 引き継ぎ: Issue #114 P4-01b デプロイ・切り戻し

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/114-deploy`（base: main da6e8db以降）。**本番への接続・デプロイ・GitHubの設定変更は一切していない**。ワークフローは変数`DEPLOY_ENABLED`が無い限り全ジョブがスキップされる。
- 完了: `infrastructure/deploy/deploy.sh`（40桁SHAのみ・flock・pull→バックアップ→up→ヘルスチェック→失敗時は前版へ・通知にsecretなし）、`.github/workflows/deploy.yml`（verify→build→deploy（environment: production）＋手動rollback）、`scripts/deploy-test.sh`（26項目）とCI `ops / deploy-script`、`check.mjs`（deployワークフローの既定無効・PR不可・承認環境）、OPERATIONSの手順（有効化の手順を含む）、DECISIONS P4-01-2。
- 実測（ローカル）: `scripts/deploy-test.sh` 26/26。deploy.shの変異（バックアップ削除・前版復帰削除）でテストが失敗、`check.mjs`の変異（`if`削除・`pull_request`追加）で失敗、`actionlint`（1.7.7）の全ワークフローが成功。
- 未実施・運用責任者の対応待ち: VPS契約・セットアップ、`production`環境の必須レビュアー設定、secret登録、`DEPLOY_ENABLED`の設定、実際の初回デプロイ（承認が必要）。
