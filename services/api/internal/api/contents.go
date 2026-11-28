package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/cmsrules"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/mdcheck"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// CMS（記事・作品）の作成・版の保存・共同編集者・制作メンバー。設計の正本は docs/design/p3-cms.md。
//
// 方針（Opusレビュー反映）:
//   - 書き込みは「Tx開始→contents行をFOR UPDATE→関係（作者/共同編集者）を読み直す→authz.Decide→更新」の順。
//   - 関係が必要な操作で権限が無い場合は、存在しないIDと同じ404にする（下書きの存在を漏らさない）。
//   - 重い処理（Markdown検査）は、認可と行ロックの後に行う。
//   - 一覧は、編集できない人には公開版の値だけを返す（作業版の書き換えを見せない）。

const maxContentBody = 512 << 10

var errNotFound = &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}

type dateIn struct {
	Label    string  `json:"label"`
	Kind     string  `json:"kind"`
	SortDate *string `json:"sortDate"`
}

// optUUID は「省略」「null（消す）」「値あり」を区別するJSONの値。
type optUUID struct {
	Set, Null bool
	Val       string
}

func (o *optUUID) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

// contentFields は版の内容。作成と版の保存で共通。
type contentFields struct {
	Title    string             `json:"title"`
	Summary  string             `json:"summary"`
	Markdown string             `json:"markdown"`
	Metadata map[string]*string `json:"metadata"`
	Date     *dateIn            `json:"date"`
	// CoverMediaID: カバー画像（アップロード済みの画像のid）。省略は維持、nullで消す。
	CoverMediaID optUUID `json:"coverMediaId"`
}

type dateView struct {
	Label    string  `json:"label"`
	Kind     string  `json:"kind"`
	SortDate *string `json:"sortDate"`
	// SortDateIsEstimated: 並び替え用に補った日。開催日として表示してはいけない（D12）
	SortDateIsEstimated bool `json:"sortDateIsEstimated"`
}

type contributorView struct {
	UserID        string  `json:"userId"`
	DisplayName   string  `json:"displayName"`
	RoleLabel     string  `json:"roleLabel"`
	AvatarMediaID *string `json:"avatarMediaId"`
}

type personView struct {
	UserID        string  `json:"userId"`
	DisplayName   string  `json:"displayName"`
	AvatarMediaID *string `json:"avatarMediaId"` // プロフィール画像（ログイン中のメンバーに配信される）。無ければnull
}

func (s *server) avatarOf(ctx context.Context, q *store.Queries, id pgtype.UUID) *string {
	m, err := q.GetAvatarMediaID(ctx, id)
	if err != nil {
		return nil
	}
	return avatarPtr(m)
}

func avatarPtr(m pgtype.UUID) *string {
	if !m.Valid {
		return nil
	}
	s := m.String()
	return &s
}

type revisionView struct {
	// CoverMedia はカバー画像（無ければnull）。publicUrlは本文に書く公開URL（公開されるまで匿名には配信されない）。
	CoverMedia   *mediaView        `json:"coverMedia"`
	RevisionNo   int               `json:"revisionNo"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Markdown     string            `json:"markdown"`
	Metadata     map[string]string `json:"metadata"`
	Date         dateView          `json:"date"`
	EditedBy     string            `json:"editedBy"`
	CreatedAt    time.Time         `json:"createdAt"`
	Contributors []contributorView `json:"contributors,omitempty"`
}

type revisionSummary struct {
	RevisionNo int       `json:"revisionNo"`
	Title      string    `json:"title"`
	EditedBy   string    `json:"editedBy"`
	CreatedAt  time.Time `json:"createdAt"`
}

type contentView struct {
	ID                  string            `json:"id"`
	Type                string            `json:"type"`
	Section             string            `json:"section"`
	Slug                string            `json:"slug"`
	Creator             personView        `json:"creator"`
	WorkingRevision     revisionView      `json:"workingRevision"`
	PublishedRevisionNo *int              `json:"publishedRevisionNo"`
	Collaborators       []personView      `json:"collaborators"`
	Revisions           []revisionSummary `json:"revisions,omitempty"`
	CanManageCollab     bool              `json:"canManageCollaborators"`
	CanPublish          bool              `json:"canPublish"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

// ---- 入力の検査 ----

// editableMetadata は誰でも書ける項目、restrictedMetadata は移行ツールとEditor以上だけが書ける項目。
// 後者は、画像の差し替えなどを防ぐ。**投稿者名（author）は版に保存しない**（D14。legacy_authorsで管理し、公開APIが配信時に解決する）。
var (
	editableMetadata   = map[string]bool{"category": true, "excerpt": true}
	restrictedMetadata = map[string]bool{"image": true, "sourceId": true, "sourceUrl": true}

	slugRe    = cmsrules.SlugRe
	urlLikeRe = regexp.MustCompile(`(?i)(https?:|www\.)`)
)

var hasControl = cmsrules.HasControl

// checkFields は認可の前に行える安い検査（長さ・形式）。Markdownの検査（重い）は含まない。
func checkFields(f *contentFields) error {
	t, sm, err := cmsrules.CheckText(f.Title, f.Summary, f.Markdown, mdcheck.MaxBytes)
	if err != nil {
		return invalid(err.Error())
	}
	f.Title, f.Summary = t, sm
	if f.Date != nil {
		d := cmsrules.Date{Label: f.Date.Label, Kind: f.Date.Kind, SortDate: f.Date.SortDate}
		if err := cmsrules.CheckDate(&d); err != nil {
			return invalid(err.Error())
		}
		f.Date.Label = d.Label
	}
	return nil
}

func validMetadataValue(key, v string) error {
	if err := cmsrules.ValidMetadataValue(key, v); err != nil {
		return invalid(err.Error())
	}
	return nil
}

// mergeMetadata は入力と前の版から、新しい版のmetadataを決める。
//   - 未知のキーは422。誰でも書ける項目は、入力に無ければ消える。
//   - 制限項目（author等）は、入力に無ければ前の版から引き継ぐ。Editor以上だけが変更・削除できる。
//     それ以外の人が**違う値**を指定したら403（同じ値の再送は許す）。
func mergeMetadata(in map[string]*string, prev map[string]string, editor bool) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range prev {
		if restrictedMetadata[k] {
			out[k] = v
		}
	}
	for k, v := range in {
		switch {
		case editableMetadata[k]:
			if v != nil && !(*v == "" && k == "excerpt") {
				if err := validMetadataValue(k, *v); err != nil {
					return nil, err
				}
				out[k] = *v
			}
		case restrictedMetadata[k]:
			if v == nil {
				if !editor {
					if _, had := prev[k]; had {
						return nil, &errTx{http.StatusForbidden, "FORBIDDEN", "この項目は編集者だけが変更できます"}
					}
					continue
				}
				delete(out, k)
				continue
			}
			if !editor {
				if prev[k] != *v {
					return nil, &errTx{http.StatusForbidden, "FORBIDDEN", "この項目は編集者だけが変更できます"}
				}
				continue
			}
			if err := validMetadataValue(k, *v); err != nil {
				return nil, err
			}
			out[k] = *v
		default:
			return nil, invalid("使えないメタデータの項目があります")
		}
	}
	return out, nil
}

