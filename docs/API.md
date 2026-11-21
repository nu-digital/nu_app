# nu_app REST API草案 v0.1

Base: `https://app.nu-digital.jp/api/v1`。公開サイトは内部ネットワーク経由で公開コンテンツAPIを呼び出す。リクエスト/レスポンスは原則JSON。DBやAPIモデルを直接返さず、用途別にレスポンスを整形。

認可の正本は[AUTHZ](AUTHZ.md)（`services/api/internal/authz`。全エンドポイントとの対応をテストで検査）。認可表記: `Public`=匿名, `Member`=認証済み有効ユーザー, `Editor`, `Admin`, `Owner` は各ロールを有するユーザー。権限はサーバーで毎回検証。OwnerはAdminの機能を実行可能。

## 1. 認証・アカウント

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| POST | `/auth/login` | Public | loginId/passwordでセッション開始 |
| POST | `/auth/logout` | Member | 現在のセッション失効 |
| GET | `/auth/me` | Member | 自分のID/ロール/状態 |
| POST | `/auth/accept-invite` | Public + token | 招待URL tokenを検証して初期パスワード設定 |
| POST | `/auth/reset-password` | Public + token | 再設定tokenの検証とパスワード更新、既存セッション失効 |
| GET | `/admin/users` | Admin | アカウント一覧（検索・ページング。個人情報・秘密値は返さない） |
| POST | `/admin/users` | Admin | loginIdと最小項目を指定してアカウント作成 |
| POST | `/admin/users/{id}/invite-link` | Admin | 一度だけ表示する招待URLを発行 |
| POST | `/admin/users/{id}/reset-link` | Admin | 本人確認後に再設定URLを発行 |
| PATCH | `/admin/users/{id}/status` | Admin | 停止・再有効化 |
| PUT | `/admin/users/{id}/editor-role` | Admin | Editorの付与/解除 |
| PUT | `/owner/users/{id}/admin-role` | Owner | Adminの付与/解除。最後のOwnerを消せない |
| POST | `/owner/transfers` | Owner | Owner移譲を申請（移譲先は有効ユーザー。有効期限24時間。pendingは申請者につき1件） |
| POST | `/owner/transfers/{id}/cancel` | 申請したOwner | 承諾前の取消 |
| GET | `/owner/transfers/incoming` | Member | 自分宛のpending申請の確認 |
| POST | `/owner/transfers/{id}/accept` | 移譲先ユーザー本人 | 承諾。単一トランザクションでOwner付与・旧OwnerをAdminへ。期限切れ/取消済みは409 |

**P1-02で実装済みの契約（暫定仕様を含む。[認証設計](design/p1-02-auth.md)）**

