package api

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 公開・公開停止（Editor以上のみ。作者・共同編集者でも不可。D01）。設計: docs/design/p3-cms.md §4。
//
//   - publishは**revisionNo必須**。ロック後に作業版と一致しなければ409（確認していない版は公開しない）。
//   - 権限は、行ロックの後にトランザクション内でロールを読み直して確認する（降格との競合を避ける）。
//   - 公開すると、その版が持つ制作メンバーがそのまま公開される（名前は配信時に同意と結合して解決）。

// isEditorPlusInTx はTx内でロールを読み直す（共有ロック）。
func isEditorPlusInTx(r *http.Request, q *store.Queries, userID pgtype.UUID) (bool, error) {
	roles, err := q.ActorRolesForShare(r.Context(), userID)
	if err != nil {
		return false, err
	}
	for _, role := range roles {
		if role == "editor" || role == "admin" || role == "owner" {
			return true, nil
		}
	}
	return false, nil
}

var errForbidden = &errTx{http.StatusForbidden, "FORBIDDEN", "この操作を行う権限がありません"}

// publishContent は POST /contents/{id}/publish。
func (s *server) publishContent(w http.ResponseWriter, r *http.Request) {
	// 存在確認より先にロールだけで判定する（存在するIDとしないIDで応答が変わらない）
	a, ok := s.authorize(w, r, "contents.publish", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req struct {
		RevisionNo *int `json:"revisionNo"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RevisionNo == nil || *req.RevisionNo < 1 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "revisionNo（公開する版の番号）を指定してください")
		return
	}
	status := http.StatusOK
	var out contentView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		c, err := q.GetContentForUpdate(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		if ok, err := isEditorPlusInTx(r, q, a.Identity.UserID); err != nil {
			return err
		} else if !ok {
			return errForbidden
		}
		work, err := q.GetContentRevision(r.Context(), c.WorkingRevisionID)
		if err != nil {
			return err
		}
		if int(work.RevisionNo) != *req.RevisionNo {
			contribs, err := q.ListWorkContributors(r.Context(), work.ID)
			if err != nil {
				return err
			}
			return &conflictErr{current: toRevisionView(work, s.displayName(r.Context(), q, work.EditedBy), contribs)}
		}
		res := authz.Resource{}
		if c.PublishedRevisionID == work.ID { // 既に公開中の同じ版: 何もしない（イベント・通知を重ねない）
			out, err = s.loadContentView(r, q, a, c, res, false, &work)
			return err
		}
		if err := markdownError(work.Markdown); err != nil { // 公開の直前にも検査する（多層防御）
			return err
		}
		if err := q.SetPublishedRevision(r.Context(), store.SetPublishedRevisionParams{ID: c.ID, PublishedRevisionID: work.ID, UpdatedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}}); err != nil {
			return err
		}
		evID, err := q.InsertPublicationEvent(r.Context(), store.InsertPublicationEventParams{ContentID: c.ID, RevisionID: work.ID, ActorUserID: a.Identity.UserID, EventType: "publish"})
		if err != nil {
			return err
		}
		if err := s.auditContent(r, q, a, c.ID, "content.published", map[string]any{"revisionNo": work.RevisionNo}); err != nil {
			return err
		}
		// 公開の通知は、公開と同じトランザクションでoutboxへ（タイトルと種別だけ。再公開も別イベントとして通知）
		if err := s.enqueueNotification(r.Context(), q, EventContentPublished, "publish:"+evID.String(), NotificationPayload{ItemName: work.Title, Target: c.Type}); err != nil {
			return err
		}
		c.PublishedRevisionID = work.ID
		out, err = s.loadContentView(r, q, a, c, res, false, &work)
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

// unpublishContent は POST /contents/{id}/unpublish。公開ポインタを外す（作業版・版の履歴は残る）。
func (s *server) unpublishContent(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "contents.unpublish", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var out contentView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		c, err := q.GetContentForUpdate(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errNotFound
			}
			return err
		}
		if ok, err := isEditorPlusInTx(r, q, a.Identity.UserID); err != nil {
			return err
		} else if !ok {
			return errForbidden
		}
		if c.PublishedRevisionID.Valid { // 公開中でなければ何もしない
			prev := c.PublishedRevisionID
			if err := q.SetPublishedRevision(r.Context(), store.SetPublishedRevisionParams{ID: c.ID, PublishedRevisionID: pgtype.UUID{}, UpdatedAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}}); err != nil {
				return err
			}
			if _, err := q.InsertPublicationEvent(r.Context(), store.InsertPublicationEventParams{ContentID: c.ID, RevisionID: prev, ActorUserID: a.Identity.UserID, EventType: "unpublish"}); err != nil {
				return err
			}
			if err := s.auditContent(r, q, a, c.ID, "content.unpublished", map[string]any{}); err != nil {
				return err
			}
			c.PublishedRevisionID = pgtype.UUID{}
		}
		out, err = s.loadContentView(r, q, a, c, authz.Resource{}, false, nil)
		return err
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
