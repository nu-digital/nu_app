package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// searchPublishedContents は GET /admin/contents/published-search?q=（Admin/Owner）。
// 個人情報の削除・公開同意の撤回の依頼（D08）のとき、公開中の記事・作品の本文・題名・役割名に、その人の名前などが残っていないかを探す。
// 公開中の版だけが対象（過去の版は公開されないが不変なので消せない。公開版を直すには新しい版を保存して公開する）。
// 検索語は監査に残さない（人名の可能性があるため）。結果の抜粋は、すでに公開されている本文の一部。
func (s *server) searchPublishedContents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "admin.contents.published_search", authz.Resource{})
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := utf8.RuneCountInString(q); n < 2 || n > 50 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "探す語を2〜50文字で入力してください")
		return
	}
	rows, err := store.New(s.Pool).SearchPublishedContents(r.Context(), q)
	if err != nil {
		s.internal(w, r)
		return
	}
	type item struct {
		Section string   `json:"section"`
		Slug    string   `json:"slug"`
		Title   string   `json:"title"`
		Found   []string `json:"found"` // title / summary / markdown / roleLabel
		Snippet string   `json:"snippet,omitempty"`
	}
	items := make([]item, 0, len(rows))
	for _, x := range rows {
		it := item{Section: x.Section, Slug: x.Slug, Title: x.Title, Found: []string{}, Snippet: strings.Join(strings.Fields(x.Snippet), " ")}
		for f, hit := range map[string]bool{"title": x.InTitle, "summary": x.InSummary, "markdown": x.InMarkdown, "roleLabel": x.InRole} {
			if hit {
				it.Found = append(it.Found, f)
			}
		}
		sortStrings(it.Found)
		items = append(items, it)
	}
	// 検索したこと自体は残す（誰が・いつ。語・結果は残さない）
	b := []byte(`{}`)
	_ = store.New(s.Pool).InsertAuditLog(r.Context(), store.InsertAuditLogParams{
		ActorUserID: a.Identity.UserID, Action: "admin.published_search", TargetType: pgtype.Text{String: "content", Valid: true}, SummaryJson: b,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "truncated": len(rows) == 100})
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
