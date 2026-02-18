package trie

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// -- Known Ethereum test vectors (from go-ethereum) --

func TestEmptyTrie(t *testing.T) {
	tr := New()
	got := tr.Hash()
	if got != emptyRoot {
		t.Fatalf("empty trie hash = %s, want %s", got.Hex(), emptyRoot.Hex())
	}
	if got != types.EmptyRootHash {
		t.Fatalf("empty trie hash does not match types.EmptyRootHash")
	}
}

func TestInsert_GethVector1(t *testing.T) {
	// From go-ethereum TestInsert case 1.
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	exp := types.HexToHash("8aad789dff2f538bca5d8ea56e8abe10f4c7ba3a5dea95fea4cd6e7c3a1168d3")
	got := tr.Hash()
	if got != exp {
		t.Fatalf("root = %s, want %s", got.Hex(), exp.Hex())
	}
}

func TestInsert_GethVector2(t *testing.T) {
	// From go-ethereum TestInsert case 2: single key with long value.
	tr := New()
	tr.Put([]byte("A"), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))

	exp := types.HexToHash("d23786fb4a010da3ce639d66d5e904a11dbc02746d1ce25029e53290cabf28ab")
	got := tr.Hash()
	if got != exp {
		t.Fatalf("root = %s, want %s", got.Hex(), exp.Hex())
	}
}

func TestDelete_GethVector(t *testing.T) {
	// From go-ethereum TestDelete / TestEmptyValues.
	tr := New()
	tr.Put([]byte("do"), []byte("verb"))
	tr.Put([]byte("ether"), []byte("wookiedoo"))
	tr.Put([]byte("horse"), []byte("stallion"))
	tr.Put([]byte("shaman"), []byte("horse"))
	tr.Put([]byte("doge"), []byte("coin"))
	tr.Delete([]byte("ether"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Delete([]byte("shaman"))

	exp := types.HexToHash("5991bb8c6514148a29db676a14ac506cd2cd5775ace63c30a4fe457715e9ac84")
	got := tr.Hash()
	if got != exp {
		t.Fatalf("root = %s, want %s", got.Hex(), exp.Hex())
	}
}

func TestEmptyValues_GethVector(t *testing.T) {
	// TestEmptyValues from go-ethereum: setting value to "" is equivalent to Delete.
	tr := New()
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"ether", ""},
		{"dog", "puppy"},
		{"shaman", ""},
	}
	for _, val := range vals {
		if val.v != "" {
			tr.Put([]byte(val.k), []byte(val.v))
		} else {
			tr.Put([]byte(val.k), nil) // Put with empty value => delete
		}
	}

	exp := types.HexToHash("5991bb8c6514148a29db676a14ac506cd2cd5775ace63c30a4fe457715e9ac84")
	got := tr.Hash()
	if got != exp {
		t.Fatalf("root = %s, want %s", got.Hex(), exp.Hex())
	}
}

// -- Get operations --

func TestGet_ExistingKeys(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("dogglesworth"), []byte("cat"))

	tests := []struct {
		key  string
		want string
	}{
		{"doe", "reindeer"},
		{"dog", "puppy"},
		{"dogglesworth", "cat"},
	}
	for _, tt := range tests {
		got, err := tr.Get([]byte(tt.key))
		if err != nil {
			t.Errorf("Get(%q) error: %v", tt.key, err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("Get(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestGet_NonExistentKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("doe"), []byte("reindeer"))

	_, err := tr.Get([]byte("unknown"))
	if err != ErrNotFound {
		t.Fatalf("Get(unknown) err = %v, want ErrNotFound", err)
	}
}

func TestGet_EmptyTrie(t *testing.T) {
	tr := New()
	_, err := tr.Get([]byte("anything"))
	if err != ErrNotFound {
		t.Fatalf("Get on empty trie: err = %v, want ErrNotFound", err)
	}
}

// -- Put operations --

func TestPut_UpdateExistingKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value1"))
	tr.Put([]byte("key"), []byte("value2"))

	got, err := tr.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if string(got) != "value2" {
		t.Fatalf("Get(key) = %q, want %q", got, "value2")
	}
}

func TestPut_NilValueDeletes(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))
	tr.Put([]byte("key"), nil)

	_, err := tr.Get([]byte("key"))
	if err != ErrNotFound {
		t.Fatalf("Get after Put(nil) err = %v, want ErrNotFound", err)
	}
	if tr.Hash() != emptyRoot {
		t.Fatal("trie not empty after deleting only key")
	}
}

