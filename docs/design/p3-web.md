# P3-04 設計メモ: 既存記事・画像の移行と公開サイトのAPI接続

- 2026-11-21 / Claude Code（Sonnet 5.5）。状態: **Opusレビュー（2026-11-21、指摘15件・高5）を反映済みの設計**。実装はこの版に従う。
- 正本: REQUIREMENTS（Phase 3）、DECISIONS D06・D08・D10・D12・D13、[p3-cms.md](p3-cms.md)（公開API・匿名化・日付）、[p3-media.md](p3-media.md)（画像の公開境界・キャッシュ要件）、`docs/legacy-baseline/`（移設前の比較基準）。確定事項は変えない。

## 0. 現状（実測）

- 公開サイト（`apps/web`）はNext.js 16の**静的export**（`output: "export"`・`trailingSlash: true`）。`lib/markdown.ts`がファイルシステム（`content/{blog,news,project}/*.md`）を同期で読み、`generateStaticParams`で全ページを事前生成。配信はnginx（静的）。
- 記事67本。frontmatter: `title`・`image`（すべて`/images/...`の静的画像）・`author`（`yurarara`等のハンドル名。公開サイトに既に出ている）・`date`（文字列。部分日付6種）・`category`（Blog/Product）・`excerpt`・`sourceId`。**実データの落とし穴**: `excerpt: >-`（折りたたみブロック）2本、二重引用符の`\n`エスケープ、単一引用符、**空の`excerpt: ''`/`""`が約10本**、引用符なしで`！`や`〜`を含む値、**本文中に`---`行が3本**（水平線・見出しの下線）。本文の生HTMLは`details`/`summary`/`div`のdata属性だけ、23本に`<!-- 元URL: … -->`。
- 並び順: `getAllContent`（`/project/`・トップのカルーセル）はファイル名順に読んだうえで日付だけで**安定ソート**、`getBlogFeed`（`/blog/`・トップ・記事横の新着）は同日を`localeCompare`（slug）で並べる。**同じ日付の組が7組**ある（例: 2026-07-20に3本）。
- 画像532ファイル（約126MiB）は`apps/web/public/images/`の静的ファイル。D06: **アップロード経路を通さず、`docs/legacy-baseline/inventory.json`のsha256で同一性を保って移設**する（変換しない）。`inventory.json`は記事ごとに`fileSha256`・`bodySha256`を持つ。

## 0.1 Opusレビューの反映一覧

