# 運用・引き継ぎ手引き（RUNBOOK）

この文書は、**本番環境（nu_digitalの公式サイト・内部ポータル）を運用・保守する人が、これだけを読めば作業できる**ことを目的にした、実際の環境の記録と手順書です。代表者・管理者が交代しても、後任がここから始められるようにします。

- 設計の方針・確定事項（D01〜D14）は [OPERATIONS](OPERATIONS.md)・[DECISIONS](DECISIONS.md)。この文書は「**今どう構成されていて、どう操作するか**」を書く。食い違いを見つけたら、実際の環境を確認してこの文書を直す。
- **パスワード・秘密鍵・トークン・WebhookのURL・証明書の秘密鍵は、この文書にも、Git・チャット・Issue・ログにも書かない。** 場所と更新手順だけを書く。
- 最終更新: 2026-11-26（初回デプロイ前）。**初回デプロイ・自動デプロイの有効化・本番データの移行は、この時点では未実施**（実施したら §15 に日付を記録する）。
- 「（要記入）」は、運用責任者が把握して書き込む項目（契約者名など。コードからは分からない）。

---

## 1. 全体像

```
利用者 ──HTTPS──> Cloudflare(DNSのみ・プロキシなし) ──> VPS 133.125.46.174
                                                          │
                                   ┌───────── ホストのNginx（80/443。TLS終端・レート制限）
                                   │   nu-digital.jp      → 127.0.0.1:8081  web（公式サイト。公開APIから描画）
                                   │                         └ /api/v1/public/media/<22文字> だけ → 127.0.0.1:8080 api
                                   │   app.nu-digital.jp  → 127.0.0.1:3000  admin（内部ポータル。/api/v1 をapiへ中継）
                                   └──────────────────────────────────────────────────────────────
                       Docker Compose（/srv/nu_app/infrastructure/compose.prod.yaml）
                         db(PostgreSQL 18) ─ migrate(1回) ─ api(Go) ─ bot ─ web(Next) ─ admin(Next)
                       永続データ: DB=Dockerの名前付きvolume nu_app_db-data ／ 画像=/srv/nu_app/media ／ バックアップ=/srv/nu_app/backups
```

- 外から届くのは **Nginxの80/443だけ**（Dockerのポートは`127.0.0.1`にしかbindしない）。DB・API・Botを直接インターネットに出さない。
- **VPSではイメージをビルドしない**。GitHub Actionsがビルドして`ghcr.io`へ置いたもの（タグ＝コミットSHA）を、承認を経てデプロイする。
- 主なリポジトリ: `nu-digital/nu_app`（このリポジトリ。サーバーの`/srv/nu_app`に読み取り専用でclone）。

## 2. VPS

| 項目 | 内容 |
|---|---|
| 事業者・プラン | さくらのVPS v5・石狩第1ゾーン（2026-11-26に契約・無料お試し期間中。**本契約への移行・料金の支払い者: （要記入）**） |
| 仕様 | 仮想4コア・メモリ約3.9GB・SSD 200GB（使用は約6GB）。Ubuntu 24.04.5 LTS amd64。タイムゾーンJST |
| IPv4 | 133.125.46.174（IPv6は使っていない。DNSにAAAAレコードなし） |
| 管理ユーザー | `ubuntu`（sudo。**パスワードあり**に戻す方針。§15）／`deploy`（GitHub Actions専用。§5） |
| さくらの管理画面 | https://secure.sakura.ad.jp/vps/ （シリアルコンソール・パケットフィルター・電源。**契約者のアカウント: （要記入）**） |
| パケットフィルター（さくら側） | TCP 22・80・443を許可 |

### 2.1 サーバー内の設定（実機で確認済み）

- **SSH**（`/etc/ssh/sshd_config.d/10-nu-hardening.conf`）: `PasswordAuthentication no`・`KbdInteractiveAuthentication no`・`PermitRootLogin no`・`AllowUsers ubuntu deploy`・`MaxAuthTries 4`・`LoginGraceTime 30`。**鍵認証だけ**。設定を変えるときは、必ず別セッションを開いたまま`sudo sshd -t && sudo systemctl reload ssh`し、新しいセッションで入れることを確認してから元のセッションを閉じる。
- **ファイアウォール**（ufw）: 既定deny。22/tcp（`limit`＝回数制限）・80・443を許可。`sudo ufw status verbose`。
- **fail2ban**: sshd用（`sudo fail2ban-client status sshd`）。自分が弾かれたら`sudo fail2ban-client set sshd unbanip <IP>`。
- **自動更新**: `unattended-upgrades`（セキュリティ更新）。カーネル更新後は再起動が必要（`/var/run/reboot-required`）。再起動の手順は§11.4。
- **Docker**: Docker Engine（公式apt。`docker --version`で確認。記録時点29.10.1）・Compose plugin。`docker.service`は有効（再起動後に自動起動）。コンテナは`restart: unless-stopped`。ログは`json-file`（10MB×5世代）。
- **Nginx**: Ubuntuのパッケージ 1.24.0（**`http2 on;`は使えない**。`listen 443 ssl http2;`）。設定は`/etc/nginx/conf.d/nu_app.conf`（リポジトリの`infrastructure/nginx/nu_app.conf`のコピー。**リポジトリを直したらサーバーにコピーして`nginx -t && systemctl reload nginx`**）。アクセスログは`/var/log/nginx/nu_app.access.log`（クエリ文字列を残さない）。
- **ディレクトリ**:

