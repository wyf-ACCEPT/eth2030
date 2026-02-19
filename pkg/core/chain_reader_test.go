package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeTestBlock creates a block at the given number with the given parent hash
// and difficulty.
func makeTestBlock(number uint64, parentHash types.Hash, difficulty uint64) *types.Block {
	header := &types.Header{
		Number:     big.NewInt(int64(number)),
		ParentHash: parentHash,
		Difficulty: big.NewInt(int64(difficulty)),
		GasLimit:   30_000_000,
		Time:       1700000000 + number*12,
	}
	return types.NewBlock(header, nil)
}

// buildTestChain creates a MemoryChain with n blocks (0 to n-1), each linking
// to the previous via ParentHash.
func buildTestChain(n int) (*MemoryChain, []*types.Block) {
	mc := NewMemoryChain()
	blocks := make([]*types.Block, n)

	for i := 0; i < n; i++ {
		var parentHash types.Hash
		if i > 0 {
			parentHash = blocks[i-1].Hash()
		}
		block := makeTestBlock(uint64(i), parentHash, 1000)
		blocks[i] = block
		mc.AddBlock(block)
	}
	return mc, blocks
}

// --- MemoryChain tests ---

func TestNewMemoryChain(t *testing.T) {
	mc := NewMemoryChain()
	if mc == nil {
		t.Fatal("expected non-nil MemoryChain")
	}
	if mc.CurrentBlock() != nil {
		t.Fatal("expected nil current block for empty chain")
	}
	if mc.CurrentHeader() != nil {
		t.Fatal("expected nil current header for empty chain")
	}
}

func TestMemoryChain_AddBlock(t *testing.T) {
	mc := NewMemoryChain()
	block := makeTestBlock(0, types.Hash{}, 1000)
	mc.AddBlock(block)

	got := mc.GetBlockByNumber(0)
	if got == nil {
		t.Fatal("expected block at number 0")
	}
	if got.Hash() != block.Hash() {
		t.Fatal("block hash mismatch")
	}
}

func TestMemoryChain_AddBlock_Nil(t *testing.T) {
	mc := NewMemoryChain()
	mc.AddBlock(nil) // should not panic
	if mc.CurrentBlock() != nil {
		t.Fatal("expected nil current block after adding nil")
	}
}

func TestMemoryChain_AutoAdvanceHead(t *testing.T) {
	mc := NewMemoryChain()
	b0 := makeTestBlock(0, types.Hash{}, 1000)
	b1 := makeTestBlock(1, b0.Hash(), 1000)

	mc.AddBlock(b0)
	if mc.CurrentBlock().NumberU64() != 0 {
		t.Fatalf("want head at 0, got %d", mc.CurrentBlock().NumberU64())
	}

	mc.AddBlock(b1)
	if mc.CurrentBlock().NumberU64() != 1 {
		t.Fatalf("want head at 1, got %d", mc.CurrentBlock().NumberU64())
	}
}

func TestMemoryChain_SetCurrentBlock(t *testing.T) {
	mc, blocks := buildTestChain(5)

	mc.SetCurrentBlock(blocks[2])
	if mc.CurrentBlock().NumberU64() != 2 {
		t.Fatalf("want head at 2, got %d", mc.CurrentBlock().NumberU64())
	}
}

func TestMemoryChain_SetCurrentBlock_Nil(t *testing.T) {
	mc, _ := buildTestChain(3)
	mc.SetCurrentBlock(nil)
	if mc.CurrentBlock() != nil {
		t.Fatal("expected nil current block")
	}
}

func TestMemoryChain_GetHeader(t *testing.T) {
	mc, blocks := buildTestChain(3)

	h := mc.GetHeader(blocks[1].Hash(), 1)
	if h == nil {
		t.Fatal("expected header for block 1")
	}
	if h.Number.Uint64() != 1 {
		t.Fatalf("want number 1, got %d", h.Number.Uint64())
	}
}

func TestMemoryChain_GetHeader_WrongHash(t *testing.T) {
	mc, _ := buildTestChain(3)

	h := mc.GetHeader(types.Hash{0xff}, 1)
	if h != nil {
		t.Fatal("expected nil header for wrong hash")
	}
}

func TestMemoryChain_GetHeader_NotFound(t *testing.T) {
	mc := NewMemoryChain()
	h := mc.GetHeader(types.Hash{}, 99)
	if h != nil {
		t.Fatal("expected nil header for missing block")
	}
}

func TestMemoryChain_GetHeaderByNumber(t *testing.T) {
	mc, blocks := buildTestChain(3)

	h := mc.GetHeaderByNumber(2)
	if h == nil {
		t.Fatal("expected header for block 2")
	}
	if h.Number.Uint64() != 2 {
		t.Fatalf("want number 2, got %d", h.Number.Uint64())
	}
	if h.ParentHash != blocks[1].Hash() {
		t.Fatal("parent hash mismatch")
	}
}

func TestMemoryChain_GetHeaderByNumber_NotFound(t *testing.T) {
	mc := NewMemoryChain()
	h := mc.GetHeaderByNumber(99)
	if h != nil {
		t.Fatal("expected nil for missing block number")
	}
}