| エンドポイント | 要求 | 成功応答 | 主なエラー |
|---|---|---|---|
| `POST /auth/login` | `{loginId,password}` | 200 `{user:{id,loginId,displayName,roles},csrfToken,sessionExpiresAt}` + Cookie | 401 `INVALID_CREDENTIALS`（理由を区別しない）、429 `RATE_LIMITED`（`Retry-After`）、503 `SERVICE_BUSY` |
| `POST /auth/logout` | – | 204（Cookie削除） | 401、403 `CSRF_FAILED` |
| `GET /auth/me` | – | 200（loginと同形式） | 401 |
| `POST /auth/accept-invite` | `{token,password}` | 204（自動ログインしない） | 400 `INVALID_LINK`（無効/期限切れ/使用済み/失効を区別しない）、422 `WEAK_PASSWORD` |
| `POST /auth/reset-password` | `{token,password}` | 204（全セッション失効） | 同上 |
| `GET /admin/users` | クエリ `q`（ログインID・表示名の部分一致）, `limit`（1〜100、既定50）, `cursor` | 200 `{items:[{id,loginId,displayName,status,roles,createdAt,activatedAt}],nextCursor}`（Admin/Owner。個人情報・秘密値は含まない） | 401、403、400 |
| `GET /me/profile` | – | 200 自分の全項目（表示名・自己紹介・学部・学科・学年・メンバー内公開の設定・**個人情報4項目（本人のみ）**・外部リンク・個人機材・公開クレジット同意） | 401 |
| `PATCH /me/profile` | 任意項目の部分更新（`displayName`・`bio`・`faculty`・`department`・`schoolYear`・`legalName`・`studentNumber`・`universityEmail`・`nearestStation`。`null`か空で任意項目を消去、表示名は消せない） | 200（GET /me/profileと同形式） | 422 |
| `PUT /me/profile/visibility` | `{bio\|faculty\|department\|schoolYear: "private"\|"members"}`（既定はprivate。個人情報4項目は指定不可） | 200 | 422 |
| `GET/PUT /me/profile/public-credit-consent` | PUT `{granted:bool}`（**メンバー内公開とは別の、インターネット公開の同意**） | 200 `{granted,grantedAt,withdrawnAt}` | 422 |
| `POST/PATCH/DELETE /me/profile/links` | `{label,url(http/https),sortOrder?,visibility?}`（最大10件） | 201/200/204 | 409 `LIMIT_REACHED`、422、404（他人の項目） |
| `POST/PATCH/DELETE /me/devices` | `{category,name,spec?,sortOrder?,visibility?}`（最大20件） | 201/200/204 | 同上 |
| `GET /members` | `q`（表示名の部分一致）, `limit`, `cursor` | 200 `{items:[{id,displayName,bio,faculty,department,schoolYear}],nextCursor}`（**有効なメンバーだけ**。本人が「メンバー内公開」にした項目だけ値が入り、他はnull。ログインID・個人情報・同意状態は返さない） | 401、400 |
| `GET /members/{id}` | – | 200 上記＋`links`・`devices`（メンバー内公開のみ） | 404（無効・招待中・存在しない・不正なID） |
| `GET /admin/users/{id}/private-profile` | – | 200 `{id,loginId,displayName,private:{legalName,studentNumber,universityEmail,nearestStation}}`（Admin/Owner。**閲覧を監査に記録**、値は記録しない） | 403、404 |
| `DELETE /admin/users/{id}/private-profile` | `{reason(必須・200字以内),anonymizeDisplayName?}` | 200 `{consentWithdrawn,displayNameAnonymized,linksDeleted,devicesDeleted}`（個人情報・プロフィール項目・リンク・機材・公開設定を削除、公開クレジット同意を撤回） | 409 `NOT_DISABLED`（**停止中のユーザーだけ**）、422、404 |
| `GET /inventory/items` | `q`（名前・カテゴリ・保管場所・説明）, `category`, `available=true`, `limit`, `cursor` | 200 `{items:[{id,name,description,category,location,mode,totalQuantity,outstanding,unitCount,available}],nextCursor,categories}`（廃止した物品は含まない。`available`=数量管理は残数、個体管理は利用可能な個体数） | 400 |
| `GET /inventory/items/{id}` | – | 200 物品＋`units`（管理番号・状態・貸出中なら借りた人の表示名と経過日数）＋`loans`（数量管理の貸出中。未返却数・経過日数） | 404 |
| `POST /inventory/items`（Admin） | `{name,description?,category?,location?,mode:"quantity"\|"individual",totalQuantity?}` | 201 | 422 |
| `PATCH /inventory/items/{id}`（Admin） | `{name?,description?,category?,location?,mode?,totalQuantity?,reason?,archived?}`。総数の変更は`reason`必須・履歴に記録 | 200 | 409 `BELOW_OUTSTANDING`（貸出中の数量を下回る総数）・`HAS_OUTSTANDING`（貸出中の廃止）・`MODE_LOCKED`（貸出履歴/個体がある物品の方式変更） |
| `POST /inventory/items/{id}/units`（Admin） | `{assetTag,serialNumber?}`（個体管理のみ） | 201 | 409 `TAG_TAKEN`・`WRONG_MODE`・`ARCHIVED` |
| `POST /inventory/loans` | 数量管理 `{itemId,quantity}`／個体管理 `{itemId,unitId}`（借りた本人はセッションのユーザー。指定項目なし） | 201 `{id,itemId,unitId,quantity,borrowedAt,remaining}` | 409 `INSUFFICIENT_STOCK`・`UNIT_UNAVAILABLE`・`ARCHIVED`、422 |
| `POST /inventory/loans/{id}/returns` | `{quantity?}`（省略=残りをすべて。**誰でも他人の貸出を返却できる**＝代理返却。部分返却可） | 200 `{loanId,returned,outstanding}` | 409 `EXCEEDS_OUTSTANDING`（未返却数を超える）・`ALREADY_RETURNED`（二重返却）、404 |
| `GET /inventory/loans` | `q`（物品名・借りた人の表示名）, `mine=true`, `itemId`, `limit`, `cursor` | 200 `{items:[{id,itemId,itemName,assetTag,borrowerDisplayName,quantity(未返却),borrowedAt,elapsedDays,mine}],nextCursor}`（古い順） | 400 |
| `GET /inventory/history` | `itemId?`, `limit`, `cursor` | 200 `{items:[{id,kind:"loan"\|"return",itemId,itemName,assetTag,borrowerDisplayName,actorDisplayName,quantity,at}],nextCursor}`（新しい順。返却では**借りた人と返却した人が別に分かる**） | 400 |
| `POST /admin/inventory/corrections`（Admin/Owner） | ヘッダー`Idempotency-Key`（必須）、`{targetType:"loan"\|"return",targetId,kind:"adjust"\|"void",deltaQuantity?,reason}`。元記録は変更せず訂正イベントを追加し、有効数量へ反映（個体管理の貸出は`void`のみ） | 201 `{id,...,replayed:false,itemOutstanding,itemAvailable}`／同じキー・同じ内容の再送は200（`replayed:true`・二重適用なし） | 409 `CORRECTION_INVALID`（在庫が負・返却数が貸出数超過・数量が1未満）・`ALREADY_VOIDED`・`UNIT_UNAVAILABLE`、422 `IDEMPOTENCY_KEY_REUSED`（同じキーで内容が違う）・入力不正、400（キー不備）、403 |
| `POST /admin/users` | `{loginId,displayName}` | 201 `{user,url,expiresAt}`（`url`=`<APP_BASE_URL>/invite#token=…`、一度だけ） | 409 `LOGIN_ID_TAKEN`、422 |
| `POST /admin/users/{id}/invite-link` | – | 200 `{url,expiresAt}`（invitedのみ） | 409 `INVALID_STATE`・`CONFLICT` |
| `POST /admin/users/{id}/reset-link` | – | 200 `{url,expiresAt}`（activeのみ。管理者対象はOwnerのみ） | 403、409 |
| `PATCH /admin/users/{id}/status` | `{status:"disabled"\|"active"}` | 200 `{id,loginId,status}` | 409 `CANNOT_DISABLE_SELF`・`LAST_OWNER`、403 |
| `PUT /admin/users/{id}/editor-role` | `{enabled:bool}` | 200 `{id,loginId,roles}`（冪等。Admin/Owner） | 403、404、422 |
| `PUT /owner/users/{id}/admin-role` | `{enabled:bool}` | 200 `{id,loginId,roles}`（冪等。Ownerのみ。Owner権限は付与できない） | 403、404、422 |
| `POST /owner/transfers` | `{toUserId}` | 201 `{id,toUserId,status:"pending",expiresAt}`（有効期限24時間） | 403、409 `TRANSFER_PENDING`、422 `INVALID_TARGET`（自分・非active・既にOwner・存在しない） |
| `POST /owner/transfers/{id}/cancel` | – | 200（申請者のみ。承諾前） | 404（申請者以外）、409 `TRANSFER_NOT_PENDING`（処理済み/期限切れ） |
| `GET /owner/transfers/incoming` | – | 200 `{items:[{id,fromLoginId,fromDisplayName,expiresAt}]}`（自分宛の未処理・未失効） | 401 |
| `POST /owner/transfers/{id}/accept` | – | 200 `{roles}`（移譲先本人のみ。1トランザクションで入替、旧Ownerはadmin） | 404（移譲先以外）、409 `TRANSFER_NOT_PENDING`・`TRANSFER_INVALID` |

