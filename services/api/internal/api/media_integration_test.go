//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nu-digital/nu_app/services/api/internal/api"
	"github.com/nu-digital/nu_app/services/api/internal/media"
	xwebp "golang.org/x/image/webp"
)

func testGradient(w, h int, seed int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetNRGBA(x, y, color.NRGBA{uint8((x + seed) * 255 / max(1, w-1)), uint8(y * 255 / max(1, h-1)), uint8(seed), 255})
		}
	}
	return m
}

func pngImg(t *testing.T, w, h, seed int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, testGradient(w, h, seed)); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegImg(t *testing.T, w, h int) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, testGradient(w, h, 3), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type mediaOut struct {
	ID, PublicURL, URL, PublicID, MimeType, Purpose string
	Width, Height, ByteSize                         int
}

// mediaSetup はCMSの登場人物に加えて、MEDIA_DIRを一時ディレクトリに設定する。
func mediaSetup(t *testing.T) (*env, cActors, string) {
	e, as := cmsSetup(t)
	dir := t.TempDir()
	st, err := media.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e.media = st
	e.mlimits = api.MediaLimits{MinFreeBytes: 1} // テスト環境の空き容量に依存しない
	e.h = e.build()
	return e, as, dir
}

func (e *env) upload(a actor, purpose, ct string, body []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	path := "/media"
	if purpose != "" {
		path += "?purpose=" + purpose
	}
	opts := []reqOpt{withCookie(a.cookie), withHeader("X-CSRF-Token", a.csrf)}
	if ct != "" {
		opts = append(opts, withHeader("Content-Type", ct))
	}
	return e.do("POST", path, string(body), opts...)
}

func (e *env) uploadOK(a actor, body []byte) mediaOut {
	e.t.Helper()
	rec := e.upload(a, "cms", "application/octet-stream", body)
	var m mediaOut
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != 201 {
		e.t.Fatalf("アップロード: %d %s", rec.Code, rec.Body.String())
	}
	return m
}

func (e *env) mediaGet(a actor, path string, o ...reqOpt) *httptest.ResponseRecorder {
	return e.do("GET", path, "", append([]reqOpt{withCookie(a.cookie)}, o...)...)
}

func TestMediaUploadValidation(t *testing.T) {
	e, as, dir := mediaSetup(t)
	png1 := pngImg(t, 40, 30, 1)
	if rec := e.do("POST", "/media?purpose=cms", string(png1), withHeader("Content-Type", "image/png")); rec.Code != 401 {
		t.Errorf("匿名: %d", rec.Code)
	}
	// 入力の検査
	for name, c := range map[string]struct {
		purpose, ct string
		body        []byte
		status      int
	}{
		"purposeなし":         {"", "image/png", png1, 422},
		"未知のpurpose":        {"x", "image/png", png1, 422},
		"Content-Typeがhtml": {"cms", "text/html", png1, 415},
		"JSONで送る":           {"cms", "application/json", png1, 415},
		"空":                 {"cms", "image/png", nil, 422},
		"SVG":               {"cms", "image/png", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 415},
		"GIF":               {"cms", "image/png", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), 415},
		"HTML":              {"cms", "image/jpeg", []byte("<html><script>alert(1)</script></html>"), 415},
		"テキスト":              {"cms", "application/octet-stream", []byte("これは画像ではない"), 415},
		"切れたPNG":            {"cms", "image/png", png1[:len(png1)/2], 422},
	} {
		rec := e.upload(as.author, c.purpose, c.ct, c.body)
		if rec.Code != c.status {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "alert(") {
			t.Errorf("%s: 応答に入力の内容が出ている", name)
		}
	}
	if n := e.count(`SELECT count(*)::int FROM media_assets`); n != 0 {
		t.Errorf("拒否したのに行ができた: %d", n)
	}
	// 上限+1バイトは413（10MiB）
	if rec := e.upload(as.author, "cms", "application/octet-stream", bytes.Repeat([]byte{0xFF}, 10<<20+1)); rec.Code != 413 {
		t.Errorf("上限超過: %d", rec.Code)
	}
	// 成功: 応答に保存先のパスを出さない。ファイルは0640で、再エンコード後
	m := e.uploadOK(as.author, png1)
	if m.Width != 40 || m.Height != 30 || m.MimeType != "image/webp" || m.Purpose != "cms" || len(m.PublicID) != 22 || m.PublicURL != "/api/v1/public/media/"+m.PublicID || m.URL != "/api/v1/media/"+m.ID {
		t.Fatalf("応答: %+v", m)
	}
	rec := e.upload(as.author, "cms", "image/png", png1)
	if strings.Contains(rec.Body.String(), dir) || strings.Contains(rec.Body.String(), "storage") || strings.Contains(rec.Body.String(), ".png\"") {
		t.Errorf("応答に保存先の情報が出ている: %s", rec.Body.String())
	}
	var key string
	var size int
	if err := e.pool.QueryRow(context.Background(), `SELECT storage_key, byte_size FROM media_assets WHERE id=$1`, m.ID).Scan(&key, &size); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, key))
	if err != nil || st.Mode().Perm() != 0o640 || int(st.Size()) != size {
		t.Errorf("保存したファイル: %v %v", st, err)
	}
	if es, _ := os.ReadDir(filepath.Join(dir, "tmp")); len(es) != 0 {
		t.Errorf("tmpに残っている: %d", len(es))
	}
	// public_idはidから導けない・毎回違う
	if strings.Contains(m.PublicID, m.ID[:8]) {
		t.Errorf("public_idがidを含む")
	}
}

