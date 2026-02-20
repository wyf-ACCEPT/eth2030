package ssz

import (
	"bytes"
	"testing"
)

// --- Basic type decode tests ---

func TestUnmarshalBoolValues(t *testing.T) {
	tests := []struct {
		input []byte
		want  bool
		err   error
	}{
		{[]byte{0}, false, nil},
		{[]byte{1}, true, nil},
		{[]byte{2}, false, ErrInvalidBool},
		{[]byte{0xff}, false, ErrInvalidBool},
		{nil, false, ErrSize},
		{[]byte{}, false, ErrSize},
		{[]byte{0, 0}, false, ErrSize},
	}
	for _, tt := range tests {
		got, err := UnmarshalBool(tt.input)
		if err != tt.err {
			t.Errorf("UnmarshalBool(%v): err = %v, want %v", tt.input, err, tt.err)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("UnmarshalBool(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestUnmarshalUint8Values(t *testing.T) {
	for _, v := range []uint8{0, 1, 127, 255} {
		got, err := UnmarshalUint8(MarshalUint8(v))
		if err != nil {
			t.Fatalf("UnmarshalUint8(%d): %v", v, err)
		}
		if got != v {
			t.Fatalf("UnmarshalUint8(%d) = %d", v, got)
		}
	}
}

func TestUnmarshalUint16Values(t *testing.T) {
	for _, v := range []uint16{0, 1, 0xff, 0xffff} {
		got, err := UnmarshalUint16(MarshalUint16(v))
		if err != nil {
			t.Fatalf("uint16 roundtrip %d: %v", v, err)
		}
		if got != v {
			t.Fatalf("uint16 roundtrip %d: got %d", v, got)
		}
	}
}

func TestUnmarshalUint32Values(t *testing.T) {
	for _, v := range []uint32{0, 1, 0xdeadbeef, 0xffffffff} {
		got, err := UnmarshalUint32(MarshalUint32(v))
		if err != nil {
			t.Fatalf("uint32 roundtrip %x: %v", v, err)
		}
		if got != v {
			t.Fatalf("uint32 roundtrip %x: got %x", v, got)
		}
	}
}

func TestUnmarshalUint64Values(t *testing.T) {
	for _, v := range []uint64{0, 1, 0xdeadbeef, 0xffffffffffffffff} {
		got, err := UnmarshalUint64(MarshalUint64(v))
		if err != nil {
			t.Fatalf("uint64 roundtrip %x: %v", v, err)
		}
		if got != v {
			t.Fatalf("uint64 roundtrip %x: got %x", v, got)
		}
	}
}

func TestUnmarshalUint128Roundtrip(t *testing.T) {
	tests := [][2]uint64{
		{0, 0},
		{1, 0},
		{0, 1},
		{0xffffffffffffffff, 0xffffffffffffffff},
		{42, 99},
	}
	for _, tt := range tests {
		lo, hi, err := UnmarshalUint128(MarshalUint128(tt[0], tt[1]))
		if err != nil {
			t.Fatalf("uint128 roundtrip (%d, %d): %v", tt[0], tt[1], err)
		}
		if lo != tt[0] || hi != tt[1] {
			t.Fatalf("uint128 roundtrip (%d, %d): got (%d, %d)", tt[0], tt[1], lo, hi)
		}
	}
}

func TestUnmarshalUint256Roundtrip(t *testing.T) {
	limbs := [4]uint64{1, 2, 3, 4}
	got, err := UnmarshalUint256(MarshalUint256(limbs))
	if err != nil {
		t.Fatalf("uint256 roundtrip: %v", err)
	}
	if got != limbs {
		t.Fatalf("uint256 roundtrip: got %v, want %v", got, limbs)
	}
}

// --- Size error tests ---

func TestUnmarshalSizeErrorsExtended(t *testing.T) {
	if _, err := UnmarshalUint8([]byte{}); err != ErrSize {
		t.Errorf("uint8 empty: %v", err)
	}
	if _, err := UnmarshalUint8([]byte{1, 2}); err != ErrSize {
		t.Errorf("uint8 too long: %v", err)
	}
	if _, err := UnmarshalUint16([]byte{1}); err != ErrSize {
		t.Errorf("uint16 too short: %v", err)
	}
	if _, err := UnmarshalUint32([]byte{1, 2}); err != ErrSize {
		t.Errorf("uint32 too short: %v", err)
	}
	if _, err := UnmarshalUint64([]byte{1}); err != ErrSize {
		t.Errorf("uint64 too short: %v", err)
	}
	if _, _, err := UnmarshalUint128([]byte{1, 2, 3}); err != ErrSize {
		t.Errorf("uint128 too short: %v", err)
	}
	if _, err := UnmarshalUint256([]byte{1, 2, 3}); err != ErrSize {
		t.Errorf("uint256 too short: %v", err)
	}
}

// --- Vector/List decode tests ---

func TestUnmarshalVectorValid(t *testing.T) {
	data := make([]byte, 24) // 3 elements * 8 bytes each
	data[0] = 1
	data[8] = 2
	data[16] = 3

	elems, err := UnmarshalVector(data, 3, 8)
	if err != nil {
		t.Fatalf("UnmarshalVector: %v", err)
	}
	if len(elems) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(elems))
	}
	for i, elem := range elems {
		if len(elem) != 8 {
			t.Errorf("elem %d length = %d, want 8", i, len(elem))
		}
	}
}

func TestUnmarshalVectorWrongSize(t *testing.T) {
	_, err := UnmarshalVector([]byte{1, 2, 3}, 2, 2) // expects 4 bytes
	if err != ErrSize {
		t.Errorf("expected ErrSize, got %v", err)
	}
}

func TestUnmarshalListValid(t *testing.T) {
	data := make([]byte, 12) // 3 * 4-byte elements
	elems, err := UnmarshalList(data, 4)
	if err != nil {
		t.Fatalf("UnmarshalList: %v", err)
	}
	if len(elems) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(elems))
	}
}

