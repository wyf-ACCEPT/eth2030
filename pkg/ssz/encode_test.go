package ssz

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// --- Basic type encode tests ---

func TestMarshalBoolValues(t *testing.T) {
	if got := MarshalBool(false); !bytes.Equal(got, []byte{0}) {
		t.Errorf("MarshalBool(false) = %v, want [0]", got)
	}
	if got := MarshalBool(true); !bytes.Equal(got, []byte{1}) {
		t.Errorf("MarshalBool(true) = %v, want [1]", got)
	}
}

func TestMarshalUint8Values(t *testing.T) {
	tests := []uint8{0, 1, 127, 255}
	for _, v := range tests {
		got := MarshalUint8(v)
		if len(got) != 1 || got[0] != v {
			t.Errorf("MarshalUint8(%d) = %v", v, got)
		}
	}
}

func TestMarshalUint16LittleEndian(t *testing.T) {
	got := MarshalUint16(0x0102)
	if !bytes.Equal(got, []byte{0x02, 0x01}) {
		t.Errorf("MarshalUint16(0x0102) = %x, want [02 01]", got)
	}
}

func TestMarshalUint32LittleEndian(t *testing.T) {
	got := MarshalUint32(0xaabbccdd)
	expected := make([]byte, 4)
	binary.LittleEndian.PutUint32(expected, 0xaabbccdd)
	if !bytes.Equal(got, expected) {
		t.Errorf("MarshalUint32(0xaabbccdd) = %x, want %x", got, expected)
	}
}

func TestMarshalUint64LittleEndian(t *testing.T) {
	got := MarshalUint64(0xdeadbeef)
	expected := make([]byte, 8)
	binary.LittleEndian.PutUint64(expected, 0xdeadbeef)
	if !bytes.Equal(got, expected) {
		t.Errorf("MarshalUint64(0xdeadbeef) = %x, want %x", got, expected)
	}
}

func TestMarshalUint64Zero(t *testing.T) {
	got := MarshalUint64(0)
	if !bytes.Equal(got, make([]byte, 8)) {
		t.Errorf("MarshalUint64(0) should be 8 zero bytes")
	}
}

func TestMarshalUint128Values(t *testing.T) {
	got := MarshalUint128(0xaa, 0xbb)
	if len(got) != 16 {
		t.Fatalf("length = %d, want 16", len(got))
	}
	lo := binary.LittleEndian.Uint64(got[0:8])
	hi := binary.LittleEndian.Uint64(got[8:16])
	if lo != 0xaa || hi != 0xbb {
		t.Errorf("MarshalUint128(0xaa, 0xbb): lo=%x, hi=%x", lo, hi)
	}
}

func TestMarshalUint256Values(t *testing.T) {
	limbs := [4]uint64{1, 2, 3, 4}
	got := MarshalUint256(limbs)
	if len(got) != 32 {
		t.Fatalf("length = %d, want 32", len(got))
	}
	for i, l := range limbs {
		v := binary.LittleEndian.Uint64(got[i*8 : (i+1)*8])
		if v != l {
			t.Errorf("limb %d = %d, want %d", i, v, l)
		}
	}
}

// --- Vector/List/Container encode tests ---

func TestMarshalVectorConcatenates(t *testing.T) {
	elems := [][]byte{{1, 2}, {3, 4}, {5, 6}}
	got := MarshalVector(elems)
	if !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6}) {
		t.Errorf("MarshalVector = %v, want [1 2 3 4 5 6]", got)
	}
}

func TestMarshalVectorEmpty(t *testing.T) {
	got := MarshalVector(nil)
	if len(got) != 0 {
		t.Errorf("MarshalVector(nil) length = %d, want 0", len(got))
	}
}

func TestMarshalFixedContainer(t *testing.T) {
	fields := [][]byte{MarshalUint32(1), MarshalUint32(2)}
	got := MarshalFixedContainer(fields)
	expected := make([]byte, 8)
	binary.LittleEndian.PutUint32(expected[0:4], 1)
	binary.LittleEndian.PutUint32(expected[4:8], 2)
	if !bytes.Equal(got, expected) {
		t.Errorf("MarshalFixedContainer = %x, want %x", got, expected)
	}
}

func TestMarshalListEqualsVector(t *testing.T) {
	elems := [][]byte{{1}, {2}, {3}}
	if !bytes.Equal(MarshalList(elems), MarshalVector(elems)) {
		t.Error("MarshalList should produce same output as MarshalVector")
	}
}

func TestMarshalVariableContainerBasic(t *testing.T) {
	// Container: uint32 (fixed), bytes (variable)
	fixed0 := MarshalUint32(42)
	variable0 := []byte("hello")

	fixedParts := [][]byte{fixed0, nil}
	variableParts := [][]byte{variable0}
	variableIndices := []int{1}

	encoded := MarshalVariableContainer(fixedParts, variableParts, variableIndices)

	// Fixed part: 4 bytes for uint32 + 4 bytes for offset = 8 bytes.
	// Variable part: 5 bytes for "hello".
	if len(encoded) != 8+5 {
		t.Fatalf("length = %d, want 13", len(encoded))
	}

	// Decode the uint32.
	v := binary.LittleEndian.Uint32(encoded[0:4])
	if v != 42 {
		t.Errorf("field 0 = %d, want 42", v)
	}

	// Decode the offset.
	offset := binary.LittleEndian.Uint32(encoded[4:8])
	if offset != 8 {
		t.Errorf("offset = %d, want 8", offset)
	}

	// Decode the variable part.
	if !bytes.Equal(encoded[8:], []byte("hello")) {
		t.Errorf("variable part = %q, want %q", encoded[8:], "hello")
	}
}