type revData struct {
	Title, Summary, Markdown string
	Metadata                 map[string]string
	DateLabel, DateKind      string
	SortDate                 pgtype.Date
	Estimated                bool
	Cover                    pgtype.UUID
}

func dateFromInput(d *dateIn, prev *store.ContentRevision) (label, kind string, sd pgtype.Date, est bool) {
	switch {
	case d != nil:
		label, kind = d.Label, d.Kind
		if d.SortDate != nil {
			t, _ := time.Parse("2006-01-02", *d.SortDate)
			sd = pgtype.Date{Time: t, Valid: true}
		}
		est = kind != "day" && sd.Valid
	case prev != nil:
		label, kind, sd, est = prev.DateLabel, prev.DateKind, prev.SortDate, prev.SortDateIsEstimated
	default:
		kind = "unknown"
	}
	return
}

func decodeMetadata(b []byte) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(b, &m)
	return m
}

func sameMetadata(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func sameRevision(d revData, r store.ContentRevision) bool {
	return d.Title == r.Title && d.Summary == r.Summary && d.Markdown == r.Markdown && d.DateLabel == r.DateLabel && d.DateKind == r.DateKind &&
		d.SortDate == r.SortDate && d.Estimated == r.SortDateIsEstimated && d.Cover == r.CoverMediaID && sameMetadata(d.Metadata, decodeMetadata(r.MetadataJson))
}

// checkMarkdown はMarkdownを検査し、画像の参照（public_id）を返す。違反は利用者に見せてよい文言にまとめる（本文の内容は含めない）。
func checkMarkdown(src string) ([]string, error) {
	res := mdcheck.Check(src)
	vs := res.Violations
	if len(vs) == 0 {
		return res.MediaPublicIDs, nil
	}
	parts := make([]string, 0, 3)
	for i, v := range vs {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("ほか%d件", len(vs)-3))
			break
		}
		parts = append(parts, v.String())
	}
	return nil, invalid("本文に使えない記述があります: " + strings.Join(parts, " / "))
}

// markdownError は検査だけ行う（画像の参照は使わない場合）。
func markdownError(src string) error {
	_, err := checkMarkdown(src)
	return err
}

// maxMediaRefs は1つの版が参照できる画像の数（容量の水増し・掃除できない画像の積み上がりを防ぐ）。
const maxMediaRefs = 100

var errMediaNotFound = invalid("画像が見つかりません。アップロードし直してください")

// resolveMedia は本文の画像（public_id）とカバー画像を検査して解決し、行を共有ロックする（孤児の掃除との競合を避ける）。
// 参照できる画像: 存在し、redactedでなく、purpose=cmsで、(a)自分がアップロードした (b)このコンテンツの過去の版が既に参照している (c)Editor以上。
// 条件に合わなければ、存在の有無を区別せず同じ422。
func (s *server) resolveMedia(r *http.Request, q *store.Queries, a authCtx, contentID pgtype.UUID, publicIDs []string, cover optUUID, prevCover pgtype.UUID) (inline []pgtype.UUID, coverID pgtype.UUID, err error) {
	ctx := r.Context()
	coverID = prevCover
	switch {
	case cover.Set && cover.Null:
		coverID = pgtype.UUID{}
	case cover.Set:
		var u pgtype.UUID
		if e := u.Scan(cover.Val); e != nil || !u.Valid {
			return nil, coverID, errMediaNotFound
		}
		coverID = u
	}
	if len(publicIDs)+1 > maxMediaRefs {
		return nil, coverID, invalid("1つの版で使える画像は100枚までです")
	}
	editor := a.Identity.Subject().IsEditorPlus()
	allowed := func(id pgtype.UUID, uploadedBy pgtype.UUID, purpose string, redacted pgtype.Timestamptz) (bool, error) {
		if purpose != "cms" || redacted.Valid {
			return false, nil
		}
		if editor || uploadedBy == a.Identity.UserID {
			return true, nil
		}
		if contentID.Valid {
			return q.MediaReferencedByContent(ctx, store.MediaReferencedByContentParams{MediaID: id, ContentID: contentID})
		}
		return false, nil
	}
	if len(publicIDs) > 0 {
		rows, e := q.LockMediaByPublicIDs(ctx, publicIDs)
		if e != nil {
			return nil, coverID, e
		}
		if len(rows) != len(publicIDs) {
			return nil, coverID, errMediaNotFound
		}
		for _, m := range rows {
			ok, e := allowed(m.ID, m.UploadedBy, m.Purpose, m.RedactedAt)
			if e != nil {
				return nil, coverID, e
			}
			if !ok {
				return nil, coverID, errMediaNotFound
			}
			inline = append(inline, m.ID)
		}
	}
	if coverID.Valid {
		rows, e := q.LockMediaByIDs(ctx, []pgtype.UUID{coverID})
		if e != nil {
			return nil, coverID, e
		}
		if len(rows) != 1 {
			return nil, coverID, errMediaNotFound
		}
		ok, e := allowed(rows[0].ID, rows[0].UploadedBy, rows[0].Purpose, rows[0].RedactedAt)
		if e != nil {
			return nil, coverID, e
		}
		if !ok {
			return nil, coverID, errMediaNotFound
		}
	}
	return inline, coverID, nil
}

