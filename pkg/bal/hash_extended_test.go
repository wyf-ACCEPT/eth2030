package bal

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func testBAL() *BlockAccessList {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x1111"),
		AccessIndex: 1,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xaa")},
		},
	})
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x2222"),
		AccessIndex: 2,
		StorageChanges: []StorageChange{
			{Slot: types.HexToHash("0x02"), OldValue: types.HexToHash("0x10"), NewValue: types.HexToHash("0x20")},
		},
		BalanceChange: &BalanceChange{OldValue: big.NewInt(100), NewValue: big.NewInt(200)},
	})
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x3333"),
		AccessIndex: 3,
		NonceChange: &NonceChange{OldValue: 0, NewValue: 1},
		CodeChange:  &CodeChange{OldCode: nil, NewCode: []byte{0x60, 0x80}},
	})
	return bal
}

func TestMerkleRootDeterministic(t *testing.T) {
	bal := testBAL()
	r1 := bal.MerkleRoot()
	r2 := bal.MerkleRoot()
	if r1 != r2 {
		t.Fatal("merkle root not deterministic")
	}
	if r1.IsZero() {
		t.Fatal("merkle root should not be zero for non-empty BAL")
	}
}

func TestMerkleRootEmpty(t *testing.T) {
	bal := NewBlockAccessList()
	r := bal.MerkleRoot()
	if !r.IsZero() {
		t.Fatal("empty BAL should have zero merkle root")
	}
}

func TestMerkleRootNil(t *testing.T) {
	var bal *BlockAccessList
	r := bal.MerkleRoot()
	if !r.IsZero() {
		t.Fatal("nil BAL should have zero merkle root")
	}
}

func TestMerkleRootSingleEntry(t *testing.T) {
	bal := NewBlockAccessList()
	bal.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0xaaaa"),
		AccessIndex: 1,
	})
	r := bal.MerkleRoot()
	// Single entry: root should equal the hash of that entry.
	h := HashAccessEntry(&bal.Entries[0])
	if r != h {
		t.Fatalf("single-entry root should equal entry hash: got %v, want %v", r, h)
	}
}

func TestMerkleRootDiffersForDifferentBALs(t *testing.T) {
	bal1 := testBAL()
	bal2 := NewBlockAccessList()
	bal2.AddEntry(AccessEntry{
		Address:     types.HexToAddress("0x9999"),
		AccessIndex: 1,
	})
	if bal1.MerkleRoot() == bal2.MerkleRoot() {
		t.Fatal("different BALs should have different merkle roots")
	}
}

func TestHashAccessEntryNil(t *testing.T) {
	h := HashAccessEntry(nil)
	if !h.IsZero() {
		t.Fatal("nil entry should have zero hash")
	}
}

func TestHashAccessEntryDeterministic(t *testing.T) {
	entry := &AccessEntry{
		Address:     types.HexToAddress("0x1234"),
		AccessIndex: 5,
		StorageReads: []StorageAccess{
			{Slot: types.HexToHash("0x01"), Value: types.HexToHash("0xff")},
		},
	}
	h1 := HashAccessEntry(entry)
	h2 := HashAccessEntry(entry)
	if h1 != h2 {
		t.Fatal("entry hash not deterministic")
	}
}

func TestHashAddressSlot(t *testing.T) {
	addr := types.HexToAddress("0x1111")
	slot := types.HexToHash("0x01")
	h1 := HashAddressSlot(addr, slot)
	h2 := HashAddressSlot(addr, slot)
	if h1 != h2 {
		t.Fatal("address-slot hash not deterministic")
	}

	// Different slot should produce different hash.
	h3 := HashAddressSlot(addr, types.HexToHash("0x02"))
	if h1 == h3 {
		t.Fatal("different slots should produce different hashes")
	}
}

func TestHashConflictPair(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	slot := types.HexToHash("0xbb")
	h1 := HashConflictPair(0, 1, addr, slot)
	h2 := HashConflictPair(0, 1, addr, slot)
	if h1 != h2 {
		t.Fatal("conflict pair hash not deterministic")
	}

	// Swapped tx indices should differ.
	h3 := HashConflictPair(1, 0, addr, slot)
	if h1 == h3 {
		t.Fatal("swapped tx indices should produce different hash")
	}
}

