#!/usr/bin/env bash
# DB（pg_dump）とメディア（tar）を、同一世代IDの一組としてバックアップする（D04）。
#   - 作業領域に作成 → チェックサム・読み取り検証 → 世代として確定（rename）。途中の失敗では**既存の世代に一切触らない**。
#   - 古い世代の削除は、新世代の確定が成功した後にだけ行い、保持は成功した世代のうち新しい順に RETENTION 個。
#   - 順序は「DB → メディア」。画像はファイルを先に書いてからDBの行を作るため、この順なら（メディアのファイル ⊇ DBの行）になり、
#     復元後に「行はあるがファイルが無い」状態にならない（余ったファイルは `nuctl media-gc` が掃除する）。短時間の書き込み停止は不要。
#   - 失敗は BACKUP_NOTIFY_WEBHOOK_URL（Discordの運用チャンネルのWebhook。秘密）へ通知する。通知に個人情報・秘密は含めない。
# 使い方: backup.sh   （設定は環境変数。下の一覧）
set -Eeuo pipefail

BACKUP_ROOT="${BACKUP_ROOT:-/srv/nu_app/backups}"
MEDIA_DIR="${MEDIA_DIR:-/srv/nu_app/media}"
RETENTION="${RETENTION:-7}"
MIN_FREE_MB="${MIN_FREE_MB:-1024}"
COMPOSE_FILE="${COMPOSE_FILE:-/srv/nu_app/infrastructure/compose.prod.yaml}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-/srv/nu_app/.env}"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-nu_app}"
DB_SERVICE="${DB_SERVICE:-db}"
HOST_LABEL="${HOST_LABEL:-nu_app}"
NOTIFY_URL="${BACKUP_NOTIFY_WEBHOOK_URL:-}"
DEPLOYED_TAG="${DEPLOYED_TAG:-}"

# compose.prod.yamlはNU_TAG（動いている版）が必須。systemdのタイマーから呼ばれるときは未設定なので、デプロイが記録した値を使う
if [ -z "${NU_TAG:-}" ]; then NU_TAG="$(cat "${STATE_DIR:-/srv/nu_app/state}/current_tag" 2>/dev/null || echo unknown)"; fi
export NU_TAG
compose() { docker compose -p "$COMPOSE_PROJECT" -f "$COMPOSE_FILE" --env-file "$COMPOSE_ENV_FILE" "$@"; }
log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }

step="開始前"
work=""
notify() { # $1=メッセージ。通知の失敗はバックアップの結果を変えない
  [ -n "$NOTIFY_URL" ] || return 0
  local body
  body=$(printf '{"content":"%s"}' "$1")
  curl -fsS --max-time 15 -H 'Content-Type: application/json' -d "$body" "$NOTIFY_URL" >/dev/null 2>&1 || log "通知を送れませんでした"
}
on_error() {
  local code=$?
  trap - ERR EXIT
  log "失敗しました（${step}）"
  [ -n "$work" ] && rm -rf -- "$work"
  notify "[${HOST_LABEL}] バックアップに失敗しました（${step}）。既存の世代は変更していません。"
  exit "$code"
}
trap on_error ERR

mkdir -p "$BACKUP_ROOT"
exec 9>"$BACKUP_ROOT/.lock"
if ! flock -n 9; then log "別のバックアップが実行中です"; exit 75; fi

# 確定済みの世代（新しい順）。確定の印（COMPLETE）がある世代だけ
generations() { find "$BACKUP_ROOT" -mindepth 2 -maxdepth 2 -name COMPLETE -printf '%h\n' | xargs -r -n1 basename | sort -r; }

step="空き容量の確認"
last_mb=0
latest=$(generations | head -n1 || true)
if [ -n "$latest" ]; then last_mb=$(( $(du -sk "$BACKUP_ROOT/$latest" | cut -f1) / 1024 )); fi
need_mb=$(( last_mb * 2 > MIN_FREE_MB ? last_mb * 2 : MIN_FREE_MB ))
free_mb=$(df -Pk "$BACKUP_ROOT" | awk 'NR==2 {print int($4/1024)}')
if [ "$free_mb" -lt "$need_mb" ]; then
  log "空き容量が足りません（空き ${free_mb}MiB・必要 ${need_mb}MiB）。世代数は黙って減らしません"
  false
fi

id=$(date -u +%Y%m%dT%H%M%SZ)
work="$BACKUP_ROOT/.work-$id"
rm -rf -- "$work"; mkdir -p "$work"

# DB のユーザー名・DB名はコンテナの環境変数から（秘密を引数・ログに出さない）
step="DBのダンプ"
compose exec -T "$DB_SERVICE" sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner' > "$work/db.dump"
[ -s "$work/db.dump" ]
step="DBのダンプの検証"
toc=$(compose exec -T "$DB_SERVICE" pg_restore --list < "$work/db.dump" | wc -l)
[ "$toc" -gt 10 ]
schema=$(compose exec -T "$DB_SERVICE" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT coalesce(max(version_id),0) FROM goose_db_version"')
counts=$(compose exec -T "$DB_SERVICE" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc "SELECT (SELECT count(*) FROM users)||chr(32)||(SELECT count(*) FROM contents)||chr(32)||(SELECT count(*) FROM media_assets)"')

# ぬる（agentスキーマ）の行数。復元の確認で突き合わせる（スキーマが無い版では none）
agent_counts=$(compose exec -T "$DB_SERVICE" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At' < "$(dirname "$0")/agent-counts.sql")

step="メディアのアーカイブ"
[ -d "$MEDIA_DIR" ]
tar -C "$MEDIA_DIR" --exclude=./tmp -czf "$work/media.tar.gz" .
step="メディアのアーカイブの検証"
tar -tzf "$work/media.tar.gz" >/dev/null
media_files=$(tar -tzf "$work/media.tar.gz" | grep -vc '/$' || true)

step="マニフェストとチェックサム"
{
  echo "generation=$id"
  echo "created_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "schema_version=$schema"
  echo "users_contents_media_assets=$counts"
  echo "agent_counts=$agent_counts"
  echo "media_files=$media_files"
  echo "deployed_tag=$DEPLOYED_TAG"
} > "$work/manifest.txt"
( cd "$work" && sha256sum db.dump media.tar.gz manifest.txt > SHA256SUMS && sha256sum -c SHA256SUMS >/dev/null )

step="世代の確定"
touch "$work/COMPLETE"
chmod -R go-rwx "$work"
mv -- "$work" "$BACKUP_ROOT/$id"
work=""
ln -sfn "$id" "$BACKUP_ROOT/LATEST"
sync

# 新世代が確定してから、保持数を超えた古い世代だけを削除する
step="古い世代の整理"
generations | tail -n +"$((RETENTION + 1))" | while read -r old; do
  [ -n "$old" ] && [ "$old" != "$id" ] && rm -rf -- "${BACKUP_ROOT:?}/$old" && log "古い世代を削除: $old"
done
# 中断された作業領域の掃除（今回のものは確定済み）
find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -name '.work-*' -mmin +120 -exec rm -rf -- {} + 2>/dev/null || true

trap - ERR
log "完了: $id（DB $(du -h "$BACKUP_ROOT/$id/db.dump" | cut -f1)・メディア $(du -h "$BACKUP_ROOT/$id/media.tar.gz" | cut -f1)・保持 $(generations | wc -l) 世代）"
