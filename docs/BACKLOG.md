# 実装用の小さい作業単位

[TASKS](TASKS.md) のPhaseは維持する。以下のIDは分割案でありGitHub Issue番号ではない。Issue作成後は実番号を併記し、完了は実PRと検証結果に基づいて更新する。

作成済み: [準備PR #1](https://github.com/nu-digital/nu_app/pull/1)、[P0-01 / Issue #2](https://github.com/nu-digital/nu_app/issues/2)。P0-01は完了（PR #5。調査結果は[legacy-baseline](legacy-baseline/README.md)）。P0-02〜P0-04は完了（PR #10・#14・#16・#18・#20と P0-04。[P0完了報告](handoffs/P0-complete.md)）。P1は進行中: P1-01（#26・#30・#28）はmainにマージ済み。P1-02（認証・招待・再設定・セッション・CSRF・レート制限・停止）は積み重ねDraft PR（#32 → #34 → #36 → #38 → #40 → #42）でレビュー待ち。P1-03（#44 Editor/Adminロール、#46 Owner二段階移譲、#48 緊急復旧CLI）も積み重ねDraft PRで実装済み。P1-04（内部アプリUI）は#50で実装済み（Draft PR）。P1の実装は一通り完了で、運用責任者のレビューと実機確認待ち。P2-01（プロフィール）は積み重ねDraft PR（#54 API → #56 メンバー一覧・個人情報 → #58 画面）で実装済み。次はP2-02（物品登録・貸出・返却）。実装ブランチ例は `chore/2-legacy-baseline`。

| ID | 作業と完了条件 | 依存 |
|---|---|---|
| P0-01 | 既存サイトを再現し、67記事・ルート・画像・日付・ギャラリーの基準を採取。実コマンドと画像を記録 | 準備PR |
| P0-02 | `git subtree add`（squashなし）で履歴/ライセンスを保ってapps/webへ移設。P0-01と同じlockfile。workspace、lint/typecheck/build、URLと表示smokeをCIへ追加（D13） | P0-01 |
| P0-02b | 移設直後の独立PRで依存更新: audit/公式アドバイザリーを再確認しサポート中の固定版を選定、next/eslint-config-next整合、Playwright等固定。`audit fix --force`・無条件の最新メジャーは不可。lint/型/build/主要操作/表示回帰（D11） | P0-02 |
| P0-03 | apps/admin・Go API/Bot・DB境界、固定toolchain、開発Compose、架空.env.example、起動手順。機能本体なし | P0-02 |
| P0-04 | Linuxで新規clone起動と全build、CI必須化、UI基準確認。P0受け入れを証拠付きで完了 | P0-03 |
| P1-01 | 認証schema、認可表、CLI bootstrap。実DBと競合テスト | P0 |
| P1-02 | 招待・再設定・セッション・CSRF・レート制限・停止と失効。正常/拒否系 | P1-01 |
| P1-03 | Admin/Editor管理、最後のOwner保護、Owner二段階移譲、緊急復旧CLI。権限と競合テスト | P1-02 |
| P1-04 | 内部アプリUI調査・共通トークン・ログイン・写真ランチャー。PC/スマホE2E | P1-02 |
| P2-01 | プロフィール/メンバー内・インターネット公開の別同意/個人情報削除。API漏えいテスト | P1 |
| P2-02 | 物品登録と数量/個体貸出・代理/部分返却。DB同時実行テスト | P1 |
| P2-03 | 物品検索とスマホ操作、訂正イベント/履歴。画面・権限・競合・冪等検証（a: 訂正API #63、b: 会員向け画面 #65 完了、c: 管理者画面（物品登録・個体・総数調整・廃止・訂正）#67 完了） | P2-02 |
| P2-04 | 共通outboxとDiscord worker・設定。429/障害/再送でも業務確定（a: outbox・設定/一覧/再送API #69 完了、b: Discord送信worker・接続テストAPI #71 完了、c: 設定画面 #73 完了） | P2-02 |
| P3-01 | CMS版管理・共同編集・公開API。権限/競合/draft漏えい検証（設計: `docs/design/p3-cms.md`・Opusレビュー反映済み。a: 作成・版保存・共同編集 #77 完了、b: 公開・公開API・匿名化） | P1 |
| P3-02 | メディア永続化・認可・公開と非公開の分離。D06の上限・縮小・再エンコード。画像攻撃/参照整合性/メモリ実測 | P3-01 |
| P3-03 | CMS UI・制作クレジット・公開同意/撤回の匿名化・YouTubeのみ埋め込みとサニタイズ | P3-02 |
| P3-04 | 再実行可能な記事/画像移行、公開サイトAPI接続、全URL/表示比較 | P3-03 |
| P4-01 | Actions経由のUbuntuデプロイ、Nginx/TLS自動更新と失敗検知/secret/healthcheck/失敗通知/切り戻し | P1基盤、VPS契約後の実情報 |
| P4-02 | DB+画像のVPS内バックアップ(毎日03:00 JST・7世代)/復元検証、4GB実測、運用手順、公開前総合検証 | 対象Phase完成 |

P4は最後まで放置せず各Phaseで逐次実装する。本番公開は運用準備と公開対象機能の受け入れが揃ってから。大きすぎるIssueはさらに分割し、機能途中でもmainを壊さない単位にする。

最初に作るIssueはP0-01。本文には [Issueテンプレート](../.github/ISSUE_TEMPLATE/task.yml) の6項目、対象要件、非対象、テスト、判断待ちを埋める。先のPhaseの未確定仕様を確定済みとして一括起票しない。