func TestConflictSetHashEmpty(t *testing.T) {
	h := ConflictSetHash(nil)
	if !h.IsZero() {
		t.Fatal("empty conflict set should have zero hash")
	}
}

func TestConflictSetHashDeterministic(t *testing.T) {
	conflicts := []Conflict{
		{TxA: 0, TxB: 1, Type: ConflictWriteWrite, Address: types.HexToAddress("0x01")},
		{TxA: 0, TxB: 2, Type: ConflictReadWrite, Address: types.HexToAddress("0x02")},
	}
	h1 := ConflictSetHash(conflicts)
	h2 := ConflictSetHash(conflicts)
	if h1 != h2 {
		t.Fatal("conflict set hash not deterministic")
	}
}

func TestConflictSetHashOrderIndependent(t *testing.T) {
	c1 := []Conflict{
		{TxA: 0, TxB: 1, Type: ConflictWriteWrite, Address: types.HexToAddress("0x01")},
		{TxA: 0, TxB: 2, Type: ConflictReadWrite, Address: types.HexToAddress("0x02")},
	}
	c2 := []Conflict{
		{TxA: 0, TxB: 2, Type: ConflictReadWrite, Address: types.HexToAddress("0x02")},
		{TxA: 0, TxB: 1, Type: ConflictWriteWrite, Address: types.HexToAddress("0x01")},
	}
	// The function sorts internally, so order should not matter.
	if ConflictSetHash(c1) != ConflictSetHash(c2) {
		t.Fatal("conflict set hash should be order-independent")
	}
}

func TestParallelMerkleRootMatchesSerial(t *testing.T) {
	bal := testBAL()
	serial := bal.MerkleRoot()
	parallel := ParallelMerkleRoot(bal, 2)
	if serial != parallel {
		t.Fatalf("parallel root %v != serial root %v", parallel, serial)
	}
}

func TestParallelMerkleRootSingleWorker(t *testing.T) {
	bal := testBAL()
	serial := bal.MerkleRoot()
	parallel := ParallelMerkleRoot(bal, 1)
	if serial != parallel {
		t.Fatal("single-worker parallel root should match serial root")
	}
}

func TestParallelMerkleRootManyWorkers(t *testing.T) {
	bal := testBAL()
	serial := bal.MerkleRoot()
	parallel := ParallelMerkleRoot(bal, 100)
	if serial != parallel {
		t.Fatal("many-worker parallel root should match serial root")
	}
}

func TestParallelMerkleRootEmpty(t *testing.T) {
	bal := NewBlockAccessList()
	r := ParallelMerkleRoot(bal, 4)
	if !r.IsZero() {
		t.Fatal("empty BAL parallel root should be zero")
	}
}

func TestParallelMerkleRootNil(t *testing.T) {
	r := ParallelMerkleRoot(nil, 4)
	if !r.IsZero() {
		t.Fatal("nil BAL parallel root should be zero")
	}
}

func TestEntryHashes(t *testing.T) {
	bal := testBAL()
	hashes := EntryHashes(bal)
	if len(hashes) != 3 {
		t.Fatalf("expected 3 hashes, got %d", len(hashes))
	}
	for i, h := range hashes {
		if h.IsZero() {
			t.Fatalf("hash %d should not be zero", i)
		}
	}
}

func TestEntryHashesNil(t *testing.T) {
	hashes := EntryHashes(nil)
	if hashes != nil {
		t.Fatal("nil BAL should return nil hashes")
	}
}

func TestVerifyMerkleRoot(t *testing.T) {
	bal := testBAL()
	root := bal.MerkleRoot()
	if !VerifyMerkleRoot(bal, root) {
		t.Fatal("verify should return true for correct root")
	}
	if VerifyMerkleRoot(bal, types.Hash{}) {
		t.Fatal("verify should return false for zero root")
	}
}