| # | 重大度 | 指摘 | 反映 |
|---|---|---|---|
| 1 | 高 | 並び順: APIは同日の中をUUID順にするため「同日はslug昇順」が成り立たない。既存は比較規則が2種類 | サイトが全件取得後、**呼び出し元ごとに既存と同じ比較関数で並べ直す**。slug列をゴールデンで固定（§3） |
| 2 | 高 | 抜粋が空の約10本が、本文の断片に変わる | `summary`に原文の抜粋（空も）を入れ、サイトは`metadata.excerpt ?? summary`だけを使う（本文へのフォールバックを**やめる**）（§2・§3） |
| 3 | 高 | frontmatterの手書きパースは壊れる | **`yaml.v3`（`yaml.Node`で値をそのまま文字列に）**を使い、gray-matter（今のサイトのパーサー）の出力をゴールデンにして7項目×67本の一致を検査。入力の`.md`のsha256を`inventory.json`と照合（§2） |
| 4 | 高 | 静的イメージへの切り戻しで、CMSで非公開にした内容が再公開される | 切り戻し先は「API接続版の前のイメージ」に限る。静的イメージはCMSで最初に変更した時点で使えない（OPERATIONSに明記）（§1） |
| 5 | 高 | 段階の順序のせいで比較の基準とCIの検証を失う | **基準の取得を最初の別PR（P3-04-0）**に。web CIは擬似の公開API（`content/*.md`から）で動かす。smokeは「bootstrap→招待受諾→移行→全ルート200→比較」（§1・§3・§4） |
| 6 | 中 | 静的画像を取り下げる手段が無く、web-verifyが失敗する（D08） | 取り下げ一覧（`takedown.json`）を検証の例外にする。移行CLIも一覧の記事を移行しない（§2・§3） |
| 7 | 中 | `metadata.author`がD08と矛盾し得る | **ユーザーが確定（D14, 2026-11-21）**。移行元の投稿者名を`legacy_authors`で管理し、公開可否は紐付けと別（既定は`nu_digital`）。名前は版・JSONに保存せず、公開APIが配信ごとに解決する（§2.1・§5） |
| 8 | 中 | 動的・no-storeの範囲と再発防止 | P4-01の要件（HTML・公開JSONをプロキシ/CDNでキャッシュしない）、`noarchive`、削除依頼の手順、**CIで禁止設定をgrep**、smokeでRSC応答も確認（§3・§4） |
| 9 | 中 | サニタイズのコピーは事例集だけではドリフトを検出しきれない | スキーマ・プラグインを独立ファイルにし、web/adminで**バイト単位で同一**をCIで検査。67本で「旧パイプライン」と「新パイプライン」のHTMLが完全一致（§3） |
| 10 | 中 | 比較の指紋に抜けがある | DOMの正規化ハッシュ・メタ・カード・URL変種のステータス等を追加。ボットのUAでも取る（§4） |
| 11 | 中 | 障害時の挙動・入力の扱い | `app/error.tsx`・5xxは`no-store`をテスト・ヘルスチェックはAPI非依存・取得全体に1つの期限・slugを検査してから`encodeURIComponent`（§3） |
| 12 | 中 | 移行CLIの細部 | 再実行は公開ポインタに触れない・作業版と公開版の両方を比較・`--as`をTx内でロックして検証・dry-runは実行してROLLBACK・advisory lock・検査関数の共通化・コメント除去の厳密化・取り下げ一覧（§2） |
| 13 | 低 | 日付の分類（`2025年3月〜 6月`の空白） | 空白を許す。ゴールデンにkind・estimatedも固定。`<time dateTime>`は今と同じくlabelがYYYY-MM-DDのときだけ（§2・§3） |
| 14 | 低 | 公開APIの中継の範囲 | 完全一致の`/api/v1/public/media/<22文字>`だけ・GET/HEAD・Cookie/Authorizationを外す・キャッシュしない（§3） |
| 15 | 低 | standaloneのコンテナ | `HOSTNAME=0.0.0.0`・`public/`と`.next/static`・`poweredByHeader: false`・`.next/cache`をtmpfs・トップも`force-dynamic`（§3） |

## 1. 段階と方針

| 段階 | 内容 |
|---|---|
| **P3-04-0** | **移設前の基準の固定**（現行の静的exportから`docs/legacy-baseline/pages.json`と並びのゴールデン、frontmatterのゴールデン、取り下げ一覧の枠）。サイトのコードは変えない |
| P3-04a | 再実行可能な移行CLI（`nuctl import-legacy`）。67記事をDBへ。画像は移さない。**投稿者は`legacy_authors`へ（名前は版に入れない）。公開APIは配信時に投稿者を解決** |
| P3-04a-2 | 投稿者の管理（D14）: 管理者API（一覧・公開許可の確認と記録・撤回・ユーザーへの紐付け/解除）、同意撤回・個人情報削除のフック、確認一覧の画面 |
| （別PR） | 画像のWebP統一（D06追記）。エンコーダーの選定は別の設計メモ＋Opusレビュー。**既存画像の一括変換は含めない** |
| P3-04b | 公開サイトのAPI接続: データ層をAPIに置き換え、描画時のサニタイズ、動的配信（Next standalone）、擬似の公開API（CI用）、画像の配信経路とキャッシュ方針 |
| P3-04c | 全URL・表示の比較をsmokeで常時実行、撤回の反映の確認 |

**切り替えは二重化しない**: `output: export`と動的配信は両立しない（route segment configは静的な値でなければならない）ため、ファイル版とAPI版を環境変数で併存させない。P3-04bで`apps/web`をAPI接続版に**置き換える**。`content/*.md`はリポジトリに残し（D13）、**移行の入力・ゴールデンの生成・擬似API**にだけ使う。**切り戻し**は「API接続版の前のイメージ」へ戻すことに限る。**静的イメージ（`content/*.md`の原文をビルドしたもの）へは戻さない**: CMSで公開停止・本文の修正（D08・個人情報の対応）をした後に静的版へ戻すと、消した内容が再び公開されるため。静的版は、CMSで最初に変更した時点で使えない（OPERATIONSに明記）。本番へのデプロイはmainマージでは走らない（deployワークフローは無い）。

