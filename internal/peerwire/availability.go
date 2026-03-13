package peerwire

// Bitmap 记录某个 peer 宣告自己持有哪些 piece。
type Bitmap []byte

// Contains 判断某个 piece 对应的位是否已经被置位。
func (b Bitmap) Contains(piece int) bool {
	byteIndex := piece / 8
	bitOffset := piece % 8
	if byteIndex < 0 || byteIndex >= len(b) {
		return false
	}
	return b[byteIndex]>>(7-uint(bitOffset))&1 == 1
}

// Mark 在索引合法时把对应 piece 标记为可用。
func (b Bitmap) Mark(piece int) {
	byteIndex := piece / 8
	bitOffset := piece % 8
	if byteIndex < 0 || byteIndex >= len(b) {
		return
	}
	b[byteIndex] |= 1 << (7 - uint(bitOffset))
}
