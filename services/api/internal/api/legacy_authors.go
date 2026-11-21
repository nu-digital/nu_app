package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 既存記事の投稿者（D14）の管理。公開許可の確認・撤回と、ユーザーへの紐付けは**別の操作**で、互いに状態を変えない。
// 監査には名前・理由を入れない（種類とIDだけ）。

func (s *server) legacyAudit(ctx context.Context, q *store.Queries, actor, authorID pgtype.UUID, action string, summary map[string]any) error {
	b, _ := json.Marshal(summary)
	return q.InsertAuditLog(ctx, store.InsertAuditLogParams{
		ActorUserID: actor, Action: action, TargetType: pgtype.Text{String: "legacy_author", Valid: true},
		TargetID: pgtype.Text{String: authorID.String(), Valid: true}, SummaryJson: b,
	})
}

// listLegacyAuthors は GET /admin/legacy-authors（確認一覧。未確認が先頭）。
func (s *server) listLegacyAuthors(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "admin.legacy_authors.list", authz.Resource{}); !ok {
		return
	}
	rows, err := store.New(s.Pool).ListLegacyAuthors(r.Context())
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	type item struct {
		ID                string     `json:"id"`
		Name              string     `json:"name"`
		State             string     `json:"publicationState"`
		ConfirmedAt       *time.Time `json:"confirmedAt"`
		WithdrawnAt       *time.Time `json:"withdrawnAt"`
		Erased            bool       `json:"erased"`
		LinkedUserID      *string    `json:"linkedUserId"`
		LinkedDisplayName *string    `json:"linkedDisplayName"`
		ContentCount      int32      `json:"contentCount"`
	}
	out := make([]item, 0, len(rows))
	for _, a := range rows {
		it := item{ID: a.ID.String(), Name: a.SourceName, State: a.PublicationState, Erased: a.ErasedAt.Valid, ContentCount: a.ContentCount}
		if a.LinkedUserID.Valid {
			n := a.LinkedDisplayName
			it.LinkedDisplayName = &n
		}
		if a.ConfirmedAt.Valid {
			t := a.ConfirmedAt.Time
			it.ConfirmedAt = &t
		}
		if a.WithdrawnAt.Valid {
			t := a.WithdrawnAt.Time
			it.WithdrawnAt = &t
		}
		if a.LinkedUserID.Valid {
			u := a.LinkedUserID.String()
			it.LinkedUserID = &u
		}
		out = append(out, it)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": out})
}

