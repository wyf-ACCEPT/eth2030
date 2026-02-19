package das

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewTeradataManager(t *testing.T) {
	cfg := DefaultTeradataConfig()
	m := NewTeradataManager(cfg)
	if m == nil {
		t.Fatal("NewTeradataManager returned nil")
	}
	if m.config.MaxDataSize != cfg.MaxDataSize {
		t.Errorf("config mismatch: got MaxDataSize=%d, want %d", m.config.MaxDataSize, cfg.MaxDataSize)
	}
}

func TestStoreAndRetrieveL2Data(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	data := []byte("hello teradata L2 data availability")

	receipt, err := m.StoreL2Data(42, data)
	if err != nil {
		t.Fatalf("StoreL2Data failed: %v", err)
	}
	if receipt == nil {
		t.Fatal("receipt is nil")
	}
	if receipt.L2ChainID != 42 {
		t.Errorf("got L2ChainID=%d, want 42", receipt.L2ChainID)
	}
	if receipt.Size != uint64(len(data)) {
		t.Errorf("got Size=%d, want %d", receipt.Size, len(data))
	}
	if receipt.CommitmentHash.IsZero() {
		t.Error("commitment hash is zero")
	}
	if receipt.Slot == 0 {
		t.Error("slot is zero")
	}
	if receipt.Timestamp == 0 {
		t.Error("timestamp is zero")
	}

	// Retrieve the data.
	retrieved, err := m.RetrieveL2Data(receipt.CommitmentHash)
	if err != nil {
		t.Fatalf("RetrieveL2Data failed: %v", err)
	}
	if !bytes.Equal(retrieved, data) {
		t.Errorf("retrieved data mismatch: got %q, want %q", retrieved, data)
	}
}

func TestStoreL2Data_EmptyData(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	_, err := m.StoreL2Data(1, nil)
	if err != ErrTeradataDataEmpty {
		t.Errorf("expected ErrTeradataDataEmpty, got %v", err)
	}
	_, err = m.StoreL2Data(1, []byte{})
	if err != ErrTeradataDataEmpty {
		t.Errorf("expected ErrTeradataDataEmpty for empty slice, got %v", err)
	}
}

func TestStoreL2Data_ZeroChainID(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	_, err := m.StoreL2Data(0, []byte("data"))
	if err != ErrTeradataInvalidChainID {
		t.Errorf("expected ErrTeradataInvalidChainID, got %v", err)
	}
}

func TestStoreL2Data_TooLarge(t *testing.T) {
	cfg := TeradataConfig{MaxDataSize: 10}
	m := NewTeradataManager(cfg)
	_, err := m.StoreL2Data(1, make([]byte, 11))
	if err != ErrTeradataDataTooLarge {
		t.Errorf("expected ErrTeradataDataTooLarge, got %v", err)
	}
}

