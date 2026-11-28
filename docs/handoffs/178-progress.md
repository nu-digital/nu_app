# #178 進捗 — 管理ポータルのログイン画面でCharcoalを試験導入

- 2026-11-28、Windows 11ネイティブ / Claude Code（Opus 5.5）。Codexの作業（#176、PR #177）を引き継ぎ。ブランチ `feat/178-admin-charcoal-login`（base: `docs/176-frontend-development-plan`。#177のマージ後にbaseをmainへ付け替える）。
- 対象要件: [charcoal.md](../design/charcoal.md) の導入判断点1。DB・API・認可の変更なし。切り戻しはPRのrevert（依存・CSS・ログイン画面のみ）。

## 完了

- 依存の追加（完全固定）、`components/ui/Button`・`TextField`、`app/charcoal-theme.css`、ログイン画面の置き換え。
- 検証と結果は [charcoal.md「試作の結果」](../design/charcoal.md) に記録（tsc・eslint・admin test 7件・next build・npm audit 0件・`node scripts/check.mjs`・Edgeでの操作）。

## 未実施

- macOS・Linuxでのビルド、Composeでの実API結合（ログイン成功まで）、ダークテーマ。CIのadmin・composeジョブで結合を確認する。
- `next dev` が `apps/admin/AGENTS.md` を生成するが、扱いは分割案1で決めるためコミットしていない。

## 次の一手

運用責任者が「試作の結果」の課題1（入力の見た目の統一とPasswordFieldの移行）・2（入力欄の境界）を決める → 判断点2（Icon・Modal・Storybook）。
