package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeQueueTx creates a legacy transaction for queue manager tests.
func makeQueueTx(sender types.Address, nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xca, 0xfe})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
	})
	tx.SetSender(sender)
	return tx
}

var (
	qmSender1 = types.BytesToAddress([]byte{0x11})
	qmSender2 = types.BytesToAddress([]byte{0x22})
	qmSender3 = types.BytesToAddress([]byte{0x33})
)

func TestQueueManagerAddAndGet(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	tx5 := makeQueueTx(qmSender1, 5, 100, 21000)
	tx7 := makeQueueTx(qmSender1, 7, 200, 21000)

	evicted, err := qm.Add(qmSender1, tx5)
	if err != nil || evicted != nil {
		t.Fatalf("Add(nonce=5): evicted=%v, err=%v", evicted, err)
	}
	evicted, err = qm.Add(qmSender1, tx7)
	if err != nil || evicted != nil {
		t.Fatalf("Add(nonce=7): evicted=%v, err=%v", evicted, err)
	}

	if qm.Len() != 2 {
		t.Fatalf("Len: got %d, want 2", qm.Len())
	}

	got := qm.Get(qmSender1, 5)
	if got == nil || got.Nonce() != 5 {
		t.Fatalf("Get(5): got %v", got)
	}
	got = qm.Get(qmSender1, 6)
	if got != nil {
		t.Fatalf("Get(6): expected nil, got %v", got)
	}
}

func TestQueueManagerReplacement(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	tx := makeQueueTx(qmSender1, 5, 100, 21000)
	qm.Add(qmSender1, tx)

	// Insufficient bump.
	txLow := makeQueueTx(qmSender1, 5, 105, 21000)
	_, err := qm.Add(qmSender1, txLow)
	if err != ErrReplacementUnderpriced {
		t.Fatalf("expected ErrReplacementUnderpriced, got %v", err)
	}

	// Sufficient bump (10% of 100 = 10, need >= 110).
	txHigh := makeQueueTx(qmSender1, 5, 110, 21000)
	evicted, err := qm.Add(qmSender1, txHigh)
	if err != nil {
		t.Fatalf("replacement err: %v", err)
	}
	if evicted == nil || evicted.GasPrice().Int64() != 100 {
		t.Fatalf("evicted: got %v, want old tx with price 100", evicted)
	}
	// Count unchanged since it's a replacement.
	if qm.Len() != 1 {
		t.Fatalf("Len after replace: got %d, want 1", qm.Len())
	}
}

func TestQueueManagerPerAccountCapacity(t *testing.T) {
	cfg := QueueManagerConfig{MaxPerAccount: 3, MaxGlobal: 100}
	qm := NewQueueManager(cfg, nil)

	// Fill account to capacity.
	qm.Add(qmSender1, makeQueueTx(qmSender1, 10, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 11, 200, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 12, 300, 21000))

	if qm.Len() != 3 {
		t.Fatalf("Len: got %d, want 3", qm.Len())
	}

	// Adding a 4th should evict the lowest price (100).
	evicted, err := qm.Add(qmSender1, makeQueueTx(qmSender1, 13, 400, 21000))
	if err != nil {
		t.Fatalf("Add at capacity: err=%v", err)
	}
	if evicted == nil {
		t.Fatal("expected eviction, got nil")
	}
	if evicted.GasPrice().Int64() != 100 {
		t.Fatalf("evicted price: got %d, want 100", evicted.GasPrice().Int64())
	}
	if qm.Len() != 3 {
		t.Fatalf("Len after evict: got %d, want 3", qm.Len())
	}
}

func TestQueueManagerGlobalCapacity(t *testing.T) {
	cfg := QueueManagerConfig{MaxPerAccount: 100, MaxGlobal: 3}
	qm := NewQueueManager(cfg, nil)

	// Fill global capacity across different senders.
	qm.Add(qmSender1, makeQueueTx(qmSender1, 10, 100, 21000))
	qm.Add(qmSender2, makeQueueTx(qmSender2, 10, 200, 21000))
	qm.Add(qmSender3, makeQueueTx(qmSender3, 10, 300, 21000))

	// Adding a 4th should evict the globally cheapest (sender1, price 100).
	_, err := qm.Add(qmSender1, makeQueueTx(qmSender1, 11, 400, 21000))
	if err != nil {
		t.Fatalf("Add at global capacity: err=%v", err)
	}
	if qm.Len() != 3 {
		t.Fatalf("Len: got %d, want 3", qm.Len())
	}
	// Sender1's nonce 10 (price 100) should have been evicted.
	if qm.Get(qmSender1, 10) != nil {
		t.Fatal("sender1 nonce 10 should have been evicted")
	}
}

