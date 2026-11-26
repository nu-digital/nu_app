# 引き継ぎ: ぬる P1-3a（Claudeクライアント・予算）

- 2026-11-27 / Claude Code（Sonnet 5.5）。ブランチ `feat/140-nul-p1-llm`。
- 完了: `internal/llm`（`Client`インターフェース・偽物・実体（anthropic-sdk-go v2.9.0のベータ名前空間））、`internal/budget`（単価表・キャッシュ込みの金額・70/90/100%の段階・月の期間（付与日）・一人あたりの1日の上限・クレジット切れで次の期間まで寝る・読めないときは安全側＝停止）、store（使用量の記録・期間の合計・1日の回数（原子的））。
- claude-apiスキルの注意を、リクエストの形のテストで固定: システムは固定でcache_control、**tool_choice・thinking・temperature等を送らない**、effortはoutput_config、サーバー側フォールバック（`fallbacks:"default"`＋ベータ`server-side-fallback-2026-07-01`。Haikuには付けない）、会話途中のシステムメッセージ（気分・日時）、画像はbase64、返答（thinking署名つき）をそのまま履歴へ戻す、refusalの検出、エラーの分類（429・クレジット切れ・その他。本文・秘密を含めない）。
- 発見: SDK v2はAPIキーを`Authorization: Bearer`で送る。
- 実測: vet・race test・統合テスト・`govulncheck` 成功。**実際のClaude APIは呼んでいない**。
- 次: P1-3b（brain: キャラ設定・気分・モデルの選択・think_hard・画像の縮小）。
