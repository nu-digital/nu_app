# 引き継ぎ: Issue #75 P3-01 CMS設計とOpusレビュー

- 2026-11-21 / Claude Code（Sonnet 5.5。設計レビューのみOpusのsubagent 1件）。ブランチ `docs/75-p3-cms-design`（base: main）。docsのみ。
- 完了: `docs/design/p3-cms.md`（段階分割P3-01a/b〜P3-04、データ・認可・版/公開の競合・Markdown検査・公開APIと匿名化・通知・テスト計画）。Opusレビュー（指摘15件・高4）を反映し、冒頭の反映一覧に残した。
- Opusが指摘し反映した主な点: (1)publishは`revisionNo`必須（確認していない版の公開を防ぐ）、(2)制作メンバーを版ごとに持つ（公開後の変更が公開されない）、(3)描画側`rehype-sanitize`を必須に（既存`ContentArticle`は`rehype-raw`のみ）、(4)Memberの一覧は公開版の列だけ（作業版タイトルを出さない）、書き込みは行ロック後に関係を読み直す、403/404の表、ETagは応答全体のハッシュ、HTMLコメント・`metadata.author`の扱い、許可リストの細部、サイズ/頻度の上限、複合FKの作り方。
- 自分で事実確認した点: 23記事にHTMLコメント、`recall.md`に属性なしの`div`、公開サイトの描画にサニタイズ無し、`authorize()`は関係が無いと403、個人情報削除で同意も撤回される。
- 未確認（Opusも未確認）: goldmarkの挙動（リンク先のエンティティ解読の有無など）。実装時に実測してテストに固定する。
- 未実施: 実装（P3-01a以降）。依存（goldmark v1.8.7＝2026-10-30公開、golang.org/x/net）の採用記録とgovulncheck。
- 次の一手: P3-01a（migration・Markdown検査・作成/一覧/取得/版保存/共同編集者/制作メンバーのAPI）。
