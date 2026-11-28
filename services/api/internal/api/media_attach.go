package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// アバター・物品写真（P3-02b）。画像は`purpose=avatar/inventory`でアップロードし、所有者（プロフィール・物品）へ紐づける。
// 配信はログイン中のメンバー向けだけ（`GET /media/{id}`。有効なユーザーの現在のアバター、アーカイブされていない物品の現在の写真）。
// 匿名・公開には出さない。差し替え・解除で古い参照は外れ、`nuctl media-gc`が（24時間後に）ファイルごと消す。

type attachReq struct {
	MediaID string `json:"mediaId"`
}

// attachMedia はメディアを検査・ロックして、所有者の参照を差し替える（同じトランザクション）。
func (s *server) attachMedia(ctx context.Context, q *store.Queries, actor, owner pgtype.UUID, mediaID, purpose, ownerType, refPurpose string, set func(media pgtype.UUID) error) error {
	var mid pgtype.UUID
	if err := mid.Scan(mediaID); err != nil || !mid.Valid {
		return &errTx{http.StatusUnprocessableEntity, "INVALID_INPUT", "mediaIdが不正です"}
	}
	rows, err := q.LockMediaByIDs(ctx, []pgtype.UUID{mid})
	if err != nil {
		return err
	}
	// 他人の画像の存在を区別して返さない（自分がアップロードした、この用途の、無効化されていない画像だけ）
	if len(rows) != 1 || rows[0].Purpose != purpose || rows[0].UploadedBy != actor || rows[0].RedactedAt.Valid {
		return &errTx{http.StatusUnprocessableEntity, "INVALID_INPUT", "画像が見つかりません"}
	}
	if _, err := q.DeleteOwnerMediaReference(ctx, store.DeleteOwnerMediaReferenceParams{OwnerType: ownerType, OwnerID: owner}); err != nil {
		return err
	}
	if err := q.InsertOwnerMediaReference(ctx, store.InsertOwnerMediaReferenceParams{MediaID: mid, OwnerType: ownerType, OwnerID: owner, Purpose: refPurpose}); err != nil {
		return err
	}
	return set(mid)
}

