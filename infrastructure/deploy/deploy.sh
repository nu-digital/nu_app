#!/usr/bin/env bash
# VPS上で、指定したコミットSHAのイメージへ更新する（P4-01）。GitHub Actionsの`deploy`ワークフロー（承認つき）がSSHで呼ぶ唯一の入口。
# 使い方: sudo /srv/nu_app/infrastructure/deploy/deploy.sh <コミットSHA（40桁）>
#   - 直列化（flock）。途中で別のデプロイに中断されてmigrationだけ進む状態を避ける
#   - 引数は40桁の16進だけ（他の文字を一切通さない）
#   - 更新前にイメージをpull（失敗したら何も変えない）→ 直前のバックアップ（失敗したら更新しない）→ up → ヘルスチェック
#   - 失敗したら、動いていた版（current_tag）のイメージへ戻して通知する。**DBのmigrationは戻らない**
#     （前版のイメージは新しいスキーマで動く必要がある。破壊的なschema変更は段階移行にする。docs/OPERATIONS.md）
#   - 成功通知はヘルスチェックの成功後だけ。通知にsecret・個人情報・tokenは含めない
set -Eeuo pipefail

APP_DIR="${APP_DIR:-/srv/nu_app}"
COMPOSE_FILE="${COMPOSE_FILE:-$APP_DIR/infrastructure/compose.prod.yaml}"
ENV_FILE="${ENV_FILE:-$APP_DIR/.env}"
STATE_DIR="${STATE_DIR:-$APP_DIR/state}"
BACKUP_SCRIPT="${BACKUP_SCRIPT:-$APP_DIR/infrastructure/backup/backup.sh}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-120}"
NOTIFY_URL="${DEPLOY_NOTIFY_WEBHOOK_URL:-${BACKUP_NOTIFY_WEBHOOK_URL:-}}"
HOST_LABEL="${HOST_LABEL:-nu_app}"
SKIP_GIT="${SKIP_GIT:-0}"

tag="${1:-}"
if ! [[ "$tag" =~ ^[0-9a-f]{40}$ ]]; then echo "使い方: deploy.sh <コミットSHA（40桁の16進）>" >&2; exit 2; fi
short="${tag:0:12}"

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" >&2; }
notify() {
  [ -n "$NOTIFY_URL" ] || return 0
  curl -fsS --max-time 15 -H 'Content-Type: application/json' -d "$(printf '{"content":"%s"}' "$1")" "$NOTIFY_URL" >/dev/null 2>&1 || log "通知を送れませんでした"
}
# COMPOSE_ARGS（例: "-p nu_trial -f override.yaml"）と SKIP_PULL=1 は、本番ではない試験環境（ローカルでビルドしたイメージ）での予行演習用。本番では設定しない
# shellcheck disable=SC2086
compose() { NU_TAG="$1" docker compose ${COMPOSE_ARGS:-} -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "${@:2}"; }

mkdir -p "$STATE_DIR"
exec 9>"$STATE_DIR/.lock"
if ! flock -n 9; then log "別のデプロイが実行中です"; exit 75; fi

current=""; [ -s "$STATE_DIR/current_tag" ] && current="$(cat "$STATE_DIR/current_tag")"

healthy() { # $1=待つ秒数。API・web・adminが応答し、公開サイトの一覧が200
  local end=$((SECONDS + $1))
  while [ "$SECONDS" -lt "$end" ]; do
    if curl -fsS --max-time 5 http://127.0.0.1:8080/readyz >/dev/null 2>&1 \
      && curl -fsS --max-time 5 http://127.0.0.1:8081/healthz/ >/dev/null 2>&1 \
      && curl -fsS --max-time 5 http://127.0.0.1:3000/healthz >/dev/null 2>&1 \
      && curl -fsS --max-time 10 -o /dev/null http://127.0.0.1:8081/blog/ 2>/dev/null; then return 0; fi
    sleep 3
  done
  return 1
}

stage="準備"
rollback_attempted=0
fail() {
  local code=$?
  trap - ERR
  log "失敗しました（$stage）"
  if [ "$rollback_attempted" = 0 ] && { [ "$stage" = "更新" ] || [ "$stage" = "ヘルスチェック" ]; }; then
    if [ -n "$current" ]; then
      rollback_attempted=1
      log "動いていた版（${current:0:12}）へ戻します（DBのmigrationは戻りません）"
      if compose "$current" up -d --remove-orphans >/dev/null 2>&1 && healthy "$HEALTH_TIMEOUT"; then
        notify "[${HOST_LABEL}] デプロイに失敗しました（${stage}）: ${short}。前の版（${current:0:12}）へ戻しました。"
      else
        notify "[${HOST_LABEL}] デプロイに失敗し、前の版（${current:0:12}）への復帰も確認できません。至急、手動で確認してください（${short}）。"
      fi
    else
      notify "[${HOST_LABEL}] 初回のデプロイに失敗しました（${stage}）: ${short}。戻す版がありません。確認してください。"
    fi
  else
    notify "[${HOST_LABEL}] デプロイを中止しました（${stage}）: ${short}。動いている版は変更していません。"
  fi
  exit "${code:-1}"
}
trap fail ERR

# デプロイ先のリポジトリ（Compose・Nginx・スクリプト）も、同じコミットにそろえる。スクリプト自身が入れ替わるため、そろえたら実行し直す
if [ "$SKIP_GIT" != 1 ] && [ -z "${DEPLOY_REEXEC:-}" ]; then
  stage="リポジトリの更新"
  git -C "$APP_DIR" fetch --quiet --depth 1 origin "$tag"
  git -C "$APP_DIR" checkout --quiet --detach "$tag"
  DEPLOY_REEXEC=1 exec "$APP_DIR/infrastructure/deploy/deploy.sh" "$tag"
fi

stage="イメージの取得"
log "イメージを取得: $short"
[ "${SKIP_PULL:-0}" = 1 ] || compose "$tag" pull --quiet

if [ -n "$current" ]; then
  stage="直前のバックアップ"
  log "更新前のバックアップ"
  DEPLOYED_TAG="$current" "$BACKUP_SCRIPT"
fi

stage="更新"
log "更新: ${current:0:12} → $short"
compose "$tag" up -d --remove-orphans

stage="ヘルスチェック"
healthy "$HEALTH_TIMEOUT"

stage="記録"
[ -n "$current" ] && echo "$current" > "$STATE_DIR/previous_tag"
echo "$tag" > "$STATE_DIR/current_tag"
trap - ERR
log "デプロイ成功: $short"
notify "[${HOST_LABEL}] デプロイに成功しました: ${short}"
