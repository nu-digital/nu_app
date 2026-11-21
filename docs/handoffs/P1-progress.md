# P1 進捗と再開の手引き（2026-11-21時点）

## 状態
- mainにマージ済み: P1-01a（schema・sqlc・実DB統合テスト基盤 #26）、P1-01c（初期Owner作成CLI #30）、P1-01b（認可表 #28）
- **レビュー待ちの積み重ねDraft PR（マージ順）**: #32（マージ運用の文書）→ #34（認証設計）→ #36（サービス層）→ #38（HTTP基盤・ログイン）→ #40（招待受諾・再設定）→ #42（管理API・認証E2E）。すべてCI 10/10成功。前がマージされたら次のbaseをmainへ付け替える
- 次の作業: P1-03（Admin/Editor付与、Owner二段階移譲、緊急復旧CLI）→ P1-04（内部アプリUI。調査 `docs/design/<issue>.md`、共通トークン、ログイン・ランチャー）。P1-03はP1-02の積み重ねの上に作る

## 再開時の手順
1. `git status`・`gh pr list` で積み重ねの状態とCIを確認（`docs/handoffs/<issue>-progress.md` に各PRの詳細）
2. 実PostgreSQLの統合テスト: `scripts/test-integration.sh`。Compose全体: `npm run compose:smoke`（認証E2Eを含む）
3. 暫定仕様はDECISIONSの「暫定採用」（P1-02-1〜8）。運用責任者が触って判断する前提で、変更容易に保つ

## 注意
- 本番デプロイ・本番データ変更・OS権限変更・追加課金は禁止。マージは運用責任者のレビュー待ち
- 認証・認可・個人情報・DB整合性は安全側。変更前に必ずテスト
