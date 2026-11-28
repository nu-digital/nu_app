# 第3フェーズ 進捗（#199 P0 / #201 会計P1）

更新: 2026-11-29。事実と未実施を分ける。

## 完了（PR #200・#202。Draft。マージ・本番反映は運用責任者の承認待ち）
- #200（P0）: DM廃止（Intents・受信破棄・応答拒否・DMからの/nul無視）、人格改訂・プロンプト簡略化、リマインダー作成1日10回・1返答のツール6回、ツール監査ログ（00024）、10分に拒否・失敗30件で自動全停止、文脈の文字数上限、起動時のDM由来データ削除（`PurgeDMData`・冪等）、ツール許可リストの固定テスト、Administratorを維持するリスクのdocs。`go vet`・`go test`・`scripts/test-integration.sh`成功（ローカル）。
- #202（会計P1の一部。#200の上に積んだ）: migration 00025（年度・カテゴリ・予算枠・projects・規則・申請・履歴。申請削除不可・履歴追記のみ）、`internal/expense`（金額計算・再承認・状態遷移・職務分掌・集計）。単体・統合テスト成功。
- 設計: design/phase3-nul-safety.md、design/phase3-accounting.md（既存シート調査・集計範囲の差+35,220円・規則・方針・Drive OAuth・移行設計・運用責任者の作業一覧）。

## 未実施・次の一手
1. CIの結果確認（#200・#202。最新コミットで実行中だった）。
2. 会計P1の残り: APIハンドラー（申請・承認・予算・projects・規則）・authz表（`expense.*`。API.md/AUTHZ.mdと1対1テスト）・管理画面。
3. P2（URL取得のSSRF防御・AI下書き）→P3（承認・購入報告・Discord通知）→P4（Sheets同期）→P5（Drive領収書・OAuth）→P6→P7。
4. **本番反映時の注意**: 起動時にDM由来データが削除される（件数は`settings.dm_purge.last`）。古いバックアップにはDMが残る。デプロイ前にバックアップ確認。
5. 承認待ち: 本番デプロイ・スプレッドシートの修正（集計範囲）・所有権移譲・既存領収書の移行・Google OAuth設定と秘密の登録（手順は design/phase3-accounting.md §12）。