func TestPut_EmptyValueDeletes(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))
	tr.Put([]byte("key"), []byte{})

	_, err := tr.Get([]byte("key"))
	if err != ErrNotFound {
		t.Fatalf("Get after Put(empty) err = %v, want ErrNotFound", err)
	}
}

// -- Delete operations --

func TestDelete_ExistingKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))
	if err := tr.Delete([]byte("key")); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	_, err := tr.Get([]byte("key"))
	if err != ErrNotFound {
		t.Fatalf("Get after Delete err = %v, want ErrNotFound", err)
	}
}

func TestDelete_NonExistentKey(t *testing.T) {
	tr := New()
	tr.Put([]byte("hello"), []byte("world"))
	h1 := tr.Hash()

	if err := tr.Delete([]byte("nonexistent")); err != nil {
		t.Fatalf("Delete non-existent error: %v", err)
	}
	if h2 := tr.Hash(); h1 != h2 {
		t.Fatalf("hash changed after deleting non-existent key")
	}
}

func TestDelete_EmptyTrie(t *testing.T) {
	tr := New()
	if err := tr.Delete([]byte("anything")); err != nil {
		t.Fatalf("Delete on empty trie error: %v", err)
	}
	if tr.Hash() != emptyRoot {
		t.Fatal("empty trie hash changed after delete")
	}
}

func TestDelete_AllKeys(t *testing.T) {
	tr := New()
	keys := []string{"do", "dog", "doge", "horse"}
	for _, k := range keys {
		tr.Put([]byte(k), []byte("val"))
	}
	for _, k := range keys {
		tr.Delete([]byte(k))
	}
	if tr.Hash() != emptyRoot {
		t.Fatal("trie not empty after deleting all keys")
	}
}

// -- Root hash consistency --

func TestHash_Deterministic(t *testing.T) {
	tr1 := New()
	tr1.Put([]byte("a"), []byte("1"))
	tr1.Put([]byte("b"), []byte("2"))
	tr1.Put([]byte("c"), []byte("3"))

	tr2 := New()
	tr2.Put([]byte("c"), []byte("3"))
	tr2.Put([]byte("a"), []byte("1"))
	tr2.Put([]byte("b"), []byte("2"))

	if tr1.Hash() != tr2.Hash() {
		t.Fatal("different insertion order produced different root hashes")
	}
}

func TestHash_NotAffectedByGetOrRepeatedHash(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("value"))
	h1 := tr.Hash()

	tr.Get([]byte("key"))
	tr.Get([]byte("nonexistent"))
	h2 := tr.Hash()
	h3 := tr.Hash()

	if h1 != h2 || h2 != h3 {
		t.Fatal("root hash changed after Get or repeated Hash call")
	}
}

func TestHash_ChangesAfterPut(t *testing.T) {
	tr := New()
	tr.Put([]byte("key1"), []byte("val1"))
	h1 := tr.Hash()
	tr.Put([]byte("key2"), []byte("val2"))
	if h1 == tr.Hash() {
		t.Fatal("root hash did not change after inserting new key")
	}
}

func TestHash_ChangesAfterDelete(t *testing.T) {
	tr := New()
	tr.Put([]byte("key1"), []byte("val1"))
	tr.Put([]byte("key2"), []byte("val2"))
	h1 := tr.Hash()
	tr.Delete([]byte("key1"))
	if h1 == tr.Hash() {
		t.Fatal("root hash did not change after delete")
	}
}

// -- Overlapping prefix tests (branch node with value) --

func TestOverlappingPrefixes(t *testing.T) {
	tr := New()
	tr.Put([]byte("do"), []byte("verb"))
	tr.Put([]byte("dog"), []byte("puppy"))
	tr.Put([]byte("doge"), []byte("coin"))

	for _, tt := range []struct{ key, want string }{
		{"do", "verb"}, {"dog", "puppy"}, {"doge", "coin"},
	} {
		got, err := tr.Get([]byte(tt.key))
		if err != nil || string(got) != tt.want {
			t.Errorf("Get(%q) = %q, %v; want %q", tt.key, got, err, tt.want)
		}
	}

	// Delete middle key, verify others still work.
	tr.Delete([]byte("dog"))
	got, err := tr.Get([]byte("do"))
	if err != nil || string(got) != "verb" {
		t.Fatalf("Get(do) after delete dog: %q, %v", got, err)
	}
	got, err = tr.Get([]byte("doge"))
	if err != nil || string(got) != "coin" {
		t.Fatalf("Get(doge) after delete dog: %q, %v", got, err)
	}
}

