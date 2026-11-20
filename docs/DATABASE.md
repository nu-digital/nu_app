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
| `owner_transfer_requests` | `id`, `from_user_id`, `to_user_id`, `status`, `requested_at`, `expires_at`, `resolved_at`, `resolved_by` | D05。status=pending/accepted/cancelled/expired。`expires_at`は申請から24時間。from_user単位でpendingは1件（部分UNIQUE）。承諾は行ロック+単一トランザクションでto_userにowner付与しfrom_userをadminにする。失効は承諾時にも`expires_at`で判定（ジョブ遅延に依存しない） |
| `emergency_recovery_events` | `id`, `executed_by_os_user`, `target_user_id`, `approver_note`, `created_at` | 管理CLI専用の追記専用監査。個人情報・秘密値を保存しない。実行時に対象のセッションを全失効 |

`Owner`と`Admin`の階層はアプリ側の認可ポリシーで統一。DB上の`owner`行だけでAdmin権限を付与可能とする（Admin行を二重作成する必要はない）。Ownerが0になる更新はサービス層・トランザクションで禁止。

## 2. メンバー・プロフィール

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `profiles` | `user_id`, `display_name`, `bio`, `avatar_media_id`, `faculty`, `department`, `school_year`, `updated_at` | **本人入力で必須なのは`display_name`のみ**（他はNULL可）。表示名は重複可。本人編集。`login_id`は`users`側で管理者が管理 |
| `personal_profiles` | `user_id`, `legal_name`, `student_number`, `university_email`, `nearest_station` | 全列NULL可（任意入力）。APIからデフォルト返却禁止。本人・権限あるAdmin/Ownerのみ。公開範囲の設定項目を持たない |
| `profile_field_visibility` | `user_id`, `field_key`, `visibility` | UNIQUE(user_id,field_key)、visibility=private/members（**メンバー内公開**のみ）。行が無い場合はprivate（初期値非公開）。個人情報4項目はprivate固定でトグルなし |
| `public_credit_consents` | `user_id`, `granted_at`, `withdrawn_at`, `updated_by` | **インターネット公開**の同意。メンバー内公開とは別。有効 = `granted_at IS NOT NULL AND withdrawn_at IS NULL`。変更履歴は`audit_logs`にも記録（氏名の値は保存しない） |
| `profile_links` | `id`, `user_id`, `label`, `url`, `sort_order`, `visibility` | GitHub/ポートフォリオ等。本人管理 |
| `personal_devices` | `id`, `user_id`, `category`, `name`, `specs_json`, `visibility` | 個人所有機材。団体物品とは別 |

公開プロフィールのレスポンスは各項目の公開設定に応じて組み立てる。表示名・アバターはログイン済みメンバーへ表示。それ以外は初期値非公開。氏名・大学メール・学生証番号・最寄り駅は一般メンバーに公開するトグル自体を設けない。インターネット向けの公開クレジットは`public_credit_consents`で、メンバー向け公開設定とは別に扱う。

## 3. 物品・貸出

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `inventory_items` | `id`, `name`, `description`, `category`, `location`, `cover_media_id`, `mode`, `total_quantity`, `archived_at` | mode=quantity/individual。数量管理時`total_quantity >= 0` |
| `inventory_units` | `id`, `item_id`, `asset_tag`, `serial_number`, `state`, `archived_at` | 個体管理のみ。管理番号は同一物品内で一意 |
| `inventory_loans` | `id`, `item_id`, `unit_id`, `borrower_user_id`, `quantity`, `borrowed_at` | 数量管理時 unit_id NULL。個体管理時 quantity=1 |
| `inventory_returns` | `id`, `loan_id`, `actor_user_id`, `quantity`, `returned_at` | 返却者は借りた人と異なってよい。partial return可 |
| `inventory_adjustments` | `id`, `item_id`, `actor_user_id`, `before_quantity`, `after_quantity`, `reason`, `created_at` | 管理数量の変更や訂正履歴 |
| `inventory_correction_events` | `id`, `item_id`, `target_type`, `target_id`, `kind`, `delta_quantity`, `reason`, `actor_user_id`, `idempotency_key`, `created_at` | D09。追記専用（UPDATE/DELETE禁止）。kind=quantity_adjust/void。`reason`NOT NULL・空文字不可。UNIQUE(`idempotency_key`)で重複適用を防ぐ。元の`inventory_loans/returns`の操作者・日時は不変。訂正者・訂正日時は本テーブルに記録。個人情報は含めない |

