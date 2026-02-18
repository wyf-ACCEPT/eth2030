package trie

import (
	"bytes"
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// -- Prove basic cases --

func TestProve_SimpleKeys(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()

	tests := []struct {
		key  string
		want string
	}{
		{"doe", "reindeer"},
		{"dog", "puppy"},
		{"dogglesworth", "cat"},
	}
	for _, tt := range tests {
		proof, err := tr.Prove([]byte(tt.key))
		if err != nil {
			t.Errorf("Prove(%q) error: %v", tt.key, err)
			continue
		}
		if len(proof) == 0 {
			t.Errorf("Prove(%q) returned empty proof", tt.key)
			continue
		}
		val, err := VerifyProof(root, []byte(tt.key), proof)
		if err != nil {
			t.Errorf("VerifyProof(%q) error: %v", tt.key, err)
			continue
		}
		if string(val) != tt.want {
			t.Errorf("VerifyProof(%q) = %q, want %q", tt.key, val, tt.want)
		}
	}
}

func TestProve_NonExistentKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))

	_, err := tr.Prove([]byte("nonexistent"))
	if err != ErrNotFound {
		t.Fatalf("Prove(nonexistent) err = %v, want ErrNotFound", err)
	}
}

func TestProve_EmptyTrie(t *testing.T) {
	tr := New()
	_, err := tr.Prove([]byte("anything"))
	if err != ErrNotFound {
		t.Fatalf("Prove on empty trie err = %v, want ErrNotFound", err)
	}
}

func TestProve_SingleKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("only"), []byte("one"))

	root := tr.Hash()
	proof, err := tr.Prove([]byte("only"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}

	val, err := VerifyProof(root, []byte("only"), proof)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if string(val) != "one" {
		t.Fatalf("VerifyProof = %q, want %q", val, "one")
	}
}

// -- Verify proof failures --

func TestVerifyProof_EmptyProof(t *testing.T) {
	// An empty proof against a non-empty root should fail.
	root := types.Hash{1, 2, 3}
	_, err := VerifyProof(root, []byte("key"), nil)
	if err != ErrProofInvalid {
		t.Fatalf("VerifyProof(nil proof, non-empty root) err = %v, want ErrProofInvalid", err)
	}

	_, err = VerifyProof(root, []byte("key"), [][]byte{})
	if err != ErrProofInvalid {
		t.Fatalf("VerifyProof(empty proof, non-empty root) err = %v, want ErrProofInvalid", err)
	}

	// An empty proof against the empty root should succeed (absence).
	val, err := VerifyProof(emptyRoot, []byte("key"), nil)
	if err != nil {
		t.Fatalf("VerifyProof(nil proof, empty root) err = %v, want nil", err)
	}
	if val != nil {
		t.Fatalf("VerifyProof(nil proof, empty root) val = %x, want nil", val)
	}
}

func TestVerifyProof_WrongRootHash(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))

	proof, _ := tr.Prove([]byte("key"))
	wrongRoot := types.Hash{0xff, 0xfe, 0xfd}

	_, err := VerifyProof(wrongRoot, []byte("key"), proof)
	if err == nil {
		t.Fatal("VerifyProof with wrong root should fail")
	}
}

func TestVerifyProof_TamperedProofData(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("dog"))

	// Tamper with the first proof node.
	tampered := make([][]byte, len(proof))
	for i := range proof {
		tampered[i] = make([]byte, len(proof[i]))
		copy(tampered[i], proof[i])
	}
	tampered[0][0] ^= 0xff

	_, err := VerifyProof(root, []byte("dog"), tampered)
	if err == nil {
		t.Fatal("VerifyProof with tampered proof should fail")
	}
}

func TestVerifyProof_WrongKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("dog"))

	// Use the proof for "dog" but verify against "doe".
	_, err := VerifyProof(root, []byte("doe"), proof)
	if err == nil {
		t.Fatal("VerifyProof with wrong key should fail")
	}
}