// writeMediaRefs は新しい版の画像参照（本文の画像=inline、カバー=cover）を追記する。版は不変なので参照も追記だけ。
func writeMediaRefs(r *http.Request, q *store.Queries, revID pgtype.UUID, inline []pgtype.UUID, cover pgtype.UUID) error {
	for _, id := range inline {
		if err := q.InsertMediaReference(r.Context(), store.InsertMediaReferenceParams{MediaID: id, OwnerID: revID, Purpose: "inline"}); err != nil {
			return err
		}
	}
	if cover.Valid {
		return q.InsertMediaReference(r.Context(), store.InsertMediaReferenceParams{MediaID: cover, OwnerID: revID, Purpose: "cover"})
	}
	return nil
}

// ---- 認可 ----

// memberOrReject は認証済みの有効なユーザーだけを通す（未認証は401）。関係が必要な操作の最初の入口。
func (s *server) memberOrReject(w http.ResponseWriter, r *http.Request) (authCtx, bool) {
	return s.authorize(w, r, "contents.list", authz.Resource{})
}

// contentResource は作者・共同編集者の関係をDBから読む（毎回。キャッシュしない）。
func contentResource(r *http.Request, q *store.Queries, a authCtx, c store.Content) (authz.Resource, error) {
	res := authz.Resource{IsAuthor: c.CreatorUserID == a.Identity.UserID}
	if !res.IsAuthor {
		ok, err := q.IsContentCollaborator(r.Context(), store.IsContentCollaboratorParams{ContentID: c.ID, UserID: a.Identity.UserID})
		if err != nil {
			return res, err
		}
		res.IsCollaborator = ok
	}
	return res, nil
}

// lockContent は contents行をロックして関係を読み直し、actionが許されなければ、閲覧できない人には404（存在しないのと同じ）、閲覧できる人には403を返す。
func (s *server) lockContent(r *http.Request, q *store.Queries, a authCtx, id pgtype.UUID, action string) (store.Content, error) {
	c, err := q.GetContentForUpdate(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return c, errNotFound
		}
		return c, err
	}
	res, err := contentResource(r, q, a, c)
	if err != nil {
		return c, err
	}
	subject := a.Identity.Subject()
	if d := authz.Decide(subject, action, res); !d.Allowed {
		// 閲覧できない人には存在しないのと同じ404。閲覧できる人（共同編集者が共同編集者を指定しようとした等）には403
		if authz.Decide(subject, "contents.get", res).Allowed {
			return c, &errTx{http.StatusForbidden, "FORBIDDEN", "この操作を行う権限がありません"}
		}
		return c, errNotFound
	}
	return c, nil
}

