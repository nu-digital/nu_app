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
	br := &brain.Brain{LLM: claude, Budget: tracker, State: brainState{db}, Memory: db, Log: log}
	resp := &responder.Responder{Ctl: ctl, Brain: br, Hist: db, Mem: db, Send: gw, Img: discord.NewImageFetcher(),
		Reacts: db, BotID: gw.BotUserID, TimesChannel: int64(cfg.TimesChannel), Log: log}
	gw.SetTimesChannel(cfg.TimesChannel)
	sch := &schedule.Scheduler{Ctl: ctl, Budget: tracker, Brain: br, Post: gw, Mem: db, Set: db, TimesCh: int64(cfg.TimesChannel), BotID: gw.BotUserID, Log: log}
	gw.OnIncoming = resp.Handle
	if err := gw.Open(); err != nil {
		return err
	}
	defer gw.Close()
	st.SetMode("running")
	st.SetHealthy(true)

	sum := &summarize.Job{Store: db, LLM: claude, Budget: tracker, Log: log}
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
	go sch.Run(ctx) // 毎日の気分・気まぐれ投稿・予算停止のお知らせ

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

// brainState は store.State を brain.State に変える。
type brainState struct{ db *store.Store }

func (b brainState) GetState(ctx context.Context) (brain.State, error) {
	st, err := b.db.GetState(ctx)
	return brain.State{Mood: st.Mood, Energy: st.Energy, Hobby: st.Hobby}, err
}

func (b brainState) GetCloseness(ctx context.Context, userID int64) (int, error) {
	return b.db.GetCloseness(ctx, userID)
}