// -- Overlapping prefix proofs --

func TestProve_OverlappingPrefixes(t *testing.T) {
	tr := New()
	tr.Put([]byte("a"), []byte("1"))
	tr.Put([]byte("ab"), []byte("2"))
	tr.Put([]byte("abc"), []byte("3"))

	root := tr.Hash()

	for _, tc := range []struct {
		key  string
		want string
	}{
		{"a", "1"},
		{"ab", "2"},
		{"abc", "3"},
	} {
		proof, err := tr.Prove([]byte(tc.key))
		if err != nil {
			t.Fatalf("Prove(%q) error: %v", tc.key, err)
		}
		val, err := VerifyProof(root, []byte(tc.key), proof)
		if err != nil {
			t.Fatalf("VerifyProof(%q) error: %v", tc.key, err)
		}
		if string(val) != tc.want {
			t.Fatalf("VerifyProof(%q) = %q, want %q", tc.key, val, tc.want)
		}
	}
}

// -- Large value proof --

func TestProve_LargeValue(t *testing.T) {
	tr := New()
	largeVal := bytes.Repeat([]byte{0x42}, 1024)
	tr.Put([]byte("big"), largeVal)
	tr.Put([]byte("small"), []byte("tiny"))

	root := tr.Hash()
	proof, err := tr.Prove([]byte("big"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	val, err := VerifyProof(root, []byte("big"), proof)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if !bytes.Equal(val, largeVal) {
		t.Fatal("large value mismatch in proof")
	}
}

// -- Many keys proof --

func TestProve_ManyKeys(t *testing.T) {
	tr := New()
	entries := make(map[string]string)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%03d", i)
		val := fmt.Sprintf("value-%03d", i)
		tr.Put([]byte(key), []byte(val))
		entries[key] = val
	}

	root := tr.Hash()

	// Prove and verify every key.
	for key, want := range entries {
		proof, err := tr.Prove([]byte(key))
		if err != nil {
			t.Fatalf("Prove(%q) error: %v", key, err)
		}
		val, err := VerifyProof(root, []byte(key), proof)
		if err != nil {
			t.Fatalf("VerifyProof(%q) error: %v", key, err)
		}
		if string(val) != want {
			t.Fatalf("VerifyProof(%q) = %q, want %q", key, val, want)
		}
	}
}

// -- Proof after updates --

func TestProve_AfterUpdate(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("original"))

	// Prove before update.
	root1 := tr.Hash()
	proof1, _ := tr.Prove([]byte("key"))
	val1, _ := VerifyProof(root1, []byte("key"), proof1)
	if string(val1) != "original" {
		t.Fatalf("before update: got %q", val1)
	}

	// Update value.
	tr.Put([]byte("key"), []byte("updated"))
	root2 := tr.Hash()
	proof2, _ := tr.Prove([]byte("key"))
	val2, err := VerifyProof(root2, []byte("key"), proof2)
	if err != nil {
		t.Fatalf("after update VerifyProof error: %v", err)
	}
	if string(val2) != "updated" {
		t.Fatalf("after update: got %q", val2)
	}

	// Old proof should no longer verify against new root.
	_, err = VerifyProof(root2, []byte("key"), proof1)
	if err == nil {
		t.Fatal("old proof should not verify against new root")
	}
}

// -- Inline node proof (small trie where nodes < 32 bytes) --

func TestProve_InlineNodes(t *testing.T) {
	// Small keys and values produce inline nodes in the trie.
	tr := New()
	tr.Put([]byte{0x01}, []byte("a"))
	tr.Put([]byte{0x11}, []byte("b"))

	root := tr.Hash()

	for _, key := range [][]byte{{0x01}, {0x11}} {
		proof, err := tr.Prove(key)
		if err != nil {
			t.Fatalf("Prove(%x) error: %v", key, err)
		}
		val, err := VerifyProof(root, key, proof)
		if err != nil {
			t.Fatalf("VerifyProof(%x) error: %v", key, err)
		}
		if len(val) != 1 {
			t.Fatalf("VerifyProof(%x) value length = %d, want 1", key, len(val))
		}
	}
}

