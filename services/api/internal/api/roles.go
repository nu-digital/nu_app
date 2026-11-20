package api

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

type roleRequest struct {
	Enabled *bool `json:"enabled"`
}

type roleResponse struct {
	ID      string   `json:"id"`
	LoginID string   `json:"loginId"`
	Roles   []string `json:"roles"`
}

// setRole はEditor/Adminロールの付与・解除（冪等）。Owner権限はこのAPIでは扱わない（移譲・緊急復旧は別手順）。
// ロールは毎回DBから読まれるため、対象の既存セッションにも次のリクエストで反映される。
func (s *server) setRole(w http.ResponseWriter, r *http.Request, action, role, auditPrefix string) {
	a, ok := s.authorize(w, r, action, authz.Resource{})
	if !ok {
		return
	}
	target, ok := parseUserID(w, r)
	if !ok {
		return
	}
	var req roleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_REQUEST", "enabled に true か false を指定してください")
		return
	}
	var out roleResponse
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		u, err := q.GetUserForUpdate(r.Context(), target)
		if err != nil {
			if err == pgx.ErrNoRows {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		if *req.Enabled {
			n, err := q.GrantRoleIfMissing(r.Context(), store.GrantRoleIfMissingParams{UserID: target, Role: role, AssignedBy: a.Identity.UserID})
			if err != nil {
				return err
			}
			if n == 1 {
				if err := s.audit(r.Context(), q, a.Identity.UserID, target, auditPrefix+"_granted", u.LoginID); err != nil {
					return err
				}
			}
		} else {
			n, err := q.RevokeRole(r.Context(), store.RevokeRoleParams{UserID: target, Role: role})
			if err != nil {
				return err
			}
			if n == 1 {
				if err := s.audit(r.Context(), q, a.Identity.UserID, target, auditPrefix+"_revoked", u.LoginID); err != nil {
					return err
				}
			}
		}
		roles, err := q.ListUserRoles(r.Context(), target)
		if err != nil {
			return err
		}
		if roles == nil {
			roles = []string{}
		}
		out = roleResponse{ID: target.String(), LoginID: u.LoginID, Roles: roles}
		return nil
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (s *server) editorRole(w http.ResponseWriter, r *http.Request) {
	s.setRole(w, r, "admin.users.editor_role", "editor", "admin.editor_role")
}

func (s *server) adminRole(w http.ResponseWriter, r *http.Request) {
	s.setRole(w, r, "owner.users.admin_role", "admin", "owner.admin_role")
}
