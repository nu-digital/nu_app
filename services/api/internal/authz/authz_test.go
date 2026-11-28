package authz

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// 主体の種類。期待表（下のwant）は、API.mdの認可列と docs/AUTHZ.md を見て手で書いたもので、実装のコピーではない。
var (
	anon     = Anonymous
	invited  = Subject{Authenticated: true, Status: StatusInvited}
	disabled = Subject{Authenticated: true, Status: StatusDisabled, Roles: []Role{RoleOwner, RoleAdmin, RoleEditor}} // 停止中は権限があっても無効
	member   = Subject{Authenticated: true, Status: StatusActive}
	editor   = Subject{Authenticated: true, Status: StatusActive, Roles: []Role{RoleEditor}}
	admin    = Subject{Authenticated: true, Status: StatusActive, Roles: []Role{RoleAdmin}}
	owner    = Subject{Authenticated: true, Status: StatusActive, Roles: []Role{RoleOwner}}
)

var subjects = map[string]Subject{"anon": anon, "invited": invited, "disabled": disabled, "member": member, "editor": editor, "admin": admin, "owner": owner}

func repoFile(rel string) string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "..", "..", rel)
}

// 関係を持たない一般の資源に対する期待（relation なし）。キー=アクションID、値=許可される主体名。
var wantNoRelation = map[string][]string{
	"auth.login":                       {"anon", "invited", "disabled", "member", "editor", "admin", "owner"},
	"auth.accept_invite":               {"anon", "invited", "disabled", "member", "editor", "admin", "owner"},
	"auth.reset_password":              {"anon", "invited", "disabled", "member", "editor", "admin", "owner"},
	"public.contents.list":             {"anon", "invited", "disabled", "member", "editor", "admin", "owner"},
	"public.contents.get":              {"anon", "invited", "disabled", "member", "editor", "admin", "owner"},
	"public.media.get":                 {"anon", "invited", "disabled", "member", "editor", "admin", "owner"},
	"auth.logout":                      memberUp,
	"auth.me":                          memberUp,
	"owner.transfers.incoming":         memberUp,
	"members.list":                     memberUp,
	"members.get":                      memberUp,
	"me.profile.get":                   memberUp,
	"me.profile.update":                memberUp,
	"me.profile.visibility":            memberUp,
	"me.profile.public_credit_consent": memberUp,
	"me.profile.links.create":          memberUp,
	"me.profile.links.update":          memberUp,
	"me.profile.links.delete":          memberUp,
	"me.devices.create":                memberUp,
	"me.devices.update":                memberUp,
	"me.devices.delete":                memberUp,
	"inventory.items.list":             memberUp,
	"inventory.items.get":              memberUp,
	"inventory.loans.create":           memberUp,
	"inventory.loans.return":           memberUp,
	"inventory.loans.list":             memberUp,
	"inventory.history":                memberUp,
	"contents.list":                    memberUp,
	"contents.create":                  memberUp,
	"media.upload":                     memberUp,
	"dashboard.feed":                   memberUp,

	"admin.users.list":                    adminUp,
	"admin.users.create":                  adminUp,
	"admin.users.editor_role":             adminUp,
	"admin.users.private_profile.get":     adminUp,
	"admin.users.private_profile.delete":  adminUp,
	"admin.legacy_authors.list":           adminUp,
	"admin.legacy_authors.permit":         adminUp,
	"admin.legacy_authors.withdraw":       adminUp,
	"admin.legacy_authors.link":           adminUp,
	"admin.legacy_authors.unlink":         adminUp,
	"admin.contents.published_search":     adminUp,
	"me.profile.avatar.set":               memberUp,
	"me.profile.avatar.clear":             memberUp,
	"inventory.items.photo.set":           adminUp,
	"inventory.items.photo.clear":         adminUp,
	"inventory.items.images.set":          adminUp,
	"inventory.categories.list":           memberUp,
	"inventory.categories.create":         adminUp,
	"inventory.categories.reorder":        adminUp,
	"inventory.categories.update":         adminUp,
	"inventory.categories.delete":         adminUp,
	"inventory.items.create":              adminUp,
	"inventory.items.update":              adminUp,
	"inventory.units.create":              adminUp,
	"inventory.corrections.create":        adminUp,
	"notifications.settings.list":         adminUp,
	"notifications.settings.update":       adminUp,
	"notifications.deliveries.list":       adminUp,
	"notifications.deliveries.retry":      adminUp,
	"notifications.test":                  adminUp,
	"admin.notifications.settings.get":    adminUp,
	"admin.notifications.settings.update": adminUp,
	"admin.notifications.test":            adminUp,
	"admin.notifications.deliveries":      adminUp,
	// 対象が一般メンバーのときのAdminの停止・招待URL・再設定URL
	"admin.users.invite_link": adminUp,
	"admin.users.reset_link":  adminUp,
	"admin.users.status":      adminUp,

	"owner.users.admin_role": {"owner"},
	"owner.transfers.create": {"owner"},
	"owner.transfers.cancel": nil, // 申請者本人でなければ誰も不可（下の関係テストで確認）
	"owner.transfers.accept": nil, // 移譲先本人でなければ誰も不可

	// 作者でも共同編集者でもない他人: Editor/Admin/Ownerのみ。
	"contents.get":                  editorUp,
	"contents.revisions.create":     editorUp,
	"contents.collaborators.update": editorUp,
	"contents.contributors.update":  editorUp,
	"contents.publish":              editorUp,
	"contents.unpublish":            editorUp,
	"contents.delete":               editorUp,
	// 他人が上げた画像
	"media.get":           editorUp,
	"media.get_by_public": editorUp,
}

