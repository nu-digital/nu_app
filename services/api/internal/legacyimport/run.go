package legacyimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// Options は移行の設定。
type Options struct {
	ContentDir  string // content/{blog,news,project}の親
	BaselineDir string // inventory.json・takedown.jsonのあるディレクトリ（docs/legacy-baseline）
	PublicDir   string // 任意。静的画像（public）の実在確認（空なら確認しない）
	AsLoginID   string // 記事の作成者・公開者として記録する有効なOwnerのログインID
	OSUser      string // 実行したOSユーザー（監査ログ）
	Apply       bool   // falseなら、同じ処理を最後まで実行してROLLBACK（dry-run）
	// AllowSourceChanges: 移行元の.mdのsha256がinventory.jsonと違っていても続ける（意図して記事を変えた場合だけ）
	AllowSourceChanges bool
	Now                func() time.Time
}

// Report は結果。Problemsがあるときは何も書いていない。
type Report struct {
	Created       []string // 新しく作ったもの（区分/slug）
	Skipped       []string // 既に移行済みで、作業版・公開版とも移行元と同じもの
	Drift         []string // 既に移行済みだが、作業版か公開版が移行元と違うもの（CMSで編集済み。上書きしない）
	Takedown      []string // 取り下げ一覧にあり、移行しなかったもの
	Problems      []string
	AuthorsBefore int64
	AuthorsAfter  int64
	Applied       bool
}

type inventory struct {
	Articles []struct {
		Collection, Slug, File, FileSha256 string
		Cover                              struct{ Ref string }
		BodyImages                         []struct{ Ref string }
	}
}

