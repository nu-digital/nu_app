package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// memberView は他のメンバーに見せてよい項目だけ。ログインID・個人情報4項目・公開クレジット同意・メールなどは含めない。
// bio・faculty・department・schoolYear は本人が「メンバー内公開」にした場合だけ値が入る（それ以外は null）。
type memberView struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	Bio         *string `json:"bio"`
	Faculty     *string `json:"faculty"`
	Department  *string `json:"department"`
	SchoolYear  *int16  `json:"schoolYear"`
}

type memberLink struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	SortOrder int32  `json:"sortOrder"`
}

type memberDevice struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	Name      string `json:"name"`
	Spec      string `json:"spec"`
	SortOrder int32  `json:"sortOrder"`
}

// publicText は、メンバー内公開にしている（public=true）ときだけ値を返す。非公開の値はここで必ず落とす。
func publicText(v pgtype.Text, public bool) *string {
	if !public {
		return nil
	}
	return strPtr(v)
}

func publicInt2(v pgtype.Int2, public bool) *int16 {
	if !public {
		return nil
	}
	return int2Ptr(v)
}

func int2Ptr(v pgtype.Int2) *int16 {
	if !v.Valid {
		return nil
	}
	x := v.Int16
	return &x
}

func encodeNameCursor(name string, id pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id.String() + "|" + name))
}

func decodeNameCursor(c string) (string, pgtype.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", pgtype.UUID{}, false
	}
	idStr, name, ok := strings.Cut(string(raw), "|")
	var id pgtype.UUID
	if !ok || id.Scan(idStr) != nil || !id.Valid {
		return "", pgtype.UUID{}, false
	}
	return name, id, true
}

// listMembers は GET /members。有効なメンバーだけ（退会・停止・招待中は現役一覧に出さない）。
func (s *server) listMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "members.list", authz.Resource{}); !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) > 100 {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "検索語は100文字以内にしてください")
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit は1〜100で指定してください")
			return
		}
		limit = n
	}
	params := store.ListMembersParams{Column1: likeEscape(q), Limit: int32(limit + 1)}
	if c := r.URL.Query().Get("cursor"); c != "" {
		name, id, ok := decodeNameCursor(c)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		params.Column2, params.Column3, params.Column5 = name, id, true
	}
	rows, err := store.New(s.Pool).ListMembers(r.Context(), params)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := encodeNameCursor(last.DisplayName, last.ID)
		next = &c
	}
	items := make([]memberView, 0, len(rows))
	for _, x := range rows {
		items = append(items, memberView{ID: x.ID.String(), DisplayName: x.DisplayName, Bio: publicText(x.Bio, x.BioPublic), Faculty: publicText(x.Faculty, x.FacultyPublic), Department: publicText(x.Department, x.DepartmentPublic), SchoolYear: publicInt2(x.SchoolYear, x.SchoolYearPublic)})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

// getMember は GET /members/{id}。有効でないユーザー・存在しないIDは同じ404。
func (s *server) getMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "members.get", authz.Resource{}); !ok {
		return
	}
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}
	q := store.New(s.Pool)
	m, err := q.GetMember(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
			return
		}
		s.internal(w, r)
		return
	}
	links, err := q.ListMemberLinks(r.Context(), id)
	if err != nil {
		s.internal(w, r)
		return
	}
	devs, err := q.ListMemberDevices(r.Context(), id)
	if err != nil {
		s.internal(w, r)
		return
	}
	out := struct {
		memberView
		Links   []memberLink   `json:"links"`
		Devices []memberDevice `json:"devices"`
	}{memberView: memberView{ID: m.ID.String(), DisplayName: m.DisplayName, Bio: publicText(m.Bio, m.BioPublic), Faculty: publicText(m.Faculty, m.FacultyPublic), Department: publicText(m.Department, m.DepartmentPublic), SchoolYear: publicInt2(m.SchoolYear, m.SchoolYearPublic)},
		Links: make([]memberLink, 0, len(links)), Devices: make([]memberDevice, 0, len(devs))}
	for _, l := range links {
		out.Links = append(out.Links, memberLink{ID: l.ID.String(), Label: l.Label, URL: l.Url, SortOrder: l.SortOrder})
	}
	for _, d := range devs {
		out.Devices = append(out.Devices, memberDevice{ID: d.ID.String(), Category: d.Category, Name: d.Name, Spec: specText(d.SpecsJson), SortOrder: d.SortOrder})
	}
	httpx.JSON(w, http.StatusOK, out)
}
