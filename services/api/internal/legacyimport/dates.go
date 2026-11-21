package legacyimport

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// 既存サイトの formatArticleDate・articleDateOrder（apps/web/lib/content-presentation.mjs）と同じ規則。
var (
	dayRangeRe = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日?\s*[〜～~–]\s*(\d{1,2})日$`)
	fullRe     = regexp.MustCompile(`^(\d{4})[-./年](\d{1,2})[-./月](\d{1,2})日?$`)
	monthRe    = regexp.MustCompile(`^(\d{4})[-./年](\d{1,2})月?$`)
	orderRe    = regexp.MustCompile(`^(\d{4})[年./-](\d{1,2})(?:[月./-](\d{1,2}))?`)
	rangeSepRe = regexp.MustCompile(`[〜～~–]`)
)

// DateInfo は移行する日付（D12）。Labelは表示用の原文。SortDateは並び替え用の開始日（`articleDateOrder`と同じ規則）。
type DateInfo struct {
	Label     string
	Kind      string  // day / month / range / unknown
	SortDate  *string // YYYY-MM-DD。無ければnil
	Estimated bool    // 補った並び替え用の日（day以外）。開催日として表示しない
}

// SortEpochMillis は`articleDateOrder`の戻り値（UTCのミリ秒。日付が読めなければ0）と同じ値。
func SortEpochMillis(label string) int64 {
	d := sortDateOf(label)
	if d == nil {
		return 0
	}
	t, _ := time.Parse("2006-01-02", *d)
	return t.UnixMilli()
}

func sortDateOf(label string) *string {
	m := orderRe.FindStringSubmatch(label)
	if m == nil {
		return nil
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d := 1
	if m[3] != "" {
		d, _ = strconv.Atoi(m[3])
	}
	// JavaScriptのDate.UTCと同じく、範囲外の月日は繰り上げる（time.Dateも同じ）
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	s := fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
	return &s
}

// ClassifyDate は原文の日付から kind・sort_date・estimated を決める。表示は常に原文（date_label）。
func ClassifyDate(label string) DateInfo {
	label = trimSpace(label)
	sd := sortDateOf(label)
	d := DateInfo{Label: label, Kind: "unknown", SortDate: sd}
	switch {
	case fullRe.MatchString(label):
		d.Kind = "day"
	case dayRangeRe.MatchString(label):
		d.Kind = "range"
	case monthRe.MatchString(label):
		d.Kind = "month"
	case sd != nil && rangeSepRe.MatchString(label):
		d.Kind = "range" // 「2025年9月〜11月」「2025年3月〜 6月」（空白あり）など
	}
	d.Estimated = d.Kind != "day" && sd != nil
	return d
}
