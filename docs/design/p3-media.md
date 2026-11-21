# P3-02 設計メモ: メディア（画像）

- 2026-11-21 / Claude Code（Sonnet 5.5）。状態: **Opusレビュー（2026-11-21、指摘15件・高4）を反映済みの設計**。実装はこの版に従う。
- 正本: DECISIONS D06（画像の上限・縮小・再エンコード）、REQUIREMENTS（Phase 3の画像）、DATABASE §5（`media_assets`・`media_references`）、ARCHITECTURE（保存先と配信）、AUTHZ（`media.*`）。確定事項は変えない。ここはその範囲で決めきれない細部。
- 親設計: [p3-cms.md](p3-cms.md)（公開版が参照する画像を、公開版の間だけ公開する）。
- 正本文書との差（実装PRで同時に直す）: DATABASE §5の`visibility`列を持たず公開を導出する／`public_id`・`redacted_at`・`sha256`を追加／ARCHITECTUREの「公開時に公開領域へ複製」「`.webp`のURL例」は採らない／AUTHZの`public.media.get`の「トークン検証はハンドラー側」は匿名で参照判定のみ。

## 0. Opusレビューの反映一覧

| # | 重大度 | 指摘 | 反映 |
|---|---|---|---|
| 1 | 高 | ディスク枯渇・消せない画像の積み上がり | 利用者ごとの1日の枚数・合計バイト上限、空き容量の検査（507）、1版あたりの画像参照の上限、容量の監視（§3・§6・OPERATIONS） |
| 2 | 高 | 本人情報を含む画像を消す手段が無い | `nuctl media-redact`（行と参照は残し、`redacted_at`を立ててファイルを消す。配信は404。公開・保存で参照を拒否）（§6） |
| 3 | 高 | WebPのVP8X寸法とフレーム寸法のずれ・JPEG/PNGの構造検査の甘さ | RIFF/JPEG/PNGを**自前で厳密に走査**（canvasとフレームの一致、全チャンク走査、SOF/SOS/DNL、IHDR位置、`fcTL`/`fdAT`）（§3） |
| 4 | 高 | メモリ見積りが楽観的・OOMはrecoverできない | **順序を「デコード→縮小→向き補正」**に変更、`GOMEMLIMIT`と`mem_limit`、最悪ケースを実測して同時数を決める（§3） |
| 5 | 中 | 本文を読み切ってから枠を取る順序・サーバーのタイムアウト・Nextの中継の本文上限 | 受信中の件数の枠を先に取る、ハンドラーごとに期限を延長、**Nextの`proxyClientMaxBodySize`（既定10MiB）を引き上げる**（確認済み）（§3・§4） |
| 6 | 中 | CPUによる枠の独占 | 利用者ごとの同時処理数1、SOS数の上限、縮小前に高速経路の型へ変換、処理時間のログ（§3） |
| 7 | 中 | 版をコピーする経路で画像参照が引き継がれない | 版を作る全経路（作成・保存・制作メンバー変更）で参照を作る/コピー、`sameRevision`にカバーを含める、`coverMediaId`の省略は維持（§6） |
| 8 | 中 | 下書きの画像をプレビューする経路が無い | 認証つきの`GET /media/by-public/{public_id}`（アップロード者・Editor以上・その画像を参照するコンテンツの作者/共同編集者）（§5） |
| 9 | 中 | GCの競合・ファイル操作の順序 | `media_references.media_id`にFK、GCは1件ずつ条件つきDELETE→コミット後にファイル削除、保存側は`FOR KEY SHARE`、親ディレクトリもfsync、`tmp`の掃除（§6） |
| 10 | 中 | キャッシュ | 非公開は`private, no-store`、公開は参照判定の**後に**304、公開画像を長期キャッシュ・静的コピーしないことをP3-04とNginxの要件に（§4・§5） |
| 11 | 中 | 本文からのpublic_id抽出はASTから | `mdcheck`が解決済みImageノードのURLから抽出し、厳密な形だけ許す（§6） |
| 12 | 低 | 色・特殊な形式 | CMYK/16bit/パレット+tRNSのテスト。ICCは捨てる（Display P3はsRGBへ。記録）（§3・§8） |
| 13 | 低 | EXIFパーサーの範囲 | IFD0のみ・SHORT/count=1/1〜8のみ・uint64でオフセット・最初のExifのみ・補正後の寸法を保存（§3） |
| 14 | 低 | 配信ヘッダー・公開SQL・トリガーの範囲 | CORP、`purpose='cms'`等の絞り込み、追記専用は`content_revision`のみ（§2・§4・§5） |
| 15 | 低 | 正本文書との不整合 | 上の「正本文書との差」に列挙し、実装PRで直す |

