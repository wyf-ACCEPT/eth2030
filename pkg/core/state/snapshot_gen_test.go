package state

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func addrFromByte(b byte) types.Address {
	var a types.Address
	a[types.AddressLength-1] = b
	return a
}

func hashFromByte2(b byte) types.Hash {
	var h types.Hash
	h[types.HashLength-1] = b
	return h
}

func makeAccountSnap(b byte, balance int64) AccountSnapshot {
	return AccountSnapshot{
		Address:  addrFromByte(b),
		Balance:  big.NewInt(balance),
		Nonce:    uint64(b),
		CodeHash: types.EmptyCodeHash,
		Root:     types.EmptyRootHash,
	}
}

func TestNewSnapshotGenerator(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	if sg == nil {
		t.Fatal("expected non-nil generator")
	}
	if sg.AccountCount() != 0 {
		t.Fatalf("expected 0 accounts, got %d", sg.AccountCount())
	}
	if sg.GenerateProgress() != 0 {
		t.Fatalf("expected 0 progress, got %f", sg.GenerateProgress())
	}
}

func TestAddAndGetAccount(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})

	snap := makeAccountSnap(0x01, 1000)
	if err := sg.AddAccount(snap); err != nil {
		t.Fatal(err)
	}
	if sg.AccountCount() != 1 {
		t.Fatalf("expected 1 account, got %d", sg.AccountCount())
	}

	got := sg.GetAccount(addrFromByte(0x01))
	if got == nil {
		t.Fatal("expected to find account")
	}
	if got.Balance.Int64() != 1000 {
		t.Fatalf("expected balance 1000, got %d", got.Balance.Int64())
	}
	if got.Nonce != 1 {
		t.Fatalf("expected nonce 1, got %d", got.Nonce)
	}
}

func TestGetAccountNotFound(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	got := sg.GetAccount(addrFromByte(0xFF))
	if got != nil {
		t.Fatal("expected nil for missing account")
	}
}

func TestAddAccountUpdatesExisting(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	addr := addrFromByte(0x01)

	snap1 := AccountSnapshot{
		Address: addr,
		Balance: big.NewInt(100),
		Nonce:   1,
	}
	sg.AddAccount(snap1)

	snap2 := AccountSnapshot{
		Address: addr,
		Balance: big.NewInt(200),
		Nonce:   2,
	}
	sg.AddAccount(snap2)

	if sg.AccountCount() != 1 {
		t.Fatalf("expected 1 account after update, got %d", sg.AccountCount())
	}
	got := sg.GetAccount(addr)
	if got.Balance.Int64() != 200 {
		t.Fatalf("expected updated balance 200, got %d", got.Balance.Int64())
	}
	if got.Nonce != 2 {
		t.Fatalf("expected updated nonce 2, got %d", got.Nonce)
	}
}

func TestAddAccountNilBalance(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	snap := AccountSnapshot{
		Address: addrFromByte(0x01),
		Balance: nil,
		Nonce:   5,
	}
	if err := sg.AddAccount(snap); err != nil {
		t.Fatal(err)
	}
	got := sg.GetAccount(addrFromByte(0x01))
	if got == nil {
		t.Fatal("expected account")
	}
	if got.Balance == nil || got.Balance.Sign() != 0 {
		t.Fatalf("expected zero balance for nil input, got %v", got.Balance)
	}
}

func TestMaxAccountsLimit(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{MaxAccounts: 2})

	if err := sg.AddAccount(makeAccountSnap(0x01, 100)); err != nil {
		t.Fatal(err)
	}
	if err := sg.AddAccount(makeAccountSnap(0x02, 200)); err != nil {
		t.Fatal(err)
	}

	// Third account should fail.
	err := sg.AddAccount(makeAccountSnap(0x03, 300))
	if err == nil {
		t.Fatal("expected error when exceeding MaxAccounts")
	}
	if sg.AccountCount() != 2 {
		t.Fatalf("expected 2 accounts, got %d", sg.AccountCount())
	}
}

func TestMaxAccountsAllowsUpdate(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{MaxAccounts: 1})

	if err := sg.AddAccount(makeAccountSnap(0x01, 100)); err != nil {
		t.Fatal(err)
	}
	// Updating the same address should succeed even at limit.
	updated := makeAccountSnap(0x01, 999)
	if err := sg.AddAccount(updated); err != nil {
		t.Fatalf("expected update to succeed at limit, got %v", err)
	}
	got := sg.GetAccount(addrFromByte(0x01))
	if got.Balance.Int64() != 999 {
		t.Fatalf("expected updated balance 999, got %d", got.Balance.Int64())
	}
}

