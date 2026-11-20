# 引き継ぎ: Issue #47 P1-03c 緊急復旧CLI

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/47-emergency-recovery`（base: `feat/45-owner-transfer`＝PR #46。積み重ね: #32→#34→#36→#38→#40→#42→#44→#46→このPR）
- 完了: `nuctl recover-owner --login-id --confirm-login-id --approver-note`（internal/recovery）。既存Ownerなら全セッション・未使用トークンを失効し、停止中なら有効に戻し、再設定（未設定なら招待）トークンを一度だけ表示。activeなOwnerが0人のときに限り既存ユーザーをOwnerに指名。承認メモ・確認ID・実行者必須。イベントと監査を同一トランザクションで記録（token・承認メモは監査に入れない）。`scripts/check.mjs`が`recover-owner`を含むworkflowを失敗させる（公開Web入口なし・デプロイから呼ばない）。docs/OPERATIONSに手順と通常デプロイとの区別
- 実測（実PostgreSQL）: 既存Ownerの復旧（停止→active、セッション失効、旧トークン失効、新トークン保存・メモ記録）、未設定Ownerは招待トークン、**activeなOwnerが居る間は別ユーザーをOwner化できない**、全Owner停止時のみ指名可、入力不備/存在しないIDの拒否で何も残さない、CLI終了コード、復旧トークンでパスワード設定→ログイン（HTTP）。ミューテーション3種（Owner化の制限を外す／セッション失効を外す／承認メモ検査を外す）でテストが失敗、復元済み。harnessテスト11件成功、workflowに書いた負の対照で失敗することも確認
- **本番では実行していない**（本番未構築）
- 未実施: PR CI結果。P1-04（内部アプリUI）が残り