var (
	memberUp = []string{"member", "editor", "admin", "owner"}
	editorUp = []string{"editor", "admin", "owner"}
	adminUp  = []string{"admin", "owner"}
)

func TestMatrixWithoutRelation(t *testing.T) {
	if len(wantNoRelation) != len(Actions()) {
		t.Fatalf("期待表(%d件)と認可表(%d件)の件数が違う。新しいアクションには期待を追加すること", len(wantNoRelation), len(Actions()))
	}
	for _, a := range Actions() {
		want, ok := wantNoRelation[a.ID]
		if !ok {
			t.Errorf("期待表に %s が無い", a.ID)
			continue
		}
		for name, s := range subjects {
			got := Decide(s, a.ID, Resource{}).Allowed
			if exp := contains(want, name); got != exp {
				t.Errorf("%s / %s: 許可=%v 期待=%v", a.ID, name, got, exp)
			}
		}
	}
}

func TestRelations(t *testing.T) {
	cases := []struct {
		action string
		s      string
		res    Resource
		want   bool
	}{
		// コンテンツ: 作者・共同編集者は編集できるが公開はできない（D01）
		{"contents.get", "member", Resource{IsAuthor: true}, true},
		{"contents.revisions.create", "member", Resource{IsAuthor: true}, true},
		{"contents.revisions.create", "member", Resource{IsCollaborator: true}, true},
		{"contents.contributors.update", "member", Resource{IsCollaborator: true}, true},
		{"contents.collaborators.update", "member", Resource{IsAuthor: true}, true},
		{"contents.collaborators.update", "member", Resource{IsCollaborator: true}, false}, // 共同編集者は共同編集者を変更できない
		{"contents.publish", "member", Resource{IsAuthor: true}, false},
		{"contents.delete", "member", Resource{IsAuthor: true}, true},
		{"contents.delete", "member", Resource{IsCollaborator: true}, false}, // 共同編集者は削除できない
		{"contents.unpublish", "member", Resource{IsAuthor: true, IsCollaborator: true}, false},
		{"contents.get", "member", Resource{}, false},
		// 停止中の作者は何もできない
		{"contents.get", "disabled", Resource{IsAuthor: true}, false},
		{"contents.revisions.create", "invited", Resource{IsAuthor: true}, false},
		{"contents.get", "anon", Resource{IsAuthor: true}, false},
		// Owner移譲: 移譲先本人だけが承諾、申請したOwnerだけが取消
		{"owner.transfers.accept", "member", Resource{IsSelf: true}, true},
		{"owner.transfers.accept", "member", Resource{}, false},
		{"owner.transfers.accept", "owner", Resource{}, false}, // Ownerでも他人宛は承諾できない
		{"owner.transfers.accept", "disabled", Resource{IsSelf: true}, false},
		{"owner.transfers.cancel", "owner", Resource{IsRequester: true}, true},
		{"owner.transfers.cancel", "owner", Resource{}, false}, // 他のOwnerが出した申請は取消不可
		{"owner.transfers.cancel", "admin", Resource{IsRequester: true}, false},
		{"owner.transfers.cancel", "member", Resource{IsRequester: true}, false},
		// 画像: アップロード者は自分の画像を取得できる
		{"media.get", "member", Resource{IsUploader: true}, true},
		{"media.get", "member", Resource{}, false},
		{"media.get", "disabled", Resource{IsUploader: true}, false},
		{"media.get", "member", Resource{IsMediaContentMember: true}, true},
		{"media.get", "member", Resource{IsMemberImage: true}, true},
		{"media.get", "disabled", Resource{IsMemberImage: true}, false},
		{"media.get_by_public", "member", Resource{IsMemberImage: true}, false},
		{"media.get_by_public", "member", Resource{IsMediaContentMember: true}, true},
		{"media.get_by_public", "member", Resource{}, false},
		{"media.get", "disabled", Resource{IsMediaContentMember: true}, false},
		// 権限昇格の防止: Adminは管理者権限を持つ対象に対して停止・URL発行ができない
		{"admin.users.status", "admin", Resource{TargetIsAdmin: true}, false},
		{"admin.users.status", "admin", Resource{TargetIsOwner: true, TargetIsAdmin: true}, false},
		{"admin.users.reset_link", "admin", Resource{TargetIsOwner: true, TargetIsAdmin: true}, false},
		{"admin.users.invite_link", "admin", Resource{TargetIsAdmin: true}, false},
		{"admin.users.reset_link", "owner", Resource{TargetIsAdmin: true}, true},
		{"admin.users.reset_link", "owner", Resource{TargetIsOwner: true, TargetIsAdmin: true}, true},
		{"admin.users.status", "admin", Resource{}, true},
		{"admin.users.reset_link", "member", Resource{}, false},
		{"admin.users.reset_link", "editor", Resource{}, false},
		// 一般メンバーが管理者対象に指定しても拒否（元から管理権限がない）
		{"admin.users.status", "member", Resource{TargetIsAdmin: true}, false},
	}
	for _, c := range cases {
		if got := Decide(subjects[c.s], c.action, c.res).Allowed; got != c.want {
			t.Errorf("%s / %s / %+v: 許可=%v 期待=%v", c.action, c.s, c.res, got, c.want)
		}
	}
}

