# nu_app DB設計 v0.1（論理モデル案）

対象: PostgreSQL。主キーは原則 UUID、日時は `timestamptz`、全テーブルの外部キーとCHECK制約をマイグレーションで管理。以下は**設計案**であり、確定済みのDDLではない。

## 1. 認証・権限

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `users` | `id`, `login_id`, `status`, `created_at`, `disabled_at` | `login_id` UNIQUE、status = invited / active / disabled。ユーザーIDは変更しない |
| `password_credentials` | `user_id`, `password_hash`, `updated_at` | 1ユーザー1行。平文保存禁止 |
| `sessions` | `id`, `user_id`, `token_hash`, `expires_at`, `revoked_at` | 長期セッションの停止・一括失効対応 |
| `one_time_tokens` | `id`, `user_id`, `purpose`, `token_hash`, `expires_at`, `used_at`, `created_by` | purpose = invite / password_reset。平文tokenをDBに保存しない |
| `user_roles` | `user_id`, `role`, `assigned_by`, `assigned_at` | 複合UNIQUE(user_id,role)。role = owner/admin/editor。Memberはactive userの基本権限 |
| `owner_transfer_requests` | `id`, `from_user_id`, `to_user_id`, `status`, `expires_at` | Owner移譲の二段階フロー用。要最終確認 |

`Owner`と`Admin`の階層はアプリ側の認可ポリシーで統一。DB上の`owner`行だけでAdmin権限を付与可能とする（Admin行を二重作成する必要はない）。Ownerが0になる更新はサービス層・トランザクションで禁止。

## 2. メンバー・プロフィール

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `profiles` | `user_id`, `display_name`, `bio`, `avatar_media_id`, `faculty`, `department`, `school_year`, `updated_at` | 表示名は重複可。本人編集 |
| `personal_profiles` | `user_id`, `legal_name`, `student_number`, `university_email`, `nearest_station` | APIからデフォルト返却禁止。本人・権限あるAdmin/Ownerのみ |
| `profile_field_visibility` | `user_id`, `field_key`, `visibility` | UNIQUE(user_id,field_key)、visibility=private/members。個人情報の一部はprivate固定 |
| `profile_links` | `id`, `user_id`, `label`, `url`, `sort_order`, `visibility` | GitHub/ポートフォリオ等。本人管理 |
| `personal_devices` | `id`, `user_id`, `category`, `name`, `specs_json`, `visibility` | 個人所有機材。団体物品とは別 |

公開プロフィールのレスポンスは各項目の公開設定に応じて組み立てる。表示名・アバター以外は初期値非公開を推奨。学生証番号・メール・最寄り駅は一般メンバーに公開するトグル自体を設けない。公開作品でのクレジット同意はプロフィールのメンバー向け公開設定と分離する。

## 3. 物品・貸出

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `inventory_items` | `id`, `name`, `description`, `category`, `location`, `cover_media_id`, `mode`, `total_quantity`, `archived_at` | mode=quantity/individual。数量管理時`total_quantity >= 0` |
| `inventory_units` | `id`, `item_id`, `asset_tag`, `serial_number`, `state`, `archived_at` | 個体管理のみ。管理番号は同一物品内で一意 |
| `inventory_loans` | `id`, `item_id`, `unit_id`, `borrower_user_id`, `quantity`, `borrowed_at` | 数量管理時 unit_id NULL。個体管理時 quantity=1 |
| `inventory_returns` | `id`, `loan_id`, `actor_user_id`, `quantity`, `returned_at` | 返却者は借りた人と異なってよい。partial return可 |
| `inventory_adjustments` | `id`, `item_id`, `actor_user_id`, `before_quantity`, `after_quantity`, `reason`, `created_at` | 管理数量の変更や訂正履歴 |
| `inventory_corrections` | `id`, `target_type`, `target_id`, `actor_user_id`, `reason`, `before_json`, `after_json`, `created_at` | 監査用。個人情報は含めない |

数量管理の残数 = 総数 − Σ(貸出数量 − Σ返却数量)。
`remaining_quantity`を別列として保持する場合はDBトランザクションで厳密同期し、二重更新を防ぐ。最初は導出値としてもよい。個体管理では、未返却の単一個体貸出を許さない部分ユニーク制約に相当する仕組み（状態テーブル又はDB整合性制約）を実装する。