## 2. P3-04a: 移行CLI

`nuctl import-legacy --content-dir <dir> --inventory <inventory.json> --as <ownerのログインID> [--apply]`。運用者がサーバー上で手動で実行する（workflowから呼べない。`check.mjs`の禁止に追加）。`api`イメージ（distroless）は`content/`を含まないため、運用手順に**読み取り専用のマウント**を書く: `docker compose run --rm --no-deps -v "$PWD/apps/web/content:/content:ro" -v "$PWD/docs/legacy-baseline/inventory.json:/inventory.json:ro" --entrypoint /nuctl api import-legacy --content-dir /content --inventory /inventory.json --as <オーナー> [--apply]`。

- **入力の検査（全件・適用前）**: (1) 各`.md`のsha256を`inventory.json`の`fileSha256`と照合（一致しなければ中止。意図して変えた場合だけ`--allow-source-changes`）、(2) **frontmatterを`yaml.v3`で読む**（`yaml.Node`を辿り、スカラーを**タグを解釈せずそのまま文字列**で取る＝`2026-10-20`が日時型にならない）。frontmatterの範囲は**先頭の`---`と、次の単独の`---`行**まで（本文中の`---`は触れない）、(3) 67本すべてを`mdcheck`に通す（元URLコメントを外した後）、(4) 項目の検査は**APIと共通の関数**（`checkFields`・`validMetadataValue`・slug・日付の整合を`internal/cmsrules`へ切り出して両方が使う）、(5) `title`・`date`必須、`image`が`/images/`始まり、`category`はBlog/Product。**1本でも違反なら何も書かずに一覧を出して終了**（本文を勝手に書き換えない）。
- **元URLコメントの除去**: 「本文の**最初の空でない1行**が`<!-- 元URL: https://… | 取得日: YYYY-MM-DD -->`に完全一致」する場合だけ外し、URLを`metadata.sourceUrl`へ（それ以外のコメントは`mdcheck`が違反にする）。
- **対応づけ**: `content/blog`→article/blog、`news`→article/news、`project`→project/project。slug＝ファイル名（大文字・ドットを保つ）。`metadata`: `category`・`image`・`excerpt`（**空でないときだけ**）・`sourceId`・`sourceUrl`。**`author`は`metadata`に入れない**（§2.1）。**`summary`＝原文の`excerpt`（空なら空）**。カバー（`cover_media_id`）は使わない。制作メンバーは登録しない。
- **日付（D12）**: 原文を`date_label`に保ち、`date_kind`・`sort_date`・`estimated`を決める。`sort_date`は**`articleDateOrder`と同じ規則**（先頭の「年・月・（あれば日）」を読み、日が無ければ1日とする開始日。区切りは「年月日」「.」「/」「-」）。kind: 1日（`YYYY-MM-DD`・`YYYY年M月D日`）→day、日・月の範囲（`2026年8月1〜2日`・`2025年9月〜11月`・**`2025年3月〜 6月`（空白あり）**等）→range（estimated）、年月のみ→month（estimated）、それ以外→unknown。**検証**: 67本の`sort_date`・`kind`・`estimated`が、既存`articleDateOrder`/`formatArticleDate`（JS）の出力を固めた**ゴールデンファイル**と一致（Goのテスト）。
- **書き込み**: 1トランザクション。**トランザクション内で`--as`が有効なOwnerであることをロックして読み直す**。`pg_advisory_xact_lock`で同時実行を防ぐ。版1を作り、作業版＝公開版にし、`content_publication_events`に`publish`、`audit_logs`に`content.imported`（sourceIdと`executedByOsUser`。本文は入れない）。**公開の通知は積まない**。**dry-run（既定）は、同じトランザクションを最後まで実行してから`ROLLBACK`**する（DBの制約も検査できる）。
- **再実行（冪等）**: `(section, slug)`が既にあり`metadata.sourceId`が一致するもの: 作業版と公開版の**両方**を移行元と比べ、同じならスキップ、違えば「CMSで編集済み／差分あり」として**上書きせず報告**。**既存の行の公開ポインタ・公開状態には一切触れない**（公開停止中のものを再公開しない）。`sourceId`が違う同slugは衝突として全体を中止。
- **取り下げ一覧**: `docs/legacy-baseline/takedown.json`（記事: section/slug・理由・日付。画像: パス・理由・日付）。**一覧の記事は移行しない**（災害復旧後の再実行で取り下げた記事が再公開されない）。一覧の画像は`web-verify`の例外（§4）。
- **画像**: DBに移さない。`/images/...`は`apps/web/public`の静的ファイルのまま。dry-runで、作業ツリーの`public/images`に画像が実在するかを確認する（ただし**デプロイされたイメージの中身の保証にはならない**。本番の確認はsmokeとイメージ内の`web-verify`）。
- テスト: 実PostgreSQLで、実データ67本を移行→区分別の件数・全て公開済み・`sourceId`の一意・日付のkind/sort/estimated（6件の部分日付と空白あり）・frontmatterがゴールデン（gray-matter）と一致（折りたたみ・エスケープ・空の抜粋・本文中の`---`）・本文がコメントを除いて一致・`summary`に空の抜粋が保たれる・通知が積まれない・監査ログ・公開APIで読める。再実行（スキップ）・差分の検出・**公開停止中の記事を再実行しても公開されない**・衝突・入力の違反で何も書かれない・dry-runは何も書かない・Ownerでない`--as`の拒否・ソースのsha256不一致で中止・取り下げ一覧の記事は入らない。

