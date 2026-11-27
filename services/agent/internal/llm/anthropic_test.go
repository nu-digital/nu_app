package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const okBody = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[
 {"type":"thinking","thinking":"","signature":"SIG-ABC"},
 {"type":"text","text":"こんにちは"},
 {"type":"tool_use","id":"tu_1","name":"think_hard","input":{"task":"調べて"}}],
 "stop_reason":"tool_use","stop_sequence":null,
 "usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}`

// サーバーを立て、受け取ったリクエスト（本文・ヘッダー）を返す。
func serve(t *testing.T, status int, body string) (*Anthropic, *[]map[string]any, *http.Header) {
	t.Helper()
	var reqs []map[string]any
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		reqs = append(reqs, m)
		hdr = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewAnthropic("sk-ant-test-SECRET", option.WithBaseURL(srv.URL), option.WithMaxRetries(0)), &reqs, &hdr
}

var tool = Tool{Name: "think_hard", Description: "難しい仕事をOpusに任せる", Schema: json.RawMessage(`{"properties":{"task":{"type":"string"}},"required":["task"]}`)}

func baseReq() Request {
	return Request{Model: Sonnet, MaxTokens: 1000, Effort: "low", System: "あなたは「ぬる」です。", Tools: []Tool{tool}, Purpose: "chat",
		Messages: []Message{
			{Role: User, Blocks: []Block{{Type: Text, Text: "やあ"}, {Type: Image, MediaType: "image/jpeg", Data: []byte("JPEGDATA")}}},
			{Role: System, Blocks: []Block{{Type: Text, Text: "機嫌: +1 / 現在: 2026-11-27 23:00"}}},
		}}
}

func TestRequestShape(t *testing.T) {
	c, reqs, hdr := serve(t, 200, okBody)
	if _, err := c.Complete(context.Background(), baseReq()); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if r["model"] != "claude-sonnet-5-5" || r["max_tokens"].(float64) != 1000 {
		t.Errorf("model/max_tokens: %v %v", r["model"], r["max_tokens"])
	}
	// システムは固定で、cache_controlが付く
	sys := r["system"].([]any)[0].(map[string]any)
	if sys["text"] != "あなたは「ぬる」です。" || sys["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Errorf("system: %v", sys)
	}
	// ツール定義
	tl := r["tools"].([]any)[0].(map[string]any)
	if tl["name"] != "think_hard" || tl["input_schema"].(map[string]any)["required"].([]any)[0] != "task" {
		t.Errorf("tools: %v", tl)
	}
	// **forced tool_choice・thinkingの設定を送らない**（opus-5-5/sonnet-5-5では400になる）
	for _, k := range []string{"tool_choice", "thinking", "temperature", "top_p", "top_k"} {
		if _, ok := r[k]; ok {
			t.Errorf("%s を送っている", k)
		}
	}
	// effort（output_config）
	if r["output_config"].(map[string]any)["effort"] != "low" {
		t.Errorf("effort: %v", r["output_config"])
	}
	// 拒否のフォールバック: サーバー側（default）とベータヘッダー
	if r["fallbacks"] != "default" || !strings.Contains(hdr.Get("Anthropic-Beta"), "server-side-fallback-2026-07-01") {
		t.Errorf("fallbacks: %v / %q", r["fallbacks"], hdr.Get("Anthropic-Beta"))
	}
	// メッセージ: 画像（base64）→ 最後は会話途中のシステムメッセージ（気分・日時）
	msgs := r["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "user" || msgs[1].(map[string]any)["role"] != "system" {
		t.Fatalf("messages: %v", msgs)
	}
	img := msgs[0].(map[string]any)["content"].([]any)[1].(map[string]any)
	if img["type"] != "image" || img["source"].(map[string]any)["media_type"] != "image/jpeg" || img["source"].(map[string]any)["data"] != "SlBFR0RBVEE=" {
		t.Errorf("image: %v", img)
	}
	if !strings.Contains(msgs[1].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string), "機嫌") {
		t.Errorf("system message: %v", msgs[1])
	}
	// APIキーはAuthorizationヘッダーだけ（本文に出ない。SDK v2の送り方）
	if hdr.Get("Authorization") != "Bearer sk-ant-test-SECRET" {
		t.Errorf("APIキーのヘッダー: %v", *hdr)
	}
}

func TestHaikuHasNoFallbacks(t *testing.T) {
	c, reqs, hdr := serve(t, 200, okBody)
	rq := baseReq()
	rq.Model, rq.Tools, rq.Purpose = Haiku, nil, "reaction"
	if _, err := c.Complete(context.Background(), rq); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if _, ok := r["fallbacks"]; ok || strings.Contains(hdr.Get("Anthropic-Beta"), "fallback") {
		t.Errorf("Haikuにフォールバックを付けている: %v %q", r["fallbacks"], hdr.Get("Anthropic-Beta"))
	}
	if _, ok := r["tools"]; ok {
		t.Error("ツールなしのはず")
	}
}

