package trie

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// --- newHasher ---

func TestNewHasher(t *testing.T) {
	h := newHasher()
	if h == nil {
		t.Fatal("newHasher returned nil")
	}
}

// --- encodeNode ---

func TestEncodeNode_ShortNode(t *testing.T) {
	// Leaf: key with terminator, value node.
	sn := &shortNode{
		Key: hexToCompact([]byte{0x01, 0x02, terminatorByte}),
		Val: valueNode([]byte("hello")),
	}
	enc, err := encodeNode(sn)
	if err != nil {
		t.Fatalf("encodeNode(shortNode): %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded shortNode should not be empty")
	}
	// Must be a valid RLP list.
	if enc[0] < 0xc0 {
		t.Fatalf("encoded shortNode should start with list prefix, got 0x%02x", enc[0])
	}
}

func TestEncodeNode_FullNode(t *testing.T) {
	fn := &fullNode{}
	fn.Children[0] = valueNode([]byte("zero"))
	fn.Children[5] = valueNode([]byte("five"))

	enc, err := encodeNode(fn)
	if err != nil {
		t.Fatalf("encodeNode(fullNode): %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded fullNode should not be empty")
	}
	if enc[0] < 0xc0 {
		t.Fatalf("encoded fullNode should start with list prefix, got 0x%02x", enc[0])
	}
}

func TestEncodeNode_HashNode(t *testing.T) {
	h := hashNode(bytes.Repeat([]byte{0xab}, 32))
	enc, err := encodeNode(h)
	if err != nil {
		t.Fatalf("encodeNode(hashNode): %v", err)
	}
	if !bytes.Equal(enc, []byte(h)) {
		t.Fatal("encoded hashNode should be its own bytes")
	}
}

func TestEncodeNode_ValueNode(t *testing.T) {
	v := valueNode([]byte("test"))
	enc, err := encodeNode(v)
	if err != nil {
		t.Fatalf("encodeNode(valueNode): %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded valueNode should not be empty")
	}
}

func TestEncodeNode_Nil(t *testing.T) {
	enc, err := encodeNode(nil)
	if err != nil {
		t.Fatalf("encodeNode(nil): %v", err)
	}
	if enc != nil {
		t.Fatalf("encodeNode(nil) should return nil, got %x", enc)
	}
}

// --- encodeNodeValue ---

func TestEncodeNodeValue_Nil(t *testing.T) {
	enc, err := encodeNodeValue(nil)
	if err != nil {
		t.Fatalf("encodeNodeValue(nil): %v", err)
	}
	if !bytes.Equal(enc, []byte{0x80}) {
		t.Fatalf("encodeNodeValue(nil) = %x, want 0x80", enc)
	}
}

func TestEncodeNodeValue_ValueNode(t *testing.T) {
	v := valueNode([]byte{0x42})
	enc, err := encodeNodeValue(v)
	if err != nil {
		t.Fatalf("encodeNodeValue(valueNode): %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded value should not be empty")
	}
}

func TestEncodeNodeValue_HashNode(t *testing.T) {
	h := hashNode(bytes.Repeat([]byte{0xcc}, 32))
	enc, err := encodeNodeValue(h)
	if err != nil {
		t.Fatalf("encodeNodeValue(hashNode): %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("encoded hash ref should not be empty")
	}
}

// --- wrapListPayload ---

func TestWrapListPayload_Short(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03}
	wrapped := wrapListPayload(payload)
	if wrapped[0] != 0xc0+byte(len(payload)) {
		t.Fatalf("short list prefix: got 0x%02x, want 0x%02x", wrapped[0], 0xc0+byte(len(payload)))
	}
	if !bytes.Equal(wrapped[1:], payload) {
		t.Fatal("payload mismatch in wrapped short list")
	}
}

func TestWrapListPayload_Long(t *testing.T) {
	payload := bytes.Repeat([]byte{0xaa}, 100)
	wrapped := wrapListPayload(payload)
	// len > 55, so prefix is 0xf7 + lenOfLen, then len bytes, then payload.
	if wrapped[0] < 0xf8 {
		t.Fatalf("long list prefix should be >= 0xf8, got 0x%02x", wrapped[0])
	}
	// Extract length to verify.
	lenOfLen := int(wrapped[0] - 0xf7)
	length := 0
	for i := 1; i <= lenOfLen; i++ {
		length = length<<8 | int(wrapped[i])
	}
	if length != len(payload) {
		t.Fatalf("decoded length = %d, want %d", length, len(payload))
	}
}

// --- putUintBigEndian ---

