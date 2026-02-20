package state

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestStateHistoryReaderNew(t *testing.T) {
	r := NewStateHistoryReader(256)
	if r == nil {
		t.Fatal("NewStateHistoryReader returned nil")
	}
	if r.RetentionWindow() != 256 {
		t.Fatalf("retention window: got %d, want 256", r.RetentionWindow())
	}
	hr := r.Range()
	if hr.MinBlock != 0 || hr.MaxBlock != 0 {
		t.Fatalf("empty range: got %d-%d, want 0-0", hr.MinBlock, hr.MaxBlock)
	}
}

func TestStateHistoryReaderAddAndGetAccount(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 100,
		Address:     addr,
		Nonce:       5,
		Balance:     []byte{0x03, 0xe8}, // 1000
		CodeHash:    types.HexToHash("0xabcdef"),
	})

	entry, err := r.GetAccountAt(addr, 100)
	if err != nil {
		t.Fatalf("GetAccountAt failed: %v", err)
	}
	if entry.Nonce != 5 {
		t.Fatalf("nonce: got %d, want 5", entry.Nonce)
	}
}

func TestStateHistoryReaderGetAccountLatestBefore(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")

	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 100,
		Address:     addr,
		Nonce:       1,
	})
	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 200,
		Address:     addr,
		Nonce:       2,
	})
	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 300,
		Address:     addr,
		Nonce:       3,
	})

	// Query at block 250: should return entry at block 200.
	entry, err := r.GetAccountAt(addr, 250)
	if err != nil {
		t.Fatalf("GetAccountAt(250) failed: %v", err)
	}
	if entry.Nonce != 2 {
		t.Fatalf("nonce at block 250: got %d, want 2", entry.Nonce)
	}
	if entry.BlockNumber != 200 {
		t.Fatalf("block: got %d, want 200", entry.BlockNumber)
	}
}

func TestStateHistoryReaderGetAccountOutOfRange(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")

	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 100,
		Address:     addr,
		Nonce:       1,
	})

	// Block 50 is below the min block (100).
	_, err := r.GetAccountAt(addr, 50)
	if err != ErrBlockNotInRange {
		t.Fatalf("expected ErrBlockNotInRange, got %v", err)
	}
}

func TestStateHistoryReaderGetAccountNoHistory(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x4444444444444444444444444444444444444444")

	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 100,
		Address:     types.HexToAddress("0x5555555555555555555555555555555555555555"),
		Nonce:       1,
	})

	_, err := r.GetAccountAt(addr, 100)
	if err != ErrNoHistoryAvailable {
		t.Fatalf("expected ErrNoHistoryAvailable, got %v", err)
	}
}

func TestStateHistoryReaderAddAndGetStorage(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x6666666666666666666666666666666666666666")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")

	r.AddStorageEntry(StorageHistoryEntry{
		BlockNumber: 150,
		Address:     addr,
		Slot:        slot,
		Value:       types.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ff"),
	})

	entry, err := r.GetStorageAt(addr, slot, 150)
	if err != nil {
		t.Fatalf("GetStorageAt failed: %v", err)
	}
	if entry.Value != types.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000ff") {
		t.Fatalf("unexpected storage value: %x", entry.Value)
	}
}

func TestStateHistoryReaderGetStorageLatestBefore(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x7777777777777777777777777777777777777777")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")

	r.AddStorageEntry(StorageHistoryEntry{
		BlockNumber: 100,
		Address:     addr,
		Slot:        slot,
		Value:       types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
	})
	r.AddStorageEntry(StorageHistoryEntry{
		BlockNumber: 200,
		Address:     addr,
		Slot:        slot,
		Value:       types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002"),
	})

	entry, err := r.GetStorageAt(addr, slot, 175)
	if err != nil {
		t.Fatalf("GetStorageAt(175) failed: %v", err)
	}
	if entry.BlockNumber != 100 {
		t.Fatalf("storage entry block: got %d, want 100", entry.BlockNumber)
	}
}

func TestStateHistoryReaderPruneHistory(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x8888888888888888888888888888888888888888")

	for i := uint64(100); i <= 200; i += 10 {
		r.AddAccountEntry(AccountHistoryEntry{
			BlockNumber: i,
			Address:     addr,
			Nonce:       i,
		})
	}

	// Prune everything before block 150.
	pruned, err := r.PruneHistory(150)
	if err != nil {
		t.Fatalf("PruneHistory failed: %v", err)
	}
	// Blocks 100, 110, 120, 130, 140 should be pruned (5 entries).
	if pruned != 5 {
		t.Fatalf("pruned: got %d, want 5", pruned)
	}

	hr := r.Range()
	if hr.MinBlock != 150 {
		t.Fatalf("after prune, min block: got %d, want 150", hr.MinBlock)
	}

	// Entries at 150-200 should remain.
	remaining := r.AccountEntryCount()
	if remaining != 6 {
		t.Fatalf("remaining entries: got %d, want 6", remaining)
	}
}

func TestStateHistoryReaderPruneAll(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0x9999999999999999999999999999999999999999")

	r.AddAccountEntry(AccountHistoryEntry{
		BlockNumber: 100,
		Address:     addr,
		Nonce:       1,
	})

	pruned, err := r.PruneHistory(200)
	if err != nil {
		t.Fatalf("PruneHistory failed: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned: got %d, want 1", pruned)
	}
	if r.UniqueAddressCount() != 0 {
		t.Fatalf("after pruning all, unique addresses: got %d, want 0", r.UniqueAddressCount())
	}
}

