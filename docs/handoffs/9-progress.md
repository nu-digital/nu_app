# 引き継ぎ: Issue #9 P0-02 既存公式サイトのapps/web移設

- 日付 / 担当: 2026-11-21 / Claude Code（Sonnet 5.5）
- Issue / PR: #9 / Draft PR（番号はPR本文を正とする）
- ブランチ / 対象SHA: `chore/9-migrate-web`、基点 main `bf327aa`。subtree取り込み元 `nu-digital/nu_digital@e1c031389c6cb621742caf4b270316da93b85168`
- 課金: 追加課金・Fable・API・subagent不使用
- 対象要件 / 非対象: P0-02、D13、D11の移設部分。依存更新(P0-02b)、admin/Go/DB/Compose(P0-03)は非対象
- 完了内容: `git subtree add --prefix=apps/web`（squashなし、tree hash一致）、`apps/web/.github`（Pages workflow）削除、`apps/web/ASSETS.md`、ルート`package.json`（`--prefix`委譲）、`scripts/web-verify.mjs`、`.github/workflows/web.yml`、`scripts/check.mjs`のリンク検査から`apps/web/content/`を除外（公開URLリンクのため）とテスト追加、README・BACKLOG・DECISIONS・legacy-baseline更新
- 実行コマンド・結果: `npm run web:ci`/`web:lint`/`web:typecheck`/`build` 成功、`web:verify` 成功（ルート74・記事67・画像532一致）、`npm run test:unit` 成功（harness 10件、test:editor 1件、content-presentation 2件）、`node scripts/check.mjs` 成功、`npm ls --all` 548件が移設前と一致、lockfile無変更、`legacy-baseline-capture.mjs`で72ルート200・ギャラリー25チェック・横溢れ0・画像17枚が基準とバイト同一
- 未実施: CIでの実行結果（PR作成後）、Playwrightを使うブラウザー確認のCI化（P0-02b）、公開Pagesとの比較、基準画像の人による目視（P0-01から継続）
- **要注意（マージ方式）:** このPRは**mergeコミットでマージ**する。Squash/Rebaseだとupstreamの11コミットが失われD13に反する。リポジトリ設定がSquashのみなら一時的な変更が必要（設定変更は人が判断）
- 既知の問題: 既存サイトの不具合L1〜L6は未変更（next 16.1.1のaudit指摘等はP0-02bで対応）。`npm ci`はauditの警告を出す
- DB / API / 切り戻し: なし。revert（merge commitのrevertは`-m 1`）
- 次に実行する最小の作業: マージ後P0-02b（依存更新）→P0-03
