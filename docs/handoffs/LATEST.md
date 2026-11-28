# 最新の引き継ぎ（現在地と次の一手）

最終更新: 2026-11-28。**この文書は、作業の区切りごとに更新する**（古くなったら、実際のGit・CI・本番を確認して直す）。

## 現在地（事実）

- 公式サイト・内部ポータル・API・ぬるは**本番で稼働中**（https://nu-digital.jp ・ https://app.nu-digital.jp）。デプロイは「mainにマージ → CI → 運用責任者がGitHubで承認 → 自動」。自動デプロイは有効（`DEPLOY_ENABLED=true`）。
- ぬる（`services/agent`）は2026-11-28に本番で有効化し、静かモードで会話テスト・キャラ設定の調整を経て、通常モードへの移行（最初の挨拶を含む）を進めた。実際のモード・設定は`/nul status`とDB（`agent.settings`）で確認する。
- 運用前の独立レビューの指摘（高・中の全件）は修正済み（[163-review.md](163-review.md)）。バックアップの復元確認はagentスキーマの行数まで突き合わせる。
- ドキュメントを整理した（README・START_HERE・FEATURES・NUL_OVERVIEW・HISTORY・SERVER_SETUP・COSTS_AND_CONTRACTS・AI_GUIDEなどを新設/更新）。

## 残件・次の一手

- **UI/UX大改善（2026-11-28）**: 物品（カテゴリ管理・複数画像・状態表示）・記事/作品（カード一覧・日付1つ・メンバー統合・プレビュー修正）・アカウント設定（行形式）・ランチャーを刷新。計画と検証は [design/ui-overhaul.md](../design/ui-overhaul.md)、記録は [186-progress](186-progress.md)。PRはレビュー待ち（migration 00021を含む。デプロイ前にバックアップ確認）。
- **フロントエンド（2026-11-28、マージ待ち）**。管理ポータルの見た目をCharcoal（pixiv・BOOTH風）にそろえ、Docker無しでログイン後の画面を確認できる開発用モック（`npm run admin:dev:mock`）を追加した。PRは積み重ねで、マージ順は #177（計画の文書）→ #179（Charcoal導入・ログイン画面）→ #181（ログイン後の画面全体）→ #183（開発用モックと実APIとの契約比較）→ #185（画面のボタン・入力を `components/ui` へ）。経緯と利用ルールは [design/charcoal.md](../design/charcoal.md)・[DESIGN](../DESIGN.md)「管理ポータルの部品の使い方」、モックは [LOCAL_DEV](../LOCAL_DEV.md)。本番反映は通常どおり mainマージ → CI → production承認。残り: セレクト・テキストエリアのCharcoal化、ホーム（ランチャー）の視覚試作、公式サイトの1コマンド起動（[FRONTEND](../FRONTEND.md) 分割案1）。既存の運用・ぬるの残件は以下のとおり。

1. **運用責任者が決めること・用意するもの**: ぬるの管理画面を作るか（[設計書 §9](../design/nul-admin-dashboard.md)の5点）／GitHub App（ぬるのGitHub読み取り）／nu_app連携（[NUL-1](../DECISIONS.md)の設計の確認）／思い出アルバムの容量／VPS外バックアップ（当面なし）／[COSTS_AND_CONTRACTS](../COSTS_AND_CONTRACTS.md)の「（要記入）」を埋める／本契約（無料お試し期間の終了前）。
2. 開発の候補（[BACKLOG](../BACKLOG.md)）: ぬるの管理画面（設計→N-1〜N-4）・GitHub読み取り・AUTHZの通知ID重複の整理・ダッシュボードのフィード・Dependabot（#3・#4）の対応。
3. ぬるの人格・関係性は、運用しながら調整する（プロンプトの見直しは[NUL-14](../DECISIONS.md)）。変更後は偽クライアントのテストと、実機での会話確認。

## 作業を始める前に

[AI_GUIDE](../AI_GUIDE.md)（AIエージェント）または[START_HERE](../START_HERE.md)（人）→ `git status`・`gh pr list`・直近のCIで実態を確認 → 該当する文書を読む。
