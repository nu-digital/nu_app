package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nu-digital/nu_app/services/api/internal/authz"
	"github.com/nu-digital/nu_app/services/api/internal/httpx"
	"github.com/nu-digital/nu_app/services/api/internal/media"
	"github.com/nu-digital/nu_app/services/api/internal/store"
)

// メディア（画像）のAPI。設計の正本: docs/design/p3-media.md。
//
//   - 取り込みは「安い検査→重い処理」。受信中・デコード中の同時数を制限し、利用者ごとに同時1・1日の上限を設ける。
//   - 公開は列で持たず、公開中の版が参照している間だけ（GetServablePublicMedia）。
//   - 権限が無い・存在しない・未公開・redactedは、同じ404（IDの総当たりで存在が分からない）。

// MediaLimits は取り込みの上限。ゼロ値のフィールドは既定を使う。
type MediaLimits struct {
	DailyCount   int64         // 利用者ごとの1日の枚数（既定200）
	DailyBytes   int64         // 利用者ごとの1日の合計バイト（既定200MiB）
	MinFreeBytes uint64        // 空きがこれを下回ったら507（既定1GiB）
	ReceiveSlots int           // 受信中の同時件数（既定8）
	DecodeSlots  int           // 同時デコード数（既定1。最悪ケースの実測では2件同時でmem_limit(1g)を超えるため。docs/OPERATIONS.md）
	SlotWait     time.Duration // 枠を待つ時間（既定2秒）
}

type mediaState struct {
	limits   MediaLimits
	rate     *saveLimiter
	recv     chan struct{}
	decode   chan struct{}
	mu       sync.Mutex
	inflight map[string]bool
}

func (s *server) initMedia() {
	l := s.MediaLimits
	if l.DailyCount == 0 {
		l.DailyCount = 200
	}
	if l.DailyBytes == 0 {
		l.DailyBytes = 200 << 20
	}
	if l.MinFreeBytes == 0 {
		l.MinFreeBytes = 1 << 30
	}
	if l.ReceiveSlots == 0 {
		l.ReceiveSlots = 8
	}
	if l.DecodeSlots == 0 {
		l.DecodeSlots = 1
	}
	if l.SlotWait == 0 {
		l.SlotWait = 2 * time.Second
	}
	s.media = mediaState{limits: l, rate: newSaveLimiter(20), recv: make(chan struct{}, l.ReceiveSlots), decode: make(chan struct{}, l.DecodeSlots), inflight: map[string]bool{}}
}

func acquire(ch chan struct{}, wait time.Duration) bool {
	select {
	case ch <- struct{}{}:
		return true
	default:
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case ch <- struct{}{}:
		return true
	case <-t.C:
		return false
	}
}

func mediaUnavailable(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusServiceUnavailable, "MEDIA_NOT_CONFIGURED", "画像の保存先が設定されていません")
}

func mediaBusy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "5")
	httpx.Error(w, r, http.StatusServiceUnavailable, "BUSY", "混み合っています。しばらくしてからお試しください")
}

