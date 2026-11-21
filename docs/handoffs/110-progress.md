# 引き継ぎ: Issue #110 P4-02 バックアップ・復元検証

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/110-backup`（base: main 76eb720以降）。VPS未契約でも作れる範囲を実装。
- 完了: `infrastructure/backup/backup.sh`（DB→メディア・検証・COMPLETE・世代の整理・失敗通知・flock・空き容量確認）、`restore-verify.sh`（隔離コンテナでの復元・スキーマ/行数/ファイル数・`nuctl media-verify`）、systemd service/timer（03:00 JST）、`scripts/backup-restore-test.sh`（18項目）とCI `ops / backup-restore`、OPERATIONSの手順、DECISIONS P4-02-1。
- 実測（ローカル）: `scripts/backup-restore-test.sh` 18/18合格。作る過程で、`set -e`だけではfunction内の失敗でERRトラップが動かない（`set -E`が必要）、postgresイメージは初期化用の一時サーバーのせいで`pg_isready`が早く成功してしまう（ログの「ready」2回を待つ）を直した。
- 未実施・VPS契約後: 本番サイズでの所要時間・容量の実測、実際のDiscord Webhookへの通知、`systemd`への配置と実行、実環境での復元検証。
