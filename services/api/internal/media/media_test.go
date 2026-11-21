package media

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	xwebp "golang.org/x/image/webp"
)

// ---- 画像の生成 ----

func gradient(w, h int) *image.NRGBA {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.SetNRGBA(x, y, color.NRGBA{uint8(x * 255 / max(1, w-1)), uint8(y * 255 / max(1, h-1)), 128, 255})
		}
	}
	return m
}

func pngBytes(t *testing.T, m image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegBytes(t *testing.T, m image.Image) []byte {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, m, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func b64(t *testing.T, s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 極小のWebP（1x1）。
func webpLossy(t *testing.T) []byte {
	return b64(t, "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
}
func webpLossless(t *testing.T) []byte {
	return b64(t, "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
}
func webpAlpha(t *testing.T) []byte {
	return b64(t, "UklGRkoAAABXRUJQVlA4WAoAAAAQAAAAAAAAAAAAQUxQSAwAAAARBxAR/Q9ERP8DAABWUDggGAAAABQBAJ0BKgEAAQAAAP4AAA3AAP7mtQAAAA==")
}
func webpAnimated(t *testing.T) []byte {
	return b64(t, "UklGRlIAAABXRUJQVlA4WAoAAAASAAAAAAAAAAAAQU5JTQYAAAD/////AABBTk1GJgAAAAAAAAAAAAAAAAAAAGQAAABWUDhMDQAAAC8AAAAQBxAREYiI/gcA")
}

func riff(chunks ...[]byte) []byte {
	var body bytes.Buffer
	body.WriteString("WEBP")
	for _, c := range chunks {
		body.Write(c)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func chunk(cc string, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString(cc)
	binary.Write(&b, binary.LittleEndian, uint32(len(data)))
	b.Write(data)
	if len(data)%2 == 1 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

func vp8x(flags byte, w, h int) []byte {
	d := make([]byte, 10)
	d[0] = flags
	d[4], d[5], d[6] = byte(w-1), byte((w-1)>>8), byte((w-1)>>16)
	d[7], d[8], d[9] = byte(h-1), byte((h-1)>>8), byte((h-1)>>16)
	return chunk("VP8X", d)
}

// VP8Lのヘッダーだけ（フレームの寸法を宣言する）。デコードはできない（構造検査の試験用）。
func vp8lHeader(w, h int) []byte {
	d := make([]byte, 5)
	d[0] = 0x2F
	binary.LittleEndian.PutUint32(d[1:], uint32(w-1)|uint32(h-1)<<14)
	return chunk("VP8L", append(d, 0, 0, 0, 0))
}

// EXIF（TIFF）。Orientationだけを持つ。
func tiffOrient(o int, bigEndian bool) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	hd := "II"
	if bigEndian {
		bo, hd = binary.BigEndian, "MM"
	}
	b := make([]byte, 8+2+12+4)
	copy(b, hd)
	bo.PutUint16(b[2:], 42)
	bo.PutUint32(b[4:], 8)
	bo.PutUint16(b[8:], 1)
	bo.PutUint16(b[10:], 0x0112)
	bo.PutUint16(b[12:], 3)
	bo.PutUint32(b[14:], 1)
	bo.PutUint16(b[18:], uint16(o))
	return b
}

// JPEGにAPP1(Exif)を差し込む。GPS等の文字列も入れて、出力に残らないことを確かめる。
func withExif(jpg []byte, tiff []byte, extra string) []byte {
	seg := append([]byte("Exif\x00\x00"), tiff...)
	seg = append(seg, []byte(extra)...)
	var out bytes.Buffer
	out.Write(jpg[:2])
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(&out, binary.BigEndian, uint16(len(seg)+2))
	out.Write(seg)
	out.Write(jpg[2:])
	return out.Bytes()
}

// pngChunk は正しいCRCつきのチャンクを作る（デコーダーはCRCを検査する）。
func pngChunk(typ string, data []byte) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(append([]byte(typ), data...)))
}

func insertBeforeIEND(png []byte, chunk []byte) []byte {
	iend := bytes.LastIndex(png, []byte("IEND")) - 4
	return append(append(append([]byte(nil), png[:iend]...), chunk...), png[iend:]...)
}

func status(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func process(t *testing.T, data []byte, p Purpose) (Result, error) {
	t.Helper()
	info, err := Scan(data)
	if err != nil {
		return Result{}, err
	}
	return Process(data, info, p)
}

// ---- 形式の判定 ----

func TestSniffRejectsOtherFormats(t *testing.T) {
	for name, b := range map[string][]byte{
		"空":      {},
		"SVG":    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"GIF":    []byte("GIF89a\x01\x00\x01\x00"),
		"BMP":    []byte("BM\x00\x00\x00\x00"),
		"HTML":   []byte("<html><script>alert(1)</script></html>"),
		"PDF":    []byte("%PDF-1.7"),
		"ZIP":    []byte("PK\x03\x04"),
		"短いRIFF": []byte("RIFF\x00\x00\x00\x00WAVE"),
	} {
		if _, err := Scan(b); status(err) != 415 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// ---- 構造の検査（デコード前） ----

func TestScanPNG(t *testing.T) {
	g := pngBytes(t, gradient(40, 30))
	if info, err := Scan(g); err != nil || info.W != 40 || info.H != 30 || info.Format != PNG {
		t.Fatalf("正常: %+v %v", info, err)
	}
	// 途中で切れている（IENDが無い）・チャンク長の偽装
	if _, err := Scan(g[:len(g)-20]); status(err) != 422 {
		t.Errorf("切れたPNG: %v", err)
	}
	forged := append([]byte(nil), g...)
	binary.BigEndian.PutUint32(forged[8:12], 0xFFFFFFF0) // IHDRの長さを偽装
	if _, err := Scan(forged); status(err) != 422 {
		t.Errorf("長さの偽装: %v", err)
	}
	// IHDRが先頭でない
	noIHDR := append(append([]byte(nil), g[:8]...), g[8+25:]...)
	if _, err := Scan(noIHDR); status(err) != 422 {
		t.Errorf("IHDRが無い: %v", err)
	}
	// 巨大な寸法を宣言（小さなデータ）
	big := append([]byte(nil), g...)
	binary.BigEndian.PutUint32(big[16:20], 40000)
	binary.BigEndian.PutUint32(big[20:24], 40000)
	if _, err := Scan(big); status(err) != 422 {
		t.Errorf("巨大な寸法: %v", err)
	}
	// APNG: acTL / fcTL / fdAT
	for _, typ := range []string{"acTL", "fcTL", "fdAT"} {
		ins := make([]byte, 0, 12)
		ins = binary.BigEndian.AppendUint32(ins, 0)
		ins = append(ins, typ...)
		ins = append(ins, 0, 0, 0, 0)
		apng := append(append(append([]byte(nil), g[:33]...), ins...), g[33:]...)
		if _, err := Scan(apng); status(err) != 422 || !strings.Contains(err.Error(), "アニメーション") {
			t.Errorf("%s: %v", typ, err)
		}
	}
}

func TestScanJPEG(t *testing.T) {
	j := jpegBytes(t, gradient(64, 48))
	if info, err := Scan(j); err != nil || info.W != 64 || info.H != 48 || info.Format != JPEG {
		t.Fatalf("正常: %+v %v", info, err)
	}
	if _, err := Scan(j[:len(j)/2]); status(err) != 422 {
		t.Errorf("切れたJPEG: %v", err)
	}
	// SOFが2つ（寸法の偽装）
	i := bytes.Index(j, []byte{0xFF, 0xC0})
	sofLen := int(binary.BigEndian.Uint16(j[i+2:])) + 2
	dup := append(append(append([]byte(nil), j[:i+sofLen]...), j[i:i+sofLen]...), j[i+sofLen:]...)
	if _, err := Scan(dup); status(err) != 422 {
		t.Errorf("SOFの重複: %v", err)
	}
	// DNL
	dnl := append(append(append([]byte(nil), j[:i]...), 0xFF, 0xDC, 0x00, 0x04, 0x00, 0x10), j[i:]...)
	if _, err := Scan(dnl); status(err) != 422 {
		t.Errorf("DNL: %v", err)
	}
	// 算術符号（SOF9）・ロスレス（SOF3）は扱わない
	for _, m := range []byte{0xC3, 0xC9} {
		x := append([]byte(nil), j...)
		x[i+1] = m
		if _, err := Scan(x); status(err) != 415 {
			t.Errorf("SOF%x: %v", m, err)
		}
	}
	// SOSが多すぎる
	k := bytes.Index(j, []byte{0xFF, 0xDA})
	sosLen := int(binary.BigEndian.Uint16(j[k+2:])) + 2
	many := append([]byte(nil), j[:k]...)
	for n := 0; n < 40; n++ {
		many = append(many, j[k:k+sosLen]...)
	}
	many = append(many, j[k+sosLen:]...)
	if _, err := Scan(many); status(err) != 422 {
		t.Errorf("SOS過多: %v", err)
	}
	// 巨大な寸法を宣言
	big := append([]byte(nil), j...)
	binary.BigEndian.PutUint16(big[i+5:], 30000)
	binary.BigEndian.PutUint16(big[i+7:], 30000)
	if _, err := Scan(big); status(err) != 422 {
		t.Errorf("巨大な寸法: %v", err)
	}
	// EXIFを拾う
	if info, err := Scan(withExif(j, tiffOrient(6, false), "")); err != nil || orientation(info.Exif) != 6 {
		t.Errorf("EXIF: %v", err)
	}
}

func TestScanWebP(t *testing.T) {
	for name, b := range map[string][]byte{"lossy": webpLossy(t), "lossless": webpLossless(t), "alpha": webpAlpha(t)} {
		if info, err := Scan(b); err != nil || info.W != 1 || info.H != 1 || info.Format != WebP {
			t.Errorf("%s: %+v %v", name, info, err)
		}
	}
	if info, _ := Scan(webpAlpha(t)); !info.HasAlphaHint {
		t.Errorf("アルファの目安")
	}
	// アニメーション: 固定のアニメーションWebP、VP8Xのフラグだけ、ANIM/ANMFが末尾に隠れている
	if _, err := Scan(webpAnimated(t)); status(err) != 422 {
		t.Errorf("アニメーションWebP: %v", err)
	}
	lossy := webpLossy(t)[12:]
	if _, err := Scan(riff(vp8x(0x02, 1, 1), lossy)); status(err) != 422 {
		t.Errorf("VP8Xのアニメーションフラグ: %v", err)
	}
	if _, err := Scan(riff(vp8x(0, 1, 1), lossy, chunk("ANIM", make([]byte, 6)))); status(err) != 422 {
		t.Errorf("末尾に隠したANIM: %v", err)
	}
	if _, err := Scan(riff(vp8x(0, 1, 1), lossy, chunk("ANMF", make([]byte, 16)))); status(err) != 422 {
		t.Errorf("末尾に隠したANMF: %v", err)
	}
	// canvas（VP8X）とフレームの寸法が違う（canvasだけ小さく偽装してフレームで巨大な領域を確保させる）
	if _, err := Scan(riff(vp8x(0, 100, 100), vp8lHeader(16000, 16000))); status(err) != 422 {
		t.Errorf("canvasとフレームの不一致: %v", err)
	}
	if _, err := Scan(riff(vp8x(0, 100, 100), lossy)); status(err) != 422 { // フレームは1x1
		t.Errorf("canvasとフレームの不一致(2): %v", err)
	}
	// フレーム自体が巨大（宣言だけ）
	if _, err := Scan(riff(vp8lHeader(16383, 16383))); status(err) != 422 {
		t.Errorf("巨大なフレーム: %v", err)
	}
	// RIFFのサイズとファイル長の不一致・切れ・フレーム無し・フレーム2つ
	w := webpLossy(t)
	if _, err := Scan(append(append([]byte(nil), w...), 0, 0)); status(err) != 422 {
		t.Errorf("末尾の付加データ: %v", err)
	}
	if _, err := Scan(w[:len(w)-4]); status(err) != 422 {
		t.Errorf("切れたWebP: %v", err)
	}
	if _, err := Scan(riff(chunk("EXIF", []byte("x")))); status(err) != 422 {
		t.Errorf("フレーム無し: %v", err)
	}
	if _, err := Scan(riff(lossy, lossy)); status(err) != 422 {
		t.Errorf("フレーム2つ: %v", err)
	}
}

// ---- EXIFの向き ----

func TestOrientationParser(t *testing.T) {
	for _, be := range []bool{false, true} {
		for o := 1; o <= 8; o++ {
			if got := orientation(tiffOrient(o, be)); got != o {
				t.Errorf("o=%d be=%v: %d", o, be, got)
			}
		}
	}
	good := tiffOrient(6, false)
	for name, b := range map[string][]byte{
		"空":      nil,
		"短い":     good[:6],
		"値が9":    tiffOrient(9, false),
		"値が0":    tiffOrient(0, false),
		"マジック違い": append([]byte("XX"), good[2:]...),
		"IFDの位置が範囲外": func() []byte {
			b := append([]byte(nil), good...)
			binary.LittleEndian.PutUint32(b[4:], 0xFFFFFFF0)
			return b
		}(),
		"エントリ数が巨大": func() []byte {
			b := append([]byte(nil), good...)
			binary.LittleEndian.PutUint16(b[8:], 65535)
			return b
		}(),
		"型がSHORTでない":  func() []byte { b := append([]byte(nil), good...); binary.LittleEndian.PutUint16(b[12:], 4); return b }(),
		"countが1でない":  func() []byte { b := append([]byte(nil), good...); binary.LittleEndian.PutUint32(b[14:], 2); return b }(),
		"エントリが途中で切れる": good[:15],
	} {
		if got := orientation(b); got != 1 {
			t.Errorf("%s: %d（補正なしのはず）", name, got)
		}
	}
}

// ---- 取り込み（縮小・再エンコード） ----

func TestProcessStripsMetadataAndTrailingData(t *testing.T) {
	jpg := withExif(jpegBytes(t, gradient(80, 60)), tiffOrient(1, false), "GPSLatitude=35.0 撮影者=秘密の人")
	jpg = append(jpg, []byte("<script>alert(1)</script>PK\x03\x04ZIPTRAILER")...)
	res, err := process(t, jpg, CMS)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Exif", "GPSLatitude", "秘密の人", "<script>", "ZIPTRAILER", "PK\x03\x04"} {
		if bytes.Contains(res.Data, []byte(leak)) {
			t.Errorf("出力に %q が残っている", leak)
		}
	}
	if res.Mime != "image/webp" || res.Ext != "webp" || res.W != 80 || res.H != 60 {
		t.Errorf("%+v", res.Mime)
	}
	if _, err := xwebp.Decode(bytes.NewReader(res.Data)); err != nil {
		t.Errorf("出力が読めない: %v", err)
	}
	assertWebPChunks(t, res.Data, false)
	// PNG: tEXt/eXIf・末尾
	p := pngBytes(t, gradient(40, 30))
	p2 := insertBeforeIEND(p, pngChunk("tEXt", []byte("Comment\x00秘密のコメント")))
	p2 = insertBeforeIEND(p2, pngChunk("eXIf", append(tiffOrient(1, false), []byte("GPSLatitude=35.0")...)))
	p2 = append(p2, []byte("TRAILER-DATA")...)
	res, err = process(t, p2, CMS)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"tEXt", "eXIf", "秘密のコメント", "GPSLatitude", "TRAILER-DATA"} {
		if bytes.Contains(res.Data, []byte(leak)) {
			t.Errorf("PNGの出力に %q が残っている", leak)
		}
	}
	if res.Mime != "image/webp" {
		t.Errorf("PNGもWebPで出る: %s", res.Mime)
	}
	assertWebPChunks(t, res.Data, false)
}

func TestProcessOrientation(t *testing.T) {
	// 幅4・高さ2。左上が赤、他は青。向きごとに赤の位置を確認する
	m := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			m.SetNRGBA(x, y, color.NRGBA{0, 0, 255, 255})
		}
	}
	m.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	base := pngBytes(t, m)
	withO := func(o int) []byte { return insertBeforeIEND(base, pngChunk("eXIf", tiffOrient(o, false))) }
	want := map[int][3]int{ // 向き: 出力の幅, 高さ, 赤の位置(x*10+y は下で別に確認)
		1: {4, 2, 0}, 2: {4, 2, 0}, 3: {4, 2, 0}, 4: {4, 2, 0}, 5: {2, 4, 0}, 6: {2, 4, 0}, 7: {2, 4, 0}, 8: {2, 4, 0},
	}
	red := map[int][2]int{1: {0, 0}, 2: {3, 0}, 3: {3, 1}, 4: {0, 1}, 5: {0, 0}, 6: {1, 0}, 7: {1, 3}, 8: {0, 3}}
	for o := 1; o <= 8; o++ {
		res, err := process(t, withO(o), CMS)
		if err != nil {
			t.Fatalf("o=%d: %v", o, err)
		}
		if res.W != want[o][0] || res.H != want[o][1] {
			t.Errorf("o=%d: 寸法 %dx%d", o, res.W, res.H)
		}
		img, err := xwebp.Decode(bytes.NewReader(res.Data))
		if err != nil {
			t.Fatal(err)
		}
		r := red[o]
		got := color.NRGBAModel.Convert(img.At(r[0], r[1])).(color.NRGBA)
		if got.R < 200 || got.B > 60 {
			t.Errorf("o=%d: 赤の位置が違う（(%d,%d)=%v）", o, r[0], r[1], got)
		}
	}
	// 壊れたEXIFでも落ちず、補正なし
	broken := insertBeforeIEND(base, pngChunk("eXIf", tiffOrient(6, false)[:12]))
	if res, err := process(t, broken, CMS); err != nil || res.W != 4 || res.H != 2 {
		t.Errorf("壊れたEXIF: %+v %v", res, err)
	}
}

func TestProcessResizeOnlyWhenLarger(t *testing.T) {
	// 長辺が上限を超える画像だけ縮み、小さい画像は拡大されない。avatarは512、他は2560
	res, err := process(t, jpegBytes(t, gradient(3000, 1500)), CMS)
	if err != nil || res.W != 2560 || res.H != 1280 {
		t.Errorf("横長: %+v %v", res, err)
	}
	res, err = process(t, pngBytes(t, gradient(1200, 1800)), Avatar)
	if err != nil || res.W != 341 || res.H != 512 {
		t.Errorf("縦長のavatar: %+v %v", res, err)
	}
	res, err = process(t, jpegBytes(t, gradient(300, 200)), Avatar)
	if err != nil || res.W != 300 || res.H != 200 {
		t.Errorf("小さい画像は拡大しない: %+v %v", res, err)
	}
	res, err = process(t, jpegBytes(t, gradient(2560, 100)), CMS)
	if err != nil || res.W != 2560 || res.H != 100 {
		t.Errorf("上限ちょうどは縮めない: %+v %v", res, err)
	}
	res, err = process(t, jpegBytes(t, gradient(5000, 1)), CMS) // 極端な縦横比でも高さが0にならない
	if err != nil || res.W != 2560 || res.H != 1 {
		t.Errorf("極端な縦横比: %+v %v", res, err)
	}
}

// 出力のチャンクが画像の符号化に必要なものだけ（EXIF・ICC・XMPを含まない）で、アルファの有無が期待どおりであること。
func assertWebPChunks(t *testing.T, b []byte, wantAlpha bool) {
	t.Helper()
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		t.Fatalf("WebPではない")
	}
	alpha := false
	for pos := 12; pos+8 <= len(b); {
		cc := string(b[pos : pos+4])
		size := int(uint32(b[pos+4]) | uint32(b[pos+5])<<8 | uint32(b[pos+6])<<16 | uint32(b[pos+7])<<24)
		switch cc {
		case "VP8X", "VP8 ", "VP8L":
		case "ALPH":
			alpha = true
		default:
			t.Errorf("出力に余分なチャンク %q", cc)
		}
		pos += 8 + size + size%2
	}
	info, err := scanWebP(b)
	if err != nil {
		t.Fatal(err)
	}
	if wantAlpha != (alpha || info.HasAlphaHint) {
		t.Errorf("アルファの有無が期待と違う（want=%v alphaChunk=%v hint=%v）", wantAlpha, alpha, info.HasAlphaHint)
	}
}

func webpChunkKinds(b []byte) string {
	kinds := ""
	for pos := 12; pos+8 <= len(b); {
		size := int(uint32(b[pos+4]) | uint32(b[pos+5])<<8 | uint32(b[pos+6])<<16 | uint32(b[pos+7])<<24)
		kinds += string(b[pos:pos+4]) + ","
		pos += 8 + size + size%2
	}
	return kinds
}

func TestProcessWebPAndColorTypes(t *testing.T) {
	// どの入力形式でも、出力はWebP
	for name, b := range map[string][]byte{"WebP(ロッシー)": webpLossy(t), "WebP(ロスレス)": webpLossless(t), "JPEG": jpegBytes(t, gradient(30, 20)), "PNG": pngBytes(t, gradient(30, 20))} {
		res, err := process(t, b, CMS)
		if err != nil || res.Mime != "image/webp" || res.Ext != "webp" {
			t.Errorf("%s: %+v %v", name, res.Mime, err)
			continue
		}
		if name != "WebP(ロスレス)" { // この固定データはアルファのビットを持つ
			assertWebPChunks(t, res.Data, false)
		}
	}
	// アルファ付きのWebPは、アルファを保つ
	res, err := process(t, webpAlpha(t), CMS)
	if err != nil || res.Mime != "image/webp" {
		t.Fatalf("WebP(アルファ): %v", err)
	}
	assertWebPChunks(t, res.Data, true)
	// 16bit PNG・グレースケール・パレット+tRNSが落ちない（画素が妥当）
	rgba64 := image.NewNRGBA64(image.Rect(0, 0, 20, 10))
	for i := range rgba64.Pix {
		rgba64.Pix[i] = byte(i * 7)
	}
	gray := image.NewGray(image.Rect(0, 0, 20, 10))
	for i := range gray.Pix {
		gray.Pix[i] = byte(i * 3)
	}
	pal := image.NewPaletted(image.Rect(0, 0, 20, 10), color.Palette{color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 128}})
	for i := range pal.Pix {
		pal.Pix[i] = uint8(i % 2)
	}
	for name, m := range map[string]image.Image{"16bit": rgba64, "gray": gray, "palette": pal} {
		res, err := process(t, pngBytes(t, m), CMS)
		if err != nil || res.W != 20 || res.H != 10 || res.Mime != "image/webp" {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if _, err := xwebp.Decode(bytes.NewReader(res.Data)); err != nil {
			t.Errorf("%s: 出力が読めない: %v", name, err)
		}
	}
	// 透明画素の下の色は残らない（隠しデータの除去）。透明は透明のまま
	hidden := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < len(hidden.Pix); i += 4 {
		hidden.Pix[i], hidden.Pix[i+1], hidden.Pix[i+2], hidden.Pix[i+3] = 11, 22, 33, 0
	}
	res, err = process(t, pngBytes(t, hidden), CMS)
	if err != nil {
		t.Fatal(err)
	}
	out, err := xwebp.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.NRGBAModel.Convert(out.At(0, 0)).(color.NRGBA); c.A != 0 || c.R != 0 || c.G != 0 || c.B != 0 {
		t.Errorf("透明の下の色が残っている: %v", c)
	}
}

// 圧縮方式の選択: 文字・ロゴ風（低色数）は画素が完全に一致するロスレス、写真風はロッシー。透過は維持され、不透明な画像にアルファは付かない。
func TestProcessChoosesLosslessForGraphicsAndLossyForPhotos(t *testing.T) {
	logo := image.NewNRGBA(image.Rect(0, 0, 200, 100)) // 3色＋透明
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			switch {
			case (x/10+y/10)%3 == 0:
				logo.SetNRGBA(x, y, color.NRGBA{20, 20, 20, 255})
			case x%40 < 8:
				logo.SetNRGBA(x, y, color.NRGBA{230, 40, 40, 255})
			}
		}
	}
	res, err := process(t, pngBytes(t, logo), CMS)
	if err != nil {
		t.Fatal(err)
	}
	if k := webpChunkKinds(res.Data); !strings.Contains(k, "VP8L") || strings.Contains(k, "VP8 ") {
		t.Errorf("低色数がロスレスにならない: %s", k)
	}
	assertWebPChunks(t, res.Data, true) // 透明の画素がある
	out, err := xwebp.Decode(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if got, want := color.NRGBAModel.Convert(out.At(x, y)).(color.NRGBA), logo.NRGBAAt(x, y); got != want {
				t.Fatalf("ロスレスなのに画素が違う (%d,%d): %v != %v", x, y, got, want)
			}
		}
	}
	// 写真風（連続階調＋ノイズ。色数が多い）はロッシー
	photo := gradient(300, 200)
	for i := 0; i < len(photo.Pix); i += 4 {
		photo.Pix[i] ^= byte(i / 4 % 13)
	}
	res, err = process(t, pngBytes(t, photo), CMS)
	if err != nil {
		t.Fatal(err)
	}
	if k := webpChunkKinds(res.Data); !strings.Contains(k, "VP8 ") || strings.Contains(k, "VP8L") {
		t.Errorf("写真がロッシーにならない: %s", k)
	}
	assertWebPChunks(t, res.Data, false)
	// 半透明の写真風画像: アルファが保たれる
	soft := gradient(300, 200)
	for i := 3; i < len(soft.Pix); i += 4 {
		soft.Pix[i] = byte(100 + i/4%100)
	}
	res, err = process(t, pngBytes(t, soft), CMS)
	if err != nil {
		t.Fatal(err)
	}
	assertWebPChunks(t, res.Data, true)
}

