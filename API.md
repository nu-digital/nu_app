# nu_app REST API草案 v0.1

Base: `https://app.nu-digital.jp/api/v1`。公開サイトは内部ネットワーク経由で公開コンテンツAPIを呼び出す。リクエスト/レスポンスは原則JSON。DBやAPIモデルを直接返さず、用途別にレスポンスを整形。

認可表記: `Public`=匿名, `Member`=認証済み有効ユーザー, `Editor`, `Admin`, `Owner` は各ロールを有するユーザー。権限はサーバーで毎回検証。OwnerはAdminの機能を実行可能。

## 1. 認証・アカウント

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| POST | `/auth/login` | Public | loginId/passwordでセッション開始 |
| POST | `/auth/logout` | Member | 現在のセッション失効 |
| GET | `/auth/me` | Member | 自分のID/ロール/状態 |
| POST | `/auth/accept-invite` | Public + token | 招待URL tokenを検証して初期パスワード設定 |
| POST | `/auth/reset-password` | Public + token | 再設定tokenの検証とパスワード更新、既存セッション失効 |
| POST | `/admin/users` | Admin | loginIdと最小項目を指定してアカウント作成 |
| POST | `/admin/users/{id}/invite-link` | Admin | 一度だけ表示する招待URLを発行 |
| POST | `/admin/users/{id}/reset-link` | Admin | 本人確認後に再設定URLを発行 |
| PATCH | `/admin/users/{id}/status` | Admin | 停止・再有効化 |
| PUT | `/admin/users/{id}/editor-role` | Admin | Editorの付与/解除 |
| PUT | `/owner/users/{id}/admin-role` | Owner | Adminの付与/解除。最後のOwnerを消せない |
| POST | `/owner/transfers` | Owner | Owner移譲を申し出る（案） |
| POST | `/owner/transfers/{id}/accept` | 対象ユーザー | Owner移譲の承諾（案） |

**注意:** リセット申請そのものを匿名APIで受け付ける必要はない。本人は運営者へ連絡し、AdminがURLを発行する。初期Ownerの作成は通常公開APIに設けずCLIで行う。

## 2. メンバー・プロフィール

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| GET | `/members` | Member | 公開設定済み項目のみ返すメンバー一覧 |
| GET | `/members/{id}` | Member | 公開設定済みプロフィール、作品参加実績 |
| GET | `/me/profile` | Member | 自分の全登録プロフィール |
| PATCH | `/me/profile` | Member | 表示名、自己紹介、学部等の編集 |
| PUT | `/me/profile/visibility` | Member | 項目ごとの公開範囲を変更 |
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
| POST | `/admin/inventory/corrections` | Admin | 誤操作修正と監査イベント |

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
| GET | `/contents/{id}` | 作者 / 共同編集者 / Editor/Admin | 下書き・編集履歴取得 |
| POST | `/contents/{id}/revisions` | 作者 / 共同編集者 / Editor/Admin | baseRevision指定で新しい版を保存 |
| PUT | `/contents/{id}/collaborators` | 作者 / Editor/Admin | 共同編集者を更新 |
| PUT | `/contents/{id}/contributors` | 作者 / Editor/Admin（暫定） | 作品の制作メンバーと担当役割を更新 |
| POST | `/contents/{id}/publish` | Editor/Admin | working revisionを公開版にする |
| POST | `/contents/{id}/unpublish` | Editor/Admin | 公開停止 |
| GET | `/public/contents` | Public | 公開記事・作品一覧（下書きは除外） |
| GET | `/public/contents/{section}/{slug}` | Public | 公開済みの版だけ返す |

編集保存例:

```json
{
  "baseRevision": 4,
  "title": "展示会に参加しました",
  "summary": "活動の紹介",
  "markdown": "## 展示内容\n...",
  "metadata": { "dateLabel": "2026.11", "category": "Blog" }
}
```

保存時点で公開サイトの内容は変わらない。公開操作はDBのpublished revision pointerを切り替え、画像公開処理と表示キャッシュ更新を実行。競合時は `409 Conflict` と現在のrevisionを返す。公開権限を持たないMemberがAPIを直接呼んでも `403 Forbidden`。

作品の下書き・公開権限は記事と同一にすることを暫定提案。ユーザーの最終確認前に強制確定しない。

## 5. メディア・通知・ダッシュボード

| Method | Path | 権限 | 動作 |
|---|---|---|---|
| POST | `/media` | Member | 指定用途の画像アップロード、非公開で保存 |
| GET | `/media/{id}` | 画像所有者・必要権限 | 非公開画像の認可付き配信 |
| GET | `/public/media/{opaqueId}` | Public | 公開されている画像のみ |
| GET | `/dashboard/feed` | Member | CMS公開・アプリ更新等のイベント |
| GET | `/admin/notifications/settings` | Admin | イベント種別と通知先チャンネル一覧 |
| PUT | `/admin/notifications/settings/{eventType}` | Admin | ON/OFF、チャンネルID変更 |
| POST | `/admin/notifications/test` | Admin | Bot接続・送信先設定のテスト |
| GET | `/admin/notifications/deliveries` | Admin | 通知送信失敗一覧（個人情報除外） |
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
