// Package authz はAPIの認可判断を集約する。認可はこのパッケージ（Go API）だけが決め、UIの非表示では代替しない。
//
// 方針:
//   - 既定は拒否。表に無いアクションは拒否する。
//   - 認証済みで有効（status=active）なユーザーだけがMember以上の権限を持つ。停止・招待中は何もできない。
//   - Ownerは Admin の権限を内包する。AdminはEditorを内包しない（CMSの編集・公開は、規則に Admin/Owner を明記している）。
//   - 管理者権限（Admin/Owner）を持つユーザーを対象にした操作（停止・招待URL/再設定URLの発行）はOwnerだけが行える。
//     AdminがOwnerやAdminの再設定URLを発行できると、権限昇格（アカウント乗っ取り）になるため。
//
// 根拠: docs/API.md の認可列、docs/ARCHITECTURE.md §C、docs/AUTHZ.md。
package authz

// Role は明示的に付与されるロール。Memberはactiveなユーザーの基本権限で、行を持たない。
type Role string

const (
	RoleEditor Role = "editor"
	RoleAdmin  Role = "admin"
	RoleOwner  Role = "owner"
)

// Status はアカウント状態（users.status）。
type Status string

const (
	StatusInvited  Status = "invited"
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// Subject は操作者。未認証なら Authenticated=false。
type Subject struct {
	Authenticated bool
	Status        Status
	Roles         []Role
}

// Anonymous は未認証の操作者。
var Anonymous = Subject{}

func (s Subject) active() bool { return s.Authenticated && s.Status == StatusActive }

func (s Subject) has(r Role) bool {
	for _, have := range s.Roles {
		if have == r {
			return true
		}
	}
	return false
}

// IsEditorPlus はEditor・Admin・Ownerのいずれか（有効なユーザーのみ）。コンテンツの全件編集・公開の権限に当たる。
func (s Subject) IsEditorPlus() bool {
	return s.active() && (s.has(RoleEditor) || s.has(RoleAdmin) || s.has(RoleOwner))
}

// isAdmin はAdminまたはOwner（OwnerはAdminを内包）。
func (s Subject) isAdmin() bool { return s.has(RoleAdmin) || s.has(RoleOwner) }

// Resource は対象リソースと操作者の関係。呼び出し側がDBから判定して渡す。
type Resource struct {
	// IsSelf: 対象が操作者自身（申請の移譲先本人、自分のプロフィールなど）。
	IsSelf bool
	// IsRequester: 操作者がそのOwner移譲申請を出した本人。
	IsRequester bool
	// IsAuthor / IsCollaborator: コンテンツの作者・共同編集者。
	IsAuthor       bool
	IsCollaborator bool
	// IsUploader: 画像のアップロード者。
	IsUploader bool
	// IsMediaContentMember: その画像を（いずれかの版が）参照しているコンテンツの作者または共同編集者（下書きのプレビュー用）。
	IsMediaContentMember bool
	// IsMemberImage: ログイン中のメンバーに見せてよい画像（有効なユーザーの現在のアバター、アーカイブされていない物品の現在の写真）。
	IsMemberImage bool
	// TargetIsAdmin / TargetIsOwner: 対象ユーザーが管理者権限を持つ（Ownerは両方true）。
	TargetIsAdmin bool
	TargetIsOwner bool
}

// Action はAPIの1操作。Method+PathはAPI.mdの表と1対1で対応する（テストで検査）。
type Action struct {
	ID     string
	Method string
	Path   string
	rule   rule
}

type rule struct {
	// public: 認証不要（トークン検証などはハンドラー側）。
	public bool
	// member: 有効なユーザーなら誰でも。
	member bool
	// roles: いずれかを持つ有効なユーザー（Ownerは Admin を内包）。
	roles []Role
	// relations: 資源との関係が1つでも成り立てば許可（有効なユーザーに限る）。
	relations []relation
	// requireRole: 他の条件に加えて必ず必要なロール（ANDの条件）。
	requireRole Role
	// notPrivilegedTarget: Admin（Ownerでない）は、管理者権限を持つ対象には使えない。
	notPrivilegedTarget bool
}

type relation func(Resource) bool

var (
	self         relation = func(r Resource) bool { return r.IsSelf }
	requester    relation = func(r Resource) bool { return r.IsRequester }
	author       relation = func(r Resource) bool { return r.IsAuthor }
	collaborator relation = func(r Resource) bool { return r.IsCollaborator }
	uploader     relation = func(r Resource) bool { return r.IsUploader }
	mediaContent relation = func(r Resource) bool { return r.IsMediaContentMember }
	memberImage  relation = func(r Resource) bool { return r.IsMemberImage }
)

// Decision は判断結果。
type Decision struct {
	Allowed bool
	// Reason は拒否理由の種別（ログ・テスト用。利用者向け文言ではない）。
	Reason string
}

const (
	ReasonUnknownAction = "unknown_action"
	ReasonUnauthed      = "unauthenticated"
	ReasonInactive      = "inactive_account"
	ReasonForbidden     = "forbidden"
	ReasonPrivileged    = "privileged_target"
)

// Decide は subject が action を resource に対して実行してよいかを返す。
func Decide(s Subject, id string, res Resource) Decision {
	a, ok := byID[id]
	if !ok {
		return Decision{false, ReasonUnknownAction}
	}
	r := a.rule
	if r.public {
		return Decision{true, ""}
	}
	if !s.Authenticated {
		return Decision{false, ReasonUnauthed}
	}
	if !s.active() {
		return Decision{false, ReasonInactive}
	}
	allowed := r.member
	for _, role := range r.roles {
		if s.has(role) || (role == RoleAdmin && s.isAdmin()) {
			allowed = true
		}
	}
	for _, rel := range r.relations {
		if rel(res) {
			allowed = true
		}
	}
	if r.requireRole != "" && !(s.has(r.requireRole) || (r.requireRole == RoleAdmin && s.isAdmin())) {
		allowed = false
	}
	if !allowed {
		return Decision{false, ReasonForbidden}
	}
	if r.notPrivilegedTarget && !s.has(RoleOwner) && (res.TargetIsAdmin || res.TargetIsOwner) {
		return Decision{false, ReasonPrivileged}
	}
	return Decision{true, ""}
}

// Actions は定義済みの全アクション（API.mdと同じ順）。
func Actions() []Action { return append([]Action(nil), table...) }

var byID = func() map[string]Action {
	m := make(map[string]Action, len(table))
	for _, a := range table {
		if _, dup := m[a.ID]; dup {
			panic("authz: アクションIDが重複: " + a.ID)
		}
		m[a.ID] = a
	}
	return m
}()

var (
	pub        = rule{public: true}
	anyMember  = rule{member: true}
	adminOnly  = rule{roles: []Role{RoleAdmin}}
	ownerOnly  = rule{roles: []Role{RoleOwner}}
	editorPlus = []Role{RoleEditor, RoleAdmin, RoleOwner}
)

var table = []Action{
	// 1. 認証・アカウント
	{"auth.login", "POST", "/auth/login", pub},
	{"auth.logout", "POST", "/auth/logout", anyMember},
	{"auth.me", "GET", "/auth/me", anyMember},
	{"auth.accept_invite", "POST", "/auth/accept-invite", pub},
	{"auth.reset_password", "POST", "/auth/reset-password", pub},
	{"admin.users.list", "GET", "/admin/users", adminOnly},
	{"admin.users.create", "POST", "/admin/users", adminOnly},
	{"admin.users.invite_link", "POST", "/admin/users/{id}/invite-link", rule{roles: []Role{RoleAdmin}, notPrivilegedTarget: true}},
	{"admin.users.reset_link", "POST", "/admin/users/{id}/reset-link", rule{roles: []Role{RoleAdmin}, notPrivilegedTarget: true}},
	{"admin.users.status", "PATCH", "/admin/users/{id}/status", rule{roles: []Role{RoleAdmin}, notPrivilegedTarget: true}},
	{"admin.users.editor_role", "PUT", "/admin/users/{id}/editor-role", adminOnly},
	{"owner.users.admin_role", "PUT", "/owner/users/{id}/admin-role", ownerOnly},
	{"owner.transfers.create", "POST", "/owner/transfers", ownerOnly},
	{"owner.transfers.cancel", "POST", "/owner/transfers/{id}/cancel", rule{requireRole: RoleOwner, relations: []relation{requester}}},
	{"owner.transfers.incoming", "GET", "/owner/transfers/incoming", anyMember},
	{"owner.transfers.accept", "POST", "/owner/transfers/{id}/accept", rule{relations: []relation{self}}},

	// 2. メンバー・プロフィール
	{"members.list", "GET", "/members", anyMember},
	{"members.get", "GET", "/members/{id}", anyMember},
	{"me.profile.get", "GET", "/me/profile", anyMember},
	{"me.profile.update", "PATCH", "/me/profile", anyMember},
	{"me.profile.visibility", "PUT", "/me/profile/visibility", anyMember},
	{"me.profile.public_credit_consent", "GET/PUT", "/me/profile/public-credit-consent", anyMember},
	{"me.profile.links.create", "POST", "/me/profile/links", anyMember},
	{"me.profile.links.update", "PATCH", "/me/profile/links/{id}", anyMember},
	{"me.profile.links.delete", "DELETE", "/me/profile/links/{id}", anyMember},
	{"me.devices.create", "POST", "/me/devices", anyMember},
	{"me.devices.update", "PATCH", "/me/devices/{id}", anyMember},
	{"me.devices.delete", "DELETE", "/me/devices/{id}", anyMember},
	{"admin.users.private_profile.get", "GET", "/admin/users/{id}/private-profile", adminOnly},
	{"admin.users.private_profile.delete", "DELETE", "/admin/users/{id}/private-profile", adminOnly},
	{"me.profile.avatar.set", "PUT", "/me/profile/avatar", anyMember},
	{"me.profile.avatar.clear", "DELETE", "/me/profile/avatar", anyMember},
	{"inventory.items.photo.set", "PUT", "/inventory/items/{id}/photo", adminOnly},
	{"inventory.items.photo.clear", "DELETE", "/inventory/items/{id}/photo", adminOnly},
	{"inventory.items.images.set", "PUT", "/inventory/items/{id}/images", adminOnly},
	{"inventory.categories.list", "GET", "/inventory/categories", anyMember},
	{"inventory.categories.create", "POST", "/inventory/categories", adminOnly},
	{"inventory.categories.reorder", "PUT", "/inventory/categories/order", adminOnly},
	{"inventory.categories.update", "PATCH", "/inventory/categories/{id}", adminOnly},
	{"inventory.categories.delete", "DELETE", "/inventory/categories/{id}", adminOnly},
	{"admin.contents.published_search", "GET", "/admin/contents/published-search", adminOnly},
	{"admin.legacy_authors.list", "GET", "/admin/legacy-authors", adminOnly},
	{"admin.legacy_authors.permit", "POST", "/admin/legacy-authors/{id}/permit", adminOnly},
	{"admin.legacy_authors.withdraw", "POST", "/admin/legacy-authors/{id}/withdraw", adminOnly},
	{"admin.legacy_authors.link", "PUT", "/admin/legacy-authors/{id}/link", adminOnly},
	{"admin.legacy_authors.unlink", "DELETE", "/admin/legacy-authors/{id}/link", adminOnly},

	// 3. 物品管理
	{"inventory.items.list", "GET", "/inventory/items", anyMember},
	{"inventory.items.get", "GET", "/inventory/items/{id}", anyMember},
	{"inventory.items.create", "POST", "/inventory/items", adminOnly},
	{"inventory.items.update", "PATCH", "/inventory/items/{id}", adminOnly},
	{"inventory.units.create", "POST", "/inventory/items/{id}/units", adminOnly},
	{"inventory.loans.create", "POST", "/inventory/loans", anyMember},
	{"inventory.loans.return", "POST", "/inventory/loans/{id}/returns", anyMember},
	{"inventory.loans.list", "GET", "/inventory/loans", anyMember},
	{"inventory.history", "GET", "/inventory/history", anyMember},
	{"inventory.corrections.create", "POST", "/admin/inventory/corrections", adminOnly},
	{"notifications.settings.list", "GET", "/admin/notifications/settings", adminOnly},
	{"notifications.settings.update", "PUT", "/admin/notifications/settings/{eventType}", adminOnly},
	{"notifications.deliveries.list", "GET", "/admin/notifications/deliveries", adminOnly},
	{"notifications.test", "POST", "/admin/notifications/test", adminOnly},
	{"notifications.deliveries.retry", "POST", "/admin/notifications/deliveries/{id}/retry", adminOnly},

	// 4. CMS（記事・作品。D01: 記事と作品は共通）
	{"contents.list", "GET", "/contents", anyMember},
	{"contents.create", "POST", "/contents", anyMember},
	{"contents.get", "GET", "/contents/{id}", rule{roles: editorPlus, relations: []relation{author, collaborator}}},
	{"contents.delete", "DELETE", "/contents/{id}", rule{roles: editorPlus, relations: []relation{author}}}, // 公開中の記事は、ハンドラーでEditor以上に限る
	{"contents.revisions.create", "POST", "/contents/{id}/revisions", rule{roles: editorPlus, relations: []relation{author, collaborator}}},
	{"contents.collaborators.update", "PUT", "/contents/{id}/collaborators", rule{roles: editorPlus, relations: []relation{author}}},
	{"contents.contributors.update", "PUT", "/contents/{id}/contributors", rule{roles: editorPlus, relations: []relation{author, collaborator}}},
	{"contents.publish", "POST", "/contents/{id}/publish", rule{roles: editorPlus}},
	{"contents.unpublish", "POST", "/contents/{id}/unpublish", rule{roles: editorPlus}},
	{"public.contents.list", "GET", "/public/contents", pub},
	{"public.contents.get", "GET", "/public/contents/{section}/{slug}", pub},

	// 5. メディア・通知・ダッシュボード
	{"media.upload", "POST", "/media", anyMember},
	{"media.get", "GET", "/media/{id}", rule{roles: editorPlus, relations: []relation{uploader, mediaContent, memberImage}}},
	{"media.get_by_public", "GET", "/media/by-public/{publicId}", rule{roles: editorPlus, relations: []relation{uploader, mediaContent}}},
	{"public.media.get", "GET", "/public/media/{publicId}", pub},
	{"dashboard.feed", "GET", "/dashboard/feed", anyMember},
	{"admin.notifications.settings.get", "GET", "/admin/notifications/settings", adminOnly},
	{"admin.notifications.settings.update", "PUT", "/admin/notifications/settings/{eventType}", adminOnly},
	{"admin.notifications.test", "POST", "/admin/notifications/test", adminOnly},
	{"admin.notifications.deliveries", "GET", "/admin/notifications/deliveries", adminOnly},
}
