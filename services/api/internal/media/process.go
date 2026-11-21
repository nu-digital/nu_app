package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	_ "image/jpeg" // デコード（出力はWebPのみ）
	_ "image/png"
	"io"
	"net/http"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
	xwebp "golang.org/x/image/webp" // 入力のWebPのデコード（エンコーダー側のgen2brain/webpにもデコーダーがあるため、入力の解釈は明示的にこちらを使う）
)

// Purpose は用途。縮小の上限が違う。
type Purpose string

const (
	Avatar    Purpose = "avatar"
	Inventory Purpose = "inventory"
	CMS       Purpose = "cms"
)

// MaxBytes は用途ごとのアップロードの上限。
func MaxBytes(p Purpose) int64 {
	if p == Avatar {
		return 5 << 20
	}
	return 10 << 20
}

func longEdge(p Purpose) int {
	if p == Avatar {
		return 512
	}
	return 2560
}

// 圧縮方式の選択（docs/design/p3-media.md §10）。
const (
	losslessMaxPixels = 2_500_000 // これを超える低色数画像はロッシー高品質（ロスレスは遅く重い。実測: 2.5Mpx・1.3秒）
	lowColorMax       = 1500      // これ以下の色数（透明は1色）の画像は、文字・ロゴ・図としてロスレス
	qualityPhoto      = 82        // 写真（実測: 入力比約53%・q90とのPSNR差は小さい）
	qualityLarge      = 90        // 画素数の大きい低色数画像（文字のにじみを抑える）
	alphaLongEdge     = 1600      // 透過画像は縮小の上限を下げる（実測: 6.5Mpxの透過ロッシーは約930MiB・12秒。2.5Mpxは約370MiB・2.8秒）
)

// Result は再エンコード後の画像。メタデータ（EXIF・GPS・ICC・コメント・末尾の付加データ）を含まない。
type Result struct {
	Data   []byte
	Mime   string
	Ext    string
	W, H   int
	Source Format
}

// Process は検査済みのバイト列をデコードし、縮小→向き補正→再エンコードする。
// 順序は「デコード→縮小→向き補正」（向き補正は縮小後の小さい画像に対して行い、全画素のコピーを減らす）。
// 呼び出し側が同時実行数を制限する（メモリを多く使う）。
func Process(data []byte, info Info, p Purpose) (res Result, err error) {
	defer func() { // 不正なデータでのpanicを500にしない（OOMはrecoverできない。メモリ上限で防ぐ）
		if r := recover(); r != nil {
			res, err = Result{}, invalid("画像を処理できませんでした")
		}
	}()
	// 宣言された寸法と、デコーダーが読む寸法が一致すること
	decodeConfig, decode := image.DecodeConfig, image.Decode
	if info.Format == WebP {
		decodeConfig = func(r io.Reader) (image.Config, string, error) { c, e := xwebp.DecodeConfig(r); return c, "webp", e }
		decode = func(r io.Reader) (image.Image, string, error) { m, e := xwebp.Decode(r); return m, "webp", e }
	}
	cfg, _, derr := decodeConfig(bytes.NewReader(data))
	if derr != nil || cfg.Width != info.W || cfg.Height != info.H {
		return Result{}, invalid("画像の寸法が正しくありません")
	}
	src, _, derr := decode(bytes.NewReader(data))
	if derr != nil {
		return Result{}, invalid("画像を読み込めませんでした（壊れているか、対応していない形式です）")
	}
	b := src.Bounds()
	if b.Dx() != info.W || b.Dy() != info.H {
		return Result{}, invalid("画像の寸法が正しくありません")
	}
	// 縮小（長辺が上限を超えるときだけ。拡大しない）
	img := src
	limit := longEdge(p)
	if !isOpaque(src) {
		limit = min(limit, alphaLongEdge)
	}
	if limit < max(b.Dx(), b.Dy()) {
		w, h := b.Dx(), b.Dy()
		if w >= h {
			h = max(1, h*limit/w)
			w = limit
		} else {
			w = max(1, w*limit/h)
			h = limit
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), toFast(src), src.Bounds(), draw.Src, nil)
		img = dst
	} else if !fast(src) { // 16bit・CMYK等は、8bitの扱いやすい型へ（出力サイズも抑える）
		img = toNRGBA(src)
	}
	if o := orientation(info.Exif); o != 1 {
		img = orient(img, o)
	}
	src = nil // 元の画素を、エンコードの前に解放できるようにする
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		nrgba = toNRGBA(img)
	}
	img = nil
	var buf bytes.Buffer
	if err := webp.Encode(&buf, nrgba, chooseOptions(nrgba)); err != nil {
		return Result{}, invalid("画像を変換できませんでした")
	}
	img = nrgba
	res = Result{Mime: "image/webp", Ext: "webp"}
	if err := checkOutput(buf.Bytes(), nrgba); err != nil {
		return Result{}, err
	}
	nb := img.Bounds()
	res.Data, res.W, res.H, res.Source = buf.Bytes(), nb.Dx(), nb.Dy(), info.Format
	if len(res.Data) == 0 {
		return Result{}, &Error{http.StatusInternalServerError, "画像を保存できませんでした"}
	}
	return res, nil
}

