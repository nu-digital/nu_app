# テスト戦略

目的は、個人情報と活動履歴を守りながら変更できること。全行カバレッジを目標にせず、危険な境界と実際の操作を検証する。失敗したテストをskip、無条件成功、スナップショット一括更新で通さない。

## 実際に実行できるコマンドと、CIとの対応（2026-11-28時点）

| 何を検証するか | コマンド（ローカル） | CIジョブ（`.github/workflows/`） |
|---|---|---|
| 文書・設定・命名・禁止ファイルの準備用チェック、チェック自体のテスト | `node scripts/check.mjs`・`node --test scripts/harness.test.mjs` | `harness / repository`・`harness / policy` |
| 公式サイト（lint・型・ビルド・単体） | `npm run web:ci && npm run web:lint && npm run web:typecheck && npm run web:build`・`npm run test:unit` | `web / lint-typecheck-build` |
| 内部ポータル（lint・型・ビルド・単体） | `npm run admin:ci && npm run admin:lint && npm run admin:typecheck && npm run admin:test && npm run admin:build` | `admin / lint-typecheck-build` |
| Go（api・bot・**agent**）の整形・vet・単体・race | `scripts/go-docker.sh services/<名前> sh -c 'test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./...'` | `go / services/api`・`go / services/bot`・`go / services/agent` |
| DB・クエリ（実PostgreSQLで本物のmigrationを適用。api・agent） | `scripts/test-integration.sh`（`TEST_DATABASE_URL`が無いと失敗し、skipしない）・`scripts/sqlc.sh generate`／`diff` | `go / integration`・`go / sqlc` |
| コンテナのビルド（api・bot・migrate・agent） | `docker build`（CIと同じ） | `go / docker build` |
| 全サービスの起動・全ルート・DB停止時の挙動・既存サイトとの一致・画面E2E | `npm run compose:smoke`（専用プロジェクトで起動して片付ける）・`npm run web:site-check`・`npm run web:e2e` | `compose / smoke` |
| バックアップ→隔離環境での復元・Nginx・デプロイスクリプト・証明書確認 | `scripts/backup-restore-test.sh`・`scripts/nginx-test.sh`・`scripts/deploy-test.sh`・`scripts/cert-check-test.sh` | `ops / backup-restore`・`ops / nginx`・`ops / deploy-script` |

- マージの必須チェック（ブランチ保護）は[DEVELOPMENT](DEVELOPMENT.md)。デプロイは、そのコミットの**CI全成功**（`ops`・`go / services/agent`を含む）を確認してから進む（`deploy.yml`の`verify`）。
- **ぬる（`services/agent`）のテスト方針**: 実際のClaude API・Discordを呼ばない。`llm.Fake`（偽のClaude）・`llm.FakeBatch`・偽のSender/Store/設定で、判断・予算の段階・全停止/静か・公開/非公開・DM・忘れる処理・プロンプトの固定部分・ツールの安全（他人のデータを触れない）・入力のデータ扱いを検証する。DBを使う部分は統合テスト（実PostgreSQL）。**実機でしか分からないこと（Discordの権限の実際・応答の質・費用の実績）は、[NUL_LAUNCH](NUL_LAUNCH.md)のチェックリストで人が確認する**。
- **実装した対象のテストを、同じPRでCIに足す**。導入済みの対象を、ファイルの存在チェックなどでskipして成功扱いにしない。
- `node scripts/check.mjs`は準備用チェック（文書のリンク・設定・命名規則）で、**アプリの機能テストの成功を意味しない**。

## 検証の層（方針）

| 層 | 手段・方針 |
|---|---|
| 静的検査 | Next ESLint、`tsc --noEmit`、Go gofmt/vet、ビルド |
| 単体 | Go標準testing、UIロジックのテスト。実装のコピーではなく、入力と利用者から見える結果 |
| API・DB統合 | 本番と同じPostgreSQLメジャー、実migration、HTTP handler経由で認証と認可。SQLiteで代替しない |
| 並行・障害 | 複数DB接続、同時要求、件数・残数・履歴・outbox・上限の照合。外部Discord・Claudeは偽物で429/5xx/timeout |
| E2E | Playwright。実API・ローカルDB・架空seedで主要導線。モックだけで認証完了としない |
| 表示回帰 | 同じブラウザー・viewportで、移設前基準との比較（`web:site-check`・`compose / smoke`）。差分は人が確認 |
| 運用 | コンテナbuild・health/readiness・migration互換性・復元・前版イメージへの復帰・Nginx |

ツールの具体的バージョンは[TOOLCHAIN](TOOLCHAIN.md)。

## Phase別の必須シナリオ

