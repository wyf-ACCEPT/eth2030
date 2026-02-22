package core

import (
	"errors"
	"math/big"
	"runtime"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// Helper to create a tx with sender set.
func makeSenderTx(nonce uint64, sender, to types.Address, gas uint64) *types.Transaction {
	toAddr := to
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1),
		Gas:      gas,
		To:       &toAddr,
		Value:    big.NewInt(0),
	})
	tx.SetSender(sender)
	return tx
}

func TestDefaultGigagasExecutorConfig(t *testing.T) {
	cfg := DefaultGigagasExecutorConfig()
	if cfg.Workers != runtime.NumCPU() {
		t.Errorf("Workers: got %d, want %d", cfg.Workers, runtime.NumCPU())
	}
	if cfg.TargetGasPerSec != 1_000_000_000 {
		t.Errorf("TargetGasPerSec: got %d, want 1000000000", cfg.TargetGasPerSec)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries: got %d, want 3", cfg.MaxRetries)
	}
}

func TestGigagasExecutor_EmptyBatch(t *testing.T) {
	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), nil)
	result, err := exec.ExecuteBatch(nil, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed != 0 {
		t.Errorf("GasUsed: got %d, want 0", result.GasUsed)
	}
	if len(result.TxResults) != 0 {
		t.Errorf("TxResults: got %d, want 0", len(result.TxResults))
	}
}

func TestGigagasExecutor_SingleTx(t *testing.T) {
	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), nil)
	tx := makeSenderTx(0, types.Address{0x01}, types.Address{0x02}, 21000)

	result, err := exec.ExecuteBatch([]*types.Transaction{tx}, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed != 21000 {
		t.Errorf("GasUsed: got %d, want 21000", result.GasUsed)
	}
	if len(result.TxResults) != 1 {
		t.Fatalf("TxResults: got %d, want 1", len(result.TxResults))
	}
	if !result.TxResults[0].Success {
		t.Error("expected tx success")
	}
	if result.TxResults[0].GasUsed != 21000 {
		t.Errorf("tx GasUsed: got %d, want 21000", result.TxResults[0].GasUsed)
	}
}

func TestGigagasExecutor_ParallelIndependent(t *testing.T) {
	// 4 independent senders should execute in parallel.
	var mu sync.Mutex
	concurrentMax := 0
	concurrent := 0

	execFn := func(tx *types.Transaction, _ interface{}) (uint64, []types.Hash, error) {
		mu.Lock()
		concurrent++
		if concurrent > concurrentMax {
			concurrentMax = concurrent
		}
		mu.Unlock()

		// Simulate work.
		gas := tx.Gas()

		mu.Lock()
		concurrent--
		mu.Unlock()

		return gas, nil, nil
	}

	cfg := GigagasExecutorConfig{Workers: 4, TargetGasPerSec: 1_000_000_000, MaxRetries: 3}
	exec := NewGigagasExecutor(cfg, execFn)

	txs := make([]*types.Transaction, 4)
	for i := 0; i < 4; i++ {
		sender := types.Address{byte(i + 1)}
		recipient := types.Address{byte(i + 0x10)}
		txs[i] = makeSenderTx(0, sender, recipient, 21000)
	}

	result, err := exec.ExecuteBatch(txs, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed != 84000 {
		t.Errorf("GasUsed: got %d, want 84000", result.GasUsed)
	}
	if result.Parallelism != 4 {
		t.Errorf("Parallelism: got %d, want 4", result.Parallelism)
	}
	for i, r := range result.TxResults {
		if !r.Success {
			t.Errorf("tx %d not successful: %s", i, r.Error)
		}
	}
}

func TestGigagasExecutor_SameSenderSequential(t *testing.T) {
	// Multiple txs from same sender should be in same group.
	sender := types.Address{0x01}
	txs := []*types.Transaction{
		makeSenderTx(0, sender, types.Address{0x10}, 21000),
		makeSenderTx(1, sender, types.Address{0x20}, 21000),
		makeSenderTx(2, sender, types.Address{0x30}, 21000),
	}

	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), nil)
	result, err := exec.ExecuteBatch(txs, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed != 63000 {
		t.Errorf("GasUsed: got %d, want 63000", result.GasUsed)
	}
	// With only one sender, parallelism should be 1.
	if result.Parallelism != 1 {
		t.Errorf("Parallelism: got %d, want 1", result.Parallelism)
	}
}

