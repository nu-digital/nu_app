# 155 進捗: ぬる P3-3（新メンバーへの反応・APIキー期限の催促）

## 状態（事実）
- 実装: `Gateway.OnJoin`（GuildMemberAdd）、`Scheduler.Greet`（固定文言・名前の無害化・1時間5件）、`nagKey`（apikey.expires の14日前から毎日夜）。
- 実測: `go vet`・`go test ./...`（services/agent）ok。実Discordは未使用。

## 未実施
- 実機確認（Guild Members Intentの有効化の確認・実際の参加イベント）。

## 次の一手
- P3の残りはGitHub読み取り（GitHub App待ち）。他に進められるもの: 内省（毎晩のノート・関係更新）、リマインダー（P5）。
