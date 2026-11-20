# 引き継ぎ: Issue #13 P0-02b 依存更新

- 日付 / 担当: 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `chore/13-update-web-deps`、基点 main 222536d
- 対象要件 / 非対象: D11。見た目・記事・URLの変更、Docker/Go/DBは非対象
- 完了内容: next/eslint-config-next 16.4.6、eslint.config.mjs（flat直import）、`audit fix`（forceなし）、未使用の react-syntax-highlighter と @eslint/eslintrc 削除、postcss-selector-parser override、playwright 1.65.1固定、CIにブラウザー確認追加、capture scriptが失敗で終了コード1
- 結果: npm audit 21→0、lint/tsc/build/test:unit/web:verify/web:e2e 成功（詳細は docs/legacy-baseline/README.md）
- 既知の問題: 基準画像は旧Chromium由来。新Chromiumとはピクセル差が小さく（home最大1/255、ライトボックス最大38/255）、旧ビルドと新ビルドは同一Chromiumで同一。基準は更新していない。L3（ロゴ重なり）は未変更
- 未実施: PRのCI結果（PR作成後に確認）
- DB / API / 切り戻し: なし。revert
- 次: マージ後 P0-03
