# #184 進捗 — 各画面のボタン・入力を components/ui の共通部品に置き換え

- 2026-11-28、Windows 11ネイティブ / Claude Code（Opus 5.5）。ブランチ `feat/184-admin-ui-components`（base: `feat/182-admin-dev-mock`）。マージ順は #177 → #179 → #181 → #183 → 本PR。
- DB・API・URL・権限の変更なし。切り戻しはPRのrevert。

## 完了

- ボタン80個・テキスト入力29個を `components/ui` へ。部品の選び方を DESIGN に追記。詳細と検証は [charcoal.md「画面のボタン・入力を共通部品へ」](../design/charcoal.md)。

## 未実施

- 実APIでの操作（CIで確認）。セレクト・テキストエリアのCharcoal化（Charcoalの DropdownSelector はネイティブselectと操作が違い、E2Eの `selectOption` も変わるため保留）。

## 次の一手

CIを確認 → 運用責任者がPRを順にマージ → mainのCI成功後、deployの承認で本番へ。
