package trie

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// -- Basic CRUD --

func TestBinaryTrie_EmptyTrie(t *testing.T) {
	bt := NewBinaryTrie()
	if !bt.Empty() {
		t.Fatal("new binary trie should be empty")
	}
	if bt.Len() != 0 {
		t.Fatalf("Len = %d, want 0", bt.Len())
	}
	if bt.Hash() != zeroHash {
		t.Fatalf("empty trie hash = %s, want zero hash", bt.Hash().Hex())
	}
}

func TestBinaryTrie_PutGet(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("hello"), []byte("world"))

	got, err := bt.Get([]byte("hello"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if string(got) != "world" {
		t.Fatalf("Get = %q, want %q", got, "world")
	}
}

func TestBinaryTrie_GetNonExistent(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("exists"), []byte("yes"))

	_, err := bt.Get([]byte("missing"))
	if err != ErrNotFound {
		t.Fatalf("Get(missing) err = %v, want ErrNotFound", err)
	}
}

func TestBinaryTrie_GetEmptyTrie(t *testing.T) {
	bt := NewBinaryTrie()
	_, err := bt.Get([]byte("anything"))
	if err != ErrNotFound {
		t.Fatalf("Get on empty trie: err = %v, want ErrNotFound", err)
	}
}

func TestBinaryTrie_Update(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("v1"))
	bt.Put([]byte("key"), []byte("v2"))

	got, err := bt.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if string(got) != "v2" {
		t.Fatalf("Get = %q, want %q", got, "v2")
	}
	if bt.Len() != 1 {
		t.Fatalf("Len = %d, want 1", bt.Len())
	}
}

func TestBinaryTrie_Delete(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("val"))
	bt.Delete([]byte("key"))

	_, err := bt.Get([]byte("key"))
	if err != ErrNotFound {
		t.Fatalf("Get after Delete: err = %v, want ErrNotFound", err)
	}
	if !bt.Empty() {
		t.Fatal("trie should be empty after deleting only key")
	}
}

func TestBinaryTrie_DeleteNonExistent(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("val"))
	h1 := bt.Hash()

	bt.Delete([]byte("nonexistent"))

	if bt.Hash() != h1 {
		t.Fatal("hash changed after deleting non-existent key")
	}
}

func TestBinaryTrie_DeleteAllKeys(t *testing.T) {
	bt := NewBinaryTrie()
	keys := []string{"alpha", "beta", "gamma", "delta"}
	for _, k := range keys {
		bt.Put([]byte(k), []byte("val"))
	}
	for _, k := range keys {
		bt.Delete([]byte(k))
	}
	if !bt.Empty() {
		t.Fatal("trie not empty after deleting all keys")
	}
	if bt.Hash() != zeroHash {
		t.Fatal("hash not zero after deleting all keys")
	}
}

func TestBinaryTrie_PutNilValueDeletes(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("val"))
	bt.Put([]byte("key"), nil)

	_, err := bt.Get([]byte("key"))
	if err != ErrNotFound {
		t.Fatalf("Get after Put(nil): err = %v, want ErrNotFound", err)
	}
}

func TestBinaryTrie_PutEmptyValueDeletes(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("val"))
	bt.Put([]byte("key"), []byte{})

	_, err := bt.Get([]byte("key"))
	if err != ErrNotFound {
		t.Fatalf("Get after Put(empty): err = %v, want ErrNotFound", err)
	}
}

// -- Hash consistency --

func TestBinaryTrie_HashDeterministic(t *testing.T) {
	bt1 := NewBinaryTrie()
	bt1.Put([]byte("a"), []byte("1"))
	bt1.Put([]byte("b"), []byte("2"))
	bt1.Put([]byte("c"), []byte("3"))

	bt2 := NewBinaryTrie()
	bt2.Put([]byte("c"), []byte("3"))
	bt2.Put([]byte("a"), []byte("1"))
	bt2.Put([]byte("b"), []byte("2"))

	if bt1.Hash() != bt2.Hash() {
		t.Fatal("different insertion order produced different root hashes")
	}
}

func TestBinaryTrie_HashNotEmpty(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	if bt.Hash() == zeroHash {
		t.Fatal("non-empty trie should not have zero hash")
	}
}

func TestBinaryTrie_HashStable(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	h1 := bt.Hash()
	h2 := bt.Hash()
	if h1 != h2 {
		t.Fatal("repeated Hash() calls return different values")
	}
}

func TestBinaryTrie_HashChangesAfterPut(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("k1"), []byte("v1"))
	h1 := bt.Hash()
	bt.Put([]byte("k2"), []byte("v2"))
	if h1 == bt.Hash() {
		t.Fatal("hash did not change after inserting new key")
	}
}

func TestBinaryTrie_HashChangesAfterDelete(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("k1"), []byte("v1"))
	bt.Put([]byte("k2"), []byte("v2"))
	h1 := bt.Hash()
	bt.Delete([]byte("k1"))
	if h1 == bt.Hash() {
		t.Fatal("hash did not change after delete")
	}
}

// -- Proof generation and verification --

