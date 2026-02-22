package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// Helper to create a minimal chain config with Hogota enabled.
func testGigagasChainConfig() *ChainConfig {
	hogotaTime := uint64(1000)
	return &ChainConfig{
		ChainID:    big.NewInt(1),
		HogotaTime: &hogotaTime,
	}
}

// Helper to create a test block with no transactions.
func testEmptyBlock(num int64) *types.Block {
	header := &types.Header{Number: big.NewInt(num), GasLimit: 1_000_000}
	return types.NewBlock(header, nil)
}

// Helper to create a test transaction.
func testGigagasTx(nonce uint64, to types.Address, gas uint64) *types.Transaction {
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		To:       &to,
		Value:    big.NewInt(100),
		Gas:      gas,
		GasPrice: big.NewInt(1),
	})
	return tx
}

// Helper to create a block with transactions.
func testBlockWithTxs(num int64, txs []*types.Transaction) *types.Block {
	header := &types.Header{Number: big.NewInt(num), GasLimit: 1_000_000}
	body := &types.Body{Transactions: txs}
	return types.NewBlock(header, body)
}

func TestGigagasBlockProcessorNew(t *testing.T) {
	cfg := DefaultGigagasBlockConfig()
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(cfg, cc, nil)
	if proc == nil {
		t.Fatal("processor should not be nil")
	}
	if proc.CurrentWorkers() < 2 {
		t.Errorf("expected at least 2 workers, got %d", proc.CurrentWorkers())
	}
}

func TestGigagasBlockProcessorIsEnabled(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)

	// Before Hogota.
	if proc.IsEnabled(500) {
		t.Error("should not be enabled before Hogota")
	}
	// At Hogota.
	if !proc.IsEnabled(1000) {
		t.Error("should be enabled at Hogota")
	}
	// After Hogota.
	if !proc.IsEnabled(2000) {
		t.Error("should be enabled after Hogota")
	}

	// Nil chain config.
	proc2 := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), nil, nil)
	if proc2.IsEnabled(2000) {
		t.Error("should not be enabled with nil chain config")
	}
}

func TestGigagasBlockProcessorNilBlock(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)
	stateDB := state.NewShardedStateDB()

	_, err := proc.ProcessBlockParallel(nil, stateDB, 1_000_000, 2000)
	if err != ErrGigagasNilBlock {
		t.Fatalf("expected nil block error, got %v", err)
	}
}

func TestGigagasBlockProcessorNilState(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)
	block := testEmptyBlock(1)

	_, err := proc.ProcessBlockParallel(block, nil, 1_000_000, 2000)
	if err != ErrGigagasNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}
}

func TestGigagasBlockProcessorNotEnabled(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)
	stateDB := state.NewShardedStateDB()
	block := testEmptyBlock(1)

	// Block time before Hogota.
	_, err := proc.ProcessBlockParallel(block, stateDB, 1_000_000, 500)
	if err != ErrGigagasNotEnabled {
		t.Fatalf("expected not enabled error, got %v", err)
	}
}

func TestGigagasBlockProcessorEmptyBlock(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)
	stateDB := state.NewShardedStateDB()
	block := testEmptyBlock(1)

	result, err := proc.ProcessBlockParallel(block, stateDB, 1_000_000, 2000)
	if err != nil {
		t.Fatalf("empty block failed: %v", err)
	}
	if result.GasUsed != 0 {
		t.Errorf("expected 0 gas used, got %d", result.GasUsed)
	}
	if len(result.Receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(result.Receipts))
	}
}

func TestGigagasBlockProcessorWithTransactions(t *testing.T) {
	cc := testGigagasChainConfig()

	// Custom exec func that charges the tx gas.
	execFunc := func(tx *types.Transaction, _ interface{}) (uint64, []types.Hash, error) {
		return tx.Gas(), nil, nil
	}

	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, execFunc)
	stateDB := state.NewShardedStateDB()

	to := types.Address{0x30}
	tx1 := testGigagasTx(0, to, 21000)
	tx2 := testGigagasTx(1, to, 42000)

	// Set different senders on the transactions.
	sender1 := types.Address{0x10}
	sender2 := types.Address{0x20}
	tx1.SetSender(sender1)
	tx2.SetSender(sender2)

	block := testBlockWithTxs(10, []*types.Transaction{tx1, tx2})

	result, err := proc.ProcessBlockParallel(block, stateDB, 1_000_000, 2000)
	if err != nil {
		t.Fatalf("block processing failed: %v", err)
	}
	if result.GasUsed != 63000 {
		t.Errorf("expected 63000 gas used, got %d", result.GasUsed)
	}
	if len(result.Receipts) != 2 {
		t.Fatalf("expected 2 receipts, got %d", len(result.Receipts))
	}
	for i, r := range result.Receipts {
		if !r.Success {
			t.Errorf("receipt %d should be successful", i)
		}
	}
	if result.WorkerCount < 2 {
		t.Errorf("expected at least 2 workers, got %d", result.WorkerCount)
	}
}

func TestGigagasBlockProcessorMetrics(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)
	stateDB := state.NewShardedStateDB()

	to := types.Address{0x02}
	tx := testGigagasTx(0, to, 21000)
	sender := types.Address{0x01}
	tx.SetSender(sender)

	block := testBlockWithTxs(1, []*types.Transaction{tx})

	_, err := proc.ProcessBlockParallel(block, stateDB, 1_000_000, 2000)
	if err != nil {
		t.Fatalf("processing failed: %v", err)
	}

	blocks, conflicts := proc.Metrics()
	if blocks != 1 {
		t.Errorf("expected 1 block, got %d", blocks)
	}
	_ = conflicts // conflicts depend on tx overlap
}

