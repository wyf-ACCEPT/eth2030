package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// pqMakeTx creates a test transaction with the given nonce and gas price.
func pqMakeTx(nonce uint64, gasPrice int64) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
}

// pqMakeDynTx creates a test EIP-1559 tx with given nonce, tip cap, and fee cap.
func pqMakeDynTx(nonce uint64, tipCap, feeCap int64) *types.Transaction {
	return types.NewTransaction(&types.DynamicFeeTx{
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		Value:     big.NewInt(0),
		ChainID:   big.NewInt(1),
		V:         big.NewInt(0),
		R:         big.NewInt(1),
		S:         big.NewInt(1),
	})
}

func TestTxPriorityQueueInsertAndPop(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	tx1 := pqMakeTx(0, 10)
	tx2 := pqMakeTx(1, 20)
	tx3 := pqMakeTx(2, 15)

	sender := types.Address{0x01}
	pq.Insert(tx1, sender)
	pq.Insert(tx2, sender)
	pq.Insert(tx3, sender)

	if pq.Size() != 3 {
		t.Errorf("expected size 3, got %d", pq.Size())
	}

	// Pop should return highest tip first.
	top := pq.Pop()
	if top == nil {
		t.Fatal("expected non-nil pop")
	}
	if top.TxHash != tx2.Hash() {
		t.Errorf("expected tx2 (highest tip) first, got %v", top.TxHash)
	}
}

func TestTxPriorityQueuePeek(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	tx := pqMakeTx(0, 50)
	sender := types.Address{0x01}
	pq.Insert(tx, sender)

	top := pq.Peek()
	if top == nil {
		t.Fatal("expected non-nil peek")
	}
	if top.TxHash != tx.Hash() {
		t.Errorf("peek returned wrong hash")
	}
	// Peek should not remove.
	if pq.Size() != 1 {
		t.Errorf("expected size 1 after peek, got %d", pq.Size())
	}
}

func TestTxPriorityQueueDuplicate(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	tx := pqMakeTx(0, 10)
	sender := types.Address{0x01}

	_, err := pq.Insert(tx, sender)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err = pq.Insert(tx, sender)
	if err != ErrPQDuplicate {
		t.Errorf("expected ErrPQDuplicate, got %v", err)
	}
}

func TestTxPriorityQueueNilTx(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})
	_, err := pq.Insert(nil, types.Address{})
	if err != ErrPQNilTx {
		t.Errorf("expected ErrPQNilTx, got %v", err)
	}
}

func TestTxPriorityQueueRemove(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	tx := pqMakeTx(0, 10)
	sender := types.Address{0x01}
	pq.Insert(tx, sender)

	if !pq.Remove(tx.Hash()) {
		t.Error("Remove returned false for existing entry")
	}
	if pq.Size() != 0 {
		t.Errorf("expected size 0 after remove, got %d", pq.Size())
	}
	if pq.Remove(tx.Hash()) {
		t.Error("second Remove should return false")
	}
}

func TestTxPriorityQueueContains(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	tx := pqMakeTx(0, 10)
	sender := types.Address{0x01}
	pq.Insert(tx, sender)

	if !pq.Contains(tx.Hash()) {
		t.Error("Contains returned false for existing tx")
	}
	if pq.Contains(types.Hash{0xFF}) {
		t.Error("Contains returned true for missing hash")
	}
}

func TestTxPriorityQueueCapacityEviction(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 3})

	sender := types.Address{0x01}
	tx1 := pqMakeTx(0, 10)
	tx2 := pqMakeTx(1, 20)
	tx3 := pqMakeTx(2, 30)
	tx4 := pqMakeTx(3, 25) // higher than tx1, should evict tx1

	pq.Insert(tx1, sender)
	pq.Insert(tx2, sender)
	pq.Insert(tx3, sender)

	_, err := pq.Insert(tx4, sender)
	if err != nil {
		t.Fatalf("insert of tx4: %v", err)
	}
	if pq.Size() != 3 {
		t.Errorf("expected size 3, got %d", pq.Size())
	}

	// tx1 (lowest tip=10) should have been evicted.
	if pq.Contains(tx1.Hash()) {
		t.Error("tx1 should have been evicted")
	}
}

