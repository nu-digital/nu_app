# nu_app DB設計（論理モデル）

状態: **実装済み**（2026-11-28に実装に合わせて更新）。**DDLの正本は `db/migrations/`**（goose・前進のみ・00001〜00021）。この文書は、各テーブルの役割と不変条件を説明する論理モデルで、細かな型・索引・CHECKはmigrationを見る。ぬるの`agent`スキーマは§8。

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
| `inventory_items` | `id`, `name`, `description`, `category`（名前。`category_id`と同期する旧クライアント向けの列）, `category_id`（NULL可・FK。00021）, `location`, `cover_media_id`（先頭の画像と同期）, `mode`, `total_quantity`, `archived_at` | mode=quantity/individual。数量管理時`total_quantity >= 0` |
| `inventory_categories` | `id`, `name`（UNIQUE・30文字以内）, `sort_order` | 管理者が追加・名称変更・並び替え・削除（00021）。初期は機材・書籍・その他。使用中（廃止した物品を含む）の削除は移し先を指定したときだけ。既存の文字列カテゴリはmigrationでカテゴリ行に移行し、空は「その他」 |
| `inventory_item_images` | `item_id`, `media_id`, `sort_order` | 物品の画像（複数。先頭がメイン。最大8枚はAPIで検査）。`media_references`の`inventory_item`/`photo`と対で更新（00021で「1所有者1画像」の制約を`profile`だけにした） |
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
| `work_contributors` | `id`, `content_id`, **`revision_id`（版ごと。変更は新しい版）**, `user_id`, `role_label`, `sort_order`, `added_by` | 作品typeのみ。**制作メンバー登録のみ**（編集権限者が操作）。公開クレジット可否は`public_credit_consents`（本人のみ変更）で別管理。名前はrevisionに焼き込まず配信時に解決 |
| `content_publication_events` | `id`, `content_id`, `revision_id`, `actor_user_id`, `event_type`, `created_at` | publish/unpublish履歴 |

`working_revision_id` / `published_revision_id` は同一 `content_id` に属することをサービス層・可能ならDB制約/triggerで検証。公開前は `published_revision_id IS NULL`。公開記事を書き換えても公開ポインタは変えない。

旧URLを保つため、現行サイトのMarkdownパスを `section`+`slug` に移す（大文字・ドット入りslugは変更しない）。現行のfrontmatterにある`category=Blog/Product`, `date`, `author`, `image`, `excerpt`, `sourceId`を損なわず保存する。

**日付（D12）:** `content_revisions`に`date_label`(原文。表示は`formatArticleDate`相当)、`date_kind`(day/month/range/unknown)、`sort_date`(DATE。unknownはNULL可)、`sort_date_is_estimated`(bool)を持つ。補った`sort_date`を実際の開催日として表示しない。

**公開クレジットの匿名化（D08）:** 公開APIは`work_contributors`と`public_credit_consents`を配信時に結合して名前を出す。同意撤回・個人情報削除後は、過去の公開版・古いrevisionを配信しても「退会メンバー」等になる（revision本文に名前を保存しない）。撤回時に公開キャッシュを無効化する。自由記述本文・画像内の本人情報は自動匿名化できないため、管理者の確認対象として別途一覧化する。

## 5. メディア・通知・監査

| テーブル | 主要カラム | 制約・補足 |
|---|---|---|
| `media_assets` | `id`, `public_id`（128bit乱数・22文字。idから導けない。本文の公開URLに使う）, `storage_key`（相対キー。絶対パスはAPIに出さない）, `mime_type`（保存するのはWebPのみ。再エンコード後）, `byte_size`, `width`, `height`（向き補正後）, `sha256`, `purpose`（avatar/inventory/cms）, `uploaded_by`, `created_at`, `redacted_at` | **`visibility`列は持たない**: 公開は「公開中の版が参照しているか」で毎回導く（P3-02）。保存は再エンコード後の値（元のEXIF等を保持しない）。`redacted_at`は本人情報を含む画像を`nuctl media-redact`で無効化した印（行と参照は残し、ファイルを消し、配信・参照・公開を止める） |
| `media_references` | `media_id`（FK・RESTRICT）, `owner_type`（content_revision）, `owner_id`, `purpose`（cover/inline） | 版は不変なので、版への参照は追記専用（トリガー）。`content_revisions.cover_media_id`もFK |
| `notification_settings` | `event_type`, `enabled`, `discord_channel_id`, `updated_by` | Adminが変更可能 |
| `notification_outbox` | `id`, `event_type`, `dedupe_key`（1イベント1行・UNIQUE）, `payload_json`（機密を入れない）, `status`（pending/sent/dead）, `attempts`, `next_attempt_at`, `last_error`（要約のみ）, `created_at`, `sent_at` | DB処理と同一トランザクションで作成。チャンネル未設定の間はpendingで待機 |
| `audit_logs` | `id`, `actor_user_id`, `action`, `target_type`, `target_id`, `summary_json`, `created_at` | 追記専用。トークン・パスワード・学生証番号の値を保存しない |
| ~~`dashboard_events`~~ | — | **未実装**（ダッシュボードの自動フィードは未実装。[FEATURES](FEATURES.md)）。実装するときにmigrationで作る |
| `legacy_authors` | `id`, `source_name`（UNIQUE）, `publication_state`（unconfirmed/permitted/withdrawn。既定は未確認＝公開しない）, `confirmed_by/at/note`, `withdrawn_at`, `linked_user_id/by/at`, `erased_at` | 既存記事の投稿者（移行元の情報）。アプリのユーザーとは別に管理し、**本人の公開許可が記録されるまで公開サイトでは「nu_digital」と表示**（D14）。名前は版（不変）に保存しない。個人情報削除では匿名の記号に置換（00010） |
| `bootstrap_state` | （1行） | 初期Ownerを**1回だけ**作れることを保証する印（00002）。2回目は別のログインIDでも拒否 |

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

