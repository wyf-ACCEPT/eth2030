package sync

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- mock implementations for chain inserter tests ---

// ciBlockInserter is a mock BlockInserter.
type ciBlockInserter struct {
	blocks  []*types.Block
	current *types.Block
	failAt  int // if > 0, fail at this total block count
}

func newCIBlockInserter(genesis *types.Block) *ciBlockInserter {
	return &ciBlockInserter{
		blocks:  []*types.Block{genesis},
		current: genesis,
	}
}

func (bi *ciBlockInserter) InsertChain(blocks []*types.Block) (int, error) {
	startLen := len(bi.blocks)
	for i, b := range blocks {
		if bi.failAt > 0 && startLen+i >= bi.failAt {
			return i, errors.New("mock insert failure")
		}
		bi.blocks = append(bi.blocks, b)
		bi.current = b
	}
	return len(blocks), nil
}

func (bi *ciBlockInserter) CurrentBlock() *types.Block {
	return bi.current
}

// ciBlockExecutor is a mock BlockExecutor.
type ciBlockExecutor struct {
	stateRoot types.Hash
	receipts  []*types.Receipt
	err       error
}

func (e *ciBlockExecutor) ExecuteBlock(block *types.Block) (types.Hash, []*types.Receipt, error) {
	return e.stateRoot, e.receipts, e.err
}

// ciBlockCommitter is a mock BlockCommitter.
type ciBlockCommitter struct {
	committed []*types.Block
	err       error
}

func (c *ciBlockCommitter) CommitBlock(block *types.Block) error {
	if c.err != nil {
		return c.err
	}
	c.committed = append(c.committed, block)
	return nil
}

// --- helpers ---

func ciTestHeader(num uint64, parentHash types.Hash) *types.Header {
	return &types.Header{
		Number:      new(big.Int).SetUint64(num),
		ParentHash:  parentHash,
		Difficulty:  new(big.Int),
		GasLimit:    30_000_000,
		GasUsed:     0,
		Time:        1000 + num*12,
		Root:        types.Hash{byte(num)},
		ReceiptHash: types.EmptyRootHash,
	}
}

func ciTestBlock(num uint64, parentHash types.Hash) *types.Block {
	h := ciTestHeader(num, parentHash)
	return types.NewBlock(h, nil)
}

func ciTestChain(start, end uint64) []*types.Block {
	var blocks []*types.Block
	var parentHash types.Hash
	if start > 0 {
		// Create genesis.
		genesis := ciTestBlock(start-1, types.Hash{})
		parentHash = genesis.Hash()
	}
	for i := start; i <= end; i++ {
		block := ciTestBlock(i, parentHash)
		blocks = append(blocks, block)
		parentHash = block.Hash()
	}
	return blocks
}

// --- tests ---

func TestCINewChainInserter(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)
	config := DefaultChainInserterConfig()

	ci := NewChainInserter(config, inserter)
	if ci == nil {
		t.Fatal("NewChainInserter returned nil")
	}

	prog := ci.Progress()
	if prog.BlocksInserted != 0 {
		t.Errorf("BlocksInserted = %d, want 0", prog.BlocksInserted)
	}
}

func TestCIInsertBlocksSingle(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false // no executor in this test
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	block1 := ciTestBlock(1, genesis.Hash())
	n, err := ci.InsertBlocks([]*types.Block{block1})
	if err != nil {
		t.Fatalf("InsertBlocks: %v", err)
	}
	if n != 1 {
		t.Errorf("InsertBlocks returned %d, want 1", n)
	}

	prog := ci.Progress()
	if prog.BlocksInserted != 1 {
		t.Errorf("BlocksInserted = %d, want 1", prog.BlocksInserted)
	}
	if prog.LastBlockNum != 1 {
		t.Errorf("LastBlockNum = %d, want 1", prog.LastBlockNum)
	}
}

