// Package redact は、ノート・要約・日誌・検索語に入れてはいけない個人情報の形（メールアドレス・電話番号・長い数字＝学籍番号など・パスワード表現）の検出と伏せ字。
// 全角を半角にそろえ（NFKC）てから調べる（全角数字・全角＠・全角ハイフンのすり抜けを防ぐ）。
// 完全な検出ではない（氏名などは判定できない）。「入れさせない」ための安全網で、プロンプトの指示と併用する。
package redact

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const Mask = "［伏せ字］"

var (
	// 正規化後の本文にそのまま当てるもの
	direct = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+\.[A-Za-z0-9.-]+` + // メールアドレス
		`|\d{2,4}[-ー－―\s.]\d{2,4}[-ー－―\s.]\d{3,4}` + // 区切りつきの電話番号
		`|\d{7,}` + // 長い数字（学籍番号・電話番号など）
		`|[A-Za-z]{1,3}\d{5,}` + // 英字つきの学籍番号
		`|(?i:パスワード|password|passwd)\s*[:：=は]`)
	// 指示めいた語を含む題は捨てる（Webの内容に由来する題が、システムメッセージで指示として働かないように）
	instructionLike = regexp.MustCompile(`(?i)指示|無視|命令|システム|プロンプト|ロール|ignore|instruction|system|prompt|assistant|override|jailbreak`)
	separators      = regexp.MustCompile(`[\s\-ー－―.]`)
	longDigits      = regexp.MustCompile(`\d{9,}`)
)

// Has は、個人情報の形を含むか。区切りを除いた数字の並び（「090 1234 5678」など）も調べる。
func Has(s string) bool {
	n := norm.NFKC.String(s)
	if direct.MatchString(n) {
		return true
	}
	return longDigits.MatchString(separators.ReplaceAllString(n, ""))
}

// Redact は、個人情報の形を伏せ字にする（全角は半角にそろえた文字列を返す）。区切りつきの数字の並びは伏せ字にしきれないので、必要なら Has で弾く。
func Redact(s string) string {
	return direct.ReplaceAllString(norm.NFKC.String(s), Mask)
}

// Squeeze は、改行・連続する空白を1つにそろえる（1行の題などに使う）。
func Squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }

// Topic は、題・ハマりのような短い語句を整える。文字・数字・空白・「・ー-」だけを残し（記号・改行・命令めいた装飾を除く）、
// 個人情報の形を含むなら空にし、max文字に切る。Webの内容に由来する題を、システムメッセージに入れる前に通す。
func Topic(s string, max int) string {
	if Has(s) || instructionLike.MatchString(norm.NFKC.String(s)) {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), r == '・', r == 'ー', r == '-':
			return r
		case unicode.IsSpace(r):
			return ' '
		}
		return -1
	}, norm.NFKC.String(s))
	s = Squeeze(s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return strings.TrimSpace(s)
}
