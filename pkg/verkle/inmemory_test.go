package verkle

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- InMemoryVerkleTree interface tests ---

func TestInMemoryVerkleTree_PutAndGet(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	key[StemSize] = 0x05

	value := make([]byte, ValueSize)
	value[0] = 0xaa
	value[1] = 0xbb

	if err := tree.Put(key, value); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := tree.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if !bytes.Equal(got, value) {
		t.Errorf("Get mismatch: got %x, want %x", got, value)
	}
}

func TestInMemoryVerkleTree_GetNonExistent(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0xff

	got, err := tree.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent key, got %x", got)
	}
}

func TestInMemoryVerkleTree_Delete(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01

	value := make([]byte, ValueSize)
	value[0] = 0xcc

	tree.Put(key, value)

	if err := tree.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := tree.Get(key)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %x", got)
	}
}

func TestInMemoryVerkleTree_DeleteNonExistent(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0xff

	// Deleting a non-existent key should not error.
	if err := tree.Delete(key); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestInMemoryVerkleTree_InvalidKeySize(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	short := make([]byte, 16)
	long := make([]byte, 64)
	value := make([]byte, ValueSize)

	if _, err := tree.Get(short); err == nil {
		t.Error("Get(short key) should error")
	}
	if _, err := tree.Get(long); err == nil {
		t.Error("Get(long key) should error")
	}
	if err := tree.Put(short, value); err == nil {
		t.Error("Put(short key) should error")
	}
	if err := tree.Delete(long); err == nil {
		t.Error("Delete(long key) should error")
	}
}

func TestInMemoryVerkleTree_InvalidValueSize(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	short := make([]byte, 10)
	long := make([]byte, 64)

	if err := tree.Put(key, short); err == nil {
		t.Error("Put(short value) should error")
	}
	if err := tree.Put(key, long); err == nil {
		t.Error("Put(long value) should error")
	}
}

func TestInMemoryVerkleTree_Commit(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	// Empty tree commit.
	root1, err := tree.Commit()
	if err != nil {
		t.Fatalf("Commit (empty): %v", err)
	}
	if root1.IsZero() {
		t.Error("empty tree root should not be zero hash")
	}

	// Add a value and commit again.
	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa

	tree.Put(key, value)

	root2, err := tree.Commit()
	if err != nil {
		t.Fatalf("Commit (after put): %v", err)
	}

	if root1 == root2 {
		t.Error("root should change after insert")
	}

	// Same state should yield same root.
	root3, err := tree.Commit()
	if err != nil {
		t.Fatalf("Commit (stable): %v", err)
	}
	if root2 != root3 {
		t.Error("root should be deterministic")
	}
}

func TestInMemoryVerkleTree_CommitDeterministic(t *testing.T) {
	// Two trees with the same data should produce the same root.
	tree1 := NewInMemoryVerkleTree()
	tree2 := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x42
	value := make([]byte, ValueSize)
	value[0] = 0xde

	tree1.Put(key, value)
	tree2.Put(key, value)

	root1, _ := tree1.Commit()
	root2, _ := tree2.Commit()

	if root1 != root2 {
		t.Errorf("deterministic mismatch: %x != %x", root1, root2)
	}
}

func TestInMemoryVerkleTree_MultipleKeys(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	// Insert 10 keys with different stems.
	for i := byte(0); i < 10; i++ {
		key := make([]byte, KeySize)
		key[0] = i
		value := make([]byte, ValueSize)
		value[0] = i * 10
		tree.Put(key, value)
	}

	// Verify all keys.
	for i := byte(0); i < 10; i++ {
		key := make([]byte, KeySize)
		key[0] = i
		got, err := tree.Get(key)
		if err != nil {
			t.Fatalf("Get key %d: %v", i, err)
		}
		if got == nil {
			t.Fatalf("key %d: got nil", i)
		}
		if got[0] != i*10 {
			t.Errorf("key %d: got %d, want %d", i, got[0], i*10)
		}
	}
}

// --- Prove tests ---

func TestInMemoryVerkleTree_ProveExistingKey(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	key[StemSize] = 0x02

	value := make([]byte, ValueSize)
	value[0] = 0xaa

	tree.Put(key, value)

	proof, err := tree.Prove(key)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}

	// Should be an inclusion proof.
	if !proof.IsSufficiencyProof() {
		t.Error("expected sufficiency (inclusion) proof")
	}
	if proof.IsAbsenceProof() {
		t.Error("should not be absence proof")
	}
	if !proof.ExtensionPresent {
		t.Error("ExtensionPresent should be true")
	}
	if proof.Value == nil {
		t.Fatal("proof value is nil")
	}
	if !bytes.Equal(proof.Value[:], value) {
		t.Errorf("proof value mismatch: got %x, want %x", proof.Value[:], value)
	}
	if len(proof.CommitmentsByPath) == 0 {
		t.Error("CommitmentsByPath should not be empty")
	}
	if len(proof.IPAProof) == 0 {
		t.Error("IPAProof should not be empty")
	}
	if proof.D.IsZero() {
		t.Error("D commitment should not be zero")
	}
}

