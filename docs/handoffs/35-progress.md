# 引き継ぎ: Issue #35 P1-02b 認証のサービス層

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/35-auth-services`（base: `docs/33-auth-design`＝PR #34、さらに #32に積み重ね）
- 完了: auth/password（Argon2id・方針・同時実行制限・ダミー検証）、auth/token（New/Hash/Issue/Consume。使用は条件付きUPDATEでアトミック、再発行で旧tokenを失効、衝突はErrConflict）、auth/session（Create/Validate/Revoke/RevokeAll、絶対・アイドル期限、last_seen間引き、ロールは毎回DB、CSRFトークン導出）、auth/ratelimit（(ID,IP)/IP/ID、正規化、件数上限）、sqlcクエリ（db/queries/auth.sql）、bootstrapがtoken.Newを使うよう整理
- 実測: gofmt/vet/`scripts/test-integration.sh`（実PostgreSQL 18.7）全成功。ミューテーション: ConsumeTokenのused_at条件を外す／停止ユーザーを有効扱い／アイドル期限無効化／IDの小文字化をやめる、のいずれもテストが失敗することを確認して復元（※1回、未コミットのファイルに`git checkout`が効かず変異が残ったが、直後に内容を確認して手で復元・sqlc再生成で差分なしを確認）
- 未実施: PR CI結果
- マージ順: #32 → #34 → このPR → P1-02c（HTTP基盤とログイン）
- 次: P1-02c
