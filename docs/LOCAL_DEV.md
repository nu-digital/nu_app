# ローカル開発の起動手順（P0）

前提: WSL2/LinuxのUbuntu、Docker Engine（`docker compose`）、Node.js（[.node-version](../.node-version)）、Git。初回の準備は[WSL_SETUP](WSL_SETUP.md)。固定版は[TOOLCHAIN](TOOLCHAIN.md)。ホストにGoは不要。

## 1. 起動

```sh
git clone https://github.com/nu-digital/nu_app && cd nu_app
cp .env.example .env          # 架空のローカル用の値。必要ならポートを変更
npm run dev:up                # db → migrate → api → bot / web / admin をビルドして起動し、healthyまで待つ
```

| サービス | URL（127.0.0.1のみ） | 内容 |
|---|---|---|
| web | http://127.0.0.1:8081/ | 既存の公式サイト（静的export）。URLは本番想定と同じ |
| admin | http://127.0.0.1:3000/ | 内部アプリの骨組み（`/healthz`のみ。画面はP1以降） |
| api | http://127.0.0.1:8080/healthz ・ `/readyz` | `readyz`はDB接続を含む |
| db | `127.0.0.1:15432`（PostgreSQL） | ローカル専用。ポートは`.env`の`DB_PORT` |
| bot | – | 起動のみ（Discord連携はP2） |

`.env`が無い、または`POSTGRES_*`が空のときComposeは起動を拒否する。`.env`は`.gitignore`済みでGitに入れない。

## 2. 停止・初期化

```sh
npm run dev:down      # 停止（DBデータは残る）
npm run dev:reset     # 停止してDBのvolumeも削除（開発用プロジェクト nu_app のみ。本番では使わない）
```

## 3. 検証

```sh
npm run check            # harness・web・adminのlint/型検査
npm run test:unit        # 単体テスト
npm run web:e2e          # 全ルート・ギャラリーのブラウザー確認（要 playwright install。README参照）
npm run compose:smoke    # 専用プロジェクト(nu_app_smoke)で起動し、全サービスの健全性・全72ルート・DB停止時の挙動を確認して片付ける
scripts/go-docker.sh services/api go test -race -count=1 ./...
scripts/test-integration.sh   # 実PostgreSQLの統合テスト（使い捨てコンテナ）
scripts/sqlc.sh generate      # クエリ/migration変更後にsqlcの生成コードを更新
```

`compose:smoke`は開発用の`nu_app`プロジェクトとvolumeに触れない。

## 4. つまずいたら

- ポート競合: `.env`の`*_PORT`を変更。
- DBパスワードに記号を使うとDSNが壊れる。英数字と`- _ . ~`だけにする。
- 一部サービスだけ作り直す: `docker compose --env-file .env -f infrastructure/compose.yaml up --build -d <service>`。
- migrationは`migrate`サービスが起動時に`up`を実行する（再実行は安全）。スキーマはP1以降。
