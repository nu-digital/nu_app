package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// anonymizedName は個人情報の削除時に、希望があれば表示名を置き換える名前（D08: 公開表示は「退会メンバー」等）。
const anonymizedName = "退会メンバー"

// getPrivateProfile は GET /admin/users/{id}/private-profile。個人情報4項目を返し、閲覧を監査に残す（値は残さない）。
func (s *server) getPrivateProfile(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "admin.users.private_profile.get", authz.Resource{})
	if !ok {
		return
	}
	target, ok := parseUserID(w, r)
	if !ok {
		return
	}
	var out struct {
		ID          string `json:"id"`
		LoginID     string `json:"loginId"`
		DisplayName string `json:"displayName"`
		Private     struct {
			LegalName       *string `json:"legalName"`
			StudentNumber   *string `json:"studentNumber"`
			UniversityEmail *string `json:"universityEmail"`
			NearestStation  *string `json:"nearestStation"`
		} `json:"private"`
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		u, err := q.GetUserByID(r.Context(), target)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		name, err := q.GetDisplayName(r.Context(), target)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		out.ID, out.LoginID, out.DisplayName = target.String(), u.LoginID, name
		if pp, err := q.GetPersonalProfile(r.Context(), target); err == nil {
			out.Private.LegalName, out.Private.StudentNumber = strPtr(pp.LegalName), strPtr(pp.StudentNumber)
			out.Private.UniversityEmail, out.Private.NearestStation = strPtr(pp.UniversityEmail), strPtr(pp.NearestStation)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// 閲覧そのものを監査に残す（誰が・誰の・いつ）。値は残さない。
		return s.auditEvent(r.Context(), q, a.Identity.UserID, target, "admin.private_profile_viewed", map[string]any{})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// deletePrivateProfile は DELETE /admin/users/{id}/private-profile（個人情報の手動削除）。
//
//   - 停止中のユーザーだけが対象（有効なメンバーの誤削除を防ぐ。先に停止する）。理由が必須。
//   - 個人情報4項目・自己紹介・学部学科学年・外部リンク・個人機材・公開設定を削除し、公開クレジット同意があれば撤回する。
//   - ユーザー行・監査ログ・活動履歴の参照は維持する。表示名は任意で「退会メンバー」に置き換える。
//   - 監査には削除した値を残さない（理由と、削除した種類だけ）。冪等。
func (s *server) deletePrivateProfile(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "admin.users.private_profile.delete", authz.Resource{})
	if !ok {
		return
	}
	target, ok := parseUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason               string `json:"reason"`
		AnonymizeDisplayName bool   `json:"anonymizeDisplayName"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "削除の理由を200文字以内で入力してください（個人情報は書かないでください）")
		return
	}
	var out struct {
		ConsentWithdrawn      bool  `json:"consentWithdrawn"`
		DisplayNameAnonymized bool  `json:"displayNameAnonymized"`
		Links                 int64 `json:"linksDeleted"`
		Devices               int64 `json:"devicesDeleted"`
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		u, err := q.GetUserForUpdate(r.Context(), target)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		if u.Status != "disabled" {
			return &errTx{http.StatusConflict, "NOT_DISABLED", "個人情報を削除できるのは停止中のユーザーだけです。先にユーザーを停止してください"}
		}
		if _, err := q.ClearPersonalProfile(r.Context(), target); err != nil {
			return err
		}
		if err := q.ClearProfileExtras(r.Context(), target); err != nil {
			return err
		}
		if out.Links, err = q.DeleteAllProfileLinks(r.Context(), target); err != nil {
			return err
		}
		if out.Devices, err = q.DeleteAllPersonalDevices(r.Context(), target); err != nil {
			return err
		}
		if _, err := q.DeleteAllFieldVisibility(r.Context(), target); err != nil {
			return err
		}
		// D08: 公開同意があれば撤回し、公開表示は匿名化される
		n, err := q.WithdrawPublicCreditConsentByAdmin(r.Context(), store.WithdrawPublicCreditConsentByAdminParams{UserID: target, UpdatedBy: a.Identity.UserID, WithdrawnAt: pgtype.Timestamptz{Time: s.Now(), Valid: true}})
		if err != nil {
			return err
		}
		_ = n
		if c, err := q.GetPublicCreditConsent(r.Context(), target); err == nil && c.WithdrawnAt.Valid {
			out.ConsentWithdrawn = true
		}
		if req.AnonymizeDisplayName {
			if err := q.SetDisplayName(r.Context(), store.SetDisplayNameParams{UserID: target, DisplayName: anonymizedName}); err != nil {
				return err
			}
			out.DisplayNameAnonymized = true
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, target, "admin.private_profile_deleted", map[string]any{
			"reason": reason, "anonymizeDisplayName": req.AnonymizeDisplayName, "linksDeleted": out.Links, "devicesDeleted": out.Devices,
		})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
