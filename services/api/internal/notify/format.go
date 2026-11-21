package notify

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type payload struct {
	Version   int    `json:"v"`
	ItemName  string `json:"itemName"`
	AssetTag  string `json:"assetTag"`
	Quantity  int    `json:"quantity"`
	Borrower  string `json:"borrower"`
	Actor     string `json:"actor"`
	Proxy     bool   `json:"proxy"`
	Remaining *int64 `json:"remaining"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Delta     int    `json:"delta"`
}

// clean は利用者が入力した文字列（表示名・物品名）を1行の表示用に整える。
// 改行・制御文字を空白にし、長さを制限し、メンション・チャンネル参照の記号を全角にして無効化する（allowed_mentionsとの二重の防御）。
func clean(s string, max int) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '@':
			b.WriteRune('＠')
		case r == '<':
			b.WriteRune('＜')
		case r == '>':
			b.WriteRune('＞')
		case unicode.IsControl(r) || r == ' ' || r == ' ':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > max {
		out = string([]rune(out)[:max]) + "…"
	}
	return out
}

// Format はoutboxの行からDiscordの本文を作る。本文の最後に通知ID（outboxのID）を入れ、重複投稿を調べられるようにする。
func Format(eventType, id string, payloadJSON []byte) (string, error) {
	var p payload
	if err := json.Unmarshal(payloadJSON, &p); err != nil {
		return "", fmt.Errorf("通知の内容を読めません")
	}
	item := "「" + clean(p.ItemName, 100) + "」"
	if p.AssetTag != "" {
		item += "（" + clean(p.AssetTag, 50) + "）"
	}
	var msg string
	switch eventType {
	case "inventory.loan_created":
		msg = fmt.Sprintf("【貸出】%sさんが%sを%s借りました", clean(p.Borrower, 64), item, count(p))
		if p.Remaining != nil {
			msg += fmt.Sprintf("（残り%d）", *p.Remaining)
		}
	case "inventory.loan_returned":
		msg = fmt.Sprintf("【返却】%sさんが%sを%s返却しました", clean(p.Actor, 64), item, count(p))
		if p.Proxy {
			msg += fmt.Sprintf("（借りた人: %sさん）", clean(p.Borrower, 64))
		}
	case "inventory.correction_created":
		target := "貸出"
		if p.Target == "return" {
			target = "返却"
		}
		what := "記録を取り消しました"
		if p.Kind == "adjust" {
			what = fmt.Sprintf("数量を%+d訂正しました", p.Delta)
		}
		msg = fmt.Sprintf("【訂正】%sさんが%sの%sの%s", clean(p.Actor, 64), item, target, what)
	case "content.published":
		kind := "記事"
		if p.Target == "project" {
			kind = "作品"
		}
		msg = fmt.Sprintf("【公開】%s「%s」を公開しました", kind, clean(p.ItemName, 200))
	default:
		return "", fmt.Errorf("未対応の通知種別です")
	}
	return msg + "\n通知ID: " + id, nil
}

func count(p payload) string {
	if p.AssetTag != "" {
		return ""
	}
	return fmt.Sprintf("%d個", p.Quantity)
}