| パス | 所有者・権限 | 内容 |
|---|---|---|
| `/srv/nu_app/` | root・755 | リポジトリ（デプロイが同じコミットに揃える。**直接編集しない**） |
| `/srv/nu_app/.env` | root・600 | 本番の設定（DBパスワード等の秘密を含む。Git外）。キー: `NU_REGISTRY`・`POSTGRES_USER`・`POSTGRES_DB`・`POSTGRES_PASSWORD`・`APP_BASE_URL`・`MEDIA_HOST_DIR`（`NU_TAG`はデプロイスクリプトが付ける） |
| `/srv/nu_app/media/` | 65532:65532・750 | アップロード画像（APIコンテナのnonroot）。**バックアップ対象** |
| `/srv/nu_app/backups/` | root・700 | バックアップの世代（§9） |
| `/srv/nu_app/state/` | root・755 | `current_tag`（動いている版）・`previous_tag`・デプロイのロック |
| `/etc/nu_app/backup.env` | root・600 | バックアップ・通知の設定（`BACKUP_NOTIFY_WEBHOOK_URL`等。秘密を含む） |
| `/etc/nu_app/repo_deploy_key`（`.pub`） | root・600 | サーバーがGitHubからリポジトリを読むための鍵（§5） |
| `/root/.docker/config.json` | root・600 | GHCRからpullするための認証（§6.4）。秘密 |
| `/etc/letsencrypt/` | root | TLS証明書と秘密鍵・certbotの設定（§4） |

- **systemd**: `certbot.timer`（Ubuntu標準。証明書の自動更新）、`nu-cert-check.timer`（毎日09:00 JST。証明書の期限確認）、`nu-backup.timer`（毎日03:00 JST。バックアップ。**初回デプロイ後に有効化**。§9）。ユニットはリポジトリの`infrastructure/{nginx,backup}/systemd/`のコピーを`/etc/systemd/system/`へ。

## 3. ドメイン・DNS（Cloudflare）

- ドメイン: `nu-digital.jp`（**レジストラ・契約者・更新期限: （要記入）**。更新を忘れるとサイトが止まる。更新の担当と時期を決めておく）。
- DNS管理: Cloudflare（アカウント: （要記入））。レコード:

| 種別 | 名前 | 値 | プロキシ |
|---|---|---|---|
| A | `nu-digital.jp`（`@`） | 133.125.46.174 | **DNS only（グレーの雲）** |
| A | `app` | 133.125.46.174 | **DNS only** |

- **プロキシ（オレンジの雲）にしない理由**: 現在の構成は、クライアントのIPをNginxの`$remote_addr`から取り、レート制限・ログ・APIの信頼ルールがそれに依存する。プロキシにすると全員がCloudflareのIPに見える。プロキシにするなら、(1) NginxにCloudflareのIP範囲を`set_real_ip_from`で設定、(2) SSLモードをFull (strict)、(3) HTMLと公開JSONをCloudflareでキャッシュしない設定（D08のため。Cache Rulesで`Bypass cache`）、をすべて行ってから切り替える。
- IPアドレスが変わる場合（VPSの作り直し等）: Cloudflareで2つのAレコードを更新 → 反映後に証明書は再取得不要（同じ名前）。ただしVPSを作り直したらサーバーの初期構築（§2・§5・§6）からやり直し、バックアップから復旧する（§9.4）。

## 4. Nginx・TLS証明書