// -- Large value and replication tests --

func TestLargeValue(t *testing.T) {
	tr := New()
	largeVal := bytes.Repeat([]byte{0x42}, 1024)
	tr.Put([]byte("key"), largeVal)

	got, err := tr.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !bytes.Equal(got, largeVal) {
		t.Fatal("large value mismatch")
	}
}

func TestReplication(t *testing.T) {
	tr := New()
	entries := []struct{ k, v string }{
		{"do", "verb"}, {"ether", "wookiedoo"}, {"horse", "stallion"},
		{"shaman", "horse"}, {"doge", "coin"}, {"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
	}
	for _, e := range entries {
		tr.Put([]byte(e.k), []byte(e.v))
	}
	h1 := tr.Hash()

	// Re-insert same entries; hash should not change.
	for _, e := range entries {
		tr.Put([]byte(e.k), []byte(e.v))
	}
	if h2 := tr.Hash(); h1 != h2 {
		t.Fatalf("hash changed after reinserting same entries: %s vs %s", h1.Hex(), h2.Hex())
	}
}

func TestWikiVector_SinglePair(t *testing.T) {
	tr := New()
	tr.Put([]byte("do"), []byte("verb"))
	if h := tr.Hash(); h == emptyRoot {
		t.Fatal("single-pair trie should not be empty")
	}
	got, err := tr.Get([]byte("do"))
	if err != nil || string(got) != "verb" {
		t.Fatalf("Get(do) = %q, %v", got, err)
	}
}

// -- Specific hex key vectors from go-ethereum fuzzer --

func TestSpecificHexKeys(t *testing.T) {
	tr := New()
	key1, _ := hex.DecodeString("d51b182b95d677e5f1c82508c0228de96b73092d78ce78b2230cd948674f66fd1483bd")
	key2, _ := hex.DecodeString("c2a38512b83107d665c65235b0250002882ac2022eb00711552354832c5f1d030d0e408e")

	tr.Put(key1, []byte{0, 0, 0, 0, 0, 0, 0, 2})
	tr.Put(key2, []byte{0, 0, 0, 0, 0, 0, 0, 8})
	tr.Put(key1, []byte{0, 0, 0, 0, 0, 0, 0, 9})

	got, err := tr.Get(key1)
	if err != nil || !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0, 9}) {
		t.Fatalf("Get(key1) = %x, err=%v", got, err)
	}
	got, err = tr.Get(key2)
	if err != nil || !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0, 8}) {
		t.Fatalf("Get(key2) = %x, err=%v", got, err)
	}

	tr.Delete(key2)
	if _, err = tr.Get(key2); err != ErrNotFound {
		t.Fatal("key2 should be deleted")
	}
	tr.Put(key2, []byte{0, 0, 0, 0, 0, 0, 0, 0x11})
	got, _ = tr.Get(key2)
	if !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0, 0x11}) {
		t.Fatalf("Get(key2) after re-insert = %x", got)
	}
}

// -- Binary and hex key coverage --

func TestBinaryKeys(t *testing.T) {
	tr := New()
	keys := [][]byte{
		{0x00}, {0x00, 0x01}, {0x00, 0x01, 0x02},
		{0xff}, {0xff, 0xfe}, {0x80, 0x00, 0x00},
	}
	for i, k := range keys {
		tr.Put(k, []byte(fmt.Sprintf("val%d", i)))
	}
	for i, k := range keys {
		got, err := tr.Get(k)
		if err != nil {
			t.Fatalf("Get(%x) error: %v", k, err)
		}
		if want := fmt.Sprintf("val%d", i); string(got) != want {
			t.Fatalf("Get(%x) = %q, want %q", k, got, want)
		}
	}
}

func TestHexEncodedKeys(t *testing.T) {
	tr := New()
	for i := 0; i < 16; i++ {
		tr.Put([]byte{byte(i << 4)}, []byte{byte(i)})
	}
	if tr.Hash() == emptyRoot {
		t.Fatal("trie should not be empty")
	}
	for i := 0; i < 16; i++ {
		key := []byte{byte(i << 4)}
		got, err := tr.Get(key)
		if err != nil || !bytes.Equal(got, []byte{byte(i)}) {
			t.Fatalf("Get(%x) = %x, err=%v", key, got, err)
		}
	}
}