## 1. 段階

| 段階 | 内容 |
|---|---|
| P3-02a | 基盤（保存・取り込み・認可配信・公開配信）＋CMS連携（カバー・本文の画像・参照整合性）＋運用CLI（`media-gc`・`media-verify`・`media-redact`）。`purpose=cms`だけ受け付ける |
| P3-02b | アバター・物品写真の連携（プロフィール/物品への紐づけ、メンバーへの配信規則）。`purpose=avatar/inventory`を有効化 |

## 2. データ（migration 00009）

- `media_assets`: `id`(uuid) / **`public_id`**（128bit乱数のbase64url 22文字。`id`から導けない）UNIQUE / `storage_key`（`ab/<uuid>.jpg|png`。絶対パスはAPIに出さない）UNIQUE / `mime_type`（CHECK: jpeg・png・webp。**実際に保存するのはjpegとpng**）/ `byte_size` / `width` / `height`（**向き補正後**の寸法。CHECK: 各辺1〜10000、画素数≤2,000万）/ `sha256`（再エンコード後）/ `purpose`（CHECK: avatar/inventory/cms）/ `uploaded_by` / `created_at` / **`redacted_at`**（NULL可）。**`visibility`列は持たない**: 公開かどうかは§5のとおり「公開中の版が参照しているか」で導く（公開フラグの切り替えは、公開停止・再公開・複数の版の参照で戻し忘れ/消し忘れを起こす）。
- `media_references`: `(media_id, owner_type, owner_id, purpose)` PK。`media_id`は`media_assets`へのFK（ON DELETE RESTRICT）。`owner_type=content_revision`（`owner_id`＝版のid、`purpose`＝cover/inline）。**追記専用トリガーは`owner_type=content_revision`の行だけ**（版は不変。P3-02bでアバターの差し替えに参照の削除が要るため、他の種別は対象外）。
- `content_revisions.cover_media_id`にFK（`media_assets(id)`、RESTRICT）。
- 利用者ごとの上限の集計用に`media_assets (uploaded_by, created_at)`のインデックス。

## 3. 取り込みパイプライン（`POST /media?purpose=cms`）

本文は生のバイト列（multipartは使わない）。`bodyAndType`はこのパスだけJSON以外と大きな本文（上限10MiB+4KiB）を許す。順序は**安い検査→重い処理**。

1. 認証。利用者ごとの頻度制限（1分20回）。`purpose`の検査（P3-02aは`cms`のみ。他は422）。**利用者ごとの1日の上限**（200枚・合計200MiB。DBの`media_assets`を集計）→ 429。**空き容量の検査**（`MEDIA_DIR`のstatfsで空きが1GiB未満なら507。しきい値は設定可）。
2. **受信中の枠**（同時8件）を、本文を読む**前**に取る（取れなければ503＋`Retry-After`）。利用者ごとの同時処理数は1（2件目は429）。受信と処理の期限は`http.ResponseController`でこのハンドラーだけ延長（受信2分。サーバー全体の`ReadTimeout`15秒は据え置き）。
3. 本文を上限（avatar 5MiB、他10MiB）まで読む。超えたら**読み切らずに**413。0バイトは422。
4. **マジックバイトで形式を決める**: JPEG(`FF D8 FF`)・PNG(8バイトのシグネチャ)・WebP(`RIFF....WEBP`)以外は415。
5. **構造の事前検査（デコードしない・自前の厳密な走査）**:
   - **PNG**: 先頭チャンクがIHDRであること、全チャンクを走査（長さがファイル末尾を越えない・チャンク数の上限・CRCは検査しない）、`acTL`・`fcTL`・`fdAT`があれば拒否（APNG）、IHDRの寸法を採る。
   - **WebP**: RIFFのサイズとファイル長が一致すること、全チャンクを走査（奇数長のパディングを考慮）、`VP8X`のアニメーションフラグ・`ANIM`・`ANMF`が**どこにあっても**拒否、**VP8X（canvas）とVP8/VP8Lのフレームヘッダーの寸法が一致**しなければ拒否（canvasだけ小さく偽装してフレームで巨大な領域を確保させる攻撃を防ぐ）。
   - **JPEG**: マーカーを走査し、SOFは1つだけ・対応する種別（baseline/progressive）だけ・**DNLは拒否**、**SOSの数は32まで**、寸法を採る。
   - 採った寸法と`image.DecodeConfig`の結果が一致しなければ拒否。各辺>10000・画素数>2,000万なら**デコード前に**422（画素数の算術は`uint64`）。
