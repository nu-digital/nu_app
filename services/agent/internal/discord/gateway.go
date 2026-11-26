package discord

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Gateway はdiscordgoとの接続。受信したイベントを Recorder・Commander へ渡し、過去ログを History として読む。
// **ぬるが呼べるDiscordの操作は、ここに書いたものだけ**（読む・スラッシュコマンドへの返信）。送信・リアクション・削除などはP1-4以降に、
// 許可リストの関数としてこのパッケージへ足す（削除・BAN・キック・ロール／チャンネル／設定の変更は実装しない）。
type Gateway struct {
	s         *discordgo.Session
	guildID   string
	rec       *Recorder
	cmd       *Commander
	log       *slog.Logger
	connected atomic.Bool
	lastUp    atomic.Int64 // 最後に接続していた時刻（unix秒）
}

// NewGateway は接続の準備をする（まだ接続しない）。token は秘密（ログに出さない）。
func NewGateway(token string, guildID uint64, rec *Recorder, cmd *Commander, log *slog.Logger) (*Gateway, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, errors.New("Discordのクライアントを作れません")
	}
	s.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages |
		discordgo.IntentsGuildMessageReactions | discordgo.IntentsGuildMembers | discordgo.IntentMessageContent
	s.StateEnabled = true
	g := &Gateway{s: s, guildID: strconv.FormatUint(guildID, 10), rec: rec, cmd: cmd, log: log}
	s.AddHandler(g.onReady)
	s.AddHandler(g.onConnect)
	s.AddHandler(g.onDisconnect)
	s.AddHandler(g.onResumed)
	s.AddHandler(g.onMessageCreate)
	s.AddHandler(g.onMessageUpdate)
	s.AddHandler(g.onMessageDelete)
	s.AddHandler(g.onMessageDeleteBulk)
	s.AddHandler(g.onInteraction)
	return g, nil
}

// Open はDiscordへ接続する。
func (g *Gateway) Open() error {
	if err := g.s.Open(); err != nil {
		return errors.New("Discordに接続できません（tokenとIntentsの設定を確認してください）")
	}
	return nil
}

func (g *Gateway) Close() { _ = g.s.Close() }

// Connected は、いま接続しているか。Down は、切断が続いている時間（接続中は0）。
func (g *Gateway) Connected() bool { return g.connected.Load() }
func (g *Gateway) Down() time.Duration {
	if g.connected.Load() {
		return 0
	}
	return time.Since(time.Unix(g.lastUp.Load(), 0))
}

func (g *Gateway) markUp() { g.connected.Store(true); g.lastUp.Store(time.Now().Unix()) }

func (g *Gateway) onConnect(*discordgo.Session, *discordgo.Connect)       { g.markUp() }
func (g *Gateway) onResumed(*discordgo.Session, *discordgo.Resumed)       { g.markUp() }
func (g *Gateway) onDisconnect(*discordgo.Session, *discordgo.Disconnect) { g.connected.Store(false) }

func (g *Gateway) onReady(s *discordgo.Session, r *discordgo.Ready) {
	g.markUp()
	cmds := []*discordgo.ApplicationCommand{{
		Name:        "nul",
		Description: "ぬるの管理（Adminロールのみ）",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "stop", Description: "全停止（応答も自発投稿もしない）"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "quiet", Description: "timesを含め、自分からは投稿しない"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "resume", Description: "通常に戻す"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: "状態を見る"},
		},
	}}
	if _, err := s.ApplicationCommandBulkOverwrite(r.Application.ID, g.guildID, cmds); err != nil {
		g.log.Error("スラッシュコマンドを登録できません")
		return
	}
	g.log.Info("Discordに接続しました", "guild", g.guildID)
}

// ---- メッセージの記録 ----

func (g *Gateway) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if msg, ok := g.convert(m.Message); ok {
		g.rec.OnCreate(context.Background(), msg)
	}
}

func (g *Gateway) onMessageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	if msg, ok := g.convert(m.Message); ok {
		g.rec.OnUpdate(context.Background(), msg)
	}
}

func (g *Gateway) onMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	if id, err := strconv.ParseInt(m.ID, 10, 64); err == nil {
		g.rec.OnDelete(context.Background(), id)
	}
}

