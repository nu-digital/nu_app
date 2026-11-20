//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

type tfResp struct {
	ID, ToUserID, Status string
	ExpiresAt            time.Time
	Roles                []string
}

func (e *env) transferSetup() (owner, member, other actor) {
	e.addUser("owner.example", "active", goodPass, "owner")
	e.addUser("member.example", "active", goodPass)
	e.addUser("other.example", "active", goodPass)
	return e.signIn("owner.example"), e.signIn("member.example"), e.signIn("other.example")
}

func (e *env) request(a actor, toLogin string) (int, tfResp) {
	e.t.Helper()
	rec := e.as(a, "POST", "/owner/transfers", fmt.Sprintf(`{"toUserId":%q}`, e.userID(toLogin).String()))
	var tr tfResp
	_ = json.Unmarshal(rec.Body.Bytes(), &tr)
	return rec.Code, tr
}

func (e *env) ownerCount() int {
	return e.count(`SELECT count(*) FROM user_roles r JOIN users u ON u.id=r.user_id WHERE r.role='owner' AND u.status='active'`)
}

func (e *env) hasRole(login, role string) bool {
	return e.count(`SELECT count(*) FROM user_roles r JOIN users u ON u.id=r.user_id WHERE u.login_id=$1 AND r.role=$2`, login, role) == 1
}

func TestTransferRequestRulesAndExpiry(t *testing.T) {
	e := newEnv(t, opts{})
	owner, member, _ := e.transferSetup()
	e.addUser("admin.example", "active", goodPass, "admin")
	e.addUser("invited.example", "invited", "")
	e.addUser("disabled.example", "disabled", goodPass)
	admin := e.signIn("admin.example")

	// 申請できるのはOwnerだけ
	if c, _ := e.request(member, "other.example"); c != 403 {
		t.Errorf("Memberの申請 = %d", c)
	}
	if c, _ := e.request(admin, "other.example"); c != 403 {
		t.Errorf("Adminの申請 = %d", c)
	}
	if rec := e.do("POST", "/owner/transfers", `{"toUserId":"x"}`); rec.Code != 401 {
		t.Errorf("匿名の申請 = %d", rec.Code)
	}
	// 条件: 自分宛・存在しない・招待中・停止中・不正なID は拒否
	for name, body := range map[string]string{
		"自分":    fmt.Sprintf(`{"toUserId":%q}`, e.userID("owner.example").String()),
		"存在しない": `{"toUserId":"00000000-0000-0000-0000-000000000000"}`,
		"招待中":   fmt.Sprintf(`{"toUserId":%q}`, e.userID("invited.example").String()),
		"停止中":   fmt.Sprintf(`{"toUserId":%q}`, e.userID("disabled.example").String()),
		"不正なID": `{"toUserId":"nope"}`,
		"空":     `{}`,
	} {
		if rec := e.as(owner, "POST", "/owner/transfers", body); rec.Code != 422 {
			t.Errorf("%s宛の申請 = %d（422のはず）", name, rec.Code)
		}
	}
	// 有効期限は申請から24時間。pendingは申請者ごとに1件。
	start := *e.clock
	c, tr := e.request(owner, "member.example")
	if c != 201 || tr.Status != "pending" || !tr.ExpiresAt.Equal(start.Add(24*time.Hour)) && tr.ExpiresAt.Sub(start.Add(24*time.Hour)).Abs() > time.Second {
		t.Fatalf("申請: %d %+v", c, tr)
	}
	if c, _ := e.request(owner, "other.example"); c != 409 {
		t.Errorf("pendingがあるのに2件目が申請できた: %d", c)
	}
	// 取消後は再申請できる
	if rec := e.as(owner, "POST", "/owner/transfers/"+tr.ID+"/cancel", ""); rec.Code != 200 {
		t.Fatalf("取消: %d", rec.Code)
	}
	if c, _ = e.request(owner, "other.example"); c != 201 {
		t.Fatalf("取消後の再申請: %d", c)
	}
	// 失効後は新しい申請ができる（古いpendingがexpiredになる）
	*e.clock = e.clock.Add(24*time.Hour + time.Second)
	if c, _ = e.request(owner, "member.example"); c != 201 {
		t.Fatalf("失効後の再申請: %d", c)
	}
	if n := e.count(`SELECT count(*) FROM owner_transfer_requests WHERE status='expired'`); n != 1 {
		t.Fatalf("失効した申請がexpiredになっていない: %d", n)
	}
	// 既にOwnerの相手には申請できない
	e.addUser("owner2.example", "active", goodPass, "owner")
	if rec := e.as(owner, "POST", "/owner/transfers", fmt.Sprintf(`{"toUserId":%q}`, e.userID("owner2.example").String())); rec.Code != 422 {
		t.Errorf("既にOwnerの相手への申請 = %d", rec.Code)
	}
}

