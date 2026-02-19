package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func makeBlobPoolTx(nonce uint64, blobFeeCap, gasFeeCap, gasTipCap int64, blobCount int) *types.Transaction {
	hashes := make([]types.Hash, blobCount)
	for i := range hashes {
		hashes[i] = types.BytesToHash([]byte{byte(nonce), byte(i + 1)})
	}
	to := types.BytesToAddress([]byte{0x01})
	tx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      nonce,
		GasTipCap:  big.NewInt(gasTipCap),
		GasFeeCap:  big.NewInt(gasFeeCap),
		Gas:        21000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(blobFeeCap),
		BlobHashes: hashes,
	})
	addr := types.BytesToAddress([]byte{0xAA})
	tx.SetSender(addr)
	return tx
}

func makeBlobPoolTxFrom(nonce uint64, blobFeeCap int64, from types.Address) *types.Transaction {
	hashes := []types.Hash{types.BytesToHash([]byte{byte(nonce), 0x01})}
	to := types.BytesToAddress([]byte{0x01})
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
	tx.SetSender(from)
	return tx
}

func TestBlobPool_AddAndGet(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 2)

	if err := pool.Add(tx); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1", pool.Count())
	}

	got := pool.Get(tx.Hash())
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Hash() != tx.Hash() {
		t.Errorf("got hash %s, want %s", got.Hash().Hex(), tx.Hash().Hex())
	}
}

func TestBlobPool_Has(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)

	if pool.Has(tx.Hash()) {
		t.Error("Has returned true before Add")
	}

	pool.Add(tx)

	if !pool.Has(tx.Hash()) {
		t.Error("Has returned false after Add")
	}
}

func TestBlobPool_Remove(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)

	pool.Add(tx)
	pool.Remove(tx.Hash())

	if pool.Count() != 0 {
		t.Errorf("Count = %d, want 0 after Remove", pool.Count())
	}
	if pool.Has(tx.Hash()) {
		t.Error("Has returned true after Remove")
	}
}

func TestBlobPool_RejectNonBlob(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)

	to := types.BytesToAddress([]byte{0x01})
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})

	err := pool.Add(legacyTx)
	if err != ErrNotBlobTx {
		t.Errorf("expected ErrNotBlobTx, got %v", err)
	}
}

func TestBlobPool_RejectDuplicate(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)

	pool.Add(tx)
	err := pool.Add(tx)
	if err != ErrBlobAlreadyKnown {
		t.Errorf("expected ErrBlobAlreadyKnown, got %v", err)
	}
}

func TestBlobPool_RejectMissingHashes(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)

	to := types.BytesToAddress([]byte{0x01})
	tx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(100),
		GasFeeCap:  big.NewInt(1000),
		Gas:        21000,
		To:         to,
		BlobFeeCap: big.NewInt(100),
		BlobHashes: nil,
	})
	addr := types.BytesToAddress([]byte{0xAA})
	tx.SetSender(addr)

	err := pool.Add(tx)
	if err != ErrBlobMissingHashes {
		t.Errorf("expected ErrBlobMissingHashes, got %v", err)
	}
}

func TestBlobPool_AccountLimit(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.MaxBlobsPerAccount = 2
	pool := NewBlobPool(config, nil)

	addr := types.BytesToAddress([]byte{0xBB})
	tx1 := makeBlobPoolTxFrom(0, 100, addr)
	tx2 := makeBlobPoolTxFrom(1, 100, addr)
	tx3 := makeBlobPoolTxFrom(2, 100, addr)

	pool.Add(tx1)
	pool.Add(tx2)

	err := pool.Add(tx3)
	if err != ErrBlobAccountLimit {
		t.Errorf("expected ErrBlobAccountLimit, got %v", err)
	}
}

func TestBlobPool_PoolFull_Eviction(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.MaxBlobs = 2
	config.MaxBlobsPerAccount = 10
	pool := NewBlobPool(config, nil)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	tx1 := makeBlobPoolTxFrom(0, 100, addr1)
	tx2 := makeBlobPoolTxFrom(0, 200, addr2)
	pool.Add(tx1)
	pool.Add(tx2)

	// Add a higher-priced tx - should evict the cheapest (tx1).
	tx3 := makeBlobPoolTxFrom(0, 300, addr3)
	err := pool.Add(tx3)
	if err != nil {
		t.Fatalf("expected eviction to succeed, got: %v", err)
	}

	if pool.Count() != 2 {
		t.Errorf("Count = %d, want 2", pool.Count())
	}
	if pool.Has(tx1.Hash()) {
		t.Error("tx1 should have been evicted (cheapest)")
	}
	if !pool.Has(tx2.Hash()) {
		t.Error("tx2 should still be in pool")
	}
	if !pool.Has(tx3.Hash()) {
		t.Error("tx3 should be in pool")
	}
}

func TestBlobPool_PoolFull_RejectCheaper(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.MaxBlobs = 2
	config.MaxBlobsPerAccount = 10
	pool := NewBlobPool(config, nil)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	tx1 := makeBlobPoolTxFrom(0, 200, addr1)
	tx2 := makeBlobPoolTxFrom(0, 300, addr2)
	pool.Add(tx1)
	pool.Add(tx2)

	// Add a cheaper tx - should be rejected since it can't evict anything.
	tx3 := makeBlobPoolTxFrom(0, 100, addr3)
	err := pool.Add(tx3)
	if err != ErrBlobPoolFull {
		t.Errorf("expected ErrBlobPoolFull, got %v", err)
	}
}