func TestDefaultDenyAndReasons(t *testing.T) {
	if d := Decide(owner, "no.such.action", Resource{}); d.Allowed || d.Reason != ReasonUnknownAction {
		t.Fatalf("未定義アクションは拒否される必要がある: %+v", d)
	}
	if d := Decide(Subject{Authenticated: true, Status: StatusActive, Roles: []Role{"superuser"}}, "admin.users.create", Resource{}); d.Allowed {
		t.Fatalf("未知のロールで許可された")
	}
	if d := Decide(anon, "auth.me", Resource{}); d.Reason != ReasonUnauthed {
		t.Fatalf("匿名の理由: %+v", d)
	}
	if d := Decide(disabled, "auth.me", Resource{}); d.Reason != ReasonInactive {
		t.Fatalf("停止の理由: %+v", d)
	}
	if d := Decide(member, "admin.users.create", Resource{}); d.Reason != ReasonForbidden {
		t.Fatalf("権限不足の理由: %+v", d)
	}
	if d := Decide(admin, "admin.users.status", Resource{TargetIsOwner: true}); d.Reason != ReasonPrivileged {
		t.Fatalf("特権対象の理由: %+v", d)
	}
}

// 停止・招待中のユーザーは、どんなロールを持っていても公開以外の全操作を拒否される。
func TestInactiveAccountsCanOnlyUsePublicActions(t *testing.T) {
	everything := Resource{IsSelf: true, IsRequester: true, IsAuthor: true, IsCollaborator: true, IsUploader: true}
	for _, a := range Actions() {
		if a.rule.public {
			continue
		}
		for _, name := range []string{"anon", "invited", "disabled"} {
			if Decide(subjects[name], a.ID, everything).Allowed {
				t.Errorf("%s で %s が許可された", name, a.ID)
			}
		}
	}
}

// Adminだけが持つ権限でOwner専用操作ができてはならない。
func TestOwnerOnlyActionsAreNotAvailableToAdmin(t *testing.T) {
	for _, id := range []string{"owner.users.admin_role", "owner.transfers.create"} {
		if Decide(admin, id, Resource{}).Allowed {
			t.Errorf("Adminが %s を実行できる", id)
		}
	}
}

// docs/API.md の全エンドポイントが認可表に1対1で存在する（API契約と認可表のドリフト検出）。
func TestAPIDocAndTableMatch(t *testing.T) {
	data, err := os.ReadFile(repoFile("docs/API.md"))
	if err != nil {
		t.Fatal(err)
	}
	// 認可表の対象外: ライブネス/レディネス（インフラ用。ネットワーク層で制限する）
	infra := map[string]bool{"GET /healthz": true, "GET /readyz": true}
	row := regexp.MustCompile("(?m)^\\| (GET/PUT|GET|POST|PUT|PATCH|DELETE) \\| `([^`]+)` \\|")
	doc := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(string(data), -1) {
		key := m[1] + " " + m[2]
		if !infra[key] {
			doc[key] = true
		}
	}
	tab := map[string]bool{}
	for _, a := range Actions() {
		tab[a.Method+" "+a.Path] = true
	}
	var missing, extra []string
	for k := range doc {
		if !tab[k] {
			missing = append(missing, k)
		}
	}
	for k := range tab {
		if !doc[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing)+len(extra) > 0 {
		t.Fatalf("API.mdと認可表が一致しない。API.mdにのみ: %v / 認可表にのみ: %v", missing, extra)
	}
	if len(doc) < 50 {
		t.Fatalf("API.mdの解析結果が少なすぎる(%d件)。正規表現を確認", len(doc))
	}
}

// docs/AUTHZ.md が全アクションIDを載せている。
func TestAuthzDocListsEveryAction(t *testing.T) {
	data, err := os.ReadFile(repoFile("docs/AUTHZ.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range Actions() {
		if !strings.Contains(string(data), "`"+a.ID+"`") {
			t.Errorf("docs/AUTHZ.md に %s が無い", a.ID)
		}
	}
}

func TestActionIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, a := range Actions() {
		if seen[a.ID] {
			t.Errorf("重複: %s", a.ID)
		}
		seen[a.ID] = true
		if a.Method == "" || a.Path == "" {
			t.Errorf("%s のMethod/Pathが空", a.ID)
		}
	}
	_ = fmt.Sprint
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