- **構成**: 設定は`infrastructure/nginx/nu_app.conf`。公開ホストは`GET/HEAD`だけで、APIは画像の公開配信（`/api/v1/public/media/<22文字>`）だけ中継、それ以外の`/api/`は404。管理ホストはadminへ中継（本文12MiBまで）。**HTML・公開JSONはキャッシュしない**（`proxy_cache`を使わない）。X-Forwarded-Forは`$remote_addr`で上書き。HSTS付き。詳細は[OPERATIONS](OPERATIONS.md)「TLS・Nginx」。
- **証明書**: Let's Encrypt。certbot（Ubuntuのパッケージ 2.9.0）、webroot方式（`/var/www/certbot`）。`nu-digital.jp`と`app.nu-digital.jp`をそれぞれ別の証明書で取得（`/etc/letsencrypt/live/<ホスト名>/`）。**2026-11-26に取得、期限2027-02-23**（約90日。残り30日あたりでcertbotが自動更新）。certbotのアカウントは**メールなしで登録**（`--register-unsafely-without-email`）。期限切れの通知メールは来ないので、下の`cert-check`のDiscord通知を頼りにする。
- **自動更新**: `certbot.timer`（1日2回）が`certbot renew`を実行 → 更新できたら`/etc/letsencrypt/renewal-hooks/deploy/nu-nginx-reload.sh`が`nginx -t`のあと再読み込み。**確認**: `systemctl list-timers | grep certbot`・`sudo certbot renew --dry-run`・`sudo certbot certificates`。
- **期限の監視**: `nu-cert-check.timer`（毎日09:00 JST）が、証明書ファイルと**実際に配信されている証明書**の残日数を見て、14日以下・読めない・配信側が確認できないとき、DiscordへWebhookで通知する（`journalctl -u nu-cert-check`）。通知が来たら§11の「証明書」へ。
- **新しいホスト名を足す**: (1) DNSにAレコード、(2) `/etc/nginx/conf.d/nu_app.conf`の80番のserverの`server_name`に追加して反映、(3) `sudo certbot certonly --webroot -w /var/www/certbot -d <新ホスト>`、(4) 443のserverを追加（リポジトリの設定を直し、テスト`scripts/nginx-test.sh`を通してからサーバーへ）。

## 5. SSH鍵の一覧と管理

**鍵の一覧（秘密鍵そのものは書かない。場所だけ）**

| 鍵（コメント） | 用途 | 公開鍵の置き場所 | 秘密鍵の場所 | できること |
|---|---|---|---|---|
| `nu_app-vps-admin` | **管理者（運用担当）の個人の鍵**。サーバー管理 | VPSの`/home/ubuntu/.ssh/authorized_keys` | 運用担当のPC（パスフレーズ付きを推奨） | `ubuntu`でログイン・sudo |
| `claude-nu_app-ops` | AI開発エージェント（Claude Code）の作業用。構築・検証のため | 同上 | 開発環境のWSL `~/.ssh/nu_app_vps` | 同上（**`ubuntu`と同じ権限**） |
| `nu_app-actions-deploy` | GitHub Actionsのデプロイ専用 | VPSの`/home/deploy/.ssh/authorized_keys`（`restrict,command="/srv/nu_app/infrastructure/deploy/ssh-entry.sh"`付き） | GitHubの環境`production`のsecret `DEPLOY_SSH_KEY`（と、作成した開発環境の`~/.ssh/nu_app_actions`） | **`deploy.sh <40桁のコミットSHA>`だけ**（入口が他のコマンドを拒否。端末・ポート転送なし） |
| `nu_app-vps-repo-readonly` | VPSがGitHubからリポジトリを読む（deploy key） | GitHub: リポジトリ → Settings → Deploy keys（**読み取り専用**） | VPSの`/etc/nu_app/repo_deploy_key`（root・600） | リポジトリの読み取りのみ |

**運用ルール**
- 管理者が増える・交代するときは、**新しい人の公開鍵を`authorized_keys`に追加 → 新しい人が実際に入れることを確認 → 去る人の鍵を削除**の順（逆にしない）。`ssh-keygen -lf ~/.ssh/authorized_keys`で一覧と指紋を確認できる。
- **`claude-nu_app-ops`は、AIエージェントに作業させる期間だけ**置く。作業が終わったら`authorized_keys`から削除する（運用責任者の判断）。
- 鍵を作り直す（漏えいの疑い・担当交代）: (a) 新しい鍵を作る → (b) サーバーに追加して接続を確認 → (c) 古い鍵を削除。Actionsの鍵は、(a)新しい鍵ペアを作る → (b)VPSの`/home/deploy/.ssh/authorized_keys`の鍵を差し替え（`restrict,command=...`のプレフィックスを必ず付ける） → (c)GitHubのsecret `DEPLOY_SSH_KEY`を更新 → (d)古い鍵を削除。repoのdeploy keyは、(a)`sudo ssh-keygen -t ed25519 -N "" -f /etc/nu_app/repo_deploy_key`で作り直し → (b)GitHubのDeploy keysを入れ替え。
- サーバーの**ホスト鍵**の指紋（接続時の確認用。公開してよい）: `SHA256:SVvijkywaLzF6U1+F42yixOUJ4Wsemb9ELA1EGsRcxw`（ED25519）。さくらのシリアルコンソールで`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`で照合できる。OSを再インストールすると変わる（GitHubの`DEPLOY_KNOWN_HOSTS`も更新する）。
- **締め出されたら**: さくらの管理画面の**シリアルコンソール**から（ネットワーク経由でないので、SSHの設定に関係なく入れる。`ubuntu`のパスワードが必要＝§12の引き継ぎ対象）。