状態変更は`Origin`（または`Referer`）が許可オリジンと一致し、認証済みの場合は`X-CSRF-Token`（`/auth/me`・ログイン応答の`csrfToken`）が必要。Cookieは`HttpOnly; Secure; SameSite=Lax`（Secure時は`__Host-nu_session`）。

**注意:** リセット申請そのものを匿名APIで受け付ける必要はない。本人は運営者へ連絡し、AdminがURLを発行する。初期Ownerの作成と**緊急復旧（D05）は公開APIに設けず管理CLIのみ**。CLIは運用責任者の本人確認と明示承認の下でVPS上で実行し、GitHub Actionsのデプロイ手順から呼べない。実行時に対象のセッションを全失効し監査に残す。最後のOwnerを失う操作は、API・CLIとも拒否する。

## 2. メンバー・プロフィール

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| GET | `/members` | Member | 公開設定済み項目のみ返すメンバー一覧 |
| GET | `/members/{id}` | Member | 公開設定済みプロフィール、作品参加実績 |
| GET | `/me/profile` | Member | 自分の全登録プロフィール |
| PATCH | `/me/profile` | Member | 表示名（必須。空にできない）、自己紹介、学部等（任意）の編集 |
| PUT | `/me/profile/visibility` | Member | 項目ごとのメンバー内公開(private/members)を変更。初期値private。個人情報4項目は対象外 |
| GET/PUT | `/me/profile/public-credit-consent` | Member | インターネット向け公開クレジットの同意の確認・付与・撤回（メンバー内公開とは別）。撤回で公開表示を匿名化 |
| POST | `/me/profile/links` | Member | 任意URL追加 |
| PATCH | `/me/profile/links/{id}` | Member | URL・名称・表示順・公開設定変更 |
| DELETE | `/me/profile/links/{id}` | Member | リンク削除 |
| POST | `/me/devices` | Member | 個人所有機材を追加 |
| PATCH | `/me/devices/{id}` | Member | 機材情報を編集 |
| DELETE | `/me/devices/{id}` | Member | 個人所有機材を削除 |
| GET | `/admin/users/{id}/private-profile` | Admin | 個人情報を閲覧（監査対象） |
| DELETE | `/admin/users/{id}/private-profile` | Admin | 必要な個人情報を手動削除（確認・監査） |

