package verkle

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func testAddress(b byte) types.Address {
	var addr types.Address
	addr[0] = b
	addr[19] = b
	return addr
}

func TestNewStemAccessor(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x42)
	sa := NewStemAccessor(tree, addr)

	stem := sa.Stem()
	// Stem should be non-zero (derived from address).
	allZero := true
	for _, b := range stem {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("stem should be non-zero")
	}
}

func TestNewStemAccessorFromStem(t *testing.T) {
	tree := NewTree()
	var stem [StemSize]byte
	stem[0] = 0xAA
	stem[30] = 0xBB

	sa := NewStemAccessorFromStem(tree, stem)
	if sa.Stem() != stem {
		t.Fatal("stem mismatch")
	}
}

func TestStemAccessorGetSetEmpty(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x01)
	sa := NewStemAccessor(tree, addr)

	// Get from empty tree should return nil.
	val := sa.Get(VersionLeafKey)
	if val != nil {
		t.Fatal("expected nil for empty tree")
	}
}

func TestStemAccessorSetAndGet(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x02)
	sa := NewStemAccessor(tree, addr)

	var value [ValueSize]byte
	binary.BigEndian.PutUint64(value[24:], 99)

	if err := sa.Set(VersionLeafKey, value); err != nil {
		t.Fatal(err)
	}

	// Read back via a fresh accessor (forces reload from tree).
	sa2 := NewStemAccessor(tree, addr)
	got := sa2.Get(VersionLeafKey)
	if got == nil {
		t.Fatal("expected non-nil value")
	}
	if *got != value {
		t.Fatal("value mismatch")
	}
}

func TestAccountHeaderRoundtrip(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x03)
	sa := NewStemAccessor(tree, addr)

	header := &AccountHeader{
		Version:  1,
		Balance:  big.NewInt(1000000),
		Nonce:    42,
		CodeHash: [32]byte{0xAA, 0xBB, 0xCC},
		CodeSize: 256,
	}

	if err := sa.WriteAccountHeader(header); err != nil {
		t.Fatal(err)
	}

	// Read back.
	sa2 := NewStemAccessor(tree, addr)
	got := sa2.ReadAccountHeader()

	if got.Version != header.Version {
		t.Fatalf("version: expected %d, got %d", header.Version, got.Version)
	}
	if got.Balance.Cmp(header.Balance) != 0 {
		t.Fatalf("balance: expected %s, got %s", header.Balance, got.Balance)
	}
	if got.Nonce != header.Nonce {
		t.Fatalf("nonce: expected %d, got %d", header.Nonce, got.Nonce)
	}
	if got.CodeHash != header.CodeHash {
		t.Fatal("code_hash mismatch")
	}
	if got.CodeSize != header.CodeSize {
		t.Fatalf("code_size: expected %d, got %d", header.CodeSize, got.CodeSize)
	}
}

func TestAccountHeaderZeroBalance(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x04)
	sa := NewStemAccessor(tree, addr)

	header := &AccountHeader{
		Balance: big.NewInt(0),
	}
	if err := sa.WriteAccountHeader(header); err != nil {
		t.Fatal(err)
	}

	sa2 := NewStemAccessor(tree, addr)
	got := sa2.ReadAccountHeader()
	if got.Balance.Sign() != 0 {
		t.Fatalf("expected zero balance, got %s", got.Balance)
	}
}

func TestAccountHeaderLargeBalance(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x05)
	sa := NewStemAccessor(tree, addr)

	// 2^128 - 1 (fits in 16 bytes).
	balance := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	header := &AccountHeader{
		Balance: balance,
	}
	if err := sa.WriteAccountHeader(header); err != nil {
		t.Fatal(err)
	}

	sa2 := NewStemAccessor(tree, addr)
	got := sa2.ReadAccountHeader()
	if got.Balance.Cmp(balance) != 0 {
		t.Fatalf("balance: expected %s, got %s", balance, got.Balance)
	}
}

func TestChunkifyCodeEmpty(t *testing.T) {
	chunks := ChunkifyCode(nil)
	if chunks != nil {
		t.Fatal("expected nil for empty code")
	}
	chunks = ChunkifyCode([]byte{})
	if chunks != nil {
		t.Fatal("expected nil for zero-length code")
	}
}

