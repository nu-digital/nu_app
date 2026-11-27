package discord

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"github.com/nu-digital/nu_app/services/agent/internal/imageprep"
)

// ---- 送信の許可リスト（仕様§17.1）----
// ここにある送信は「呼ばれた場所への返信」「リアクション」「入力中表示」「timesへの投稿」だけ。
// 削除・BAN・キック・ロール／チャンネル／サーバー設定の変更は無い。誰にも通知（メンション）を飛ばさない。

const maxMessageRunes = 2000

// Reply は、メッセージへの返信を送る。
func (g *Gateway) Reply(ctx context.Context, channelID, replyToID int64, text string) error {
	text = clip(text)
	if strings.TrimSpace(text) == "" {
		return errors.New("empty")
	}
	_, err := g.s.ChannelMessageSendComplex(strconv.FormatInt(channelID, 10), &discordgo.MessageSend{
		Content:         text,
		Reference:       &discordgo.MessageReference{MessageID: strconv.FormatInt(replyToID, 10), ChannelID: strconv.FormatInt(channelID, 10)},
		AllowedMentions: &discordgo.MessageAllowedMentions{}, // 誰にもメンションを飛ばさない
	}, discordgo.WithContext(ctx))
	return err
}

// PostTimes は、設定したtimesチャンネルにだけ投稿する（送信先は引数で変えられない）。誰にもメンションを飛ばさない。
func (g *Gateway) PostTimes(ctx context.Context, text string) error {
	text = clip(text)
	if g.timesID == "" || strings.TrimSpace(text) == "" {
		return errors.New("no target")
	}
	_, err := g.s.ChannelMessageSendComplex(g.timesID, &discordgo.MessageSend{
		Content: text, AllowedMentions: &discordgo.MessageAllowedMentions{},
	}, discordgo.WithContext(ctx))
	return err
}

// Remind は、リマインダーを送る。送り先は、依頼を受けた場所（チャンネル・スレッド・DM）。**メンションは依頼した本人にだけ**（everyone・here・ロール・他の人には飛ばない）。
func (g *Gateway) Remind(ctx context.Context, channelID, userID int64, text string) error {
	uid := strconv.FormatInt(userID, 10)
	body := clip("<@" + uid + "> " + text)
	_, err := g.s.ChannelMessageSendComplex(strconv.FormatInt(channelID, 10), &discordgo.MessageSend{
		Content:         body,
		AllowedMentions: &discordgo.MessageAllowedMentions{Users: []string{uid}},
	}, discordgo.WithContext(ctx))
	return err
}

// React は絵文字リアクションを付ける。
func (g *Gateway) React(ctx context.Context, channelID, messageID int64, emoji string) error {
	return g.s.MessageReactionAdd(strconv.FormatInt(channelID, 10), strconv.FormatInt(messageID, 10), emoji, discordgo.WithContext(ctx))
}

// Typing は入力中を表示する。
func (g *Gateway) Typing(ctx context.Context, channelID int64) error {
	return g.s.ChannelTyping(strconv.FormatInt(channelID, 10), discordgo.WithContext(ctx))
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxMessageRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxMessageRunes-1]) + "…"
}

// ---- 添付画像の取得 ----

// ImageFetcher はDiscordのCDNから画像を取得して縮小する。
// 取得先はDiscordのCDNのホストに限る（SSRF対策）。大きさ・時間に上限。
type ImageFetcher struct {
	Client   *http.Client
	MaxBytes int64
}

var cdnHosts = map[string]bool{"cdn.discordapp.com": true, "media.discordapp.net": true}

func NewImageFetcher() *ImageFetcher {
	return &ImageFetcher{Client: &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !allowedURL(req.URL) || len(via) > 3 {
				return errors.New("redirect not allowed")
			}
			return nil
		},
	}, MaxBytes: 10 << 20}
}

func allowedURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && cdnHosts[strings.ToLower(u.Hostname())]
}

var ErrImageHost = errors.New("image host not allowed")

// Fetch は縮小済みのJPEGを返す。
func (f *ImageFetcher) Fetch(ctx context.Context, a Attachment) ([]byte, error) {
	u, err := url.Parse(a.URL)
	if err != nil || !allowedURL(u) {
		return nil, ErrImageHost
	}
	if int64(a.Size) > f.MaxBytes {
		return nil, imageprep.ErrTooLarge
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		return nil, errors.New("fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("fetch status")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		return nil, errors.New("fetch read")
	}
	if int64(len(data)) > f.MaxBytes {
		return nil, imageprep.ErrTooLarge
	}
	out, _, _, err := imageprep.Prepare(data)
	return out, err
}