| Phase | 完了前に通す検証 |
|---|---|
| P0 | 新規cloneから起動、lockfile install、lint/typecheck/build、Compose設定と起動、画像・67件の既存記事・主要URL、ギャラリーの拡大/左右/Esc/フォーカス復帰 |
| P1 | Owner継承（申請24時間・承諾前の取消・失効後の承諾409・同時承諾/同時申請・承諾で旧OwnerがAdmin・最後のOwnerが消える操作の拒否）、緊急復旧CLI（公開Web入口なし・セッション失効・監査・最後のOwner保護）、表示名のみ必須で他が未入力でも主要機能が動く、メンバー内公開とインターネット公開の同意が別、bootstrapの競合/再実行拒否、招待/再設定tokenの期限・再使用・並行利用、ログアウト/停止/権限変更時の既存セッション失効、CSRF/Origin、レート制限、匿名401・Member→管理403・Admin→Owner403、最後のOwner保護 |
| P2 | 訂正API（Admin/Owner以外は403、理由必須、元記録不変・訂正イベント追加、在庫が負/返却数>貸出数は拒否、同一`Idempotency-Key`の再送で二重適用なし、訂正と通常の貸出/返却の同時実行で整合）、非公開項目が一覧/詳細/エラー/ログに出ない、本人と管理者の閲覧範囲、代理返却actor、部分返却、残1への同時貸出の成功1件、同時返却の超過拒否、個体二重貸出拒否、廃止/数量変更競合、Discord障害でも業務確定・outbox再送 |
| P3 | 画像アップロード（JPEG/PNG/WebP以外・SVG・アニメ・拡張子偽装・超過サイズ/画素/辺・EXIF除去・向き補正・拡大しない・縮小上限・同時処理制限）、既存画像は変換されず同一sha256、YouTubeのみ埋め込み(許可ホスト/ID検査)・生iframe/script/video拒否・既存11記事のYouTube表示回帰、部分日付（年月のみ・期間・日付不明）の表示と並び・補完日を開催日として出さない、公開クレジットの撤回/個人情報削除で公開版・キャッシュ・古いrevisionも匿名、制作メンバー登録と同意が別、全ロール×作者/共同編集者/第三者の認可、他人のIDによる越権拒否、公開中の編集で旧版維持、古いrevision409、匿名へのdraft本文/metadata/検索/画像/キャッシュ漏えいなし、危険HTML/URL/画像拒否、公開クレジット同意、移行再実行で重複なし |
| P4 | 毎日03:00 JSTのバックアップ・7世代保持・8世代目で最古のみ削除・失敗時に既存世代が残り通知・DB+メディアの整合復元（隔離環境）・容量不足は黙って世代を減らさず報告、Nginx証明書の自動更新と更新失敗/期限接近の検知、空DB初期化と前版schemaからの更新、migration二重実行制御、旧アプリ互換性、DB+メディア復元と参照一致、前版イメージ切り戻し、失敗通知、4GB VPSの実測、秘密値のマスキング |

各シナリオをIssueの受け入れ条件・実テスト名へ対応付ける。成功HTTPだけでなくDBの不変条件もassertする。認可表は匿名/Member/Editor/Admin/Ownerに加え、停止ユーザーと他人のリソースを含める。

## データと再現性

- `example.invalid`等の架空データのみ。本番DB、実在学生の情報、実Discord tokenをfixtureや画像に含めない。
- 統合テストは専用DBと一意なCompose projectを使う。削除対象がテスト専用と確認できなければDB/volumeを消さない。
- 時間・乱数・再試行間隔を制御可能にし、sleep頼みの競合テストを避ける。Goのrace detectorだけではDB競合を証明できない。
- 外部通信は固定fixtureまたは偽サーバーへ。E2Eのtrace・スクリーンショットにセッションや秘密値を残さない。失敗artifactの保存は必要最小限、短期間にする。
- migrationは空DB適用、runner再実行時の適用済み扱い、直前schemaからの更新を確認。SQLを何度でも直接実行可能にするという意味ではない。

## 画面の受け入れ

PC 1440×900、スマホ390×844を基本に、幅320pxで横溢れ、200%ズーム、キーボードのみの操作を確認。日本語長文・未入力・空一覧・読込中・通信失敗・権限不足・保存競合を含める。日時の並びと日数は固定時刻でテストする。

スクリーンショット差分の許容値を失敗回避のために広げない。意図した変更点と比較画像をPRに添える。既存サイトの基準は初回に人が確認し、見ていない画像を基準に採用しない。

## PRの結果記録

```text
対象コミット: <SHA>
環境: OS / Node / Go / PostgreSQL / ブラウザー
実行: <実際のコマンド>
結果: 終了コード、件数、観測した結果
未実施: <項目、理由、再実行方法>
既知の問題: <影響範囲>
```

CIの成功と手動確認を別々に記載する。重要テスト未実施ならDraftを維持し、受け入れ済みにしない。依存更新でも認可・公開境界など影響する回帰テストを実行する。