func (s *server) saveAllowed(w http.ResponseWriter, r *http.Request, a authCtx) bool {
	ok, retry := s.saves.allow(a.Identity.UserID.String(), s.Now())
	if ok {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	httpx.Error(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "保存の回数が多すぎます。しばらくしてからお試しください")
	return false
}

// ---- 表示 ----

func toRevisionView(r store.ContentRevision, editedBy string, contribs []store.ListWorkContributorsRow) revisionView {
	v := revisionView{RevisionNo: int(r.RevisionNo), Title: r.Title, Summary: r.Summary, Markdown: r.Markdown, Metadata: decodeMetadata(r.MetadataJson),
		Date: dateView{Label: r.DateLabel, Kind: r.DateKind, SortDateIsEstimated: r.SortDateIsEstimated}, EditedBy: editedBy, CreatedAt: r.CreatedAt.Time}
	if r.SortDate.Valid {
		d := r.SortDate.Time.Format("2006-01-02")
		v.Date.SortDate = &d
	}
	for _, c := range contribs {
		v.Contributors = append(v.Contributors, contributorView{UserID: c.UserID.String(), DisplayName: c.DisplayName, RoleLabel: c.RoleLabel, AvatarMediaID: avatarPtr(c.AvatarMediaID)})
	}
	return v
}

func withCover(v revisionView, c *mediaView) revisionView {
	v.CoverMedia = c
	return v
}

// loadContentView は作業版・共同編集者などを組み立てる（閲覧権限は呼び出し元が確認済み）。
func (s *server) loadContentView(r *http.Request, q *store.Queries, a authCtx, c store.Content, res authz.Resource, withHistory bool, rev *store.ContentRevision) (contentView, error) {
	ctx := r.Context()
	if rev == nil {
		w, err := q.GetContentRevision(ctx, c.WorkingRevisionID)
		if err != nil {
			return contentView{}, err
		}
		rev = &w
	}
	contribs, err := q.ListWorkContributors(ctx, rev.ID)
	if err != nil {
		return contentView{}, err
	}
	collabs, err := q.ListContentCollaborators(ctx, c.ID)
	if err != nil {
		return contentView{}, err
	}
	var cover *mediaView
	if rev.CoverMediaID.Valid {
		if m, err := q.GetMediaAsset(ctx, rev.CoverMediaID); err == nil && !m.RedactedAt.Valid {
			mv := toMediaView(m)
			cover = &mv
		}
	}
	v := contentView{
		ID: c.ID.String(), Type: c.Type, Section: c.Section, Slug: c.Slug,
		Creator:         personView{UserID: c.CreatorUserID.String(), DisplayName: s.displayName(ctx, q, c.CreatorUserID), AvatarMediaID: s.avatarOf(ctx, q, c.CreatorUserID)},
		WorkingRevision: withCover(toRevisionView(*rev, s.displayName(ctx, q, rev.EditedBy), contribs), cover),
		Collaborators:   []personView{},
		CreatedAt:       c.CreatedAt.Time, UpdatedAt: c.UpdatedAt.Time,
	}
	for _, cl := range collabs {
		v.Collaborators = append(v.Collaborators, personView{UserID: cl.UserID.String(), DisplayName: cl.DisplayName, AvatarMediaID: avatarPtr(cl.AvatarMediaID)})
	}
	subject := a.Identity.Subject()
	v.CanManageCollab = authz.Decide(subject, "contents.collaborators.update", res).Allowed
	v.CanPublish = authz.Decide(subject, "contents.publish", res).Allowed
	if c.PublishedRevisionID.Valid {
		p, err := q.GetContentRevision(ctx, c.PublishedRevisionID)
		if err != nil {
			return contentView{}, err
		}
		n := int(p.RevisionNo)
		v.PublishedRevisionNo = &n
	}
	if withHistory {
		hist, err := q.ListContentRevisionSummaries(ctx, c.ID)
		if err != nil {
			return contentView{}, err
		}
		for _, h := range hist {
			v.Revisions = append(v.Revisions, revisionSummary{RevisionNo: int(h.RevisionNo), Title: h.Title, EditedBy: h.EditedByName, CreatedAt: h.CreatedAt.Time})
		}
	}
	return v, nil
}

// ---- 作成 ----

// createContent は POST /contents（Member）。下書きを作る（版1）。公開はしない。
func (s *server) createContent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "contents.create", authz.Resource{})
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContentBody)
	var req struct {
		Type    string `json:"type"`
		Section string `json:"section"`
		Slug    string `json:"slug"`
		contentFields
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !s.saveAllowed(w, r, a) {
		return
	}
	if err := checkFields(&req.contentFields); err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	if !slugRe.MatchString(req.Slug) {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "URL用の名前（slug）は英数字・.・_・-で、100文字までにしてください")
		return
	}
	if !(req.Type == "article" && (req.Section == "blog" || req.Section == "news")) && !(req.Type == "project" && req.Section == "project") {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "種別と区分の組み合わせが正しくありません")
		return
	}
	editor := a.Identity.Subject().IsEditorPlus()
	var out contentView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		// 重い検査は、認証済み・入力の形式確認の後に行う
		mediaIDs, err := checkMarkdown(req.Markdown)
		if err != nil {
			return err
		}
		meta, err := mergeMetadata(req.Metadata, nil, editor)
		if err != nil {
			return err
		}
		inline, coverID, err := s.resolveMedia(r, q, a, pgtype.UUID{}, mediaIDs, req.CoverMediaID, pgtype.UUID{})
		if err != nil {
			return err
		}
		label, kind, sd, est := dateFromInput(req.Date, nil)
		if _, err := tx.Exec(r.Context(), `SET CONSTRAINTS contents_working_revision_fk, contents_published_revision_fk, content_revisions_content_id_fkey DEFERRED`); err != nil {
			return err
		}
		contentID, err := q.NewUUID(r.Context())
		if err != nil {
			return err
		}
		revID, err := q.NewUUID(r.Context())
		if err != nil {
			return err
		}
		c, err := q.CreateContent(r.Context(), store.CreateContentParams{ID: contentID, Type: req.Type, Section: req.Section, Slug: req.Slug, CreatorUserID: a.Identity.UserID, WorkingRevisionID: revID})
		if err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				return &errTx{http.StatusConflict, "SLUG_TAKEN", "このURL用の名前（slug）は既に使われています"}
			}
			return err
		}
		mj, _ := json.Marshal(meta)
		rev, err := q.InsertContentRevision(r.Context(), store.InsertContentRevisionParams{ID: revID, ContentID: contentID, RevisionNo: 1, Title: req.Title, Summary: req.Summary, Markdown: req.Markdown,
			MetadataJson: mj, DateLabel: label, DateKind: kind, SortDate: sd, SortDateIsEstimated: est, EditedBy: a.Identity.UserID, CoverMediaID: coverID})
		if err != nil {
			return err
		}
		if err := writeMediaRefs(r, q, revID, inline, coverID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `SET CONSTRAINTS contents_working_revision_fk, contents_published_revision_fk, content_revisions_content_id_fkey IMMEDIATE`); err != nil {
			return err
		}
		if err := s.auditContent(r, q, a, c.ID, "content.created", map[string]any{"type": c.Type, "section": c.Section}); err != nil {
			return err
		}
		out, err = s.loadContentView(r, q, a, c, authz.Resource{IsAuthor: true}, false, &rev)
		return err
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

