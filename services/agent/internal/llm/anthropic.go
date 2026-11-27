package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
	"github.com/anthropics/anthropic-sdk-go/v2/packages/param"
)

// Anthropic は実際のClaude API（anthropic-sdk-go）。
// 守ること（claude-apiスキルの注意）:
//   - システムプロンプトとツール定義は固定し、システムの末尾に cache_control（キャッシュは tools→system→messages の順の接頭辞）
//   - 気分・日時などの変わるものは、会話の途中のシステムメッセージ（role:"system"）で渡す
//   - forced tool_choice（any/tool）は使わない（400になる）。tool_choice自体を送らない（auto）
//   - thinkingは設定しない（opus-5-5・sonnet-5-5は既定でadaptive。無効にできない）。深さはeffortで調整
//   - 拒否（refusal）はサーバー側フォールバック（`fallbacks: "default"`、ベータ server-side-fallback-2026-07-01）で救い、それでも拒否なら Refused
//   - Haikuはフォールバックが無いので付けない
type Anthropic struct {
	c anthropic.Client
}

// NewAnthropic は apiKey（秘密。ログに出さない）でクライアントを作る。opts は試験で base URL を差し替えるために使う。
func NewAnthropic(apiKey string, opts ...option.RequestOption) *Anthropic {
	all := append([]option.RequestOption{option.WithAPIKey(apiKey), option.WithMaxRetries(2), option.WithRequestTimeout(5 * time.Minute)}, opts...)
	return &Anthropic{c: anthropic.NewClient(all...)}
}

func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	p, err := buildParams(req)
	if err != nil {
		return Response{}, err
	}
	msg, err := a.c.Beta.Messages.New(ctx, p)
	if err != nil {
		return Response{}, classify(err)
	}
	return parse(msg), nil
}

func buildParams(req Request) (anthropic.BetaMessageNewParams, error) {
	if req.Model == "" || req.MaxTokens <= 0 {
		return anthropic.BetaMessageNewParams{}, errors.New("modelとmax_tokensが必要です")
	}
	p := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
	}
	if req.System != "" {
		blk := anthropic.BetaTextBlockParam{Text: req.System, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}
		p.System = []anthropic.BetaTextBlockParam{blk}
	}
	for _, t := range req.Tools {
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(t.Schema, &schema); err != nil {
			return p, errors.New("ツールのスキーマが不正です")
		}
		tp := anthropic.BetaToolParam{Name: t.Name, Description: anthropic.String(t.Description),
			InputSchema: anthropic.BetaToolInputSchemaParam{Properties: schema.Properties, Required: schema.Required}}
		p.Tools = append(p.Tools, anthropic.BetaToolUnionParam{OfTool: &tp})
	}
	if ws := req.WebSearch; ws != nil {
		t := anthropic.BetaWebSearchTool20260209Param{BlockedDomains: ws.BlockedDomains}
		if ws.MaxUses > 0 {
			t.MaxUses = anthropic.Int(int64(ws.MaxUses))
		}
		p.Tools = append(p.Tools, anthropic.BetaToolUnionParam{OfWebSearchTool20260209: &t})
	}
	for _, m := range req.Messages {
		bm, err := toBetaMessage(m)
		if err != nil {
			return p, err
		}
		p.Messages = append(p.Messages, bm)
	}
	if req.Effort != "" {
		p.OutputConfig = anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffort(req.Effort)}
	}
	// 拒否のフォールバック（Sonnet・Opus。Haikuには無い）
	if req.Model != Haiku {
		p.Betas = []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01}
		p.Fallbacks = anthropic.BetaFallbacksParamOfDefault()
	}
	return p, nil
}

