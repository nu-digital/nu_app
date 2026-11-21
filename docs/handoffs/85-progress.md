# 引き継ぎ: Issue #85 P3-03a CMS画面（一覧・編集・プレビュー・保存と競合・公開）

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/85-cms-ui`（base: main。#82・#84はマージ済み）
- 完了: `/contents`（一覧）・`/contents/new`・`/contents/[id]`（編集・プレビュー・保存・競合・公開・履歴）、ランチャーの「記事・作品」有効化、プレビューの**描画時サニタイズ**（`lib/markdown-preview.mjs`＋`components/MarkdownPreview.tsx`）、`npm run admin:test`（CIに追加）。暫定採用はDECISIONS P3-03-1・2。API変更なし。
- 実測: `npm run admin:lint`・`admin:typecheck`・`admin:build`・`admin:test`（7件）成功。**JSのサニタイズテスト**: 共有の事例集（Goの`mdcheck`と同じ`corpus.json`）の全件で出力が安全（script・iframe・イベント属性・style・class/id/name・危険なスキーム・許可外のhref/src・許可外のdiv属性が無い）、許可の入力は意図した要素が残る、攻撃的な入力33件（イベント属性・svg・math・form・base・meta・入れ子・CDATA・コメント・NULL・表のセル・プロトコル相対・バックスラッシュ・外部画像・エンティティのjavascript・autolink・参照定義 など）、YouTubeのdata属性は11桁IDのみ、既存67記事で出力が安全かつYouTube・details・画像の数が保たれる。**compose smoke 372項目OK**（PC/スマホ/320px: 下書き作成・保存・プレビュー・**保存前の入力に仕込んだXSS（img onerror・script・javascript:・iframe）がプレビューで実行されず取り除かれる**・危険な記述の保存は422で入力が保持される・保存の競合（編集者が先に保存→競合の案内と2つの選択肢→最新を読み込む）・編集者だけが公開できる・公開すると公開APIに出る・公開後の編集は再公開まで反映されない・一覧の状態表示・公開停止で404・無関係なメンバーには編集画面も一覧も出ない・横溢れなし・44px以上）。スクリーンショットは`.local/ui-shots/*-cms-*.png`（Git外）を目視。
- ミューテーション（JS）: `rehype-sanitize`を外す／URLの絞り込みを外す／データ属性の制限を外す、でテスト失敗。`javascript`プロトコルを許す変更は後段の絞り込みが落とすため失敗しない（二重防御）。
- テストが見つけた抜け: 描画側の許可リストが`div`のdata属性を複数同時に許していた（サーバーは「ちょうど1つ」）→`rehypeSingleDataAttr`で揃えた。
- 作成中の誤り: `node --test`にディレクトリを渡せない（globに）、テストの正規表現の順序・エスケープ、E2Eのユーザー作成の応答は`user.id`（`id`ではない）、事例集の件数の閾値、マージ待ちのうちに古いmainから分岐して事例集が古かった（マージ後にrebase）。
- 未実施: PR CI結果、画像のアップロードUI・カバー選択・本文への画像挿入（P3-03b）、共同編集者・制作メンバーの管理UI（P3-03b）、プロフィールの制作実績の表示（P3-03b）、公開同意の導線の文言（制作メンバーUIと一緒に）、公式サイト側の描画（P3-04。`apps/web`の`ContentArticle`は`rehype-raw`のみでサニタイズが無い＝CMS接続前に必ず塞ぐ）。
- 次の一手: P3-03b。