func TestQueueManagerPromoteReady(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	// Queued nonces: 5, 6, 7, 9 (gap at 8).
	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 6, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 7, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 9, 100, 21000))

	// Promote starting from nonce 5.
	promoted := qm.PromoteReady(qmSender1, 5)
	if len(promoted) != 3 {
		t.Fatalf("promoted count: got %d, want 3 (nonces 5,6,7)", len(promoted))
	}
	for i, tx := range promoted {
		if tx.Nonce() != uint64(5+i) {
			t.Fatalf("promoted[%d].Nonce: got %d, want %d", i, tx.Nonce(), 5+i)
		}
	}
	// Nonce 9 should remain.
	if qm.Len() != 1 {
		t.Fatalf("remaining: got %d, want 1", qm.Len())
	}
}

func TestQueueManagerPromoteNothingReady(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))

	// Base nonce is 3, but first queued is 5 -- gap at 3,4 -- nothing promotable.
	promoted := qm.PromoteReady(qmSender1, 3)
	if len(promoted) != 0 {
		t.Fatalf("promoted: got %d, want 0", len(promoted))
	}
}

func TestQueueManagerRemove(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 6, 200, 21000))

	if !qm.Remove(qmSender1, 5) {
		t.Fatal("Remove(5) should return true")
	}
	if qm.Remove(qmSender1, 99) {
		t.Fatal("Remove(99) should return false")
	}
	if qm.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", qm.Len())
	}
}

func TestQueueManagerUpdateStateNonce(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 3, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 4, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))

	// Advance state nonce to 5: removes nonces 3 and 4.
	removed := qm.UpdateStateNonce(qmSender1, 5)
	if removed != 2 {
		t.Fatalf("removed: got %d, want 2", removed)
	}
	if qm.Len() != 1 {
		t.Fatalf("Len: got %d, want 1", qm.Len())
	}
}

func TestQueueManagerAccountStats(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 6, 200, 21000))
	qm.UpdateStateNonce(qmSender1, 3)

	count, stateNonce := qm.AccountStats(qmSender1)
	if count != 2 {
		t.Fatalf("count: got %d, want 2", count)
	}
	if stateNonce != 3 {
		t.Fatalf("stateNonce: got %d, want 3", stateNonce)
	}
}

func TestQueueManagerAccountNonces(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 10, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 7, 200, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 15, 300, 21000))

	nonces := qm.AccountNonces(qmSender1)
	if len(nonces) != 3 {
		t.Fatalf("nonces count: got %d, want 3", len(nonces))
	}
	// Should be sorted ascending since insertion maintains order.
	if nonces[0] != 7 || nonces[1] != 10 || nonces[2] != 15 {
		t.Fatalf("nonces: got %v, want [7 10 15]", nonces)
	}
}

func TestQueueManagerEvictAll(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))
	qm.Add(qmSender1, makeQueueTx(qmSender1, 6, 200, 21000))

	removed := qm.EvictAll(qmSender1)
	if removed != 2 {
		t.Fatalf("EvictAll: got %d, want 2", removed)
	}
	if qm.Len() != 0 {
		t.Fatalf("Len after EvictAll: got %d, want 0", qm.Len())
	}
	if qm.AccountCount() != 0 {
		t.Fatalf("AccountCount: got %d, want 0", qm.AccountCount())
	}
}

func TestQueueManagerAccountCount(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	qm.Add(qmSender1, makeQueueTx(qmSender1, 5, 100, 21000))
	qm.Add(qmSender2, makeQueueTx(qmSender2, 5, 200, 21000))

	if qm.AccountCount() != 2 {
		t.Fatalf("AccountCount: got %d, want 2", qm.AccountCount())
	}

	qm.EvictAll(qmSender1)
	if qm.AccountCount() != 1 {
		t.Fatalf("AccountCount after evict: got %d, want 1", qm.AccountCount())
	}
}

func TestQueueManagerSetBaseFee(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, big.NewInt(10))

	qm.SetBaseFee(big.NewInt(50))
	qm.mu.RLock()
	if qm.baseFee.Int64() != 50 {
		t.Fatalf("baseFee: got %d, want 50", qm.baseFee.Int64())
	}
	qm.mu.RUnlock()
}

func TestQueueManagerEmptyOperations(t *testing.T) {
	qm := NewQueueManager(QueueManagerConfig{}, nil)

	if qm.Len() != 0 {
		t.Fatalf("empty Len: got %d", qm.Len())
	}
	count, nonce := qm.AccountStats(qmSender1)
	if count != 0 || nonce != 0 {
		t.Fatalf("empty AccountStats: count=%d, nonce=%d", count, nonce)
	}
	promoted := qm.PromoteReady(qmSender1, 0)
	if len(promoted) != 0 {
		t.Fatalf("empty PromoteReady: got %d", len(promoted))
	}
	if qm.Remove(qmSender1, 0) {
		t.Fatal("empty Remove should return false")
	}
}
