package mdcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type corpusCase struct {
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
	OK       bool   `json:"ok"`
}

// 許可・拒否の事例集。公開サイトの描画側サニタイズ（rehype-sanitize）のテストも同じファイルを使う（P3-04）。
func TestCorpus(t *testing.T) {
	b, err := os.ReadFile("testdata/corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []corpusCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 60 {
		t.Fatalf("事例集が少なすぎる: %d", len(cases))
	}
	for _, c := range cases {
		got := Validate(c.Markdown)
		if c.OK && len(got) != 0 {
			t.Errorf("【許可のはず】%s: %v", c.Name, got)
		}
		if !c.OK && len(got) == 0 {
			t.Errorf("【拒否のはず】%s", c.Name)
		}
		for _, v := range got { // 違反の文言に本文の内容（攻撃文字列）を含めない
			if strings.Contains(v.Message, "alert(") || strings.Contains(v.Message, "evil") {
				t.Errorf("%s: 文言に入力の内容が出ている: %q", c.Name, v.Message)
			}
		}
	}
}

func TestSizeLimit(t *testing.T) {
	if v := Validate(strings.Repeat("あ", MaxBytes/3+10)); len(v) != 1 || !strings.Contains(v[0].Message, "大きすぎます") {
		t.Errorf("上限超過: %v", v)
	}
	if v := Validate(strings.Repeat("a", MaxBytes)); len(v) != 0 {
		t.Errorf("上限ちょうどは許可: %v", v)
	}
}

func TestDepthLimit(t *testing.T) {
	deep := strings.Repeat("<div>\n\n", 20) + "x\n\n" + strings.Repeat("</div>\n\n", 20)
	if v := Validate(deep); len(v) == 0 {
		t.Error("深すぎる入れ子を許可した")
	}
	ok := strings.Repeat("<div>\n\n", 5) + "x\n\n" + strings.Repeat("</div>\n\n", 5)
	if v := Validate(ok); len(v) != 0 {
		t.Errorf("浅い入れ子は許可: %v", v)
	}
}

func TestViolationLinesAndLimit(t *testing.T) {
	v := Validate("a\n\nb\n\n<script>x</script>\n")
	if len(v) == 0 || v[0].Line != 5 {
		t.Errorf("行番号: %+v", v)
	}
	many := strings.Repeat("[x](javascript:a)\n\n", 50)
	if got := Validate(many); len(got) != maxViolations {
		t.Errorf("違反の報告は%d件まで: %d", maxViolations, len(got))
	}
}

func TestIsYouTubeID(t *testing.T) {
	for id, want := range map[string]bool{"ZEguS-om_Ec": true, "abcdefghijk": true, "short": false, "abcdefghijkl": false, "abc def ghi": false, "abcdefghij\"": false, "": false} {
		if IsYouTubeID(id) != want {
			t.Errorf("%q", id)
		}
	}
}

var leadingComment = regexp.MustCompile(`(?m)^<!-- 元URL:[^\n]*-->[ \t]*\n?`)

// 既存の67記事がすべて許可リストを通る（移行時に「元URL」コメントだけを取り出して本文から外す前提）。
// コメント付きのまま新規保存すると拒否されることも確かめる（コメントは内部メモ等が公開APIの本文に出るため）。
func TestLegacyArticlesPass(t *testing.T) {
	files, _ := filepath.Glob("../../../../apps/web/content/*/*.md")
	if len(files) != 67 {
		t.Fatalf("既存記事が67本でない: %d（baselineと照合すること）", len(files))
	}
	withComment := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		if strings.HasPrefix(body, "---\n") {
			if i := strings.Index(body[4:], "\n---\n"); i >= 0 {
				body = body[4+i+5:]
			}
		}
		if leadingComment.MatchString(body) {
			withComment++
			if v := Validate(body); len(v) == 0 {
				t.Errorf("%s: コメント付きが通った", f)
			}
			body = leadingComment.ReplaceAllString(body, "")
		}
		if v := Validate(body); len(v) != 0 {
			t.Errorf("%s: %v", filepath.Base(f), v)
		}
	}
	if withComment != 23 {
		t.Errorf("元URLコメントのある記事 %d本（23本のはず）", withComment)
	}
}
