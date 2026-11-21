# 認可表（P1-01b）

認可判断はGo APIの`services/api/internal/authz`が行う。UIの非表示では代替しない。表の正本はコードで、`go test ./internal/authz`が**docs/API.mdの全エンドポイントとの1対1対応**と、この文書が全アクションを含むことを検査する。

## 共通ルール

- 既定は拒否。表に無いアクションは拒否する。
- 未認証は401相当、**停止（disabled）・招待中（invited）のアカウントは、ロールを持っていても公開アクション以外すべて拒否**する。
- Memberはactiveなユーザーの基本権限で、ロール行を持たない。Ownerは Admin の権限を内包する。AdminはEditorを内包しない（CMSの編集・公開は規則にAdmin/Ownerを明記）。
- 他人のリソース（作者でも共同編集者でもないコンテンツ、他人宛のOwner移譲など）は、関係が無ければEditor以上の権限が無い限り拒否する。
- **採用方針（権限昇格の防止）**: 管理者権限を持つユーザー（Admin/Owner）を対象にした停止・招待URL発行・再設定URL発行は**Ownerのみ**。AdminがOwnerやAdminの再設定URLを発行できると、アカウント乗っ取りによる権限昇格になるため。要件の「Adminはメンバーの発行・停止を担当」を、一般メンバー対象と解釈している（運用責任者が2026-11-21に承認。[DECISIONS](DECISIONS.md)）。
- `GET /members/{id}`・`GET /me/profile`などの**応答に含める項目の制限**（公開設定・個人情報）は認可表ではなくAPIのDTO組み立てで行う（`docs/ARCHITECTURE.md`）。

## アクション一覧（API.mdと同じ順）