func TestSingleByteKeys(t *testing.T) {
	tr := New()
	for i := 0; i < 256; i++ {
		tr.Put([]byte{byte(i)}, []byte{byte(i), byte(i)})
	}
	if tr.Hash() == emptyRoot {
		t.Fatal("trie with 256 keys should not be empty")
	}
	for i := 0; i < 256; i++ {
		got, err := tr.Get([]byte{byte(i)})
		if err != nil || !bytes.Equal(got, []byte{byte(i), byte(i)}) {
			t.Fatalf("Get(%02x) = %x, err=%v", i, got, err)
		}
	}
}

// -- Transaction trie root test (simulates block_builder usage) --

func TestTransactionTrieRoot(t *testing.T) {
	tr := New()
	for i := 0; i < 10; i++ {
		var key []byte
		if i == 0 {
			key = []byte{0x80} // RLP of 0
		} else {
			key = []byte{byte(i)} // RLP of 1..127
		}
		tr.Put(key, bytes.Repeat([]byte{byte(i)}, 100))
	}
	if tr.Hash() == emptyRoot {
		t.Fatal("transaction trie should not be empty")
	}
	for i := 0; i < 10; i++ {
		var key []byte
		if i == 0 {
			key = []byte{0x80}
		} else {
			key = []byte{byte(i)}
		}
		if _, err := tr.Get(key); err != nil {
			t.Fatalf("Get(tx %d) error: %v", i, err)
		}
	}
}

// -- Edge cases --

func TestEmptyTrieRoot_MatchesYellowPaper(t *testing.T) {
	// Per the Yellow Paper, the empty trie root is keccak256(RLP("")).
	// RLP("") = 0x80, so emptyRoot = keccak256([0x80]).
	// Expected: 56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421
	tr := New()
	got := tr.Hash()
	exp := types.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")
	if got != exp {
		t.Fatalf("empty root = %s, want %s", got.Hex(), exp.Hex())
	}
}

func TestSingleNodeTrie_SmallRootForceHashed(t *testing.T) {
	// Even if the root node's RLP is < 32 bytes, Hash() should still
	// return a proper 32-byte hash (force=true for root).
	tr := New()
	tr.Put([]byte{0x01}, []byte{0x02})

	root := tr.Hash()
	if root == emptyRoot {
		t.Fatal("single-entry trie root should not be emptyRoot")
	}
	if root == (types.Hash{}) {
		t.Fatal("root should not be zero hash")
	}
	// Verify the hash is 32 bytes and deterministic.
	if root != tr.Hash() {
		t.Fatal("hash not deterministic")
	}
}

func TestExtensionNodeCompaction_AfterDelete(t *testing.T) {
	// After deleting a key that causes a branch node to collapse, the
	// resulting extension node should be properly compacted (merged
	// with its child short node).
	tr := New()
	tr.Put([]byte("abc"), []byte("1"))
	tr.Put([]byte("abd"), []byte("2"))
	tr.Put([]byte("xyz"), []byte("3"))

	// Deleting "xyz" should leave a branch that may collapse.
	tr.Delete([]byte("xyz"))

	// Both remaining keys should be accessible.
	got, err := tr.Get([]byte("abc"))
	if err != nil || string(got) != "1" {
		t.Fatalf("Get(abc) = %q, %v", got, err)
	}
	got, err = tr.Get([]byte("abd"))
	if err != nil || string(got) != "2" {
		t.Fatalf("Get(abd) = %q, %v", got, err)
	}

	// Build same final state from scratch and compare hashes.
	tr2 := New()
	tr2.Put([]byte("abc"), []byte("1"))
	tr2.Put([]byte("abd"), []byte("2"))
	if tr.Hash() != tr2.Hash() {
		t.Fatalf("hash after delete differs from fresh build: %s vs %s",
			tr.Hash().Hex(), tr2.Hash().Hex())
	}
}

