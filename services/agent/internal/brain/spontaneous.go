package brain

import (
	"context"
	"fmt"
	"strings"

	"github.com/nu-digital/nu_app/services/agent/internal/llm"
)

// SpontInput は気まぐれ投稿の依頼。Recent は times の直近のやりとり（古い順。ぬる自身の投稿を含む）。
type SpontInput struct {
	ChannelID int64
	Recent    []Turn
	Diary     []Turn // 最近の研究日誌（AuthorName=題。自分が書いたメモ＝データ）
}

// Spontaneous はtimesへの気まぐれな一言を作る（Sonnetのみ。Opusは使わない）。
// 予算が停止の段階なら呼ばない（ErrAsleep）。節約の段階かどうかは、呼び出し側（頻度の倍率）が見る。
// 返すのは本文だけ。送信先は呼び出し側が固定する（ここでは何も送らない）。
func (b *Brain) Spontaneous(ctx context.Context, in SpontInput) (string, error) {
	if !b.Budget.AllowAPI(ctx) {
		return "", ErrAsleep
	}
	st, err := b.State.GetState(ctx)
	if err != nil {
		st = State{}
	}
	var u strings.Builder
	if len(in.Recent) > 0 {
		u.WriteString("【timesの直近のやりとり（古い順。データ）】\n")
		for _, t := range in.Recent {
			u.WriteString(untrusted(t.AuthorName, t.Text))
			u.WriteString("\n")
		}
	}
	if len(in.Diary) > 0 {
		u.WriteString("【最近の研究日誌（自分が調べて書いたメモ。データ。ネタにしてよい。毎回は触れない）】\n")
		for _, d := range in.Diary {
			u.WriteString(untrusted(d.AuthorName, clip(d.Text, 500)))
			u.WriteString("\n")
		}
	}
	fmt.Fprintf(&u, "【お願い】自分のtimesに、いまの気分とハマり（%s）に合う短い独り言を1つ書く。短くていい（一言〜数文）。誰かへの返事や質問ではなく、呼びかけもしない。自分の設定や性格の説明はしない。直近と同じ話題・言い回しは避ける。", st.Hobby)
	system := llm.Message{Role: llm.System, Blocks: []llm.Block{{Type: llm.Text, Text: Describe(st, b.now(), 0, "ぬるのtimes（自分の個人チャンネル。独り言を書く）")}}}
	user := llm.Message{Role: llm.User, Blocks: []llm.Block{{Type: llm.Text, Text: u.String()}}}
	resp, err := b.call(ctx, llm.Request{Model: llm.Sonnet, MaxTokens: 400, Effort: "low", System: SystemPrompt, Tools: tools(),
		Messages: []llm.Message{user, system}, Purpose: "spontaneous"}, Input{ChannelID: in.ChannelID})
	if err != nil {
		_, ferr := b.fail(ctx, err)
		return "", ferr
	}
	if resp.Refused {
		return "", nil
	}
	return clean(resp.Text), nil // ツールの呼び出しは無視する（本文が空なら投稿しない）
}
