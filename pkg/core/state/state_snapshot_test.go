package state

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestSnapshotBaseLayer_AccountCRUD(t *testing.T) {
	root := types.BytesToHash([]byte{0x01})
	bl := NewSnapshotBaseLayer(root)

	accHash := types.BytesToHash([]byte{0xaa})
	acc := &SnapshotAccount{Nonce: 1, Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash}

	// Set and retrieve.
	bl.SetAccount(accHash, acc)
	got, err := bl.AccountData(accHash)
	if err != nil {
		t.Fatalf("AccountData: %v", err)
	}
	if got == nil || got.Nonce != 1 {
		t.Errorf("expected nonce 1, got %v", got)
	}

	// Delete by setting nil.
	bl.SetAccount(accHash, nil)
	got, err = bl.AccountData(accHash)
	if err != nil {
		t.Fatalf("AccountData after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after deletion")
	}
}

func TestSnapshotBaseLayer_StorageCRUD(t *testing.T) {
	root := types.BytesToHash([]byte{0x02})
	bl := NewSnapshotBaseLayer(root)

	accHash := types.BytesToHash([]byte{0xbb})
	slotHash := types.BytesToHash([]byte{0xcc})
	value := []byte{1, 2, 3, 4}

	// Set and retrieve.
	bl.SetStorage(accHash, slotHash, value)
	got, err := bl.StorageData(accHash, slotHash)
	if err != nil {
		t.Fatalf("StorageData: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Errorf("storage mismatch: got %x, want %x", got, value)
	}

	// Delete by setting empty.
	bl.SetStorage(accHash, slotHash, nil)
	got, err = bl.StorageData(accHash, slotHash)
	if err != nil {
		t.Fatalf("StorageData after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after storage deletion")
	}
}

func TestSnapshotBaseLayer_Stale(t *testing.T) {
	root := types.BytesToHash([]byte{0x03})
	bl := NewSnapshotBaseLayer(root)

	if bl.Stale() {
		t.Error("new base layer should not be stale")
	}

	bl.MarkStale()
	if !bl.Stale() {
		t.Error("expected stale after MarkStale")
	}

	// Reads on stale layer should return error.
	accHash := types.BytesToHash([]byte{0xdd})
	_, err := bl.AccountData(accHash)
	if err != ErrSnapLayerStale {
		t.Errorf("expected ErrSnapLayerStale, got %v", err)
	}

	_, err = bl.StorageData(accHash, types.Hash{})
	if err != ErrSnapLayerStale {
		t.Errorf("expected ErrSnapLayerStale for storage, got %v", err)
	}
}

func TestSnapshotDiffLayer_AccountLookup(t *testing.T) {
	root := types.BytesToHash([]byte{0x10})
	bl := NewSnapshotBaseLayer(root)

	baseAccHash := types.BytesToHash([]byte{0x01})
	bl.SetAccount(baseAccHash, &SnapshotAccount{Nonce: 5})

	// Create diff layer with a new account.
	diffRoot := types.BytesToHash([]byte{0x20})
	diffAccHash := types.BytesToHash([]byte{0x02})
	accounts := map[types.Hash]*SnapshotAccount{
		diffAccHash: {Nonce: 10},
	}
	dl := NewSnapshotDiffLayer(bl, diffRoot, accounts, nil)

	// Diff layer should find its own account.
	acc, err := dl.AccountData(diffAccHash)
	if err != nil {
		t.Fatalf("AccountData diff: %v", err)
	}
	if acc.Nonce != 10 {
		t.Errorf("expected nonce 10, got %d", acc.Nonce)
	}

	// Diff layer should fall through to base for base accounts.
	acc, err = dl.AccountData(baseAccHash)
	if err != nil {
		t.Fatalf("AccountData fallthrough: %v", err)
	}
	if acc.Nonce != 5 {
		t.Errorf("expected nonce 5 from base, got %d", acc.Nonce)
	}

	// Unknown account should return nil.
	acc, err = dl.AccountData(types.BytesToHash([]byte{0xff}))
	if err != nil {
		t.Fatalf("AccountData unknown: %v", err)
	}
	if acc != nil {
		t.Error("expected nil for unknown account")
	}
}

func TestSnapshotDiffLayer_AccountDeletion(t *testing.T) {
	root := types.BytesToHash([]byte{0x10})
	bl := NewSnapshotBaseLayer(root)

	accHash := types.BytesToHash([]byte{0x01})
	bl.SetAccount(accHash, &SnapshotAccount{Nonce: 1})

	// Diff layer deletes the account (nil value).
	diffRoot := types.BytesToHash([]byte{0x20})
	accounts := map[types.Hash]*SnapshotAccount{
		accHash: nil,
	}
	dl := NewSnapshotDiffLayer(bl, diffRoot, accounts, nil)

	acc, err := dl.AccountData(accHash)
	if err != nil {
		t.Fatalf("AccountData: %v", err)
	}
	if acc != nil {
		t.Error("expected nil for deleted account")
	}
}

func TestSnapshotDiffLayer_StorageLookup(t *testing.T) {
	root := types.BytesToHash([]byte{0x10})
	bl := NewSnapshotBaseLayer(root)

	accHash := types.BytesToHash([]byte{0x01})
	slotHash := types.BytesToHash([]byte{0xaa})
	bl.SetStorage(accHash, slotHash, []byte{1, 2, 3})

	// Diff layer overrides one slot and adds another.
	diffRoot := types.BytesToHash([]byte{0x20})
	newSlotHash := types.BytesToHash([]byte{0xbb})
	storage := map[types.Hash]map[types.Hash][]byte{
		accHash: {
			slotHash:    []byte{4, 5, 6},
			newSlotHash: []byte{7, 8, 9},
		},
	}
	dl := NewSnapshotDiffLayer(bl, diffRoot, nil, storage)

	// Overridden slot.
	got, err := dl.StorageData(accHash, slotHash)
	if err != nil {
		t.Fatalf("StorageData override: %v", err)
	}
	if !bytes.Equal(got, []byte{4, 5, 6}) {
		t.Errorf("expected [4,5,6], got %x", got)
	}

	// New slot.
	got, err = dl.StorageData(accHash, newSlotHash)
	if err != nil {
		t.Fatalf("StorageData new: %v", err)
	}
	if !bytes.Equal(got, []byte{7, 8, 9}) {
		t.Errorf("expected [7,8,9], got %x", got)
	}
}

func TestSnapshotDiffLayer_Stale(t *testing.T) {
	root := types.BytesToHash([]byte{0x10})
	bl := NewSnapshotBaseLayer(root)
	dl := NewSnapshotDiffLayer(bl, types.BytesToHash([]byte{0x20}), nil, nil)

	if dl.Stale() {
		t.Error("new diff layer should not be stale")
	}
	dl.MarkStale()
	if !dl.Stale() {
		t.Error("expected stale after MarkStale")
	}

	_, err := dl.AccountData(types.Hash{})
	if err != ErrSnapLayerStale {
		t.Errorf("expected ErrSnapLayerStale, got %v", err)
	}
}

func TestSnapshotDiffLayer_MemoryAndCounts(t *testing.T) {
	root := types.BytesToHash([]byte{0x10})
	bl := NewSnapshotBaseLayer(root)

	accounts := map[types.Hash]*SnapshotAccount{
		types.BytesToHash([]byte{0x01}): {Nonce: 1},
		types.BytesToHash([]byte{0x02}): {Nonce: 2},
	}
	storage := map[types.Hash]map[types.Hash][]byte{
		types.BytesToHash([]byte{0x01}): {
			types.BytesToHash([]byte{0xaa}): []byte{1},
			types.BytesToHash([]byte{0xbb}): []byte{2},
		},
	}
	dl := NewSnapshotDiffLayer(bl, types.BytesToHash([]byte{0x20}), accounts, storage)

	if dl.AccountCount() != 2 {
		t.Errorf("expected 2 accounts, got %d", dl.AccountCount())
	}
	if dl.StorageCount() != 2 {
		t.Errorf("expected 2 storage slots, got %d", dl.StorageCount())
	}
	if dl.Memory() == 0 {
		t.Error("expected non-zero memory estimate")
	}
}

func TestSnapshotTree_Update(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	if st.Size() != 1 {
		t.Errorf("expected size 1, got %d", st.Size())
	}

	// Update with a new diff layer.
	blockRoot := types.BytesToHash([]byte{0x02})
	accounts := map[types.Hash]*SnapshotAccount{
		types.BytesToHash([]byte{0xaa}): {Nonce: 42},
	}
	err := st.Update(blockRoot, baseRoot, accounts, nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if st.Size() != 2 {
		t.Errorf("expected size 2, got %d", st.Size())
	}

	// Verify the new layer is accessible.
	snap := st.Snapshot(blockRoot)
	if snap == nil {
		t.Fatal("expected snapshot for blockRoot")
	}
	acc, err := snap.AccountData(types.BytesToHash([]byte{0xaa}))
	if err != nil {
		t.Fatalf("AccountData: %v", err)
	}
	if acc.Nonce != 42 {
		t.Errorf("expected nonce 42, got %d", acc.Nonce)
	}
}

func TestSnapshotTree_UpdateCycle(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	err := st.Update(baseRoot, baseRoot, nil, nil)
	if err != ErrSnapCycle {
		t.Errorf("expected ErrSnapCycle, got %v", err)
	}
}

func TestSnapshotTree_UpdateUnknownParent(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	unknownParent := types.BytesToHash([]byte{0xff})
	err := st.Update(types.BytesToHash([]byte{0x02}), unknownParent, nil, nil)
	if err != ErrSnapLayerNotFound {
		t.Errorf("expected ErrSnapLayerNotFound, got %v", err)
	}
}

func TestSnapshotTree_Flatten(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	// Build a chain: base -> diff1 -> diff2 -> diff3.
	roots := []types.Hash{
		types.BytesToHash([]byte{0x02}),
		types.BytesToHash([]byte{0x03}),
		types.BytesToHash([]byte{0x04}),
	}
	parent := baseRoot
	for i, r := range roots {
		accs := map[types.Hash]*SnapshotAccount{
			types.BytesToHash([]byte{byte(0x10 + i)}): {Nonce: uint64(i + 1)},
		}
		if err := st.Update(r, parent, accs, nil); err != nil {
			t.Fatalf("Update %d: %v", i, err)
		}
		parent = r
	}
	if st.Size() != 4 {
		t.Errorf("expected 4 layers, got %d", st.Size())
	}

	// Flatten keeping 1 diff layer. Should flatten 2 diff layers into base.
	n, err := st.Flatten(roots[2], 1)
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 flattened, got %d", n)
	}

	// After pruning stale layers.
	pruned := st.Prune()
	if pruned < 0 {
		t.Error("prune returned negative")
	}
}

func TestSnapshotTree_Prune(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	blockRoot := types.BytesToHash([]byte{0x02})
	st.Update(blockRoot, baseRoot, nil, nil)

	snap := st.Snapshot(blockRoot)
	if dl, ok := snap.(*SnapshotDiffLayer); ok {
		dl.MarkStale()
	}

	pruned := st.Prune()
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if st.Size() != 1 {
		t.Errorf("expected 1 remaining layer, got %d", st.Size())
	}
}

func TestGenerateFromTrie(t *testing.T) {
	root := types.BytesToHash([]byte{0x99})

	accHash := types.BytesToHash([]byte{0x01})
	accounts := map[types.Hash]*SnapshotAccount{
		accHash: {Nonce: 100},
	}
	slotHash := types.BytesToHash([]byte{0xaa})
	storage := map[types.Hash]map[types.Hash][]byte{
		accHash: {slotHash: []byte{42}},
	}

	base := GenerateFromTrie(root, accounts, storage)
	if base.Root() != root {
		t.Errorf("root mismatch: got %s", base.Root())
	}

	acc, err := base.AccountData(accHash)
	if err != nil {
		t.Fatalf("AccountData: %v", err)
	}
	if acc.Nonce != 100 {
		t.Errorf("expected nonce 100, got %d", acc.Nonce)
	}

	val, err := base.StorageData(accHash, slotHash)
	if err != nil {
		t.Fatalf("StorageData: %v", err)
	}
	if !bytes.Equal(val, []byte{42}) {
		t.Errorf("expected [42], got %x", val)
	}
}

func TestRecoverSnapshot(t *testing.T) {
	root := types.BytesToHash([]byte{0x01})
	base := NewSnapshotBaseLayer(root)

	acc1 := types.BytesToHash([]byte{0x01})
	acc2 := types.BytesToHash([]byte{0x02})
	acc3 := types.BytesToHash([]byte{0x03})
	base.SetAccount(acc1, &SnapshotAccount{Nonce: 1})
	base.SetAccount(acc2, &SnapshotAccount{Nonce: 2})
	base.SetAccount(acc3, &SnapshotAccount{Nonce: 3})
	base.SetStorage(acc2, types.BytesToHash([]byte{0xaa}), []byte{1})

	// Only acc1 and acc3 are valid.
	valid := map[types.Hash]struct{}{
		acc1: {},
		acc3: {},
	}
	removed := RecoverSnapshot(base, valid)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	// acc2 should be gone.
	acc, _ := base.AccountData(acc2)
	if acc != nil {
		t.Error("acc2 should have been removed")
	}

	// acc2's storage should also be gone.
	val, _ := base.StorageData(acc2, types.BytesToHash([]byte{0xaa}))
	if val != nil {
		t.Error("acc2 storage should have been removed")
	}

	// acc1 and acc3 should remain.
	acc, _ = base.AccountData(acc1)
	if acc == nil || acc.Nonce != 1 {
		t.Error("acc1 should still exist")
	}
	acc, _ = base.AccountData(acc3)
	if acc == nil || acc.Nonce != 3 {
		t.Error("acc3 should still exist")
	}
}

func TestSnapshotAccount_IsEmpty(t *testing.T) {
	// Empty account.
	sa := &SnapshotAccount{}
	if !sa.IsEmpty() {
		t.Error("zero-valued account should be empty")
	}

	// Account with empty root and empty code hash.
	sa = &SnapshotAccount{Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash}
	if !sa.IsEmpty() {
		t.Error("account with only empty root and code hash should be empty")
	}

	// Non-empty nonce.
	sa = &SnapshotAccount{Nonce: 1}
	if sa.IsEmpty() {
		t.Error("account with nonce > 0 should not be empty")
	}

	// Non-zero balance.
	sa = &SnapshotAccount{Balance: [32]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}}
	if sa.IsEmpty() {
		t.Error("account with non-zero balance should not be empty")
	}

	// Non-empty code hash.
	sa = &SnapshotAccount{CodeHash: types.BytesToHash([]byte{0xff})}
	if sa.IsEmpty() {
		t.Error("account with code hash should not be empty")
	}
}

