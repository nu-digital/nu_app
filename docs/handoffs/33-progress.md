# 引き継ぎ: Issue #33 P1-02a 認証フロー設計

- 2026-11-21 / Claude Code（Sonnet 5.5。設計レビューのみOpus）。ブランチ `docs/33-auth-design`（base: `docs/31-review-gated-merge`＝PR #32に積み重ね）
- 完了: docs/design/p1-02-auth.md（脅威と対策、フロー、暫定値）、Opusの設計レビューを反映（重大4件を採用: 停止時にトークン失効＋状態条件付き受諾、信頼プロキシ設定によるIP取得、トークンをURLフラグメントへ、最後のOwner保護のロック）、DECISIONSの暫定採用 P1-02-1〜7
- 未実装: すべて設計のみ。実装はP1-02b以降（b: サービス層、c: HTTP基盤とログイン、d: 招待受諾・再設定、e: 管理API）
- マージ順: #32 → このPR → P1-02b以降
