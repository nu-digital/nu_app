# 147 進捗: ぬる P2-2（日次要約・Batch API）

## 状態（事実）
- 実装: migration 00015（summaries）、`llm.Batch`（実API・偽物）、`budget.RecordBatch/EstimateBatch/Remaining`、`summarize.Job`、`digest`ツール、削除連動（その日の要約を消す）、main配線（15分ごと）。
- 実測: `go vet`・`go test ./...`（services/agent）ok、`scripts/test-integration.sh` ok、sqlc再生成、`node scripts/check.mjs` PASS。
- 実Claude APIは未使用（SDKのBatchはhttptestの偽サーバーで要求形式・結果の読み取りを確認）。

## 未実施
- 実機確認（実際のBatch APIでの完了・使用量の記録。有効化後）。
- 見積もりの係数（1文字≒1.5トークン）は安全側の粗い値。実績で調整。

## 次の一手
- P2-3: 週報（金曜21:00）・月報（月初。予算の使い道と残り）。
