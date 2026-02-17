package trie

// Hex-prefix (HP) encoding as specified in the Ethereum Yellow Paper, Appendix C.
//
// Nibble sequences are encoded with a prefix that encodes both the parity of
// the sequence length and a "terminator" flag that distinguishes leaf nodes
// from extension nodes.
//
// Hex nibble representation uses values 0x0-0xf for data nibbles and 0x10
// (the terminator) to mark the end of a leaf key.

const terminatorByte = 16

// hexToCompact converts a hex nibble sequence (with possible terminator) to
// compact (hex-prefix) encoding per the Yellow Paper.
//
// The high nibble of the first byte encodes flags:
//   - bit 1 (0x20): set if the key is a leaf (terminator present)
//   - bit 0 (0x10): set if the nibble count is odd
//
// If the nibble count is odd, the low nibble of the first byte is the first
// nibble. If even, the low nibble is zero and acts as padding.
func hexToCompact(hex []byte) []byte {
	terminator := byte(0)
	if hasTerm(hex) {
		terminator = 1
		hex = hex[:len(hex)-1] // strip terminator for encoding
	}
	buf := make([]byte, len(hex)/2+1)
	buf[0] = terminator << 5 // set leaf flag in bit 5
	if len(hex)&1 == 1 {
		buf[0] |= 1 << 4 // set odd-length flag in bit 4
		buf[0] |= hex[0] // first nibble goes in low nibble of first byte
		hex = hex[1:]
	}
	decodeNibbles(hex, buf[1:])
	return buf
}

// compactToHex converts compact (hex-prefix) encoded bytes back to the
// hex nibble sequence. If the compact encoding represents a leaf, the
// returned nibble sequence includes the terminator.
func compactToHex(compact []byte) []byte {
	if len(compact) == 0 {
		return compact
	}
	base := keybytesToHex(compact)
	// Remove the last nibble (terminator from keybytesToHex) since HP already encodes it.
	base = base[:len(base)-1]
	// The first nibble of the expanded form contains the flags.
	// If bit 0 of the flags nibble is 0, the key length is even, so there's
	// a padding nibble we need to skip (chop 2 nibbles). If odd, chop 1.
	chop := 2 - base[0]&1
	// If bit 1 of the flags nibble is set, it's a leaf -- append terminator.
	if base[0]&2 != 0 {
		// Leaf node.
		result := make([]byte, len(base)-int(chop)+1)
		copy(result, base[chop:])
		result[len(result)-1] = terminatorByte
		return result
	}
	return base[chop:]
}

// keybytesToHex converts a raw byte key to a hex nibble sequence, appending
// a terminator nibble (0x10) at the end.
func keybytesToHex(str []byte) []byte {
	l := len(str)*2 + 1
	nibbles := make([]byte, l)
	for i, b := range str {
		nibbles[i*2] = b / 16
		nibbles[i*2+1] = b % 16
	}
	nibbles[l-1] = terminatorByte
	return nibbles
}

// hexToKeybytes converts a hex nibble sequence (without terminator) back to
// the original byte key. The nibble sequence length must be even.
func hexToKeybytes(hex []byte) []byte {
	if hasTerm(hex) {
		hex = hex[:len(hex)-1]
	}
	if len(hex)&1 != 0 {
		panic("hexToKeybytes: odd length hex key")
	}
	key := make([]byte, len(hex)/2)
	decodeNibbles(hex, key)
	return key
}

// decodeNibbles packs pairs of nibbles into bytes.
func decodeNibbles(nibbles []byte, bytes []byte) {
	for bi, ni := 0, 0; ni < len(nibbles); bi, ni = bi+1, ni+2 {
		bytes[bi] = nibbles[ni]<<4 | nibbles[ni+1]
	}
}

// prefixLen returns the length of the common prefix of a and b.
func prefixLen(a, b []byte) int {
	var i, length int
	if len(a) < len(b) {
		length = len(a)
	} else {
		length = len(b)
	}
	for ; i < length; i++ {
		if a[i] != b[i] {
			break
		}
	}
	return i
}

// hasTerm returns true if the hex nibble sequence ends with the terminator.
func hasTerm(s []byte) bool {
	return len(s) > 0 && s[len(s)-1] == terminatorByte
}
