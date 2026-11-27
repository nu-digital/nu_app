# 157 進捗: ぬる P5-1（リマインダー）

## 状態（事実）
- 実装: migration 00018、store（登録・一覧・取り消し・原子的な確保・結果反映）、brainのツール3つ、`Gateway.Remind`（依頼者だけメンション）、`Scheduler.deliverReminders`（全停止で保留）。
- 実測: `go vet`・`go test ./...`・`scripts/test-integration.sh`（並行の確保・上限・落ちた配信の再送）ok、sqlc再生成。実Discord・Claude APIは未使用。

## 未実施
- 実機確認。繰り返し・「毎週」は未対応。

## 次の一手
- 内省（毎晩のノート・関係・気分の更新。Haiku Batch）。GitHub読み取りはGitHub App待ち。
