# 引き継ぎ: Issue #136 ぬる P1-1（骨組み）

- 2026-11-27 / Claude Code（Sonnet 5.5）。ブランチ `feat/136-nul-p1-skeleton`。
- 完了: `services/agent`（Go・無効モード・healthz・Dockerfile）、migration 00013（`agent`スキーマ・NOLOGINの`nu_agent`ロール・テーブル8つ）、CI（go.ymlにagent追加・docker build）、deploy.ymlのbuildにagent、`compose.prod.yaml`のagentサービス（秘密・IDが無ければ無効モード）、`.env.production.example`、RUNBOOK §16（設定・DBロールの有効化・止め方）、sqlcの再生成（agentの型が`models.go`に出る。未使用）。
- 実測: `services/agent` vet/race test 成功、`go build`（api）成功、統合テスト（nu_agentがagentスキーマだけを使え、publicを読めない・書けない・スーパーユーザー等でない）成功、compose config・actionlint成功、Dockerイメージのbuild・起動（無効モード）。
- 判断: `ANTHROPIC_API_KEY`は他と混ざらないよう`.env`では`NUL_ANTHROPIC_API_KEY`にし、コンテナ内で`ANTHROPIC_API_KEY`へ渡す。DBロールは、migrationに秘密を書かないためNOLOGINで作り、運用者が初回にパスワードを設定する。
- 次: P1-2（Discord受信・記録・過去ログ取り込み・/nulコマンド）。**運用責任者に必要なもの**: Guild ID・timesチャンネルID・AdminロールID（P1-2の動作確認から）、Bot token・APIキー（P1のデプロイ直前）。