func TestParseAndReplayAssistantUnchanged(t *testing.T) {
	c, reqs, _ := serve(t, 200, okBody)
	resp, err := c.Complete(context.Background(), baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "こんにちは" || resp.StopReason != "tool_use" || resp.Model != "claude-sonnet-5-5" || resp.Refused {
		t.Errorf("%+v", resp)
	}
	if len(resp.ToolUses) != 1 || resp.ToolUses[0].ID != "tu_1" || resp.ToolUses[0].Name != "think_hard" || !strings.Contains(string(resp.ToolUses[0].Input), "調べて") {
		t.Errorf("tool use: %+v", resp.ToolUses)
	}
	if resp.Usage != (Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3, CacheWriteTokens: 2}) {
		t.Errorf("usage: %+v", resp.Usage)
	}
	// 返答をそのまま履歴へ戻す: thinkingブロック（署名つき）が変わらずに送られる。ツール結果はuserメッセージで
	rq := baseReq()
	rq.Messages = append(rq.Messages[:1], resp.Assistant, Message{Role: User, Blocks: []Block{{Type: ToolResult, ToolUseID: "tu_1", Text: "調べた結果"}}})
	if _, err := c.Complete(context.Background(), rq); err != nil {
		t.Fatal(err)
	}
	msgs := (*reqs)[1]["messages"].([]any)
	as := msgs[1].(map[string]any)["content"].([]any)
	if as[0].(map[string]any)["type"] != "thinking" || as[0].(map[string]any)["signature"] != "SIG-ABC" || as[2].(map[string]any)["type"] != "tool_use" {
		t.Errorf("assistantの返答が変わっている: %v", as)
	}
	tr := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["tool_use_id"] != "tu_1" {
		t.Errorf("tool_result: %v", tr)
	}
}

func TestRefusalIsReported(t *testing.T) {
	body := `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":"refusal","stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`
	c, _, _ := serve(t, 200, body)
	resp, err := c.Complete(context.Background(), baseReq())
	if err != nil || !resp.Refused || resp.StopReason != "refusal" {
		t.Errorf("%+v %v", resp, err)
	}
}

func TestErrorsAreClassifiedWithoutLeakingAnything(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, ErrRateLimited},
		{400, `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API."}}`, ErrCreditExhausted},
		{402, `{"type":"error","error":{"type":"billing_error","message":"x"}}`, ErrCreditExhausted},
	} {
		c, _, _ := serve(t, tc.status, tc.body)
		if _, err := c.Complete(context.Background(), baseReq()); !errors.Is(err, tc.want) {
			t.Errorf("%d: %v", tc.status, err)
		}
	}
	c, _, _ := serve(t, 500, `{"type":"error","error":{"type":"api_error","message":"内部の事情 secret-detail"}}`)
	_, err := c.Complete(context.Background(), baseReq())
	if err == nil || errors.Is(err, ErrCreditExhausted) || errors.Is(err, ErrRateLimited) {
		t.Fatalf("%v", err)
	}
	if strings.Contains(err.Error(), "secret-detail") || strings.Contains(err.Error(), "sk-ant") {
		t.Errorf("エラーに詳細・秘密が出ている: %v", err)
	}
}

func TestInvalidRequestsAreRejectedBeforeSending(t *testing.T) {
	c, reqs, _ := serve(t, 200, okBody)
	if _, err := c.Complete(context.Background(), Request{Model: Sonnet}); err == nil {
		t.Error("max_tokens無しを受け付けた")
	}
	rq := baseReq()
	rq.Tools = []Tool{{Name: "x", Schema: json.RawMessage(`{bad`)}}
	if _, err := c.Complete(context.Background(), rq); err == nil {
		t.Error("不正なスキーマを受け付けた")
	}
	rq = baseReq()
	rq.Messages = []Message{{Role: System, Blocks: []Block{{Type: Image}}}}
	if _, err := c.Complete(context.Background(), rq); err == nil {
		t.Error("システムメッセージに画像を受け付けた")
	}
	if len(*reqs) != 0 {
		t.Errorf("不正なリクエストを送信した: %d", len(*reqs))
	}
}

const webBody = `{"id":"msg_w","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[
 {"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"Go 1.27 リリース"}},
 {"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://go.dev/doc/go1.27","title":"Go 1.27","encrypted_content":"x","page_age":null}]},
 {"type":"text","text":"Go 1.27が出ている"}],
 "stop_reason":"pause_turn","stop_sequence":null,
 "usage":{"input_tokens":100,"output_tokens":20,"server_tool_use":{"web_search_requests":2,"web_fetch_requests":0}}}`

func TestWebSearchToolRequestAndParse(t *testing.T) {
	c, reqs, _ := serve(t, 200, webBody)
	req := baseReq()
	req.Tools = nil
	req.WebSearch = &WebSearch{MaxUses: 3, BlockedDomains: []string{"example.com"}}
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	tools := (*reqs)[0]["tools"].([]any)
	ws := tools[len(tools)-1].(map[string]any)
	if ws["type"] != "web_search_20260209" || ws["name"] != "web_search" || ws["max_uses"].(float64) != 3 || ws["blocked_domains"].([]any)[0] != "example.com" {
		t.Errorf("web_searchの定義: %v", ws)
	}
	if resp.Usage.WebSearches != 2 || resp.StopReason != "pause_turn" || resp.Text != "Go 1.27が出ている" {
		t.Errorf("%+v", resp)
	}
	if len(resp.Sources) != 1 || resp.Sources[0] != "https://go.dev/doc/go1.27" {
		t.Errorf("出典: %v", resp.Sources)
	}
}

func TestSearchURLsIgnoresErrorsAndOddSchemes(t *testing.T) {
	if got := searchURLs(`{"type":"web_search_tool_result","content":{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}}`); len(got) != 0 {
		t.Errorf("%v", got)
	}
	if got := searchURLs(`{"content":[{"url":"javascript:alert(1)"},{"url":"https://a.example/x"}]}`); len(got) != 1 {
		t.Errorf("%v", got)
	}
}
