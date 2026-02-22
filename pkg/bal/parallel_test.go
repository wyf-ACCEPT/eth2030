package bal

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestEmptyBALParallelSets(t *testing.T) {
	bal := NewBlockAccessList()
	groups := ComputeParallelSets(bal)
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups for empty BAL, got %d", len(groups))
	}
}

func TestNilBALParallelSets(t *testing.T) {
	groups := ComputeParallelSets(nil)
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups for nil BAL, got %d", len(groups))
	}
}

func TestTwoIndependentTxs(t *testing.T) {
	bal := NewBlockAccessList()

	// Tx 0 accesses address 0x1111, slot 0x01
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x1111"),
		AccessIndex: 1, // tx index 0
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})

	// Tx 1 accesses address 0x2222, slot 0x02
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x2222"),
		AccessIndex: 2, // tx index 1
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x02"), Value: types.HexToHash("0xbb")},
		},
	})

	groups := ComputeParallelSets(bal)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group (full parallel), got %d groups", len(groups))
	}
	if len(groups[0].TxIndices) != 2 {
		t.Fatalf("expected 2 txs in group, got %d", len(groups[0].TxIndices))
	}
}

func TestTwoConflictingTxs(t *testing.T) {
	bal := NewBlockAccessList()
	addr := types.HexToAddress("0x1111")
	slot := types.HexToHash("0x01")

	// Tx 0 reads slot
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: slot, Value: types.HexToHash("0xaa")},
		},
	})

	// Tx 1 writes same slot
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 2,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0xaa"), NewValue: types.HexToHash("0xbb")},
		},
	})

	groups := ComputeParallelSets(bal)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups (serial), got %d groups", len(groups))
	}
}

func TestThreeTxsPartialConflicts(t *testing.T) {
	bal := NewBlockAccessList()
	slot := types.HexToHash("0x01")
	addr := types.HexToAddress("0x1111")

	// Tx 0 writes to addr/slot
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x01")},
		},
	})

	// Tx 1 reads from addr/slot (conflicts with tx 0)
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 2,
		StorageReads: []StorageAccess{
			{Slot: slot, Value: types.HexToHash("0x01")},
		},
	})

	// Tx 2 accesses different address (no conflict)
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x2222"),
		AccessIndex: 3,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x02"), Value: types.HexToHash("0xcc")},
		},
	})

	groups := ComputeParallelSets(bal)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// One group should have 2 txs (tx0 + tx2 or tx1 + tx2), other should have 1 tx.
	sizes := map[int]int{}
	for _, g := range groups {
		sizes[len(g.TxIndices)]++
	}
	if sizes[2] != 1 || sizes[1] != 1 {
		t.Fatalf("expected one group of 2 and one of 1, got sizes: %v", sizes)
	}
}

func TestWriteWriteConflict(t *testing.T) {
	bal := NewBlockAccessList()
	addr := types.HexToAddress("0x1111")
	slot := types.HexToHash("0x01")

	// Tx 0 writes slot
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 1,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x00"), NewValue: types.HexToHash("0x01")},
		},
	})

	// Tx 1 also writes same slot
	bal.AddEntry(AccessEntry{
		Address:     addr,
		AccessIndex: 2,
		StorageChanges: []StorageChange{
			{Slot: slot, OldValue: types.HexToHash("0x01"), NewValue: types.HexToHash("0x02")},
		},
	})

	groups := ComputeParallelSets(bal)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups for write-write conflict, got %d", len(groups))
	}
}

func TestMaxParallelismEmpty(t *testing.T) {
	bal := NewBlockAccessList()
	if MaxParallelism(bal) != 0 {
		t.Fatal("expected 0 parallelism for empty BAL")
	}
}

func TestMaxParallelismIndependent(t *testing.T) {
	bal := NewBlockAccessList()
	for i := uint64(1); i <= 4; i++ {
		bal.AddEntry(AccessEntry{
			Address:     types.BytesToAddress([]byte{byte(i)}),
			AccessIndex: i,
			StorageReads: []StorageAccess{
				{Slot: types.BytesToHash([]byte{byte(i)}), Value: types.HexToHash("0x01")},
			},
		})
	}

	p := MaxParallelism(bal)
	if p != 4 {
		t.Fatalf("expected max parallelism 4, got %d", p)
	}
}

func TestPreExecEntriesIgnored(t *testing.T) {
	bal := NewBlockAccessList()

	// Pre-execution entry (AccessIndex=0) should be ignored
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x1111"),
		AccessIndex: 0,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})

	groups := ComputeParallelSets(bal)
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups (pre-exec entries ignored), got %d", len(groups))
	}
}

func TestBalanceConflict(t *testing.T) {
	bal := NewBlockAccessList()
	addr := types.HexToAddress("0x1111")

	// Tx 0 changes balance
	bal.AddEntry(AccessEntry{
		Address:       addr,
		AccessIndex:   1,
		BalanceChange: &BalanceChange{OldValue: nil, NewValue: nil},
	})

	// Tx 1 also changes balance of same address
	bal.AddEntry(AccessEntry{
		Address:       addr,
		AccessIndex:   2,
		BalanceChange: &BalanceChange{OldValue: nil, NewValue: nil},
	})

	groups := ComputeParallelSets(bal)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups for balance conflict, got %d", len(groups))
	}
}
