# Claude Teamの内包使用量で開発する

ユーザー指定（2026-11-20）。Claude Code標準のモデル選択、Plan mode、Subagents、セッション再開、コンテキスト圧縮を優先する。独自モデルルーター、API代理呼び出し、使用量回復のポーリング、無限再起動は実装しない。

## 課金境界

- 人がTeam組織のclaude.aiアカウントでログインし、席のClaude Code利用権と使用量を確認する。Console/APIログインとは区別する。
- Team管理者が追加使用量（usage credits / extra usage）、自動購入・補充、利用者への追加使用量割当を確認し、追加料金を発生させない設定にする。アカウント設定はこのリポジトリから変更していない。
- `ANTHROPIC_API_KEY`、別プロバイダー、カスタムendpoint、`apiKeyHelper`等がある場合は接続経路を人が確認する。APIキーによる利用は内包使用量の代替にしない。APIクレジットが付帯していても今回のClaude Code運用に流用しない。
- Fable、Fast mode、追加クレジットが必要な機能、外部の有料AIサービスは明示許可なしに使用しない。課金確認が出たら同意せず停止する。Opus/Sonnet/Haikuもその席で内包対象と確認できたものだけ使う。
- `/usage`のプラン残量と管理画面を確認する。トークンから算出されたドル表示を請求額と断定しない。`--max-budget-usd`をTeam内包枠の上限設定として使わない。

## モデルの選択

| 作業 | 優先 | 切り替えと戻し方 |
|---|---|---|
| 通常実装・修正・テスト | Sonnet | プロジェクト初期値。`/model sonnet` |
| 複雑な設計・タスク分解・認可/DB競合の設計 | Opus | `/model opusplan`を選びPlan modeで設計し、実装時はSonnetへ。設計だけなら`/model opus`、完了後Sonnetへ戻す |
| 限定したファイル探索・文書整理・機械的な確認 | Haiku | 必要なら`/model haiku`またはモデル指定可能な標準subagent。軽微な作業のためだけに別agentを増やさない |

`opusplan`はPlan/実行モードの切替であり、失敗履歴に応じた万能な自動ルーターではない。選択可能性と実際の使用モデルは `/model` で確認する。aliasesの対応版は変わり得るため、CLI版と実モデルを引き継ぎに記録する。

同じ受け入れ条件が、原因仮説を変えた2回の修正でも失敗したら、再現条件・エラー要約・試行を保存する。HaikuならSonnetへ、Sonnetなら限定したOpus設計/調査へエスカレーションする。これはプロジェクトの運用基準でありClaude Codeの自動判定設定ではない。認可・データ破壊に関わる不確実性は2回を待たずOpusへ。環境不足や認証/使用量制限はモデル変更で解決しようとしない。上位モデルも使えなければ停止し、使えるモデルで安全な独立作業だけ続ける。

subagentは通常0、必要時に1件ずつ。独立した探索・レビューなど、親で処理するより明確な効果がある場合に限定する。目的・対象・終了条件・モデル・結果の保存先を渡す。不要な並列起動、agent teamsの常用、子からの再委任はしない。標準Explore/Plan等を先に検討し、組み込みagentが必要な仕様を継承すると思い込まず対象要件を明示する。課金枠は親と別に無料になるわけではない。

`.claude/settings.json`はSonnet初期値と3系列のモデル候補を設定する。ただしproject設定のallowlistは他の設定と結合され得るため、課金の強制遮断ではない。強制制限が必要なら管理者が公式managed settingsの `availableModels` と `enforceAvailableModels` を検討し、実クライアントで確認する。未知の組織IDや管理設定をこちらで導入しない。

## 中断と再開

**制限後にはモデルが保存処理を実行できない場合がある。最後に必ず保存できるとは保証しない。** Stop hookも正常終了補助であり、制限・クラッシュ・電源断での引き継ぎ生成を保証しない。

1. 開始時にIssue・ブランチ・対象SHAを確認し、`docs/handoffs/<issue番号>-progress.md`を作る。
2. 調査終了、実装の小区切り、テスト実行後、モデル変更前、圧縮前に進捗を更新。完了/作業中/未着手、実コマンドと結果、次の一手を保存する。安全な単位は作業ブランチへcommit/pushし、未完コードを完成扱いにしない。
3. `/usage`は開始時と作業の区切りに確認。制限が近い表示なら新しい大仕事を始めず、引き継ぎを先に保存する。残量を読めなければ推測しない。
4. 制限を検知したら新規モデル要求・subagent起動を停止。可能な範囲で状態を記録し、進行中コマンドの完了/失敗/不明を区別する。追加使用量への同意・別認証への変更・制限回避のAPI呼び出しはしない。
5. 利用可能になった後、人が同じWSL cloneで `claude --continue` または `claude --resume` を実行して対象セッションを選ぶ。自動再開デーモンは追加しない。
6. 再開後は指示ファイル、Issue、引き継ぎ、`git status`と差分、直近CIを照合する。不明な処理を完了と推測せず、DB操作等を盲目的に再実行しない。未完了の受け入れ条件から続ける。

標準のauto-compaction、必要時の `/compact` を使う。`/clear`や新セッションの前にも引き継ぎを保存する。会話履歴・auto memoryだけを仕様/進捗の正本にせず、モデルを変えてもdocsとGitから復元可能にする。

自律作業の単位は、Issueから、PR最新コミットのCI成功を確認したDraft PRまで（PR #28以降。マージは運用責任者のレビュー待ち。[DEVELOPMENT](DEVELOPMENT.md)の「マージとレビューの運用」）。依存するPRは積み重ねて次の作業へ進んでよい。難しい設計・比較検討は必要に応じてOpus、通常の実装はSonnetを優先する。完成までの段階計画はBACKLOGに保ち、報告はPhase完了・重要な問題・判断が必要な場合に絞る。無人の無制限連続運転やGitHub自動マージ機能は導入しない。使用量制限に備えて進捗を途中保存し、制限時は新規作業を止めて安全に停止する。

## 標準機能との分担と出典

2026-11-20確認。利用条件・UI・設定対応は導入時にも再確認する。

- 認証/課金は標準ログインと管理画面: [Team利用](https://support.claude.com/en/articles/11845131-use-claude-code-with-your-team-or-enterprise-plan)、[APIキーの扱い](https://support.claude.com/en/articles/12304248-manage-api-key-environment-variables-in-claude-code)。
- 選択は標準 `/model` / opusplan: [モデル設定](https://code.claude.com/docs/en/model-config)。ハーネスは難易度/失敗時の判断基準だけを補う。
- 委任は標準 [Subagents](https://code.claude.com/docs/en/sub-agents)。固定の多重agent編成は作らない。
- 再開/圧縮は標準 [Sessions](https://code.claude.com/docs/en/sessions)。ハーネスは永続する作業記録を補う。
- [Hooks](https://code.claude.com/docs/en/hooks)は既存のローカルcommand型チェックだけ。追加のLLM呼び出しや課金APIは含めない。
