package llm

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// Batch は Message Batches API（半額・24時間以内に完了）の窓口。裏方の要約に使う（会話には使わない）。
type Batch interface {
	// Submit はリクエストをまとめて送り、バッチIDを返す。CustomID は呼び出し側で一意（結果との対応づけ）。
	Submit(ctx context.Context, reqs []BatchRequest) (string, error)
	// Poll は結果を調べる。まだ処理中なら done=false。
	Poll(ctx context.Context, batchID string) (done bool, results []BatchResult, err error)
}

type BatchRequest struct {
	CustomID  string
	Model     string
	System    string
	User      string
	MaxTokens int
}

type BatchResult struct {
	CustomID string
	OK       bool // 成功（失敗・取り消し・期限切れはfalse）
	Text     string
	Model    string
	Usage    Usage
}

// SubmitBatch / PollBatch は実際のAPI。
func (a *Anthropic) Submit(ctx context.Context, reqs []BatchRequest) (string, error) {
	if len(reqs) == 0 {
		return "", errors.New("リクエストが空です")
	}
	var rs []anthropic.MessageBatchNewParamsRequest
	for _, r := range reqs {
		p := anthropic.MessageBatchNewParamsRequestParams{
			Model: anthropic.Model(r.Model), MaxTokens: int64(r.MaxTokens),
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(r.User))},
		}
		if r.System != "" {
			p.System = []anthropic.TextBlockParam{{Text: r.System}}
		}
		rs = append(rs, anthropic.MessageBatchNewParamsRequest{CustomID: r.CustomID, Params: p})
	}
	b, err := a.c.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{Requests: rs})
	if err != nil {
		return "", classify(err)
	}
	return b.ID, nil
}

func (a *Anthropic) Poll(ctx context.Context, batchID string) (bool, []BatchResult, error) {
	b, err := a.c.Messages.Batches.Get(ctx, batchID, anthropic.MessageBatchGetParams{})
	if err != nil {
		return false, nil, classify(err)
	}
	if b.ProcessingStatus != anthropic.MessageBatchProcessingStatusEnded {
		return false, nil, nil
	}
	stream := a.c.Messages.Batches.ResultsStreaming(ctx, batchID, anthropic.MessageBatchResultsParams{})
	defer stream.Close()
	var out []BatchResult
	for stream.Next() {
		r := stream.Current()
		res := BatchResult{CustomID: r.CustomID}
		if s := r.Result.AsSucceeded(); s.Type == "succeeded" {
			var sb strings.Builder
			for _, c := range s.Message.Content {
				if c.Type == "text" {
					sb.WriteString(c.Text)
				}
			}
			res.OK, res.Text, res.Model = true, sb.String(), string(s.Message.Model)
			res.Usage = Usage{InputTokens: s.Message.Usage.InputTokens, OutputTokens: s.Message.Usage.OutputTokens,
				CacheReadTokens: s.Message.Usage.CacheReadInputTokens, CacheWriteTokens: s.Message.Usage.CacheCreationInputTokens}
		}
		out = append(out, res)
	}
	if err := stream.Err(); err != nil {
		return false, nil, classify(err)
	}
	return true, out, nil
}

// FakeBatch は試験用。Submitで受けたものを覚え、Pollで（Handlerの返す内容を）結果として返す。
type FakeBatch struct {
	mu      sync.Mutex
	Batches map[string][]BatchRequest
	Ready   bool                      // trueならPollは完了を返す
	Handler func(BatchRequest) string // 返す要約（nilなら固定文）
	Fail    map[string]bool           // CustomID → 失敗にする
	Err     error
	next    int
}

func (f *FakeBatch) Submit(_ context.Context, reqs []BatchRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	if f.Batches == nil {
		f.Batches = map[string][]BatchRequest{}
	}
	f.next++
	id := "batch_fake_" + string(rune('a'+f.next))
	f.Batches[id] = reqs
	return id, nil
}

func (f *FakeBatch) Poll(_ context.Context, id string) (bool, []BatchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.Ready {
		return false, nil, nil
	}
	var out []BatchResult
	for _, r := range f.Batches[id] {
		res := BatchResult{CustomID: r.CustomID, Model: r.Model, OK: !f.Fail[r.CustomID], Usage: Usage{InputTokens: 1000, OutputTokens: 100}}
		if res.OK {
			res.Text = "（偽の要約）"
			if f.Handler != nil {
				res.Text = f.Handler(r)
			}
		}
		out = append(out, res)
	}
	return true, out, nil
}
