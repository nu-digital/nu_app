#!/usr/bin/env bash
# sqlcを固定版のイメージで実行する。使い方: scripts/sqlc.sh generate | diff
set -euo pipefail
SQLC_IMAGE="sqlc/sqlc:1.31.1@sha256:70f53171d27b2424e9358869975455a6e955a5aa8e58a998a270a6e34e525537"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
exec docker run --rm -u "$(id -u):$(id -g)" -v "$root:/src" -w /src/db "$SQLC_IMAGE" "$@"
