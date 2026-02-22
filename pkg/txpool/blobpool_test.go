package txpool

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
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

// EIP-8070 sparse blobpool tests.

func TestBlobPool_AddBlobTxWithSidecar(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 2)

	sidecar := &BlobSidecar{
		TxHash:      tx.Hash(),
		BlobHashes:  tx.BlobHashes(),
		BlobData:    [][]byte{make([]byte, 1024), make([]byte, 1024)},
		Commitments: [][]byte{make([]byte, 48), make([]byte, 48)},
		Proofs:      [][]byte{make([]byte, 48), make([]byte, 48)},
		CellIndices: []uint64{0, 1, 2, 3},
	}

	if err := pool.AddBlobTx(tx, sidecar); err != nil {
		t.Fatalf("AddBlobTx: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1", pool.Count())
	}

	sc, err := pool.GetBlobSidecar(tx.Hash())
	if err != nil {
		t.Fatalf("GetBlobSidecar: %v", err)
	}
	if sc.TxHash != tx.Hash() {
		t.Errorf("sidecar TxHash mismatch")
	}
}

func TestBlobPool_AddBlobTxNilSidecar(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)

	if err := pool.AddBlobTx(tx, nil); err != nil {
		t.Fatalf("AddBlobTx with nil sidecar: %v", err)
	}

	if pool.Count() != 1 {
		t.Errorf("Count = %d, want 1", pool.Count())
	}

	// Sidecar should not be found.
	_, err := pool.GetBlobSidecar(tx.Hash())
	if err != ErrBlobSidecarNotFound {
		t.Errorf("expected ErrBlobSidecarNotFound, got %v", err)
	}
}

func TestBlobPool_RemoveBlobTx(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)
	sidecar := &BlobSidecar{
		TxHash:      tx.Hash(),
		BlobHashes:  tx.BlobHashes(),
		BlobData:    [][]byte{make([]byte, 512)},
		CellIndices: []uint64{0, 1},
	}

	pool.AddBlobTx(tx, sidecar)
	pool.RemoveBlobTx(tx.Hash())

	if pool.Count() != 0 {
		t.Errorf("Count = %d, want 0", pool.Count())
	}
	_, err := pool.GetBlobSidecar(tx.Hash())
	if err != ErrBlobSidecarNotFound {
		t.Errorf("expected ErrBlobSidecarNotFound after remove, got %v", err)
	}
}

func TestBlobPool_CustodyFilter(t *testing.T) {
	cfg := CustodyConfig{
		CustodyColumns:  []uint64{0, 1, 2, 3},
		CellSampleCount: DefaultCellSampleCount,
	}

	// Cells 0-3 map to columns 0-3, should be kept.
	// Cells 4-7 map to columns 4-7, should be filtered out.
	indices := []uint64{0, 1, 2, 3, 4, 5, 6, 7}
	kept := cfg.CustodyFilter(indices)

	if len(kept) != 4 {
		t.Fatalf("CustodyFilter kept %d cells, want 4", len(kept))
	}
	for _, idx := range kept {
		if idx > 3 {
			t.Errorf("unexpected custody cell index %d", idx)
		}
	}
}

func TestBlobPool_CustodyFilterWraparound(t *testing.T) {
	cfg := CustodyConfig{
		CustodyColumns:  []uint64{0, 64},
		CellSampleCount: 8,
	}

	// Cell 0 -> col 0 (custodied), Cell 64 -> col 64 (custodied),
	// Cell 1 -> col 1 (not custodied), Cell 128 -> col 0 (custodied, wraps).
	indices := []uint64{0, 1, 64, 128}
	kept := cfg.CustodyFilter(indices)

	if len(kept) != 3 {
		t.Fatalf("CustodyFilter kept %d cells, want 3", len(kept))
	}
}

func TestBlobPool_AddBlobTxWithCustodyFiltering(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.Custody = CustodyConfig{
		CustodyColumns:  []uint64{0, 1},
		CellSampleCount: 8,
	}
	pool := NewBlobPool(config, nil)

	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)
	sidecar := &BlobSidecar{
		TxHash:      tx.Hash(),
		BlobHashes:  tx.BlobHashes(),
		BlobData:    [][]byte{[]byte("cell0"), []byte("cell1"), []byte("cell5"), []byte("cell10")},
		Commitments: [][]byte{[]byte("c0"), []byte("c1"), []byte("c5"), []byte("c10")},
		Proofs:      [][]byte{[]byte("p0"), []byte("p1"), []byte("p5"), []byte("p10")},
		CellIndices: []uint64{0, 1, 5, 10},
	}

	if err := pool.AddBlobTx(tx, sidecar); err != nil {
		t.Fatalf("AddBlobTx: %v", err)
	}

	sc, err := pool.GetBlobSidecar(tx.Hash())
	if err != nil {
		t.Fatalf("GetBlobSidecar: %v", err)
	}

	// Only cells 0 and 1 should be kept (columns 0, 1).
	if len(sc.CellIndices) != 2 {
		t.Errorf("filtered sidecar has %d cells, want 2", len(sc.CellIndices))
	}
}

