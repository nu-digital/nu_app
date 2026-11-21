# nu_app アーキテクチャ設計 v0.1（提案）

## A. サービス境界

```text
Internet
  └─ Nginx (TLS終端, rate limit, request size, reverse proxy)  ※D03で正式採用。認証・認可の判断はしない
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
   - Owner継承（D05）: 現Ownerの申請(24時間有効・承諾前に取消可)→移譲先の承諾の二段階。承諾は単一トランザクションで確定し旧OwnerはAdminに残る。最後のOwnerが消える操作は禁止。
   - 緊急復旧は運用責任者の本人確認・明示承認を伴う管理CLIのみ。公開Webの復旧入口は作らず、通常のデプロイ手順とは別経路（GitHub Actionsから呼べない）。実行時に対象のセッションを全失効し監査に残す。
2. AdminがログインIDを持つユーザーを作成 → 使い捨て招待URL（トークンはランダムでDBにはハッシュのみ） → 本人がパスワード設定。
3. Adminによる本人確認後、パスワードリセットURLを発行。使用後は既存セッションを無効化。
4. パスワードはArgon2id等の適切なハッシュ形式。平文・可逆暗号・ログ出力は禁止。
5. セッションはサーバー管理。Cookie は HttpOnly, Secure, SameSite、CSRFとOrigin検証。セッションIDは十分な長さの暗号学的乱数、DBにはハッシュ値を保存。
6. 権限は `Member` を基本権限とし、`Editor`, `Admin`, `Owner` は追加ロール。OwnerはAdmin権限を内包するが、管理者権限を付与できるのはOwnerだけ。
7. `OWNER`を0人にする操作は禁止。権限変更・アカウント停止時はトランザクションで権限/セッションの変更を反映。
8. 公開プロフィールは明示的なDTOを組み立てる。DBモデルをそのままJSON出力しない。
9. 公開サイトのHTML/Markdownは許可リスト方式でサニタイズし、危険な生HTMLを実行させない。
10. プロフィール（D02）: 本人必須は表示名のみ。個人情報4項目は本人と権限あるAdmin/Ownerのみ。その他は任意・初期非公開。メンバー内公開とインターネット公開は別の同意。

## D. 画像・メディア

- VPS永続領域：`/srv/nu-system/data/media` （例）、DBの`media_assets`にはID、所有者、状態、MIME、サイズ、ストレージキー、用途、参照を保持。
- 公開画像 `/api/v1/public/media/{public_id}` は認証不要だが、**公開中の版が参照している間だけ**配信する（APIが毎回判定。複製・公開領域・公開フラグを持たない）。非公開画像 `/api/v1/media/{id}` は認可が必要。全ファイルは同じ非公開ディレクトリに置き、Nginx等が直接配信しない。再エンコード後の形式はJPEG/PNG（cgoを避けるためWebPは出力しない）。
- 下書き画像は公開URLで配信されない（参照しているのが下書きだけなら404）。設計: [design/p3-media.md](design/p3-media.md)。
- 新規アップロード（D06）: JPEG/PNG/WebPのみ。実体検査（拡張子を信用しない）、アニメーション画像・SVG拒否、サイズ（avatar 5MiB/他10MiB）、デコード**前**の寸法検査（≤2,000万画素・各辺≤10,000px）、向き補正、縮小（avatar≤512px、他は長辺≤2,560px、拡大しない）、再エンコード、EXIF削除、名前の無害化、ディレクトリトラバーサル防止。デコードの同時実行数を制限し、4GB環境のピークメモリを実測する。
- 既存サイトの静的画像（約126MiB）はこの経路を通さず、移設で同一性（sha256）を保つ。上限を理由に黙って削除・変換しない。本番アップロードはVPS永続領域のみ。
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
- 訂正（D09）: Admin/Ownerのみ、理由必須。元記録は更新・削除せず訂正イベントを追記し有効数量へ反映。対象物品行を`FOR UPDATE`でロックして通常の貸出・返却と直列化し、`Idempotency-Key`で重複適用を防ぐ。在庫が負・返却数超過になる訂正は拒否。

## F. CMS公開の整合性

- `contents`はURL・種類・作者・working_revision_id・published_revision_idを持つ。
- 編集を保存するたび不変の`content_revisions`を作り、楽観ロック（base revision）で上書き競合を防ぐ。
- 公開は現在のworking revisionをpublished pointerへ反映する権限付き操作。
- 公式サイトはpublished pointerのみ読む。下書きデータは絶対に公開APIから見えない。
- 初期は公開サイトで掲載リスト/詳細を動的取得、または短時間キャッシュ＋明示無効化を選択可能。公開のためのGit commit / 毎回のサイト全体ビルドは不要。
- 既存サイトの`/blog/:slug/`, `/news/:slug/`, `/project/:slug/` と記事frontmatterをマッピング。現行ギャラリー・日付表示をリグレッションテスト。
- 公開サイトへの制作メンバー表示は各ユーザーの公開同意（`public_credit_consents`）を配信時に確認。名前をrevisionへ焼き込まず、撤回・個人情報削除後は旧版・キャッシュでも匿名（D08）。本文・画像内の本人情報は管理者が確認・修正する。
- 権限（D01）: 記事と作品で共通。作者・共同編集者は編集、Editor/Admin/Ownerは全件編集・公開・非公開化。制作メンバー登録と公開クレジット同意は別。
- 埋め込み（D10）: YouTubeのみ。許可URLから動画IDを抽出しアプリ側でiframeを生成。生HTMLは禁止。

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
- 外部バックアップは初期版ではない。VPS内バックアップは毎日03:00(JST)・成功7世代・DBとメディアを一組（D04、詳細は[OPERATIONS](OPERATIONS.md)）。VPS喪失時にデータを失う事実を隠さない。
- Nginxの証明書は自動更新し、更新失敗・期限接近を検知して通知する（D03）。事業者固有機能に依存せずDocker Composeで構築（D07）。