## 6. GitHub（Actions・環境・Secrets・GHCR）

### 6.1 構成
- リポジトリ: `nu-digital/nu_app`（Organization配下）。ワークフローは`.github/workflows/`。デプロイは`deploy.yml`（**変数`DEPLOY_ENABLED`が`true`のときだけ動く**。既定は無効）。
- **Organizationの設定**: 「Deploy keys」の作成を許可する設定を有効にしてある（無効だとサーバーがリポジトリを読めない）。
- 環境（Settings → Environments）`production`: **必須レビュアー＝承認者**（現在: `yurarara23`）。デプロイ・切り戻しのジョブは、この承認を経ないと実行されない。**承認者を、卒業・交代のときに必ず更新する**（§12）。

### 6.2 環境のSecrets（`production`）— 4つ（値は書かない）
| 名前 | 内容 | 更新方法 |
|---|---|---|
| `DEPLOY_HOST` | `133.125.46.174` | VPSのIPが変わったとき |
| `DEPLOY_USER` | `deploy` | 変えない |
| `DEPLOY_SSH_KEY` | Actions専用の秘密鍵の全文（§5） | 鍵を作り直したとき（§5） |
| `DEPLOY_KNOWN_HOSTS` | `133.125.46.174 ssh-ed25519 AAAA…`（サーバーのホスト公開鍵1行） | `ssh-keyscan -t ed25519 <IP>`の結果を、**さくらのシリアルコンソールで指紋を照合してから**登録 |

### 6.3 変数・自動デプロイ
- リポジトリの変数`DEPLOY_ENABLED`（Settings → Secrets and variables → Actions → Variables）。**`true`にすると、mainへのpushごとに「CI全成功の確認 → GHCRへビルド → 承認待ち → デプロイ」が動く**。止めたいときは削除か`false`にする（実行中のジョブは止まらない。止めるときはActionsの画面でcancel）。
- 手動の切り戻し: Actions → deploy → Run workflow（入力: 戻したいコミットSHA）。

### 6.4 GHCR（コンテナイメージ）
- イメージ: `ghcr.io/nu-digital/nu_app-{api,bot,migrate,web,admin}:<コミットSHA>`。ビルド・pushはActionsが`GITHUB_TOKEN`で行う（追加のsecret不要）。
- **サーバーがpullするための認証**: GitHubの**Personal access token (classic)**（スコープは`read:packages`のみ）を作り、サーバーで`sudo docker login ghcr.io -u <ユーザー名> --password-stdin`（トークンは標準入力に貼る。履歴・チャットに残さない）。認証は`/root/.docker/config.json`に保存される。**トークンの作成者: （要記入）**。作成者が卒業・退会したらトークンが失効するので、**後任が新しいトークンを作り直して同じコマンドで登録する**（有効期限を設定した場合は期限前に更新）。
- 古いイメージの整理: GHCRのパッケージ画面から削除。**切り戻し用に、直近数版は残す**。VPS上の古いイメージは`sudo docker image prune -a --filter "until=720h"`（動いているコンテナのイメージは消えない）。

## 7. Discord通知

- 用途: バックアップの失敗、デプロイの成功・失敗（前の版へ戻した結果を含む）、TLS証明書の期限警告を、運用チャンネルへ通知する。**通知にパスワード・token・個人情報は含めない**。
- 設定: チャンネルの「連携サービス → ウェブフック」で作ったURL（**秘密**）を、サーバーの`/etc/nu_app/backup.env`の`BACKUP_NOTIFY_WEBHOOK_URL=`に書く（root・600。Gitに入れない）。デプロイ・バックアップ・証明書確認のスクリプトが読む。**2026-11-26に設定し、接続テスト（204）を確認**。
- URLを作り直す（漏えいの疑い・担当交代）: Discordでウェブフックを削除して作り直し → `backup.env`の値を差し替え → 接続テスト:
  `sudo bash -c 'set -a; . /etc/nu_app/backup.env; set +a; curl -fsS -H "Content-Type: application/json" -d "{\"content\":\"[nu_app] テスト\"}" "$BACKUP_NOTIFY_WEBHOOK_URL"'`（204が返る）。
- アプリ内のDiscord通知（物品の貸出・返却。Botのtoken）は別系統: 管理画面「通知の設定」と、`.env`の`DISCORD_BOT_TOKEN`。Botのtokenはまだ設定していない（通知は送信待ちに溜まる）。設定するときは、Discord Developer PortalでBotを作り、tokenを`/srv/nu_app/.env`へ書いてデプロイし直す。

