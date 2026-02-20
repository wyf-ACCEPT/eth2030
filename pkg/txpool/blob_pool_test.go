package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeBlobTxPool creates a BlobTxPool with a rich mock state for testing.
func makeBlobTxPool(capacity int) *BlobTxPool {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)

	config := DefaultBlobTxPoolConfig()
	if capacity > 0 {
		config.Capacity = capacity
	}
	return NewBlobTxPool(config, state)
}

// makeBlobTxForPool creates a blob transaction with the given parameters.
func makeBlobTxForPool(nonce uint64, blobFeeCap int64, blobCount int) *types.Transaction {
	to := types.BytesToAddress([]byte{0xbe, 0xef})
	hashes := make([]types.Hash, blobCount)
	for i := range hashes {
		hashes[i][0] = 0x01
		hashes[i][1] = byte(i + 1)
		hashes[i][2] = byte(nonce)
	}
	tx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      nonce,
		GasTipCap:  big.NewInt(1_000_000_000),
		GasFeeCap:  big.NewInt(10_000_000_000),
		Gas:        21000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(blobFeeCap),
		BlobHashes: hashes,
	})
	tx.SetSender(testSender)
	return tx
}

func TestBlobTxPoolAddAndGet(t *testing.T) {
	pool := makeBlobTxPool(0)
	tx := makeBlobTxForPool(0, 1000, 2)

	if err := pool.Add(tx); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if pool.Len() != 1 {
		t.Fatalf("expected 1, got %d", pool.Len())
	}

	got := pool.Get(tx.Hash())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Hash() != tx.Hash() {
		t.Fatalf("hash mismatch: got %x, want %x", got.Hash(), tx.Hash())
	}
}

func TestBlobTxPoolRejectNonBlobTx(t *testing.T) {
	pool := makeBlobTxPool(0)
	legacyTx := makeTx(0, 1000, 21000)

	err := pool.Add(legacyTx)
	if err != ErrBlobTxNotType3 {
		t.Fatalf("expected ErrBlobTxNotType3, got %v", err)
	}
}

func TestBlobTxPoolRejectDuplicate(t *testing.T) {
	pool := makeBlobTxPool(0)
	tx := makeBlobTxForPool(0, 1000, 1)

	if err := pool.Add(tx); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}
	err := pool.Add(tx)
	if err != ErrBlobTxDuplicate {
		t.Fatalf("expected ErrBlobTxDuplicate, got %v", err)
	}
}

func TestBlobTxPoolRejectNoHashes(t *testing.T) {
	pool := makeBlobTxPool(0)
	to := types.BytesToAddress([]byte{0xbe, 0xef})
	tx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(1_000_000_000),
		GasFeeCap:  big.NewInt(10_000_000_000),
		Gas:        21000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(1000),
		BlobHashes: nil,
	})
	tx.SetSender(testSender)

	err := pool.Add(tx)
	if err != ErrBlobTxNoHashes {
		t.Fatalf("expected ErrBlobTxNoHashes, got %v", err)
	}
}

func TestBlobTxPoolRemove(t *testing.T) {
	pool := makeBlobTxPool(0)
	tx := makeBlobTxForPool(0, 1000, 1)

	pool.Add(tx)
	pool.Remove(tx.Hash())

	if pool.Len() != 0 {
		t.Fatalf("expected 0 after remove, got %d", pool.Len())
	}
	if pool.Get(tx.Hash()) != nil {
		t.Fatal("Get should return nil after remove")
	}
}

func TestBlobTxPoolPending(t *testing.T) {
	pool := makeBlobTxPool(0)

	// Add three blob txs with different fee caps.
	tx1 := makeBlobTxForPool(0, 500, 1)
	tx2 := makeBlobTxForPool(1, 1000, 1)
	tx3 := makeBlobTxForPool(2, 750, 1)

	pool.Add(tx1)
	pool.Add(tx2)
	pool.Add(tx3)

	pending := pool.Pending()
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(pending))
	}

	// Should be sorted by blob fee cap descending.
	if pending[0].BlobGasFeeCap().Cmp(pending[1].BlobGasFeeCap()) < 0 {
		t.Fatal("pending list not sorted by blob fee cap descending")
	}
}

