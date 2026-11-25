# 引き継ぎ: Issue #122 Nginx 1.24互換・TLS取得（実機）

- 2026-11-26 / Claude Code（Sonnet 5.5）。ブランチ `fix/122-nginx-124-compat`。
- 実機: DNS（Cloudflare・DNS only）を確認、さくらのパケットフィルター（22・80・443）を確認、証明書取得（`nu-digital.jp`・`app.nu-digital.jp`。Let's Encrypt・期限2027-02-23・メールなし登録）、`certbot.timer`と`certbot renew --dry-run`成功、Nginxを起動（上流が未起動のため502。公開ホストの`/api/`は404・HTTP→HTTPS 301・HSTS等を実機で確認）。
- 発見: Ubuntu 24.04のNginx 1.24は`http2 on;`を解釈できない（CIは1.30で通っていた）。設定を`listen ... ssl http2;`に直し、テストのNginxを1.24.0（digest固定）に変更。33/33。
- 残り: 設定をサーバーへ反映（main取り込み後）、systemdタイマー（backup・cert-check）、GHCRトークン・GitHub設定・Discord Webhook、初回デプロイ（要確認）、`NOPASSWD:ALL`の解除。
