package eth

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestProtocolVersionConstants verifies the eth protocol version constants.
func TestProtocolVersionConstants(t *testing.T) {
	if ETH68 != 68 {
		t.Fatalf("ETH68 = %d, want 68", ETH68)
	}
	if ETH70 != 70 {
		t.Fatalf("ETH70 = %d, want 70", ETH70)
	}
	if ETH71 != 71 {
		t.Fatalf("ETH71 = %d, want 71", ETH71)
	}
}

// TestMaxLimits verifies the protocol limit constants.
func TestMaxLimits(t *testing.T) {
	if MaxHeaders != 1024 {
		t.Fatalf("MaxHeaders = %d, want 1024", MaxHeaders)
	}
	if MaxBodies != 512 {
		t.Fatalf("MaxBodies = %d, want 512", MaxBodies)
	}
	if MaxPartialReceipts != 256 {
		t.Fatalf("MaxPartialReceipts = %d, want 256", MaxPartialReceipts)
	}
	if MaxAccessLists != 64 {
		t.Fatalf("MaxAccessLists = %d, want 64", MaxAccessLists)
	}
}

// TestStatusInfo_Fields tests that StatusInfo fields can be populated.
func TestStatusInfo_Fields(t *testing.T) {
	info := StatusInfo{
		ProtocolVersion: ETH68,
		NetworkID:       1,
		Head:            types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		Genesis:         types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		OldestBlock:     100,
	}
	if info.ProtocolVersion != 68 {
		t.Fatalf("want 68, got %d", info.ProtocolVersion)
	}
	if info.NetworkID != 1 {
		t.Fatalf("want 1, got %d", info.NetworkID)
	}
	if info.OldestBlock != 100 {
		t.Fatalf("want 100, got %d", info.OldestBlock)
	}
	if info.Head == (types.Hash{}) {
		t.Fatal("Head should not be zero")
	}
	if info.Genesis == (types.Hash{}) {
		t.Fatal("Genesis should not be zero")
	}
}

// TestAccessListEntry_Fields tests the AccessListEntry type.
func TestAccessListEntry_Fields(t *testing.T) {
	entry := AccessListEntry{
		Address:     types.HexToAddress("0xaaaa"),
		AccessIndex: 5,
		StorageKeys: []types.Hash{
			types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
			types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002"),
		},
	}
	if entry.Address == (types.Address{}) {
		t.Fatal("Address should not be zero")
	}
	if entry.AccessIndex != 5 {
		t.Fatalf("want 5, got %d", entry.AccessIndex)
	}
	if len(entry.StorageKeys) != 2 {
		t.Fatalf("want 2 storage keys, got %d", len(entry.StorageKeys))
	}
}

// TestBlockchainInterface verifies the Blockchain interface methods.
func TestBlockchainInterface(t *testing.T) {
	// Just verify the interface has the expected methods by checking
	// it can be assigned. A nil value is fine for type checking.
	var _ Blockchain = (Blockchain)(nil)
}

// TestTxPoolInterface verifies the TxPool interface methods.
func TestTxPoolInterface(t *testing.T) {
	var _ TxPool = (TxPool)(nil)
}

// TestReceiptProviderInterface verifies the ReceiptProvider interface.
func TestReceiptProviderInterface(t *testing.T) {
	var _ ReceiptProvider = (ReceiptProvider)(nil)
}

// TestAccessListProviderInterface verifies the AccessListProvider interface.
func TestAccessListProviderInterface(t *testing.T) {
	var _ AccessListProvider = (AccessListProvider)(nil)
}

// TestStatusInfo_ZeroValues tests default zero values for StatusInfo.
func TestStatusInfo_ZeroValues(t *testing.T) {
	var info StatusInfo
	if info.ProtocolVersion != 0 {
		t.Fatalf("want 0, got %d", info.ProtocolVersion)
	}
	if info.NetworkID != 0 {
		t.Fatalf("want 0, got %d", info.NetworkID)
	}
	if info.TD != nil {
		t.Fatal("TD should be nil by default")
	}
	if info.OldestBlock != 0 {
		t.Fatalf("want 0, got %d", info.OldestBlock)
	}
}

// TestAccessListEntry_EmptyStorageKeys tests an entry with no storage keys.
func TestAccessListEntry_EmptyStorageKeys(t *testing.T) {
	entry := AccessListEntry{
		Address:     types.HexToAddress("0xbbbb"),
		AccessIndex: 0,
	}
	if entry.StorageKeys != nil {
		t.Fatal("StorageKeys should be nil by default")
	}
}
