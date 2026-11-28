package api

import (
	"context"
	"net/http"
	"strings"

	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// 物品のカテゴリ管理（UI/UX大改善。docs/design/ui-overhaul.md §5.1）。
// 一覧は全メンバー、追加・名称変更・並び替え・削除は管理者（Admin/Owner）だけ。
// 使用中のカテゴリは、移し先（moveTo）を指定したときだけ削除でき、物品は消えずに移る。

type categoryView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int32  `json:"sortOrder"`
	ItemCount int64  `json:"itemCount"` // 使用中（廃止していない）の物品の数
}

const maxCategories = 50

func (s *server) categoryViews(ctx context.Context, q *store.Queries) ([]categoryView, error) {
	rows, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]categoryView, 0, len(rows))
	for _, c := range rows {
		out = append(out, categoryView{ID: c.ID.String(), Name: c.Name, SortOrder: c.SortOrder, ItemCount: c.ItemCount})
	}
	return out, nil
}

// categoryName は、カテゴリ名の検査（空・長さ・改行）。
func categoryName(v string) (string, error) {
	v, err := checkText(v, "カテゴリ名", 1, 30)
	if err == nil && strings.ContainsAny(v, "\n\r\t") {
		err = invalid("カテゴリ名に改行は使えません")
	}
	return v, err
}

func nameTaken(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return &errTx{http.StatusConflict, "NAME_TAKEN", "同じ名前のカテゴリが既にあります"}
	}
	return err
}

// listCategories は GET /inventory/categories。
func (s *server) listCategories(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "inventory.categories.list", authz.Resource{}); !ok {
		return
	}
	cv, err := s.categoryViews(r.Context(), store.New(s.Pool))
	if err != nil {
		s.internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": cv})
}

// createCategory は POST /inventory/categories（Admin）。末尾に追加する。
func (s *server) createCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.categories.create", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name, err := categoryName(req.Name)
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	var out categoryView
	err = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if err := q.LockCategories(r.Context()); err != nil {
			return err
		}
		list, err := q.ListCategories(r.Context())
		if err != nil {
			return err
		}
		if len(list) >= maxCategories {
			return &errTx{http.StatusConflict, "TOO_MANY", "カテゴリは" + itoa(maxCategories) + "件までです"}
		}
		c, err := q.CreateCategory(r.Context(), name)
		if err != nil {
			return nameTaken(err)
		}
		out = categoryView{ID: c.ID.String(), Name: c.Name, SortOrder: c.SortOrder}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.category_created", map[string]any{"categoryId": c.ID.String()})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// renameCategory は PATCH /inventory/categories/{id}（Admin）。物品の（旧クライアント向けの）カテゴリ名も同期する。
func (s *server) renameCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.categories.update", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name, err := categoryName(req.Name)
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	var out categoryView
	err = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.GetCategoryForUpdate(r.Context(), id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		c, err := q.RenameCategory(r.Context(), store.RenameCategoryParams{ID: id, Name: name})
		if err != nil {
			return nameTaken(err)
		}
		if err := q.SyncItemCategoryName(r.Context(), store.SyncItemCategoryNameParams{CategoryID: id, Category: name}); err != nil {
			return err
		}
		out = categoryView{ID: c.ID.String(), Name: c.Name, SortOrder: c.SortOrder}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.category_renamed", map[string]any{"categoryId": id.String()})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// reorderCategories は PUT /inventory/categories/order（Admin）。全カテゴリのIDを、並べたい順に渡す（過不足は拒否）。
func (s *server) reorderCategories(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.categories.reorder", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if err := q.LockCategories(r.Context()); err != nil {
			return err
		}
		list, err := q.ListCategories(r.Context())
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, c := range list {
			known[c.ID.String()] = true
		}
		seen := map[string]bool{}
		if len(req.IDs) != len(list) {
			return invalid("すべてのカテゴリを並べて指定してください")
		}
		for _, raw := range req.IDs {
			var u pgtype.UUID
			if err := u.Scan(raw); err != nil || !u.Valid || !known[u.String()] || seen[u.String()] {
				return invalid("カテゴリの指定が正しくありません")
			}
			seen[u.String()] = true
		}
		for i, raw := range req.IDs {
			var u pgtype.UUID
			_ = u.Scan(raw)
			if err := q.SetCategoryOrder(r.Context(), store.SetCategoryOrderParams{ID: u, SortOrder: int32((i + 1) * 10)}); err != nil {
				return err
			}
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.category_reordered", map[string]any{})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	cv, err := s.categoryViews(r.Context(), store.New(s.Pool))
	if err != nil {
		s.internal(w, r)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": cv})
}

// deleteCategory は DELETE /inventory/categories/{id}?moveTo=<カテゴリID>（Admin）。
// 物品（廃止したものを含む）が使っているときは、moveTo を指定したときだけ、物品を移してから削除する。最後の1件は削除できない。
func (s *server) deleteCategory(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.categories.delete", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	moveTo := strings.TrimSpace(r.URL.Query().Get("moveTo"))
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if err := q.LockCategories(r.Context()); err != nil {
			return err
		}
		if _, err := q.GetCategory(r.Context(), id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		list, err := q.ListCategories(r.Context())
		if err != nil {
			return err
		}
		if len(list) <= 1 {
			return &errTx{http.StatusConflict, "LAST_CATEGORY", "最後のカテゴリは削除できません"}
		}
		n, err := q.CountItemsInCategory(r.Context(), id)
		if err != nil {
			return err
		}
		if n > 0 {
			if moveTo == "" {
				return &errTx{http.StatusConflict, "IN_USE", "使われているカテゴリです（" + itoa(int(n)) + "件）。移し先のカテゴリを選んでください"}
			}
			var to pgtype.UUID
			if err := to.Scan(moveTo); err != nil || !to.Valid || to == id {
				return invalid("移し先のカテゴリが不正です")
			}
			dest, err := q.GetCategory(r.Context(), to)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return invalid("移し先のカテゴリが見つかりません")
				}
				return err
			}
			if err := q.MoveItemsToCategory(r.Context(), store.MoveItemsToCategoryParams{CategoryID: id, CategoryID_2: to, Category: dest.Name}); err != nil {
				return err
			}
		}
		if err := q.DeleteCategory(r.Context(), id); err != nil {
			return err
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.category_deleted", map[string]any{"categoryId": id.String(), "moved": n})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
