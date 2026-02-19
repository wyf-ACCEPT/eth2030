package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestGasRateTracker_Basic(t *testing.T) {
	tracker := NewGasRateTracker(100)

	// No records -> rate is 0.
	if rate := tracker.CurrentGasRate(); rate != 0 {
		t.Fatalf("expected 0 rate with no records, got %f", rate)
	}

	// Single record -> still 0 (need at least 2).
	tracker.RecordBlockGas(1, 15_000_000, 100)
	if rate := tracker.CurrentGasRate(); rate != 0 {
		t.Fatalf("expected 0 rate with 1 record, got %f", rate)
	}

	// Two records: 15M gas each, 12 sec apart.
	tracker.RecordBlockGas(2, 15_000_000, 112)
	rate := tracker.CurrentGasRate()
	// Total gas = 30M, time delta = 12s, rate = 2.5M/s.
	expected := 30_000_000.0 / 12.0
	if rate < expected-1 || rate > expected+1 {
		t.Fatalf("expected rate ~%f, got %f", expected, rate)
	}
}

func TestGasRateTracker_Window(t *testing.T) {
	tracker := NewGasRateTracker(3)

	tracker.RecordBlockGas(1, 1000, 10)
	tracker.RecordBlockGas(2, 2000, 20)
	tracker.RecordBlockGas(3, 3000, 30)
	tracker.RecordBlockGas(4, 4000, 40) // pushes out record 1

	// Window should be [2, 3, 4], total gas = 9000, delta = 40 - 20 = 20.
	rate := tracker.CurrentGasRate()
	expected := 9000.0 / 20.0
	if rate < expected-0.1 || rate > expected+0.1 {
		t.Fatalf("expected rate ~%f, got %f", expected, rate)
	}
}

func TestIsGigagasEnabled(t *testing.T) {
	// Not enabled without Hogota.
	config := &ChainConfig{}
	if IsGigagasEnabled(config, 1000) {
		t.Fatal("expected gigagas disabled without Hogota")
	}

	// Enabled with Hogota active.
	hogotaTime := uint64(500)
	config.HogotaTime = &hogotaTime
	if !IsGigagasEnabled(config, 1000) {
		t.Fatal("expected gigagas enabled with Hogota active")
	}
	if IsGigagasEnabled(config, 100) {
		t.Fatal("expected gigagas disabled before Hogota time")
	}
}

func makeSimpleTx(nonce uint64, from, to types.Address) *types.Transaction {
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1),
	})
	tx.SetSender(from)
	return tx
}

func TestParallelExecutionHints_Independent(t *testing.T) {
	// 3 independent transactions (different senders and recipients).
	txs := []*types.Transaction{
		makeSimpleTx(0, types.Address{0x01}, types.Address{0x10}),
		makeSimpleTx(0, types.Address{0x02}, types.Address{0x20}),
		makeSimpleTx(0, types.Address{0x03}, types.Address{0x30}),
	}

	groups := ParallelExecutionHints(txs)
	if len(groups) != 3 {
		t.Fatalf("expected 3 independent groups, got %d", len(groups))
	}
}

func TestParallelExecutionHints_Conflicting(t *testing.T) {
	// Two txs from the same sender -> must be in same group.
	sender := types.Address{0x01}
	txs := []*types.Transaction{
		makeSimpleTx(0, sender, types.Address{0x10}),
		makeSimpleTx(1, sender, types.Address{0x20}),
	}

	groups := ParallelExecutionHints(txs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group for same-sender txs, got %d", len(groups))
	}
	if len(groups[0]) != 2 {
		t.Fatalf("expected group of 2, got %d", len(groups[0]))
	}
}

func TestParallelExecutionHints_SharedRecipient(t *testing.T) {
	// Two txs to the same recipient -> conflict.
	recipient := types.Address{0xAA}
	txs := []*types.Transaction{
		makeSimpleTx(0, types.Address{0x01}, recipient),
		makeSimpleTx(0, types.Address{0x02}, recipient),
	}

	groups := ParallelExecutionHints(txs)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group for shared-recipient, got %d", len(groups))
	}
}

func TestParallelExecutionHints_Empty(t *testing.T) {
	groups := ParallelExecutionHints(nil)
	if groups != nil {
		t.Fatal("expected nil for empty txs")
	}
}

func TestEstimateParallelSpeedup(t *testing.T) {
	// 4 groups of size 1 -> speedup = 4.
	groups := [][]int{{0}, {1}, {2}, {3}}
	speedup := EstimateParallelSpeedup(groups)
	if speedup != 4.0 {
		t.Fatalf("expected speedup 4.0, got %f", speedup)
	}

	// 1 group of size 4 -> speedup = 1.
	groups = [][]int{{0, 1, 2, 3}}
	speedup = EstimateParallelSpeedup(groups)
	if speedup != 1.0 {
		t.Fatalf("expected speedup 1.0, got %f", speedup)
	}

	// 2 groups: [0,1] and [2] -> 3/2 = 1.5.
	groups = [][]int{{0, 1}, {2}}
	speedup = EstimateParallelSpeedup(groups)
	if speedup != 1.5 {
		t.Fatalf("expected speedup 1.5, got %f", speedup)
	}

	// Empty.
	speedup = EstimateParallelSpeedup(nil)
	if speedup != 1.0 {
		t.Fatalf("expected speedup 1.0, got %f", speedup)
	}
}

func TestDefaultGigagasConfig(t *testing.T) {
	cfg := DefaultGigagasConfig
	if cfg.TargetGasPerSecond != 1_000_000_000 {
		t.Fatalf("expected 1 Ggas/s target, got %d", cfg.TargetGasPerSecond)
	}
	if cfg.MaxBlockGas != 500_000_000 {
		t.Fatalf("expected 500M max block gas, got %d", cfg.MaxBlockGas)
	}
	if cfg.ParallelExecutionSlots != 16 {
		t.Fatalf("expected 16 parallel slots, got %d", cfg.ParallelExecutionSlots)
	}
}