func TestMemoryChain_GetBlock(t *testing.T) {
	mc, blocks := buildTestChain(3)

	b := mc.GetBlock(blocks[1].Hash(), 1)
	if b == nil {
		t.Fatal("expected block 1")
	}
	if b.NumberU64() != 1 {
		t.Fatalf("want block 1, got %d", b.NumberU64())
	}
}

func TestMemoryChain_GetBlock_WrongHash(t *testing.T) {
	mc, _ := buildTestChain(3)

	b := mc.GetBlock(types.Hash{0xff}, 1)
	if b != nil {
		t.Fatal("expected nil for wrong hash")
	}
}

func TestMemoryChain_GetBlock_NotFound(t *testing.T) {
	mc := NewMemoryChain()
	b := mc.GetBlock(types.Hash{}, 99)
	if b != nil {
		t.Fatal("expected nil for missing block")
	}
}

func TestMemoryChain_GetBlockByNumber(t *testing.T) {
	mc, _ := buildTestChain(3)

	b := mc.GetBlockByNumber(0)
	if b == nil {
		t.Fatal("expected block 0")
	}
	if b.NumberU64() != 0 {
		t.Fatalf("want block 0, got %d", b.NumberU64())
	}
}

func TestMemoryChain_GetBlockByNumber_NotFound(t *testing.T) {
	mc := NewMemoryChain()
	b := mc.GetBlockByNumber(99)
	if b != nil {
		t.Fatal("expected nil for missing block number")
	}
}

func TestMemoryChain_CurrentBlock(t *testing.T) {
	mc, blocks := buildTestChain(5)

	cur := mc.CurrentBlock()
	if cur == nil {
		t.Fatal("expected non-nil current block")
	}
	// Should be the last block added (highest number).
	if cur.NumberU64() != 4 {
		t.Fatalf("want head at 4, got %d", cur.NumberU64())
	}
	if cur.Hash() != blocks[4].Hash() {
		t.Fatal("current block hash mismatch")
	}
}

func TestMemoryChain_CurrentHeader(t *testing.T) {
	mc, _ := buildTestChain(3)

	h := mc.CurrentHeader()
	if h == nil {
		t.Fatal("expected non-nil current header")
	}
	if h.Number.Uint64() != 2 {
		t.Fatalf("want header number 2, got %d", h.Number.Uint64())
	}
}

func TestMemoryChain_HasBlock(t *testing.T) {
	mc, blocks := buildTestChain(3)

	if !mc.HasBlock(blocks[0].Hash(), 0) {
		t.Fatal("expected HasBlock(0) = true")
	}
	if !mc.HasBlock(blocks[2].Hash(), 2) {
		t.Fatal("expected HasBlock(2) = true")
	}
}

func TestMemoryChain_HasBlock_False(t *testing.T) {
	mc, _ := buildTestChain(3)

	if mc.HasBlock(types.Hash{0xff}, 0) {
		t.Fatal("expected HasBlock with wrong hash = false")
	}
	if mc.HasBlock(types.Hash{}, 99) {
		t.Fatal("expected HasBlock for missing number = false")
	}
}

func TestMemoryChain_HasBlock_Empty(t *testing.T) {
	mc := NewMemoryChain()
	if mc.HasBlock(types.Hash{}, 0) {
		t.Fatal("expected false for empty chain")
	}
}

// --- ChainIterator tests ---

func TestChainIterator(t *testing.T) {
	mc, blocks := buildTestChain(5)

	it := NewChainIterator(mc, 1, 3)
	if it.BlockCount() != 3 {
		t.Fatalf("want block count 3, got %d", it.BlockCount())
	}

	b, ok := it.Next()
	if !ok || b == nil {
		t.Fatal("expected block 1")
	}
	if b.NumberU64() != 1 {
		t.Fatalf("want block 1, got %d", b.NumberU64())
	}

	b, ok = it.Next()
	if !ok || b == nil {
		t.Fatal("expected block 2")
	}
	if b.NumberU64() != 2 {
		t.Fatalf("want block 2, got %d", b.NumberU64())
	}

	b, ok = it.Next()
	if !ok || b == nil {
		t.Fatal("expected block 3")
	}
	if b.NumberU64() != 3 {
		t.Fatalf("want block 3, got %d", b.NumberU64())
	}
	if b.Hash() != blocks[3].Hash() {
		t.Fatal("block 3 hash mismatch")
	}

	// Iterator exhausted.
	b, ok = it.Next()
	if ok {
		t.Fatal("expected iterator to be exhausted")
	}
	if b != nil {
		t.Fatal("expected nil block after exhaustion")
	}
}

func TestChainIterator_SingleBlock(t *testing.T) {
	mc, _ := buildTestChain(3)

	it := NewChainIterator(mc, 1, 1)
	if it.BlockCount() != 1 {
		t.Fatalf("want block count 1, got %d", it.BlockCount())
	}

	b, ok := it.Next()
	if !ok || b == nil {
		t.Fatal("expected block 1")
	}

	_, ok = it.Next()
	if ok {
		t.Fatal("expected exhaustion after single block")
	}
}

