#!/usr/bin/env bash
# certbotが証明書を更新した後に実行する（`/etc/letsencrypt/renewal-hooks/deploy/`に置く）。設定を検査してからNginxを再読み込みする。
set -euo pipefail
nginx -t
systemctl reload nginx
