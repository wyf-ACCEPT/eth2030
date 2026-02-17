package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeDynTx creates an EIP-1559 dynamic fee transaction with the given parameters.
func makeDynTx(nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
	tx.SetSender(testSender)
	return tx
}

func makeDynTxFrom(from types.Address, nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
	tx.SetSender(from)
	return tx
}

func TestEffectiveGasPrice(t *testing.T) {
	tests := []struct {
		name    string
		tx      *types.Transaction
		baseFee *big.Int
		want    int64
	}{
		{
			name:    "legacy tx, nil baseFee",
			tx:      makeTx(0, 5000, 21000),
			baseFee: nil,
			want:    5000,
		},
		{
			name:    "legacy tx, with baseFee",
			tx:      makeTx(0, 5000, 21000),
			baseFee: big.NewInt(1000),
			want:    5000,
		},
		{
			name:    "EIP-1559 tx, nil baseFee returns feeCap",
			tx:      makeDynTx(0, 200, 3000, 21000),
			baseFee: nil,
			want:    3000,
		},
		{
			name:    "EIP-1559 tx, baseFee + tipCap < feeCap",
			tx:      makeDynTx(0, 200, 3000, 21000),
			baseFee: big.NewInt(1000),
			want:    1200, // baseFee(1000) + tipCap(200) = 1200 < feeCap(3000)
		},
		{
			name:    "EIP-1559 tx, baseFee + tipCap > feeCap, capped at feeCap",
			tx:      makeDynTx(0, 2500, 3000, 21000),
			baseFee: big.NewInt(1000),
			want:    3000, // baseFee(1000) + tipCap(2500) = 3500 > feeCap(3000)
		},
		{
			name:    "EIP-1559 tx, baseFee + tipCap == feeCap",
			tx:      makeDynTx(0, 2000, 3000, 21000),
			baseFee: big.NewInt(1000),
			want:    3000, // baseFee(1000) + tipCap(2000) = 3000 == feeCap(3000)
		},
		{
			name:    "EIP-1559 tx, high baseFee",
			tx:      makeDynTx(0, 100, 500, 21000),
			baseFee: big.NewInt(600),
			want:    500, // baseFee(600) + tipCap(100) = 700 > feeCap(500)
		},
		{
			name:    "EIP-1559 tx, zero tipCap",
			tx:      makeDynTx(0, 0, 1000, 21000),
			baseFee: big.NewInt(500),
			want:    500, // baseFee(500) + tipCap(0) = 500 < feeCap(1000)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EffectiveGasPrice(tt.tx, tt.baseFee)
			if got.Int64() != tt.want {
				t.Errorf("EffectiveGasPrice() = %d, want %d", got.Int64(), tt.want)
			}
		})
	}
}

func TestPendingSorted(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	// Add txs with different gas prices from different senders.
	senders := []types.Address{
		types.BytesToAddress([]byte{0x10}),
		types.BytesToAddress([]byte{0x20}),
		types.BytesToAddress([]byte{0x30}),
		types.BytesToAddress([]byte{0x40}),
	}

	prices := []int64{100, 5000, 300, 2000}
	for i, s := range senders {
		tx := makeTxFrom(s, 0, prices[i], 21000)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal sender %d: %v", i, err)
		}
	}

	sorted := pool.PendingSorted()
	if len(sorted) != 4 {
		t.Fatalf("PendingSorted returned %d txs, want 4", len(sorted))
	}

	// Verify descending order: 5000, 2000, 300, 100.
	expectedOrder := []int64{5000, 2000, 300, 100}
	for i, tx := range sorted {
		got := tx.GasPrice().Int64()
		if got != expectedOrder[i] {
			t.Errorf("sorted[%d] gas price = %d, want %d", i, got, expectedOrder[i])
		}
	}
}

func TestPendingSorted_EIP1559(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)
	pool.SetBaseFee(big.NewInt(100))

	s1 := types.BytesToAddress([]byte{0x10})
	s2 := types.BytesToAddress([]byte{0x20})
	s3 := types.BytesToAddress([]byte{0x30})

	// Legacy tx: gasPrice 500 -> effective 500
	pool.AddLocal(makeTxFrom(s1, 0, 500, 21000))
	// EIP-1559 tx: tipCap=200, feeCap=1000 -> effective min(1000, 100+200) = 300
	pool.AddLocal(makeDynTxFrom(s2, 0, 200, 1000, 21000))
	// EIP-1559 tx: tipCap=600, feeCap=800 -> effective min(800, 100+600) = 700
	pool.AddLocal(makeDynTxFrom(s3, 0, 600, 800, 21000))

	sorted := pool.PendingSorted()
	if len(sorted) != 3 {
		t.Fatalf("PendingSorted returned %d txs, want 3", len(sorted))
	}

	// Expected order: 700 (s3), 500 (s1), 300 (s2).
	expectedEffective := []int64{700, 500, 300}
	baseFee := big.NewInt(100)
	for i, tx := range sorted {
		got := EffectiveGasPrice(tx, baseFee).Int64()
		if got != expectedEffective[i] {
			t.Errorf("sorted[%d] effective price = %d, want %d", i, got, expectedEffective[i])
		}
	}
}

