// Package mdcheck は、CMSで保存するMarkdownが許可リストの範囲内かを検査する（docs/design/p3-cms.md §5）。
//
// 役割: サーバー側の**入口の検査**（方針に反する入力を422で返す）。本当の防壁は公開サイト側の描画時サニタイズ
// （rehype-sanitize。同じ許可リストと、testdata/corpus.json の事例集をGo・JS双方のテストで回す）。
//
// 方式: goldmarkでASTを作り、HTMLノードを golang.org/x/net/html のトークナイザーで検査する（正規表現でHTMLを検査しない）。
// リンク・画像・autolink・参照定義の結果は、すべてLink/ImageノードとしてAST上で同じ検査を通す。
package mdcheck

import (
	"fmt"
	"html"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	nethtml "golang.org/x/net/html"
)

// MaxBytes は本文の上限（256KiB）。
const MaxBytes = 256 << 10

const (
	maxDepth      = 16
	maxViolations = 10
)

// Violation は許可リストに反する箇所。MessageはCMSの利用者に見せてよい日本語で、本文の内容は含めない。
type Violation struct {
	Line    int // 1始まり。分からなければ0
	Message string
}

func (v Violation) String() string {
	if v.Line > 0 {
		return fmt.Sprintf("%d行目: %s", v.Line, v.Message)
	}
	return v.Message
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Validate は検査し、違反の一覧を返す（空なら許可）。
func Validate(src string) []Violation {
	if len(src) > MaxBytes {
		return []Violation{{Message: "本文が大きすぎます（256KiBまで）"}}
	}
	b := []byte(src)
	c := &checker{src: b}
	doc := md.Parser().Parse(text.NewReader(b))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.HTMLBlock:
			var sb strings.Builder
			for i := 0; i < v.Lines().Len(); i++ {
				seg := v.Lines().At(i)
				sb.Write(seg.Value(b))
			}
			if v.HasClosure() {
				sb.Write(v.ClosureLine.Value(b))
			}
			c.html(sb.String(), c.lineOf(v.Lines().At(0).Start))
		case *ast.RawHTML:
			var sb strings.Builder
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				sb.Write(seg.Value(b))
			}
			line := 0
			if v.Segments.Len() > 0 {
				line = c.lineOf(v.Segments.At(0).Start)
			}
			c.html(sb.String(), line)
		case *ast.Link:
			c.link(string(v.Destination), false, c.nodeLine(n))
		case *ast.Image:
			c.link(string(v.Destination), true, c.nodeLine(n))
		case *ast.AutoLink:
			c.link(string(v.URL(b)), false, c.nodeLine(n))
		}
		return ast.WalkContinue, nil
	})
	if len(c.stack) > 0 && len(c.out) < maxViolations {
		c.add(0, "閉じられていないHTMLタグがあります")
	}
	return c.out
}

type checker struct {
	src   []byte
	out   []Violation
	stack []string
}

func (c *checker) add(line int, msg string) {
	if len(c.out) < maxViolations {
		c.out = append(c.out, Violation{Line: line, Message: msg})
	}
}

func (c *checker) lineOf(off int) int {
	if off < 0 || off > len(c.src) {
		return 0
	}
	return strings.Count(string(c.src[:off]), "\n") + 1
}

func (c *checker) nodeLine(n ast.Node) int {
	for p := n; p != nil; p = p.Parent() {
		if p.Type() == ast.TypeBlock && p.Lines() != nil && p.Lines().Len() > 0 {
			return c.lineOf(p.Lines().At(0).Start)
		}
	}
	return 0
}

// html は1つのHTML断片（HTMLブロックまたはインラインHTML）を検査し、開閉の対応を文書全体で追跡する。
func (c *checker) html(frag string, line int) {
	z := nethtml.NewTokenizer(strings.NewReader(frag))
	var pendingEmpty string // 子なしのdata属性付きdivを開いた直後。同じ断片内で閉じる必要がある
	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			if z.Err() != io.EOF {
				c.add(line, "HTMLを解釈できません")
			}
			break
		}
		switch tt {
		case nethtml.CommentToken:
			c.add(line, "HTMLコメントは使えません")
		case nethtml.DoctypeToken:
			c.add(line, "このHTMLは使えません")
		case nethtml.TextToken:
			if pendingEmpty != "" && strings.TrimSpace(string(z.Text())) != "" {
				c.add(line, "<div data-…>の中には何も書けません")
			}
		case nethtml.SelfClosingTagToken:
			name, _ := z.TagName()
			c.add(line, fmt.Sprintf("<%s />の形式は使えません", strings.ToLower(string(name))))
		case nethtml.StartTagToken:
			pendingEmpty = c.start(z, line, pendingEmpty)
		case nethtml.EndTagToken:
			name, _ := z.TagName()
			n := strings.ToLower(string(name))
			if pendingEmpty != "" && n == "div" {
				pendingEmpty = ""
			}
			c.end(n, line)
		}
	}
	if pendingEmpty != "" {
		c.add(line, "<div data-…>は同じ行で閉じてください")
		c.stack = c.stack[:len(c.stack)-1]
	}
}

