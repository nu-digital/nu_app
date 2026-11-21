package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // WebPのデコード
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
	cfg, _, derr := image.DecodeConfig(bytes.NewReader(data))
	if derr != nil || cfg.Width != info.W || cfg.Height != info.H {
		return Result{}, invalid("画像の寸法が正しくありません")
	}
	src, _, derr := image.Decode(bytes.NewReader(data))
	if derr != nil {
		return Result{}, invalid("画像を読み込めませんでした（壊れているか、対応していない形式です）")
	}
	b := src.Bounds()
	if b.Dx() != info.W || b.Dy() != info.H {
		return Result{}, invalid("画像の寸法が正しくありません")
	}
	// 縮小（長辺が上限を超えるときだけ。拡大しない）
	img := src
	if limit := longEdge(p); b.Dx() > limit || b.Dy() > limit {
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
	opaque := isOpaque(img)
	var buf bytes.Buffer
	switch {
	case info.Format == PNG || (info.Format == WebP && !opaque):
		enc := png.Encoder{CompressionLevel: png.DefaultCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return Result{}, invalid("画像を変換できませんでした")
		}
		res = Result{Mime: "image/png", Ext: "png"}
	default:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return Result{}, invalid("画像を変換できませんでした")
		}
		res = Result{Mime: "image/jpeg", Ext: "jpg"}
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