公開一覧ではDBの非公開列は返さない。公開許可のない機材スペック・外部リンクも除外する。本人も他人のprivate項目を取得できない。

## 3. 物品管理

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| GET | `/inventory/items` | Member | 一覧・検索・カテゴリ・利用可否 |
| GET | `/inventory/items/{id}` | Member | 詳細、個体状況、残数、現在の貸出状況 |
| POST | `/inventory/items` | Admin | 物品追加（mode/総数/写真/保管場所等） |
| PATCH | `/inventory/items/{id}` | Admin | 物品編集・数量調整（履歴保存） |
| POST | `/inventory/items/{id}/units` | Admin | 個体登録 |
| POST | `/inventory/loans` | Member | 物品を借りる。数量または個体IDを指定 |
| POST | `/inventory/loans/{id}/returns` | Member | 貸出レコードに返却数量を記録。代理返却可 |
| GET | `/inventory/loans` | Member | 貸出中の一覧・経過日数、検索 |
| GET | `/inventory/history` | Member | 物品の貸出・返却履歴 |
| POST | `/admin/inventory/corrections` | Admin / Owner | 貸出・返却数量の訂正、誤登録の取消。`reason`必須、`Idempotency-Key`ヘッダー必須。訂正イベントを追加（元記録は不変）。在庫が負・返却数>貸出数になる訂正は409。同じキーの再送は同じ結果を返し二重適用しない |

`POST /inventory/loans` (数量管理の例):

```json
{
  "itemId": "uuid-of-item",
  "quantity": 2
}
```

`POST /inventory/loans/{id}/returns` (代理返却の例):

```json
{
  "quantity": 1
}
```

actor/borrower はクライアント入力を信用せず、セッションのユーザーIDから設定。代理返却時も`actor_user_id`だけが現在の操作者になる。貸出数不足・二重返却は `409 Conflict`。貸出経過日数は借用日時から表示時点で計算し、返却期限は持たない。

## 4. CMS（記事・作品）

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| GET | `/contents` | Member | 下書き・公開済み一覧（閲覧できるもののみ） |
| POST | `/contents` | Member | 記事/作品の下書き作成 |
| GET | `/contents/{id}` | 作者 / 共同編集者 / Editor/Admin/Owner | 下書き・編集履歴取得 |
| POST | `/contents/{id}/revisions` | 作者 / 共同編集者 / Editor/Admin/Owner | baseRevision指定で新しい版を保存 |
| PUT | `/contents/{id}/collaborators` | 作者 / Editor/Admin/Owner | 共同編集者を更新 |
| PUT | `/contents/{id}/contributors` | 作者 / 共同編集者 / Editor/Admin/Owner | 作品の制作メンバーと担当役割の登録（公開クレジット同意は変更不可・別API） |
| POST | `/contents/{id}/publish` | Editor/Admin/Owner | working revisionを公開版にする |
| POST | `/contents/{id}/unpublish` | Editor/Admin/Owner | 公開停止 |
| GET | `/public/contents` | Public | 公開記事・作品一覧（下書きは除外） |
| GET | `/public/contents/{section}/{slug}` | Public | 公開済みの版だけ返す |

編集保存例:

