package discord

import (
	"context"
	"log/slog"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

// MessageStore は記録先（store.Store が満たす。試験では偽物）。
type MessageStore interface {
	UpsertMessage(ctx context.Context, m store.Message) error
	InsertImported(ctx context.Context, m store.Message) error
	UpdateMessage(ctx context.Context, id int64, content string, att []store.Attachment, editedAt time.Time) error
	DeleteMessages(ctx context.Context, ids ...int64) (int64, error)
}

// Recorder は、読めるチャンネルのメッセージの作成・編集・削除を記録に反映する。
// Botの発言も記録する。ログには本文を出さない。
type Recorder struct {
	st  MessageStore
	log *slog.Logger
}

func NewRecorder(st MessageStore, log *slog.Logger) *Recorder { return &Recorder{st: st, log: log} }

func toStore(m Msg) store.Message {
	kind := "guild"
	if m.GuildID == 0 {
		kind = "dm"
	}
	att := make([]store.Attachment, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		att = append(att, store.Attachment{ID: a.ID, Filename: a.Filename, ContentType: a.ContentType, Size: a.Size})
	}
	return store.Message{ID: m.ID, ChannelID: m.ChannelID, ThreadID: m.ThreadID, Kind: kind, AuthorID: m.AuthorID, AuthorName: m.AuthorName,
		IsBot: m.IsBot, Public: m.Public, Content: m.Content, Attachments: att, CreatedAt: m.CreatedAt, EditedAt: m.EditedAt}
}

func (r *Recorder) OnCreate(ctx context.Context, m Msg) {
	if err := r.st.UpsertMessage(ctx, toStore(m)); err != nil {
		r.log.Error("メッセージを記録できません", "message", m.ID, "channel", m.ChannelID)
	}
}

// OnUpdate は編集を反映する。Discordは本文なしの更新（埋め込みの展開など）も送るので、本文が空で添付も無い更新は無視する。
func (r *Recorder) OnUpdate(ctx context.Context, m Msg) {
	if m.Content == "" && len(m.Attachments) == 0 {
		return
	}
	edited := m.CreatedAt
	if m.EditedAt != nil {
		edited = *m.EditedAt
	}
	sm := toStore(m)
	if err := r.st.UpdateMessage(ctx, m.ID, sm.Content, sm.Attachments, edited); err != nil {
		r.log.Error("編集を反映できません", "message", m.ID)
	}
}

// OnDelete はDiscordで消されたメッセージを、記録からも消す（物理削除）。
func (r *Recorder) OnDelete(ctx context.Context, ids ...int64) {
	if n, err := r.st.DeleteMessages(ctx, ids...); err != nil {
		r.log.Error("削除を反映できません", "count", len(ids))
	} else if n > 0 {
		r.log.Info("削除を記録に反映しました", "count", n)
	}
}
