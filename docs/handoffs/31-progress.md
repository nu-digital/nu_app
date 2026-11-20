# 引き継ぎ: Issue #31 マージ運用の変更（PR #28以降はレビュー待ち）

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `docs/31-review-gated-merge`、基点 main 4f6f6bd（PR #28までをマージ済み。main CI成功）
- 指示: PR #28以降は、作業ブランチへのpush・Draft PR作成・CI確認まで自律で行い、マージは運用責任者のレビューを待つ。依存PRは積み重ねてよい。D01〜D13以外の細かな仕様は自律判断、暫定採用はDECISIONSに理由・代替案・影響を記録して区別。難しい設計はOpus、実装はSonnet。本番デプロイ・本番データ変更・OS権限変更・追加課金は禁止。
- 反映: AGENTS.md / DEVELOPMENT.md（「マージとレビューの運用」「未確定仕様の扱い」） / CLAUDE_TEAM.md / CLAUDE.md / CLAUDE_HANDOFF.md / 引き継ぎテンプレート / DECISIONS（暫定採用の表）
- 次: P1-02（招待受諾・ログイン・セッション・CSRF・レート制限）を積み重ねDraft PRで進める
