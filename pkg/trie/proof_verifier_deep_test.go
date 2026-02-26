package trie

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- ProofSizeEstimator tests ---

func TestProofSizeEstimator_MPT(t *testing.T) {
	e := NewProofSizeEstimator()
	size := e.EstimateMPTProofSize(8)
	// 8 * 200 + 32 = 1632
	if size != 1632 {
		t.Errorf("expected 1632, got %d", size)
	}
}

func TestProofSizeEstimator_MPTZeroDepth(t *testing.T) {
	e := NewProofSizeEstimator()
	if e.EstimateMPTProofSize(0) != 0 {
		t.Error("expected 0 for zero depth")
	}
}

func TestProofSizeEstimator_Binary(t *testing.T) {
	e := NewProofSizeEstimator()
	// 256-bit key: depth 256, value 64 bytes.
	size := e.EstimateBinaryProofSize(256, 64)
	// 256*32 + 32 + 64 = 8288
	if size != 8288 {
		t.Errorf("expected 8288, got %d", size)
	}
}

func TestProofSizeEstimator_BinaryZeroDepth(t *testing.T) {
	e := NewProofSizeEstimator()
	size := e.EstimateBinaryProofSize(0, 32)
	// 0*32 + 32 + 32 = 64
	if size != 64 {
		t.Errorf("expected 64, got %d", size)
	}
}

func TestProofSizeEstimator_IPA(t *testing.T) {
	e := NewProofSizeEstimator()
	size := e.EstimateIPAProofSize(5)
	// 5*32 + 544 + 32 = 736
	if size != 736 {
		t.Errorf("expected 736, got %d", size)
	}
}

func TestProofSizeEstimator_IPAZeroDepth(t *testing.T) {
	e := NewProofSizeEstimator()
	if e.EstimateIPAProofSize(0) != 0 {
		t.Error("expected 0 for zero depth")
	}
}

// --- CompactProofEncoder tests ---

func TestCompactProofEncoder_RoundTrip(t *testing.T) {
	enc := NewCompactProofEncoder()
	proof := [][]byte{
		{0x01, 0x02, 0x03, 0x04, 0x05},
		{0x01, 0x02, 0x03, 0x06, 0x07},
		{0x01, 0x02, 0x08, 0x09, 0x0A},
	}

	cp, err := enc.Encode(proof)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if cp.NumNodes != 3 {
		t.Errorf("NumNodes = %d, want 3", cp.NumNodes)
	}

	decoded, err := enc.Decode(cp)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(decoded))
	}
	for i := range proof {
		if !bytes.Equal(decoded[i], proof[i]) {
			t.Errorf("node %d mismatch: got %x, want %x", i, decoded[i], proof[i])
		}
	}
}

func TestCompactProofEncoder_SingleNode(t *testing.T) {
	enc := NewCompactProofEncoder()
	proof := [][]byte{{0xAA, 0xBB, 0xCC}}

	cp, err := enc.Encode(proof)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := enc.Decode(cp)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if !bytes.Equal(decoded[0], proof[0]) {
		t.Errorf("mismatch: got %x, want %x", decoded[0], proof[0])
	}
}

func TestCompactProofEncoder_EmptyProof(t *testing.T) {
	enc := NewCompactProofEncoder()
	_, err := enc.Encode(nil)
	if err != ErrCompactProofEmpty {
		t.Fatalf("expected ErrCompactProofEmpty, got %v", err)
	}
}

func TestCompactProofEncoder_CorruptDecode(t *testing.T) {
	enc := NewCompactProofEncoder()
	_, err := enc.Decode(nil)
	if err != ErrCompactProofCorrupt {
		t.Fatalf("expected ErrCompactProofCorrupt, got %v", err)
	}

	_, err = enc.Decode(&CompactProof{EncodedData: []byte{0x00}})
	if err != ErrCompactProofCorrupt {
		t.Fatalf("expected ErrCompactProofCorrupt, got %v", err)
	}
}

func TestCompactProof_CompressionRatio(t *testing.T) {
	enc := NewCompactProofEncoder()
	// Nodes with lots of shared prefix -> good compression.
	proof := [][]byte{
		bytes.Repeat([]byte{0x42}, 100),
		bytes.Repeat([]byte{0x42}, 100), // identical
	}
	cp, err := enc.Encode(proof)
	if err != nil {
		t.Fatal(err)
	}
	ratio := cp.CompressionRatio()
	if ratio >= 1.0 {
		t.Errorf("expected compression < 1.0, got %f", ratio)
	}
}

func TestCompactProof_CompressionRatioEmpty(t *testing.T) {
	cp := &CompactProof{OriginalSize: 0}
	if cp.CompressionRatio() != 1.0 {
		t.Error("expected 1.0 for zero original size")
	}
}

func TestCompactProofEncoder_RealMPTProof(t *testing.T) {
	// Generate a real MPT proof and round-trip through compact encoding.
	tr := New()
	tr.Put([]byte("alpha"), []byte("one"))
	tr.Put([]byte("bravo"), []byte("two"))
	tr.Put([]byte("charlie"), []byte("three"))

	proof, err := tr.Prove([]byte("bravo"))
	if err != nil {
		t.Fatal(err)
	}

	enc := NewCompactProofEncoder()
	cp, err := enc.Encode(proof)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := enc.Decode(cp)
	if err != nil {
		t.Fatal(err)
	}
	for i := range proof {
		if !bytes.Equal(decoded[i], proof[i]) {
			t.Errorf("node %d mismatch after round-trip", i)
		}
	}
}