func (s *server) auditContent(r *http.Request, q *store.Queries, a authCtx, id pgtype.UUID, action string, summary map[string]any) error {
	b, _ := json.Marshal(summary)
	return q.InsertAuditLog(r.Context(), store.InsertAuditLogParams{
		ActorUserID: a.Identity.UserID, Action: action, TargetType: pgtype.Text{String: "content", Valid: true},
		TargetID: pgtype.Text{String: id.String(), Valid: true}, SummaryJson: b,
	})
}

// ---- 取得・一覧 ----

// getContent は GET /contents/{id}（作者・共同編集者・Editor以上）。権限が無ければ存在しないのと同じ404。
// ?revision=N で過去の版の内容を返す。
func (s *server) getContent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.memberOrReject(w, r)
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	q := store.New(s.Pool)
	c, err := q.GetContent(r.Context(), id)
	if err != nil {
		httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return
	}
	res, err := contentResource(r, q, a, c)
	if err != nil {
		s.internal(w, r)
		return
	}
	if !authz.Decide(a.Identity.Subject(), "contents.get", res).Allowed {
		httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
		return
	}
	var rev *store.ContentRevision
	if v := r.URL.Query().Get("revision"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "revision が正しくありません")
			return
		}
		got, err := q.GetContentRevisionByNo(r.Context(), store.GetContentRevisionByNoParams{ContentID: c.ID, RevisionNo: int32(n)})
		if err != nil {
			httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
			return
		}
		rev = &got
	}
	v, err := s.loadContentView(r, q, a, c, res, true, rev)
	if err != nil {
		s.internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

type contentListItem struct {
	ID                    string    `json:"id"`
	Type                  string    `json:"type"`
	Section               string    `json:"section"`
	Slug                  string    `json:"slug"`
	Title                 string    `json:"title"`
	Summary               string    `json:"summary"`
	RevisionNo            int       `json:"revisionNo"`
	IsPublished           bool      `json:"isPublished"`
	CanEdit               bool      `json:"canEdit"`
	HasUnpublishedChanges bool      `json:"hasUnpublishedChanges"`
	UpdatedAt             time.Time `json:"updatedAt"`
	// DateLabel は利用者が設定した日付の表示（空なら未設定）。SortDate は並び順の日付（YYYY-MM-DD）。
	DateLabel string  `json:"dateLabel"`
	SortDate  *string `json:"sortDate"`
	// CoverURL はカバー画像の表示URL。編集できる人には作業版の画像（ログイン中のメンバー向けの配信）、そうでない人には公開版の画像（公開URL）。
	// 既存記事は公式サイトのファイル（/images/…。管理ポータルでは中継して表示する）。
	CoverURL *string `json:"coverUrl"`
	// Members は関わるメンバー（作者・共同編集者・表示している版の制作メンバー）。編集できない人には作者と公開版の制作メンバーだけ。
	Members []listMember `json:"members"`
}

type listMember struct {
	UserID        string  `json:"userId"`
	DisplayName   string  `json:"displayName"`
	AvatarMediaID *string `json:"avatarMediaId"`
}

// listContents は GET /contents（Member）。編集できるものは作業版、そうでない公開中のものは**公開版の値だけ**を返す。
func (s *server) listContents(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "contents.list", authz.Resource{})
	if !ok {
		return
	}
	qp := r.URL.Query()
	limit := 30
	if v := qp.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit は1〜100で指定してください")
			return
		}
		limit = n
	}
	q := strings.TrimSpace(qp.Get("q"))
	if utf8.RuneCountInString(q) > 100 {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "検索語が長すぎます")
		return
	}
	status := qp.Get("status")
	if status != "" && status != "published" && status != "draft" {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "status は published か draft を指定してください")
		return
	}
	p := store.ListContentsParams{Q: likeEscape(q), Status: status, IsEditor: a.Identity.Subject().IsEditorPlus(), UserID: a.Identity.UserID, Mine: qp.Get("mine") == "true", Lim: int32(limit + 1)}
	if v := qp.Get("section"); v != "" {
		if v != "blog" && v != "news" && v != "project" {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "section が正しくありません")
			return
		}
		p.Section = pgtype.Text{String: v, Valid: true}
	}
	if c := qp.Get("cursor"); c != "" {
		t, id, ok := decodeTimeCursor(c)
		if !ok {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		p.BeforeAt, p.BeforeID = pgtype.Timestamptz{Time: t, Valid: true}, id
	}
	rows, err := store.New(s.Pool).ListContents(r.Context(), p)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		last := rows[limit-1]
		c := encodeTimeCursor(last.UpdatedAt.Time, last.ID)
		next = &c
		rows = rows[:limit]
	}
	people, err := s.listPeople(r.Context(), store.New(s.Pool), rows)
	if err != nil {
		s.internal(w, r)
		return
	}
	items := make([]contentListItem, 0, len(rows))
	for _, x := range rows {
		it := contentListItem{ID: x.ID.String(), Type: x.Type, Section: x.Section, Slug: x.Slug, Title: x.Title, Summary: x.Summary, RevisionNo: int(x.RevisionNo),
			IsPublished: x.IsPublished, CanEdit: x.CanEdit, HasUnpublishedChanges: x.HasUnpublishedChanges, UpdatedAt: x.UpdatedAt.Time, DateLabel: x.DateLabel,
			Members: people[x.ID.String()]}
		if it.Members == nil {
			it.Members = []listMember{}
		}
		if x.SortDate.Valid {
			d := x.SortDate.Time.Format("2006-01-02")
			it.SortDate = &d
		}
		// 既存記事（移行した記事）のカバーは metadata.image（公式サイトのファイル）。公式サイトでもこちらが優先されるので、一覧でも優先する
		if strings.HasPrefix(x.LegacyImage, "/images/") && !strings.Contains(x.LegacyImage, "..") {
			u := x.LegacyImage
			it.CoverURL = &u
		} else if x.CoverMediaID.Valid {
			var u string
			if x.CanEdit {
				u = "/api/v1/media/" + x.CoverMediaID.String()
			} else if x.CoverPublicID != "" {
				u = "/api/v1/public/media/" + x.CoverPublicID
			}
			if u != "" {
				it.CoverURL = &u
			}
		}
		items = append(items, it)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

// ---- 版の保存 ----

func (s *server) conflict(w http.ResponseWriter, r *http.Request, current revisionView) {
	var b struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId,omitempty"`
		} `json:"error"`
		Current revisionView `json:"current"`
	}
	b.Error.Code, b.Error.Message, b.Error.RequestID = "REVISION_CONFLICT", "他の人が先に保存しました。最新の内容を確認してから保存し直してください", httpx.RequestID(r.Context())
	b.Current = current
	httpx.JSON(w, http.StatusConflict, b)
}

