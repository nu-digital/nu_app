package legacyimport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	contentDir  = "../../../../apps/web/content"
	baselineDir = "../../../../docs/legacy-baseline"
)

type fmGolden struct {
	Collection, Slug string
	Fm               map[string]*string
	Types            map[string]string
	BodyLength       int
}

type dateGolden struct {
	Collection, Slug, Label, Formatted string
	Order                              int64
	TimeDatetime                       *string
}

func readGolden[T any](t *testing.T, name string) []T {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(baselineDir, name))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Articles []T }
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Articles
}

func TestSplitFrontmatter(t *testing.T) {
	for name, c := range map[string]struct{ in, fm, body string }{
		"通常":            {"---\ntitle: a\n---\n本文\n", "\ntitle: a", "本文\n"},
		"本文中の---は触れない":  {"---\ntitle: a\n---\n上\n\n---\n\n下\n---\n", "\ntitle: a", "上\n\n---\n\n下\n---\n"},
		"frontmatterなし": {"本文だけ\n---\nx\n", "", "本文だけ\n---\nx\n"},
		"開始が---xはfmでない": {"---x\ntitle: a\n---\nb", "", "---x\ntitle: a\n---\nb"},
		"CRLF":          {"---\r\ntitle: a\r\n---\r\n本文", "\r\ntitle: a\r", "本文"},
		"閉じが無い":         {"---\ntitle: a\n", "\ntitle: a\n", ""},
		"閉じの直後に空行":      {"---\ntitle: a\n---\n\n本文", "\ntitle: a", "\n本文"},
	} {
		fm, body := SplitFrontmatter(c.in)
		if fm != c.fm || body != c.body {
			t.Errorf("%s: fm=%q body=%q", name, fm, body)
		}
	}
}

func TestParseFrontmatterForms(t *testing.T) {
	fm, err := ParseFrontmatter("\ntitle: \"A: B\"\nimage: '/images/a.webp'\nauthor: yurarara\ndate: 2026年8月13日〜22日\ncategory: Blog\nexcerpt: >-\n  一行目\n  二行目\nsourceId: blog/x\n")
	if err != nil {
		t.Fatal(err)
	}
	if fm.Title != "A: B" || fm.Image != "/images/a.webp" || fm.Date != "2026年8月13日〜22日" || fm.Excerpt != "一行目 二行目" || !fm.Present["excerpt"] {
		t.Errorf("%+v", fm)
	}
	// 引用符つきの日付は文字列のまま（日時型にならない）
	if fm, _ := ParseFrontmatter("date: 2026-10-20\n"); fm.Date != "2026-10-20" {
		t.Errorf("引用符なしの日付: %q", fm.Date)
	}
	if fm, _ := ParseFrontmatter("excerpt: ''\n"); fm.Excerpt != "" || !fm.Present["excerpt"] {
		t.Errorf("空の抜粋: %+v", fm)
	}
	if fm, _ := ParseFrontmatter("title: \"a\\nb\"\n"); fm.Title != "a\nb" {
		t.Errorf("エスケープ: %q", fm.Title)
	}
	for _, bad := range []string{"title: [a, b]\n", "- a\n- b\n", "title: {a: b}\n", "a: b: c: [\n"} {
		if _, err := ParseFrontmatter(bad); err == nil {
			t.Errorf("誤りを受け付けた: %q", bad)
		}
	}
}

// 今のサイトのパーサー（gray-matter）の出力を固めたゴールデンと、7項目×67本・本文の長さまで一致する。
func TestFrontmatterMatchesGrayMatterGolden(t *testing.T) {
	arts, problems := Load(contentDir)
	if len(problems) != 0 || len(arts) != 67 {
		t.Fatalf("読み込み: %d件 %v", len(arts), problems)
	}
	gold := readGolden[fmGolden](t, "frontmatter.json")
	if len(gold) != 67 {
		t.Fatalf("ゴールデン %d件", len(gold))
	}
	byKey := map[string]Article{}
	for _, a := range arts {
		byKey[a.Collection+"/"+a.Slug] = a
	}
	for _, g := range gold {
		a, ok := byKey[g.Collection+"/"+g.Slug]
		if !ok {
			t.Errorf("%s/%s: 読み込めていない", g.Collection, g.Slug)
			continue
		}
		got := map[string]string{"title": a.Title, "image": a.Image, "author": a.Author, "date": a.Date, "category": a.Category, "excerpt": a.Excerpt, "sourceId": a.SourceID}
		for k, want := range g.Fm {
			if want == nil {
				if a.Present[k] {
					t.Errorf("%s/%s: %sが書かれていないはずなのに有る", g.Collection, g.Slug, k)
				}
				continue
			}
			if got[k] != *want || !a.Present[k] {
				t.Errorf("%s/%s: %s = %q（gray-matterは%q）", g.Collection, g.Slug, k, got[k], *want)
			}
		}
		if JSLength(a.Body) != g.BodyLength {
			t.Errorf("%s/%s: 本文の長さ %d（gray-matterは%d）", g.Collection, g.Slug, JSLength(a.Body), g.BodyLength)
		}
	}
}

