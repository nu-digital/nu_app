# 引き継ぎ: Issue #91 P3-04-0 移設前の基準の固定

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/91-legacy-fingerprint`（base: `docs/89-p3-web-design`＝PR #90。設計メモ）。サイトのコードは変えていない。
- 完了: `scripts/page-fingerprint.mjs`（抽出・比較・URL変種）、`scripts/legacy-fingerprint-capture.mjs`（取得・比較のCLI。`--twice`で安定性）、`docs/legacy-baseline/pages.json`（74URL・変種18。**現行の静的イメージ（nginx）から取得し、2回で一致**）、`scripts/legacy-goldens.mjs`（`frontmatter.json`・`dates.json`のゴールデン。`--check`をweb CIに追加）、`takedown.json`（空の枠）と`web-verify`の取り下げ対応、`scripts/page-fingerprint.test.mjs`（比較ロジックのテスト）。
- 実測: `npm run test:unit`成功（指紋の比較・メタ抽出のテスト4件を含む）、`npm run web:goldens`成功、`node scripts/web-verify.mjs`成功（ルート74・記事67・画像532）。取り下げの挙動は、一時的に一覧へ画像と記事を入れて確認（画像が残っていれば失敗・一覧を戻すと通る）。比較モードは、同じサイトで`PASS`・基準を1か所書き換えると差を検出して`FAIL`。
- 基準から分かった事実: 全67記事のfrontmatterの値はすべて文字列（日時型なし）、抜粋が空の記事は11本、部分日付は6種（`2025年3月〜 6月`は空白あり）。`/blog/`・`/news/`の一覧は区分を問わず**全67件**を並べる（`getBlogFeed`）、`/project/`は21件。変種: スラッシュなしは301、`/x/index.html`は200、`/404/`・`/_not-found/`は200（API接続後に変わる差は比較時に`--known`で明示）。
- 作成中の誤り: 画像の読み込み完了を待つ処理が、遅延読み込み（`loading="lazy"`）の画像で永久に待った→待たない、`<main>`の無いページ（404）で待ちがタイムアウト→bodyを対象に、変種のURLが区分と合っていなかった→区分ごとに、**`pkill -f`が自分のシェルコマンドも止めた**（コマンド文字列に同じ名前を含むため）。
- 未実施: PR CI結果。移行CLI（P3-04a）、サイト接続（P3-04b）、比較の常時実行（P3-04c）。
- 次の一手: P3-04a（`nuctl import-legacy`。`yaml.v3`・`internal/cmsrules`の切り出し・ゴールデンとの照合・冪等・取り下げ一覧）。