func TestGigagasExecutor_GasLimit(t *testing.T) {
	// Gas limit of 42000 should allow only 2 txs of 21000.
	txs := make([]*types.Transaction, 3)
	for i := 0; i < 3; i++ {
		sender := types.Address{byte(i + 1)}
		txs[i] = makeSenderTx(0, sender, types.Address{0xAA}, 21000)
	}

	exec := NewGigagasExecutor(GigagasExecutorConfig{Workers: 1, TargetGasPerSec: 1_000_000_000, MaxRetries: 3}, nil)
	result, err := exec.ExecuteBatch(txs, nil, 42000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed > 42000 {
		t.Errorf("GasUsed %d exceeds limit 42000", result.GasUsed)
	}
}

func TestGigagasExecutor_ExecError(t *testing.T) {
	execFn := func(tx *types.Transaction, _ interface{}) (uint64, []types.Hash, error) {
		return 0, nil, errors.New("execution failed")
	}

	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), execFn)
	tx := makeSenderTx(0, types.Address{0x01}, types.Address{0x02}, 21000)

	result, err := exec.ExecuteBatch([]*types.Transaction{tx}, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected batch error: %v", err)
	}
	if result.TxResults[0].Success {
		t.Error("expected tx failure")
	}
	if result.TxResults[0].Error != "execution failed" {
		t.Errorf("Error: got %q, want %q", result.TxResults[0].Error, "execution failed")
	}
	if result.Conflicts != 1 {
		t.Errorf("Conflicts: got %d, want 1", result.Conflicts)
	}
}

func TestGigagasExecutor_Metrics(t *testing.T) {
	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), nil)

	tx1 := makeSenderTx(0, types.Address{0x01}, types.Address{0x02}, 21000)
	tx2 := makeSenderTx(0, types.Address{0x03}, types.Address{0x04}, 30000)

	exec.ExecuteBatch([]*types.Transaction{tx1}, nil, 1_000_000)
	exec.ExecuteBatch([]*types.Transaction{tx2}, nil, 1_000_000)

	executed, gasUsed, conflicts := exec.GigagasMetrics()
	if executed != 2 {
		t.Errorf("executed: got %d, want 2", executed)
	}
	if gasUsed != 51000 {
		t.Errorf("gasUsed: got %d, want 51000", gasUsed)
	}
	if conflicts != 0 {
		t.Errorf("conflicts: got %d, want 0", conflicts)
	}
}

func TestGigagasExecutor_GasPerSecond(t *testing.T) {
	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), nil)
	tx := makeSenderTx(0, types.Address{0x01}, types.Address{0x02}, 21000)

	result, err := exec.ExecuteBatch([]*types.Transaction{tx}, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasPerSecond <= 0 {
		t.Error("GasPerSecond should be > 0")
	}
	if result.Duration <= 0 {
		t.Error("Duration should be > 0")
	}
}

func TestGigagasExecutor_SetWorkers(t *testing.T) {
	exec := NewGigagasExecutor(GigagasExecutorConfig{Workers: 2, TargetGasPerSec: 1_000_000_000, MaxRetries: 3}, nil)
	if exec.Workers() != 2 {
		t.Errorf("Workers: got %d, want 2", exec.Workers())
	}
	exec.SetWorkers(8)
	if exec.Workers() != 8 {
		t.Errorf("Workers: got %d, want 8", exec.Workers())
	}
	// Zero should not change.
	exec.SetWorkers(0)
	if exec.Workers() != 8 {
		t.Errorf("Workers: got %d, want 8 (unchanged)", exec.Workers())
	}
}

func TestGigagasExecutor_NoSenderTxs(t *testing.T) {
	// Txs without sender set should still execute (grouped together).
	toAddr := types.Address{0x02}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &toAddr,
		Value:    big.NewInt(0),
	})

	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), nil)
	result, err := exec.ExecuteBatch([]*types.Transaction{tx}, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.GasUsed != 21000 {
		t.Errorf("GasUsed: got %d, want 21000", result.GasUsed)
	}
}

func TestGigagasExecutor_WithLogs(t *testing.T) {
	logHash := types.Hash{0xAB, 0xCD}
	execFn := func(tx *types.Transaction, _ interface{}) (uint64, []types.Hash, error) {
		return tx.Gas(), []types.Hash{logHash}, nil
	}

	exec := NewGigagasExecutor(DefaultGigagasExecutorConfig(), execFn)
	tx := makeSenderTx(0, types.Address{0x01}, types.Address{0x02}, 21000)

	result, err := exec.ExecuteBatch([]*types.Transaction{tx}, nil, 1_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.TxResults[0].Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(result.TxResults[0].Logs))
	}
	if result.TxResults[0].Logs[0] != logHash {
		t.Errorf("log hash mismatch")
	}
}

