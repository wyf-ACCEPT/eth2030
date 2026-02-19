package bintrie

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

var (
	zeroKey  = types.Hash{}
	oneKey   = types.HexToHash("0101010101010101010101010101010101010101010101010101010101010101")
	twoKey   = types.HexToHash("0202020202020202020202020202020202020202020202020202020202020202")
	threeKey = types.HexToHash("0303030303030303030303030303030303030303030303030303030303030303")
	fourKey  = types.HexToHash("0404040404040404040404040404040404040404040404040404040404040404")
	ffKey    = types.HexToHash("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
)

func TestSingleEntry(t *testing.T) {
	tree := NewBinaryNode()
	tree, err := tree.Insert(zeroKey[:], oneKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tree.GetHeight() != 1 {
		t.Fatal("invalid depth")
	}
	got := tree.Hash()
	if got == (types.Hash{}) {
		t.Fatal("hash should not be zero for non-empty tree")
	}
}

func TestTwoEntriesDiffFirstBit(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	tree, err = tree.Insert(zeroKey[:], oneKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	key2 := types.HexToHash("8000000000000000000000000000000000000000000000000000000000000000")
	tree, err = tree.Insert(key2[:], twoKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tree.GetHeight() != 2 {
		t.Fatal("invalid height")
	}
}

func TestOneStemColocatedValues(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	keys := []types.Hash{
		types.HexToHash("0000000000000000000000000000000000000000000000000000000000000003"),
		types.HexToHash("0000000000000000000000000000000000000000000000000000000000000004"),
		types.HexToHash("0000000000000000000000000000000000000000000000000000000000000009"),
		types.HexToHash("00000000000000000000000000000000000000000000000000000000000000FF"),
	}
	for _, k := range keys {
		tree, err = tree.Insert(k[:], oneKey[:], nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if tree.GetHeight() != 1 {
		t.Fatal("invalid height")
	}
}

func TestTwoStemColocatedValues(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	// stem: 0...0
	k1 := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000003")
	k2 := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000004")
	// stem: 10...0
	k3 := types.HexToHash("8000000000000000000000000000000000000000000000000000000000000003")
	k4 := types.HexToHash("8000000000000000000000000000000000000000000000000000000000000004")

	for _, k := range []types.Hash{k1, k2, k3, k4} {
		tree, err = tree.Insert(k[:], oneKey[:], nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if tree.GetHeight() != 2 {
		t.Fatal("invalid height")
	}
}

func TestTwoKeysMatchFirst42Bits(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	key1 := types.HexToHash("0000000000C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0C0")
	key2 := types.HexToHash("0000000000E00000000000000000000000000000000000000000000000000000")
	tree, err = tree.Insert(key1[:], oneKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	tree, err = tree.Insert(key2[:], twoKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tree.GetHeight() != 1+42+1 {
		t.Fatal("invalid height")
	}
}

func TestInsertDuplicateKey(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	tree, err = tree.Insert(oneKey[:], oneKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	tree, err = tree.Insert(oneKey[:], twoKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if tree.GetHeight() != 1 {
		t.Fatal("invalid height")
	}
	// Verify that the value is updated
	if !bytes.Equal(tree.(*StemNode).Values[1], twoKey[:]) {
		t.Fatal("value should be updated")
	}
}

func TestLargeNumberOfEntries(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	for i := range StemNodeWidth {
		var key [HashSize]byte
		key[0] = byte(i)
		tree, err = tree.Insert(key[:], ffKey[:], nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	height := tree.GetHeight()
	if height != 1+8 {
		t.Fatalf("invalid height, wanted %d, got %d", 1+8, height)
	}
}

func TestMerkleizeMultipleEntries(t *testing.T) {
	var err error
	tree := NewBinaryNode()
	keys := [][]byte{
		zeroKey[:],
		types.HexToHash("8000000000000000000000000000000000000000000000000000000000000000").Bytes(),
		types.HexToHash("0100000000000000000000000000000000000000000000000000000000000000").Bytes(),
		types.HexToHash("8100000000000000000000000000000000000000000000000000000000000000").Bytes(),
	}
	for i, key := range keys {
		var v [HashSize]byte
		binary.LittleEndian.PutUint64(v[:8], uint64(i))
		tree, err = tree.Insert(key, v[:], nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	got := tree.Hash()
	if got == (types.Hash{}) {
		t.Fatal("hash should not be zero for non-empty tree")
	}
}

func TestHashDeterministic(t *testing.T) {
	// Insert same entries in different order, verify same hash
	keys := []types.Hash{
		types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001"),
		types.HexToHash("0000000000000000000000000000000000000000000000000000000000000002"),
		types.HexToHash("8000000000000000000000000000000000000000000000000000000000000001"),
	}

	tree1 := NewBinaryNode()
	var err error
	for _, k := range keys {
		tree1, err = tree1.Insert(k[:], oneKey[:], nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	hash1 := tree1.Hash()

	// Insert in reverse order
	tree2 := NewBinaryNode()
	for i := len(keys) - 1; i >= 0; i-- {
		tree2, err = tree2.Insert(keys[i][:], oneKey[:], nil, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	hash2 := tree2.Hash()

	if hash1 != hash2 {
		t.Fatalf("hashes should be deterministic: %x != %x", hash1, hash2)
	}
}

func TestEmptyTreeHash(t *testing.T) {
	tree := NewBinaryNode()
	hash := tree.Hash()
	if hash != (types.Hash{}) {
		t.Fatalf("empty tree should hash to zero, got %x", hash)
	}
}

func TestBinaryTrieGetPutDelete(t *testing.T) {
	trie := New()

	key1 := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
	val1 := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")

	// Put
	if err := trie.Put(key1[:], val1[:]); err != nil {
		t.Fatalf("Put error: %v", err)
	}

	// Get
	got, err := trie.Get(key1[:])
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if !bytes.Equal(got, val1[:]) {
		t.Fatalf("Get returned wrong value: got %x, want %x", got, val1[:])
	}

	// Hash should be non-zero
	hash := trie.Hash()
	if hash == (types.Hash{}) {
		t.Fatal("hash should not be zero after insert")
	}

	// Delete
	if err := trie.Delete(key1[:]); err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// Value should now be zero
	got, err = trie.Get(key1[:])
	if err != nil {
		t.Fatalf("Get after delete error: %v", err)
	}
	if got != nil && !bytes.Equal(got, zero[:]) {
		t.Fatalf("Get after delete returned non-zero: %x", got)
	}
}

func TestBinaryTrieMultiplePuts(t *testing.T) {
	trie := New()
	n := 100
	for i := range n {
		var key, val [32]byte
		binary.BigEndian.PutUint64(key[24:], uint64(i))
		binary.BigEndian.PutUint64(val[24:], uint64(i+1000))
		if err := trie.Put(key[:], val[:]); err != nil {
			t.Fatalf("Put(%d) error: %v", i, err)
		}
	}

	for i := range n {
		var key, expected [32]byte
		binary.BigEndian.PutUint64(key[24:], uint64(i))
		binary.BigEndian.PutUint64(expected[24:], uint64(i+1000))
		got, err := trie.Get(key[:])
		if err != nil {
			t.Fatalf("Get(%d) error: %v", i, err)
		}
		if !bytes.Equal(got, expected[:]) {
			t.Fatalf("Get(%d) = %x, want %x", i, got, expected[:])
		}
	}
}

func TestBinaryTrieAccountRoundTrip(t *testing.T) {
	trie := New()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	acc := &types.Account{
		Nonce:    42,
		Balance:  big.NewInt(1000000000),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}

	// Update account
	if err := trie.UpdateAccount(addr, acc, 0); err != nil {
		t.Fatalf("UpdateAccount error: %v", err)
	}

	// Get account back
	gotAcc, err := trie.GetAccount(addr)
	if err != nil {
		t.Fatalf("GetAccount error: %v", err)
	}
	if gotAcc == nil {
		t.Fatal("GetAccount returned nil")
	}

	if gotAcc.Nonce != acc.Nonce {
		t.Fatalf("nonce mismatch: got %d, want %d", gotAcc.Nonce, acc.Nonce)
	}
	if gotAcc.Balance.Cmp(acc.Balance) != 0 {
		t.Fatalf("balance mismatch: got %s, want %s", gotAcc.Balance, acc.Balance)
	}
}

func TestBinaryTrieStorageRoundTrip(t *testing.T) {
	trie := New()
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// Create account first
	acc := &types.Account{
		Nonce:    1,
		Balance:  big.NewInt(1000),
		CodeHash: types.EmptyCodeHash.Bytes(),
	}
	if err := trie.UpdateAccount(addr, acc, 0); err != nil {
		t.Fatalf("UpdateAccount error: %v", err)
	}

	// Storage slots
	slots := []types.Hash{
		types.HexToHash("00000000000000000000000000000000000000000000000000000000000000FF"),
		types.HexToHash("0100000000000000000000000000000000000000000000000000000000000001"),
	}

	val := []byte{0xde, 0xad, 0xbe, 0xef}

	for _, slot := range slots {
		if err := trie.UpdateStorage(addr, slot[:], val); err != nil {
			t.Fatalf("UpdateStorage(%x) error: %v", slot, err)
		}

		got, err := trie.GetStorage(addr, slot[:])
		if err != nil {
			t.Fatalf("GetStorage(%x) error: %v", slot, err)
		}
		if len(got) == 0 {
			t.Fatalf("GetStorage(%x) returned empty", slot)
		}

		// Value is right-justified in 32 bytes
		var expected [HashSize]byte
		copy(expected[HashSize-len(val):], val)
		if !bytes.Equal(got, expected[:]) {
			t.Fatalf("GetStorage(%x) = %x, want %x", slot, got, expected)
		}

		// Delete
		if err := trie.DeleteStorage(addr, slot[:]); err != nil {
			t.Fatalf("DeleteStorage(%x) error: %v", slot, err)
		}

		got, err = trie.GetStorage(addr, slot[:])
		if err != nil {
			t.Fatalf("GetStorage(%x) after delete error: %v", slot, err)
		}
		if len(got) > 0 && !bytes.Equal(got, zero[:]) {
			t.Fatalf("GetStorage(%x) after delete = %x, expected zero", slot, got)
		}
	}
}

func TestBinaryTrieCopy(t *testing.T) {
	trie := New()
	key := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")

	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	cp := trie.Copy()
	if cp.Hash() != trie.Hash() {
		t.Fatal("copy should have same hash")
	}

	// Modify the copy, original should be unchanged
	key2 := types.HexToHash("8000000000000000000000000000000000000000000000000000000000000001")
	if err := cp.Put(key2[:], val[:]); err != nil {
		t.Fatal(err)
	}
	if cp.Hash() == trie.Hash() {
		t.Fatal("modified copy should have different hash")
	}
}

func TestChunkifyCode(t *testing.T) {
	// Simple code: no PUSH instructions
	code := make([]byte, 100)
	for i := range code {
		code[i] = 0x01 // ADD
	}
	chunks := ChunkifyCode(code)

	// Expected: ceil(100/31) = 4 chunks, each 32 bytes
	expectedChunks := (len(code) + StemSize - 1) / StemSize
	if len(chunks) != expectedChunks*HashSize {
		t.Fatalf("ChunkifyCode: expected %d bytes, got %d", expectedChunks*HashSize, len(chunks))
	}
}

func TestChunkifyCodeWithPush(t *testing.T) {
	// Code with PUSH32 that spans chunk boundary
	code := make([]byte, 64)
	code[30] = push32 // PUSH32 at position 30, pushes 32 bytes
	chunks := ChunkifyCode(code)

	// Should have ceil(64/31) = 3 chunks
	expectedChunks := (len(code) + StemSize - 1) / StemSize
	if len(chunks) != expectedChunks*HashSize {
		t.Fatalf("ChunkifyCode with PUSH: expected %d bytes, got %d", expectedChunks*HashSize, len(chunks))
	}
}

func TestChunkifyCodeEmpty(t *testing.T) {
	chunks := ChunkifyCode(nil)
	if len(chunks) != 0 {
		t.Fatalf("ChunkifyCode(nil): expected empty, got %d bytes", len(chunks))
	}
}

func TestGetBinaryTreeKey(t *testing.T) {
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	var key [32]byte
	key[31] = 42

	treeKey := GetBinaryTreeKey(addr, key[:])
	if len(treeKey) != 32 {
		t.Fatalf("tree key should be 32 bytes, got %d", len(treeKey))
	}
	// Last byte should be the leaf index
	if treeKey[31] != 42 {
		t.Fatalf("last byte should be 42, got %d", treeKey[31])
	}
}

func TestGetBinaryTreeKeyBasicData(t *testing.T) {
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	key := GetBinaryTreeKeyBasicData(addr)
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
	if key[31] != BasicDataLeafKey {
		t.Fatalf("last byte should be %d, got %d", BasicDataLeafKey, key[31])
	}
}

func TestGetBinaryTreeKeyCodeHash(t *testing.T) {
	addr := types.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	key := GetBinaryTreeKeyCodeHash(addr)
	if len(key) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key))
	}
	if key[31] != CodeHashLeafKey {
		t.Fatalf("last byte should be %d, got %d", CodeHashLeafKey, key[31])
	}
}

func TestStorageIndex(t *testing.T) {
	// Header storage slot (< 64)
	slot := make([]byte, 32)
	slot[31] = 10
	idx, sub := StorageIndex(slot)
	if idx.Sign() != 0 {
		t.Fatalf("header slot: tree index should be 0, got %s", idx.String())
	}
	if sub != 10+64 {
		t.Fatalf("header slot: sub index should be %d, got %d", 10+64, sub)
	}
}

func TestGetBinaryTreeKeyStorageSlot(t *testing.T) {
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// Header storage (slot < 64)
	headerSlot := make([]byte, 32)
	headerSlot[31] = 5
	key1 := GetBinaryTreeKeyStorageSlot(addr, headerSlot)
	if len(key1) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key1))
	}

	// Main storage (slot >= 64)
	mainSlot := make([]byte, 32)
	mainSlot[31] = 0xFF
	key2 := GetBinaryTreeKeyStorageSlot(addr, mainSlot)
	if len(key2) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(key2))
	}

	// Different slots should produce different keys
	if bytes.Equal(key1, key2) {
		t.Fatal("different slots should produce different keys")
	}
}
