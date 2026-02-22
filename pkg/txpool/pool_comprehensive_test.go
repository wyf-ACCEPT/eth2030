package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// ---------------------------------------------------------------------------
// Test: Pool Add Valid Transaction
// ---------------------------------------------------------------------------

// TestPoolAddValidTransaction adds a valid transaction and verifies it appears
// in the pending set with the correct properties.
func TestPoolAddValidTransaction(t *testing.T) {
	pool, _ := newRichPool()

	tx := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(tx); err != nil {
		t.Fatalf("AddLocal: %v", err)
	}

	if pool.PendingCount() != 1 {
		t.Errorf("PendingCount = %d, want 1", pool.PendingCount())
	}

	// Verify the tx is retrievable.
	got := pool.Get(tx.Hash())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Nonce() != 0 {
		t.Errorf("nonce = %d, want 0", got.Nonce())
	}
	if got.GasPrice().Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("gas price = %s, want 1000", got.GasPrice())
	}

	// Verify it appears in Pending grouped by sender.
	pending := pool.Pending()
	senderTxs, ok := pending[testSender]
	if !ok {
		t.Fatal("sender not found in pending")
	}
	if len(senderTxs) != 1 {
		t.Errorf("pending[sender] length = %d, want 1", len(senderTxs))
	}
}

// ---------------------------------------------------------------------------
// Test: Pool Reject Invalid Nonce
// ---------------------------------------------------------------------------

