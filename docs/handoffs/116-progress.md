# 引き継ぎ: Issue #116 P4 ログ・運用手順の索引・進捗文書の最終更新

- 2026-11-22 / Claude Code（Sonnet 5.5）。ブランチ `feat/116-ops-docs-logs`（base: main add7a93以降）。
- 完了: Nginxのアクセスログ（クエリ文字列・Cookie・Authorizationを残さない。nginx-testに2項目追加、33/33）、OPERATIONSの「管理者向け運用手順（索引）」、README・TASKS・BACKLOGを実PR・CIに合わせて更新（P0〜P3完了、P4は契約前に作れる範囲まで完了）。
- 残り（運用責任者の対応・確認が必要）: VPS契約・DNS・secret・`production`環境の承認者・`DEPLOY_ENABLED`、4GB実測、実ドメインでのTLS取得、初回デプロイ、実環境での復元検証、本番への`import-legacy --apply`、投稿者名の公開許可の確認、Discordの実送信確認、Dependabot #3・#4の判断。
