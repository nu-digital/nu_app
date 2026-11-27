# サーバーの新規構築と移行の手順書

VPSを**新しく作る**とき（作り直し・契約変更・別事業者への引っ越し）に、この文書だけで構築・移行できることを目指した手順。**現在の本番の構成・値は [RUNBOOK](RUNBOOK.md)（実績の正本）**。この文書はそれを再現するための「順序立てたコマンド」。

> **注意**: 実機で確認済みの構成（RUNBOOK §2.1・§13）をコマンドにしたもの。**一部のコマンド（`※要確認`）は、構築時の記録が文書に残っていなかったため、一般的な方法で書いている**。移行のときは、旧サーバーで実際の設定を確認して合わせる（例: `sudo cat /etc/nu_app/backup.env`・`sudo ls -l /etc/sudoers.d/`）。各ステップに「確認」を付けたので、通らなければ先へ進まない。
> **各ステップの実行前に、運用責任者の確認を取る**（本番の切り替えを含むため）。秘密の値はチャット・Gitに書かない。

## 0. 前提・用意するもの

| 必要なもの | 備考 |
|---|---|
| VPS（Ubuntu 24.04 LTS・amd64・4コア・メモリ4GB以上・SSD 100GB以上を推奨） | 現行: さくらのVPS v5（石狩第1）。事業者固有の機能に依存しない構成（Docker Compose） |
| ドメインのDNS編集権限（Cloudflare） | 本番: `nu-digital.jp`・`app.nu-digital.jp`のAレコード（**DNS only。プロキシにしない**。[RUNBOOK §3](RUNBOOK.md)） |
| GitHub Organization `nu-digital` の管理権限 | 環境`production`・Secrets・Deploy keys・変数 |
| 管理者のSSH鍵（公開鍵） | パスフレーズ付きの鍵を推奨 |
| 秘密の保管先（パスワードマネージャー） | 下の「秘密の一覧」を保管する |
| （移行のとき）旧サーバーへのSSH・最新バックアップ | §9 |

### 秘密と設定の置き場所の一覧（値は書かない）

| 何 | 置き場所 | 用途 |
|---|---|---|
| `/srv/nu_app/.env`（root・600） | VPS | `POSTGRES_*`・`APP_BASE_URL`・`NU_REGISTRY`・`MEDIA_HOST_DIR`・ぬるの`NUL_*`（Botトークン・APIキー・DBパスワード・ID類）。雛形: `.env.production.example` |
| `/etc/nu_app/backup.env`（root・600） | VPS | バックアップ・デプロイ・証明書確認の設定と、運用チャンネルのWebhook URL。キーは§6.2 |
| `/etc/nu_app/repo_deploy_key`（root・600） | VPS | VPSがGitHubからリポジトリを読む鍵（読み取り専用のdeploy key） |
| `/root/.docker/config.json` | VPS | GHCRへのログイン（`read:packages`のPAT） |
| GitHub環境`production`のSecrets 4つ | GitHub | `DEPLOY_HOST`・`DEPLOY_USER`・`DEPLOY_SSH_KEY`・`DEPLOY_KNOWN_HOSTS`（[RUNBOOK §6.2](RUNBOOK.md)） |
| `ubuntu`のパスワード | パスワードマネージャー | `sudo`とシリアルコンソール用 |
| 各アカウント | パスワードマネージャー | さくら・Cloudflare・レジストラ・GitHub・Anthropic Console・Discord Developer Portal（[RUNBOOK §12](RUNBOOK.md)） |

## 1. OSの初期設定（`ubuntu`ユーザー）

```sh
# 1-1. 更新・タイムゾーン・必要なパッケージ
sudo apt-get update && sudo apt-get -y upgrade
sudo timedatectl set-timezone Asia/Tokyo
sudo apt-get install -y ufw fail2ban unattended-upgrades nginx certbot git curl ca-certificates
sudo dpkg-reconfigure -plow unattended-upgrades          # セキュリティ更新の自動適用を有効に

# 1-2. SSH: 鍵認証だけ（先に自分の公開鍵を ~/.ssh/authorized_keys に入れ、別セッションで入れることを確認してから）
sudo tee /etc/ssh/sshd_config.d/10-nu-hardening.conf >/dev/null <<'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers ubuntu deploy
MaxAuthTries 4
LoginGraceTime 30
EOF
sudo sshd -t && sudo systemctl reload ssh      # 別のセッションを開いたまま。新しいセッションで入れることを確認してから元を閉じる

# 1-3. ファイアウォール（さくら側のパケットフィルターでも 22・80・443 を許可しておく）
sudo ufw default deny incoming && sudo ufw default allow outgoing
sudo ufw limit 22/tcp && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp
sudo ufw --force enable

# 1-4. fail2ban（sshd）
sudo systemctl enable --now fail2ban
```

