package trie

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestTrieHasher_EmptyTrie(t *testing.T) {
	th := NewTrieHasher()
	got := th.HashRoot(nil)
	if got != emptyRoot {
		t.Fatalf("empty HashRoot = %s, want %s", got.Hex(), emptyRoot.Hex())
	}
	got = th.HashRoot([]KeyValuePair{})
	if got != emptyRoot {
		t.Fatalf("empty slice HashRoot = %s, want %s", got.Hex(), emptyRoot.Hex())
	}
}

func TestTrieHasher_SingleEntry(t *testing.T) {
	th := NewTrieHasher()
	pairs := []KeyValuePair{
		{Key: []byte("A"), Value: []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	}
	got := th.HashRoot(pairs)

	// Compare against direct trie computation.
	tr := New()
	tr.Put([]byte("A"), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	want := tr.Hash()

	if got != want {
		t.Fatalf("single entry: got %s, want %s", got.Hex(), want.Hex())
	}
}

func TestTrieHasher_MultipleEntries(t *testing.T) {
	th := NewTrieHasher()
	pairs := []KeyValuePair{
		{Key: []byte("doe"), Value: []byte("reindeer")},
		{Key: []byte("dog"), Value: []byte("puppy")},
		{Key: []byte("dogglesworth"), Value: []byte("cat")},
	}
	got := th.HashRoot(pairs)

	// Known go-ethereum test vector.
	exp := types.HexToHash("8aad789dff2f538bca5d8ea56e8abe10f4c7ba3a5dea95fea4cd6e7c3a1168d3")
	if got != exp {
		t.Fatalf("multiple entries: got %s, want %s", got.Hex(), exp.Hex())
	}
}

func TestTrieHasher_UnsortedPairs(t *testing.T) {
	th := NewTrieHasher()

	// Provide pairs in reverse order -- should produce the same root
	// as sorted order since HashRoot sorts internally.
	pairs := []KeyValuePair{
		{Key: []byte("dogglesworth"), Value: []byte("cat")},
		{Key: []byte("dog"), Value: []byte("puppy")},
		{Key: []byte("doe"), Value: []byte("reindeer")},
	}
	got := th.HashRoot(pairs)

	exp := types.HexToHash("8aad789dff2f538bca5d8ea56e8abe10f4c7ba3a5dea95fea4cd6e7c3a1168d3")
	if got != exp {
		t.Fatalf("unsorted pairs: got %s, want %s", got.Hex(), exp.Hex())
	}
}

func TestTrieHasher_EmptyValues_Skipped(t *testing.T) {
	th := NewTrieHasher()

	// Pairs with empty values should be skipped.
	pairs := []KeyValuePair{
		{Key: []byte("a"), Value: []byte("val")},
		{Key: []byte("b"), Value: nil},
		{Key: []byte("c"), Value: []byte{}},
	}
	got := th.HashRoot(pairs)

	// Should match a trie with only key "a".
	tr := New()
	tr.Put([]byte("a"), []byte("val"))
	want := tr.Hash()

	if got != want {
		t.Fatalf("empty values: got %s, want %s", got.Hex(), want.Hex())
	}
}

func TestTrieHasher_SecureTrie_Empty(t *testing.T) {
	th := NewTrieHasher()
	got := th.HashSecureTrie(nil)
	if got != emptyRoot {
		t.Fatalf("empty secure trie: got %s, want %s", got.Hex(), emptyRoot.Hex())
	}
}

func TestTrieHasher_SecureTrie_SingleEntry(t *testing.T) {
	th := NewTrieHasher()
	pairs := []KeyValuePair{
		{Key: []byte("hello"), Value: []byte("world")},
	}
	got := th.HashSecureTrie(pairs)

	// Build the same thing manually.
	tr := New()
	hashedKey := crypto.Keccak256([]byte("hello"))
	tr.Put(hashedKey, []byte("world"))
	want := tr.Hash()

	if got != want {
		t.Fatalf("secure trie single: got %s, want %s", got.Hex(), want.Hex())
	}

	// Should differ from non-secure hash.
	plain := th.HashRoot(pairs)
	if got == plain {
		t.Fatal("secure trie should differ from plain trie")
	}
}

func TestTrieHasher_SecureTrie_MultipleEntries(t *testing.T) {
	th := NewTrieHasher()
	pairs := []KeyValuePair{
		{Key: []byte("doe"), Value: []byte("reindeer")},
		{Key: []byte("dog"), Value: []byte("puppy")},
		{Key: []byte("dogglesworth"), Value: []byte("cat")},
	}
	got := th.HashSecureTrie(pairs)

	// Build the same thing manually with keccak-hashed keys.
	tr := New()
	for _, p := range pairs {
		hashedKey := crypto.Keccak256(p.Key)
		tr.Put(hashedKey, p.Value)
	}
	want := tr.Hash()

	if got != want {
		t.Fatalf("secure trie multiple: got %s, want %s", got.Hex(), want.Hex())
	}
}

func TestTrieHasher_HashChildren_ShortNode(t *testing.T) {
	th := NewTrieHasher()

	// A short node (< 32 bytes) should be returned as-is (inlined).
	short := []byte("hello")
	got := th.HashChildren(short)
	if !bytes.Equal(got, short) {
		t.Fatalf("short node should be inlined, got %x", got)
	}
}

func TestTrieHasher_HashChildren_LongNode(t *testing.T) {
	th := NewTrieHasher()

	// A long node (>= 32 bytes) should be hashed.
	long := bytes.Repeat([]byte{0xab}, 64)
	got := th.HashChildren(long)
	want := crypto.Keccak256(long)
	if !bytes.Equal(got, want) {
		t.Fatalf("long node hash mismatch: got %x, want %x", got, want)
	}
}

func TestTrieHasher_HashChildren_Exactly32Bytes(t *testing.T) {
	th := NewTrieHasher()

	// Exactly 32 bytes should be hashed (not inlined).
	data := bytes.Repeat([]byte{0xcd}, 32)
	got := th.HashChildren(data)
	want := crypto.Keccak256(data)
	if !bytes.Equal(got, want) {
		t.Fatalf("32-byte node should be hashed: got %x, want %x", got, want)
	}
}

func TestTrieHasher_Deterministic(t *testing.T) {
	th := NewTrieHasher()
	pairs := []KeyValuePair{
		{Key: []byte("c"), Value: []byte("3")},
		{Key: []byte("a"), Value: []byte("1")},
		{Key: []byte("b"), Value: []byte("2")},
	}

	h1 := th.HashRoot(pairs)
	h2 := th.HashRoot(pairs)
	if h1 != h2 {
		t.Fatalf("HashRoot not deterministic: %s vs %s", h1.Hex(), h2.Hex())
	}
}

func TestTrieHasher_DoesNotMutateInput(t *testing.T) {
	th := NewTrieHasher()
	pairs := []KeyValuePair{
		{Key: []byte("b"), Value: []byte("2")},
		{Key: []byte("a"), Value: []byte("1")},
	}
	// Save original order.
	origFirst := string(pairs[0].Key)

	th.HashRoot(pairs)

	// Verify the input slice was not sorted in-place.
	if string(pairs[0].Key) != origFirst {
		t.Fatal("HashRoot mutated the input slice order")
	}
}

func TestEstimateTrieSize_Zero(t *testing.T) {
	got := EstimateTrieSize(0, 32, 32)
	if got != 0 {
		t.Fatalf("EstimateTrieSize(0, ...) = %d, want 0", got)
	}
}

func TestEstimateTrieSize_Negative(t *testing.T) {
	got := EstimateTrieSize(-1, 32, 32)
	if got != 0 {
		t.Fatalf("EstimateTrieSize(-1, ...) = %d, want 0", got)
	}
}

func TestEstimateTrieSize_Positive(t *testing.T) {
	got := EstimateTrieSize(100, 32, 64)
	if got <= 0 {
		t.Fatalf("EstimateTrieSize(100, 32, 64) = %d, want > 0", got)
	}

	// More keys should produce a larger estimate.
	got2 := EstimateTrieSize(1000, 32, 64)
	if got2 <= got {
		t.Fatalf("more keys should produce larger estimate: %d <= %d", got2, got)
	}
}

func TestEstimateTrieSize_ScalesWithValueSize(t *testing.T) {
	small := EstimateTrieSize(100, 32, 32)
	large := EstimateTrieSize(100, 32, 128)
	if large <= small {
		t.Fatalf("larger values should produce larger estimate: %d <= %d", large, small)
	}
}

func TestTrieHasher_GethVector_MatchesDirectTrie(t *testing.T) {
	// Verify that HashRoot produces exactly the same result as
	// building the trie directly via Put + Hash.
	entries := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"dog", "puppy"},
		{"somethingveryoddindeedthis is", "myothernodedata"},
	}

	pairs := make([]KeyValuePair, len(entries))
	tr := New()
	for i, e := range entries {
		pairs[i] = KeyValuePair{Key: []byte(e.k), Value: []byte(e.v)}
		tr.Put([]byte(e.k), []byte(e.v))
	}

	th := NewTrieHasher()
	got := th.HashRoot(pairs)
	want := tr.Hash()

	if got != want {
		t.Fatalf("HashRoot vs Trie.Hash: got %s, want %s", got.Hex(), want.Hex())
	}
}
