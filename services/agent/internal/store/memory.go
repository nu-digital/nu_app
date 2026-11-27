package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- ログ検索 ----

type Hit struct {
	MessageID  int64
	ChannelID  int64
	AuthorName string
	Content    string
	CreatedAt  time.Time
}

const MaxQueryRunes = 50

// likePattern は検索語を ILIKE のパターンにする（%・_・\ は文字として扱う）。
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// SearchMessages は、サーバーの**公開チャンネル**（@everyoneが見られる。is_public）のログの本文を部分一致で探す（新しい順）。
// **DMは対象にしない**（DMは、そのDMの会話の中だけ。検索には混ぜない）。ぬる自身の発言は除く。
func (s *Store) SearchMessages(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > MaxQueryRunes {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT message_id, channel_id, author_name, content, created_at FROM messages
	  WHERE kind = 'guild' AND is_public AND NOT is_bot AND content ILIKE $1 ORDER BY message_id DESC LIMIT $2`, likePattern(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.MessageID, &h.ChannelID, &h.AuthorName, &h.Content, &h.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- ノート ----

type Note struct {
	ID              int64
	SubjectUserID   *int64
	Content         string
	Private         bool
	SourceMessageID *int64
	SourceChannelID *int64
	CreatedAt       time.Time
}

const (
	MaxNoteRunes     = 500
	MaxNotesPerUser  = 100
	MaxGeneralNotes  = 500
	ErrNoteLimitText = "ノートが多すぎる"
)

var ErrNoteLimit = errors.New(ErrNoteLimitText)

// AddNote はノートを追加する。内容は1〜500字。1人あたり100件・一般のメモは500件まで（暴走・水増しの対策）。
func (s *Store) AddNote(ctx context.Context, n Note) (int64, error) {
	c := strings.TrimSpace(n.Content)
	if r := []rune(c); len(r) == 0 || len(r) > MaxNoteRunes {
		return 0, errors.New("ノートは1〜500文字")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var count int
	limit := MaxGeneralNotes
	if n.SubjectUserID != nil {
		limit = MaxNotesPerUser
		err = tx.QueryRow(ctx, `SELECT count(*) FROM notes WHERE subject_user_id = $1`, *n.SubjectUserID).Scan(&count)
	} else {
		err = tx.QueryRow(ctx, `SELECT count(*) FROM notes WHERE subject_user_id IS NULL`).Scan(&count)
	}
	if err != nil {
		return 0, err
	}
	if count >= limit {
		return 0, ErrNoteLimit
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO notes (subject_user_id, content, private, source_message_id, source_channel_id) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		n.SubjectUserID, c, n.Private, n.SourceMessageID, n.SourceChannelID).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// SearchNotes は、いまの話し相手が見てよいノートを探す（新しい順）。query が空なら最近のもの。
// 見てよいのは: 公開のノート（private=false）全部、と、**DMの中では、その相手についての private なノート**。
func (s *Store) SearchNotes(ctx context.Context, query string, askerID int64, inDM bool, limit int) ([]Note, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) > MaxQueryRunes {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, subject_user_id, content, private, source_message_id, source_channel_id, created_at FROM notes
	  WHERE (NOT private OR ($2 AND subject_user_id = $3)) AND ($1 = '' OR content ILIKE $4) ORDER BY id DESC LIMIT $5`,
		query, inDM, askerID, likePattern(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.SubjectUserID, &n.Content, &n.Private, &n.SourceMessageID, &n.SourceChannelID, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ForgetNote は、その人についてのノート1件を消す。**本人についてのノートだけ**（他の人のノートは消せない）。消せたら true。
func (s *Store) ForgetNote(ctx context.Context, id, subjectUserID int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notes WHERE id = $1 AND subject_user_id = $2`, id, subjectUserID)
	return tag.RowsAffected() == 1, err
}

// ForgetPerson は、その人についてのノートと、リマインダー、親密度・呼び名を全部消す（「私のことを忘れて」）。サーバーの発言ログ（messages）と、それから作った要約は消さない（仕様どおり）。消したノートの件数を返す。
func (s *Store) ForgetPerson(ctx context.Context, userID int64) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM notes WHERE subject_user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM relationships WHERE discord_user_id = $1`, userID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	// 本人のリマインダー（DMで頼んだ私事を含む）も消す
	if _, err := tx.Exec(ctx, `DELETE FROM reminders WHERE user_id = $1`, userID); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// ---- 日次要約 ----

// Group は要約の候補（チャンネル×日（JST））。
type Group struct {
	ChannelID int64
	Day       time.Time
	Count     int
	Chars     int
}

// SummaryCandidates は、まだ要約が無い（チャンネル×日）を、新しい日から返す。
// サーバーのチャンネルの人間の発言（DM・Bot除く）が minMsgs 件以上あり、日が before（JSTの日付。含まない）より前のもの。since があればその日以降だけ。
func (s *Store) SummaryCandidates(ctx context.Context, before time.Time, since *time.Time, minMsgs, limit int) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `SELECT m.channel_id, d.day, count(*), sum(char_length(m.content))
	  FROM messages m, LATERAL (SELECT (m.created_at AT TIME ZONE 'Asia/Tokyo')::date AS day) d
	  WHERE m.kind = 'guild' AND m.is_public AND NOT m.is_bot AND d.day < $1::date AND ($2::date IS NULL OR d.day >= $2::date)
	    AND NOT EXISTS (SELECT 1 FROM summaries s WHERE s.channel_id = m.channel_id AND s.day = d.day)
	  GROUP BY m.channel_id, d.day HAVING count(*) >= $3
	  ORDER BY d.day DESC, m.channel_id LIMIT $4`, before, since, minMsgs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ChannelID, &g.Day, &g.Count, &g.Chars); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// DayMessages は、そのチャンネルのその日（JST）の人間の発言を古い順に返す（サーバーのチャンネルのみ）。
func (s *Store) DayMessages(ctx context.Context, channelID int64, day time.Time, limit int) ([]Message, error) {
	rows, err := s.pool.Query(ctx, `SELECT message_id, author_name, content, created_at FROM messages
	  WHERE kind = 'guild' AND is_public AND NOT is_bot AND channel_id = $1 AND (created_at AT TIME ZONE 'Asia/Tokyo')::date = $2::date
	  ORDER BY message_id LIMIT $3`, channelID, day, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		m := Message{ChannelID: channelID, Kind: "guild"}
		if err := rows.Scan(&m.ID, &m.AuthorName, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type Summary struct {
	ChannelID int64
	Day       time.Time
	Content   string
	MsgCount  int
}

// SaveSummary は要約を保存する（その日のメッセージがバッチの処理中に消えていた場合に備え、既にあれば上書きしない）。
func (s *Store) SaveSummary(ctx context.Context, sm Summary) error {
	c := strings.TrimSpace(sm.Content)
	if r := []rune(c); len(r) == 0 || len(r) > 3000 {
		return errors.New("要約は1〜3000文字")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO summaries (channel_id, day, content, msg_count) VALUES ($1, $2::date, $3, $4) ON CONFLICT DO NOTHING`,
		sm.ChannelID, sm.Day, c, sm.MsgCount)
	return err
}

// SearchSummaries は要約を探す（新しい日から）。query が空なら最近のもの。
func (s *Store) SearchSummaries(ctx context.Context, query string, limit int) ([]Summary, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) > MaxQueryRunes {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT channel_id, day, content, msg_count FROM summaries
	  WHERE $1 = '' OR content ILIKE $2 ORDER BY day DESC, channel_id LIMIT $3`, query, likePattern(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var x Summary
		if err := rows.Scan(&x.ChannelID, &x.Day, &x.Content, &x.MsgCount); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// SummariesBetween は、from（含む）〜to（含まない）の日の要約を、発言が多かった順に返す（週報・月報の材料）。
func (s *Store) SummariesBetween(ctx context.Context, from, to time.Time, limit int) ([]Summary, error) {
	rows, err := s.pool.Query(ctx, `SELECT channel_id, day, content, msg_count FROM summaries
	  WHERE day >= $1::date AND day < $2::date ORDER BY msg_count DESC, day, channel_id LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var x Summary
		if err := rows.Scan(&x.ChannelID, &x.Day, &x.Content, &x.MsgCount); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---- 研究日誌 ----

type Diary struct {
	ID      int64
	Day     time.Time
	Kind    string // dig | web
	Topic   string
	Content string
}

// AddDiary は日誌を1件加える（その日のその種別が既にあれば何もしない。加えたら true）。
func (s *Store) AddDiary(ctx context.Context, d Diary) (bool, error) {
	topic, content := strings.TrimSpace(d.Topic), strings.TrimSpace(d.Content)
	if r := []rune(topic); len(r) == 0 || len(r) > 100 {
		return false, errors.New("題は1〜100文字")
	}
	if r := []rune(content); len(r) == 0 || len(r) > 1500 {
		return false, errors.New("日誌は1〜1500文字")
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO diary (day, kind, topic, content) VALUES ($1::date, $2, $3, $4) ON CONFLICT (day, kind) DO NOTHING`, d.Day, d.Kind, topic, content)
	return tag.RowsAffected() == 1, err
}

// RecentDiary は最近の日誌（新しい順）。
func (s *Store) RecentDiary(ctx context.Context, limit int) ([]Diary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, day, kind, topic, content FROM diary ORDER BY day DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Diary
	for rows.Next() {
		var d Diary
		if err := rows.Scan(&d.ID, &d.Day, &d.Kind, &d.Topic, &d.Content); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RandomOldSummary は、before（含まない）より前の日の要約から、発言が minMsgs 件以上のものを1つランダムに返す（発掘日誌の材料）。無ければ ok=false。
func (s *Store) RandomOldSummary(ctx context.Context, before time.Time, minMsgs int) (Summary, bool, error) {
	var x Summary
	err := s.pool.QueryRow(ctx, `SELECT channel_id, day, content, msg_count FROM summaries WHERE day < $1::date AND msg_count >= $2 ORDER BY random() LIMIT 1`,
		before, minMsgs).Scan(&x.ChannelID, &x.Day, &x.Content, &x.MsgCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return x, false, nil
	}
	return x, err == nil, err
}