// -- Binary key proofs --

func TestProve_BinaryKeys(t *testing.T) {
	tr := New()
	keys := [][]byte{
		{0x00, 0x00},
		{0x00, 0xff},
		{0xff, 0x00},
		{0xff, 0xff},
	}
	for i, k := range keys {
		tr.Put(k, []byte(fmt.Sprintf("val%d", i)))
	}

	root := tr.Hash()
	for i, k := range keys {
		proof, err := tr.Prove(k)
		if err != nil {
			t.Fatalf("Prove(%x) error: %v", k, err)
		}
		val, err := VerifyProof(root, k, proof)
		if err != nil {
			t.Fatalf("VerifyProof(%x) error: %v", k, err)
		}
		want := fmt.Sprintf("val%d", i)
		if string(val) != want {
			t.Fatalf("VerifyProof(%x) = %q, want %q", k, val, want)
		}
	}
}

// -- Random proof test --

func TestProve_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	tr := New()
	var keys [][]byte

	for i := 0; i < 200; i++ {
		keyLen := rng.Intn(32) + 1
		key := make([]byte, keyLen)
		rng.Read(key)
		val := make([]byte, rng.Intn(100)+1)
		rng.Read(val)
		tr.Put(key, val)
		keys = append(keys, key)
	}

	root := tr.Hash()

	// Prove and verify each key.
	for _, key := range keys {
		proof, err := tr.Prove(key)
		if err != nil {
			t.Fatalf("Prove(%x) error: %v", key, err)
		}
		gotVal, err := VerifyProof(root, key, proof)
		if err != nil {
			t.Fatalf("VerifyProof(%x) error: %v", key, err)
		}
		// Verify the value matches Get.
		expected, _ := tr.Get(key)
		if !bytes.Equal(gotVal, expected) {
			t.Fatalf("VerifyProof(%x) value mismatch", key)
		}
	}
}

// -- Proof root hash consistency --

func TestProve_RootHashConsistency(t *testing.T) {
	// The hash of the first proof node should match the trie root.
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("dog"))

	// First proof node hash should equal root.
	firstNodeHash := crypto.Keccak256Hash(proof[0])
	if firstNodeHash != root {
		t.Fatalf("first proof node hash = %s, root = %s", firstNodeHash.Hex(), root.Hex())
	}
}

// -- Proof with single element trie that's < 32 bytes RLP --

func TestProve_TinyRootNode(t *testing.T) {
	// A trie with a very small root (< 32 bytes encoded) still gets force-hashed.
	tr := New()
	tr.Put([]byte{0x01}, []byte{0x02})

	root := tr.Hash()
	proof, err := tr.Prove([]byte{0x01})
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	val, err := VerifyProof(root, []byte{0x01}, proof)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if !bytes.Equal(val, []byte{0x02}) {
		t.Fatalf("value = %x, want 02", val)
	}
}

// ============================================================
// Proof of absence tests
// ============================================================

func TestProveAbsence_EmptyTrie(t *testing.T) {
	tr := New()
	root := tr.Hash()

	proof, err := tr.ProveAbsence([]byte("anything"))
	if err != nil {
		t.Fatalf("ProveAbsence on empty trie: %v", err)
	}
	if proof != nil {
		t.Fatalf("ProveAbsence on empty trie should return nil proof, got %d nodes", len(proof))
	}

	// VerifyProof with empty proof and empty root proves absence.
	val, err := VerifyProof(root, []byte("anything"), proof)
	if err != nil {
		t.Fatalf("VerifyProof absence on empty trie: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value for absence, got %x", val)
	}
}