// 日付: 並び用の開始日が、既存のarticleDateOrderと完全に同じ。kindは1日・範囲・年月を区別する。
func TestDatesMatchGolden(t *testing.T) {
	gold := readGolden[dateGolden](t, "dates.json")
	kinds := map[string]int{}
	for _, g := range gold {
		d := ClassifyDate(g.Label)
		if got := SortEpochMillis(g.Label); got != g.Order {
			t.Errorf("%s: 並び用の日付 %d（articleDateOrderは%d）", g.Slug, got, g.Order)
		}
		if g.Order != 0 && (d.SortDate == nil) || g.Order == 0 && d.SortDate != nil {
			t.Errorf("%s: sort_dateの有無", g.Slug)
		}
		if g.TimeDatetime != nil && d.Kind != "day" { // <time dateTime>が付く記事＝YYYY-MM-DDの1日
			t.Errorf("%s: 1日のはずが%s", g.Slug, d.Kind)
		}
		if d.Kind == "day" && d.Estimated || d.Kind != "day" && d.SortDate != nil && !d.Estimated {
			t.Errorf("%s: estimatedの扱い %+v", g.Slug, d)
		}
		kinds[d.Kind]++
	}
	// 部分日付6種（D12）。表示は原文のまま、種類と開始日を持つ
	for label, want := range map[string]struct{ kind, sort string }{
		"2026年8月1〜2日":    {"range", "2026-08-01"},
		"2026年8月13日〜22日": {"range", "2026-08-13"},
		"2026年7月22日〜24日": {"range", "2026-07-22"},
		"2025年9月〜11月":    {"range", "2025-09-01"},
		"2025年3月〜 6月":    {"range", "2025-03-01"}, // 空白あり
		"2025年12月":       {"month", "2025-12-01"},
		"2025-12-22":     {"day", "2025-12-22"},
		"2026年7月22日":     {"day", "2026-07-22"},
		"":               {"unknown", ""},
		"時期不明":           {"unknown", ""},
	} {
		d := ClassifyDate(label)
		got := ""
		if d.SortDate != nil {
			got = *d.SortDate
		}
		if d.Kind != want.kind || got != want.sort || d.Label != label {
			t.Errorf("%q: %+v（%s・%sのはず）", label, d, want.kind, want.sort)
		}
	}
	if kinds["range"] != 5 || kinds["month"] != 1 || kinds["day"] != 61 || kinds["unknown"] != 0 {
		t.Errorf("種類の内訳: %v", kinds)
	}
}

func TestExtractSourceComment(t *testing.T) {
	c := "<!-- 元URL: https://example.com/blog/x | 取得日: 2026-10-16 -->"
	for name, in := range map[string]string{
		"先頭":          c + "\n\n本文",
		"先頭の空行の後":     "\n\n" + c + "\n本文",
		"CRLF":        c + "\r\n\r\n本文",
		"コメントの後に空行なし": c + "\n本文",
	} {
		rest, url := ExtractSourceComment(in)
		if url != "https://example.com/blog/x" || !strings.HasPrefix(rest, "本文") {
			t.Errorf("%s: rest=%q url=%q", name, rest, url)
		}
	}
	// 形が違う・先頭でない・複数は、外さない（mdcheckがコメントを違反にする）
	for name, in := range map[string]string{
		"形が違う":   "<!-- 元URL: http://example.com | 取得日: 2026-10-16 -->\n本文",
		"日付が違う":  "<!-- 元URL: https://example.com | 取得日: 昨日 -->\n本文",
		"先頭でない":  "本文\n" + c + "\n",
		"別のコメント": "<!-- 内部メモ -->\n本文",
		"コメントなし": "本文だけ",
	} {
		if rest, url := ExtractSourceComment(in); url != "" || rest != in {
			t.Errorf("%s: 外してしまった rest=%q url=%q", name, rest, url)
		}
	}
	// 外すのは最初の1つだけ。2つ目は本文に残り、mdcheckが違反にする
	_, ps := Prepare(Article{Collection: "blog", Slug: "dup", Frontmatter: Frontmatter{Title: "t", Date: "2026-01-01", Category: "Blog", Image: "/images/a.webp", SourceID: "blog/dup"}, Body: c + "\n\n" + c + "\n"})
	if len(ps) == 0 {
		t.Error("2つ目のコメントを許した")
	}
}

