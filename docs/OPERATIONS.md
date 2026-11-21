# Ubuntu VPS運用の実装前契約

区分: **ユーザー確定**／**技術上の採用方針**／**契約後に確認する実情報**を分けて読むこと。この文書は本番デプロイ・VPS契約・DNS設定の実行許可ではない。

## 確定事項と未確認の実情報（D07）

| 項目 | 状態 |
|---|---|
| VPS事業者 | 未契約。さくらのVPSまたはConoHa VPSの4GBプラン予定。**契約後に確認** |
| CPUアーキテクチャ・ディスク容量 | **契約後に確認**（未確認のまま値を仮定しない） |
| OS | Ubuntu 24.04 LTSを第一候補。契約先での提供状況を**確認** |
| ドメイン | `nu-digital.jp`は取得済み。公式=`nu-digital.jp`、内部アプリ=`app.nu-digital.jp` |
| DNS設定・本番接続 | **未完了。完了済みとして扱わない**。SSH接続・DNS変更は行わない |
| 構築方式 | 事業者固有機能に依存せずDocker Compose |
| リバースプロキシ | Nginx正式採用（D03） |

## P0で用意するもの

- 開発用Composeと架空値の `.env.example`。DBポートを公開するならloopbackだけにbindする。
- Next.js web/adminとGo API/Botの責任境界。バージョン・lockfile・採用理由を固定。
- Linux CIでビルド・テスト。ローカル環境との差（パス大文字小文字、LF、実行権限）も確認。

## 本番に接続する前に決めるもの

契約後に実情報（事業者・CPUアーキテクチャ・ディスク容量・Ubuntu提供状況）を確認し、DNS設定、デプロイ権限、secret配置を決める。Nginx・バックアップ・メディア上限・Owner復旧はD03〜D06確定済み。未入力の値を仮定してSSH接続やDNS変更をしない。

## デプロイ設計の合格条件

- mainマージ後のGitHub Actionsでテスト済みSHAのイメージをビルド・push。VPSではビルドしない。PRに本番secretを渡さない。
- 本番デプロイは直列化。途中で新デプロイに中断されてmigrationだけ進む状態を避ける。
- 前版SHAを保存し、DB変更の互換性確認後にmigrationと更新を行う。livenessとDBを含むreadinessを区別し、失敗時は成功通知を送らない。
- DB/API/Botを直接インターネットに公開しない。HTTPS、最小権限ユーザー、ホスト鍵検証、Docker権限、Firewallを確認する。
- secretはGit外。実token・招待URL・学生情報をログや通知に含めない。
- DBとメディアはコンテナ外の永続領域。`docker compose down -v`を本番運用に使わない。
- 切り戻しもレビューされたGitHub Actions経由。前版イメージ復帰はDBの完全巻き戻しではない。破壊的schema変更は段階移行する。
- バックアップは下記「バックアップ（D04）」に従う。VPS外バックアップは初期スコープ外であり、VPS喪失時はデータを失う可能性がある。
- 4GB上で全サービスのメモリ/CPU/ディスク、ログ増加、バックアップ容量を実測する。開発サーバーは常駐させない。

この準備では本番ワークフローを有効化していない。初回公開前に `docs/TESTING.md` のP4と管理者手順を満たす。

## TLS・Nginx（D03）

- Nginxは`nu-digital.jp`と`app.nu-digital.jp`のTLS終端・リバースプロキシ・レート制限・リクエストサイズ制限を担当する。**認証・認可はしない**（Go APIが判断）。
- 証明書は自動更新する。取得・更新の具体的な手段と版は、P4-01の実装時に一次資料で調べて固定し理由を記録する（今は決めない）。
- 更新失敗と期限接近を検知する: 期限の残日数を定期確認し、閾値以下や更新失敗でDiscordの運用通知を送る。更新後はNginxを再読み込みし、HTTPS応答の証明書期限を確認する手順を運用文書に書く。
- DNS未設定の間はACME取得を試験できない。実ドメインでの取得確認は契約・DNS設定後の項目。

