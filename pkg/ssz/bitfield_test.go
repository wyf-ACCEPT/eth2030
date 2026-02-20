package ssz

import (
	"testing"
)

// --- Bitlist creation tests ---

func TestNewBitlist(t *testing.T) {
	bl, err := NewBitlist(10)
	if err != nil {
		t.Fatalf("NewBitlist: %v", err)
	}
	if bl.Len() != 10 {
		t.Errorf("expected length 10, got %d", bl.Len())
	}
	if bl.Count() != 0 {
		t.Errorf("expected 0 set bits, got %d", bl.Count())
	}
}

func TestNewBitlistZero(t *testing.T) {
	_, err := NewBitlist(0)
	if err != ErrBitlistZeroLength {
		t.Errorf("expected ErrBitlistZeroLength, got %v", err)
	}
}

func TestNewBitlistNegative(t *testing.T) {
	_, err := NewBitlist(-5)
	if err != ErrBitlistZeroLength {
		t.Errorf("expected ErrBitlistZeroLength, got %v", err)
	}
}

// --- Bitlist Set/Get tests ---

func TestBitlistSetGet(t *testing.T) {
	bl, _ := NewBitlist(16)

	bl.Set(0)
	bl.Set(5)
	bl.Set(15)

	if !bl.Get(0) {
		t.Error("bit 0 should be set")
	}
	if !bl.Get(5) {
		t.Error("bit 5 should be set")
	}
	if !bl.Get(15) {
		t.Error("bit 15 should be set")
	}
	if bl.Get(1) {
		t.Error("bit 1 should not be set")
	}
	if bl.Get(14) {
		t.Error("bit 14 should not be set")
	}
}

func TestBitlistSetOutOfBounds(t *testing.T) {
	bl, _ := NewBitlist(8)
	bl.Set(100) // should silently ignore
	bl.Set(-1)  // should silently ignore
	if bl.Count() != 0 {
		t.Errorf("no bits should be set, got %d", bl.Count())
	}
}

func TestBitlistGetOutOfBounds(t *testing.T) {
	bl, _ := NewBitlist(8)
	if bl.Get(100) {
		t.Error("out of bounds Get should return false")
	}
	if bl.Get(-1) {
		t.Error("negative Get should return false")
	}
}

func TestBitlistClear(t *testing.T) {
	bl, _ := NewBitlist(8)
	bl.Set(3)
	if !bl.Get(3) {
		t.Fatal("bit 3 should be set")
	}
	bl.Clear(3)
	if bl.Get(3) {
		t.Error("bit 3 should be cleared")
	}
}

// --- Bitlist Count tests ---

func TestBitlistCount(t *testing.T) {
	bl, _ := NewBitlist(32)
	bl.Set(0)
	bl.Set(7)
	bl.Set(15)
	bl.Set(31)

	if bl.Count() != 4 {
		t.Errorf("expected 4, got %d", bl.Count())
	}
}

func TestBitlistCountAll(t *testing.T) {
	bl, _ := NewBitlist(8)
	for i := 0; i < 8; i++ {
		bl.Set(i)
	}
	if bl.Count() != 8 {
		t.Errorf("expected 8, got %d", bl.Count())
	}
}

// --- Bitlist OR tests ---

func TestBitlistOR(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(8)

	a.Set(0)
	a.Set(2)
	b.Set(1)
	b.Set(2)

	result, err := a.OR(b)
	if err != nil {
		t.Fatalf("OR: %v", err)
	}

	if !result.Get(0) || !result.Get(1) || !result.Get(2) {
		t.Error("bits 0, 1, 2 should be set in OR result")
	}
	if result.Get(3) {
		t.Error("bit 3 should not be set")
	}
	if result.Count() != 3 {
		t.Errorf("expected 3 set bits, got %d", result.Count())
	}
}

func TestBitlistORLengthMismatch(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(16)
	_, err := a.OR(b)
	if err != ErrBitlistLengthMismatch {
		t.Errorf("expected ErrBitlistLengthMismatch, got %v", err)
	}
}

