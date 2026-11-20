# 引き継ぎ: Issue #25 P1-01a 認証schemaとsqlc・統合テスト基盤

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/25-auth-schema`、基点 main 9cb27cc
- 完了: db/migrations/00002_auth_schema.sql（users・password_credentials・sessions・one_time_tokens・user_roles・owner_transfer_requests・profiles・personal_profiles・profile_field_visibility・public_credit_consents・audit_logs・emergency_recovery_events・bootstrap_state）、sqlc（db/sqlc.yaml、scripts/sqlc.sh、生成コードservices/api/internal/store）、統合テスト基盤（internal/testutil/pgtest、scripts/test-integration.sh、`-tags integration`）、制約テスト、CIジョブ（go / sqlc、go / integration）
- 実測: gofmt/vet（integrationタグ含む）成功。`scripts/test-integration.sh`で全テスト成功（実PostgreSQL 18.7）。ミューテーション確認: users_login_id_format制約を外すとTestUsersが失敗することを確認（テストが制約を実際に検証している）。`sqlc diff`クリーン
- 未実施: PR CI結果、新ジョブをmain保護の必須チェックへ追加（マージ後）
- 次: P1-01b 認可表、P1-01c Owner作成CLI
