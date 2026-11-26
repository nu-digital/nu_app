# 143 進捗: ぬる P1-4b（自発投稿・リアクション・気分・予算停止のお知らせ）

## 状態（事実）
- 実装: `internal/schedule`（毎分Tick: 04:00気分更新・気まぐれ投稿・停止お知らせ）、`brain.Spontaneous`、responderのリアクション（規則ベース・Claude API不使用）、`Gateway.PostTimes`（設定した1チャンネルだけ）、store `TakeReaction`。スレッド内メッセージへの返信は親でなくスレッドへ送るよう修正（P1-4aの不具合）。
- 実測: `go vet ./...`・`go test ./...`（services/agent）ok、`scripts/test-integration.sh` ok（TakeReactionの上限・日替わり含む）、`node scripts/check.mjs` PASS。
- 実Claude/Discord APIは未使用（偽物のみ）。

## 未実施
- 実機確認（RUNBOOK 16.5。token・APIキー設置後。運用責任者）。
- 自発投稿の確率・頻度は暫定（1日最大3回、深夜多め、間隔90分以上）。実機で様子を見て調整。

## 次の一手
- P1完了。デプロイ承認（production環境）→ 秘密設置 → 16.2 → 16.5。その後 P2（記憶）。
