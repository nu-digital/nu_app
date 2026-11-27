package brain

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// State はぬるの状態（毎回の会話に、システムメッセージとして渡す）。
type State struct {
	Mood   int // 機嫌 -2〜+2
	Energy int // 基礎のテンション -2〜+2（日ごとに揺らぐ。時間帯の補正は Describe で足す）
	Hobby  string
}

func clamp(n, lo, hi int) int { return max(lo, min(hi, n)) }

// DailyDrift は毎日の機嫌の揺らぎ（-1〜+1動き、極端な値は少し平均へ戻る）。テンションは日ごとに引き直す。
func DailyDrift(s State, rng *rand.Rand) State {
	step := rng.IntN(3) - 1
	pull := 0
	if s.Mood >= 2 {
		pull = -1
	} else if s.Mood <= -2 {
		pull = 1
	}
	s.Mood = clamp(s.Mood+step+pull, -2, 2)
	s.Energy = clamp(rng.IntN(5)-2, -2, 2)
	return s
}

// Praised は褒められた・好きな話題のとき、Roughed は雑に扱われたとき。
func (s State) Praised() State { s.Mood = clamp(s.Mood+1, -2, 2); return s }
func (s State) Roughed() State { s.Mood = clamp(s.Mood-1, -2, 2); return s }

// hourEnergy は時間帯によるテンション（夜型。深夜に高く、朝は低い）。JSTの時。
func hourEnergy(hour int) int {
	switch {
	case hour >= 0 && hour < 4:
		return 2
	case hour >= 4 && hour < 9:
		return -2
	case hour >= 9 && hour < 12:
		return -1
	case hour >= 12 && hour < 18:
		return 0
	case hour >= 18 && hour < 22:
		return 1
	default:
		return 2
	}
}

func moodLabel(m int) string {
	return map[int]string{-2: "かなり機嫌が悪い。そっけない（でも頼まれた仕事はやる）", -1: "少し機嫌が悪い", 0: "ふつう", 1: "機嫌がいい", 2: "かなり機嫌がいい"}[m]
}

func energyLabel(e int) string {
	switch {
	case e <= -2:
		return "眠くてだるい（短め）"
	case e == -1:
		return "低め"
	case e == 0:
		return "ふつう"
	case e == 1:
		return "高め"
	}
	return "テンション高い"
}

// Closeness は親密度の段階（会話の回数など）。
func closenessLabel(n int) string {
	switch {
	case n >= 50:
		return "よく話す相手。気を遣わず、砕けた態度でいい"
	case n >= 10:
		return "顔なじみ。少し砕けてもいい"
	}
	return "あまり話していない相手。きつい言い方はしない"
}

// Describe は会話の途中のシステムメッセージの本文（気分・日時・場所・親密度）。固定部分（SystemPrompt）には入れない。
// where は「DM」「times」「#チャンネル名」など。個人情報は含めない。
func Describe(s State, now time.Time, closeness int, where string) string {
	jst := now.In(time.FixedZone("JST", 9*3600))
	energy := clamp(s.Energy+hourEnergy(jst.Hour()), -2, 2)
	var b strings.Builder
	fmt.Fprintf(&b, "【いまの状態】日時: %s（JST）／場所: %s\n", jst.Format("2006年1月2日(Mon) 15:04"), where)
	fmt.Fprintf(&b, "機嫌: %s／テンション: %s", moodLabel(s.Mood), energyLabel(energy))
	if s.Hobby != "" {
		fmt.Fprintf(&b, "／最近ハマっていること: %s", s.Hobby)
	}
	fmt.Fprintf(&b, "\n話している相手との親密度: %s", closenessLabel(closeness))
	return b.String()
}
