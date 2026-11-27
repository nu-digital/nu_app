// Package discord はぬるのDiscord側。**ぬるができるDiscord操作は、このパッケージが公開する狭い関数だけ**（許可リスト。仕様§17.1）。
// 削除・BAN・キック・ロール／チャンネル／サーバー設定の変更は、実装しない（インターフェースにも出さない）。
// ログにメッセージ本文を出さない（IDと長さだけ）。
package discord

import (
	"context"
	"time"
)

// Msg はDiscordのメッセージのうち、ぬるが使う項目（discordgoの型に依存させない）。
type Msg struct {
	ID          int64
	ChannelID   int64 // スレッドの場合は親チャンネル
	ThreadID    *int64
	GuildID     int64 // 0ならDM
	AuthorID    int64
	AuthorName  string
	IsBot       bool
	Public      bool // @everyoneが見られるチャンネルのメッセージ（検索・要約などの材料にしてよい）
	Content     string
	Attachments []Attachment
	CreatedAt   time.Time
	EditedAt    *time.Time
}

type Attachment struct {
	ID          string
	Filename    string
	ContentType string
	Size        int
	// URL は取得用（署名つきで期限切れになる。保存しない）
	URL string
}

// Channel は取り込み対象の候補。
type Channel struct {
	ID       int64
	ParentID int64 // スレッドのとき親
	Name     string
	IsThread bool
	Public   bool
}

// History は過去ログを読む窓口（実体はdiscordgo、試験では偽物）。
type History interface {
	// Channels は、取り込める（メッセージを持つ）チャンネルとスレッド（アクティブ・アーカイブ済み）の一覧。
	Channels(ctx context.Context, guildID int64) ([]Channel, error)
	// MessagesBefore は beforeID（0なら最新）より古いメッセージを新しい順に最大limit件返す。
	MessagesBefore(ctx context.Context, ch Channel, beforeID int64, limit int) ([]Msg, error)
}

// Incoming は受信したメッセージと、その文脈（応答の判断に使う）。
type Incoming struct {
	Msg           Msg
	MentionsBot   bool // ぬるへのメンション
	RepliesToBot  bool // ぬるのメッセージへの返信
	IsBotAuthored bool // Botの発言（ぬる自身を含む）
}