// MEDIA_DIRが未設定なら503（アップロード・配信とも）。
func TestMediaNotConfigured(t *testing.T) {
	e, as := cmsSetup(t)
	if rec := e.upload(as.author, "cms", "image/png", pngImg(t, 10, 10, 1)); rec.Code != 503 || !strings.Contains(rec.Body.String(), "MEDIA_NOT_CONFIGURED") {
		t.Errorf("アップロード: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.mediaGet(as.author, "/media/00000000-0000-0000-0000-000000000000"); rec.Code != 503 {
		t.Errorf("配信: %d", rec.Code)
	}
}

func TestMediaStripsMetadataAndTrailingData(t *testing.T) {
	e, as, dir := mediaSetup(t)
	j := jpegImg(t, 80, 60)
	// EXIF（GPS・撮影者）と、画像の後ろの付加データ
	seg := append([]byte("Exif\x00\x00"), []byte("II*\x00\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00GPSLatitude=35.0 撮影者=秘密の人")...)
	var withExif bytes.Buffer
	withExif.Write(j[:2])
	withExif.Write([]byte{0xFF, 0xE1})
	binary.Write(&withExif, binary.BigEndian, uint16(len(seg)+2))
	withExif.Write(seg)
	withExif.Write(j[2:])
	withExif.WriteString("<script>alert(1)</script>PK\x03\x04ZIPTRAILER")
	m := e.uploadOK(as.author, withExif.Bytes())
	var key string
	_ = e.pool.QueryRow(context.Background(), `SELECT storage_key FROM media_assets WHERE id=$1`, m.ID).Scan(&key)
	out, err := os.ReadFile(filepath.Join(dir, key))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Exif", "GPSLatitude", "秘密の人", "<script>", "ZIPTRAILER"} {
		if bytes.Contains(out, []byte(leak)) {
			t.Errorf("保存したファイルに %q が残っている", leak)
		}
	}
	if _, err := xwebp.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("保存したファイルが読めない: %v", err)
	}
	// 長辺が上限を超える画像は縮む
	big := e.uploadOK(as.author, jpegImg(t, 3000, 1500))
	if big.Width != 2560 || big.Height != 1280 {
		t.Errorf("縮小: %dx%d", big.Width, big.Height)
	}
}

