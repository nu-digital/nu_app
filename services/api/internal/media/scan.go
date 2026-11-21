// Package media は画像の取り込み（検査・縮小・再エンコード）と保存を行う。設計: docs/design/p3-media.md。
//
// 取り込みは「安い検査→重い処理」の順。デコードの前に、形式ごとの**自前の厳密な構造検査**で、アニメーション・
// 寸法の偽装（canvasとフレームの不一致）・異常な構造を拒否する。ファイル名・拡張子・Content-Typeは信用しない。
package media

import (
	"bytes"
	"encoding/binary"
	"net/http"
)

const (
	MaxSide   = 10000
	MaxPixels = 20_000_000
)

// Format は画像の形式（実体から判定したもの）。
type Format string

const (
	JPEG Format = "jpeg"
	PNG  Format = "png"
	WebP Format = "webp"
)

// Error は呼び出し側がHTTPの状態にそのまま使える失敗。Messageは利用者に見せてよい日本語で、入力の内容を含めない。
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func unsupported() error {
	return &Error{http.StatusUnsupportedMediaType, "JPEG・PNG・WebPの画像だけアップロードできます"}
}
func invalid(msg string) error { return &Error{http.StatusUnprocessableEntity, msg} }

// Info は構造検査の結果。
type Info struct {
	Format Format
	W, H   int
	// Exif はEXIF（TIFF形式のバイト列。向きの読み取り用）。無ければnil。
	Exif []byte
	// HasAlphaHint はWebP/PNGのアルファの有無の目安（最終判定はデコード後の画素で行う）。
	HasAlphaHint bool
}

// Sniff はマジックバイトで形式を決める。該当しなければ415。
func Sniff(b []byte) (Format, error) {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return JPEG, nil
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return PNG, nil
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return WebP, nil
	}
	return "", unsupported()
}

// Scan は形式を決め、デコードせずに構造を検査して寸法を返す。寸法の上限（各辺・画素数）もここで検査する。
func Scan(b []byte) (Info, error) {
	f, err := Sniff(b)
	if err != nil {
		return Info{}, err
	}
	var info Info
	switch f {
	case PNG:
		info, err = scanPNG(b)
	case JPEG:
		info, err = scanJPEG(b)
	default:
		info, err = scanWebP(b)
	}
	if err != nil {
		return Info{}, err
	}
	info.Format = f
	if info.W < 1 || info.H < 1 {
		return Info{}, invalid("画像の寸法が正しくありません")
	}
	if info.W > MaxSide || info.H > MaxSide || uint64(info.W)*uint64(info.H) > MaxPixels {
		return Info{}, invalid("画像が大きすぎます（各辺10,000px・2,000万画素まで）")
	}
	return info, nil
}

// ---- PNG ----

func scanPNG(b []byte) (Info, error) {
	const maxChunks = 2000
	var info Info
	pos := uint64(8)
	n := uint64(len(b))
	first, sawIDAT, sawIEND := true, false, false
	for chunks := 0; pos < n; chunks++ {
		if chunks > maxChunks {
			return info, invalid("PNGの構造が正しくありません")
		}
		if pos+12 > n {
			return info, invalid("PNGが途中で切れています")
		}
		length := uint64(binary.BigEndian.Uint32(b[pos : pos+4]))
		typ := string(b[pos+4 : pos+8])
		if pos+12+length > n {
			return info, invalid("PNGが途中で切れています")
		}
		data := b[pos+8 : pos+8+length]
		if first {
			if typ != "IHDR" || length != 13 {
				return info, invalid("PNGの構造が正しくありません")
			}
			info.W = int(binary.BigEndian.Uint32(data[0:4]))
			info.H = int(binary.BigEndian.Uint32(data[4:8]))
			ct := data[9]
			info.HasAlphaHint = ct == 4 || ct == 6
			first = false
		}
		switch typ {
		case "acTL", "fcTL", "fdAT":
			return info, invalid("アニメーション画像は使えません")
		case "IDAT":
			sawIDAT = true
		case "eXIf":
			if info.Exif == nil {
				info.Exif = append([]byte(nil), data...)
			}
		case "IEND":
			sawIEND = true
		}
		pos += 12 + length
		if sawIEND {
			break
		}
	}
	if first || !sawIDAT || !sawIEND {
		return info, invalid("PNGが途中で切れています")
	}
	return info, nil
}

// ---- JPEG ----