### 2.1 投稿者（author）の扱い（D14。ユーザー確定）

**要点**: 既存authorは「移行元の投稿者情報」であり、アプリのユーザーとは別。公開できるのは、管理者が本人の公開許可を確認・記録した投稿者名だけ。それ以外は`nu_digital`。名前は版にもJSONにも保存せず、**公開APIが配信のたびに解決**する。

- **データ**（migration）:
  - `legacy_authors`: `id`・`source_name`（移行元の名前。UNIQUE。個人情報削除で匿名の記号へ置換・`erased_at`）・`publication_state`（`unconfirmed`＝既定／`permitted`／`withdrawn`）・`confirmed_by`・`confirmed_at`（公開許可を確認・記録した管理者と日時）・`withdrawn_at`・`linked_user_id`（NULL可。FK→users。**紐付けは公開可否と独立**）・`linked_by`・`linked_at`・`erased_at`・`created_at`。
  - `contents.legacy_author_id`（NULL可。FK）。移行で、frontmatterの`author`が`nu_digital`（団体名）なら**NULL**（今も既定の`nu_digital`を表示。同じ出力）、それ以外は`legacy_authors`をsource_nameで取得/作成して参照する。**同じ投稿者の複数記事は同じ行を共有**する（公開可否・紐付けを一貫して管理できる）。
  - **名前は`content_revisions.metadata_json`に入れない**（版は不変で消せず、D08の個人情報削除と両立しないため）。`author`は`metadata`の許可キーから**外す**（Memberが他人の名前を載せる経路も無くなる。`sourceId`等はEditor以上/移行ツールのみのまま）。CMSで新規に作った記事は`legacy_author_id`がNULL＝`nu_digital`。
