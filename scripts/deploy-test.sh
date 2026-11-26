#!/usr/bin/env bash
# infrastructure/deploy/deploy.sh のテスト。docker・curl・git を偽物（呼び出しを記録するスクリプト）に差し替えて、本番には何も触れずに挙動を確認する。
#   不正な引数・直列化・初回/通常の成功・取得失敗・バックアップ失敗・ヘルスチェック失敗での前版復帰・通知に秘密を含めない
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"; trap 'rm -rf -- "$tmp"' EXIT
pass=0; failed=0
check() { if [ "$2" = "0" ]; then pass=$((pass+1)); echo "OK  $1"; else failed=$((failed+1)); echo "NG  $1"; fi; }
mkdir -p "$tmp/bin" "$tmp/app/infrastructure/deploy" "$tmp/app/state"
export LOG="$tmp/calls.log" STUB="$tmp/stub"; mkdir -p "$STUB"
# 偽のdocker: 呼び出しを記録。`up` は NU_TAG を「動いている版」として保存。`pull` はFAKE_PULL_FAILで失敗
cat > "$tmp/bin/docker" <<'S'
#!/usr/bin/env bash
echo "docker NU_TAG=${NU_TAG:-} $*" >> "$LOG"
case " $* " in
  *" pull "*) [ -z "${FAKE_PULL_FAIL:-}" ] || exit 1 ;;
  *" up "*) echo "${NU_TAG:-}" > "$STUB/running" ;;
esac
exit 0
S
# 偽のcurl: 通知（POST）は本文を記録。ヘルスチェックは、動いている版がFAKE_BAD_TAGなら失敗
cat > "$tmp/bin/curl" <<'S'
#!/usr/bin/env bash
if printf '%s\n' "$*" | grep -q -- "-d "; then
  for a in "$@"; do last="$a"; done
  printf '%s\n' "$*" | sed 's/.*-d //' >> "$NOTIFY_LOG"
  exit 0
fi
[ -n "${FAKE_BAD_TAG:-}" ] && [ "$(cat "$STUB/running" 2>/dev/null)" = "$FAKE_BAD_TAG" ] && exit 22
exit 0
S
cat > "$tmp/bin/git" <<'S'
#!/usr/bin/env bash
echo "git $*" >> "$LOG"; exit 0
S
cat > "$tmp/backup.sh" <<'S'
#!/usr/bin/env bash
echo "backup DEPLOYED_TAG=${DEPLOYED_TAG:-}" >> "$LOG"; [ -z "${FAKE_BACKUP_FAIL:-}" ] || exit 1
S
chmod +x "$tmp/bin/"* "$tmp/backup.sh"
export NOTIFY_LOG="$tmp/notify.log"
export PATH="$tmp/bin:$PATH" APP_DIR="$tmp/app" STATE_DIR="$tmp/app/state" COMPOSE_FILE=/dev/null ENV_FILE=/dev/null BACKUP_SCRIPT="$tmp/backup.sh" SKIP_GIT=1 HEALTH_TIMEOUT=2 HOST_LABEL=test
export DEPLOY_NOTIFY_WEBHOOK_URL="https://discord.example/api/webhooks/SECRET-TOKEN-123"
A=$(printf 'a%.0s' $(seq 1 40)); B=$(printf 'b%.0s' $(seq 1 40)); C=$(printf 'c%.0s' $(seq 1 40))
reset() { : > "$LOG"; : > "$NOTIFY_LOG"; unset FAKE_PULL_FAIL FAKE_BAD_TAG FAKE_BACKUP_FAIL; }
deploy() { "$root/infrastructure/deploy/deploy.sh" "$@" >"$tmp/out.log" 2>&1; }
set +e

reset; deploy "abc"; [ $? = 2 ]; check "短い・不正な引数は終了コード2" $?
[ ! -s "$LOG" ]; check "不正な引数ではdockerを呼ばない" $?
reset; deploy "$(printf 'a%.0s' $(seq 1 39))g"; [ $? = 2 ]; check "16進以外の文字は拒否" $?
reset; deploy "$A; rm -rf /"; [ $? = 2 ]; check "コマンドを混ぜた引数は拒否" $?

reset; deploy "$A"; rc=$?
[ $rc = 0 ]; check "初回のデプロイが成功する" $?
[ "$(cat "$STATE_DIR/current_tag")" = "$A" ]; check "成功したらcurrent_tagを記録する" $?
! grep -q "backup" "$LOG"; check "初回（前の版が無い）はバックアップを呼ばない" $?
grep -q "pull" "$LOG" && grep -q "up -d" "$LOG"; check "pull→upの順で呼ぶ" $?
grep -q "デプロイに成功しました" "$NOTIFY_LOG"; check "成功が通知される" $?

