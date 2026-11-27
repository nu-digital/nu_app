// Package control はぬるの緊急停止（/nul stop・quiet・resume）。状態はDBに保存し、再起動後も保つ。
package control

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type Mode string

const (
	Running Mode = "running" // 通常
	Quiet   Mode = "quiet"   // timesだけ黙る（自発投稿をしない。呼ばれたら応答する）
	Stopped Mode = "stopped" // 全停止（応答も自発投稿もしない。記録と /nul resume だけ）
)

const settingKey = "control.mode"

// Settings は設定の保存先（store.Store が満たす。試験では偽物）。
type Settings interface {
	GetSetting(ctx context.Context, key string) (json.RawMessage, bool, error)
	SetSetting(ctx context.Context, key string, value any) error
}

type Controller struct {
	st    Settings
	setMu sync.Mutex // Set全体（保存とメモリの更新）を直列にする。2人が同時に操作しても食い違わない
	mu    sync.RWMutex
	mode  Mode
}

// New は保存された状態を読む。読めない・不明な値のときは**安全側（Stopped）**にする。
func New(ctx context.Context, st Settings) (*Controller, error) {
	c := &Controller{st: st, mode: Running}
	raw, ok, err := st.GetSetting(ctx, settingKey)
	if err != nil {
		c.mode = Stopped
		return c, fmt.Errorf("停止状態を読めません。安全のため停止状態にします")
	}
	if ok {
		var m Mode
		if json.Unmarshal(raw, &m) != nil || !valid(m) {
			c.mode = Stopped
			return c, nil
		}
		c.mode = m
	}
	return c, nil
}

func valid(m Mode) bool { return m == Running || m == Quiet || m == Stopped }

func (c *Controller) Mode() Mode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}

// Set は状態を変えて保存する。保存できなければ状態を変えない（変えたつもりで再起動後に戻るのを避ける）。
func (c *Controller) Set(ctx context.Context, m Mode) error {
	if !valid(m) {
		return fmt.Errorf("不明な状態です")
	}
	c.setMu.Lock()
	defer c.setMu.Unlock()
	if err := c.st.SetSetting(ctx, settingKey, m); err != nil {
		return fmt.Errorf("停止状態を保存できません")
	}
	c.mu.Lock()
	c.mode = m
	c.mu.Unlock()
	return nil
}

// MayRespond は、呼ばれたときに応答してよいか（Stopped以外）。
func (c *Controller) MayRespond() bool { return c.Mode() != Stopped }

// MayPostSpontaneously は、自発投稿（times）をしてよいか（Runningのときだけ）。
func (c *Controller) MayPostSpontaneously() bool { return c.Mode() == Running }

// MayControl は、/nul コマンドを使えるか。**Adminロールを持つ人だけ**（Discordの権限ではなく、ここで検査する）。
func MayControl(memberRoleIDs []string, adminRoleID string) bool {
	if adminRoleID == "" {
		return false
	}
	for _, r := range memberRoleIDs {
		if r == adminRoleID {
			return true
		}
	}
	return false
}