// --- Bitlist AND tests ---

func TestBitlistAND(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(8)

	a.Set(0)
	a.Set(2)
	a.Set(4)
	b.Set(2)
	b.Set(4)
	b.Set(6)

	result, err := a.AND(b)
	if err != nil {
		t.Fatalf("AND: %v", err)
	}
	if result.Count() != 2 {
		t.Errorf("expected 2 set bits, got %d", result.Count())
	}
	if !result.Get(2) || !result.Get(4) {
		t.Error("bits 2 and 4 should be set")
	}
	if result.Get(0) || result.Get(6) {
		t.Error("bits 0 and 6 should not be set")
	}
}

// --- Bitlist Overlaps tests ---

func TestBitlistOverlaps(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(8)

	a.Set(0)
	a.Set(2)
	b.Set(1)
	b.Set(3)

	if a.Overlaps(b) {
		t.Error("no overlap expected")
	}

	b.Set(2)
	if !a.Overlaps(b) {
		t.Error("overlap expected at bit 2")
	}
}

func TestBitlistOverlapsLengthMismatch(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(16)
	if a.Overlaps(b) {
		t.Error("different lengths should not overlap")
	}
}

// --- Bitlist IsZero tests ---

func TestBitlistIsZero(t *testing.T) {
	bl, _ := NewBitlist(8)
	if !bl.IsZero() {
		t.Error("new bitlist should be zero")
	}
	bl.Set(3)
	if bl.IsZero() {
		t.Error("bitlist with set bit should not be zero")
	}
}

// --- Bitlist serialization ---

func TestBitlistFromBytes(t *testing.T) {
	bl, _ := NewBitlist(5)
	bl.Set(0)
	bl.Set(3)

	data := bl.Bytes()
	restored, err := BitlistFromBytes(data)
	if err != nil {
		t.Fatalf("BitlistFromBytes: %v", err)
	}
	if restored.Len() != bl.Len() {
		t.Errorf("length mismatch: got %d, want %d", restored.Len(), bl.Len())
	}
	if !BitlistEqual(bl, restored) {
		t.Error("restored bitlist should equal original")
	}
}

func TestBitlistFromBytesEmpty(t *testing.T) {
	_, err := BitlistFromBytes(nil)
	if err == nil {
		t.Error("expected error for nil data")
	}
}

func TestBitlistFromBytesNoSentinel(t *testing.T) {
	_, err := BitlistFromBytes([]byte{0x00})
	if err == nil {
		t.Error("expected error for no sentinel")
	}
}

// --- Bitlist Marshal/Unmarshal ---

func TestBitlistMarshalUnmarshal(t *testing.T) {
	bl, _ := NewBitlist(10)
	bl.Set(0)
	bl.Set(5)
	bl.Set(9)

	data := BitlistMarshalSSZ(bl)
	restored, err := BitlistUnmarshalSSZ(data)
	if err != nil {
		t.Fatalf("BitlistUnmarshalSSZ: %v", err)
	}
	if !BitlistEqual(bl, restored) {
		t.Error("marshal/unmarshal roundtrip failed")
	}
}

// --- Bitvector creation tests ---

func TestNewBitvector(t *testing.T) {
	bv, err := NewBitvector(16)
	if err != nil {
		t.Fatalf("NewBitvector: %v", err)
	}
	if bv.Len() != 16 {
		t.Errorf("expected length 16, got %d", bv.Len())
	}
}

func TestNewBitvectorZero(t *testing.T) {
	_, err := NewBitvector(0)
	if err != ErrBitvectorZeroLength {
		t.Errorf("expected ErrBitvectorZeroLength, got %v", err)
	}
}

// --- Bitvector Set/Get tests ---

func TestBitvectorSetGet(t *testing.T) {
	bv, _ := NewBitvector(32)
	bv.Set(0)
	bv.Set(15)
	bv.Set(31)

	if !bv.Get(0) {
		t.Error("bit 0 should be set")
	}
	if !bv.Get(15) {
		t.Error("bit 15 should be set")
	}
	if !bv.Get(31) {
		t.Error("bit 31 should be set")
	}
	if bv.Get(1) {
		t.Error("bit 1 should not be set")
	}
}

