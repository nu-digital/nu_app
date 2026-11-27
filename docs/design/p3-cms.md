# P3 設計メモ: CMS（記事・作品）・メディア・公開API

> **状態（2026-11-28）: 実装済み。これは設計の経緯と理由の記録（履歴）**で、実装の正本はコードと[docs/README.md](../README.md)の各正本文書（[ARCHITECTURE](../ARCHITECTURE.md)・[DATABASE](../DATABASE.md)・[API](../API.md)・[AUTHZ](../AUTHZ.md)）。以下の「暫定」「提案」「未実装」などの表現は、書かれた当時のもの。

- 2026-11-21 / Claude Code（Sonnet 5.5）。状態: **Opusレビュー（2026-11-21、指摘15件）を反映済みの設計**。実装はこの版に従う。
- 正本: REQUIREMENTS（Phase 3）、DATABASE §4、API §4、AUTHZ（`contents.*` / `public.*`）、DECISIONS D01・D06・D08・D10・D12。ここは**それらの範囲で決めきれない細部**を書く。確定事項（D01〜D13）は変えない。暫定は実装時にDECISIONSの「暫定採用」へ移す。
- DATABASE §4 との差: `work_contributors`に`revision_id`を足し**版ごとに持つ**（§2。公開後の変更が公開されない・公開版の制作メンバーが固定されるため）。

## 0. Opusレビューの反映一覧

| # | 重大度 | 指摘 | 反映 |
|---|---|---|---|
| 1 | 高 | publishが「その時点の作業版」だと、Editorが確認していない版が公開される | publishは`revisionNo`必須。作業版と違えば409（§4） |
| 2 | 高 | 制作メンバーに版が無く、公開後の変更がEditorの操作なしで公開される | 制作メンバーを版ごとに持つ。変更は新しい版（§2・§4） |
| 3 | 高 | 描画側にサニタイズが無い（既存`ContentArticle`は`rehype-raw`のみ） | 描画時の`rehype-sanitize`を**必須**にし、P3-03のプレビューとP3-04の公開描画の前提にする。許可・拒否の事例集をGo/JS両方で回す（§5） |
| 4 | 高 | Memberの一覧で、公開中コンテンツの作業版タイトルが漏れる | 権限の無い行は公開版の列だけを返すクエリに分ける。テストで固定（§3） |
| 5 | 中 | 関係の判定とDB更新の間のTOCTOU | 書き込みは「Tx開始→`contents FOR UPDATE`→関係を読み直し→判定→更新」（§3） |
| 6 | 中 | 403/404の出し分けで下書きの存在が分かる | 表で固定（§3） |
| 7 | 中 | 「公開版ID一覧・クレジット変更時刻」を公開APIで出すと撤回の時刻が外から見える | 公開APIでは出さない。Admin限定の世代番号に変更（§6・P3-04） |
| 8 | 中 | ETagの計算が不十分（表示名の変更を拾えない） | 応答バイト列全体のハッシュ（強いETag）。公開APIは認証情報で応答を変えない（§6） |
| 9 | 中 | HTMLコメント・`metadata.author`から内部メモや本人情報が出る | コメントは新規保存では拒否、移行時は`metadata.sourceUrl`へ取り出し本文から外す。`author`/`image`/`sourceId`は移行ツールとEditor以上だけが書く（§2・§5） |
| 10 | 中 | 許可リストの具体的な抜け | §5に明記（重複属性・エンティティ解読後・開閉の対応・属性なしdiv・URL正規化・`//`拒否・参照定義/autolink・複数行RawHTML・外部画像） |
| 11 | 中 | 本文サイズ・頻度の上限が無い | 本文256KiB・リクエスト512KiB・保存の頻度制限。認可とロックは重い解析の前（§4） |
| 12 | 低 | 複合FK＋DEFERRABLEの細部 | UUIDをGo側で先に発行、`working_revision_id`はNOT NULL、制約は`DEFERRABLE INITIALLY IMMEDIATE`で作成時だけ`SET CONSTRAINTS ... DEFERRED`（§2） |
| 13 | 低 | 追記専用の範囲 | `content_publication_events`にも拒否トリガー。協力者・制作メンバーの変更は監査ログ。TRUNCATEは既存と同水準で割り切り（§2） |
| 14 | 低 | 通知のdedupeキー | `dedupe_key`は公開イベントのID。payloadは公開版のタイトル。公開停止後の遅延送信は許容して記録（§7） |
| 15 | 低 | 同一内容の保存・アーカイブ | baseRevisionの比較を先に。アーカイブのAPIはP3-01では作らない（`archived_at`は未使用）（§4） |

## 1. 段階分割（1 Issue / 1 PR を保つため）

