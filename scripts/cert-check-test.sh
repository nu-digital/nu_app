#!/usr/bin/env bash
# infrastructure/nginx/cert-check.sh のテスト（自己署名の証明書と擬似のWebhookで。本番には何もしない）
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"; hook_pid=""
trap '[ -n "$hook_pid" ] && kill "$hook_pid" 2>/dev/null; rm -rf -- "$tmp"' EXIT
pass=0; failed=0
check() { if [ "$2" = "0" ]; then pass=$((pass+1)); echo "OK  $1"; else failed=$((failed+1)); echo "NG  $1"; fi; }
port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')
python3 - "$port" "$tmp/hook.log" <<'PY' &
import sys, http.server
port, path = int(sys.argv[1]), sys.argv[2]
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        open(path, 'ab').write(self.rfile.read(int(self.headers.get('content-length', 0))) + b'\n'); self.send_response(204); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', port), H).serve_forever()
PY
hook_pid=$!; sleep 1
mk() { mkdir -p "$tmp/$1"; openssl req -x509 -newkey rsa:2048 -nodes -days "$2" -subj "/CN=$1" -keyout "$tmp/$1/k.pem" -out "$tmp/$1/fullchain.pem" 2>/dev/null; }
mk long.example 90; mk short.example 5
export NOTIFY_URL="http://127.0.0.1:$port/hook" HOST_LABEL=test WARN_DAYS=14
set +e
CERT_FILES="$tmp/long.example/fullchain.pem" "$root/infrastructure/nginx/cert-check.sh" 2>/dev/null; check "残り90日は問題なし（終了コード0・通知なし）" $?
[ ! -s "$tmp/hook.log" ]; check "問題が無いときは通知しない" $?
CERT_FILES="$tmp/short.example/fullchain.pem" "$root/infrastructure/nginx/cert-check.sh" 2>/dev/null; [ $? = 1 ]; check "残り5日は警告（終了コード1）" $?
grep -q "証明書の期限が近い" "$tmp/hook.log"; check "期限が近いと通知される" $?
: > "$tmp/hook.log"
CERT_FILES="$tmp/none/fullchain.pem" "$root/infrastructure/nginx/cert-check.sh" 2>/dev/null; [ $? = 1 ]; check "証明書が読めないと警告（終了コード1）" $?
grep -q "証明書を読めません" "$tmp/hook.log"; check "読めないと通知される" $?
! grep -qiE "BEGIN|PRIVATE|webhook|http://" "$tmp/hook.log"; check "通知に鍵・URLなどが含まれない" $?
: > "$tmp/hook.log"
CERT_FILES="" PROBE_HOSTS="127.0.0.1:1" "$root/infrastructure/nginx/cert-check.sh" 2>/dev/null; [ $? = 1 ]; check "配信中の証明書を確認できないと警告" $?
grep -q "配信中の証明書を確認できません" "$tmp/hook.log"; check "配信側の確認不能が通知される" $?
echo; echo "合格 $pass・失敗 $failed"; [ "$failed" = 0 ]
