#!/usr/bin/env bash
# infrastructure/nginx/nu_app.conf の実際の挙動を、使い捨てのNginxコンテナと擬似のバックエンドで確認する（本番には何もしない）。
#   公開ホストが画像の公開配信以外のAPIを出さない・GET/HEADだけ・Cookieを渡さない・XFFを上書き・12MiB上限・HTTP→HTTPS・HSTS・キャッシュしない
# 使い方: scripts/nginx-test.sh   （Docker・python3・openssl・curl必須）
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NGINX_IMAGE="nginx:1.30.6-alpine@sha256:901e593c95dc85f5a729f5765516321366ef4ac401821431d408897e95287d0a"
tmp="$(mktemp -d)"
cid=""; pids=()
cleanup() { [ -n "$cid" ] && docker rm -f "$cid" >/dev/null 2>&1 || true; for p in "${pids[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done; rm -rf -- "$tmp"; }
trap cleanup EXIT
free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }
pass=0; failed=0
check() { if [ "$2" = "0" ]; then pass=$((pass+1)); echo "OK  $1"; else failed=$((failed+1)); echo "NG  $1"; fi; }

P_API=$(free_port); P_WEB=$(free_port); P_ADMIN=$(free_port); P_HTTP=$(free_port); P_HTTPS=$(free_port)
# 擬似のバックエンド: 受け取ったリクエスト行とヘッダーをそのまま本文に返す（どのバックエンドかも示す）
cat > "$tmp/stub.py" <<'PY'
import sys, http.server
name, port = sys.argv[1], int(sys.argv[2])
class H(http.server.BaseHTTPRequestHandler):
    def _echo(self):
        n = int(self.headers.get('content-length') or 0)
        if n: self.rfile.read(n)
        body = (f"BACKEND={name}\n{self.command} {self.path}\n" + "".join(f"{k}: {v}\n" for k, v in self.headers.items())).encode()
        self.send_response(200); self.send_header('content-length', str(len(body))); self.send_header('X-Powered-By', 'stub'); self.end_headers()
        if self.command != 'HEAD': self.wfile.write(body)
    do_GET = do_POST = do_HEAD = do_PUT = do_DELETE = _echo
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(('127.0.0.1', port), H).serve_forever()
PY
for pair in "api:$P_API" "web:$P_WEB" "admin:$P_ADMIN"; do python3 "$tmp/stub.py" "${pair%%:*}" "${pair##*:}" & pids+=($!); done

# 自己署名証明書（試験用）。設定のポート・証明書のパスだけを差し替えて、他は本番の設定のまま使う
for h in nu-digital.jp app.nu-digital.jp; do
  mkdir -p "$tmp/le/live/$h"
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj "/CN=$h" -keyout "$tmp/le/live/$h/privkey.pem" -out "$tmp/le/live/$h/fullchain.pem" 2>/dev/null
done
sed -e "s#listen 80;#listen $P_HTTP;#; s#listen \[::\]:80;#listen [::]:$P_HTTP;#; s#listen 443 ssl;#listen $P_HTTPS ssl;#; s#listen \[::\]:443 ssl;#listen [::]:$P_HTTPS ssl;#" \
    -e "s#127.0.0.1:8080#127.0.0.1:$P_API#g; s#127.0.0.1:8081#127.0.0.1:$P_WEB#g; s#127.0.0.1:3000#127.0.0.1:$P_ADMIN#g" \
    -e "s#/etc/letsencrypt#/le#g" "$root/infrastructure/nginx/nu_app.conf" > "$tmp/nu_app.conf"
