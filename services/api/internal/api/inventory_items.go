package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

const maxQuantity = 100000

type itemView struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Category      string  `json:"category"`   // カテゴリ名（categoryIdと同期。旧クライアント向けに残す）
	CategoryID    *string `json:"categoryId"` // カテゴリ（inventory_categories）
	Location      string  `json:"location"`
	Mode          string  `json:"mode"`
	TotalQuantity int32   `json:"totalQuantity"` // 数量管理のときの総数（個体管理は0）
	Outstanding   int64   `json:"outstanding"`   // 貸出中の数量
	UnitCount     int64   `json:"unitCount"`     // 個体管理のときの有効な個体数
	Available     int64   `json:"available"`     // 借りられる数（数量管理=残数、個体管理=利用可能な個体数）
	Archived      bool    `json:"archived"`
	// PhotoMediaID は現在の写真。画像は GET /media/{id}（ログイン中のメンバーだけ）。
	PhotoMediaID *string `json:"photoMediaId"`
	// BorrowedByMe は自分が借りている未返却の数量（0なら借りていない）。
	BorrowedByMe int64 `json:"borrowedByMe"`
	// Images は物品の画像（メディアID。先頭がメイン）。詳細だけに入る。
	Images []string `json:"images,omitempty"`
}

type unitView struct {
	ID           string      `json:"id"`
	AssetTag     string      `json:"assetTag"`
	SerialNumber *string     `json:"serialNumber"`
	State        string      `json:"state"`
	Loaned       *loanedView `json:"loaned"`
	Mine         bool        `json:"mine"` // 自分が借りている個体
}

type loanedView struct {
	BorrowerDisplayName string    `json:"borrowerDisplayName"`
	Since               time.Time `json:"since"`
	ElapsedDays         int       `json:"elapsedDays"`
}

type outstandingLoanView struct {
	ID                  string    `json:"id"`
	BorrowerDisplayName string    `json:"borrowerDisplayName"`
	Quantity            int32     `json:"quantity"` // 未返却の数量
	BorrowedAt          time.Time `json:"borrowedAt"`
	ElapsedDays         int       `json:"elapsedDays"`
}