**注意:** 個体管理の管理番号は別カラムで保存する。物品一覧の写真やカテゴリは`inventory_items`に統合し、個体ごとの画像は必要になるまで増やさない。

## 4. CMS・作品

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `contents` | `id`, `type`, `section`, `slug`, `creator_user_id`, `working_revision_id`, `published_revision_id`, `archived_at` | type=article/project。section=blog/news/project等。section+slug UNIQUE |
| `content_revisions` | `id`, `content_id`, `revision_no`, `title`, `summary`, `markdown`, `cover_media_id`, `metadata_json`, `edited_by`, `created_at` | 保存ごとに不変の版。content_id+revision_no UNIQUE |
| `content_collaborators` | `content_id`, `user_id`, `added_by`, `added_at` | 記事作成者が指定。Editor/Adminは指定不要で全件編集可 |
| `work_contributors` | `id`, `content_id`, `user_id`, `role_label`, `sort_order`, `allow_public_credit` | 作品typeのみ。公開名表示は本人の許可が必要 |
| `content_publication_events` | `id`, `content_id`, `revision_id`, `actor_user_id`, `event_type`, `created_at` | publish/unpublish履歴 |

`working_revision_id` / `published_revision_id` は同一 `content_id` に属することをサービス層・可能ならDB制約/triggerで検証。公開前は `published_revision_id IS NULL`。公開記事を書き換えても公開ポインタは変えない。

旧URLを保つため、現行サイトのMarkdownパスを `section`+`slug` に移す。現行のfrontmatterにある`category=Blog/Product`, `date`（部分日付を含む）, `author`, `image`, `excerpt`, `sourceId`を損なわず保存できるよう`metadata_json`の移行マッピングを設計する。

## 5. メディア・通知・監査

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `media_assets` | `id`, `storage_key`, `visibility`, `mime_type`, `byte_size`, `width`, `height`, `uploaded_by`, `created_at` | private/public。保存先への絶対パスはAPIで返さない |
| `media_references` | `media_id`, `owner_type`, `owner_id`, `purpose` | 公開済み画像の参照管理や安全な削除用 |
| `notification_settings` | `event_type`, `enabled`, `discord_channel_id`, `updated_by` | Adminが変更可能 |
| `notification_outbox` | `id`, `event_type`, `payload_json`, `status`, `attempts`, `next_attempt_at`, `created_at`, `sent_at` | DB処理と同一トランザクションで作成 |
| `audit_logs` | `id`, `actor_user_id`, `action`, `target_type`, `target_id`, `summary_json`, `created_at` | 追記専用。トークン・パスワード・学生証番号の値を保存しない |
| `dashboard_events` | `id`, `event_type`, `title`, `target_url`, `created_at` | 公開作品やアプリ更新を自動表示 |

## 6. 関係と不変条件

```text
users ──1:1── profiles ──1:N── profile_links
  │             └────1:N── personal_devices
  ├──1:N── sessions
  ├──1:N── user_roles
  ├──1:N── inventory_loans ──1:N── inventory_returns
  └──1:N── contents ──1:N── content_revisions
                            ├── published_revision_id
                            └── working_revision_id
contents ──N:N── users (content_collaborators)
contents(type=project) ──N:N── users (work_contributors, role_label)
inventory_items ──1:N── inventory_units
inventory_items ──1:N── inventory_loans
```

- どのuser関連の行も`login_id`や`display_name`を外部キーにしてはいけない。
- アカウント停止時、履歴の`user_id`は残す。個人情報手動削除時も履歴の参照を維持。
- DBを変更する全APIではトランザクションの範囲と一意性制約を明示。
- 削除操作の監査ログは個人情報の内容をコピーしない。

## 7. 実装前に最小限決める内容

- 住所や駅などの取り扱いの最小化/必須項目。
- 既存のHTML/Markdown記事で扱う埋め込みメディアの制約。
- `user_id`を退会者の制作クレジットにどのように表示するか。
- 管理者のみが操作する訂正APIの厳密な契約。
