# 引き継ぎ: Issue #73 P2-04c 通知設定画面

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/73-notification-ui`（base: main。#70・#72はマージ済み）
- 完了: `/admin/notifications`（種別ごとのON/OFF・チャンネルID・保存・接続テスト、送信の状況＝状態絞り込み・止まった通知の再送）、「物品の管理」からの導線。API変更なし。暫定採用はDECISIONS P2-04-3。
- 実測: `npm run admin:lint`・`admin:typecheck` 成功。`node scripts/compose-smoke.mjs` 308項目すべてOK（PC/スマホ/320px: 一般メンバーには権限なしの案内、3種別の表示、保存の活性/非活性、数字以外のIDは拒否、保存の保持、Bot未設定の接続テストの案内、OFFの保持、止まった通知の要約と再送→送信待ちに戻る・本文は出ない、横溢れなし・44px以上）。スクリーンショットは `.local/ui-shots/*-notifications.png`（Git外）を目視。
- E2Eで見つけた不具合: `describe()`が503を一律「混み合っています」に変えており、Bot未設定の案内が管理者に届かなかった（`BOT_NOT_CONFIGURED`だけAPIの文言を出すよう修正）。
- 補助: smokeのみ、使い捨てDBへSQLを流す`dbExec`で「失敗して停止」の通知を用意（APIでは作れない状態のため。本番DBには触れない）。
- 未実施: PR CI結果、実Discordでの送信（tokenが無いため接続テストは「Bot未設定」の案内確認まで）、実スマホ確認。
- 次の一手: P2は完了（P2-01〜04）。BACKLOGのP3（CMS・メディア・アバター）へ。設計が大きいので先にDECISIONS/設計メモと、画像（D06）・公開クレジット（D08）の整理から。