## 8. デプロイ・切り戻し

**通常のデプロイ**（`DEPLOY_ENABLED=true`のとき）: mainにマージ → 自動で (1) そのコミットのCIが全成功か確認 → (2) GHCRへビルド・push → (3) **承認待ち**（承認者にGitHubから通知。Actionsの画面で内容・SHAを確認して「Approve and deploy」） → (4) VPSで`deploy.sh <SHA>`: 直列化 → リポジトリを同じSHAへ → イメージpull → **更新前バックアップ**（失敗したら更新しない）→ `compose up -d` → ヘルスチェック（api `/readyz`・web `/healthz/`・admin `/healthz`・`/blog/`） → 成功を通知。**失敗したら動いていた版へ自動で戻して通知**（DBのmigrationは戻らない）。

**切り戻し（手動）**: Actions → deploy → Run workflow → 戻したいSHAを入力 → 承認。状態は`/srv/nu_app/state/current_tag`・`previous_tag`で確認。DBのスキーマは前進のみなので、**前の版が新しいスキーマで動く**ことがルール（[OPERATIONS](OPERATIONS.md)「デプロイ・切り戻し」の「DBの互換性のルール」）。DBまで巻き戻す必要がある場合は§9.4の復旧。

**サーバーで直接確認**: `ssh ubuntu@<IP>` → `sudo docker compose -f /srv/nu_app/infrastructure/compose.prod.yaml --env-file /srv/nu_app/.env ps`（※`NU_TAG`が必要: `sudo env NU_TAG=$(sudo cat /srv/nu_app/state/current_tag) docker compose ...`）。**手でイメージを入れ替えない**（通常はActions経由）。

## 9. バックアップ・復元

### 9.1 仕様（D04）
毎日03:00 JST・成功した7世代・DBとメディアを同じ世代として保存・失敗はDiscord通知・失敗しても既存の世代は消さない・新しい世代の確定後にだけ古いものを消す。場所は`/srv/nu_app/backups/<UTC時刻の世代ID>/`（`db.dump`・`media.tar.gz`・`manifest.txt`・`SHA256SUMS`・`COMPLETE`。権限700）。`LATEST`が最新を指す。**VPS外へのバックアップはしていない**（初期スコープ外。VPS自体・ディスクを失うと復旧できない。最大約24時間分の更新を失い得る）。

### 9.2 有効化（初回デプロイ後に実施）
`sudo systemctl enable --now nu-backup.timer`。`systemctl list-timers | grep nu-backup`で次回実行を確認。手動実行は`sudo systemctl start nu-backup.service`、結果は`journalctl -u nu-backup -n 30`。**初回デプロイ前は有効にしない**（DBが無く失敗して通知が出るため）。

### 9.3 復元検証（公開前・四半期に1回・方式変更時）
```
sudo env POSTGRES_IMAGE='postgres:18.7@sha256:eb63372c1e05c755f55fa30cbd07aecc443676fc6c0fa90b9002d01537841e1a' \
         NUCTL_IMAGE="ghcr.io/nu-digital/nu_app-api:$(sudo cat /srv/nu_app/state/current_tag)" \
         /srv/nu_app/infrastructure/backup/restore-verify.sh
```
隔離した一時コンテナ・一時ディレクトリに復元して、チェックサム・スキーマ・行数・メディアのファイル数・DBの行とファイルの整合（`nuctl media-verify`）を確認する。本番には触れない。`OK:`と出れば成功。

### 9.4 実際の復旧（障害時・手動）
1. 影響を考え、`web`・`admin`・`api`・`bot`を止める（`compose stop web admin api bot`）。2. 復旧する世代を決める（`ls /srv/nu_app/backups`）。3. DB: 空のDBを用意し`pg_restore --no-owner --clean --if-exists -d <db> db.dump`（`compose exec -T db pg_restore ...`）。4. メディア: `tar -xzf media.tar.gz -C /srv/nu_app/media`（所有者を`65532:65532`に）。5. `nuctl media-verify`で整合確認。6. 起動して`/healthz`・`/readyz`・サイトを確認。7. 事後: 失った期間の更新を関係者に伝える。**VPSごと失った場合**: 新しいVPSを§2・§5・§6の手順で構築し、(VPS外にバックアップが無いため)復旧できない。この場合に備えるなら、運用責任者がVPS外のバックアップ（別の場所へ`backups/`を定期コピー）を追加で決める（現在は未対応）。

## 10. 定期・随時の更新作業