func TestProveAbsence_KeyNotInTrie(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()

	// "cat" is not in the trie.
	proof, err := tr.ProveAbsence([]byte("cat"))
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}
	if len(proof) == 0 {
		t.Fatal("ProveAbsence should return at least one node for a non-empty trie")
	}

	val, err := VerifyProof(root, []byte("cat"), proof)
	if err != nil {
		t.Fatalf("VerifyProof absence error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value for absence, got %x", val)
	}
}

func TestProveAbsence_PrefixKey(t *testing.T) {
	// Test absence proof for a key that is a prefix of an existing key.
	tr := New()
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()

	// "dog" is a prefix of "dogglesworth" but not in the trie.
	proof, err := tr.ProveAbsence([]byte("dog"))
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}

	val, err := VerifyProof(root, []byte("dog"), proof)
	if err != nil {
		t.Fatalf("VerifyProof absence error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value for absent prefix key, got %x", val)
	}
}

func TestProveAbsence_DivergentNibble(t *testing.T) {
	// Test absence where the key diverges at a branch node (no child).
	tr := New()
	tr.Put([]byte{0x10}, []byte("a"))
	tr.Put([]byte{0x20}, []byte("b"))
	tr.Put([]byte{0x30}, []byte("c"))

	root := tr.Hash()

	// 0x40 would need nibble 4 at the branch, which doesn't exist.
	proof, err := tr.ProveAbsence([]byte{0x40})
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}

	val, err := VerifyProof(root, []byte{0x40}, proof)
	if err != nil {
		t.Fatalf("VerifyProof absence error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value for absent nibble, got %x", val)
	}
}

func TestProveAbsence_SimilarKey(t *testing.T) {
	// Absence proof for a key that shares a long common prefix with an existing key.
	tr := New()
	tr.Put([]byte("abcdefgh"), []byte("value1"))
	tr.Put([]byte("abcdefxy"), []byte("value2"))

	root := tr.Hash()

	// "abcdefzz" shares prefix "abcdef" but diverges at 'z'.
	proof, err := tr.ProveAbsence([]byte("abcdefzz"))
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}

	val, err := VerifyProof(root, []byte("abcdefzz"), proof)
	if err != nil {
		t.Fatalf("VerifyProof absence error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value for absent similar key, got %x", val)
	}
}

func TestProveAbsence_TamperedProofFails(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))

	root := tr.Hash()

	proof, err := tr.ProveAbsence([]byte("cat"))
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}
	if len(proof) == 0 {
		t.Fatal("expected non-empty proof")
	}

	// Tamper with the proof.
	tampered := make([][]byte, len(proof))
	for i := range proof {
		tampered[i] = make([]byte, len(proof[i]))
		copy(tampered[i], proof[i])
	}
	tampered[0][len(tampered[0])-1] ^= 0xff

	_, err = VerifyProof(root, []byte("cat"), tampered)
	if err == nil {
		t.Fatal("VerifyProof should fail with tampered absence proof")
	}
}

func TestProveAbsence_ExistingKeyNotAbsent(t *testing.T) {
	// ProveAbsence for a key that actually exists should still produce a valid
	// proof that walks to the value. Verifying this proof should return the value.
	tr := New()
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("doe"), []byte("reindeer"))

	root := tr.Hash()

	// "dog" exists, so ProveAbsence still collects the path. VerifyProof
	// should return the value, not nil.
	proof, err := tr.ProveAbsence([]byte("dog"))
	if err != nil {
		t.Fatalf("ProveAbsence error: %v", err)
	}

	val, err := VerifyProof(root, []byte("dog"), proof)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if string(val) != "puppy" {
		t.Fatalf("expected %q, got %q", "puppy", string(val))
	}
}