func TestExtensionNodeCompaction_DeepMerge(t *testing.T) {
	// Create a deep trie and delete entries to trigger deep merging.
	tr := New()
	tr.Put([]byte("abcdef"), []byte("1"))
	tr.Put([]byte("abcdeg"), []byte("2"))
	tr.Put([]byte("abcdeh"), []byte("3"))

	// Delete two of three, leaving a single leaf. The branch at "abcde"
	// should collapse into a merged extension+leaf.
	tr.Delete([]byte("abcdeg"))
	tr.Delete([]byte("abcdeh"))

	got, err := tr.Get([]byte("abcdef"))
	if err != nil || string(got) != "1" {
		t.Fatalf("Get(abcdef) = %q, %v", got, err)
	}

	// Hash should match a trie with just this one entry.
	tr2 := New()
	tr2.Put([]byte("abcdef"), []byte("1"))
	if tr.Hash() != tr2.Hash() {
		t.Fatalf("hash mismatch after deep compaction")
	}
}

func TestBranchNodeValueSlot(t *testing.T) {
	// Test that a value at a branch node (Children[16]) works correctly
	// when combined with child entries. Keys that share a common prefix
	// but one is a prefix of another trigger this case.
	tr := New()
	tr.Put([]byte("a"), []byte("val_a"))
	tr.Put([]byte("ab"), []byte("val_ab"))
	tr.Put([]byte("abc"), []byte("val_abc"))
	tr.Put([]byte("b"), []byte("val_b"))

	for _, tc := range []struct{ k, v string }{
		{"a", "val_a"},
		{"ab", "val_ab"},
		{"abc", "val_abc"},
		{"b", "val_b"},
	} {
		got, err := tr.Get([]byte(tc.k))
		if err != nil {
			t.Fatalf("Get(%q) error: %v", tc.k, err)
		}
		if string(got) != tc.v {
			t.Fatalf("Get(%q) = %q, want %q", tc.k, got, tc.v)
		}
	}

	// Delete the prefix key and verify others still work.
	tr.Delete([]byte("a"))
	_, err := tr.Get([]byte("a"))
	if err != ErrNotFound {
		t.Fatalf("Get(a) after delete: err = %v, want ErrNotFound", err)
	}
	got, err := tr.Get([]byte("ab"))
	if err != nil || string(got) != "val_ab" {
		t.Fatalf("Get(ab) = %q, %v", got, err)
	}

	// Delete middle key.
	tr.Delete([]byte("ab"))
	got, err = tr.Get([]byte("abc"))
	if err != nil || string(got) != "val_abc" {
		t.Fatalf("Get(abc) = %q, %v", got, err)
	}
}

func TestLen(t *testing.T) {
	tr := New()
	if tr.Len() != 0 {
		t.Fatal("empty trie Len should be 0")
	}

	tr.Put([]byte("a"), []byte("1"))
	tr.Put([]byte("b"), []byte("2"))
	tr.Put([]byte("c"), []byte("3"))
	if tr.Len() != 3 {
		t.Fatalf("Len = %d, want 3", tr.Len())
	}

	tr.Delete([]byte("b"))
	if tr.Len() != 2 {
		t.Fatalf("Len = %d, want 2 after delete", tr.Len())
	}

	tr.Delete([]byte("a"))
	tr.Delete([]byte("c"))
	if tr.Len() != 0 {
		t.Fatalf("Len = %d, want 0 after all deletes", tr.Len())
	}
}

func TestEmptyKeyInTrie(t *testing.T) {
	// An empty key ([]byte{}) should work. keybytesToHex([]) produces
	// just the terminator [16].
	tr := New()
	tr.Put([]byte{}, []byte("empty_key_value"))
	tr.Put([]byte("a"), []byte("a_value"))

	got, err := tr.Get([]byte{})
	if err != nil {
		t.Fatalf("Get(empty) error: %v", err)
	}
	if string(got) != "empty_key_value" {
		t.Fatalf("Get(empty) = %q, want %q", got, "empty_key_value")
	}

	got, err = tr.Get([]byte("a"))
	if err != nil || string(got) != "a_value" {
		t.Fatalf("Get(a) = %q, %v", got, err)
	}

	// Delete the empty key.
	tr.Delete([]byte{})
	_, err = tr.Get([]byte{})
	if err != ErrNotFound {
		t.Fatalf("Get(empty) after delete: err = %v, want ErrNotFound", err)
	}
	got, err = tr.Get([]byte("a"))
	if err != nil || string(got) != "a_value" {
		t.Fatalf("Get(a) after empty delete = %q, %v", got, err)
	}
}
