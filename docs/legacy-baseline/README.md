# 既存サイト比較基準（P0-01 / Issue #2）

移設前の `nu-digital/nu_digital` を**無改変**で再現して採取した基準。移設（P0-02）後の差分はこの基準と比較する。
参照SHA: `e1c031389c6cb621742caf4b270316da93b85168`（2026-11-04、全11コミット）。clone先は無視対象 `.local/legacy-site/`。

## 再現手順

```sh
git clone https://github.com/nu-digital/nu_digital .local/legacy-site
git -C .local/legacy-site checkout e1c031389c6cb621742caf4b270316da93b85168
cd .local/legacy-site && npm ci && npm run lint && npx tsc --noEmit
npm run build && mv out ../out-root                       # ルート配信
NEXT_PUBLIC_BASE_PATH=/nu_digital NEXT_PUBLIC_SITE_URL=https://nu-digital.github.io/nu_digital npm run build && mv out ../out-base  # Pages相当
cd ../.. && node scripts/legacy-inventory.mjs .local/legacy-site .local/out-root > docs/legacy-baseline/inventory.json
PLAYWRIGHT_DIR=<playwrightの在るディレクトリ> node scripts/legacy-baseline-capture.mjs .local/out-root docs/legacy-baseline/screenshots
PLAYWRIGHT_DIR=... node scripts/legacy-baseline-capture.mjs .local/out-base <別の出力先> /nu_digital
```

Playwright（1.56.1、Chromium 141 headless）はリポジトリの依存ではなく、スクラッチ領域へ一時導入した。採用・固定はP0-02以降（D11参照）。

## 環境と注意

- Node v22.23.5 / npm 10.9.9（`.node-version`と同じ）、WSL2 Ubuntu。
- Chromium起動に必要なOSライブラリ（nspr/nss/alsa）が無く、sudo無しで.debを展開して `LD_LIBRARY_PATH` で供給した。日本語フォントも無かったため `fonts-noto-cjk` を展開し `FONTCONFIG_FILE` で指定。サイトCSSは `Inter, "Noto Sans JP", ...` を指定し**Webフォントを配信しない**ので、画面の字形は実行環境のフォント依存。**画像比較は同じフォント環境でのみ有効**。ピクセル自動比較の基準にする場合はフォントを固定すること。
- 撮影は reduced-motion、アニメ/transition無効、ja-JP / Asia/Tokyo、スクロール位置は先頭（viewport分のみ。フルページではない）。ヒーローは1枚目のスライド。
- 画面は撮影者(Claude)が `Read` で目視した（pc-home、sp-blog-gallery-surugadai、pc-gallery-lightbox-open の3枚）。**他15枚は撮影・横溢れ測定のみで目視未了**。人による基準確認が必要（TESTING: 見ていない画像を基準にしない）。

## 結果（実測）

| 項目 | 結果 |
|---|---|
| `npm ci` | 489パッケージ、約2分、終了0。lockfileどおり（next 16.1.1 / react 19.2.3 / eslint 9.25.0 / eslint-config-next 15.3.1 / typescript 5.8.3） |
| `npm run lint`（eslint .） | 終了0、警告/エラー出力なし |
| `npx tsc --noEmit` | 終了0 |
| `npm run build`（ルート / basePath=/nu_digital） | ともに終了0、警告なし、約8秒、静的出力74ルート |
| `npm run test:editor` | 1件成功 |
| `node --test scripts/content-presentation.test.mjs` | 2件成功 |
| 全ルートHTTP（72ルート＋存在しないURL） | 72件すべて200、存在しないURLは404。壊れた画像0、同一オリジン失敗0、consoleエラー0（root/basePathとも） |
| 記事 | 67件（blog45/news1/project21）。frontmatter欠落0、sourceId全件あり・重複0、外部画像参照0、画像欠落0 |
| 画像 | public/images 532ファイル・約126MiB。記事から参照309、未参照223（`inventory.json`にsha256） |
| 横溢れ（PC1440×900 / SP390×844、8ページ×2） | すべて0px |
| ギャラリー（`/blog/surugadai-ourisai-2026/`） | クリック/Enter/Spaceで開く、背景スクロール抑止、閉じるボタンへフォーカス、←→・前後ボタン、先頭から←で末尾へ循環、Esc/閉じる/背景クリックで閉じ、開いた画像へフォーカス復帰、スクロール復帰：25チェックすべて成功（`screenshots/capture-result.json`） |

外部ホスト（YouTube等）への通信は検証環境が許す範囲で成功/失敗を区別せず `externalFailedHosts` に記録。YouTube埋め込みは記事内のURLから自動生成され、HTMLの`iframe/script/video`タグを含む記事は0件（D10の入力）。

## 維持すべき事項（観測）

- URL: `/`, `/blog/`, `/news/`, `/project/`, `/terms/`（末尾スラッシュ）と、記事 `/{blog,news,project}/<slug>/`。`readContentFile` は `^[\w.-]+$` のslugのみ許容。大文字・ドット入り: `/blog/newDocument/`, `/blog/nu.metasou/`, `/blog/vketReal/`。大文字小文字を区別するため正規化しない。
- basePath: `lib/paths.ts` の `withBasePath` がネイティブURL・画像に付与。Pages版は `/nu_digital`。VPS公開時（ドメイン直下）はbasePath無しで同じコードが成立することを確認（rootビルドが全て200）。ドメイン変更とbasePath除去は別々に検証する。
- 部分日付（`date`は文字列のまま保持。表示は `formatArticleDate`、並びは `articleDateOrder` の開始日基準）:
  `2026年8月1〜2日`→2026.08.01–02 / `2026年8月13日〜22日`→2026.08.13–22 / `2026年7月22日〜24日`→2026.07.22–24 / `2025年9月〜11月`→2025.09–11（並びは2025-09-01）/ `2025年12月`→2025.12 / `2025年3月〜 6月`→2025.03–06。DBの`date`型だけに畳むと失われるため、原文と並び替え用日付を分ける必要がある（D12）。
