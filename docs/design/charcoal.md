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

## 試作の結果（判断点1、#178）

2026-11-28、Windows 11 / Node 22.23.5 / Claude Code。ログイン画面の「ログインID」入力と「ログイン」ボタンだけをCharcoalに置き換えた。パスワード欄（`PasswordField`）は招待・再設定と共用のため現行のまま。

- **導入した依存**（完全固定）: `@charcoal-ui/react` 6.3.0、`@charcoal-ui/theme` 6.3.0、peerの `react-aria` 3.53.1・`react-stately` 3.51.0。Reactは19.2.3の単一版のまま（`npm ls` で確認）。`@charcoal-ui/icons` はreactの依存として入るが、まだ使っていない。
- **構成**: `app/layout.tsx` で `layered.css`（`@layer charcoal`）→ `theme/css/v2/light.css` → 既存の `globals.css` → 団体の上書き `app/charcoal-theme.css` の順に読み、`<html class="ch-token-v2">`。画面は `components/ui/Button`・`components/ui/TextField` だけをimportし、`@charcoal-ui/react` を直接importしない。
- **団体側で上書きしたもの**（`charcoal-theme.css`）: 主操作の色を団体色 `#004aad`（hover `#003580`）へ、タップ寸法を40px→44px（`--tap`）へ、フォーカス表示をポータル共通の二重リング（`--focus`）へ。Charcoal既定の淡い青のリングは白地で見えにくいため。`TextField` はCharcoal既定の `showLabel=false` だとラベルが視覚的に隠れるので、共通部品で常に表示に固定した。
- **上書き時の注意**: 全体の `:focus-visible` が `border-radius: 4px` を付けるため、丸いボタンではフォーカス時の角丸を打ち消している。Charcoalの部品CSSはレイヤー内なので、レイヤーなしの既存CSS（`*`・`p`・`h1` 等）が常に勝つ。今回の範囲では崩れは見られなかった。

| 確認 | 結果 |
|---|---|
| `tsc --noEmit`・`eslint .`・`npm test`（admin） | 終了0。テスト7件成功 |
| `next build` | 終了0。19ルートを事前描画 |
| `/login` が読むJS（事前描画HTMLの参照分） | 580,485B → 588,637B（gzip 178,441B → 181,369B、**+2.9KB**） |
| 全画面共通のCSS | 13,020B → 98,259B（gzip 3,111B → 14,529B、**+11.4KB**）。`layered.css` に全部品のCSSが入っており、部品単位のCSSは公開されていない |
| Edge（アプリ内ブラウザー、`next dev`・APIは停止）で操作 | ラベルと入力が関連付き（textbox「ログインID」）。Tabで ID→パスワード→表示する→ログイン の順。Enterで送信し、通信失敗の通知にフォーカスが移り入力は保持。ボタンの背景 `rgb(0,74,173)`・高さ44px、フォーカス時に二重リング。375px幅で崩れなし |

**分かった課題（採否の判断材料）**

1. 見た目の混在: Charcoalの入力は枠なし・薄い灰色の塗り、既存の入力は2pxの枠。1画面に両方あると不統一に見える。展開するなら `PasswordField` を `TextField` の `suffix` で作り直し、招待・再設定も同時に移す。
2. 入力欄の境界: 灰色の塗りは白いカード上で境界が弱い（既存の枠線 `#c9d0dc` も白との比は約1.5:1で、どちらも非テキストのコントラスト3:1に届かない）。団体テーマで枠線を付けるか、塗りを濃くするかを決める。
3. CSSの増分 +11.4KB（gzip）は内部ポータルとしては許容範囲と考えるが、使う部品が増えなくても全量を読む。
4. ボタンの形はCharcoal標準の丸型のまま。団体の角丸（`--radius`）に寄せるかは見た目の判断として残す。

### 運用責任者の判断（2026-11-28）と反映