func TestMarshalVariableContainerMultipleVarFields(t *testing.T) {
	fixed0 := MarshalUint32(1)
	var0 := []byte("abc")
	var1 := []byte("defgh")

	encoded := MarshalVariableContainer(
		[][]byte{fixed0, nil, nil},
		[][]byte{var0, var1},
		[]int{1, 2},
	)

	// Fixed: 4 (uint32) + 4 (offset1) + 4 (offset2) = 12
	// Variable: 3 + 5 = 8
	if len(encoded) != 20 {
		t.Fatalf("length = %d, want 20", len(encoded))
	}

	offset1 := binary.LittleEndian.Uint32(encoded[4:8])
	offset2 := binary.LittleEndian.Uint32(encoded[8:12])
	if offset1 != 12 {
		t.Errorf("offset1 = %d, want 12", offset1)
	}
	if offset2 != 15 {
		t.Errorf("offset2 = %d, want 15", offset2)
	}
}

// --- Bitfield encode tests ---

func TestMarshalBitvectorSingleByte(t *testing.T) {
	bits := []bool{true, false, true, true, false, false, true, false}
	got := MarshalBitvector(bits)
	// bits[0]=1, bits[2]=1, bits[3]=1, bits[6]=1 -> 0b01001101 = 0x4d
	if len(got) != 1 || got[0] != 0x4d {
		t.Errorf("MarshalBitvector = %x, want [4d]", got)
	}
}

func TestMarshalBitvectorMultipleBytes(t *testing.T) {
	bits := make([]bool, 16)
	bits[0] = true
	bits[8] = true
	got := MarshalBitvector(bits)
	if len(got) != 2 {
		t.Fatalf("length = %d, want 2", len(got))
	}
	if got[0] != 1 || got[1] != 1 {
		t.Errorf("MarshalBitvector 16 bits = %v, want [1, 1]", got)
	}
}

func TestMarshalBitvectorEmpty(t *testing.T) {
	got := MarshalBitvector(nil)
	if len(got) != 0 {
		t.Errorf("MarshalBitvector(nil) length = %d, want 0", len(got))
	}
}

func TestMarshalBitlistWithSentinel(t *testing.T) {
	bits := []bool{true, false, true}
	got := MarshalBitlist(bits)
	// 3 data bits + 1 sentinel = 4 bits = 1 byte.
	// bits: [1, 0, 1, 1(sentinel)] -> 0b1101 = 0x0d
	if len(got) != 1 || got[0] != 0x0d {
		t.Errorf("MarshalBitlist([1,0,1]) = %x, want [0d]", got)
	}
}

func TestMarshalBitlistEmpty(t *testing.T) {
	got := MarshalBitlist(nil)
	// Just the sentinel: 1 bit = 1 byte = 0x01
	if len(got) != 1 || got[0] != 0x01 {
		t.Errorf("MarshalBitlist(nil) = %x, want [01]", got)
	}
}

func TestMarshalBitlistBoundary(t *testing.T) {
	// 7 data bits + sentinel = 8 bits = 1 byte.
	bits := []bool{true, true, true, true, true, true, true}
	got := MarshalBitlist(bits)
	// 0b11111111 = 0xff
	if len(got) != 1 || got[0] != 0xff {
		t.Errorf("MarshalBitlist(7 true) = %x, want [ff]", got)
	}
}

func TestMarshalBitlistOverByte(t *testing.T) {
	// 8 data bits + sentinel = 9 bits = 2 bytes.
	bits := make([]bool, 8)
	got := MarshalBitlist(bits)
	if len(got) != 2 {
		t.Fatalf("length = %d, want 2", len(got))
	}
	// 8 false bits in first byte, sentinel (1) in second byte.
	if got[0] != 0x00 || got[1] != 0x01 {
		t.Errorf("MarshalBitlist(8 false) = %x, want [00 01]", got)
	}
}

// --- ByteVector/ByteList encode tests ---

func TestMarshalByteVectorCopy(t *testing.T) {
	data := []byte{1, 2, 3, 4}
	got := MarshalByteVector(data)
	if !bytes.Equal(got, data) {
		t.Errorf("MarshalByteVector mismatch")
	}
	// Verify it's a copy.
	data[0] = 99
	if got[0] == 99 {
		t.Error("MarshalByteVector should return a copy")
	}
}

func TestMarshalByteListCopy(t *testing.T) {
	data := []byte{5, 6, 7}
	got := MarshalByteList(data)
	if !bytes.Equal(got, data) {
		t.Errorf("MarshalByteList mismatch")
	}
	data[0] = 99
	if got[0] == 99 {
		t.Error("MarshalByteList should return a copy")
	}
}

// --- isVariableIndex tests ---

func TestIsVariableIndex(t *testing.T) {
	indices := []int{1, 3, 5}
	if !isVariableIndex(1, indices) {
		t.Error("1 should be variable")
	}
	if !isVariableIndex(3, indices) {
		t.Error("3 should be variable")
	}
	if isVariableIndex(0, indices) {
		t.Error("0 should not be variable")
	}
	if isVariableIndex(2, indices) {
		t.Error("2 should not be variable")
	}
}
