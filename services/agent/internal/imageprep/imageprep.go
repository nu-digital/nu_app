// Package imageprep は、Discordの添付画像をClaudeへ送る前の準備（検査と縮小）。コストを抑え、危険な入力を弾く。
package imageprep

import (
	"bytes"
	"errors"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxBytes    = 10 << 20   // 取り込むバイト数の上限
	MaxPixels   = 25_000_000 // 画素数の上限（展開の爆弾対策。デコードの前に検査する）
	LongEdge    = 1568       // 縮小後の長辺（これより大きい画像はClaude側でも縮小されるため、先に縮めて通信とトークンを抑える）
	JPEGQuality = 80
)

var (
	ErrTooLarge    = errors.New("画像が大きすぎます")
	ErrUnsupported = errors.New("対応していない画像です")
)

// Prepare は画像のバイト列を検査し、長辺を LongEdge 以下に縮小したJPEGを返す。
// 対応: JPEG・PNG・WebP（アニメーションは最初のフレーム）。GIFなどは弾く。透過は白の背景にする。
func Prepare(data []byte) (jpegBytes []byte, w, h int, err error) {
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, 0, 0, ErrTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return nil, 0, 0, ErrUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, 0, 0, ErrTooLarge // デコードの前に弾く（展開の爆弾）
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, ErrUnsupported
	}
	b := src.Bounds()
	tw, th := b.Dx(), b.Dy()
	if m := max(tw, th); m > LongEdge {
		tw, th = max(1, tw*LongEdge/m), max(1, th*LongEdge/m)
	}
	// 白の背景に重ねて（透過をJPEGで黒にしない）、必要なら縮小する
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, 0, 0, ErrUnsupported
	}
	return buf.Bytes(), tw, th, nil
}
