package state

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// helper: test addresses and slots.
var (
	cdAddr1 = types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cdAddr2 = types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	cdAddr3 = types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	cdSlot1 = types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	cdSlot2 = types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000002")
	cdSlot3 = types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000003")
)

func TestConflictDetectorNew(t *testing.T) {
	cd := NewConflictDetector()
	if cd == nil {
		t.Fatal("expected non-nil detector")
	}
	if cd.TxCount() != 0 {
		t.Errorf("expected 0 tx count, got %d", cd.TxCount())
	}
	if cd.IsFinalized() {
		t.Error("expected not finalized")
	}
}

func TestConflictDetectorRegisterTx(t *testing.T) {
	cd := NewConflictDetector()

	rw, err := cd.RegisterTx(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rw == nil {
		t.Fatal("expected non-nil rw set")
	}
	if rw.TxIndex != 0 {
		t.Errorf("expected tx index 0, got %d", rw.TxIndex)
	}
	if cd.TxCount() != 1 {
		t.Errorf("expected 1 tx count, got %d", cd.TxCount())
	}
}

func TestConflictDetectorRegisterDuplicate(t *testing.T) {
	cd := NewConflictDetector()
	_, _ = cd.RegisterTx(0)

	_, err := cd.RegisterTx(0)
	if err != ErrCDTxAlreadyExists {
		t.Errorf("expected ErrCDTxAlreadyExists, got %v", err)
	}
}

func TestConflictDetectorRegisterNegativeIndex(t *testing.T) {
	cd := NewConflictDetector()

	_, err := cd.RegisterTx(-1)
	if err != ErrCDNegativeTxIndex {
		t.Errorf("expected ErrCDNegativeTxIndex, got %v", err)
	}
}

func TestConflictDetectorNoConflicts(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	// Tx 0 reads/writes addr1.
	rw0.ReadAccount(cdAddr1)
	rw0.WriteAccount(cdAddr1)

	// Tx 1 reads/writes addr2 (completely disjoint).
	rw1.ReadAccount(cdAddr2)
	rw1.WriteAccount(cdAddr2)

	result := cd.Detect()
	if result.HasConflicts {
		t.Error("expected no conflicts")
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("expected 0 conflicts, got %d", len(result.Conflicts))
	}
	if result.TxCount != 2 {
		t.Errorf("expected tx count 2, got %d", result.TxCount)
	}
}

func TestConflictDetectorAccountWriteWrite(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.WriteAccount(cdAddr1)
	rw1.WriteAccount(cdAddr1) // WW conflict

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected conflicts")
	}
	if result.WWCount != 1 {
		t.Errorf("expected 1 WW conflict, got %d", result.WWCount)
	}
	if len(result.Conflicts) < 1 {
		t.Fatal("expected at least 1 conflict")
	}
	c := result.Conflicts[0]
	if c.Kind != ConflictWriteWrite {
		t.Errorf("expected ConflictWriteWrite, got %s", c.Kind)
	}
	if c.Location.Address != cdAddr1 {
		t.Errorf("expected addr1, got %s", c.Location.Address.Hex())
	}
}

func TestConflictDetectorAccountReadWrite(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.ReadAccount(cdAddr1)
	rw1.WriteAccount(cdAddr1) // RW conflict

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected conflicts")
	}
	if result.RWCount != 1 {
		t.Errorf("expected 1 RW conflict, got %d", result.RWCount)
	}
}

func TestConflictDetectorAccountWriteRead(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.WriteAccount(cdAddr1)
	rw1.ReadAccount(cdAddr1) // WR conflict

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected conflicts")
	}
	if result.WRCount != 1 {
		t.Errorf("expected 1 WR conflict, got %d", result.WRCount)
	}
}

func TestConflictDetectorStorageWriteWrite(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.WriteStorage(cdAddr1, cdSlot1)
	rw1.WriteStorage(cdAddr1, cdSlot1) // WW on storage

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected storage WW conflict")
	}
	if result.WWCount != 1 {
		t.Errorf("expected 1 WW conflict, got %d", result.WWCount)
	}
	c := result.Conflicts[0]
	if c.Location.Kind != LocationStorage {
		t.Errorf("expected LocationStorage, got %s", c.Location.Kind)
	}
}