func TestBlobTxPoolCapacityEviction(t *testing.T) {
	pool := makeBlobTxPool(2) // capacity of 2

	tx1 := makeBlobTxForPool(0, 500, 1)
	tx2 := makeBlobTxForPool(1, 1000, 1)
	tx3 := makeBlobTxForPool(2, 1500, 1)

	pool.Add(tx1)
	pool.Add(tx2)

	// Adding tx3 should evict the cheapest (tx1).
	if err := pool.Add(tx3); err != nil {
		t.Fatalf("Add should succeed with eviction: %v", err)
	}
	if pool.Len() != 2 {
		t.Fatalf("expected 2 after eviction, got %d", pool.Len())
	}
	if pool.Get(tx1.Hash()) != nil {
		t.Fatal("cheapest tx should have been evicted")
	}
}

func TestBlobTxPoolPerAccountLimit(t *testing.T) {
	state := newMockState()
	state.balances[testSender] = new(big.Int).Set(richBalance)
	config := DefaultBlobTxPoolConfig()
	config.PerAccountMax = 2
	pool := NewBlobTxPool(config, state)

	pool.Add(makeBlobTxForPool(0, 1000, 1))
	pool.Add(makeBlobTxForPool(1, 1000, 1))

	err := pool.Add(makeBlobTxForPool(2, 1000, 1))
	if err != ErrBlobTxAccountMax {
		t.Fatalf("expected ErrBlobTxAccountMax, got %v", err)
	}
}

func TestBlobTxPoolTotalBlobGas(t *testing.T) {
	pool := makeBlobTxPool(0)

	// 2 blobs = 2 * BlobGasPerBlobUnit gas.
	tx := makeBlobTxForPool(0, 1000, 2)
	pool.Add(tx)

	totalGas := pool.TotalBlobGas()
	expected := uint64(2 * BlobGasPerBlobUnit)
	if totalGas != expected {
		t.Fatalf("expected blob gas %d, got %d", expected, totalGas)
	}
}

func TestBlobTxPoolBlobBaseFeeUpdate(t *testing.T) {
	pool := makeBlobTxPool(0)

	// Add a tx with low blob fee cap.
	tx := makeBlobTxForPool(0, 100, 1)
	pool.Add(tx)

	// Set excess blob gas that produces a higher base fee than 100.
	// e^(excess/3338477) > 100 requires excess > ln(100)*3338477 ≈ 15.4M.
	// Using 20M gives e^5.99 ≈ 400 > 100.
	pool.SetExcessBlobGas(20_000_000)

	// The tx should have been evicted.
	if pool.Len() != 0 {
		t.Fatalf("expected 0 after base fee increase evicts tx, got %d", pool.Len())
	}
}

func TestBlobTxPoolExcessBlobGasTracking(t *testing.T) {
	pool := makeBlobTxPool(0)

	pool.SetExcessBlobGas(500_000)
	if pool.ExcessBlobGas() != 500_000 {
		t.Fatalf("expected 500000, got %d", pool.ExcessBlobGas())
	}
}

func TestBlobTxPoolNonceTooLow(t *testing.T) {
	state := newMockState()
	state.nonces[testSender] = 5
	state.balances[testSender] = new(big.Int).Set(richBalance)
	pool := NewBlobTxPool(DefaultBlobTxPoolConfig(), state)

	tx := makeBlobTxForPool(3, 1000, 1) // nonce 3 < state nonce 5
	err := pool.Add(tx)
	if err != ErrBlobTxNonceLow {
		t.Fatalf("expected ErrBlobTxNonceLow, got %v", err)
	}
}

func TestBlobTxPoolReplacement(t *testing.T) {
	pool := makeBlobTxPool(0)

	tx1 := makeBlobTxForPool(0, 1000, 1)
	pool.Add(tx1)

	// Replacement with >= 10% bump should succeed.
	tx2 := makeBlobTxForPool(0, 1100, 1) // 10% bump
	if err := pool.Add(tx2); err != nil {
		t.Fatalf("replacement with bump should succeed: %v", err)
	}
	if pool.Len() != 1 {
		t.Fatalf("expected 1 after replacement, got %d", pool.Len())
	}

	// Old tx should be gone.
	if pool.Get(tx1.Hash()) != nil {
		t.Fatal("old tx should have been replaced")
	}
}

