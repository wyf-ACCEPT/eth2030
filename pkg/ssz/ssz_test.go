package ssz

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// --- Encoding tests ---

func TestMarshalBool(t *testing.T) {
	if got := MarshalBool(false); !bytes.Equal(got, []byte{0}) {
		t.Fatalf("MarshalBool(false) = %v, want [0]", got)
	}
	if got := MarshalBool(true); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("MarshalBool(true) = %v, want [1]", got)
	}
}

func TestMarshalUint8(t *testing.T) {
	if got := MarshalUint8(0); !bytes.Equal(got, []byte{0}) {
		t.Fatalf("MarshalUint8(0) = %v, want [0]", got)
	}
	if got := MarshalUint8(255); !bytes.Equal(got, []byte{255}) {
		t.Fatalf("MarshalUint8(255) = %v, want [255]", got)
	}
}

func TestMarshalUint16(t *testing.T) {
	if got := MarshalUint16(0x0102); !bytes.Equal(got, []byte{0x02, 0x01}) {
		t.Fatalf("MarshalUint16(0x0102) = %x, want [02 01]", got)
	}
}

func TestMarshalUint32(t *testing.T) {
	if got := MarshalUint32(1); !bytes.Equal(got, []byte{1, 0, 0, 0}) {
		t.Fatalf("MarshalUint32(1) = %v, want [1 0 0 0]", got)
	}
}

func TestMarshalUint64(t *testing.T) {
	// uint64(0) should encode as 8 zero bytes.
	if got := MarshalUint64(0); !bytes.Equal(got, make([]byte, 8)) {
		t.Fatalf("MarshalUint64(0) = %v, want 8 zero bytes", got)
	}
	// uint64(1) should encode as [1, 0, 0, 0, 0, 0, 0, 0].
	expected := []byte{1, 0, 0, 0, 0, 0, 0, 0}
	if got := MarshalUint64(1); !bytes.Equal(got, expected) {
		t.Fatalf("MarshalUint64(1) = %v, want %v", got, expected)
	}
}

func TestMarshalUint128(t *testing.T) {
	got := MarshalUint128(1, 0)
	expected := make([]byte, 16)
	expected[0] = 1
	if !bytes.Equal(got, expected) {
		t.Fatalf("MarshalUint128(1, 0) = %v, want %v", got, expected)
	}
}

func TestMarshalUint256(t *testing.T) {
	got := MarshalUint256([4]uint64{1, 0, 0, 0})
	expected := make([]byte, 32)
	expected[0] = 1
	if !bytes.Equal(got, expected) {
		t.Fatalf("MarshalUint256 mismatch")
	}
}

// --- Decoding tests ---

func TestUnmarshalBool(t *testing.T) {
	v, err := UnmarshalBool([]byte{0})
	if err != nil || v {
		t.Fatalf("UnmarshalBool(0) = %v, %v", v, err)
	}
	v, err = UnmarshalBool([]byte{1})
	if err != nil || !v {
		t.Fatalf("UnmarshalBool(1) = %v, %v", v, err)
	}
	_, err = UnmarshalBool([]byte{2})
	if err != ErrInvalidBool {
		t.Fatalf("UnmarshalBool(2) err = %v, want ErrInvalidBool", err)
	}
	_, err = UnmarshalBool([]byte{})
	if err != ErrSize {
		t.Fatalf("UnmarshalBool(empty) err = %v, want ErrSize", err)
	}
}

func TestUnmarshalUint64(t *testing.T) {
	v, err := UnmarshalUint64([]byte{1, 0, 0, 0, 0, 0, 0, 0})
	if err != nil || v != 1 {
		t.Fatalf("UnmarshalUint64 = %d, %v", v, err)
	}
	v, err = UnmarshalUint64(MarshalUint64(0xdeadbeef))
	if err != nil || v != 0xdeadbeef {
		t.Fatalf("roundtrip failed: got %x", v)
	}
}

func TestUnmarshalUint16(t *testing.T) {
	v, err := UnmarshalUint16(MarshalUint16(0x1234))
	if err != nil || v != 0x1234 {
		t.Fatalf("roundtrip uint16 failed: got %x", v)
	}
}

func TestUnmarshalUint32(t *testing.T) {
	v, err := UnmarshalUint32(MarshalUint32(0xaabbccdd))
	if err != nil || v != 0xaabbccdd {
		t.Fatalf("roundtrip uint32 failed: got %x", v)
	}
}

