package sync

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- mock implementations ---

// testBlockInserter is a mock BlockInserter for block processor tests.
type testBlockInserter struct {
	blocks  []*types.Block
	current *types.Block
	failAt  int // if > 0, fail when total blocks reach this count
}

func newTestBlockInserter(genesis *types.Block) *testBlockInserter {
	return &testBlockInserter{
		blocks:  []*types.Block{genesis},
		current: genesis,
	}
}

func (ti *testBlockInserter) InsertChain(blocks []*types.Block) (int, error) {
	for i, b := range blocks {
		if ti.failAt > 0 && len(ti.blocks)+i >= ti.failAt {
			return i, errors.New("mock insert error")
		}
		ti.blocks = append(ti.blocks, b)
		ti.current = b
	}
	return len(blocks), nil
}

func (ti *testBlockInserter) CurrentBlock() *types.Block {
	return ti.current
}

// testReceiptHasher is a mock ReceiptHasher.
type testReceiptHasher struct {
	root types.Hash
}

func (h *testReceiptHasher) ComputeReceiptRoot(receipts []*types.Receipt) types.Hash {
	return h.root
}

// testStateExecutor is a mock StateExecutor.
type testStateExecutor struct {
	stateRoot types.Hash
	receipts  []*types.Receipt
	err       error
}

func (e *testStateExecutor) ExecuteBlock(block *types.Block) (types.Hash, []*types.Receipt, error) {
	return e.stateRoot, e.receipts, e.err
}

// testAncestorLookup is a mock AncestorLookup.
type testAncestorLookup struct {
	headers map[types.Hash]*types.Header
	blocks  map[types.Hash]*types.Block
}

func newTestAncestorLookup() *testAncestorLookup {
	return &testAncestorLookup{
		headers: make(map[types.Hash]*types.Header),
		blocks:  make(map[types.Hash]*types.Block),
	}
}

func (al *testAncestorLookup) GetHeader(hash types.Hash) *types.Header {
	return al.headers[hash]
}

func (al *testAncestorLookup) GetBlock(hash types.Hash) *types.Block {
	return al.blocks[hash]
}

// --- helpers ---

func bpTestHeader(num uint64, parentHash types.Hash) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(num),
		ParentHash: parentHash,
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       1000 + num*12,
	}
}

func bpTestBlock(num uint64, parentHash types.Hash) *types.Block {
	h := bpTestHeader(num, parentHash)
	return types.NewBlock(h, nil)
}

func bpTestBlockWithUncles(num uint64, parentHash types.Hash, uncles []*types.Header) *types.Block {
	h := bpTestHeader(num, parentHash)
	body := &types.Body{Uncles: uncles}
	return types.NewBlock(h, body)
}

// buildBPChain creates a genesis block and a sequence of child blocks.
func buildBPChain(count int) (*types.Block, []*types.Block) {
	genesis := bpTestBlock(0, types.Hash{})
	blocks := make([]*types.Block, count)
	prev := genesis
	for i := 0; i < count; i++ {
		b := bpTestBlock(uint64(i+1), prev.Hash())
		blocks[i] = b
		prev = b
	}
	return genesis, blocks
}

// --- tests ---

func TestNewBlockProcessor(t *testing.T) {
	inserter := newTestBlockInserter(bpTestBlock(0, types.Hash{}))
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)
	if bp == nil {
		t.Fatal("NewBlockProcessor returned nil")
	}
	if bp.QueueSize() != 0 {
		t.Errorf("QueueSize = %d, want 0", bp.QueueSize())
	}
}

func TestNewBlockProcessor_DefaultConfig(t *testing.T) {
	inserter := newTestBlockInserter(bpTestBlock(0, types.Hash{}))
	bp := NewBlockProcessor(BlockProcessorConfig{}, inserter)
	if bp.config.MaxQueueSize != 4096 {
		t.Errorf("MaxQueueSize = %d, want 4096", bp.config.MaxQueueSize)
	}
	if bp.config.BatchSize != 64 {
		t.Errorf("BatchSize = %d, want 64", bp.config.BatchSize)
	}
}

func TestEnqueue_Success(t *testing.T) {
	genesis, blocks := buildBPChain(3)
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)

	for _, b := range blocks {
		if err := bp.Enqueue(b, nil); err != nil {
			t.Fatalf("Enqueue block %d: %v", b.NumberU64(), err)
		}
	}
	if bp.QueueSize() != 3 {
		t.Errorf("QueueSize = %d, want 3", bp.QueueSize())
	}
}

