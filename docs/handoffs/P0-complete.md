# P0完了報告と受け入れ証拠（2026-11-21）

対象: docs/TASKS.md P0。完了したPR: #5(P0-01)、#10(P0-02)、#14(P0-02b)、#16(P0-03a)、#18(P0-03b)、#20(P0-03c)、P0-04（Issue #21）。副次: #12（自律マージ運用）、#8（D01〜D13反映）。

## 受け入れ条件の照合

| 受け入れ条件 | 結果 | 証拠 |
|---|---|---|
| 新規PC上でREADMEに沿って起動できる | 達成 | 新規clone（`git clone`、`cp .env.example .env`、`npm run dev:up`）でdb/api/bot/web/adminが全てhealthy、api `/healthz` `/readyz` 200、admin `/healthz` 200、web主要URL200。`npm run web:ci`/`admin:ci`/`check`/`test:unit`/`build`/`web:verify`すべて成功。`dev:reset`でvolume削除まで確認。**ベースのイメージ層キャッシュがある環境での実測（55秒）。キャッシュ無しの初回所要時間は未計測** |
| 既存サイトの主要ページが移行前と同等 | 達成 | `web:verify`: 74ルート・67記事・532画像が移設前基準と一致。ブラウザー72ルート200・ギャラリー25チェック・横溢れ0。依存更新後も旧ビルドと新ビルドは同一Chromiumで撮影17枚がバイト同一 |
| 秘密情報がGitに含まれない | 達成（走査範囲内） | `.env`系・鍵ファイルのtrack無し。パターン走査の該当は架空のローカル用値（`.env.example`、smokeの一時env）のみ。ユーザーから伝えられた機密値がリポジトリ・履歴にないことを全履歴検索で確認。**秘密値の完全検出は保証しない**（専用scannerは未導入） |

## P0のチェックリスト

- モノレポ構成（apps/web・apps/admin・services/api・services/bot・db・infrastructure・docs）: 済
- 既存サイトの再現・基準保存: 済（docs/legacy-baseline）
- 履歴付き移設（subtree、squashなし、tree同一）: 済（#10）
- 依存の固定と更新（next 16.4.6、audit 21→0、Playwright 1.65.1、Go 1.27.4、PostgreSQL 18.7、pgx、goose）: 済（docs/TOOLCHAIN.md）
- Docker Composeでローカル起動、`.env.example`、手順、CI: 済（docs/LOCAL_DEV.md）
- main保護とCI必須化: P0-04で有効化（docs/DEVELOPMENT.md）

## 実行したCI（main最新 / 各PR最新コミット）
harness（repository・policy）、web、admin、go（api・bot・docker build）、compose smoke。すべて対象コミットで成功を確認してからマージ。

## 未実施・既知の問題
- 基準画像の人による確認（Claudeは15/17枚を目視、崩れなし）。公開Pagesとの実画面比較は未実施。
- Firefox/WebKit、幅320px・200%ズーム、実機タッチ、外部リンク/YouTube再生は未確認。
- L3（ライトボックスのロゴ重なり）など既存の見た目の不具合は未修正（移設では直さない方針）。
- Dependabot PR #3・#4（actions/checkout 7、setup-node 7）は未対応。メジャー更新のため、全workflowのpinと合わせて別PRで確認して対応する。
- ベースイメージのdigest更新、`npm audit`の継続確認（本番公開前に再確認）。
- 本番関連は一切未実施（VPS未契約、DNS未設定、デプロイworkflow無し）。

## P1の着手計画（BACKLOG P1-01〜P1-04）

順序: P1-01 → P1-02 → P1-03 / P1-04（P1-02後に並行可）。各PRは小さく、実PostgreSQLの統合テストをCIに追加する（TESTING P1の必須シナリオを実テスト名へ対応付ける）。

1. **P1-01 認証schema・認可表・CLI bootstrap**: users / password_credentials / sessions / one_time_tokens / user_roles / audit_logs / profiles(表示名のみ必須) / public_credit_consents / owner_transfer_requestsのmigration、認可表（匿名/Member/Editor/Admin/Owner＋停止ユーザー）、Owner作成のCLI（単回・再実行拒否・競合テスト）。sqlc v1.31.1とpgxを最初に使う。統合テストはCIでPostgreSQL 18.7を起動。
2. **P1-02 招待・再設定・ログイン**: 招待/再設定URL（期限・使い捨て・並行利用）、Argon2idログイン、サーバー管理セッション(HttpOnly/Secure/SameSite)、CSRF/Origin、レート制限、停止時のセッション失効。
3. **P1-03 Admin/Editor管理・Owner移譲・緊急復旧CLI**: 権限付与と最後のOwner保護、24時間の二段階移譲（申請・取消・失効・承諾の競合テスト）、緊急復旧CLI（セッション失効・監査）。
4. **P1-04 内部アプリUI**: `docs/DESIGN.md`に従い日本の公開サービスを調査して`docs/design/<issue>.md`に記録、共通トークン、ログイン、写真ランチャー、PC/スマホE2E。

**P1で人の確認が必要になる見込みの点**（独立作業は止めずに進める）:
- 認証・認可の重要設計（セッション寿命、パスワード方針、招待URLの有効期限など数値）: 私が案を出し、DECISIONSへ提案として記録してから実装する。重要な設計変更に当たるため、確認を求める。
- 初期OwnerのCLI作成そのもの、main自動デプロイ基盤（P1の「PR CIとmain自動デプロイの基盤・ヘルスチェック」）は**本番デプロイ・VPS契約・DNS**に触れるため、契約と実情報が揃うまで実装・有効化しない。デプロイworkflowは「無効のまま用意」まで。
- プロフィール項目の具体的な一覧（任意項目の種類）は、D02の範囲で最小の案を出して確認する。