type takedown struct {
	Articles []struct{ Section, Slug string }
	Images   []struct{ Path string }
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Run は検査→（適用時のみ）1トランザクションで書き込み。
func Run(ctx context.Context, pool *pgxpool.Pool, o Options) (Report, error) {
	var rep Report
	if o.Now == nil {
		o.Now = time.Now
	}
	var inv inventory
	var td takedown
	if err := readJSON(filepath.Join(o.BaselineDir, "inventory.json"), &inv); err != nil {
		return rep, errors.New("inventory.jsonを読めません")
	}
	if err := readJSON(filepath.Join(o.BaselineDir, "takedown.json"), &td); err != nil {
		return rep, errors.New("takedown.jsonを読めません")
	}
	tdArticle := map[string]bool{}
	for _, a := range td.Articles {
		tdArticle[a.Section+"/"+a.Slug] = true
	}
	tdImage := map[string]bool{}
	for _, i := range td.Images {
		tdImage[i.Path] = true
	}
	shaOf := map[string]string{}
	imagesOf := map[string][]string{}
	for _, a := range inv.Articles {
		key := a.Collection + "/" + a.Slug
		shaOf[key] = a.FileSha256
		if a.Cover.Ref != "" {
			imagesOf[key] = append(imagesOf[key], a.Cover.Ref)
		}
		for _, b := range a.BodyImages {
			imagesOf[key] = append(imagesOf[key], b.Ref)
		}
	}

	// 1. 読み込みと検査（全件。1本でも違反があれば何も書かない）
	arts, problems := Load(o.ContentDir)
	var prepared []Prepared
	for _, a := range arts {
		key := a.Collection + "/" + a.Slug
		if tdArticle[key] {
			rep.Takedown = append(rep.Takedown, key)
			continue
		}
		want, known := shaOf[key]
		switch {
		case !known:
			problems = append(problems, key+": inventory.jsonに無い記事です")
		case want != a.FileSHA256 && !o.AllowSourceChanges:
			problems = append(problems, key+": ファイルが基準（inventory.json）と違います（意図した変更なら --allow-source-changes）")
		}
		p, ps := Prepare(a)
		problems = append(problems, ps...)
		if o.PublicDir != "" {
			for _, ref := range imagesOf[key] {
				if tdImage[ref] {
					continue
				}
				if _, err := os.Stat(filepath.Join(o.PublicDir, ref)); err != nil {
					problems = append(problems, fmt.Sprintf("%s: 画像が見つかりません: %s", key, ref))
				}
			}
		}
		prepared = append(prepared, p)
	}
	seen := map[string]string{}
	for _, p := range prepared { // sourceIdの重複（別の記事が同じ移行元を名乗る）
		if other, ok := seen[p.SourceID]; ok && p.SourceID != "" {
			problems = append(problems, fmt.Sprintf("%s/%s: sourceIdが%sと重複しています", p.Collection, p.Slug, other))
		}
		seen[p.SourceID] = p.Collection + "/" + p.Slug
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		rep.Problems = problems
		return rep, nil
	}

	// 2. 書き込み（1トランザクション。dry-runは最後までやってROLLBACK）
	tx, err := pool.Begin(ctx)
	if err != nil {
		return rep, errors.New("DBに接続できません")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 同時実行を防ぐ
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('nu_app.import_legacy'))`); err != nil {
		return rep, err
	}
	q := store.New(tx)
	// --asは、トランザクション内で「有効なOwner」であることをロックして確認する
	u, err := q.GetActiveUserByLoginIDForShare(ctx, o.AsLoginID)
	if err != nil || u.Status != "active" {
		rep.Problems = []string{"--as のユーザーが見つからない、または有効ではありません"}
		return rep, nil
	}
	roles, err := q.ActorRolesForShare(ctx, u.ID)
	if err != nil {
		return rep, err
	}
	if !contains(roles, "owner") {
		rep.Problems = []string{"--as のユーザーはOwnerではありません"}
		return rep, nil
	}
	if rep.AuthorsBefore, err = countAuthors(ctx, tx); err != nil {
		return rep, err
	}
	for _, p := range prepared {
		key := p.Collection + "/" + p.Slug
		existing, err := q.GetContentBySectionSlug(ctx, store.GetContentBySectionSlugParams{Section: p.Section, Slug: p.Slug})
		switch {
		case err == nil:
			status, perr := compareExisting(ctx, q, existing, p)
			if perr != "" {
				rep.Problems = append(rep.Problems, key+": "+perr)
				continue
			}
			if status == "same" {
				rep.Skipped = append(rep.Skipped, key)
			} else {
				rep.Drift = append(rep.Drift, key+"（"+status+"）")
			}
		case errors.Is(err, pgx.ErrNoRows):
			if err := create(ctx, tx, q, u.ID, o, p); err != nil {
				return rep, fmt.Errorf("%s: 作成に失敗しました", key)
			}
			rep.Created = append(rep.Created, key)
		default:
			return rep, err
		}
	}
	if len(rep.Problems) > 0 {
		return rep, nil
	}
	if rep.AuthorsAfter, err = countAuthors(ctx, tx); err != nil {
		return rep, err
	}
	if !o.Apply {
		return rep, nil // dry-run: deferのRollback
	}
	if err := tx.Commit(ctx); err != nil {
		return rep, errors.New("記録を確定できませんでした")
	}
	rep.Applied = true
	return rep, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func countAuthors(ctx context.Context, tx pgx.Tx) (int64, error) {
	var n int64
	err := tx.QueryRow(ctx, `SELECT count(*) FROM legacy_authors`).Scan(&n)
	return n, err
}

func pgDate(s *string) pgtype.Date {
	if s == nil {
		return pgtype.Date{}
	}
	t, _ := time.Parse("2006-01-02", *s)
	return pgtype.Date{Time: t, Valid: true}
}

func create(ctx context.Context, tx pgx.Tx, q *store.Queries, owner pgtype.UUID, o Options, p Prepared) error {
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS contents_working_revision_fk, contents_published_revision_fk, content_revisions_content_id_fkey DEFERRED`); err != nil {
		return err
	}
	contentID, err := q.NewUUID(ctx)
	if err != nil {
		return err
	}
	revID, err := q.NewUUID(ctx)
	if err != nil {
		return err
	}
	var authorID pgtype.UUID
	if p.AuthorName != "" { // 同じ投稿者の記事は同じ行を共有する。公開可否・紐付けには触れない（未確認で作られる）
		a, err := q.UpsertLegacyAuthor(ctx, p.AuthorName)
		if err != nil {
			return err
		}
		authorID = a.ID
	}
	c, err := q.CreateContent(ctx, store.CreateContentParams{ID: contentID, Type: p.Type, Section: p.Section, Slug: p.Slug, CreatorUserID: owner, WorkingRevisionID: revID, LegacyAuthorID: authorID})
	if err != nil {
		return err
	}
	mj, _ := json.Marshal(p.Metadata)
	if _, err := q.InsertContentRevision(ctx, store.InsertContentRevisionParams{
		ID: revID, ContentID: contentID, RevisionNo: 1, Title: p.Title, Summary: p.Summary, Markdown: p.Markdown, MetadataJson: mj,
		DateLabel: p.Date.Label, DateKind: p.Date.Kind, SortDate: pgDate(p.Date.SortDate), SortDateIsEstimated: p.Date.Estimated, EditedBy: owner,
	}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SET CONSTRAINTS contents_working_revision_fk, contents_published_revision_fk, content_revisions_content_id_fkey IMMEDIATE`); err != nil {
		return err
	}
	now := pgtype.Timestamptz{Time: o.Now(), Valid: true}
	if err := q.SetPublishedRevision(ctx, store.SetPublishedRevisionParams{ID: c.ID, PublishedRevisionID: revID, UpdatedAt: now}); err != nil {
		return err
	}
	// 公開の履歴は残すが、公開の通知（outbox）は積まない（67件のDiscord通知を防ぐ）
	if _, err := q.InsertPublicationEvent(ctx, store.InsertPublicationEventParams{ContentID: c.ID, RevisionID: revID, ActorUserID: owner, EventType: "publish"}); err != nil {
		return err
	}
	summary, _ := json.Marshal(map[string]any{"sourceId": p.SourceID, "executedByOsUser": o.OSUser})
	return q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorUserID: owner, Action: "content.imported", TargetType: pgtype.Text{String: "content", Valid: true},
		TargetID: pgtype.Text{String: c.ID.String(), Valid: true}, SummaryJson: summary,
	})
}

// compareExisting は、既に有る記事を移行元と比べる。戻り値: "same"／差の説明／（衝突）problem。
// **公開ポインタ・公開状態・投稿者には一切触れない**（公開停止中のものを再公開しない。移行後の運用を消さない）。
func compareExisting(ctx context.Context, q *store.Queries, c store.Content, p Prepared) (string, string) {
	work, err := q.GetContentRevision(ctx, c.WorkingRevisionID)
	if err != nil {
		return "", "作業版を読めません"
	}
	if decodeMeta(work.MetadataJson)["sourceId"] != p.SourceID {
		return "", "同じ区分・slugの記事が、別の移行元（sourceId）で既にあります（衝突）"
	}
	var diffs []string
	if !sameRevision(work, p) {
		diffs = append(diffs, "作業版が移行元と異なる")
	}
	if c.PublishedRevisionID.Valid && c.PublishedRevisionID != c.WorkingRevisionID {
		pub, err := q.GetContentRevision(ctx, c.PublishedRevisionID)
		if err != nil {
			return "", "公開版を読めません"
		}
		if !sameRevision(pub, p) {
			diffs = append(diffs, "公開版が移行元と異なる")
		}
	}
	if len(diffs) == 0 {
		return "same", ""
	}
	return strings.Join(diffs, "・"), ""
}

func decodeMeta(b []byte) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(b, &m)
	return m
}

func sameRevision(r store.ContentRevision, p Prepared) bool {
	pm, rm := p.Metadata, decodeMeta(r.MetadataJson)
	if len(pm) != len(rm) {
		return false
	}
	for k, v := range pm {
		if rm[k] != v {
			return false
		}
	}
	sd := pgDate(p.Date.SortDate)
	return r.Title == p.Title && r.Summary == p.Summary && r.Markdown == p.Markdown && r.DateLabel == p.Date.Label &&
		r.DateKind == p.Date.Kind && r.SortDate == sd && r.SortDateIsEstimated == p.Date.Estimated
}
