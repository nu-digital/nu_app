# 開発の入口 — 環境構築から、開発・テスト・PR・デプロイまで

初めてこのプロジェクトに入る人（人間でもAIエージェントでも）が、**この順に読んで・実行すれば**、一人で開発して本番に反映できることを目指した手順です。詳細は各リンク先。サーバーの運用は [RUNBOOK](RUNBOOK.md)、サーバーの新規構築・移行は [SERVER_SETUP](SERVER_SETUP.md)。

## 0. 先に用意するもの（アカウント・権限）

| 何 | なぜ・どこで |
|---|---|
| GitHub アカウントと、Organization `nu-digital` への参加（リポジトリへの書き込み権限） | コードの取得・PR。`production`環境の承認者になるには、運用責任者が追加する（[RUNBOOK §6](RUNBOOK.md)） |
| 開発用のPC（Windows＋WSL2のUbuntu 24.04、またはLinux/macOS） | 開発は主にWSL2。[WSL_SETUP](WSL_SETUP.md) |
| Docker（Engine・`docker compose`） | ローカル起動・統合テスト・Goのビルド（ホストにGoは不要） |
| Node.js（[.node-version](../.node-version) と同じ版） | web・admin・各種スクリプト |
| （AIを使う場合）Claude Code | 使い方・課金の線引きは [CLAUDE_TEAM](CLAUDE_TEAM.md)。**AIがなくても開発できる**（以下はAI不要の手順） |

秘密（`.env`・APIキー・トークン）は**Gitに入れない**。ローカルは架空の値（`.env.example`）で動く。

## 1. 取得して起動する

```sh
git clone https://github.com/nu-digital/nu_app.git && cd nu_app
cp .env.example .env           # ローカル用の架空の値
npm run web:ci && npm run admin:ci   # npm ci（初回）
npm run dev:up                 # db・migrate・api・bot・web・adminをビルドして起動（healthyまで待つ）
```

- 公式サイト http://127.0.0.1:8081/ ・ ポータル http://127.0.0.1:3000/ ・ API http://127.0.0.1:8080/healthz。詳細・初期Ownerの作り方・つまずき: [LOCAL_DEV](LOCAL_DEV.md)。
- **ぬる（`services/agent`）はローカルのComposeに含まれない**。実際のDiscord・Claude APIには、開発・CIから接続しない方針（偽のクライアントでテストする）。ぬるの検証は `scripts/go-docker.sh services/agent go test ./...` と統合テスト（下）。

## 2. コードの地図

| ディレクトリ | 中身 |
|---|---|
| `apps/web` | 公式サイト（Next.js。既存サイトをGit履歴ごと移設）。公開版のAPIから描画 |
| `apps/admin` | 内部ポータル（Next.js）。画面は`app/`配下 |
| `services/api` | Go API。`internal/`: `api`（ハンドラ）・`authz`（認可表）・`auth`・`store`（DB・sqlc）・`media`・`notify`・`cmsrules`・`mdcheck`・`legacyimport`・`recovery` ほか。`cmd/server`・`cmd/nuctl`（運用CLI） |
| `services/bot` | 骨組み（通知workerはAPI内） |
| `services/agent` | AIメンバー「ぬる」（[NUL_OVERVIEW](NUL_OVERVIEW.md)） |
| `db/migrations` | goose（前進のみ。00001〜）。`db/queries`はsqlc用 |
| `infrastructure/` | `compose.yaml`（ローカル）・`compose.prod.yaml`（本番）・`nginx`・`backup`・`deploy` |
| `scripts/` | `check.mjs`（文書・設定のチェック）・`go-docker.sh`・`test-integration.sh`・`sqlc.sh`・`compose-smoke.mjs` ほか |
| `docs/` | 文書（[docs/README.md](README.md)に一覧） |

## 3. 検証（変更したら実行する）

| 変更したもの | 実行するもの |
|---|---|
| 文書・設定 | `node scripts/check.mjs` |
| web・admin | `npm run check`（lint・型）・`npm run test:unit`・必要なら `npm run web:e2e`・`npm run compose:smoke` |
| Go（api・bot・agent） | `scripts/go-docker.sh services/<名前> go vet ./... && scripts/go-docker.sh services/<名前> go test -race -count=1 ./...` |
| DB・クエリ | `scripts/test-integration.sh`（実PostgreSQLの使い捨てコンテナ）・`scripts/sqlc.sh generate`（生成コードを更新してコミット） |
| インフラのスクリプト | CIの `ops / *`（バックアップ復元・Nginx・デプロイスクリプト）。ローカルは `scripts/nginx-test.sh` など |

「テスト成功」と書くときは、**実際に実行した結果**を示す。何を検証するコマンドか・CIとの対応は [TESTING](TESTING.md)。

## 4. 変更して、PRを出して、マージする

1. Issueを作る（または既存のIssueを選ぶ）。`main`から `<type>/<issue番号>-<英小文字の要約>` のブランチを切る（type: feat/fix/docs/refactor/test/chore/ci）。
2. 実装し、§3を実行。DB変更はmigrationを**追加**（既存を書き換えない・前進のみ）。仕様が未確定なら[DECISIONS](DECISIONS.md)に提案を書く。
3. コミット・Issue・PRは**日本語**。PRタイトルは `type(scope): 日本語`（CIが検査）。本文に `Closes #番号`・実測結果・未実施を書く。
4. push → PR。**CI（14ジョブ）が最新コミットで全部成功**してからマージ（Squash）。mainへ直接pushしない。
5. 詳しい規約・マージの運用: [DEVELOPMENT](DEVELOPMENT.md)・[AGENTS.md](../AGENTS.md)。

## 5. 本番へ反映する（デプロイ）

- **コード・設定を変えるPRをmainにマージすると**、GitHub Actionsが（`verify`: CI全成功の確認 → `build`: GHCRへイメージ → `deploy`: **承認待ち**）と進む。**`docs/`と`*.md`だけの変更ではデプロイは動かない**。
- **承認**: GitHub → Actions → 該当のrun → **Review deployments** → `production` → **Approve and deploy**。承認できるのは環境`production`の必須レビュアーだけ。承認すると、VPSで更新前バックアップ → `docker compose up -d` → ヘルスチェック（失敗したら前の版へ自動で戻る）。
- **切り戻し**: Actions → deploy → Run workflow → 戻したいコミットSHA。DBのmigrationは戻らない（前の版が新しいスキーマでも動く設計。破壊的変更は段階に分ける。[OPERATIONS](OPERATIONS.md)）。
- 詳細・確認コマンド・障害時: [RUNBOOK §8・§11](RUNBOOK.md)。

## 6. 困ったとき・次に読むもの

- 仕様の経緯・理由 → [HISTORY](HISTORY.md)・[DECISIONS](DECISIONS.md)。どこに何が書いてあるか → [docs/README.md](README.md)。
- 本番で障害 → [RUNBOOK §11](RUNBOOK.md)。ぬるを止めたい → `/nul stop`、効かなければ `sudo docker stop nu_app-agent-1`（[RUNBOOK §16.3](RUNBOOK.md)）。
- 今後の予定・課題 → [BACKLOG](BACKLOG.md)。