func TestChainIterator_Reset(t *testing.T) {
	mc, _ := buildTestChain(3)

	it := NewChainIterator(mc, 0, 2)

	// Exhaust the iterator.
	for i := 0; i < 3; i++ {
		it.Next()
	}
	_, ok := it.Next()
	if ok {
		t.Fatal("expected exhaustion")
	}

	// Reset and iterate again.
	it.Reset()
	b, ok := it.Next()
	if !ok || b == nil {
		t.Fatal("expected block 0 after reset")
	}
	if b.NumberU64() != 0 {
		t.Fatalf("want block 0, got %d", b.NumberU64())
	}
}

func TestChainIterator_GapInChain(t *testing.T) {
	mc := NewMemoryChain()
	mc.AddBlock(makeTestBlock(0, types.Hash{}, 1000))
	// Skip block 1.
	mc.AddBlock(makeTestBlock(2, types.Hash{}, 1000))

	it := NewChainIterator(mc, 0, 2)

	b, ok := it.Next()
	if !ok || b == nil {
		t.Fatal("expected block 0")
	}

	// Block 1 is missing - should return false.
	b, ok = it.Next()
	if ok {
		t.Fatal("expected false for missing block 1")
	}
}

func TestChainIterator_EmptyRange(t *testing.T) {
	mc := NewMemoryChain()
	it := NewChainIterator(mc, 5, 3)
	if it.BlockCount() != 0 {
		t.Fatalf("want block count 0 for inverted range, got %d", it.BlockCount())
	}
	_, ok := it.Next()
	if ok {
		t.Fatal("expected false for inverted range")
	}
}

// --- GetAncestor tests ---

func TestGetAncestor(t *testing.T) {
	mc, blocks := buildTestChain(5)

	// Ancestor of block 4 at distance 2 should be block 2.
	hash, num := GetAncestor(mc, blocks[4].Hash(), 4, 2)
	if num != 2 {
		t.Fatalf("want ancestor at number 2, got %d", num)
	}
	if hash != blocks[2].Hash() {
		t.Fatal("ancestor hash mismatch")
	}
}

func TestGetAncestor_ZeroDistance(t *testing.T) {
	mc, blocks := buildTestChain(3)

	hash, num := GetAncestor(mc, blocks[2].Hash(), 2, 0)
	if num != 2 {
		t.Fatalf("want number 2, got %d", num)
	}
	if hash != blocks[2].Hash() {
		t.Fatal("hash mismatch for zero distance")
	}
}

func TestGetAncestor_ToGenesis(t *testing.T) {
	mc, blocks := buildTestChain(5)

	hash, num := GetAncestor(mc, blocks[4].Hash(), 4, 4)
	if num != 0 {
		t.Fatalf("want ancestor at 0, got %d", num)
	}
	if hash != blocks[0].Hash() {
		t.Fatal("ancestor hash mismatch for genesis")
	}
}

func TestGetAncestor_TooFar(t *testing.T) {
	mc, blocks := buildTestChain(3)

	hash, num := GetAncestor(mc, blocks[2].Hash(), 2, 5)
	if num != 0 {
		t.Fatalf("want 0 for too-far ancestor, got %d", num)
	}
	if hash != (types.Hash{}) {
		t.Fatal("expected zero hash for too-far ancestor")
	}
}

func TestGetAncestor_NotFound(t *testing.T) {
	mc := NewMemoryChain()

	hash, num := GetAncestor(mc, types.Hash{0x01}, 5, 1)
	if num != 0 {
		t.Fatalf("want 0 for missing block, got %d", num)
	}
	if hash != (types.Hash{}) {
		t.Fatal("expected zero hash for missing block")
	}
}

// --- GetTD tests ---

func TestGetTD(t *testing.T) {
	mc, blocks := buildTestChain(5)

	td := GetTD(mc, blocks[4].Hash(), 4)
	if td == nil {
		t.Fatal("expected non-nil TD")
	}
	// Each block has difficulty 1000, 5 blocks -> TD = 5000.
	want := big.NewInt(5000)
	if td.Cmp(want) != 0 {
		t.Fatalf("want TD 5000, got %s", td.String())
	}
}

func TestGetTD_Genesis(t *testing.T) {
	mc, blocks := buildTestChain(1)

	td := GetTD(mc, blocks[0].Hash(), 0)
	if td == nil {
		t.Fatal("expected non-nil TD")
	}
	want := big.NewInt(1000)
	if td.Cmp(want) != 0 {
		t.Fatalf("want TD 1000, got %s", td.String())
	}
}

func TestGetTD_NotFound(t *testing.T) {
	mc := NewMemoryChain()

	td := GetTD(mc, types.Hash{0x01}, 5)
	if td != nil {
		t.Fatal("expected nil TD for missing block")
	}
}

// --- Interface compliance ---

func TestMemoryChainImplementsChainReader(t *testing.T) {
	var _ ChainReader = (*MemoryChain)(nil)
}
