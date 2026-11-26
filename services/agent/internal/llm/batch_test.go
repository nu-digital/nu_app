package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

func batchServer(t *testing.T, status string, results string) (*Anthropic, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(b))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages/batches"):
			_, _ = w.Write([]byte(`{"id":"msgbatch_1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2026-11-28T00:00:00Z","expires_at":"2026-11-29T00:00:00Z"}`))
		case strings.HasSuffix(r.URL.Path, "/results"):
			w.Header().Set("Content-Type", "application/x-jsonl")
			_, _ = w.Write([]byte(results))
		default:
			_, _ = w.Write([]byte(`{"id":"msgbatch_1","type":"message_batch","processing_status":"` + status + `","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2026-11-28T00:00:00Z","expires_at":"2026-11-29T00:00:00Z","results_url":"http://x/results"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return NewAnthropic("sk-ant-test", option.WithBaseURL(srv.URL), option.WithMaxRetries(0)), &seen
}

func TestBatchSubmitShape(t *testing.T) {
	a, seen := batchServer(t, "in_progress", "")
	id, err := a.Submit(context.Background(), []BatchRequest{{CustomID: "c1_2026-11-20", Model: Haiku, System: "SYS", User: "本文", MaxTokens: 600}})
	if err != nil || id != "msgbatch_1" {
		t.Fatalf("%q %v", id, err)
	}
	b := (*seen)[0]
	for _, w := range []string{`"custom_id":"c1_2026-11-20"`, `"model":"claude-haiku-5-5"`, `"max_tokens":600`, "SYS", "本文"} {
		if !strings.Contains(b, w) {
			t.Errorf("リクエストに %q が無い: %s", w, b)
		}
	}
	if _, err := a.Submit(context.Background(), nil); err == nil {
		t.Error("空のバッチを送れた")
	}
}

func TestBatchPollInProgressAndEnded(t *testing.T) {
	a, _ := batchServer(t, "in_progress", "")
	if done, _, err := a.Poll(context.Background(), "msgbatch_1"); done || err != nil {
		t.Errorf("処理中: done=%v err=%v", done, err)
	}
	line1 := `{"custom_id":"c1_2026-11-20","result":{"type":"succeeded","message":{"id":"m","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"- 要約"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1000,"output_tokens":50}}}}`
	line2 := `{"custom_id":"c2_2026-11-20","result":{"type":"expired"}}`
	b, _ := batchServer(t, "ended", line1+"\n"+line2+"\n")
	done, rs, err := b.Poll(context.Background(), "msgbatch_1")
	if !done || err != nil || len(rs) != 2 {
		t.Fatalf("done=%v err=%v rs=%+v", done, err, rs)
	}
	if !rs[0].OK || rs[0].Text != "- 要約" || rs[0].Usage.InputTokens != 1000 || rs[0].Model != "claude-haiku-5-5" {
		t.Errorf("%+v", rs[0])
	}
	if rs[1].OK {
		t.Error("期限切れは成功にしない")
	}
}