func TestHashAddress(t *testing.T) {
	addr := types.Address{0x01, 0x02, 0x03}
	h1 := HashAddress(addr)
	h2 := HashAddress(addr)
	if h1 != h2 {
		t.Error("same address should produce same hash")
	}
	if h1 == (types.Hash{}) {
		t.Error("hash should not be zero")
	}

	// Different addresses should produce different hashes.
	addr2 := types.Address{0x04, 0x05, 0x06}
	h3 := HashAddress(addr2)
	if h1 == h3 {
		t.Error("different addresses should produce different hashes")
	}
}

func TestHashStorageKey(t *testing.T) {
	key := types.BytesToHash([]byte{0xaa, 0xbb})
	h1 := HashStorageKey(key)
	h2 := HashStorageKey(key)
	if h1 != h2 {
		t.Error("same key should produce same hash")
	}
	if h1 == (types.Hash{}) {
		t.Error("hash should not be zero")
	}
}

func TestSnapshotDiffLayer_LayeredLookup(t *testing.T) {
	// Build a 3-layer chain: base -> diff1 -> diff2.
	baseRoot := types.BytesToHash([]byte{0x01})
	bl := NewSnapshotBaseLayer(baseRoot)

	accHash := types.BytesToHash([]byte{0xaa})
	bl.SetAccount(accHash, &SnapshotAccount{Nonce: 1})

	// diff1 updates nonce to 2.
	diff1Root := types.BytesToHash([]byte{0x02})
	dl1 := NewSnapshotDiffLayer(bl, diff1Root, map[types.Hash]*SnapshotAccount{
		accHash: {Nonce: 2},
	}, nil)

	// diff2 does not modify accHash, should fall through to diff1.
	diff2Root := types.BytesToHash([]byte{0x03})
	otherAcc := types.BytesToHash([]byte{0xbb})
	dl2 := NewSnapshotDiffLayer(dl1, diff2Root, map[types.Hash]*SnapshotAccount{
		otherAcc: {Nonce: 99},
	}, nil)

	// Should get nonce 2 from diff1 (not 1 from base).
	acc, err := dl2.AccountData(accHash)
	if err != nil {
		t.Fatalf("AccountData: %v", err)
	}
	if acc.Nonce != 2 {
		t.Errorf("expected nonce 2 from diff1, got %d", acc.Nonce)
	}

	// Other account should be found in diff2.
	acc, err = dl2.AccountData(otherAcc)
	if err != nil {
		t.Fatalf("AccountData other: %v", err)
	}
	if acc.Nonce != 99 {
		t.Errorf("expected nonce 99, got %d", acc.Nonce)
	}
}