func TestBlobTxPoolReplacementTooLow(t *testing.T) {
	pool := makeBlobTxPool(0)

	tx1 := makeBlobTxForPool(0, 1000, 1)
	pool.Add(tx1)

	// Replacement without sufficient bump should fail.
	tx2 := makeBlobTxForPool(0, 1050, 1) // only 5% bump
	err := pool.Add(tx2)
	if err != ErrBlobTxReplaceTooLow {
		t.Fatalf("expected ErrBlobTxReplaceTooLow, got %v", err)
	}
}

func TestCalcBlobBaseFee(t *testing.T) {
	// Zero excess should yield minimum.
	fee := CalcBlobBaseFee(0)
	if fee.Cmp(big.NewInt(MinBlobBaseFee)) != 0 {
		t.Fatalf("expected %d for zero excess, got %s", MinBlobBaseFee, fee)
	}

	// Non-zero excess should yield > minimum.
	// Need excess large enough that e^(excess/3338477) > 1 after integer truncation.
	// e^(excess/3338477) > 1 requires excess/3338477 > 0, but floor truncation
	// means we need e^(excess/3338477) >= 2, so excess > ln(2)*3338477 ≈ 2.3M.
	fee = CalcBlobBaseFee(5_000_000)
	if fee.Cmp(big.NewInt(MinBlobBaseFee)) <= 0 {
		t.Fatalf("expected > %d for non-zero excess, got %s", MinBlobBaseFee, fee)
	}

	// Higher excess should yield higher fee.
	feeLow := CalcBlobBaseFee(5_000_000)
	feeHigh := CalcBlobBaseFee(10_000_000)
	if feeHigh.Cmp(feeLow) <= 0 {
		t.Fatalf("higher excess should yield higher fee: %s vs %s", feeLow, feeHigh)
	}
}

func TestCalcExcessBlobGas(t *testing.T) {
	// Below target: excess should be 0.
	excess := CalcExcessBlobGas(0, TargetBlobGasPerBlock-1)
	if excess != 0 {
		t.Fatalf("expected 0, got %d", excess)
	}

	// At target: excess should be parent excess.
	excess = CalcExcessBlobGas(100_000, TargetBlobGasPerBlock)
	if excess != 100_000 {
		t.Fatalf("expected 100000, got %d", excess)
	}

	// Above target: excess should increase.
	used := uint64(MaxBlobGasPerBlock) // max blobs used
	excess = CalcExcessBlobGas(0, used)
	expected := used - TargetBlobGasPerBlock
	if excess != expected {
		t.Fatalf("expected %d, got %d", expected, excess)
	}
}

func TestBlobTxPoolBlobBaseFeeGetter(t *testing.T) {
	pool := makeBlobTxPool(0)

	// Default should be MinBlobBaseFee.
	fee := pool.BlobBaseFee()
	if fee.Cmp(big.NewInt(MinBlobBaseFee)) != 0 {
		t.Fatalf("expected min blob base fee, got %s", fee)
	}

	// After setting excess, fee should update.
	// Need large enough excess for fee > 1 after integer truncation.
	pool.SetExcessBlobGas(5_000_000)
	fee2 := pool.BlobBaseFee()
	if fee2.Cmp(big.NewInt(MinBlobBaseFee)) <= 0 {
		t.Fatalf("expected fee > min after setting excess, got %s", fee2)
	}
}

func TestBlobTxPoolGasExceeded(t *testing.T) {
	pool := makeBlobTxPool(0)

	// Create tx with too many blobs (exceeds MaxBlobGasPerBlock).
	// MaxBlobsPerBlock = 6, so 7 blobs should exceed.
	tx := makeBlobTxForPool(0, 1000, 7)
	err := pool.Add(tx)
	if err != ErrBlobTxGasExceeded {
		t.Fatalf("expected ErrBlobTxGasExceeded, got %v", err)
	}
}
