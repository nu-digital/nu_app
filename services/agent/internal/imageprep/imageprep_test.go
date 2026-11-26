package imageprep

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngOf(w, h int, c color.Color) []byte {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, m)
	return b.Bytes()
}

func TestResizesLargeImagesAndKeepsSmallOnes(t *testing.T) {
	out, w, h, err := Prepare(pngOf(2000, 1000, color.NRGBA{200, 50, 50, 255}))
	if err != nil || w != 1568 || h != 784 {
		t.Fatalf("横長: %d×%d %v", w, h, err)
	}
	img, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil || img.Bounds().Dx() != 1568 {
		t.Fatalf("出力がJPEGで読めない: %v", err)
	}
	if _, w, h, err := Prepare(pngOf(800, 600, color.NRGBA{1, 2, 3, 255})); err != nil || w != 800 || h != 600 {
		t.Errorf("小さい画像は拡大・縮小しない: %d×%d %v", w, h, err)
	}
	if _, w, h, _ := Prepare(pngOf(1000, 5000, color.NRGBA{1, 2, 3, 255})); h != 1568 || w != 313 {
		t.Errorf("縦長: %d×%d", w, h)
	}
}

func TestTransparencyBecomesWhiteNotBlack(t *testing.T) {
	out, _, _, err := Prepare(pngOf(20, 20, color.NRGBA{0, 0, 0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	img, _ := jpeg.Decode(bytes.NewReader(out))
	r, g, b, _ := img.At(5, 5).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("透過が白にならない: %d %d %d", r>>8, g>>8, b>>8)
	}
}

func TestRejectsBadInputs(t *testing.T) {
	if _, _, _, err := Prepare(nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("空: %v", err)
	}
	if _, _, _, err := Prepare(make([]byte, MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("大きすぎるバイト数: %v", err)
	}
	if _, _, _, err := Prepare([]byte("<svg onload=alert(1)>")); !errors.Is(err, ErrUnsupported) {
		t.Errorf("SVG等: %v", err)
	}
	var g bytes.Buffer
	_ = gif.Encode(&g, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil)
	if _, _, _, err := Prepare(g.Bytes()); !errors.Is(err, ErrUnsupported) {
		t.Errorf("GIF: %v", err)
	}
	p := pngOf(10, 10, color.Black)
	if _, _, _, err := Prepare(p[:len(p)/2]); !errors.Is(err, ErrUnsupported) {
		t.Errorf("切れたPNG: %v", err)
	}
}

// 展開の爆弾: 宣言された寸法が巨大なPNGは、デコードする前に弾く（メモリを使わない）。
func TestDecompressionBombIsRejectedBeforeDecoding(t *testing.T) {
	chunk := func(typ string, data []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
		b.WriteString(typ)
		b.Write(data)
		_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(typ), data...)))
		return b.Bytes()
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 40000) // 40000×40000 = 16億画素
	binary.BigEndian.PutUint32(ihdr[4:], 40000)
	ihdr[8], ihdr[9] = 8, 6 // 8bit・RGBA
	p := append([]byte("\x89PNG\r\n\x1a\n"), chunk("IHDR", ihdr)...)
	p = append(p, chunk("IEND", nil)...)
	if _, _, _, err := Prepare(p); !errors.Is(err, ErrTooLarge) {
		t.Errorf("爆弾を弾けない: %v", err)
	}
}
