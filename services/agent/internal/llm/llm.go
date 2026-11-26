// Package llm はClaude APIの窓口。brain・budgetは、このインターフェースだけに依存する（試験では偽物。**開発・CIで実際のAPIは呼ばない**）。
package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// モデル名（docs/design/nul-agent.md §4）。
const (
	Sonnet = "claude-sonnet-5-5"
	Opus   = "claude-opus-5-5"
	Haiku  = "claude-haiku-5-5"
)

type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
	// System は会話の途中のシステムメッセージ（気分・日時など、変わるものを渡す。固定部分のキャッシュを壊さない）
	System Role = "system"
)

type BlockType string

const (
	Text       BlockType = "text"
	Image      BlockType = "image"
	ToolResult BlockType = "tool_result"
)

// Block は送る内容の1ブロック。
type Block struct {
	Type BlockType
	Text string // Text / ToolResult の本文
	// Image: メディアタイプ（image/jpeg など）とバイト列（縮小済み）
	MediaType string
	Data      []byte
	// ToolResult
	ToolUseID string
	IsError   bool
}

type Message struct {
	Role   Role
	Blocks []Block
	// native は、Claudeの返答をそのまま次のリクエストへ戻すための実体（thinkingなどを変えずに返す必要がある）。実クライアントだけが使う
	native any
}

// Tool は自前のツール定義。固定（毎回同じバイト列）にしてプロンプトキャッシュに載せる。
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON Schema（properties / required）
}

type Request struct {
	Model     string
	MaxTokens int
	// Effort は low|medium|high|xhigh|max（空ならモデルの既定）
	Effort string
	// System は固定のシステムプロンプト（キャラ設定。**呼び出しごとに同じバイト列**）。気分・日時は含めない（Messages に System ロールで渡す）
	System   string
	Tools    []Tool
	Messages []Message
	// Purpose は予算の記録用（chat|think_hard|reaction|...）。APIには送らない
	Purpose string
}

type ToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

type Usage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
}

type Response struct {
	Text     string
	ToolUses []ToolUse
	// StopReason: end_turn | max_tokens | tool_use | refusal | ...
	StopReason string
	// Refused は、安全のための拒否（refusal）が最終結果のとき。フォールバックでも拒否されたとき
	Refused bool
	// Model は実際に応答したモデル（フォールバックで変わり得る）
	Model string
	Usage Usage
	// Assistant はこの返答をそのまま履歴へ戻すためのメッセージ
	Assistant Message
}

type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

var (
	// ErrCreditExhausted はAPIクレジットが尽きたとき。リトライせず、次の付与日まで寝る（追加課金はしない）。
	ErrCreditExhausted = errors.New("APIクレジットが尽きています")
	// ErrRateLimited は429。短い待ちのあとに1回だけ再試行してよい（SDKが自動でも再試行する）
	ErrRateLimited = errors.New("APIのレート制限です")
)
