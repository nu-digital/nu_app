# 引き継ぎ: Issue #19 P0-03c ローカルCompose

- 日付 / 担当: 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/19-local-compose`、基点 main fa874c2（main CI: go/harness成功確認済み、web/adminの最新runはPR前のため要追跡）
- 完了内容: infrastructure/compose.yaml、.env.example（架空値）、scripts/compose-smoke.mjs、.github/workflows/compose.yml、docs/LOCAL_DEV.md、README、npm scripts（dev:up/dev:down/dev:reset/compose:smoke）
- 実測: `.env`必須値なしで`compose config`が失敗（拒否）。`npm run compose:smoke` PASS（db/api/web/adminがhealthy、migrate正常終了、bot実行中、公開ポートは127.0.0.1のみ、api healthz/readyz、admin healthz、web全72ルート200・未定義404、DB停止でreadyz 503・healthz 200、非root）。smoke後にnu_app_smokeのコンテナ/volumeは残らない
- 未実施: PR CI結果
- 次: マージ後P0-04（新規clone相当での起動確認、CI必須化の可否、P0受け入れ判定と報告）