func TestBitvectorClear(t *testing.T) {
	bv, _ := NewBitvector(8)
	bv.Set(5)
	bv.Clear(5)
	if bv.Get(5) {
		t.Error("bit 5 should be cleared")
	}
}

// --- Bitvector Count tests ---

func TestBitvectorCount(t *testing.T) {
	bv, _ := NewBitvector(16)
	bv.Set(0)
	bv.Set(4)
	bv.Set(8)
	bv.Set(12)

	if bv.Count() != 4 {
		t.Errorf("expected 4, got %d", bv.Count())
	}
}

// --- Bitvector OR tests ---

func TestBitvectorOR(t *testing.T) {
	a, _ := NewBitvector(8)
	b, _ := NewBitvector(8)

	a.Set(0)
	a.Set(2)
	b.Set(1)
	b.Set(2)

	result, err := a.OR(b)
	if err != nil {
		t.Fatalf("OR: %v", err)
	}
	if result.Count() != 3 {
		t.Errorf("expected 3, got %d", result.Count())
	}
}

func TestBitvectorORLengthMismatch(t *testing.T) {
	a, _ := NewBitvector(8)
	b, _ := NewBitvector(16)
	_, err := a.OR(b)
	if err != ErrBitvectorLengthMismatch {
		t.Errorf("expected ErrBitvectorLengthMismatch, got %v", err)
	}
}

// --- Bitvector AND tests ---

func TestBitvectorAND(t *testing.T) {
	a, _ := NewBitvector(8)
	b, _ := NewBitvector(8)

	a.Set(1)
	a.Set(3)
	b.Set(3)
	b.Set(5)

	result, err := a.AND(b)
	if err != nil {
		t.Fatalf("AND: %v", err)
	}
	if result.Count() != 1 {
		t.Errorf("expected 1, got %d", result.Count())
	}
	if !result.Get(3) {
		t.Error("bit 3 should be set")
	}
}

// --- Bitvector Overlaps tests ---

func TestBitvectorOverlaps(t *testing.T) {
	a, _ := NewBitvector(8)
	b, _ := NewBitvector(8)

	a.Set(0)
	b.Set(1)
	if a.Overlaps(b) {
		t.Error("no overlap expected")
	}

	b.Set(0)
	if !a.Overlaps(b) {
		t.Error("overlap expected")
	}
}

// --- Bitvector IsZero ---

func TestBitvectorIsZero(t *testing.T) {
	bv, _ := NewBitvector(8)
	if !bv.IsZero() {
		t.Error("new bitvector should be zero")
	}
	bv.Set(0)
	if bv.IsZero() {
		t.Error("bitvector with set bit should not be zero")
	}
}

// --- Bitvector serialization ---

func TestBitvectorFromBytes(t *testing.T) {
	bv, _ := NewBitvector(10)
	bv.Set(0)
	bv.Set(9)

	data := bv.Bytes()
	restored, err := BitvectorFromBytes(data, 10)
	if err != nil {
		t.Fatalf("BitvectorFromBytes: %v", err)
	}
	if !BitvectorEqual(bv, restored) {
		t.Error("restored bitvector should equal original")
	}
}

func TestBitvectorMarshalUnmarshal(t *testing.T) {
	bv, _ := NewBitvector(12)
	bv.Set(0)
	bv.Set(5)
	bv.Set(11)

	data := BitvectorMarshalSSZ(bv)
	restored, err := BitvectorUnmarshalSSZ(data, 12)
	if err != nil {
		t.Fatalf("BitvectorUnmarshalSSZ: %v", err)
	}
	if !BitvectorEqual(bv, restored) {
		t.Error("marshal/unmarshal roundtrip failed")
	}
}

// --- Hash tree root tests ---

