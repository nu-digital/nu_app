# 引き継ぎ: Issue #29 P1-01c 初期OwnerのCLI作成

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/29-bootstrap-owner`、基点 main cb6dffb（P1-01bのPR #28は認可の重要設計の確認待ちのため未マージ。本PRはauthzに依存しない）
- 完了: internal/bootstrap（単回・トランザクション・tokenはSHA-256のみ）、cmd/nuctl（bootstrap-owner）、sqlcクエリ追加、APIイメージに/nuctl、compose smokeにCLIの1回目成功・2回目拒否を追加、docs/LOCAL_DEV・OPERATIONS
- 実測: 実PostgreSQL 18.7で統合テスト成功（作成内容/監査/hash、2回目拒否、owner存在時拒否で非消費、失敗時に非消費、12並行で成功1件・各表1件、CLI終了コード、接続情報を出力しない）。ミューテーション: bootstrap_state確保を無効化→2回目拒否・並行テストが失敗、監査にtokenを入れる→失敗（いずれも復元）
- 本番では実行していない（本番未構築）
- 次: PR CI→マージ。P1-02（招待・ログイン）は認可表(#28)の確認が済んでから着手が望ましい（Subject生成・ミドルウェアが依存）
