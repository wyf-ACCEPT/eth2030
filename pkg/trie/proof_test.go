package trie

import (
	"bytes"
	"fmt"
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
	root := types.Hash{1, 2, 3}
	_, err := VerifyProof(root, []byte("key"), nil)
	if err != ErrProofInvalid {
		t.Fatalf("VerifyProof(nil proof) err = %v, want ErrProofInvalid", err)
	}

	_, err = VerifyProof(root, []byte("key"), [][]byte{})
	if err != ErrProofInvalid {
		t.Fatalf("VerifyProof(empty proof) err = %v, want ErrProofInvalid", err)
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