func TestBinaryTrie_ProveAndVerify(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("alice"), []byte("balance:100"))
	bt.Put([]byte("bob"), []byte("balance:200"))
	bt.Put([]byte("charlie"), []byte("balance:300"))

	root := bt.Hash()

	proof, err := bt.Prove([]byte("bob"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	if err := VerifyBinaryProof(root, proof); err != nil {
		t.Fatalf("VerifyBinaryProof error: %v", err)
	}
	if string(proof.Value) != "balance:200" {
		t.Fatalf("proof value = %q, want %q", proof.Value, "balance:200")
	}
}

func TestBinaryTrie_ProveNonExistent(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("exists"), []byte("yes"))

	_, err := bt.Prove([]byte("missing"))
	if err != ErrNotFound {
		t.Fatalf("Prove(missing) err = %v, want ErrNotFound", err)
	}
}

func TestBinaryTrie_ProofTampered(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("key"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}

	// Tamper with the value.
	proof.Value = []byte("tampered")
	if err := VerifyBinaryProof(root, proof); err == nil {
		t.Fatal("tampered proof should fail verification")
	}
}

func TestBinaryTrie_ProofWrongRoot(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	bt.Hash()

	proof, err := bt.Prove([]byte("key"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}

	wrongRoot := types.Hash{0xff}
	if err := VerifyBinaryProof(wrongRoot, proof); err == nil {
		t.Fatal("proof with wrong root should fail verification")
	}
}

func TestBinaryTrie_ProofSize(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("key"), []byte("value"))
	bt.Hash()

	proof, err := bt.Prove([]byte("key"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	size := proof.ProofSize()
	// Proof size = siblings*32 + key(32) + value(5 bytes)
	expected := len(proof.Siblings)*32 + 32 + len(proof.Value)
	if size != expected {
		t.Fatalf("ProofSize = %d, want %d", size, expected)
	}
}

func TestBinaryTrie_ProveVerifySingleEntry(t *testing.T) {
	bt := NewBinaryTrie()
	bt.Put([]byte("only"), []byte("one"))
	root := bt.Hash()

	proof, err := bt.Prove([]byte("only"))
	if err != nil {
		t.Fatalf("Prove error: %v", err)
	}
	// Single entry trie: no siblings needed.
	if len(proof.Siblings) != 0 {
		t.Fatalf("single-entry proof should have 0 siblings, got %d", len(proof.Siblings))
	}
	if err := VerifyBinaryProof(root, proof); err != nil {
		t.Fatalf("VerifyBinaryProof error: %v", err)
	}
}

// -- Iterator --

func TestBinaryIterator_EmptyTrie(t *testing.T) {
	bt := NewBinaryTrie()
	it := NewBinaryIterator(bt)
	if it.Next() {
		t.Fatal("iterator on empty trie should not advance")
	}
}

func TestBinaryIterator_AllEntries(t *testing.T) {
	bt := NewBinaryTrie()
	entries := map[string]string{
		"alice": "100", "bob": "200", "charlie": "300",
		"dave": "400", "eve": "500",
	}
	for k, v := range entries {
		bt.Put([]byte(k), []byte(v))
	}

	found := make(map[string]string)
	it := NewBinaryIterator(bt)
	for it.Next() {
		// Keys in the binary trie are hashed, so we need to match by
		// checking that the hash of each original key matches.
		hk := types.BytesToHash(it.Key())
		for k, v := range entries {
			if crypto.Keccak256Hash([]byte(k)) == hk {
				found[k] = string(it.Value())
				if string(it.Value()) != v {
					t.Fatalf("iterator value for %q = %q, want %q", k, it.Value(), v)
				}
			}
		}
	}
	if len(found) != len(entries) {
		t.Fatalf("iterator found %d entries, want %d", len(found), len(entries))
	}
}

// -- Large dataset --

func TestBinaryTrie_LargeDataset(t *testing.T) {
	bt := NewBinaryTrie()
	n := 1000
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		val := []byte(fmt.Sprintf("val-%04d", i))
		bt.Put(key, val)
	}
	if bt.Len() != n {
		t.Fatalf("Len = %d, want %d", bt.Len(), n)
	}

	// Verify all entries can be retrieved.
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		val := []byte(fmt.Sprintf("val-%04d", i))
		got, err := bt.Get(key)
		if err != nil {
			t.Fatalf("Get(%s) error: %v", key, err)
		}
		if !bytes.Equal(got, val) {
			t.Fatalf("Get(%s) = %q, want %q", key, got, val)
		}
	}

	// Hash should be deterministic.
	h1 := bt.Hash()
	h2 := bt.Hash()
	if h1 != h2 {
		t.Fatal("hash not deterministic on large dataset")
	}

	// Verify proofs for a sample.
	root := bt.Hash()
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i*100))
		proof, err := bt.Prove(key)
		if err != nil {
			t.Fatalf("Prove(%s) error: %v", key, err)
		}
		if err := VerifyBinaryProof(root, proof); err != nil {
			t.Fatalf("VerifyBinaryProof(%s) error: %v", key, err)
		}
	}

	// Delete half and verify remaining.
	for i := 0; i < n/2; i++ {
		bt.Delete([]byte(fmt.Sprintf("key-%04d", i)))
	}
	if bt.Len() != n/2 {
		t.Fatalf("Len after deleting half = %d, want %d", bt.Len(), n/2)
	}
	for i := n / 2; i < n; i++ {
		key := []byte(fmt.Sprintf("key-%04d", i))
		val := []byte(fmt.Sprintf("val-%04d", i))
		got, err := bt.Get(key)
		if err != nil {
			t.Fatalf("Get(%s) error after partial delete: %v", key, err)
		}
		if !bytes.Equal(got, val) {
			t.Fatalf("Get(%s) = %q, want %q after partial delete", key, got, val)
		}
	}
}