## バックアップ（D04）

- 頻度と保持: 毎日03:00(JST)、**成功した**バックアップを7世代保持。
- 内容: DBとメディアを復元時に整合する一組（同一世代ID・同一マニフェスト）として扱う。
- 方式（採用方針）: 短時間のメンテナンスモード（API書き込み拒否）中に`pg_dump`とメディアのスナップショットを取得し、再開する。作業領域に作成→チェックサム検証→世代として確定。初期は書き込み停止を許容し、簡潔さを優先。停止時間は実測して記録する。
- 失敗時: 失敗をDiscordの運用通知で知らせる。**既存の正常な世代は削除しない**。7世代超の古い世代は、新世代の確定・検証が成功した後にだけ削除する。
- 容量: 実測し、不足する場合は**黙って世代数を減らさず**報告して判断を仰ぐ。
- 復元検証: 公開前とバックアップ方式の変更時に、隔離環境へDB+メディアを復元して参照整合（`media_assets`↔ファイル、記事↔画像）を確認する。本番DB/volumeを復元検証に使わない。
- **明記すべきリスク:** 日次成功時でも最大約24時間分の更新を失い得る。VPS自体（ディスク含む）を失った場合は復旧できない。VPS外バックアップは初期スコープに追加しない。運用責任者は利用開始前にこのリスクを確認する。

## Owner緊急復旧（D05、P1-03c）

通常のOwner移譲はWebの二段階フロー（現Ownerが申請→移譲先が承諾）。緊急復旧は**管理CLIのみ**で、公開Webの復旧入口や誰でもOwnerになれる機能は作らない。

```sh
# 運用責任者がサーバー上で、本人確認と承認を行った上で実行する（本番はまだ未構築。実行は運用責任者の手順で別途決める）
docker compose run --rm --no-deps --entrypoint /nuctl api recover-owner \
  --login-id <Ownerのログインid> --confirm-login-id <同じid> \
  --approver-note "<誰が・どう本人確認し・誰が承認したか>"
```

- **できること**: 対象が既存のOwnerなら、全セッションと未使用トークンを失効し、停止中なら有効に戻し、新しい再設定（パスワード未設定なら招待）トークンを一度だけ表示する。**activeなOwnerが1人もいない場合に限り**、既存ユーザーをOwnerに指名できる。activeなOwnerが居るのに別ユーザーをOwnerにすることはできない。
- **必須**: `--approver-note`（空不可・500文字以内）と`--confirm-login-id`（ログインIDの再入力）。
- **記録**: `emergency_recovery_events`（実行OSユーザー・対象・承認メモ）と監査ログ（`recovery.owner_recovered`）に同一トランザクションで残す。トークン・パスワードは記録しない。承認メモに個人情報や秘密値を書かない。
- **通常のデプロイとの区別**: このCLIはGitHub Actionsのworkflowから実行しない（`scripts/check.mjs`が`recover-owner`を含むworkflowを失敗させる）。デプロイ手順書とも別の文書・別の操作として扱い、実行のたびに運用責任者が承認する。
- **レート制限のロック**はAPIプロセスのメモリ上にあるため、APIの再起動で解除される（DBには保存していない）。
- 最後のOwnerが不在になる操作（停止・移譲・降格）はAPI側で拒否する。復旧CLIは「Ownerを復活・指名する」操作に限る。

## 初期Owner作成（P1-01c）

`nuctl bootstrap-owner`（APIイメージの`/nuctl`）が唯一の経路。公開Web/APIに入口はない。`bootstrap_state`（最大1行）の確保とユーザー作成を1トランザクションで行い、並行実行・再実行・別ログインIDでの再実行はすべて拒否する。招待tokenは一度だけ標準出力へ表示し、DBにはSHA-256のみ保存、監査にも残さない（有効期間の既定は24時間）。**本番での実行は、VPS契約・構築後に運用責任者が行う手順として別途定める（まだ実行しない）**。緊急復旧は別のCLI（P1-03）。