- **公開APIの解決**: `author` = `legacy_author_id`があり、その`publication_state='permitted'`かつ`erased_at IS NULL`なら`source_name`、それ以外は`"nu_digital"`。**公開版の内容（版・公開ポインタ）には依存しない**ので、過去の公開版・再公開・切り戻し・公開停止からの復活で、非公開の名前は復活しない。`metadata.author`は公開APIの出力から削除（出力に`author`キーは常に1つ・解決済みの値）。**HTML・メタ情報**（記事ページの「投稿者」・OGP・`description`）も、サイトはAPIの`author`だけを使う。キャッシュしない（P3-04b）ので、撤回は次のリクエストから反映。
- **遷移（管理者API。P3-04a-2）**: `unconfirmed→permitted`（管理者が本人の公開許可を確認し記録。確認者・日時・メモ。**Adminであること・過去に公開されていた事実・プロフィールのメンバー内公開設定・Discordの一覧は根拠にしない**ことを画面に明記）、`permitted→withdrawn`、`withdrawn→permitted`（本人が改めて許可した場合。再確認として記録）。紐付け: **管理者が本人を確認して明示的に**`linked_user_id`を設定/解除する。**表示名の一致による自動紐付け・固有名のコードへの埋め込みはしない**（同じ表示名の別ユーザーへ誤って紐付かないよう、紐付けはユーザーIDで行い、候補の検索は表示名だが決定は管理者が選ぶIDで行う）。**紐付け・解除・ユーザーの停止・退会では`publication_state`は変わらない**（継続している公開同意を自動で取り消さない）。
- **撤回・削除の連動**: 紐付けたユーザーが**公開クレジットの同意を撤回**したとき（本人・管理者による個人情報削除での撤回を含む）、そのユーザーに紐付く`legacy_authors`の`publication_state`を`withdrawn`にする（同じトランザクション。公開表示は即座に`nu_digital`）。**個人情報の削除**（管理者）では、さらに`source_name`を匿名の記号へ置換し`erased_at`を立てる（移行元情報の保持は無期限保持を意味しない。D08と整合）。未紐付けの投稿者の撤回・削除は、管理者が`legacy_authors`の操作で行う（同じ効果）。監査ログに記録する（名前は入れない）。
- **管理者確認一覧**: 投稿者ごとに「状態（未確認/許可/撤回）・紐付け先・記事数・最後の確認者」を一覧し、未確認を順に処理できる補助。**漏えい防止の仕組みは「既定がnu_digital」であり、手作業のauthor削除ではない**。
- **テスト**（D14の最低限＋α）: (1)未登録・許可確認済みの名前を公開APIが返す (2)未登録・未確認は`nu_digital` (3)紐付けで公開可否が変わらない（未確認のまま紐付けても`nu_digital`、許可済みの紐付けでも`permitted`のまま） (4)同意撤回・個人情報削除の後、公開API・版・一覧・移行の再実行・DBの文字列検索のいずれからも名前が出ない (5)表示名が同じ別ユーザーへ誤って紐付かない（紐付けはID指定のみ。同名ユーザーが複数でも指定したIDにだけ紐付く）、(6)停止・退会で公開可否が変わらない、(7)過去の公開版・再公開・公開停止→再公開で、非公開の名前が出ない、(8)`metadata.author`がMemberの入力で受け付けられない、(9)公開APIの出力に`author`のキーが1つで、`metadata`に名前が出ない。

## 3. P3-04b: 公開サイトのAPI接続