| 対象 | 頻度の目安 | 手順 |
|---|---|---|
| OSのセキュリティ更新 | 自動（unattended-upgrades）。月1回は`apt list --upgradable`と`/var/run/reboot-required`を確認 | 再起動が必要なら§11.4 |
| Docker Engine | 数か月に1回 | `sudo apt update && sudo apt install --only-upgrade docker-ce docker-ce-cli containerd.io docker-compose-plugin`。コンテナは再起動される（直前にバックアップを取る） |
| アプリ・依存（Go・Next・イメージのベース） | 随時（Dependabot・脆弱性の通知） | PR → CI → マージ → デプロイ（承認）。ツールの版は[TOOLCHAIN](TOOLCHAIN.md) |
| TLS証明書 | 自動（残り約30日で更新） | 通知（`nu-cert-check`）が来たら§11.2 |
| GHCR読み取りトークン | 作成時の有効期限・作成者の卒業前 | §6.4。新しいトークンで`docker login` |
| Actionsのデプロイ鍵・VPSの管理鍵 | 担当交代・漏えいの疑い・年1回の見直し | §5 |
| Discord Webhook | 担当交代・漏えいの疑い | §7 |
| ドメイン | 毎年（期限は§3の記入欄） | レジストラの更新 |
| バックアップの復元検証 | 四半期に1回 | §9.3 |
| ディスク使用量 | 月1回 | `df -h /`・`du -sh /srv/nu_app/{media,backups}`・`sudo docker system df`。不足しそうなら古いイメージ・世代を整理（**バックアップの世代数は黙って減らさず、運用責任者が判断**） |

## 11. 障害対応

まず、**何が壊れているか**を切り分ける。サーバーに入る: `ssh ubuntu@133.125.46.174`。状態確認: `sudo docker ps`、`curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/readyz`（api）・`:8081/healthz/`（web）・`:3000/healthz`（admin）、`sudo systemctl status nginx`、`df -h /`、`free -m`。

### 11.1 サイトが開かない・502
- Nginxが動いているか（`systemctl is-active nginx`）。動いていて502 → 上流（web/admin/api）が落ちている: `sudo docker ps -a`。落ちているコンテナのログ: `sudo docker logs --tail 100 nu_app-<service>-1`。`compose up -d`で再起動（`NU_TAG`を付けて。§8）。
- **APIだけ落ちている場合**: 公式サイトのコンテンツのページ（トップ含む）は5xxになる（静的ではなく、描画時にAPIから取るため）。`/healthz/`は応答する。APIを直す。
- 直前のデプロイが原因なら、切り戻し（§8）。

### 11.2 証明書（通知が来た・ブラウザーの警告）
`sudo certbot certificates`で期限を確認 → `sudo certbot renew`（失敗したら理由が出る。80番が閉じている・DNSが違う・Nginxのwebroot設定が壊れた、が多い）→ `sudo nginx -t && sudo systemctl reload nginx` → ブラウザーで確認。**更新済みなのに古い証明書が配られている**場合は再読み込み忘れ。

### 11.3 ディスクが満杯
`df -h /`・`du -sh /srv/nu_app/* /var/lib/docker /var/log`。多い順に: Docker（`sudo docker system df`・`image prune`）、ログ（`journalctl --vacuum-size=200M`）、バックアップ（世代・サイズを確認し、運用責任者が判断。**黙って世代を減らさない**）、メディア（孤立した画像は`nuctl media-gc`（§11.7の形式で実行））。DBが書き込めなくなる前に対応する。

### 11.4 サーバーの再起動
バックアップを取ってから（`sudo systemctl start nu-backup.service`）、`sudo reboot`。再起動後に`docker ps`で全コンテナが`healthy`・`systemctl list-timers`でタイマーが有効・サイトが開くことを確認。Nginx・Dockerは自動で起動する。

### 11.5 通知が来た（バックアップ・デプロイ）
- **バックアップ失敗**: `journalctl -u nu-backup -n 50`。空き容量不足・DBが止まっている・権限、が多い。既存の世代は消えていない（`ls /srv/nu_app/backups`）。直して`sudo systemctl start nu-backup.service`。
- **デプロイ失敗（前の版へ戻した）**: ActionsのログとDiscordの通知で原因を確認。アプリの不具合ならPRで直す。**「復帰も確認できません」の通知が来たら至急**: §11.1でコンテナの状態を見て、必要なら§9.4。
- **Discordに通知が来ない**: Webhookが失効・削除された可能性。§7で作り直す。

### 11.6 SSHで入れない
鍵・IP・ufw・fail2ban（自分のIPが弾かれた）・sshdの設定を疑う。**さくらの管理画面のシリアルコンソール**から入って確認（§5）。