// 利用者ごとの上限・頻度・同時処理、空き容量。
func TestMediaLimits(t *testing.T) {
	e, as, _ := mediaSetup(t)
	e.mlimits = api.MediaLimits{DailyCount: 3, DailyBytes: 1 << 30, MinFreeBytes: 1}
	e.h = e.build()
	for i := 0; i < 3; i++ {
		e.uploadOK(as.author, pngImg(t, 20, 20, i))
	}
	if rec := e.upload(as.author, "cms", "image/png", pngImg(t, 20, 20, 9)); rec.Code != 429 || !strings.Contains(rec.Body.String(), "DAILY_LIMIT") {
		t.Errorf("1日の枚数: %d %s", rec.Code, rec.Body.String())
	}
	// 他の利用者は影響を受けない。1日の合計バイトの上限
	e.uploadOK(as.other, pngImg(t, 20, 20, 1))
	e.mlimits = api.MediaLimits{DailyCount: 100, DailyBytes: 10, MinFreeBytes: 1}
	e.h = e.build()
	if rec := e.upload(as.author, "cms", "image/png", pngImg(t, 20, 20, 8)); rec.Code != 429 {
		t.Errorf("1日の合計バイト: %d", rec.Code)
	}
	// 空き容量が足りなければ507（本文を読む前）
	e.mlimits = api.MediaLimits{MinFreeBytes: 1 << 62}
	e.h = e.build()
	if rec := e.upload(as.editor, "cms", "image/png", pngImg(t, 20, 20, 1)); rec.Code != 507 || !strings.Contains(rec.Body.String(), "STORAGE_FULL") {
		t.Errorf("空き容量: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMediaUploadRateLimit(t *testing.T) {
	e, as, _ := mediaSetup(t)
	small := pngImg(t, 4, 4, 1)
	var last int
	for i := 0; i < 21; i++ {
		last = e.upload(as.author, "cms", "image/png", small).Code
		if i < 20 && last != 201 {
			t.Fatalf("%d回目: %d", i+1, last)
		}
	}
	if last != 429 {
		t.Errorf("21回目: %d", last)
	}
	if rec := e.upload(as.other, "cms", "image/png", small); rec.Code != 201 {
		t.Errorf("他の利用者: %d", rec.Code)
	}
}

// 同じ利用者の同時アップロードは1件ずつ。全体の同時デコード数は上限を超えない。
func TestMediaConcurrency(t *testing.T) {
	e, as, _ := mediaSetup(t)
	var cur, peak int32
	block := make(chan struct{})
	entered := make(chan struct{}, 20)
	e.onDecode = func(enter bool) {
		if enter {
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			entered <- struct{}{}
			<-block
		} else {
			atomic.AddInt32(&cur, -1)
		}
	}
	e.mlimits = api.MediaLimits{MinFreeBytes: 1, DecodeSlots: 2, SlotWait: 300 * time.Millisecond}
	e.h = e.build()
	img := pngImg(t, 30, 30, 1)
	// 同じ利用者の2件目は、1件目の処理中に429
	var first int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); first = e.upload(as.author, "cms", "image/png", img).Code }()
	<-entered
	if rec := e.upload(as.author, "cms", "image/png", img); rec.Code != 429 {
		t.Errorf("同じ利用者の同時アップロード: %d", rec.Code)
	}
	close(block)
	wg.Wait()
	if first != 201 {
		t.Errorf("1件目: %d", first)
	}
	// 別々の利用者10人が同時に: 同時デコードは2まで。取れなかったものは503＋Retry-After
	block = make(chan struct{})
	e.onDecode = func(enter bool) {
		if enter {
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(150 * time.Millisecond)
		} else {
			atomic.AddInt32(&cur, -1)
		}
	}
	atomic.StoreInt32(&peak, 0)
	e.h = e.build()
	users := []actor{as.author, as.collab, as.other, as.editor, as.admin}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("extra%d.example", i)
		e.addUser(id, "active", goodPass)
		users = append(users, e.signIn(id))
	}
	codes := make([]int, len(users))
	retry := make([]string, len(users))
	start := make(chan struct{})
	for i, u := range users {
		wg.Add(1)
		go func(i int, u actor) {
			defer wg.Done()
			<-start
			rec := e.upload(u, "cms", "image/png", pngImg(t, 30, 30, i))
			codes[i], retry[i] = rec.Code, rec.Header().Get("Retry-After")
		}(i, u)
	}
	close(start)
	wg.Wait()
	ok, busy := 0, 0
	for i, c := range codes {
		switch c {
		case 201:
			ok++
		case 503:
			busy++
			if retry[i] == "" {
				t.Errorf("503にRetry-Afterが無い")
			}
		default:
			t.Errorf("想定外: %d", c)
		}
	}
	if p := atomic.LoadInt32(&peak); p > 2 || p == 0 {
		t.Errorf("同時デコード数の最大 %d（2まで）", p)
	}
	if ok < 2 || ok+busy != len(users) {
		t.Errorf("成功%d・混雑%d", ok, busy)
	}
}

