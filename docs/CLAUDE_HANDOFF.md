# 新しいセッション・新しいAIエージェントの始め方

Claude Codeなどを、このリポジトリで新しく始めるとき（セッションの再開・担当の交代を含む）の手順と、渡す依頼文のひな形。**初回（P0-01）の依頼文は、役目を終えたので削除した**（経緯は[HISTORY](HISTORY.md)・[handoffs/P0-complete.md](handoffs/P0-complete.md)）。

## 1. 起動前（人）

- 初回の環境準備は [WSL_SETUP](WSL_SETUP.md)。課金の境界・モデル選択・中断再開は [CLAUDE_TEAM](CLAUDE_TEAM.md)（追加課金・API・Fableは使わない。ぬるの**実行時**のAPIだけが例外）。
- `claude`をリポジトリ直下で起動。`/memory`で指示（`CLAUDE.md`→`AGENTS.md`）、`/hooks`でStop hookを確認。**実行権限の全解除は不要**。
- **秘密（APIキー・トークン・SSH鍵・VPSのパスワード）は渡さない**。本番へのデプロイ・VPSの操作・APIへの接続は、人が確認して行う。

## 2. 依頼文のひな形（作業を頼むとき）

```text
README.md、AGENTS.md、docs/AI_GUIDE.md、docs/handoffs/LATEST.md を先に読んでください。
git status・直近のコミット・開いているIssue/PR・直近のCIで、実際の状態を確認してください（文書より実態が正です）。
今回の作業は <Issue番号 または 作業の内容> だけです。Issueを重複作成しないでください。
作業の種類に応じて、AI_GUIDEの「変更の種類別の目安」に挙がった文書を読み、DECISIONSに関係する決定（D01〜D15・暫定採用）があれば従ってください。未確定の仕様は決めずに、DECISIONSに提案を書いてください。
開始時に、対象要件・影響範囲・DB変更・API変更・必要なテスト・ロールバックの考え方を短く書いてください。
Issue番号付きのブランチで、日本語のコミット・PRにしてください。実際に実行したコマンドと結果を示し、未実施の検証を分けて報告してください。
本番のデプロイ・本番データの変更・実際のClaude API/Discordへの接続・秘密の扱いは、私が確認してから行います。しないでください。
完了時は、docs/handoffs/ に対象SHA・実行結果・残件・次の一手を残してください。
```

## 3. 再開するとき

使用量の制限などで止まったあとは、同じcloneで `claude --continue` または `claude --resume`。再開後は、指示ファイル・Issue・`docs/handoffs/`の最新・`git status`と差分・直近のCIを照合し、**不明な処理を完了と推測しない**（DB操作などを盲目的に再実行しない）。未完了の受け入れ条件から続ける。

## 4. 次に読むもの

[AI_GUIDE](AI_GUIDE.md)（設計思想・実装状況・落とし穴）・[docs/README.md](README.md)（文書の地図）。
