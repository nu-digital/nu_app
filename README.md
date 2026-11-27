# nu_app

nu_digital（大学のデジタル創作サークル）の**公式サイト・内部ポータル・API・AIメンバー「ぬる」**をまとめたモノレポ。約20人の運営の手間を減らすための、**本番稼働中**のサービスです。技術デモではなく、個人情報と団体の活動履歴を預かっています。

- 公式サイト: https://nu-digital.jp ／ 内部ポータル: https://app.nu-digital.jp
- 現在の状態（2026-11-28）: 公式サイト・ポータル・ぬるのすべてが本番で稼働中。デプロイは「mainにマージ → CI → 運用責任者がGitHubで承認 → 自動デプロイ」だけ。残件と今後は [docs/BACKLOG.md](docs/BACKLOG.md)。

## これは何か（目的）

団体の運営を軽くする共通ポータルです。メンバー管理（プロフィール）、物品の貸出・返却、記事・作品の公開（CMS）、Discord通知を1か所にまとめ、既存の公式サイトを同じ基盤から配信します。さらに、Discordに住むAIメンバー「ぬる」が、会話・調べもの・記憶・リマインダーなどで団体の日常に加わります。

| 要素 | 内容 | 詳細 |
|---|---|---|
| **公式サイト**（`apps/web`） | 既存サイトの見た目・URLを維持したまま、公開版のAPIから描画 | [FEATURES](docs/FEATURES.md) |
| **内部ポータル**（`apps/admin`） | ログイン・メンバー・プロフィール・物品貸出・記事/作品の編集と公開・Discord通知の設定 | [FEATURES](docs/FEATURES.md) |
| **API**（`services/api`・Go） | 認証・権限・業務ロジックのすべて（UIの非表示だけで制限しない） | [API](docs/API.md)・[AUTHZ](docs/AUTHZ.md) |
| **Bot**（`services/bot`・Go） | 骨組みのみ（通知workerはAPI内で動く。[ARCHITECTURE](docs/ARCHITECTURE.md)） | — |
| **ぬる**（`services/agent`・Go） | DiscordのAIメンバー。Claude APIで会話・記憶・自律行動 | [NUL_OVERVIEW](docs/NUL_OVERVIEW.md) |
| **DB**（`db/`） | PostgreSQL 18。前進のみのmigration（goose） | [DATABASE](docs/DATABASE.md) |
| **インフラ**（`infrastructure/`） | Docker Compose・Nginx・バックアップ・デプロイ・証明書の確認 | [RUNBOOK](docs/RUNBOOK.md)・[SERVER_SETUP](docs/SERVER_SETUP.md) |

## システム構成

```
利用者 ──HTTPS──> Cloudflare(DNSのみ) ──> さくらのVPS（Ubuntu 24.04・4コア4GB）
                                           ├ ホストのNginx（TLS・レート制限）
                                           │   nu-digital.jp     → web（Next.js。公開版APIから描画）
                                           │   app.nu-digital.jp → admin（Next.js）→ /api/v1 → api（Go）
                                           └ Docker Compose: db(PostgreSQL) ─ migrate ─ api ─ bot ─ web ─ admin ─ agent
                                                                                                          │
Discord ◀──────────────────────────────────────── ぬる(agent) ──▶ Claude API（Anthropic）

GitHub: mainにマージ → Actions（CI・ビルド→GHCR）→ 運用責任者が承認 → VPSへ自動デプロイ
バックアップ: 毎日03:00(JST)・VPS内に7世代（DB＋画像）。VPS外はなし
```

## 使っている主な技術・サービス（採用理由は [docs/TOOLCHAIN.md](docs/TOOLCHAIN.md)）

Go 1.27・PostgreSQL 18（pgx・goose・sqlc）／Next.js 16・React・Node 22／Docker Compose・Nginx・Let's Encrypt／GitHub Actions・GHCR／さくらのVPS・Cloudflare（DNS）／Discord（discordgo）・Anthropic Claude API（Sonnet/Opus/Haiku・Batch・Web検索）。費用・契約・更新は [docs/COSTS_AND_CONTRACTS.md](docs/COSTS_AND_CONTRACTS.md)。

## まず読む（目的別）

| あなたは… | 読むもの |
|---|---|
| **初めて来た開発者** | このREADME → [docs/START_HERE.md](docs/START_HERE.md)（環境構築→開発→テスト→PR→デプロイの一続きの手順）→ [docs/LOCAL_DEV.md](docs/LOCAL_DEV.md) |
| **運用担当（保守・障害対応・引き継ぎ）** | [docs/RUNBOOK.md](docs/RUNBOOK.md)（本番の構成と手順の正本）→ [docs/COSTS_AND_CONTRACTS.md](docs/COSTS_AND_CONTRACTS.md)・[docs/SERVER_SETUP.md](docs/SERVER_SETUP.md)（サーバーの新規構築・移行） |
| **ぬるを触る・直す人** | [docs/NUL_OVERVIEW.md](docs/NUL_OVERVIEW.md) → [設計の経緯](docs/design/nul-agent.md)・[DECISIONS](docs/DECISIONS.md)（D15・NUL-*）→ [RUNBOOK §16](docs/RUNBOOK.md) |
| **AIエージェント（Claude Codeなど）** | [AGENTS.md](AGENTS.md)（ルール）→ [docs/AI_GUIDE.md](docs/AI_GUIDE.md)（設計思想・実装状況・落とし穴）→ [docs/handoffs/LATEST.md](docs/handoffs/LATEST.md) |
| **仕様・設計を知りたい** | [REQUIREMENTS](docs/REQUIREMENTS.md)（要件）→ [ARCHITECTURE](docs/ARCHITECTURE.md) → [DATABASE](docs/DATABASE.md) → [API](docs/API.md) → [AUTHZ](docs/AUTHZ.md) |
| **経緯・判断の理由** | [docs/HISTORY.md](docs/HISTORY.md)・[docs/DECISIONS.md](docs/DECISIONS.md) |

全文書の一覧と役割は [docs/README.md](docs/README.md)。

## 開発の基本ルール（要点。詳細は [AGENTS.md](AGENTS.md)・[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)）

- **1 Issue = 1 PR**。ブランチ名は `<type>/<issue番号>-<英小文字の要約>`、コミット・Issue・PRは日本語。PRタイトルは `type(scope): 日本語`。
- **mainへ直接pushしない**。CI（14ジョブ）が全部成功してからマージ。デプロイはマージ後のGitHub Actionsだけで、`production`環境の承認が要る（手動SSHで本番を変えない）。
- **認証・権限はGo APIに集約**。個人情報（学籍番号・メール等）をログ・Discord・テストに出さない。画像をGitに入れない。秘密は`.env`／VPS／GitHub Secretsだけ（Gitに入れない）。
- **開発・CI・テストで実際のClaude API・Discordを呼ばない**（偽物で試験）。
- 未確定の仕様は勝手に決めず、[DECISIONS](docs/DECISIONS.md)に提案として残す。

## すぐ試す（ローカル）

```sh
cp .env.example .env && npm run dev:up    # db・api・bot・web・adminを起動（Docker）。詳細: docs/LOCAL_DEV.md
node scripts/check.mjs                    # 文書・設定の準備用チェック（アプリの機能テストではない）
```

## ライセンス・素材

公式サイトの素材の権利は [apps/web/ASSETS.md](apps/web/ASSETS.md)。