// -- Large values --

func TestBinaryTrie_LargeValue(t *testing.T) {
	bt := NewBinaryTrie()
	largeVal := bytes.Repeat([]byte{0x42}, 4096)
	bt.Put([]byte("big"), largeVal)

	got, err := bt.Get([]byte("big"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !bytes.Equal(got, largeVal) {
		t.Fatal("large value mismatch")
	}
}

// -- Migration from MPT --

func TestMigrateFromMPT(t *testing.T) {
	mpt := New()
	entries := map[string]string{
		"alice": "100", "bob": "200", "charlie": "300",
	}
	for k, v := range entries {
		mpt.Put([]byte(k), []byte(v))
	}

	bt := MigrateFromMPT(mpt)

	if bt.Len() != len(entries) {
		t.Fatalf("migrated Len = %d, want %d", bt.Len(), len(entries))
	}

	// Verify all entries were migrated. The MPT iterator returns raw keys,
	// and MigrateFromMPT hashes them, so we look up by hashed key.
	for k, v := range entries {
		hk := crypto.Keccak256Hash([]byte(k))
		got, err := bt.GetHashed(hk)
		if err != nil {
			t.Fatalf("GetHashed(%s) error: %v", k, err)
		}
		if string(got) != v {
			t.Fatalf("GetHashed(%s) = %q, want %q", k, got, v)
		}
	}

	// Hash should be non-zero.
	if bt.Hash() == zeroHash {
		t.Fatal("migrated trie should not have zero hash")
	}
}

func TestMigrateFromMPT_EmptyTrie(t *testing.T) {
	mpt := New()
	bt := MigrateFromMPT(mpt)
	if !bt.Empty() {
		t.Fatal("migrated empty MPT should be empty binary trie")
	}
}

// -- getBit helper --

func TestGetBit(t *testing.T) {
	// 0x80 = 10000000 in binary
	h := types.Hash{0x80}
	if getBit(h, 0) != 1 {
		t.Fatal("bit 0 of 0x80 should be 1")
	}
	if getBit(h, 1) != 0 {
		t.Fatal("bit 1 of 0x80 should be 0")
	}

	// 0x01 = 00000001
	h = types.Hash{0x01}
	if getBit(h, 7) != 1 {
		t.Fatal("bit 7 of 0x01 should be 1")
	}
	if getBit(h, 0) != 0 {
		t.Fatal("bit 0 of 0x01 should be 0")
	}

	// 0xFF = 11111111
	h = types.Hash{0xFF}
	for i := 0; i < 8; i++ {
		if getBit(h, i) != 1 {
			t.Fatalf("bit %d of 0xFF should be 1", i)
		}
	}
}

// -- Hashed key operations --

func TestBinaryTrie_HashedKeyOps(t *testing.T) {
	bt := NewBinaryTrie()
	hk := crypto.Keccak256Hash([]byte("test"))

	bt.PutHashed(hk, []byte("value"))
	got, err := bt.GetHashed(hk)
	if err != nil {
		t.Fatalf("GetHashed error: %v", err)
	}
	if string(got) != "value" {
		t.Fatalf("GetHashed = %q, want %q", got, "value")
	}

	bt.DeleteHashed(hk)
	_, err = bt.GetHashed(hk)
	if err != ErrNotFound {
		t.Fatalf("GetHashed after DeleteHashed: err = %v, want ErrNotFound", err)
	}
}

// -- Proof with many entries (deep tree) --

func TestBinaryTrie_ProofDeepTree(t *testing.T) {
	bt := NewBinaryTrie()
	for i := 0; i < 100; i++ {
		bt.Put([]byte(fmt.Sprintf("entry-%d", i)), []byte(fmt.Sprintf("val-%d", i)))
	}
	root := bt.Hash()

	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("entry-%d", i))
		proof, err := bt.Prove(key)
		if err != nil {
			t.Fatalf("Prove(%s) error: %v", key, err)
		}
		if err := VerifyBinaryProof(root, proof); err != nil {
			t.Fatalf("VerifyBinaryProof(%s) error: %v", key, err)
		}
	}
}

// -- Verify nil proof --

func TestBinaryTrie_VerifyNilProof(t *testing.T) {
	if err := VerifyBinaryProof(types.Hash{}, nil); err == nil {
		t.Fatal("nil proof should fail verification")
	}
}