func TestEnqueue_Duplicate(t *testing.T) {
	genesis, blocks := buildBPChain(1)
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)

	bp.Enqueue(blocks[0], nil)
	err := bp.Enqueue(blocks[0], nil)
	if err != ErrDuplicateBlock {
		t.Errorf("expected ErrDuplicateBlock, got %v", err)
	}
}

func TestEnqueue_QueueFull(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.MaxQueueSize = 2
	bp := NewBlockProcessor(cfg, inserter)

	b1 := bpTestBlock(1, genesis.Hash())
	b2 := bpTestBlock(2, b1.Hash())
	b3 := bpTestBlock(3, b2.Hash())

	bp.Enqueue(b1, nil)
	bp.Enqueue(b2, nil)
	err := bp.Enqueue(b3, nil)
	if err != ErrQueueFull {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestEnqueue_Closed(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)
	bp.Close()

	err := bp.Enqueue(bpTestBlock(1, genesis.Hash()), nil)
	if err != ErrProcessorClosed {
		t.Errorf("expected ErrProcessorClosed, got %v", err)
	}
}

func TestProcessReady_EmptyQueue(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)
	bp.SetNextExpected(1)

	n, err := bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	if n != 0 {
		t.Errorf("processed = %d, want 0", n)
	}
}

func TestProcessReady_Contiguous(t *testing.T) {
	genesis, blocks := buildBPChain(3)
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	for _, b := range blocks {
		bp.Enqueue(b, nil)
	}

	n, err := bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	if n != 3 {
		t.Errorf("processed = %d, want 3", n)
	}
	if bp.NextExpected() != 4 {
		t.Errorf("NextExpected = %d, want 4", bp.NextExpected())
	}
}

func TestProcessReady_Gap(t *testing.T) {
	genesis, blocks := buildBPChain(3)
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	// Enqueue blocks 1 and 3 (skip 2).
	bp.Enqueue(blocks[0], nil)
	bp.Enqueue(blocks[2], nil)

	n, err := bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	// Only block 1 should be processed (block 2 missing breaks contiguity).
	if n != 1 {
		t.Errorf("processed = %d, want 1", n)
	}
	if bp.NextExpected() != 2 {
		t.Errorf("NextExpected = %d, want 2", bp.NextExpected())
	}
}

func TestProcessReady_ParentHashVerification(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	// Create a block with wrong parent hash.
	badParent := types.Hash{0xff}
	badBlock := bpTestBlock(1, badParent)
	bp.Enqueue(badBlock, nil)

	_, err := bp.ProcessReady()
	if err == nil {
		t.Fatal("expected error for bad parent hash")
	}
	if !errors.Is(err, ErrMissingParent) {
		t.Errorf("expected ErrMissingParent, got %v", err)
	}
}

func TestProcessReady_StateRootVerification(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = true
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	block := bpTestBlock(1, genesis.Hash())
	bp.Enqueue(block, nil)

	// Set executor that returns a mismatched state root.
	wrongRoot := types.Hash{0xde, 0xad}
	bp.SetExecutor(&testStateExecutor{stateRoot: wrongRoot})

	_, err := bp.ProcessReady()
	if err == nil {
		t.Fatal("expected state root mismatch error")
	}
	if !errors.Is(err, ErrStateRootMismatch) {
		t.Errorf("expected ErrStateRootMismatch, got %v", err)
	}
}

func TestProcessReady_StateRootMatch(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = true
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	block := bpTestBlock(1, genesis.Hash())
	bp.Enqueue(block, nil)

	// Set executor that returns the correct state root.
	bp.SetExecutor(&testStateExecutor{stateRoot: block.Root()})

	n, err := bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	if n != 1 {
		t.Errorf("processed = %d, want 1", n)
	}
}

func TestProcessReady_ReceiptRootVerification(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = true
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	block := bpTestBlock(1, genesis.Hash())
	receipts := []*types.Receipt{{Status: 1}}

	bp.Enqueue(block, receipts)

	// Set a hasher that returns a mismatched root.
	wrongRoot := types.Hash{0xba, 0xad}
	bp.SetReceiptHasher(&testReceiptHasher{root: wrongRoot})

	_, err := bp.ProcessReady()
	if err == nil {
		t.Fatal("expected receipt root mismatch error")
	}
	if !errors.Is(err, ErrBadReceiptRoot) {
		t.Errorf("expected ErrBadReceiptRoot, got %v", err)
	}
}