// 実データ67本がすべて検査を通り、メタデータに投稿者名が入らない。
func TestPrepareAllLegacyArticles(t *testing.T) {
	arts, _ := Load(contentDir)
	var withComment, emptyExcerpt, authors, orgAuthors int
	for _, a := range arts {
		p, problems := Prepare(a)
		if len(problems) != 0 {
			t.Errorf("%s/%s: %v", a.Collection, a.Slug, problems)
			continue
		}
		if _, ok := p.Metadata["author"]; ok {
			t.Errorf("%s/%s: メタデータに投稿者名が入った（D14: 版に保存しない）", a.Collection, a.Slug)
		}
		if strings.Contains(p.Markdown, "<!--") {
			t.Errorf("%s/%s: コメントが残っている", a.Collection, a.Slug)
		}
		if rest, url := ExtractSourceComment(a.Body); url != "" {
			withComment++
			if p.Metadata["sourceUrl"] != url || p.Markdown != rest {
				t.Errorf("%s/%s: 元URLの取り出し", a.Collection, a.Slug)
			}
		} else if p.Markdown != a.Body {
			t.Errorf("%s/%s: コメントが無いのに本文が変わった", a.Collection, a.Slug)
		}
		if a.Excerpt == "" {
			emptyExcerpt++
			if _, ok := p.Metadata["excerpt"]; ok || p.Summary != "" {
				t.Errorf("%s/%s: 空の抜粋が空のまま保たれない", a.Collection, a.Slug)
			}
		} else if p.Metadata["excerpt"] != a.Excerpt || p.Summary != a.Excerpt {
			t.Errorf("%s/%s: 抜粋", a.Collection, a.Slug)
		}
		if p.AuthorName != "" {
			authors++
		} else {
			orgAuthors++
		}
		if p.Metadata["sourceId"] != a.SourceID || p.Metadata["image"] != a.Image {
			t.Errorf("%s/%s: メタデータ", a.Collection, a.Slug)
		}
	}
	if withComment != 23 || emptyExcerpt != 11 || authors+orgAuthors != 67 || orgAuthors != 7 {
		t.Errorf("コメント付き%d・空の抜粋%d・投稿者あり%d・団体名%d", withComment, emptyExcerpt, authors, orgAuthors)
	}
}

func TestPrepareRejectsViolations(t *testing.T) {
	ok := Article{Collection: "blog", Slug: "ok", Frontmatter: Frontmatter{Title: "題", Date: "2026-01-01", Category: "Blog", Image: "/images/a.webp", SourceID: "blog/ok", Author: "x"}, Body: "本文"}
	if _, ps := Prepare(ok); len(ps) != 0 {
		t.Fatalf("正常な記事が違反: %v", ps)
	}
	mut := func(f func(*Article)) []string { a := ok; f(&a); _, ps := Prepare(a); return ps }
	for name, ps := range map[string][]string{
		"scriptを含む本文": mut(func(a *Article) { a.Body = "<script>alert(1)</script>" }),
		"外部画像":        mut(func(a *Article) { a.Body = "![x](https://tracker.example/p.png)" }),
		"区分が不明":       mut(func(a *Article) { a.Collection = "page" }),
		"slugの形式":     mut(func(a *Article) { a.Slug = "a b" }),
		"titleなし":     mut(func(a *Article) { a.Title = "" }),
		"dateなし":      mut(func(a *Article) { a.Date = "" }),
		"categoryが不正": mut(func(a *Article) { a.Category = "Other" }),
		"imageが外部":    mut(func(a *Article) { a.Image = "https://example.com/a.png" }),
		"imageのパス移動":  mut(func(a *Article) { a.Image = "/images/../x.png" }),
		"sourceIdなし":  mut(func(a *Article) { a.SourceID = "" }),
		"抜粋が長すぎる":     mut(func(a *Article) { a.Excerpt = strings.Repeat("あ", 301) }),
		"投稿者名に制御文字":   mut(func(a *Article) { a.Author = "a\nb" }),
		"投稿者名が長い":     mut(func(a *Article) { a.Author = strings.Repeat("a", 65) }),
	} {
		if len(ps) == 0 {
			t.Errorf("%s: 違反として検出されない", name)
		}
	}
	// 団体名・未設定は投稿者なし（NULL）。それ以外は投稿者として別管理（メタデータには入らない）
	for author, want := range map[string]string{"nu_digital": "", "": "", "  ": "", "yurarara": "yurarara"} {
		p, _ := Prepare(func() Article { a := ok; a.Author = author; return a }())
		if p.AuthorName != want {
			t.Errorf("author=%q → %q", author, p.AuthorName)
		}
	}
}
