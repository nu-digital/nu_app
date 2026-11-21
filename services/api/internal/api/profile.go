package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 暫定の上限（DECISIONS P2-01-1）。
const (
	maxLinks   = 10
	maxDevices = 20
)

// optStr は「未指定」「null」「値あり」を区別できる文字列（PATCHの部分更新用）。
type optStr struct {
	Set, Null bool
	Val       string
}

func (o *optStr) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

type optInt struct {
	Set, Null bool
	Val       int
}

func (o *optInt) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

type fieldErr struct{ msg string }

func (e *fieldErr) Error() string { return e.msg }

func invalid(msg string) error { return &errTx{http.StatusUnprocessableEntity, "INVALID_INPUT", msg} }

// textOrNil は前後の空白を除き、空なら nil（任意項目の消去）。長さは文字数で検査する。
func textOrNil(o optStr, name string, max int) (*string, error) {
	if !o.Set || o.Null {
		return nil, nil
	}
	v := strings.TrimSpace(o.Val)
	if utf8.RuneCountInString(v) > max {
		return nil, invalid(name + "は" + itoa(max) + "文字以内で入力してください")
	}
	if v == "" {
		return nil, nil
	}
	return &v, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func pgText(p *string) pgtype.Text {
	if p == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *p, Valid: true}
}

func strPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

// ---- 応答の形 ----

type linkView struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	SortOrder  int32  `json:"sortOrder"`
	Visibility string `json:"visibility"`
}

type deviceView struct {
	ID         string `json:"id"`
	Category   string `json:"category"`
	Name       string `json:"name"`
	Spec       string `json:"spec"`
	SortOrder  int32  `json:"sortOrder"`
	Visibility string `json:"visibility"`
}

type consentView struct {
	Granted     bool       `json:"granted"`
	GrantedAt   *time.Time `json:"grantedAt"`
	WithdrawnAt *time.Time `json:"withdrawnAt"`
}

type myProfileView struct {
	DisplayName string            `json:"displayName"`
	Bio         *string           `json:"bio"`
	Faculty     *string           `json:"faculty"`
	Department  *string           `json:"department"`
	SchoolYear  *int16            `json:"schoolYear"`
	Visibility  map[string]string `json:"visibility"`
	// Private は氏名・大学メール・学生証番号・最寄り駅。本人と権限あるAdmin/Ownerだけが読める（メンバーには決して返さない）。
	Private struct {
		LegalName       *string `json:"legalName"`
		StudentNumber   *string `json:"studentNumber"`
		UniversityEmail *string `json:"universityEmail"`
		NearestStation  *string `json:"nearestStation"`
	} `json:"private"`
	Links               []linkView   `json:"links"`
	Devices             []deviceView `json:"devices"`
	PublicCreditConsent consentView  `json:"publicCreditConsent"`
}

var visibilityKeys = map[string]string{"bio": "bio", "faculty": "faculty", "department": "department", "schoolYear": "school_year"}

func specText(raw []byte) string {
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		return m["text"]
	}
	return ""
}

func toLinkView(l store.ProfileLink) linkView {
	return linkView{ID: l.ID.String(), Label: l.Label, URL: l.Url, SortOrder: l.SortOrder, Visibility: l.Visibility}
}

func toDeviceView(d store.PersonalDevice) deviceView {
	return deviceView{ID: d.ID.String(), Category: d.Category, Name: d.Name, Spec: specText(d.SpecsJson), SortOrder: d.SortOrder, Visibility: d.Visibility}
}

func toConsentView(c store.PublicCreditConsent, found bool) consentView {
	v := consentView{}
	if !found {
		return v
	}
	if c.GrantedAt.Valid {
		t := c.GrantedAt.Time
		v.GrantedAt = &t
	}
	if c.WithdrawnAt.Valid {
		t := c.WithdrawnAt.Time
		v.WithdrawnAt = &t
	}
	v.Granted = c.GrantedAt.Valid && !c.WithdrawnAt.Valid
	return v
}

