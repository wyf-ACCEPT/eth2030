package trie

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestEncBranchNode_AllEmpty(t *testing.T) {
	var children [17][]byte
	enc, err := EncBranchNode(children)
	if err != nil {
		t.Fatalf("EncBranchNode: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("expected non-empty encoding")
	}
	// Should be a valid RLP list.
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if len(items) != 17 {
		t.Fatalf("expected 17 items, got %d", len(items))
	}
	// All children should be empty.
	for i := 0; i < 17; i++ {
		if len(items[i]) != 0 {
			t.Fatalf("child %d should be empty, got %x", i, items[i])
		}
	}
}

func TestEncBranchNode_WithValue(t *testing.T) {
	var children [17][]byte
	children[16] = []byte("hello")
	enc, err := EncBranchNode(children)
	if err != nil {
		t.Fatalf("EncBranchNode: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if !bytes.Equal(items[16], []byte("hello")) {
		t.Fatalf("expected value 'hello', got %x", items[16])
	}
}

func TestEncBranchNode_WithChildHash(t *testing.T) {
	var children [17][]byte
	childHash := crypto.Keccak256([]byte("child data"))
	children[3] = childHash
	enc, err := EncBranchNode(children)
	if err != nil {
		t.Fatalf("EncBranchNode: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if !bytes.Equal(items[3], childHash) {
		t.Fatalf("child 3: expected %x, got %x", childHash, items[3])
	}
}

func TestEncExtensionNode(t *testing.T) {
	hexKey := []byte{0x01, 0x02, 0x03}
	childRef := crypto.Keccak256([]byte("child"))

	enc, err := EncExtensionNode(hexKey, childRef)
	if err != nil {
		t.Fatalf("EncExtensionNode: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// Decode the compact key back and check nibbles.
	nibbles := compactToHex(items[0])
	if hasTerm(nibbles) {
		t.Fatal("extension node should not have terminator")
	}
	if !bytes.Equal(nibbles, hexKey) {
		t.Fatalf("key mismatch: expected %v, got %v", hexKey, nibbles)
	}
}

func TestEncLeafNode(t *testing.T) {
	hexKey := []byte{0x04, 0x05}
	value := []byte("leaf value")

	enc, err := EncLeafNode(hexKey, value)
	if err != nil {
		t.Fatalf("EncLeafNode: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// Decode the compact key.
	nibbles := compactToHex(items[0])
	if !hasTerm(nibbles) {
		t.Fatal("leaf node should have terminator")
	}
	// Strip the terminator and compare.
	if !bytes.Equal(nibbles[:len(nibbles)-1], hexKey) {
		t.Fatalf("key mismatch: expected %v, got %v", hexKey, nibbles[:len(nibbles)-1])
	}
	if !bytes.Equal(items[1], value) {
		t.Fatalf("value mismatch: expected %s, got %s", value, items[1])
	}
}

func TestEncNodeHash_Inline(t *testing.T) {
	// Small data should be inlined.
	smallData := []byte{0xc1, 0x80} // small RLP list
	result := EncNodeHash(smallData, false)
	if !result.Inline {
		t.Fatal("expected inline for small node")
	}
	if result.Hash != (types.Hash{}) {
		t.Fatal("expected zero hash for inline node")
	}
}

func TestEncNodeHash_Hashed(t *testing.T) {
	// Large data should be hashed.
	largeData := make([]byte, 64)
	for i := range largeData {
		largeData[i] = byte(i)
	}
	result := EncNodeHash(largeData, false)
	if result.Inline {
		t.Fatal("expected non-inline for large node")
	}
	expected := crypto.Keccak256Hash(largeData)
	if result.Hash != expected {
		t.Fatalf("hash mismatch: expected %s, got %s", expected, result.Hash)
	}
}

func TestEncNodeHash_Force(t *testing.T) {
	// Small data with force=true should not be inlined.
	smallData := []byte{0xc1, 0x80}
	result := EncNodeHash(smallData, true)
	if result.Inline {
		t.Fatal("expected non-inline with force=true")
	}
	expected := crypto.Keccak256Hash(smallData)
	if result.Hash != expected {
		t.Fatalf("hash mismatch: expected %s, got %s", expected, result.Hash)
	}
}

func TestKeyToNibbles_RoundTrip(t *testing.T) {
	key := []byte{0xAB, 0xCD, 0xEF}
	nibbles := KeyToNibbles(key)
	expected := []byte{0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}
	if !bytes.Equal(nibbles, expected) {
		t.Fatalf("KeyToNibbles: expected %v, got %v", expected, nibbles)
	}
	back := NibblesToKey(nibbles)
	if !bytes.Equal(back, key) {
		t.Fatalf("NibblesToKey roundtrip: expected %x, got %x", key, back)
	}
}

func TestKeyToNibblesWithTerm(t *testing.T) {
	key := []byte{0x12}
	nibbles := KeyToNibblesWithTerm(key)
	if len(nibbles) != 3 {
		t.Fatalf("expected 3 nibbles, got %d", len(nibbles))
	}
	if nibbles[0] != 0x01 || nibbles[1] != 0x02 {
		t.Fatalf("unexpected nibbles: %v", nibbles)
	}
	if nibbles[2] != terminatorByte {
		t.Fatalf("expected terminator at end, got %d", nibbles[2])
	}
}

func TestNibblesToKey_Empty(t *testing.T) {
	result := NibblesToKey(nil)
	if len(result) != 0 {
		t.Fatalf("expected empty key, got %x", result)
	}
}

func TestNibblesToKey_OddLength(t *testing.T) {
	// Odd-length nibbles should be zero-padded.
	nibbles := []byte{0x0A, 0x0B, 0x0C}
	result := NibblesToKey(nibbles)
	// Should be padded to {0x00, 0x0A, 0x0B, 0x0C} -> {0x00, 0xAB, 0x0C}
	// After padding: [0, A, B, C] -> [0x0A, 0xBC]
	if len(result) != 2 {
		t.Fatalf("expected 2 bytes, got %d", len(result))
	}
}

func TestNibblesToKey_WithTerminator(t *testing.T) {
	nibbles := []byte{0x01, 0x02, terminatorByte}
	result := NibblesToKey(nibbles)
	expected := []byte{0x12}
	if !bytes.Equal(result, expected) {
		t.Fatalf("expected %x, got %x", expected, result)
	}
}

func TestCompactEncodeHex_Leaf(t *testing.T) {
	nibbles := []byte{0x01, 0x02, 0x03, 0x04}
	compact := CompactEncodeHex(nibbles, true)
	decoded, isLeaf := CompactDecodeHex(compact)
	if !isLeaf {
		t.Fatal("expected leaf flag")
	}
	if !bytes.Equal(decoded, nibbles) {
		t.Fatalf("roundtrip mismatch: expected %v, got %v", nibbles, decoded)
	}
}

func TestCompactEncodeHex_Extension(t *testing.T) {
	nibbles := []byte{0x0A, 0x0B}
	compact := CompactEncodeHex(nibbles, false)
	decoded, isLeaf := CompactDecodeHex(compact)
	if isLeaf {
		t.Fatal("expected extension (not leaf)")
	}
	if !bytes.Equal(decoded, nibbles) {
		t.Fatalf("roundtrip mismatch: expected %v, got %v", nibbles, decoded)
	}
}

func TestCompactEncodeHex_OddLength(t *testing.T) {
	nibbles := []byte{0x01, 0x02, 0x03}
	compact := CompactEncodeHex(nibbles, true)
	decoded, isLeaf := CompactDecodeHex(compact)
	if !isLeaf {
		t.Fatal("expected leaf flag")
	}
	if !bytes.Equal(decoded, nibbles) {
		t.Fatalf("roundtrip mismatch: expected %v, got %v", nibbles, decoded)
	}
}

func TestSharedNibblePrefix(t *testing.T) {
	tests := []struct {
		a, b     []byte
		expected int
	}{
		{[]byte{1, 2, 3}, []byte{1, 2, 4}, 2},
		{[]byte{1, 2, 3}, []byte{1, 2, 3}, 3},
		{[]byte{1, 2, 3}, []byte{4, 5, 6}, 0},
		{nil, []byte{1}, 0},
		{nil, nil, 0},
	}
	for _, tt := range tests {
		got := SharedNibblePrefix(tt.a, tt.b)
		if got != tt.expected {
			t.Errorf("SharedNibblePrefix(%v, %v) = %d, want %d", tt.a, tt.b, got, tt.expected)
		}
	}
}

func TestEncNodeInline(t *testing.T) {
	if !EncNodeInline(make([]byte, 31)) {
		t.Fatal("31 bytes should be inline")
	}
	if EncNodeInline(make([]byte, 32)) {
		t.Fatal("32 bytes should NOT be inline")
	}
	if EncNodeInline(make([]byte, 64)) {
		t.Fatal("64 bytes should NOT be inline")
	}
}

func TestEncNodeRef_Small(t *testing.T) {
	data := []byte{0xc1, 0x80}
	ref := EncNodeRef(data)
	// Small node: ref should be the data itself.
	if !bytes.Equal(ref, data) {
		t.Fatalf("expected inline ref, got %x", ref)
	}
}

func TestEncNodeRef_Large(t *testing.T) {
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}
	ref := EncNodeRef(data)
	if len(ref) != 32 {
		t.Fatalf("expected 32-byte hash, got %d bytes", len(ref))
	}
	expected := crypto.Keccak256(data)
	if !bytes.Equal(ref, expected) {
		t.Fatalf("hash mismatch")
	}
}

func TestNodeEncTracker(t *testing.T) {
	tracker := NewNodeEncTracker()
	h := types.BytesToHash([]byte{0x01})

	if tracker.DirtyCount() != 0 {
		t.Fatal("initial dirty count should be 0")
	}

	tracker.MarkDirty(h)
	if !tracker.IsDirty(h) {
		t.Fatal("should be dirty after MarkDirty")
	}
	if tracker.DirtyCount() != 1 {
		t.Fatal("dirty count should be 1")
	}

	result := &NodeEncResult{RLP: []byte{0x80}, Hash: h, Kind: NodeEncLeaf}
	tracker.CacheResult(h, result)

	if tracker.IsDirty(h) {
		t.Fatal("should not be dirty after CacheResult")
	}
	if tracker.CachedCount() != 1 {
		t.Fatal("cached count should be 1")
	}
	got := tracker.GetCached(h)
	if got == nil || got.Hash != h {
		t.Fatal("cached result mismatch")
	}
	if tracker.TotalTracked() != 1 {
		t.Fatal("total tracked should be 1")
	}

	tracker.Reset()
	if tracker.DirtyCount() != 0 || tracker.CachedCount() != 0 {
		t.Fatal("should be empty after Reset")
	}
}

func TestNodeEncTracker_MarkDirtyInvalidatesCache(t *testing.T) {
	tracker := NewNodeEncTracker()
	h := types.BytesToHash([]byte{0x02})

	result := &NodeEncResult{RLP: []byte{0x80}, Hash: h}
	tracker.CacheResult(h, result)
	if tracker.GetCached(h) == nil {
		t.Fatal("should be cached")
	}

	tracker.MarkDirty(h)
	if tracker.GetCached(h) != nil {
		t.Fatal("cache should be invalidated by MarkDirty")
	}
}

func TestNodeEncBatch(t *testing.T) {
	batch := NewNodeEncBatch()
	if batch.Len() != 0 || batch.Size() != 0 {
		t.Fatal("empty batch should have zero len and size")
	}

	h1 := types.BytesToHash([]byte{0x01})
	h2 := types.BytesToHash([]byte{0x02})
	batch.Add(h1, []byte("data1"))
	batch.Add(h2, []byte("data2"))

	if batch.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", batch.Len())
	}
	if batch.Size() != 10 {
		t.Fatalf("expected size 10, got %d", batch.Size())
	}

	// Flush to a NodeDatabase.
	db := NewNodeDatabase(nil)
	batch.FlushTo(db)
	if db.DirtyCount() != 2 {
		t.Fatalf("expected 2 dirty nodes in DB, got %d", db.DirtyCount())
	}

	// Verify entries.
	entries := batch.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	batch.Reset()
	if batch.Len() != 0 || batch.Size() != 0 {
		t.Fatal("batch should be empty after reset")
	}
}

func TestClassifyNodeEnc(t *testing.T) {
	// Test leaf node classification.
	leafEnc, err := EncLeafNode([]byte{0x01}, []byte("val"))
	if err != nil {
		t.Fatal(err)
	}
	if classifyNodeEnc(leafEnc) != NodeEncLeaf {
		t.Fatal("expected NodeEncLeaf")
	}

	// Test extension node classification.
	extEnc, err := EncExtensionNode([]byte{0x01}, crypto.Keccak256([]byte("child")))
	if err != nil {
		t.Fatal(err)
	}
	if classifyNodeEnc(extEnc) != NodeEncExtension {
		t.Fatal("expected NodeEncExtension")
	}

	// Test branch node classification.
	var children [17][]byte
	branchEnc, err := EncBranchNode(children)
	if err != nil {
		t.Fatal(err)
	}
	if classifyNodeEnc(branchEnc) != NodeEncBranch {
		t.Fatal("expected NodeEncBranch")
	}

	// Empty data.
	if classifyNodeEnc(nil) != NodeEncEmpty {
		t.Fatal("expected NodeEncEmpty for nil")
	}
}

func TestEncCollapseBranch(t *testing.T) {
	fn := &fullNode{flags: nodeFlag{dirty: true}}
	fn.Children[0] = &shortNode{
		Key:   []byte{0x01, terminatorByte},
		Val:   valueNode([]byte("test")),
		flags: nodeFlag{dirty: true},
	}
	fn.Children[16] = valueNode([]byte("branchval"))

	enc, err := EncCollapseBranch(fn)
	if err != nil {
		t.Fatalf("EncCollapseBranch: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if len(items) != 17 {
		t.Fatalf("expected 17 items, got %d", len(items))
	}
	// Value at index 16 should be present.
	if !bytes.Equal(items[16], []byte("branchval")) {
		t.Fatalf("branch value mismatch: got %s", items[16])
	}
	// Child 0 should be non-empty.
	if len(items[0]) == 0 {
		t.Fatal("child 0 should be non-empty")
	}
}

func TestEncCollapseShort_Leaf(t *testing.T) {
	sn := &shortNode{
		Key:   []byte{0x01, 0x02, terminatorByte},
		Val:   valueNode([]byte("leafval")),
		flags: nodeFlag{dirty: true},
	}
	enc, err := EncCollapseShort(sn)
	if err != nil {
		t.Fatalf("EncCollapseShort: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	nibbles := compactToHex(items[0])
	if !hasTerm(nibbles) {
		t.Fatal("expected leaf terminator")
	}
	if !bytes.Equal(items[1], []byte("leafval")) {
		t.Fatalf("value mismatch: got %s", items[1])
	}
}

func TestEncCollapseShort_Extension(t *testing.T) {
	child := &shortNode{
		Key:   []byte{0x03, terminatorByte},
		Val:   valueNode([]byte("deep")),
		flags: nodeFlag{dirty: true},
	}
	sn := &shortNode{
		Key:   []byte{0x01, 0x02},
		Val:   child,
		flags: nodeFlag{dirty: true},
	}
	enc, err := EncCollapseShort(sn)
	if err != nil {
		t.Fatalf("EncCollapseShort: %v", err)
	}
	items, err := decodeRLPList(enc)
	if err != nil {
		t.Fatalf("decodeRLPList: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	nibbles := compactToHex(items[0])
	if hasTerm(nibbles) {
		t.Fatal("extension node should not have terminator")
	}
}