reset; deploy "$B"; rc=$?
[ $rc = 0 ] && [ "$(cat "$STATE_DIR/current_tag")" = "$B" ] && [ "$(cat "$STATE_DIR/previous_tag")" = "$A" ]; check "2回目の成功で、current=新・previous=旧になる" $?
grep -q "backup DEPLOYED_TAG=$A" "$LOG"; check "更新前に（動いていた版を記録して）バックアップを取る" $?
[ "$(grep -n backup "$LOG" | head -1 | cut -d: -f1)" -lt "$(grep -n ' up ' "$LOG" | head -1 | cut -d: -f1)" ]; check "バックアップはupより前" $?

reset; FAKE_BAD_TAG="$C" deploy "$C"; rc=$?
[ $rc != 0 ]; check "ヘルスチェックに失敗すると終了コードが非0" $?
[ "$(cat "$STATE_DIR/current_tag")" = "$B" ]; check "失敗してもcurrent_tagは動いていた版のまま" $?
[ "$(cat "$STUB/running")" = "$B" ]; check "動いていた版（B）のイメージへ戻す" $?
grep -q "前の版（${B:0:12}）へ戻しました" "$NOTIFY_LOG"; check "失敗と前版への復帰が通知される" $?
! grep -q "デプロイに成功しました" "$NOTIFY_LOG"; check "失敗時に成功通知を送らない" $?

reset; FAKE_PULL_FAIL=1 deploy "$A"; rc=$?
[ $rc != 0 ] && [ "$(cat "$STATE_DIR/current_tag")" = "$B" ]; check "イメージの取得に失敗したらcurrentを変えない" $?
! grep -q ' up ' "$LOG"; check "取得に失敗したらupしない（動いている版に触れない）" $?
grep -q "デプロイを中止しました（イメージの取得）" "$NOTIFY_LOG"; check "取得失敗が通知される" $?

reset; FAKE_BACKUP_FAIL=1 deploy "$A"; rc=$?
[ $rc != 0 ] && [ "$(cat "$STATE_DIR/current_tag")" = "$B" ]; check "バックアップに失敗したら更新しない" $?
! grep -q ' up ' "$LOG"; check "バックアップに失敗したらupしない" $?

reset; ( exec 9>"$STATE_DIR/.lock"; flock -n 9; deploy "$A"; echo $? > "$tmp/lockrc" ) 2>/dev/null
[ "$(cat "$tmp/lockrc")" = 75 ]; check "別のデプロイ（ロック）が実行中なら終了コード75で何もしない" $?
[ ! -s "$LOG" ]; check "ロック中はdockerを呼ばない" $?

! grep -qE "SECRET-TOKEN|webhooks|https://" "$NOTIFY_LOG"; check "通知の本文にWebhookのURL・トークンが含まれない" $?
! grep -qE "SECRET-TOKEN" "$tmp/out.log"; check "ログにWebhookのトークンが出ない" $?

# 運用設定ファイル（NU_ENV_FILE）の通知先が使われる
reset; printf 'DEPLOY_NOTIFY_WEBHOOK_URL=https://discord.example/api/webhooks/FROM-ENV-FILE\n' > "$tmp/ops.env"
( unset DEPLOY_NOTIFY_WEBHOOK_URL; export NU_ENV_FILE="$tmp/ops.env"; deploy "$C" ); grep -q "デプロイに成功しました" "$NOTIFY_LOG"; check "運用設定ファイルの通知先でも通知できる" $?
grep -q "FROM-ENV-FILE" "$NOTIFY_LOG"; check "通知は運用設定ファイルのURLへ送られる" $?

# SSH鍵の入口（ssh-entry.sh）: deploy.sh <40桁SHA> 以外は実行できない
mkdir -p "$tmp/sshbin"; printf '#!/bin/sh\necho "SUDO-CALLED $*"\n' > "$tmp/sshbin/sudo"; chmod +x "$tmp/sshbin/sudo"
entry() { SSH_ORIGINAL_COMMAND="$1" PATH="$tmp/sshbin:$PATH" "$root/infrastructure/deploy/ssh-entry.sh" 2>&1; }
out=$(entry "sudo /srv/nu_app/infrastructure/deploy/deploy.sh $A"); [ "$out" = "SUDO-CALLED /srv/nu_app/infrastructure/deploy/deploy.sh $A" ]; check "SSH入口: 許可された形式だけがdeploy.shへ渡る" $?
for bad in "" "bash" "sudo /srv/nu_app/infrastructure/deploy/deploy.sh $A; id" "sudo /srv/nu_app/infrastructure/deploy/deploy.sh abc" "sudo /srv/nu_app/infrastructure/deploy/deploy.sh $A $A" "sudo /bin/sh" "sudo /srv/nu_app/infrastructure/deploy/deploy.sh ../$A"; do
  out=$(entry "$bad"); rc=$?; [ $rc = 126 ] && ! echo "$out" | grep -q SUDO-CALLED; check "SSH入口: 拒否される: ${bad:-（空）}" $?
done

echo; echo "合格 $pass・失敗 $failed"; [ "$failed" = 0 ]