| 段階 | 内容 |
|---|---|
| P3-01a | migration（contents/revisions/collaborators/contributors/events）、Markdownの検査、作成・一覧・取得・版の保存（競合409）・共同編集者・制作メンバーのAPI。公開はまだ無い |
| P3-01b | publish/unpublish（版指定）、公開API（公開版のみ・クレジットを配信時に解決）、プロフィールの制作実績、公開の通知（outbox）、管理者の確認一覧 |
| P3-02 | メディア（保存・認可配信・公開/非公開の分離・D06の検査と再エンコード） |
| P3-03 | CMS画面（下書き一覧・編集・共同編集者・制作メンバー・公開）。公開同意の導線。**プレビューは`rehype-sanitize`必須** |
| P3-04 | 既存67記事・画像の再実行可能な移行、公開サイトAPI接続（描画に`rehype-sanitize`）、全URL/表示比較、撤回時の再生成/無効化 |

## 2. データ（DATABASE §4 に加える具体）

- `contents`: `type`(article/project)と`section`(blog/news/project)の組はCHECK（article→blog/news、project→project）。`slug`は`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`（大文字・ドットを保つ）。`(section, slug)` UNIQUE。**slugは作成後に変えない**。`archived_at`はP3-01では使わない（APIを作らない）。
- 版の不変性: `content_revisions`は追記専用（UPDATE/DELETEをトリガーで拒否）。`(content_id, revision_no)` UNIQUE。`revision_no`は1から連番。
- ポインタの整合性: `contents.working_revision_id`は**NOT NULL**、`published_revision_id`はNULL可。どちらも**複合FK** `(id, working_revision_id) → content_revisions(content_id, id)`（同じコンテンツの版だけを指せる。参照先に`UNIQUE(content_id, id)`）。循環参照（revision.content_id→contents、contents→revision）は両FKとも`DEFERRABLE INITIALLY IMMEDIATE`にし、作成処理だけが`SET CONSTRAINTS ... DEFERRED`で、**版のUUIDをGo側で先に発行**してcontents→revisionの順に挿入する（違反は通常のFKエラーとして出る）。
- 日付（D12）: `date_label` / `date_kind` / `sort_date` / `sort_date_is_estimated`。CHECK: kind=dayは`sort_date`必須かつestimated=false、month/rangeは`sort_date`あり（estimated=true可）、unknownは`sort_date` NULL可。表示は常に`date_label`。
- `cover_media_id`はP3-02までFKなしのuuid（NULL可。P3-01aでは常にNULL）。P3-02で`media`を作るときFKを追加。
- `metadata_json`: キー許可リスト（`category`, `excerpt`, `image`, `sourceId`, `sourceUrl`。**`author`は2026-11-21のD14で許可キーから外す**＝投稿者名は版に保存せず`legacy_authors`で管理し、公開APIが配信時に解決する。[p3-web.md §2.1](p3-web.md)）。未知のキーは422。**`image`・`sourceId`・`sourceUrl`は移行ツール（コマンドライン）とEditor以上だけが書ける**（`author`はD14で廃止）（Memberは書けない＝なりすまし・D08を通らない名前の掲載を防ぐ）。長さ・型を制限。
- `work_contributors`: 列に`revision_id`（`content_revisions`へのFK）を加え、**その版の制作メンバー**とする。`(revision_id, user_id, role_label)` UNIQUE。`type=project`のコンテンツの版だけ（トリガーで検査）。ユーザーは停止・退会済みでも登録できる（D08）。`role_label`は1〜50文字で、URL・改行・制御文字・`@ < >`を含めない。制作メンバーを変えると**新しい版**ができる（§4）。
- `content_collaborators`: 作者本人は含めない（冪等に無視）。新規に追加できるのは有効（active）なユーザーだけ。上限20人。変更は監査ログ。
- `content_publication_events`: publish/unpublishの追記（誰が・どの版を・いつ）。**追記専用トリガー**（`content_revisions`と同じ）。

## 3. 認可（AUTHZ既存。実装での具体）

- **書き込み系は次の順**: トランザクション開始 → `contents`行を`FOR UPDATE` → トランザクション内で作者・共同編集者を**読み直す** → `authz.Decide` → 更新。ロック順は`contents` → collaborators/contributors → `users`(active確認)で固定。publishはトランザクション内で`user_roles`も読み直す。
- **403と404の表**（下書きの存在を漏らさない）:

| 操作 | 存在しない | 存在するが関係が無い | 権限あり |
|---|---|---|---|
| `GET /contents/{id}`・`POST .../revisions`・`PUT .../collaborators`・`PUT .../contributors` | 404 | **404（同じ文言）** | 200/201 |
| `POST .../publish`・`unpublish` | **403**（DBを見る前にロールだけで判定） | 403 | 200 |
| `GET /public/...` | 404 | 404（未公開・アーカイブも同じ） | 200 |

  `authorize()`は関係が無いと403を返すため、get/revisions/collaborators/contributorsは**関係を先に判定して404へ変換**する専用の経路を作る。