D01〜D15は[DECISIONS](DECISIONS.md)で確定済み。以下は、当初の「実装時に決める詳細」と、その結果:

- 各テーブルの具体的な型・索引・CHECK → `db/migrations`で確定済み。
- `inventory_correction_events`から有効数量を求める集計方法（ビューか導出クエリ）。
- バックアップのマニフェストに含めるスキーマ版の持ち方（[OPERATIONS](OPERATIONS.md)）。
- 既存記事の`metadata_json`移行マッピング → 実装済み（`nuctl import-legacy`。[RUNBOOK §14](RUNBOOK.md)）。

## 8. `agent`スキーマ（ぬる。`services/agent`専用）

専用のDBロール`nu_agent`（migration 00013で作成。**NOLOGIN→運用者がパスワードを設定してLOGIN**。[RUNBOOK §16.2](RUNBOOK.md)）が、`agent`スキーマだけを使える。**nu_app本体（`public`）のテーブルは読めない**。日本語の部分一致検索は`pg_trgm`（`agent`スキーマ内に作成。00014）。仕組み全体は[NUL_OVERVIEW](NUL_OVERVIEW.md)。

| テーブル | 主な列・役割 | migration |
|---|---|---|
| `settings` | `key`・`value`(jsonb)。停止状態`control.mode`・予算の状態・各種の「一度きり」の記録（`intro.posted`・`report.*`・`diary.last`・`apikey.expires`など） | 00013 |
| `state` | 1行。気分`mood`(-2〜2)・テンション`energy`・ハマり`hobby` | 00013 |
| `relationships` | `discord_user_id`・親密度`closeness`（話した回数）・呼び名・最終 | 00013 |
| `messages` | ログの記録。`message_id`・`channel_id`・`thread_id`・`kind`(guild/dm)・`author_*`・`is_bot`・`content`・`attachments`(メタ情報のみ)・`created_at`・`edited_at`・**`is_public`**（@everyoneが見られるチャンネルか。検索・要約の材料は公開のみ）。Discordで削除されたら物理削除 | 00013・00020 |
| `import_progress` | 過去ログの取り込みの進捗（チャンネルごと。再開可能） | 00013 |
| `api_usage` | Claude APIの使用量の記録（モデル・用途・トークン・金額`cost_micro_usd`・チャンネル・ユーザー）。**本文は入れない**。予算の根拠 | 00013 |
| `daily_calls` | 一人あたりの1日の回数（kind: reply/opus/web） | 00013・00016 |
| `reactions_daily` | リアクションの1日の件数 | 00013 |
| `notes` | ぬるのノート。`subject_user_id`（本人についてのメモ。NULLは内省の一般メモ）・`content`≤500字・`private`（DM由来）・`source_message_id`（元のメッセージ。消されたらノートも消す） | 00014 |
| `summaries` | 日次要約（チャンネル×日(JST)）。公開チャンネルだけ | 00015 |
| `diary` | 研究日誌（`kind`: dig=古い要約から／web=Webで調べた／intro=内省の明日の話題。日×種別で1件） | 00017・00019 |
| `reminders` | リマインダー（`user_id`・送り先`channel_id`・`message`≤200字・`remind_at`・`status`(pending/sending/failed)・`private`（DMで頼んだもの）。配信したら削除） | 00018・00020 |

不変条件: DMの内容（`kind='dm'`・`private`）は検索・要約・他の人の会話に使わない／ノートは本人についてのメモだけ（他人のIDで作れない）／非公開チャンネルの内容は材料にしない／メッセージ・ノート・要約・リマインダーの上限と削除の連動（コードで強制。[NUL_OVERVIEW §5](NUL_OVERVIEW.md)）。バックアップは既存のDBダンプに含まれる（復元後は`nu_agent`のパスワードを再設定）。

