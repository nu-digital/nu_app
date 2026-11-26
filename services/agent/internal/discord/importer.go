package discord

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/store"
)

// ImportStore は取り込みの記録先（store.Store が満たす）。
type ImportStore interface {
	InsertImported(ctx context.Context, m store.Message) error
	GetProgress(ctx context.Context, channelID int64) (store.Progress, bool, error)
	SaveProgress(ctx context.Context, p store.Progress) error
}

// Importer は過去ログを、新しい方から古い方へ、ゆっくり取り込む。再開できる（チャンネルごとに進捗を保存する）。
// 本文を保存するだけで、Claude APIは使わない（要約はP2）。ログには本文を出さない。
type Importer struct {
	Hist      History
	St        ImportStore
	Log       *slog.Logger
	PageSize  int           // 1回に取るメッセージ数（Discordの上限100）
	PageDelay time.Duration // ページ間の待ち（API制限への配慮）
	// Allow が false を返したら中断する（停止中など）。nilなら常に続ける
	Allow func() bool
}

// Run はサーバーの全チャンネル・スレッドを順に取り込む。ctx終了・Allow=falseで中断（次回は続きから）。
func (im *Importer) Run(ctx context.Context, guildID int64) error {
	if im.PageSize <= 0 || im.PageSize > 100 {
		im.PageSize = 100
	}
	chans, err := im.Hist.Channels(ctx, guildID)
	if err != nil {
		return errors.New("チャンネルの一覧を取れません")
	}
	for _, ch := range chans {
		if err := im.importChannel(ctx, ch); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// 1つのチャンネルの失敗（権限・一時的なエラー）で全体を止めない。進捗は保存済みなので次回に続きから
			im.Log.Warn("チャンネルの取り込みに失敗しました（次回に続きから）", "channel", ch.ID)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

func (im *Importer) importChannel(ctx context.Context, ch Channel) error {
	prog, ok, err := im.St.GetProgress(ctx, ch.ID)
	if err != nil {
		return err
	}
	if ok && prog.Done {
		return nil
	}
	prog.ChannelID = ch.ID
	var before int64
	if prog.OldestMessageID != nil {
		before = *prog.OldestMessageID
	}
	for {
		if im.Allow != nil && !im.Allow() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		page, err := im.Hist.MessagesBefore(ctx, ch, before, im.PageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			prog.Done = true
			return im.St.SaveProgress(ctx, prog)
		}
		oldest := page[0].ID
		for _, m := range page {
			if m.ID < oldest {
				oldest = m.ID
			}
			if err := im.St.InsertImported(ctx, toStore(m)); err != nil {
				return err
			}
		}
		prog.Imported += int64(len(page))
		prog.OldestMessageID = &oldest
		before = oldest
		if len(page) < im.PageSize {
			prog.Done = true
		}
		if err := im.St.SaveProgress(ctx, prog); err != nil {
			return err
		}
		if prog.Done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(im.PageDelay):
		}
	}
}
