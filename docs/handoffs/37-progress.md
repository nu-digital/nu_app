# 引き継ぎ: Issue #37 P1-02c HTTP基盤とログイン/ログアウト/me

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/37-http-auth`（base: `feat/35-auth-services`＝PR #36。マージ順は #32 → #34 → #36 → このPR）
- 完了: config（APP_BASE_URL必須・Secure Cookie安全装置・信頼プロキシ）、httpx（リクエストID・JSONエラー・ClientIP・Origin検証）、api（基盤ミドルウェア、login/logout/me、認可表の利用）、main配線（/api/v1）、パスワードの待ち上限(ErrBusy→503)、compose/.env.exampleにAPP_BASE_URL等、compose smokeに`/api/v1/auth/me`=401・Origin無しログイン=403
- 実測: 実PostgreSQL統合テスト全成功（Cookie属性、失敗の同一応答8種、レート制限（(ID,IP)・存在しないID・解除）、XFF偽装不可/信頼プロキシ右端、Origin/CSRF（9ケース）、ログインでセッション再発行と旧Cookie失効、停止の即時反映、本文サイズ/JSON以外/未知フィールド、ログに秘密値なし、混雑503）。ミューテーション4種（Origin検証を外す・CSRF検証を外す・XFFを無条件に信用・停止/招待中の扱いを緩める）はいずれもテストが失敗、復元済み。compose smoke PASS
- 未実施: PR CI結果。招待受諾・再設定（P1-02d）、管理API（P1-02e）、ログインUI（P1-04）
- 注意: ローカルComposeはAPP_BASE_URL=http://127.0.0.1:3000＋API_INSECURE_COOKIES=true（httpのため）。本番(https)でinsecureを設定すると起動失敗する