### 11.7 個人情報・不具合の対応（アプリ）
- 退会・個人情報の削除の依頼、公開版に残る名前の確認、画像の緊急削除（`nuctl media-redact`）、投稿者名の公開許可の撤回: [OPERATIONS](OPERATIONS.md)の該当節。
- 運用CLI（`nuctl`）は**サーバー上で運用者が手動で**実行する（Actionsからは禁止）。形式: `sudo env NU_TAG=$TAG docker compose -f /srv/nu_app/infrastructure/compose.prod.yaml --env-file /srv/nu_app/.env run --rm --no-deps --entrypoint /nuctl api <サブコマンド>`。
- Owner不在・ロックアウト: `nuctl recover-owner`（[OPERATIONS](OPERATIONS.md)「Owner緊急復旧」）。

## 12. 代表者・管理者が交代するときの引き継ぎ

**引き継ぎ前の準備（現任）**: 1) この文書の「（要記入）」を埋める。2) 後任に、下の「アカウントと権限」を渡す／招待する。3) 後任と一緒に§9.3（復元検証）と§8（承認の操作）を1回やってみる。

**アカウントと権限の一覧（交代のたびに見直す）**

| 対象 | 何の権限か | 交代の手順 |
|---|---|---|
| GitHub Organization `nu-digital` | リポジトリ・Actions・Secrets・Deploy keys・GHCR | 後任を**Owner**に追加 → 現任をOwnerから外す（Ownerが最低2人いる状態を保つ） |
| GitHub 環境`production`の必須レビュアー | デプロイの承認 | 後任を追加（現任を外す前に）。承認者が0人だとデプロイが止まる |
| GitHub PAT（GHCR読み取り） | サーバーがイメージをpull | 後任が新しいPATを作り、サーバーで`docker login ghcr.io`（§6.4）。現任のPATを削除 |
| さくらのVPS（契約者のアカウント） | サーバーの電源・シリアルコンソール・パケットフィルター・**料金** | 契約者の変更（さくらの手続き）か、アカウント情報の安全な引き継ぎ。**支払い方法を後任のものに切り替える** |
| Cloudflare | DNS | 後任をメンバーに追加（権限はDNS編集）。現任を外す |
| ドメインのレジストラ | `nu-digital.jp`の所有・更新 | 登録者情報・請求先の変更（レジストラの手続き）。**ここが最も重要（ドメインを失うと全て止まる）** |
| サーバーのSSH | `ubuntu`での管理 | §5（後任の鍵を追加→確認→現任の鍵を削除）。`ubuntu`のパスワード（シリアルコンソール用）は、後任が変更（`sudo passwd ubuntu`）。パスワード管理は組織で決めた方法（パスワードマネージャー等）で |
| Discord（運用チャンネルのWebhook・Bot） | 通知 | Webhookを後任の管理するチャンネル設定で作り直す（§7）。Botのtokenを使う場合は、Developer Portalの所有者を移す |
| アプリのOwner | 内部ポータルの最高権限 | 管理画面の二段階移譲（現Ownerが申請→24時間→後任が承諾）。緊急時は`nuctl recover-owner` |
| Claude Code等のAI | 構築・開発の補助 | `claude-nu_app-ops`鍵の削除（§5）。AIにサーバー権限を与え続けない |

**引き継ぎ後の確認**: 後任が (1) SSHで入れる、(2) Actionsの承認画面を開ける、(3) Discordの通知を受け取れる、(4) 管理画面にOwnerでログインできる、(5) この文書を読んで§8・§9・§11の手順を実行できる。去る人のアクセス（鍵・PAT・Webhook・各アカウントの権限）が**すべて削除・失効**していること。

## 13. 実機検証の結果（2026-11-26・本番ではない試験環境）

試験は本番の永続領域とは別のDockerプロジェクト（`nu_trial`・`/srv/nu_trial`）で行い、終了後にコンテナ・volume・ネットワーク・イメージ・ディレクトリを**すべて削除**した（本番のデータ・設定には触れていない）。イメージはVPS上で試験用にビルドした（本番はCIがビルドしGHCRから取得する）。

