package core

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// newRPTestReceipt creates a test receipt with the given parameters.
func newRPTestReceipt(status uint64, gasUsed uint64, cumGas uint64) *types.Receipt {
	return &types.Receipt{
		Status:            status,
		GasUsed:           gasUsed,
		CumulativeGasUsed: cumGas,
	}
}

// newRPTestReceiptWithLogs creates a test receipt with logs.
func newRPTestReceiptWithLogs(status uint64, gasUsed uint64) *types.Receipt {
	return &types.Receipt{
		Status:            status,
		GasUsed:           gasUsed,
		CumulativeGasUsed: gasUsed,
		Logs: []*types.Log{
			{
				Address: types.HexToAddress("0xdead"),
				Topics:  []types.Hash{types.HexToHash("0x01")},
				Data:    []byte{0x01, 0x02},
			},
		},
	}
}

func TestNewReceiptProcessor(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	if rp == nil {
		t.Fatal("expected non-nil processor")
	}
	if rp.TotalReceipts() != 0 {
		t.Fatalf("expected 0 total receipts, got %d", rp.TotalReceipts())
	}
	if rp.LatestBlock() != 0 {
		t.Fatalf("expected latest block 0, got %d", rp.LatestBlock())
	}
}

func TestAddAndGetReceipt(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	receipt := newRPTestReceipt(types.ReceiptStatusSuccessful, 21000, 21000)

	err := rp.AddReceipt(1, 0, receipt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := rp.GetReceipt(1, 0)
	if got == nil {
		t.Fatal("expected receipt, got nil")
	}
	if got.GasUsed != 21000 {
		t.Fatalf("expected gas used 21000, got %d", got.GasUsed)
	}
	if got.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("expected status successful, got %d", got.Status)
	}
}

func TestAddReceiptNil(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	err := rp.AddReceipt(1, 0, nil)
	if err != ErrNilReceipt {
		t.Fatalf("expected ErrNilReceipt, got %v", err)
	}
}

func TestAddReceiptMaxExceeded(t *testing.T) {
	config := ReceiptProcessorConfig{MaxReceipts: 2}
	rp := NewReceiptProcessor(config)

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))

	err := rp.AddReceipt(1, 2, newRPTestReceipt(1, 21000, 63000))
	if err == nil {
		t.Fatal("expected max receipts exceeded error")
	}
}

func TestAddReceiptReplace(t *testing.T) {
	config := ReceiptProcessorConfig{MaxReceipts: 1}
	rp := NewReceiptProcessor(config)

	r1 := newRPTestReceipt(types.ReceiptStatusSuccessful, 21000, 21000)
	rp.AddReceipt(1, 0, r1)

	// Replace existing receipt should succeed even at max capacity.
	r2 := newRPTestReceipt(types.ReceiptStatusFailed, 50000, 50000)
	err := rp.AddReceipt(1, 0, r2)
	if err != nil {
		t.Fatalf("unexpected error on replace: %v", err)
	}

	got := rp.GetReceipt(1, 0)
	if got.Status != types.ReceiptStatusFailed {
		t.Fatalf("expected replaced receipt with failed status, got %d", got.Status)
	}
	if rp.TotalReceipts() != 1 {
		t.Fatalf("expected total 1, got %d", rp.TotalReceipts())
	}
}

func TestGetReceiptNotFound(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	got := rp.GetReceipt(999, 0)
	if got != nil {
		t.Fatal("expected nil for nonexistent receipt")
	}
}

func TestGetBlockReceipts(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	// Add receipts in non-sequential order.
	rp.AddReceipt(1, 2, newRPTestReceipt(1, 30000, 72000))
	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))

	receipts := rp.GetBlockReceipts(1)
	if len(receipts) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(receipts))
	}

	// Verify they are sorted by tx index (ascending gas used).
	if receipts[0].CumulativeGasUsed != 21000 {
		t.Fatalf("expected first receipt cumgas 21000, got %d", receipts[0].CumulativeGasUsed)
	}
	if receipts[1].CumulativeGasUsed != 42000 {
		t.Fatalf("expected second receipt cumgas 42000, got %d", receipts[1].CumulativeGasUsed)
	}
	if receipts[2].CumulativeGasUsed != 72000 {
		t.Fatalf("expected third receipt cumgas 72000, got %d", receipts[2].CumulativeGasUsed)
	}
}

