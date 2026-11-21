#!/usr/bin/env bash
# バックアップ・復元検証（infrastructure/backup）の自動テスト。使い捨てのDB（開発用Compose）とテスト用の画像ファイルで、実際に動かす。
#   成功・世代の保持（失敗で消さない／成功後に古いものだけ消す）・DB停止での失敗と通知・壊れた世代の検出・行とファイルの食い違いの検出
# 使い方: scripts/backup-restore-test.sh   （Docker必須。本番には何もしない）
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
proj="nu_backup_test_$$"
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
pass=0; failed=0
check() { if [ "$2" = "0" ]; then pass=$((pass+1)); echo "OK  $1"; else failed=$((failed+1)); echo "NG  $1"; for f in "$tmp"/*.log; do [ -s "$f" ] && { echo "--- $(basename "$f")"; tail -8 "$f"; }; done; fi; }
cleanup() {
  docker compose -p "$proj" -f "$root/infrastructure/compose.yaml" --env-file "$tmp/.env" down -v --remove-orphans >/dev/null 2>&1 || true
  [ -n "${hook_pid:-}" ] && kill "$hook_pid" 2>/dev/null || true
  rm -rf -- "$tmp"
}
trap cleanup EXIT

cat > "$tmp/.env" <<ENV
POSTGRES_USER=nu_app
POSTGRES_DB=nu_app
POSTGRES_PASSWORD=backup-test-only-not-secret
DB_PORT=$(free_port)
API_PORT=$(free_port)
WEB_PORT=$(free_port)
ADMIN_PORT=$(free_port)
ENV
export COMPOSE_FILE="$root/infrastructure/compose.yaml" COMPOSE_ENV_FILE="$tmp/.env" COMPOSE_PROJECT="$proj"
dc() { docker compose -p "$proj" -f "$COMPOSE_FILE" --env-file "$tmp/.env" "$@"; }

echo "== 準備: DB・migration・APIイメージ"
dc up -d --build --wait db >/dev/null
dc build api >/dev/null
dc run --rm migrate >/dev/null 2>&1 || dc up migrate >/dev/null 2>&1
export NUCTL_IMAGE="${proj}-api"
export POSTGRES_IMAGE="$(dc config --format json | python3 -c 'import json,sys; print(json.load(sys.stdin)["services"]["db"]["image"])')"
psql() { dc exec -T db psql -U nu_app -d nu_app -Atc "$1"; }

# 試験データ: 架空のユーザー・画像1枚（行とファイルが対応）
mkdir -p "$tmp/media/ab"
id="abababab-abab-abab-abab-abababababab"
printf 'dummy-webp-bytes' > "$tmp/media/ab/$id.webp"
psql "INSERT INTO users (login_id, status) VALUES ('backup.user','invited')" >/dev/null
psql "INSERT INTO media_assets (id, public_id, storage_key, mime_type, byte_size, width, height, sha256, purpose, uploaded_by) VALUES ('$id','AAAAAAAAAAAAAAAAAAAAAA','ab/$id.webp','image/webp',16,10,10,repeat('a',64),'cms',(SELECT id FROM users LIMIT 1))" >/dev/null

# 通知の受け口（受け取った本文をファイルへ）
hook_port=$(free_port)
python3 - "$hook_port" "$tmp/hook.log" <<'PY' &
import sys, http.server
port, path = int(sys.argv[1]), sys.argv[2]
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get('content-length', 0)); open(path, 'ab').write(self.rfile.read(n) + b'\n')
        self.send_response(204); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', port), H).serve_forever()
PY
hook_pid=$!
sleep 1
export BACKUP_ROOT="$tmp/backups" MEDIA_DIR="$tmp/media" RETENTION=2 MIN_FREE_MB=1 HOST_LABEL=test BACKUP_NOTIFY_WEBHOOK_URL="http://127.0.0.1:$hook_port/hook"
gens() { find "$BACKUP_ROOT" -mindepth 2 -maxdepth 2 -name COMPLETE -printf '%h\n' | xargs -r -n1 basename | sort; }

set +e   # 以降は、失敗する場合も含めて終了コードを自分で検査する
echo "== バックアップ"
"$root/infrastructure/backup/backup.sh" 2>"$tmp/b1.log"; rc=$?; check "バックアップが成功する" $rc
g1="$(gens | tail -n1)"
[ "$(gens | wc -l)" = 1 ] && [ -f "$BACKUP_ROOT/$g1/db.dump" ] && [ -f "$BACKUP_ROOT/$g1/media.tar.gz" ] && [ -f "$BACKUP_ROOT/$g1/SHA256SUMS" ] && [ -f "$BACKUP_ROOT/$g1/manifest.txt" ]; check "世代にDB・メディア・チェックサム・マニフェストがある" $?
[ "$(readlink "$BACKUP_ROOT/LATEST")" = "$g1" ]; check "LATESTが新しい世代を指す" $?
[ "$(stat -c %a "$BACKUP_ROOT/$g1")" = "700" ]; check "世代の権限が700（他のユーザーが読めない）" $?
grep -q '^media_files=1$' "$BACKUP_ROOT/$g1/manifest.txt"; check "マニフェストにメディアのファイル数がある" $?
! find "$BACKUP_ROOT" -name '.work-*' | grep -q .; check "作業領域が残らない" $?

echo "== 復元検証（隔離環境）"
"$root/infrastructure/backup/restore-verify.sh" 2>"$tmp/r1.log"; rc=$?; check "隔離環境への復元と整合の確認が成功する" $rc
grep -q "本番には触れていません" "$tmp/r1.log"; check "復元検証の結果に、本番に触れていないことが出る" $?
! docker ps -a --format '{{.Names}}' | grep -q '^nu-restore-'; check "復元検証の一時コンテナが残らない" $?

echo "== 世代の保持（成功後にだけ古いものを消す）"
sleep 1; "$root/infrastructure/backup/backup.sh" 2>/dev/null; sleep 1; "$root/infrastructure/backup/backup.sh" 2>/dev/null
[ "$(gens | wc -l)" = 2 ]; check "保持数（2）を超えた古い世代が消える" $?
! gens | grep -q "^$g1$"; check "消えたのは最も古い世代" $?
before="$(gens | tr '\n' ' ')"

echo "== 失敗（DB停止）: 既存の世代は消えず、通知される"
dc stop db >/dev/null 2>&1
: > "$tmp/hook.log"
"$root/infrastructure/backup/backup.sh" 2>"$tmp/bf.log"; rc=$?
[ "$rc" != 0 ]; check "DBが止まっているとバックアップは失敗する" $?
[ "$(gens | tr '\n' ' ')" = "$before" ]; check "失敗しても既存の世代は変わらない" $?
! find "$BACKUP_ROOT" -name '.work-*' | grep -q .; check "失敗した作業領域は消える" $?
grep -q "バックアップに失敗しました" "$tmp/hook.log"; check "失敗がWebhookに通知される" $?
! grep -qiE "password|backup-test-only|postgres://" "$tmp/hook.log"; check "通知に秘密が含まれない" $?
dc start db >/dev/null 2>&1; dc up -d --wait db >/dev/null 2>&1

echo "== 壊れた世代・食い違いの検出"
latest="$(gens | tail -n1)"
cp -a "$BACKUP_ROOT/$latest" "$BACKUP_ROOT/corrupt-copy"
printf 'X' | dd of="$BACKUP_ROOT/corrupt-copy/media.tar.gz" bs=1 seek=20 conv=notrunc 2>/dev/null
"$root/infrastructure/backup/restore-verify.sh" "$BACKUP_ROOT/corrupt-copy" 2>/dev/null; rc=$?
[ "$rc" != 0 ]; check "チェックサムが合わない世代は復元検証が失敗する" $?
rm -rf "$BACKUP_ROOT/corrupt-copy"
rm -f "$tmp/media/ab/$id.webp"   # DBの行はあるのにファイルが無い状態でバックアップを取る
sleep 1; "$root/infrastructure/backup/backup.sh" 2>/dev/null
"$root/infrastructure/backup/restore-verify.sh" 2>/dev/null; rc=$?
[ "$rc" != 0 ]; check "DBの行とメディアのファイルの食い違いを復元検証が検出する" $?

echo
echo "合格 $pass・失敗 $failed"
[ "$failed" = 0 ]
