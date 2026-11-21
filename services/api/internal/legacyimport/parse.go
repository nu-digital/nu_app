// Package legacyimport は、既存サイト（apps/web/content）の記事をCMSへ移行する（nuctl import-legacy。docs/design/p3-web.md §2）。
//
// 入力の検査は全件・書き込みの前に行い、1本でも違反があれば何も書かない。frontmatterは yaml.v3 で読み、
// 今のサイトと同じパーサー（gray-matter）の出力を固めたゴールデン（docs/legacy-baseline/frontmatter.json）と照合する。
package legacyimport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"gopkg.in/yaml.v3"
)

// Frontmatter は記事のfrontmatterのうち移行で使う項目。値は**タグを解釈しない文字列**
// （`2026-10-20`のような値が日時型にならない）。Presentは、そのキーが書かれていたか。
type Frontmatter struct {
	Title, Image, Author, Date, Category, Excerpt, SourceID string
	Present                                                 map[string]bool
}

// Article は読み込んだ1本の記事。
type Article struct {
	Collection string // blog / news / project
	Slug       string
	File       string // 相対パス（content/blog/xxx.md）
	FileSHA256 string
	Frontmatter
	Body string // frontmatterを除いた本文（gray-matterの`content`と同じ）
}

var fmKeys = []string{"title", "image", "author", "date", "category", "excerpt", "sourceId"}

// SplitFrontmatter は、先頭の`---`行から次の`---`までをfrontmatter、その後を本文として返す（gray-matterと同じ規則）。
// 本文中の`---`（水平線・見出しの下線）には触れない。frontmatterが無ければ空のfrontmatterと全体を本文として返す。
func SplitFrontmatter(src string) (fm string, body string) {
	if !strings.HasPrefix(src, "---") {
		return "", src
	}
	rest := src[3:]
	// 開始の`---`は行全体（後ろに文字が続く`---x`はfrontmatterではない）
	if rest != "" && rest[0] != '\n' && rest[0] != '\r' {
		return "", src
	}
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return rest, ""
	}
	fm = rest[:idx]
	body = rest[idx+len("\n---"):]
	if strings.HasPrefix(body, "\r") {
		body = body[1:]
	}
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return fm, body
}

// ParseFrontmatter はYAMLのfrontmatterから移行で使う項目を取り出す。スカラー以外・未知の型は誤りにする。
func ParseFrontmatter(fm string) (Frontmatter, error) {
	out := Frontmatter{Present: map[string]bool{}}
	if strings.TrimSpace(fm) == "" {
		return out, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return out, fmt.Errorf("frontmatterを読めません")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return out, errors.New("frontmatterがキーと値の形ではありません")
	}
	m := doc.Content[0]
	vals := map[string]string{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if v.Kind != yaml.ScalarNode {
			return out, fmt.Errorf("frontmatterの%qがスカラーではありません", k.Value)
		}
		vals[k.Value] = v.Value // タグを解釈せず、書かれた文字列（引用符・折りたたみ・エスケープは解決済み）
		out.Present[k.Value] = true
	}
	out.Title, out.Image, out.Author, out.Date = vals["title"], vals["image"], vals["author"], vals["date"]
	out.Category, out.Excerpt, out.SourceID = vals["category"], vals["excerpt"], vals["sourceId"]
	return out, nil
}

// JSLength はJavaScriptの`String.length`（UTF-16コード単位の数）。ゴールデンの本文の長さとの照合に使う。
func JSLength(s string) int { return len(utf16.Encode([]rune(s))) }

// ParseArticle は1ファイルを読む。collection・slugはパスから決める（slug＝ファイル名。大文字・ドットを保つ）。
func ParseArticle(contentDir, collection, name string) (Article, error) {
	path := filepath.Join(contentDir, collection, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return Article{}, fmt.Errorf("%s/%sを読めません", collection, name)
	}
	sum := sha256.Sum256(raw)
	fmText, body := SplitFrontmatter(string(raw))
	fm, err := ParseFrontmatter(fmText)
	if err != nil {
		return Article{}, fmt.Errorf("%s/%s: %v", collection, name, err)
	}
	return Article{
		Collection: collection, Slug: strings.TrimSuffix(name, ".md"), File: "content/" + collection + "/" + name,
		FileSHA256: hex.EncodeToString(sum[:]), Frontmatter: fm, Body: body,
	}, nil
}

// Load はcontent/{blog,news,project}の全記事を、区分・slug順に読む。
func Load(contentDir string) ([]Article, []string) {
	var arts []Article
	var problems []string
	for _, c := range []string{"blog", "news", "project"} {
		entries, err := os.ReadDir(filepath.Join(contentDir, c))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%sのディレクトリを読めません", c))
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			a, err := ParseArticle(contentDir, c, e.Name())
			if err != nil {
				problems = append(problems, err.Error())
				continue
			}
			arts = append(arts, a)
		}
	}
	sort.Slice(arts, func(i, j int) bool {
		if arts[i].Collection != arts[j].Collection {
			return arts[i].Collection < arts[j].Collection
		}
		return arts[i].Slug < arts[j].Slug
	})
	return arts, problems
}
