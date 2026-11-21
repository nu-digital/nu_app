# 引き継ぎ: Issue #71 P2-04b Discord送信worker

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/71-discord-worker`（base: `feat/69-notification-outbox`＝PR #70）
- 完了: `internal/notify`（Discord RESTクライアント・本文整形・worker）、`ClaimDueNotification`等のクエリ、`cmd/server`でのworker起動（`DISCORD_BOT_TOKEN`がある時だけ。停止時はctx終了を待つ）、`POST /admin/notifications/test`、設定`DiscordBotToken`（空白・制御文字は拒否、値はエラーに出さない）、compose/.env.exampleへの任意のtoken、OPERATIONS/LOCAL_DEV/API/AUTHZ。暫定採用はDECISIONS P2-04-2。
- 実測（`scripts/test-integration.sh`全体=実PostgreSQL＋偽Discord(httptest)・`-race`）: 全パッケージ成功。単体: 送信ヘッダー(Bot認証)・`allowed_mentions`空・不正チャンネルIDは送らない・429(ヘッダー/本文/値なし/1時間上限)・5xx・401/403/404の分類・応答本文やtokenが要約に出ない・接続不可/タイムアウト・リダイレクトでAuthorizationを送らない・本文整形（貸出/個体/代理返却/訂正/取消、`@ < >`・改行・U+2028・長文の無害化）・バックオフ30秒〜30分。worker: 送信済み化と通知ID、チャンネル未設定/無効は待機し設定後に送る、5xxで30秒→倍の待ち→復旧後1回だけ届く、上限でdead→管理者再送で届く、403は即dead、429は待ち・試行を数えず巡回を止め後続に触れない、壊れたpayloadはdead、落ちた確保のリース回復、4 worker並行で30件が各1回、tokenと応答本文がDB・ログに残らない。API: 接続テストの認可・入力検査・Bot未設定503・失敗種別、**Discord障害中でも貸出・返却が確定し通知は送信待ちに残る**。
- ミューテーション: 429の試行払い戻しを外す→失敗／`SKIP LOCKED`を外す→並行テスト失敗／`allowed_mentions`を外す→失敗。復元済み。
- 作成中の誤り: gofmt未整形、テストの`jsonStr`がU+2028を誤ってエスケープ（`encoding/json`へ置換）。
- 未実施: **実Discordへの送信は一切していない**（tokenを持たない。偽サーバーでのみ検証）。実機でのBot権限・チャンネルIDの確認は運用責任者がtokenを設定して`POST /admin/notifications/test`で行う。PR CI結果、compose smoke、設定画面（P2-04c）。
- 次の一手: P2-04c（通知設定画面: 種別ごとのON/OFF・チャンネルID・接続テスト・送信状況と再送）。
