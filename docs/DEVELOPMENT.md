# 開発とGitHub運用

## 基本

`main`を唯一の統合ブランチとする。長期のdevelopブランチは作らない。1 Issue / 1目的 / 1短命ブランチ / 1 PRを基本に、動作・テスト・必要なmigrationと文書を一緒にレビューする。

ブランチ: `<type>/<issue番号>-<kebab-case>`。
typeは `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`。
例: `feat/12-invite-acceptance`, `test/24-concurrent-loans`。
最初の準備だけ `chore/bootstrap-agent-harness` を例外とする。

コミットとPRタイトル: `<type>(任意のscope): 日本語の変更内容`。
例: `feat(auth): 招待URLの使い回しを拒否する`、`docs: 物品貸出の受け入れ条件を整理する`。
ブランチ名・識別子は英語、説明は日本語。コミットは一つの変更意図にまとめる。

## 1タスクの流れ

1. mainとIssueを読み、未コミット変更を確認。他人の変更を消さない。担当範囲と依存PRを確認。
2. 新規ブランチを作り、対象要件・影響範囲・DB・API・テスト・切り戻しを6項目で記す。
3. 既存実装とテストを調査。未確定事項は [DECISIONS](DECISIONS.md) に提案・影響・代替案・誰の判断が必要かを書く。
4. 受け入れ条件に沿って小さく実装。バグ修正は可能なら先に再現テストを書く。
5. 準備用チェックと変更対象のテストを実行。差分を読み直し、不要ファイル・秘密値・仕様逸脱を点検。
6. 日本語コミット、作業ブランチへpush、Draft PR作成。本文に `Closes #番号`、実測結果、未実施を記載。
7. CI失敗を修正。画面変更は操作確認と画像比較、DB変更はPostgreSQLで検証。レビュー可能になったらReadyへ。
8. 運用責任者が差分と結果を確認してマージ。Squash mergeのタイトルも日本語。次の作業は最新mainから始める。

環境不足は成功に数えない。無関係の作業は進め、問題のある受け入れ条件は未完了と明記する。別のエージェントや新セッションは [引き継ぎ](handoffs/TEMPLATE.md) と実際のGit/CIを照合して再開する。

## 自律的に決められること

要件内の実装分割、命名、テスト追加、リファクタ、内部UIのレイアウト・余白・状態表示は調査して決めてよい。各選択で許可を求めない。依存ライブラリは既存構成と互換性を確認し、固定バージョンと採用理由を記録する。

未確定の業務仕様・個人情報公開・スコープ拡張は決めない。本番変更、mainへの直接push、force push、保護ルールの緩和は通常の自律作業から除く。初回の初期仕様main登録のみユーザー承認済みの例外。

## GitHub側で設定する項目

初回pushとCI実行後にSettingsで設定・確認する。ファイルを置くだけでは有効にならない。

- デフォルトブランチmain、Squash merge、マージ済みブランチの自動削除。
- mainのRulesetまたはbranch protection: PR必須、force push・削除禁止、会話解決必須、CI必須。管理者の恒常的bypassも避ける。
- 現在の必須チェック候補は `harness / repository`、`harness / policy`。初回実行で表示された正確な名前を指定する。P0以降にアプリ検証も必須化する。
- 複数のレビュー担当者がいる場合は承認1件・最新差分で承認失効を設定。一人運用では自分のPRを自分で承認できないため、承認数必須は無理に設定せず手動レビューとCIで運用し、体制を記録する。
- privateリポジトリの保護機能はGitHubプランを確認する。設定不可なら制限を明記し、保護済みと呼ばない。
- Actionsは最小権限。PRのコードに本番secretを渡さない。`pull_request_target`でPRコードを実行しない。
- レビュー担当のGitHub IDが確定したらCODEOWNERSを追加。存在しないチーム名を仮登録しない。

Issueテンプレートは準備済み。実IssueとPRはGitHubを進捗の正本とし、[BACKLOG](BACKLOG.md) は分割と依存関係の地図にする。

Dependabotが作成した専用ブランチのPRだけは自動生成のブランチ名・英語タイトルをCIで例外扱いする。レビュー担当者はマージ時のSquashタイトルを日本語に直す。人のPRにはこの例外を適用しない。

## ハーネスの境界

`CLAUDE.md`はAGENTS.mdを読み込む入口。`.claude/settings.json`のStop hookは終了前に軽量チェックを行う。リトライ無限ループ防止のためhookによる再開中はブロックを繰り返さない。チェック失敗が解決したことを意味しない。

hookは信頼済みプロジェクトのClaude Codeで読み込みを確認する。NodeがPATHにない環境やhook未対応クライアントでは動かないためCIで同じチェックを行う。hookは権限制御・secret scannerではない。独自の常駐エージェント、無制限の自己反復、必須の並列エージェントは導入しない。

参照（2026-11-20確認）: [Claude Codeの推奨運用](https://code.claude.com/docs/en/best-practices)、[指示ファイル](https://code.claude.com/docs/en/memory)、[hooks](https://code.claude.com/docs/en/hooks)、[GitHubブランチ保護](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches)。
