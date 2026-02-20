package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// phMakeLegacyTx creates a legacy tx with the given nonce and gas price.
func phMakeLegacyTx(nonce uint64, gasPrice int64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPrice),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	return tx
}

// phMakeDynTx creates an EIP-1559 dynamic fee tx.
func phMakeDynTx(nonce uint64, tipCap, feeCap int64) *types.Transaction {
	to := types.BytesToAddress([]byte{0xde, 0xad})
	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     nonce,
		GasTipCap: big.NewInt(tipCap),
		GasFeeCap: big.NewInt(feeCap),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
	})
	return tx
}

func TestPriceHeap_NewPriceHeap(t *testing.T) {
	ph := NewPriceHeap(big.NewInt(100))
	if ph.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", ph.PendingCount())
	}
	if ph.QueuedCount() != 0 {
		t.Fatalf("expected 0 queued, got %d", ph.QueuedCount())
	}
}

func TestPriceHeap_NilBaseFee(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})
	tx := phMakeLegacyTx(0, 5000)
	tx.SetSender(sender)

	ph.AddPending(tx, sender)
	if ph.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", ph.PendingCount())
	}
}

func TestPriceHeap_AddPending(t *testing.T) {
	ph := NewPriceHeap(big.NewInt(100))
	sender := types.BytesToAddress([]byte{0x01})

	tx1 := phMakeLegacyTx(0, 500)
	tx1.SetSender(sender)
	tx2 := phMakeLegacyTx(1, 1000)
	tx2.SetSender(sender)

	ph.AddPending(tx1, sender)
	ph.AddPending(tx2, sender)

	if ph.PendingCount() != 2 {
		t.Fatalf("expected 2 pending, got %d", ph.PendingCount())
	}
}

func TestPriceHeap_AddPending_Duplicate(t *testing.T) {
	ph := NewPriceHeap(big.NewInt(100))
	sender := types.BytesToAddress([]byte{0x01})

	tx := phMakeLegacyTx(0, 500)
	tx.SetSender(sender)

	ph.AddPending(tx, sender)
	ph.AddPending(tx, sender) // duplicate

	if ph.PendingCount() != 1 {
		t.Fatalf("expected 1 pending (dedup), got %d", ph.PendingCount())
	}
}

func TestPriceHeap_AddQueued(t *testing.T) {
	ph := NewPriceHeap(big.NewInt(100))
	sender := types.BytesToAddress([]byte{0x02})

	tx := phMakeLegacyTx(5, 300)
	tx.SetSender(sender)

	ph.AddQueued(tx, sender)
	if ph.QueuedCount() != 1 {
		t.Fatalf("expected 1 queued, got %d", ph.QueuedCount())
	}
	if ph.PendingCount() != 0 {
		t.Fatalf("expected 0 pending, got %d", ph.PendingCount())
	}
}

func TestPriceHeap_PopCheapest_Order(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	// Add transactions with different gas prices.
	prices := []int64{300, 100, 500, 200}
	for i, p := range prices {
		tx := phMakeLegacyTx(uint64(i), p)
		tx.SetSender(sender)
		ph.AddPending(tx, sender)
	}

	// PopCheapest should return the lowest-priced transaction first.
	tx := ph.PopCheapest()
	if tx == nil {
		t.Fatal("expected non-nil tx from PopCheapest")
	}
	if tx.GasPrice().Int64() != 100 {
		t.Fatalf("expected cheapest price 100, got %d", tx.GasPrice().Int64())
	}
}

func TestPriceHeap_PopCheapest_Empty(t *testing.T) {
	ph := NewPriceHeap(nil)
	tx := ph.PopCheapest()
	if tx != nil {
		t.Fatal("expected nil from empty heap")
	}
}