数量管理の残数 = 総数 − Σ(有効貸出数量 − Σ有効返却数量)。有効数量は元記録に訂正イベントを加味した導出値（元行は更新しない）。訂正は残数が負・返却数が貸出数超過になる場合は拒否し、対象物品行を`FOR UPDATE`でロックした同一トランザクションで検証する。
`remaining_quantity`を別列として保持する場合はDBトランザクションで厳密同期し、二重更新を防ぐ。最初は導出値としてもよい。個体管理では、未返却の単一個体貸出を許さない部分ユニーク制約に相当する仕組み（状態テーブル又はDB整合性制約）を実装する。

**注意:** 個体管理の管理番号は別カラムで保存する。物品一覧の写真やカテゴリは`inventory_items`に統合し、個体ごとの画像は必要になるまで増やさない。

## 4. CMS・作品

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `contents` | `id`, `type`, `section`, `slug`, `creator_user_id`, `working_revision_id`, `published_revision_id`, `archived_at` | type=article/project。section=blog/news/project等。section+slug UNIQUE |
| `content_revisions` | `id`, `content_id`, `revision_no`, `title`, `summary`, `markdown`, `cover_media_id`, `metadata_json`, `edited_by`, `created_at` | 保存ごとに不変の版。content_id+revision_no UNIQUE |
| `content_collaborators` | `content_id`, `user_id`, `added_by`, `added_at` | 記事作成者が指定。Editor/Adminは指定不要で全件編集可 |
| `work_contributors` | `id`, `content_id`, `user_id`, `role_label`, `sort_order`, `added_by` | 作品typeのみ。**制作メンバー登録のみ**（編集権限者が操作）。公開クレジット可否は`public_credit_consents`（本人のみ変更）で別管理。名前はrevisionに焼き込まず配信時に解決 |
| `content_publication_events` | `id`, `content_id`, `revision_id`, `actor_user_id`, `event_type`, `created_at` | publish/unpublish履歴 |

`working_revision_id` / `published_revision_id` は同一 `content_id` に属することをサービス層・可能ならDB制約/triggerで検証。公開前は `published_revision_id IS NULL`。公開記事を書き換えても公開ポインタは変えない。

旧URLを保つため、現行サイトのMarkdownパスを `section`+`slug` に移す（大文字・ドット入りslugは変更しない）。現行のfrontmatterにある`category=Blog/Product`, `date`, `author`, `image`, `excerpt`, `sourceId`を損なわず保存する。

**日付（D12）:** `content_revisions`に`date_label`(原文。表示は`formatArticleDate`相当)、`date_kind`(day/month/range/unknown)、`sort_date`(DATE。unknownはNULL可)、`sort_date_is_estimated`(bool)を持つ。補った`sort_date`を実際の開催日として表示しない。

**公開クレジットの匿名化（D08）:** 公開APIは`work_contributors`と`public_credit_consents`を配信時に結合して名前を出す。同意撤回・個人情報削除後は、過去の公開版・古いrevisionを配信しても「退会メンバー」等になる（revision本文に名前を保存しない）。撤回時に公開キャッシュを無効化する。自由記述本文・画像内の本人情報は自動匿名化できないため、管理者の確認対象として別途一覧化する。

## 5. メディア・通知・監査

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `media_assets` | `id`, `storage_key`, `visibility`, `mime_type`, `byte_size`, `width`, `height`, `purpose`, `uploaded_by`, `created_at` | private/public。mime_typeはimage/jpeg・png・webpのみ（CHECK）。purpose=avatar/inventory/cms。保存先への絶対パスはAPIで返さない。保存は再エンコード後の値（元ファイルのEXIFを保持しない） |
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

## 7. 確定済みの事項と、実装時に決めること

D01〜D13は[DECISIONS](DECISIONS.md)で確定済み。実装時に決める詳細:

- 各テーブルの具体的な型・索引・CHECK（DDLはP1以降のmigrationで確定）。
- `inventory_correction_events`から有効数量を求める集計方法（ビューか導出クエリ）。
- バックアップのマニフェストに含めるスキーマ版の持ち方（[OPERATIONS](OPERATIONS.md)）。
- 既存記事の`metadata_json`移行マッピング（P3-04）。