func TestBitlistHashTreeRoot(t *testing.T) {
	bl, _ := NewBitlist(8)
	bl.Set(0)
	bl.Set(3)
	bl.Set(7)

	root := BitlistHashTreeRoot(bl, 16)
	if root == ([32]byte{}) {
		t.Error("bitlist hash tree root should be non-zero")
	}

	// Same bitlist should produce same root.
	root2 := BitlistHashTreeRoot(bl, 16)
	if root != root2 {
		t.Error("hash tree root should be deterministic")
	}
}

func TestBitlistHashTreeRootDifferentBits(t *testing.T) {
	a, _ := NewBitlist(8)
	a.Set(0)

	b, _ := NewBitlist(8)
	b.Set(1)

	rootA := BitlistHashTreeRoot(a, 16)
	rootB := BitlistHashTreeRoot(b, 16)
	if rootA == rootB {
		t.Error("different bitlists should have different roots")
	}
}

func TestBitvectorHashTreeRoot(t *testing.T) {
	bv, _ := NewBitvector(16)
	bv.Set(0)
	bv.Set(8)
	bv.Set(15)

	root := BitvectorHashTreeRoot(bv)
	if root == ([32]byte{}) {
		t.Error("bitvector hash tree root should be non-zero")
	}

	root2 := BitvectorHashTreeRoot(bv)
	if root != root2 {
		t.Error("hash tree root should be deterministic")
	}
}

func TestBitvectorHashTreeRootDifferentBits(t *testing.T) {
	a, _ := NewBitvector(8)
	a.Set(0)

	b, _ := NewBitvector(8)
	b.Set(7)

	rootA := BitvectorHashTreeRoot(a)
	rootB := BitvectorHashTreeRoot(b)
	if rootA == rootB {
		t.Error("different bitvectors should have different roots")
	}
}

// --- ChunkCount tests ---

func TestChunkCount(t *testing.T) {
	tests := []struct {
		bitLen int
		want   int
	}{
		{0, 1},
		{1, 1},
		{256, 1},
		{257, 2},
		{512, 2},
		{513, 3},
		{1024, 4},
	}
	for _, tt := range tests {
		got := ChunkCount(tt.bitLen)
		if got != tt.want {
			t.Errorf("ChunkCount(%d) = %d, want %d", tt.bitLen, got, tt.want)
		}
	}
}

// --- Equality tests ---

func TestBitlistEqual(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(8)

	a.Set(3)
	b.Set(3)

	if !BitlistEqual(a, b) {
		t.Error("same bitlists should be equal")
	}

	b.Set(5)
	if BitlistEqual(a, b) {
		t.Error("different bitlists should not be equal")
	}
}

func TestBitlistEqualDifferentLength(t *testing.T) {
	a, _ := NewBitlist(8)
	b, _ := NewBitlist(16)
	if BitlistEqual(a, b) {
		t.Error("different lengths should not be equal")
	}
}

func TestBitvectorEqual(t *testing.T) {
	a, _ := NewBitvector(8)
	b, _ := NewBitvector(8)

	a.Set(7)
	b.Set(7)

	if !BitvectorEqual(a, b) {
		t.Error("same bitvectors should be equal")
	}

	a.Set(0)
	if BitvectorEqual(a, b) {
		t.Error("different bitvectors should not be equal")
	}
}

// --- Non-byte-aligned bitlist tests ---

func TestBitlistNonByteAligned(t *testing.T) {
	// 5 bits: not aligned to byte boundary.
	bl, _ := NewBitlist(5)
	bl.Set(0)
	bl.Set(4)

	if bl.Count() != 2 {
		t.Errorf("expected 2 set bits, got %d", bl.Count())
	}

	data := bl.Bytes()
	restored, err := BitlistFromBytes(data)
	if err != nil {
		t.Fatalf("BitlistFromBytes: %v", err)
	}
	if restored.Len() != 5 {
		t.Errorf("expected length 5, got %d", restored.Len())
	}
	if !BitlistEqual(bl, restored) {
		t.Error("non-byte-aligned roundtrip failed")
	}
}
