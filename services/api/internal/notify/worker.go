package notify

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// Worker はoutboxの未送信分を順に取り出してDiscordへ送る。
//
// 再試行の方針（暫定。DECISIONS P2-04-2）:
//   - 一時的な失敗（5xx・タイムアウト）: 30秒から倍々に待つ（上限30分）。MaxAttempts回で止める(dead)。
//   - 429: Discordが指示した時間だけ待つ。失敗として数えない（試行回数を戻す）。
//   - 恒久的な拒否（401/403/404等）: 再試行せず止める(dead)。管理者が原因を直してから再送する。
//   - 本文を作れない行（壊れたpayload）: 止める(dead)。
type Worker struct {
	Pool   *pgxpool.Pool
	Sender Sender
	Log    *slog.Logger
	Now    func() time.Time

	Interval    time.Duration // 取り出しの間隔
	Lease       time.Duration // 確保した行を他のworkerが取り直せるまでの時間（workerが落ちたときの回復）
	MaxAttempts int32
	MaxPerRun   int // 1回の巡回で送る最大件数
}

// NewWorker は既定値のworkerを作る。
func NewWorker(pool *pgxpool.Pool, s Sender, log *slog.Logger) *Worker {
	return &Worker{Pool: pool, Sender: s, Log: log, Now: time.Now, Interval: 5 * time.Second, Lease: 2 * time.Minute, MaxAttempts: 8, MaxPerRun: 20}
}

func backoff(attempt int32) time.Duration {
	d := 30 * time.Second
	for i := int32(1); i < attempt && d < 30*time.Minute; i++ {
		d *= 2
	}
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}

// Run はctxが終わるまで一定間隔で巡回する。
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		if _, err := w.RunOnce(ctx); err != nil && ctx.Err() == nil {
			w.Log.Error("通知workerの巡回に失敗しました", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

// RunOnce は送れるものを最大MaxPerRun件送り、処理した件数を返す。429を受けたら巡回を止める（次の巡回まで待つ）。
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	q := store.New(w.Pool)
	done := 0
	for done < w.MaxPerRun {
		if ctx.Err() != nil {
			return done, nil
		}
		now := w.Now()
		row, err := q.ClaimDueNotification(ctx, store.ClaimDueNotificationParams{LeaseUntil: ts(now.Add(w.Lease)), Now: ts(now)})
		if errors.Is(err, pgx.ErrNoRows) {
			return done, nil
		}
		if err != nil {
			return done, err
		}
		done++
		id := row.ID.String()
		content, ferr := Format(row.EventType, id, row.PayloadJson)
		if ferr != nil {
			if err := w.dead(ctx, q, row.ID, "通知の内容を作れませんでした"); err != nil {
				return done, err
			}
			continue
		}
		serr := w.Sender.Send(ctx, row.ChannelID, content)
		if serr == nil {
			if _, err := q.MarkNotificationSent(ctx, row.ID); err != nil {
				return done, err
			}
			continue
		}
		var se *SendError
		if !errors.As(serr, &se) {
			se = &SendError{Kind: KindTransient, Summary: "送信に失敗しました"}
		}
		if ctx.Err() != nil {
			return done, nil // 停止中。確保したまま（リース期限後に取り直される）
		}
		w.Log.Warn("通知の送信に失敗しました", "notificationId", id, "kind", int(se.Kind), "attempts", row.Attempts, "summary", se.Summary)
		switch se.Kind {
		case KindRateLimited:
			if _, err := q.RescheduleNotification(ctx, store.RescheduleNotificationParams{ID: row.ID, NextAttemptAt: ts(w.Now().Add(se.RetryAfter)), LastError: pgtype.Text{String: se.Summary, Valid: true}, RefundAttempt: true}); err != nil {
				return done, err
			}
			return done, nil // 上限に達している。他の行も送れないので次の巡回へ
		case KindPermanent:
			if err := w.dead(ctx, q, row.ID, se.Summary); err != nil {
				return done, err
			}
		default:
			if row.Attempts >= w.MaxAttempts {
				if err := w.dead(ctx, q, row.ID, se.Summary+"（再試行の上限）"); err != nil {
					return done, err
				}
				continue
			}
			if _, err := q.RescheduleNotification(ctx, store.RescheduleNotificationParams{ID: row.ID, NextAttemptAt: ts(w.Now().Add(backoff(row.Attempts))), LastError: pgtype.Text{String: se.Summary, Valid: true}}); err != nil {
				return done, err
			}
		}
	}
	return done, nil
}

func (w *Worker) dead(ctx context.Context, q *store.Queries, id pgtype.UUID, summary string) error {
	_, err := q.MarkNotificationDead(ctx, store.MarkNotificationDeadParams{ID: id, LastError: pgtype.Text{String: summary, Valid: true}})
	return err
}