- Blog/Product: `articleCategory` は frontmatter が `Blog`/`Product` ならそれを採用、それ以外は collection=project→Product。本SHAでは**フォルダーとカテゴリの不一致0件**（blog45+news1=Blog 46、project21=Product 21）。news は独立フォルダー・カテゴリBlog。一覧 `getBlogFeed` は3コレクションを統合し日付降順、同日はslug順。
- `sourceId`: `blog/…`, `news/…`, `works/…`（projectは`works/`）。全件重複なし。移行の冪等キー候補。
- ギャラリー: `rehypeArticleGallery` が隣接する画像ブロックをグループ化し、img に `role=button`/`tabIndex=0`/`aria-label="<alt>を拡大"` を付与。`ArticleMedia` は `<dialog>` + showModal。ライトボックス枚数は本文内の全img（ギャラリー外のカバー画像等を含む。18枚のギャラリーで19枚表示）。

## 履歴・ライセンス・取り込み方針

- LICENSE: MIT、`Copyright (c) 2025 yurarara23`（コミット2085fc6で追加）。移設後も `apps/web` に全文と著作権表示を残す。
- 画像・ロゴ・団体素材・フォント・外部由来の記事画像（記事に`元URL`コメントあり、取得日2026-10-16）の**権利・利用許諾はMITの対象とは限らず、本調査では確認できない**。MITがコードにのみ及ぶのか、画像・本文にも及ぶのかは権利者（運用責任者・著者）の確認が必要（D13）。
- 履歴: 11コミット、著者はすべて `yurarara`（2026-10-17〜11-04）。`public/images` が約126MiB（初回コミット39573f7で追加）。履歴を保つ移設は `git subtree add --prefix=apps/web`（または`git filter-repo`でサブディレクトリ化→merge --allow-unrelated-histories）が候補。画像126MiBを履歴ごと取り込むとcloneが大きくなる。AGENTS.mdは「本番アップロード画像をGitに保存しない」のみ禁じており、既存サイトの静的素材は対象外と解釈できるが、確定は運用責任者（D13）。P0-02で選ぶ。
- Pages deploy workflow（`.github/workflows/*.yml`）は移設時にコピーしない。`tools/content-editor`・`scripts/*.py`・`docs/screenshots`・LICENSE/README以外の扱いはP0-02で決める。LocalEditorは本番に含めない。

## 既存不具合・懸念（移設による差分ではない）

| ID | 内容 | 再現・根拠 | 影響・扱い |
|---|---|---|---|
| L1 | `next@16.1.1`（lockfile）に多数のnpm audit指摘。critical 4件（Image Optimization・`use cache`等の認証不要RCE。最新の指摘は`<=16.4.4`が対象）、high多数 | `npm audit`（2026-11-20）：全21件(low2/mod9/high9/crit1。直接依存はnextのみcritical)、`--omit=dev`で7件(mod6/high1)。`next`は `<=16.4.4` 範囲 | 現在は`output:"export"`・`images.unoptimized`の静的配信で、Image Optimization・Server Actions・middleware等のサーバー機能を使わないため直接の悪用経路は限定的と推測（未検証）。P3でVPS動的配信にする前、P0-02の依存固定時に更新方針を決める。ここでは更新せず基準を維持（D11） |
| L2 | `eslint-config-next@15.3.1` が `next@16.1.1` と主版が不一致 | package.json。`npm ls`は不整合を報告せず、eslint 9.25/lintは成功 | lintは通る。ルール差で将来の誤検知/見逃し可能性あり。更新するとlint結果が変わり得るので移設と混ぜず別PRで回帰確認（D11） |
| L3 | ライトボックス表示時、サイトのロゴ（サイドバー）がdialogより前面に残り、枚数表示「2 / 19」と重なる | `pc-gallery-lightbox-open.png` で目視。ロゴ位置と枚数ラベルが重なる | 見た目の軽微な不具合。基準として記録、移設では直さない |
| L4 | `.node-version`/engines無し。`@types/node ^20`はNode 22 CIと不一致 | package.json | 型検査・buildは成功。固定はP0-02 |
| L5 | `npm ci` 時点でnpm警告・audit 21件 | 上記 | L1参照 |
| L6 | 既存CIは `actions/*@v4` のfloating tag | 既存workflow | 新リポジトリへはコピーしない（Dependabot/SHA固定の方針と不一致） |

## 未実施・限界

- 公開サイト（GitHub Pages）との**実画面比較は未実施**。ローカルbuildのみ。
- 画像のピクセル差分比較は未実装（基準はスクリーンショットとsha256）。フォント非固定のため自動差分は未導入。
- 全67記事の目視、他ブラウザー（Firefox/WebKit）、幅320px・200%ズーム、実機タッチは未確認。
- `/_not-found/`・`/404/` は出力に存在するが画面確認なし。
- 外部リンクの生存確認、YouTube埋め込み再生は未実施。
