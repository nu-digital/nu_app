package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/nu-digital/nu_app/services/agent/internal/control"
)

// Commander は /nul コマンド（stop・quiet・resume・status）の中身。Adminロールの人だけ。Discordの権限ではなく、ここで検査する。
type Commander struct {
	Ctl       *control.Controller
	AdminRole string
	// Status は /nul status の本文（状態・記録・取り込みの進み。予算はP1-3で追加）
	Status func(ctx context.Context) (string, error)
}

// Handle は sub（stop|quiet|resume|status）を実行し、本人にだけ見える返信の本文を返す。
func (c *Commander) Handle(ctx context.Context, sub string, memberRoles []string) string {
	if !control.MayControl(memberRoles, c.AdminRole) {
		return "このコマンドは、Adminロールの人だけが使えます。"
	}
	switch sub {
	case "stop":
		return c.set(ctx, control.Stopped, "全停止しました。呼ばれても応答せず、自分からも投稿しません（記録は続けます）。再開は /nul resume。")
	case "quiet":
		return c.set(ctx, control.Quiet, "timesを含めて、自分からの投稿をやめました。呼ばれたら応答します。元に戻すのは /nul resume。")
	case "resume":
		return c.set(ctx, control.Running, "通常の動きに戻しました。")
	case "status":
		if c.Status == nil {
			return fmt.Sprintf("状態: %s", modeLabel(c.Ctl.Mode()))
		}
		s, err := c.Status(ctx)
		if err != nil {
			return "状態を取得できませんでした。"
		}
		return s
	}
	return "不明なコマンドです。"
}

func (c *Commander) set(ctx context.Context, m control.Mode, ok string) string {
	if err := c.Ctl.Set(ctx, m); err != nil {
		// 保存できなかったときは、状態は変わっていない（変わったと誤解させない）
		return "状態を保存できなかったため、変更していません。もう一度試してください。"
	}
	return ok
}

func modeLabel(m control.Mode) string {
	switch m {
	case control.Running:
		return "通常"
	case control.Quiet:
		return "静か（自発投稿なし）"
	case control.Stopped:
		return "全停止"
	}
	return string(m)
}

// FormatStatus は /nul status の標準の本文。
func FormatStatus(m control.Mode, messages int64, importChannels, importDone int, imported int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "状態: %s\n", modeLabel(m))
	fmt.Fprintf(&b, "記録しているメッセージ: %d 件\n", messages)
	fmt.Fprintf(&b, "過去ログの取り込み: %d/%d チャンネル完了（取り込み済み %d 件）", importDone, importChannels, imported)
	return b.String()
}
