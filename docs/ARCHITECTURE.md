# nu_app アーキテクチャ設計 v0.1（提案）

## A. サービス境界

```text
Internet
  └─ Nginx (TLS, rate limit, request size, reverse proxy)  ※正式採用待ち
      ├─ nu-digital.jp         → apps/web (Next.js, public website)
      ├─ app.nu-digital.jp     → apps/admin (Next.js, login/dashboard/CMS)
      ├─ app.nu-digital.jp/api/v1/* → services/api (Go)
      └─ app.nu-digital.jp/media/*  → Go media access / Nginx public assets

private Docker network
  ├─ services/api (Go; auth, member, inventory, CMS, media, notifications)
  ├─ services/bot (Go; Discord outbound worker, API shared contracts)
  ├─ PostgreSQL
  └─ persistent media directory (host-mounted; outside container image)
```

- APIに業務ルールと権限判定を集約し、Next.jsのServer ActionsやAPI routesに重複実装しない。
- 公開サイトは、Goの読み取り専用公開コンテンツAPIを内部ネットワークで呼び出す。**未公開版のAPIを絶対に使わない**。
- メンバー向けAPIは `app.nu-digital.jp/api/v1/*` の同一オリジンで公開し、Cookie認証とCSRF防御を適用。
- DB、Bot、Goの内部ポートは外部公開しない。
- アプリランチャーのカードはコード上のアプリ定義（id, title, icon, route, release status）から作る。全員に共通表示。権限は個別APIで検証。
- モノレポにまとめるが、Next.js web/adminは別のビルド・Dockerイメージにする。開発用サービスを本番VPSへ常駐させない。

## B. 想定ディレクトリ

```text
nu-system/
├── apps/
│   ├── web/                     # 既存公式サイトの移設先
│   └── admin/                   # nu_app管理画面
├── services/
│   ├── api/
│   │   ├── cmd/server/
│   │   └── internal/{auth,members,inventory,cms,media,notifications,platform}/
│   └── bot/                     # Discord worker（必要ならAPIと同一Go binaryも可）
├── db/
│   ├── migrations/
│   └── queries/                 # sqlc用SQL
├── infrastructure/
│   ├── compose.yaml
│   ├── nginx/
│   └── scripts/
├── packages/                    # 共通TS型やUI等（必要なものだけ）
├── docs/
│   ├── REQUIREMENTS.md
│   ├── ARCHITECTURE.md
│   ├── DATABASE.md
│   ├── API.md
│   └── TASKS.md
├── AGENTS.md
└── .github/workflows/
```

- 既存サイトはnpm / Next.js。移行リスクを下げるため、初期モノレポはnpm workspacesを第一候補とする（pnpm等への移行は必須でない）。
- Go PostgreSQLアクセスは `pgx/v5 + sqlc` を候補とする。SQLスキーマは明示的なマイグレーションで管理。
- 共有Goパッケージは必要最小限とし、循環依存を避ける。

## C. 認証・セキュリティ

1. 初回Owner登録はCLI経由で使い捨てbootstrap tokenを生成。最初のOwner作成後はbootstrapを無効化。
2. AdminがログインIDを持つユーザーを作成 → 使い捨て招待URL（トークンはランダムでDBにはハッシュのみ） → 本人がパスワード設定。
3. Adminによる本人確認後、パスワードリセットURLを発行。使用後は既存セッションを無効化。
4. パスワードはArgon2id等の適切なハッシュ形式。平文・可逆暗号・ログ出力は禁止。
5. セッションはサーバー管理。Cookie は HttpOnly, Secure, SameSite、CSRFとOrigin検証。セッションIDは十分な長さの暗号学的乱数、DBにはハッシュ値を保存。
6. 権限は `Member` を基本権限とし、`Editor`, `Admin`, `Owner` は追加ロール。OwnerはAdmin権限を内包するが、管理者権限を付与できるのはOwnerだけ。
7. `OWNER`を0人にする操作は禁止。権限変更・アカウント停止時はトランザクションで権限/セッションの変更を反映。
8. 公開プロフィールは明示的なDTOを組み立てる。DBモデルをそのままJSON出力しない。
9. 公開サイトのHTML/Markdownは許可リスト方式でサニタイズし、危険な生HTMLを実行させない。

## D. 画像・メディア