type mediaView struct {
	ID        string `json:"id"`
	PublicURL string `json:"publicUrl"`
	URL       string `json:"url"`
	PublicID  string `json:"publicId"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	MimeType  string `json:"mimeType"`
	ByteSize  int64  `json:"byteSize"`
	Purpose   string `json:"purpose"`
}

func toMediaView(m store.MediaAsset) mediaView {
	return mediaView{ID: m.ID.String(), PublicID: m.PublicID, PublicURL: "/api/v1/public/media/" + m.PublicID, URL: "/api/v1/media/" + m.ID.String(),
		Width: int(m.Width), Height: int(m.Height), MimeType: m.MimeType, ByteSize: m.ByteSize, Purpose: m.Purpose}
}

func newPublicID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// uploadMedia は POST /media?purpose=cms（Member）。生のバイト列を受け、検査・縮小・再エンコードして保存する。
func (s *server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	a, ok := s.authorize(w, r, "media.upload", authz.Resource{})
	if !ok {
		return
	}
	if s.Media == nil {
		mediaUnavailable(w, r)
		return
	}
	purpose := media.Purpose(r.URL.Query().Get("purpose"))
	switch purpose {
	case media.CMS:
	case media.Avatar, media.Inventory:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "この用途の画像はまだアップロードできません")
		return
	default:
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "purposeを指定してください")
		return
	}
	switch ct := r.Header.Get("Content-Type"); {
	case ct == "image/jpeg", ct == "image/png", ct == "image/webp", ct == "application/octet-stream":
	default:
		httpx.Error(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "JPEG・PNG・WebPの画像を、生のバイト列として送信してください")
		return
	}
	uid := a.Identity.UserID.String()
	lim := s.media.limits
	// 1. 安い検査: 頻度・1日の上限・空き容量・同時処理（本文を読む前）
	if ok, retry := s.media.rate.allow(uid, s.Now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
		httpx.Error(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "アップロードの回数が多すぎます。しばらくしてからお試しください")
		return
	}
	usage, err := store.New(s.Pool).UserMediaUsage(r.Context(), store.UserMediaUsageParams{UploadedBy: a.Identity.UserID, CreatedAt: pgtype.Timestamptz{Time: s.Now().Add(-24 * time.Hour), Valid: true}})
	if err != nil {
		s.internal(w, r)
		return
	}
	if usage.N >= lim.DailyCount || usage.Bytes >= lim.DailyBytes {
		httpx.Error(w, r, http.StatusTooManyRequests, "DAILY_LIMIT", "1日にアップロードできる画像の上限に達しました。明日もう一度お試しください")
		return
	}
	if free, err := s.Media.FreeBytes(); err != nil || free < lim.MinFreeBytes {
		httpx.Error(w, r, http.StatusInsufficientStorage, "STORAGE_FULL", "画像の保存先の空きが足りません。管理者に連絡してください")
		return
	}
	s.media.mu.Lock()
	if s.media.inflight[uid] {
		s.media.mu.Unlock()
		httpx.Error(w, r, http.StatusTooManyRequests, "BUSY", "前の画像を処理しています。終わってからもう一度お試しください")
		return
	}
	s.media.inflight[uid] = true
	s.media.mu.Unlock()
	defer func() { s.media.mu.Lock(); delete(s.media.inflight, uid); s.media.mu.Unlock() }()
	// 受信中の枠は、本文を読む前に取る（遅い回線の接続でメモリを占有されないように件数を制限する）
	if !acquire(s.media.recv, lim.SlotWait) {
		mediaBusy(w, r)
		return
	}
	released := false
	releaseRecv := func() {
		if !released {
			released = true
			<-s.media.recv
		}
	}
	defer releaseRecv()
	// このハンドラーだけ期限を延長する（サーバー全体のReadTimeout/WriteTimeoutでは遅い回線で10MiBを送り切れない）
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(2 * time.Minute))
	_ = rc.SetWriteDeadline(time.Now().Add(3 * time.Minute))
	// 2. 本文（上限まで。超えたら読み切らずに413）
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxBytes(purpose))
	data, err := io.ReadAll(r.Body)
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		httpx.Error(w, r, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "画像が大きすぎます（10MiBまで）")
		return
	}
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "INVALID_REQUEST", "画像を受け取れませんでした")
		return
	}
	if len(data) == 0 {
		httpx.Error(w, r, http.StatusUnprocessableEntity, "INVALID_INPUT", "画像が空です")
		return
	}
	// 3. 形式と構造の検査（デコードしない）
	info, err := media.Scan(data)
	if err != nil {
		s.mediaErr(w, r, err)
		return
	}
	// 4. 重い処理（デコード・縮小・再エンコード）は、同時数を制限する
	if !acquire(s.media.decode, lim.SlotWait) {
		mediaBusy(w, r)
		return
	}
	start := time.Now()
	if s.OnMediaDecode != nil {
		s.OnMediaDecode(true)
	}
	res, perr := media.Process(data, info, purpose)
	if s.OnMediaDecode != nil {
		s.OnMediaDecode(false)
	}
	<-s.media.decode
	data = nil // 元のバイト列を早く解放する
	releaseRecv()
	if perr != nil {
		s.mediaErr(w, r, perr)
		return
	}
	// 5. 保存（ファイル→DBの順。DBに失敗したらファイルを消す）
	var out store.MediaAsset
	q := store.New(s.Pool)
	id, err := q.NewUUID(r.Context())
	if err != nil {
		s.internal(w, r)
		return
	}
	key, err := media.NewKey(id.String(), res.Ext)
	pid, perr2 := newPublicID()
	if err != nil || perr2 != nil {
		s.internal(w, r)
		return
	}
	if err := s.Media.Put(key, res.Data); err != nil {
		s.Log.Error("画像を保存できません", "requestId", httpx.RequestID(r.Context()))
		s.internal(w, r)
		return
	}
	sum := sha256.Sum256(res.Data)
	out, err = q.InsertMediaAsset(r.Context(), store.InsertMediaAssetParams{ID: id, PublicID: pid, StorageKey: key, MimeType: res.Mime, ByteSize: int64(len(res.Data)),
		Width: int32(res.W), Height: int32(res.H), Sha256: hex.EncodeToString(sum[:]), Purpose: string(purpose), UploadedBy: a.Identity.UserID})
	if err != nil {
		_ = s.Media.Remove(key)
		s.internal(w, r)
		return
	}
	s.Log.Info("画像を取り込みました", "format", string(res.Source), "width", res.W, "height", res.H, "bytes", len(res.Data), "ms", time.Since(start).Milliseconds())
	httpx.JSON(w, http.StatusCreated, toMediaView(out))
}

func (s *server) mediaErr(w http.ResponseWriter, r *http.Request, err error) {
	var me *media.Error
	if errors.As(err, &me) {
		code := "INVALID_INPUT"
		if me.Status == http.StatusUnsupportedMediaType {
			code = "UNSUPPORTED_MEDIA_TYPE"
		}
		httpx.Error(w, r, me.Status, code, me.Message)
		return
	}
	s.internal(w, r)
}

func mediaNotFound(w http.ResponseWriter, r *http.Request) {
	httpx.Error(w, r, http.StatusNotFound, "NOT_FOUND", "見つかりません")
}

// canViewMedia: アップロード者、Editor以上、またはその画像を参照するコンテンツの作者・共同編集者。
func (s *server) canViewMedia(r *http.Request, q *store.Queries, a authCtx, m store.MediaAsset, action string) (bool, error) {
	res := authz.Resource{IsUploader: m.UploadedBy == a.Identity.UserID}
	if !res.IsUploader && !a.Identity.Subject().IsEditorPlus() {
		via, err := q.MediaViewableViaContent(r.Context(), store.MediaViewableViaContentParams{MediaID: m.ID, UserID: a.Identity.UserID})
		if err != nil {
			return false, err
		}
		res.IsMediaContentMember = via
	}
	return authz.Decide(a.Identity.Subject(), action, res).Allowed, nil
}

// serveMedia はファイルを返す。Content-TypeはDBの値（ファイル名・拡張子に依存しない）。
func (s *server) serveMedia(w http.ResponseWriter, r *http.Request, m store.MediaAsset, public bool) {
	if m.RedactedAt.Valid || !media.ValidKey(m.StorageKey) {
		mediaNotFound(w, r)
		return
	}
	f, err := s.Media.Open(m.StorageKey)
	if err != nil {
		s.Log.Error("画像ファイルを開けません", "requestId", httpx.RequestID(r.Context()))
		mediaNotFound(w, r)
		return
	}
	defer f.Close()
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(3 * time.Minute))
	h := w.Header()
	h.Set("Content-Type", m.MimeType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Disposition", "inline")
	h.Set("Referrer-Policy", "no-referrer")
	if public {
		h.Set("Cache-Control", "no-cache")
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
		h.Set("ETag", `"`+m.Sha256+`"`)
	} else {
		h.Set("Cache-Control", "private, no-store")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
	}
	http.ServeContent(w, r, "", m.CreatedAt.Time, f) // Range・If-None-Matchはここで処理（公開は参照判定の後にだけ到達する）
}

func (s *server) getMediaCommon(w http.ResponseWriter, r *http.Request, action string, lookup func(*store.Queries) (store.MediaAsset, error)) {
	a, ok := s.memberOrReject(w, r)
	if !ok {
		return
	}
	if s.Media == nil {
		mediaUnavailable(w, r)
		return
	}
	q := store.New(s.Pool)
	m, err := lookup(q)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			mediaNotFound(w, r)
			return
		}
		s.internal(w, r)
		return
	}
	ok, err = s.canViewMedia(r, q, a, m, action)
	if err != nil {
		s.internal(w, r)
		return
	}
	if !ok {
		mediaNotFound(w, r) // 権限が無い人には存在しないのと同じ404
		return
	}
	s.serveMedia(w, r, m, false)
}

// getMedia は GET /media/{id}（認証必須）。
func (s *server) getMedia(w http.ResponseWriter, r *http.Request) {
	id, ok := parseItemID(w, r)
	if !ok {
		return
	}
	s.getMediaCommon(w, r, "media.get", func(q *store.Queries) (store.MediaAsset, error) { return q.GetMediaAsset(r.Context(), id) })
}

// getMediaByPublic は GET /media/by-public/{publicId}（認証必須。本文のURLからのプレビュー用）。
func (s *server) getMediaByPublic(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("publicId")
	s.getMediaCommon(w, r, "media.get_by_public", func(q *store.Queries) (store.MediaAsset, error) {
		return q.GetMediaAssetByPublicID(r.Context(), pid)
	})
}

// getPublicMedia は GET /public/media/{publicId}（匿名）。公開中の版が参照している画像だけ。それ以外は同じ404。
func (s *server) getPublicMedia(w http.ResponseWriter, r *http.Request) {
	if s.Media == nil {
		mediaNotFound(w, r)
		return
	}
	m, err := store.New(s.Pool).GetServablePublicMedia(r.Context(), r.PathValue("publicId"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			mediaNotFound(w, r)
			return
		}
		s.internal(w, r)
		return
	}
	s.serveMedia(w, r, m, true)
}
