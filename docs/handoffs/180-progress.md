# #180 進捗 — ログイン後の画面全体をCharcoalのデザインにそろえる

- 2026-11-28、Windows 11ネイティブ / Claude Code（Opus 5.5）。ブランチ `feat/180-admin-charcoal-inner`（base: `feat/178-admin-charcoal-login`。マージ順は #177 → #179 → 本PR）。
- 対象要件: 運用責任者の依頼（中の画面全体をCharcoalデザインに）。DB・API・認可・URLの変更なし。切り戻しはPRのrevert（CSS・ヘッダー部品・ユーザー管理のヘッダーのみ）。

## 完了

- 共通CSSのトークンと部品クラスをCharcoalのパレットへ。共通ヘッダー `components/AppHeader`（アプリの切り替えつき）を追加し、AppShell・ランチャー・ユーザー管理で共有。
- 内容と検証は [charcoal.md「ログイン後の画面全体」](../design/charcoal.md)。

## 未実施

- 実APIでの操作（CIで確認）。手元ではDocker・Goが無いため、スクラッチパッドの使い捨てモック（コミットしない）で表示だけ確認した。
- 各画面のJSXを `components/ui` の部品へ置き換えること（見た目はCSSでそろえ済み）。

## 次の一手

CIの結果を確認 → 運用責任者が実画面を見て調整を依頼 → 分割案2（契約検査つきのadminモック）を入れると、Docker無しでも中の画面を誰でも確認できる。
