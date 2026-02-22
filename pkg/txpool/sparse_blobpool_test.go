package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// makeSparseBlob creates a blob transaction with versioned hashes (0x01 prefix).
func makeSparseBlob(nonce uint64, blobFeeCap, gasFeeCap, gasTipCap int64, blobCount int, from types.Address) *types.Transaction {
	hashes := make([]types.Hash, blobCount)
	for i := range hashes {
		h := types.Hash{}
		h[0] = VersionedHashPrefix // 0x01 versioned hash prefix
		h[1] = byte(nonce)
		h[2] = byte(i + 1)
		hashes[i] = h
	}
	to := types.BytesToAddress([]byte{0x42})
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
	tx.SetSender(from)
	return tx
}

func TestSparseBlobPool_AddAndGet(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
	addr := types.BytesToAddress([]byte{0xA1})
	tx := makeSparseBlob(0, 100, 1000, 50, 2, addr)

	if err := pool.AddBlobTx(tx); err != nil {
		t.Fatalf("AddBlobTx: %v", err)
	}

	got, ok := pool.GetBlobTx(tx.Hash())
	if !ok {
		t.Fatal("GetBlobTx returned false")
	}
	if got.Hash() != tx.Hash() {
		t.Errorf("hash mismatch: got %s, want %s", got.Hash().Hex(), tx.Hash().Hex())
	}

	if pool.TxCount() != 1 {
		t.Errorf("TxCount = %d, want 1", pool.TxCount())
	}
	if pool.BlobCount() != 2 {
		t.Errorf("BlobCount = %d, want 2", pool.BlobCount())
	}
}

func TestSparseBlobPool_RejectNonBlob(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
	to := types.BytesToAddress([]byte{0x01})
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})

	err := pool.AddBlobTx(legacyTx)
	if err != ErrSparseNotBlobTx {
		t.Errorf("expected ErrSparseNotBlobTx, got %v", err)
	}
}

func TestSparseBlobPool_RejectDuplicate(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
	addr := types.BytesToAddress([]byte{0xA2})
	tx := makeSparseBlob(0, 100, 1000, 50, 1, addr)

	pool.AddBlobTx(tx)
	err := pool.AddBlobTx(tx)
	if err != ErrSparseBlobDuplicate {
		t.Errorf("expected ErrSparseBlobDuplicate, got %v", err)
	}
}

func TestSparseBlobPool_RejectMissingHashes(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
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
	tx.SetSender(types.BytesToAddress([]byte{0xA3}))

	err := pool.AddBlobTx(tx)
	if err != ErrSparseBlobMissing {
		t.Errorf("expected ErrSparseBlobMissing, got %v", err)
	}
}

func TestSparseBlobPool_Remove(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
	addr := types.BytesToAddress([]byte{0xA4})
	tx := makeSparseBlob(0, 100, 1000, 50, 3, addr)

	pool.AddBlobTx(tx)

	ok := pool.RemoveBlobTx(tx.Hash())
	if !ok {
		t.Fatal("RemoveBlobTx returned false")
	}
	if pool.TxCount() != 0 {
		t.Errorf("TxCount = %d, want 0", pool.TxCount())
	}
	if pool.BlobCount() != 0 {
		t.Errorf("BlobCount = %d, want 0 after remove", pool.BlobCount())
	}

	// Remove non-existent should return false.
	if pool.RemoveBlobTx(tx.Hash()) {
		t.Error("removing non-existent tx should return false")
	}
}

func TestSparseBlobPool_PendingBlobTxsSorted(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())

	addr1 := types.BytesToAddress([]byte{0xB1})
	addr2 := types.BytesToAddress([]byte{0xB2})
	addr3 := types.BytesToAddress([]byte{0xB3})

	tx1 := makeSparseBlob(0, 100, 1000, 50, 1, addr1)
	tx2 := makeSparseBlob(0, 300, 1000, 50, 1, addr2)
	tx3 := makeSparseBlob(0, 200, 1000, 50, 1, addr3)

	pool.AddBlobTx(tx1)
	pool.AddBlobTx(tx2)
	pool.AddBlobTx(tx3)

	pending := pool.PendingBlobTxs()
	if len(pending) != 3 {
		t.Fatalf("PendingBlobTxs returned %d, want 3", len(pending))
	}

	// Should be sorted descending by blob fee cap: 300, 200, 100.
	wantFees := []int64{300, 200, 100}
	for i, tx := range pending {
		gotFee := tx.BlobGasFeeCap().Int64()
		if gotFee != wantFees[i] {
			t.Errorf("pending[%d] blob fee cap = %d, want %d", i, gotFee, wantFees[i])
		}
	}
}

