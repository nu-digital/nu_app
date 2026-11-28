# ドキュメント一覧と、それぞれの役割

プロジェクトの入口は [../README.md](../README.md)。ここは**全文書の地図**。迷ったら「正本」の列を見る。食い違いを見つけたら、実装・本番の実態を確認して**正本のほうを直す**（実装が正）。

## 目的別の入口

| 目的 | 文書 |
|---|---|
| 全体像・現在の状態・読む順序 | [../README.md](../README.md) |
| 初めて開発する（環境構築→開発→テスト→PR→デプロイ） | [START_HERE](START_HERE.md) → [LOCAL_DEV](LOCAL_DEV.md)・[WSL_SETUP](WSL_SETUP.md)・[DEVELOPMENT](DEVELOPMENT.md)・[TESTING](TESTING.md) |
| 運用・保守・障害対応・引き継ぎ | [RUNBOOK](RUNBOOK.md) |
| VPSの新規構築・サーバー移行 | [SERVER_SETUP](SERVER_SETUP.md) |
| 費用・契約・更新が必要なもの | [COSTS_AND_CONTRACTS](COSTS_AND_CONTRACTS.md) |
| ぬる（AIメンバー）の仕組み・運用 | [NUL_OVERVIEW](NUL_OVERVIEW.md)・[RUNBOOK §16](RUNBOOK.md)・[NUL_LAUNCH](NUL_LAUNCH.md)（有効化の記録と再有効化の手順） |
| AIエージェントが引き継ぐ | [../AGENTS.md](../AGENTS.md)・[AI_GUIDE](AI_GUIDE.md)・[handoffs/LATEST](handoffs/LATEST.md) |

## 文書の役割（正本）

### 概要・経緯
| 文書 | 役割 |
|---|---|
| [FEATURES](FEATURES.md) | **いま実装済みの機能・仕様の一覧**（本番で何ができるか）と、スコープ外・未実装 |
| [HISTORY](HISTORY.md) | 開発の経緯（年表）・主な設計判断の理由・運用で学んだこと |
| [DECISIONS](DECISIONS.md) | **判断記録の正本**（D01〜D15＋暫定採用NUL-1〜14ほか）。確定事項と暫定採用を区別 |
| [BACKLOG](BACKLOG.md) | **進捗の正本**（Phaseごとの作業と状態）・今後の予定と課題 |
| [TASKS](TASKS.md) | 各Phaseの受け入れ条件（進捗の記載はBACKLOGへ） |

### 仕様・設計
| 文書 | 役割 |
|---|---|
| [REQUIREMENTS](REQUIREMENTS.md) | 運用責任者が決めた要件とスコープ外 |
| [ARCHITECTURE](ARCHITECTURE.md) | サービス境界・責任・セキュリティ・ディレクトリ構成 |
| [DATABASE](DATABASE.md) | 論理モデルとデータ整合性（**DDLの正本は `db/migrations`**） |
| [API](API.md) | APIの契約と権限（認可表の正本は [AUTHZ](AUTHZ.md)＋`services/api/internal/authz`） |
| [DESIGN](DESIGN.md) | UI・デザインの方針 |
| [FRONTEND](FRONTEND.md) | フロントエンドの現状、単体開発・モック・共通部品の比較と段階導入案（レビュー待ち） |
| [design/ui-overhaul.md](design/ui-overhaul.md) | **UI/UX大改善プロジェクトの計画・仕様・進捗**（物品・記事・プロフィール・全体。実施中） |
| [design/ui-overhaul-2.md](design/ui-overhaul-2.md) | **UI/UX第2フェーズ**（物品の返却・カテゴリ、記事の削除・編集画面、ホーム・モーション）と、**ポータル全体のデザイン原則・モーション設計** |
| [design/](design/) | 設計メモ（履歴。各ファイル冒頭に状態を書く）: 認証・内部UI・CMS・メディア・公式サイト配信・**ぬる（`nul-agent.md`=仕様v0.1、`nul-agent-p1.md`=P1計画、`nul-admin-dashboard.md`=管理画面の設計）** |
| [LEGACY_SITE](LEGACY_SITE.md)・[legacy-baseline/](legacy-baseline/README.md) | 既存公式サイトの調査と、移設前の比較基準（P0時点の記録） |

### 開発・運用
| 文書 | 役割 |
|---|---|
| [START_HERE](START_HERE.md) | 開発の入口（一続きの手順） |
| [LOCAL_DEV](LOCAL_DEV.md)・[WSL_SETUP](WSL_SETUP.md) | ローカル起動・開発PCの準備 |
| [DEVELOPMENT](DEVELOPMENT.md)・[TESTING](TESTING.md) | ブランチ・PR・マージの運用／何をどう検証するか |
| [TOOLCHAIN](TOOLCHAIN.md) | **使っている依存・外部サービスと固定版・採用理由** |
| [OPERATIONS](OPERATIONS.md) | 運用の**方針と契約**（なぜそうするか。TLS・バックアップ・デプロイ・画像など） |
| [RUNBOOK](RUNBOOK.md) | 運用の**実績と手順**（本番の構成・値・操作・障害対応・引き継ぎ）。**サービス構成・`.env`・イメージの正本** |
| [SERVER_SETUP](SERVER_SETUP.md) | VPSの新規構築・移行の手順 |
| [COSTS_AND_CONTRACTS](COSTS_AND_CONTRACTS.md) | 費用・契約・更新の台帳 |

### ぬる
| 文書 | 役割 |
|---|---|
| [NUL_OVERVIEW](NUL_OVERVIEW.md) | **いま動いているぬるの仕組み**（人格・記憶・自律行動・モデル・予算・安全・データ） |
| [design/nul-agent.md](design/nul-agent.md) | 仕様の経緯と理由（v0.1。実装の正本ではない） |
| [NUL_LAUNCH](NUL_LAUNCH.md) | 本番有効化の記録と、再有効化（移行時）の手順・実機テストのチェックリスト |
| [RUNBOOK §16](RUNBOOK.md) | 日常の運用（設定・停止・SQL） |
| [handoffs/163-review.md](handoffs/163-review.md) | 運用前の独立レビューの結果と対応 |

### AIエージェント向け
| 文書 | 役割 |
|---|---|
| [../AGENTS.md](../AGENTS.md)（`CLAUDE.md`が読み込む） | **ルールの正本** |
| [AI_GUIDE](AI_GUIDE.md) | 設計思想・実装状況・作業の落とし穴 |
| [CLAUDE_TEAM](CLAUDE_TEAM.md) | Claude Teamの課金境界・モデル選択・中断と再開 |
| [CLAUDE_HANDOFF](CLAUDE_HANDOFF.md) | 新しいセッションの始め方（依頼文のひな形） |
| [handoffs/](handoffs/README.md) | 作業ごとの進捗記録（**過去の記録。正本ではない**）。最新は [LATEST](handoffs/LATEST.md) |

## 文書を直すときのルール

- **実装・本番の実態が正**。文書を読んで実装と違えば、実態を確認して文書を直す。
- 同じ内容を複数の文書に書かない（片方が古くなる）。**正本に書き、他からはリンクする**。
- 状態を表す言葉（「未実装」「未契約」「本番に接続していない」など）は古くなりやすい。書くなら日付を付ける。
- **秘密（トークン・パスワード・APIキー・鍵・WebhookのURL）と個人情報（実在のメール・ログインID・学生情報）は書かない**。IPアドレスはDNSで公開されている情報として、運用手順に必要な範囲で書いてよい。
- 文書だけを変えるPRは、デプロイが動かない（`paths-ignore`）。リンク切れは `node scripts/check.mjs` が検査する。
