#!/usr/bin/env bash
# バックアップの1世代を、**隔離した一時環境**（使い捨てのPostgreSQLコンテナ・一時ディレクトリ）へ復元して整合を確認する（D04）。
# 本番のDB・メディアには一切触らない（読むのは世代のディレクトリだけ）。
# 確認: ①チェックサム ②DBの復元（pg_restore）③スキーマのバージョンと行数がマニフェストと一致 ④メディアの展開
#       ⑤**DBの行とメディアのファイルの参照整合**（`nuctl media-verify`。APIイメージの運用CLI）
# 使い方: restore-verify.sh [世代のディレクトリ（既定: BACKUP_ROOT/LATEST）]
# 環境変数: BACKUP_ROOT, POSTGRES_IMAGE（本番のDBと同じイメージ）, NUCTL_IMAGE（APIイメージ。media-verifyに使う）
set -Eeuo pipefail

BACKUP_ROOT="${BACKUP_ROOT:-/srv/nu_app/backups}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:?POSTGRES_IMAGE（本番のDBと同じイメージ）を指定してください}"
NUCTL_IMAGE="${NUCTL_IMAGE:?NUCTL_IMAGE（APIイメージ）を指定してください}"
gen="${1:-$BACKUP_ROOT/LATEST}"
gen=$(readlink -f -- "$gen")
log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
fail() { log "NG: $*"; exit 1; }

[ -f "$gen/COMPLETE" ] || fail "確定していない世代です: $gen"
suffix="$$-$RANDOM"
net="nu-restore-$suffix"
pg="nu-restore-pg-$suffix"
tmp=$(mktemp -d)
cleanup() { docker rm -f "$pg" >/dev/null 2>&1 || true; docker network rm "$net" >/dev/null 2>&1 || true; rm -rf -- "$tmp"; }
trap cleanup EXIT

log "チェックサムの確認"
( cd "$gen" && sha256sum -c SHA256SUMS >/dev/null ) || fail "チェックサムが一致しません"
manifest() { sed -n "s/^$1=//p" "$gen/manifest.txt"; }

log "隔離環境のPostgreSQLを起動"
docker network create --internal "$net" >/dev/null
pgpass=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)
docker run -d --name "$pg" --network "$net" -e POSTGRES_USER=restore -e POSTGRES_PASSWORD="$pgpass" -e POSTGRES_DB=restore "$POSTGRES_IMAGE" >/dev/null
# postgresイメージは初期化用の一時サーバーが一度立ち上がってから本番の起動になる。「接続を受け付ける」の記録が2回出るまで待つ
for _ in $(seq 1 90); do
  [ "$(docker logs "$pg" 2>&1 | grep -c 'ready to accept connections')" -ge 2 ] && break
  sleep 1
done
docker exec "$pg" pg_isready -U restore -d restore >/dev/null 2>&1 || fail "一時のPostgreSQLが起動しません"

log "DBを復元"
docker exec -i "$pg" pg_restore -U restore -d restore --no-owner --exit-on-error < "$gen/db.dump" || fail "pg_restoreに失敗しました"
q() { docker exec "$pg" psql -U restore -d restore -Atc "$1"; }
schema=$(q "SELECT coalesce(max(version_id),0) FROM goose_db_version")
[ "$schema" = "$(manifest schema_version)" ] || fail "スキーマのバージョンが違います（復元 $schema・マニフェスト $(manifest schema_version)）"
counts=$(q "SELECT (SELECT count(*) FROM users)||' '||(SELECT count(*) FROM contents)||' '||(SELECT count(*) FROM media_assets)")
[ "$counts" = "$(manifest users_contents_media_assets)" ] || fail "行数が違います（復元 $counts・マニフェスト $(manifest users_contents_media_assets)）"

log "メディアを展開"
mkdir -p "$tmp/media"
tar -xzf "$gen/media.tar.gz" -C "$tmp/media"
files=$(find "$tmp/media" -type f | wc -l)
[ "$files" = "$(manifest media_files)" ] || fail "メディアのファイル数が違います（展開 $files・マニフェスト $(manifest media_files)）"

log "DBの行とメディアのファイルの整合を確認（nuctl media-verify）"
# 展開したメディアはAPIイメージの利用者（nonroot）が読み書きできるよう、一時ディレクトリの権限だけを緩める（本番のファイルではない）
chmod -R a+rwX "$tmp/media"
docker run --rm --network "$net" --user 65532:65532 \
  -e "DATABASE_URL=postgres://restore:${pgpass}@${pg}:5432/restore?sslmode=disable" -e MEDIA_DIR=/media \
  -v "$tmp/media:/media" --entrypoint /nuctl "$NUCTL_IMAGE" media-verify || fail "DBの行とメディアのファイルが食い違っています"

log "OK: $(basename "$gen") を隔離環境へ復元し、スキーマ $schema・行数 $counts・メディア ${files}ファイルの整合を確認しました（本番には触れていません）"