6. **同時デコード数の枠**（初期値2。最悪ケースの実測で決める）。獲得に2秒待っても取れなければ503＋`Retry-After`。デコードは`recover`で包む（OOMは捕まえられないため、メモリ上限で防ぐ）。
7. デコード → **縮小**（avatar長辺512px、他2,560px。拡大しない）→ **向き補正**（縮小後の小さい画像に対して）。縮小の前に、CMYK・パレット・16bit等は**高速経路のある型（RGBA/NRGBA/Gray）へ変換**する。縮小は`x/image/draw`のCatmullRom。EXIFのOrientationは、JPEG(APP1)・WebP(EXIF)・PNG(eXIf)から読む自前の小さなパーサー: **IFD0だけ**・SHORT型・count=1・値1〜8だけ採用（他は補正なし）・オフセットは`uint64`で範囲検査・複数のAPP1があれば最初のExifだけ。
8. **再エンコード**（メタデータは一切書かない＝EXIF・GPS・ICC・コメント・末尾の付加データは消える）: JPEG→JPEG（品質85）、PNG→PNG、WebP→アルファがあればPNG、無ければJPEG。透明画素の下の色は0になる（隠しデータの除去として良い性質なのでテストで固定）。`mime_type`は出力の形式、寸法は補正後。**ICCを捨てる**ため、Display P3の写真はsRGBとして表示され彩度が落ちる（暫定。必要なら既知のsRGB/P3プロファイルだけ再付与する案を別途）。
9. 保存: `<MEDIA_DIR>/tmp`に書いて`fsync`→保存先へ`rename`→**親ディレクトリも`fsync`**（`0640`）。DBの行を挿入。DB失敗ならファイルを消す。ファイル書き込み失敗なら行を作らない。処理時間をログに残す（件数・形式・寸法・ミリ秒。内容は出さない）。
10. 応答 201: `{id, publicUrl:"/api/v1/public/media/<public_id>", url:"/api/v1/media/<id>", width, height, mimeType, byteSize, purpose}`。この時点では`publicUrl`は配信されない。

**WebPの出力形式（暫定）**: 純Goの実用的なWebPエンコーダーが無く、cgoは`distroless/static`と両立しないため、再エンコード後はJPEG/PNG。代替: cgoを許してlibwebpを入れる／ロスレスWebPのみの純Go。

**メモリ**: 最悪ケース（16bit RGBA PNG・プログレッシブJPEG・インターレースPNG・CMYK JPEG・20Mpx）を**テストで実測**し、`OPERATIONS`に記録して同時デコード数を決める。`GOMEMLIMIT`（ソフト上限）とコンテナの`mem_limit`を設定し、OOMでプロセスが落ちる前にGCを促す。向き補正を縮小後に行うため、20Mpxの全画素コピーが1回減る。

## 4. 保存と配信

- 保存先は`MEDIA_DIR`（環境変数。永続ボリューム。Git・イメージの外）。**全ファイルを同じ非公開ディレクトリに置き、Nginx等が直接配信しない**。公開・非公開はAPIの判定（§5）で決める。
- 読み出しは`os.OpenRoot(MEDIA_DIR)`の下で`storage_key`を開く。`storage_key`は形式`^[0-9a-f]{2}/[0-9a-f-]{36}\.(jpg|png)$`を読み出し前にも検査する。`redacted_at`があれば404（ファイルを開かない）。
- 応答ヘッダー: `Content-Type`はDBの`mime_type`、`X-Content-Type-Options: nosniff`、`Content-Security-Policy: default-src 'none'; sandbox`、`Content-Disposition: inline`、`Referrer-Policy: no-referrer`。**非公開**: `Cache-Control: private, no-store`・`Cross-Origin-Resource-Policy: same-origin`。**公開**: `Cache-Control: no-cache`＋強いETag（`sha256`）・`Cross-Origin-Resource-Policy: cross-origin`（公式サイトから読ませるため）。Rangeは`http.ServeContent`に任せる。**公開の304は、参照判定に通った後でだけ返す**。
- 配信ハンドラーの書き込み期限も延長する（遅い回線で大きな画像が切れないように）。
- 要件（P3-04とNginx設定）: **公開画像を長期キャッシュ・静的コピーしない**（`proxy_cache`・Nextの画像最適化キャッシュ・静的書き出しは、公開停止後も画像が残る）。
- `MEDIA_DIR`が未設定なら503 `MEDIA_NOT_CONFIGURED`。admin（Next）の中継は`experimental.proxyClientMaxBodySize`を上限より大きく（例: 12MiB）設定する（既定10MiBでは上限ちょうどの画像が通らない。実装時にE2Eで確認）。

