package peerwire

// Bitmap tracks which pieces a peer has advertised.
type Bitmap []byte

// Contains reports whether the bit for a piece index is set.
func (b Bitmap) Contains(piece int) bool {
	byteIndex := piece / 8
	bitOffset := piece % 8
	if byteIndex < 0 || byteIndex >= len(b) {
		return false
	}
	return b[byteIndex]>>(7-uint(bitOffset))&1 == 1
}

// Mark flips the bit for a piece index if the index falls inside the bitmap.
func (b Bitmap) Mark(piece int) {
	byteIndex := piece / 8
	bitOffset := piece % 8
	if byteIndex < 0 || byteIndex >= len(b) {
		return
	}
	b[byteIndex] |= 1 << (7 - uint(bitOffset))
}
