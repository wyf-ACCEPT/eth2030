package bal

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewTracker(t *testing.T) {
	tr := NewTracker()
	if tr == nil {
		t.Fatal("NewTracker returned nil")
	}
}

func TestRecordStorageRead(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x1111")
	slot := types.HexToHash("0x01")
	value := types.HexToHash("0xaa")

	tr.RecordStorageRead(addr, slot, value)

	bal := tr.Build(1)
	if bal.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", bal.Len())
	}
	if len(bal.Entries[0].StorageReads) != 1 {
		t.Fatalf("expected 1 read, got %d", len(bal.Entries[0].StorageReads))
	}
	if bal.Entries[0].StorageReads[0].Slot != slot {
		t.Fatal("slot mismatch")
	}
	if bal.Entries[0].StorageReads[0].Value != value {
		t.Fatal("value mismatch")
	}
}

func TestRecordStorageChange(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x2222")
	slot := types.HexToHash("0x02")
	old := types.HexToHash("0x10")
	new := types.HexToHash("0x20")

	tr.RecordStorageChange(addr, slot, old, new)

	bal := tr.Build(2)
	if bal.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", bal.Len())
	}
	if len(bal.Entries[0].StorageChanges) != 1 {
		t.Fatalf("expected 1 change, got %d", len(bal.Entries[0].StorageChanges))
	}
	sc := bal.Entries[0].StorageChanges[0]
	if sc.Slot != slot || sc.OldValue != old || sc.NewValue != new {
		t.Fatal("storage change mismatch")
	}
}

func TestRecordBalanceChange(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x3333")
	old := big.NewInt(1000)
	new := big.NewInt(900)

	tr.RecordBalanceChange(addr, old, new)

	bal := tr.Build(1)
	if bal.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", bal.Len())
	}
	bc := bal.Entries[0].BalanceChange
	if bc == nil {
		t.Fatal("expected balance change, got nil")
	}
	if bc.OldValue.Cmp(old) != 0 || bc.NewValue.Cmp(new) != 0 {
		t.Fatal("balance change value mismatch")
	}
}

func TestRecordNonceChange(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x4444")

	tr.RecordNonceChange(addr, 5, 6)

	bal := tr.Build(1)
	nc := bal.Entries[0].NonceChange
	if nc == nil {
		t.Fatal("expected nonce change, got nil")
	}
	if nc.OldValue != 5 || nc.NewValue != 6 {
		t.Fatalf("nonce change: got %d->%d, want 5->6", nc.OldValue, nc.NewValue)
	}
}

func TestRecordCodeChange(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x5555")
	oldCode := []byte(nil)
	newCode := []byte{0x60, 0x80, 0x60, 0x40}

	tr.RecordCodeChange(addr, oldCode, newCode)

	bal := tr.Build(1)
	cc := bal.Entries[0].CodeChange
	if cc == nil {
		t.Fatal("expected code change, got nil")
	}
	if cc.OldCode != nil {
		t.Fatal("expected nil old code")
	}
	if len(cc.NewCode) != 4 || cc.NewCode[0] != 0x60 {
		t.Fatal("code change mismatch")
	}
}

func TestRecordCodeChangeCopiesValues(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x5555")
	code := []byte{0x60, 0x80}

	tr.RecordCodeChange(addr, nil, code)

	// Mutate original.
	code[0] = 0xff

	bal := tr.Build(1)
	if bal.Entries[0].CodeChange.NewCode[0] != 0x60 {
		t.Fatal("code change was mutated via original slice")
	}
}

func TestBuildAccessIndex(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x1111")
	tr.RecordStorageRead(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))

	bal := tr.Build(42)
	if bal.Entries[0].AccessIndex != 42 {
		t.Fatalf("expected access index 42, got %d", bal.Entries[0].AccessIndex)
	}
}

func TestBuildMultipleAddresses(t *testing.T) {
	tr := NewTracker()
	addr1 := types.HexToAddress("0x1111")
	addr2 := types.HexToAddress("0x2222")

	tr.RecordStorageRead(addr1, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	tr.RecordStorageRead(addr2, types.HexToHash("0x02"), types.HexToHash("0xbb"))

	bal := tr.Build(1)
	if bal.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", bal.Len())
	}
}

func TestBuildMultipleReadsAndChanges(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x1111")

	tr.RecordStorageRead(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	tr.RecordStorageRead(addr, types.HexToHash("0x02"), types.HexToHash("0xbb"))
	tr.RecordStorageChange(addr, types.HexToHash("0x03"), types.HexToHash("0x10"), types.HexToHash("0x20"))

	bal := tr.Build(1)
	if bal.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", bal.Len())
	}
	if len(bal.Entries[0].StorageReads) != 2 {
		t.Fatalf("expected 2 reads, got %d", len(bal.Entries[0].StorageReads))
	}
	if len(bal.Entries[0].StorageChanges) != 1 {
		t.Fatalf("expected 1 change, got %d", len(bal.Entries[0].StorageChanges))
	}
}

func TestReset(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x1111")

	tr.RecordStorageRead(addr, types.HexToHash("0x01"), types.HexToHash("0xaa"))
	tr.RecordBalanceChange(addr, big.NewInt(100), big.NewInt(200))
	tr.RecordNonceChange(addr, 0, 1)

	tr.Reset()

	bal := tr.Build(1)
	if bal.Len() != 0 {
		t.Fatalf("expected empty BAL after reset, got %d entries", bal.Len())
	}
}

func TestBalanceChangeCopiesValues(t *testing.T) {
	tr := NewTracker()
	addr := types.HexToAddress("0x1111")
	old := big.NewInt(100)
	new := big.NewInt(200)

	tr.RecordBalanceChange(addr, old, new)

	// Mutate original values.
	old.SetInt64(999)
	new.SetInt64(999)

	bal := tr.Build(1)
	bc := bal.Entries[0].BalanceChange
	if bc.OldValue.Cmp(big.NewInt(100)) != 0 {
		t.Fatal("balance change old value was mutated")
	}
	if bc.NewValue.Cmp(big.NewInt(200)) != 0 {
		t.Fatal("balance change new value was mutated")
	}
}
