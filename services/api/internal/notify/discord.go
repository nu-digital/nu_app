// Package notify はoutboxの通知をDiscordへ送るworkerと、Discord APIクライアントを持つ。
//
// 方針（docs/ARCHITECTURE.md §G, docs/DECISIONS.md P2-04-1/2）:
//   - Discordへの送信は業務のトランザクションとは別。失敗しても元の業務操作は巻き戻らない。
//   - Discordへのexactly-onceは保証しない（少なくとも1回）。本文に通知IDを入れ、重複を調べられるようにする。
//   - Bot tokenは環境変数からだけ読み、ログ・DB・エラー文言に出さない。Discordの応答本文も保存しない。
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// DefaultAPIBase はDiscord REST APIの基点。
const DefaultAPIBase = "https://discord.com/api/v10"

// Sender は1通のメッセージを送る。workerとAPI（接続テスト）が使う。
type Sender interface {
	Send(ctx context.Context, channelID, content string) error
}

// ErrKind は送信失敗の種類。再試行の方針が変わる。
type ErrKind int

const (
	// KindTransient は一時的な失敗（タイムアウト・5xx・通信エラー）。間隔を空けて再試行する。
	KindTransient ErrKind = iota
	// KindRateLimited は429。Discordが指示した時間だけ待つ（失敗として数えない）。
	KindRateLimited
	// KindPermanent は再試行しても直らない拒否（権限・チャンネルの誤り等）。止めて管理者に知らせる。
	KindPermanent
)

// SendError は送信失敗。Summaryは利用者・管理者に見せてよい短い文言で、tokenや応答本文を含まない。
type SendError struct {
	Kind       ErrKind
	RetryAfter time.Duration
	Summary    string
}

func (e *SendError) Error() string { return e.Summary }

var channelPattern = regexp.MustCompile(`^[0-9]{5,25}$`)

// Client はDiscord REST APIの最小クライアント（メッセージ送信だけ）。
type Client struct {
	base  string
	token string
	hc    *http.Client
}

// NewClient はBot tokenでクライアントを作る。base が空なら本物のDiscord。テストでは偽サーバーを指定する。
func NewClient(token, base string) (*Client, error) {
	if token == "" || containsControl(token) {
		return nil, errors.New("Discord Bot tokenが空、または不正な文字を含みます")
	}
	if base == "" {
		base = DefaultAPIBase
	}
	return &Client{
		base: base, token: token,
		hc: &http.Client{
			Timeout: 10 * time.Second,
			// リダイレクト先へAuthorizationを送らない
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x21 || r == 0x7f {
			return true
		}
	}
	return false
}

// String はtokenを出さない（誤って%vで表示されても漏れない）。
func (c *Client) String() string { return "notify.Client{token:***}" }

// Send はチャンネルへ投稿する。メンションは一切解釈させない（表示名に@everyone等が入っていても通知が飛ばない）。
func (c *Client) Send(ctx context.Context, channelID, content string) error {
	if !channelPattern.MatchString(channelID) {
		return &SendError{Kind: KindPermanent, Summary: "チャンネルIDが正しくありません"}
	}
	body, err := json.Marshal(map[string]any{"content": content, "allowed_mentions": map[string]any{"parse": []string{}}})
	if err != nil {
		return &SendError{Kind: KindPermanent, Summary: "送信内容を作れません"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/channels/"+channelID+"/messages", bytes.NewReader(body))
	if err != nil {
		return &SendError{Kind: KindPermanent, Summary: "送信先の指定が正しくありません"}
	}
	req.Header.Set("Authorization", "Bot "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "nu_app-notifier (https://github.com/nu-digital/nu_app, 1)")
	resp, err := c.hc.Do(req)
	if err != nil {
		// エラー文字列にURLやヘッダーが入る可能性があるため、種類だけを要約する
		if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
			return &SendError{Kind: KindTransient, Summary: "通信エラー（タイムアウト）"}
		}
		if errors.Is(err, context.Canceled) {
			return &SendError{Kind: KindTransient, Summary: "送信を中断しました"}
		}
		return &SendError{Kind: KindTransient, Summary: "通信エラー（Discordに接続できません）"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) // 応答本文は保存しない。429の待ち時間を読むためだけ
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		d := retryAfter(resp.Header.Get("Retry-After"), raw)
		return &SendError{Kind: KindRateLimited, RetryAfter: d, Summary: fmt.Sprintf("Discordの送信上限（429）。%d秒後に再試行", int(d.Seconds()+0.999))}
	case resp.StatusCode >= 500:
		return &SendError{Kind: KindTransient, Summary: fmt.Sprintf("Discordのサーバーエラー（HTTP %d）", resp.StatusCode)}
	default:
		return &SendError{Kind: KindPermanent, Summary: fmt.Sprintf("Discordが拒否しました（HTTP %d）。Botの権限と送信先チャンネルを確認してください", resp.StatusCode)}
	}
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// retryAfter はRetry-Afterヘッダー（秒）か、429の本文の retry_after（秒・小数）を読む。1秒〜1時間に収める。
func retryAfter(header string, body []byte) time.Duration {
	secs := -1.0
	if f, err := strconv.ParseFloat(header, 64); err == nil {
		secs = f
	} else {
		var b struct {
			RetryAfter float64 `json:"retry_after"`
		}
		if json.Unmarshal(body, &b) == nil && b.RetryAfter > 0 {
			secs = b.RetryAfter
		}
	}
	d := time.Duration(secs * float64(time.Second))
	if secs < 0 || d < time.Second {
		d = time.Second
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}