// 透過画像は長辺1600までに縮む（メモリの上限。不透明は2560まで）。
func TestProcessAlphaImagesAreDownscaledMore(t *testing.T) {
	a := gradient(3000, 1500)
	for i := 3; i < len(a.Pix); i += 4 {
		a.Pix[i] = 128
	}
	res, err := process(t, pngBytes(t, a), CMS)
	if err != nil || res.W != 1600 || res.H != 800 {
		t.Errorf("透過: %dx%d %v", res.W, res.H, err)
	}
	res, err = process(t, pngBytes(t, gradient(3000, 1500)), CMS)
	if err != nil || res.W != 2560 {
		t.Errorf("不透明: %dx%d %v", res.W, res.H, err)
	}
}

func TestStorageKeys(t *testing.T) {
	id := "0b8f3a52-1111-4222-8333-444455556666"
	if k, err := NewKey(id, "webp"); err != nil || !ValidKey(k) {
		t.Errorf("webp: %v", err)
	}
	for _, ext := range []string{"jpg", "png", "gif", "", "webp/../x"} {
		if _, err := NewKey(id, ext); err == nil {
			t.Errorf("新規の保存で %q を許してしまう", ext)
		}
	}
	for _, ext := range []string{"jpg", "png", "webp"} { // 移行前に保存した既存ファイルは、読み出し・削除・検証で扱える
		if !ValidKey(id[:2] + "/" + id + "." + ext) {
			t.Errorf("既存の .%s を扱えない", ext)
		}
	}
}

