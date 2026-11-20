# Claude Code の入口

@AGENTS.md

@docs/CLAUDE_TEAM.md

最初に [README](README.md) で現在の実装状態を確認し、AGENTS.mdの順序で仕様書を読む。
作業方法は [DEVELOPMENT](docs/DEVELOPMENT.md)、検証基準は [TESTING](docs/TESTING.md)。
最初の依頼文は [CLAUDE_HANDOFF](docs/CLAUDE_HANDOFF.md) にある。

このリポジトリのハーネスは、短い指示・小さいIssue/PR・実行可能なチェック・CI・人によるマージ判断で構成する。
Stop hookは準備用チェックだけを補助実行する。アプリの受け入れテストやサーバー側ブランチ保護を代替しない。
権限チェックを無効化する起動オプションや、本番secretを渡した無人実行は使用しない。
