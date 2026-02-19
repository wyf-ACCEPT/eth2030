package ssz

import (
	"encoding/binary"
	"testing"
)

// FuzzSSZDecode feeds random bytes to SSZ decode functions.
// They must never panic on arbitrary input.
func FuzzSSZDecode(f *testing.F) {
	// Seed: valid SSZ-encoded bool (true).
	f.Add([]byte{1})
	// Seed: valid SSZ-encoded bool (false).
	f.Add([]byte{0})
	// Seed: valid SSZ-encoded uint8.
	f.Add([]byte{42})
	// Seed: valid SSZ-encoded uint16 (little-endian 0x0102 = 258).
	f.Add([]byte{0x02, 0x01})
	// Seed: valid SSZ-encoded uint32 (1).
	f.Add([]byte{1, 0, 0, 0})
	// Seed: valid SSZ-encoded uint64 (256).
	f.Add([]byte{0, 1, 0, 0, 0, 0, 0, 0})
	// Seed: valid SSZ-encoded uint128 (16 bytes).
	f.Add(make([]byte, 16))
	// Seed: valid SSZ-encoded uint256 (32 bytes).
	f.Add(make([]byte, 32))
	// Seed: empty byte slice.
	f.Add([]byte{})
	// Seed: invalid bool value.
	f.Add([]byte{0x02})
	// Seed: short byte array.
	f.Add([]byte{0xde, 0xad, 0xbe, 0xef})
	// Seed: bitlist with sentinel bit.
	f.Add([]byte{0x83}) // sentinel bit at position 7, data bits: 0,1 (first 2 bits)

	f.Fuzz(func(t *testing.T, data []byte) {
		// UnmarshalBool: must not panic.
		_, _ = UnmarshalBool(data)

		// UnmarshalUint8: must not panic.
		_, _ = UnmarshalUint8(data)

		// UnmarshalUint16: must not panic.
		_, _ = UnmarshalUint16(data)

		// UnmarshalUint32: must not panic.
		_, _ = UnmarshalUint32(data)

		// UnmarshalUint64: must not panic.
		_, _ = UnmarshalUint64(data)

		// UnmarshalUint128: must not panic.
		_, _, _ = UnmarshalUint128(data)

		// UnmarshalUint256: must not panic.
		_, _ = UnmarshalUint256(data)

		// UnmarshalList with various element sizes: must not panic.
		for _, elemSize := range []int{1, 2, 4, 8, 32} {
			_, _ = UnmarshalList(data, elemSize)
		}

		// UnmarshalBitvector: try several bit counts.
		for _, n := range []int{0, 1, 8, 16, 64} {
			_, _ = UnmarshalBitvector(data, n)
		}

		// UnmarshalBitlist: must not panic.
		_, _ = UnmarshalBitlist(data)

		// UnmarshalVariableContainer: try simple configs, must not panic.
		if len(data) >= 12 {
			// Container with 2 fields: 1 fixed (4 bytes) + 1 variable.
			_, _ = UnmarshalVariableContainer(data, 2, []int{4, 0})
		}
	})
}