func TestReplaceByFee_Accept(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	// Add tx with gas price 1000.
	tx1 := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx1); err != nil {
		t.Fatalf("AddLocal tx1: %v", err)
	}
	if pool.Count() != 1 {
		t.Fatalf("Count = %d, want 1", pool.Count())
	}

	// Replace with 10% higher = 1100. Should succeed.
	tx2 := makeTx(0, 1100, 21000)
	if err := pool.AddLocal(tx2); err != nil {
		t.Fatalf("AddLocal tx2 (replacement): %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count after replacement = %d, want 1", pool.Count())
	}

	// Verify the old tx is gone and new tx is present.
	if pool.Get(tx1.Hash()) != nil {
		t.Error("old tx still in pool after replacement")
	}
	if pool.Get(tx2.Hash()) == nil {
		t.Error("replacement tx not found in pool")
	}

	// Verify it's in pending with the new gas price.
	pending := pool.PendingSorted()
	if len(pending) != 1 {
		t.Fatalf("PendingSorted = %d txs, want 1", len(pending))
	}
	if pending[0].GasPrice().Int64() != 1100 {
		t.Errorf("pending tx gas price = %d, want 1100", pending[0].GasPrice().Int64())
	}
}

func TestReplaceByFee_AcceptLargerBump(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	tx1 := makeTx(0, 1000, 21000)
	pool.AddLocal(tx1)

	// 50% bump should also be accepted.
	tx2 := makeTx(0, 1500, 21000)
	if err := pool.AddLocal(tx2); err != nil {
		t.Fatalf("replacement with 50%% bump failed: %v", err)
	}
	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1", pool.Count())
	}
}

func TestReplaceByFee_Reject(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	// Add tx with gas price 1000.
	tx1 := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx1); err != nil {
		t.Fatalf("AddLocal tx1: %v", err)
	}

	// Try replacing with only 9% bump (1090 < 1100 threshold).
	tx2 := makeTx(0, 1090, 21000)
	err := pool.AddLocal(tx2)
	if err != ErrReplacementUnderpriced {
		t.Errorf("expected ErrReplacementUnderpriced, got: %v", err)
	}

	// Count should still be 1, original tx preserved.
	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1", pool.Count())
	}
	if pool.Get(tx1.Hash()) == nil {
		t.Error("original tx lost after failed replacement")
	}
}

func TestReplaceByFee_ExactThreshold(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	tx1 := makeTx(0, 1000, 21000)
	pool.AddLocal(tx1)

	// Exact 10% bump: 1100. Should succeed (>= threshold).
	tx2 := makeTx(0, 1100, 21000)
	if err := pool.AddLocal(tx2); err != nil {
		t.Fatalf("exact 10%% bump rejected: %v", err)
	}
	if pool.Get(tx2.Hash()) == nil {
		t.Error("replacement tx not found")
	}
}

func TestReplaceByFee_JustBelow(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	tx1 := makeTx(0, 1000, 21000)
	pool.AddLocal(tx1)

	// Just below threshold: 1099 < 1100.
	tx2 := makeTx(0, 1099, 21000)
	err := pool.AddLocal(tx2)
	if err != ErrReplacementUnderpriced {
		t.Errorf("expected ErrReplacementUnderpriced, got: %v", err)
	}
}

func TestReplaceByFee_QueuedTx(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	// Nonce 5 goes to queue (state nonce is 0).
	tx1 := makeTx(5, 1000, 21000)
	pool.AddLocal(tx1)
	if pool.QueuedCount() != 1 {
		t.Fatalf("QueuedCount = %d, want 1", pool.QueuedCount())
	}

	// Replace queued tx with sufficient bump.
	tx2 := makeTx(5, 1200, 21000)
	if err := pool.AddLocal(tx2); err != nil {
		t.Fatalf("replacement of queued tx failed: %v", err)
	}
	if pool.QueuedCount() != 1 {
		t.Errorf("QueuedCount after replacement = %d, want 1", pool.QueuedCount())
	}
	if pool.Get(tx1.Hash()) != nil {
		t.Error("old queued tx still in pool")
	}
}

