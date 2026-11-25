#!/usr/bin/env bash
# GitHub Actionsのデプロイ用SSH鍵の**入口**（authorized_keysの command= に指定する）。
# この鍵では「`sudo /srv/nu_app/infrastructure/deploy/deploy.sh <40桁のコミットSHA>`」以外を一切実行できない。
#   authorized_keys の例（deployユーザー）:
#     restrict,command="/srv/nu_app/infrastructure/deploy/ssh-entry.sh" ssh-ed25519 AAAA... nu_app-actions-deploy
# `restrict` でポート転送・エージェント・PTY・X11を無効にする。このファイルはroot所有（deployユーザーは書き換えられない）。
set -euo pipefail
cmd="${SSH_ORIGINAL_COMMAND:-}"
if [[ "$cmd" =~ ^sudo\ /srv/nu_app/infrastructure/deploy/deploy\.sh\ ([0-9a-f]{40})$ ]]; then
  exec sudo /srv/nu_app/infrastructure/deploy/deploy.sh "${BASH_REMATCH[1]}"
fi
echo "このSSH鍵で許可されているのは deploy.sh <40桁のコミットSHA> だけです" >&2
exit 126
