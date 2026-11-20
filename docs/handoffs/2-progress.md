# 引き継ぎ: Issue #2 P0-01 既存公式サイトの比較基準

- 日付 / 担当: 2026-11-20 / Claude Code（Sonnet 5.5）
- Issue / PR: #2 / Draft PR（番号と最終SHAはPR本文を正とする）
- ブランチ / 対象SHA: `chore/2-legacy-baseline`、基点 main `8481964`。調査対象upstream `e1c031389c6cb621742caf4b270316da93b85168`
- Claude CLI版 / 実モデル / 課金: 実モデルSonnet 5.5。Team認証・追加使用量無効は人が確認する項目で、本セッションでは未確認。APIキー・追加課金・Fable・subagentは不使用
- 停止理由: なし（Issue範囲を完了。レビュー待ち）
- 試行した仮説・失敗履歴: Chromiumが起動できず（OSライブラリ不足）→sudo不使用でdebを展開しLD_LIBRARY_PATHで解決。日本語フォント無し→fonts-noto-cjkを展開。撮影スクリプトの初回は同一オリジンのprefetch中断(ERR_ABORTED)を失敗に数えて全ルート失敗扱いだったため、判定を修正して再実行。ギャラリーの「循環」チェックは初版が弱かったため厳密化して再実行
- 実行中だった処理: なし
- 対象要件 / 今回の非対象: P0-01。移設・CMS・認証・物品管理・Docker・本番操作は非対象
- 完了内容 / 未コミット変更: `docs/legacy-baseline/`（README、inventory.json、画面18枚+capture-result.json）、`scripts/legacy-inventory.mjs`、`scripts/legacy-baseline-capture.mjs`、LEGACY_SITE/DECISIONS(D10追記、D11〜D13)/BACKLOG更新
- 実行コマンド・結果: 詳細は `docs/legacy-baseline/README.md`。要点: upstream `npm ci` 0、`npm run lint` 0、`npx tsc --noEmit` 0、`npm run build` 0（root/basePath）、test:editor 1件成功、content-presentation 2件成功、全72ルート200、ギャラリー25チェック成功、横溢れ0。nu_app: `node scripts/check.mjs` 成功、`node --test scripts/harness.test.mjs` 9件成功
- 未実施検証と理由: 公開Pagesとの実画面比較、目視は18枚中3枚のみ、全記事目視、Firefox/WebKit、320px/200%ズーム、画像ピクセル差分（フォント非固定）、外部リンク/YouTube再生
- 既知の問題 / 判断待ち: 既存不具合L1〜L6（next 16.1.1のaudit指摘が最大）。D10〜D13は2026-11-21にユーザー確定（Issue #6 / docs/6-confirm-decisions）
- DB / API / 切り戻しへの影響: なし。このPRのdocs/scripts/画像をrevertすれば戻る。upstreamと本番は変更なし
- GitHubのCI・main保護・レビュー状態: main保護は未設定（privateのため403、DEVELOPMENT参照）。CI結果はPRで確認
- 次に実行する最小の作業: 人が基準画像18枚を確認→D13（権利・履歴方針）を決定→P0-02（apps/webへ移設）。Playwrightはリポジトリ依存に未追加
- 補足: 人からsudoパスワードが提示されたが使用せず記録もしていない。回避策で足りたため。Chromium用の展開ライブラリはscratchpadにあり、リポジトリに含まれない