func TestEviction(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 3

	state := newMockState()
	pool := New(config, state)

	// Add 3 txs from same sender with increasing nonces and different prices.
	pool.AddLocal(makeTx(0, 100, 21000))  // cheapest, eviction candidate
	pool.AddLocal(makeTx(1, 500, 21000))  // mid-price
	pool.AddLocal(makeTx(2, 2000, 21000)) // highest, protected (highest nonce)

	if pool.Count() != 3 {
		t.Fatalf("Count = %d, want 3", pool.Count())
	}

	// Add a 4th tx from different sender with high price. Should evict the cheapest.
	sender2 := types.BytesToAddress([]byte{0xaa})
	tx4 := makeTxFrom(sender2, 0, 3000, 21000)
	if err := pool.AddLocal(tx4); err != nil {
		t.Fatalf("AddLocal with eviction failed: %v", err)
	}

	if pool.Count() != 3 {
		t.Errorf("Count after eviction = %d, want 3", pool.Count())
	}

	// The cheapest tx (price=100) should be evicted.
	sorted := pool.PendingSorted()
	for _, tx := range sorted {
		if tx.GasPrice().Int64() == 100 {
			t.Error("cheapest tx (price=100) should have been evicted")
		}
	}
}

func TestEviction_ProtectsOnlyTxPerSender(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 2

	state := newMockState()
	pool := New(config, state)

	// Two senders, each with one tx (protected).
	s1 := types.BytesToAddress([]byte{0x10})
	s2 := types.BytesToAddress([]byte{0x20})
	pool.AddLocal(makeTxFrom(s1, 0, 100, 21000))
	pool.AddLocal(makeTxFrom(s2, 0, 200, 21000))

	// Adding a 3rd tx should fail because both existing txs are protected.
	s3 := types.BytesToAddress([]byte{0x30})
	err := pool.AddLocal(makeTxFrom(s3, 0, 5000, 21000))
	if err != ErrTxPoolFull {
		t.Errorf("expected ErrTxPoolFull, got: %v", err)
	}
}

func TestEviction_EvictsQueuedFirst(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 2

	state := newMockState()
	pool := New(config, state)

	// One pending tx (nonce 0) and one queued tx (nonce 5) from the same sender.
	pool.AddLocal(makeTx(0, 500, 21000))
	pool.AddLocal(makeTx(5, 50, 21000)) // queued, cheap

	if pool.Count() != 2 {
		t.Fatalf("Count = %d, want 2", pool.Count())
	}

	// Add a new tx from a different sender. The queued tx should be evicted.
	s2 := types.BytesToAddress([]byte{0xbb})
	if err := pool.AddLocal(makeTxFrom(s2, 0, 1000, 21000)); err != nil {
		t.Fatalf("AddLocal with queued eviction failed: %v", err)
	}

	if pool.Count() != 2 {
		t.Errorf("Count after eviction = %d, want 2", pool.Count())
	}
	if pool.QueuedCount() != 0 {
		t.Errorf("QueuedCount after eviction = %d, want 0", pool.QueuedCount())
	}
}

func TestSetBaseFee(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	s1 := types.BytesToAddress([]byte{0x10})
	s2 := types.BytesToAddress([]byte{0x20})
	s3 := types.BytesToAddress([]byte{0x30})

	// Add EIP-1559 txs with different fee caps.
	// tx1: feeCap=500, tipCap=100
	pool.AddLocal(makeDynTxFrom(s1, 0, 100, 500, 21000))
	// tx2: feeCap=200, tipCap=50
	pool.AddLocal(makeDynTxFrom(s2, 0, 50, 200, 21000))
	// tx3: feeCap=1000, tipCap=300 (legacy-like high fee cap)
	pool.AddLocal(makeDynTxFrom(s3, 0, 300, 1000, 21000))

	if pool.PendingCount() != 3 {
		t.Fatalf("PendingCount = %d, want 3", pool.PendingCount())
	}

	// Set base fee to 300. tx2 (feeCap=200) should be demoted to queue.
	pool.SetBaseFee(big.NewInt(300))

	if pool.PendingCount() != 2 {
		t.Errorf("PendingCount after baseFee=300: %d, want 2", pool.PendingCount())
	}
	if pool.QueuedCount() != 1 {
		t.Errorf("QueuedCount after baseFee=300: %d, want 1", pool.QueuedCount())
	}
	// Total count unchanged.
	if pool.Count() != 3 {
		t.Errorf("Count after baseFee change = %d, want 3", pool.Count())
	}
}