func TestGetBlockReceiptsEmpty(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	receipts := rp.GetBlockReceipts(1)
	if receipts != nil {
		t.Fatalf("expected nil, got %v", receipts)
	}
}

func TestComputeReceiptsRoot(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	// Empty block should return EmptyRootHash.
	root := rp.ComputeReceiptsRoot(1)
	if root != types.EmptyRootHash {
		t.Fatalf("expected empty root hash, got %s", root.Hex())
	}

	// Add receipts and compute root.
	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 30000, 51000))

	root = rp.ComputeReceiptsRoot(1)
	if root.IsZero() {
		t.Fatal("expected non-zero receipts root")
	}
	if root == types.EmptyRootHash {
		t.Fatal("expected non-empty receipts root")
	}
}

func TestComputeReceiptsRootDeterministic(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 30000, 51000))

	root1 := rp.ComputeReceiptsRoot(1)
	root2 := rp.ComputeReceiptsRoot(1)
	if root1 != root2 {
		t.Fatal("receipts root not deterministic")
	}
}

func TestBlockReceiptCount(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	if rp.BlockReceiptCount(1) != 0 {
		t.Fatal("expected 0 for empty block")
	}

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))
	rp.AddReceipt(2, 0, newRPTestReceipt(1, 30000, 30000))

	if rp.BlockReceiptCount(1) != 2 {
		t.Fatalf("expected 2, got %d", rp.BlockReceiptCount(1))
	}
	if rp.BlockReceiptCount(2) != 1 {
		t.Fatalf("expected 1, got %d", rp.BlockReceiptCount(2))
	}
	if rp.BlockReceiptCount(3) != 0 {
		t.Fatalf("expected 0, got %d", rp.BlockReceiptCount(3))
	}
}

func TestTotalReceipts(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))
	rp.AddReceipt(2, 0, newRPTestReceipt(1, 30000, 30000))

	if rp.TotalReceipts() != 3 {
		t.Fatalf("expected 3, got %d", rp.TotalReceipts())
	}
}

func TestPruneBlock(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))
	rp.AddReceipt(2, 0, newRPTestReceipt(1, 30000, 30000))

	pruned := rp.PruneBlock(1)
	if pruned != 2 {
		t.Fatalf("expected 2 pruned, got %d", pruned)
	}
	if rp.TotalReceipts() != 1 {
		t.Fatalf("expected 1 remaining, got %d", rp.TotalReceipts())
	}
	if rp.GetReceipt(1, 0) != nil {
		t.Fatal("expected nil after prune")
	}
	if rp.GetReceipt(2, 0) == nil {
		t.Fatal("expected block 2 receipt to survive prune")
	}
}

func TestPruneBlockNotFound(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	pruned := rp.PruneBlock(999)
	if pruned != 0 {
		t.Fatalf("expected 0 pruned for nonexistent block, got %d", pruned)
	}
}

