#!/usr/bin/env bash
# 使い捨てのPostgreSQL 18.7コンテナを起動し、Go統合テスト(-tags integration)を実行して片付ける。
# 使い方: scripts/test-integration.sh [go testの追加引数...]   例: scripts/test-integration.sh -run TestUsers
set -euo pipefail
PG_IMAGE="postgres:18.7@sha256:eb63372c1e05c755f55fa30cbd07aecc443676fc6c0fa90b9002d01537841e1a"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
name="nu-app-itest-$$"
net="$name"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; docker network rm "$net" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker network create "$net" >/dev/null
docker run -d --name "$name" --network "$net" -e POSTGRES_PASSWORD=itest-only-not-secret "$PG_IMAGE" >/dev/null
for _ in $(seq 1 60); do docker exec "$name" pg_isready -U postgres >/dev/null 2>&1 && break; sleep 1; done
"$root/scripts/go-docker.sh" --network "$net" services/api \
  env TEST_DATABASE_URL="postgres://postgres:itest-only-not-secret@$name:5432/postgres?sslmode=disable" \
  go test -tags integration -race -count=1 ./... "$@"
