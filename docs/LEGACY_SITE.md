# 既存サイト調査（2026-11-20）

ソース: [nu-digital/nu_digital](https://github.com/nu-digital/nu_digital)。参照HEAD: `e1c031389c6cb621742caf4b270316da93b85168`。
READMEとPages workflowに記載された公開先: [nu-digital.github.io/nu_digital](https://nu-digital.github.io/nu_digital/)。2026-11-20のP0-01でローカルbuild・lint・全ルート・ギャラリー操作を実測した（結果は[legacy-baseline](legacy-baseline/README.md)）。公開サイトとの実画面比較、全記事の目視は未了。

## 観測した構成

- Next.js / React / TypeScript / Tailwind CSS。package.jsonはNext `^16.1.1`、React `^19.2.3`、eslint-config-next `15.3.1`。lockfileがある。これは観測値であり、この組合せの互換性を保証しない。
- `output: "export"`、`trailingSlash: true`、`images.unoptimized: true`。
- 既存Pages workflowはNode 22、`NEXT_PUBLIC_BASE_PATH=/nu_digital`、`NEXT_PUBLIC_SITE_URL=https://nu-digital.github.io/nu_digital` でbuild。
- `app/`のページ: `/`, `/blog/`, `/news/`, `/project/`, `/terms/` とblog/news/projectそれぞれのslug詳細。
- Markdownはblog 45件、news 1件、project 21件、計67件。大文字やドットを含むslugがあるので勝手に小文字化・正規化しない。
- 団体カラーは `#004aad`。写真ギャラリーは `ArticleMedia.tsx`、日付・カテゴリ・画像のグループ化は `lib/content-presentation.mjs`。
- LocalEditorの `test:editor` はあるが、UI全体のE2Eや独立typecheck scriptはpackage.jsonにない。既存CIはlintとbuildを行う。
- 既存サイトの上流リポジトリ（`nu-digital/nu_digital`）の `docs/screenshots/website.png` は参考画像（**このリポジトリには含まれない**）。撮影条件不明のためそのまま自動比較の基準にはしない。

## P0の移設で守ること

1. upstreamの参照SHA・履歴取り込み方法・ライセンスを記録。移設前にlockfileのまま再現し、互換性修正と見た目変更を混ぜない。取り込みは`git subtree add --prefix=apps/web`（squashしない、D13）。履歴は11コミット・pack 126.19MiB・最大blob約4.05MiBで、GitHubのファイル制限に抵触せず書き換え不要（2026-11-21実測）。素材全体にMITを新たに付与する意味ではなく、既存の著作権表示・出典・第三者素材の条件を保持する。依存更新は移設の直後の独立PR（D11）。
2. 全ルートと記事slug、本文・画像数/ハッシュ、frontmatterを記録。公開先のbasePath除去とドメイン変更を分けて検証する。
3. Nextバージョンとeslint-config-next等の整合性を確認し、修正が必要なら理由と回帰テストを添える。
4. 原文の期間・月のみの日付、Blog/Productと保存フォルダーの違い、sourceIdを保持する。
5. ギャラリーはクリック/Enter/Spaceで開く、左右移動、Esc/閉じる、開いた画像へフォーカス復帰、背景スクロール抑止を確認。
6. Pagesへのdeploy workflowは新repoへ有効なままコピーしない。LocalEditorを本番に含めない。
7. VPSでの動的配信とCMS接続はP3。P0で認証やCMSを先回りしない。

参照cloneはローカルの無視対象 `.local/legacy-site/` にあり、nu_appへコードを移設したものではない。Claudeは自分の環境で元リポジトリと参照SHAを取得する。

## P0-01の実測基準（2026-11-20）

[docs/legacy-baseline/README.md](legacy-baseline/README.md)に再現手順、lint/tsc/build結果、67記事・画像の`inventory.json`、PC/スマホ基準画像、ギャラリー検証、既存不具合（L1〜L6）、履歴・ライセンス方針を記録した。