func scanJPEG(b []byte) (Info, error) {
	const maxScans = 32
	var info Info
	n := len(b)
	pos := 2
	sof, scans := false, 0
	for {
		// マーカーの先頭の0xFFを探す（0xFFの連続は詰め物）
		for pos < n && b[pos] != 0xFF {
			pos++
		}
		for pos < n && b[pos] == 0xFF {
			pos++
		}
		if pos >= n {
			return info, invalid("JPEGが途中で切れています")
		}
		m := b[pos]
		pos++
		switch {
		case m == 0xD9: // EOI
			if !sof || scans == 0 {
				return info, invalid("JPEGの構造が正しくありません")
			}
			return info, nil
		case m == 0x00 || m == 0x01 || (m >= 0xD0 && m <= 0xD8): // 長さを持たない
			continue
		}
		if pos+2 > n {
			return info, invalid("JPEGが途中で切れています")
		}
		l := int(binary.BigEndian.Uint16(b[pos : pos+2]))
		if l < 2 || pos+l > n {
			return info, invalid("JPEGが途中で切れています")
		}
		seg := b[pos+2 : pos+l]
		switch {
		case m == 0xC0 || m == 0xC1 || m == 0xC2: // baseline / extended / progressive
			if sof {
				return info, invalid("JPEGの構造が正しくありません")
			}
			if len(seg) < 6 || (seg[5] != 1 && seg[5] != 3 && seg[5] != 4) {
				return info, invalid("JPEGの構造が正しくありません")
			}
			info.H = int(binary.BigEndian.Uint16(seg[1:3]))
			info.W = int(binary.BigEndian.Uint16(seg[3:5]))
			sof = true
		case (m >= 0xC3 && m <= 0xCF) && m != 0xC4 && m != 0xC8 && m != 0xCC: // ロスレス・算術符号・階層型は扱わない
			return info, unsupported()
		case m == 0xDC: // DNL（行数を後から指定する。寸法の偽装に使える）
			return info, invalid("このJPEGは扱えません")
		case m == 0xE1 && info.Exif == nil && len(seg) > 6 && string(seg[:6]) == "Exif\x00\x00":
			info.Exif = append([]byte(nil), seg[6:]...)
		case m == 0xDA: // SOS
			if !sof {
				return info, invalid("JPEGの構造が正しくありません")
			}
			scans++
			if scans > maxScans {
				return info, invalid("このJPEGは扱えません")
			}
			pos += l
			// エントロピー符号化データを読み飛ばす（次のマーカーまで。FF00・RSTnはデータの一部）
			for pos < n {
				if b[pos] != 0xFF {
					pos++
					continue
				}
				if pos+1 >= n {
					return info, invalid("JPEGが途中で切れています")
				}
				nx := b[pos+1]
				if nx == 0x00 || (nx >= 0xD0 && nx <= 0xD7) {
					pos += 2
					continue
				}
				if nx == 0xFF {
					pos++
					continue
				}
				break
			}
			continue
		}
		pos += l
	}
}

// ---- WebP ----

func scanWebP(b []byte) (Info, error) {
	var info Info
	n := uint64(len(b))
	if uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != n {
		return info, invalid("WebPのサイズが正しくありません")
	}
	pos := uint64(12)
	var canvasW, canvasH, frameW, frameH int
	haveVP8X, frames := false, 0
	for pos < n {
		if pos+8 > n {
			return info, invalid("WebPが途中で切れています")
		}
		cc := string(b[pos : pos+4])
		size := uint64(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		if pos+8+size > n {
			return info, invalid("WebPが途中で切れています")
		}
		data := b[pos+8 : pos+8+size]
		switch cc {
		case "VP8X":
			if size < 10 || haveVP8X || pos != 12 {
				return info, invalid("WebPの構造が正しくありません")
			}
			haveVP8X = true
			if data[0]&0x02 != 0 {
				return info, invalid("アニメーション画像は使えません")
			}
			info.HasAlphaHint = data[0]&0x10 != 0
			canvasW = int(uint32(data[4])|uint32(data[5])<<8|uint32(data[6])<<16) + 1
			canvasH = int(uint32(data[7])|uint32(data[8])<<8|uint32(data[9])<<16) + 1
		case "ANIM", "ANMF":
			return info, invalid("アニメーション画像は使えません")
		case "VP8 ":
			frames++
			if size < 10 || data[3] != 0x9D || data[4] != 0x01 || data[5] != 0x2A {
				return info, invalid("WebPの構造が正しくありません")
			}
			frameW = int(binary.LittleEndian.Uint16(data[6:8]) & 0x3FFF)
			frameH = int(binary.LittleEndian.Uint16(data[8:10]) & 0x3FFF)
		case "VP8L":
			frames++
			if size < 5 || data[0] != 0x2F {
				return info, invalid("WebPの構造が正しくありません")
			}
			bits := binary.LittleEndian.Uint32(data[1:5])
			frameW = int(bits&0x3FFF) + 1
			frameH = int((bits>>14)&0x3FFF) + 1
			if (bits>>28)&1 == 1 {
				info.HasAlphaHint = true
			}
		case "ALPH":
			info.HasAlphaHint = true
		case "EXIF":
			if info.Exif == nil {
				d := data
				if len(d) > 6 && string(d[:6]) == "Exif\x00\x00" {
					d = d[6:]
				}
				info.Exif = append([]byte(nil), d...)
			}
		}
		pos += 8 + size + size%2 // 奇数長はパディング
	}
	if pos != n || frames != 1 {
		return info, invalid("WebPの構造が正しくありません")
	}
	if haveVP8X && (canvasW != frameW || canvasH != frameH) {
		return info, invalid("WebPの寸法が正しくありません")
	}
	info.W, info.H = frameW, frameH
	return info, nil
}