// elapsedDays は貸出日時から表示時点までの経過日数（返却期限は持たない）。
func elapsedDays(since, now time.Time) int {
	d := int(now.Sub(since).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

func itemFrom(i store.InventoryItem, outstanding, unitCount int64) itemView {
	v := itemView{ID: i.ID.String(), Name: i.Name, Description: i.Description, Category: i.Category, Location: i.Location, Mode: i.Mode,
		TotalQuantity: i.TotalQuantity, Outstanding: outstanding, UnitCount: unitCount, Archived: i.ArchivedAt.Valid}
	if i.CoverMediaID.Valid {
		p := i.CoverMediaID.String()
		v.PhotoMediaID = &p
	}
	if i.CategoryID.Valid {
		c := i.CategoryID.String()
		v.CategoryID = &c
	}
	if i.Mode == "quantity" {
		v.Available = int64(i.TotalQuantity) - outstanding
	}
	return v
}

// defaultCategoryName は、カテゴリの指定が無いときに入れるカテゴリ（初期カテゴリの1つ）。
const defaultCategoryName = "その他"

// resolveCategory は、categoryId（優先）またはカテゴリ名から、存在するカテゴリを求める。どちらも空なら既定のカテゴリ。
func resolveCategory(ctx context.Context, q *store.Queries, categoryID, name string) (store.InventoryCategory, error) {
	var c store.InventoryCategory
	var err error
	switch {
	case strings.TrimSpace(categoryID) != "":
		var id pgtype.UUID
		if e := id.Scan(strings.TrimSpace(categoryID)); e != nil || !id.Valid {
			return c, invalid("categoryIdが不正です")
		}
		c, err = q.GetCategory(ctx, id)
	case strings.TrimSpace(name) != "":
		// 旧クライアント（カテゴリ名の文字列だけを送る）との互換: 無い名前は、管理者の操作としてカテゴリを作る
		name = strings.TrimSpace(name)
		if utf8.RuneCountInString(name) > 30 || strings.ContainsAny(name, "\n\r\t") {
			return c, invalid("カテゴリは30文字以内で、改行を含めないでください")
		}
		c, err = q.GetCategoryByName(ctx, name)
		if errors.Is(err, pgx.ErrNoRows) {
			c, err = q.CreateCategory(ctx, name)
		}
		return c, err
	default:
		c, err = q.GetCategoryByName(ctx, defaultCategoryName)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return c, invalid("カテゴリが見つかりません。カテゴリの管理画面で追加してください")
	}
	return c, err
}

func checkText(v, name string, min, max int) (string, error) {
	v = strings.TrimSpace(v)
	n := utf8.RuneCountInString(v)
	if n < min || n > max {
		if min > 0 {
			return "", invalid(name + "は" + itoa(min) + "〜" + itoa(max) + "文字で入力してください")
		}
		return "", invalid(name + "は" + itoa(max) + "文字以内で入力してください")
	}
	return v, nil
}

// createItem は POST /inventory/items（Admin）。
func (s *server) createItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.items.create", authz.Resource{})
	if !ok {
		return
	}
	var req struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		Category      string `json:"category"`
		CategoryID    string `json:"categoryId"`
		Location      string `json:"location"`
		Mode          string `json:"mode"`
		TotalQuantity int    `json:"totalQuantity"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	name, err := checkText(req.Name, "名前", 1, 100)
	if err == nil {
		req.Description, err = checkText(req.Description, "説明", 0, 1000)
	}
	if err == nil {
		req.Category, err = checkText(req.Category, "カテゴリ", 0, 30)
	}
	if err == nil {
		req.Location, err = checkText(req.Location, "保管場所", 0, 50)
	}
	if err == nil && req.Mode != "quantity" && req.Mode != "individual" {
		err = invalid("管理方式は quantity（数量管理）か individual（個体管理）を指定してください")
	}
	if err == nil && (req.TotalQuantity < 0 || req.TotalQuantity > maxQuantity) {
		err = invalid("総数は0〜" + itoa(maxQuantity) + "で入力してください")
	}
	if err == nil && req.Mode == "individual" && req.TotalQuantity != 0 {
		err = invalid("個体管理の物品は総数を指定せず、個体を登録してください")
	}
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	var out itemView
	err = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		cat, err := resolveCategory(r.Context(), q, req.CategoryID, req.Category)
		if err != nil {
			return err
		}
		it, err := q.CreateInventoryItem(r.Context(), store.CreateInventoryItemParams{Name: name, Description: req.Description, Category: cat.Name, CategoryID: cat.ID, Location: req.Location, Mode: req.Mode, TotalQuantity: int32(req.TotalQuantity)})
		if err != nil {
			return err
		}
		out = itemFrom(it, 0, 0)
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.item_created", map[string]any{"itemId": it.ID.String(), "mode": it.Mode})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// updateItem は PATCH /inventory/items/{id}（Admin）。
//
//   - 総数の変更は理由必須・履歴(inventory_adjustments)に残し、貸出中の数量を下回る総数は拒否する。
//   - 廃止は貸出中だと拒否する。管理方式の変更は貸出履歴・個体が無いときだけ。
//   - 物品行をロックして、貸出・返却と直列化する。
func (s *server) updateItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.items.update", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name          optStr `json:"name"`
		Description   optStr `json:"description"`
		Category      optStr `json:"category"`
		CategoryID    optStr `json:"categoryId"`
		Location      optStr `json:"location"`
		Mode          optStr `json:"mode"`
		TotalQuantity *int   `json:"totalQuantity"`
		Reason        string `json:"reason"`
		Archived      *bool  `json:"archived"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var out itemView
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		cur, err := q.GetInventoryItemForUpdate(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		p := store.UpdateInventoryItemParams{ID: id, Name: cur.Name, Description: cur.Description, Category: cur.Category, CategoryID: cur.CategoryID, Location: cur.Location, TotalQuantity: cur.TotalQuantity, ArchivedAt: cur.ArchivedAt, Mode: cur.Mode}
		text := func(o optStr, label string, min, max int, dst *string) error {
			if !o.Set {
				return nil
			}
			if o.Null {
				return invalid(label + "を空にはできません")
			}
			v, err := checkText(o.Val, label, min, max)
			if err != nil {
				return err
			}
			*dst = v
			return nil
		}
		if err := text(req.Name, "名前", 1, 100, &p.Name); err != nil {
			return err
		}
		if err := text(req.Description, "説明", 0, 1000, &p.Description); err != nil {
			return err
		}
		if (req.CategoryID.Set && !req.CategoryID.Null) || (req.Category.Set && !req.Category.Null) {
			cat, err := resolveCategory(r.Context(), q, req.CategoryID.Val, req.Category.Val)
			if err != nil {
				return err
			}
			p.Category, p.CategoryID = cat.Name, cat.ID
		}
		if err := text(req.Location, "保管場所", 0, 50, &p.Location); err != nil {
			return err
		}
		outstanding, err := q.ItemOutstanding(r.Context(), id)
		if err != nil {
			return err
		}
		mode := cur.Mode
		if req.Mode.Set && req.Mode.Val != cur.Mode {
			if req.Mode.Null || (req.Mode.Val != "quantity" && req.Mode.Val != "individual") {
				return invalid("管理方式は quantity か individual を指定してください")
			}
			loans, err := q.ItemLoanCount(r.Context(), id)
			if err != nil {
				return err
			}
			units, err := q.CountItemUnits(r.Context(), id)
			if err != nil {
				return err
			}
			if loans > 0 || units > 0 {
				return &errTx{http.StatusConflict, "MODE_LOCKED", "貸出の履歴や個体の登録がある物品は、管理方式を変更できません"}
			}
			mode = req.Mode.Val
			if mode == "individual" {
				p.TotalQuantity = 0
			}
		}
		if req.TotalQuantity != nil {
			if mode != "quantity" {
				return invalid("総数を指定できるのは数量管理の物品だけです")
			}
			total := *req.TotalQuantity
			if total < 0 || total > maxQuantity {
				return invalid("総数は0〜" + itoa(maxQuantity) + "で入力してください")
			}
			if int64(total) < outstanding {
				return &errTx{http.StatusConflict, "BELOW_OUTSTANDING", "貸出中の数量（" + strconv.FormatInt(outstanding, 10) + "）より少ない総数にはできません"}
			}
			if int32(total) != cur.TotalQuantity {
				reason, err := checkText(req.Reason, "変更の理由", 1, 200)
				if err != nil {
					return err
				}
				if err := q.InsertInventoryAdjustment(r.Context(), store.InsertInventoryAdjustmentParams{ItemID: id, ActorUserID: a.Identity.UserID, BeforeQuantity: cur.TotalQuantity, AfterQuantity: int32(total), Reason: reason}); err != nil {
					return err
				}
				p.TotalQuantity = int32(total)
			}
		}
		if req.Archived != nil {
			switch {
			case *req.Archived && !cur.ArchivedAt.Valid:
				if outstanding > 0 {
					return &errTx{http.StatusConflict, "HAS_OUTSTANDING", "貸出中の物品は廃止できません。返却されてから廃止してください"}
				}
				p.ArchivedAt = pgtype.Timestamptz{Time: s.Now(), Valid: true}
			case !*req.Archived:
				p.ArchivedAt = pgtype.Timestamptz{}
			}
		}
		p.Mode = mode // 方式と総数は1つのUPDATEで同時に変える（個体管理は総数0のCHECKがあるため）
		it, err := q.UpdateInventoryItem(r.Context(), p)
		if err != nil {
			return err
		}
		units, err := q.CountItemUnits(r.Context(), id)
		if err != nil {
			return err
		}
		out = itemFrom(it, outstanding, units)
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.item_updated", map[string]any{"itemId": id.String()})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// createUnit は POST /inventory/items/{id}/units（Admin。個体管理の物品だけ）。
func (s *server) createUnit(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.units.create", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	var req struct {
		AssetTag     string  `json:"assetTag"`
		SerialNumber *string `json:"serialNumber"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tag, err := checkText(req.AssetTag, "管理番号", 1, 50)
	var serial pgtype.Text
	if err == nil && req.SerialNumber != nil {
		var sn string
		if sn, err = checkText(*req.SerialNumber, "シリアル番号", 0, 100); err == nil && sn != "" {
			serial = pgtype.Text{String: sn, Valid: true}
		}
	}
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	var out unitView
	err = s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		it, err := q.GetInventoryItemForUpdate(r.Context(), id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		if it.Mode != "individual" {
			return &errTx{http.StatusConflict, "WRONG_MODE", "個体を登録できるのは個体管理の物品だけです"}
		}
		if it.ArchivedAt.Valid {
			return &errTx{http.StatusConflict, "ARCHIVED", "廃止した物品には個体を登録できません"}
		}
		u, err := q.CreateInventoryUnit(r.Context(), store.CreateInventoryUnitParams{ItemID: id, AssetTag: tag, SerialNumber: serial})
		if err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "23505" {
				return &errTx{http.StatusConflict, "TAG_TAKEN", "その管理番号は、この物品で既に使われています"}
			}
			return err
		}
		out = unitView{ID: u.ID.String(), AssetTag: u.AssetTag, SerialNumber: strPtr(u.SerialNumber), State: u.State}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.unit_created", map[string]any{"itemId": id.String(), "unitId": u.ID.String()})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// listItems は GET /inventory/items（検索・カテゴリ・利用可否・ページング）。
func (s *server) listItems(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.items.list", authz.Resource{})
	if !ok {
		return
	}
	qs := r.URL.Query()
	search := strings.TrimSpace(qs.Get("q"))
	category := strings.TrimSpace(qs.Get("category"))
	if utf8.RuneCountInString(search) > 100 || utf8.RuneCountInString(category) > 30 {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "検索語が長すぎます")
		return
	}
	limit := 50
	if v := qs.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "limit は1〜100で指定してください")
			return
		}
		limit = n
	}
	sort := qs.Get("sort")
	switch sort {
	case "", "name", "newest", "available":
	default:
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "sort は name・newest・available のいずれかです")
		return
	}
	offset := 0
	if c := qs.Get("cursor"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 || n > 100000 {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "cursor が正しくありません")
			return
		}
		offset = n
	}
	params := store.ListInventoryItemsParams{Q: likeEscape(search), Category: category, OnlyAvailable: qs.Get("available") == "true", OnlyMine: qs.Get("mine") == "true",
		Sort: sort, RowOffset: int32(offset), RowLimit: int32(limit + 1), UserID: a.Identity.UserID}
	if v := qs.Get("categoryId"); v != "" {
		if err := params.CategoryID.Scan(v); err != nil || !params.CategoryID.Valid {
			httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "categoryId が正しくありません")
			return
		}
	}
	rows, err := store.New(s.Pool).ListInventoryItems(r.Context(), params)
	if err != nil {
		s.internal(w, r)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		c := strconv.Itoa(offset + limit)
		next = &c
	}
	items := make([]itemView, 0, len(rows))
	itemIDs := make([]pgtype.UUID, 0, len(rows))
	for _, x := range rows {
		itemIDs = append(itemIDs, x.ID)
	}
	photos, err := s.photoIDs(r.Context(), store.New(s.Pool), itemIDs)
	if err != nil {
		s.internal(w, r)
		return
	}
	for _, x := range rows {
		v := itemView{PhotoMediaID: ptrOf(photos, x.ID.String()), ID: x.ID.String(), Name: x.Name, Description: x.Description, Category: x.Category, Location: x.Location, Mode: x.Mode,
			TotalQuantity: x.TotalQuantity, Outstanding: x.Outstanding, UnitCount: x.UnitCount, Available: x.Available, BorrowedByMe: x.Mine}
		if x.CategoryID.Valid {
			c := x.CategoryID.String()
			v.CategoryID = &c
		}
		items = append(items, v)
	}
	cats, err := store.New(s.Pool).ListInventoryCategories(r.Context())
	if err != nil {
		s.internal(w, r)
		return
	}
	if cats == nil {
		cats = []string{}
	}
	cv, err := s.categoryViews(r.Context(), store.New(s.Pool))
	if err != nil {
		s.internal(w, r)
		return
	}
	// categories は旧クライアント向け（使われている名前の一覧）。categoryList が管理されたカテゴリ（並び順つき）
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next, "categories": cats, "categoryList": cv})
}

// getItem は GET /inventory/items/{id}（個体の状況・残数・現在の貸出状況）。
func (s *server) getItem(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.items.get", authz.Resource{})
	if !ok {
		return
	}
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	q := store.New(s.Pool)
	it, err := q.GetInventoryItem(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
			return
		}
		s.internal(w, r)
		return
	}
	outstanding, err := q.ItemOutstanding(r.Context(), id)
	if err != nil {
		s.internal(w, r)
		return
	}
	units, err := q.ListItemUnits(r.Context(), id)
	if err != nil {
		s.internal(w, r)
		return
	}
	active := int64(0)
	var availableUnits, mineUnits int64
	now := s.Now()
	uv := make([]unitView, 0, len(units))
	for _, u := range units {
		v := unitView{ID: u.ID.String(), AssetTag: u.AssetTag, SerialNumber: strPtr(u.SerialNumber), State: u.State}
		if u.State == "active" {
			active++
			if !u.CurrentLoanID.Valid {
				availableUnits++
			}
		}
		if u.CurrentLoanID.Valid && u.BorrowerUserID == a.Identity.UserID {
			v.Mine = true
			mineUnits++
		}
		if u.CurrentLoanID.Valid && u.LoanedAt.Valid {
			v.Loaned = &loanedView{BorrowerDisplayName: u.BorrowerDisplayName, Since: u.LoanedAt.Time, ElapsedDays: elapsedDays(u.LoanedAt.Time, now)}
		}
		uv = append(uv, v)
	}
	view := itemFrom(it, outstanding, active)
	if it.Mode == "individual" {
		view.Available = availableUnits
	}
	view.BorrowedByMe = mineUnits
	imgs, err := q.ListItemImages(r.Context(), id)
	if err != nil {
		s.internal(w, r)
		return
	}
	view.Images = make([]string, 0, len(imgs))
	for _, m := range imgs {
		view.Images = append(view.Images, m.String())
	}
	loans, err := q.ListItemLoansOutstanding(r.Context(), id)
	if err != nil {
		s.internal(w, r)
		return
	}
	lv := make([]outstandingLoanView, 0, len(loans))
	for _, l := range loans {
		if l.BorrowerUserID == a.Identity.UserID {
			view.BorrowedByMe += int64(l.Outstanding)
		}
		lv = append(lv, outstandingLoanView{ID: l.ID.String(), BorrowerDisplayName: l.BorrowerDisplayName, Quantity: l.Outstanding, BorrowedAt: l.BorrowedAt.Time, ElapsedDays: elapsedDays(l.BorrowedAt.Time, now)})
	}
	httpx.JSON(w, http.StatusOK, struct {
		itemView
		Units []unitView            `json:"units"`
		Loans []outstandingLoanView `json:"loans"`
	}{view, uv, lv})
}
