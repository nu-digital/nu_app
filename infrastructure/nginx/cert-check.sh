#!/usr/bin/env bash
# TLS証明書の期限を確認し、閾値以下・読めない・配信中の証明書が古い場合に、Discordの運用チャンネルへ通知する（D03）。
# certbotの自動更新が止まっていても、期限が切れる前に気づくための確認（systemd timerで毎日）。
#   CERT_FILES   : 確認する証明書ファイル（空白区切り。既定は /etc/letsencrypt/live/<ホスト>/fullchain.pem）
#   PROBE_HOSTS  : 実際にHTTPSで配信されている証明書も確認するホスト:ポート（空白区切り。例: nu-digital.jp:443）。更新後にNginxを再読み込みしたかの確認
#   WARN_DAYS    : 残り日数がこれ以下なら警告（既定14）
#   BACKUP_NOTIFY_WEBHOOK_URL / NOTIFY_URL : 通知先（秘密）。通知に秘密・個人情報は含めない
set -uo pipefail
CERT_FILES="${CERT_FILES:-/etc/letsencrypt/live/nu-digital.jp/fullchain.pem /etc/letsencrypt/live/app.nu-digital.jp/fullchain.pem}"
PROBE_HOSTS="${PROBE_HOSTS:-}"
WARN_DAYS="${WARN_DAYS:-14}"
NOTIFY_URL="${NOTIFY_URL:-${BACKUP_NOTIFY_WEBHOOK_URL:-}}"
HOST_LABEL="${HOST_LABEL:-nu_app}"
problems=()
days_left() { # 標準入力のPEMの残り日数。読めなければ空
  local end
  end=$(openssl x509 -noout -enddate 2>/dev/null | sed 's/^notAfter=//') || return 0
  [ -n "$end" ] || return 0
  echo $(( ( $(date -u -d "$end" +%s) - $(date -u +%s) ) / 86400 ))
}
for f in $CERT_FILES; do
  d=$(days_left < "$f" 2>/dev/null)
  if [ -z "$d" ]; then problems+=("証明書を読めません: $(basename "$(dirname "$f")")")
  elif [ "$d" -le "$WARN_DAYS" ]; then problems+=("証明書の期限が近い（残り${d}日）: $(basename "$(dirname "$f")")"); fi
done
for hp in $PROBE_HOSTS; do
  host="${hp%%:*}"
  d=$(echo | openssl s_client -connect "$hp" -servername "$host" 2>/dev/null | days_left)
  if [ -z "$d" ]; then problems+=("配信中の証明書を確認できません: $host")
  elif [ "$d" -le "$WARN_DAYS" ]; then problems+=("配信中の証明書の期限が近い（残り${d}日。更新後にNginxを再読み込みしたか確認）: $host"); fi
done
if [ "${#problems[@]}" -eq 0 ]; then echo "OK: 証明書の期限に問題はありません（閾値 ${WARN_DAYS}日）" >&2; exit 0; fi
msg="[${HOST_LABEL}] TLS証明書の確認で問題: $(IFS='；'; echo "${problems[*]}")"
echo "$msg" >&2
if [ -n "$NOTIFY_URL" ]; then
  curl -fsS --max-time 15 -H 'Content-Type: application/json' -d "$(printf '{"content":"%s"}' "$msg")" "$NOTIFY_URL" >/dev/null 2>&1 || echo "通知を送れませんでした" >&2
fi
exit 1