func TestConflictDetectorStorageReadWrite(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.ReadStorage(cdAddr1, cdSlot2)
	rw1.WriteStorage(cdAddr1, cdSlot2)

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected storage RW conflict")
	}
	if result.RWCount != 1 {
		t.Errorf("expected 1 RW conflict, got %d", result.RWCount)
	}
}

func TestConflictDetectorStorageWriteRead(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.WriteStorage(cdAddr2, cdSlot3)
	rw1.ReadStorage(cdAddr2, cdSlot3)

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected storage WR conflict")
	}
	if result.WRCount != 1 {
		t.Errorf("expected 1 WR conflict, got %d", result.WRCount)
	}
}

func TestConflictDetectorMultipleConflicts(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)
	rw2, _ := cd.RegisterTx(2)

	// Tx0 writes addr1 account and addr2 storage slot1.
	rw0.WriteAccount(cdAddr1)
	rw0.WriteStorage(cdAddr2, cdSlot1)

	// Tx1 writes addr1 account (WW with tx0) and reads addr2 slot1 (WR with tx0).
	rw1.WriteAccount(cdAddr1)
	rw1.ReadStorage(cdAddr2, cdSlot1)

	// Tx2 reads addr1 (RW with tx0 and tx1).
	rw2.ReadAccount(cdAddr1)

	result := cd.Detect()
	if !result.HasConflicts {
		t.Error("expected multiple conflicts")
	}
	// Should have at least: WW(tx0,tx1) on addr1, WR(tx0,tx1) on slot1, RW(tx2,tx0), RW(tx2,tx1).
	if result.TxCount != 3 {
		t.Errorf("expected tx count 3, got %d", result.TxCount)
	}
	if len(result.Conflicts) < 4 {
		t.Errorf("expected at least 4 conflicts, got %d", len(result.Conflicts))
	}
}

func TestConflictDetectorDifferentSlotsSameAddress(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.WriteStorage(cdAddr1, cdSlot1)
	rw1.WriteStorage(cdAddr1, cdSlot2) // different slot, no conflict

	result := cd.Detect()
	if result.HasConflicts {
		t.Error("expected no conflicts for different storage slots")
	}
}

func TestConflictDetectorGetTxSet(t *testing.T) {
	cd := NewConflictDetector()

	rw, _ := cd.RegisterTx(5)
	rw.WriteAccount(cdAddr1)

	got, err := cd.GetTxSet(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != rw {
		t.Error("expected same rw set pointer")
	}

	_, err = cd.GetTxSet(99)
	if err != ErrCDTxNotFound {
		t.Errorf("expected ErrCDTxNotFound, got %v", err)
	}
}

func TestConflictDetectorFinalizedRejectsNewTx(t *testing.T) {
	cd := NewConflictDetector()
	_, _ = cd.RegisterTx(0)

	cd.Detect() // finalizes

	if !cd.IsFinalized() {
		t.Error("expected finalized")
	}

	_, err := cd.RegisterTx(1)
	if err != ErrCDAlreadyFinalized {
		t.Errorf("expected ErrCDAlreadyFinalized, got %v", err)
	}
}

func TestConflictDetectorReset(t *testing.T) {
	cd := NewConflictDetector()

	rw, _ := cd.RegisterTx(0)
	rw.WriteAccount(cdAddr1)
	cd.Detect()

	cd.Reset()

	if cd.TxCount() != 0 {
		t.Errorf("expected 0 tx count after reset, got %d", cd.TxCount())
	}
	if cd.IsFinalized() {
		t.Error("expected not finalized after reset")
	}

	// Should be able to register again.
	_, err := cd.RegisterTx(0)
	if err != nil {
		t.Fatalf("unexpected error after reset: %v", err)
	}
}

func TestConflictDetectorConflictFreeGroupsNoConflicts(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)
	rw2, _ := cd.RegisterTx(2)

	rw0.WriteAccount(cdAddr1)
	rw1.WriteAccount(cdAddr2)
	rw2.WriteAccount(cdAddr3)

	groups := cd.ConflictFreeGroups()
	if len(groups) != 1 {
		t.Errorf("expected 1 group (all conflict-free), got %d", len(groups))
	}
	if len(groups[0]) != 3 {
		t.Errorf("expected 3 txs in group, got %d", len(groups[0]))
	}
}

