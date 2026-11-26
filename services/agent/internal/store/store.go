// Package store はぬるの永続化（PostgreSQLの agent スキーマ。専用ロール nu_agent）。
// アプリのログにメッセージ本文を出さない。ここも本文は返すだけで、ログに書かない。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Open は接続URL（秘密を含む。ログに出さない）で接続する。
func Open(ctx context.Context, url string) (*Store, error) {
	cfg, err := PoolConfig(url)
	if err != nil {
		return nil, errors.New("DBの接続設定が不正です")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("DBに接続できません")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("DBに接続できません（nu_agentのパスワードの設定を確認してください）")
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// PoolConfig は接続設定。search_path を agent に固定する（ロールの既定に頼らない。表名にスキーマを付けない）。
func PoolConfig(url string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "agent"
	return cfg, nil
}

// ---- 設定 ----

// GetSetting は設定の値（JSON）を返す。無ければ ok=false。
func (s *Store) GetSetting(ctx context.Context, key string) (json.RawMessage, bool, error) {
	var v json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetSetting(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, b)
	return err
}

// ---- メッセージの記録 ----

type Message struct {
	ID          int64
	ChannelID   int64
	ThreadID    *int64
	Kind        string // guild | dm
	AuthorID    int64
	AuthorName  string
	IsBot       bool
	Content     string
	Attachments []Attachment
	CreatedAt   time.Time
	EditedAt    *time.Time
}

// Attachment は添付のメタ情報だけ（URLは期限切れになるので保存しない）。
type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
}

// UpsertMessage はリアルタイムに受けたメッセージを記録する（既にあれば内容を更新）。
func (s *Store) UpsertMessage(ctx context.Context, m Message) error {
	return s.insert(ctx, m, true)
}

// InsertImported は過去ログの取り込み。既にあるもの（リアルタイムで記録済み）は変えない。
func (s *Store) InsertImported(ctx context.Context, m Message) error {
	return s.insert(ctx, m, false)
}

func (s *Store) insert(ctx context.Context, m Message, update bool) error {
	att, err := json.Marshal(nonNil(m.Attachments))
	if err != nil {
		return err
	}
	q := `INSERT INTO messages (message_id, channel_id, thread_id, kind, author_id, author_name, is_bot, content, attachments, created_at, edited_at)
	      VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	if update {
		q += ` ON CONFLICT (message_id) DO UPDATE SET content = EXCLUDED.content, attachments = EXCLUDED.attachments, edited_at = EXCLUDED.edited_at, author_name = EXCLUDED.author_name`
	} else {
		q += ` ON CONFLICT (message_id) DO NOTHING`
	}
	_, err = s.pool.Exec(ctx, q, m.ID, m.ChannelID, m.ThreadID, m.Kind, m.AuthorID, m.AuthorName, m.IsBot, m.Content, att, m.CreatedAt, m.EditedAt)
	return err
}

func nonNil(a []Attachment) []Attachment {
	if a == nil {
		return []Attachment{}
	}
	return a
}

// UpdateMessage は編集を反映する（記録に無いメッセージは何もしない）。
func (s *Store) UpdateMessage(ctx context.Context, id int64, content string, att []Attachment, editedAt time.Time) error {
	b, err := json.Marshal(nonNil(att))
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE messages SET content = $2, attachments = $3, edited_at = $4 WHERE message_id = $1`, id, content, b, editedAt)
	return err
}