type conflictErr struct{ current revisionView }

func (e *conflictErr) Error() string { return "revision conflict" }

// saveRevision は POST /contents/{id}/revisions（作者・共同編集者・Editor以上）。
// baseRevisionが現在の作業版と違えば409。公開ポインタは変えない（公開は再公開まで旧版のまま）。
func (s *server) saveRevision(w http.ResponseWriter, r *http.Request) {
	a, ok := s.memberOrReject(w, r)
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxContentBody)
	var req struct {
		BaseRevision *int `json:"baseRevision"`
		contentFields
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BaseRevision == nil || *req.BaseRevision < 1 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "baseRevision（編集の元にした版の番号）を指定してください")
		return
	}
	if !s.saveAllowed(w, r, a) {
		return
	}
	if err := checkFields(&req.contentFields); err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	editor := a.Identity.Subject().IsEditorPlus()
	status := http.StatusCreated
	var out contentView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		c, err := s.lockContent(r, q, a, id, "contents.revisions.create")
		if err != nil {
			return err
		}
		cur, err := q.GetContentRevision(r.Context(), c.WorkingRevisionID)
		if err != nil {
			return err
		}
		if int(cur.RevisionNo) != *req.BaseRevision {
			contribs, err := q.ListWorkContributors(r.Context(), cur.ID)
			if err != nil {
				return err
			}
			return &conflictErr{current: toRevisionView(cur, s.displayName(r.Context(), q, cur.EditedBy), contribs)}
		}
		if s.BeforeContentSave != nil {
			s.BeforeContentSave() // テスト専用: 全員が確認を終えた状態で止めて、ロックが無ければ必ず競合させる
		}
		// 認可と行ロックの後に、重い検査を行う
		mediaIDs, err := checkMarkdown(req.Markdown)
		if err != nil {
			return err
		}
		meta, err := mergeMetadata(req.Metadata, decodeMetadata(cur.MetadataJson), editor)
		if err != nil {
			return err
		}
		inline, coverID, err := s.resolveMedia(r, q, a, c.ID, mediaIDs, req.CoverMediaID, cur.CoverMediaID)
		if err != nil {
			return err
		}
		label, kind, sd, est := dateFromInput(req.Date, &cur)
		d := revData{Title: req.Title, Summary: req.Summary, Markdown: req.Markdown, Metadata: meta, DateLabel: label, DateKind: kind, SortDate: sd, Estimated: est, Cover: coverID}
		res, err := contentResource(r, q, a, c)
		if err != nil {
			return err
		}
		if sameRevision(d, cur) { // 内容が同じなら版を増やさない（自動保存の連打）
			status = http.StatusOK
			out, err = s.loadContentView(r, q, a, c, res, false, &cur)
			return err
		}
		newRev, err := s.insertNextRevision(r, q, a, c, cur, d, true, false)
		if err != nil {
			return err
		}
		if err := writeMediaRefs(r, q, newRev.ID, inline, coverID); err != nil {
			return err
		}
		if err := s.auditContent(r, q, a, c.ID, "content.revision_saved", map[string]any{"revisionNo": newRev.RevisionNo}); err != nil {
			return err
		}
		out, err = s.loadContentView(r, q, a, c, res, false, &newRev)
		return err
	})
	var ce *conflictErr
	if errors.As(err, &ce) {
		s.conflict(w, r, ce.current)
		return
	}
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, status, out)
}

// insertNextRevision は新しい版を追加して作業版ポインタを進める（公開ポインタは変えない）。
// copyContributors=trueなら前の版の制作メンバーを、copyRefs=trueなら前の版の画像参照を引き継ぐ（版を作る全経路で参照が欠けないように）。
func (s *server) insertNextRevision(r *http.Request, q *store.Queries, a authCtx, c store.Content, cur store.ContentRevision, d revData, copyContributors, copyRefs bool) (store.ContentRevision, error) {
	revID, err := q.NewUUID(r.Context())
	if err != nil {
		return store.ContentRevision{}, err
	}
	mj, _ := json.Marshal(d.Metadata)
	rev, err := q.InsertContentRevision(r.Context(), store.InsertContentRevisionParams{ID: revID, ContentID: c.ID, RevisionNo: cur.RevisionNo + 1, Title: d.Title, Summary: d.Summary, Markdown: d.Markdown,
		MetadataJson: mj, DateLabel: d.DateLabel, DateKind: d.DateKind, SortDate: d.SortDate, SortDateIsEstimated: d.Estimated, EditedBy: a.Identity.UserID, CoverMediaID: d.Cover})
	if err != nil {
		return rev, err
	}
	if copyRefs {
		if err := q.CopyMediaReferences(r.Context(), store.CopyMediaReferencesParams{ToRevisionID: revID, FromRevisionID: cur.ID}); err != nil {
			return rev, err
		}
	}
	if copyContributors {
		if err := q.CopyWorkContributors(r.Context(), store.CopyWorkContributorsParams{ToRevisionID: revID, FromRevisionID: cur.ID}); err != nil {
			return rev, err
		}
	}
	if err := q.SetWorkingRevision(r.Context(), store.SetWorkingRevisionParams{ID: c.ID, WorkingRevisionID: revID, UpdatedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}}); err != nil {
		return rev, err
	}
	return rev, nil
}