- VPS永続領域：`/srv/nu-system/data/media` （例）、DBの`media_assets`にはID、所有者、状態、MIME、サイズ、ストレージキー、用途、参照を保持。
- 公開画像 `/media/public/{opaque-id}.webp` は認証不要。非公開画像 `/api/v1/media/{id}` は認可が必要。
- 下書き画像は絶対に公開URLに置かない。公開時に画像を公開領域に複製し、公開版でのみ公開URLを参照する方式を第一候補とする。
- MIME実体検査、ファイルサイズ・ピクセル数上限、名前の無害化、可能なら再エンコードとEXIF削除、ディレクトリトラバーサル防止。
- 公開済みの記事が参照している画像は、編集時に即削除しない。未参照画像の掃除は安全な参照チェック後に実施。
- DBとメディアを別々に復元すると参照不整合が起きるため、バックアップ手順には両者の整合性確認を含める。

## E. 物品の整合性

- `inventory_items.mode` = `quantity` または `individual`。
- 数量管理：各貸出の未返却数を記録。貸出処理は該当物品行を `SELECT ... FOR UPDATE` でロックし、残数が不足していれば409を返す。
- 個体管理：`inventory_units`の単位で貸出を追跡し、貸出中の個体を二重貸出させない制約と行ロックを使う。
- 誰でも返却可能。`loan_id`に対して返却数を指定し、借りた本人と実際の返却操作者は別々に記録する。
- 返却操作と監査ログ、Discord向けoutboxイベントの作成は同じDBトランザクションで確定。
- 数量管理物品に複数の貸出履歴がある場合、返却UIで対象貸出を選択できるようにする。
- 貸出中の管理方式変更、貸出数未満への総数変更、未返却個体の廃止は禁止。

## F. CMS公開の整合性

- `contents`はURL・種類・作者・working_revision_id・published_revision_idを持つ。
- 編集を保存するたび不変の`content_revisions`を作り、楽観ロック（base revision）で上書き競合を防ぐ。
- 公開は現在のworking revisionをpublished pointerへ反映する権限付き操作。
- 公式サイトはpublished pointerのみ読む。下書きデータは絶対に公開APIから見えない。
- 初期は公開サイトで掲載リスト/詳細を動的取得、または短時間キャッシュ＋明示無効化を選択可能。公開のためのGit commit / 毎回のサイト全体ビルドは不要。
- 既存サイトの`/blog/:slug/`, `/news/:slug/`, `/project/:slug/` と記事frontmatterをマッピング。現行ギャラリー・日付表示をリグレッションテスト。
- 公開サイトへの制作メンバー表示は各ユーザーの公開同意を確認。非公開の内部人物情報は表示しない。

## G. Discord通知

- 業務DB更新と同じトランザクションで`notification_outbox`へイベント保存。
- Go workerが一定間隔で未送信イベントを取得し、Discord APIへ投稿。成功・失敗・試行回数・次回再試行をDB管理。
- `notification_settings`でイベント種別別の有効/無効とDiscord channel idを管理。変更はAdmin/Ownerのみ。
- エラー時の再送は重複投稿の可能性があるため、イベントIDを通知に埋め込むなど重複調査を可能にする。Discordへのexactly-onceは保証しない。
- Bot Tokenは環境変数/secretで管理。本文には学生証番号・メール・招待URL・トークンを含めない。

## H. CI/CD・運用

- PR: frontend lint/typecheck/test/build、Go fmt/vet/test/build、DB migration検証、Docker build。
- main merge後: GitHub ActionsでDockerイメージをbuild/push → VPSが既知のSHAに対応するイメージをpull → ヘルスチェック → 完了/失敗通知。
- DBマイグレーションはアプリの後方互換性を考慮し、破壊的変更を同一デプロイで実施しない。
- ロールバックは前回のイメージSHAへ戻す手順を設ける。DBを含む完全ロールバックと同義にはしない。
- 最小権限のデプロイユーザー、SSH鍵、Firewall、イメージ固定、secretを出力しないログ設計。
- 4GB VPSではビルド処理をしない。Next.js standaloneイメージを採用し、メモリ・ディスク・稼働状況を監視。
- 外部バックアップは初期版ではない。ローカルバックアップ・保存世代は要確認。VPS喪失時にデータを失う事実を隠さない。