| 項目 | 結果 |
|---|---|
| ビルド（api・bot・migrate・web・admin） | 全て成功（4コアで数分） |
| 起動（`compose.prod.yaml`） | db→migrate→api→bot→web→adminの順に起動し全て healthy |
| アイドル時のメモリ | api 7MiB・web 37MiB・admin 35MiB・bot 4MiB・db 31〜35MiB（合計約120MiB）。ホスト全体の使用は約0.9GB（ページキャッシュ含む） |
| 公開サイトへ並列400リクエスト（`/blog/`・20並列） | 全て200・約6秒。web最大130MiB（上限384MiB）・api 13MiB・db 39MiB |
| 約9MBの乱数PNG（1000×3000）のアップロード・再エンコード | 201・約1.5秒。api最大約236MiB（観測。上限1GiB・GOMEMLIMIT 768MiB） |
| 既存記事の取り込み（`import-legacy --apply`、試験DB） | 新規67件・公開サイト全ルート200（トップ0.12秒・一覧0.04秒） |
| `down`→`up`（`-v`なし） | DB（記事67・画像1）・メディアのファイルが残り、公開サイトも200（永続化OK） |
| バックアップ→隔離復元検証 | バックアップ約0.6秒・復元検証約2.2秒（スキーマ12・行数・メディア整合OK）。一時コンテナは残らない |
| デプロイの予行演習（`deploy.sh`・ローカルビルドのイメージ） | 初回成功／**壊れた版（APIが起動しない）→更新前バックアップ→失敗を検知→動いていた版へ自動で戻り、公開サイト200** |
| 安全装置 | `APP_BASE_URL`がhttpで`API_INSECURE_COOKIES`が無いとAPIは起動を拒否する。Dockerのポートは127.0.0.1のみ |
| TLS・Nginx（本番） | 証明書取得（両ホスト）・自動更新の予行（dry-run）成功。HTTP→HTTPS 301・HSTS等のヘッダー・公開ホストの`/api/`が404・HTTP/2を実機で確認。**Nginx 1.24では`http2 on;`が使えない**ことを実機で発見し修正（CIのNginxも1.24.0に変更） |
| 通知 | Discord Webhookの接続テスト 204（2026-11-26） |

未確認: VPS再起動後の自動起動、GHCRからの実際のpull、本番サイズ（数百〜数千記事・画像数GB）での所要時間・容量、`nu-backup.timer`の実行（初回デプロイ後に有効化）、実際のデプロイ（未実施）。

## 14. 初回公開の手順（チェックリスト。**各項目の実行前に運用責任者の確認を取る**）

1. [ ] 本RUNBOOKの「（要記入）」を埋めた。`production`環境の承認者・secret（4つ）・GHCRトークン・Webhookを確認した。
2. [ ] リポジトリ変数`DEPLOY_ENABLED=true`にする（**自動デプロイの有効化。確認が必要**）。mainに何かマージ（または空コミット）→ `verify`・`build`が通る → `deploy`の承認待ちになる。
3. [ ] 承認してデプロイ（**初回の本番デプロイ。確認が必要**）。ヘルスチェック成功・Discordに成功通知。
4. [ ] `sudo systemctl enable --now nu-backup.timer`。手動で1回バックアップを取り、§9.3の復元検証を実行。
5. [ ] 初期Ownerの作成（`nuctl bootstrap-owner`。表示されたtokenを本人にだけ渡す）。Owner本人がパスワードを設定してログイン。
6. [ ] 既存記事の取り込み（**本番データの移行。確認が必要**）: `nuctl import-legacy`をまずdry-run（`--apply`なし）で確認 → 問題なければ`--apply`。公式サイトで全記事・画像・日付・ギャラリー・YouTubeを目視確認。
7. [ ] 管理画面の「既存記事の投稿者」で、本人の許可を確認できた投稿者だけ公開許可を記録（それまでは「nu_digital」と表示）。
8. [ ] ブラウザー・スマホで`https://nu-digital.jp`・`https://app.nu-digital.jp`を確認。証明書・リダイレクト・ログイン。
9. [ ] 公開前のセキュリティ確認: `ufw status`・`sshd -T | grep -i passwordauth`・`ss -ltnp`（80/443/22以外が外に開いていない）・Dockerのポートが127.0.0.1のみ・`/api/v1/auth/me`が公開ホストから404。
10. [ ] §15の一時設定を解除する。

## 15. 作業の履歴・一時設定

| 日付 | 内容 |
|---|---|
| 2026-11-26 | VPS構築（SSH強化・ufw・fail2ban・自動更新・Docker・ディレクトリ・deployユーザー・deploy key）、試験環境での実機検証（§13）、DNS（Cloudflare）・さくらのパケットフィルター設定、TLS証明書取得・Nginx起動、GitHub環境`production`・Secrets・GHCRログイン・Discord Webhook設定（運用責任者）。実施: 運用責任者とClaude Code（AI開発エージェント。SSHは作業用の鍵） |
| （未実施） | `DEPLOY_ENABLED=true`、初回デプロイ、バックアップタイマーの有効化、初期Owner作成、既存記事の取り込み |

**一時設定（作業完了後に解除する）**: `/etc/sudoers.d/90-nu-setup`（`ubuntu`のNOPASSWD:ALL。構築作業のため一時的に設定）。**解除すると、`ubuntu`のsudoはパスワードが必要になる**（`sudo`のパスワード＝`ubuntu`のパスワード。無い場合は、さくらのシリアルコンソールで設定してから解除する）。解除済みになったらこの行に日付を書く。
