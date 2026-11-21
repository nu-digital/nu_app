# 引き継ぎ: Issue #59 P2-02a 物品のスキーマと登録・一覧API

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/59-inventory-items`（base: `feat/57-profile-ui`＝PR #58。積み重ね: …→#58→このPR）
- 完了: migration 00004（inventory_items・units・loans・returns・adjustments。貸出・返却・調整は追記専用trigger、個体の二重貸出はcurrent_loan_idのUNIQUE、数量/総数/個数のCHECK）、`POST/PATCH /inventory/items`・`POST /inventory/items/{id}/units`（Admin）、`GET /inventory/items`（検索・カテゴリ・利用可否・ページング）・`GET /inventory/items/{id}`（個体状況・未返却・経過日数）
- 実測（実PostgreSQL）: 認可（Memberは閲覧のみ）、作成の検証、総数変更は理由必須・履歴・追記専用、個体登録（管理番号の重複・方式違い・廃止）、方式変更と廃止の制約、一覧の検索/カテゴリ/利用可能/ページング、DB制約（個体二重貸出不可・追記専用・数量CHECK）。ミューテーション4種（理由不要化／個体登録の方式チェックを外す／方式変更の制限を外す／DBの二重貸出ユニーク制約を外す）でテストが失敗、復元済み
- 実装中に見つけた不具合: 方式変更を別UPDATEにすると総数のCHECKで失敗する（500）→ 方式と総数を1つのUPDATEにまとめた
- 未実施: PR CI結果。貸出・返却（P2-02b）、画面・訂正API（P2-03）、写真（P3）
