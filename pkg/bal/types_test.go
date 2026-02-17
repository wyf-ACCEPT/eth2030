package bal

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewBlockAccessList(t *testing.T) {
	bal := NewBlockAccessList()
	if bal == nil {
		t.Fatal("NewBlockAccessList returned nil")
	}
	if bal.Len() != 0 {
		t.Fatalf("expected empty BAL, got %d entries", bal.Len())
	}
}

func TestAddEntry(t *testing.T) {
	bal := NewBlockAccessList()

	addr := types.HexToAddress("0x1234")
	entry := AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xff")},
		},
	}

	bal.AddEntry(entry)
	if bal.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", bal.Len())
	}

	if bal.Entries[0].Address != addr {
		t.Fatalf("address mismatch: got %v, want %v", bal.Entries[0].Address, addr)
	}
	if bal.Entries[0].AccessIndex != 1 {
		t.Fatalf("access index mismatch: got %d, want 1", bal.Entries[0].AccessIndex)
	}
	if len(bal.Entries[0].StorageReads) != 1 {
		t.Fatalf("expected 1 storage read, got %d", len(bal.Entries[0].StorageReads))
	}
}

func TestAddMultipleEntries(t *testing.T) {
	bal := NewBlockAccessList()

	for i := uint64(0); i < 5; i++ {
		bal.AddEntry(AccessEntry{
			Address:     types.HexToAddress("0x1234"),
			AccessIndex: i,
		})
	}

	if bal.Len() != 5 {
		t.Fatalf("expected 5 entries, got %d", bal.Len())
	}
}

func TestAccessEntryWithAllFields(t *testing.T) {
	entry := AccessEntry{
		Address:     types.HexToAddress("0xabcd"),
		AccessIndex: 2,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0x10")},
		},
		StorageChanges: []StorageChange{
			{
				Slot:     types.HexToHash("0x02"),
				OldValue: types.HexToHash("0x20"),
				NewValue: types.HexToHash("0x30"),
			},
		},
		BalanceChange: &BalanceChange{
			OldValue: big.NewInt(100),
			NewValue: big.NewInt(200),
		},
		NonceChange: &NonceChange{
			OldValue: 0,
			NewValue: 1,
		},
		CodeChange: &CodeChange{
			OldCode: nil,
			NewCode: []byte{0x60, 0x80, 0x60, 0x40},
		},
	}

	bal := NewBlockAccessList()
	bal.AddEntry(entry)

	got := bal.Entries[0]
	if got.BalanceChange == nil {
		t.Fatal("expected BalanceChange, got nil")
	}
	if got.BalanceChange.OldValue.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("balance old value: got %v, want 100", got.BalanceChange.OldValue)
	}
	if got.BalanceChange.NewValue.Cmp(big.NewInt(200)) != 0 {
		t.Fatalf("balance new value: got %v, want 200", got.BalanceChange.NewValue)
	}
	if got.NonceChange == nil || got.NonceChange.OldValue != 0 || got.NonceChange.NewValue != 1 {
		t.Fatalf("unexpected nonce change: %+v", got.NonceChange)
	}
	if got.CodeChange == nil || got.CodeChange.OldCode != nil {
		t.Fatalf("unexpected code change: %+v", got.CodeChange)
	}
	if len(got.CodeChange.NewCode) != 4 {
		t.Fatalf("expected 4-byte new code, got %d", len(got.CodeChange.NewCode))
	}
}

func TestStorageAccessFields(t *testing.T) {
	slot := types.HexToHash("0xaabb")
	value := types.HexToHash("0xccdd")
	sa := StorageAccess{Slot: slot, Value: value}
	if sa.Slot != slot || sa.Value != value {
		t.Fatal("StorageAccess fields mismatch")
	}
}

func TestStorageChangeFields(t *testing.T) {
	sc := StorageChange{
		Slot:     types.HexToHash("0x01"),
		OldValue: types.HexToHash("0x02"),
		NewValue: types.HexToHash("0x03"),
	}
	if sc.Slot != types.HexToHash("0x01") {
		t.Fatal("StorageChange Slot mismatch")
	}
	if sc.OldValue != types.HexToHash("0x02") {
		t.Fatal("StorageChange OldValue mismatch")
	}
	if sc.NewValue != types.HexToHash("0x03") {
		t.Fatal("StorageChange NewValue mismatch")
	}
}
