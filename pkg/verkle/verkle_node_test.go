package verkle

import (
	"bytes"
	"testing"
)

func TestVerkleEmptyNode_Singleton(t *testing.T) {
	if EmptyVerkleNode() != EmptyVerkleNode() {
		t.Error("EmptyVerkleNode should return the same singleton")
	}
}

func TestVerkleEmptyNode_Properties(t *testing.T) {
	e := EmptyVerkleNode()
	if e.NodeType() != VerkleNodeEmpty {
		t.Error("wrong type")
	}
	if e.NodeCommitment() != ([32]byte{}) {
		t.Error("should be zero")
	}
	if e.IsDirty() {
		t.Error("should not be dirty")
	}
	if err := e.Insert(make([]byte, KeySize), make([]byte, ValueSize)); err != ErrInsertIntoEmpty {
		t.Errorf("Insert: got %v, want ErrInsertIntoEmpty", err)
	}
	v, err := e.GetValue(make([]byte, KeySize))
	if err != nil || v != nil {
		t.Error("GetValue should return nil, nil")
	}
}

func TestVerkleLeafNode_Create(t *testing.T) {
	config := NewPedersenConfig(16)
	var stem [StemSize]byte
	stem[0] = 0xAB
	leaf := NewVerkleLeafNode(stem, config)
	if leaf.NodeType() != VerkleNodeLeaf {
		t.Error("wrong type")
	}
	if leaf.Stem() != stem {
		t.Error("stem mismatch")
	}
	if !leaf.IsDirty() {
		t.Error("should be dirty")
	}
	if leaf.ValueCount() != 0 {
		t.Errorf("ValueCount = %d, want 0", leaf.ValueCount())
	}
}

func TestVerkleLeafNode_SetGetDelete(t *testing.T) {
	config := DefaultPedersenConfig()
	leaf := NewVerkleLeafNode([StemSize]byte{}, config)
	var val [ValueSize]byte
	val[31] = 42
	leaf.SetValue(5, val)
	if !leaf.HasValue(5) {
		t.Error("HasValue(5) should be true")
	}
	if leaf.HasValue(6) {
		t.Error("HasValue(6) should be false")
	}
	got := leaf.ValueAt(5)
	if got == nil || *got != val {
		t.Error("ValueAt(5) mismatch")
	}
	if leaf.ValueAt(6) != nil {
		t.Error("ValueAt(6) should be nil")
	}
	leaf.DeleteValue(5)
	if leaf.ValueCount() != 0 {
		t.Error("should be 0 after delete")
	}
	if leaf.HasValue(5) {
		t.Error("HasValue(5) false after delete")
	}
}

func TestVerkleLeafNode_Insert(t *testing.T) {
	config := DefaultPedersenConfig()
	var stem [StemSize]byte
	stem[0] = 0x01
	leaf := NewVerkleLeafNode(stem, config)
	key := make([]byte, KeySize)
	copy(key[:StemSize], stem[:])
	key[StemSize] = 10
	val := make([]byte, ValueSize)
	val[31] = 77
	if err := leaf.Insert(key, val); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, _ := leaf.GetValue(key)
	if !bytes.Equal(got, val) {
		t.Error("GetValue mismatch")
	}
}

func TestVerkleLeafNode_InsertErrors(t *testing.T) {
	config := DefaultPedersenConfig()
	leaf := NewVerkleLeafNode([StemSize]byte{0x01}, config)
	// Wrong stem.
	key := make([]byte, KeySize)
	key[0] = 0x02
	if err := leaf.Insert(key, make([]byte, ValueSize)); err == nil {
		t.Error("wrong stem should fail")
	}
	// Invalid key size.
	if err := leaf.Insert([]byte{1, 2}, make([]byte, ValueSize)); err != ErrInvalidNodeKey {
		t.Error("short key")
	}
	// Invalid value size.
	if err := leaf.Insert(make([]byte, KeySize), []byte{1}); err != ErrInvalidNodeValue {
		t.Error("short value")
	}
}

