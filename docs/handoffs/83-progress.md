# 引き継ぎ: Issue #83 P3-02a メディア基盤とCMS連携

- 2026-11-21 / Claude Code（Sonnet 5.5）。ブランチ `feat/83-media`（base: `docs/81-p3-media-design`＝PR #82。設計メモ）
- 完了: `internal/media`（構造の自前検査・向き補正・縮小・再エンコード・保存）、migration 00009（`media_assets`・`media_references`・`cover_media_id`のFK）、`POST /media`・`GET /media/{id}`・`GET /media/by-public/{publicId}`・`GET /public/media/{publicId}`、CMS連携（`coverMediaId`・本文の画像参照・版を作る全経路での引き継ぎ・公開直前の再確認・公開APIの`coverUrl`）、運用CLI（`nuctl media-gc|verify|redact`）、compose（`media-data`・`GOMEMLIMIT`・`mem_limit`）、adminの中継の本文上限、`mdcheck`の画像URLの厳密化と抽出。暫定採用はDECISIONS P3-02-1〜4。
- 実測（`scripts/test-integration.sh`・実PostgreSQL・`-race`）: media単体＝形式の判定・PNG/JPEG/WebPの構造検査（アニメーション・寸法偽装・VP8Xとフレームの不一致・RIFF不一致・DNL・SOF重複・SOS過多・切れ）・EXIFの向き1〜8と壊れたEXIF・メタデータと末尾データの除去・縮小のみ・16bit/グレー/パレット・爆弾はデコード前に拒否しメモリを使わない・保存（0640・tmpに残らない・パストラバーサルとシンボリックリンクで外へ出られない）。API＝アップロードの検査・MEDIA_DIR未設定503・1日の上限・頻度・空き容量507・利用者ごと同時1と同時デコード上限・配信の認可と404の同一性・ヘッダー・Range・**公開境界（未参照/下書き/公開中/公開停止/再公開/古い版だけ/共有）**・参照整合性（他人・存在しない・不正な形・コードブロック・カバーの維持/消去・100枚・FK・追記専用）・制作メンバー変更後も画像が出る・redactedの効果・保存失敗で行が残らない。nuctl＝gcのdry-run/apply・verify・redact。**compose smoke 320項目OK**（中継経由の約9MBアップロード・10MiB超は中継経由でもAPIが413・公開境界・APIコンテナ再起動後も画像が残る・コンテナ内media-verify）。
- **メモリ実測（重要）**: 20Mpx 16bit RGBA PNGの単体ピーク約564MiB、2件同時は約973〜1,055MiB（`GOMEMLIMIT`768MiBでも）で`mem_limit`1gを超えるため、**同時デコード数の既定を2→1に変更**（設計の初期値2を実測で修正）。
- ミューテーション: 公開判定から「公開中の版が参照」を外す／参照できる画像の所有者規則を外す／GCが参照を見ない／`acTL`を許す／adminの中継の本文上限設定を外す（smokeで500になり検出）、でテストが失敗、復元済み。最初の「所有者規則を外す」はコンパイルエラーで無効だったため作り直した。
- 作成中の誤り: BOMの紛れ込み、PNGの試験データにCRCが無くデコーダーに拒否された、`Keys()`がシンボリックリンクを通常のファイルとして数えていた、テストのSQLでパラメーターの型推論が衝突、保存先ごと消すと空き容量の確認で507になり書き込み失敗の経路に届かない（tmpをファイルに置き換えて再現）、smokeの「中継の本文上限」の検査が8.6MiBでは設定の有無を区別できない（10MiB超を送る検査に変更）。
- 未実施: PR CI結果、**プログレッシブJPEG・インターレースPNG・CMYK JPEGのメモリ実測**（標準ライブラリで生成できない。契約後に実機の実写真で）、`purpose=avatar/inventory`（P3-02b）、画像のUI（P3-03）、公式サイト側の描画・キャッシュ方針の実装（P3-04）、4GB VPS実機での実測。
- 次の一手: P3-02b（アバター・物品写真）または P3-03（CMS UI）。P3-03はプレビューの`rehype-sanitize`必須。
