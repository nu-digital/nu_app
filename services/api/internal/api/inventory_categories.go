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
// 削除すると、使っていた物品は自動で「その他」（システムカテゴリ。削除・名前変更はできない）へ移る。移し先（moveTo）は任意。

type categoryView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int32  `json:"sortOrder"`
	ItemCount int64  `json:"itemCount"` // 使用中（廃止していない）の物品の数
	System    bool   `json:"system"`    // 「その他」。削除・名前変更できない（削除したカテゴリの物品の移し先）
}

// otherCategoryName は、削除したカテゴリの物品を自動で移す先の名前。
const otherCategoryName = "その他"

// ensureOtherCategory は、システムカテゴリ（その他）を返す。無ければ（名前を変えた・消した環境でも）作るか、同名のカテゴリに印を付ける。
func ensureOtherCategory(ctx context.Context, q *store.Queries) (store.InventoryCategory, error) {
	c, err := q.GetSystemCategory(ctx)
	if err == nil {
		return c, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return c, err
	}
	c, err = q.GetCategoryByName(ctx, otherCategoryName)
	if errors.Is(err, pgx.ErrNoRows) {
		c, err = q.CreateCategory(ctx, otherCategoryName)
	}
	if err != nil {
		return c, err
	}
	if err := q.MarkCategorySystem(ctx, c.ID); err != nil {
		return c, err
	}
	c.IsSystem = true
	return c, nil
}

const maxCategories = 50

func (s *server) categoryViews(ctx context.Context, q *store.Queries) ([]categoryView, error) {
	rows, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]categoryView, 0, len(rows))
	for _, c := range rows {
		out = append(out, categoryView{ID: c.ID.String(), Name: c.Name, SortOrder: c.SortOrder, ItemCount: c.ItemCount, System: c.IsSystem})
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
		cur, err := q.GetCategoryForUpdate(r.Context(), id)
		if err == nil && cur.IsSystem {
			return &errTx{http.StatusConflict, "SYSTEM_CATEGORY", "「" + cur.Name + "」は物品の移し先として使うため、名前を変えられません"}
		}
		if err != nil {
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

// deleteCategory は DELETE /inventory/categories/{id}（Admin。?moveTo=<カテゴリID> は任意）。
// 使われていなければそのまま削除する。物品（廃止したものを含む）が使っていれば、自動で「その他」へ移してから削除する（貸出の履歴は物品IDで残る）。
// 「その他」（システムカテゴリ）は削除できない。
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
	var movedTo string
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if err := q.LockCategories(r.Context()); err != nil {
			return err
		}
		cur, err := q.GetCategory(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		if cur.IsSystem {
			return &errTx{http.StatusConflict, "SYSTEM_CATEGORY", "「" + cur.Name + "」は物品の移し先として使うため、削除できません"}
		}
		active, err := q.CountItemsInCategory(r.Context(), id)
		if err != nil {
			return err
		}
		all, err := q.CountAllItemsInCategory(r.Context(), id)
		if err != nil {
			return err
		}
		if all > 0 {
			var dest store.InventoryCategory
			if moveTo != "" {
				var to pgtype.UUID
				if err := to.Scan(moveTo); err != nil || !to.Valid || to == id {
					return invalid("移し先のカテゴリが不正です")
				}
				dest, err = q.GetCategory(r.Context(), to)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						return invalid("移し先のカテゴリが見つかりません")
					}
					return err
				}
			} else if dest, err = ensureOtherCategory(r.Context(), q); err != nil {
				return err
			}
			if err := q.MoveItemsToCategory(r.Context(), store.MoveItemsToCategoryParams{CategoryID: id, CategoryID_2: dest.ID, Category: dest.Name}); err != nil {
				return err
			}
			movedTo = dest.Name
		}
		if err := q.DeleteCategory(r.Context(), id); err != nil {
			return err
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.category_deleted", map[string]any{"categoryId": id.String(), "moved": active, "movedTo": movedTo})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