func TestMediaServeAuthorization(t *testing.T) {
	e, as, _ := mediaSetup(t)
	m := e.uploadOK(as.author, pngImg(t, 40, 30, 1))
	missing := "00000000-0000-0000-0000-000000000000"
	for _, path := range []string{"/media/" + m.ID, "/media/by-public/" + m.PublicID} {
		if rec := e.do("GET", path, ""); rec.Code != 401 {
			t.Errorf("匿名 %s: %d", path, rec.Code)
		}
		for name, a := range map[string]actor{"アップロード者": as.author, "Editor": as.editor, "Admin": as.admin} {
			rec := e.mediaGet(a, path)
			if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/webp" {
				t.Errorf("%s %s: %d %s", name, path, rec.Code, rec.Header().Get("Content-Type"))
			}
			h := rec.Header()
			if h.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("Cache-Control") != "private, no-store" ||
				h.Get("Cross-Origin-Resource-Policy") != "same-origin" || h.Get("Content-Disposition") != "inline" || h.Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("%s: ヘッダー %v", path, h)
			}
			if _, err := xwebp.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
				t.Errorf("%s: 本文が画像でない", path)
			}
		}
		// 関係の無いメンバーは、存在しないものと同じ404（同じ本文）
		c1 := e.mediaGet(as.other, path)
		missingPath := strings.Replace(path, m.ID, missing, 1)
		if strings.Contains(path, "by-public") {
			missingPath = "/media/by-public/ZZZZZZZZZZZZZZZZZZZZZZ"
		}
		c2 := e.mediaGet(as.other, missingPath)
		norm := func(s string) string { return s[:strings.Index(s, `"requestId"`)] }
		if c1.Code != 404 || c2.Code != 404 || norm(c1.Body.String()) != norm(c2.Body.String()) {
			t.Errorf("関係の無いメンバー %s: %d / 存在しない: %d", path, c1.Code, c2.Code)
		}
	}
	// 形式の不正なid
	for _, p := range []string{"/media/not-a-uuid", "/media/by-public/short"} {
		if rec := e.mediaGet(as.author, p); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	// Range
	rec := e.mediaGet(as.author, "/media/"+m.ID, withHeader("Range", "bytes=0-9"))
	if rec.Code != 206 || rec.Body.Len() != 10 {
		t.Errorf("Range: %d %d", rec.Code, rec.Body.Len())
	}
	// 共同編集者は、その画像を参照するコンテンツの下書きの作者・共同編集者であれば、プレビューできる
	_, v := e.createContent(as.author, article("preview-post"))
	e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String()))
	if rec := e.mediaGet(as.collab, "/media/by-public/"+m.PublicID); rec.Code != 404 {
		t.Errorf("参照されていないあいだは共同編集者にも見えない: %d", rec.Code)
	}
	body := fmt.Sprintf(`{"baseRevision":1,"title":"t","markdown":"![x](/api/v1/public/media/%s)"}`, m.PublicID)
	if rec := e.as(as.author, "POST", "/contents/"+v.ID+"/revisions", body); rec.Code != 201 {
		t.Fatalf("参照: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.mediaGet(as.collab, "/media/by-public/"+m.PublicID); rec.Code != 200 {
		t.Errorf("共同編集者のプレビュー: %d", rec.Code)
	}
	if rec := e.mediaGet(as.collab, "/media/"+m.ID); rec.Code != 200 {
		t.Errorf("共同編集者(id): %d", rec.Code)
	}
	if rec := e.mediaGet(as.other, "/media/by-public/"+m.PublicID); rec.Code != 404 {
		t.Errorf("無関係なメンバー: %d", rec.Code)
	}
}

func (e *env) pubMedia(publicID string, o ...reqOpt) *httptest.ResponseRecorder {
	return e.do("GET", "/public/media/"+publicID, "", o...)
}

func mdImg(publicIDs ...string) string {
	var b strings.Builder
	for _, p := range publicIDs {
		fmt.Fprintf(&b, "![x](/api/v1/public/media/%s)\n\n", p)
	}
	return b.String()
}

func (e *env) saveWith(a actor, id string, base int, title, md string, extra string) (int, *httptest.ResponseRecorder) {
	e.t.Helper()
	body := fmt.Sprintf(`{"baseRevision":%d,"title":%q,"markdown":%q%s}`, base, title, md, extra)
	rec := e.as(a, "POST", "/contents/"+id+"/revisions", body)
	return rec.Code, rec
}

