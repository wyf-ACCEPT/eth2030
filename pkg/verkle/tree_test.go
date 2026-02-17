package verkle

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewTree(t *testing.T) {
	tree := NewTree()
	if tree.Root() == nil {
		t.Fatal("root should not be nil")
	}
	if tree.Root().Type() != InternalNodeType {
		t.Errorf("root type = %d, want %d", tree.Root().Type(), InternalNodeType)
	}
}

func TestPutAndGet(t *testing.T) {
	tree := NewTree()

	key := [KeySize]byte{0x01, 0x02, 0x03}
	value := [ValueSize]byte{0xaa, 0xbb, 0xcc}

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
	if *got != value {
		t.Errorf("Get value mismatch: got %x, want %x", got, value)
	}
}

func TestGetNonExistent(t *testing.T) {
	tree := NewTree()

	key := [KeySize]byte{0x01}
	got, err := tree.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent key, got %x", got)
	}
}

func TestMultiplePuts(t *testing.T) {
	tree := NewTree()

	// Insert multiple values with different stems.
	keys := [][KeySize]byte{
		{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01},
		{0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02},
		{0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03},
	}

	for i, key := range keys {
		var val [ValueSize]byte
		val[0] = byte(i + 1)
		tree.Put(key, val)
	}

	for i, key := range keys {
		got, _ := tree.Get(key)
		if got == nil {
			t.Fatalf("key %d: got nil", i)
		}
		if got[0] != byte(i+1) {
			t.Errorf("key %d: got %d, want %d", i, got[0], i+1)
		}
	}
}

func TestSameStemDifferentSuffixes(t *testing.T) {
	tree := NewTree()

	// Two keys with same stem but different suffix.
	key1 := [KeySize]byte{0x01}
	key1[StemSize] = 0x00 // suffix 0

	key2 := [KeySize]byte{0x01}
	key2[StemSize] = 0x01 // suffix 1

	val1 := [ValueSize]byte{0xaa}
	val2 := [ValueSize]byte{0xbb}

	tree.Put(key1, val1)
	tree.Put(key2, val2)

	got1, _ := tree.Get(key1)
	got2, _ := tree.Get(key2)

	if got1 == nil || *got1 != val1 {
		t.Errorf("key1: got %v, want %v", got1, val1)
	}
	if got2 == nil || *got2 != val2 {
		t.Errorf("key2: got %v, want %v", got2, val2)
	}
}

func TestDelete(t *testing.T) {
	tree := NewTree()

	key := [KeySize]byte{0x01, 0x02}
	value := [ValueSize]byte{0xaa}

	tree.Put(key, value)
	tree.Delete(key)

	got, _ := tree.Get(key)
	if got != nil {
		t.Errorf("expected nil after delete, got %x", got)
	}
}

func TestRootCommitment(t *testing.T) {
	tree := NewTree()

	// Empty tree commitment.
	c1 := tree.RootCommitment()

	// Add a value.
	key := [KeySize]byte{0x01}
	val := [ValueSize]byte{0xaa}
	tree.Put(key, val)

	c2 := tree.RootCommitment()

	// Commitments should differ.
	if c1 == c2 {
		t.Error("root commitment should change after insert")
	}

	// Same state should give same commitment.
	c3 := tree.RootCommitment()
	if c2 != c3 {
		t.Error("root commitment should be stable")
	}
}

func TestLeafNode(t *testing.T) {
	stem := [StemSize]byte{0x01, 0x02, 0x03}
	leaf := NewLeafNode(stem)

	if leaf.Type() != LeafNodeType {
		t.Errorf("type = %d, want %d", leaf.Type(), LeafNodeType)
	}
	if leaf.Stem() != stem {
		t.Error("stem mismatch")
	}
	if leaf.ValueCount() != 0 {
		t.Errorf("ValueCount = %d, want 0", leaf.ValueCount())
	}

	val := [ValueSize]byte{0xaa}
	leaf.Set(0, val)
	if leaf.ValueCount() != 1 {
		t.Errorf("ValueCount = %d, want 1", leaf.ValueCount())
	}

	got := leaf.Get(0)
	if got == nil || *got != val {
		t.Errorf("Get(0) = %v, want %v", got, val)
	}

	leaf.Delete(0)
	if leaf.Get(0) != nil {
		t.Error("expected nil after delete")
	}
}

func TestInternalNode(t *testing.T) {
	node := NewInternalNode(0)

	if node.Type() != InternalNodeType {
		t.Errorf("type = %d, want %d", node.Type(), InternalNodeType)
	}
	if node.ChildCount() != 0 {
		t.Errorf("ChildCount = %d, want 0", node.ChildCount())
	}

	leaf := NewLeafNode([StemSize]byte{0x01})
	node.SetChild(0x01, leaf)

	if node.ChildCount() != 1 {
		t.Errorf("ChildCount = %d, want 1", node.ChildCount())
	}
	if node.Child(0x01) != leaf {
		t.Error("Child(0x01) mismatch")
	}
	if node.Child(0x00) != nil {
		t.Error("Child(0x00) should be nil")
	}
}

func TestEmptyNode(t *testing.T) {
	empty := &EmptyNode{}
	if empty.Type() != EmptyNodeType {
		t.Errorf("type = %d, want %d", empty.Type(), EmptyNodeType)
	}
	if !empty.Commit().IsZero() {
		t.Error("empty node commitment should be zero")
	}
}