## 画像処理の資源（D06・P3-02）

- **同時デコード数は1**（`MediaLimits.DecodeSlots`の既定）。理由: 2026-11-21の実測（Go 1.27.4・`-race`なし・`go test -run TestWorstCaseMemory`）で、最悪ケースの20Mpx・16bit RGBA PNGの単体のピークのヒープが約564MiB（8bit RGBA PNGは約412MiB、baseline JPEG 20Mpxは約384MiB、10Mpxは約243MiB。処理時間は1〜2.6秒）。**2件同時だと約973〜1,055MiB（OSから取得約1.19GiB）**で、`GOMEMLIMIT=768MiB`を付けても抑えきれず、コンテナの`mem_limit`（1g）を超えるため、1にした。実測のコマンドは`services/api/internal/media/media_test.go`の`TestWorstCaseMemory`・`TestWorstCaseConcurrentMemory`（`MEDIA_MEM_TEST=1`で手動実行）。
- 未実測: プログレッシブJPEG・インターレースPNG・CMYK JPEG（生成手段が標準ライブラリに無い）。**契約後に4GB VPSの実機で、実際の写真（特にプログレッシブJPEG・iPhoneのHEIC変換物等）で再実測する**。他サービス（Go API、Next.js×2、PostgreSQL、Bot、Nginx）との同時稼働でも収まるか確認する。
- 受信中の件数は8（本文は最大10MiBずつメモリに置く＝最大約80MiB）。利用者ごとに同時1件・1分20回・1日200枚/200MiB。空きが1GiB未満なら507。
- compose: `MEDIA_DIR=/data/media`（名前付きボリューム`media-data`。イメージの`/data`は`nonroot`所有）、`GOMEMLIMIT`（既定768MiB）、`mem_limit`（既定1g）。`.env`の`API_GOMEMLIMIT`・`API_MEM_LIMIT`で変更できる。

### 画像の運用（P3-02）

- 画像はVPSの永続ボリューム（`MEDIA_DIR`）に、**再エンコード後**の形で保存する。バックアップ（D04）は**DBとメディアを1組**で取る。容量は`nuctl media-verify`が報告する（有効な画像の件数・バイト数・保存先の空き）。**`MEDIA_DIR`の合計容量と空きを監視し、バックアップ7世代分の容量を見積もる**（利用者ごとの上限: 1日200枚・200MiB）。
- 削除APIは無い。次の運用コマンドを、**運用者がサーバー上で手動で実行する**（GitHub Actionsのworkflowから呼ばない。`check.mjs`が禁止）:
  - `nuctl media-gc [--older-than 24h] [--apply]`: 参照が無く24時間以上前の画像（アップロードしたが記事に使われなかったもの）を消す。既定はdry-run。`--older-than`は1時間以上。確認と削除は1文で行い、コミット後にファイルを消す。古い`tmp`も掃除する。
  - `nuctl media-verify`: DBの行にファイルが無い／ファイルに行が無い／無効化済みなのにファイルが残る／想定外のファイルを報告する（何も消さない。食い違いがあれば終了コード1）。
  - `nuctl media-redact --id <uuid> --reason "<理由>"`: **本人情報を含む画像を誤ってアップロードした場合・個人情報削除の依頼**で使う。ファイルを消し、配信・新規参照・公開を止める（行と参照は監査のため残す。`audit_logs`に理由と実行したOSユーザーを記録）。公開中の記事がその画像を参照していれば、画像はすぐ404になり、記事は画像が欠けた状態になる（記事側の修正は別途）。redactedな画像を参照する版は公開できない。
