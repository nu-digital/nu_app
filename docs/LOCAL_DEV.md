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
| web | http://127.0.0.1:8081/ | 公式サイト（動的配信。コンテンツは公開API（api）から描画時に取得）。URLは本番想定と同じ。APIが落ちている間は、コンテンツのページは5xxになる（`/healthz/`は応答する）。移行前の記事は、運用者が`nuctl import-legacy`で取り込むまで表示されない |
| admin | http://127.0.0.1:3000/ | 内部アプリの骨組み（`/healthz`のみ。画面はP1以降） |
| api | http://127.0.0.1:8080/healthz ・ `/readyz` | `readyz`はDB接続を含む |
| db | `127.0.0.1:15432`（PostgreSQL） | ローカル専用。ポートは`.env`の`DB_PORT` |
| bot | – | 起動のみ（Discord通知の送信はAPI内のworker。`DISCORD_BOT_TOKEN`を.envに設定したときだけ有効。docs/OPERATIONS.md） |

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

`compose:smoke`は開発用の`nu_app`プロジェクトとvolumeに触れない。画面E2E（Playwright。初回は`npx playwright install chromium`）を含み、**スクリーンショットなどの検証成果物は`.local/ui-shots/`に保存される（Git管理外）**。ブラウザー未導入の環境では`PLAYWRIGHT_SKIP=1`で画面E2Eだけ省略できる（省略した旨が表示され、成功扱いにはならない記録として残す）。

## 4. 初期Ownerの作成（ローカルの動作確認用）

初期Owner（最初の管理者）は、公開APIやWebではなく**管理CLI**でだけ作成できる。1回だけ実行でき、2回目は別のログインIDでも拒否される。

```sh
docker compose --env-file .env -f infrastructure/compose.yaml run --rm --no-deps --entrypoint /nuctl api \
  bootstrap-owner --login-id owner.example --display-name "ローカル用オーナー"
```

招待tokenが**一度だけ**表示される（DBにはSHA-256のみ。ログ・監査に残らない）。パスワード設定とログインはP1-02で提供する。やり直すには`npm run dev:reset`（開発用DBの初期化）。**本番では実行しない**（本番は未構築。実行は運用責任者の手順で別途決める）。

## 5. つまずいたら

- ポート競合: `.env`の`*_PORT`を変更。
- DBパスワードに記号を使うとDSNが壊れる。英数字と`- _ . ~`だけにする。
- 一部サービスだけ作り直す: `docker compose --env-file .env -f infrastructure/compose.yaml up --build -d <service>`。
- migrationは`migrate`サービスが起動時に`up`を実行する（再実行は安全）。スキーマはP1以降。

## 画像（メディア）

- composeのAPIは`MEDIA_DIR=/data/media`（名前付きボリューム`media-data`）に、再エンコード後の画像を保存する。ローカルでAPIを直接起動する場合は`MEDIA_DIR`に絶対パスを指定する（未設定ならアップロード・配信は503）。ボリュームを消すと画像も消える（`docker compose down -v`に注意）。
- 運用コマンド: `docker compose run --rm --no-deps --entrypoint /nuctl api media-verify`（`media-gc`・`media-redact`も同様。docs/OPERATIONS.md）。