- **見た目はCharcoal（pixiv・BOOTH）に寄せる。** 「AIがその場で考えた見た目」にしないため、形・余白・文字の大きさはCharcoalの標準を使い、団体側で変えるのは主操作の色（#004aad）と、アクセシビリティの最低基準（タップ44px・二重のフォーカスリング・入力欄の枠）だけにする。ボタンの丸型もCharcoalのまま（上の課題4）。
- **入力欄は「Charcoalの薄い塗り＋1pxの枠」**（課題1・2）。枠の色は `--color-field-border: #8a8f99`（白との比 3.2:1）。
- **パスワードの表示切り替えは、枠の中の目のアイコン**（Charcoalのアイコン `24/Show`・`24/Hide`）。読み上げ名「パスワードを表示する／隠す」と `aria-pressed` を付ける。`PasswordField` をCharcoalの `TextField` で作り直したため、招待・再設定の画面も同じ見た目になる。

反映した内容:

- 部品へ移していない既存画面も、全体CSSの `.btn`（丸型・14px太字・secondaryは薄い塗り）・`.input`（薄い塗り＋1px枠・角丸4px）・`.toggle` をCharcoalの見た目に合わせた。画面ごとに `components/ui` の部品へ置き換えていく。
- `components/ui/IconButton` を追加。Charcoalの `IconButton` は `<pixiv-icon>` に寸法を付けずに描画するため、要素の登録後に付くstyleがSSRとずれてhydrationの警告が出た。団体の部品はCharcoalの `Icon`（`fixedSize` がSSR時にも寸法を付ける）と同じクラス名で組み、警告が消えることを確認した。
- アイコンは `@charcoal-ui/icons`（直接依存に追加、6.3.0固定）が同梱の `@charcoal-ui/icon-files` から読み込む。外部への通信はない（ソースで確認）。
- E2E（`scripts/admin-ui-e2e.mjs`）の「表示する」ボタンの検査を、目のアイコンが枠内にあること・押すと表示/非表示が切り替わることの検査に置き換えた。

| 確認（反映後） | 結果 |
|---|---|
| `tsc --noEmit`・`eslint .`・`npm test`・`next build`・`node scripts/check.mjs` | すべて終了0（テスト7件） |
| `/login` のJS | gzip 193,150B（導入前から **+14.7KB**。アイコンの登録処理を含む） |
| 全画面共通のCSS | gzip 14,692B（導入前から +11.6KB） |
| Edge（`next dev`・APIは停止） | 目のアイコンが枠内（40×40px）、押すと type=text・「パスワードを隠す」・`aria-pressed=true`。Tab順は ID→パスワード→目のアイコン→ログイン。アイコンにフォーカスすると二重リング。375px幅で横あふれなし。hydrationの警告なし |
| 未実施 | 招待・再設定の画面は有効なトークンが必要なため手元では未確認（CIのcompose・admin UI E2Eで確認）。ダークテーマ |

次は判断点2（ModalとStorybook）と、既存画面の `components/ui` への置き換え。

## ログイン後の画面全体（#180）

2026-11-28。運用責任者の依頼で、ランチャー・物品・記事・メンバー・プロフィール・管理の各画面をCharcoalの見た目にそろえた。機能・URL・権限・API呼び出しは変えていない。

- **やり方**: 画面のJSXはほぼそのままにし、共通CSS（`app/globals.css`）のトークンと部品クラスをCharcoalのパレットに置き換えた。部品（`components/ui`）への移行は画面ごとに後で進める。
- **トークン**: 本文 `#1f1f1f`（neutral-90）、補足 `#5c5c5c`（白との比6.7:1）、面 `#f3f3f3`（background-secondary）、区切り `#e8e8e8`、角丸 8px（カード）・16px（パネル）。成功・失敗の淡い背景はCharcoalのgreen-5・red-5。
- **部品の見た目**: パネル・一覧の行・ランチャーのカードは枠線を無くし、灰色の面と角丸で区切る（pixivの設定画面風）。灰色の面の上の入力・副ボタン・タグは白にして見分ける。物品の一覧は写真の角を丸めた枠なしカード（BOOTH風）。状態のタグはTagItem風の小さい札。通知は左に色の線がある淡い帯。
- **共通ヘッダー**（`components/AppHeader`）: ロゴ・表示名・ログアウトに、アプリの切り替え（物品／記事・作品／メンバー／プロフィール／ユーザー管理）をCharcoalのタブ風に置いた。選択中は `aria-current="page"` と団体色の下線。狭い画面では横にスクロールする。ランチャーではカードが同じ役割なので出さない（E2Eもランチャーのカードのリンク名で操作している）。全員に同じ項目を出し、権限はAPIが強制する（ランチャーのカードと同じ方針）。ユーザー管理だけ独自のヘッダーだったため共通ヘッダーにそろえ、「ランチャーへ」ボタンはロゴのリンクに統合した。
- **ファイル選択**のボタン部分も副ボタンと同じ見た目にした。