- 公開画像は**長期キャッシュ・静的コピーをしない**: `/api/v1/public/media/*`は`Cache-Control: no-cache`（強いETag）。Nginxの`proxy_cache`・Next.jsの画像最適化キャッシュ・静的書き出しに焼き込むと、公開停止・redact後も画像が残るため、公式サイト側（P3-04）でもこの方針を守る。
### 既存記事の移行と投稿者名（P3-04a・D14）

- `nuctl import-legacy --content-dir <apps/web/content> --baseline-dir <docs/legacy-baseline> --as <ownerのログインID> [--apply]`（既定はdry-run）。運用者がサーバー上で手動で実行する（workflowから呼ばない。`check.mjs`が禁止）。`api`イメージは`content/`を含まないため**読み取り専用でマウント**する: `docker compose run --rm --no-deps -v "$PWD/apps/web/content:/content:ro" -v "$PWD/docs/legacy-baseline:/baseline:ro" --entrypoint /nuctl api import-legacy --content-dir /content --baseline-dir /baseline --as <オーナー> [--apply]`。ゴールデン（`docs/legacy-baseline/`）と食い違えば何も書かずに失敗する。再実行は冪等（既存の記事は触らない）。
- **投稿者名は`legacy_authors`にだけ置き、記事の版・監査ログ・Discordには入れない**。移行直後はすべて公開許可が**未確認**で、公開APIの`author`は「nu_digital」。名前の公開は、管理者が本人の許可を確認して記録した後だけ（P3-04a-2の管理画面）。ユーザーへの紐付けは公開可否と独立で、表示名での自動紐付けはしない。
- **投稿者の管理API（P3-04a-2）**: `GET /admin/legacy-authors`（未確認が先頭）、`POST .../permit`（確認の方法の記録が必須。確認者・日時を保存）、`POST .../withdraw`、`PUT/DELETE .../link`（ユーザーIDで紐付け。表示名では紐付けない）。紐付けと公開可否は互いに変えない。**同意の撤回**で紐付く投稿者は撤回状態になり（再同意しても自動では再公開しない）、**個人情報の削除**で名前は匿名の記号へ置換され再許可・再紐付けもできない。監査に名前・確認の記録内容は入らない。画面は `/admin/legacy-authors`（ユーザー管理から入る）。
- 取り下げ: `docs/legacy-baseline/takedown.json`に載せた記事は移行しない（災害復旧後の再実行でも再公開されない）。
- **ロールバック**: 移行は追加のみ。誤りは記事を公開停止する（`unpublish`）。CMS更新後に静的イメージへ戻さない（更新が失われ、取り下げ済みの内容が復活し得る）。

- 色: ICCプロファイルは破棄するため、Display P3の写真はsRGBとして表示され彩度が落ちる（暫定）。

## Discord通知workerの運用（P2-04）

- 通知は業務のDB更新と同じトランザクションでoutboxへ記録され、APIプロセス内のworkerがDiscordへ送る。Discordが落ちても貸出・返却は確定し、通知は送信待ちに残る。
- 有効にする手順: Discord Botを作成してサーバーへ招待（メッセージ送信の権限だけ）→ Botのtokenを`.env`の`DISCORD_BOT_TOKEN`へ（**Git・チャット・Issueに貼らない**）→ APIを再起動 → 管理画面/APIで通知種別ごとにチャンネルIDを設定 → `POST /admin/notifications/test`で接続テスト。tokenが無い間・チャンネル未設定の間は送らず溜まる（設定後に送られる）。
- 確認: `GET /admin/notifications/deliveries?status=pending|dead`。`dead`は恒久的な拒否（権限・チャンネルの誤り）か再試行の上限。原因を直してから`POST /admin/notifications/deliveries/{id}/retry`で再送する。
- 重複: 障害の切り替わりで同じ通知が2回届くことがある（exactly-onceは保証しない）。本文の「通知ID」で同一かを確認できる。
- tokenを漏らした疑いがあるときは、Discordの開発者ポータルでtokenを再生成し、`.env`を差し替えてAPIを再起動する。ログ・DB・APIの応答にtokenは出ない設計（テストで確認）。