func TestStoreL2Data_TooManyChains(t *testing.T) {
	cfg := TeradataConfig{MaxL2Chains: 2, MaxDataSize: 1024}
	m := NewTeradataManager(cfg)

	if _, err := m.StoreL2Data(1, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StoreL2Data(2, []byte("b")); err != nil {
		t.Fatal(err)
	}
	_, err := m.StoreL2Data(3, []byte("c"))
	if err != ErrTeradataTooManyChains {
		t.Errorf("expected ErrTeradataTooManyChains, got %v", err)
	}

	// Storing to an existing chain should still work.
	if _, err := m.StoreL2Data(1, []byte("d")); err != nil {
		t.Errorf("storing to existing chain failed: %v", err)
	}
}

func TestStoreL2Data_StorageLimitExceeded(t *testing.T) {
	cfg := TeradataConfig{TotalStorageLimit: 20, MaxDataSize: 100}
	m := NewTeradataManager(cfg)

	if _, err := m.StoreL2Data(1, make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.StoreL2Data(1, make([]byte, 10)); err != nil {
		t.Fatal(err)
	}
	_, err := m.StoreL2Data(1, make([]byte, 1))
	if err != ErrTeradataStorageFull {
		t.Errorf("expected ErrTeradataStorageFull, got %v", err)
	}
}

func TestRetrieveL2Data_NotFound(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	_, err := m.RetrieveL2Data(types.Hash{})
	if err != ErrTeradataNotFound {
		t.Errorf("expected ErrTeradataNotFound, got %v", err)
	}
}

func TestRetrieveL2Data_ReturnsCopy(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	data := []byte("immutable check")
	receipt, _ := m.StoreL2Data(1, data)

	retrieved, _ := m.RetrieveL2Data(receipt.CommitmentHash)
	// Mutate the returned slice.
	retrieved[0] = 0xFF

	// Retrieve again; original should be intact.
	again, _ := m.RetrieveL2Data(receipt.CommitmentHash)
	if again[0] == 0xFF {
		t.Error("RetrieveL2Data returned a reference, not a copy")
	}
}

func TestVerifyL2Data(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	data := []byte("verify this data")
	receipt, _ := m.StoreL2Data(7, data)

	if !m.VerifyL2Data(receipt, data) {
		t.Error("VerifyL2Data returned false for correct data")
	}

	// Wrong data.
	if m.VerifyL2Data(receipt, []byte("wrong data 12345")) {
		t.Error("VerifyL2Data returned true for wrong data")
	}

	// Nil receipt.
	if m.VerifyL2Data(nil, data) {
		t.Error("VerifyL2Data returned true for nil receipt")
	}

	// Empty data.
	if m.VerifyL2Data(receipt, nil) {
		t.Error("VerifyL2Data returned true for nil data")
	}

	// Wrong size.
	if m.VerifyL2Data(receipt, []byte("short")) {
		t.Error("VerifyL2Data returned true for data with wrong size")
	}
}

func TestGetL2Stats(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())

	// No data yet.
	if stats := m.GetL2Stats(1); stats != nil {
		t.Error("expected nil stats for unknown chain")
	}

	m.StoreL2Data(1, make([]byte, 100))
	m.StoreL2Data(1, make([]byte, 200))
	m.StoreL2Data(1, make([]byte, 300))

	stats := m.GetL2Stats(1)
	if stats == nil {
		t.Fatal("stats is nil")
	}
	if stats.ChainID != 1 {
		t.Errorf("ChainID=%d, want 1", stats.ChainID)
	}
	if stats.TotalBlobs != 3 {
		t.Errorf("TotalBlobs=%d, want 3", stats.TotalBlobs)
	}
	if stats.TotalBytes != 600 {
		t.Errorf("TotalBytes=%d, want 600", stats.TotalBytes)
	}
	if stats.AvgBlobSize != 200 {
		t.Errorf("AvgBlobSize=%d, want 200", stats.AvgBlobSize)
	}
}

func TestPruneOldData(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())

	// Store 5 blobs, which will get slots 1..5.
	var receipts []*TeradataReceipt
	for i := 0; i < 5; i++ {
		r, _ := m.StoreL2Data(1, []byte{byte(i)})
		receipts = append(receipts, r)
	}

	// Prune entries with slot < 3 (slots 1 and 2).
	deleted := m.PruneOldData(3)
	if deleted != 2 {
		t.Errorf("PruneOldData returned %d, want 2", deleted)
	}

	// Slots 1 and 2 should be gone.
	for _, r := range receipts[:2] {
		_, err := m.RetrieveL2Data(r.CommitmentHash)
		if err != ErrTeradataNotFound {
			t.Errorf("expected ErrTeradataNotFound for pruned slot %d, got %v", r.Slot, err)
		}
	}

	// Slots 3, 4, 5 should still be present.
	for _, r := range receipts[2:] {
		_, err := m.RetrieveL2Data(r.CommitmentHash)
		if err != nil {
			t.Errorf("slot %d should still exist, got: %v", r.Slot, err)
		}
	}

	// Stats should reflect pruning.
	stats := m.GetL2Stats(1)
	if stats.TotalBlobs != 3 {
		t.Errorf("after prune TotalBlobs=%d, want 3", stats.TotalBlobs)
	}
}

func TestPruneOldData_RemovesEmptyChain(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	m.StoreL2Data(99, []byte("only one"))

	deleted := m.PruneOldData(100)
	if deleted != 1 {
		t.Errorf("deleted=%d, want 1", deleted)
	}

	chains := m.ListL2Chains()
	if len(chains) != 0 {
		t.Errorf("expected no chains after full prune, got %v", chains)
	}
}

func TestListL2Chains(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())

	chains := m.ListL2Chains()
	if len(chains) != 0 {
		t.Errorf("expected empty chain list, got %v", chains)
	}

	m.StoreL2Data(10, []byte("a"))
	m.StoreL2Data(3, []byte("b"))
	m.StoreL2Data(7, []byte("c"))
	m.StoreL2Data(3, []byte("d")) // duplicate chain

	chains = m.ListL2Chains()
	expected := []uint64{3, 7, 10}
	if len(chains) != len(expected) {
		t.Fatalf("got %v, want %v", chains, expected)
	}
	for i := range expected {
		if chains[i] != expected[i] {
			t.Errorf("chains[%d]=%d, want %d", i, chains[i], expected[i])
		}
	}
}