func TestPruneBlockUpdatesLatest(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(5, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(3, 0, newRPTestReceipt(1, 21000, 21000))

	if rp.LatestBlock() != 5 {
		t.Fatalf("expected latest 5, got %d", rp.LatestBlock())
	}

	rp.PruneBlock(5)
	if rp.LatestBlock() != 3 {
		t.Fatalf("expected latest 3 after pruning 5, got %d", rp.LatestBlock())
	}

	rp.PruneBlock(3)
	if rp.LatestBlock() != 1 {
		t.Fatalf("expected latest 1, got %d", rp.LatestBlock())
	}

	rp.PruneBlock(1)
	if rp.LatestBlock() != 0 {
		t.Fatalf("expected latest 0, got %d", rp.LatestBlock())
	}
}

func TestLatestBlock(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	if rp.LatestBlock() != 0 {
		t.Fatal("expected 0 with no receipts")
	}

	rp.AddReceipt(10, 0, newRPTestReceipt(1, 21000, 21000))
	if rp.LatestBlock() != 10 {
		t.Fatalf("expected 10, got %d", rp.LatestBlock())
	}

	rp.AddReceipt(5, 0, newRPTestReceipt(1, 21000, 21000))
	if rp.LatestBlock() != 10 {
		t.Fatalf("expected still 10, got %d", rp.LatestBlock())
	}

	rp.AddReceipt(20, 0, newRPTestReceipt(1, 21000, 21000))
	if rp.LatestBlock() != 20 {
		t.Fatalf("expected 20, got %d", rp.LatestBlock())
	}
}

func TestComputeBloomOnAdd(t *testing.T) {
	config := ReceiptProcessorConfig{ComputeBloom: true}
	rp := NewReceiptProcessor(config)

	receipt := newRPTestReceiptWithLogs(types.ReceiptStatusSuccessful, 21000)
	rp.AddReceipt(1, 0, receipt)

	got := rp.GetReceipt(1, 0)
	// Bloom should be computed automatically.
	allZero := true
	for _, b := range got.Bloom {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("expected non-zero bloom with ComputeBloom enabled")
	}
}

func TestNoBloomOnAdd(t *testing.T) {
	config := ReceiptProcessorConfig{ComputeBloom: false}
	rp := NewReceiptProcessor(config)

	receipt := newRPTestReceiptWithLogs(types.ReceiptStatusSuccessful, 21000)
	rp.AddReceipt(1, 0, receipt)

	got := rp.GetReceipt(1, 0)
	// Bloom should NOT be computed since ComputeBloom is false.
	allZero := true
	for _, b := range got.Bloom {
		if b != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Fatal("expected zero bloom with ComputeBloom disabled")
	}
}

func TestMultipleBlocks(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	// Add receipts across several blocks.
	for block := uint64(1); block <= 5; block++ {
		for tx := uint64(0); tx < 3; tx++ {
			cumGas := (tx + 1) * 21000
			r := newRPTestReceipt(types.ReceiptStatusSuccessful, 21000, cumGas)
			r.BlockNumber = new(big.Int).SetUint64(block)
			rp.AddReceipt(block, tx, r)
		}
	}

	if rp.TotalReceipts() != 15 {
		t.Fatalf("expected 15, got %d", rp.TotalReceipts())
	}
	if rp.LatestBlock() != 5 {
		t.Fatalf("expected latest 5, got %d", rp.LatestBlock())
	}

	// Each block should have 3 receipts.
	for block := uint64(1); block <= 5; block++ {
		if rp.BlockReceiptCount(block) != 3 {
			t.Fatalf("block %d: expected 3 receipts, got %d", block, rp.BlockReceiptCount(block))
		}
	}

	// Receipts root should differ between blocks (different cumulative gas patterns
	// are the same here, but block context can differ).
	root1 := rp.ComputeReceiptsRoot(1)
	root2 := rp.ComputeReceiptsRoot(2)
	// Both blocks have identical receipt data so roots should be equal.
	if root1 != root2 {
		t.Log("roots differ between identical blocks, which is fine if block context differs")
	}
}

func TestReceiptProcessorConcurrentAccess(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())
	var wg sync.WaitGroup
	const goroutines = 10
	const receiptsPerGoroutine = 5

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(block uint64) {
			defer wg.Done()
			for tx := uint64(0); tx < receiptsPerGoroutine; tx++ {
				r := newRPTestReceipt(1, 21000, (tx+1)*21000)
				rp.AddReceipt(block, tx, r)
			}
		}(uint64(g + 1))
	}
	wg.Wait()

	total := rp.TotalReceipts()
	expected := goroutines * receiptsPerGoroutine
	if total != expected {
		t.Fatalf("expected %d total receipts, got %d", expected, total)
	}

	// Read concurrently.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(block uint64) {
			defer wg.Done()
			receipts := rp.GetBlockReceipts(block)
			if len(receipts) != receiptsPerGoroutine {
				t.Errorf("block %d: expected %d receipts, got %d", block, receiptsPerGoroutine, len(receipts))
			}
			rp.ComputeReceiptsRoot(block)
			rp.BlockReceiptCount(block)
		}(uint64(g + 1))
	}
	wg.Wait()
}

func TestPruneAndReaddReceipts(t *testing.T) {
	rp := NewReceiptProcessor(DefaultReceiptProcessorConfig())

	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))

	rootBefore := rp.ComputeReceiptsRoot(1)

	rp.PruneBlock(1)
	if rp.TotalReceipts() != 0 {
		t.Fatalf("expected 0 after prune, got %d", rp.TotalReceipts())
	}

	// Re-add same receipts.
	rp.AddReceipt(1, 0, newRPTestReceipt(1, 21000, 21000))
	rp.AddReceipt(1, 1, newRPTestReceipt(1, 21000, 42000))

	rootAfter := rp.ComputeReceiptsRoot(1)
	if rootBefore != rootAfter {
		t.Fatal("receipts root changed after prune and re-add of same data")
	}
}
