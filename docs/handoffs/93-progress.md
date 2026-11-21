# 引き継ぎ: Issue #93 P3-04a 既存記事の移行CLI（D14）

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/93-import-legacy`（base: main）。
- 完了: migration 00010（`legacy_authors`・`contents.legacy_author_id`）、`nuctl import-legacy`（`internal/legacyimport`、ゴールデン照合・冪等・取り下げ一覧・dry-run既定）、`internal/cmsrules`（API検査の共通化）、公開APIの`author`解決（許可済みかつ未削除のみ名前、他は「nu_digital」）、`check.mjs`にworkflow禁止を追加。
- 実測: `scripts/test-integration.sh`全パッケージok（api 153s・legacyimport 26s）、`go vet -tags integration ./...`・gofmt・`sqlc diff`・`node scripts/check.mjs`成功、`govulncheck`（v1.10.0）「No vulnerabilities found」。D14の最小テスト5件（未登録+確認済み表示／未確認はnu_digital／紐付けで公開可否不変・同名別ユーザーに誤紐付けしない／撤回・削除後は取得不可・再実行で復活しない／再公開・切り戻しで復活しない）。
- 変異確認: 公開SQLの`publication_state='permitted'`条件を外すと3テストが失敗。`erased_at`条件だけを外してもテストは通る（削除は撤回状態を伴い、DB制約が許可へ戻すことを禁じるため二重の防御。仕様どおり）。
- 未実施: compose smoke（import-legacyは本番へ流していない）、管理画面（a-2）、フック（同意撤回・個人情報削除→投稿者の撤回/匿名化）。
- 次の一手: PR作成→CI→マージ→P3-04a-2（#94: 管理API・確認記録・紐付け・フック・レビュー一覧）→WebPの設計メモ（別PR）。