func TestSparseBlobPool_AccountLimit(t *testing.T) {
	config := DefaultSparseBlobPoolConfig()
	config.MaxPerAccount = 2
	pool := NewSparseBlobPool(config)

	addr := types.BytesToAddress([]byte{0xC1})
	tx1 := makeSparseBlob(0, 100, 1000, 50, 1, addr)
	tx2 := makeSparseBlob(1, 100, 1000, 50, 1, addr)
	tx3 := makeSparseBlob(2, 100, 1000, 50, 1, addr)

	pool.AddBlobTx(tx1)
	pool.AddBlobTx(tx2)

	err := pool.AddBlobTx(tx3)
	if err != ErrSparseAccountLimit {
		t.Errorf("expected ErrSparseAccountLimit, got %v", err)
	}
}

func TestSparseBlobPool_PruneExpired(t *testing.T) {
	config := DefaultSparseBlobPoolConfig()
	config.ExpirySlots = 10
	pool := NewSparseBlobPool(config)

	addr1 := types.BytesToAddress([]byte{0xD1})
	addr2 := types.BytesToAddress([]byte{0xD2})

	// Insert at slot 0.
	pool.SetCurrentSlot(0)
	tx1 := makeSparseBlob(0, 100, 1000, 50, 1, addr1)
	pool.AddBlobTx(tx1)

	// Insert at slot 5.
	pool.SetCurrentSlot(5)
	tx2 := makeSparseBlob(0, 200, 1000, 50, 1, addr2)
	pool.AddBlobTx(tx2)

	// Prune at slot 11: cutoff = 11 - 10 = 1, so tx1 (inserted at 0) expires.
	pool.PruneExpired(11)

	if pool.TxCount() != 1 {
		t.Errorf("TxCount = %d, want 1 after prune", pool.TxCount())
	}
	_, ok := pool.GetBlobTx(tx1.Hash())
	if ok {
		t.Error("tx1 should have been pruned (expired)")
	}
	_, ok = pool.GetBlobTx(tx2.Hash())
	if !ok {
		t.Error("tx2 should still be in pool")
	}
}

func TestSparseBlobPool_SpaceUsed(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
	addr := types.BytesToAddress([]byte{0xE1})

	// 2 blobs, estimated at 128KB each = 256KB.
	tx := makeSparseBlob(0, 100, 1000, 50, 2, addr)
	pool.AddBlobTx(tx)

	used := pool.SpaceUsed()
	expected := uint64(2) * 128 * 1024
	if used != expected {
		t.Errorf("SpaceUsed = %d, want %d", used, expected)
	}

	// After removal, space should be 0.
	pool.RemoveBlobTx(tx.Hash())
	if pool.SpaceUsed() != 0 {
		t.Errorf("SpaceUsed after remove = %d, want 0", pool.SpaceUsed())
	}
}

func TestSparseBlobPool_EvictionOnFull(t *testing.T) {
	config := DefaultSparseBlobPoolConfig()
	config.MaxBlobs = 2
	config.MaxPerAccount = 10
	pool := NewSparseBlobPool(config)

	addr1 := types.BytesToAddress([]byte{0xF1})
	addr2 := types.BytesToAddress([]byte{0xF2})
	addr3 := types.BytesToAddress([]byte{0xF3})

	tx1 := makeSparseBlob(0, 100, 1000, 50, 1, addr1)
	tx2 := makeSparseBlob(0, 200, 1000, 50, 1, addr2)
	pool.AddBlobTx(tx1)
	pool.AddBlobTx(tx2)

	// Pool is full. Adding a higher-fee tx should evict the cheapest.
	tx3 := makeSparseBlob(0, 300, 1000, 50, 1, addr3)
	err := pool.AddBlobTx(tx3)
	if err != nil {
		t.Fatalf("expected eviction, got: %v", err)
	}

	if pool.TxCount() != 2 {
		t.Errorf("TxCount = %d, want 2", pool.TxCount())
	}
	if _, ok := pool.GetBlobTx(tx1.Hash()); ok {
		t.Error("tx1 should have been evicted (lowest fee)")
	}
	if _, ok := pool.GetBlobTx(tx3.Hash()); !ok {
		t.Error("tx3 should be in pool")
	}
}