func TestAcceptSwapsOwnerAndKeepsOldOwnerAsAdmin(t *testing.T) {
	e := newEnv(t, opts{})
	owner, member, other := e.transferSetup()
	_, tr := e.request(owner, "member.example")

	// 自分宛の未処理申請だけが見える
	var inc struct {
		Items []struct{ ID, FromLoginID string }
	}
	decode(t, e.as(member, "GET", "/owner/transfers/incoming", ""), &inc)
	if len(inc.Items) != 1 || inc.Items[0].ID != tr.ID || inc.Items[0].FromLoginID != "owner.example" {
		t.Fatalf("incoming: %+v", inc)
	}
	decode(t, e.as(other, "GET", "/owner/transfers/incoming", ""), &inc)
	if len(inc.Items) != 0 {
		t.Fatal("他人宛の申請が見える")
	}
	// 移譲先以外（Owner自身・第三者）は承諾できず、存在も分からない
	for name, a := range map[string]actor{"申請者": owner, "第三者": other} {
		if rec := e.as(a, "POST", "/owner/transfers/"+tr.ID+"/accept", ""); rec.Code != 404 {
			t.Errorf("%sの承諾 = %d（404のはず）", name, rec.Code)
		}
	}
	if e.hasRole("member.example", "owner") {
		t.Fatal("承諾していないのにOwnerになった")
	}
	rec := e.as(member, "POST", "/owner/transfers/"+tr.ID+"/accept", "")
	if rec.Code != 200 {
		t.Fatalf("承諾: %d %s", rec.Code, rec.Body)
	}
	if !e.hasRole("member.example", "owner") || e.hasRole("owner.example", "owner") || !e.hasRole("owner.example", "admin") {
		t.Fatal("Ownerの入れ替わり・旧Ownerのadmin残留が正しくない")
	}
	if n := e.ownerCount(); n != 1 {
		t.Fatalf("activeなOwnerが%d人（1人のはず）", n)
	}
	// 既存セッションに即時反映: 新OwnerはOwner専用操作ができ、旧OwnerはAdminなのでできない
	if rec := e.as(member, "PUT", "/owner/users/"+e.userID("other.example").String()+"/admin-role", `{"enabled":true}`); rec.Code != 200 {
		t.Errorf("新Ownerの操作 = %d", rec.Code)
	}
	if rec := e.as(owner, "PUT", "/owner/users/"+e.userID("other.example").String()+"/admin-role", `{"enabled":false}`); rec.Code != 403 {
		t.Errorf("旧Ownerが Owner専用操作できた: %d", rec.Code)
	}
	if rec := e.as(owner, "POST", "/admin/users", `{"loginId":"x.example","displayName":"x"}`); rec.Code != 201 {
		t.Errorf("旧OwnerはAdminとして管理操作できるはず: %d", rec.Code)
	}
	// 再承諾・承諾後の取消は不可
	if rec := e.as(member, "POST", "/owner/transfers/"+tr.ID+"/accept", ""); rec.Code != 409 {
		t.Errorf("再承諾 = %d", rec.Code)
	}
	// 旧OwnerはAdminになったため、取消はOwner権限不足で403（取消できないことが重要）
	if rec := e.as(owner, "POST", "/owner/transfers/"+tr.ID+"/cancel", ""); rec.Code != 403 {
		t.Errorf("承諾後の取消 = %d（403のはず）", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM audit_logs WHERE action IN ('owner.transfer_requested','owner.transfer_accepted')`); n != 2 {
		t.Fatalf("監査: %d", n)
	}
}

func TestCancelAndExpiredTransfersCannotBeAccepted(t *testing.T) {
	e := newEnv(t, opts{})
	owner, member, other := e.transferSetup()
	_, tr := e.request(owner, "member.example")
	// 申請者以外は取消できない（存在も隠す）
	if rec := e.as(other, "POST", "/owner/transfers/"+tr.ID+"/cancel", ""); rec.Code != 404 {
		t.Errorf("第三者の取消 = %d", rec.Code)
	}
	if rec := e.as(member, "POST", "/owner/transfers/"+tr.ID+"/cancel", ""); rec.Code != 404 {
		t.Errorf("移譲先の取消 = %d（取消は申請者のみ）", rec.Code)
	}
	if rec := e.as(owner, "POST", "/owner/transfers/"+tr.ID+"/cancel", ""); rec.Code != 200 {
		t.Fatalf("取消: %d", rec.Code)
	}
	if rec := e.as(member, "POST", "/owner/transfers/"+tr.ID+"/accept", ""); rec.Code != 409 {
		t.Errorf("取消後の承諾 = %d", rec.Code)
	}
	if rec := e.as(owner, "POST", "/owner/transfers/"+tr.ID+"/cancel", ""); rec.Code != 409 {
		t.Errorf("二重取消 = %d", rec.Code)
	}
	// 失効: 24時間経過後は承諾も取消もできず、expiredとして確定する
	_, tr2 := e.request(owner, "member.example")
	*e.clock = e.clock.Add(24*time.Hour + time.Second)
	if rec := e.as(member, "POST", "/owner/transfers/"+tr2.ID+"/accept", ""); rec.Code != 409 {
		t.Errorf("失効後の承諾 = %d", rec.Code)
	}
	if n := e.count(`SELECT count(*) FROM owner_transfer_requests WHERE id=$1 AND status='expired' AND resolved_at IS NOT NULL`, tr2.ID); n != 1 {
		t.Fatalf("失効が確定していない: %d", n)
	}
	if e.hasRole("member.example", "owner") || !e.hasRole("owner.example", "owner") {
		t.Fatal("失効した申請でOwnerが変わった")
	}
	var inc struct{ Items []struct{ ID string } }
	decode(t, e.as(member, "GET", "/owner/transfers/incoming", ""), &inc)
	if len(inc.Items) != 0 {
		t.Fatal("失効した申請がincomingに見える")
	}
	_, tr3 := e.request(owner, "other.example")
	*e.clock = e.clock.Add(25 * time.Hour)
	if rec := e.as(owner, "POST", "/owner/transfers/"+tr3.ID+"/cancel", ""); rec.Code != 409 {
		t.Errorf("失効後の取消 = %d", rec.Code)
	}
}

func TestAcceptIsRefusedWhenRequesterIsNoLongerAnActiveOwner(t *testing.T) {
	e := newEnv(t, opts{})
	e.addUser("owner1.example", "active", goodPass, "owner")
	e.addUser("owner2.example", "active", goodPass, "owner")
	e.addUser("member.example", "active", goodPass)
	o1, o2, m := e.signIn("owner1.example"), e.signIn("owner2.example"), e.signIn("member.example")
	_, tr := e.request(o1, "member.example")
	// 申請後にowner2がowner1を停止 → 申請者が非activeになる
	if rec := e.as(o2, "PATCH", "/admin/users/"+e.userID("owner1.example").String()+"/status", `{"status":"disabled"}`); rec.Code != 200 {
		t.Fatalf("停止: %d", rec.Code)
	}
	if rec := e.as(m, "POST", "/owner/transfers/"+tr.ID+"/accept", ""); rec.Code != 409 {
		t.Fatalf("申請者が停止された申請の承諾 = %d（409のはず）", rec.Code)
	}
	if e.hasRole("member.example", "owner") {
		t.Fatal("拒否したのにOwnerになった")
	}
	if n := e.ownerCount(); n != 1 {
		t.Fatalf("activeなOwner = %d", n)
	}
	_ = o2
}

// 承諾と取消の同時実行: 結果は必ず一貫する（どちらか一方だけが成功し、Ownerは常に1人）。
func TestConcurrentAcceptAndCancelAreConsistent(t *testing.T) {
	for round := 0; round < 6; round++ {
		e := newEnv(t, opts{})
		owner, member, _ := e.transferSetup()
		_, tr := e.request(owner, "member.example")
		var wg sync.WaitGroup
		start := make(chan struct{})
		var acc, can int
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			acc = e.as(member, "POST", "/owner/transfers/"+tr.ID+"/accept", "").Code
		}()
		go func() {
			defer wg.Done()
			<-start
			can = e.as(owner, "POST", "/owner/transfers/"+tr.ID+"/cancel", "").Code
		}()
		close(start)
		wg.Wait()
		ownerIsMember := e.hasRole("member.example", "owner")
		switch {
		case acc == 200 && (can == 409 || can == 401 || can == 403) && ownerIsMember:
		case can == 200 && acc == 409 && !ownerIsMember:
		default:
			t.Fatalf("round %d: 一貫しない結果 accept=%d cancel=%d memberIsOwner=%v", round, acc, can, ownerIsMember)
		}
		if n := e.ownerCount(); n != 1 {
			t.Fatalf("round %d: activeなOwner=%d", round, n)
		}
	}
}

func TestConcurrentDoubleAcceptSucceedsOnce(t *testing.T) {
	e := newEnv(t, opts{})
	owner, member, _ := e.transferSetup()
	_, tr := e.request(owner, "member.example")
	const workers = 6
	var wg sync.WaitGroup
	start := make(chan struct{})
	codes := make([]int, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = e.as(member, "POST", "/owner/transfers/"+tr.ID+"/accept", "").Code
		}(i)
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		} else if c != 409 {
			t.Fatalf("想定外: %v", codes)
		}
	}
	if ok != 1 {
		t.Fatalf("成功が%d件（1件のはず）: %v", ok, codes)
	}
	if n := e.ownerCount(); n != 1 || !e.hasRole("owner.example", "admin") {
		t.Fatalf("結果が不正: owners=%d", n)
	}
}

func TestTransferEndpointsRequireOriginAndCSRF(t *testing.T) {
	e := newEnv(t, opts{})
	owner, member, _ := e.transferSetup()
	_, tr := e.request(owner, "member.example")
	if rec := e.do("POST", "/owner/transfers/"+tr.ID+"/accept", "", withCookie(member.cookie)); rec.Code != 403 {
		t.Errorf("CSRFなしの承諾 = %d", rec.Code)
	}
	if rec := e.as(member, "POST", "/owner/transfers/"+tr.ID+"/accept", "", withoutHeader("Origin")); rec.Code != 403 {
		t.Errorf("Originなしの承諾 = %d", rec.Code)
	}
	if e.hasRole("member.example", "owner") {
		t.Fatal("拒否された承諾でOwnerになった")
	}
}
