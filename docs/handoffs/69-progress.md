# 引き継ぎ: Issue #69 P2-04a 通知アウトボックス（スキーマ・記録・設定API）

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/69-notification-outbox`（base: `feat/67-inventory-admin-ui`＝PR #68。積み重ね: …→#66→#68→このPR）
- 完了: migration 00006（`notification_settings`初期3種別・`notification_outbox`）、貸出・返却・訂正の業務トランザクション内でのoutbox記録（設定が有効なときだけ・`dedupe_key`で1イベント1行）、`GET/PUT /admin/notifications/settings`、`GET /admin/notifications/deliveries`（本文は返さない）、`POST .../deliveries/{id}/retry`（deadのみ）、認可表・AUTHZ.md・API.md・DATABASE.md。暫定採用はDECISIONS P2-04-1。
- 実測（実PostgreSQL・`scripts/test-integration.sh`全体）: 全パッケージ成功。通知の本文に氏名・学生証番号・大学メール・最寄り駅・ログインID・訂正の理由・管理者のログインIDが含まれない、代理返却は借りた人と返却した人が別、設定が無効なら積まれず業務は成功、**outboxに積めないとき貸出も巻き戻る（500・貸出0件）**、10並行の貸出で成功3件＝通知3件、同じキーの訂正の再送で通知が増えない、設定API（匿名401・Member403・入力検査・未知404・監査ログ）、一覧は本文を返さない・status絞り込み・再送はdeadだけ（二重は409）。`sqlc diff`差分なし。
- ミューテーション: 返却の通知を積まない→件数検査で失敗／設定の有効判定を外す→「無効なのに積まれた」で失敗、復元済み（最初の変異はコンパイルエラーで無効だったため作り直した）。
- 作成中の誤り: 既存クエリ名`GetDisplayName`との重複、訂正ハンドラ内の変数`delta`との衝突、テストの管理者表示名にログインIDが含まれていた、jsonbの出力が空白入りで文字列比較に合わない。
- 未実施: PR CI結果、compose smokeでの確認（この引き継ぎ作成時点）。送信worker（P2-04b）、設定画面（P2-04c）、`POST /admin/notifications/test`（workerと同時）。
- 次の一手: P2-04b（Discord送信worker。429のRetry-After・5xx・タイムアウトの再試行と上限、`dead`化、tokenを環境変数に限定しログに出さない、偽Discord（httptest）での検証）。
