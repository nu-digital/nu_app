# nu_app

nu_digitalの公式サイト、内部ポータル、Go API、Discord通知を段階的に構築するモノレポ。

**現在の実装状態（2026-11-21）:** P0（基盤・CI）、P1（認証・権限・管理画面）、P2（プロフィール・物品貸出・Discord通知）は完了。P3（CMS・画像・記事の画面・既存記事の移行CLI）は途中で、公開サイトのAPI接続と表示比較が残っています。P4（VPSデプロイ・バックアップ）は未着手で、本番への接続・デプロイはしていません。最新の進捗は [BACKLOG](docs/BACKLOG.md)・[TASKS](docs/TASKS.md)。

## まず読む

WSL2で初めて作業する人は [初期セットアップ](docs/WSL_SETUP.md) と [Claude Team運用](docs/CLAUDE_TEAM.md) を先に確認してください。

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

## 公式サイト（apps/web）

既存サイト `nu-digital/nu_digital`（参照SHA `e1c0313`）を `git subtree` で移設しました（履歴維持・squashなし）。依存は移設前と同じlockfileで、更新は別PR（P0-02b）です。Node.jsは[.node-version](.node-version)。

```sh
npm run web:ci        # npm ci（apps/web）
npm run web:lint && npm run web:typecheck
npm run build         # 静的出力: apps/web/out
npm run web:verify    # 出力が移設前基準（docs/legacy-baseline）と一致するか
npm run test:unit     # 既存の単体テスト
```

`npm run web:e2e`は全ルート・ギャラリー操作のブラウザー確認（Playwrightはapps/webの固定devDependency。初回は`npx playwright install chromium`が必要で、Linuxでは依存ライブラリも要る）。

比較基準・画面確認の手順は [docs/legacy-baseline](docs/legacy-baseline/README.md)。素材の権利については [apps/web/ASSETS.md](apps/web/ASSETS.md)。

## ローカル起動

Docker Composeでweb・admin・api・bot・PostgreSQLを起動できます（手順は [docs/LOCAL_DEV.md](docs/LOCAL_DEV.md)）。

```sh
cp .env.example .env && npm run dev:up
```

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