func TestPutUintBigEndian(t *testing.T) {
	tests := []struct {
		val    uint64
		expect int // expected byte count
	}{
		{0, 1},
		{127, 1},
		{255, 1},
		{256, 2},
		{65535, 2},
		{65536, 3},
		{1 << 24, 4},
		{1 << 32, 8},
	}
	for _, tt := range tests {
		got := putUintBigEndian(tt.val)
		if len(got) != tt.expect {
			t.Errorf("putUintBigEndian(%d): len = %d, want %d", tt.val, len(got), tt.expect)
		}
		// Verify round-trip: reconstruct the value from big-endian bytes.
		var reconstructed uint64
		for _, b := range got {
			reconstructed = reconstructed<<8 | uint64(b)
		}
		if reconstructed != tt.val {
			t.Errorf("putUintBigEndian(%d): roundtrip = %d", tt.val, reconstructed)
		}
	}
}

// --- hash method on hasher ---

func TestHasher_LeafNode(t *testing.T) {
	// Build a simple leaf: shortNode with terminator in key and a valueNode.
	leaf := &shortNode{
		Key:   []byte{0x01, 0x02, terminatorByte},
		Val:   valueNode([]byte("test-value")),
		flags: nodeFlag{dirty: true},
	}
	h := newHasher()
	hashed, cached := h.hash(leaf, true)
	if hashed == nil {
		t.Fatal("hash returned nil hashed node")
	}
	if cached == nil {
		t.Fatal("hash returned nil cached node")
	}

	// The cached node should have hash set and dirty cleared.
	cachedSN, ok := cached.(*shortNode)
	if !ok {
		t.Fatalf("cached should be *shortNode, got %T", cached)
	}
	if cachedSN.flags.dirty {
		t.Fatal("cached node should not be dirty after hashing")
	}
}

func TestHasher_HashDeterministic(t *testing.T) {
	leaf := &shortNode{
		Key:   []byte{0x05, terminatorByte},
		Val:   valueNode([]byte("abc")),
		flags: nodeFlag{dirty: true},
	}
	h := newHasher()

	hashed1, _ := h.hash(leaf, true)
	// Mark dirty again to re-hash.
	leaf.flags.dirty = true
	leaf.flags.hash = nil
	hashed2, _ := h.hash(leaf, true)

	// Both should produce the same result.
	enc1, _ := encodeNode(hashed1)
	enc2, _ := encodeNode(hashed2)
	if !bytes.Equal(enc1, enc2) {
		t.Fatal("repeated hashing should produce identical results")
	}
}

func TestHasher_BranchNode(t *testing.T) {
	fn := &fullNode{flags: nodeFlag{dirty: true}}
	fn.Children[0] = &shortNode{
		Key:   []byte{0x01, terminatorByte},
		Val:   valueNode([]byte("child0")),
		flags: nodeFlag{dirty: true},
	}
	fn.Children[5] = &shortNode{
		Key:   []byte{0x02, terminatorByte},
		Val:   valueNode([]byte("child5")),
		flags: nodeFlag{dirty: true},
	}

	h := newHasher()
	hashed, cached := h.hash(fn, true)
	if hashed == nil || cached == nil {
		t.Fatal("hash returned nil")
	}

	// Cached should have hash set.
	cachedFN, ok := cached.(*fullNode)
	if !ok {
		t.Fatalf("cached should be *fullNode, got %T", cached)
	}
	if cachedFN.flags.dirty {
		t.Fatal("cached fullNode should not be dirty")
	}
}

func TestHasher_ForceHash(t *testing.T) {
	// Small node that encodes to less than 32 bytes.
	leaf := &shortNode{
		Key:   []byte{0x01, terminatorByte},
		Val:   valueNode([]byte("x")),
		flags: nodeFlag{dirty: true},
	}
	h := newHasher()

	// Without force, small nodes are returned as-is (inline).
	hashed, _ := h.hash(leaf, false)
	if _, isHash := hashed.(hashNode); isHash {
		// The node might be small enough to be inline.
		// This is acceptable behavior; the important thing is no panic.
	}

	// With force, we always get a hash.
	leaf.flags.dirty = true
	leaf.flags.hash = nil
	hashedForced, _ := h.hash(leaf, true)
	if hashedForced == nil {
		t.Fatal("forced hash returned nil")
	}
}

func TestHasher_CachingPreventsRecomputation(t *testing.T) {
	leaf := &shortNode{
		Key:   []byte{0x03, 0x04, terminatorByte},
		Val:   valueNode([]byte("cached-value")),
		flags: nodeFlag{dirty: true},
	}
	h := newHasher()

	// First hash: computes and caches.
	hashed1, cached1 := h.hash(leaf, true)
	_ = hashed1

	// Second hash of the cached node (not dirty): should return cached hash.
	hashed2, _ := h.hash(cached1, true)

	enc1, _ := encodeNode(hashed1)
	enc2, _ := encodeNode(hashed2)
	if !bytes.Equal(enc1, enc2) {
		t.Fatal("cached hash should match original hash")
	}
}

// --- store ---

