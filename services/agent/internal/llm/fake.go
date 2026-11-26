package llm

import (
	"context"
	"sync"
)

// Fake は偽のClaude（試験用）。呼び出しを記録し、用意した返答を順に返す。
type Fake struct {
	mu        sync.Mutex
	Calls     []Request
	Responses []Response
	Err       error
	// Handler があれば、それで返答を決める（Responsesより優先）
	Handler func(Request) (Response, error)
}

func (f *Fake) Complete(_ context.Context, req Request) (Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, req)
	if f.Handler != nil {
		return f.Handler(req)
	}
	if f.Err != nil {
		return Response{}, f.Err
	}
	if len(f.Responses) == 0 {
		return Response{Text: "（偽の返答）", StopReason: "end_turn", Model: req.Model}, nil
	}
	r := f.Responses[0]
	f.Responses = f.Responses[1:]
	if r.Model == "" {
		r.Model = req.Model
	}
	return r, nil
}