// ---- 共同編集者 ----

const maxCollaborators = 20

// putCollaborators は PUT /contents/{id}/collaborators（作者・Editor以上。共同編集者は不可）。全件置き換え。
// 新しく加えられるのは有効（active）なユーザーだけ。作者本人は無視する。
func (s *server) putCollaborators(w http.ResponseWriter, r *http.Request) {
	a, ok := s.memberOrReject(w, r)
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req struct {
		UserIDs []string `json:"userIds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !s.saveAllowed(w, r, a) {
		return
	}
	if len(req.UserIDs) > maxCollaborators {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", fmt.Sprintf("共同編集者は%d人までです", maxCollaborators))
		return
	}
	want := make([]pgtype.UUID, 0, len(req.UserIDs))
	seen := map[string]bool{}
	for _, raw := range req.UserIDs {
		var u pgtype.UUID
		if err := u.Scan(raw); err != nil || !u.Valid {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "ユーザーIDが正しくありません")
			return
		}
		if !seen[u.String()] {
			seen[u.String()] = true
			want = append(want, u)
		}
	}
	var out []personView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		c, err := s.lockContent(r, q, a, id, "contents.collaborators.update")
		if err != nil {
			return err
		}
		prev, err := q.ListContentCollaborators(r.Context(), c.ID)
		if err != nil {
			return err
		}
		had := map[string]bool{}
		for _, p := range prev {
			had[p.UserID.String()] = true
		}
		var added, final []pgtype.UUID
		for _, u := range want {
			if u == c.CreatorUserID {
				continue // 作者は最初から編集できる
			}
			final = append(final, u)
			if !had[u.String()] {
				added = append(added, u)
			}
		}
		if len(added) > 0 { // 新しく加えるのは有効なユーザーだけ（停止・招待中は不可）
			act, err := q.ActiveUserIDs(r.Context(), added)
			if err != nil {
				return err
			}
			ok := map[string]bool{}
			for _, u := range act {
				ok[u.String()] = true
			}
			for _, u := range added {
				if !ok[u.String()] {
					return invalid("共同編集者に指定できるのは、有効なユーザーだけです")
				}
			}
		}
		if err := q.DeleteContentCollaborators(r.Context(), c.ID); err != nil {
			return err
		}
		for _, u := range final {
			if err := q.InsertContentCollaborator(r.Context(), store.InsertContentCollaboratorParams{ContentID: c.ID, UserID: u, AddedBy: a.Identity.UserID}); err != nil {
				return err
			}
		}
		if err := s.auditContent(r, q, a, c.ID, "content.collaborators_updated", map[string]any{"count": len(final)}); err != nil {
			return err
		}
		list, err := q.ListContentCollaborators(r.Context(), c.ID)
		if err != nil {
			return err
		}
		out = make([]personView, 0, len(list))
		for _, p := range list {
			out = append(out, personView{UserID: p.UserID.String(), DisplayName: p.DisplayName, AvatarMediaID: avatarPtr(p.AvatarMediaID)})
		}
		return nil
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"collaborators": out})
}

// ---- 制作メンバー ----

const maxContributors = 50

type contributorIn struct {
	UserID    string `json:"userId"`
	RoleLabel string `json:"roleLabel"`
}

// putContributors は PUT /contents/{id}/contributors（作者・共同編集者・Editor以上）。作品だけ。
// 制作メンバーは版ごとに持つため、変更は**新しい版**になる（公開版には影響しない）。baseRevisionが必要。
// 公開クレジットの同意は変えない（本人だけが別のAPIで変更する）。
func (s *server) putContributors(w http.ResponseWriter, r *http.Request) {
	a, ok := s.memberOrReject(w, r)
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	var req struct {
		BaseRevision *int            `json:"baseRevision"`
		Contributors []contributorIn `json:"contributors"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.BaseRevision == nil || *req.BaseRevision < 1 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "baseRevision（編集の元にした版の番号）を指定してください")
		return
	}
	if len(req.Contributors) > maxContributors {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", fmt.Sprintf("制作メンバーは%d件までです", maxContributors))
		return
	}
	if !s.saveAllowed(w, r, a) {
		return
	}
	type cm struct {
		user pgtype.UUID
		role string
	}
	list := make([]cm, 0, len(req.Contributors))
	pair := map[string]bool{}
	ids := make([]pgtype.UUID, 0, len(req.Contributors))
	for _, c := range req.Contributors {
		var u pgtype.UUID
		role := strings.TrimSpace(c.RoleLabel)
		if err := u.Scan(c.UserID); err != nil || !u.Valid {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "ユーザーIDが正しくありません")
			return
		}
		if role == "" || utf8.RuneCountInString(role) > 50 || hasControl(role, false) || strings.ContainsAny(role, "@<>") || urlLikeRe.MatchString(role) {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "役割は1〜50文字で、URLや記号（@ < >）を含めないでください")
			return
		}
		k := u.String() + "|" + role
		if pair[k] {
			httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "同じ人の同じ役割が重複しています")
			return
		}
		pair[k] = true
		list = append(list, cm{u, role})
		ids = append(ids, u)
	}
	status := http.StatusCreated
	var out contentView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		c, err := s.lockContent(r, q, a, id, "contents.contributors.update")
		if err != nil {
			return err
		}
		if c.Type != "project" {
			return invalid("制作メンバーを登録できるのは作品だけです")
		}
		cur, err := q.GetContentRevision(r.Context(), c.WorkingRevisionID)
		if err != nil {
			return err
		}
		curContribs, err := q.ListWorkContributors(r.Context(), cur.ID)
		if err != nil {
			return err
		}
		if int(cur.RevisionNo) != *req.BaseRevision {
			return &conflictErr{current: toRevisionView(cur, s.displayName(r.Context(), q, cur.EditedBy), curContribs)}
		}
		if len(ids) > 0 { // 停止・退会済みでも登録できる（D08）。存在しないIDだけ拒否
			exist, err := q.ExistingUserIDs(r.Context(), ids)
			if err != nil {
				return err
			}
			have := map[string]bool{}
			for _, u := range exist {
				have[u.String()] = true
			}
			for _, u := range ids {
				if !have[u.String()] {
					return invalid("存在しないユーザーが含まれています")
				}
			}
		}
		res, err := contentResource(r, q, a, c)
		if err != nil {
			return err
		}
		same := len(curContribs) == len(list)
		if same {
			for i, cc := range curContribs {
				if cc.UserID != list[i].user || cc.RoleLabel != list[i].role {
					same = false
					break
				}
			}
		}
		if same { // 変更なしなら新しい版を作らない
			status = http.StatusOK
			out, err = s.loadContentView(r, q, a, c, res, false, &cur)
			return err
		}
		d := revData{Title: cur.Title, Summary: cur.Summary, Markdown: cur.Markdown, Metadata: decodeMetadata(cur.MetadataJson),
			DateLabel: cur.DateLabel, DateKind: cur.DateKind, SortDate: cur.SortDate, Estimated: cur.SortDateIsEstimated, Cover: cur.CoverMediaID}
		newRev, err := s.insertNextRevision(r, q, a, c, cur, d, false, true)
		if err != nil {
			return err
		}
		for i, cc := range list {
			if err := q.InsertWorkContributor(r.Context(), store.InsertWorkContributorParams{ContentID: c.ID, RevisionID: newRev.ID, UserID: cc.user, RoleLabel: cc.role, SortOrder: int32(i), AddedBy: a.Identity.UserID}); err != nil {
				return err
			}
		}
		if err := s.auditContent(r, q, a, c.ID, "content.contributors_updated", map[string]any{"revisionNo": newRev.RevisionNo, "count": len(list)}); err != nil {
			return err
		}
		out, err = s.loadContentView(r, q, a, c, res, false, &newRev)
		return err
	})
	var ce *conflictErr
	if errors.As(err, &ce) {
		s.conflict(w, r, ce.current)
		return
	}
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, status, out)
}

