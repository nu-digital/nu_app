package api

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// userListItem は一覧に返す項目。個人情報（氏名・メール・学生証番号・最寄り駅）・パスワード・トークン関連は含めない。
type userListItem struct {
	ID          string     `json:"id"`
	LoginID     string     `json:"loginId"`
	DisplayName string     `json:"displayName"`
	Status      string     `json:"status"`
	Roles       []string   `json:"roles"`
	CreatedAt   time.Time  `json:"createdAt"`
	ActivatedAt *time.Time `json:"activatedAt"`
}

// likeEscape はLIKEのワイルドカード文字を無効にする（検索語をそのまま部分一致に使うため）。
func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func encodeCursor(t time.Time, id pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(c string) (time.Time, pgtype.UUID, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, false
	}
	ts, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, pgtype.UUID{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	var id pgtype.UUID
	if err != nil || id.Scan(idStr) != nil || !id.Valid {
		return time.Time{}, pgtype.UUID{}, false
	}
	return t, id, true
}

// listUsers は GET /admin/users。
func (s *server) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "admin.users.list", authz.Resource{}); !ok {
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
	params := store.ListUsersParams{Column1: likeEscape(q), Limit: int32(limit + 1)}
	if c := r.URL.Query().Get("cursor"); c != "" {
		t, id, ok := decodeCursor(c)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		params.Column2 = pgtype.Timestamptz{Time: t, Valid: true}
		params.Column3 = id
	}
	rows, err := store.New(s.Pool).ListUsers(r.Context(), params)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := encodeCursor(last.CreatedAt.Time, last.ID)
		next = &c
	}
	items := make([]userListItem, 0, len(rows))
	for _, x := range rows {
		it := userListItem{ID: x.ID.String(), LoginID: x.LoginID, DisplayName: x.DisplayName, Status: x.Status, Roles: x.Roles, CreatedAt: x.CreatedAt.Time}
		if it.Roles == nil {
			it.Roles = []string{}
		}
		if x.ActivatedAt.Valid {
			t := x.ActivatedAt.Time
			it.ActivatedAt = &t
		}
		items = append(items, it)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}