func (s *server) loadMyProfile(ctx context.Context, q *store.Queries, uid pgtype.UUID) (myProfileView, error) {
	p, err := q.GetProfile(ctx, uid)
	if err != nil {
		return myProfileView{}, err
	}
	v := myProfileView{DisplayName: p.DisplayName, Bio: strPtr(p.Bio), Faculty: strPtr(p.Faculty), Department: strPtr(p.Department),
		Visibility: map[string]string{"bio": "private", "faculty": "private", "department": "private", "schoolYear": "private"}} // 既定は非公開
	if p.SchoolYear.Valid {
		y := p.SchoolYear.Int16
		v.SchoolYear = &y
	}
	vis, err := q.ListFieldVisibility(ctx, uid)
	if err != nil {
		return myProfileView{}, err
	}
	for _, x := range vis {
		for api, db := range visibilityKeys {
			if db == x.FieldKey {
				v.Visibility[api] = x.Visibility
			}
		}
	}
	if pp, err := q.GetPersonalProfile(ctx, uid); err == nil {
		v.Private.LegalName, v.Private.StudentNumber = strPtr(pp.LegalName), strPtr(pp.StudentNumber)
		v.Private.UniversityEmail, v.Private.NearestStation = strPtr(pp.UniversityEmail), strPtr(pp.NearestStation)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return myProfileView{}, err
	}
	links, err := q.ListProfileLinks(ctx, uid)
	if err != nil {
		return myProfileView{}, err
	}
	v.Links = make([]linkView, 0, len(links))
	for _, l := range links {
		v.Links = append(v.Links, toLinkView(l))
	}
	devs, err := q.ListPersonalDevices(ctx, uid)
	if err != nil {
		return myProfileView{}, err
	}
	v.Devices = make([]deviceView, 0, len(devs))
	for _, d := range devs {
		v.Devices = append(v.Devices, toDeviceView(d))
	}
	c, err := q.GetPublicCreditConsent(ctx, uid)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return myProfileView{}, err
	}
	v.PublicCreditConsent = toConsentView(c, err == nil)
	return v, nil
}

// auditEvent は個人情報の値を含まない要約で監査ログを残す。
func (s *server) auditEvent(ctx context.Context, q *store.Queries, actor, target pgtype.UUID, action string, summary map[string]any) error {
	b, _ := json.Marshal(summary)
	return q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorUserID: actor, Action: action, TargetType: pgtype.Text{String: "user", Valid: true},
		TargetID: pgtype.Text{String: target.String(), Valid: true}, SummaryJson: b,
	})
}