func TestSlotMonotonicallyIncreases(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	var lastSlot uint64
	for i := 0; i < 10; i++ {
		r, _ := m.StoreL2Data(1, []byte{byte(i)})
		if r.Slot <= lastSlot {
			t.Errorf("slot did not increase: prev=%d, curr=%d", lastSlot, r.Slot)
		}
		lastSlot = r.Slot
	}
}

func TestDifferentDataDifferentCommitments(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	r1, _ := m.StoreL2Data(1, []byte("data_a"))
	r2, _ := m.StoreL2Data(1, []byte("data_b"))
	if r1.CommitmentHash == r2.CommitmentHash {
		t.Error("different data produced same commitment")
	}
}

func TestSameDataDifferentChainDifferentCommitments(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	data := []byte("shared payload")
	r1, _ := m.StoreL2Data(1, data)
	r2, _ := m.StoreL2Data(2, data)
	if r1.CommitmentHash == r2.CommitmentHash {
		t.Error("same data on different chains produced same commitment")
	}
}

func TestStorageTrackingAfterPrune(t *testing.T) {
	cfg := TeradataConfig{TotalStorageLimit: 100, MaxDataSize: 100}
	m := NewTeradataManager(cfg)

	// Fill storage.
	m.StoreL2Data(1, make([]byte, 50))
	m.StoreL2Data(1, make([]byte, 50))

	// Storage is full.
	_, err := m.StoreL2Data(1, make([]byte, 1))
	if err != ErrTeradataStorageFull {
		t.Fatalf("expected storage full, got %v", err)
	}

	// Prune everything.
	m.PruneOldData(100)

	// Should be able to store again.
	_, err = m.StoreL2Data(1, make([]byte, 50))
	if err != nil {
		t.Errorf("expected success after prune, got %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	var wg sync.WaitGroup

	// Concurrent stores.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			chainID := uint64(id%5) + 1
			data := []byte{byte(id), byte(id >> 8)}
			m.StoreL2Data(chainID, data)
		}(i)
	}
	wg.Wait()

	// Concurrent reads.
	chains := m.ListL2Chains()
	for _, c := range chains {
		wg.Add(1)
		go func(chainID uint64) {
			defer wg.Done()
			m.GetL2Stats(chainID)
		}(c)
	}
	wg.Wait()

	// Concurrent prune.
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.PruneOldData(10)
	}()
	wg.Wait()
}

func TestDefaultTeradataConfig(t *testing.T) {
	cfg := DefaultTeradataConfig()
	if cfg.MaxDataSize == 0 {
		t.Error("MaxDataSize should not be zero")
	}
	if cfg.MaxL2Chains == 0 {
		t.Error("MaxL2Chains should not be zero")
	}
	if cfg.RetentionSlots == 0 {
		t.Error("RetentionSlots should not be zero")
	}
	if cfg.TotalStorageLimit == 0 {
		t.Error("TotalStorageLimit should not be zero")
	}
}

func TestUint64ToBytes(t *testing.T) {
	tests := []struct {
		input uint64
		want  []byte
	}{
		{0, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{1, []byte{0, 0, 0, 0, 0, 0, 0, 1}},
		{256, []byte{0, 0, 0, 0, 0, 0, 1, 0}},
		{0xFFFFFFFFFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
	}
	for _, tt := range tests {
		got := uint64ToBytes(tt.input)
		if !bytes.Equal(got, tt.want) {
			t.Errorf("uint64ToBytes(%d) = %x, want %x", tt.input, got, tt.want)
		}
	}
}

func TestStoreL2Data_CallerCannotMutateInternalData(t *testing.T) {
	m := NewTeradataManager(DefaultTeradataConfig())
	data := []byte("original data here")
	receipt, _ := m.StoreL2Data(1, data)

	// Mutate the original slice after storing.
	data[0] = 0xFF

	// Internal data should be unaffected.
	retrieved, _ := m.RetrieveL2Data(receipt.CommitmentHash)
	if retrieved[0] == 0xFF {
		t.Error("StoreL2Data did not copy input data; caller mutation visible")
	}
}