- **データ層**: `lib/markdown.ts`の関数を、**API（`API_INTERNAL_URL`。サーバー側のみ）から読む非同期の関数**に置き換える。`GET /public/contents`（`limit=100`でカーソルを辿る。**取得全体に1つの期限**5秒。ページ数ぶん延びない）と`GET /public/contents/{section}/{slug}`。**Cookie等の認証情報は一切送らない**。slugは`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`で検査し、通らなければ`notFound()`、通ってから`encodeURIComponent`でAPIへ。404→`notFound()`、5xx/通信失敗→例外。同じリクエスト内の重複取得は`React.cache`で1回。
- **並び（既存と同一）**: APIの順序（同日はUUID順）は**使わない**。全件取得後、呼び出し元ごとに**既存と同じ比較関数**で並べ直す（`getBlogFeed`系＝`articleDateOrder`相当の開始日の降順→slugの`localeCompare`、`getAllContent`系＝取得順（slug昇順で取得してから）日付だけの安定ソート）。開始日は`sortDate`（API応答。移行で`articleDateOrder`と同じ規則で作ってある）。**各一覧のslug列をゴールデンで固定**（一覧・トップのフィード・記事横の新着・カルーセル）。
- **対応づけ**: `image`＝`metadata.image`→`coverUrl`→既存の既定画像、`author`＝APIの`author`（解決済み。公開許可のある投稿者名、それ以外は`nu_digital`。サイトは名前をここ以外から作らない）、`category`＝`articleCategory`、表示日付＝`date.label`（`formatArticleDate`）、`<time dateTime>`は**今と同じく`date.label`がYYYY-MM-DDのときだけ**（`sortDate`を使わない＝補った日を開催日として出さない。D12）、**`excerpt`＝`metadata.excerpt ?? summary`（本文へのフォールバックはしない。既存の空の抜粋の約10本は空のまま）**。
- **配信モデル**: **動的レンダリング**（Next standalone。`output: "export"`をやめ、`generateStaticParams`を削除、**トップを含むコンテンツを使う全ページを`force-dynamic`**＝ビルド時にAPIへ取りに行かず空の一覧を焼き込まない）。理由: D08（撤回・個人情報削除を**既存の公開版・キャッシュにも反映**）を、キャッシュ無効化の仕組みなしで満たす。Next 16.4.6の既定（動的ページの`no-store`系ヘッダー・`staleTimes.dynamic=0`・`cacheComponents=false`）の下で、`force-dynamic`＋`fetch(..., {cache: "no-store"})`でNext内部のキャッシュを使わない。**将来の再発防止**: `apps/web`で`revalidate`・`"use cache"`・`unstable_cache`・`cacheComponents`・`staleTimes`・`force-static`・`images.unoptimized`の削除をCIでgrep検査して禁止する。**P4-01（Nginx・CDN）の要件**: HTMLと公開JSON（`/api/v1/public/contents*`）はプロキシ・CDNでキャッシュしない（秒単位のmicrocacheも不可。D08の「撤回時に明示的に無効化」と合わない）。**検索エンジン・アーカイブ**: 作品ページに`robots: noarchive`。撤回・削除依頼で検索結果に残る場合の削除依頼の手順をOPERATIONSに書く。代替案（却下）: 静的export＋再ビルド（撤回の反映に再ビルド・基盤未整備）、ISR（再検証までの間、撤回した名前が残る）。
- **障害時**: `app/error.tsx`を置く。エラー時は**5xxかつ`no-store`**（200のエラーページをキャッシュさせない。テストで固定）。ヘルスチェックはAPIに依存しない経路（adminの`/healthz`と同じ）。APIが落ちている間、コンテンツを使うページ（トップ含む）はエラーになる（静的配信ではなくなる代償）。OPERATIONSに記録し、P4-01の死活監視に含める。
- **URL・見た目の維持**: パスと`trailingSlash`、デザイン・ギャラリー・YouTube・カルーセルの挙動は変えない。**記録する差**: スラッシュなしのURLが301→308、`/x/index.html`と`/404/`が404になる。
- **描画時のサニタイズ（必須）**: `rehype-raw`の**後**に`rehype-sanitize`＋URL・data属性の絞り込み。ギャラリー化（`rehypeArticleGallery`。`role`・`tabIndex`・`aria-label`を付ける）は**サニタイズの後**（前だと属性が消える）。**許可リストは独立ファイル`markdown-sanitize.mjs`（スキーマ・`rehypeSafeUrls`・`rehypeSingleDataAttr`・プラグインの並び）にして、`apps/web/lib`と`apps/admin/lib`で内容を同一**にする。**ドリフトの検出**: (1) 2つのファイルが**バイト単位で同一**であることをCI（`check.mjs`）で検査、(2) 共有の事例集と攻撃的な入力（adminのテストにあるものを共有ファイル`services/api/internal/mdcheck/testdata/render-adversarial.json`へ移す）を、web・adminの両方のJSテストで回す、(3) **既存67記事それぞれで、「旧パイプライン（raw→gallery）」と「新パイプライン（raw→sanitize→safeUrls→singleData→gallery）」のHTML文字列が完全一致**することをwebのNodeテストにする（既存の見た目が変わらないことの検査）。`web / lint-typecheck-build`にこのテストを追加。
- **制作メンバー**: 作品のページに`contributors`（同意中は表示名、それ以外は「匿名のメンバー」＋役割）を表示（既存の見た目に合う小さな節。移行した既存記事は制作メンバーが無いので変わらない）。
- **画像の配信**: 静的画像（`/images/...`）は`apps/web/public`のまま（standaloneの出力にも`public/`を含める）。アップロード画像（`/api/v1/public/media/<22文字>`）は、サイトのホストで**その経路だけ**をAPIへ中継する: 完全一致の`^/api/v1/public/media/[A-Za-z0-9_-]{22}$`、**GET・HEADのみ**、`Cookie`・`Authorization`を外す、`proxy_cache`を使わない、`proxy_pass`はURIなしで書く（本番はNginxでP4-01）。ローカル・smokeは`next.config`の`rewrites`（`/api/v1/public/media/:id`のみ。メソッド制限ができないのでsmoke専用。**ローカルのCookieはポートを区別しないので、adminのCookieがwebの中継経由でAPIに届く点に注意**＝公開APIはCookieを見ないので結果は変わらない）。それ以外の`/api/v1/*`（認証が要る経路・管理）はサイトのホストへ出さない。長期キャッシュ・静的コピーをしない（p3-media.md）。
- **コンテナ**: `apps/web`のDockerfileをNext standaloneに（adminに合わせる）: 非root・`read_only`・**`HOSTNAME=0.0.0.0`**・`public/`と`.next/static`をコピー・`poweredByHeader: false`・`images.unoptimized`を維持・`.next/cache`をtmpfs。compose: `API_INTERNAL_URL`・ヘルスチェック・`depends_on: api`。サイトのCSS・コンポーネントの見た目は触らない。
- **web CIの検証を失わない**: web CIの`web:e2e`・全ルートの検査は、**擬似の公開API**（`scripts/mock-public-api.mjs`。`content/*.md`をgray-matterで読み、移行と同じ規則で`/api/v1/public/contents*`を返すだけの小さなNodeサーバー）を起動して`next start`と組み合わせて動かす（APIとDBが無くても全ルートを検査できる）。擬似APIは並び・日付・excerptの扱いを**APIの契約どおり**に返し、契約の差（実APIとの）はsmokeの比較で検出する。