// DeleteMessages はDiscordでの削除に合わせて、記録から**物理削除**する。
func (s *Store) DeleteMessages(ctx context.Context, ids ...int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `DELETE FROM messages WHERE message_id = ANY($1) RETURNING channel_id, (created_at AT TIME ZONE 'Asia/Tokyo')::date`, ids)
	if err != nil {
		return 0, err
	}
	type key struct {
		ch  int64
		day time.Time
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.ch, &k.day); err != nil {
			return 0, err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// 消されたメッセージを含む日の要約も消す（残った内容で作り直される）
	for _, k := range keys {
		if _, err := tx.Exec(ctx, `DELETE FROM summaries WHERE channel_id = $1 AND day = $2`, k.ch, k.day); err != nil {
			return 0, err
		}
	}
	n := int64(len(keys))
	// 元のメッセージが消されたら、それを元にしたノートも消す（削除の連動）
	if _, err := tx.Exec(ctx, `DELETE FROM notes WHERE source_message_id = ANY($1)`, ids); err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

// CountMessages は記録の件数（チャンネルを指定しなければ全体）。
func (s *Store) CountMessages(ctx context.Context, channelID *int64) (int64, error) {
	var n int64
	var err error
	if channelID == nil {
		err = s.pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT count(*) FROM messages WHERE channel_id = $1`, *channelID).Scan(&n)
	}
	return n, err
}

// ---- 過去ログの取り込みの進捗 ----

type Progress struct {
	ChannelID       int64
	OldestMessageID *int64
	Done            bool
	Imported        int64
}

func (s *Store) GetProgress(ctx context.Context, channelID int64) (Progress, bool, error) {
	p := Progress{ChannelID: channelID}
	err := s.pool.QueryRow(ctx, `SELECT oldest_message_id, done, imported FROM import_progress WHERE channel_id = $1`, channelID).Scan(&p.OldestMessageID, &p.Done, &p.Imported)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}

func (s *Store) SaveProgress(ctx context.Context, p Progress) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO import_progress (channel_id, oldest_message_id, done, imported) VALUES ($1,$2,$3,$4)
	  ON CONFLICT (channel_id) DO UPDATE SET oldest_message_id = EXCLUDED.oldest_message_id, done = EXCLUDED.done, imported = EXCLUDED.imported, updated_at = now()`,
		p.ChannelID, p.OldestMessageID, p.Done, p.Imported)
	return err
}

// ImportSummary は取り込み全体の進み具合（/nul status 用）。
func (s *Store) ImportSummary(ctx context.Context) (channels, done int, imported int64, err error) {
	err = s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE done), coalesce(sum(imported),0) FROM import_progress`).Scan(&channels, &done, &imported)
	return
}

// ---- API使用量（予算） ----

type UsageRecord struct {
	Model        string
	Purpose      string
	Input        int64
	Output       int64
	CacheRead    int64
	CacheWrite   int64
	CostMicroUSD int64
	ChannelID    *int64
	UserID       *int64
}

// RecordUsage はAPI呼び出し1回の使用量を記録する（本文は入れない）。
func (s *Store) RecordUsage(ctx context.Context, u UsageRecord) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO api_usage (model, purpose, input_tokens, output_tokens, cache_read, cache_write, cost_micro_usd, channel_id, user_id)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, u.Model, u.Purpose, u.Input, u.Output, u.CacheRead, u.CacheWrite, u.CostMicroUSD, u.ChannelID, u.UserID)
	return err
}

// SpentMicroSince は from 以降の使用額（マイクロドル）。
func (s *Store) SpentMicroSince(ctx context.Context, from time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(cost_micro_usd), 0) FROM api_usage WHERE ts >= $1`, from).Scan(&n)
	return n, err
}

// IncDaily は一人あたりの1日の回数を1増やし、増やした後の回数を返す（原子的）。kind は reply|opus。
func (s *Store) IncDaily(ctx context.Context, day time.Time, userID int64, kind string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `INSERT INTO daily_calls (day, discord_user_id, kind, n) VALUES ($1::date, $2, $3, 1)
	  ON CONFLICT (day, discord_user_id, kind) DO UPDATE SET n = daily_calls.n + 1 RETURNING n`, day, userID, kind).Scan(&n)
	return n, err
}

// ---- ぬるの状態・親密度 ----

type State struct {
	Mood   int
	Energy int
	Hobby  string
}

// GetState は気分などの状態（1行）。
func (s *Store) GetState(ctx context.Context) (State, error) {
	var st State
	err := s.pool.QueryRow(ctx, `SELECT mood, energy, hobby FROM state WHERE id`).Scan(&st.Mood, &st.Energy, &st.Hobby)
	return st, err
}

func (s *Store) SaveState(ctx context.Context, st State) error {
	_, err := s.pool.Exec(ctx, `UPDATE state SET mood = $1, energy = $2, hobby = $3, updated_at = now() WHERE id`, st.Mood, st.Energy, st.Hobby)
	return err
}

// GetCloseness は人ごとの親密度（話した回数。無ければ0）。
func (s *Store) GetCloseness(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT closeness FROM relationships WHERE discord_user_id = $1`, userID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// BumpCloseness は話したことを1回数える（最後に話した時刻も更新）。
func (s *Store) BumpCloseness(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO relationships (discord_user_id, closeness) VALUES ($1, 1)
	  ON CONFLICT (discord_user_id) DO UPDATE SET closeness = relationships.closeness + 1, last_seen = now()`, userID)
	return err
}

// RecentMessages は、そのやりとりの直近のメッセージを古い順に返す（beforeIDより古いもの。0なら最新から）。
// threadID があればそのスレッド、なければチャンネル（スレッドを除く）。DMは channelID がDMチャンネルなので、そのDMの中だけ（他の場所のメッセージは混ざらない）。
func (s *Store) RecentMessages(ctx context.Context, channelID int64, threadID *int64, beforeID int64, limit int) ([]Message, error) {
	q := `SELECT message_id, channel_id, thread_id, kind, author_id, author_name, is_bot, content, created_at FROM messages
	      WHERE channel_id = $1 AND ((thread_id IS NULL AND $2::bigint IS NULL) OR thread_id = $2) AND ($3 = 0 OR message_id < $3)
	      ORDER BY message_id DESC LIMIT $4`
	rows, err := s.pool.Query(ctx, q, channelID, threadID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ChannelID, &m.ThreadID, &m.Kind, &m.AuthorID, &m.AuthorName, &m.IsBot, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { // 古い順にする
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// TakeReaction は、その日のリアクションの枠を1つ使う（上限 cap まで。原子的）。使えたら true。
func (s *Store) TakeReaction(ctx context.Context, day time.Time, cap int) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `INSERT INTO reactions_daily (day, n) SELECT $1::date, 1 WHERE $2 > 0
	  ON CONFLICT (day) DO UPDATE SET n = reactions_daily.n + 1 WHERE reactions_daily.n < $2 RETURNING n`, day, cap).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
