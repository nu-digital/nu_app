// Command agent はAIメンバー「ぬる」（Discord）。docs/design/nul-agent.md。
// 必要な設定（Discordのtoken・Claude APIキー・DBのパスワード・ID類）が揃っていないときは「無効モード」で起動し、
// Discordにも接続せずClaude APIも呼ばない（healthzだけ応答する）。secretを置く前にデプロイしても、スタックは壊れない。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/nu-digital/nu_app/services/agent/internal/brain"
	"github.com/nu-digital/nu_app/services/agent/internal/budget"
	"github.com/nu-digital/nu_app/services/agent/internal/config"
	"github.com/nu-digital/nu_app/services/agent/internal/control"
	"github.com/nu-digital/nu_app/services/agent/internal/discord"
	"github.com/nu-digital/nu_app/services/agent/internal/health"
	"github.com/nu-digital/nu_app/services/agent/internal/introspect"
	"github.com/nu-digital/nu_app/services/agent/internal/llm"
	"github.com/nu-digital/nu_app/services/agent/internal/responder"
	"github.com/nu-digital/nu_app/services/agent/internal/schedule"
	"github.com/nu-digital/nu_app/services/agent/internal/store"
	"github.com/nu-digital/nu_app/services/agent/internal/summarize"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "コンテナのヘルスチェック（/healthzを確認して終了）")
	flag.Parse()
	cfg, err := config.Load(os.Getenv)
	if *healthcheck {
		if err := health.Check(cfg.HealthAddr); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy")
			os.Exit(1)
		}
		return
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err != nil {
		log.Error("設定が不正です", "error", err.Error())
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, log, cfg); err != nil {
		log.Error("ぬるが異常終了しました", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, cfg config.Config) error {
	st := health.New()
	if m := cfg.Missing(); len(m) > 0 {
		// 値は出さない。足りない設定の名前だけ
		log.Info("無効モードで起動します（Discordに接続せず、Claude APIも呼びません）", "missing", m)
		st.SetMode("disabled")
		return st.Serve(ctx, cfg.HealthAddr)
	}
	log.Info("設定が揃っています。有効モードで起動します", "config", cfg.String())
	st.SetMode("starting")
	go func() {
		if err := runEnabled(ctx, log, cfg, st); err != nil && ctx.Err() == nil {
			log.Error("ぬるが停止しました", "error", err.Error())
			st.SetHealthy(false)
		}
	}()
	return st.Serve(ctx, cfg.HealthAddr)
}

// runEnabled はDBに接続し、Discordに接続して、記録・過去ログの取り込み・/nulコマンドを動かす（P1-2。応答・発言はP1-3以降）。
func runEnabled(ctx context.Context, log *slog.Logger, cfg config.Config, st *health.Status) error {
	// DBに接続できない（nu_agentのパスワード未設定など）間は、再試行する（落ちて再起動を繰り返さない）
	var db *store.Store
	for {
		var err error
		if db, err = store.Open(ctx, cfg.DatabaseURL()); err == nil {
			break
		}
		log.Error("DBに接続できません。30秒後に再試行します", "error", err.Error())
		st.SetHealthy(false)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
	defer db.Close()
	// DMは廃止した（第3フェーズ P0）。以前のDM由来のデータを削除する（冪等。件数だけを記録し、内容はログに出さない）
	if pr, err := db.PurgeDMData(ctx); err != nil {
		log.Error("DM由来のデータを削除できませんでした", "error", err.Error())
	} else if pr != (store.PurgeResult{}) {
		log.Info("DM由来のデータを削除しました", "messages", pr.Messages, "notes", pr.Notes, "reminders", pr.Reminders, "summaries", pr.Summaries, "import_progress", pr.Progress)
		_ = db.SetSetting(ctx, "dm_purge.last", map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "messages": pr.Messages, "notes": pr.Notes, "reminders": pr.Reminders, "summaries": pr.Summaries, "import_progress": pr.Progress})
	}
	ctl, err := control.New(ctx, db)
	if err != nil {
		log.Error("停止状態を読めないため、停止状態で始めます")
	}
	rec := discord.NewRecorder(db, log)
	cmd := &discord.Commander{Ctl: ctl, AdminRole: strconv.FormatUint(cfg.AdminRole, 10)}
	cmd.Status = func(ctx context.Context) (string, error) {
		n, err := db.CountMessages(ctx, nil)
		if err != nil {
			return "", err
		}
		ch, done, imp, err := db.ImportSummary(ctx)
		if err != nil {
			return "", err
		}
		return discord.FormatStatus(ctl.Mode(), n, ch, done, imp), nil
	}
	gw, err := discord.NewGateway(cfg.DiscordToken, cfg.GuildID, rec, cmd, log)
	if err != nil {
		return err
	}
	// 応答（P1-4a）: メンション・返信・DM・times。ぬるの頭脳は実Claude APIに繋ぐ（上限はbudgetが守る）
	tracker := budget.New(db, budget.Config{}, nil)
	claude := llm.NewAnthropic(cfg.AnthropicKey)
	br := &brain.Brain{LLM: claude, Budget: tracker, State: brainState{db}, Memory: db, Reminders: db, Audit: db, Log: log}
	resp := &responder.Responder{Ctl: ctl, Brain: br, Hist: db, Mem: db, Send: gw, Img: discord.NewImageFetcher(),
		Reacts: db, BotID: gw.BotUserID, TimesChannel: int64(cfg.TimesChannel), Log: log}
	gw.SetTimesChannel(cfg.TimesChannel)
	sch := &schedule.Scheduler{Ctl: ctl, Budget: tracker, Brain: br, Post: gw, Notify: gw, Rem: db, Mem: db, Set: db, TimesCh: int64(cfg.TimesChannel), BotID: gw.BotUserID, Log: log}
	gw.OnIncoming = resp.Handle
	gw.OnJoin = sch.Greet
	if err := gw.Open(); err != nil {
		return err
	}
	defer gw.Close()
	st.SetMode("running")
	st.SetHealthy(true)

	sum := &summarize.Job{Store: db, LLM: claude, Budget: tracker, Allow: ctl.MayRespond, Log: log}
	go func() { // 日次要約（Batch API）。15分ごとに、進行中のバッチの確認か新しいバッチの送信
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			sum.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	intro := &introspect.Job{Store: db, LLM: claude, Budget: tracker, Allow: ctl.MayRespond, Log: log}
	go func() { // 内省（毎晩。Batch API）
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			intro.Tick(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	go sch.Run(ctx) // 毎日の気分・気まぐれ投稿・予算停止のお知らせ

	// 異常検知: 10分間にツールの拒否・失敗が30件以上なら、自動で全停止にして管理者に知らせる（暴走・攻撃への緊急停止。再開は /nul resume）
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !ctl.MayRespond() {
					continue
				}
				if n, err := db.ToolDenials(ctx, time.Now().Add(-10*time.Minute)); err == nil && n >= toolDenialStopThreshold {
					log.Error("ツールの拒否・失敗が多すぎるため、全停止にします", "count", n)
					if err := ctl.Set(ctx, control.Stopped); err == nil {
						_ = gw.PostTimes(ctx, "ツールの失敗が短時間に大量に出たので、安全のため自分で止まる。管理者が /nul resume で再開して。")
					}
				}
			}
		}
	}()
	// 切断が3分以上続いたら異常にする（コンテナのヘルスチェックに出る）
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st.SetHealthy(gw.Down() < 3*time.Minute)
			}
		}
	}()
	// 過去ログの取り込み。ゆっくり（1ページごとに1秒待つ）。完了済みは飛ばすので、6時間ごとに新しいスレッドを拾う
	im := &discord.Importer{Hist: gw.History(), St: db, Log: log, PageSize: 100, PageDelay: time.Second}
	for {
		if err := db.Maintain(ctx, time.Now()); err != nil {
			log.Warn("定期の掃除に失敗しました")
		}
		if err := im.Run(ctx, int64(cfg.GuildID)); err != nil && ctx.Err() == nil {
			log.Warn("過去ログの取り込みを中断しました（次回に続きから）")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(6 * time.Hour):
		}
	}
}

// toolDenialStopThreshold は、10分間のツール拒否・失敗がこの件数以上なら自動で全停止にする閾値。
const toolDenialStopThreshold = 30

// brainState は store.State を brain.State に変える。
type brainState struct{ db *store.Store }

func (b brainState) GetState(ctx context.Context) (brain.State, error) {
	st, err := b.db.GetState(ctx)
	return brain.State{Mood: st.Mood, Energy: st.Energy, Hobby: st.Hobby}, err
}

func (b brainState) GetCloseness(ctx context.Context, userID int64) (int, error) {
	return b.db.GetCloseness(ctx, userID)
}