// --- VerifyBinaryProofBatch tests ---

func TestVerifyBinaryProofBatch_EmptyInput(t *testing.T) {
	_, err := VerifyBinaryProofBatch(types.Hash{}, nil)
	if err != ErrBatchProofEmpty {
		t.Fatalf("expected ErrBatchProofEmpty, got %v", err)
	}
}

func TestVerifyBinaryProofBatch_AllValid(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("a"), []byte("1"))
	bt.Put([]byte("b"), []byte("2"))
	bt.Put([]byte("c"), []byte("3"))
	root := bt.Hash()

	proofs := make([]*BinaryProof, 3)
	for i, k := range []string{"a", "b", "c"} {
		p, err := bt.Prove([]byte(k))
		if err != nil {
			t.Fatal(err)
		}
		proofs[i] = p
	}

	result, err := VerifyBinaryProofBatch(root, proofs)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid != 3 {
		t.Errorf("expected 3 valid, got %d", result.Valid)
	}
	if result.Invalid != 0 {
		t.Errorf("expected 0 invalid, got %d", result.Invalid)
	}
}

func TestVerifyBinaryProofBatch_MixedValidInvalid(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("val"))
	root := bt.Hash()

	validProof, _ := bt.Prove([]byte("key"))

	proofs := []*BinaryProof{validProof, nil}

	result, err := VerifyBinaryProofBatch(root, proofs)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid != 1 || result.Invalid != 1 {
		t.Errorf("expected 1 valid / 1 invalid, got %d/%d", result.Valid, result.Invalid)
	}
}

func TestVerifyBinaryProofBatch_TamperedProof(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("alpha"), []byte("one"))
	bt.Put([]byte("bravo"), []byte("two"))
	bt.Put([]byte("charlie"), []byte("three"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("alpha"))
	if err != nil {
		t.Fatal(err)
	}

	// Tamper with a sibling hash.
	if len(proof.Siblings) > 0 {
		proof.Siblings[0][0] ^= 0xff
	} else {
		t.Skip("no siblings to tamper with")
	}

	result, err := VerifyBinaryProofBatch(root, []*BinaryProof{proof})
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid != 0 || result.Invalid != 1 {
		t.Errorf("expected 0 valid / 1 invalid for tampered, got %d/%d",
			result.Valid, result.Invalid)
	}
}

// --- ProofCache tests ---

func TestProofCache_PutAndGet(t *testing.T) {
	cache := NewProofCache(10)
	root := types.HexToHash("0x01")
	key := []byte("test")
	proof := [][]byte{{0x01, 0x02}, {0x03, 0x04}}

	cache.Put(root, key, proof)
	if cache.Len() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.Len())
	}

	got := cache.Get(root, key)
	if got == nil {
		t.Fatal("expected cached proof")
	}
	if len(got) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(got))
	}
}

func TestProofCache_Miss(t *testing.T) {
	cache := NewProofCache(10)
	got := cache.Get(types.HexToHash("0x01"), []byte("missing"))
	if got != nil {
		t.Error("expected cache miss")
	}
}

func TestProofCache_Eviction(t *testing.T) {
	cache := NewProofCache(2)
	root := types.HexToHash("0x01")

	cache.Put(root, []byte("a"), [][]byte{{0x01}})
	cache.Put(root, []byte("b"), [][]byte{{0x02}})
	cache.Put(root, []byte("c"), [][]byte{{0x03}})

	// One should have been evicted.
	if cache.Len() != 2 {
		t.Errorf("expected 2 entries after eviction, got %d", cache.Len())
	}
}

func TestProofCache_Clear(t *testing.T) {
	cache := NewProofCache(10)
	root := types.HexToHash("0x01")
	cache.Put(root, []byte("a"), [][]byte{{0x01}})
	cache.Clear()
	if cache.Len() != 0 {
		t.Errorf("expected 0 entries after clear, got %d", cache.Len())
	}
}

// --- CompareCrossTrieProofs tests ---

func TestCompareCrossTrieProofs_Matching(t *testing.T) {
	// Build MPT and binary trie with same data.
	mpt := New()
	mpt.Put([]byte("key"), []byte("value"))
	mptRoot := mpt.Hash()

	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	btRoot := bt.Hash()

	mptProof, _ := mpt.Prove([]byte("key"))
	btProof, _ := bt.Prove([]byte("key"))

	result, err := CompareCrossTrieProofs(mptRoot, []byte("key"), mptProof, btRoot, btProof)
	if err != nil {
		t.Fatalf("CompareCrossTrieProofs: %v", err)
	}
	if !result.Match {
		t.Error("expected matching proofs")
	}
	if !result.MPTExists || !result.BinaryExists {
		t.Error("both should exist")
	}
}

func TestCompareCrossTrieProofs_NilKey(t *testing.T) {
	_, err := CompareCrossTrieProofs(types.Hash{}, nil, nil, types.Hash{}, nil)
	if err != ErrProofNilInput {
		t.Fatalf("expected ErrProofNilInput, got %v", err)
	}
}

func TestCompareCrossTrieProofs_NoBinaryProof(t *testing.T) {
	mpt := New()
	mpt.Put([]byte("key"), []byte("value"))
	mptRoot := mpt.Hash()
	mptProof, _ := mpt.Prove([]byte("key"))

	result, err := CompareCrossTrieProofs(mptRoot, []byte("key"), mptProof, types.Hash{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.MPTExists == result.BinaryExists {
		// MPT exists, binary doesn't (no proof), so they shouldn't match.
		if result.Match {
			t.Error("should not match when binary proof is missing")
		}
	}
}
