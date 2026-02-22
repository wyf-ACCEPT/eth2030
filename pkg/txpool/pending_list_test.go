package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makePendingTx creates a legacy transaction for pending list tests.
func makePendingTx(sender types.Address, nonce uint64, gasPrice int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xbe, 0xef})
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

// makeDynamicPendingTx creates an EIP-1559 transaction for pending list tests.
func makeDynamicPendingTx(sender types.Address, nonce uint64, tipCap, feeCap int64, gas uint64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xbe, 0xef})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       gas,
		To:        &to,
		Value:     big.NewInt(0),
	})
	tx.SetSender(sender)
	return tx
}

var (
	plSender1 = types.BytesToAddress([]byte{0x01})
	plSender2 = types.BytesToAddress([]byte{0x02})
)

func TestPendingListAddAndGet(t *testing.T) {
	pl := NewPendingList(nil)

	tx0 := makePendingTx(plSender1, 0, 100, 21000)
	tx1 := makePendingTx(plSender1, 1, 200, 21000)

	replaced, err := pl.Add(plSender1, tx0)
	if err != nil || replaced {
		t.Fatalf("Add(tx0): replaced=%v, err=%v", replaced, err)
	}
	replaced, err = pl.Add(plSender1, tx1)
	if err != nil || replaced {
		t.Fatalf("Add(tx1): replaced=%v, err=%v", replaced, err)
	}

	if pl.Len() != 2 {
		t.Fatalf("Len: got %d, want 2", pl.Len())
	}
	if pl.AccountLen(plSender1) != 2 {
		t.Fatalf("AccountLen: got %d, want 2", pl.AccountLen(plSender1))
	}

	got := pl.Get(plSender1, 0)
	if got == nil || got.Nonce() != 0 {
		t.Fatalf("Get(nonce=0): got %v", got)
	}
	got = pl.Get(plSender1, 1)
	if got == nil || got.Nonce() != 1 {
		t.Fatalf("Get(nonce=1): got %v", got)
	}
	got = pl.Get(plSender1, 99)
	if got != nil {
		t.Fatalf("Get(nonce=99): expected nil, got %v", got)
	}
}

func TestPendingListReplacementByFee(t *testing.T) {
	pl := NewPendingList(nil)

	tx := makePendingTx(plSender1, 0, 100, 21000)
	pl.Add(plSender1, tx)

	// Replacement with insufficient bump (only 5% increase).
	txLowBump := makePendingTx(plSender1, 0, 105, 21000)
	_, err := pl.Add(plSender1, txLowBump)
	if err != ErrReplacementUnderpriced {
		t.Fatalf("expected ErrReplacementUnderpriced, got %v", err)
	}

	// Replacement with exactly 10% bump (100 * 1.10 = 110).
	txGoodBump := makePendingTx(plSender1, 0, 110, 21000)
	replaced, err := pl.Add(plSender1, txGoodBump)
	if err != nil || !replaced {
		t.Fatalf("10%% bump: replaced=%v, err=%v", replaced, err)
	}

	// Verify the replacement is in place.
	got := pl.Get(plSender1, 0)
	if got.GasPrice().Int64() != 110 {
		t.Fatalf("replacement gas price: got %d, want 110", got.GasPrice().Int64())
	}
	if pl.Len() != 1 {
		t.Fatalf("Len after replace: got %d, want 1", pl.Len())
	}
}

func TestPendingListReplacementDynamicFee(t *testing.T) {
	baseFee := big.NewInt(10)
	pl := NewPendingList(baseFee)

	// Original: tip=100, feeCap=200
	tx := makeDynamicPendingTx(plSender1, 0, 100, 200, 21000)
	pl.Add(plSender1, tx)

	// Replacement needs 10% bump on BOTH effective price AND tip cap.
	// tip must be >= 110, feeCap must be >= 220
	txBad := makeDynamicPendingTx(plSender1, 0, 105, 220, 21000) // tip too low
	_, err := pl.Add(plSender1, txBad)
	if err != ErrReplacementUnderpriced {
		t.Fatalf("expected ErrReplacementUnderpriced for low tip, got %v", err)
	}

	txGood := makeDynamicPendingTx(plSender1, 0, 112, 230, 21000)
	replaced, err := pl.Add(plSender1, txGood)
	if err != nil || !replaced {
		t.Fatalf("dynamic replacement: replaced=%v, err=%v", replaced, err)
	}
}

func TestPendingListNonceGapDetection(t *testing.T) {
	pl := NewPendingList(nil)
	pl.UpdateState(plSender1, 0, big.NewInt(1e18))

	// Add nonces 0, 1, 3, 5 (gaps at 2 and 4).
	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 3, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 5, 100, 21000))

	gaps := pl.DetectGaps(plSender1)
	if len(gaps) != 2 {
		t.Fatalf("gaps: got %v, want [2 4]", gaps)
	}
	if gaps[0] != 2 || gaps[1] != 4 {
		t.Fatalf("gaps: got %v, want [2 4]", gaps)
	}
}

func TestPendingListNoGaps(t *testing.T) {
	pl := NewPendingList(nil)
	pl.UpdateState(plSender1, 0, big.NewInt(1e18))

	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 2, 100, 21000))

	gaps := pl.DetectGaps(plSender1)
	if len(gaps) != 0 {
		t.Fatalf("expected no gaps, got %v", gaps)
	}
}