func TestPriceHeap_PopCheapestQueued(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	tx1 := phMakeLegacyTx(0, 200)
	tx1.SetSender(sender)
	tx2 := phMakeLegacyTx(1, 50)
	tx2.SetSender(sender)

	ph.AddQueued(tx1, sender)
	ph.AddQueued(tx2, sender)

	tx := ph.PopCheapestQueued()
	if tx == nil {
		t.Fatal("expected non-nil tx")
	}
	if tx.GasPrice().Int64() != 50 {
		t.Fatalf("expected cheapest queued price 50, got %d", tx.GasPrice().Int64())
	}
}

func TestPriceHeap_PopCheapestQueued_Empty(t *testing.T) {
	ph := NewPriceHeap(nil)
	if ph.PopCheapestQueued() != nil {
		t.Fatal("expected nil from empty queued heap")
	}
}

func TestPriceHeap_Remove_LazyDeletion(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	tx1 := phMakeLegacyTx(0, 100)
	tx1.SetSender(sender)
	tx2 := phMakeLegacyTx(1, 200)
	tx2.SetSender(sender)

	ph.AddPending(tx1, sender)
	ph.AddPending(tx2, sender)

	ph.Remove(tx1.Hash())

	if ph.StaleCount() != 1 {
		t.Fatalf("expected 1 stale, got %d", ph.StaleCount())
	}

	// PopCheapest should skip the removed tx and return the live one.
	tx := ph.PopCheapest()
	if tx == nil {
		t.Fatal("expected non-nil tx after removing cheaper one")
	}
	if tx.Nonce() != 1 {
		t.Fatalf("expected nonce 1, got %d", tx.Nonce())
	}
}

func TestPriceHeap_Remove_Unknown(t *testing.T) {
	ph := NewPriceHeap(nil)
	// Removing unknown hash should not panic.
	ph.Remove(types.Hash{0x01})
	if ph.StaleCount() != 0 {
		t.Fatal("expected 0 stale for unknown remove")
	}
}

func TestPriceHeap_DetectNonceGaps(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	// Add nonces 0, 1, 3, 5 -- gaps at 2 and 4.
	for _, n := range []uint64{0, 1, 3, 5} {
		tx := phMakeLegacyTx(n, 100)
		tx.SetSender(sender)
		ph.AddPending(tx, sender)
	}

	gaps := ph.DetectNonceGaps(sender, 0)
	if len(gaps) != 2 {
		t.Fatalf("expected 2 gaps, got %d: %v", len(gaps), gaps)
	}
	// Gaps should be nonces 2 and 4.
	expectedGaps := map[uint64]bool{2: true, 4: true}
	for _, g := range gaps {
		if !expectedGaps[g] {
			t.Fatalf("unexpected gap nonce: %d", g)
		}
	}
}

func TestPriceHeap_DetectNonceGaps_NoGaps(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	for n := uint64(0); n < 4; n++ {
		tx := phMakeLegacyTx(n, 100)
		tx.SetSender(sender)
		ph.AddPending(tx, sender)
	}

	gaps := ph.DetectNonceGaps(sender, 0)
	if len(gaps) != 0 {
		t.Fatalf("expected no gaps, got %v", gaps)
	}
}

func TestPriceHeap_DetectNonceGaps_UnknownSender(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0xFF})

	gaps := ph.DetectNonceGaps(sender, 0)
	if gaps != nil {
		t.Fatalf("expected nil for unknown sender, got %v", gaps)
	}
}

func TestPriceHeap_SetBaseFee(t *testing.T) {
	baseFee := big.NewInt(100)
	ph := NewPriceHeap(baseFee)
	sender := types.BytesToAddress([]byte{0x01})

	// EIP-1559 tx with tipCap=20, feeCap=200.
	// Effective = min(200, 100+20) = 120.
	tx := phMakeDynTx(0, 20, 200)
	tx.SetSender(sender)
	ph.AddPending(tx, sender)

	// Update base fee to 150.
	// New effective = min(200, 150+20) = 170.
	ph.SetBaseFee(big.NewInt(150))

	got := ph.PopCheapest()
	if got == nil {
		t.Fatal("expected tx after SetBaseFee")
	}
	// Verify it is the same tx.
	if got.Hash() != tx.Hash() {
		t.Fatal("expected same tx hash after reheap")
	}
}

