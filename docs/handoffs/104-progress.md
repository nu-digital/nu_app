# 引き継ぎ: Issue #104 P3-04b 公式サイトのAPI接続（動的配信）

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/104-web-api-site`（base: main fad2483以降）。
- 完了: `lib/markdown.ts`を公開API（`API_INTERNAL_URL`・no-store・全体5秒・React.cache）に置換、全ページを`force-dynamic`、`error.tsx`・`/healthz/`・制作メンバー節・作品の`noarchive`、`output: "standalone"`、アップロード画像のrewrite、Dockerfile（standalone・非root）・compose（apiに依存・API_INTERNAL_URL）、擬似の公開API（`scripts/mock-public-api.mjs`）、`web:site-check`（web-verify・e2e・指紋の比較・API停止時の5xx/no-store）、web CIの組み替え、`check.mjs`にキャッシュ設定の禁止（※#103のDockerfile検査が空振りしていたため併せて追加）。
- 指紋: 投稿者名を伏せたDOMのハッシュ（`mainDomShaMasked`・`articleDomShaMasked`）と`author`を追加。基準`pages.json`へは**既存の項目を一切変えずに**追加（旧サイトを再ビルドして再取得し、既存項目が全て一致することを確認してから統合）。`known-diffs.json`は意図した差197件（理由つき）。
- 実測（ローカル。ChromiumはapT配布の.debを展開して`LD_LIBRARY_PATH`で動かした＝sudo不要）: `web:site-check`成功（web-verify ルート72・記事67・画像532／e2e 72ルート・ギャラリー25／指紋 74ページ・18変種／API停止時 5xx・no-store）、`web:lint`・`web:typecheck`・`test:unit`成功。
- 判断: 末尾スラッシュ。`skipTrailingSlashRedirect`はLinkのhrefからスラッシュを落とすため不採用。ドットを含むslugは308（Nextの仕様）で受け入れ、`known-diffs.json`に記録し、リンクの比較は正規化。
- 教訓: パッチ用スクリプトで`replace`が空振りしても気づけなかった（#103のcheck.mjs）。以後は`assert`を入れる。
- 未実施: CI結果、compose smokeでの実API接続の比較（P3-04c）、Dockerイメージ実機起動。
