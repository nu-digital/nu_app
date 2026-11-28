# 管理ポータルへのCharcoal導入案

2026-11-28。運用責任者の希望により**第一採用候補**。正式導入は代表画面を評価してから決める。日本語の資料とクリエイター向けサービスの操作部品を利用でき、毎年入れ替わる担当者が同じ見本を参照できる。最初の対象は管理ポータル。公式サイトには移設時の比較基準と独立したテーマがある。[公式Storybook](https://charcoal-web.pixiv.design/)・[調査した公式ソース](https://github.com/pixiv/charcoal/tree/ea20970e80bc15850fca6ce3b32ef107e8235e39)

## 互換性の実測

WindowsのGit管理外に最小のApp Routerアプリを作成。Node 22.23.5、Next 16.4.6、React / React DOM 19.2.3を指定し、npmでreact・themeの両パッケージを導入した。配布版はともに6.3.0。Charcoal ReactのpeerはReact >=17、react-aria >=3.48.0、react-stately >=3.46.0。今回のnpm解決結果は順に19.2.3、3.53.1、3.51.0で、Reactは単一版だった。

再現条件はルートlayoutで `@charcoal-ui/react/dist/layered.css` と `@charcoal-ui/theme/css/v2/light.css` を読み、htmlに `ch-token-v2` を設定すること。クライアント部品に `Button` をimportしクリック回数を表示した。`next build` は終了0、トップページの事前描画に成功。`next start` のページをEdgeで開き、クリックで「確認0」から「確認1」への更新を確認。computed styleは背景色が `rgba(0, 0, 0, 0.05)`、トークン `--charcoal-color-container-default` が `#fff`。検証はJSXの最小構成で、実ポータルの型検査、Dialogのフォーカス、アイコン描画、ダークテーマ、Storybook統合、3 OSのビルドは次の試作で扱う。

## パッケージの選び方

| パッケージ（すべて @charcoal-ui 配下） | 役割と対象 |
|---|---|
| theme / react | テーマ・CSS変数と、Button・TextField・ModalなどのReact部品。最初の直接依存候補 |
| foundation / utils | 基本の型・値と、混色やCSSのヘルパー。通常の画面はthemeと共通部品から利用する |
| icons / icon-files / icons-cli | SVG表示のCustom Element、配布アイコン本体、アイコン管理用CLI。ポータルの操作記号はReactのIconを共通部品で包む。表示名を日本語で付け、装飾は読み上げ対象外にする。初期描画の寸法と読み込み後の表示を確認する |
| tailwind-config / tailwind-diff | トークンのTailwindプリセットと、設定変更によるクラス差分の検査。既存のTailwind画面への影響調査に使える |
| styled | styled-componentsでCharcoalのスタイルを使う経路。既存のadminは通常のCSSなので初回の直接依存には不要 |
| token-cli / react-sandbox | トークン生成の開発ツールとReact部品の試用環境。アプリに配布する部品はreactから選ぶ |

**Tailwind v4との接続**は段階を分ける。公式Skillではv4対応は進行中で、プリセットはv3形式の設定を `@config` で読み込む方式。色や余白の既定スケールも置換し、例えば `p-4` の寸法が変わる。既存webはTailwind 4.1.4を使用している。ポータルの初回は実測済みのCSS変数方式に絞る。後にutilityが必要になったらv4の `@theme inline` で必要な団体トークンだけを対応付け、生成CSSと実寸で検証する。プリセットとの併用は今回未検証。[公式Tailwind Skill](https://github.com/pixiv/charcoal/blob/ea20970e80bc15850fca6ce3b32ef107e8235e39/skills/setting-up-charcoal-tailwind/SKILL.md)

## 団体テーマと部品の利用ルール案

運用責任者の希望として、ポータルでは用途に合うCharcoal部品・アイコン・トークンを積極的に使う。動きは押下への応答、開閉、状態の変化を伝える場面に絞る。ホームでは写真やカードに短い演出を使ってよい。業務画面では入力や連続操作を待たせず、`prefers-reduced-motion` でも同じ情報と操作を提供する。

- 団体の配色は「主操作」「本文」「背景」「危険」「フォーカス」など用途で管理する。例として、本文面は `--nu-surface: var(--charcoal-color-container-default)`、主操作は既存の団体色 `#004aad` を起点にする。主操作の通常・hover・無効・focusは共通Buttonで定義し、写真上の可読性とコントラストを確認する。上流CSSはパッケージのまま読み込み、団体側の上書きはテーマまたは共通部品に集約する。
- 人もAIも、最初に団体の部品カタログ、次にCharcoal公式Storybookで部品と状態を探す。既存のNoticeやPasswordFieldは読み上げ・入力保持を保ちながら再利用する。物品在庫表など固有のUIはセマンティックなHTMLと基本部品を組み合わせる。新しい外部依存が必要なら、足りない挙動・代替案・保守担当をPRに記す。
- 公式Storybookは上流の仕様見本。団体のStorybookには団体テーマを適用した部品を掲載する。最初はButton、入力エラー、Modalの開閉とフォーカス復帰を登録。長い日本語、キーボード、狭幅、200%ズーム、読込中を確認する。ランチャーの写真・余白と、物品一覧の情報密度は別々に評価する。
- 公式Skillsは7件。普段は `choosing-charcoal-styling`、`choosing-charcoal-tokens`、`setting-up-charcoal-css-variables` を参照する。Tailwindまたはstyledを使う案件では該当するsetup Skill、Figma資料があるときは `prepare-figma-design` と `implement-figma-design` を参照する。採用時は参照コミットを固定し、部品・トークン名を実際の配布版でも確認する。今回Skillsは資料として調査した。[公式Skills一覧](https://github.com/pixiv/charcoal/tree/ea20970e80bc15850fca6ce3b32ef107e8235e39/skills)
- 上流はApache-2.0。再配布するライセンス本文・著作権表示を保持し、該当するNOTICEや改変した配布物には必要な表示を含める。団体ロゴ・写真は団体側で権利を確認した素材を使う。導入時は直接依存とlockfileを固定し、更新PRでCSSトークン・アイコン名、画面比較、キーボード操作を確認する。特にToken v2の不安定APIは共通層に閉じる。[公式ライセンス](https://github.com/pixiv/charcoal/blob/ea20970e80bc15850fca6ce3b32ef107e8235e39/LICENSE)

## 導入の判断点

1. 計画確認後の試作PRで、ログイン画面のボタン1個と入力1個に適用する。既存CSSとの競合、TypeScript、SSRと操作、コントラスト、JS/CSSサイズの増分を現行と比較する。
2. 次のPRでIcon・ModalをStorybookに登録し、別のメンバーが使用例だけで再利用できるか確認する。アイコンの読み込み元、ModalのEsc・フォーカス復帰、入力エラー通知を実ブラウザーで確認する。
3. 合格した部品を物品一覧・ランチャーへ順に展開し、採用版をTOOLCHAINに記録する。問題が出た場合は該当部品のPR単位で戻せる。DB・API変更は不要。
