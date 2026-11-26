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

// SearchMessages は、サーバーのチャンネルのログ（kind='guild'）の本文を部分一致で探す（新しい順）。
// **DMは対象にしない**（DMは、そのDMの会話の中だけ。検索には混ぜない）。ぬる自身の発言は除く。
func (s *Store) SearchMessages(ctx context.Context, query string, limit int) ([]Hit, error) {
	query = strings.TrimSpace(query)
	if query == "" || len([]rune(query)) > MaxQueryRunes {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT message_id, channel_id, author_name, content, created_at FROM messages
	  WHERE kind = 'guild' AND NOT is_bot AND content ILIKE $1 ORDER BY message_id DESC LIMIT $2`, likePattern(query), limit)
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

// ForgetPerson は、その人についてのノートを全部消す（「私のことを忘れて」）。親密度・呼び名も初期化する。消したノートの件数を返す。
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
	return tag.RowsAffected(), tx.Commit(ctx)
}
