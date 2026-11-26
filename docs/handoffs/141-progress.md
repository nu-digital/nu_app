# 引き継ぎ: ぬる P1-3b（頭脳・気分・画像）

- 2026-11-27 / Claude Code（Sonnet 5.5）。ブランチ `feat/141-nul-p1-brain`（base: main。P1-3a #139はマージ済み）。
- 完了: `internal/brain`（キャラ設定=固定のシステムプロンプト・Opus用ワーカープロンプト・`think_hard`・気分（毎日の揺らぎ・褒め/雑・夜型のテンション）・会話途中のシステムメッセージ・モデルの選択・ツール実行ループ（上限3）・「本気出して」は必ずOpus・予算/1日の上限/クレジット切れ/refusal/レート制限の扱い・入力は`<untrusted_message>`で囲い閉じタグを無害化）、`internal/imageprep`（検査・縮小・透過は白）、store（気分・親密度）。
- 実測: vet・race test・統合テスト・`govulncheck`成功。変異確認（AllowAPI・Opusの予算・untrusted無害化を外すとテストが落ちる）。**実際のClaude APIは呼んでいない**。
- 設計の確認点: 固定部分（システム・ツール）は呼び出しごとに同一で、予算の段階でもツール定義を変えない（キャッシュを壊さない。Opusが使えないときはtool_resultのエラーで返す）。
- 次: P1-4（Discordでの応答（メンション・DM・times）・気まぐれ投稿・リアクション・統合テスト・RUNBOOK・実機確認の手順）。**運用責任者に必要なもの**: Guild ID・timesチャンネルID・AdminロールID。
