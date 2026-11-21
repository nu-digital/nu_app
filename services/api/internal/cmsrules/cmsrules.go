// Package cmsrules は、CMSの記事・作品の内容の検査規則（長さ・形式・メタデータ・日付）を持つ。
// APIの保存（services/api/internal/api）と、既存記事の移行（nuctl import-legacy）が**同じ規則**を使うための共通部分
// （移行だけが緩い・厳しい規則を持たないようにする。docs/design/p3-web.md §2）。
package cmsrules

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Error は規則違反。Messageは利用者に見せてよい日本語で、入力の内容を含めない。
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

func bad(msg string) error { return &Error{msg} }

var (
	// SlugRe はURL用の名前。大文字・ドットを保つ（既存slugは [\w.-]+）。作成後は変えない。
	SlugRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	imagePathRe = regexp.MustCompile(`^/images/[A-Za-z0-9._/-]{1,200}$`)
	sourceIDRe  = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)
	sourceURLRe = regexp.MustCompile(`^https://[^\s<>"]{1,480}$`)
)

// HasControl は制御文字（改行・タブ・U+2028/2029を含む）を含むか。allowNewlineなら改行（\n）だけは許す。
func HasControl(s string, allowNewline bool) bool {
	for _, r := range s {
		if r == '\n' && allowNewline {
			continue
		}
		if r < 0x20 || r == 0x7f || r == ' ' || r == ' ' {
			return true
		}
	}
	return false
}

// Date は版の日付（D12）。Labelは表示用の原文、Kindはday/month/range/unknown、SortDateは並び替え用（YYYY-MM-DD）。
type Date struct {
	Label    string
	Kind     string
	SortDate *string
}

// CheckText はタイトル・要約・本文の長さを検査し、前後の空白を除いたタイトルと要約を返す。本文のMarkdownの中身は別（mdcheck）。
func CheckText(title, summary, markdown string, maxMarkdownBytes int) (string, string, error) {
	title, summary = strings.TrimSpace(title), strings.TrimSpace(summary)
	if title == "" || utf8.RuneCountInString(title) > 200 || HasControl(title, false) {
		return "", "", bad("タイトルは1〜200文字で、改行を含めないでください")
	}
	if utf8.RuneCountInString(summary) > 500 || HasControl(summary, true) {
		return "", "", bad("要約は500文字までです")
	}
	if len(markdown) > maxMarkdownBytes {
		return "", "", bad("本文が大きすぎます（256KiBまで）")
	}
	return title, summary, nil
}

// CheckDate は日付を検査し、表示用の原文の前後の空白を除いて返す（nilならそのまま）。
func CheckDate(d *Date) error {
	if d == nil {
		return nil
	}
	d.Label = strings.TrimSpace(d.Label)
	if utf8.RuneCountInString(d.Label) > 100 || HasControl(d.Label, false) {
		return bad("日付の表示は100文字までです")
	}
	switch d.Kind {
	case "day", "month", "range", "unknown":
	default:
		return bad("日付の種類が正しくありません")
	}
	if d.SortDate != nil {
		if _, err := time.Parse("2006-01-02", *d.SortDate); err != nil {
			return bad("並び替え用の日付は YYYY-MM-DD で指定してください")
		}
	}
	if d.Kind != "unknown" && d.SortDate == nil {
		return bad("日付の種類がunknown以外のときは、並び替え用の日付が必要です")
	}
	return nil
}

// ValidMetadataValue はメタデータの1項目を検査する（category・excerpt・image・sourceId・sourceUrl。
// 投稿者名はメタデータに含めない＝D14）。
func ValidMetadataValue(key, v string) error {
	switch key {
	case "category":
		if v == "" || utf8.RuneCountInString(v) > 30 || HasControl(v, false) {
			return bad("カテゴリは1〜30文字で入力してください")
		}
	case "excerpt":
		if utf8.RuneCountInString(v) > 300 || HasControl(v, true) {
			return bad("抜粋は300文字までです")
		}
	case "image":
		if !imagePathRe.MatchString(v) || strings.Contains(v, "..") {
			return bad("画像はこのサイトの /images/ のパスだけ指定できます")
		}
	case "sourceId":
		if !sourceIDRe.MatchString(v) {
			return bad("sourceIdの形式が正しくありません")
		}
	case "sourceUrl":
		if !sourceURLRe.MatchString(v) {
			return bad("sourceUrlはhttpsのURLにしてください")
		}
	}
	return nil
}