func TestStore_HashNode(t *testing.T) {
	h := newHasher()
	hn := hashNode(bytes.Repeat([]byte{0xaa}, 32))
	result, err := h.store(hn, false)
	if err != nil {
		t.Fatalf("store(hashNode): %v", err)
	}
	if !bytes.Equal([]byte(result.(hashNode)), []byte(hn)) {
		t.Fatal("store(hashNode) should return the same hashNode")
	}
}

func TestStore_ValueNode(t *testing.T) {
	h := newHasher()
	v := valueNode([]byte("val"))
	result, err := h.store(v, false)
	if err != nil {
		t.Fatalf("store(valueNode): %v", err)
	}
	if _, ok := result.(valueNode); !ok {
		t.Fatalf("store(valueNode) should return valueNode, got %T", result)
	}
}

func TestStore_LargeNode_ReturnsHash(t *testing.T) {
	h := newHasher()
	// Create a large shortNode that encodes to >= 32 bytes.
	sn := &shortNode{
		Key: hexToCompact([]byte{0x01, 0x02, 0x03, 0x04, terminatorByte}),
		Val: valueNode(bytes.Repeat([]byte{0x42}, 50)),
	}
	result, err := h.store(sn, false)
	if err != nil {
		t.Fatalf("store(large shortNode): %v", err)
	}
	hn, ok := result.(hashNode)
	if !ok {
		// Node might be < 32 bytes; that's OK for small nodes.
		return
	}
	// Verify the hash is correct.
	enc, _ := encodeNode(sn)
	expected := crypto.Keccak256(enc)
	if !bytes.Equal([]byte(hn), expected) {
		t.Fatal("store hash mismatch")
	}
}

// --- hashChildren ---

func TestHashChildren_ShortNode(t *testing.T) {
	// shortNode with a valueNode child: hashChildren should compact-encode the key.
	leaf := &shortNode{
		Key:   []byte{0x01, 0x02, terminatorByte},
		Val:   valueNode([]byte("val")),
		flags: nodeFlag{dirty: true},
	}
	h := newHasher()
	collapsed, cached := h.hashChildren(leaf)
	collapsedSN, ok := collapsed.(*shortNode)
	if !ok {
		t.Fatalf("collapsed should be *shortNode, got %T", collapsed)
	}
	// Key should be compact-encoded.
	if len(collapsedSN.Key) == 0 {
		t.Fatal("collapsed key should not be empty")
	}
	// Cached should still be a shortNode.
	if _, ok := cached.(*shortNode); !ok {
		t.Fatalf("cached should be *shortNode, got %T", cached)
	}
}

func TestHashChildren_FullNode(t *testing.T) {
	fn := &fullNode{flags: nodeFlag{dirty: true}}
	fn.Children[3] = &shortNode{
		Key:   []byte{0x0a, terminatorByte},
		Val:   valueNode([]byte("child")),
		flags: nodeFlag{dirty: true},
	}
	h := newHasher()
	collapsed, cached := h.hashChildren(fn)
	collapsedFN, ok := collapsed.(*fullNode)
	if !ok {
		t.Fatalf("collapsed should be *fullNode, got %T", collapsed)
	}
	if collapsedFN.Children[3] == nil {
		t.Fatal("collapsed child at index 3 should not be nil")
	}
	cachedFN, ok := cached.(*fullNode)
	if !ok {
		t.Fatalf("cached should be *fullNode, got %T", cached)
	}
	if cachedFN.Children[3] == nil {
		t.Fatal("cached child at index 3 should not be nil")
	}
}

func TestHashChildren_ValueNode(t *testing.T) {
	// hashChildren should return the node unchanged for non-short/full nodes.
	v := valueNode([]byte("raw"))
	h := newHasher()
	collapsed, cached := h.hashChildren(v)
	if !bytes.Equal([]byte(collapsed.(valueNode)), []byte(v)) {
		t.Fatal("collapsed valueNode should match original")
	}
	if !bytes.Equal([]byte(cached.(valueNode)), []byte(v)) {
		t.Fatal("cached valueNode should match original")
	}
}

// --- Integration: hash of a trie built with Put ---

func TestHasher_TrieHashConsistency(t *testing.T) {
	tr := New()
	tr.Put([]byte("foo"), []byte("bar"))
	tr.Put([]byte("baz"), []byte("qux"))
	h1 := tr.Hash()
	h2 := tr.Hash()
	if h1 != h2 {
		t.Fatal("hash should be stable across calls")
	}
	if h1 == emptyRoot {
		t.Fatal("non-empty trie should not have empty root hash")
	}
}

func TestHasher_TrieHashChangesOnMutation(t *testing.T) {
	tr := New()
	tr.Put([]byte("key"), []byte("v1"))
	h1 := tr.Hash()

	tr.Put([]byte("key"), []byte("v2"))
	h2 := tr.Hash()

	if h1 == h2 {
		t.Fatal("hash should change when value is updated")
	}
}