func TestAddAndGetStorage(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	addr := addrFromByte(0x01)
	key := hashFromByte2(0xAA)
	val := hashFromByte2(0xBB)

	sg.AddStorage(addr, key, val)

	got := sg.GetStorage(addr, key)
	if got != val {
		t.Fatalf("storage mismatch: got %v, want %v", got, val)
	}
}

func TestGetStorageNotFound(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	got := sg.GetStorage(addrFromByte(0x01), hashFromByte2(0xAA))
	if got != (types.Hash{}) {
		t.Fatalf("expected zero hash for missing storage, got %v", got)
	}
}

func TestStorageCount(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	addr := addrFromByte(0x01)

	if sg.StorageCount(addr) != 0 {
		t.Fatalf("expected 0 storage for fresh address, got %d", sg.StorageCount(addr))
	}

	sg.AddStorage(addr, hashFromByte2(0x01), hashFromByte2(0x10))
	sg.AddStorage(addr, hashFromByte2(0x02), hashFromByte2(0x20))
	sg.AddStorage(addr, hashFromByte2(0x03), hashFromByte2(0x30))

	if sg.StorageCount(addr) != 3 {
		t.Fatalf("expected 3 storage entries, got %d", sg.StorageCount(addr))
	}

	// Different address should have 0.
	if sg.StorageCount(addrFromByte(0xFF)) != 0 {
		t.Fatal("expected 0 storage for different address")
	}
}

func TestStorageUpdateOverwrites(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	addr := addrFromByte(0x01)
	key := hashFromByte2(0xAA)

	sg.AddStorage(addr, key, hashFromByte2(0x01))
	sg.AddStorage(addr, key, hashFromByte2(0x02))

	got := sg.GetStorage(addr, key)
	if got != hashFromByte2(0x02) {
		t.Fatalf("expected overwritten value, got %v", got)
	}
	if sg.StorageCount(addr) != 1 {
		t.Fatalf("expected 1 storage entry after overwrite, got %d", sg.StorageCount(addr))
	}
}

func TestComputeRoot(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})

	// Empty snapshot should return EmptyRootHash.
	root := sg.ComputeRoot()
	if root != types.EmptyRootHash {
		t.Fatalf("expected empty root hash, got %v", root)
	}

	// Add accounts and verify root changes.
	sg.AddAccount(makeAccountSnap(0x01, 100))
	root1 := sg.ComputeRoot()
	if root1 == types.EmptyRootHash {
		t.Fatal("expected non-empty root after adding account")
	}
	if root1.IsZero() {
		t.Fatal("expected non-zero root")
	}

	// Adding another account should produce a different root.
	sg.AddAccount(makeAccountSnap(0x02, 200))
	root2 := sg.ComputeRoot()
	if root2 == root1 {
		t.Fatal("expected different root after adding second account")
	}
}

func TestComputeRootDeterministic(t *testing.T) {
	// Two generators with the same accounts should produce the same root.
	sg1 := NewSnapshotGenerator(SnapshotGenConfig{})
	sg2 := NewSnapshotGenerator(SnapshotGenConfig{})

	// Add accounts in different order.
	sg1.AddAccount(makeAccountSnap(0x01, 100))
	sg1.AddAccount(makeAccountSnap(0x02, 200))

	sg2.AddAccount(makeAccountSnap(0x02, 200))
	sg2.AddAccount(makeAccountSnap(0x01, 100))

	if sg1.ComputeRoot() != sg2.ComputeRoot() {
		t.Fatal("expected same root regardless of insertion order")
	}
}

func TestGenerateProgress(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})

	if sg.GenerateProgress() != 0 {
		t.Fatalf("expected initial progress 0, got %f", sg.GenerateProgress())
	}

	sg.SetProgress(0.5)
	if sg.GenerateProgress() != 0.5 {
		t.Fatalf("expected progress 0.5, got %f", sg.GenerateProgress())
	}

	sg.SetProgress(1.0)
	if sg.GenerateProgress() != 1.0 {
		t.Fatalf("expected progress 1.0, got %f", sg.GenerateProgress())
	}
}

func TestGenerateProgressClamp(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})

	sg.SetProgress(-0.5)
	if sg.GenerateProgress() != 0 {
		t.Fatalf("expected clamped progress 0, got %f", sg.GenerateProgress())
	}

	sg.SetProgress(1.5)
	if sg.GenerateProgress() != 1.0 {
		t.Fatalf("expected clamped progress 1.0, got %f", sg.GenerateProgress())
	}
}

