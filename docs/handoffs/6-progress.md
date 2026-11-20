# 引き継ぎ: Issue #6 D01〜D13の確定内容の反映

- 日付 / 担当: 2026-11-21 / Claude Code（Sonnet 5.5）
- Issue / PR: #6 / Draft PR（番号は作成後のPR本文を正とする）
- ブランチ / 対象SHA: `docs/6-confirm-decisions`（PR #5 のブランチ `chore/2-legacy-baseline` から分岐＝**PR #5に依存**。#5がmainにsquashされた後はrebaseが必要）
- 課金: 追加課金・Fable・API・subagent不使用
- 対象要件 / 非対象: D01〜D13を仕様へ反映する文書変更。実装・DB migration・本番デプロイ・VPS契約・DNS設定・SSHは非対象
- 完了内容: DECISIONS（確定/採用方針/契約後確認の3区分で再編）、REQUIREMENTS、DATABASE、API、ARCHITECTURE、OPERATIONS（確定事項と未確認の実情報、TLS、バックアップ、緊急復旧、画像資源）、TASKS、TESTING、LEGACY_SITE、BACKLOG（P0-02b追加）
- D13の事前確認（実測）: 参照SHA e1c0313、11コミット、pack 126.19MiB、最大blob約4.05MiB、50MiB超0件。履歴書き換え不要
- 実行コマンド・結果: `node scripts/check.mjs` 成功、`node --test scripts/harness.test.mjs` 9件成功（実装テストは対象外）
- 未実施: 実push時のGitHub受理確認（移設PRで）、実装との整合検証（実装がまだ無い）
- 判断待ち: なし（D01〜D13確定）。ただし採用方針（Claude提案）は実装PRで検証する。契約後確認: VPS事業者/CPU/ディスク/Ubuntu提供/DNS/画像メモリ/バックアップ所要時間/緊急復旧CLIの経路
- DB / API / 切り戻し: 設計記述のみ。revertで戻る
- 次に実行する最小の作業: #5→#6の順にレビュー・マージ後、P0-02（`git subtree add`、squashなし）のIssueを作成して着手。依存更新はP0-02b