func TestProveAbsence_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(54321))
	tr := New()
	existing := make(map[string]bool)

	// Insert 100 random keys.
	for i := 0; i < 100; i++ {
		keyLen := rng.Intn(16) + 1
		key := make([]byte, keyLen)
		rng.Read(key)
		val := make([]byte, rng.Intn(50)+1)
		rng.Read(val)
		tr.Put(key, val)
		existing[string(key)] = true
	}

	root := tr.Hash()

	// Generate and verify absence proofs for random non-existent keys.
	absent := 0
	for absent < 50 {
		keyLen := rng.Intn(16) + 1
		key := make([]byte, keyLen)
		rng.Read(key)
		if existing[string(key)] {
			continue
		}
		absent++

		proof, err := tr.ProveAbsence(key)
		if err != nil {
			t.Fatalf("ProveAbsence(%x) error: %v", key, err)
		}

		val, err := VerifyProof(root, key, proof)
		if err != nil {
			t.Fatalf("VerifyProof absence(%x) error: %v", key, err)
		}
		if val != nil {
			t.Fatalf("expected nil value for absent key %x, got %x", key, val)
		}
	}
}

// ============================================================
// Multiple key proofs from same trie
// ============================================================

func TestProve_MultipleKeysFromSameTrie(t *testing.T) {
	tr := New()
	entries := map[string]string{
		"alpha":   "A",
		"bravo":   "B",
		"charlie": "C",
		"delta":   "D",
		"echo":    "E",
	}
	for k, v := range entries {
		tr.Put([]byte(k), []byte(v))
	}

	root := tr.Hash()

	// Generate proofs for all keys and verify independently.
	proofs := make(map[string][][]byte)
	for k := range entries {
		proof, err := tr.Prove([]byte(k))
		if err != nil {
			t.Fatalf("Prove(%q) error: %v", k, err)
		}
		proofs[k] = proof
	}

	// Verify all proofs.
	for k, want := range entries {
		val, err := VerifyProof(root, []byte(k), proofs[k])
		if err != nil {
			t.Fatalf("VerifyProof(%q) error: %v", k, err)
		}
		if string(val) != want {
			t.Fatalf("VerifyProof(%q) = %q, want %q", k, val, want)
		}
	}

	// Cross-check: each proof should NOT verify a different key.
	for k := range entries {
		for otherKey := range entries {
			if k == otherKey {
				continue
			}
			_, err := VerifyProof(root, []byte(otherKey), proofs[k])
			if err == nil {
				// It's possible for proofs to "accidentally" work if keys
				// share the exact same path, but for these distinct keys it
				// should fail.
				val, _ := VerifyProof(root, []byte(otherKey), proofs[k])
				if val != nil && string(val) == entries[otherKey] {
					t.Fatalf("proof for %q should not produce correct value for %q", k, otherKey)
				}
			}
		}
	}
}

// ============================================================
// Account state proof tests (eth_getProof)
// ============================================================