// FuzzSSZRoundtrip encodes basic SSZ types, decodes them back, and verifies
// equality. This tests the encode-decode cycle for correctness.
func FuzzSSZRoundtrip(f *testing.F) {
	// Seeds as uint64 values that also serve as source material.
	f.Add(uint64(0), true, []byte{})
	f.Add(uint64(1), false, []byte{0xca, 0xfe})
	f.Add(uint64(0xdeadbeefcafebabe), true, []byte{1, 2, 3, 4, 5})
	f.Add(uint64(0xffffffffffffffff), false, []byte{0xff})
	f.Add(uint64(42), true, []byte{0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, val uint64, boolVal bool, byteData []byte) {
		// Bool roundtrip.
		boolEnc := MarshalBool(boolVal)
		boolDec, err := UnmarshalBool(boolEnc)
		if err != nil {
			t.Fatalf("UnmarshalBool failed: %v", err)
		}
		if boolDec != boolVal {
			t.Fatalf("Bool roundtrip: got %v, want %v", boolDec, boolVal)
		}

		// Uint8 roundtrip.
		u8 := uint8(val & 0xff)
		u8Enc := MarshalUint8(u8)
		u8Dec, err := UnmarshalUint8(u8Enc)
		if err != nil {
			t.Fatalf("UnmarshalUint8 failed: %v", err)
		}
		if u8Dec != u8 {
			t.Fatalf("Uint8 roundtrip: got %d, want %d", u8Dec, u8)
		}

		// Uint16 roundtrip.
		u16 := uint16(val & 0xffff)
		u16Enc := MarshalUint16(u16)
		u16Dec, err := UnmarshalUint16(u16Enc)
		if err != nil {
			t.Fatalf("UnmarshalUint16 failed: %v", err)
		}
		if u16Dec != u16 {
			t.Fatalf("Uint16 roundtrip: got %d, want %d", u16Dec, u16)
		}

		// Uint32 roundtrip.
		u32 := uint32(val & 0xffffffff)
		u32Enc := MarshalUint32(u32)
		u32Dec, err := UnmarshalUint32(u32Enc)
		if err != nil {
			t.Fatalf("UnmarshalUint32 failed: %v", err)
		}
		if u32Dec != u32 {
			t.Fatalf("Uint32 roundtrip: got %d, want %d", u32Dec, u32)
		}

		// Uint64 roundtrip.
		u64Enc := MarshalUint64(val)
		u64Dec, err := UnmarshalUint64(u64Enc)
		if err != nil {
			t.Fatalf("UnmarshalUint64 failed: %v", err)
		}
		if u64Dec != val {
			t.Fatalf("Uint64 roundtrip: got %d, want %d", u64Dec, val)
		}

		// Uint128 roundtrip using val as lo and ^val as hi.
		hi := ^val
		u128Enc := MarshalUint128(val, hi)
		lo128, hi128, err := UnmarshalUint128(u128Enc)
		if err != nil {
			t.Fatalf("UnmarshalUint128 failed: %v", err)
		}
		if lo128 != val || hi128 != hi {
			t.Fatalf("Uint128 roundtrip: got (%d,%d), want (%d,%d)", lo128, hi128, val, hi)
		}

		// Uint256 roundtrip.
		limbs := [4]uint64{val, ^val, val >> 1, ^val >> 1}
		u256Enc := MarshalUint256(limbs)
		u256Dec, err := UnmarshalUint256(u256Enc)
		if err != nil {
			t.Fatalf("UnmarshalUint256 failed: %v", err)
		}
		if u256Dec != limbs {
			t.Fatalf("Uint256 roundtrip: got %v, want %v", u256Dec, limbs)
		}

		// Vector roundtrip: encode byteData as 1-byte elements, decode back.
		if len(byteData) > 0 {
			elements := make([][]byte, len(byteData))
			for i, b := range byteData {
				elements[i] = []byte{b}
			}
			vecEnc := MarshalVector(elements)
			vecDec, err := UnmarshalVector(vecEnc, len(byteData), 1)
			if err != nil {
				t.Fatalf("UnmarshalVector failed: %v", err)
			}
			for i := range vecDec {
				if len(vecDec[i]) != 1 || vecDec[i][0] != byteData[i] {
					t.Fatalf("Vector roundtrip mismatch at index %d", i)
				}
			}
		}

		// Bitvector roundtrip: convert byteData to bits.
		if len(byteData) > 0 && len(byteData) <= 256 {
			bits := make([]bool, len(byteData))
			for i, b := range byteData {
				bits[i] = b > 127
			}
			bvEnc := MarshalBitvector(bits)
			bvDec, err := UnmarshalBitvector(bvEnc, len(bits))
			if err != nil {
				t.Fatalf("UnmarshalBitvector failed: %v", err)
			}
			for i := range bits {
				if bvDec[i] != bits[i] {
					t.Fatalf("Bitvector roundtrip mismatch at bit %d", i)
				}
			}
		}

		// Bitlist roundtrip.
		if len(byteData) <= 256 {
			bits := make([]bool, len(byteData))
			for i, b := range byteData {
				bits[i] = b > 127
			}
			blEnc := MarshalBitlist(bits)
			blDec, err := UnmarshalBitlist(blEnc)
			if err != nil {
				t.Fatalf("UnmarshalBitlist failed: %v", err)
			}
			if len(blDec) != len(bits) {
				t.Fatalf("Bitlist roundtrip length: got %d, want %d", len(blDec), len(bits))
			}
			for i := range bits {
				if blDec[i] != bits[i] {
					t.Fatalf("Bitlist roundtrip mismatch at bit %d", i)
				}
			}
		}
	})
}

