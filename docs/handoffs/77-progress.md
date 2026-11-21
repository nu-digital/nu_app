# 引き継ぎ: Issue #77 P3-01a CMSの作成・版の保存・共同編集・制作メンバー・Markdown検査

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/77-cms-authoring`（base: `docs/75-p3-cms-design`＝PR #76。設計メモ）
- 完了: migration 00007（contents/content_revisions/content_collaborators/work_contributors/content_publication_events。複合FKで作業版・公開版のポインタを「同じコンテンツの版」に限定、版・制作メンバー・公開履歴は追記専用、日付CHECK）、`internal/mdcheck`（goldmark AST＋x/net/htmlの許可リスト検査）、`POST/GET /contents`・`GET /contents/{id}`・`POST .../revisions`・`PUT .../collaborators`・`PUT .../contributors`、保存系の頻度制限。依存: goldmark v1.8.7、x/net v0.62.0（TOOLCHAIN）。暫定採用はDECISIONS P3-01-1/2。
- 実測（`scripts/test-integration.sh`・実PostgreSQL）: CMSの統合テスト全成功＝作成の認可と入力検査（slug・区分・タイトル・日付・本文・メタデータ）、制限項目をMemberが変更できない、**403/404の表**（関係の無い人には存在する/しないが同じ404・同じ本文）、共同編集者の解除後の閲覧・保存が拒否、**一覧が編集できない人に作業版を出さない**、baseRevision競合409（現在の版を返す）、同一内容は版を増やさない、**8並行の同一base保存で成功1・競合7**、共同編集者（有効なユーザーのみ・作者は無視・上限20・停止後も維持）、制作メンバー（作品のみ・版ごと・公開版は不変・停止ユーザーも可・役割にURL/記号不可・同意は変えない）、DB制約（複合FK・追記専用・作品のみ・日付CHECK）、保存の頻度制限（429）。mdcheck: 事例集71件、**既存67記事すべてが（元URLコメントを除けば）通る**、コメント付きは拒否（23記事）、サイズ・深さの上限。
- ミューテーション: 行ロックを外す（並行テストが500で失敗。**最初はフック無しで検出できなかったため、判定の直前で揃えるフック`BeforeContentSave`を追加して検出**）／一覧に作業版を出す／Memberの制限項目の変更を許す／関係の判定を常に許可／同一内容の保存でも版を作る／コメントを許可／生テキスト照合を外す、でテストが失敗、復元済み。スキーム判定の除去は後段の「許可した形以外は拒否」が拒否するため失敗しない＝冗長な二重防御。
- 作成中の誤り: 検査にBOMが紛れ込みコンパイルエラー、sqlcが`CASE`をinterface{}と推論（`::text`等で型を明示）、トークナイザーが重複属性を黙って捨てる（生テキストの形も検査）、タブを挟んだjavascript:はMarkdownのリンクにならず文字のまま（事例の期待値を修正）、users停止のテストデータに`disabled_at`が必要、一覧のカーソル`%%%`がURLとして無効。
- 未実施: PR CI結果、**govulncheck（goldmark・x/net）**、publish/unpublish・公開API・匿名化（P3-01b）、描画側`rehype-sanitize`（P3-03/04）。
- 次の一手: P3-01b（publishは`revisionNo`必須、公開API、制作メンバーの配信時の匿名化、プロフィールの制作実績、公開の通知）。
