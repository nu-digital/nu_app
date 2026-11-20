# 引き継ぎ: Issue #23 web e2eの待ち条件の不安定さ

- 2026-11-21 / Claude Code（Sonnet 5.5）。P0-04のPR #22マージ後のmain CIで`web:e2e`が`networkidle`待ちの30秒タイムアウトにより失敗（run 44778195887、同じコードのPR CIは成功）。
- 原因（推定）: Next.jsのリンクprefetch等でネットワークが静止せず、CI環境の速度差でタイムアウト。ログで`page.goto("/") waiting until networkidle`のTimeoutErrorを確認。再現は未確認（ローカル・PR CIでは成功）
- 修正: `networkidle`をやめ、`load`+ビューポート内画像の読込完了+Webフォント準備を待つ`open()`に統一。判定項目・厳しさは変更なし
- 検証: ローカル`web:e2e`を3回連続成功、誤basePathで失敗(負の対照)。CIで再確認予定
- 教訓: main保護の必須チェックにpath filterなしで入れたため、flakyなチェックは全PRをブロックし得る。flakyが再発したら待ち条件を直す（必須チェックから外してごまかさない）
