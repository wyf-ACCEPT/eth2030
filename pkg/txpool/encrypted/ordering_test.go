package encrypted

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// orderingTx creates a test transaction for ordering tests, distinct from
// testTx in encrypted_test.go and poolTestTx in pool_test.go.
func orderingTx(nonce uint64, gasPrice int64) *types.Transaction {
	to := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
}

// orderingCommitEntry creates a CommitEntry with the given timestamp.
func orderingCommitEntry(timestamp uint64) *CommitEntry {
	return &CommitEntry{
		Commit: &CommitTx{Timestamp: timestamp},
	}
}

// --- OrderByCommitTime ---

func TestOrdering_OrderByCommitTimeSingle(t *testing.T) {
	entries := []*CommitEntry{
		orderingCommitEntry(500),
	}
	sorted := OrderByCommitTime(entries)
	if len(sorted) != 1 {
		t.Fatalf("sorted len: want 1, got %d", len(sorted))
	}
	if sorted[0].Commit.Timestamp != 500 {
		t.Fatalf("timestamp: want 500, got %d", sorted[0].Commit.Timestamp)
	}
}

func TestOrdering_OrderByCommitTimeMultiple(t *testing.T) {
	entries := []*CommitEntry{
		orderingCommitEntry(400),
		orderingCommitEntry(100),
		orderingCommitEntry(300),
		orderingCommitEntry(200),
	}
	sorted := OrderByCommitTime(entries)
	if len(sorted) != 4 {
		t.Fatalf("sorted len: want 4, got %d", len(sorted))
	}
	for i := 0; i < len(sorted)-1; i++ {
		if sorted[i].Commit.Timestamp > sorted[i+1].Commit.Timestamp {
			t.Fatalf("sorted[%d].Timestamp=%d > sorted[%d].Timestamp=%d",
				i, sorted[i].Commit.Timestamp, i+1, sorted[i+1].Commit.Timestamp)
		}
	}
}

func TestOrdering_OrderByCommitTimeSameTimestamp(t *testing.T) {
	entries := []*CommitEntry{
		orderingCommitEntry(100),
		orderingCommitEntry(100),
		orderingCommitEntry(100),
	}
	sorted := OrderByCommitTime(entries)
	if len(sorted) != 3 {
		t.Fatalf("sorted len: want 3, got %d", len(sorted))
	}
	// All same timestamp: any order is valid, just check no crash.
	for _, e := range sorted {
		if e.Commit.Timestamp != 100 {
			t.Fatal("unexpected timestamp change")
		}
	}
}

func TestOrdering_OrderByCommitTimeNoMutation(t *testing.T) {
	entries := []*CommitEntry{
		orderingCommitEntry(300),
		orderingCommitEntry(100),
	}
	orig0 := entries[0].Commit.Timestamp
	_ = OrderByCommitTime(entries)
	if entries[0].Commit.Timestamp != orig0 {
		t.Fatal("OrderByCommitTime should not mutate original slice")
	}
}

func TestOrdering_OrderByCommitTimeEmptySlice(t *testing.T) {
	sorted := OrderByCommitTime([]*CommitEntry{})
	if len(sorted) != 0 {
		t.Fatalf("sorted len: want 0, got %d", len(sorted))
	}
}

// --- TimeBasedOrdering ---

func TestOrdering_TimeBasedName(t *testing.T) {
	p := &TimeBasedOrdering{}
	if p.Name() != "time-based" {
		t.Fatalf("Name: want 'time-based', got %q", p.Name())
	}
}

func TestOrdering_TimeBasedOrder(t *testing.T) {
	p := &TimeBasedOrdering{}
	entries := []OrderableEntry{
		{Commit: orderingCommitEntry(300), Transaction: orderingTx(0, 100)},
		{Commit: orderingCommitEntry(100), Transaction: orderingTx(1, 500)},
		{Commit: orderingCommitEntry(200), Transaction: orderingTx(2, 300)},
	}
	sorted := p.Order(entries)
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Fatal("should be sorted by timestamp ascending")
	}
	if sorted[2].Commit.Commit.Timestamp != 300 {
		t.Fatal("last should have highest timestamp")
	}
}

// --- FeeBasedOrdering ---