// FuzzSSZMerkleize feeds random leaf data to Merkleize and hash tree root
// functions. They must never panic and must always produce a 32-byte result.
func FuzzSSZMerkleize(f *testing.F) {
	// Seed: single zero chunk.
	f.Add([]byte{})
	// Seed: exactly 32 bytes (one chunk).
	f.Add(make([]byte, 32))
	// Seed: 64 bytes (two chunks).
	f.Add(make([]byte, 64))
	// Seed: non-aligned data (33 bytes, needs padding).
	f.Add(make([]byte, 33))
	// Seed: short data.
	f.Add([]byte{0xca, 0xfe, 0xba, 0xbe})
	// Seed: SSZ-encoded uint64.
	seed := make([]byte, 8)
	binary.LittleEndian.PutUint64(seed, 0xdeadbeef)
	f.Add(seed)

	f.Fuzz(func(t *testing.T, data []byte) {
		// Limit data size to avoid excessive memory allocation in the
		// Merkle tree builder when the fuzzer produces very large inputs.
		if len(data) > 8192 {
			return
		}

		// Pack + Merkleize: must not panic and must produce 32 bytes.
		chunks := Pack(data)
		root := Merkleize(chunks, 0)
		if len(root) != 32 {
			t.Fatalf("Merkleize root length: got %d, want 32", len(root))
		}

		// MixInLength: must not panic.
		mixed := MixInLength(root, uint64(len(data)))
		if len(mixed) != 32 {
			t.Fatalf("MixInLength root length: got %d, want 32", len(mixed))
		}

		// HashTreeRootByteList: must not panic.
		maxLen := len(data) + 32
		if maxLen < 1 {
			maxLen = 1
		}
		byteListRoot := HashTreeRootByteList(data, maxLen)
		if len(byteListRoot) != 32 {
			t.Fatalf("HashTreeRootByteList root length: got %d, want 32", len(byteListRoot))
		}

		// HashTreeRootBasicVector: must not panic.
		bvRoot := HashTreeRootBasicVector(data)
		if len(bvRoot) != 32 {
			t.Fatalf("HashTreeRootBasicVector root length: got %d, want 32", len(bvRoot))
		}

		// HashTreeRootBasicList: must not panic.
		if len(data) > 0 {
			count := len(data)
			blRoot := HashTreeRootBasicList(data, count, 1, count+16)
			if len(blRoot) != 32 {
				t.Fatalf("HashTreeRootBasicList root length: got %d, want 32", len(blRoot))
			}
		}

		// HashTreeRootBool: must not panic.
		_ = HashTreeRootBool(len(data)%2 == 0)

		// HashTreeRootUint64: must not panic.
		if len(data) >= 8 {
			val := binary.LittleEndian.Uint64(data[:8])
			_ = HashTreeRootUint64(val)
		}

		// Determinism check: same input produces same output.
		chunks2 := Pack(data)
		root2 := Merkleize(chunks2, 0)
		if root != root2 {
			t.Fatalf("Merkleize non-deterministic: %x vs %x", root, root2)
		}
	})
}