func TestGigagasBlockProcessorAdaptiveWorkers(t *testing.T) {
	cc := testGigagasChainConfig()
	cfg := DefaultGigagasBlockConfig()
	cfg.MinWorkers = 2
	cfg.MaxWorkers = 8
	cfg.AdaptiveEnabled = true

	proc := NewGigagasBlockProcessor(cfg, cc, nil)

	// Record some gas rates to trigger adaptation.
	proc.rateTracker.RecordBlockGas(1, 100_000_000, 1000)
	proc.rateTracker.RecordBlockGas(2, 100_000_000, 1012)

	// The rate is below target, so adaptation should increase workers.
	initialWorkers := proc.CurrentWorkers()
	proc.adaptWorkerCount()
	newWorkers := proc.CurrentWorkers()

	// Workers should be >= initial (or at max).
	if newWorkers < initialWorkers && newWorkers != cfg.MaxWorkers {
		t.Errorf("workers should not decrease when under target: %d -> %d", initialWorkers, newWorkers)
	}
}

func TestGigagasBlockProcessorDefaultConfig(t *testing.T) {
	cfg := DefaultGigagasBlockConfig()
	if cfg.MinWorkers < 2 {
		t.Error("min workers should be at least 2")
	}
	if cfg.TargetGasPerSec != 1_000_000_000 {
		t.Error("target gas per sec should be 1 Ggas")
	}
	if cfg.ConflictRetries != 3 {
		t.Error("conflict retries should be 3")
	}
}

func TestGigagasBlockProcessorCurrentGasRate(t *testing.T) {
	cc := testGigagasChainConfig()
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, nil)

	// Initially zero.
	if rate := proc.CurrentGasRate(); rate != 0 {
		t.Errorf("expected 0 initial rate, got %f", rate)
	}

	// Record some blocks.
	proc.rateTracker.RecordBlockGas(1, 500_000_000, 1000)
	proc.rateTracker.RecordBlockGas(2, 500_000_000, 1012)

	rate := proc.CurrentGasRate()
	if rate <= 0 {
		t.Error("expected positive gas rate after recording blocks")
	}
}

func TestGigagasIntegrationTestEndToEnd(t *testing.T) {
	cc := testGigagasChainConfig()
	execFunc := func(tx *types.Transaction, _ interface{}) (uint64, []types.Hash, error) {
		return tx.Gas(), nil, nil
	}
	proc := NewGigagasBlockProcessor(DefaultGigagasBlockConfig(), cc, execFunc)
	stateDB := state.NewShardedStateDB()

	t.Run("basic pipeline", func(t *testing.T) {
		to := types.Address{0x30}
		tx1 := testGigagasTx(0, to, 21000)
		tx2 := testGigagasTx(1, to, 42000)
		tx1.SetSender(types.Address{0x10})
		tx2.SetSender(types.Address{0x20})

		result, err := proc.IntegrationTest(
			[]*types.Transaction{tx1, tx2}, stateDB, 1_000_000, 2000, 1,
		)
		if err != nil {
			t.Fatalf("IntegrationTest: %v", err)
		}
		if result.GasUsed != 63000 {
			t.Errorf("GasUsed = %d, want 63000", result.GasUsed)
		}
		if len(result.Receipts) != 2 {
			t.Errorf("Receipts count = %d, want 2", len(result.Receipts))
		}
	})

	t.Run("empty txs", func(t *testing.T) {
		result, err := proc.IntegrationTest(
			nil, stateDB, 1_000_000, 2000, 2,
		)
		if err != nil {
			t.Fatalf("IntegrationTest: %v", err)
		}
		if result.GasUsed != 0 {
			t.Errorf("GasUsed = %d, want 0", result.GasUsed)
		}
	})

	t.Run("not enabled", func(t *testing.T) {
		_, err := proc.IntegrationTest(
			nil, stateDB, 1_000_000, 500, 3,
		)
		if err != ErrGigagasNotEnabled {
			t.Errorf("expected ErrGigagasNotEnabled, got %v", err)
		}
	})
}

func TestGigagasDetectConflictingTxIndicesEmpty(t *testing.T) {
	results := make(map[int]*txExecResult)
	groups := [][]int{{0}, {1}}
	conflicts := detectConflictingTxIndices(results, groups)
	if len(conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(conflicts))
	}
}

func TestGigagasDetectConflictingTxIndicesWithConflicts(t *testing.T) {
	addr := types.Address{0x01}

	rec0 := state.NewTxAccessRecord(0)
	rec0.AddWrite(addr, types.Hash{})

	rec1 := state.NewTxAccessRecord(1)
	rec1.AddRead(addr, types.Hash{})

	results := map[int]*txExecResult{
		0: {index: 0, access: rec0, receipt: &TxReceipt{Success: true}},
		1: {index: 1, access: rec1, receipt: &TxReceipt{Success: true}},
	}
	groups := [][]int{{0}, {1}}

	conflicts := detectConflictingTxIndices(results, groups)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0] != 1 {
		t.Errorf("expected conflict at index 1, got %d", conflicts[0])
	}
}
