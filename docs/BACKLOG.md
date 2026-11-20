# 実装用の小さい作業単位

[TASKS](TASKS.md) のPhaseは維持する。以下のIDは分割案でありGitHub Issue番号ではない。Issue作成後は実番号を併記し、完了は実PRと検証結果に基づいて更新する。

作成済み: [準備PR #1](https://github.com/nu-digital/nu_app/pull/1)、[P0-01 / Issue #2](https://github.com/nu-digital/nu_app/issues/2)。P0-01は未着手。実装ブランチ例は `chore/2-legacy-baseline`。

| ID | 作業と完了条件 | 依存 |
|---|---|---|
| P0-01 | 既存サイトを再現し、67記事・ルート・画像・日付・ギャラリーの基準を採取。実コマンドと画像を記録 | 準備PR |
| P0-02 | 履歴/ライセンスを保ってapps/webへ移設。workspace、lint/typecheck/build、URLと表示smokeをCIへ追加 | P0-01 |
| P0-03 | apps/admin・Go API/Bot・DB境界、固定toolchain、開発Compose、架空.env.example、起動手順。機能本体なし | P0-02 |
| P0-04 | Linuxで新規clone起動と全build、CI必須化、UI基準確認。P0受け入れを証拠付きで完了 | P0-03 |
| P1-01 | 認証schema、認可表、CLI bootstrap。実DBと競合テスト | P0、D02の必要部分 |
| P1-02 | 招待・再設定・セッション・CSRF・レート制限・停止と失効。正常/拒否系 | P1-01 |
| P1-03 | Admin/Editor管理、最後のOwner保護、移譲。権限と競合テスト | P1-02、D05 |
| P1-04 | 内部アプリUI調査・共通トークン・ログイン・写真ランチャー。PC/スマホE2E | P1-02 |
| P2-01 | プロフィール/公開設定/個人情報削除。API漏えいテスト | P1、D02、D08 |
| P2-02 | 物品登録と数量/個体貸出・代理/部分返却。DB同時実行テスト | P1 |
| P2-03 | 物品検索とスマホ操作、訂正/履歴。画面・権限・競合検証 | P2-02、D09 |
| P2-04 | 共通outboxとDiscord worker・設定。429/障害/再送でも業務確定 | P2-02 |
| P3-01 | CMS版管理・共同編集・公開API。権限/競合/draft漏えい検証 | P1、D01 |
| P3-02 | メディア永続化・認可・公開と非公開の分離。画像攻撃/参照整合性検証 | P3-01、D06 |
| P3-03 | CMS UI・制作クレジット・公開同意・サニタイズ | P3-02、D08、D10 |
| P3-04 | 再実行可能な記事/画像移行、公開サイトAPI接続、全URL/表示比較 | P3-03 |
| P4-01 | Actions経由のUbuntuデプロイ、TLS/secret/healthcheck/失敗通知/切り戻し | P1基盤、D03、D07 |
| P4-02 | DB+画像のVPS内バックアップ/復元、4GB実測、運用手順、公開前総合検証 | D04、対象Phase完成 |

P4は最後まで放置せず各Phaseで逐次実装する。本番公開は運用準備と公開対象機能の受け入れが揃ってから。大きすぎるIssueはさらに分割し、機能途中でもmainを壊さない単位にする。

最初に作るIssueはP0-01。本文には [Issueテンプレート](../.github/ISSUE_TEMPLATE/task.yml) の6項目、対象要件、非対象、テスト、判断待ちを埋める。先のPhaseの未確定仕様を確定済みとして一括起票しない。