## 4. P3-04-0 と P3-04c: 基準と比較

- **P3-04-0（最初のPR。サイトのコードを変える前）**: 現行の静的exportをビルドし、Playwrightで全URL（`/`・`/blog/`・`/news/`・`/project/`・記事67・`/terms/`）の**指紋**を`docs/legacy-baseline/pages.json`に固定（本文そのものは入れない）。指紋の内容: `title`・`meta description`・`og:image`/`og:url`/twitterのメタ・`h1`・**記事本文のDOMを「タグ＋許可した属性＋テキスト」に正規化したハッシュ**（`article-gallery`と`data-count`・`img`の`role`/`tabindex`/`aria-label`・`details`を含む。テキストだけのハッシュでは属性の欠落を検出できない）・本文テキストの長さ・記事ページの`time[datetime]`と表示日付・カテゴリ・「投稿者」・カバー画像の`src`/`alt`・本文内の`img`の`src`・リンクの`href`・YouTube埋め込みのID・**一覧の各カードのタイトル・日付・抜粋・画像・href と並び（slug列）**・トップのフィードと作品カルーセルの順・**URLの変種のステータスと`Location`**（スラッシュなし・`/x/index.html`・未知のslug・大文字・`%2F`）。フレークの原因（HeroSlider・MotionReveal・遅延読み込み・YouTubeのiframe）を避けるため、抽出範囲を`main article`とカードに限る。**Next 16の`htmlLimitedBots`（メタデータのストリーミング）で、ボット以外のUAでは`<title>`・metaが`head`の外へ出る可能性があるため、抽出はボットのUAでも行う**。frontmatterのゴールデン（gray-matterの出力）と日付のゴールデンも、このPRで固定する（取得のスクリプトは`scripts/`に置き、再生成できるようにする）。
- **P3-04c（比較を常時実行）**: smokeを「bootstrap→招待受諾→`import-legacy --apply`→サイトの全ルートが200→**同じ抽出関数で指紋を取り、基準と完全一致**」の順にする（一度きりにせず、毎回）。差分は一覧にし、既知の意図した差（§3の記録する差。無ければ0）以外は失敗。あわせて全画像532が`inventory.json`のsha256と一致（`web-verify`。**`takedown.json`の画像は「存在しないこと」を検査し、それ以外は一致**＝取り下げ手段を持つ）。デザインはスクリーンショットを目視（`docs/legacy-baseline/screenshots`）。
- **意図した差分の検証（D14）**: 投稿者名が公開許可の確認前に`nu_digital`へ変わる差は、**理由と対象を明示した「意図した差分」**として検証する（基準の一括更新・判定の緩和はしない）。指紋に**追加の項目**（`mainDomShaMasked`・`articleDomShaMasked`＝DOMの「投稿者：…」の名前を`{author}`に置き換えたハッシュ、`author`＝その名前）を足し、**既存の項目は一切変えない**（移設前のサイトから取り直し、既存の項目が現行の`pages.json`と一致することを確認する＝基準を書き換えていない証拠）。比較では、投稿者名の差が許されるのは**`known-diffs.json`に載った記事だけ**（記事・理由・期待する表示を明記。例: `yurarara`が未確認→`nu_digital`）で、その場合でも`*Masked`のハッシュは**基準と完全一致**でなければならない（投稿者名以外の差を隠さない）。許可済みの投稿者の記事は差分0。known-diffsに載っていない記事で`author`が変わったら失敗。
- **撤回の反映（smoke）**: 作品に制作メンバーを付けて公開→同意→サイトに名前が出る→撤回→**次のリクエストで匿名**に戻る（再ビルド・再公開なし）。**HTMLに加えてRSCの応答（`?_rsc`）のヘッダー（`no-store`等）と内容**も確かめる。公開停止で記事・画像が404。