func toBetaMessage(m Message) (anthropic.BetaMessageParam, error) {
	switch m.Role {
	case System:
		var blocks []anthropic.BetaSystemMessageParamContentUnion
		for _, b := range m.Blocks {
			if b.Type != Text {
				return anthropic.BetaMessageParam{}, errors.New("システムメッセージにはテキストだけを使えます")
			}
			blocks = append(blocks, anthropic.BetaSystemMessageParamContentUnion{OfText: &anthropic.BetaTextBlockParam{Text: b.Text}})
		}
		return anthropic.BetaMessageParam{OfSystem: &anthropic.BetaSystemMessageParam{Content: blocks}}, nil
	case Assistant:
		if raw, ok := m.native.([]json.RawMessage); ok && len(raw) > 0 {
			// Claudeの返答をそのまま戻す（thinkingなどを変えない）
			blocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(raw))
			for _, r := range raw {
				blocks = append(blocks, param.Override[anthropic.BetaContentBlockParamUnion](r))
			}
			return anthropic.BetaMessageParam{OfAssistant: &anthropic.BetaAssistantMessageParam{Content: blocks}}, nil
		}
		var blocks []anthropic.BetaContentBlockParamUnion
		for _, b := range m.Blocks {
			if b.Type != Text {
				return anthropic.BetaMessageParam{}, errors.New("assistantにはテキストだけを使えます")
			}
			blocks = append(blocks, anthropic.NewBetaTextBlock(b.Text))
		}
		return anthropic.BetaMessageParam{OfAssistant: &anthropic.BetaAssistantMessageParam{Content: blocks}}, nil
	case User:
		var blocks []anthropic.BetaContentBlockParamUnion
		for _, b := range m.Blocks {
			switch b.Type {
			case Text:
				blocks = append(blocks, anthropic.NewBetaTextBlock(b.Text))
			case Image:
				blocks = append(blocks, anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
					Data: base64.StdEncoding.EncodeToString(b.Data), MediaType: anthropic.BetaBase64ImageSourceMediaType(b.MediaType)}))
			case ToolResult:
				blocks = append(blocks, anthropic.NewBetaToolResultBlock(b.ToolUseID, b.Text, b.IsError))
			}
		}
		return anthropic.NewBetaUserMessage(blocks...), nil
	}
	return anthropic.BetaMessageParam{}, errors.New("不明なロールです")
}

func parse(msg *anthropic.BetaMessage) Response {
	r := Response{StopReason: string(msg.StopReason), Model: string(msg.Model), Refused: msg.StopReason == anthropic.BetaStopReasonRefusal,
		Usage: Usage{InputTokens: msg.Usage.InputTokens, OutputTokens: msg.Usage.OutputTokens,
			CacheReadTokens: msg.Usage.CacheReadInputTokens, CacheWriteTokens: msg.Usage.CacheCreationInputTokens, WebSearches: msg.Usage.ServerToolUse.WebSearchRequests}}
	var raws []json.RawMessage
	var text strings.Builder
	for _, b := range msg.Content {
		raws = append(raws, json.RawMessage(b.RawJSON()))
		switch b.Type {
		case "text":
			text.WriteString(b.AsText().Text)
		case "web_search_tool_result":
			r.Sources = append(r.Sources, searchURLs(b.RawJSON())...)
		case "tool_use":
			tu := b.AsToolUse()
			r.ToolUses = append(r.ToolUses, ToolUse{ID: tu.ID, Name: tu.Name, Input: json.RawMessage(tu.JSON.Input.Raw())})
		}
	}
	if len(r.Sources) > 8 {
		r.Sources = r.Sources[:8]
	}
	r.Text = text.String()
	r.Assistant = Message{Role: Assistant, native: raws, Blocks: []Block{{Type: Text, Text: r.Text}}}
	return r
}

// classify はSDKのエラーを、ぬるが扱う種類にする。メッセージ本文・秘密は含めない。
func classify(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		msg := strings.ToLower(apiErr.Error())
		switch {
		case apiErr.StatusCode == http.StatusTooManyRequests:
			return ErrRateLimited
		case apiErr.StatusCode == http.StatusPaymentRequired || strings.Contains(msg, "credit balance"):
			return ErrCreditExhausted
		}
		return errors.New("Claude APIがエラーを返しました（HTTP " + http.StatusText(apiErr.StatusCode) + "）")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("Claude APIに接続できません")
}

// searchURLs は web_search_tool_result ブロックから、参照されたページのURL（httpsのみ）を取り出す。
func searchURLs(raw string) []string {
	var b struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal([]byte(raw), &b) != nil {
		return nil
	}
	var items []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(b.Content, &items) != nil { // エラーの結果はオブジェクト
		return nil
	}
	var out []string
	for _, it := range items {
		if strings.HasPrefix(it.URL, "https://") || strings.HasPrefix(it.URL, "http://") {
			out = append(out, it.URL)
		}
	}
	return out
}
