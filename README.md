# nu_app

nu_digitalの公式サイト、内部ポータル、Go API、Discord通知を段階的に構築するモノレポ。

**現在はClaudeに実装を引き継ぐための準備段階です。アプリ、DB、Docker Compose、本番デプロイはまだ実装していません。**

## まず読む

1. [開発エージェントの共通ルール](AGENTS.md)
2. [要件](docs/REQUIREMENTS.md) → [構成](docs/ARCHITECTURE.md) → [DB](docs/DATABASE.md) → [API](docs/API.md) → [実装順](docs/TASKS.md)
3. [Claudeへの依頼文](docs/CLAUDE_HANDOFF.md)と[小さい作業単位の一覧](docs/BACKLOG.md)

## 準備用チェック

GitとNode.jsを用意し、リポジトリ直下で実行します。追加npmパッケージのインストールは不要です。CIのNodeバージョンは [.node-version](.node-version) に固定します。

```sh
node scripts/check.mjs
node --test scripts/harness.test.mjs
```

チェック対象は必須文書・ローカルリンク・設定・禁止ファイル名・PR命名です。秘密値の完全検出、アプリの安全性、DB整合性、UI表示は保証しません。P0で実際のアプリ検証を追加します。

## 運用文書

- [GitHubと自律実装の進め方](docs/DEVELOPMENT.md)
- [テスト戦略・Phase別の合格条件](docs/TESTING.md)
- [UI調査と画面品質](docs/DESIGN.md)
- [Ubuntu VPSとリリースの準備](docs/OPERATIONS.md)
- [未決定事項と判断記録](docs/DECISIONS.md)
- [既存サイトの調査結果](docs/LEGACY_SITE.md)
- [引き継ぎテンプレート](docs/handoffs/TEMPLATE.md)

## リポジトリ

- 新規開発: [nu-digital/nu_app](https://github.com/nu-digital/nu_app)
- 既存サイト: [nu-digital/nu_digital](https://github.com/nu-digital/nu_digital)
- 現在の公開先: [GitHub Pages](https://nu-digital.github.io/nu_digital/)

既存サイトはP0で調査・移設し、CMSへの接続はP3で行います。既存リポジトリのデプロイ設定をそのまま新リポジトリで有効化しないでください。
