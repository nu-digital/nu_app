# 149 進捗: ぬる P2-3（週報・月報）

## 状態（事実）
- 実装: `store.SummariesBetween`、`brain.Report`（Opus/Sonnet、停止でAPI不使用）、`budget.PreviousPeriod/Limit`、`schedule.maybeReports`（金曜21:00週報・月初12:00月報・1期間1回・失敗は最大3回）。
- 実測: `go vet`・`go test ./...`（services/agent）ok、`scripts/test-integration.sh` ok、`node scripts/check.mjs` は下記PR時に実行。実Claude APIは未使用（偽物）。

## 未実施
- 実機確認（金曜夜・月初の実投稿）。nu_app・GitHub由来の材料はP3/P4。

## 次の一手
- P2完了。次はP3（Web検索・GitHub読み取り・研究日誌・気まぐれ投稿のネタ）。GitHub Appの作成など運用責任者の準備が必要。