func TestBlobPool_PruneSidecars(t *testing.T) {
	config := DefaultBlobPoolConfig()
	config.Datacap = 500 // very small datacap
	pool := NewBlobPool(config, nil)

	addr1 := types.BytesToAddress([]byte{0x01})
	addr2 := types.BytesToAddress([]byte{0x02})

	tx1 := makeBlobPoolTxFrom(0, 100, addr1)
	tx2 := makeBlobPoolTxFrom(0, 200, addr2)

	sc1 := &BlobSidecar{
		TxHash:      tx1.Hash(),
		BlobData:    [][]byte{make([]byte, 300)},
		CellIndices: []uint64{0},
	}
	sc2 := &BlobSidecar{
		TxHash:      tx2.Hash(),
		BlobData:    [][]byte{make([]byte, 300)},
		CellIndices: []uint64{0},
	}

	pool.AddBlobTx(tx1, sc1)
	pool.AddBlobTx(tx2, sc2)

	// Total data is 600 > datacap 500. Pruning should evict.
	pruned := pool.PruneSidecars()
	if pruned == 0 {
		t.Error("expected at least one sidecar to be pruned")
	}
}

func TestBlobPool_EvictHeapOrdering(t *testing.T) {
	h := newBlobEvictHeap()

	h.push(types.BytesToHash([]byte{1}), big.NewInt(100))
	h.push(types.BytesToHash([]byte{2}), big.NewInt(50))
	h.push(types.BytesToHash([]byte{3}), big.NewInt(200))

	// Peek should return the lowest tip.
	item := h.peek()
	if item == nil {
		t.Fatal("peek returned nil")
	}
	if item.effectiveTip.Int64() != 50 {
		t.Errorf("peek tip = %d, want 50", item.effectiveTip.Int64())
	}

	// Remove the min and check next.
	h.remove(types.BytesToHash([]byte{2}))
	item = h.peek()
	if item.effectiveTip.Int64() != 100 {
		t.Errorf("after remove, peek tip = %d, want 100", item.effectiveTip.Int64())
	}
}

func TestBlobPool_DataUsed(t *testing.T) {
	pool := NewBlobPool(DefaultBlobPoolConfig(), nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)

	sidecar := &BlobSidecar{
		TxHash:      tx.Hash(),
		BlobData:    [][]byte{make([]byte, 1024)},
		Commitments: [][]byte{make([]byte, 48)},
		Proofs:      [][]byte{make([]byte, 48)},
		CellIndices: []uint64{0},
	}

	pool.AddBlobTx(tx, sidecar)

	used := pool.DataUsed()
	if used != 1024+48+48 {
		t.Errorf("DataUsed = %d, want %d", used, 1024+48+48)
	}

	pool.RemoveBlobTx(tx.Hash())
	if pool.DataUsed() != 0 {
		t.Errorf("DataUsed after remove = %d, want 0", pool.DataUsed())
	}
}

func TestBlobPool_JournalPersistence(t *testing.T) {
	dir := t.TempDir()
	config := DefaultBlobPoolConfig()
	config.Datadir = dir
	config.Custody = CustodyConfig{
		CustodyColumns:  []uint64{0},
		CellSampleCount: 1,
	}

	// Create pool and add a transaction with sidecar.
	pool := NewBlobPool(config, nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)
	sidecar := &BlobSidecar{
		TxHash:      tx.Hash(),
		BlobHashes:  tx.BlobHashes(),
		BlobData:    [][]byte{[]byte("testdata")},
		Commitments: [][]byte{[]byte("commit")},
		Proofs:      [][]byte{[]byte("proof")},
		CellIndices: []uint64{0},
	}

	if err := pool.AddBlobTx(tx, sidecar); err != nil {
		t.Fatalf("AddBlobTx: %v", err)
	}
	pool.Close()

	// Create a new pool from the same directory - journal recovery.
	pool2 := NewBlobPool(config, nil)
	defer pool2.Close()

	// The sidecar should be recovered from the journal.
	sc, err := pool2.GetBlobSidecar(tx.Hash())
	if err != nil {
		t.Fatalf("recovered GetBlobSidecar: %v", err)
	}
	if sc.TxHash != tx.Hash() {
		t.Errorf("recovered sidecar TxHash mismatch")
	}
	if len(sc.BlobData) == 0 {
		t.Error("recovered sidecar has no blob data")
	}
}