func TestVerkleLeafNode_Commitment(t *testing.T) {
	config := DefaultPedersenConfig()
	leaf := NewVerkleLeafNode([StemSize]byte{0xFF}, config)
	leaf.SetValue(0, [ValueSize]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	c := leaf.NodeCommitment()
	if c == ([32]byte{}) {
		t.Error("should not be zero")
	}
	if leaf.IsDirty() {
		t.Error("should not be dirty after commit")
	}

	// Deterministic: two identical leaves produce same commitment.
	leaf2 := NewVerkleLeafNode([StemSize]byte{0xFF}, config)
	leaf2.SetValue(0, [ValueSize]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	if leaf.NodeCommitment() != leaf2.NodeCommitment() {
		t.Error("determinism")
	}

	// Changes on update.
	leaf.SetValue(0, [ValueSize]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2})
	c2 := leaf.NodeCommitment()
	if c == c2 {
		t.Error("different values should differ")
	}
}

func TestVerkleInternalNode_Create(t *testing.T) {
	config := DefaultPedersenConfig()
	node := NewVerkleInternalNode(0, config)
	if node.NodeType() != VerkleNodeInternal {
		t.Error("wrong type")
	}
	if node.Depth() != 0 {
		t.Error("wrong depth")
	}
	if node.ChildCount() != 0 {
		t.Error("should have no children")
	}
}

func TestVerkleInternalNode_SetChild(t *testing.T) {
	config := DefaultPedersenConfig()
	node := NewVerkleInternalNode(0, config)
	leaf := NewVerkleLeafNode([StemSize]byte{}, config)
	node.SetChildAt(5, leaf)
	if node.ChildCount() != 1 {
		t.Error("ChildCount")
	}
	if node.ChildAt(5) != leaf {
		t.Error("ChildAt(5)")
	}
	if node.ChildAt(6) != nil {
		t.Error("ChildAt(6) should be nil")
	}
}

func TestVerkleInternalNode_InsertAndGet(t *testing.T) {
	config := DefaultPedersenConfig()
	node := NewVerkleInternalNode(0, config)

	// Single insert.
	key := make([]byte, KeySize)
	key[0] = 0x01
	key[StemSize] = 5
	val := make([]byte, ValueSize)
	val[31] = 42
	if err := node.Insert(key, val); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, _ := node.GetValue(key)
	if !bytes.Equal(got, val) {
		t.Error("value mismatch")
	}

	// Multiple same stem.
	key2 := make([]byte, KeySize)
	key2[0] = 0x01
	key2[StemSize] = 1
	val2 := make([]byte, ValueSize)
	val2[31] = 20
	node.Insert(key2, val2)
	got2, _ := node.GetValue(key2)
	if !bytes.Equal(got2, val2) {
		t.Error("key2 mismatch")
	}

	// Different stems.
	key3 := make([]byte, KeySize)
	key3[0] = 0x02
	val3 := make([]byte, ValueSize)
	val3[31] = 22
	node.Insert(key3, val3)
	got3, _ := node.GetValue(key3)
	if !bytes.Equal(got3, val3) {
		t.Error("key3 mismatch")
	}

	// Missing key.
	missingKey := make([]byte, KeySize)
	missingKey[0] = 0xFF
	got4, _ := node.GetValue(missingKey)
	if got4 != nil {
		t.Error("missing key should return nil")
	}
}

func TestVerkleInternalNode_CollidingStems(t *testing.T) {
	config := DefaultPedersenConfig()
	node := NewVerkleInternalNode(0, config)

	key1 := make([]byte, KeySize)
	key1[0] = 0xAA
	key1[1] = 0x01
	val1 := make([]byte, ValueSize)
	val1[31] = 33

	key2 := make([]byte, KeySize)
	key2[0] = 0xAA
	key2[1] = 0x02
	val2 := make([]byte, ValueSize)
	val2[31] = 44

	if err := node.Insert(key1, val1); err != nil {
		t.Fatalf("Insert key1: %v", err)
	}
	if err := node.Insert(key2, val2); err != nil {
		t.Fatalf("Insert key2: %v", err)
	}

	got1, _ := node.GetValue(key1)
	got2, _ := node.GetValue(key2)
	if !bytes.Equal(got1, val1) {
		t.Error("key1 mismatch")
	}
	if !bytes.Equal(got2, val2) {
		t.Error("key2 mismatch")
	}
}

func TestVerkleInternalNode_Commitment(t *testing.T) {
	config := DefaultPedersenConfig()
	node := NewVerkleInternalNode(0, config)
	key := make([]byte, KeySize)
	key[0] = 0x01
	val := make([]byte, ValueSize)
	val[31] = 1
	node.Insert(key, val)
	c := node.NodeCommitment()
	if c == ([32]byte{}) {
		t.Error("should not be zero")
	}

	// Deterministic.
	node2 := NewVerkleInternalNode(0, config)
	node2.Insert(key, val)
	if node.NodeCommitment() != node2.NodeCommitment() {
		t.Error("determinism")
	}
}

func TestVerkleInternalNode_InvalidKey(t *testing.T) {
	config := DefaultPedersenConfig()
	node := NewVerkleInternalNode(0, config)
	if err := node.Insert([]byte{1}, make([]byte, ValueSize)); err != ErrInvalidNodeKey {
		t.Error("short key")
	}
	if _, err := node.GetValue([]byte{1, 2, 3}); err != ErrInvalidNodeKey {
		t.Error("short key get")
	}
}

func TestVerkleTrie_InsertAndGet(t *testing.T) {
	trie := NewVerkleTrie(DefaultPedersenConfig())
	key := make([]byte, KeySize)
	key[0] = 0x10
	key[StemSize] = 3
	val := make([]byte, ValueSize)
	val[31] = 55
	if err := trie.Insert(key, val); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	got, err := trie.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, val) {
		t.Error("Get mismatch")
	}
}