func TestProcessBrokenInputsDoNotPanic(t *testing.T) {
	jpg := jpegBytes(t, gradient(64, 48))
	pn := pngBytes(t, gradient(64, 48))
	for name, b := range map[string][]byte{
		"JPEGの本体を壊す": func() []byte {
			x := append([]byte(nil), jpg...)
			copy(x[len(x)/2:], bytes.Repeat([]byte{0xFF}, 64))
			return x
		}(),
		"PNGのIDATを壊す": func() []byte { x := append([]byte(nil), pn...); copy(x[50:], bytes.Repeat([]byte{0x00}, 40)); return x }(),
		"切れたJPEG":     jpg[:len(jpg)-10],
	} {
		if _, err := process(t, b, CMS); err == nil {
			// 壊れ方によってはデコードできてしまう場合もある（画像として妥当なら許す）
			continue
		} else if s := status(err); s != 422 {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// ---- 保存 ----

func TestStore(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id := "0a1b2c3d-1111-2222-3333-444455556666"
	key, err := NewKey(id, "webp")
	if err != nil || key != "0a/"+id+".webp" {
		t.Fatalf("%q %v", key, err)
	}
	if err := s.Put(key, []byte("data")); err != nil {
		t.Fatal(err)
	}
	f, err := s.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 10)
	n, _ := f.Read(b)
	f.Close()
	if string(b[:n]) != "data" {
		t.Errorf("読み出し: %q", b[:n])
	}
	if st, err := os.Stat(dir + "/" + key); err != nil || st.Mode().Perm() != 0o640 {
		t.Errorf("権限: %v %v", st, err)
	}
	// tmpに残りが無い
	if es, _ := os.ReadDir(dir + "/tmp"); len(es) != 0 {
		t.Errorf("tmpに残っている: %d", len(es))
	}
	// 不正なキーは開けない・書けない・消せない（パストラバーサル）
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "0a/../../x.png", "0a/" + id + ".svg", "xx/" + id + ".png", id + ".png", "0a/" + strings.ToUpper(id) + ".png", ""} {
		if _, err := s.Open(bad); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open(%q): %v", bad, err)
		}
		if err := s.Put(bad, []byte("x")); err == nil {
			t.Errorf("Put(%q)が通った", bad)
		}
		if err := s.Remove(bad); err == nil {
			t.Errorf("Remove(%q)が通った", bad)
		}
	}
	// シンボリックリンクでRootの外へ出られない
	outside := t.TempDir()
	os.WriteFile(outside+"/secret.png", []byte("secret"), 0o600)
	os.MkdirAll(dir+"/ff", 0o750)
	os.Symlink(outside+"/secret.png", dir+"/ff/ffffffff-ffff-ffff-ffff-ffffffffffff.png")
	if f, err := s.Open("ff/ffffffff-ffff-ffff-ffff-ffffffffffff.png"); err == nil {
		f.Close()
		t.Error("シンボリックリンクでRootの外を開けた")
	}
	// 一覧・空き容量・tmpの掃除・削除
	keys, strays, err := s.Keys()
	if err != nil || len(keys) != 1 || keys[0] != key || len(strays) != 1 { // シンボリックリンクは通常のファイルではない=想定外として分ける
		t.Errorf("Keys: %v %v %v", keys, strays, err)
	}
	if free, err := s.FreeBytes(); err != nil || free == 0 {
		t.Errorf("空き容量: %d %v", free, err)
	}
	os.WriteFile(dir+"/tmp/old", []byte("x"), 0o640)
	os.Chtimes(dir+"/tmp/old", time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour))
	os.WriteFile(dir+"/tmp/new", []byte("x"), 0o640)
	if n, err := s.CleanTmp(time.Hour); err != nil || n != 1 {
		t.Errorf("CleanTmp: %d %v", n, err)
	}
	if _, err := os.Stat(dir + "/tmp/new"); err != nil {
		t.Errorf("新しいtmpまで消した")
	}
	if err := s.Remove(key); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(key); err != nil { // 二重の削除は無視
		t.Errorf("二重の削除: %v", err)
	}
}