func TestCIInsertBlocksMultiple(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	block1 := ciTestBlock(1, genesis.Hash())
	block2 := ciTestBlock(2, block1.Hash())
	block3 := ciTestBlock(3, block2.Hash())

	n, err := ci.InsertBlocks([]*types.Block{block1, block2, block3})
	if err != nil {
		t.Fatalf("InsertBlocks: %v", err)
	}
	if n != 3 {
		t.Errorf("InsertBlocks returned %d, want 3", n)
	}

	prog := ci.Progress()
	if prog.BlocksInserted != 3 {
		t.Errorf("BlocksInserted = %d, want 3", prog.BlocksInserted)
	}
}

func TestCIInsertBlocksEmpty(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)
	ci := NewChainInserter(DefaultChainInserterConfig(), inserter)

	_, err := ci.InsertBlocks(nil)
	if !errors.Is(err, ErrCIEmptyBatch) {
		t.Errorf("expected ErrCIEmptyBatch, got %v", err)
	}
}

func TestCIInsertBlocksClosed(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)
	ci := NewChainInserter(DefaultChainInserterConfig(), inserter)
	ci.Close()

	block := ciTestBlock(1, genesis.Hash())
	_, err := ci.InsertBlocks([]*types.Block{block})
	if !errors.Is(err, ErrCIClosedState) {
		t.Errorf("expected ErrCIClosedState, got %v", err)
	}
}

func TestCIInsertBlocksParentMismatch(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	// Create a block with wrong parent.
	badBlock := ciTestBlock(1, types.Hash{0xff})
	_, err := ci.InsertBlocks([]*types.Block{badBlock})
	if !errors.Is(err, ErrCIParentMismatch) {
		t.Errorf("expected ErrCIParentMismatch, got %v", err)
	}
}

func TestCIValidateStateRoot(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)
	ci.SetExecutor(&ciBlockExecutor{
		stateRoot: types.Hash{0xaa}, // different from block's Root
	})

	block := ciTestBlock(1, genesis.Hash())
	_, err := ci.InsertBlocks([]*types.Block{block})
	if !errors.Is(err, ErrCIStateRoot) {
		t.Errorf("expected ErrCIStateRoot, got %v", err)
	}
}

func TestCIValidateStateRootCorrect(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	block := ciTestBlock(1, genesis.Hash())
	// Executor returns matching state root.
	ci.SetExecutor(&ciBlockExecutor{
		stateRoot: block.Root(),
	})

	n, err := ci.InsertBlocks([]*types.Block{block})
	if err != nil {
		t.Fatalf("InsertBlocks: %v", err)
	}
	if n != 1 {
		t.Errorf("InsertBlocks returned %d, want 1", n)
	}
}

func TestCIValidateReceiptRoot(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	block := ciTestBlock(1, genesis.Hash())
	receipts := []*types.Receipt{
		{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21000},
	}
	// The computed receipt root won't match EmptyRootHash.
	ci.SetExecutor(&ciBlockExecutor{
		stateRoot: block.Root(),
		receipts:  receipts,
	})

	_, err := ci.InsertBlocks([]*types.Block{block})
	if !errors.Is(err, ErrCIReceiptRoot) {
		t.Errorf("expected ErrCIReceiptRoot, got %v", err)
	}
}

func TestCIValidateLogsBloom(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	// Create block with specific ReceiptHash and zero bloom.
	receipts := []*types.Receipt{
		{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 0},
	}
	receiptRoot := types.DeriveSha(receipts)
	bloom := types.CreateBloom(receipts) // should be zero bloom for no logs

	h := &types.Header{
		Number:      big.NewInt(1),
		ParentHash:  genesis.Hash(),
		Difficulty:  new(big.Int),
		Root:        types.Hash{1},
		ReceiptHash: receiptRoot,
		Bloom:       types.Bloom{0xff}, // wrong bloom
		GasUsed:     0,
		Time:        1012,
	}
	block := types.NewBlock(h, nil)

	config := DefaultChainInserterConfig()
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)
	ci.SetExecutor(&ciBlockExecutor{
		stateRoot: block.Root(),
		receipts:  receipts,
	})

	_, err := ci.InsertBlocks([]*types.Block{block})
	if !errors.Is(err, ErrCILogsBloom) {
		t.Errorf("expected ErrCILogsBloom, got %v (bloom=%x)", err, bloom)
	}
}

