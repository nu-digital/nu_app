# 165 進捗: 運用開始に向けた確認（レビュー対応その2・有効化ガイド・管理画面の設計）

## 状態（事実）
- 内省（P2-4・#160）をマージし、新機能の追加を止めた。
- 独立レビュー（別エージェントが全ソースを読み取り専用で通読）の結果と対応: [163-review.md](163-review.md)。高1・中8・低の大半を修正（#163・本PR）。
- バックアップの復元確認にagentスキーマの行数を追加（#162）。
- `docs/NUL_LAUNCH.md`（有効化の手順・実機テストのチェックリスト・運用開始後の確認・既知の制限）、`docs/design/nul-admin-dashboard.md`（管理画面の設計。実装は未）を追加。agentのGOMEMLIMITをcomposeに追加。
- 実測: `go vet`・`go test ./...`・`scripts/test-integration.sh` ok、`node scripts/check.mjs` PASS。実Discord・Claude APIは未使用。

## 運用責任者の作業（NUL_LAUNCH.md）
VPSの空き確認・Discord/Anthropicの設定確認・デプロイ承認（古い承認待ちはReject）・バックアップの復元確認・秘密の設置・停止状態での起動・段階的な実機テスト。

## 次の一手
- 運用責任者の確認後に、デプロイ承認→有効化。管理画面は設計の確認（設計書§9の5点）後に実装。