// getMyProfile は GET /me/profile。
func (s *server) getMyProfile(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.get", authz.Resource{})
	if !ok {
		return
	}
	v, err := s.loadMyProfile(r.Context(), store.New(s.Pool), a.Identity.UserID)
	if err != nil {
		s.internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

var (
	studentNumberPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
)

type patchProfileReq struct {
	DisplayName     optStr `json:"displayName"`
	Bio             optStr `json:"bio"`
	Faculty         optStr `json:"faculty"`
	Department      optStr `json:"department"`
	SchoolYear      optInt `json:"schoolYear"`
	LegalName       optStr `json:"legalName"`
	StudentNumber   optStr `json:"studentNumber"`
	UniversityEmail optStr `json:"universityEmail"`
	NearestStation  optStr `json:"nearestStation"`
}

// patchMyProfile は PATCH /me/profile（部分更新。未指定は変更なし、nullまたは空で任意項目を消去。表示名は消せない）。
func (s *server) patchMyProfile(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.update", authz.Resource{})
	if !ok {
		return
	}
	var req patchProfileReq
	if !decodeJSON(w, r, &req) {
		return
	}
	var out myProfileView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		uid := a.Identity.UserID
		if _, err := q.GetUserForUpdate(r.Context(), uid); err != nil { // 同じ本人の同時更新を直列化する
			return err
		}
		p, err := q.GetProfile(r.Context(), uid)
		if err != nil {
			return err
		}
		var changed []string
		upd := store.UpdateProfileFieldsParams{UserID: uid, DisplayName: p.DisplayName, Bio: p.Bio, Faculty: p.Faculty, Department: p.Department, SchoolYear: p.SchoolYear}
		if req.DisplayName.Set {
			name := strings.TrimSpace(req.DisplayName.Val)
			if req.DisplayName.Null || name == "" || utf8.RuneCountInString(name) > 64 {
				return invalid("表示名は1〜64文字で入力してください（空にはできません）")
			}
			upd.DisplayName = name
			changed = append(changed, "displayName")
		}
		set := func(o optStr, name string, max int, dst *pgtype.Text, key string) error {
			if !o.Set {
				return nil
			}
			v, err := textOrNil(o, name, max)
			if err != nil {
				return err
			}
			*dst = pgText(v)
			changed = append(changed, key)
			return nil
		}
		if err := set(req.Bio, "自己紹介", 1000, &upd.Bio, "bio"); err != nil {
			return err
		}
		if err := set(req.Faculty, "学部", 64, &upd.Faculty, "faculty"); err != nil {
			return err
		}
		if err := set(req.Department, "学科", 64, &upd.Department, "department"); err != nil {
			return err
		}
		if req.SchoolYear.Set {
			if req.SchoolYear.Null {
				upd.SchoolYear = pgtype.Int2{}
			} else if req.SchoolYear.Val < 1 || req.SchoolYear.Val > 10 {
				return invalid("学年は1〜10で入力してください")
			} else {
				upd.SchoolYear = pgtype.Int2{Int16: int16(req.SchoolYear.Val), Valid: true}
			}
			changed = append(changed, "schoolYear")
		}
		if err := q.UpdateProfileFields(r.Context(), upd); err != nil {
			return err
		}

		// 個人情報4項目（本人と権限あるAdmin/Ownerだけが読める）
		if req.LegalName.Set || req.StudentNumber.Set || req.UniversityEmail.Set || req.NearestStation.Set {
			pp, err := q.GetPersonalProfile(r.Context(), uid)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			up := store.UpsertPersonalProfileParams{UserID: uid, LegalName: pp.LegalName, StudentNumber: pp.StudentNumber, UniversityEmail: pp.UniversityEmail, NearestStation: pp.NearestStation}
			if err := set(req.LegalName, "氏名", 100, &up.LegalName, "legalName"); err != nil {
				return err
			}
			if err := set(req.NearestStation, "最寄り駅", 50, &up.NearestStation, "nearestStation"); err != nil {
				return err
			}
			if err := set(req.StudentNumber, "学生証番号", 32, &up.StudentNumber, "studentNumber"); err != nil {
				return err
			}
			if up.StudentNumber.Valid && !studentNumberPattern.MatchString(up.StudentNumber.String) {
				return invalid("学生証番号は半角英数字とハイフンで入力してください")
			}
			if err := set(req.UniversityEmail, "大学メール", 254, &up.UniversityEmail, "universityEmail"); err != nil {
				return err
			}
			if up.UniversityEmail.Valid {
				if addr, err := mail.ParseAddress(up.UniversityEmail.String); err != nil || addr.Address != up.UniversityEmail.String || !strings.Contains(addr.Address[strings.LastIndex(addr.Address, "@"):], ".") {
					return invalid("大学メールの形式が正しくありません")
				}
			}
			if err := q.UpsertPersonalProfile(r.Context(), up); err != nil {
				return err
			}
		}
		if len(changed) > 0 {
			// 値は記録しない。変更した項目名だけを残す。
			if err := s.auditEvent(r.Context(), q, uid, uid, "profile.updated", map[string]any{"fields": changed}); err != nil {
				return err
			}
		}
		out, err = s.loadMyProfile(r.Context(), q, uid)
		return err
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// putVisibility は PUT /me/profile/visibility（メンバー内公開の設定。初期値は非公開。個人情報4項目は対象外）。
func (s *server) putVisibility(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.visibility", authz.Resource{})
	if !ok {
		return
	}
	var req map[string]string
	if !decodeJSON(w, r, &req) {
		return
	}
	for k, v := range req {
		if _, ok := visibilityKeys[k]; !ok {
			msg := "公開設定できない項目です: " + k
			switch k {
			case "legalName", "studentNumber", "universityEmail", "nearestStation":
				msg = "氏名・大学メール・学生証番号・最寄り駅は公開設定できません（本人と管理者だけが見られます）"
			}
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", msg)
			return
		}
		if v != "private" && v != "members" {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "公開範囲は private か members を指定してください")
			return
		}
	}
	var out myProfileView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		uid := a.Identity.UserID
		for k, v := range req {
			if err := q.UpsertFieldVisibility(r.Context(), store.UpsertFieldVisibilityParams{UserID: uid, FieldKey: visibilityKeys[k], Visibility: v}); err != nil {
				return err
			}
		}
		if len(req) > 0 {
			keys := make([]string, 0, len(req))
			for k := range req {
				keys = append(keys, k)
			}
			if err := s.auditEvent(r.Context(), q, uid, uid, "profile.visibility_changed", map[string]any{"fields": keys}); err != nil {
				return err
			}
		}
		var err error
		out, err = s.loadMyProfile(r.Context(), q, uid)
		return err
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---- 公開クレジット同意（メンバー内公開とは別。D02・D08） ----

func (s *server) getConsent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.public_credit_consent", authz.Resource{})
	if !ok {
		return
	}
	c, err := store.New(s.Pool).GetPublicCreditConsent(r.Context(), a.Identity.UserID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, toConsentView(c, err == nil))
}

func (s *server) putConsent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.public_credit_consent", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		Granted *bool `json:"granted"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Granted == nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "granted に true か false を指定してください")
		return
	}
	var out consentView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		uid := a.Identity.UserID
		now := pgtype.Timestamptz{Time: s.Now(), Valid: true}
		cur, err := q.GetPublicCreditConsent(r.Context(), uid)
		active := err == nil && cur.GrantedAt.Valid && !cur.WithdrawnAt.Valid
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		switch {
		case *req.Granted && !active:
			if err := q.GrantPublicCreditConsent(r.Context(), store.GrantPublicCreditConsentParams{UserID: uid, GrantedAt: now}); err != nil {
				return err
			}
			if err := s.auditEvent(r.Context(), q, uid, uid, "profile.public_credit_consent_changed", map[string]any{"granted": true}); err != nil {
				return err
			}
		case !*req.Granted && active:
			if _, err := q.WithdrawPublicCreditConsent(r.Context(), store.WithdrawPublicCreditConsentParams{UpdatedBy: uid, WithdrawnAt: now}); err != nil {
				return err
			}
			if err := s.auditEvent(r.Context(), q, uid, uid, "profile.public_credit_consent_changed", map[string]any{"granted": false}); err != nil {
				return err
			}
		}
		c, err := q.GetPublicCreditConsent(r.Context(), uid)
		out = toConsentView(c, err == nil)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// ---- 外部リンク ----

func validateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n") {
		return "", invalid("URLを正しく入力してください")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", invalid("URLは https:// または http:// で始まる形式で入力してください（ユーザー名・パスワードを含むURLは使えません）")
	}
	return u.String(), nil
}