var allowedTags = map[string]bool{"details": true, "summary": true, "div": true}

// 開始タグの生テキストが取りうる形。トークナイザーは重複した属性を黙って捨てる（最初の値だけ残る）ため、
// 検査が見ていない属性が無いことを、生テキストの形で確かめる。
var (
	plainTagRaw = regexp.MustCompile(`^<(details|summary|div)>$`)
	dataDivRaw  = regexp.MustCompile(`^<div data-(youtube|video|carousel)="[^"<>&]*">$`)
)

func (c *checker) start(z *nethtml.Tokenizer, line int, pending string) string {
	raw := strings.ToLower(string(z.Raw())) // Next以外を呼ぶ前に取り出す
	nameB, hasAttr := z.TagName()
	name := strings.ToLower(string(nameB))
	if pending != "" {
		c.add(line, "<div data-…>の中には何も書けません")
	}
	var attrs [][2]string
	for hasAttr {
		var k, v []byte
		k, v, hasAttr = z.TagAttr()
		attrs = append(attrs, [2]string{strings.ToLower(string(k)), string(v)}) // 値はエンティティ解読済み
	}
	if !allowedTags[name] {
		c.add(line, fmt.Sprintf("<%s>は使えません（許可: details・summary・div）", name))
		return ""
	}
	empty := false
	switch {
	case len(attrs) == 0 && plainTagRaw.MatchString(raw):
	case name == "div" && len(attrs) == 1 && dataDivOK(attrs[0][0], attrs[0][1]) && dataDivRaw.MatchString(raw):
		empty = true
	default:
		c.add(line, fmt.Sprintf("<%s>に使えない属性、または使えない書き方があります", name))
	}
	if len(c.stack) >= maxDepth {
		c.add(line, "HTMLの入れ子が深すぎます")
		return ""
	}
	c.stack = append(c.stack, name)
	if empty {
		return name
	}
	return ""
}

func dataDivOK(k, v string) bool {
	switch k {
	case "data-youtube":
		return IsYouTubeID(v)
	case "data-video":
		return v == "featured"
	case "data-carousel":
		return v == "overview"
	}
	return false
}

// IsYouTubeID は11桁の動画ID（英数字・-・_）か。
func IsYouTubeID(s string) bool {
	if len(s) != 11 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (c *checker) end(name string, line int) {
	if !allowedTags[name] {
		c.add(line, fmt.Sprintf("</%s>は使えません", name))
		return
	}
	if len(c.stack) == 0 || c.stack[len(c.stack)-1] != name {
		c.add(line, fmt.Sprintf("</%s>に対応する開始タグがありません", name))
		return
	}
	c.stack = c.stack[:len(c.stack)-1]
}

// normalizeURL は、ブラウザが解釈する前の姿に近づける: エンティティを解読し、空白・制御文字を除き、小文字にする。
func normalizeURL(raw string) string {
	s := raw
	for i := 0; i < 3; i++ { // 二重のエンティティも解読
		u := html.UnescapeString(s)
		if u == s {
			break
		}
		s = u
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '\u200b' || r == '\u2060' || r == '\ufeff' {
			return -1
		}
		return r
	}, s)
	return s
}

func (c *checker) link(raw string, image bool, line int) {
	s := normalizeURL(raw)
	if s == "" {
		return // 空のリンク先は無害（何も指さない）
	}
	// %エスケープを解いた形でもスキームを判定する（%6Aavascript: のような回避）
	dec, err := url.PathUnescape(s)
	if err != nil {
		dec = s
	}
	if bad := scheme(dec); bad != "" && !(bad == "http" || bad == "https" || bad == "mailto") {
		c.add(line, "このリンク先は使えません（http・https・mailtoのみ）")
		return
	}
	if image {
		if !(strings.HasPrefix(s, "/images/") || strings.HasPrefix(s, "/api/v1/public/media/")) || strings.Contains(dec, "..") || strings.Contains(dec, `\`) {
			c.add(line, "画像はこのサイトにアップロードしたものだけ使えます")
		}
		return
	}
	switch sc := scheme(s); {
	case sc == "http" || sc == "https":
	case sc == "mailto":
	case sc != "":
		c.add(line, "このリンク先は使えません（http・https・mailtoのみ）")
	case strings.HasPrefix(s, "#"):
	case strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "/\\"):
	default:
		c.add(line, "リンクはhttp(s)のURLか、/で始まるこのサイト内のパスにしてください")
	}
}

// scheme はURLのスキームを返す（スキームが無ければ空）。「/」「?」「#」より前の「:」までをスキームとみなす。
func scheme(s string) string {
	for i, r := range s {
		switch {
		case r == ':':
			if i == 0 {
				return ":"
			}
			return strings.ToLower(s[:i])
		case r == '/' || r == '?' || r == '#':
			return ""
		}
	}
	return ""
}