func TestPendingListReady(t *testing.T) {
	pl := NewPendingList(nil)
	pl.UpdateState(plSender1, 0, big.NewInt(1e18))

	// Add nonces 0, 1, 3 -- ready prefix is [0, 1].
	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 3, 100, 21000))

	ready := pl.Ready(plSender1)
	if len(ready) != 2 {
		t.Fatalf("ready count: got %d, want 2", len(ready))
	}
	if ready[0].Nonce() != 0 || ready[1].Nonce() != 1 {
		t.Fatalf("ready nonces: [%d, %d], want [0, 1]", ready[0].Nonce(), ready[1].Nonce())
	}
}

func TestPendingListPromote(t *testing.T) {
	pl := NewPendingList(nil)
	pl.UpdateState(plSender1, 0, big.NewInt(1e18))

	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 3, 100, 21000))

	promoted := pl.Promote(plSender1)
	if len(promoted) != 2 {
		t.Fatalf("promoted count: got %d, want 2", len(promoted))
	}
	// After promote, only nonce 3 remains.
	if pl.AccountLen(plSender1) != 1 {
		t.Fatalf("remaining: got %d, want 1", pl.AccountLen(plSender1))
	}
}

func TestPendingListUpdateState(t *testing.T) {
	pl := NewPendingList(nil)

	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 2, 100, 21000))

	// Simulate new block that mined nonces 0 and 1.
	removed := pl.UpdateState(plSender1, 2, big.NewInt(1e18))
	if len(removed) != 2 {
		t.Fatalf("removed count: got %d, want 2", len(removed))
	}
	if pl.Len() != 1 {
		t.Fatalf("remaining: got %d, want 1", pl.Len())
	}
	got := pl.Get(plSender1, 2)
	if got == nil || got.Nonce() != 2 {
		t.Fatalf("remaining tx nonce: got %v", got)
	}
}

func TestPendingListRemove(t *testing.T) {
	pl := NewPendingList(nil)

	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 200, 21000))

	if !pl.Remove(plSender1, 0) {
		t.Fatal("Remove(nonce=0) should return true")
	}
	if pl.Remove(plSender1, 99) {
		t.Fatal("Remove(nonce=99) should return false")
	}
	if pl.Len() != 1 {
		t.Fatalf("Len after remove: got %d, want 1", pl.Len())
	}
}

func TestPendingListByGasPrice(t *testing.T) {
	pl := NewPendingList(nil)

	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 300, 21000))
	pl.Add(plSender2, makePendingTx(plSender2, 0, 200, 21000))

	sorted := pl.ByGasPrice()
	if len(sorted) != 3 {
		t.Fatalf("ByGasPrice count: got %d, want 3", len(sorted))
	}
	// Should be sorted descending: 300, 200, 100.
	if sorted[0].GasPrice().Int64() != 300 {
		t.Fatalf("sorted[0] gas price: got %d, want 300", sorted[0].GasPrice().Int64())
	}
	if sorted[1].GasPrice().Int64() != 200 {
		t.Fatalf("sorted[1] gas price: got %d, want 200", sorted[1].GasPrice().Int64())
	}
	if sorted[2].GasPrice().Int64() != 100 {
		t.Fatalf("sorted[2] gas price: got %d, want 100", sorted[2].GasPrice().Int64())
	}
}

func TestPendingListSenders(t *testing.T) {
	pl := NewPendingList(nil)

	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender2, makePendingTx(plSender2, 0, 200, 21000))

	senders := pl.Senders()
	if len(senders) != 2 {
		t.Fatalf("Senders count: got %d, want 2", len(senders))
	}
}

func TestPendingListSetBaseFee(t *testing.T) {
	pl := NewPendingList(big.NewInt(10))

	pl.SetBaseFee(big.NewInt(20))
	// Verify internal state updated.
	pl.mu.RLock()
	if pl.baseFee.Int64() != 20 {
		t.Fatalf("baseFee: got %d, want 20", pl.baseFee.Int64())
	}
	pl.mu.RUnlock()

	pl.SetBaseFee(nil)
	pl.mu.RLock()
	if pl.baseFee != nil {
		t.Fatalf("baseFee: got %v, want nil", pl.baseFee)
	}
	pl.mu.RUnlock()
}

func TestPendingListEmptyAccount(t *testing.T) {
	pl := NewPendingList(nil)

	if pl.Len() != 0 {
		t.Fatalf("empty Len: got %d, want 0", pl.Len())
	}
	if pl.AccountLen(plSender1) != 0 {
		t.Fatalf("empty AccountLen: got %d, want 0", pl.AccountLen(plSender1))
	}
	ready := pl.Ready(plSender1)
	if len(ready) != 0 {
		t.Fatalf("empty Ready: got %d, want 0", len(ready))
	}
	gaps := pl.DetectGaps(plSender1)
	if len(gaps) != 0 {
		t.Fatalf("empty DetectGaps: got %v", gaps)
	}
}

func TestPendingListInsertOrder(t *testing.T) {
	pl := NewPendingList(nil)
	pl.UpdateState(plSender1, 0, big.NewInt(1e18))

	// Insert out of order: 3, 1, 0, 2.
	pl.Add(plSender1, makePendingTx(plSender1, 3, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 1, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 0, 100, 21000))
	pl.Add(plSender1, makePendingTx(plSender1, 2, 100, 21000))

	// Ready prefix should include all 4 since no gaps.
	ready := pl.Ready(plSender1)
	if len(ready) != 4 {
		t.Fatalf("ready count: got %d, want 4", len(ready))
	}
	for i, tx := range ready {
		if tx.Nonce() != uint64(i) {
			t.Fatalf("ready[%d].Nonce: got %d, want %d", i, tx.Nonce(), i)
		}
	}
}
