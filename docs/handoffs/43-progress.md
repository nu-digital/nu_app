# 引き継ぎ: Issue #43 P1-03a ロール付与・解除

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/43-role-management`（base: `feat/41-admin-users-api`＝PR #42。積み重ね: #32→#34→#36→#38→#40→#42→このPR）
- 完了: `PUT /admin/users/{id}/editor-role`（Admin/Owner）、`PUT /owner/users/{id}/admin-role`（Ownerのみ）。冪等、変更があったときだけ監査、Owner権限は扱わない。付与・剥奪は対象の既存セッションの次のリクエストで反映
- 実測（実PostgreSQL）: 認可マトリクス（匿名401・Member/Editor 403・AdminはEditorのみ・Admin付与はOwnerのみ・CSRF無し403）、冪等と監査、不正入力・存在しないID、付与/剥奪が既存セッションのmeと管理操作に即時反映、Adminは Owner専用APIを使えない・Ownerにはならない・停止後は何もできない。ミューテーション（解除を無効化／付与の監査を外す）でテストが失敗、復元済み
- 未実施: PR CI結果。Owner移譲（P1-03b）、緊急復旧CLI（P1-03c）