// listPeople は、一覧の各コンテンツの関わるメンバー（作者・共同編集者・表示する版の制作メンバー）を集める。
// 共同編集者は編集できる人にだけ見せる（編集できない人には作者と、公開版の制作メンバーだけ）。
func (s *server) listPeople(ctx context.Context, q *store.Queries, rows []store.ListContentsRow) (map[string][]listMember, error) {
	out := map[string][]listMember{}
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]pgtype.UUID, 0, len(rows))
	revs := make([]pgtype.UUID, 0, len(rows))
	canEdit := map[string]bool{}
	revOf := map[string]string{}
	for _, x := range rows {
		ids = append(ids, x.ID)
		revs = append(revs, x.ShownRevisionID)
		canEdit[x.ID.String()] = x.CanEdit
		revOf[x.ShownRevisionID.String()] = x.ID.String()
	}
	order := map[string][]pgtype.UUID{} // コンテンツID → ユーザーID（重複なし・作者が先頭）
	seen := map[string]map[string]bool{}
	add := func(content string, u pgtype.UUID) {
		if seen[content] == nil {
			seen[content] = map[string]bool{}
		}
		if !seen[content][u.String()] {
			seen[content][u.String()] = true
			order[content] = append(order[content], u)
		}
	}
	ppl, err := q.ListContentPeople(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, p := range ppl {
		c := p.ContentID.String()
		if p.Ord == 0 || canEdit[c] {
			add(c, p.UserID)
		}
	}
	cs, err := q.ListContributorUsers(ctx, revs)
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		add(revOf[c.RevisionID.String()], c.UserID)
	}
	var all []pgtype.UUID
	for _, us := range order {
		all = append(all, us...)
	}
	cards, err := q.ListProfileCards(ctx, all)
	if err != nil {
		return nil, err
	}
	byUser := map[string]store.ListProfileCardsRow{}
	for _, c := range cards {
		byUser[c.UserID.String()] = c
	}
	for c, us := range order {
		for _, u := range us {
			card, ok := byUser[u.String()]
			if !ok {
				continue
			}
			m := listMember{UserID: u.String(), DisplayName: card.DisplayName}
			if card.AvatarMediaID.Valid {
				a := card.AvatarMediaID.String()
				m.AvatarMediaID = &a
			}
			out[c] = append(out[c], m)
		}
	}
	return out, nil
}