func TestChunkifyCodeSmall(t *testing.T) {
	// Small code that fits in one chunk.
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01} // PUSH1 1, PUSH1 2, ADD
	chunks := ChunkifyCode(code)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	// pushDataCount should be 0 (no spillover from previous chunk).
	if chunks[0][0] != 0 {
		t.Fatalf("expected pushDataCount=0, got %d", chunks[0][0])
	}

	// Code bytes should be at positions 1..5.
	for i, b := range code {
		if chunks[0][1+i] != b {
			t.Fatalf("code byte %d: expected %x, got %x", i, b, chunks[0][1+i])
		}
	}
}

func TestChunkifyCodePushSpillover(t *testing.T) {
	// PUSH32 followed by 32 bytes of data. Total 33 bytes.
	// Chunk 0: [0] + code[0..30] = PUSH32 + 30 bytes of data
	// Chunk 1: [2] + code[31..32] = 2 remaining push-data bytes
	code := make([]byte, 33)
	code[0] = 0x7F // PUSH32
	for i := 1; i <= 32; i++ {
		code[i] = byte(i)
	}

	chunks := ChunkifyCode(code)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// First chunk: pushDataCount = 0.
	if chunks[0][0] != 0 {
		t.Fatalf("chunk 0 pushDataCount: expected 0, got %d", chunks[0][0])
	}

	// Second chunk: pushDataCount = 2 (31 bytes of data in chunk 0,
	// PUSH32 needs 32 bytes total, so 2 spill).
	if chunks[1][0] != 2 {
		t.Fatalf("chunk 1 pushDataCount: expected 2, got %d", chunks[1][0])
	}
}

func TestChunkifyCodeMultipleChunks(t *testing.T) {
	// 64 bytes of simple opcodes (no PUSHes).
	code := make([]byte, 64)
	for i := range code {
		code[i] = 0x01 // ADD opcode
	}

	chunks := ChunkifyCode(code)
	// 64 / 31 = 3 chunks (ceil).
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}

	// All pushDataCounts should be 0 (no PUSH instructions).
	for i, chunk := range chunks {
		if chunk[0] != 0 {
			t.Fatalf("chunk %d pushDataCount: expected 0, got %d", i, chunk[0])
		}
	}
}

func TestChunkifyCodeExactChunkSize(t *testing.T) {
	// Exactly 31 bytes -> 1 chunk.
	code := make([]byte, 31)
	for i := range code {
		code[i] = byte(i + 1)
	}
	chunks := ChunkifyCode(code)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestWriteAndReadCode(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x10)
	sa := NewStemAccessor(tree, addr)

	code := []byte{0x60, 0x0A, 0x60, 0x0B, 0x01} // PUSH1 10, PUSH1 11, ADD
	if err := sa.WriteCode(addr, code); err != nil {
		t.Fatal(err)
	}

	// Read back chunk 0.
	sa2 := NewStemAccessor(tree, addr)
	chunk := sa2.ReadCodeChunk(0)
	if chunk == nil {
		t.Fatal("expected non-nil chunk 0")
	}

	// Verify code bytes.
	for i, b := range code {
		if chunk[1+i] != b {
			t.Fatalf("code byte %d: expected %x, got %x", i, b, chunk[1+i])
		}
	}
}

func TestSmallStorageSlot(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x20)
	sa := NewStemAccessor(tree, addr)

	var value [ValueSize]byte
	binary.BigEndian.PutUint64(value[24:], 12345)

	if err := sa.WriteStorageSlot(0, value); err != nil {
		t.Fatal(err)
	}

	sa2 := NewStemAccessor(tree, addr)
	got := sa2.ReadStorageSlot(0)
	if got == nil {
		t.Fatal("expected non-nil storage slot")
	}
	if *got != value {
		t.Fatal("storage slot value mismatch")
	}
}

func TestSmallStorageSlotBoundary(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x21)
	sa := NewStemAccessor(tree, addr)

	// Slot 63 is the last small storage slot.
	var value [ValueSize]byte
	value[31] = 0xFF
	if err := sa.WriteStorageSlot(63, value); err != nil {
		t.Fatal(err)
	}

	// Slot 64 is out of range for small storage.
	if err := sa.WriteStorageSlot(64, value); err == nil {
		t.Fatal("expected error for slot >= 64")
	}
}