func TestExport(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	sg.AddAccount(makeAccountSnap(0x03, 300))
	sg.AddAccount(makeAccountSnap(0x01, 100))
	sg.AddAccount(makeAccountSnap(0x02, 200))

	exported := sg.Export()
	if len(exported) != 3 {
		t.Fatalf("expected 3 exported accounts, got %d", len(exported))
	}

	// Verify sorted order by address.
	for i := 1; i < len(exported); i++ {
		if compareAddresses(exported[i-1].Address, exported[i].Address) >= 0 {
			t.Fatal("exported accounts not sorted by address")
		}
	}

	// Verify values.
	if exported[0].Balance.Int64() != 100 {
		t.Fatalf("expected first exported balance 100, got %d", exported[0].Balance.Int64())
	}
}

func TestExportEmpty(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	exported := sg.Export()
	if len(exported) != 0 {
		t.Fatalf("expected 0 exported accounts, got %d", len(exported))
	}
}

func TestExportReturnsCopies(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	sg.AddAccount(makeAccountSnap(0x01, 100))

	exported := sg.Export()
	// Mutate the exported value.
	exported[0].Balance.SetInt64(999)

	// Original should be unchanged.
	got := sg.GetAccount(addrFromByte(0x01))
	if got.Balance.Int64() != 100 {
		t.Fatalf("expected original balance 100, got %d (mutation leaked)", got.Balance.Int64())
	}
}

func TestGetAccountReturnsCopy(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	sg.AddAccount(makeAccountSnap(0x01, 100))

	got := sg.GetAccount(addrFromByte(0x01))
	got.Balance.SetInt64(999)

	// Internal state should not be affected.
	got2 := sg.GetAccount(addrFromByte(0x01))
	if got2.Balance.Int64() != 100 {
		t.Fatalf("expected original balance 100 after external mutation, got %d", got2.Balance.Int64())
	}
}

func TestAddAccountReturnsCopy(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	snap := makeAccountSnap(0x01, 100)
	sg.AddAccount(snap)

	// Mutate original after adding.
	snap.Balance.SetInt64(999)

	got := sg.GetAccount(addrFromByte(0x01))
	if got.Balance.Int64() != 100 {
		t.Fatalf("expected 100 (defensive copy), got %d", got.Balance.Int64())
	}
}

func TestConcurrentSnapshotAccess(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})

	var wg sync.WaitGroup
	// Concurrent account writers.
	for g := byte(0); g < 4; g++ {
		wg.Add(1)
		go func(offset byte) {
			defer wg.Done()
			for i := byte(0); i < 25; i++ {
				snap := makeAccountSnap(offset*25+i, int64(i)*100)
				sg.AddAccount(snap)
			}
		}(g)
	}
	// Concurrent storage writers.
	for g := byte(0); g < 4; g++ {
		wg.Add(1)
		go func(offset byte) {
			defer wg.Done()
			addr := addrFromByte(offset)
			for i := byte(0); i < 25; i++ {
				sg.AddStorage(addr, hashFromByte2(i), hashFromByte2(i+0x80))
			}
		}(g)
	}
	// Concurrent readers.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := byte(0); i < 50; i++ {
				_ = sg.GetAccount(addrFromByte(i))
				_ = sg.GetStorage(addrFromByte(i), hashFromByte2(i))
				_ = sg.AccountCount()
				_ = sg.StorageCount(addrFromByte(i))
				_ = sg.GenerateProgress()
			}
		}()
	}
	wg.Wait()

	if sg.AccountCount() != 100 {
		t.Fatalf("expected 100 accounts, got %d", sg.AccountCount())
	}
}

func TestCompareAddresses(t *testing.T) {
	a := addrFromByte(0x01)
	b := addrFromByte(0x02)
	c := addrFromByte(0x01)

	if compareAddresses(a, b) >= 0 {
		t.Fatal("expected a < b")
	}
	if compareAddresses(b, a) <= 0 {
		t.Fatal("expected b > a")
	}
	if compareAddresses(a, c) != 0 {
		t.Fatal("expected a == c")
	}
}

func TestDefaultBatchSize(t *testing.T) {
	sg := NewSnapshotGenerator(SnapshotGenConfig{})
	if sg.config.BatchSize <= 0 {
		t.Fatal("expected positive default batch size")
	}
}