- `GET /contents`（Member）: 自分が作者・共同編集者のコンテンツ（作業版の値）＋他人の**公開中**コンテンツ。後者は**公開版の列（`published_revision_id`側）だけ**を返す（作業版のタイトル・要約・メタデータは返さない）。Editor以上は全件の作業版。SQLを2本に分け、「作業版のタイトルが他のMemberの一覧に出ない」をテストで固定する（プロフィールの制作実績も同様）。
- 公開・公開停止はEditor以上のみ。**作者・共同編集者でも不可**（D01）。共同編集者の指定は作者/Editor以上（共同編集者は不可＝AUTHZ既存）。

## 4. 版の保存・公開と競合（受け入れ3・4）

- 重い処理（Markdown検査）の**前**に、サイズ上限（本文256KiB・リクエスト512KiB）と認可・行ロックを済ませる。保存は利用者ごとの頻度制限（既存のレート制限を流用）。
- `POST /contents/{id}/revisions`: `baseRevision`必須。ロック後、作業版の`revision_no`と一致しなければ**409**（応答に現在の作業版。閲覧権限は判定済み）。一致すれば`+1`の新しい版を追加し、作業版ポインタを進める。**公開ポインタは変えない**。baseRevisionの比較を**先に**行い、そのうえで内容が直前の作業版と完全に同じ（metadataは正規化して比較）なら新しい版を作らず200で現在版を返す。
- `PUT /contents/{id}/contributors`: `baseRevision`必須。**新しい版**（現在の作業版の内容のコピー＋新しい制作メンバー）を作る＝公開版には影響しない。
- `POST /contents/{id}/publish`: **`revisionNo`必須**。ロック後、作業版と一致しなければ409（Editorが確認した版と違う版は公開しない）。公開ポインタをその版へ。`content_publication_events`に追記。公開時の版が持つ制作メンバーがそのまま公開される。
- `unpublish`: 公開ポインタをNULLへ。イベントを追記。
- slug重複は409。slugの総当たりで下書きの名前を探れる点は内部メンバーに限られるため許容（記録）。
- 同時に2人が同じbaseで保存→一方201・他方409。保存と公開の同時実行でも、公開されるのは指定した版だけ（テストで保証。行ロックを外すミューテーションで検出）。

## 5. Markdownの検査（D10・API §4）