func TestInMemoryVerkleTree_ProveNonExistentKey(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	// Insert one key, prove a different one.
	existingKey := make([]byte, KeySize)
	existingKey[0] = 0x01
	existingValue := make([]byte, ValueSize)
	existingValue[0] = 0xaa
	tree.Put(existingKey, existingValue)

	missingKey := make([]byte, KeySize)
	missingKey[0] = 0xff

	proof, err := tree.Prove(missingKey)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}

	// Should be an absence proof.
	if !proof.IsAbsenceProof() {
		t.Error("expected absence proof")
	}
	if proof.IsSufficiencyProof() {
		t.Error("should not be sufficiency proof")
	}
}

func TestInMemoryVerkleTree_ProveInvalidKey(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	_, err := tree.Prove([]byte{0x01, 0x02})
	if err == nil {
		t.Error("Prove with short key should error")
	}
}

func TestVerkleProof_Verify(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	key := make([]byte, KeySize)
	key[0] = 0x01
	value := make([]byte, ValueSize)
	value[0] = 0xaa
	tree.Put(key, value)

	proof, _ := tree.Prove(key)
	root := tree.tree.RootCommitment()

	if !proof.Verify(root) {
		t.Error("valid proof should verify against root")
	}
}

func TestVerkleProof_VerifyEmptyPath(t *testing.T) {
	// A proof with empty commitments should fail.
	proof := &VerkleProof{
		CommitmentsByPath: nil,
		IPAProof:          []byte{0x01},
		Depth:             1,
	}
	if proof.Verify(Commitment{}) {
		t.Error("proof with no path commitments should fail")
	}
}

func TestVerkleProof_VerifyInvalidDepth(t *testing.T) {
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{{}},
		IPAProof:          []byte{0x01},
		Depth:             uint8(MaxDepth + 1),
	}
	if proof.Verify(Commitment{}) {
		t.Error("proof with depth > MaxDepth should fail")
	}
}

func TestVerkleProof_VerifyNoIPAProof(t *testing.T) {
	proof := &VerkleProof{
		CommitmentsByPath: []Commitment{{}},
		IPAProof:          nil,
		Depth:             1,
	}
	if proof.Verify(Commitment{}) {
		t.Error("proof with no IPA data should fail")
	}
}

// --- VerkleKeyFromAddress tests ---

func TestVerkleKeyFromAddress(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03})

	key := VerkleKeyFromAddress(addr, BalanceLeafKey)
	if len(key) != KeySize {
		t.Fatalf("key length = %d, want %d", len(key), KeySize)
	}

	// Suffix should match.
	if key[StemSize] != BalanceLeafKey {
		t.Errorf("suffix = %d, want %d", key[StemSize], BalanceLeafKey)
	}

	// Stem should match GetTreeKeyForBalance.
	expected := GetTreeKeyForBalance(addr)
	if !bytes.Equal(key, expected[:]) {
		t.Errorf("VerkleKeyFromAddress mismatch: got %x, want %x", key, expected[:])
	}
}

func TestVerkleKeyFromAddress_AllHeaderFields(t *testing.T) {
	addr := types.BytesToAddress([]byte{0xde, 0xad, 0xbe, 0xef})

	suffixes := []struct {
		name   string
		suffix byte
	}{
		{"version", VersionLeafKey},
		{"balance", BalanceLeafKey},
		{"nonce", NonceLeafKey},
		{"code_hash", CodeHashLeafKey},
		{"code_size", CodeSizeLeafKey},
	}

	// All keys should share the same stem.
	var stem []byte
	for _, tc := range suffixes {
		key := VerkleKeyFromAddress(addr, tc.suffix)
		if stem == nil {
			stem = make([]byte, StemSize)
			copy(stem, key[:StemSize])
		} else {
			if !bytes.Equal(key[:StemSize], stem) {
				t.Errorf("%s stem mismatch", tc.name)
			}
		}
		if key[StemSize] != tc.suffix {
			t.Errorf("%s suffix = %d, want %d", tc.name, key[StemSize], tc.suffix)
		}
	}
}

// --- AccountHeaderKeys tests ---