// TestPoolRejectInvalidNonce verifies that transactions with nonces below
// the sender's current state nonce are rejected.
func TestPoolRejectInvalidNonce(t *testing.T) {
	pool, state := newRichPool()
	state.nonces[testSender] = 5

	// Nonce 4 is below state nonce 5.
	tx := makeTx(4, 1000, 21000)
	err := pool.AddLocal(tx)
	if err != ErrNonceTooLow {
		t.Errorf("expected ErrNonceTooLow, got: %v", err)
	}

	// Nonce 5 (equal to state nonce) should succeed.
	tx5 := makeTx(5, 1000, 21000)
	if err := pool.AddLocal(tx5); err != nil {
		t.Errorf("expected success for nonce 5, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Pool Reject Insufficient Balance
// ---------------------------------------------------------------------------

// TestPoolRejectInsufficientBalance verifies that a transaction is rejected
// when the sender cannot afford gas * price + value.
func TestPoolRejectInsufficientBalance(t *testing.T) {
	state := newMockState()
	// Give sender 100 wei -- not enough for gas cost.
	state.balances[testSender] = big.NewInt(100)
	pool := New(DefaultConfig(), state)

	tx := makeTx(0, 1000, 21000) // cost = 21000 * 1000 = 21_000_000
	err := pool.AddLocal(tx)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds, got: %v", err)
	}

	// Also test with value transfer: sender has enough for gas but not value.
	state.balances[testSender] = big.NewInt(21_000_000) // exactly enough for gas
	valueTx := makeTxWithValue(0, 1000, 21000, big.NewInt(1))
	err = pool.AddLocal(valueTx)
	if err != ErrInsufficientFunds {
		t.Errorf("expected ErrInsufficientFunds with value, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Pool Replace By Fee
// ---------------------------------------------------------------------------

// TestPoolReplaceByFee verifies that a transaction can be replaced by one
// with a gas price at least 10% higher.
func TestPoolReplaceByFee(t *testing.T) {
	pool, _ := newRichPool()

	// Add original at gas price 1000.
	orig := makeTx(0, 1000, 21000)
	if err := pool.AddLocal(orig); err != nil {
		t.Fatalf("AddLocal original: %v", err)
	}

	// 5% bump (1050) should fail.
	lowBump := makeTx(0, 1050, 21000)
	err := pool.AddLocal(lowBump)
	if err != ErrReplacementUnderpriced {
		t.Errorf("expected ErrReplacementUnderpriced for 5%% bump, got: %v", err)
	}

	// 10% bump (1100) should succeed.
	goodBump := makeTx(0, 1100, 21000)
	if err := pool.AddLocal(goodBump); err != nil {
		t.Fatalf("AddLocal 10%% bump: %v", err)
	}

	// Pool should still have exactly 1 tx.
	if pool.Count() != 1 {
		t.Errorf("pool count = %d, want 1", pool.Count())
	}
	// Old tx gone, new tx present.
	if pool.Get(orig.Hash()) != nil {
		t.Error("original tx should have been replaced")
	}
	if pool.Get(goodBump.Hash()) == nil {
		t.Error("replacement tx should be in pool")
	}
}

// ---------------------------------------------------------------------------
// Test: Pool Capacity
// ---------------------------------------------------------------------------

// TestPoolCapacity fills the pool to capacity and verifies eviction behavior.
// Eviction protects the highest-nonce pending tx per sender, so we use
// multiple txs from a single sender to produce eviction candidates.
func TestPoolCapacity(t *testing.T) {
	config := DefaultConfig()
	config.MaxSize = 4

	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	sender2 := types.BytesToAddress([]byte{0x04})
	state.balances[sender2] = new(big.Int).Set(richBalance)

	pool := New(config, state)

	// Fill pool with 4 sequential txs from testSender (nonces 0..3).
	// Prices: 100, 200, 300, 400. Nonce 3 (price 400) is protected.
	var txs []*types.Transaction
	for i := uint64(0); i < 4; i++ {
		tx := makeTx(i, int64((i+1)*100), 21000)
		if err := pool.AddLocal(tx); err != nil {
			t.Fatalf("AddLocal nonce %d: %v", i, err)
		}
		txs = append(txs, tx)
	}

	if pool.Count() != 4 {
		t.Fatalf("pool count = %d, want 4", pool.Count())
	}

	// Adding a 5th tx from sender2 should evict the cheapest unprotected
	// tx from testSender (nonce 0, price 100).
	highTx := makeTxFrom(sender2, 0, 500, 21000)
	err := pool.AddLocal(highTx)
	if err != nil {
		t.Fatalf("AddLocal high-priced tx: %v", err)
	}

	if pool.Count() != 4 {
		t.Errorf("pool count after eviction = %d, want 4", pool.Count())
	}

	// The cheapest unprotected tx (nonce 0, price 100) should have been evicted.
	if pool.Get(txs[0].Hash()) != nil {
		t.Error("cheapest tx should have been evicted")
	}
	if pool.Get(highTx.Hash()) == nil {
		t.Error("high-priced tx should be in pool")
	}
}

// ---------------------------------------------------------------------------
// Test: Pool Pending Ordering
// ---------------------------------------------------------------------------

// TestPoolPendingOrdering verifies that transactions from the same account
// appear in nonce order within the pending set.
func TestPoolPendingOrdering(t *testing.T) {
	pool, _ := newRichPool()

	// Add transactions in reverse nonce order.
	for i := uint64(4); i > 0; i-- {
		tx := makeTx(i, 1000+int64(i), 21000)
		pool.AddLocal(tx)
	}
	// Now add nonce 0 to promote them all.
	pool.AddLocal(makeTx(0, 1000, 21000))

	if pool.PendingCount() != 5 {
		t.Fatalf("PendingCount = %d, want 5", pool.PendingCount())
	}

	// Verify nonce ordering.
	pending := pool.Pending()
	senderTxs := pending[testSender]
	if len(senderTxs) != 5 {
		t.Fatalf("sender txs = %d, want 5", len(senderTxs))
	}
	for i, tx := range senderTxs {
		if tx.Nonce() != uint64(i) {
			t.Errorf("tx[%d].Nonce() = %d, want %d", i, tx.Nonce(), i)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Blob Pool Limits
// ---------------------------------------------------------------------------

// TestBlobPoolLimitsComprehensive verifies blob pool per-account (16) and
// total (256) limits with default config.
func TestBlobPoolLimitsComprehensive(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.MaxBlobsPerAccount = 3 // Small for testing.
	config.MaxBlobs = 5

	pool := NewBlobPool(config, nil)

	addr := types.BytesToAddress([]byte{0xAA})

	// Add up to the per-account limit.
	for i := uint64(0); i < 3; i++ {
		tx := makeBlobPoolTxFrom(i, 100+int64(i), addr)
		if err := pool.Add(tx); err != nil {
			t.Fatalf("Add blob tx %d: %v", i, err)
		}
	}

	if pool.Count() != 3 {
		t.Errorf("pool count = %d, want 3", pool.Count())
	}

	// Next blob tx from same account should be rejected.
	txOver := makeBlobPoolTxFrom(3, 200, addr)
	err := pool.Add(txOver)
	if err != ErrBlobAccountLimit {
		t.Errorf("expected ErrBlobAccountLimit, got: %v", err)
	}

	// Different accounts can still add. Use distinct blobFeeCaps to
	// avoid hash collisions (tx hash does not include the sender).
	addr2 := types.BytesToAddress([]byte{0xBB})
	tx4 := makeBlobPoolTxFrom(0, 150, addr2)
	if err := pool.Add(tx4); err != nil {
		t.Fatalf("Add from addr2: %v", err)
	}

	addr3 := types.BytesToAddress([]byte{0xCC})
	tx5 := makeBlobPoolTxFrom(0, 160, addr3)
	if err := pool.Add(tx5); err != nil {
		t.Fatalf("Add from addr3: %v", err)
	}

	if pool.Count() != 5 {
		t.Errorf("pool count = %d, want 5", pool.Count())
	}

	// Pool is now full. Adding a cheap one should be rejected.
	addr4 := types.BytesToAddress([]byte{0xDD})
	txCheap := makeBlobPoolTxFrom(0, 50, addr4)
	err = pool.Add(txCheap)
	if err != ErrBlobPoolFull {
		t.Errorf("expected ErrBlobPoolFull, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Blob Pool RBF
// ---------------------------------------------------------------------------

// TestBlobPoolRBFComprehensive verifies blob transaction replace-by-fee
// requires a 10% price bump.
func TestBlobPoolRBFComprehensive(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	addr := types.BytesToAddress([]byte{0xAA})

	// Add original with blob fee cap 100.
	orig := makeBlobPoolTxFrom(0, 100, addr)
	if err := pool.Add(orig); err != nil {
		t.Fatalf("Add original: %v", err)
	}

	// 9% bump (109): should fail.
	low := makeBlobPoolTxFrom(0, 109, addr)
	err := pool.Add(low)
	if err != ErrBlobReplaceTooLow {
		t.Errorf("expected ErrBlobReplaceTooLow for 9%% bump, got: %v", err)
	}

	// 10% bump (110): should succeed.
	good := makeBlobPoolTxFrom(0, 110, addr)
	if err := pool.Add(good); err != nil {
		t.Fatalf("Add 10%% bump: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("pool count = %d, want 1", pool.Count())
	}
	if pool.Has(orig.Hash()) {
		t.Error("original should have been replaced")
	}
	if !pool.Has(good.Hash()) {
		t.Error("replacement should be in pool")
	}

	// Further replacement with a big bump.
	bigger := makeBlobPoolTxFrom(0, 500, addr)
	if err := pool.Add(bigger); err != nil {
		t.Fatalf("Add big bump: %v", err)
	}
	if pool.Count() != 1 {
		t.Errorf("pool count = %d, want 1 after second replacement", pool.Count())
	}
	if !pool.Has(bigger.Hash()) {
		t.Error("big bump tx should be in pool")
	}
}

// ---------------------------------------------------------------------------
// Test: Pool with Multiple Senders
// ---------------------------------------------------------------------------

// TestPoolMultipleSenders verifies that the pool correctly manages transactions
// from multiple senders with different nonces and gas prices.
func TestPoolMultipleSenders(t *testing.T) {
	state := newMockState()
	sender1 := types.BytesToAddress([]byte{0x01})
	sender2 := types.BytesToAddress([]byte{0x02})
	sender3 := types.BytesToAddress([]byte{0x03})
	state.balances[sender1] = new(big.Int).Set(richBalance)
	state.balances[sender2] = new(big.Int).Set(richBalance)
	state.balances[sender3] = new(big.Int).Set(richBalance)

	pool := New(DefaultConfig(), state)

	// Add sequential txs from each sender.
	for i := uint64(0); i < 3; i++ {
		pool.AddLocal(makeTxFrom(sender1, i, 1000, 21000))
		pool.AddLocal(makeTxFrom(sender2, i, 2000, 21000))
		pool.AddLocal(makeTxFrom(sender3, i, 3000, 21000))
	}

	if pool.PendingCount() != 9 {
		t.Errorf("PendingCount = %d, want 9", pool.PendingCount())
	}

	// Verify each sender has 3 pending txs.
	pending := pool.Pending()
	for _, sender := range []types.Address{sender1, sender2, sender3} {
		txs, ok := pending[sender]
		if !ok {
			t.Errorf("sender %x not found in pending", sender[:4])
			continue
		}
		if len(txs) != 3 {
			t.Errorf("sender %x: pending count = %d, want 3", sender[:4], len(txs))
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Pool Reset After Block
// ---------------------------------------------------------------------------

// TestPoolResetAfterBlock verifies that resetting the pool after a block
// removes included transactions and keeps remaining ones.
func TestPoolResetAfterBlock(t *testing.T) {
	pool, _ := newRichPool()

	// Add txs with nonces 0..4.
	for i := uint64(0); i < 5; i++ {
		pool.AddLocal(makeTx(i, 1000, 21000))
	}
	if pool.PendingCount() != 5 {
		t.Fatalf("PendingCount = %d, want 5", pool.PendingCount())
	}

	// Simulate block including nonces 0..2: state nonce advances to 3.
	newState := newMockState()
	newState.balances[testSender] = new(big.Int).Set(richBalance)
	newState.nonces[testSender] = 3

	pool.Reset(newState)

	// After reset, only nonces 3 and 4 should remain pending.
	if pool.PendingCount() != 2 {
		t.Errorf("PendingCount after reset = %d, want 2", pool.PendingCount())
	}
}

// ---------------------------------------------------------------------------
// Test: Dynamic Fee Transaction Validation
// ---------------------------------------------------------------------------

// TestPoolDynamicFeeValidation verifies EIP-1559 specific validation rules.
func TestPoolDynamicFeeValidation(t *testing.T) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := New(DefaultConfig(), state)

	// tipCap > feeCap should fail.
	txBad := makeDynamicTx(testSender, 0, 200, 100, 21000)
	err := pool.AddLocal(txBad)
	if err != ErrFeeCapBelowTip {
		t.Errorf("expected ErrFeeCapBelowTip, got: %v", err)
	}

	// tipCap == feeCap should succeed.
	txEqual := makeDynamicTx(testSender, 0, 100, 100, 21000)
	if err := pool.AddLocal(txEqual); err != nil {
		t.Errorf("expected success for tipCap==feeCap, got: %v", err)
	}

	// tipCap < feeCap should succeed.
	txOk := makeDynamicTx(testSender, 1, 50, 200, 21000)
	if err := pool.AddLocal(txOk); err != nil {
		t.Errorf("expected success for tipCap<feeCap, got: %v", err)
	}
}