// putMyAvatar は PUT /me/profile/avatar。
func (s *server) putMyAvatar(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.avatar.set", authz.Resource{})
	if !ok {
		return
	}
	var req attachReq
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := a.Identity.UserID
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if err := s.attachMedia(r.Context(), q, uid, uid, req.MediaID, "avatar", "profile", "avatar", func(m pgtype.UUID) error {
			return q.SetProfileAvatar(r.Context(), store.SetProfileAvatarParams{UserID: uid, AvatarMediaID: m})
		}); err != nil {
			return err
		}
		return s.auditEvent(r.Context(), q, uid, uid, "profile.avatar_changed", map[string]any{"set": true})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteMyAvatar は DELETE /me/profile/avatar（冪等）。
func (s *server) deleteMyAvatar(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "me.profile.avatar.clear", authz.Resource{})
	if !ok {
		return
	}
	uid := a.Identity.UserID
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if err := s.clearAvatar(r.Context(), q, uid); err != nil {
			return err
		}
		return s.auditEvent(r.Context(), q, uid, uid, "profile.avatar_changed", map[string]any{"set": false})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clearAvatar はアバターの参照とポインタを外す（個人情報の削除からも使う）。
func (s *server) clearAvatar(ctx context.Context, q *store.Queries, user pgtype.UUID) error {
	if _, err := q.DeleteOwnerMediaReference(ctx, store.DeleteOwnerMediaReferenceParams{OwnerType: "profile", OwnerID: user}); err != nil {
		return err
	}
	return q.ClearProfileAvatar(ctx, user)
}

func (s *server) itemFromPath(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	return parseUserID(w, r) // 形式の検査だけ（UUID）。名前はそのままだが、物品のIDにも使う
}

// putItemPhoto は PUT /inventory/items/{id}/photo（Admin）。
func (s *server) putItemPhoto(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.items.photo.set", authz.Resource{})
	if !ok {
		return
	}
	item, ok := s.itemFromPath(w, r)
	if !ok {
		return
	}
	var req attachReq
	if !decodeJSON(w, r, &req) {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.GetInventoryItemForUpdate(r.Context(), item); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		return s.attachMedia(r.Context(), q, a.Identity.UserID, item, req.MediaID, "inventory", "inventory_item", "photo", func(m pgtype.UUID) error {
			// 単一の写真を設定する旧API: 画像は1枚だけになる（複数枚はPUT /images）
			if err := q.DeleteItemImages(r.Context(), item); err != nil {
				return err
			}
			if err := q.InsertItemImage(r.Context(), store.InsertItemImageParams{ItemID: item, MediaID: m, SortOrder: 0}); err != nil {
				return err
			}
			return q.SetItemPhoto(r.Context(), store.SetItemPhotoParams{ID: item, CoverMediaID: m})
		})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteItemPhoto は DELETE /inventory/items/{id}/photo（Admin。冪等）。
func (s *server) deleteItemPhoto(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, "inventory.items.photo.clear", authz.Resource{}); !ok {
		return
	}
	item, ok := s.itemFromPath(w, r)
	if !ok {
		return
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.GetInventoryItemForUpdate(r.Context(), item); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		if _, err := q.DeleteOwnerMediaReference(r.Context(), store.DeleteOwnerMediaReferenceParams{OwnerType: "inventory_item", OwnerID: item}); err != nil {
			return err
		}
		if err := q.DeleteItemImages(r.Context(), item); err != nil {
			return err
		}
		return q.SetItemPhoto(r.Context(), store.SetItemPhotoParams{ID: item, CoverMediaID: pgtype.UUID{}})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// maxItemImages は物品1つの画像の最大枚数。
const maxItemImages = 8

// putItemImages は PUT /inventory/items/{id}/images（Admin）。画像を順序つきで置き換える（先頭がメイン。空なら全部外す）。
// 指定できるのは「自分がアップロードした（用途=inventory・無効化されていない）画像」と「その物品に既に付いている画像」だけ。
// 外れた画像の参照は消え、`nuctl media-gc`が（24時間後に）ファイルごと消す。1つのトランザクションで、物品行をロックして行う。
func (s *server) putItemImages(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "inventory.items.images.set", authz.Resource{})
	if !ok {
		return
	}
	item, ok := s.itemFromPath(w, r)
	if !ok {
		return
	}
	var req struct {
		MediaIDs []string `json:"mediaIds"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.MediaIDs) > maxItemImages {
		s.writeTxErr(w, r, invalid("画像は"+itoa(maxItemImages)+"枚までです"))
		return
	}
	ids := make([]pgtype.UUID, 0, len(req.MediaIDs))
	seen := map[string]bool{}
	for _, raw := range req.MediaIDs {
		var m pgtype.UUID
		if err := m.Scan(raw); err != nil || !m.Valid || seen[m.String()] {
			s.writeTxErr(w, r, invalid("mediaIdsが不正です（形式の誤り・重複）"))
			return
		}
		seen[m.String()] = true
		ids = append(ids, m)
	}
	err := s.inTx(r.Context(), func(q *store.Queries, tx pgx.Tx) error {
		if _, err := q.GetInventoryItemForUpdate(r.Context(), item); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return &errTx{http.StatusNotFound, "NOT_FOUND", "見つかりません"}
			}
			return err
		}
		current, err := q.ListItemImages(r.Context(), item)
		if err != nil {
			return err
		}
		have := map[string]bool{}
		for _, m := range current {
			have[m.String()] = true
		}
		if len(ids) > 0 {
			rows, err := q.LockMediaByIDs(r.Context(), ids)
			if err != nil {
				return err
			}
			if len(rows) != len(ids) {
				return &errTx{http.StatusUnprocessableEntity, "INVALID_INPUT", "画像が見つかりません"}
			}
			for _, m := range rows {
				// 他人の画像の存在を区別して返さない
				if m.Purpose != "inventory" || m.RedactedAt.Valid || (m.UploadedBy != a.Identity.UserID && !have[m.ID.String()]) {
					return &errTx{http.StatusUnprocessableEntity, "INVALID_INPUT", "画像が見つかりません"}
				}
			}
		}
		if _, err := q.DeleteOwnerMediaReference(r.Context(), store.DeleteOwnerMediaReferenceParams{OwnerType: "inventory_item", OwnerID: item}); err != nil {
			return err
		}
		if err := q.DeleteItemImages(r.Context(), item); err != nil {
			return err
		}
		for i, m := range ids {
			if err := q.InsertOwnerMediaReference(r.Context(), store.InsertOwnerMediaReferenceParams{MediaID: m, OwnerType: "inventory_item", OwnerID: item, Purpose: "photo"}); err != nil {
				return err
			}
			if err := q.InsertItemImage(r.Context(), store.InsertItemImageParams{ItemID: item, MediaID: m, SortOrder: int32(i)}); err != nil {
				return err
			}
		}
		var main pgtype.UUID
		if len(ids) > 0 {
			main = ids[0]
		}
		if err := q.SetItemPhoto(r.Context(), store.SetItemPhotoParams{ID: item, CoverMediaID: main}); err != nil {
			return err
		}
		return s.auditEvent(r.Context(), q, a.Identity.UserID, a.Identity.UserID, "inventory.item_images_changed", map[string]any{"itemId": item.String(), "count": len(ids)})
	})
	if err != nil {
		s.writeTxErr(w, r, err)
		return
	}
	out := make([]string, 0, len(ids))
	for _, m := range ids {
		out = append(out, m.String())
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"images": out})
}

// avatarIDs は、ユーザーIDごとの現在のアバター（メディアID）を返す。
func (s *server) avatarIDs(ctx context.Context, q *store.Queries, users []pgtype.UUID) (map[string]string, error) {
	out := map[string]string{}
	if len(users) == 0 {
		return out, nil
	}
	rows, err := q.ListAvatarsOfUsers(ctx, users)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.UserID.String()] = r.AvatarMediaID.String()
	}
	return out, nil
}

// photoIDs は、物品IDごとの現在の写真（メディアID）を返す。
func (s *server) photoIDs(ctx context.Context, q *store.Queries, items []pgtype.UUID) (map[string]string, error) {
	out := map[string]string{}
	if len(items) == 0 {
		return out, nil
	}
	rows, err := q.ListPhotosOfItems(ctx, items)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID.String()] = r.CoverMediaID.String()
	}
	return out, nil
}

func ptrOf(m map[string]string, k string) *string {
	if v, ok := m[k]; ok {
		return &v
	}
	return nil
}

var _ = httpx.JSON