func TestCIValidateGasUsed(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	receipts := []*types.Receipt{
		{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21000},
	}
	receiptRoot := types.DeriveSha(receipts)
	bloom := types.CreateBloom(receipts)

	h := &types.Header{
		Number:      big.NewInt(1),
		ParentHash:  genesis.Hash(),
		Difficulty:  new(big.Int),
		Root:        types.Hash{1},
		ReceiptHash: receiptRoot,
		Bloom:       bloom,
		GasUsed:     999, // wrong, should be 21000
		Time:        1012,
	}
	block := types.NewBlock(h, nil)

	config := DefaultChainInserterConfig()
	ci := NewChainInserter(config, inserter)
	ci.SetExecutor(&ciBlockExecutor{
		stateRoot: block.Root(),
		receipts:  receipts,
	})

	_, err := ci.InsertBlocks([]*types.Block{block})
	if !errors.Is(err, ErrCIGasUsed) {
		t.Errorf("expected ErrCIGasUsed, got %v", err)
	}
}

func TestCIValidateGasUsedCorrect(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	receipts := []*types.Receipt{
		{Status: types.ReceiptStatusSuccessful, CumulativeGasUsed: 21000},
	}
	receiptRoot := types.DeriveSha(receipts)
	bloom := types.CreateBloom(receipts)

	h := &types.Header{
		Number:      big.NewInt(1),
		ParentHash:  genesis.Hash(),
		Difficulty:  new(big.Int),
		Root:        types.Hash{1},
		ReceiptHash: receiptRoot,
		Bloom:       bloom,
		GasUsed:     21000,
		Time:        1012,
	}
	block := types.NewBlock(h, nil)

	config := DefaultChainInserterConfig()
	ci := NewChainInserter(config, inserter)
	ci.SetExecutor(&ciBlockExecutor{
		stateRoot: block.Root(),
		receipts:  receipts,
	})

	n, err := ci.InsertBlocks([]*types.Block{block})
	if err != nil {
		t.Fatalf("InsertBlocks: %v", err)
	}
	if n != 1 {
		t.Errorf("InsertBlocks returned %d, want 1", n)
	}
}

func TestCIInsertBatch(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := ChainInserterConfig{
		BatchSize:       2,
		VerifyStateRoot: false,
		VerifyReceipts:  false,
		VerifyBloom:     false,
		VerifyGasUsed:   false,
	}

	ci := NewChainInserter(config, inserter)

	block1 := ciTestBlock(1, genesis.Hash())
	block2 := ciTestBlock(2, block1.Hash())
	block3 := ciTestBlock(3, block2.Hash())

	n, err := ci.InsertBatch([]*types.Block{block1, block2, block3})
	if err != nil {
		t.Fatalf("InsertBatch: %v", err)
	}
	if n != 3 {
		t.Errorf("InsertBatch returned %d, want 3", n)
	}

	prog := ci.Progress()
	if prog.BlocksInserted != 3 {
		t.Errorf("BlocksInserted = %d, want 3", prog.BlocksInserted)
	}
}

func TestCIInsertBatchFailure(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)
	inserter.failAt = 3 // fail at 3rd total block (genesis + 2 inserts)

	config := ChainInserterConfig{
		BatchSize:       10,
		VerifyStateRoot: false,
		VerifyReceipts:  false,
		VerifyBloom:     false,
		VerifyGasUsed:   false,
	}

	ci := NewChainInserter(config, inserter)

	block1 := ciTestBlock(1, genesis.Hash())
	block2 := ciTestBlock(2, block1.Hash())
	block3 := ciTestBlock(3, block2.Hash())

	n, err := ci.InsertBatch([]*types.Block{block1, block2, block3})
	if err == nil {
		t.Fatal("expected insert failure")
	}
	if n != 2 {
		t.Errorf("InsertBatch returned %d, want 2 (failed at third)", n)
	}
}

