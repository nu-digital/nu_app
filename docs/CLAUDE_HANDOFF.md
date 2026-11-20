# Claudeへの依頼文

準備PRをレビューしてmainへマージした後、Claude Codeをリポジトリ直下で起動して以下を渡す。未マージの準備ブランチではセットアップと準備チェックだけを確認し、機能実装は始めない。

初回の人の作業は [WSL_SETUP](WSL_SETUP.md)。Team利用・モデル選択・中断再開は [CLAUDE_TEAM](CLAUDE_TEAM.md) に従う。

```text
AGENTS.md / CLAUDE.mdとdocsの必読5文書、DEVELOPMENT、TESTING、DECISIONSを先に読んでください。
CLAUDE_TEAM.mdに従い、Team内包使用量を優先し追加課金を許可しません。通常Sonnet、難しい設計Opus、軽量調査Haikuを優先し、作業の区切りで引き継ぎを保存してください。
今回はBACKLOGのP0-01、GitHub Issue #2だけを完了してください。Issueを重複作成しないでください。

既存サイトは https://github.com/nu-digital/nu_digital 、調査基準はdocs/LEGACY_SITE.mdです。
まず現在のソース・主要URL・67記事・画像・日付・ギャラリーをローカルで再現し、移設前の比較基準を作成してください。
必要な依存バージョンの整合性と、移設時に維持すべきURL・表示・ライセンス・履歴を調査してください。

開始時に対象要件・影響範囲・DB変更・API変更・必要テスト・切り戻しを短く記載してください。
Issue番号付きブランチを作り、日本語でコミット・PRを作成してください。
node scripts/check.mjs と node --test scripts/harness.test.mjs に加え、調査対象サイトのlint/buildと実画面確認を行ってください。
調査で既存不具合があれば、移設による差分と区別し、再現条件と影響を記録してください。

通常の調査・ローカル修正・検証・作業ブランチへのpush・Draft PR作成は自律的に進めて構いません。
重要な未確定仕様だけDECISIONSに提案と代替案を残し、それに依存しない作業は続けてください。
CMS、物品管理、認証本体は今回実装せず、mainマージ・本番デプロイ・本番データ変更は行わないでください。
完了時は対象SHA、差分、実行コマンドと実測結果、未実施検証、残件をPRと引き継ぎに残してください。
```

P0-01の受け入れ後にP0-02へ進む。内部アプリのUI作業では [DESIGN](DESIGN.md) に従い、現在の日本語UIを調査し、要件内のデザイン選択は自律的に行う。先に全Phaseを一気に実装する依頼はしない。

Claude Code側では `/memory` でこのリポジトリの指示、`/hooks` でStop hookの認識を確認する。実行権限を全解除する設定は不要。CIとGitHub保護ルールの有効化状況は引き継ぎで確認する。