// legacyAuthorAction は投稿者を行ロックして fn を実行し、監査を残す。
func (s *server) legacyAuthorAction(w http.ResponseWriter, r *http.Request, rule, action string, fn func(q *store.Queries, a store.LegacyAuthor, actor pgtype.UUID, now pgtype.Timestamptz) (map[string]any, error)) {
	a, ok := s.authorize(w, r, rule, authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseUserID(w, r)
	if !ok {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		la, err := q.GetLegacyAuthorForUpdate(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		sum, err := fn(q, la, a.Identity.UserID, pgtype.Timestamptz{Time: s.Now(), Valid: true})
		if err != nil {
			return err
		}
		return s.legacyAudit(r.Context(), q, a.Identity.UserID, id, action, sum)
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// permitLegacyAuthor は POST /admin/legacy-authors/{id}/permit。本人の公開許可を確認して記録する。
func (s *server) permitLegacyAuthor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	note := strings.TrimSpace(req.Note)
	if note == "" || utf8.RuneCountInString(note) > 200 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "どのように本人の許可を確認したかを200文字以内で入力してください（個人情報は書かないでください）")
		return
	}
	s.legacyAuthorAction(w, r, "admin.legacy_authors.permit", "legacy_author.permitted", func(q *store.Queries, la store.LegacyAuthor, actor pgtype.UUID, now pgtype.Timestamptz) (map[string]any, error) {
		if la.ErasedAt.Valid {
			return nil, &errTx{http.StatusConflict, "ERASED", "個人情報を削除済みの投稿者は公開できません"}
		}
		if _, err := q.PermitLegacyAuthor(r.Context(), store.PermitLegacyAuthorParams{ID: la.ID, ConfirmedBy: actor, ConfirmedAt: now, ConfirmNote: pgtype.Text{String: note, Valid: true}}); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

// withdrawLegacyAuthor は POST /admin/legacy-authors/{id}/withdraw。公開を止める（冪等）。
func (s *server) withdrawLegacyAuthor(w http.ResponseWriter, r *http.Request) {
	s.legacyAuthorAction(w, r, "admin.legacy_authors.withdraw", "legacy_author.withdrawn", func(q *store.Queries, la store.LegacyAuthor, _ pgtype.UUID, now pgtype.Timestamptz) (map[string]any, error) {
		if _, err := q.WithdrawLegacyAuthor(r.Context(), store.WithdrawLegacyAuthorParams{ID: la.ID, WithdrawnAt: now}); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

// linkLegacyAuthor は PUT /admin/legacy-authors/{id}/link {"userId"}。ユーザーIDを明示して紐付ける（表示名では紐付けない）。公開可否は変えない。
func (s *server) linkLegacyAuthor(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"userId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var uid pgtype.UUID
	if err := uid.Scan(req.UserID); err != nil || !uid.Valid {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "userIdが不正です")
		return
	}
	s.legacyAuthorAction(w, r, "admin.legacy_authors.link", "legacy_author.linked", func(q *store.Queries, la store.LegacyAuthor, actor pgtype.UUID, now pgtype.Timestamptz) (map[string]any, error) {
		if la.ErasedAt.Valid {
			return nil, &errTx{http.StatusConflict, "ERASED", "個人情報を削除済みの投稿者には紐付けできません"}
		}
		if _, err := q.GetUserByID(r.Context(), uid); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, &errTx{http.StatusUnprocessableEntity, "INVALID_INPUT", "ユーザーが見つかりません"}
			}
			return nil, err
		}
		if _, err := q.LinkLegacyAuthor(r.Context(), store.LinkLegacyAuthorParams{ID: la.ID, LinkedUserID: uid, LinkedBy: actor, LinkedAt: now}); err != nil {
			return nil, err
		}
		return map[string]any{"userId": uid.String()}, nil
	})
}

// unlinkLegacyAuthor は DELETE /admin/legacy-authors/{id}/link。公開可否は変えない。
func (s *server) unlinkLegacyAuthor(w http.ResponseWriter, r *http.Request) {
	s.legacyAuthorAction(w, r, "admin.legacy_authors.unlink", "legacy_author.unlinked", func(q *store.Queries, la store.LegacyAuthor, _ pgtype.UUID, _ pgtype.Timestamptz) (map[string]any, error) {
		if _, err := q.UnlinkLegacyAuthor(r.Context(), la.ID); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	})
}

// withdrawLegacyAuthorsOf は、ユーザーの公開クレジット同意の撤回に連動して、紐付く投稿者の公開を止める（同じトランザクション）。
func (s *server) withdrawLegacyAuthorsOf(ctx context.Context, q *store.Queries, actor, user pgtype.UUID) error {
	ids, err := q.WithdrawLegacyAuthorsOfUser(ctx, store.WithdrawLegacyAuthorsOfUserParams{LinkedUserID: user, WithdrawnAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.legacyAudit(ctx, q, actor, id, "legacy_author.withdrawn_by_consent", map[string]any{}); err != nil {
			return err
		}
	}
	return nil
}

// eraseLegacyAuthorsOf は、個人情報の削除に連動して、紐付く投稿者の名前を匿名の記号に置換する（同じトランザクション）。
func (s *server) eraseLegacyAuthorsOf(ctx context.Context, q *store.Queries, actor, user pgtype.UUID) error {
	ids, err := q.EraseLegacyAuthorsOfUser(ctx, store.EraseLegacyAuthorsOfUserParams{LinkedUserID: user, ErasedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.legacyAudit(ctx, q, actor, id, "legacy_author.erased", map[string]any{}); err != nil {
			return err
		}
	}
	return nil
}
