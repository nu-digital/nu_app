package control

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fake struct {
	m      map[string]json.RawMessage
	getErr error
	setErr error
}

func (f *fake) GetSetting(_ context.Context, k string) (json.RawMessage, bool, error) {
	if f.getErr != nil {
		return nil, false, f.getErr
	}
	v, ok := f.m[k]
	return v, ok, nil
}
func (f *fake) SetSetting(_ context.Context, k string, v any) error {
	if f.setErr != nil {
		return f.setErr
	}
	b, _ := json.Marshal(v)
	f.m[k] = b
	return nil
}

func TestModesAndPermissions(t *testing.T) {
	ctx := context.Background()
	f := &fake{m: map[string]json.RawMessage{}}
	c, _ := New(ctx, f)
	if c.Mode() != Running || !c.MayRespond() || !c.MayPostSpontaneously() {
		t.Fatal("初期は通常")
	}
	_ = c.Set(ctx, Quiet)
	if !c.MayRespond() || c.MayPostSpontaneously() {
		t.Error("quiet: 応答はするが自発投稿はしない")
	}
	_ = c.Set(ctx, Stopped)
	if c.MayRespond() || c.MayPostSpontaneously() {
		t.Error("stopped: 何もしない")
	}
	// 再起動後も保たれる
	c2, _ := New(ctx, f)
	if c2.Mode() != Stopped {
		t.Errorf("再起動後: %s", c2.Mode())
	}
	_ = c2.Set(ctx, Running)
	c3, _ := New(ctx, f)
	if c3.Mode() != Running {
		t.Error("resume後")
	}
}

func TestFailsSafe(t *testing.T) {
	ctx := context.Background()
	c, err := New(ctx, &fake{m: map[string]json.RawMessage{}, getErr: errors.New("x")})
	if err == nil || c.Mode() != Stopped {
		t.Errorf("読めないときは停止: %v %s", err, c.Mode())
	}
	c, _ = New(ctx, &fake{m: map[string]json.RawMessage{settingKey: json.RawMessage(`"banana"`)}})
	if c.Mode() != Stopped {
		t.Errorf("不明な値のときは停止: %s", c.Mode())
	}
	// 保存できなければ状態を変えない
	f := &fake{m: map[string]json.RawMessage{}}
	c, _ = New(ctx, f)
	f.setErr = errors.New("x")
	if err := c.Set(ctx, Stopped); err == nil || c.Mode() != Running {
		t.Errorf("保存失敗で状態が変わった: %v %s", err, c.Mode())
	}
	if err := c.Set(ctx, Mode("x")); err == nil {
		t.Error("不明な状態を受け付けた")
	}
}

func TestOnlyAdminRoleMayControl(t *testing.T) {
	if !MayControl([]string{"1", "333"}, "333") {
		t.Error("Adminロールの人は使える")
	}
	if MayControl([]string{"1", "2"}, "333") || MayControl(nil, "333") {
		t.Error("ロールの無い人は使えない")
	}
	if MayControl([]string{"", "333"}, "") || MayControl([]string{""}, "") {
		t.Error("AdminロールIDが未設定なら誰も使えない")
	}
}