func TestAccountHeaderKeys(t *testing.T) {
	addr := types.BytesToAddress([]byte{0xca, 0xfe})

	keys := AccountHeaderKeys(addr)

	// All 5 keys should share the same stem.
	stem := StemFromKey(keys[0])
	for i := 1; i < 5; i++ {
		if StemFromKey(keys[i]) != stem {
			t.Errorf("key %d has different stem", i)
		}
	}

	// Check suffixes in order.
	expectedSuffixes := []byte{
		VersionLeafKey,
		BalanceLeafKey,
		NonceLeafKey,
		CodeHashLeafKey,
		CodeSizeLeafKey,
	}
	for i, expected := range expectedSuffixes {
		if SuffixFromKey(keys[i]) != expected {
			t.Errorf("key %d suffix = %d, want %d", i, SuffixFromKey(keys[i]), expected)
		}
	}
}

func TestAccountHeaderKeys_MatchesIndividual(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x42})

	keys := AccountHeaderKeys(addr)

	if keys[0] != GetTreeKeyForVersion(addr) {
		t.Error("version key mismatch")
	}
	if keys[1] != GetTreeKeyForBalance(addr) {
		t.Error("balance key mismatch")
	}
	if keys[2] != GetTreeKeyForNonce(addr) {
		t.Error("nonce key mismatch")
	}
	if keys[3] != GetTreeKeyForCodeHash(addr) {
		t.Error("code hash key mismatch")
	}
	if keys[4] != GetTreeKeyForCodeSize(addr) {
		t.Error("code size key mismatch")
	}
}

// --- Integration: store account data via the VerkleTree interface ---

func TestInMemoryVerkleTree_AccountData(t *testing.T) {
	tree := NewInMemoryVerkleTree()
	addr := types.BytesToAddress([]byte{0xde, 0xad, 0xbe, 0xef})

	// Store version = 0.
	versionKey := GetTreeKeyForVersion(addr)
	versionVal := [ValueSize]byte{0}
	tree.Put(versionKey[:], versionVal[:])

	// Store balance = 1 ETH (simplified as a single byte).
	balanceKey := GetTreeKeyForBalance(addr)
	balanceVal := [ValueSize]byte{}
	balanceVal[0] = 0x01
	tree.Put(balanceKey[:], balanceVal[:])

	// Store nonce = 5.
	nonceKey := GetTreeKeyForNonce(addr)
	nonceVal := [ValueSize]byte{}
	nonceVal[0] = 0x05
	tree.Put(nonceKey[:], nonceVal[:])

	// Verify retrieval.
	gotBalance, err := tree.Get(balanceKey[:])
	if err != nil {
		t.Fatalf("Get balance: %v", err)
	}
	if gotBalance[0] != 0x01 {
		t.Errorf("balance = %x, want 01", gotBalance[0])
	}

	gotNonce, err := tree.Get(nonceKey[:])
	if err != nil {
		t.Fatalf("Get nonce: %v", err)
	}
	if gotNonce[0] != 0x05 {
		t.Errorf("nonce = %x, want 05", gotNonce[0])
	}

	// Commit and verify root is non-zero.
	root, err := tree.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if root.IsZero() {
		t.Error("root should not be zero after storing account data")
	}

	// Generate proof for balance key.
	proof, err := tree.Prove(balanceKey[:])
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !proof.IsSufficiencyProof() {
		t.Error("balance proof should be sufficiency proof")
	}
	if proof.Value[0] != 0x01 {
		t.Errorf("proof balance value = %x, want 01", proof.Value[0])
	}
}

func TestInMemoryVerkleTree_DifferentAddresses(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	// Store balances for two different addresses.
	key1 := GetTreeKeyForBalance(addr1)
	val1 := [ValueSize]byte{}
	val1[0] = 0x10
	tree.Put(key1[:], val1[:])

	key2 := GetTreeKeyForBalance(addr2)
	val2 := [ValueSize]byte{}
	val2[0] = 0x20
	tree.Put(key2[:], val2[:])

	// Verify they are independent.
	got1, _ := tree.Get(key1[:])
	got2, _ := tree.Get(key2[:])

	if got1[0] != 0x10 {
		t.Errorf("addr1 balance = %x, want 10", got1[0])
	}
	if got2[0] != 0x20 {
		t.Errorf("addr2 balance = %x, want 20", got2[0])
	}
}

// --- InnerTree access ---

func TestInMemoryVerkleTree_InnerTree(t *testing.T) {
	tree := NewInMemoryVerkleTree()

	inner := tree.InnerTree()
	if inner == nil {
		t.Fatal("InnerTree() returned nil")
	}
	if inner.Root() == nil {
		t.Fatal("InnerTree root is nil")
	}
}