## 公式サイトの動的配信（P3-04b）

- 公式サイト（`apps/web`）は、コンテンツを公開API（`API_INTERNAL_URL`。サーバー側のみ）から**描画時に取得**する（Next standalone。キャッシュしない）。撤回・個人情報削除（D08）は次のリクエストから反映される。**APIが落ちている間、コンテンツを使うページ（トップ含む）は5xx・`no-store`になる**（静的配信ではなくなった代償）。`/healthz/`はAPIに依存せず200を返す（コンテナの死活監視に使い、APIの障害でwebを再起動させない）。P4-01の死活監視には、API・webの両方のhealthzと、`/blog/`が200であることの監視を含める。
- **プロキシ・CDNの要件（P4-01）**: HTMLと公開JSON（`/api/v1/public/contents*`）は**キャッシュしない**（秒単位のmicrocacheも不可）。アップロード画像は`/api/v1/public/media/<22文字>`の**GET・HEADだけ**をAPIへ中継し（`Cookie`・`Authorization`を外す・`proxy_cache`を使わない・`proxy_pass`はURIなし）、それ以外の`/api/v1/*`はサイトのホストから出さない。
- **切り戻しは、API接続版の前のイメージへ**。静的イメージ（`content/*.md`の原文）へは戻さない（CMSで公開停止・修正した後に戻すと、消した内容が再び公開されるため）。
- **検索エンジン**: 作品ページは`noarchive`。撤回・削除の依頼で検索結果に残る場合は、各検索エンジンの削除依頼（URL削除ツール）を使う。
- 末尾スラッシュ: 標準のとおり、スラッシュ無しはスラッシュ付きへ308。ドットを含むslug（例: `/blog/nu.metasou/`）はNextがファイル扱いにして、スラッシュ無しのURLへ308する（内容は同じ）。
- 検査（CI: `web / lint-typecheck-build`）: 擬似の公開APIと組み合わせて起動し、全ルート・記事・画像、ブラウザー操作、移設前基準との**指紋の比較**（意図した差は`docs/legacy-baseline/known-diffs.json`に理由つき。投稿者名を伏せたDOMのハッシュは基準と完全一致）、APIが使えないときの5xx・`no-store`を確認する。

### アバター・物品の写真の運用（P3-02b）

- アバター・物品の写真は、WebPで`MEDIA_DIR`に保存される（cmsの画像と同じ。バックアップも同じ1組）。配信はログイン中のメンバーだけ（`GET /api/v1/media/{id}`。`Cache-Control: private, no-store`）。**公開サイトのNginxでは`/api/v1/media/`（`public`以外）を外へ出さない**。
- 差し替え・解除で外れた画像、個人情報の削除（D08）でアバターを外した画像は、参照が無くなるので`nuctl media-gc --apply`（24時間後）がファイルごと消す。**急ぐ場合**（本人の依頼で顔写真を直ちに消す等）は`nuctl media-redact --id <uuid> --reason "<理由>"`。

### 個人情報の削除の依頼があったとき（D08・P3-01c）

1. 管理画面（ユーザー管理 →「公開版の本人情報の確認」）で、本人の名前・ハンドルなどの語を探し、公開中の記事・作品の本文・題名・役割名に残っていないかを確認する（探した語は記録に残らない）。
2. 見つかった記事を編集して新しい版を公開する（過去の版は公開されないが書き換えられない）。
3. ユーザーを停止してから、個人情報の削除（`DELETE /admin/users/{id}/private-profile`）を実行する。公開クレジットの同意撤回・アバターの取り外し・紐付く既存記事の投稿者名の匿名化が連動する。
4. アバターなどの画像を直ちに消す場合は`nuctl media-redact`。それ以外は`nuctl media-gc`（24時間後）。
5. 公式サイトは次のリクエストから反映される（キャッシュしない）。検索エンジンに残る場合は、各検索エンジンの削除依頼を使う。