func TestCIExecutionError(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	ci := NewChainInserter(config, inserter)
	ci.SetExecutor(&ciBlockExecutor{
		err: errors.New("execution failed"),
	})

	block := ciTestBlock(1, genesis.Hash())
	_, err := ci.InsertBlocks([]*types.Block{block})
	if !errors.Is(err, ErrCIExecutionFailed) {
		t.Errorf("expected ErrCIExecutionFailed, got %v", err)
	}
}

func TestCICommitter(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	committer := &ciBlockCommitter{}
	ci := NewChainInserter(config, inserter)
	ci.SetCommitter(committer)

	block := ciTestBlock(1, genesis.Hash())
	n, err := ci.InsertBlocks([]*types.Block{block})
	if err != nil {
		t.Fatalf("InsertBlocks: %v", err)
	}
	if n != 1 {
		t.Errorf("InsertBlocks returned %d, want 1", n)
	}
	if len(committer.committed) != 1 {
		t.Errorf("committed = %d blocks, want 1", len(committer.committed))
	}
}

func TestCICommitterError(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	committer := &ciBlockCommitter{err: errors.New("commit failed")}
	ci := NewChainInserter(config, inserter)
	ci.SetCommitter(committer)

	block := ciTestBlock(1, genesis.Hash())
	_, err := ci.InsertBlocks([]*types.Block{block})
	if err == nil {
		t.Fatal("expected commit error")
	}
}

func TestCIProgressMetrics(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	block1 := ciTestBlock(1, genesis.Hash())
	block2 := ciTestBlock(2, block1.Hash())

	ci.InsertBlocks([]*types.Block{block1, block2})

	m := ci.Metrics()
	if m.BlocksInserted.Value() != 2 {
		t.Errorf("BlocksInserted metric = %d, want 2", m.BlocksInserted.Value())
	}

	prog := ci.Progress()
	if prog.BlocksPerSecond() < 0 {
		t.Error("BlocksPerSecond should not be negative")
	}
}

func TestCIReset(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	config := DefaultChainInserterConfig()
	config.VerifyStateRoot = false
	config.VerifyReceipts = false
	config.VerifyBloom = false
	config.VerifyGasUsed = false

	ci := NewChainInserter(config, inserter)

	block := ciTestBlock(1, genesis.Hash())
	ci.InsertBlocks([]*types.Block{block})

	ci.Reset()
	prog := ci.Progress()
	if prog.BlocksInserted != 0 {
		t.Errorf("BlocksInserted after reset = %d, want 0", prog.BlocksInserted)
	}
}

func TestCIProgressBlocksPerSecond(t *testing.T) {
	p := CIProgress{BlocksInserted: 0}
	if p.BlocksPerSecond() != 0 {
		t.Errorf("BlocksPerSecond for zero blocks = %f, want 0", p.BlocksPerSecond())
	}

	p2 := CIProgress{TxsProcessed: 0}
	if p2.TxPerSecond() != 0 {
		t.Errorf("TxPerSecond for zero txs = %f, want 0", p2.TxPerSecond())
	}
}

func TestCINoExecutor(t *testing.T) {
	genesis := ciTestBlock(0, types.Hash{})
	inserter := newCIBlockInserter(genesis)

	// No executor configured -- validation should pass.
	config := DefaultChainInserterConfig()
	ci := NewChainInserter(config, inserter)

	block := ciTestBlock(1, genesis.Hash())
	n, err := ci.InsertBlocks([]*types.Block{block})
	if err != nil {
		t.Fatalf("InsertBlocks without executor: %v", err)
	}
	if n != 1 {
		t.Errorf("InsertBlocks returned %d, want 1", n)
	}
}
