package media

import "encoding/binary"

// orientation はEXIF（TIFF形式）からOrientation（1〜8）を読む。
// 範囲検査つきの最小の実装: IFD0だけを読み、SHORT型・count=1・値1〜8のものだけ採る。それ以外・壊れたデータは1（補正なし）。
func orientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	n := uint64(len(tiff))
	ifd := uint64(bo.Uint32(tiff[4:8]))
	if ifd+2 > n {
		return 1
	}
	count := uint64(bo.Uint16(tiff[ifd : ifd+2]))
	if count > 1000 {
		return 1
	}
	for i := uint64(0); i < count; i++ {
		e := ifd + 2 + i*12
		if e+12 > n {
			return 1
		}
		if bo.Uint16(tiff[e:e+2]) != 0x0112 {
			continue
		}
		if bo.Uint16(tiff[e+2:e+4]) != 3 || bo.Uint32(tiff[e+4:e+8]) != 1 { // SHORT・count=1
			return 1
		}
		v := int(bo.Uint16(tiff[e+8 : e+10]))
		if v < 1 || v > 8 {
			return 1
		}
		return v
	}
	return 1
}