func (g *Gateway) onMessageDeleteBulk(s *discordgo.Session, m *discordgo.MessageDeleteBulk) {
	ids := make([]int64, 0, len(m.Messages))
	for _, x := range m.Messages {
		if id, err := strconv.ParseInt(x, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	g.rec.OnDelete(context.Background(), ids...)
}

// convert はdiscordgoのメッセージを Msg にする。対象サーバー以外（別のサーバー）のメッセージは記録しない（DMは記録する）。
func (g *Gateway) convert(m *discordgo.Message) (Msg, bool) {
	if m == nil || m.Author == nil {
		return Msg{}, false
	}
	if m.GuildID != "" && m.GuildID != g.guildID {
		return Msg{}, false
	}
	parent, thread := g.threadParent(m.ChannelID)
	return ConvertMessage(m, parent, thread), true
}

// threadParent は、ChannelID がスレッドなら (親チャンネルID, スレッドID) を返す。
func (g *Gateway) threadParent(channelID string) (parent, thread string) {
	ch, err := g.s.State.Channel(channelID)
	if err != nil {
		ch, err = g.s.Channel(channelID)
		if err != nil {
			return "", ""
		}
	}
	if ch.IsThread() && ch.ParentID != "" {
		return ch.ParentID, ch.ID
	}
	return "", ""
}

// ConvertMessage はdiscordgoのメッセージを Msg にする（試験できるよう公開）。parent・thread はスレッドのときだけ。
func ConvertMessage(m *discordgo.Message, parent, thread string) Msg {
	atoi := func(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
	out := Msg{ID: atoi(m.ID), ChannelID: atoi(m.ChannelID), GuildID: atoi(m.GuildID), AuthorID: atoi(m.Author.ID),
		AuthorName: displayName(m), IsBot: m.Author.Bot, Content: m.Content, CreatedAt: m.Timestamp}
	if parent != "" {
		out.ChannelID = atoi(parent)
		t := atoi(thread)
		out.ThreadID = &t
	}
	if m.EditedTimestamp != nil {
		e := *m.EditedTimestamp
		out.EditedAt = &e
	}
	for _, a := range m.Attachments {
		out.Attachments = append(out.Attachments, Attachment{ID: a.ID, Filename: a.Filename, ContentType: a.ContentType, Size: a.Size})
	}
	return out
}

func displayName(m *discordgo.Message) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}

// ---- スラッシュコマンド ----

func (g *Gateway) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand || i.ApplicationCommandData().Name != "nul" {
		return
	}
	var roles []string
	if i.Member != nil {
		roles = i.Member.Roles
	}
	sub := ""
	if opts := i.ApplicationCommandData().Options; len(opts) > 0 {
		sub = opts[0].Name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reply := g.cmd.Handle(ctx, sub, roles)
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: reply, Flags: discordgo.MessageFlagsEphemeral}, // 実行した本人にだけ見える
	})
}

// ---- 過去ログの読み取り（History） ----

// HistoryReader は discordgo で History を実装する。
type HistoryReader struct{ g *Gateway }

func (g *Gateway) History() *HistoryReader { return &HistoryReader{g: g} }

func (h *HistoryReader) Channels(ctx context.Context, guildID int64) ([]Channel, error) {
	gid := strconv.FormatInt(guildID, 10)
	chans, err := h.g.s.GuildChannels(gid, discordgo.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	var out []Channel
	var parents []string
	for _, c := range chans {
		switch c.Type {
		case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews:
			out = append(out, Channel{ID: atoi(c.ID), Name: c.Name})
			parents = append(parents, c.ID)
		case discordgo.ChannelTypeGuildForum:
			parents = append(parents, c.ID) // フォーラム自体にはメッセージが無い。投稿（スレッド）を取る
		}
	}
	active, err := h.g.s.GuildThreadsActive(gid, discordgo.WithContext(ctx))
	if err == nil {
		for _, t := range active.Threads {
			out = append(out, Channel{ID: atoi(t.ID), ParentID: atoi(t.ParentID), Name: t.Name, IsThread: true})
		}
	}
	for _, p := range parents { // アーカイブ済みの公開スレッド
		var before *time.Time
		for {
			list, err := h.g.s.ThreadsArchived(p, before, 100, discordgo.WithContext(ctx))
			if err != nil || list == nil || len(list.Threads) == 0 {
				break
			}
			for _, t := range list.Threads {
				out = append(out, Channel{ID: atoi(t.ID), ParentID: atoi(t.ParentID), Name: t.Name, IsThread: true})
				if t.ThreadMetadata != nil {
					ts := t.ThreadMetadata.ArchiveTimestamp
					before = &ts
				}
			}
			if !list.HasMore || before == nil {
				break
			}
		}
	}
	return out, nil
}

func (h *HistoryReader) MessagesBefore(ctx context.Context, ch Channel, beforeID int64, limit int) ([]Msg, error) {
	before := ""
	if beforeID != 0 {
		before = strconv.FormatInt(beforeID, 10)
	}
	ms, err := h.g.s.ChannelMessages(strconv.FormatInt(ch.ID, 10), limit, before, "", "", discordgo.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]Msg, 0, len(ms))
	for _, m := range ms {
		if m == nil || m.Author == nil {
			continue
		}
		m.GuildID = h.g.guildID // 履歴の取得ではGuildIDが入らないことがある
		parent, thread := "", ""
		if ch.IsThread {
			parent, thread = strconv.FormatInt(ch.ParentID, 10), strconv.FormatInt(ch.ID, 10)
		}
		out = append(out, ConvertMessage(m, parent, thread))
	}
	return out, nil
}

func atoi(s string) int64 { n, _ := strconv.ParseInt(s, 10, 64); return n }