func TestReadStorageSlotOutOfRange(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x22)
	sa := NewStemAccessor(tree, addr)

	if got := sa.ReadStorageSlot(64); got != nil {
		t.Fatal("expected nil for out-of-range slot")
	}
}

func TestAggregateStemProofs(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x30)

	// Write some data.
	sa := NewStemAccessor(tree, addr)
	header := &AccountHeader{
		Version: 1,
		Balance: big.NewInt(5000),
		Nonce:   10,
	}
	sa.WriteAccountHeader(header)

	// Build keys to prove.
	keys := [][KeySize]byte{
		GetTreeKeyForVersion(addr),
		GetTreeKeyForBalance(addr),
		GetTreeKeyForNonce(addr),
	}

	proofs := AggregateStemProofs(tree, keys)
	if len(proofs) != 1 {
		t.Fatalf("expected 1 stem proof, got %d", len(proofs))
	}

	if proofs[0].Stem != sa.Stem() {
		t.Fatal("stem mismatch")
	}
	if len(proofs[0].SuffixStateDiffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(proofs[0].SuffixStateDiffs))
	}
}

func TestAggregateStemProofsMultipleStems(t *testing.T) {
	tree := NewTree()
	addr1 := testAddress(0x40)
	addr2 := testAddress(0x41)

	// Write to both accounts.
	sa1 := NewStemAccessor(tree, addr1)
	sa1.WriteAccountHeader(&AccountHeader{
		Version: 1,
		Balance: big.NewInt(100),
	})
	sa2 := NewStemAccessor(tree, addr2)
	sa2.WriteAccountHeader(&AccountHeader{
		Version: 1,
		Balance: big.NewInt(200),
	})

	keys := [][KeySize]byte{
		GetTreeKeyForBalance(addr1),
		GetTreeKeyForBalance(addr2),
	}

	proofs := AggregateStemProofs(tree, keys)
	if len(proofs) != 2 {
		t.Fatalf("expected 2 stem proofs, got %d", len(proofs))
	}
}

func TestStemProofSize(t *testing.T) {
	proofs := []StemProofData{
		{SuffixStateDiffs: make([]SuffixStateDiff, 3)},
		{SuffixStateDiffs: make([]SuffixStateDiff, 2)},
		{SuffixStateDiffs: make([]SuffixStateDiff, 5)},
	}
	if size := StemProofSize(proofs); size != 10 {
		t.Fatalf("expected 10, got %d", size)
	}
}

func TestStemProofSizeEmpty(t *testing.T) {
	if size := StemProofSize(nil); size != 0 {
		t.Fatalf("expected 0, got %d", size)
	}
}

func TestPathCommitmentsCollection(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x50)
	sa := NewStemAccessor(tree, addr)

	// Write data so the tree has nodes.
	sa.WriteAccountHeader(&AccountHeader{
		Version: 1,
		Balance: big.NewInt(42),
	})

	commits := collectPathCommitments(tree, sa.Stem())
	// Should have at least the root commitment.
	if len(commits) < 1 {
		t.Fatal("expected at least 1 path commitment")
	}
}

func TestChunkifyCodePush1AtEnd(t *testing.T) {
	// Code: 30 bytes of ADD, then PUSH1 at position 30 with 1 byte of data.
	// Total: 32 bytes. Chunk 0: 31 bytes (30 ADDs + PUSH1).
	// Chunk 1: 1 byte data (pushDataCount=1).
	code := make([]byte, 32)
	for i := 0; i < 30; i++ {
		code[i] = 0x01 // ADD
	}
	code[30] = 0x60 // PUSH1
	code[31] = 0xAA // push data

	chunks := ChunkifyCode(code)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	// Chunk 1 should have pushDataCount = 1.
	if chunks[1][0] != 1 {
		t.Fatalf("chunk 1 pushDataCount: expected 1, got %d", chunks[1][0])
	}
}

func TestReadCodeChunkOutOfRange(t *testing.T) {
	tree := NewTree()
	addr := testAddress(0x60)
	sa := NewStemAccessor(tree, addr)

	// Reading a code chunk beyond the stem's range returns nil.
	chunk := sa.ReadCodeChunk(128)
	if chunk != nil {
		t.Fatal("expected nil for out-of-range code chunk")
	}
}