func TestUnmarshalListNotDivisible(t *testing.T) {
	_, err := UnmarshalList([]byte{1, 2, 3}, 2)
	if err != ErrSize {
		t.Errorf("expected ErrSize, got %v", err)
	}
}

func TestUnmarshalListZeroElemSize(t *testing.T) {
	_, err := UnmarshalList([]byte{1, 2}, 0)
	if err != ErrSize {
		t.Errorf("expected ErrSize, got %v", err)
	}
}

func TestUnmarshalListEmpty(t *testing.T) {
	elems, err := UnmarshalList([]byte{}, 4)
	if err != nil {
		t.Fatalf("UnmarshalList empty: %v", err)
	}
	if len(elems) != 0 {
		t.Errorf("expected 0 elements, got %d", len(elems))
	}
}

// --- Variable container decode tests ---

func TestUnmarshalVariableContainerBasic(t *testing.T) {
	// Container: uint32 (fixed), bytes (variable)
	fixedParts := [][]byte{MarshalUint32(42), nil}
	variableParts := [][]byte{[]byte("hello")}
	variableIndices := []int{1}
	encoded := MarshalVariableContainer(fixedParts, variableParts, variableIndices)

	fields, err := UnmarshalVariableContainer(encoded, 2, []int{4, 0})
	if err != nil {
		t.Fatalf("UnmarshalVariableContainer: %v", err)
	}

	v, _ := UnmarshalUint32(fields[0])
	if v != 42 {
		t.Errorf("field 0 = %d, want 42", v)
	}
	if !bytes.Equal(fields[1], []byte("hello")) {
		t.Errorf("field 1 = %q, want %q", fields[1], "hello")
	}
}

