package trie

import (
	"bytes"
	"testing"
)

func TestHexToCompactLeafEven(t *testing.T) {
	// Leaf with even number of nibbles: [1, 2, 3, 4, 16]
	hex := []byte{1, 2, 3, 4, terminatorByte}
	compact := hexToCompact(hex)
	// Flag: leaf=1, even => first byte = 0x20, then packed nibbles.
	expected := []byte{0x20, 0x12, 0x34}
	if !bytes.Equal(compact, expected) {
		t.Errorf("hexToCompact(%v) = %x, want %x", hex, compact, expected)
	}
}

func TestHexToCompactLeafOdd(t *testing.T) {
	// Leaf with odd number of nibbles: [1, 2, 3, 16]
	hex := []byte{1, 2, 3, terminatorByte}
	compact := hexToCompact(hex)
	// Flag: leaf=1, odd => first byte = 0x31, then packed nibbles.
	expected := []byte{0x31, 0x23}
	if !bytes.Equal(compact, expected) {
		t.Errorf("hexToCompact(%v) = %x, want %x", hex, compact, expected)
	}
}

func TestHexToCompactExtensionEven(t *testing.T) {
	// Extension with even number of nibbles: [1, 2, 3, 4]
	hex := []byte{1, 2, 3, 4}
	compact := hexToCompact(hex)
	// Flag: extension, even => first byte = 0x00, then packed nibbles.
	expected := []byte{0x00, 0x12, 0x34}
	if !bytes.Equal(compact, expected) {
		t.Errorf("hexToCompact(%v) = %x, want %x", hex, compact, expected)
	}
}

func TestHexToCompactExtensionOdd(t *testing.T) {
	// Extension with odd number of nibbles: [1, 2, 3]
	hex := []byte{1, 2, 3}
	compact := hexToCompact(hex)
	// Flag: extension, odd => first byte = 0x11, then packed nibbles.
	expected := []byte{0x11, 0x23}
	if !bytes.Equal(compact, expected) {
		t.Errorf("hexToCompact(%v) = %x, want %x", hex, compact, expected)
	}
}

func TestCompactToHexRoundtrip(t *testing.T) {
	tests := [][]byte{
		{1, 2, 3, 4, terminatorByte},    // leaf, even
		{1, 2, 3, terminatorByte},       // leaf, odd
		{1, 2, 3, 4},                    // extension, even
		{1, 2, 3},                       // extension, odd
		{0, terminatorByte},             // leaf, single nibble
		{0xf, 0xa, 0xb, terminatorByte}, // leaf, odd
		{},                              // empty extension
	}

	for _, hex := range tests {
		compact := hexToCompact(hex)
		result := compactToHex(compact)
		if !bytes.Equal(result, hex) {
			t.Errorf("compactToHex(hexToCompact(%v)) = %v, want %v", hex, result, hex)
		}
	}
}

func TestKeybytesToHex(t *testing.T) {
	key := []byte{0x12, 0x34, 0x56}
	hex := keybytesToHex(key)
	expected := []byte{1, 2, 3, 4, 5, 6, terminatorByte}
	if !bytes.Equal(hex, expected) {
		t.Errorf("keybytesToHex(%x) = %v, want %v", key, hex, expected)
	}
}

func TestHexToKeybytes(t *testing.T) {
	hex := []byte{1, 2, 3, 4, 5, 6, terminatorByte}
	key := hexToKeybytes(hex)
	expected := []byte{0x12, 0x34, 0x56}
	if !bytes.Equal(key, expected) {
		t.Errorf("hexToKeybytes(%v) = %x, want %x", hex, key, expected)
	}
}

func TestKeybytesRoundtrip(t *testing.T) {
	keys := [][]byte{
		{},
		{0x00},
		{0xff},
		{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0},
		{0x00, 0x00, 0x00},
	}
	for _, key := range keys {
		hex := keybytesToHex(key)
		result := hexToKeybytes(hex)
		if !bytes.Equal(result, key) {
			t.Errorf("hexToKeybytes(keybytesToHex(%x)) = %x, want %x", key, result, key)
		}
	}
}

func TestPrefixLen(t *testing.T) {
	tests := []struct {
		a, b []byte
		want int
	}{
		{[]byte{1, 2, 3}, []byte{1, 2, 4}, 2},
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, 3},
		{[]byte{1, 2, 3}, []byte{4, 5, 6}, 0},
		{[]byte{}, []byte{1}, 0},
		{[]byte{1}, []byte{}, 0},
	}
	for _, tt := range tests {
		got := prefixLen(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("prefixLen(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestHasTerm(t *testing.T) {
	if !hasTerm([]byte{1, 2, 3, terminatorByte}) {
		t.Error("expected hasTerm to return true")
	}
	if hasTerm([]byte{1, 2, 3}) {
		t.Error("expected hasTerm to return false")
	}
	if hasTerm([]byte{}) {
		t.Error("expected hasTerm to return false for empty")
	}
}
