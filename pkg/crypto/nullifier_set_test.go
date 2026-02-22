package crypto

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNullifierSMT_NewTreeHasEmptyRoot(t *testing.T) {
	smt := NewSparseMerkleTree()
	root := smt.Root()
	if root.IsZero() {
		t.Fatal("empty tree should have a non-zero default root")
	}
	if smt.Count() != 0 {
		t.Fatal("new tree should have count 0")
	}
}

func TestNullifierSMT_InsertAndContains(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xaaaa")

	if smt.Contains(key) {
		t.Fatal("key should not exist before insert")
	}

	smt.Insert(key)
	if !smt.Contains(key) {
		t.Fatal("key should exist after insert")
	}
	if smt.Count() != 1 {
		t.Fatalf("expected count 1, got %d", smt.Count())
	}
}

func TestNullifierSMT_InsertChangesRoot(t *testing.T) {
	smt := NewSparseMerkleTree()
	root0 := smt.Root()

	key := types.HexToHash("0xbbbb")
	root1 := smt.Insert(key)

	if root0 == root1 {
		t.Fatal("root should change after insert")
	}
}

func TestNullifierSMT_DifferentKeysProduceDifferentRoots(t *testing.T) {
	smt1 := NewSparseMerkleTree()
	smt2 := NewSparseMerkleTree()

	k1 := types.HexToHash("0x1111")
	k2 := types.HexToHash("0x2222")

	smt1.Insert(k1)
	smt2.Insert(k2)

	if smt1.Root() == smt2.Root() {
		t.Fatal("different keys should produce different roots")
	}
}

func TestNullifierSMT_DuplicateInsertUpdatesCount(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xcccc")

	smt.Insert(key)
	smt.Insert(key) // duplicate
	// Count increments each time (no dedup at this level).
	if smt.Count() != 2 {
		t.Fatalf("expected count 2, got %d", smt.Count())
	}
}

func TestNullifierSMT_BatchInsert(t *testing.T) {
	smt := NewSparseMerkleTree()
	keys := []types.Hash{
		types.HexToHash("0xaa01"),
		types.HexToHash("0xaa02"),
		types.HexToHash("0xaa03"),
	}

	root := smt.BatchInsert(keys)
	if root.IsZero() {
		t.Fatal("batch insert root should not be zero")
	}
	if smt.Count() != 3 {
		t.Fatalf("expected count 3, got %d", smt.Count())
	}

	for _, k := range keys {
		if !smt.Contains(k) {
			t.Fatalf("key %v should exist after batch insert", k)
		}
	}
}

func TestNullifierSMT_BatchInsertSkipsDuplicates(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xdd01")
	smt.Insert(key)

	// Batch insert with the same key.
	smt.BatchInsert([]types.Hash{key, types.HexToHash("0xdd02")})

	// Only 1 new key added (dd02), dd01 was skipped.
	if smt.Count() != 2 {
		t.Fatalf("expected count 2, got %d", smt.Count())
	}
}

func TestNullifierSMT_MerkleProofExists(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xee01")
	smt.Insert(key)

	proof := smt.MerkleProof(key)
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if !proof.Exists {
		t.Fatal("proof should indicate key exists")
	}
	if proof.Key != key {
		t.Fatal("proof key should match input")
	}
}

func TestNullifierSMT_MerkleProofNotExists(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xff01")

	proof := smt.MerkleProof(key)
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.Exists {
		t.Fatal("proof should indicate key does not exist")
	}
}

func TestNullifierSMT_VerifyProofExisting(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xab01")
	smt.Insert(key)

	proof := smt.MerkleProof(key)
	root := smt.Root()

	if !VerifySMTProof(proof, root) {
		t.Fatal("valid proof should verify")
	}
}

func TestNullifierSMT_VerifyProofNonExisting(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xab02")

	proof := smt.MerkleProof(key)
	root := smt.Root()

	if !VerifySMTProof(proof, root) {
		t.Fatal("non-inclusion proof should verify against empty tree root")
	}
}

func TestNullifierSMT_VerifyProofRejectsNil(t *testing.T) {
	root := types.HexToHash("0x1234")
	if VerifySMTProof(nil, root) {
		t.Fatal("nil proof should be rejected")
	}
}

func TestNullifierSMT_VerifyProofRejectsWrongRoot(t *testing.T) {
	smt := NewSparseMerkleTree()
	key := types.HexToHash("0xcd01")
	smt.Insert(key)

	proof := smt.MerkleProof(key)
	wrongRoot := types.HexToHash("0xdeadbeef")

	if VerifySMTProof(proof, wrongRoot) {
		t.Fatal("proof against wrong root should fail")
	}
}

func TestNullifierSMT_GetBit(t *testing.T) {
	// 0x80 = 10000000 in binary.
	var h types.Hash
	h[0] = 0x80

	if getBit(h, 0) != 1 {
		t.Fatal("MSB of 0x80 should be 1")
	}
	if getBit(h, 1) != 0 {
		t.Fatal("bit 1 of 0x80 should be 0")
	}
	if getBit(h, 7) != 0 {
		t.Fatal("bit 7 of 0x80 should be 0")
	}
}

func TestNullifierSMT_MultipleInsertions(t *testing.T) {
	smt := NewSparseMerkleTree()
	for i := 0; i < 100; i++ {
		var key types.Hash
		key[0] = byte(i)
		key[1] = byte(i >> 8)
		smt.Insert(key)
	}
	if smt.Count() != 100 {
		t.Fatalf("expected 100, got %d", smt.Count())
	}
}