## 5. リスクと未確定（ユーザーの確認が要るものを含む）

1. **投稿者名（D14。確定）**: §2.1。移行直後は、投稿者（`yurarara`・`iwa`・`mizmelon`）の記事は**未確認なので`nu_digital`と表示**され、基準との間に意図した差分が出る（理由: 公開許可が未確認）。管理者が本人の公開許可を確認・記録した順に、元の表示へ戻る。
2. 動的配信への切り替えで、サイトが**APIとDBの可用性に依存**する。→ 記録・監視。負荷が出たら、キャッシュではなくAPI側の最適化を先に検討（D08のため）。
3. 4GB VPSでのNode（サイト）のメモリ・起動時間は未実測。
4. 画像532ファイル（約126MiB）をサイトのコンテナイメージに含めるとイメージが大きい。→ 当面は含める（D13）。ボリュームへ出す案は別途。
5. **静的画像の取り下げ**: 本人が写った画像等を管理者が取り下げる手順（`takedown.json`＋リポジトリの作業ツリーから削除＋再デプロイ）をOPERATIONSに書く。**Git履歴には残る**（履歴の書き換えはD13・運用上行わない。公開サイトからの配信停止までを手順とする）。

## 6. 暫定採用として記録するもの

1. 切り替えは置き換え。静的イメージへは戻さない（切り戻し先はAPI接続版の前）。`content/*.md`は移行の入力・ゴールデン・擬似APIにだけ使う。
2. 移行は全件検査（sha256・frontmatter・mdcheck）→1トランザクション→公開済み・通知なし。再実行は公開ポインタに触れず、差分は上書きせず報告。取り下げ一覧の記事は移行しない。
3. 動的レンダリング（キャッシュしない）でD08を満たす。キャッシュ関連の設定をCIで禁止。
4. 並びは既存と同じ比較関数でサイト側が行う。抜粋は`metadata.excerpt ?? summary`。
5. サイトのホストへは`/api/v1/public/media/<22文字>`のGET/HEADだけを中継する。
6. サニタイズの実装は`markdown-sanitize.mjs`をweb・adminで同一に保つ（バイト一致をCI検査）。
7. 基準（指紋・ゴールデン）は、サイトのコードを変える前に固定する。
8. 投稿者は`legacy_authors`で管理（名前は版・JSONに保存しない）。公開可否は紐付けと別で既定は`nu_digital`。公開APIが配信のたびに解決（D14）。
9. 匿名化による表示差分は「意図した差分」（記事・理由・期待値を明記）として検証し、`*Masked`の指紋で投稿者名以外の差を隠さない。
