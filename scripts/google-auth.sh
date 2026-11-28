#!/usr/bin/env bash
# 会計機能（Google Drive・Sheets）の初回OAuth認可を、Docker上で対話的に実行する。自分のPC（ブラウザのある環境）で実行すること。
# クライアントシークレットは入力欄で受け取り（表示されない）、リフレッシュトークンは画面に出さず 0600 のファイルに保存する。
# 使い方: scripts/google-auth.sh [-folder <フォルダID>] [-create-root <名前>]
#   保存先: ~/.nu_app-secrets/google-oauth.env（既にあれば中止。VPSの.envへ移したら削除する）
set -euo pipefail
GO_IMAGE="golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf" # scripts/go-docker.sh と同じ固定版
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
port=53682
secrets="$HOME/.nu_app-secrets"
umask 077
mkdir -p "$secrets"
chmod 700 "$secrets"
# ホスト側は 127.0.0.1 にだけ公開（同じネットワークの他の機器には開かない）。ブラウザは http://127.0.0.1:$port/callback に戻ってくる。
exec docker run --rm -it -p "127.0.0.1:${port}:${port}" -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod \
  -v "$root:/repo:ro" -v "$secrets:/secrets" -w /repo/services/api "$GO_IMAGE" \
  go run ./cmd/googleauth -bind 0.0.0.0 -port "$port" -out /secrets/google-oauth.env "$@"
