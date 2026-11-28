# ローカル開発の起動手順

## フロントエンドだけを開く（GitとNode.js）

リポジトリのルートで実行する。Nodeは [.node-version](../.node-version) の固定版を使う。次の手順はクリーンなcloneを想定し、アプリ用の環境変数・`.env`・APIキーの設定は不要。PowerShellで `npm.ps1` が実行制限を受ける場合は `npm.cmd` と入力する。Next.jsはWindows・macOS・Linuxに対応する。[Next.jsの対応環境](https://nextjs.org/docs/app/getting-started/installation)

**公式サイト**は最初の端末で既存記事のモックを起動する。既定の8080が空いていることを確認する（Composeと同時に使う場合は次節の構成を使う）。

```sh
npm run web:ci
node scripts/mock-public-api.mjs --port 8080
```

別の端末を同じルートで開いて起動する。

```sh
npm --prefix apps/web run dev -- --hostname 127.0.0.1 --port 3001
```

ブラウザーで `http://127.0.0.1:3001/blog/` を開いて記事を選ぶ。コンポーネントやCSSの変更は開発サーバーで反映される。記事モックはMarkdownを起動時に読むため、記事データを読み直すときはモックを再起動する。

**管理ポータル**は単独の端末で起動し、`http://127.0.0.1:3000/login` を開く。

```sh
npm run admin:ci
npm --prefix apps/admin run dev -- --hostname 127.0.0.1 --port 3000
```

現状この方法で確認できるのはログインフォームなどの未認証画面。APIを起動していないと `/auth/me` の中継は失敗する。ログイン後の業務操作は下記のCompose手順を使う。サーバーを終了するときは起動した各端末でCtrl+Cを押す。Next 16.4.6が各アプリ直下に生成する `AGENTS.md` はフレームワークの案内で、団体共通のルールはルートの [AGENTS.md](../AGENTS.md) にある。

2026-11-28のWindows実測では、Node 22.23.5で両方の `next dev`、Edgeでの表示、管理ポータルのFast Refreshを確認した。macOS・Linuxの開発モードは今回未検証。権限別モックと1コマンド化は [FRONTEND](FRONTEND.md) の提案を参照。

## システム全体を起動する前提

WSL2/LinuxのUbuntu、Docker Engine（`docker compose`）、Node.js、Git。初回の準備は[WSL_SETUP](WSL_SETUP.md)。固定版は[TOOLCHAIN](TOOLCHAIN.md)。ホストにGoは不要。

## 1. 起動

```sh
git clone https://github.com/nu-digital/nu_app && cd nu_app
cp .env.example .env          # 架空のローカル用の値。必要ならポートを変更
npm run dev:up                # db → migrate → api → bot / web / admin をビルドして起動し、healthyまで待つ
```

| サービス | URL（127.0.0.1のみ） | 内容 |
|---|---|---|
| web | http://127.0.0.1:8081/ | 公式サイト（動的配信。コンテンツは公開API（api）から描画時に取得）。URLは本番想定と同じ。APIが落ちている間は、コンテンツのページは5xxになる（`/healthz/`は応答する）。移行前の記事は、運用者が`nuctl import-legacy`で取り込むまで表示されない |
| admin | http://127.0.0.1:3000/ | 内部ポータル（ログイン・メンバー・物品・記事/作品・管理画面。初期Ownerの作成は§4） |
| api | http://127.0.0.1:8080/healthz ・ `/readyz` | `readyz`はDB接続を含む |
| db | `127.0.0.1:15432`（PostgreSQL） | ローカル専用。ポートは`.env`の`DB_PORT` |
| bot | – | 骨組みのみ（Discord通知の送信はAPI内のworker。`DISCORD_BOT_TOKEN`を.envに設定したときだけ有効。docs/OPERATIONS.md） |
| **agent（ぬる）** | – | **ローカルのComposeには含めない**。実際のDiscord・Claude APIに、開発・CIから接続しない方針（[CLAUDE_TEAM](CLAUDE_TEAM.md)）。検証は`scripts/go-docker.sh services/agent go test ./...`（偽のClaude・偽のDiscord）と`scripts/test-integration.sh`（実PostgreSQL）。有効モードの実機確認は本番で[NUL_LAUNCH](NUL_LAUNCH.md)だけ |

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
npm run web:site-check   # web:build後、apps/webにPlaywrightのChromiumを導入して実行（TESTING参照）
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

招待tokenが**一度だけ**表示される（DBにはSHA-256のみ。ログ・監査に残らない）。パスワードは、表示されたtokenで`/invite`の画面から設定してログインする。やり直すには`npm run dev:reset`（開発用DBの初期化）。**本番では実行しない**（本番の初期Ownerは作成済み。再実行は拒否される。緊急時の復旧は[RUNBOOK §11](RUNBOOK.md)）。

## 5. つまずいたら

- ポート競合: `.env`の`*_PORT`を変更。
- DBパスワードに記号を使うとDSNが壊れる。英数字と`- _ . ~`だけにする。
- 一部サービスだけ作り直す: `docker compose --env-file .env -f infrastructure/compose.yaml up --build -d <service>`。
- migrationは`migrate`サービスが起動時に`up`を実行する（再実行は安全）。スキーマは`db/migrations`（00001〜）。

## 画像（メディア）

- composeのAPIは`MEDIA_DIR=/data/media`（名前付きボリューム`media-data`）に、再エンコード後の画像を保存する。ローカルでAPIを直接起動する場合は`MEDIA_DIR`に絶対パスを指定する（未設定ならアップロード・配信は503）。ボリュームを消すと画像も消える（`docker compose down -v`に注意）。
- 運用コマンド: `docker compose run --rm --no-deps --entrypoint /nuctl api media-verify`（`media-gc`・`media-redact`も同様。docs/OPERATIONS.md）。

## ローカルでPlaywright（Chromium）が動かないとき（WSLに共有ライブラリが無い）

`npm run web:site-check`・`compose:smoke`・`web:e2e`は、ホストのChromiumが共有ライブラリ（libnspr4・libnss3・libasound）を要する。sudoできない環境では、パッケージを展開して`LD_LIBRARY_PATH`で渡せる:

```sh
mkdir -p /tmp/chromelibs/debs && cd /tmp/chromelibs/debs && apt-get download libnspr4 libnss3 libasound2t64
for d in *.deb; do dpkg -x $d /tmp/chromelibs/root; done
export LD_LIBRARY_PATH=/tmp/chromelibs/root/usr/lib/x86_64-linux-gnu    # 以降、このシェルでnpm run ... を実行
```

`/tmp`は消えることがあるので、必要なら作り直す。`pkill -f <パターン>`は、そのコマンド自身を殺すことがあるので使わず、`fuser -k <port>/tcp`で止める。

