#!/usr/bin/env bash
# ホストにGoを入れずに、固定したGoイメージでコマンドを実行する。
# 使い方: scripts/go-docker.sh <services/api|services/bot> <コマンド...>   例: scripts/go-docker.sh services/api go test -race ./...
set -euo pipefail
# 固定版(docs/TOOLCHAIN.md)。Goを更新するときは go.mod のgo指定・Dockerfile・CIと一緒に変える。
GO_IMAGE="golang:1.27.4@sha256:205c7636d74e0ffbe3d007162df7d74f859f5a8c8322dfb9f907977686c126cf"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module="${1:?module dir required}"; shift
exec docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOMODCACHE=/tmp/gomod \
  -v "$root/$module:/src" -w /src "$GO_IMAGE" "$@"