## 5. 認可と公開の境界

- `POST /media`: 有効なユーザー（Member以上）。
- `GET /media/{id}` と **`GET /media/by-public/{public_id}`**（認証必須。プレビュー用）: **アップロード者、Editor以上、またはその画像を（いずれかの版が）参照しているコンテンツの作者・共同編集者**。それ以外は存在しないのと同じ404。
- `GET /public/media/{public_id}`（匿名）: **公開中の版が参照している画像だけ**。判定は1本のSQL: `media_references`（`owner_type=content_revision`）→その版が`contents.published_revision_id`で、`purpose='cms'`、`archived_at IS NULL`、`redacted_at IS NULL`。該当しなければ404（未アップロード・未公開・公開停止後・redacted・存在しないが全て同じ404）。公開停止・差し替えは**配信に即座に反映**される。認証情報を見ない。
- 公開される画像は、公開中の版が参照しているものだけ。下書き・過去の版だけが参照する画像・どの版も参照していない画像は配信されない。

## 6. CMSとの連携・参照整合性・運用

- **版を作る全経路**（作成・保存・制作メンバー変更）で、`cover_media_id`と`media_references`を作る（保存・作成は本文と`coverMediaId`から、制作メンバー変更は直前の版からコピー）。`sameRevision`はカバーも比較する。`coverMediaId`を省略した保存は**維持**（`null`の明示で消す）。公開時に、その版の「本文から抽出した参照」と「保存済みの参照」が一致し、すべて存在し`redacted_at`が無いことを再確認する。
- 参照できる画像: (a) 存在し`redacted_at`が無い、(b) `purpose=cms`、(c) **操作者が自分でアップロードした、またはそのコンテンツの過去の版が既に参照している、またはEditor以上**。違えば422（「画像が見つかりません」。他人のIDの存在を区別して返さない）。**1つの版の画像参照は100件まで**。
- 本文中の画像は`/api/v1/public/media/<public_id>`だけ（URL全体が`^/api/v1/public/media/[A-Za-z0-9_-]{22}$`。クエリ・フラグメント・%エンコード・末尾`/`は拒否）。**`mdcheck`が、検査と同じASTの解決済みImageノード（参照定義を含む）のURLから`public_id`を抽出して返す**（コードブロックや通常のリンクは参照にならない）。`/images/`は既存記事用に残す。
- 画像の削除APIは作らない。運用CLI（**ワークフローから呼べない**。`check.mjs`で`recover-owner`と同様に禁止）:
  - `nuctl media-gc`（既定はdry-run。`--apply`で、参照が無くアップロードから24時間超の画像だけを、**1件ずつ**`DELETE … WHERE NOT EXISTS(参照) AND created_at < now()-24h RETURNING storage_key`し、**コミットの後にファイルを消す**。`tmp`の古いファイルも掃除）。保存側は参照する画像の行を`FOR KEY SHARE`で読む（GCとの競合を避ける）。
  - `nuctl media-verify`（DBの行にファイルが無い／ファイルに行が無いものを報告。削除しない）。
  - **`nuctl media-redact <id> --reason …`**（本人情報を含む画像の誤アップロード・個人情報削除の依頼）: 行と参照は残し、`redacted_at`を立ててファイルを消し、監査ログに残す。以後その画像は配信されず、新しい版・公開の対象にもできない（redactedを参照する版の公開は拒否）。
- 容量: 合計容量を`media-verify`が報告し、`OPERATIONS`に監視の方法とバックアップ容量への影響を記録する。

## 7. 攻撃と失敗の想定（テスト計画）

画像はテスト内で生成（標準の`image/png`・`image/jpeg`。WebPは極小の固定バイト列。不正な形式は手でバイト列を組む）。