func TestProveAccount_ExistingAccount(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	account := &types.Account{
		Nonce:    42,
		Balance:  big.NewInt(1000000000),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	accountRLP, err := EncodeAccount(account)
	if err != nil {
		t.Fatalf("EncodeAccount error: %v", err)
	}

	// Insert account into state trie using keccak256(address) as key.
	addrHash := crypto.Keccak256(addr[:])
	stateTrie.Put(addrHash, accountRLP)

	root := stateTrie.Hash()

	// Generate account proof.
	result, err := ProveAccount(stateTrie, addr)
	if err != nil {
		t.Fatalf("ProveAccount error: %v", err)
	}

	if result.Address != addr {
		t.Fatalf("address mismatch: got %s, want %s", result.Address.Hex(), addr.Hex())
	}
	if result.Nonce != 42 {
		t.Fatalf("nonce = %d, want 42", result.Nonce)
	}
	if result.Balance.Cmp(big.NewInt(1000000000)) != 0 {
		t.Fatalf("balance = %s, want 1000000000", result.Balance)
	}
	if result.StorageHash != types.EmptyRootHash {
		t.Fatalf("storage hash mismatch")
	}
	if result.CodeHash != types.EmptyCodeHash {
		t.Fatalf("code hash mismatch")
	}

	// Verify the proof.
	val, err := VerifyProof(root, addrHash, result.AccountProof)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if !bytes.Equal(val, accountRLP) {
		t.Fatal("proof value does not match account RLP")
	}
}

func TestProveAccount_NonExistentAccount(t *testing.T) {
	stateTrie := New()

	// Insert one account.
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	account := &types.Account{
		Nonce:    1,
		Balance:  big.NewInt(100),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	accountRLP, _ := EncodeAccount(account)
	stateTrie.Put(crypto.Keccak256(addr1[:]), accountRLP)

	root := stateTrie.Hash()

	// Prove non-existent account.
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")
	result, err := ProveAccount(stateTrie, addr2)
	if err != nil {
		t.Fatalf("ProveAccount error: %v", err)
	}

	if result.Nonce != 0 {
		t.Fatalf("nonce = %d, want 0", result.Nonce)
	}
	if result.Balance.Sign() != 0 {
		t.Fatalf("balance = %s, want 0", result.Balance)
	}
	if result.StorageHash != types.EmptyRootHash {
		t.Fatalf("storage hash should be empty root for non-existent account")
	}
	if result.CodeHash != types.EmptyCodeHash {
		t.Fatalf("code hash should be empty code hash for non-existent account")
	}

	// Verify the absence proof.
	addrHash2 := crypto.Keccak256(addr2[:])
	val, err := VerifyProof(root, addrHash2, result.AccountProof)
	if err != nil {
		t.Fatalf("VerifyProof absence error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value for non-existent account, got %x", val)
	}
}

func TestProveAccount_EmptyStateTrie(t *testing.T) {
	stateTrie := New()
	root := stateTrie.Hash()

	addr := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	result, err := ProveAccount(stateTrie, addr)
	if err != nil {
		t.Fatalf("ProveAccount on empty trie error: %v", err)
	}

	if result.Nonce != 0 {
		t.Fatalf("nonce = %d, want 0", result.Nonce)
	}

	// Empty proof against empty root.
	addrHash := crypto.Keccak256(addr[:])
	val, err := VerifyProof(root, addrHash, result.AccountProof)
	if err != nil {
		t.Fatalf("VerifyProof error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil for empty trie, got %x", val)
	}
}

func TestProveAccountWithStorage(t *testing.T) {
	stateTrie := New()
	storageTrie := New()

	// Set up storage trie with some slots.
	slot1 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	slot2 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
	slot1Hash := crypto.Keccak256(slot1[:])
	slot2Hash := crypto.Keccak256(slot2[:])
	storageTrie.Put(slot1Hash, big.NewInt(100).Bytes())
	storageTrie.Put(slot2Hash, big.NewInt(200).Bytes())

	storageRoot := storageTrie.Hash()

	// Set up account in state trie.
	addr := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	account := &types.Account{
		Nonce:    10,
		Balance:  big.NewInt(5000),
		Root:     storageRoot,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	accountRLP, _ := EncodeAccount(account)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)

	root := stateTrie.Hash()

	// Request proof with two storage keys (one exists, one doesn't).
	slot3 := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000003")
	result, err := ProveAccountWithStorage(stateTrie, addr, storageTrie, []types.Hash{slot1, slot3})
	if err != nil {
		t.Fatalf("ProveAccountWithStorage error: %v", err)
	}

	// Verify account proof.
	addrHash := crypto.Keccak256(addr[:])
	val, err := VerifyProof(root, addrHash, result.AccountProof)
	if err != nil {
		t.Fatalf("VerifyProof account error: %v", err)
	}
	if !bytes.Equal(val, accountRLP) {
		t.Fatal("account proof value mismatch")
	}

	if len(result.StorageProof) != 2 {
		t.Fatalf("expected 2 storage proofs, got %d", len(result.StorageProof))
	}

	// Slot 1: should have value 100.
	sp1 := result.StorageProof[0]
	if sp1.Key != slot1 {
		t.Fatalf("storage proof 0 key mismatch")
	}
	if sp1.Value.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("storage proof 0 value = %s, want 100", sp1.Value)
	}

	// Verify slot 1 proof against storage root.
	sval, err := VerifyProof(storageRoot, slot1Hash, sp1.Proof)
	if err != nil {
		t.Fatalf("VerifyProof slot1 error: %v", err)
	}
	if new(big.Int).SetBytes(sval).Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("slot1 proof value mismatch")
	}

	// Slot 3: should be absent (value 0).
	sp3 := result.StorageProof[1]
	if sp3.Key != slot3 {
		t.Fatalf("storage proof 1 key mismatch")
	}
	if sp3.Value.Sign() != 0 {
		t.Fatalf("storage proof 1 value = %s, want 0", sp3.Value)
	}

	// Verify slot 3 absence proof.
	slot3Hash := crypto.Keccak256(slot3[:])
	sval, err = VerifyProof(storageRoot, slot3Hash, sp3.Proof)
	if err != nil {
		t.Fatalf("VerifyProof slot3 absence error: %v", err)
	}
	if sval != nil {
		t.Fatalf("expected nil for absent slot3, got %x", sval)
	}
}