func TestVerkleTrie_MultipleInserts(t *testing.T) {
	trie := NewVerkleTrie(DefaultPedersenConfig())
	for i := 0; i < 20; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i)
		key[StemSize] = byte(i % 10)
		val := make([]byte, ValueSize)
		val[31] = byte(i + 1)
		if err := trie.Insert(key, val); err != nil {
			t.Fatalf("Insert(%d): %v", i, err)
		}
	}
	for i := 0; i < 20; i++ {
		key := make([]byte, KeySize)
		key[0] = byte(i)
		key[StemSize] = byte(i % 10)
		got, _ := trie.Get(key)
		if got == nil || got[31] != byte(i+1) {
			t.Errorf("Get(%d) mismatch", i)
		}
	}
	if trie.LeafCount() != 20 {
		t.Errorf("LeafCount = %d, want 20", trie.LeafCount())
	}
}

func TestVerkleTrie_RootCommitment(t *testing.T) {
	trie := NewVerkleTrie(DefaultPedersenConfig())
	key := make([]byte, KeySize)
	key[0] = 0x01
	val := make([]byte, ValueSize)
	val[31] = 1
	trie.Insert(key, val)
	if trie.RootCommitment() == ([32]byte{}) {
		t.Error("should not be zero")
	}
}

func TestVerkleTrie_DeepTree(t *testing.T) {
	trie := NewVerkleTrie(DefaultPedersenConfig())
	key1 := make([]byte, KeySize)
	key2 := make([]byte, KeySize)
	for i := 0; i < 20; i++ {
		key1[i] = 0xAA
		key2[i] = 0xAA
	}
	key1[20] = 0x01
	key2[20] = 0x02
	val1 := make([]byte, ValueSize)
	val1[31] = 0xBB
	val2 := make([]byte, ValueSize)
	val2[31] = 0xCC
	trie.Insert(key1, val1)
	trie.Insert(key2, val2)
	got1, _ := trie.Get(key1)
	got2, _ := trie.Get(key2)
	if !bytes.Equal(got1, val1) {
		t.Error("key1 mismatch")
	}
	if !bytes.Equal(got2, val2) {
		t.Error("key2 mismatch")
	}
	if trie.NodeCount() < 3 {
		t.Errorf("NodeCount = %d, want >= 3", trie.NodeCount())
	}
}

func TestVerkleTrie_NilConfig(t *testing.T) {
	trie := NewVerkleTrie(nil)
	if trie.Root() == nil {
		t.Error("root should not be nil")
	}
}

func TestVerkleTrie_CommitMethod(t *testing.T) {
	config := DefaultPedersenConfig()
	trie := NewVerkleTrie(config)
	key := make([]byte, KeySize)
	key[0] = 0x42
	val := make([]byte, ValueSize)
	val[31] = 0x01
	trie.Insert(key, val)
	if trie.Root().Commit(config) != trie.RootCommitment() {
		t.Error("Commit != RootCommitment")
	}
}