func TestProcessReady_ReceiptRootMatch(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = true
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	block := bpTestBlock(1, genesis.Hash())
	receipts := []*types.Receipt{{Status: 1}}

	bp.Enqueue(block, receipts)

	// Set a hasher that returns the matching receipt hash.
	bp.SetReceiptHasher(&testReceiptHasher{root: block.ReceiptHash()})

	n, err := bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	if n != 1 {
		t.Errorf("processed = %d, want 1", n)
	}
}

func TestVerifyUncles_TooMany(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = true
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	// Create 3 uncles (max is 2).
	uncles := []*types.Header{
		bpTestHeader(0, types.Hash{1}),
		bpTestHeader(0, types.Hash{2}),
		bpTestHeader(0, types.Hash{3}),
	}
	block := bpTestBlockWithUncles(1, genesis.Hash(), uncles)
	bp.Enqueue(block, nil)

	_, err := bp.ProcessReady()
	if err == nil {
		t.Fatal("expected uncle count error")
	}
	if !errors.Is(err, ErrBadUncleCount) {
		t.Errorf("expected ErrBadUncleCount, got %v", err)
	}
}

func TestVerifyUncles_Duplicate(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = true
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	uncle := bpTestHeader(0, types.Hash{1})
	block := bpTestBlockWithUncles(1, genesis.Hash(), []*types.Header{uncle, uncle})
	bp.Enqueue(block, nil)

	_, err := bp.ProcessReady()
	if err == nil {
		t.Fatal("expected duplicate uncle error")
	}
	if !errors.Is(err, ErrDuplicateUncle) {
		t.Errorf("expected ErrDuplicateUncle, got %v", err)
	}
}

func TestProcessReady_Closed(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)
	bp.Close()

	_, err := bp.ProcessReady()
	if err != ErrProcessorClosed {
		t.Errorf("expected ErrProcessorClosed, got %v", err)
	}
}

func TestClose(t *testing.T) {
	genesis, blocks := buildBPChain(3)
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)

	for _, b := range blocks {
		bp.Enqueue(b, nil)
	}
	if bp.QueueSize() != 3 {
		t.Fatalf("QueueSize before close = %d", bp.QueueSize())
	}

	bp.Close()
	if bp.QueueSize() != 0 {
		t.Errorf("QueueSize after close = %d, want 0", bp.QueueSize())
	}
}

func TestSetNextExpected(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)

	bp.SetNextExpected(42)
	if bp.NextExpected() != 42 {
		t.Errorf("NextExpected = %d, want 42", bp.NextExpected())
	}
}

func TestMetrics(t *testing.T) {
	genesis := bpTestBlock(0, types.Hash{})
	inserter := newTestBlockInserter(genesis)
	bp := NewBlockProcessor(DefaultBlockProcessorConfig(), inserter)

	m := bp.Metrics()
	if m == nil {
		t.Fatal("Metrics returned nil")
	}
	if m.BlocksQueued == nil || m.BlocksProcessed == nil {
		t.Fatal("metrics counters should not be nil")
	}
}

func TestProcessReady_InsertionFailure(t *testing.T) {
	genesis, blocks := buildBPChain(3)
	inserter := newTestBlockInserter(genesis)
	inserter.failAt = 2 // fail when 2nd block is inserted
	cfg := DefaultBlockProcessorConfig()
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	for _, b := range blocks {
		bp.Enqueue(b, nil)
	}

	n, err := bp.ProcessReady()
	if err == nil {
		t.Fatal("expected insertion error")
	}
	// First block should succeed, second should fail.
	if n != 1 {
		t.Errorf("processed = %d, want 1", n)
	}
}

func TestProcessReady_BatchSize(t *testing.T) {
	genesis, blocks := buildBPChain(5)
	inserter := newTestBlockInserter(genesis)
	cfg := DefaultBlockProcessorConfig()
	cfg.BatchSize = 2
	cfg.VerifyState = false
	cfg.VerifyReceipts = false
	cfg.VerifyUncles = false
	bp := NewBlockProcessor(cfg, inserter)
	bp.SetNextExpected(1)

	for _, b := range blocks {
		bp.Enqueue(b, nil)
	}

	// First call processes at most BatchSize=2 blocks.
	n, err := bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	if n != 2 {
		t.Errorf("processed = %d, want 2", n)
	}
	if bp.NextExpected() != 3 {
		t.Errorf("NextExpected = %d, want 3", bp.NextExpected())
	}

	// Second call processes the next batch.
	n, err = bp.ProcessReady()
	if err != nil {
		t.Fatalf("ProcessReady: %v", err)
	}
	if n != 2 {
		t.Errorf("processed = %d, want 2", n)
	}
}