func TestProveAccountWithStorage_NilStorageTrie(t *testing.T) {
	stateTrie := New()

	addr := types.HexToAddress("0x1234567890123456789012345678901234567890")
	account := &types.Account{
		Nonce:    0,
		Balance:  big.NewInt(0),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	accountRLP, _ := EncodeAccount(account)
	stateTrie.Put(crypto.Keccak256(addr[:]), accountRLP)

	slot1 := types.HexToHash("0x01")
	result, err := ProveAccountWithStorage(stateTrie, addr, nil, []types.Hash{slot1})
	if err != nil {
		t.Fatalf("ProveAccountWithStorage error: %v", err)
	}

	if len(result.StorageProof) != 1 {
		t.Fatalf("expected 1 storage proof, got %d", len(result.StorageProof))
	}
	if result.StorageProof[0].Value.Sign() != 0 {
		t.Fatalf("expected zero value for nil storage trie")
	}
}

// ============================================================
// EncodeAccount / decodeAccount round-trip
// ============================================================

func TestEncodeDecodeAccount(t *testing.T) {
	account := &types.Account{
		Nonce:    42,
		Balance:  big.NewInt(123456789),
		Root:     types.HexToHash("0xabcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	encoded, err := EncodeAccount(account)
	if err != nil {
		t.Fatalf("EncodeAccount error: %v", err)
	}

	decoded, err := decodeAccount(encoded)
	if err != nil {
		t.Fatalf("decodeAccount error: %v", err)
	}

	if decoded.Nonce != account.Nonce {
		t.Fatalf("nonce = %d, want %d", decoded.Nonce, account.Nonce)
	}
	if decoded.Balance.Cmp(account.Balance) != 0 {
		t.Fatalf("balance = %s, want %s", decoded.Balance, account.Balance)
	}
	if decoded.Root != account.Root {
		t.Fatalf("root mismatch")
	}
	if !bytes.Equal(decoded.CodeHash, account.CodeHash) {
		t.Fatalf("code hash mismatch")
	}
}

// ============================================================
// Tampered proof detection (comprehensive)
// ============================================================

func TestVerifyProof_TamperedLastNode(t *testing.T) {
	tr := New()
	for i := 0; i < 20; i++ {
		tr.Put([]byte(fmt.Sprintf("key%02d", i)), []byte(fmt.Sprintf("val%02d", i)))
	}

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("key05"))

	// Tamper with the last proof node.
	tampered := make([][]byte, len(proof))
	for i := range proof {
		tampered[i] = make([]byte, len(proof[i]))
		copy(tampered[i], proof[i])
	}
	last := len(tampered) - 1
	if len(tampered[last]) > 2 {
		tampered[last][2] ^= 0x42
	}

	_, err := VerifyProof(root, []byte("key05"), tampered)
	if err == nil {
		t.Fatal("VerifyProof should fail with tampered last node")
	}
}

func TestVerifyProof_TruncatedProof(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("dogglesworth"))

	if len(proof) < 2 {
		t.Skip("proof too short to truncate")
	}

	// Remove the last node, making the proof incomplete.
	truncated := proof[:len(proof)-1]
	_, err := VerifyProof(root, []byte("dogglesworth"), truncated)
	if err == nil {
		t.Fatal("VerifyProof should fail with truncated proof")
	}
}

func TestVerifyProof_ExtraNode(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("key"))

	// Append a garbage node.
	extended := append(proof, []byte{0xc0})
	val, err := VerifyProof(root, []byte("key"), extended)
	// The proof should either succeed with the correct value (ignoring extra)
	// or fail. It should NOT return a wrong value.
	if err == nil && string(val) != "value" {
		t.Fatalf("unexpected value: %q", val)
	}
}