// 巨大な寸法を宣言した小さなデータは、デコード前に拒否され、メモリをほとんど使わない。
func TestBombsAreRejectedBeforeDecodingWithoutAllocating(t *testing.T) {
	g := pngBytes(t, gradient(40, 30))
	bigPNG := append([]byte(nil), g...)
	binary.BigEndian.PutUint32(bigPNG[16:20], 9999)
	binary.BigEndian.PutUint32(bigPNG[20:24], 9999) // 約1億画素
	j := jpegBytes(t, gradient(64, 48))
	bigJPEG := append([]byte(nil), j...)
	i := bytes.Index(bigJPEG, []byte{0xFF, 0xC0})
	binary.BigEndian.PutUint16(bigJPEG[i+5:], 9999)
	binary.BigEndian.PutUint16(bigJPEG[i+7:], 9999)
	bigWebP := riff(vp8x(0, 9999, 9999), vp8lHeader(9999, 9999))
	cases := map[string][]byte{"PNG": bigPNG, "JPEG": bigJPEG, "WebP": bigWebP}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for name, b := range cases {
		if _, err := process(t, b, CMS); status(err) != 422 {
			t.Errorf("%s: %v", name, err)
		}
	}
	runtime.ReadMemStats(&after)
	if delta := after.TotalAlloc - before.TotalAlloc; delta > 2<<20 {
		t.Errorf("拒否するだけなのに %d バイト確保した（デコードしている疑い）", delta)
	}
}