func validVisibility(v string) bool { return v == "private" || v == "members" }

func (s *server) createLink(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.links.create", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		Label      string `json:"label"`
		URL        string `json:"url"`
		SortOrder  *int32 `json:"sortOrder"`
		Visibility string `json:"visibility"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" || utf8.RuneCountInString(label) > 50 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "名前は1〜50文字で入力してください")
		return
	}
	u, err := validateURL(req.URL)
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	vis := req.Visibility
	if vis == "" {
		vis = "private" // 既定は非公開
	}
	if !validVisibility(vis) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "公開範囲は private か members を指定してください")
		return
	}
	var out linkView
	err = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		uid := a.Identity.UserID
		if _, err := q.GetUserForUpdate(r.Context(), uid); err != nil { // 上限チェックを直列化
			return err
		}
		n, err := q.CountProfileLinks(r.Context(), uid)
		if err != nil {
			return err
		}
		if n >= maxLinks {
			return &errTx{http.StatusConflict, "LIMIT_REACHED", "外部リンクは" + itoa(maxLinks) + "件までです"}
		}
		order := int32(n)
		if req.SortOrder != nil {
			order = *req.SortOrder
		}
		l, err := q.CreateProfileLink(r.Context(), store.CreateProfileLinkParams{UserID: uid, Label: label, Url: u, SortOrder: order, Visibility: vis})
		if err != nil {
			return err
		}
		out = toLinkView(l)
		return s.auditEvent(r.Context(), q, uid, uid, "profile.link_created", map[string]any{})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func parseItemID(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	var id pgtype.UUID
	if err := id.Scan(r.PathValue("id")); err != nil || !id.Valid {
		httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return pgtype.UUID{}, false
	}
	return id, true
}

func (s *server) updateLink(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.links.update", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req struct {
		Label      optStr `json:"label"`
		URL        optStr `json:"url"`
		SortOrder  *int32 `json:"sortOrder"`
		Visibility optStr `json:"visibility"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var out linkView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		cur, err := q.GetProfileLinkForUpdate(r.Context(), store.GetProfileLinkForUpdateParams{ID: id, UserID: a.Identity.UserID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"} // 他人の項目も存在しないのと同じ応答
			}
			return err
		}
		p := store.UpdateProfileLinkParams{ID: id, UserID: a.Identity.UserID, Label: cur.Label, Url: cur.Url, SortOrder: cur.SortOrder, Visibility: cur.Visibility}
		if req.Label.Set {
			l := strings.TrimSpace(req.Label.Val)
			if req.Label.Null || l == "" || utf8.RuneCountInString(l) > 50 {
				return invalid("名前は1〜50文字で入力してください")
			}
			p.Label = l
		}
		if req.URL.Set {
			if req.URL.Null {
				return invalid("URLを正しく入力してください")
			}
			u, err := validateURL(req.URL.Val)
			if err != nil {
				return err
			}
			p.Url = u
		}
		if req.SortOrder != nil {
			p.SortOrder = *req.SortOrder
		}
		if req.Visibility.Set {
			if req.Visibility.Null || !validVisibility(req.Visibility.Val) {
				return invalid("公開範囲は private か members を指定してください")
			}
			p.Visibility = req.Visibility.Val
		}
		l, err := q.UpdateProfileLink(r.Context(), p)
		if err != nil {
			return err
		}
		out = toLinkView(l)
		return nil
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *server) deleteLink(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.links.delete", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		n, err := q.DeleteProfileLink(r.Context(), store.DeleteProfileLinkParams{ID: id, UserID: a.Identity.UserID})
		if err != nil {
			return err
		}
		if n == 0 {
			return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "profile.link_deleted", map[string]any{})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// ---- 個人所有機材（団体の貸出物品とは別） ----

type deviceReq struct {
	Category   optStr `json:"category"`
	Name       optStr `json:"name"`
	Spec       optStr `json:"spec"`
	SortOrder  *int32 `json:"sortOrder"`
	Visibility optStr `json:"visibility"`
}

func requireText(o optStr, name string, max int) (string, error) {
	v := strings.TrimSpace(o.Val)
	if !o.Set || o.Null || v == "" || utf8.RuneCountInString(v) > max {
		return "", invalid(name + "は1〜" + itoa(max) + "文字で入力してください")
	}
	return v, nil
}

func specJSON(spec string) ([]byte, error) {
	if utf8.RuneCountInString(spec) > 500 {
		return nil, invalid("詳細は500文字以内で入力してください")
	}
	return json.Marshal(map[string]string{"text": spec})
}

func (s *server) createDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.devices.create", authz.Resource{})
	if !ok {
		return
	}
	var req deviceReq
	if !decodeJSON(w, r, &req) {
		return
	}
	cat, err := requireText(req.Category, "種類", 30)
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	name, err := requireText(req.Name, "名前", 100)
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	spec, err := specJSON(strings.TrimSpace(req.Spec.Val))
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	vis := "private"
	if req.Visibility.Set {
		if req.Visibility.Null || !validVisibility(req.Visibility.Val) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "公開範囲は private か members を指定してください")
			return
		}
		vis = req.Visibility.Val
	}
	var out deviceView
	err = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		uid := a.Identity.UserID
		if _, err := q.GetUserForUpdate(r.Context(), uid); err != nil {
			return err
		}
		n, err := q.CountPersonalDevices(r.Context(), uid)
		if err != nil {
			return err
		}
		if n >= maxDevices {
			return &errTx{http.StatusConflict, "LIMIT_REACHED", "個人所有機材は" + itoa(maxDevices) + "件までです"}
		}
		order := int32(n)
		if req.SortOrder != nil {
			order = *req.SortOrder
		}
		d, err := q.CreatePersonalDevice(r.Context(), store.CreatePersonalDeviceParams{UserID: uid, Category: cat, Name: name, SpecsJson: spec, SortOrder: order, Visibility: vis})
		if err != nil {
			return err
		}
		out = toDeviceView(d)
		return s.auditEvent(r.Context(), q, uid, uid, "profile.device_created", map[string]any{})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (s *server) updateDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.devices.update", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req deviceReq
	if !decodeJSON(w, r, &req) {
		return
	}
	var out deviceView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		cur, err := q.GetPersonalDeviceForUpdate(r.Context(), store.GetPersonalDeviceForUpdateParams{ID: id, UserID: a.Identity.UserID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		p := store.UpdatePersonalDeviceParams{ID: id, UserID: a.Identity.UserID, Category: cur.Category, Name: cur.Name, SpecsJson: cur.SpecsJson, SortOrder: cur.SortOrder, Visibility: cur.Visibility}
		if req.Category.Set {
			if p.Category, err = requireText(req.Category, "種類", 30); err != nil {
				return err
			}
		}
		if req.Name.Set {
			if p.Name, err = requireText(req.Name, "名前", 100); err != nil {
				return err
			}
		}
		if req.Spec.Set {
			if p.SpecsJson, err = specJSON(strings.TrimSpace(req.Spec.Val)); err != nil {
				return err
			}
		}
		if req.SortOrder != nil {
			p.SortOrder = *req.SortOrder
		}
		if req.Visibility.Set {
			if req.Visibility.Null || !validVisibility(req.Visibility.Val) {
				return invalid("公開範囲は private か members を指定してください")
			}
			p.Visibility = req.Visibility.Val
		}
		d, err := q.UpdatePersonalDevice(r.Context(), p)
		if err != nil {
			return err
		}
		out = toDeviceView(d)
		return nil
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *server) deleteDevice(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.devices.delete", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		n, err := q.DeletePersonalDevice(r.Context(), store.DeletePersonalDeviceParams{ID: id, UserID: a.Identity.UserID})
		if err != nil {
			return err
		}
		if n == 0 {
			return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "profile.device_deleted", map[string]any{})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