func TestPriceHeap_SetBaseFee_Nil(t *testing.T) {
	ph := NewPriceHeap(big.NewInt(100))
	sender := types.BytesToAddress([]byte{0x01})

	tx := phMakeLegacyTx(0, 500)
	tx.SetSender(sender)
	ph.AddPending(tx, sender)

	// Setting baseFee to nil should not panic.
	ph.SetBaseFee(nil)

	if ph.PendingCount() != 1 {
		t.Fatalf("expected 1 pending after nil baseFee, got %d", ph.PendingCount())
	}
}

func TestPriceHeap_Cleanup(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	for i := 0; i < 5; i++ {
		tx := phMakeLegacyTx(uint64(i), int64(100+i*10))
		tx.SetSender(sender)
		ph.AddPending(tx, sender)
	}

	// Remove two via lazy deletion.
	tx0 := phMakeLegacyTx(0, 100)
	tx0.SetSender(sender)
	tx1 := phMakeLegacyTx(1, 110)
	tx1.SetSender(sender)
	ph.Remove(tx0.Hash())
	ph.Remove(tx1.Hash())

	if ph.StaleCount() != 2 {
		t.Fatalf("expected 2 stale, got %d", ph.StaleCount())
	}

	ph.Cleanup()

	if ph.StaleCount() != 0 {
		t.Fatalf("expected 0 stale after cleanup, got %d", ph.StaleCount())
	}
	if ph.PendingCount() != 3 {
		t.Fatalf("expected 3 pending after cleanup, got %d", ph.PendingCount())
	}
}

func TestPriceHeap_Cleanup_NoStale(t *testing.T) {
	ph := NewPriceHeap(nil)
	// Cleanup on empty heap should not panic.
	ph.Cleanup()
	if ph.StaleCount() != 0 {
		t.Fatal("expected 0 stale")
	}
}

func TestPriceHeap_PeekHighestTip(t *testing.T) {
	ph := NewPriceHeap(nil)
	sender := types.BytesToAddress([]byte{0x01})

	// PeekHighestTip on empty heap.
	if ph.PeekHighestTip() != nil {
		t.Fatal("expected nil from empty heap")
	}

	prices := []int64{100, 500, 300}
	for i, p := range prices {
		tx := phMakeLegacyTx(uint64(i), p)
		tx.SetSender(sender)
		ph.AddPending(tx, sender)
	}

	tx := ph.PeekHighestTip()
	if tx == nil {
		t.Fatal("expected non-nil tx")
	}
	if tx.GasPrice().Int64() != 500 {
		t.Fatalf("expected highest tip 500, got %d", tx.GasPrice().Int64())
	}
}

func TestPriceHeap_DynTx_EffectivePriceSorting(t *testing.T) {
	baseFee := big.NewInt(100)
	ph := NewPriceHeap(baseFee)
	sender := types.BytesToAddress([]byte{0x01})

	// tx1: tipCap=10, feeCap=200 => effective = min(200, 100+10) = 110
	tx1 := phMakeDynTx(0, 10, 200)
	tx1.SetSender(sender)
	// tx2: tipCap=50, feeCap=200 => effective = min(200, 100+50) = 150
	tx2 := phMakeDynTx(1, 50, 200)
	tx2.SetSender(sender)
	// tx3: tipCap=5, feeCap=200 => effective = min(200, 100+5) = 105
	tx3 := phMakeDynTx(2, 5, 200)
	tx3.SetSender(sender)

	ph.AddPending(tx1, sender)
	ph.AddPending(tx2, sender)
	ph.AddPending(tx3, sender)

	// PopCheapest should return tx3 (lowest effective price 105).
	cheapest := ph.PopCheapest()
	if cheapest == nil {
		t.Fatal("expected non-nil cheapest")
	}
	if cheapest.Nonce() != 2 {
		t.Fatalf("expected nonce 2 (cheapest), got %d", cheapest.Nonce())
	}
}