**確認**: `sudo sshd -T | grep -iE 'passwordauthentication|permitrootlogin|allowusers'`、`sudo ufw status verbose`、`sudo fail2ban-client status sshd`。`ubuntu`には**パスワードを設定**（`sudo passwd ubuntu`。`sudo`とシリアルコンソール用）。**`NOPASSWD:ALL`は付けない**。

## 2. Docker

[公式の手順](https://docs.docker.com/engine/install/ubuntu/)（apt）で Docker Engine と Compose プラグインを入れる。

```sh
sudo install -m 0755 -d /etc/apt/keyrings
sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && sudo chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo $VERSION_CODENAME) stable" | sudo tee /etc/apt/sources.list.d/docker.list
sudo apt-get update && sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
sudo tee /etc/docker/daemon.json >/dev/null <<'EOF'
{ "log-driver": "json-file", "log-opts": { "max-size": "10m", "max-file": "5" } }
EOF
sudo systemctl enable --now docker && sudo systemctl restart docker
```

**確認**: `sudo docker --version`・`sudo docker compose version`・`systemctl is-enabled docker`（enabled）。コンテナは`restart: unless-stopped`なので、再起動後に自動で戻る。

## 3. ディレクトリ・リポジトリ・GHCR

```sh
# 3-1. ディレクトリと権限（RUNBOOK §2.1）
sudo mkdir -p /srv/nu_app /srv/nu_app/media /srv/nu_app/backups /srv/nu_app/state /etc/nu_app
sudo chown 65532:65532 /srv/nu_app/media && sudo chmod 750 /srv/nu_app/media      # APIコンテナ（nonroot）が書く
sudo chmod 700 /srv/nu_app/backups /etc/nu_app

# 3-2. リポジトリを読むdeploy key（読み取り専用）。公開鍵をGitHubのリポジトリ Settings → Deploy keys に登録（Allow write accessは付けない）
sudo ssh-keygen -t ed25519 -N "" -C nu_app-vps-repo-readonly -f /etc/nu_app/repo_deploy_key
sudo cat /etc/nu_app/repo_deploy_key.pub                 # これをGitHubに登録する
# 3-3. 取得（※要確認: 旧サーバーの `sudo git -C /srv/nu_app config --get core.sshCommand` と同じ設定にする）
sudo GIT_SSH_COMMAND='ssh -i /etc/nu_app/repo_deploy_key -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new' git clone git@github.com:nu-digital/nu_app.git /srv/nu_app.tmp \
  && sudo bash -c 'cp -a /srv/nu_app.tmp/. /srv/nu_app/ && rm -rf /srv/nu_app.tmp'
sudo git -C /srv/nu_app config core.sshCommand 'ssh -i /etc/nu_app/repo_deploy_key -o IdentitiesOnly=yes'   # deploy.shのgit fetchが使う

# 3-4. GHCRへのログイン（read:packagesだけのPAT classic。標準入力に貼る。履歴・チャットに残さない）
sudo docker login ghcr.io -u <GitHubのユーザー名> --password-stdin
```

**確認**: `sudo git -C /srv/nu_app fetch --depth 1 origin main`が通る・`ls -ld /srv/nu_app/media`（`65532 65532`・750）・`sudo docker pull ghcr.io/nu-digital/nu_app-api:<最新のSHA>`が通る。GitHubの組織でDeploy keysの作成が許可されていること（[RUNBOOK §6.1](RUNBOOK.md)）。

## 4. デプロイ用ユーザー（GitHub Actionsが使う）

Actionsは、**`deploy.sh <40桁のSHA>`だけ**を実行できるSSH鍵で入る（[ssh-entry.sh](../infrastructure/deploy/ssh-entry.sh)）。

```sh
sudo adduser --disabled-password --gecos "" deploy
# Actions専用の鍵ペアは、手元のPCで作る: ssh-keygen -t ed25519 -N "" -C nu_app-actions-deploy -f nu_app_actions
sudo -u deploy mkdir -p /home/deploy/.ssh && sudo chmod 700 /home/deploy/.ssh
echo 'restrict,command="/srv/nu_app/infrastructure/deploy/ssh-entry.sh" ssh-ed25519 AAAA…（Actions専用の公開鍵） nu_app-actions-deploy' | sudo -u deploy tee /home/deploy/.ssh/authorized_keys
sudo chmod 600 /home/deploy/.ssh/authorized_keys
# sudoersは、この1行だけ（※要確認: 旧サーバーの /etc/sudoers.d/20-nu-deploy と同じか）
echo 'deploy ALL=(root) NOPASSWD: /srv/nu_app/infrastructure/deploy/deploy.sh *' | sudo tee /etc/sudoers.d/20-nu-deploy && sudo chmod 440 /etc/sudoers.d/20-nu-deploy && sudo visudo -cf /etc/sudoers.d/20-nu-deploy
```

**確認**: 手元から `ssh -i nu_app_actions deploy@<IP> id` が**拒否**される（`このSSH鍵で許可されているのは deploy.sh …だけ`）こと。ホスト鍵の指紋を、さくらのシリアルコンソールで `ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` と照合してから、`ssh-keyscan -t ed25519 <IP>`の結果を`DEPLOY_KNOWN_HOSTS`に登録する（§7）。

## 5. Nginx・証明書・systemdのタイマー

```sh
# 5-1. Nginx（Ubuntuのパッケージ 1.24。`http2 on;`は使えない。`listen 443 ssl http2;`）。リポジトリの設定を置く
sudo cp /srv/nu_app/infrastructure/nginx/nu_app.conf /etc/nginx/conf.d/nu_app.conf
sudo mkdir -p /var/www/certbot
# 証明書の取得前は443のserverが証明書ファイルを読めず nginx -t が失敗する。先に80番だけの設定にして取得し、その後に443を有効にする（§9 の手順）

# 5-2. 証明書（DNSがこのサーバーを指してから）。webroot方式・別々の証明書・メールなし登録（※方針: 期限は nu-cert-check が通知）
sudo certbot certonly --webroot -w /var/www/certbot -d nu-digital.jp --register-unsafely-without-email --agree-tos
sudo certbot certonly --webroot -w /var/www/certbot -d app.nu-digital.jp
sudo install -m 755 /srv/nu_app/infrastructure/nginx/certbot-deploy-hook.sh /etc/letsencrypt/renewal-hooks/deploy/nu-nginx-reload.sh
sudo nginx -t && sudo systemctl reload nginx

# 5-3. systemdのタイマー: バックアップ(毎日03:00 JST)・証明書の期限確認(毎日09:00 JST)
sudo cp /srv/nu_app/infrastructure/backup/systemd/nu-backup.{service,timer} /srv/nu_app/infrastructure/nginx/systemd/nu-cert-check.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
# 有効化は、初回デプロイが済んでから（DBが無いとバックアップが失敗して通知が出る）: §8
```

**確認**: `sudo nginx -t`・`curl -sI https://nu-digital.jp/`（200）・`curl -sI http://nu-digital.jp/`（301）・`sudo certbot renew --dry-run`・`systemctl list-timers | grep certbot`。Nginxの設定を変えるときは`scripts/nginx-test.sh`（CIの`ops / nginx`）を通してからサーバーへ。

## 6. 設定ファイル

### 6.1 `/srv/nu_app/.env`（root・600）

`.env.production.example`をコピーして、実際の値を**運用責任者が自分で**書く。

```sh
sudo install -m 600 /srv/nu_app/.env.production.example /srv/nu_app/.env
sudo nano /srv/nu_app/.env     # POSTGRES_PASSWORD は十分に長いランダム値（英数字と - _ . ~ のみ）。APP_BASE_URL=https://app.nu-digital.jp
```

ぬるの`NUL_*`（Botトークン・APIキー・DBパスワード・ID）は、**ぬるを有効にするときに**追記する（[NUL_LAUNCH §6](NUL_LAUNCH.md)）。無ければ、ぬるは無効モードで起動する（他に影響しない）。

### 6.2 `/etc/nu_app/backup.env`（root・600）

バックアップ・デプロイ・証明書確認のスクリプトが読む。**キー名は次のとおり**（値は環境に合わせる。※要確認: 旧サーバーの実際のファイルと突き合わせる）。

```
BACKUP_ROOT=/srv/nu_app/backups
MEDIA_DIR=/srv/nu_app/media
COMPOSE_FILE=/srv/nu_app/infrastructure/compose.prod.yaml
COMPOSE_ENV_FILE=/srv/nu_app/.env
RETENTION=7
MIN_FREE_MB=1024
HOST_LABEL=nu_app
BACKUP_NOTIFY_WEBHOOK_URL=（Discordの運用チャンネルのWebhook URL。秘密）
```

`BACKUP_NOTIFY_WEBHOOK_URL`は、Discordの運用チャンネルの「連携サービス → ウェブフック」で作る。接続テストは [RUNBOOK §7](RUNBOOK.md)。

## 7. GitHubの設定（運用責任者）

[RUNBOOK §6](RUNBOOK.md)の表に沿って、次を行う（サーバーを作り直す・IPが変わるときは**更新**）。

1. 環境`production`（Settings → Environments）の**必須レビュアー**＝承認者を確認（0人だとデプロイが止まる）。
2. 環境のSecrets: `DEPLOY_HOST`（新しいIP）・`DEPLOY_USER`（`deploy`）・`DEPLOY_SSH_KEY`（§4のActions専用の秘密鍵の全文）・`DEPLOY_KNOWN_HOSTS`（§4で照合した1行）。
3. リポジトリの変数`DEPLOY_ENABLED=true`（自動デプロイを有効にする。**確認のうえで**）。

## 8. 初回デプロイ・バックアップの有効化

1. mainに何かマージする（または、Actions → deploy → Run workflow で現在のmainのSHAを指定）→ `verify`・`build`が通る → **承認待ち**。
2. 運用責任者が**Approve and deploy**。VPSで`deploy.sh`が走り、migration・各サービスの起動・ヘルスチェックが済む。失敗したら前の版へ戻る（初回は戻る先がないので、失敗として残る。原因はActionsのログで見る）。
3. バックアップを有効化し、1回取って、復元検証まで確認する:
   ```sh
   sudo systemctl enable --now nu-backup.timer nu-cert-check.timer
   sudo systemctl start nu-backup.service && journalctl -u nu-backup -n 30
   sudo env POSTGRES_IMAGE='postgres:18.7@sha256:eb63372c1e05c755f55fa30cbd07aecc443676fc6c0fa90b9002d01537841e1a' NUCTL_IMAGE="ghcr.io/nu-digital/nu_app-api:$(sudo cat /srv/nu_app/state/current_tag)" /srv/nu_app/infrastructure/backup/restore-verify.sh
   ```
4. 初期Ownerを作る（**新規構築のとき**。移行ではバックアップから戻るので不要）: `nuctl bootstrap-owner`（[RUNBOOK §14](RUNBOOK.md)）。

## 9. 別のサーバーへ移行する（データを引き継ぐ）

**考え方**: 旧サーバーで書き込みを止めて最後のバックアップを取り → 新サーバーに構築（§1〜§6）→ バックアップから復元 → DNSを切り替え → 証明書を取得 → 確認 → 旧サーバーを止める。**切り替え中は公開サイトとポータルが短時間（数十分）止まる**ので、利用者に周知する。ダウンタイムを小さくするため、**DNSのTTLを事前に短く（300秒）**しておく。

### 9.1 事前（数日前）

- [ ] 新サーバーで§1〜§6まで構築済み（Nginxは80番だけ・コンテナは未起動でよい）。
- [ ] Cloudflareの2つのAレコードのTTLを300秒に下げる。
- [ ] 旧サーバーの`/etc/nu_app/backup.env`・`/srv/nu_app/.env`の内容を、パスワードマネージャーから新サーバーへ**手で**写す（ファイル自体をチャットに貼らない）。ぬるの`NUL_*`も同じ値（Botトークンは同じものを使うので、**旧のagentを止めるまで新のagentは起動しない**）。

### 9.2 切り替え当日

1. **旧サーバーで書き込みを止める**: `compose stop web admin api bot agent`（`NU_TAG=$(cat /srv/nu_app/state/current_tag)`が要る。RUNBOOK §8）。**agentを止めるのは必須**（ぬるが旧と新の両方でDiscordに接続しないように）。
2. 旧サーバーで**最後のバックアップ**: `sudo systemctl start nu-backup.service` → 最新の世代（`/srv/nu_app/backups/LATEST`）を確認（`COMPLETE`がある）。
3. 世代のディレクトリを新サーバーへ運ぶ（VPS外バックアップがないので手動）: 手元のPCを経由して`scp -r`（個人情報を含むので、運ぶ間・運んだ後のコピーの扱いに注意し、終わったら手元のコピーを消す）。
4. 新サーバーでDBを起動して復元（[RUNBOOK §9.4](RUNBOOK.md)）。リポジトリを旧と同じSHAに合わせてから、rootで実行する:
   ```sh
   sudo -i
   T=<旧サーバーの /srv/nu_app/state/current_tag と同じ40桁のSHA>
   git -C /srv/nu_app fetch --depth 1 origin $T && git -C /srv/nu_app checkout --detach $T
   set -a; . /srv/nu_app/.env; set +a
   NC="env NU_TAG=$T docker compose -p nu_app -f /srv/nu_app/infrastructure/compose.prod.yaml --env-file /srv/nu_app/.env"
   $NC up -d db
   # 先にロールを作る（ダンプにはロールが含まれない）
   $NC exec -T db psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 < /srv/nu_app/infrastructure/backup/roles.sql
   $NC exec -T db pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" --no-owner --clean --if-exists < <世代のディレクトリ>/db.dump
   tar -xzf <世代のディレクトリ>/media.tar.gz -C /srv/nu_app/media && chown -R 65532:65532 /srv/nu_app/media
   exit
   ```
5. **ぬるのDBパスワードを再設定**（ダンプにロールのパスワードは含まれない）: [RUNBOOK §16.2](RUNBOOK.md)の`ALTER ROLE nu_agent LOGIN PASSWORD …`。
6. 新サーバーで**起動**: GitHub Secretsを新しいIPに更新（§7）→ Actions → deploy → Run workflow（旧と同じSHA）→ 承認。`nuctl media-verify`で画像とDBの整合を確認（[RUNBOOK §9](RUNBOOK.md)）。
7. **ぬるの状態を確認**: 設定（`control.mode`・`intro.posted`など）はDBに残っている。**まず`stopped`にしてから起動**し、`/nul status`で記録件数が旧と合うこと・API使用額の記録が引き継がれていること（`select count(*) from agent.api_usage`）を確認してから、`/nul quiet`→`/nul resume`と戻す（[NUL_LAUNCH](NUL_LAUNCH.md)）。
8. **DNSを切り替える**: Cloudflareの2つのAレコードを新しいIPへ（**DNS only**のまま）。反映を待つ（`dig +short nu-digital.jp`）。
9. **証明書を取得**（§5-2）して、443のserver設定を有効にし、`nginx -t`→再読み込み。ブラウザー・スマホで`https://nu-digital.jp`・`https://app.nu-digital.jp`・ログイン・画像・公式サイトの記事を確認。
10. §8の3（バックアップのタイマー有効化と復元検証）を新サーバーで実施。

### 9.3 事後

- [ ] 旧サーバーは**すぐには消さず**、数日は停止のまま残す（切り戻し用）。確認が済んだら、旧サーバーのagent・バックアップ・`.env`・鍵を**消してから**契約を解約する（個人情報が残らないように）。
- [ ] TTLを元に戻す。RUNBOOKの「（要記入）」（IP・契約情報）と[COSTS_AND_CONTRACTS](COSTS_AND_CONTRACTS.md)を更新する。さくらの管理画面のパケットフィルターを新サーバーにも設定したか確認。
- [ ] 旧サーバー用のSSH鍵・`DEPLOY_KNOWN_HOSTS`の古い値を削除する。

### 9.4 切り戻し（移行が失敗したとき）

DNSを旧サーバーのIPへ戻し（TTLが短いので数分）、旧サーバーのコンテナを`up -d`で再開する（旧のagentを再開してよいのは、新のagentが止まっているときだけ）。新サーバーのagentは必ず止める。

## 10. 構築・移行の完了条件（チェックリスト）

- [ ] `https://nu-digital.jp`・`https://app.nu-digital.jp`がHTTP/2・証明書OKで開き、`http://`は301。公開ホストの`/api/v1/auth/me`が404。
- [ ] 管理画面にOwnerでログインでき、記事・画像・物品が表示される。`nuctl media-verify`が「食い違いなし」。
- [ ] `ufw status`・`sshd -T`・`ss -ltnp`（外に開いているのは22・80・443だけ。Dockerのポートは127.0.0.1のみ）。
- [ ] `systemctl list-timers`に`nu-backup`・`nu-cert-check`・`certbot`。手動バックアップ→`restore-verify.sh`が「OK」。
- [ ] Discordの運用チャンネルに通知が届く（接続テスト204）。Actionsの承認画面を、承認者が開ける。
- [ ] ぬる: `/nul status`が期待どおり。`agent.messages`・`api_usage`の件数が移行前と合う。**旧サーバーのagentが止まっている**。
- [ ] [RUNBOOK](RUNBOOK.md)の事実（IP・日付・作業の履歴）を更新した。