func TestNodeSerialization(t *testing.T) {
	// LeafNode serialization
	stem := [StemSize]byte{0x01}
	leaf := NewLeafNode(stem)
	val := [ValueSize]byte{0xaa}
	leaf.Set(5, val)

	data := leaf.Serialize()
	if data[0] != byte(LeafNodeType) {
		t.Errorf("leaf serialize: type byte = %d, want %d", data[0], LeafNodeType)
	}

	// InternalNode serialization
	node := NewInternalNode(0)
	node.SetChild(1, leaf)

	data = node.Serialize()
	if data[0] != byte(InternalNodeType) {
		t.Errorf("internal serialize: type byte = %d, want %d", data[0], InternalNodeType)
	}
}

func TestSplitKey(t *testing.T) {
	var key [KeySize]byte
	for i := 0; i < StemSize; i++ {
		key[i] = byte(i)
	}
	key[StemSize] = 0xff

	stem, suffix := splitKey(key)
	for i := 0; i < StemSize; i++ {
		if stem[i] != byte(i) {
			t.Errorf("stem[%d] = %d, want %d", i, stem[i], i)
		}
	}
	if suffix != 0xff {
		t.Errorf("suffix = %d, want 0xff", suffix)
	}
}

func TestCommitmentStability(t *testing.T) {
	tree := NewTree()

	key1 := [KeySize]byte{0x01}
	val1 := [ValueSize]byte{0xaa}
	tree.Put(key1, val1)

	c1 := tree.RootCommitment()
	c2 := tree.RootCommitment()
	if c1 != c2 {
		t.Error("commitment not stable across calls")
	}
}

func TestStemCollision(t *testing.T) {
	tree := NewTree()

	// Two keys sharing the first byte but diverging at byte 1.
	key1 := [KeySize]byte{0x01, 0x01}
	key2 := [KeySize]byte{0x01, 0x02}
	val1 := [ValueSize]byte{0xaa}
	val2 := [ValueSize]byte{0xbb}

	tree.Put(key1, val1)
	tree.Put(key2, val2)

	got1, _ := tree.Get(key1)
	got2, _ := tree.Get(key2)

	if got1 == nil || *got1 != val1 {
		t.Errorf("key1: got %v, want %v", got1, val1)
	}
	if got2 == nil || *got2 != val2 {
		t.Errorf("key2: got %v, want %v", got2, val2)
	}
}

// Test key derivation functions.
func TestKeyDerivation(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01, 0x02, 0x03, 0x04})

	// All account keys should share the same stem.
	versionKey := GetTreeKeyForVersion(addr)
	balanceKey := GetTreeKeyForBalance(addr)
	nonceKey := GetTreeKeyForNonce(addr)
	codeHashKey := GetTreeKeyForCodeHash(addr)
	codeSizeKey := GetTreeKeyForCodeSize(addr)

	vStem := StemFromKey(versionKey)
	bStem := StemFromKey(balanceKey)
	nStem := StemFromKey(nonceKey)
	chStem := StemFromKey(codeHashKey)
	csStem := StemFromKey(codeSizeKey)

	if vStem != bStem || bStem != nStem || nStem != chStem || chStem != csStem {
		t.Error("all account header keys should share the same stem")
	}

	// Suffixes should be correct.
	if SuffixFromKey(versionKey) != VersionLeafKey {
		t.Errorf("version suffix = %d, want %d", SuffixFromKey(versionKey), VersionLeafKey)
	}
	if SuffixFromKey(balanceKey) != BalanceLeafKey {
		t.Errorf("balance suffix = %d, want %d", SuffixFromKey(balanceKey), BalanceLeafKey)
	}
	if SuffixFromKey(nonceKey) != NonceLeafKey {
		t.Errorf("nonce suffix = %d, want %d", SuffixFromKey(nonceKey), NonceLeafKey)
	}
}

func TestCodeChunkKey(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})

	// First code chunk should be in the header stem.
	k0 := GetTreeKeyForCodeChunk(addr, 0)
	k1 := GetTreeKeyForCodeChunk(addr, 1)

	stem0 := StemFromKey(k0)
	stem1 := StemFromKey(k1)
	if stem0 != stem1 {
		t.Error("code chunks 0 and 1 should share same stem")
	}

	if SuffixFromKey(k0) != CodeOffset {
		t.Errorf("chunk 0 suffix = %d, want %d", SuffixFromKey(k0), CodeOffset)
	}
	if SuffixFromKey(k1) != CodeOffset+1 {
		t.Errorf("chunk 1 suffix = %d, want %d", SuffixFromKey(k1), CodeOffset+1)
	}
}

func TestStorageSlotKey(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x01})

	// Small storage slot (< 64) should be in header stem.
	k0 := GetTreeKeyForStorageSlot(addr, 0)
	if SuffixFromKey(k0) != HeaderStorageOffset {
		t.Errorf("slot 0 suffix = %d, want %d", SuffixFromKey(k0), HeaderStorageOffset)
	}

	// Same stem as account header.
	accountStem := StemFromKey(GetTreeKeyForVersion(addr))
	slotStem := StemFromKey(k0)
	if accountStem != slotStem {
		t.Error("small storage slot should share account stem")
	}

	// Large storage slot should go to different stem.
	k1000 := GetTreeKeyForStorageSlot(addr, 1000)
	largeStem := StemFromKey(k1000)
	if largeStem == accountStem {
		t.Error("large storage slot should have different stem")
	}
}

func TestDifferentAddressesDifferentStems(t *testing.T) {
	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	k1 := GetTreeKeyForBalance(addr1)
	k2 := GetTreeKeyForBalance(addr2)

	if StemFromKey(k1) == StemFromKey(k2) {
		t.Error("different addresses should have different stems")
	}
}
