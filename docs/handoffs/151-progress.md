# 151 進捗: ぬる P3-1（Web検索）

## 状態（事実）
- 実装: `web_research`ツール（別呼び出し・web_searchのみ）、`llm.WebSearch`/`Usage.WebSearches`/`Response.Sources`、検索1回$0.01の加算、`AllowWeb`（90%未満）、一人1日5回（migration 00016）、pause_turn続行（上限3）。
- 実測: `go vet`・`go test ./...`（services/agent）ok、`scripts/test-integration.sh` ok、sqlc再生成。実Claude APIは未使用（SDKの要求形式・解析はhttptestで確認）。

## 未実施
- 実機確認（Console側のWeb検索有効化の確認・実際の検索結果の質・料金の実績）。
- web_fetch（URL指定の取得）は未採用。GitHub読み取り・研究日誌・気まぐれ投稿のネタはP3の次のPR。

## 次の一手
- P3-2: 気まぐれ投稿のネタ・研究日誌（Web検索を使うので、予算とモデルを慎重に）。GitHub読み取りはGitHub App（運用責任者の準備）が必要。