func TestStateHistoryReaderPruneZero(t *testing.T) {
	r := NewStateHistoryReader(256)
	_, err := r.PruneHistory(0)
	if err != ErrInvalidPruneRange {
		t.Fatalf("expected ErrInvalidPruneRange, got %v", err)
	}
}

func TestStateHistoryReaderPruneStorage(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000005")

	r.AddStorageEntry(StorageHistoryEntry{BlockNumber: 100, Address: addr, Slot: slot})
	r.AddStorageEntry(StorageHistoryEntry{BlockNumber: 200, Address: addr, Slot: slot})
	r.AddStorageEntry(StorageHistoryEntry{BlockNumber: 300, Address: addr, Slot: slot})

	pruned, err := r.PruneHistory(250)
	if err != nil {
		t.Fatalf("PruneHistory failed: %v", err)
	}
	if pruned != 2 {
		t.Fatalf("storage pruned: got %d, want 2", pruned)
	}

	remaining := r.StorageEntryCount()
	if remaining != 1 {
		t.Fatalf("remaining storage entries: got %d, want 1", remaining)
	}
}

func TestStateHistoryReaderHistoryRange(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 50, Address: addr})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 300, Address: addr})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 150, Address: addr})

	hr := r.Range()
	if hr.MinBlock != 50 {
		t.Fatalf("min block: got %d, want 50", hr.MinBlock)
	}
	if hr.MaxBlock != 300 {
		t.Fatalf("max block: got %d, want 300", hr.MaxBlock)
	}
	if hr.Width() != 251 {
		t.Fatalf("width: got %d, want 251", hr.Width())
	}
}

func TestStateHistoryReaderGetAccountHistory(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 300, Address: addr, Nonce: 3})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 100, Address: addr, Nonce: 1})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 200, Address: addr, Nonce: 2})

	history := r.GetAccountHistory(addr)
	if len(history) != 3 {
		t.Fatalf("history length: got %d, want 3", len(history))
	}
	// Should be sorted by block number.
	if history[0].BlockNumber != 100 {
		t.Fatalf("first entry block: got %d, want 100", history[0].BlockNumber)
	}
	if history[2].BlockNumber != 300 {
		t.Fatalf("last entry block: got %d, want 300", history[2].BlockNumber)
	}
}

func TestStateHistoryReaderGetStorageHistory(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddd")
	slot := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000007")

	r.AddStorageEntry(StorageHistoryEntry{BlockNumber: 200, Address: addr, Slot: slot})
	r.AddStorageEntry(StorageHistoryEntry{BlockNumber: 100, Address: addr, Slot: slot})

	history := r.GetStorageHistory(addr, slot)
	if len(history) != 2 {
		t.Fatalf("storage history length: got %d, want 2", len(history))
	}
	if history[0].BlockNumber != 100 {
		t.Fatalf("first storage entry: got %d, want 100", history[0].BlockNumber)
	}
}

func TestStateHistoryReaderAccountEntryCount(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 100, Address: addr1})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 200, Address: addr1})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 100, Address: addr2})

	if r.AccountEntryCount() != 3 {
		t.Fatalf("account entry count: got %d, want 3", r.AccountEntryCount())
	}
	if r.UniqueAddressCount() != 2 {
		t.Fatalf("unique address count: got %d, want 2", r.UniqueAddressCount())
	}
}

func TestHistoryRangeContains(t *testing.T) {
	hr := HistoryRange{MinBlock: 100, MaxBlock: 200}

	if !hr.Contains(100) {
		t.Fatal("should contain min block")
	}
	if !hr.Contains(200) {
		t.Fatal("should contain max block")
	}
	if !hr.Contains(150) {
		t.Fatal("should contain middle block")
	}
	if hr.Contains(99) {
		t.Fatal("should not contain block below min")
	}
	if hr.Contains(201) {
		t.Fatal("should not contain block above max")
	}
}

func TestHistoryRangeWidth(t *testing.T) {
	hr := HistoryRange{MinBlock: 100, MaxBlock: 200}
	if hr.Width() != 101 {
		t.Fatalf("width: got %d, want 101", hr.Width())
	}

	hrEmpty := HistoryRange{MinBlock: 200, MaxBlock: 100}
	if hrEmpty.Width() != 0 {
		t.Fatalf("invalid range width: got %d, want 0", hrEmpty.Width())
	}
}

func TestStateHistoryReaderGetAccountBeforeAllEntries(t *testing.T) {
	r := NewStateHistoryReader(256)
	addr := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 200, Address: addr, Nonce: 2})
	r.AddAccountEntry(AccountHistoryEntry{BlockNumber: 300, Address: addr, Nonce: 3})

	// Query at block 150: within range (200-300) but no entry at or before 150.
	// Wait - 150 is below min block 200, so it should be out of range.
	_, err := r.GetAccountAt(addr, 150)
	if err != ErrBlockNotInRange {
		t.Fatalf("expected ErrBlockNotInRange for block before min, got %v", err)
	}
}