| 確認 | 結果 |
|---|---|
| `tsc --noEmit`・`eslint .`・`npm test`（7件）・`next build`・`node scripts/check.mjs`・`git diff --check` | すべて終了0 |
| 全画面共通のCSS | gzip 15,137B（#178の時点から +0.4KB） |
| Edge（`next dev` ＋ 見た目確認用の使い捨てモック。データはすべて架空） | ランチャー・物品一覧/詳細・貸出中の一覧・記事一覧/編集・メンバー・プロフィール・ユーザー管理・通知の設定を表示。375px幅で物品・ユーザー管理に横あふれなし、44px未満のボタン・リンクなし |
| 未実施 | 実APIでの操作（CIのcompose・admin UI E2Eで確認）。使い捨てモックはリポジトリに入れていない（分割案2で契約検査つきのモックを作る）。ダークテーマ |

## 画面のボタン・入力を共通部品へ（#184）

2026-11-28。#181で見た目をCSSでそろえた後、画面のJSXも `components/ui` の部品に置き換えた。今後の実装で人もAIも同じ部品を使えるようにするため。部品の選び方は [DESIGN](../DESIGN.md)「管理ポータルの部品の使い方」。

- **ボタン** 80個（`<button className="btn …">` と `<Link className="btn …">`）を `Button` に。`secondary`→`variant="default"`、`danger`→`variant="danger"`、`inline`→`fullWidth={false}`、リンクは `href`。type の無かったボタンはネイティブの既定どおり `type="submit"` を明示した（動作は変わらない）。編集画面の「編集／プレビュー」タブも `Button` に。旧 `.btn` のCSSは削除した。
- **テキスト入力** 29個を `TextField` に（ラベルが文字だけで、値をそのまま使う単純なもの）。id・ラベルの文言・required・maxLength・autoComplete等はそのまま。数値・日付・ファイル・読み取り専用・検索窓（ラベルが別の場所にあるもの）・セレクト・テキストエリアはネイティブのまま。
- **Charcoalとの差を部品側で吸収**: `TextField` は `required` を `<input>` に付けない（ラベル表示用）ため、共通部品で付け直した。入力の文字を0.875倍に縮める処理（iOSの拡大対策）をやめて16pxで表示し、枠を内側の影にした（どちらも入力の高さが44px未満になるのを防ぐため。E2Eの検査対象）。ボタンは長い日本語で折り返せるようにし（Charcoal既定の `width: min-content` だと1文字ずつ折り返す）、Dangerの赤は白文字とのコントラストが足りないため団体のエラー色 `#b3261e` にした。ボタン形のリンクは全体の `a` の色を受けないようにした。

| 確認 | 結果 |
|---|---|
| `tsc --noEmit`・`eslint .`・`npm test`（14件）・`next build` | すべて終了0 |
| `npm run admin:dev:mock` でブラウザー操作 | owner: ユーザー管理で「Editorにする」→完了の通知。member: 三脚を1個借りる→残り2。プロフィールの全ボタン・入力が44px以上、`required` が付いている。320px幅で横あふれなし |
| 未実施 | 実APIでの全操作（CIのcompose smoke・admin UI E2Eで確認） |