// 宣言された寸法と、デコーダーの読む寸法が食い違えば拒否する。
func TestProcessRejectsDimensionMismatch(t *testing.T) {
	p := pngBytes(t, gradient(40, 30))
	info, err := Scan(p)
	if err != nil {
		t.Fatal(err)
	}
	info.W = 41
	if _, err := Process(p, info, CMS); status(err) != 422 {
		t.Errorf("%v", err)
	}
}

// 最悪ケースのメモリの実測（手動）。MEDIA_MEM_TEST=1 を付けて、-raceなしで実行する。
//
//	MEDIA_MEM_TEST=1 go test -run TestWorstCaseMemory -v ./internal/media/
//
// 結果はdocs/OPERATIONS.mdに記録する（同時デコード数の根拠）。
func TestWorstCaseMemory(t *testing.T) {
	if os.Getenv("MEDIA_MEM_TEST") == "" {
		t.Skip("手動の実測（MEDIA_MEM_TEST=1で実行）")
	}
	measure := func(name string, data []byte) {
		info, err := Scan(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		runtime.GC()
		var base runtime.MemStats
		runtime.ReadMemStats(&base)
		stop, peak := make(chan struct{}), uint64(0)
		done := make(chan struct{})
		go func() {
			defer close(done)
			var m runtime.MemStats
			for {
				select {
				case <-stop:
					return
				default:
				}
				runtime.ReadMemStats(&m)
				if m.HeapInuse > peak {
					peak = m.HeapInuse
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
		start := time.Now()
		res, err := Process(data, info, CMS)
		close(stop)
		<-done
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%-28s 入力%6.1fMiB %dx%d → 出力%5.1fMiB %dx%d  ピークのヒープ %6.1fMiB（開始時 %.1fMiB） %v",
			name, float64(len(data))/(1<<20), info.W, info.H, float64(len(res.Data))/(1<<20), res.W, res.H, float64(peak)/(1<<20), float64(base.HeapInuse)/(1<<20), time.Since(start).Round(time.Millisecond))
	}
	w, h := 5000, 4000 // 2,000万画素
	rgba64 := image.NewNRGBA64(image.Rect(0, 0, w, h))
	for i := range rgba64.Pix {
		rgba64.Pix[i] = byte(i * 31)
	}
	measure("16bit RGBA PNG 20Mpx", pngBytes(t, rgba64))
	rgba64 = nil
	runtime.GC()
	measure("JPEG 20Mpx（baseline）", jpegBytes(t, gradient(w, h)))
	measure("PNG 20Mpx（8bit RGBA）", pngBytes(t, gradient(w, h)))
	measure("JPEG 10Mpx（baseline）", jpegBytes(t, gradient(4000, 2500)))
}

// 最悪ケースを同時デコード数（2）で同時に処理したときの、プロセス全体のメモリ（手動）。GOMEMLIMITを付けて実行する。
//
//	GOMEMLIMIT=768MiB MEDIA_MEM_TEST=1 go test -run TestWorstCaseConcurrentMemory -v ./internal/media/
func TestWorstCaseConcurrentMemory(t *testing.T) {
	if os.Getenv("MEDIA_MEM_TEST") == "" {
		t.Skip("手動の実測（MEDIA_MEM_TEST=1で実行）")
	}
	rgba64 := image.NewNRGBA64(image.Rect(0, 0, 5000, 4000))
	for i := range rgba64.Pix {
		rgba64.Pix[i] = byte(i * 31)
	}
	data := pngBytes(t, rgba64)
	rgba64 = nil
	info, err := Scan(data)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	stop, peakInuse, peakSys := make(chan struct{}), uint64(0), uint64(0)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			peakInuse = max(peakInuse, m.HeapInuse)
			peakSys = max(peakSys, m.Sys)
			time.Sleep(2 * time.Millisecond)
		}
	}()
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Process(data, info, CMS); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-done
	t.Logf("16bit RGBA PNG 20Mpx ×2同時 GOMEMLIMIT=%q: ピークのヒープ %.1fMiB・OSから取得 %.1fMiB・%v", os.Getenv("GOMEMLIMIT"), float64(peakInuse)/(1<<20), float64(peakSys)/(1<<20), time.Since(start).Round(time.Millisecond))
}
