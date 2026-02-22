package bal

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestHashDeterministic(t *testing.T) {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x1111"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})

	h1 := bal.Hash()
	h2 := bal.Hash()

	if h1 != h2 {
		t.Fatalf("hash not deterministic: %v != %v", h1, h2)
	}
	if h1.IsZero() {
		t.Fatal("hash should not be zero")
	}
}

func TestHashDiffersForDifferentBALs(t *testing.T) {
	bal1 := NewBlockAccessList()
	bal1.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x1111"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})

	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x2222"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})

	if bal1.Hash() == bal2.Hash() {
		t.Fatal("different BALs should produce different hashes")
	}
}

func TestHashEmptyBAL(t *testing.T) {
	bal := NewBlockAccessList()
	h := bal.Hash()
	if h.IsZero() {
		t.Fatal("empty BAL hash should not be zero (it's the hash of the RLP encoding)")
	}
}

func TestEncodeRLP(t *testing.T) {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x1111"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})

	encoded, err := bal.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP failed: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatal("encoded bytes should not be empty")
	}
}

func TestEncodeRLPDeterministic(t *testing.T) {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xabcd"),
		AccessIndex: 3,
		StorageChanges: []StorageChange{
			{
				Slot:     types.HexToHash("0x10"),
				OldValue: types.HexToHash("0x20"),
				NewValue: types.HexToHash("0x30"),
			},
		},
		BalanceChange: &BalanceChange{
			OldValue: big.NewInt(500),
			NewValue: big.NewInt(600),
		},
	})

	enc1, err1 := bal.EncodeRLP()
	enc2, err2 := bal.EncodeRLP()

	if err1 != nil || err2 != nil {
		t.Fatalf("EncodeRLP errors: %v, %v", err1, err2)
	}
	if len(enc1) != len(enc2) {
		t.Fatal("encoded lengths differ")
	}
	for i := range enc1 {
		if enc1[i] != enc2[i] {
			t.Fatalf("encoded bytes differ at position %d", i)
		}
	}
}

func TestHashChangesWithStorageChange(t *testing.T) {
	base := func() *BlockAccessList {
		bal := NewBlockAccessList()
		bal.AddEntry(AccessEntry{
			Address:     types.HexToAddress("0x1111"),
			AccessIndex: 1,
			StorageChanges: []StorageChange{
				{
					Slot:     types.HexToHash("0x01"),
					OldValue: types.HexToHash("0x10"),
					NewValue: types.HexToHash("0x20"),
				},
			},
		})
		return bal
	}

	bal1 := base()
	bal2 := base()
	// Modify the new value in bal2.
	bal2.Entries[0].StorageChanges[0].NewValue = types.HexToHash("0x30")

	if bal1.Hash() == bal2.Hash() {
		t.Fatal("hashes should differ when storage change values differ")
	}
}