func TestOrdering_FeeBasedName(t *testing.T) {
	p := &FeeBasedOrdering{}
	if p.Name() != "fee-based" {
		t.Fatalf("Name: want 'fee-based', got %q", p.Name())
	}
}

func TestOrdering_FeeBasedOrder(t *testing.T) {
	p := &FeeBasedOrdering{}
	entries := []OrderableEntry{
		{Commit: orderingCommitEntry(100), Transaction: orderingTx(0, 100)},
		{Commit: orderingCommitEntry(200), Transaction: orderingTx(1, 500)},
		{Commit: orderingCommitEntry(300), Transaction: orderingTx(2, 300)},
	}
	sorted := p.Order(entries)
	// Should be sorted by gas price descending.
	fee0 := effectiveGasPrice(sorted[0].Transaction)
	fee2 := effectiveGasPrice(sorted[2].Transaction)
	if fee0.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("sorted[0] fee: want 500, got %s", fee0)
	}
	if fee2.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("sorted[2] fee: want 100, got %s", fee2)
	}
}

// --- HybridOrdering ---

func TestOrdering_HybridName(t *testing.T) {
	p := &HybridOrdering{FeeWeight: 0.5}
	if p.Name() != "hybrid" {
		t.Fatalf("Name: want 'hybrid', got %q", p.Name())
	}
}

func TestOrdering_HybridPureTime(t *testing.T) {
	p := &HybridOrdering{FeeWeight: 0.0}
	entries := []OrderableEntry{
		{Commit: orderingCommitEntry(300), Transaction: orderingTx(0, 500)},
		{Commit: orderingCommitEntry(100), Transaction: orderingTx(1, 100)},
	}
	sorted := p.Order(entries)
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Fatal("with FeeWeight=0.0, should be purely time-based")
	}
}

func TestOrdering_HybridPureFee(t *testing.T) {
	p := &HybridOrdering{FeeWeight: 1.0}
	entries := []OrderableEntry{
		{Commit: orderingCommitEntry(100), Transaction: orderingTx(0, 100)},
		{Commit: orderingCommitEntry(300), Transaction: orderingTx(1, 500)},
	}
	sorted := p.Order(entries)
	fee := effectiveGasPrice(sorted[0].Transaction)
	if fee.Cmp(big.NewInt(500)) != 0 {
		t.Fatal("with FeeWeight=1.0, should be purely fee-based")
	}
}

func TestOrdering_HybridEmptyInput(t *testing.T) {
	p := &HybridOrdering{FeeWeight: 0.5}
	sorted := p.Order(nil)
	if len(sorted) != 0 {
		t.Fatalf("sorted nil: want 0, got %d", len(sorted))
	}
	sorted = p.Order([]OrderableEntry{})
	if len(sorted) != 0 {
		t.Fatalf("sorted empty: want 0, got %d", len(sorted))
	}
}

func TestOrdering_HybridClamping(t *testing.T) {
	entries := []OrderableEntry{
		{Commit: orderingCommitEntry(300), Transaction: orderingTx(0, 500)},
		{Commit: orderingCommitEntry(100), Transaction: orderingTx(1, 100)},
	}

	// Negative weight clamped to 0.
	neg := &HybridOrdering{FeeWeight: -1.0}
	sorted := neg.Order(entries)
	if sorted[0].Commit.Commit.Timestamp != 100 {
		t.Fatal("negative FeeWeight should clamp to 0 (time-based)")
	}

	// Over-1 weight clamped to 1.
	over := &HybridOrdering{FeeWeight: 5.0}
	sorted = over.Order(entries)
	fee := effectiveGasPrice(sorted[0].Transaction)
	if fee.Cmp(big.NewInt(500)) != 0 {
		t.Fatal("FeeWeight>1 should clamp to 1.0 (fee-based)")
	}
}

// --- effectiveGasPrice utility ---

func TestOrdering_EffectiveGasPrice(t *testing.T) {
	tx := orderingTx(0, 42000)
	price := effectiveGasPrice(tx)
	if price.Cmp(big.NewInt(42000)) != 0 {
		t.Fatalf("effectiveGasPrice: want 42000, got %s", price)
	}
}

func TestOrdering_EffectiveGasPriceNil(t *testing.T) {
	price := effectiveGasPrice(nil)
	if price.Sign() != 0 {
		t.Fatalf("effectiveGasPrice(nil): want 0, got %s", price)
	}
}