func TestSparseBlobPool_RejectCheaperWhenFull(t *testing.T) {
	config := DefaultSparseBlobPoolConfig()
	config.MaxBlobs = 2
	config.MaxPerAccount = 10
	pool := NewSparseBlobPool(config)

	addr1 := types.BytesToAddress([]byte{0xF4})
	addr2 := types.BytesToAddress([]byte{0xF5})
	addr3 := types.BytesToAddress([]byte{0xF6})

	tx1 := makeSparseBlob(0, 200, 1000, 50, 1, addr1)
	tx2 := makeSparseBlob(0, 300, 1000, 50, 1, addr2)
	pool.AddBlobTx(tx1)
	pool.AddBlobTx(tx2)

	// Pool is full. Adding a cheaper tx should fail.
	tx3 := makeSparseBlob(0, 100, 1000, 50, 1, addr3)
	err := pool.AddBlobTx(tx3)
	if err != ErrSparseBlobPoolFull {
		t.Errorf("expected ErrSparseBlobPoolFull, got %v", err)
	}
}

func TestSparseBlobPool_GetEntry(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())
	addr := types.BytesToAddress([]byte{0xF7})
	tx := makeSparseBlob(5, 150, 1000, 50, 3, addr)

	pool.AddBlobTx(tx)

	entry, ok := pool.GetEntry(tx.Hash())
	if !ok {
		t.Fatal("GetEntry returned false")
	}
	if entry.Nonce != 5 {
		t.Errorf("Nonce = %d, want 5", entry.Nonce)
	}
	if entry.BlobCount != 3 {
		t.Errorf("BlobCount = %d, want 3", entry.BlobCount)
	}
	if entry.BlobFeeCap.Int64() != 150 {
		t.Errorf("BlobFeeCap = %d, want 150", entry.BlobFeeCap.Int64())
	}
	if len(entry.VersionedHashes) != 3 {
		t.Errorf("VersionedHashes len = %d, want 3", len(entry.VersionedHashes))
	}
	// All versioned hashes should have the 0x01 prefix.
	for i, vh := range entry.VersionedHashes {
		if vh[0] != VersionedHashPrefix {
			t.Errorf("VersionedHashes[%d] prefix = 0x%02x, want 0x01", i, vh[0])
		}
	}
}

func TestSparseBlobPool_InvalidVersionedHash(t *testing.T) {
	pool := NewSparseBlobPool(DefaultSparseBlobPoolConfig())

	// Create a blob tx with an invalid versioned hash prefix (0x00 instead of 0x01).
	hashes := []types.Hash{{0x00, 0x01, 0x02}}
	to := types.BytesToAddress([]byte{0x42})
	tx := types.NewTransaction(&types.BlobTx{
		ChainID:    big.NewInt(1),
		Nonce:      0,
		GasTipCap:  big.NewInt(50),
		GasFeeCap:  big.NewInt(1000),
		Gas:        21000,
		To:         to,
		Value:      big.NewInt(0),
		BlobFeeCap: big.NewInt(100),
		BlobHashes: hashes,
	})
	tx.SetSender(types.BytesToAddress([]byte{0xFA}))

	err := pool.AddBlobTx(tx)
	if err != ErrSparseInvalidVersion {
		t.Errorf("expected ErrSparseInvalidVersion, got %v", err)
	}
}

func TestSparseBlobPool_PruneExpiredNoop(t *testing.T) {
	config := DefaultSparseBlobPoolConfig()
	config.ExpirySlots = 100
	pool := NewSparseBlobPool(config)

	addr := types.BytesToAddress([]byte{0xFB})
	tx := makeSparseBlob(0, 100, 1000, 50, 1, addr)
	pool.AddBlobTx(tx)

	// Prune at slot 5 with expiry 100: no tx should expire since cutoff would underflow.
	pool.PruneExpired(5)

	if pool.TxCount() != 1 {
		t.Errorf("TxCount = %d, want 1 (no expiry)", pool.TxCount())
	}
}
