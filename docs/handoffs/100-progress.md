# 引き継ぎ: Issue #100 P3-04b-1 共有する描画時サニタイズ

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/100-shared-sanitize`（base: main）。
- 完了: `markdown-sanitize.mjs`をweb・adminで同一にし（`check.mjs`がバイト同一を検査）、adminのプレビューはそれを利用。公開サイトの記事描画に、rehype-rawの後・ギャラリー化の前でサニタイズを適用。webにテスト（既存67記事のHTMLが旧パイプラインと完全一致・共有事例集と攻撃入力の安全性・ギャラリー属性が残る・YouTube目印）を追加し`test:unit`に組み込み。
- 実測: admin `npm test` 7件成功、web新テスト4件成功、`web:lint`・`web:typecheck`・`build`・`web:verify`（ルート74・記事67・画像532一致）・`test:unit`成功。HTMLコメント（記事先頭の「元URL」）はReactの描画で出力されないため比較では除外。
- 追加依存（web）: rehype-sanitize 6.0.0（admin同一）、devDependencies: rehype-stringify・remark-parse・remark-rehype・unified（adminと同一版）。
- 未実施: CI結果。次: P3-04b-2（データ層のAPI化・擬似API・動的配信・コンテナ）。WebPはOpusレビュー受領済み（docs/100-webp-design。反映は別PR）。
