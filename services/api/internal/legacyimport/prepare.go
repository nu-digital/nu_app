package legacyimport

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/nu-digital/nu_app/services/api/internal/cmsrules"
	"github.com/nu-digital/nu_app/services/api/internal/mdcheck"
)

// 本文の最初の空でない1行が、この形に**完全一致**するときだけ、元URLのコメントとして取り出す（それ以外のコメントはmdcheckが拒否する）。
var sourceCommentRe = regexp.MustCompile(`^<!-- 元URL: (https://[^\s|]+) \| 取得日: \d{4}-\d{2}-\d{2} -->$`)

// DefaultAuthor は団体名。frontmatterのauthorがこれ（または空）なら投稿者は未設定（NULL）にする＝公開APIは既定の同じ名前を返す。
const DefaultAuthor = "nu_digital"

// Prepared は、検査を通った、DBへ書く直前の記事。
type Prepared struct {
	Article
	Section, Type string
	Summary       string
	Markdown      string
	Metadata      map[string]string
	Date          DateInfo
	// AuthorName は移行元の投稿者名（団体名・未設定なら空）。**版・メタデータには入れない**（D14）。legacy_authorsで管理する。
	AuthorName string
}

// ExtractSourceComment は、先頭の元URLコメントを本文から外し、URLを返す。外した場合はコメント行と、その直後の空行を取り除く。
func ExtractSourceComment(body string) (rest, url string) {
	lines := strings.SplitAfter(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		m := sourceCommentRe.FindStringSubmatch(strings.TrimRight(l, "\r\n"))
		if m == nil {
			return body, ""
		}
		return strings.TrimLeft(strings.Join(lines[i+1:], ""), "\r\n"), m[1]
	}
	return body, ""
}

// Prepare は1本を検査して整える。違反はすべて返す（APIと同じ検査規則を使う）。
func Prepare(a Article) (Prepared, []string) {
	var problems []string
	add := func(msg string) { problems = append(problems, fmt.Sprintf("%s/%s: %s", a.Collection, a.Slug, msg)) }
	p := Prepared{Article: a, Metadata: map[string]string{}}
	switch a.Collection {
	case "blog", "news":
		p.Type, p.Section = "article", a.Collection
	case "project":
		p.Type, p.Section = "project", "project"
	default:
		add("区分が不明です")
	}
	if !cmsrules.SlugRe.MatchString(a.Slug) {
		add("slug（ファイル名）が形式に合いません")
	}
	body, srcURL := ExtractSourceComment(a.Body)
	p.Markdown = body
	title, summary, err := cmsrules.CheckText(a.Title, a.Excerpt, body, mdcheck.MaxBytes)
	if err != nil {
		add(err.Error())
	}
	p.Title, p.Summary = title, summary // Articleに埋め込んだFrontmatterのTitleを検査後の値で上書き
	if strings.TrimSpace(a.Date) == "" {
		add("dateがありません")
	}
	p.Date = ClassifyDate(a.Date)
	sd := p.Date.SortDate
	if err := cmsrules.CheckDate(&cmsrules.Date{Label: p.Date.Label, Kind: p.Date.Kind, SortDate: sd}); err != nil {
		add(err.Error())
	}
	// メタデータ（投稿者名は入れない）
	if a.Category != "Blog" && a.Category != "Product" {
		add("categoryはBlogかProductです")
	} else {
		p.Metadata["category"] = a.Category
	}
	if !strings.HasPrefix(a.Image, "/images/") {
		add("imageが/images/から始まりません")
	} else if err := cmsrules.ValidMetadataValue("image", a.Image); err != nil {
		add(err.Error())
	} else {
		p.Metadata["image"] = a.Image
	}
	if a.Excerpt != "" { // 空の抜粋はメタデータに入れない（summaryに空のまま持つ。サイトは空のまま表示する）
		if err := cmsrules.ValidMetadataValue("excerpt", a.Excerpt); err != nil {
			add(err.Error())
		} else {
			p.Metadata["excerpt"] = a.Excerpt
		}
	}
	if a.SourceID == "" {
		add("sourceIdがありません")
	} else if err := cmsrules.ValidMetadataValue("sourceId", a.SourceID); err != nil {
		add(err.Error())
	} else {
		p.Metadata["sourceId"] = a.SourceID
	}
	if srcURL != "" {
		if err := cmsrules.ValidMetadataValue("sourceUrl", srcURL); err != nil {
			add(err.Error())
		} else {
			p.Metadata["sourceUrl"] = srcURL
		}
	}
	// 投稿者: 団体名・未設定ならNULL（既定の表示と同じ）。それ以外は、移行元の投稿者として別に管理する（公開可否は未確認から始まる）
	name := strings.TrimSpace(a.Author)
	if name != "" && name != DefaultAuthor {
		if len([]rune(name)) > 64 || cmsrules.HasControl(name, false) {
			add("authorが長すぎる、または制御文字を含みます")
		} else {
			p.AuthorName = name
		}
	}
	// 本文の検査（元URLコメントを外した後。許可リストに合わなければ移行しない＝本文を勝手に書き換えない）
	for i, v := range mdcheck.Validate(body) {
		if i == 3 {
			add(fmt.Sprintf("本文にほか%d件の違反があります", len(mdcheck.Validate(body))-3))
			break
		}
		add("本文: " + v.String())
	}
	return p, problems
}
