# 固定したツール版（P0-03、2026-11-21確認）

版は一次資料で確認して固定した。更新は独立PRで、lint/テスト/ビルド/E2Eを確認し理由をここへ追記する。最新を無条件に採用しない。

| 対象 | 固定版 | 確認元・採用理由 |
|---|---|---|
| Node.js | 22.23.5（`.node-version`） | [公式配布一覧](https://nodejs.org/dist/index.json)。既存サイトのNode 22系、LTS |
| npm | Nodeに同梱（10.9.9で検証） | lockfileは各アプリ(`apps/web`)が持つ |
| Go | 1.27.4（`services/*/go.mod`、`scripts/go-docker.sh`、各Dockerfile、CI） | [go.dev/dl](https://go.dev/dl/?mode=json)の最新stable、[endoflife.date](https://endoflife.date/go)でサポート中（1.26系もサポート中）。イメージは`golang:1.27.4`をdigestで固定（ホストにGoは不要） |
| PostgreSQL | 18.7（`postgres:18.7`をdigest固定） | [endoflife.date](https://endoflife.date/postgresql)でサポート中（EOL 2030-11）。19.0は2026-11-19リリースで直後のため採用せず。新規構築で最新に近いが実績のある版 |
| pgx | v5.12.0（`services/api/go.mod`。P1-01aでgoose(テスト用ライブラリ)の依存解決により5.11.0から更新） | [リリース](https://github.com/jackc/pgx/releases)の最新v5。Go標準のPostgreSQLドライバーとして広く使われる |
| migration tool | goose v3.30.1（`db/Dockerfile`でbuild） | [pressly/goose](https://github.com/pressly/goose/releases)の最新。SQLファイルでup/down、CLIのみで運用でき、アプリ側にmigration実行コードを持たなくてよい。golang-migrate(v4.20.1)も候補だったが、同等で差が小さく、SQLのトランザクション単位の扱いが明快なgooseを採用 |
| sqlc | v1.31.1（`db/sqlc.yaml`、`scripts/sqlc.sh`でdigest固定イメージ） | [sqlc-dev/sqlc](https://github.com/sqlc-dev/sqlc/releases)の最新。生成コードは`services/api/internal/store`にコミットし、CIの`go / sqlc`が差分を検出 |
| goose（Goライブラリ） | v3.30.1（`services/api`のテスト用） | 統合テストが本物のmigrationを適用するため。CLIと同じ版 |
| goldmark | v1.8.7（`services/api/go.mod`。2026-10-30公開。P3-01a） | CMSのMarkdownを**ASTで検査**するため（正規表現でHTMLを検査しない）。CommonMark準拠でGFM拡張を公式に持つ。[リリース](https://github.com/yuin/goldmark/releases)の最新。脆弱性確認: 2026-11-21に`govulncheck`（v1.10.0）を`services/api`全体に実行し「No vulnerabilities found」（依存更新のたびに再実行する） |
| golang.org/x/image | v0.47.0（`services/api/go.mod`。2026-10-09公開。P3-02a） | 画像のWebPデコード（`webp`）と縮小（`draw`のCatmullRom）。標準ライブラリにWebPデコーダー・高品質な縮小が無いため。最新はv0.48.0（2026-11-09公開）だが公開から日が浅いため1つ前を採用。脆弱性確認: 2026-11-21に`govulncheck`（v1.10.0）を`services/api`全体に再実行し「No vulnerabilities found」 |
| golang.org/x/net | v0.62.0（`services/api/go.mod`。P3-01a） | `html`トークナイザーでMarkdown中の生HTMLを検査するため。最新はv0.63.0（2026-11-17公開）だが直後のため、1つ前を採用。脆弱性確認: 2026-11-21に`govulncheck`（v1.10.0）を`services/api`全体に実行し「No vulnerabilities found」（依存更新のたびに再実行する） |
| gopkg.in/yaml.v3 | v3.0.1（`services/api/go.mod`。P3-04a） | 既存記事のfrontmatter（YAML）を、移行時にgray-matter（js-yaml）のゴールデンと同じ結果で解釈するため。保守は停止気味だが安定版で、移行CLIの入力（リポジトリ内の固定ファイル）にだけ使い公開入力は読まない。脆弱性確認: 2026-11-21に`govulncheck`（v1.10.0）を`services/api`全体に実行し「No vulnerabilities found」 |
| golang.org/x/image | v0.47.0（`services/agent/go.mod`。apiと同じ版。ぬるP1-3） | Discord添付画像の縮小（`draw`）とWebPのデコード。画像は寸法を先に検査し（2,500万画素・10MB）、長辺1568pxのJPEGにしてからClaudeへ送る |
| pg_trgm（PostgreSQL contrib） | postgres:18.7に同梱（migration 00014。`agent`スキーマ内） | ぬるのログ・ノートの日本語の部分一致検索（GIN索引）。追加のイメージ・パッケージ不要 |
| github.com/anthropics/anthropic-sdk-go/v2 | v2.9.0（`services/agent/go.mod`。2026-11-10公開。ぬるP1-3） | Claude Messages API（ベータ名前空間: サーバー側フォールバック・会話途中のシステムメッセージ・effort）。最新はv2.15.0（2026-11-24）だが、ほぼ毎日リリースされるため、公開から2週間以上の最新（v2.9.0）を採用。必要なAPI（`BetaSystemMessageParam`・`BetaFallbacksParamOfDefault`・`BetaOutputConfigParam`）がそろうことを確認。試験は`httptest`（実際のAPIは呼ばない）。確認: 2026-11-27に`govulncheck`（v1.10.0）で「No vulnerabilities found」 |
| github.com/bwmarrin/discordgo | v0.29.0（`services/agent/go.mod`。2025-05-24公開。ぬるP1-2） | Discordの接続（Gateway・REST・スラッシュコマンド）。Goで事実上標準のライブラリ。最新タグ。 |
| github.com/gorilla/websocket | v1.5.4-0.20240701034025-d67f41855da4（`services/agent`。間接依存。discordgoが要求する1.4.2を上書き） | `govulncheck`が`GO-2026-6278`（WebSocketのマスクキーが弱い乱数）を検出。修正は未タグのコミットにだけあるため、そのコミットに固定（2024-07-01。2週間以上前）。タグ付きリリースが出たら置き換える。確認: 2026-11-27に`govulncheck`（v1.10.0）で「No vulnerabilities found」 |
| golang.org/x/crypto | v0.59.0（`services/agent`。間接依存。apiと同じ版） | `GO-2026-5932`の修正済みの版へ上げた |
| github.com/gen2brain/webp | v0.6.4（`services/api/go.mod`。2026-07-01公開。P3-02-5） | WebPのエンコード（ロッシー・ロスレス・アルファ）。純Goにはロッシーが無く、cgo＋libwebpは`distroless/static`と合わない。libwebpをGoへトランスパイルした同梱実装を使う（`-tags nodynamic`で環境のlibwebpを使わない）。同梱の生成物の差分は確認できないため、更新は別PRで、`go.sum`固定・`govulncheck`の再実行を必須とする。脆弱性確認: 2026-11-21に`govulncheck`（v1.10.0、`-tags nodynamic`）を`services/api`全体に実行し「No vulnerabilities found」 |
| 実行イメージ | `gcr.io/distroless/static-debian12:nonroot`（digest固定） | 静的バイナリのみを非rootで実行。シェル・パッケージ管理を持たず攻撃面が小さい |
| 配信イメージ（web） | `nginxinc/nginx-unprivileged:1.30.6-alpine`（digest固定） | nginx stable 1.30系（[endoflife.date](https://endoflife.date/nginx)でサポート中）。1.30.7は同イメージ未公開のため1.30.6 |
| Nodeイメージ | `node:22.23.5-alpine`（digest固定） | `.node-version`と同版 |
| react-markdown / remark-gfm / rehype-raw / rehype-sanitize | 10.1.0 / 4.0.1 / 7.0.0 / 6.0.0（`apps/admin`。いずれも完全固定。P3-03a） | CMSのプレビュー。react-markdown・remark-gfm・rehype-rawは既存サイト（`apps/web`）と同じ版。`rehype-sanitize`は描画時のサニタイズ（保存型XSSの防壁）のため。`npm audit`は0件 |
| unified / remark-parse / remark-rehype / rehype-stringify | 11.0.5 / 11.0.0 / 11.1.2 / 10.0.1（`apps/admin`のdevDependency。P3-03a） | サニタイズのテスト（`node --test`）で、プレビューと同じプラグインのパイプラインをHTML文字列にするため |
| Playwright | 1.65.1（`apps/web`のdevDependency） | P0-02b。ブラウザー確認用 |
| Next.js / eslint-config-next | 16.4.6 | P0-02b。[legacy-baseline](legacy-baseline/README.md) |

## Goの実行（ホストにGoが無い場合）

```sh
scripts/go-docker.sh services/api go test -race -count=1 ./...
scripts/go-docker.sh services/api sh -c 'test -z "$(gofmt -l .)" && go vet ./... && go build ./...'
```

## 統合テスト（実PostgreSQL）

`scripts/test-integration.sh`が使い捨てのPostgreSQL 18.7を起動して`go test -tags integration`を実行する（`TEST_DATABASE_URL`が無いと失敗し、skipしない）。各テストは専用の`nu_test_*`DBを作り、本物のmigrationを適用して終了時に削除する。CIでは`go / integration`が同じテストを実行する。

## migration

`db/migrations/`のSQLを`db/Dockerfile`のgooseで適用する。最初の`00001_baseline.sql`は基盤確認用で業務テーブルを作らない（スキーマはP1）。再実行は「no migrations to run」で安全に終わる。本番でmigrationを実行する手順・順序はP4で決める。