func TestConflictDetectorConflictFreeGroupsWithConflicts(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)
	rw2, _ := cd.RegisterTx(2)

	// tx0 and tx1 conflict on addr1.
	rw0.WriteAccount(cdAddr1)
	rw1.WriteAccount(cdAddr1)

	// tx2 is independent.
	rw2.WriteAccount(cdAddr3)

	groups := cd.ConflictFreeGroups()
	if len(groups) < 2 {
		t.Errorf("expected at least 2 groups, got %d", len(groups))
	}

	// Verify no two conflicting txs are in the same group.
	for _, group := range groups {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				if group[i] == 0 && group[j] == 1 || group[i] == 1 && group[j] == 0 {
					t.Error("tx0 and tx1 should not be in the same group")
				}
			}
		}
	}
}

func TestConflictDetectorTouchedAddresses(t *testing.T) {
	cd := NewConflictDetector()

	rw0, _ := cd.RegisterTx(0)
	rw1, _ := cd.RegisterTx(1)

	rw0.ReadAccount(cdAddr1)
	rw0.WriteStorage(cdAddr2, cdSlot1)
	rw1.WriteAccount(cdAddr3)

	addrs := cd.TouchedAddresses()
	if len(addrs) != 3 {
		t.Errorf("expected 3 touched addresses, got %d", len(addrs))
	}
	for _, addr := range []types.Address{cdAddr1, cdAddr2, cdAddr3} {
		if _, ok := addrs[addr]; !ok {
			t.Errorf("expected address %s in touched set", addr.Hex())
		}
	}
}

func TestConflictDetectorTxReadWriteSetCounts(t *testing.T) {
	rw := newTxReadWriteSet(0)

	rw.ReadAccount(cdAddr1)
	rw.ReadAccount(cdAddr2)
	rw.WriteAccount(cdAddr3)
	rw.ReadStorage(cdAddr1, cdSlot1)
	rw.ReadStorage(cdAddr1, cdSlot2)
	rw.WriteStorage(cdAddr2, cdSlot3)

	if rw.AccountReadCount() != 2 {
		t.Errorf("expected 2 account reads, got %d", rw.AccountReadCount())
	}
	if rw.AccountWriteCount() != 1 {
		t.Errorf("expected 1 account write, got %d", rw.AccountWriteCount())
	}
	if rw.StorageReadCount() != 2 {
		t.Errorf("expected 2 storage reads, got %d", rw.StorageReadCount())
	}
	if rw.StorageWriteCount() != 1 {
		t.Errorf("expected 1 storage write, got %d", rw.StorageWriteCount())
	}
}

func TestConflictDetectorConflictKindStrings(t *testing.T) {
	tests := []struct {
		kind ConflictKind
		want string
	}{
		{ConflictWriteWrite, "write-write"},
		{ConflictReadWrite, "read-write"},
		{ConflictWriteRead, "write-read"},
		{ConflictKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("ConflictKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestConflictDetectorLocationKindStrings(t *testing.T) {
	tests := []struct {
		kind LocationKind
		want string
	}{
		{LocationAccount, "account"},
		{LocationStorage, "storage"},
		{LocationKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("LocationKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestConflictDetectorConflictString(t *testing.T) {
	c := Conflict{
		Kind: ConflictWriteWrite,
		Location: ConflictLocation{
			Kind:    LocationAccount,
			Address: cdAddr1,
		},
		TxA: 0,
		TxB: 1,
	}
	s := c.String()
	if s == "" {
		t.Error("expected non-empty conflict string")
	}
}

func TestConflictDetectorLocationString(t *testing.T) {
	loc1 := ConflictLocation{Kind: LocationAccount, Address: cdAddr1}
	if s := loc1.String(); s == "" {
		t.Error("expected non-empty location string for account")
	}

	loc2 := ConflictLocation{Kind: LocationStorage, Address: cdAddr1, Slot: cdSlot1}
	if s := loc2.String(); s == "" {
		t.Error("expected non-empty location string for storage")
	}
}

func TestConflictDetectorEmptyDetect(t *testing.T) {
	cd := NewConflictDetector()

	result := cd.Detect()
	if result.HasConflicts {
		t.Error("expected no conflicts with empty detector")
	}
	if result.TxCount != 0 {
		t.Errorf("expected 0 tx count, got %d", result.TxCount)
	}
}
