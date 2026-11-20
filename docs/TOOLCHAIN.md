# 固定したツール版（P0-03、2026-11-21確認）

版は一次資料で確認して固定した。更新は独立PRで、lint/テスト/ビルド/E2Eを確認し理由をここへ追記する。最新を無条件に採用しない。

| 対象 | 固定版 | 確認元・採用理由 |
|---|---|---|
| Node.js | 22.23.5（`.node-version`） | [公式配布一覧](https://nodejs.org/dist/index.json)。既存サイトのNode 22系、LTS |
| npm | Nodeに同梱（10.9.9で検証） | lockfileは各アプリ(`apps/web`)が持つ |
| Go | 1.27.4（`services/*/go.mod`、`scripts/go-docker.sh`、各Dockerfile、CI） | [go.dev/dl](https://go.dev/dl/?mode=json)の最新stable、[endoflife.date](https://endoflife.date/go)でサポート中（1.26系もサポート中）。イメージは`golang:1.27.4`をdigestで固定（ホストにGoは不要） |
| PostgreSQL | 18.7（`postgres:18.7`をdigest固定） | [endoflife.date](https://endoflife.date/postgresql)でサポート中（EOL 2030-11）。19.0は2026-11-19リリースで直後のため採用せず。新規構築で最新に近いが実績のある版 |
| pgx | v5.11.0（`services/api/go.mod`） | [リリース](https://github.com/jackc/pgx/releases)の最新v5。Go標準のPostgreSQLドライバーとして広く使われる |
| migration tool | goose v3.30.1（`db/Dockerfile`でbuild） | [pressly/goose](https://github.com/pressly/goose/releases)の最新。SQLファイルでup/down、CLIのみで運用でき、アプリ側にmigration実行コードを持たなくてよい。golang-migrate(v4.20.1)も候補だったが、同等で差が小さく、SQLのトランザクション単位の扱いが明快なgooseを採用 |
| sqlc | v1.31.1（採用予定。`db/sqlc.yaml`は最初のクエリを書くP1-01で追加） | [sqlc-dev/sqlc](https://github.com/sqlc-dev/sqlc/releases)の最新。クエリが無い段階では生成物が空になるため設定を先に置かない |
| 実行イメージ | `gcr.io/distroless/static-debian12:nonroot`（digest固定） | 静的バイナリのみを非rootで実行。シェル・パッケージ管理を持たず攻撃面が小さい |
| 配信イメージ（web） | `nginxinc/nginx-unprivileged:1.30.6-alpine`（digest固定） | nginx stable 1.30系（[endoflife.date](https://endoflife.date/nginx)でサポート中）。1.30.7は同イメージ未公開のため1.30.6 |
| Nodeイメージ | `node:22.23.5-alpine`（digest固定） | `.node-version`と同版 |
| Playwright | 1.65.1（`apps/web`のdevDependency） | P0-02b。ブラウザー確認用 |
| Next.js / eslint-config-next | 16.4.6 | P0-02b。[legacy-baseline](legacy-baseline/README.md) |

## Goの実行（ホストにGoが無い場合）

```sh
scripts/go-docker.sh services/api go test -race -count=1 ./...
scripts/go-docker.sh services/api sh -c 'test -z "$(gofmt -l .)" && go vet ./... && go build ./...'
```

## migration

`db/migrations/`のSQLを`db/Dockerfile`のgooseで適用する。最初の`00001_baseline.sql`は基盤確認用で業務テーブルを作らない（スキーマはP1）。再実行は「no migrations to run」で安全に終わる。本番でmigrationを実行する手順・順序はP4で決める。