func TestTxPriorityQueueCapacityReject(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 2})

	sender := types.Address{0x01}
	tx1 := pqMakeTx(0, 20)
	tx2 := pqMakeTx(1, 30)

	pq.Insert(tx1, sender)
	pq.Insert(tx2, sender)

	// Inserting a tx with tip=5 (lower than all) should be rejected.
	txLow := pqMakeTx(2, 5)
	_, err := pq.Insert(txLow, sender)
	if err != ErrPQFull {
		t.Errorf("expected ErrPQFull for low-tip tx, got %v", err)
	}
}

func TestTxPriorityQueueGetBySender(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	senderA := types.Address{0x01}
	senderB := types.Address{0x02}

	pq.Insert(pqMakeTx(3, 10), senderA)
	pq.Insert(pqMakeTx(1, 20), senderA)
	pq.Insert(pqMakeTx(2, 15), senderA)
	pq.Insert(pqMakeTx(0, 50), senderB)

	entries := pq.GetBySender(senderA)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries for senderA, got %d", len(entries))
	}
	// Should be sorted by nonce.
	for i := 1; i < len(entries); i++ {
		if entries[i].Nonce <= entries[i-1].Nonce {
			t.Errorf("entries not sorted by nonce: %d <= %d", entries[i].Nonce, entries[i-1].Nonce)
		}
	}

	// senderB should have 1 entry.
	entriesB := pq.GetBySender(senderB)
	if len(entriesB) != 1 {
		t.Errorf("expected 1 entry for senderB, got %d", len(entriesB))
	}
}

func TestTxPriorityQueueNonceGapDetection(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	sender := types.Address{0x01}
	pq.SetPendingNonce(sender, 5)

	// Insert nonces 5, 7, 8 (gap at 6).
	pq.Insert(pqMakeTx(5, 10), sender)
	pq.Insert(pqMakeTx(7, 20), sender)
	pq.Insert(pqMakeTx(8, 15), sender)

	gaps := pq.DetectNonceGaps(sender)
	if len(gaps) != 1 || gaps[0] != 6 {
		t.Errorf("expected gap at nonce 6, got %v", gaps)
	}
}

func TestTxPriorityQueueNonceGapNoGaps(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	sender := types.Address{0x01}
	pq.SetPendingNonce(sender, 0)

	pq.Insert(pqMakeTx(0, 10), sender)
	pq.Insert(pqMakeTx(1, 20), sender)
	pq.Insert(pqMakeTx(2, 15), sender)

	gaps := pq.DetectNonceGaps(sender)
	if len(gaps) != 0 {
		t.Errorf("expected no gaps, got %v", gaps)
	}
}

func TestTxPriorityQueueEvictLowestTip(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	sender := types.Address{0x01}
	txLow := pqMakeTx(0, 5)
	txHigh := pqMakeTx(1, 50)

	pq.Insert(txLow, sender)
	pq.Insert(txHigh, sender)

	evicted := pq.EvictLowestTip()
	if evicted == nil {
		t.Fatal("expected eviction")
	}
	if evicted.TxHash != txLow.Hash() {
		t.Errorf("expected lowest-tip tx to be evicted")
	}
	if pq.Size() != 1 {
		t.Errorf("expected size 1 after eviction, got %d", pq.Size())
	}
}

