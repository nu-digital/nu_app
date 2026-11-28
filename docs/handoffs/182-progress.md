# #182 進捗 — 管理ポータルの開発用モック（実APIとの契約比較つき）

- 2026-11-28、Windows 11ネイティブ / Claude Code（Opus 5.5）。ブランチ `feat/182-admin-dev-mock`（base: `feat/180-admin-charcoal-inner`）。マージ順は #177 → #179 → #181 → 本PR。
- 対象要件: docs/FRONTEND.md の分割案2。DB・API・アプリ本体の動作の変更なし（開発用のファイルとCIの検査を追加）。切り戻しはPRのrevert。

## 完了

- `apps/admin/mock/`（fixtures・handler・server・テスト）、`scripts/admin-dev-mock.mjs`（`npm run admin:dev:mock`）、`scripts/admin-mock-contract.mjs`（形の比較）とcompose smokeへの組み込み。
- 混入防止: アプリからのimport禁止のテスト、`.dockerignore`、`NODE_ENV=production`で起動拒否、127.0.0.1のみ。ビルド成果物にモックの文字列が無いことを確認。
- 手順は [LOCAL_DEV](../LOCAL_DEV.md)。

## 未実施

- compose smokeの契約比較は手元にDockerが無く未実行（CIで初めて動く）。差分が出たら、モックか lib/api.ts の型を実APIに合わせて直す。
- macOSの起動確認。

## 次の一手

CIの契約比較の結果を確認 → 各画面の `components/ui` への置き換え（#183予定）。