func TestBlobPool_Replacement(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	addr := types.BytesToAddress([]byte{0xCC})

	// Add a tx with blob fee cap 100.
	tx1 := makeBlobPoolTxFrom(0, 100, addr)
	pool.Add(tx1)

	// Replace with a tx with blob fee cap 111 (>= 110% of 100).
	tx2 := makeBlobPoolTxFrom(0, 111, addr)
	err := pool.Add(tx2)
	if err != nil {
		t.Fatalf("replacement should succeed: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1 after replacement", pool.Count())
	}
	if pool.Has(tx1.Hash()) {
		t.Error("old tx should be replaced")
	}
	if !pool.Has(tx2.Hash()) {
		t.Error("new tx should be in pool")
	}
}

func TestBlobPool_ReplacementTooLow(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	addr := types.BytesToAddress([]byte{0xDD})

	tx1 := makeBlobPoolTxFrom(0, 100, addr)
	pool.Add(tx1)

	// Try replacing with only 105% bump (< 110% required).
	tx2 := makeBlobPoolTxFrom(0, 105, addr)
	err := pool.Add(tx2)
	if err != ErrBlobReplaceTooLow {
		t.Errorf("expected ErrBlobReplaceTooLow, got %v", err)
	}
}

func TestBlobPool_BlobBaseFeeEviction(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	tx1 := makeBlobPoolTxFrom(0, 50, addr1)
	tx2 := makeBlobPoolTxFrom(0, 200, addr2)
	pool.Add(tx1)
	pool.Add(tx2)

	// Raise blob base fee above tx1's fee cap.
	pool.SetBlobBaseFee(big.NewInt(100))

	if pool.Has(tx1.Hash()) {
		t.Error("tx1 should be evicted (blob fee cap 50 < base fee 100)")
	}
	if !pool.Has(tx2.Hash()) {
		t.Error("tx2 should remain (blob fee cap 200 >= base fee 100)")
	}
}

func TestBlobPool_BlobBaseFeeReject(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	pool.SetBlobBaseFee(big.NewInt(200))

	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)

	err := pool.Add(tx)
	if err != ErrBlobFeeCapTooLow {
		t.Errorf("expected ErrBlobFeeCapTooLow, got %v", err)
	}
}

func TestBlobPool_GetMetadata(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(5, 100, 1000, 100, 3)

	pool.Add(tx)

	meta := pool.GetMetadata(tx.Hash())
	if meta == nil {
		t.Fatal("GetMetadata returned nil")
	}
	if meta.TxHash != tx.Hash() {
		t.Errorf("TxHash = %s, want %s", meta.TxHash.Hex(), tx.Hash().Hex())
	}
	if meta.BlobCount != 3 {
		t.Errorf("BlobCount = %d, want 3", meta.BlobCount)
	}
	if meta.Nonce != 5 {
		t.Errorf("Nonce = %d, want 5", meta.Nonce)
	}
	if meta.BlobGas != 3*BlobGasPerBlob {
		t.Errorf("BlobGas = %d, want %d", meta.BlobGas, 3*BlobGasPerBlob)
	}
}

func TestBlobPool_PendingSorted(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.MaxBlobsPerAccount = 10
	pool := NewBlobPool(config, nil)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})
	addr3 := types.BytesToAddress([]byte{0x03})

	tx1 := makeBlobPoolTxFrom(0, 100, addr1)
	tx2 := makeBlobPoolTxFrom(0, 300, addr2)
	tx3 := makeBlobPoolTxFrom(0, 200, addr3)
	pool.Add(tx1)
	pool.Add(tx2)
	pool.Add(tx3)

	sorted := pool.PendingSorted()
	if len(sorted) != 3 {
		t.Fatalf("PendingSorted returned %d txs, want 3", len(sorted))
	}

	// Should be sorted by blob fee cap descending: 300, 200, 100.
	prices := []int64{300, 200, 100}
	for i, tx := range sorted {
		got := tx.BlobGasFeeCap().Int64()
		if got != prices[i] {
			t.Errorf("sorted[%d] blob fee cap = %d, want %d", i, got, prices[i])
		}
	}
}

func TestBlobPool_Pending(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	addr := types.BytesToAddress([]byte{0xEE})

	tx1 := makeBlobPoolTxFrom(0, 100, addr)
	tx2 := makeBlobPoolTxFrom(1, 200, addr)
	pool.Add(tx1)
	pool.Add(tx2)

	pending := pool.Pending()
	if len(pending) != 1 {
		t.Fatalf("Pending has %d senders, want 1", len(pending))
	}
	if txs := pending[addr]; len(txs) != 2 {
		t.Errorf("sender has %d txs, want 2", len(txs))
	}
}

func TestBlobPool_NonceTooLow(t *testing.T) {
	state := newMockState()
	addr := types.BytesToAddress([]byte{0xFF})
	state.nonces[addr] = 5

	pool := NewBlobPool(DefaultBlobPoolConfig(), state)

	tx := makeBlobPoolTxFrom(3, 100, addr)
	err := pool.Add(tx)
	if err != ErrBlobNonceTooLow {
		t.Errorf("expected ErrBlobNonceTooLow, got %v", err)
	}
}