func TestVerifyProof_SwappedNodes(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	root := tr.Hash()
	proof, _ := tr.Prove([]byte("dogglesworth"))

	if len(proof) < 2 {
		t.Skip("proof too short to swap")
	}

	// Swap two nodes.
	swapped := make([][]byte, len(proof))
	copy(swapped, proof)
	swapped[0], swapped[1] = swapped[1], swapped[0]

	_, err := VerifyProof(root, []byte("dogglesworth"), swapped)
	if err == nil {
		t.Fatal("VerifyProof should fail with swapped nodes")
	}
}

// ============================================================
// Multiple key proofs from same trie (large scale)
// ============================================================

func TestProve_MultipleKeysLargeScale(t *testing.T) {
	rng := rand.New(rand.NewSource(99999))
	tr := New()
	keys := make([][]byte, 50)

	for i := range keys {
		keyLen := rng.Intn(20) + 1
		key := make([]byte, keyLen)
		rng.Read(key)
		val := make([]byte, rng.Intn(50)+1)
		rng.Read(val)
		tr.Put(key, val)
		keys[i] = key
	}

	root := tr.Hash()

	// Generate and verify all proofs.
	for _, key := range keys {
		proof, err := tr.Prove(key)
		if err != nil {
			t.Fatalf("Prove(%x) error: %v", key, err)
		}

		val, err := VerifyProof(root, key, proof)
		if err != nil {
			t.Fatalf("VerifyProof(%x) error: %v", key, err)
		}

		expected, _ := tr.Get(key)
		if !bytes.Equal(val, expected) {
			t.Fatalf("VerifyProof(%x) value mismatch", key)
		}
	}
}

func TestProve_MultipleAbsenceProofsFromSameTrie(t *testing.T) {
	tr := New()
	tr.Put([]byte("alpha"), []byte("1"))
	tr.Put([]byte("bravo"), []byte("2"))
	tr.Put([]byte("charlie"), []byte("3"))

	root := tr.Hash()

	absentKeys := []string{"delta", "echo", "foxtrot", "golf", "hotel"}
	for _, k := range absentKeys {
		proof, err := tr.ProveAbsence([]byte(k))
		if err != nil {
			t.Fatalf("ProveAbsence(%q) error: %v", k, err)
		}

		val, err := VerifyProof(root, []byte(k), proof)
		if err != nil {
			t.Fatalf("VerifyProof absence(%q) error: %v", k, err)
		}
		if val != nil {
			t.Fatalf("expected nil for absent key %q, got %x", k, val)
		}
	}
}