func TestBlobPool_JournalRemoveRecovery(t *testing.T) {
	dir := t.TempDir()
	config := DefaultBlobPoolConfig()
	config.Datadir = dir

	pool := NewBlobPool(config, nil)
	tx := makeBlobPoolTx(0, 100, 1000, 100, 1)
	sidecar := &BlobSidecar{
		TxHash:      tx.Hash(),
		BlobData:    [][]byte{[]byte("data")},
		CellIndices: []uint64{0},
	}

	pool.AddBlobTx(tx, sidecar)
	pool.RemoveBlobTx(tx.Hash())
	pool.Close()

	// After recovery, the sidecar should not exist.
	pool2 := NewBlobPool(config, nil)
	defer pool2.Close()

	_, err := pool2.GetBlobSidecar(tx.Hash())
	if err != ErrBlobSidecarNotFound {
		t.Errorf("expected ErrBlobSidecarNotFound after journal recovery with remove, got %v", err)
	}
}

func TestBlobPool_DefaultCustodyConfig(t *testing.T) {
	cfg := DefaultCustodyConfig()
	if len(cfg.CustodyColumns) != DefaultCustodyColumns {
		t.Errorf("DefaultCustodyConfig columns = %d, want %d", len(cfg.CustodyColumns), DefaultCustodyColumns)
	}
	if cfg.CellSampleCount != DefaultCellSampleCount {
		t.Errorf("CellSampleCount = %d, want %d", cfg.CellSampleCount, DefaultCellSampleCount)
	}
	// Columns should be 0, 1, 2, 3.
	for i, col := range cfg.CustodyColumns {
		if col != uint64(i) {
			t.Errorf("column[%d] = %d, want %d", i, col, i)
		}
	}
}

func TestBlobPool_IsCustodyColumn(t *testing.T) {
	cfg := CustodyConfig{CustodyColumns: []uint64{0, 5, 10}}

	if !cfg.IsCustodyColumn(0) {
		t.Error("column 0 should be custodied")
	}
	if !cfg.IsCustodyColumn(5) {
		t.Error("column 5 should be custodied")
	}
	if cfg.IsCustodyColumn(3) {
		t.Error("column 3 should not be custodied")
	}
}

func TestBlobPool_BlobEffectiveTip(t *testing.T) {
	tx := makeBlobPoolTx(0, 200, 1000, 50, 1)

	// With no base fee, tip = tipCap.
	tip := blobEffectiveTip(tx, nil)
	if tip.Int64() != 50 {
		t.Errorf("tip with nil baseFee = %d, want 50", tip.Int64())
	}

	// With base fee 100, excess = 200 - 100 = 100, min(50, 100) = 50.
	tip = blobEffectiveTip(tx, big.NewInt(100))
	if tip.Int64() != 50 {
		t.Errorf("tip with baseFee=100 = %d, want 50", tip.Int64())
	}

	// With high base fee 190, excess = 200 - 190 = 10, min(50, 10) = 10.
	tip = blobEffectiveTip(tx, big.NewInt(190))
	if tip.Int64() != 10 {
		t.Errorf("tip with baseFee=190 = %d, want 10", tip.Int64())
	}
}

func TestBlobPool_SidecarSize(t *testing.T) {
	sc := &BlobSidecar{
		BlobData:    [][]byte{make([]byte, 100), make([]byte, 200)},
		Commitments: [][]byte{make([]byte, 48)},
		Proofs:      [][]byte{make([]byte, 48)},
	}
	got := sidecarSize(sc)
	if got != 396 {
		t.Errorf("sidecarSize = %d, want 396", got)
	}

	// Nil sidecar.
	if sidecarSize(nil) != 0 {
		t.Error("sidecarSize(nil) should be 0")
	}
}

func TestBlobPool_Close(t *testing.T) {
	dir := t.TempDir()
	config := DefaultBlobPoolConfig()
	config.Datadir = dir

	pool := NewBlobPool(config, nil)
	if err := pool.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Double close should be safe.
	if err := pool.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
}
