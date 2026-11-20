# 引き継ぎ: Issue #27 P1-01b 認可表

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/27-authz-table`、基点 main cb6dffb（`go / sqlc`・`go / integration`を必須チェックへ追加済み、docs/DEVELOPMENT.mdへの反映は本PR）
- 完了: services/api/internal/authz（57アクション、既定拒否、停止/招待中は公開以外拒否、Owner⊇Admin、Admin管理者対象の制限）、期待表テスト（主体7種×57アクション、関係あり/なしのケース、拒否理由）、API.mdとの1対1ドリフト検査、docs/AUTHZ.mdとその網羅検査
- 実測: gofmt/vet/`go test -race` 成功。ミューテーション3種（reset_linkの特権対象制限を外す・publishをMemberに開放・停止ユーザー検査を外す）で、いずれもテストが失敗することを確認し復元
- **確認が必要な点**: 「管理者権限を持つ対象への停止・招待/再設定URL発行はOwnerのみ」は要件の解釈（権限昇格防止）。DECISIONSに記録。認可の重要設計なので運用責任者の確認を求める（実装は提案どおり進めてよいが、異論があれば`notPrivilegedTarget`を変更）
- 未実施: PR CI結果
- 次: P1-01c Owner作成CLI