- 役割: **サーバーの検査は「方針違反を422で返す」もの。本当の防壁は描画側の`rehype-sanitize`**（`rehype-raw`の**後**。許可リストはサーバーと同じ）。公開サイトの描画（P3-04）とCMSのプレビュー（P3-03）の前提とし、**許可・拒否の事例集（JSON）を1つ作り、GoとJSの両方のテストで回す**。既存の`ContentArticle`は`rehype-raw`のみでサニタイズが無い（既知。CMS接続前に必ず塞ぐ）。
- 許可リスト（既存67記事の実測。生`iframe/script/video`は0件）:
  - Markdown標準＋GFM
  - 生HTMLは次の**だけ**: `details`、`summary`、`div`（**(a)属性なし**で子にMarkdownを含められる＝`recall.md`の実例、**(b)`data-youtube=<11桁ID>` / `data-video=featured` / `data-carousel=overview`のいずれか1つだけを持ち子なし**）。他のタグ・属性（`on*`・`style`・`class`・`id`含む）は422。
  - 同じ属性が2回ある場合は拒否。属性値は**エンティティを解読した後**で検査。`details`/`summary`/`div`は開閉の数が合わなければ拒否。属性名は小文字に正規化して判定。
  - **HTMLコメントは新規保存では拒否**。移行時は`<!-- 元URL: … | 取得日: … -->`を`metadata.sourceUrl`などへ取り出し、本文から外す（23記事）。
  - URL: エンティティ解読・`%`復号・タブ/改行/先頭空白の除去の**後**でスキームを判定。リンクは`http`/`https`/`mailto`と`/`始まりの相対パス（**`//`と`/\`は拒否**）。画像は`/images/`と`/api/v1/public/media/`の相対パスだけ（**外部の`https`画像は不可**＝閲覧者のIP追跡とD06のEXIF除去が効かないため。暫定）。参照定義・autolink・linkifyの結果も、Link/ImageノードとしてAST上で同じ検査を通す。
  - 複数行にまたがるインラインHTMLは全セグメントをつなげてからトークナイズ。
  - YouTube: 本文のURL（許可ホストと11桁ID）または`data-youtube`から、アプリ側が動画IDで埋め込みを生成。利用者のiframeは通さない。
- 実装は**Markdownパーサー（goldmark）でASTを走査**し、HTMLノードを`golang.org/x/net/html`のトークナイザーで検査する（正規表現でHTMLを検査しない）。goldmarkの挙動（リンク先のエンティティ解読の有無など）は実装時に実測してテストに固定する。依存は版を固定し、採用理由をTOOLCHAINに書く。
- 既存記事は移行時にこの検査を**67本すべて**通す（落ちたものは一覧にして人が判断。勝手に書き換えない）。

## 6. 公開API（P3-01b）と匿名化（D08・受け入れ7・8）

- `GET /public/contents/{section}/{slug}`と`GET /public/contents`は**公開ポインタの版だけ**を返す。下書き・未公開・存在しないものは同じ404（判定は1本のSQL）。作業版・版の履歴・作者のユーザーID・共同編集者・内部IDは返さない。
- 制作メンバーは**公開版が持つ**一覧を、配信時に`public_credit_consents`と結合して解決する。同意中（`withdrawn_at IS NULL`の行がある）のメンバーだけ**プロフィールの表示名**＋役割。それ以外（未同意・撤回・個人情報削除。削除時は同意も撤回される＝`private_profile.go`）は**「匿名のメンバー」**（役割は出す。名前・ユーザーIDは出さない）。名前を版・Markdownに焼き込まない。
- キャッシュ: 公開APIは認証情報（Cookie等）で応答を変えない。ETagは**応答バイト列全体のハッシュ（強いETag）**。`Cache-Control: no-cache`。公開サイトが静的に焼く場合（P3-04）は、撤回・削除・表示名の変更で再生成/無効化が必要（Next.jsのfetchキャッシュ・ISR・静的書き出しにも適用）。そのための材料は**公開APIでは出さず**、Admin限定のエンドポイントで「公開クレジットの世代番号」（時刻でなくカウンター）を返す。
- 既存記事の`metadata.author`（ハンドル名等）は移行した原文の事実だが、D08の自動匿名化の対象外。管理者向けに「公開版の本文・`metadata.author`・`role_label`に残る可能性のある本人情報」の確認一覧を出す。

## 7. 通知（P2-04の再利用）

- 公開時に`notification_outbox`へ`content.published`（公開した版のタイトルと種別だけ。機密なし）を**公開と同じトランザクション**で積む。`dedupe_key`は公開イベントのID（再公開の通知が捨てられない）。`notification_settings`に種別を1つ追加するmigrationを同じPRに含める。公開停止は通知しない。チャンネル未設定の間に公開停止されても後から公開通知が送られる点は許容（記録）。

## 8. テスト計画（実PostgreSQL）

作成の認可（Memberは作成可・匿名401）、閲覧・保存の可否（作者/共同編集者/Editor/他のMember）、**403/404の表**、解除された共同編集者の保存が拒否される、baseRevision競合（並行で201+409）、保存で公開ポインタが変わらない、**公開は指定した版だけ（並行保存と競合）**、publishは作者でも403・Editorは可、複合FKが別コンテンツの版を拒否、追記専用（版・公開イベント）、slug形式と重複、日付CHECK、metadata許可リストと**Memberは`author`等を書けない**、Markdown検査の表（許可・拒否。XSS・`javascript:`・生iframe・`on*`・重複属性・エンティティ・コメント・`//`・外部画像・属性なしdivと`recall.md`・**実記事67本すべて**）、制作メンバーは作品のみ・退会者も登録可・版ごとで公開版に影響しない、collaboratorsの上限・停止ユーザー拒否、**Memberの一覧に作業版のタイトルが出ない**、公開APIが下書き/作業版/内部IDを返さない、同意撤回後に古い版でも匿名、ETagの変化、ミューテーション（行ロック・版指定・公開ポインタ・許可リスト・同意結合・一覧の列の取り違え）。

## 9. 暫定採用として記録するもの（実装時にDECISIONSへ）

1. 公開クレジットの名前はプロフィールの表示名（氏名ではない）。同意していない・撤回した人は「匿名のメンバー」（D08の「退会メンバー等」の範囲）。
2. slugは作成後に変更不可。アーカイブのAPIは作らない。
3. 制作メンバーは版ごと。変更は新しい版。公開はEditorが版を指定。
4. 外部の`https`画像は不可（画像は自サイトのパスのみ）。HTMLコメントは新規保存で拒否。
5. `metadata.author`等は移行ツールとEditor以上のみ。
6. 版の保持は上限なし（本文256KiB/版・頻度制限。容量は実測してから判断）。
7. 公開画像の切り替えはP3-02で設計（P3-01は画像参照を持たない）。