func TestSnapshotBaseLayer_Root(t *testing.T) {
	root := types.BytesToHash([]byte{0x42})
	bl := NewSnapshotBaseLayer(root)
	if bl.Root() != root {
		t.Errorf("root mismatch: got %s, want %s", bl.Root(), root)
	}
}

func TestSnapshotDiffLayer_Root(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	bl := NewSnapshotBaseLayer(baseRoot)
	diffRoot := types.BytesToHash([]byte{0x02})
	dl := NewSnapshotDiffLayer(bl, diffRoot, nil, nil)
	if dl.Root() != diffRoot {
		t.Errorf("diff root mismatch: got %s, want %s", dl.Root(), diffRoot)
	}
}

func TestSnapshotDiffLayer_Parent(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	bl := NewSnapshotBaseLayer(baseRoot)
	dl := NewSnapshotDiffLayer(bl, types.BytesToHash([]byte{0x02}), nil, nil)
	parent := dl.Parent()
	if parent != bl {
		t.Error("parent should be the base layer")
	}
}

func TestSnapshotTree_SnapshotNotFound(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	snap := st.Snapshot(types.BytesToHash([]byte{0xff}))
	if snap != nil {
		t.Error("expected nil for unknown root")
	}
}

func TestSnapshotTree_FlattenUnknownRoot(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	_, err := st.Flatten(types.BytesToHash([]byte{0xff}), 0)
	if err != ErrSnapLayerNotFound {
		t.Errorf("expected ErrSnapLayerNotFound, got %v", err)
	}
}

func TestSnapshotTree_FlattenBaseOnly(t *testing.T) {
	baseRoot := types.BytesToHash([]byte{0x01})
	st := NewSnapshotTree(baseRoot)

	// Flattening when there are no diff layers should do nothing.
	n, err := st.Flatten(baseRoot, 0)
	if err != nil {
		t.Fatalf("Flatten: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 flattened, got %d", n)
	}
}