- **容量・枠**: 本文が上限+1バイトで413（読み切らない）、0バイト、`Content-Length`偽装、1日の枚数/バイトの上限（429）、空き容量不足（507。しきい値を大きくして再現）、利用者ごとの同時処理1、受信枠・デコード枠の超過（503＋`Retry-After`）、10並行でも同時デコード数が上限を超えない（フックで計数）
- **形式**: SVG・GIF・BMP・HTML・PDF・拡張子偽装・Content-Type偽装 → 415、実体で判定
- **爆弾・偽装寸法**: IHDR/SOF/VP8で巨大寸法を宣言した小さなデータ → デコード前に422（`MemStats`でメモリを使わないこと）。**WebPでVP8X(canvas)だけ小さくフレームが巨大**、JPEGのSOF重複・DNL・SOS過多、PNGのIHDR位置・チャンク長の偽装 → 422
- **壊れたデータ**: 途中で切れたJPEG/PNG/WebP、RIFFサイズ不一致 → 422（500・panicにならない）
- **アニメーション**: APNG（`acTL`/`fcTL`/`fdAT`）、アニメーションWebP（VP8X/ANIM/ANMF。**末尾に隠したもの**も）→ 422
- **メタデータ・付加データ**: EXIF（GPS・撮影者）・コメント・ICC・PNGのtEXt/eXIf・**画像の後ろの`<script>`やZIP**・透明画素の下の色 → 出力に残らない（バイト列を検索）
- **向き**: Orientation 1〜8で画素が補正され、5〜8で幅と高さが入れ替わりDBの寸法も補正後。壊れたEXIF（範囲外オフセット・count≠1・値9）でも落ちず補正なし
- **縮小・色**: 長辺が上限を超える画像だけ縮み、小さい画像は拡大されない。CMYK JPEG・16bit PNG・パレット＋tRNS・グレースケールが落ちず、画素が妥当
- **最悪ケースのメモリ実測**: 16bit RGBA 20Mpx PNG・プログレッシブJPEG 20Mpx・インターレースPNG（実測値をログに残し、同時2での上限を確認）
- **配信**: 匿名・他のメンバー・アップロード者・Editor・参照するコンテンツの作者/共同編集者の可否と404、`Content-Type`・nosniff・CSP・CORP・Cache-Control、Range、`storage_key`をDBで改ざんしても`OpenRoot`の外を開けない、redactedは404
- **公開境界**: 参照が無い／下書きだけ／過去の版だけ／公開中の版／公開停止後／再公開 の各状態で`/public/media/{public_id}`が200/404。複数の版・コンテンツが同じ画像を参照する場合に、一方の公開停止で他方が影響を受けない。`id`と`public_id`から他方を導けない。公開の304は参照判定の後
- **参照整合性**: 存在しない/他人の/`purpose≠cms`/redacted/不正な形の`public_id`を本文や`coverMediaId`に書くと422。コードブロック内のURLは参照にならない。版を作る全経路（作成・保存・制作メンバー変更）で参照とカバーが引き継がれ、公開後に画像が出る。`coverMediaId`省略で維持・null で消去。1版100件の上限。公開時の再確認。行をSQLで削除しようとすると参照があればFKで拒否。版の参照は追記専用
- **保存の障害**: DB挿入失敗でファイルが残らない、ファイル書き込み失敗で行が残らない、`MEDIA_DIR`未設定で503
- **運用CLI**: gcのdry-runは何も消さない、`--apply`は孤児（24時間超・参照なし）だけ、参照あり・24時間未満は消さない、GC中に新しい参照が付いても整合、verifyの報告、redactで配信・公開・新規参照が止まり監査ログが残る

## 8. 暫定採用として記録するもの（実装時にDECISIONSへ）

1. 出力はJPEG/PNG（WebPのエンコードはしない）。ICCは捨てる（Display P3はsRGBに）。
2. 同時デコード数の初期値2・利用者ごとの同時1・受信枠8。メモリは最悪ケースの実測と契約後の実機で見直す。
3. 公開は「公開中の版が参照している間」だけ（公開フラグを持たない）。
4. 画像の削除APIは作らない。孤児は`media-gc`、本人情報を含む画像は`media-redact`（管理者のCLI）。
5. 取り込みは生のバイト列（multipartを使わない）。1日の上限は200枚・200MiB、空き容量1GiB未満は507。
6. 1つの版の画像参照は100件まで。