func TestTxPriorityQueueReinsertAfterReorg(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	sender := types.Address{0x01}
	tx1 := pqMakeTx(0, 10)
	tx2 := pqMakeTx(1, 20)

	pq.Insert(tx1, sender)

	// Simulate reorg: these entries need to be reinserted.
	reorgEntries := []*QueueEntry{
		{
			TxHash:       tx2.Hash(),
			Sender:       sender,
			EffectiveTip: big.NewInt(20),
			GasUsed:      21000,
			Nonce:        1,
		},
	}

	count := pq.ReinsertAfterReorg(reorgEntries)
	if count != 1 {
		t.Errorf("expected 1 reinserted, got %d", count)
	}
	if pq.Size() != 2 {
		t.Errorf("expected size 2, got %d", pq.Size())
	}
}

func TestTxPriorityQueueReinsertDuplicate(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	sender := types.Address{0x01}
	tx := pqMakeTx(0, 10)
	pq.Insert(tx, sender)

	reorgEntries := []*QueueEntry{
		{
			TxHash:       tx.Hash(),
			Sender:       sender,
			EffectiveTip: big.NewInt(10),
			GasUsed:      21000,
			Nonce:        0,
		},
	}

	count := pq.ReinsertAfterReorg(reorgEntries)
	if count != 0 {
		t.Errorf("expected 0 reinserted (duplicate), got %d", count)
	}
}

func TestEffectiveTipCalculatorLegacy(t *testing.T) {
	calc := NewEffectiveTipCalculator(nil, nil)
	tx := pqMakeTx(0, 1000)
	tip := calc.CalcEffectiveTip(tx)
	if tip.Int64() != 1000 {
		t.Errorf("expected tip 1000, got %d", tip.Int64())
	}
}

func TestEffectiveTipCalculatorEIP1559(t *testing.T) {
	baseFee := big.NewInt(100)
	calc := NewEffectiveTipCalculator(baseFee, nil)

	// tipCap=50, feeCap=200 => effectiveTip = min(50, 200-100) = 50
	tx := pqMakeDynTx(0, 50, 200)
	tip := calc.CalcEffectiveTip(tx)
	if tip.Int64() != 50 {
		t.Errorf("expected tip 50, got %d", tip.Int64())
	}

	// tipCap=200, feeCap=150 => effectiveTip = min(200, 150-100) = 50
	tx2 := pqMakeDynTx(1, 200, 150)
	tip2 := calc.CalcEffectiveTip(tx2)
	if tip2.Int64() != 50 {
		t.Errorf("expected tip 50 (capped by feeCap-baseFee), got %d", tip2.Int64())
	}
}

func TestEffectiveTipCalculatorBelowBaseFee(t *testing.T) {
	baseFee := big.NewInt(100)
	calc := NewEffectiveTipCalculator(baseFee, nil)

	// feeCap=50 < baseFee=100 => effectiveTip = 0
	tx := pqMakeDynTx(0, 10, 50)
	tip := calc.CalcEffectiveTip(tx)
	if tip.Sign() != 0 {
		t.Errorf("expected tip 0 for feeCap below baseFee, got %d", tip.Int64())
	}
}

func TestTxPriorityQueuePopEmpty(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	if pq.Pop() != nil {
		t.Error("expected nil pop on empty queue")
	}
	if pq.Peek() != nil {
		t.Error("expected nil peek on empty queue")
	}
}

func TestTxPriorityQueueOrdering(t *testing.T) {
	pq := NewTxPriorityQueue(TxPriorityQueueConfig{MaxSize: 100})

	sender := types.Address{0x01}
	tips := []int64{5, 50, 25, 100, 10}
	for i, tip := range tips {
		pq.Insert(pqMakeTx(uint64(i), tip), sender)
	}

	// Pop in order: should get 100, 50, 25, 10, 5.
	expected := []int64{100, 50, 25, 10, 5}
	for i, exp := range expected {
		entry := pq.Pop()
		if entry == nil {
			t.Fatalf("nil pop at index %d", i)
		}
		if entry.EffectiveTip.Int64() != exp {
			t.Errorf("pop %d: expected tip %d, got %d", i, exp, entry.EffectiveTip.Int64())
		}
	}
}