func TestUnmarshalVariableContainerMultipleVar(t *testing.T) {
	// Container: uint32, bytes, bytes
	f0 := MarshalUint32(10)
	v0 := []byte("abc")
	v1 := []byte("defgh")

	encoded := MarshalVariableContainer(
		[][]byte{f0, nil, nil},
		[][]byte{v0, v1},
		[]int{1, 2},
	)

	fields, err := UnmarshalVariableContainer(encoded, 3, []int{4, 0, 0})
	if err != nil {
		t.Fatalf("UnmarshalVariableContainer: %v", err)
	}

	val, _ := UnmarshalUint32(fields[0])
	if val != 10 {
		t.Errorf("field 0 = %d, want 10", val)
	}
	if !bytes.Equal(fields[1], v0) {
		t.Errorf("field 1 = %q, want %q", fields[1], v0)
	}
	if !bytes.Equal(fields[2], v1) {
		t.Errorf("field 2 = %q, want %q", fields[2], v1)
	}
}

func TestUnmarshalVariableContainerTruncated(t *testing.T) {
	// Too short data should fail.
	_, err := UnmarshalVariableContainer([]byte{1}, 2, []int{4, 0})
	if err == nil {
		t.Error("expected error for truncated data")
	}
}

// --- Bitvector decode tests ---

func TestUnmarshalBitvectorValid(t *testing.T) {
	bits := []bool{true, false, true, true, false, false, true, false}
	encoded := MarshalBitvector(bits)
	decoded, err := UnmarshalBitvector(encoded, 8)
	if err != nil {
		t.Fatalf("UnmarshalBitvector: %v", err)
	}
	for i, b := range bits {
		if decoded[i] != b {
			t.Errorf("bit %d: got %v, want %v", i, decoded[i], b)
		}
	}
}

func TestUnmarshalBitvectorPartialByte(t *testing.T) {
	// 5 bits = 1 byte
	bits := []bool{true, true, false, true, false}
	encoded := MarshalBitvector(bits)
	decoded, err := UnmarshalBitvector(encoded, 5)
	if err != nil {
		t.Fatalf("UnmarshalBitvector(5 bits): %v", err)
	}
	for i, b := range bits {
		if decoded[i] != b {
			t.Errorf("bit %d: got %v, want %v", i, decoded[i], b)
		}
	}
}

func TestUnmarshalBitvectorWrongSize(t *testing.T) {
	_, err := UnmarshalBitvector([]byte{0xff}, 16) // expects 2 bytes
	if err != ErrSize {
		t.Errorf("expected ErrSize, got %v", err)
	}
}

// --- Bitlist decode tests ---

func TestUnmarshalBitlistValid(t *testing.T) {
	bits := []bool{true, false, true, false, true}
	encoded := MarshalBitlist(bits)
	decoded, err := UnmarshalBitlist(encoded)
	if err != nil {
		t.Fatalf("UnmarshalBitlist: %v", err)
	}
	if len(decoded) != len(bits) {
		t.Fatalf("length = %d, want %d", len(decoded), len(bits))
	}
	for i, b := range bits {
		if decoded[i] != b {
			t.Errorf("bit %d: got %v, want %v", i, decoded[i], b)
		}
	}
}

func TestUnmarshalBitlistEmpty(t *testing.T) {
	encoded := MarshalBitlist([]bool{})
	decoded, err := UnmarshalBitlist(encoded)
	if err != nil {
		t.Fatalf("UnmarshalBitlist empty: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("expected 0 bits, got %d", len(decoded))
	}
}

func TestUnmarshalBitlistNoData(t *testing.T) {
	_, err := UnmarshalBitlist([]byte{})
	if err != ErrSize {
		t.Errorf("expected ErrSize, got %v", err)
	}
}

func TestUnmarshalBitlistNoSentinel(t *testing.T) {
	_, err := UnmarshalBitlist([]byte{0x00})
	if err != ErrSize {
		t.Errorf("expected ErrSize (no sentinel), got %v", err)
	}
}

func TestUnmarshalBitlistAllOnes(t *testing.T) {
	bits := []bool{true, true, true, true, true, true, true, true}
	encoded := MarshalBitlist(bits)
	decoded, err := UnmarshalBitlist(encoded)
	if err != nil {
		t.Fatalf("UnmarshalBitlist all ones: %v", err)
	}
	if len(decoded) != 8 {
		t.Fatalf("length = %d, want 8", len(decoded))
	}
	for i, b := range decoded {
		if !b {
			t.Errorf("bit %d should be true", i)
		}
	}
}