// fast は縮小・符号化で高速な経路がある（かつ8bitの）型か。
func fast(m image.Image) bool {
	switch m.(type) {
	case *image.RGBA, *image.NRGBA, *image.YCbCr, *image.Gray:
		return true
	case *image.Paletted:
		return true
	}
	return false
}

// toFast は縮小の前に、遅い汎用経路になる型（16bit・CMYK・パレット等）を高速な型へ変換する。
func toFast(m image.Image) image.Image {
	if _, ok := m.(*image.Paletted); ok {
		return toNRGBA(m)
	}
	if fast(m) {
		return m
	}
	return toNRGBA(m)
}

func toNRGBA(m image.Image) *image.NRGBA {
	b := m.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), m, b.Min, draw.Src)
	return dst
}

func isOpaque(m image.Image) bool {
	if o, ok := m.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}

// orient はEXIFのOrientation（2〜8）に従って画素を並べ直す（縮小後の画像に対して使う）。
func orient(m image.Image, o int) image.Image {
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := w, h
	if o >= 5 {
		nw, nh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // 左右反転
				dx, dy = w-1-x, y
			case 3: // 180度
				dx, dy = w-1-x, h-1-y
			case 4: // 上下反転
				dx, dy = x, h-1-y
			case 5: // 転置
				dx, dy = y, x
			case 6: // 時計回りに90度
				dx, dy = h-1-y, x
			case 7: // 反転した転置
				dx, dy = h-1-y, w-1-x
			case 8: // 反時計回りに90度
				dx, dy = y, w-1-x
			default:
				dx, dy = x, y
			}
			dst.Set(dx, dy, color.NRGBAModel.Convert(m.At(b.Min.X+x, b.Min.Y+y)))
		}
	}
	return dst
}

// chooseOptions は圧縮方式を選ぶ（全画素を数えるので結果は決定的）。透明な画素は1色として数える。
// 低色数（文字・ロゴ・図）→ロスレス（画素数の上限あり）、それ以外→ロッシー。Exact=false（透明な画素の下の色は捨てる）。
func chooseOptions(m *image.NRGBA) webp.Options {
	px := m.Bounds().Dx() * m.Bounds().Dy()
	if !lowColor(m, lowColorMax) {
		return webp.Options{Quality: qualityPhoto, Method: 4}
	}
	if px <= losslessMaxPixels {
		return webp.Options{Lossless: true, Quality: 75, Method: 4}
	}
	return webp.Options{Quality: qualityLarge, Method: 4}
}

// lowColor は異なる色が max 以下か（超えた時点で打ち切る）。
func lowColor(m *image.NRGBA, max int) bool {
	seen := make(map[uint32]struct{}, 64)
	for i := 0; i+3 < len(m.Pix); i += 4 {
		var k uint32
		if m.Pix[i+3] != 0 {
			k = uint32(m.Pix[i])<<24 | uint32(m.Pix[i+1])<<16 | uint32(m.Pix[i+2])<<8 | uint32(m.Pix[i+3])
		} else {
			k = 0 // 完全に透明な画素は1色
		}
		if _, ok := seen[k]; !ok {
			if len(seen) >= max {
				return false
			}
			seen[k] = struct{}{}
		}
	}
	return true
}

// checkOutput は出力のWebPを検査する: 構造が正しい・寸法が一致・チャンクが画像の符号化に必要なものだけ（EXIF・ICC・XMP等を含まない）・不透明な画像にアルファが無い。
func checkOutput(b []byte, m *image.NRGBA) error {
	info, err := scanWebP(b)
	if err != nil || info.W != m.Bounds().Dx() || info.H != m.Bounds().Dy() {
		return &Error{http.StatusInternalServerError, "画像を変換できませんでした"}
	}
	for pos := uint64(12); pos+8 <= uint64(len(b)); {
		cc := string(b[pos : pos+4])
		size := uint64(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		switch cc {
		case "VP8X", "VP8 ", "VP8L", "ALPH":
		default:
			return &Error{http.StatusInternalServerError, "画像を変換できませんでした"}
		}
		pos += 8 + size + size%2
	}
	if m.Opaque() && info.HasAlphaHint {
		return &Error{http.StatusInternalServerError, "画像を変換できませんでした"}
	}
	return nil
}