| アクションID | Method | Path | 許可 |
|---|---|---|---|
| `auth.login` | POST | `/auth/login` | 誰でも（匿名可）。トークン検証はハンドラー側 |
| `auth.logout` | POST | `/auth/logout` | 有効なユーザー（Member以上） |
| `auth.me` | GET | `/auth/me` | 有効なユーザー（Member以上） |
| `auth.accept_invite` | POST | `/auth/accept-invite` | 誰でも（匿名可）。トークン検証はハンドラー側 |
| `auth.reset_password` | POST | `/auth/reset-password` | 誰でも（匿名可）。トークン検証はハンドラー側 |
| `admin.users.list` | GET | `/admin/users` | Admin / Owner（個人情報・秘密値は返さない） |
| `admin.users.create` | POST | `/admin/users` | Admin / Owner |
| `admin.users.invite_link` | POST | `/admin/users/{id}/invite-link` | Admin / Owner。**対象が管理者権限（Admin/Owner）のときはOwnerのみ** |
| `admin.users.reset_link` | POST | `/admin/users/{id}/reset-link` | Admin / Owner。**対象が管理者権限（Admin/Owner）のときはOwnerのみ** |
| `admin.users.status` | PATCH | `/admin/users/{id}/status` | Admin / Owner。**対象が管理者権限（Admin/Owner）のときはOwnerのみ** |
| `admin.users.editor_role` | PUT | `/admin/users/{id}/editor-role` | Admin / Owner |
| `owner.users.admin_role` | PUT | `/owner/users/{id}/admin-role` | Ownerのみ |
| `owner.transfers.create` | POST | `/owner/transfers` | Ownerのみ |
| `owner.transfers.cancel` | POST | `/owner/transfers/{id}/cancel` | その申請を出したOwner本人のみ |
| `owner.transfers.incoming` | GET | `/owner/transfers/incoming` | 有効なユーザー（Member以上） |
| `owner.transfers.accept` | POST | `/owner/transfers/{id}/accept` | 移譲先ユーザー本人のみ（Ownerでも他人宛は不可） |
| `members.list` | GET | `/members` | 有効なユーザー（Member以上） |
| `members.get` | GET | `/members/{id}` | 有効なユーザー（Member以上） |
| `me.profile.get` | GET | `/me/profile` | 有効なユーザー（Member以上） |
| `me.profile.update` | PATCH | `/me/profile` | 有効なユーザー（Member以上） |
| `me.profile.visibility` | PUT | `/me/profile/visibility` | 有効なユーザー（Member以上） |
| `me.profile.public_credit_consent` | GET/PUT | `/me/profile/public-credit-consent` | 有効なユーザー（Member以上） |
| `me.profile.links.create` | POST | `/me/profile/links` | 有効なユーザー（Member以上） |
| `me.profile.links.update` | PATCH | `/me/profile/links/{id}` | 有効なユーザー（Member以上） |
| `me.profile.links.delete` | DELETE | `/me/profile/links/{id}` | 有効なユーザー（Member以上） |
| `me.devices.create` | POST | `/me/devices` | 有効なユーザー（Member以上） |
| `me.devices.update` | PATCH | `/me/devices/{id}` | 有効なユーザー（Member以上） |
| `me.devices.delete` | DELETE | `/me/devices/{id}` | 有効なユーザー（Member以上） |
| `admin.users.private_profile.get` | GET | `/admin/users/{id}/private-profile` | Admin / Owner |
| `admin.users.private_profile.delete` | DELETE | `/admin/users/{id}/private-profile` | Admin / Owner |
| `admin.legacy_authors.list` | GET | `/admin/legacy-authors` | Admin / Owner |
| `admin.legacy_authors.permit` | POST | `/admin/legacy-authors/{id}/permit` | Admin / Owner |
| `admin.legacy_authors.withdraw` | POST | `/admin/legacy-authors/{id}/withdraw` | Admin / Owner |
| `admin.legacy_authors.link` | PUT | `/admin/legacy-authors/{id}/link` | Admin / Owner |
| `admin.legacy_authors.unlink` | DELETE | `/admin/legacy-authors/{id}/link` | Admin / Owner |
| `inventory.items.list` | GET | `/inventory/items` | 有効なユーザー（Member以上） |
| `inventory.items.get` | GET | `/inventory/items/{id}` | 有効なユーザー（Member以上） |
| `inventory.items.create` | POST | `/inventory/items` | Admin / Owner |
| `inventory.items.update` | PATCH | `/inventory/items/{id}` | Admin / Owner |
| `inventory.units.create` | POST | `/inventory/items/{id}/units` | Admin / Owner |
| `inventory.loans.create` | POST | `/inventory/loans` | 有効なユーザー（Member以上） |
| `inventory.loans.return` | POST | `/inventory/loans/{id}/returns` | 有効なユーザー（Member以上） |
| `inventory.loans.list` | GET | `/inventory/loans` | 有効なユーザー（Member以上） |
| `inventory.history` | GET | `/inventory/history` | 有効なユーザー（Member以上） |
| `inventory.corrections.create` | POST | `/admin/inventory/corrections` | Admin / Owner |
| `notifications.settings.list` | GET | `/admin/notifications/settings` | Admin / Owner |
| `notifications.settings.update` | PUT | `/admin/notifications/settings/{eventType}` | Admin / Owner |
| `notifications.deliveries.list` | GET | `/admin/notifications/deliveries` | Admin / Owner |
| `notifications.test` | POST | `/admin/notifications/test` | Admin / Owner |
| `notifications.deliveries.retry` | POST | `/admin/notifications/deliveries/{id}/retry` | Admin / Owner |
| `contents.list` | GET | `/contents` | 有効なユーザー（Member以上） |
| `contents.create` | POST | `/contents` | 有効なユーザー（Member以上） |
| `contents.get` | GET | `/contents/{id}` | 作者 / 共同編集者 / Editor / Admin / Owner |
| `contents.revisions.create` | POST | `/contents/{id}/revisions` | 作者 / 共同編集者 / Editor / Admin / Owner |
| `contents.collaborators.update` | PUT | `/contents/{id}/collaborators` | 作者 / Editor / Admin / Owner（共同編集者は不可） |
| `contents.contributors.update` | PUT | `/contents/{id}/contributors` | 作者 / 共同編集者 / Editor / Admin / Owner（公開クレジット同意は別API・本人のみ） |
| `contents.publish` | POST | `/contents/{id}/publish` | Editor / Admin / Owner（作者・共同編集者でも不可） |
| `contents.unpublish` | POST | `/contents/{id}/unpublish` | Editor / Admin / Owner（作者・共同編集者でも不可） |
| `public.contents.list` | GET | `/public/contents` | 誰でも（匿名可）。トークン検証はハンドラー側 |
| `public.contents.get` | GET | `/public/contents/{section}/{slug}` | 誰でも（匿名可）。トークン検証はハンドラー側 |
| `media.upload` | POST | `/media` | 有効なユーザー（Member以上） |
| `media.get` | GET | `/media/{id}` | アップロード者 / その画像を参照するコンテンツの作者・共同編集者 / Editor / Admin / Owner（それ以外は存在しないのと同じ404） |
| `media.get_by_public` | GET | `/media/by-public/{publicId}` | 同上（本文のURLからの下書きプレビュー用） |
| `public.media.get` | GET | `/public/media/{publicId}` | 誰でも（匿名可）。認証情報は見ない。**公開中の版が参照している画像だけ**をハンドラーが判定する（それ以外は同じ404） |
| `dashboard.feed` | GET | `/dashboard/feed` | 有効なユーザー（Member以上） |
| `admin.notifications.settings.get` | GET | `/admin/notifications/settings` | Admin / Owner |
| `admin.notifications.settings.update` | PUT | `/admin/notifications/settings/{eventType}` | Admin / Owner |
| `admin.notifications.test` | POST | `/admin/notifications/test` | Admin / Owner |
| `admin.notifications.deliveries` | GET | `/admin/notifications/deliveries` | Admin / Owner |

対象外: `GET /healthz`・`GET /readyz`（インフラ用。ネットワーク層で公開範囲を制限する）。

## 拒否の理由（ログ・テスト用の種別）

`unknown_action`（未定義）、`unauthenticated`（未認証）、`inactive_account`（停止・招待中）、`forbidden`（権限・関係の不足）、`privileged_target`（管理者権限を持つ対象への操作）。利用者向けの文言ではない。