```json
{
  "baseRevision": 4,
  "title": "展示会に参加しました",
  "summary": "活動の紹介",
  "markdown": "## 展示内容\n...",
  "metadata": { "category": "Blog" },
  "date": { "label": "2026年11月", "kind": "month", "sortDate": "2026-11-01" }
}
```

保存時点で公開サイトの内容は変わらない。公開操作はDBのpublished revision pointerを切り替え、画像公開処理と表示キャッシュ更新を実行。競合時は `409 Conflict` と現在のrevisionを返す。公開権限を持たないMemberがAPIを直接呼んでも `403 Forbidden`。

作品の下書き・公開権限は記事と同一（D01確定）。`date.kind`はday/month/range/unknown。`sortDate`は並び替え用で、`kind`がday以外のときは実際の日付として表示しない（`label`の原文を表示する）。

Markdownは許可リスト方式でサニタイズする。生のiframe/script/video HTMLは保存時に拒否または除去。埋め込みはYouTube URL（許可ホストと11桁動画ID）だけをアプリ側が動画IDからiframeを生成して表示し、他の動画サービスは通常の外部リンクとする（D10）。公開APIは公開クレジットを配信時に同意状態と結合し、撤回後は匿名表示にする（D08）。

## 5. メディア・通知・ダッシュボード

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| POST | `/media` | Member | 指定用途(`purpose`=avatar/inventory/cms)の画像アップロード、非公開で保存。JPEG/PNG/WebPのみ。avatar≤5MiB、他≤10MiB。デコード前に寸法を検査（≤2,000万画素・各辺≤10,000px）。実体検査・アニメーション/SVG拒否・向き補正・縮小(avatar長辺≤512px、他≤2,560px、拡大しない)・再エンコード・EXIF除去。同時処理数を制限し超過は429/503で再試行を案内。不正入力は400/413/415/422（TESTING参照） |
| GET | `/media/{id}` | 画像所有者・必要権限 | 非公開画像の認可付き配信 |
| GET | `/public/media/{opaqueId}` | Public | 公開されている画像のみ |
| GET | `/dashboard/feed` | Member | CMS公開・アプリ更新等のイベント |
| GET | `/admin/notifications/settings` | Admin | イベント種別と通知先チャンネル一覧 |
| PUT | `/admin/notifications/settings/{eventType}` | Admin | `{enabled, discordChannelId?}`。ON/OFFと送信チャンネル変更（数字5〜25桁。空/nullで未設定＝送信せず待機）。未知の種別は404、入力不正は422。監査ログに変更の事実だけ残す |
| POST | `/admin/notifications/test` | Admin | `{discordChannelId}`のチャンネルへ接続テストを1通送る。200 `{ok:true}`／Bot未設定は503 `BOT_NOT_CONFIGURED`、Discordの拒否は502 `DISCORD_REJECTED`、上限は429 `DISCORD_RATE_LIMITED`、通信失敗は502 `DISCORD_UNAVAILABLE`、入力不正は422。応答にDiscordの本文・tokenを含めない |
| GET | `/admin/notifications/deliveries` | Admin | 通知の送信状況一覧（`status`=pending/sent/dead、`limit`≤100、新しい順。**本文は返さない**。種別・状態・試行回数・次回試行・失敗の要約） |
| POST | `/admin/notifications/deliveries/{id}/retry` | Admin | 送信に失敗して止まった(dead)通知だけを再送待ちに戻す。204／dead以外は409 `NOT_RETRYABLE` |
| GET | `/healthz` | Infra | 単純なLiveness |
| GET | `/readyz` | Infra | DB依存を含めたReadiness、公開範囲制限 |

## 6. API共通契約

- ページング: `limit`, `cursor`。検索: `q`, `category`, `status`など必要最低限。
- JSONエラー例:

```json
{
  "error": {
    "code": "INVENTORY_UNAVAILABLE",
    "message": "貸出可能な数量が不足しています",
    "requestId": "opaque-request-id"
  }
}
```

- HTTP: 400不正入力、401未認証、403権限なし、404存在しない/存在を秘匿、409競合・在庫不足、422妥当性エラー、429レート制限、500内部エラー。
- DB更新APIには監査ログを書き、外部イベント通知は同一トランザクションでoutboxへ記録。
- 書き込みはCSRF対策、Origin検証、入力検証、長さ制限、適切なレート制限。
- メディア/認証token/機微プロフィールのレスポンスに`Cache-Control: no-store`を適用。
- APIスキーマの実装にはOpenAPIを推奨。エージェントはAPI契約の変更とテストを同じPRで行う。