cat > "$tmp/nginx.conf" <<CONF
events {}
http {
  include /etc/nginx/conf.d/*.conf;
}
CONF
docker run --rm -v "$tmp/nginx.conf:/etc/nginx/nginx.conf:ro" -v "$tmp/nu_app.conf:/etc/nginx/conf.d/nu_app.conf:ro" -v "$tmp/le:/le:ro" "$NGINX_IMAGE" nginx -t >"$tmp/t.log" 2>&1; check "nginx -t（構文・証明書のパス）が通る" $?
cid=$(docker run -d --network host -v "$tmp/nginx.conf:/etc/nginx/nginx.conf:ro" -v "$tmp/nu_app.conf:/etc/nginx/conf.d/nu_app.conf:ro" -v "$tmp/le:/le:ro" "$NGINX_IMAGE")
for _ in $(seq 1 30); do curl -sk -o /dev/null "https://127.0.0.1:$P_HTTPS/" --resolve "nu-digital.jp:$P_HTTPS:127.0.0.1" && break; sleep 0.5; done

set +e
site() { curl -sk --max-time 10 --resolve "nu-digital.jp:$P_HTTPS:127.0.0.1" "$@"; }
app() { curl -sk --max-time 10 --resolve "app.nu-digital.jp:$P_HTTPS:127.0.0.1" "$@"; }
S="https://nu-digital.jp:$P_HTTPS"; A="https://app.nu-digital.jp:$P_HTTPS"
ID22="AbCdEfGhIjKlMnOpQrStUv"

code=$(curl -s -o /dev/null -w '%{http_code} %{redirect_url}' --resolve "nu-digital.jp:$P_HTTP:127.0.0.1" "http://nu-digital.jp:$P_HTTP/blog/")
[ "$code" = "301 https://nu-digital.jp/blog/" ]; check "HTTPはHTTPSへ301（パスを保つ）: $code" $?
code=$(curl -s -o /dev/null -w '%{http_code}' --resolve "nu-digital.jp:$P_HTTP:127.0.0.1" "http://nu-digital.jp:$P_HTTP/.well-known/acme-challenge/x")
[ "$code" = "404" ]; check "ACMEの確認用パスはHTTPのまま（リダイレクトしない）" $?

body=$(site -H 'X-Forwarded-For: 6.6.6.6' -H 'Cookie: session=secret' "$S/blog/")
echo "$body" | grep -q 'BACKEND=web'; check "公開ホストの / はwebへ" $?
echo "$body" | grep -q 'X-Forwarded-For: 127.0.0.1$'; check "X-Forwarded-Forはクライアントのアドレスで上書き（偽装を引き継がない）" $?
! echo "$body" | grep -q '6.6.6.6'; check "偽装したX-Forwarded-Forが残らない" $?
hdr=$(site -sI "$S/blog/")
echo "$hdr" | grep -qi '^strict-transport-security: max-age=31536000'; check "HSTSが付く" $?
echo "$hdr" | grep -qi '^x-content-type-options: nosniff'; check "nosniffが付く" $?
! echo "$hdr" | grep -qi '^server: nginx/'; check "Nginxのバージョンを出さない" $?
! echo "$hdr" | grep -qi '^x-powered-by'; check "X-Powered-Byを出さない" $?
! echo "$hdr" | grep -qi '^cache-control'; check "Nginxがキャッシュ関連のヘッダーを足さない（上流の指定のまま）" $?

body=$(site -H 'Cookie: session=secret' -H 'Authorization: Bearer x' "$S/api/v1/public/media/$ID22")
echo "$body" | grep -q 'BACKEND=api'; check "画像の公開配信（22文字のID）はAPIへ" $?
! echo "$body" | grep -qi 'cookie: session\|authorization: bearer'; check "画像の中継にCookie・Authorizationを渡さない" $?
echo "$body" | grep -q "GET /api/v1/public/media/$ID22"; check "画像の中継はパスをそのまま渡す" $?
[ "$(site -s -o /dev/null -w '%{http_code}' -I "$S/api/v1/public/media/$ID22")" = 200 ]; check "画像のHEADも通る" $?
[ "$(site -s -o /dev/null -w '%{http_code}' "$S/api/v1/public/media/${ID22}x")" = 404 ]; check "画像のIDが22文字でなければ404（公開ホストにAPIを出さない）" $?
for p in /api/v1/public/contents /api/v1/auth/me /api/v1/auth/login /api/v1/admin/users /api/v1/media/x /api/v1/public/media/$ID22/ ; do
  c=$(site -s -o /dev/null -w '%{http_code}' "$S$p"); [ "$c" = 404 ]; check "公開ホストから $p は見えない（$c）" $?
done
[ "$(site -s -o /dev/null -w '%{http_code}' -X POST "$S/api/v1/public/media/$ID22")" = 405 ]; check "公開ホストのPOSTは405" $?
[ "$(site -s -o /dev/null -w '%{http_code}' -X DELETE "$S/blog/")" = 405 ]; check "公開ホストのDELETEは405" $?

body=$(app -H 'X-Forwarded-For: 6.6.6.6' "$A/login")
echo "$body" | grep -q 'BACKEND=admin'; check "管理ホストの / はadminへ" $?
echo "$body" | grep -q 'X-Forwarded-For: 127.0.0.1$' && ! echo "$body" | grep -q '6.6.6.6'; check "管理ホストでもX-Forwarded-Forを上書き" $?
echo "$(app "$A/api/v1/auth/me")" | grep -q 'BACKEND=admin'; check "管理ホストの /api/v1 はadmin（Nextの中継）へ" $?
head -c 11534336 /dev/zero > "$tmp/11m.bin"; head -c 13631488 /dev/zero > "$tmp/13m.bin"
[ "$(app -s -o /dev/null -w '%{http_code}' -X POST --data-binary @"$tmp/11m.bin" "$A/api/v1/media?purpose=cms")" = 200 ]; check "管理ホストは11MiBの本文を受ける" $?
[ "$(app -s -o /dev/null -w '%{http_code}' -X POST --data-binary @"$tmp/13m.bin" "$A/api/v1/media?purpose=cms")" = 413 ]; check "12MiBを超える本文は413" $?
head -c 2048 /dev/zero > "$tmp/2k.bin"
[ "$(site -s -o /dev/null -w '%{http_code}' -X GET --data-binary @"$tmp/2k.bin" "$S/blog/")" = 413 ]; check "公開ホストは1KiBを超える本文を拒否" $?

# ログイン系はNginxでも回数を絞る（10回/分・burst 5）。連続して送ると429が出る
n429=0; for _ in $(seq 1 25); do c=$(app -s -o /dev/null -w '%{http_code}' -X POST -d '{}' "$A/api/v1/auth/login"); [ "$c" = 429 ] || [ "$c" = 503 ] && n429=$((n429+1)); done
[ "$n429" -ge 5 ]; check "ログインへの連続アクセスはNginxで制限される（$n429/25件が拒否）" $?

# アクセスログにクエリ文字列・Cookie・Authorizationを残さない
site -s -o /dev/null -H 'Cookie: session=LOGSECRET1' -H 'Authorization: Bearer LOGSECRET2' "$S/blog/?token=LOGSECRET3&email=a@example.invalid" >/dev/null
sleep 1
docker exec "$cid" cat /var/log/nginx/nu_app.access.log > "$tmp/access.log" 2>/dev/null
grep -q '"GET /blog/"' "$tmp/access.log"; check "アクセスログにパス（クエリなし）が残る" $?
! grep -q 'LOGSECRET\|a@example.invalid\|token=' "$tmp/access.log"; check "アクセスログにクエリ文字列・Cookie・Authorizationが残らない" $?

echo
echo "合格 $pass・失敗 $failed"
[ "$failed" = 0 ]
