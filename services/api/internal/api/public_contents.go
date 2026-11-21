package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 公開API（匿名可）。設計: docs/design/p3-cms.md §6。
//
//   - **公開ポインタの版だけ**を返す。下書き・未公開・存在しないものは同じ404。作業版・版の履歴・作者・共同編集者・内部IDは返さない。
//   - 制作メンバーは配信のたびに、公開クレジットに同意中のメンバーだけ表示名を出す。それ以外は「匿名のメンバー」。
//   - 認証情報（Cookie等）を見ない＝応答を変えない。ETagは応答全体のハッシュ。Cache-Control: no-cache。

const anonymousMember = "匿名のメンバー"

// publicMetadataKeys は公開APIに出すメタデータ。sourceId・sourceUrl（移行元の情報）は出さない。
var publicMetadataKeys = map[string]bool{"category": true, "excerpt": true, "image": true, "author": true}

type publicDate struct {
	Label               string  `json:"label"`
	Kind                string  `json:"kind"`
	SortDate            *string `json:"sortDate"`
	SortDateIsEstimated bool    `json:"sortDateIsEstimated"`
}

type publicItem struct {
	Type     string            `json:"type"`
	Section  string            `json:"section"`
	Slug     string            `json:"slug"`
	Title    string            `json:"title"`
	Summary  string            `json:"summary"`
	Metadata map[string]string `json:"metadata"`
	Date     publicDate        `json:"date"`
}

type publicContributor struct {
	DisplayName string `json:"displayName"`
	RoleLabel   string `json:"roleLabel"`
}

type publicDetail struct {
	publicItem
	Markdown     string              `json:"markdown"`
	Contributors []publicContributor `json:"contributors"`
}

func publicMeta(raw []byte) map[string]string {
	out := map[string]string{}
	for k, v := range decodeMetadata(raw) {
		if publicMetadataKeys[k] {
			out[k] = v
		}
	}
	return out
}

func publicDateOf(label, kind string, sd pgtype.Date, est bool) publicDate {
	d := publicDate{Label: label, Kind: kind, SortDateIsEstimated: est}
	if sd.Valid {
		s := sd.Time.Format("2006-01-02")
		d.SortDate = &s
	}
	return d
}

// writePublicJSON は強いETag（応答バイト列全体のハッシュ）つきで返す。If-None-Matchが一致すれば304。
func writePublicJSON(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "INTERNAL", "サーバーでエラーが発生しました")
		return
	}
	sum := sha256.Sum256(b)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		for _, c := range strings.Split(inm, ",") {
			if strings.TrimSpace(c) == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(b, '\n'))
}

func publicNotFound(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func validSection(s string) bool { return s == "blog" || s == "news" || s == "project" }

// getPublicContent は GET /public/contents/{section}/{slug}。
func (s *server) getPublicContent(w http.ResponseWriter, r *http.Request) {
	section, slug := r.PathValue("section"), r.PathValue("slug")
	if !validSection(section) || !slugRe.MatchString(slug) {
		publicNotFound(w, r)
		return
	}
	q := store.New(s.Pool)
	c, err := q.GetPublicContent(r.Context(), store.GetPublicContentParams{Section: section, Slug: slug})
	if err != nil { // 行なし（未公開・存在しない）も、DBの失敗も、内容を漏らさない同じ形で返す
		if isNoRows(err) {
			publicNotFound(w, r)
			return
		}
		s.internal(w, r)
		return
	}
	cs, err := q.ListPublicContributors(r.Context(), c.RevisionID)
	if err != nil {
		s.internal(w, r)
		return
	}
	out := publicDetail{
		publicItem: publicItem{Type: c.Type, Section: c.Section, Slug: c.Slug, Title: c.Title, Summary: c.Summary, Metadata: publicMeta(c.MetadataJson),
			Date: publicDateOf(c.DateLabel, c.DateKind, c.SortDate, c.SortDateIsEstimated)},
		Markdown: c.Markdown, Contributors: make([]publicContributor, 0, len(cs)),
	}
	for _, x := range cs {
		name := anonymousMember
		if x.DisplayName != "" {
			name = x.DisplayName
		}
		out.Contributors = append(out.Contributors, publicContributor{DisplayName: name, RoleLabel: x.RoleLabel})
	}
	writePublicJSON(w, r, out)
}

// listPublicContents は GET /public/contents（section・limit・cursor）。日付の新しい順。
func (s *server) listPublicContents(w http.ResponseWriter, r *http.Request) {
	qp := r.URL.Query()
	limit := 50
	if v := qp.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit は1〜100で指定してください")
			return
		}
		limit = n
	}
	p := store.ListPublicContentsParams{Lim: int32(limit + 1)}
	if v := qp.Get("section"); v != "" {
		if !validSection(v) {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "section が正しくありません")
			return
		}
		p.Section = pgtype.Text{String: v, Valid: true}
	}
	if c := qp.Get("cursor"); c != "" {
		raw, err := base64.RawURLEncoding.DecodeString(c)
		parts := strings.SplitN(string(raw), "|", 2)
		var id pgtype.UUID
		d, derr := time.Parse("2006-01-02", firstOf(parts))
		if err != nil || len(parts) != 2 || derr != nil || id.Scan(parts[1]) != nil || !id.Valid {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		p.BeforeDate, p.BeforeID = pgtype.Date{Time: d, Valid: true}, id
	}
	rows, err := store.New(s.Pool).ListPublicContents(r.Context(), p)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		last := rows[limit-1]
		c := base64.RawURLEncoding.EncodeToString([]byte(last.OrderDate.Time.Format("2006-01-02") + "|" + last.ID.String()))
		next = &c
		rows = rows[:limit]
	}
	items := make([]publicItem, 0, len(rows))
	for _, x := range rows {
		items = append(items, publicItem{Type: x.Type, Section: x.Section, Slug: x.Slug, Title: x.Title, Summary: x.Summary, Metadata: publicMeta(x.MetadataJson),
			Date: publicDateOf(x.DateLabel, x.DateKind, x.SortDate, x.SortDateIsEstimated)})
	}
	writePublicJSON(w, r, map[string]any{"items": items, "nextCursor": next})
}

func firstOf(p []string) string {
	if len(p) == 0 {
		return ""
	}
	return p[0]
}