func TestUnmarshalUint128(t *testing.T) {
	lo, hi, err := UnmarshalUint128(MarshalUint128(42, 99))
	if err != nil || lo != 42 || hi != 99 {
		t.Fatalf("roundtrip uint128 failed: lo=%d hi=%d err=%v", lo, hi, err)
	}
}

func TestUnmarshalUint256(t *testing.T) {
	limbs := [4]uint64{1, 2, 3, 4}
	got, err := UnmarshalUint256(MarshalUint256(limbs))
	if err != nil || got != limbs {
		t.Fatalf("roundtrip uint256 failed: got %v err=%v", got, err)
	}
}

// --- Roundtrip tests for vectors/lists ---

func TestVectorRoundtrip(t *testing.T) {
	elems := [][]byte{
		MarshalUint64(100),
		MarshalUint64(200),
		MarshalUint64(300),
	}
	encoded := MarshalVector(elems)
	decoded, err := UnmarshalVector(encoded, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range decoded {
		v, _ := UnmarshalUint64(d)
		expected := uint64((i + 1) * 100)
		if v != expected {
			t.Fatalf("element %d: got %d, want %d", i, v, expected)
		}
	}
}

func TestListRoundtrip(t *testing.T) {
	elems := [][]byte{
		MarshalUint32(10),
		MarshalUint32(20),
	}
	encoded := MarshalList(elems)
	decoded, err := UnmarshalList(encoded, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 {
		t.Fatalf("list length = %d, want 2", len(decoded))
	}
}

// --- Variable container tests ---

func TestVariableContainerRoundtrip(t *testing.T) {
	// Container with: uint32 (fixed), bytes (variable), uint32 (fixed).
	fixedField0 := MarshalUint32(42)
	variableField := []byte("hello ssz")
	fixedField2 := MarshalUint32(99)

	fixedParts := [][]byte{fixedField0, nil, fixedField2}
	variableParts := [][]byte{variableField}
	variableIndices := []int{1}

	encoded := MarshalVariableContainer(fixedParts, variableParts, variableIndices)

	// Decode.
	fixedSizes := []int{4, 0, 4} // 0 = variable
	fields, err := UnmarshalVariableContainer(encoded, 3, fixedSizes)
	if err != nil {
		t.Fatal(err)
	}

	v0, _ := UnmarshalUint32(fields[0])
	if v0 != 42 {
		t.Fatalf("field 0 = %d, want 42", v0)
	}
	if !bytes.Equal(fields[1], variableField) {
		t.Fatalf("field 1 = %q, want %q", fields[1], variableField)
	}
	v2, _ := UnmarshalUint32(fields[2])
	if v2 != 99 {
		t.Fatalf("field 2 = %d, want 99", v2)
	}
}

// --- Bitfield tests ---

func TestBitvectorRoundtrip(t *testing.T) {
	bits := []bool{true, false, true, true, false, false, true, false, true}
	encoded := MarshalBitvector(bits)
	decoded, err := UnmarshalBitvector(encoded, 9)
	if err != nil {
		t.Fatal(err)
	}
	for i := range bits {
		if bits[i] != decoded[i] {
			t.Fatalf("bit %d: got %v, want %v", i, decoded[i], bits[i])
		}
	}
}

func TestBitlistRoundtrip(t *testing.T) {
	bits := []bool{true, false, true, false, true}
	encoded := MarshalBitlist(bits)
	decoded, err := UnmarshalBitlist(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != len(bits) {
		t.Fatalf("bitlist length = %d, want %d", len(decoded), len(bits))
	}
	for i := range bits {
		if bits[i] != decoded[i] {
			t.Fatalf("bit %d: got %v, want %v", i, decoded[i], bits[i])
		}
	}
}

func TestBitlistEmpty(t *testing.T) {
	// Empty bitlist: just the sentinel bit.
	encoded := MarshalBitlist([]bool{})
	decoded, err := UnmarshalBitlist(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 0 {
		t.Fatalf("empty bitlist decoded length = %d, want 0", len(decoded))
	}
}

// --- Merkleization tests ---

func TestHashTreeRootBasicTypes(t *testing.T) {
	// bool(false) -> 32 zero bytes.
	root := HashTreeRootBool(false)
	if root != [32]byte{} {
		t.Fatalf("hash_tree_root(false) should be zero chunk")
	}

	// bool(true) -> [1, 0, 0, ..., 0].
	root = HashTreeRootBool(true)
	var expected [32]byte
	expected[0] = 1
	if root != expected {
		t.Fatalf("hash_tree_root(true) mismatch")
	}

	// uint8(0) -> 32 zero bytes.
	root = HashTreeRootUint8(0)
	if root != [32]byte{} {
		t.Fatalf("hash_tree_root(uint8(0)) should be zero chunk")
	}

	// uint64(1) -> [1, 0, 0, 0, 0, 0, 0, 0, 0, ..., 0].
	root = HashTreeRootUint64(1)
	expected = [32]byte{}
	expected[0] = 1
	if root != expected {
		t.Fatalf("hash_tree_root(uint64(1)) mismatch")
	}
}

func TestHashTreeRootUint64Values(t *testing.T) {
	// For a basic type that fits in one chunk, the hash_tree_root is the
	// value itself zero-padded to 32 bytes.
	root := HashTreeRootUint64(0xdeadbeef)
	var expected [32]byte
	binary.LittleEndian.PutUint64(expected[:8], 0xdeadbeef)
	if root != expected {
		t.Fatalf("hash_tree_root(uint64(deadbeef)) = %x, want %x", root, expected)
	}
}

func TestMerkleizeOneChunk(t *testing.T) {
	var chunk [32]byte
	chunk[0] = 0xab
	root := Merkleize([][32]byte{chunk}, 0)
	if root != chunk {
		t.Fatalf("Merkleize of single chunk should return the chunk itself")
	}
}

func TestMerkleizeTwoChunks(t *testing.T) {
	var a, b [32]byte
	a[0] = 1
	b[0] = 2
	root := Merkleize([][32]byte{a, b}, 0)
	expected := sha256Sum(append(a[:], b[:]...))
	if root != expected {
		t.Fatalf("Merkleize of two chunks mismatch")
	}
}

func TestMerkleizePaddedToLimit(t *testing.T) {
	// One chunk with limit=4 should pad with 3 zero chunks and produce a
	// 4-leaf tree.
	var chunk [32]byte
	chunk[0] = 0xff
	root := Merkleize([][32]byte{chunk}, 4)

	// Build the expected tree manually.
	z := [32]byte{}
	left := sha256Sum(append(chunk[:], z[:]...))
	right := sha256Sum(append(z[:], z[:]...))
	expected := sha256Sum(append(left[:], right[:]...))
	if root != expected {
		t.Fatalf("Merkleize with limit=4 mismatch")
	}
}

func TestMixInLength(t *testing.T) {
	var root [32]byte
	root[0] = 0xaa
	result := MixInLength(root, 5)

	var lenChunk [32]byte
	binary.LittleEndian.PutUint64(lenChunk[:8], 5)
	expected := sha256Sum(append(root[:], lenChunk[:]...))
	if result != expected {
		t.Fatalf("MixInLength mismatch")
	}
}

func TestHashTreeRootContainer(t *testing.T) {
	// Container { a: uint64, b: uint64 }
	rootA := HashTreeRootUint64(10)
	rootB := HashTreeRootUint64(20)
	containerRoot := HashTreeRootContainer([][32]byte{rootA, rootB})

	// Should be hash(rootA, rootB).
	expected := sha256Sum(append(rootA[:], rootB[:]...))
	if containerRoot != expected {
		t.Fatalf("container hash tree root mismatch")
	}
}

func TestHashTreeRootVector(t *testing.T) {
	// Vector[uint64, 2] with values [10, 20].
	rootA := HashTreeRootUint64(10)
	rootB := HashTreeRootUint64(20)
	vecRoot := HashTreeRootVector([][32]byte{rootA, rootB})

	expected := sha256Sum(append(rootA[:], rootB[:]...))
	if vecRoot != expected {
		t.Fatalf("vector hash tree root mismatch")
	}
}

func TestHashTreeRootList(t *testing.T) {
	// List[uint64, 4] with values [10, 20].
	rootA := HashTreeRootUint64(10)
	rootB := HashTreeRootUint64(20)
	listRoot := HashTreeRootList([][32]byte{rootA, rootB}, 4)

	// Merkleize with limit=4, then mix_in_length(2).
	merkleRoot := Merkleize([][32]byte{rootA, rootB}, 4)
	expected := MixInLength(merkleRoot, 2)
	if listRoot != expected {
		t.Fatalf("list hash tree root mismatch")
	}
}

func TestHashTreeRootByteList(t *testing.T) {
	data := []byte("hello")
	root := HashTreeRootByteList(data, 32)
	if root == [32]byte{} {
		t.Fatal("byte list root should not be zero")
	}
}

func TestHashTreeRootBitvector(t *testing.T) {
	bits := []bool{true, false, true, true, false, false, true, false}
	root := HashTreeRootBitvector(bits)
	// The packed byte is 0b01001101 = 0x4d, padded to 32 bytes.
	var expected [32]byte
	expected[0] = 0x4d
	if root != expected {
		t.Fatalf("bitvector root = %x, want %x", root, expected)
	}
}

func TestHashTreeRootBitlist(t *testing.T) {
	bits := []bool{true, false, true}
	root := HashTreeRootBitlist(bits, 8)
	if root == [32]byte{} {
		t.Fatal("bitlist root should not be zero")
	}
}

func TestPackEmpty(t *testing.T) {
	chunks := Pack(nil)
	if len(chunks) != 1 {
		t.Fatalf("Pack(nil) should return 1 zero chunk, got %d", len(chunks))
	}
	if chunks[0] != [32]byte{} {
		t.Fatal("Pack(nil) chunk should be zero")
	}
}

func TestPackExact(t *testing.T) {
	data := make([]byte, 64)
	data[0] = 1
	data[32] = 2
	chunks := Pack(data)
	if len(chunks) != 2 {
		t.Fatalf("Pack(64 bytes) should return 2 chunks, got %d", len(chunks))
	}
	if chunks[0][0] != 1 || chunks[1][0] != 2 {
		t.Fatal("Pack data mismatch")
	}
}

func TestHashTreeRootBasicVector(t *testing.T) {
	// Vector[uint64, 4] packed: 4 uint64 values = 32 bytes = exactly 1 chunk.
	data := make([]byte, 0, 32)
	data = append(data, MarshalUint64(1)...)
	data = append(data, MarshalUint64(2)...)
	data = append(data, MarshalUint64(3)...)
	data = append(data, MarshalUint64(4)...)
	root := HashTreeRootBasicVector(data)

	// Should equal the single packed chunk.
	var expected [32]byte
	copy(expected[:], data)
	if root != expected {
		t.Fatalf("basic vector root mismatch")
	}
}

func TestHashTreeRootBasicList(t *testing.T) {
	// List[uint64, 8] with 2 elements.
	data := make([]byte, 0, 16)
	data = append(data, MarshalUint64(100)...)
	data = append(data, MarshalUint64(200)...)
	root := HashTreeRootBasicList(data, 2, 8, 8)
	if root == [32]byte{} {
		t.Fatal("basic list root should not be zero")
	}
}

// --- Edge case tests ---

func TestUnmarshalSizeErrors(t *testing.T) {
	_, err := UnmarshalUint64([]byte{1, 2, 3})
	if err != ErrSize {
		t.Fatalf("expected ErrSize, got %v", err)
	}
	_, err = UnmarshalUint32([]byte{1})
	if err != ErrSize {
		t.Fatalf("expected ErrSize, got %v", err)
	}
	_, err = UnmarshalUint16([]byte{})
	if err != ErrSize {
		t.Fatalf("expected ErrSize, got %v", err)
	}
	_, err = UnmarshalUint8([]byte{})
	if err != ErrSize {
		t.Fatalf("expected ErrSize, got %v", err)
	}
	_, _, err = UnmarshalUint128([]byte{1, 2, 3})
	if err != ErrSize {
		t.Fatalf("expected ErrSize, got %v", err)
	}
	_, err = UnmarshalUint256([]byte{1, 2, 3})
	if err != ErrSize {
		t.Fatalf("expected ErrSize, got %v", err)
	}
}

func TestVariableContainerMultipleVariable(t *testing.T) {
	// Container: uint32 (fixed), bytes (variable), bytes (variable).
	f0 := MarshalUint32(1)
	v0 := []byte("foo")
	v1 := []byte("barbaz")

	fixedParts := [][]byte{f0, nil, nil}
	variableParts := [][]byte{v0, v1}
	variableIndices := []int{1, 2}

	encoded := MarshalVariableContainer(fixedParts, variableParts, variableIndices)

	fixedSizes := []int{4, 0, 0}
	fields, err := UnmarshalVariableContainer(encoded, 3, fixedSizes)
	if err != nil {
		t.Fatal(err)
	}

	val, _ := UnmarshalUint32(fields[0])
	if val != 1 {
		t.Fatalf("field 0 = %d, want 1", val)
	}
	if !bytes.Equal(fields[1], v0) {
		t.Fatalf("field 1 = %q, want %q", fields[1], v0)
	}
	if !bytes.Equal(fields[2], v1) {
		t.Fatalf("field 2 = %q, want %q", fields[2], v1)
	}
}

// helper
func sha256Sum(data []byte) [32]byte {
	return sha256.Sum256(data)
}