func TestAccessSet_ConflictDetection(t *testing.T) {
	// No conflict: A reads x, B reads x.
	a := NewAccessSet()
	a.AddRead(types.Address{0x01})
	b := NewAccessSet()
	b.AddRead(types.Address{0x01})
	if a.ConflictsWith(b) {
		t.Error("read-read should not conflict")
	}

	// Conflict: A writes x, B reads x.
	c := NewAccessSet()
	c.AddWrite(types.Address{0x01})
	d := NewAccessSet()
	d.AddRead(types.Address{0x01})
	if !c.ConflictsWith(d) {
		t.Error("write-read should conflict")
	}

	// Conflict: A reads x, B writes x.
	e := NewAccessSet()
	e.AddRead(types.Address{0x01})
	f := NewAccessSet()
	f.AddWrite(types.Address{0x01})
	if !e.ConflictsWith(f) {
		t.Error("read-write should conflict")
	}

	// Conflict: A writes x, B writes x.
	g := NewAccessSet()
	g.AddWrite(types.Address{0x01})
	h := NewAccessSet()
	h.AddWrite(types.Address{0x01})
	if !g.ConflictsWith(h) {
		t.Error("write-write should conflict")
	}

	// No conflict: different addresses.
	i := NewAccessSet()
	i.AddWrite(types.Address{0x01})
	j := NewAccessSet()
	j.AddWrite(types.Address{0x02})
	if i.ConflictsWith(j) {
		t.Error("different addresses should not conflict")
	}
}

func TestDetectConflicts(t *testing.T) {
	a := NewAccessSet()
	a.AddWrite(types.Address{0x01})
	b := NewAccessSet()
	b.AddRead(types.Address{0x01})
	c := NewAccessSet()
	c.AddWrite(types.Address{0x02})

	// a-b conflict, a-c no conflict, b-c no conflict => 1 conflict.
	n := DetectConflicts([]*AccessSet{a, b, c})
	if n != 1 {
		t.Errorf("expected 1 conflict, got %d", n)
	}
}

func TestDetectConflicts_Empty(t *testing.T) {
	n := DetectConflicts(nil)
	if n != 0 {
		t.Errorf("expected 0 conflicts, got %d", n)
	}
}

func TestPartitionBySender(t *testing.T) {
	s1, s2 := types.Address{0x01}, types.Address{0x02}
	txs := []*types.Transaction{
		makeSenderTx(0, s1, types.Address{0x10}, 21000),
		makeSenderTx(1, s1, types.Address{0x20}, 21000),
		makeSenderTx(0, s2, types.Address{0x30}, 21000),
	}
	groups := partitionBySender(txs)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	// Find group sizes.
	sizes := make(map[int]bool)
	for _, g := range groups {
		sizes[len(g)] = true
	}
	if !sizes[2] || !sizes[1] {
		t.Error("expected groups of size 2 and 1")
	}
}

func TestGigagasExecutor_ConcurrentSafety(t *testing.T) {
	exec := NewGigagasExecutor(GigagasExecutorConfig{Workers: 4, TargetGasPerSec: 1_000_000_000, MaxRetries: 3}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sender := types.Address{byte(idx + 1)}
			tx := makeSenderTx(0, sender, types.Address{0xAA}, 21000)
			_, err := exec.ExecuteBatch([]*types.Transaction{tx}, nil, 1_000_000)
			if err != nil {
				t.Errorf("batch %d error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	executed, gasUsed, _ := exec.GigagasMetrics()
	if executed != 10 {
		t.Errorf("executed: got %d, want 10", executed)
	}
	if gasUsed != 210000 {
		t.Errorf("gasUsed: got %d, want 210000", gasUsed)
	}
}

func TestGigagasExecutor_OrderPreserved(t *testing.T) {
	// Verify results are ordered matching input transaction order.
	txs := make([]*types.Transaction, 5)
	for i := 0; i < 5; i++ {
		sender := types.Address{byte(i + 1)}
		txs[i] = makeSenderTx(0, sender, types.Address{byte(i + 0x10)}, uint64(21000+i*1000))
	}

	exec := NewGigagasExecutor(GigagasExecutorConfig{Workers: 2, TargetGasPerSec: 1_000_000_000, MaxRetries: 3}, nil)
	result, err := exec.ExecuteBatch(txs, nil, 10_000_000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, tx := range txs {
		if result.TxResults[i].TxHash != tx.Hash() {
			t.Errorf("tx %d hash mismatch: result has %s, input has %s",
				i, result.TxResults[i].TxHash.Hex(), tx.Hash().Hex())
		}
	}
}

func TestNewGigagasExecutor_DefaultsForZeroConfig(t *testing.T) {
	exec := NewGigagasExecutor(GigagasExecutorConfig{}, nil)
	if exec.Workers() <= 0 {
		t.Error("workers should default to > 0")
	}
	if exec.config.TargetGasPerSec != 1_000_000_000 {
		t.Errorf("TargetGasPerSec: got %d, want 1000000000", exec.config.TargetGasPerSec)
	}
	if exec.config.MaxRetries != 3 {
		t.Errorf("MaxRetries: got %d, want 3", exec.config.MaxRetries)
	}
}