func TestSetBaseFee_AllDemoted(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	s1 := types.BytesToAddress([]byte{0x10})
	s2 := types.BytesToAddress([]byte{0x20})

	pool.AddLocal(makeDynTxFrom(s1, 0, 10, 100, 21000))
	pool.AddLocal(makeDynTxFrom(s2, 0, 20, 200, 21000))

	if pool.PendingCount() != 2 {
		t.Fatalf("PendingCount = %d, want 2", pool.PendingCount())
	}

	// Huge base fee demotes everything.
	pool.SetBaseFee(big.NewInt(10000))

	if pool.PendingCount() != 0 {
		t.Errorf("PendingCount after huge baseFee: %d, want 0", pool.PendingCount())
	}
	if pool.QueuedCount() != 2 {
		t.Errorf("QueuedCount after huge baseFee: %d, want 2", pool.QueuedCount())
	}
}

func TestSetBaseFee_LegacyTxDemoted(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	// Legacy tx with gasPrice=500. If baseFee goes above 500, it should be demoted.
	pool.AddLocal(makeTx(0, 500, 21000))

	if pool.PendingCount() != 1 {
		t.Fatalf("PendingCount = %d, want 1", pool.PendingCount())
	}

	pool.SetBaseFee(big.NewInt(600))

	if pool.PendingCount() != 0 {
		t.Errorf("PendingCount after baseFee > gasPrice: %d, want 0", pool.PendingCount())
	}
	if pool.QueuedCount() != 1 {
		t.Errorf("QueuedCount after baseFee > gasPrice: %d, want 1", pool.QueuedCount())
	}
}

func TestSetBaseFee_NoDemotionWhenAffordable(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	pool.AddLocal(makeTx(0, 5000, 21000))

	pool.SetBaseFee(big.NewInt(100))

	// gasPrice(5000) > baseFee(100), no demotion.
	if pool.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", pool.PendingCount())
	}
	if pool.QueuedCount() != 0 {
		t.Errorf("QueuedCount = %d, want 0", pool.QueuedCount())
	}
}

func TestPendingSorted_EmptyPool(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)

	sorted := pool.PendingSorted()
	if len(sorted) != 0 {
		t.Errorf("PendingSorted on empty pool = %d txs, want 0", len(sorted))
	}
}

func TestReplaceByFee_EIP1559(t *testing.T) {
	state := newMockState()
	pool := New(DefaultConfig(), state)
	pool.SetBaseFee(big.NewInt(100))

	// Add EIP-1559 tx: tipCap=100, feeCap=500. Effective = min(500, 100+100) = 200.
	tx1 := makeDynTx(0, 100, 500, 21000)
	if err := pool.AddLocal(tx1); err != nil {
		t.Fatalf("AddLocal tx1: %v", err)
	}

	// Replace with higher tip. effective = min(600, 100+150) = 250.
	// 10% of 200 = 20, so need >= 220. 250 >= 220, should succeed.
	tx2 := makeDynTx(0, 150, 600, 21000)
	if err := pool.AddLocal(tx2); err != nil {
		t.Fatalf("replacement tx2 failed: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count after EIP-1559 replacement = %d, want 1", pool.Count())
	}
}

func TestEvictLowest_Direct(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 100 // large, so no auto-eviction during add

	state := newMockState()
	pool := New(config, state)

	// Add txs from same sender with different prices.
	pool.AddLocal(makeTx(0, 100, 21000))
	pool.AddLocal(makeTx(1, 300, 21000))
	pool.AddLocal(makeTx(2, 500, 21000))

	if pool.Count() != 3 {
		t.Fatalf("Count = %d, want 3", pool.Count())
	}

	// Evict manually.
	pool.mu.Lock()
	evicted := pool.evictLowest(nil)
	pool.mu.Unlock()

	if evicted != 1 {
		t.Errorf("evictLowest returned %d, want 1", evicted)
	}
	if pool.Count() != 2 {
		t.Errorf("Count after eviction = %d, want 2", pool.Count())
	}

	// Verify the cheapest (price=100) was evicted.
	sorted := pool.PendingSorted()
	for _, tx := range sorted {
		if tx.GasPrice().Int64() == 100 {
			t.Error("cheapest tx should have been evicted")
		}
	}
}