// 公開境界: 公開中の版が参照している間だけ、匿名に配信される。
func TestPublicMediaBoundary(t *testing.T) {
	e, as, _ := mediaSetup(t)
	m := e.uploadOK(as.author, pngImg(t, 40, 30, 1))
	// 参照が無い・存在しない・不正な形は、同じ404
	norm := func(s string) string { return s[:strings.Index(s, `"requestId"`)] }
	a, b := e.pubMedia(m.PublicID), e.pubMedia("ZZZZZZZZZZZZZZZZZZZZZZ")
	if a.Code != 404 || b.Code != 404 || norm(a.Body.String()) != norm(b.Body.String()) {
		t.Fatalf("未参照: %d / %d", a.Code, b.Code)
	}
	if rec := e.pubMedia(m.ID); rec.Code != 404 { // idでは引けない
		t.Errorf("idで公開側を引けた: %d", rec.Code)
	}
	// 下書きだけが参照 → まだ匿名には出ない
	_, v := e.createContent(as.author, article("boundary"))
	if code, rec := e.saveWith(as.author, v.ID, 1, "版2", mdImg(m.PublicID), ""); code != 201 {
		t.Fatalf("%d %s", code, rec.Body.String())
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 404 {
		t.Errorf("下書きだけが参照: %d", rec.Code)
	}
	// 公開 → 匿名に配信される
	if code, _ := e.publish(as.editor, v.ID, 2); code != 200 {
		t.Fatal(code)
	}
	rec := e.pubMedia(m.PublicID)
	h := rec.Header()
	if rec.Code != 200 || h.Get("Content-Type") != "image/webp" || h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "no-cache" ||
		h.Get("Cross-Origin-Resource-Policy") != "cross-origin" || !strings.HasPrefix(h.Get("ETag"), `"`) || strings.HasPrefix(h.Get("ETag"), "W/") || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("公開配信: %d %v", rec.Code, h)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("Cookieを発行した")
	}
	// 認証情報で応答が変わらない
	with := e.pubMedia(m.PublicID, withCookie(as.editor.cookie))
	if !bytes.Equal(with.Body.Bytes(), rec.Body.Bytes()) || with.Header().Get("ETag") != h.Get("ETag") {
		t.Errorf("認証情報で応答が変わった")
	}
	// 304は、参照判定の後にだけ返る
	if r := e.pubMedia(m.PublicID, withHeader("If-None-Match", h.Get("ETag"))); r.Code != 304 {
		t.Errorf("304: %d", r.Code)
	}
	// 公開停止 → すぐ404（ETagを持っていても304にならない）
	if rec := e.as(as.editor, "POST", "/contents/"+v.ID+"/unpublish", ""); rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	if r := e.pubMedia(m.PublicID, withHeader("If-None-Match", h.Get("ETag"))); r.Code != 404 {
		t.Errorf("公開停止後: %d", r.Code)
	}
	// 再公開 → また配信される。次の版で画像を外して再公開 → 404（古い版だけが参照している）
	if code, _ := e.publish(as.editor, v.ID, 2); code != 200 {
		t.Fatal(code)
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 200 {
		t.Errorf("再公開: %d", rec.Code)
	}
	if code, rec := e.saveWith(as.author, v.ID, 2, "版3（画像なし）", "画像を外した", ""); code != 201 {
		t.Fatalf("%d %s", code, rec.Body.String())
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 200 {
		t.Errorf("保存だけでは公開版が変わらない: %d", rec.Code)
	}
	if code, _ := e.publish(as.editor, v.ID, 3); code != 200 {
		t.Fatal(code)
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 404 {
		t.Errorf("古い版だけが参照: %d", rec.Code)
	}
	// 2つのコンテンツが同じ画像を参照: 一方の公開停止で他方は影響を受けない
	_, v1 := e.createContent(as.author, article("share-a"))
	_, v2 := e.createContent(as.author, article("share-b"))
	for _, c := range []cView{v1, v2} {
		if code, rec := e.saveWith(as.author, c.ID, 1, "共有", mdImg(m.PublicID), ""); code != 201 {
			t.Fatalf("%d %s", code, rec.Body.String())
		}
		if code, _ := e.publish(as.editor, c.ID, 2); code != 200 {
			t.Fatal(code)
		}
	}
	e.as(as.editor, "POST", "/contents/"+v1.ID+"/unpublish", "")
	if rec := e.pubMedia(m.PublicID); rec.Code != 200 {
		t.Errorf("他方が公開中: %d", rec.Code)
	}
	e.as(as.editor, "POST", "/contents/"+v2.ID+"/unpublish", "")
	if rec := e.pubMedia(m.PublicID); rec.Code != 404 {
		t.Errorf("両方停止: %d", rec.Code)
	}
}

// 参照の整合性: 本文・カバーの画像は、自分の画像・そのコンテンツが既に参照する画像・Editor以上だけ。版を作る全経路で引き継がれる。
func TestMediaReferenceIntegrity(t *testing.T) {
	e, as, _ := mediaSetup(t)
	mine := e.uploadOK(as.author, pngImg(t, 30, 30, 1))
	theirs := e.uploadOK(as.other, pngImg(t, 30, 30, 2))
	_, v := e.createContent(as.author, article("refs"))
	norm := func(s string) string { return s[:strings.Index(s, `"requestId"`)] }
	// 存在しない・他人の画像は、区別できない同じ422
	c1, r1 := e.saveWith(as.author, v.ID, 1, "t", mdImg("ZZZZZZZZZZZZZZZZZZZZZZ"), "")
	c2, r2 := e.saveWith(as.author, v.ID, 1, "t", mdImg(theirs.PublicID), "")
	if c1 != 422 || c2 != 422 || norm(r1.Body.String()) != norm(r2.Body.String()) {
		t.Errorf("存在しない=%d 他人の画像=%d\n%s\n%s", c1, c2, r1.Body.String(), r2.Body.String())
	}
	for name, md := range map[string]string{
		"クエリつき":    fmt.Sprintf("![x](/api/v1/public/media/%s?x=1)", mine.PublicID),
		"認証つきのURL": fmt.Sprintf("![x](/api/v1/media/%s)", mine.ID),
		"外部画像":     "![x](https://tracker.example/p.png)",
	} {
		if code, _ := e.saveWith(as.author, v.ID, 1, "t", md, ""); code != 422 {
			t.Errorf("%s: %d", name, code)
		}
	}
	// コードブロックのURLは参照にならない
	code, rec := e.saveWith(as.author, v.ID, 1, "コード", "```\n![x](/api/v1/public/media/"+theirs.PublicID+")\n```", "")
	if code != 201 {
		t.Fatalf("コードブロック: %d %s", code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*)::int FROM media_references WHERE media_id=$1`, theirs.ID); n != 0 {
		t.Errorf("コードブロックのURLが参照になった: %d", n)
	}
	// 自分の画像: 本文とカバー。参照が作られ、応答にカバーが出る
	code, rec = e.saveWith(as.author, v.ID, 2, "画像つき", mdImg(mine.PublicID), fmt.Sprintf(`,"coverMediaId":%q`, mine.ID))
	var got cView
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if code != 201 || got.WorkingRevision.RevisionNo != 3 {
		t.Fatalf("画像つき: %d %s", code, rec.Body.String())
	}
	if n := e.count(`SELECT count(*)::int FROM media_references r JOIN content_revisions v ON v.id=r.owner_id WHERE v.content_id=$1 AND v.revision_no=3`, v.ID); n != 2 { // inline + cover
		t.Errorf("参照の数: %d", n)
	}
	var cover struct {
		WorkingRevision struct{ CoverMedia *mediaOut }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cover)
	if cover.WorkingRevision.CoverMedia == nil || cover.WorkingRevision.CoverMedia.ID != mine.ID || cover.WorkingRevision.CoverMedia.PublicURL == "" {
		t.Errorf("カバー: %+v", cover.WorkingRevision.CoverMedia)
	}
	// coverMediaIdを省略した保存は維持。カバーだけの変更は新しい版。nullで消せる
	if code, _ := e.saveWith(as.author, v.ID, 3, "題名を変える", mdImg(mine.PublicID), ""); code != 201 {
		t.Fatal(code)
	}
	if n := e.count(`SELECT count(*)::int FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.id=$1 AND r.cover_media_id=$2`, v.ID, mine.ID); n != 1 {
		t.Errorf("省略でカバーが維持されない")
	}
	if code, _ := e.saveWith(as.author, v.ID, 4, "題名を変える", mdImg(mine.PublicID), ""); code != 200 { // 同じ内容
		t.Errorf("同じ内容: %d", code)
	}
	other := e.uploadOK(as.author, pngImg(t, 30, 30, 5))
	if code, _ := e.saveWith(as.author, v.ID, 4, "題名を変える", mdImg(mine.PublicID), fmt.Sprintf(`,"coverMediaId":%q`, other.ID)); code != 201 {
		t.Errorf("カバーだけの変更は新しい版: %d", code)
	}
	if code, _ := e.saveWith(as.author, v.ID, 5, "題名を変える", mdImg(mine.PublicID), `,"coverMediaId":null`); code != 201 {
		t.Errorf("カバーを消す: %d", code)
	}
	if n := e.count(`SELECT count(*)::int FROM contents c JOIN content_revisions r ON r.id=c.working_revision_id WHERE c.id=$1 AND r.cover_media_id IS NULL`, v.ID); n != 1 {
		t.Errorf("カバーが消えていない")
	}
	// 他人の画像をカバーに: 422。そのコンテンツが既に参照している画像なら、共同編集者も使える。Editor以上は誰の画像でも
	if code, _ := e.saveWith(as.author, v.ID, 6, "t", mdImg(mine.PublicID), fmt.Sprintf(`,"coverMediaId":%q`, theirs.ID)); code != 422 {
		t.Errorf("他人の画像をカバーに: %d", code)
	}
	e.as(as.author, "PUT", "/contents/"+v.ID+"/collaborators", fmt.Sprintf(`{"userIds":[%q]}`, e.userID("collab.example").String()))
	if code, _ := e.saveWith(as.collab, v.ID, 6, "共同編集者", mdImg(mine.PublicID), ""); code != 201 {
		t.Errorf("既に参照されている画像は共同編集者も使える: %d", code)
	}
	if code, _ := e.saveWith(as.collab, v.ID, 7, "共同編集者", mdImg(mine.PublicID, theirs.PublicID), ""); code != 422 {
		t.Errorf("共同編集者が他人の新しい画像: %d", code)
	}
	if code, rec := e.saveWith(as.editor, v.ID, 7, "Editor", mdImg(mine.PublicID, theirs.PublicID), ""); code != 201 {
		t.Errorf("Editorは誰の画像でも: %d %s", code, rec.Body.String())
	}
	// 作成時にも参照できる（自分の画像）。他人の画像は422
	if code, _ := e.createContent(as.author, fmt.Sprintf(`{"type":"article","section":"blog","slug":"create-img","title":"t","markdown":%q,"coverMediaId":%q}`, mdImg(mine.PublicID), mine.ID)); code != 201 {
		t.Errorf("作成時の画像: %d", code)
	}
	if code, _ := e.createContent(as.author, fmt.Sprintf(`{"type":"article","section":"blog","slug":"create-img2","title":"t","markdown":%q}`, mdImg(theirs.PublicID))); code != 422 {
		t.Errorf("作成時に他人の画像: %d", code)
	}
	// 100枚まで
	many := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		many = append(many, "ABCDEFGHIJKLMNOPQRSTU"+string(rune('A'+i%26)))
	}
	if code, _ := e.saveWith(as.editor, v.ID, 8, "t", mdImg(many...), ""); code != 422 {
		t.Errorf("101枚: %d", code)
	}
	// DB: 参照のある画像の行は消せない（FK）。版への参照は追記専用
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM media_assets WHERE id=$1`, mine.ID); err == nil {
		t.Error("参照のある画像の行を削除できた")
	}
	if _, err := e.pool.Exec(context.Background(), `DELETE FROM media_references WHERE media_id=$1`, mine.ID); err == nil {
		t.Error("版への参照を削除できた")
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE media_references SET purpose='cover' WHERE media_id=$1`, mine.ID); err == nil {
		t.Error("版への参照を更新できた")
	}
}

// 版を作る全経路（制作メンバーの変更を含む）で画像参照が引き継がれ、公開後も画像が出る。
func TestMediaReferencesSurviveContributorChange(t *testing.T) {
	e, as, _ := mediaSetup(t)
	m := e.uploadOK(as.author, pngImg(t, 30, 30, 1))
	cv := e.uploadOK(as.author, pngImg(t, 30, 30, 2))
	code, w := e.createContent(as.author, fmt.Sprintf(`{"type":"project","section":"project","slug":"proj-img","title":"作品","markdown":%q,"coverMediaId":%q}`, mdImg(m.PublicID), cv.ID))
	if code != 201 {
		t.Fatal(code)
	}
	body := fmt.Sprintf(`{"baseRevision":1,"contributors":[{"userId":%q,"roleLabel":"企画"}]}`, e.userID("collab.example"))
	if rec := e.as(as.author, "PUT", "/contents/"+w.ID+"/contributors", body); rec.Code != 201 {
		t.Fatalf("制作メンバーの変更: %d %s", rec.Code, rec.Body.String())
	}
	if code, rec := e.publish(as.editor, w.ID, 2); code != 200 {
		t.Fatalf("公開: %d %s", code, rec.Body.String())
	}
	for _, p := range []string{m.PublicID, cv.PublicID} {
		if rec := e.pubMedia(p); rec.Code != 200 {
			t.Errorf("公開後の画像 %s: %d", p, rec.Code)
		}
	}
	rec := e.pub("/project/proj-img")
	if !strings.Contains(rec.Body.String(), `"coverUrl":"/api/v1/public/media/`+cv.PublicID+`"`) {
		t.Errorf("公開APIのカバー: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), cv.ID) || strings.Contains(rec.Body.String(), m.ID) {
		t.Errorf("公開APIに画像の内部idが出ている")
	}
	if !strings.Contains(e.pub("").Body.String(), `"coverUrl":"/api/v1/public/media/`+cv.PublicID+`"`) {
		t.Errorf("公開一覧のカバー")
	}
}

// 公開の直前の再確認: 本文の画像と保存済みの参照が食い違う版は公開しない。
func TestPublishRechecksMediaReferences(t *testing.T) {
	e, as, _ := mediaSetup(t)
	m := e.uploadOK(as.author, pngImg(t, 30, 30, 1))
	_, v := e.createContent(as.author, article("recheck-media"))
	// 検査を経ずに混入した版（本文は画像を参照するが、参照の行が無い）
	if _, err := e.pool.Exec(context.Background(), `INSERT INTO content_revisions (content_id, revision_no, title, markdown, edited_by) SELECT id, 2, 't', $2, creator_user_id FROM contents WHERE id=$1`, v.ID, mdImg(m.PublicID)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE contents SET working_revision_id=(SELECT id FROM content_revisions WHERE content_id=$1 AND revision_no=2) WHERE id=$1`, v.ID); err != nil {
		t.Fatal(err)
	}
	if code, rec := e.publish(as.editor, v.ID, 2); code != 422 {
		t.Errorf("参照が欠けた版の公開: %d %s", code, rec.Body.String())
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 404 {
		t.Errorf("公開されてしまった: %d", rec.Code)
	}
}

// 保存先に書けないときは行を作らない。
func TestMediaStorageFailureLeavesNoRow(t *testing.T) {
	e, as, dir := mediaSetup(t)
	// 空き容量は取れるが、tmpへ書けない状態（tmpをファイルに置き換える）
	if err := os.RemoveAll(filepath.Join(dir, "tmp")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := e.upload(as.author, "cms", "image/png", pngImg(t, 10, 10, 1)); rec.Code != 500 {
		t.Errorf("書き込めない: %d", rec.Code)
	}
	// 保存先そのものが無い場合は、空きを確認できないので507
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if rec := e.upload(as.editor, "cms", "image/png", pngImg(t, 10, 10, 1)); rec.Code != 507 {
		t.Errorf("保存先が無い: %d", rec.Code)
	}
	if n := e.count(`SELECT count(*)::int FROM media_assets`); n != 0 {
		t.Errorf("行が残った: %d", n)
	}
	_ = http.StatusOK
}

// redact（本人情報を含む画像の無効化）の効果: 配信・プレビュー・新規参照・公開が止まる。行と参照は残る。
func TestRedactedMediaIsNeitherServedNorReferenceable(t *testing.T) {
	e, as, _ := mediaSetup(t)
	m := e.uploadOK(as.author, pngImg(t, 30, 30, 1))
	_, v := e.createContent(as.author, article("redact-me"))
	if code, rec := e.saveWith(as.author, v.ID, 1, "t", mdImg(m.PublicID), ""); code != 201 {
		t.Fatalf("%d %s", code, rec.Body.String())
	}
	if code, _ := e.publish(as.editor, v.ID, 2); code != 200 {
		t.Fatal(code)
	}
	if rec := e.pubMedia(m.PublicID); rec.Code != 200 {
		t.Fatalf("公開中: %d", rec.Code)
	}
	if _, err := e.pool.Exec(context.Background(), `UPDATE media_assets SET redacted_at=now() WHERE id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"公開配信":       e.pubMedia(m.PublicID),
		"アップロード者":    e.mediaGet(as.author, "/media/"+m.ID),
		"Editor(id)": e.mediaGet(as.editor, "/media/"+m.ID),
		"プレビュー":      e.mediaGet(as.author, "/media/by-public/"+m.PublicID),
	} {
		if rec.Code != 404 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	// 新しい版からの参照（本文・カバー）は422。Editorでも同じ
	for _, a := range []actor{as.author, as.editor} {
		if code, _ := e.saveWith(a, v.ID, 2, "t2", mdImg(m.PublicID), ""); code != 422 {
			t.Errorf("redactedを本文で参照: %d", code)
		}
		if code, _ := e.saveWith(a, v.ID, 2, "t3", "本文", fmt.Sprintf(`,"coverMediaId":%q`, m.ID)); code != 422 {
			t.Errorf("redactedをカバーに: %d", code)
		}
	}
	// redactedを参照する既存の版の再公開は拒否される（公開停止→再公開）
	e.as(as.editor, "POST", "/contents/"+v.ID+"/unpublish", "")
	if code, _ := e.publish(as.editor, v.ID, 2); code != 422 {
		t.Errorf("redactedを参照する版の再公開: %d", code)
	}
	// 行と参照は残る（監査）
	if n := e.count(`SELECT count(*)::int FROM media_assets WHERE id=$1`, m.ID); n != 1 {
		t.Errorf("行が消えた")
	}
	if n := e.count(`SELECT count(*)::int FROM media_references WHERE media_id=$1`, m.ID); n != 1 {
		t.Errorf("参照が消えた: %d", n)
	}
}
