package state

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func addr(b byte) types.Address {
	return types.BytesToAddress([]byte{b})
}

func hash(b byte) types.Hash {
	return types.BytesToHash([]byte{b})
}

func TestNewStateDiffBuilderEmpty(t *testing.T) {
	b := NewStateDiffBuilder(100, hash(0xaa))
	if !b.IsEmpty() {
		t.Error("new builder should be empty")
	}
	diff := b.Build()
	if diff.BlockNumber != 100 {
		t.Errorf("expected block 100, got %d", diff.BlockNumber)
	}
	if diff.BlockHash != hash(0xaa) {
		t.Error("block hash mismatch")
	}
	if len(diff.AccountDiffs) != 0 {
		t.Errorf("expected 0 account diffs, got %d", len(diff.AccountDiffs))
	}
}

func TestRecordBalanceChange(t *testing.T) {
	b := NewStateDiffBuilder(1, hash(0x01))
	b.RecordBalanceChange(addr(0x10), big.NewInt(100), big.NewInt(200))

	if b.IsEmpty() {
		t.Error("builder should not be empty after recording change")
	}

	diff := b.Build()
	if len(diff.AccountDiffs) != 1 {
		t.Fatalf("expected 1 account diff, got %d", len(diff.AccountDiffs))
	}

	ad := diff.AccountDiffs[0]
	if ad.Address != addr(0x10) {
		t.Errorf("address mismatch: got %s", ad.Address.Hex())
	}
	if ad.BalanceChange == nil {
		t.Fatal("BalanceChange is nil")
	}
	if ad.BalanceChange.From.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("From balance: expected 100, got %s", ad.BalanceChange.From)
	}
	if ad.BalanceChange.To.Cmp(big.NewInt(200)) != 0 {
		t.Errorf("To balance: expected 200, got %s", ad.BalanceChange.To)
	}

	// Other fields should be nil.
	if ad.NonceChange != nil {
		t.Error("NonceChange should be nil")
	}
	if ad.CodeChange != nil {
		t.Error("CodeChange should be nil")
	}
	if len(ad.StorageChanges) != 0 {
		t.Error("StorageChanges should be empty")
	}
}

func TestRecordNonceChange(t *testing.T) {
	b := NewStateDiffBuilder(2, hash(0x02))
	b.RecordNonceChange(addr(0x20), 5, 6)

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if ad.NonceChange == nil {
		t.Fatal("NonceChange is nil")
	}
	if ad.NonceChange.From != 5 || ad.NonceChange.To != 6 {
		t.Errorf("nonce change: expected 5->6, got %d->%d",
			ad.NonceChange.From, ad.NonceChange.To)
	}
}

func TestRecordCodeChange(t *testing.T) {
	b := NewStateDiffBuilder(3, hash(0x03))
	oldCode := []byte{0x60, 0x00}
	newCode := []byte{0x60, 0x01, 0x60, 0x00}
	b.RecordCodeChange(addr(0x30), oldCode, newCode)

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if ad.CodeChange == nil {
		t.Fatal("CodeChange is nil")
	}
	if len(ad.CodeChange.From) != 2 || ad.CodeChange.From[1] != 0x00 {
		t.Error("From code mismatch")
	}
	if len(ad.CodeChange.To) != 4 {
		t.Errorf("To code length: expected 4, got %d", len(ad.CodeChange.To))
	}
}

func TestCodeChangeMakesCopy(t *testing.T) {
	b := NewStateDiffBuilder(3, hash(0x03))
	oldCode := []byte{0x60, 0x00}
	newCode := []byte{0x60, 0x01}
	b.RecordCodeChange(addr(0x30), oldCode, newCode)

	// Mutate the original slices.
	oldCode[0] = 0xff
	newCode[0] = 0xfe

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if ad.CodeChange.From[0] != 0x60 {
		t.Error("from code should not be affected by external mutation")
	}
	if ad.CodeChange.To[0] != 0x60 {
		t.Error("to code should not be affected by external mutation")
	}
}

func TestRecordStorageChange(t *testing.T) {
	b := NewStateDiffBuilder(4, hash(0x04))
	b.RecordStorageChange(addr(0x40), hash(0x01), hash(0xaa), hash(0xbb))
	b.RecordStorageChange(addr(0x40), hash(0x02), hash(0xcc), hash(0xdd))

	diff := b.Build()
	if len(diff.AccountDiffs) != 1 {
		t.Fatalf("expected 1 account diff, got %d", len(diff.AccountDiffs))
	}

	ad := diff.AccountDiffs[0]
	if len(ad.StorageChanges) != 2 {
		t.Fatalf("expected 2 storage changes, got %d", len(ad.StorageChanges))
	}

	// Should be sorted by key.
	if ad.StorageChanges[0].Key != hash(0x01) {
		t.Error("first storage change key should be 0x01")
	}
	if ad.StorageChanges[1].Key != hash(0x02) {
		t.Error("second storage change key should be 0x02")
	}
}

func TestStorageChangeOverwrite(t *testing.T) {
	b := NewStateDiffBuilder(5, hash(0x05))
	b.RecordStorageChange(addr(0x50), hash(0x01), hash(0xaa), hash(0xbb))
	// Overwrite with new values for the same key.
	b.RecordStorageChange(addr(0x50), hash(0x01), hash(0xcc), hash(0xdd))

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if len(ad.StorageChanges) != 1 {
		t.Fatalf("expected 1 storage change after overwrite, got %d", len(ad.StorageChanges))
	}
	if ad.StorageChanges[0].From != hash(0xcc) {
		t.Error("overwritten storage change should use latest From value")
	}
	if ad.StorageChanges[0].To != hash(0xdd) {
		t.Error("overwritten storage change should use latest To value")
	}
}

func TestMultipleAccounts(t *testing.T) {
	b := NewStateDiffBuilder(10, hash(0x10))
	b.RecordBalanceChange(addr(0x03), big.NewInt(0), big.NewInt(100))
	b.RecordBalanceChange(addr(0x01), big.NewInt(0), big.NewInt(200))
	b.RecordNonceChange(addr(0x02), 0, 1)

	diff := b.Build()
	if len(diff.AccountDiffs) != 3 {
		t.Fatalf("expected 3 account diffs, got %d", len(diff.AccountDiffs))
	}

	// Should be sorted by address.
	if diff.AccountDiffs[0].Address != addr(0x01) {
		t.Error("first account should be 0x01")
	}
	if diff.AccountDiffs[1].Address != addr(0x02) {
		t.Error("second account should be 0x02")
	}
	if diff.AccountDiffs[2].Address != addr(0x03) {
		t.Error("third account should be 0x03")
	}
}

func TestAffectedAddresses(t *testing.T) {
	b := NewStateDiffBuilder(20, hash(0x20))
	b.RecordBalanceChange(addr(0x03), big.NewInt(0), big.NewInt(1))
	b.RecordNonceChange(addr(0x01), 0, 1)

	addrs := b.AffectedAddresses()
	if len(addrs) != 2 {
		t.Fatalf("expected 2 affected addresses, got %d", len(addrs))
	}
	if addrs[0] != addr(0x01) {
		t.Error("first affected address should be 0x01")
	}
	if addrs[1] != addr(0x03) {
		t.Error("second affected address should be 0x03")
	}
}

func TestAffectedAddressesEmpty(t *testing.T) {
	b := NewStateDiffBuilder(0, types.Hash{})
	addrs := b.AffectedAddresses()
	if len(addrs) != 0 {
		t.Errorf("expected 0 affected addresses, got %d", len(addrs))
	}
}

func TestMultipleChangesPerAccount(t *testing.T) {
	b := NewStateDiffBuilder(30, hash(0x30))
	a := addr(0x55)
	b.RecordBalanceChange(a, big.NewInt(100), big.NewInt(50))
	b.RecordNonceChange(a, 10, 11)
	b.RecordCodeChange(a, nil, []byte{0x60, 0x00})
	b.RecordStorageChange(a, hash(0x01), hash(0x00), hash(0xff))

	diff := b.Build()
	if len(diff.AccountDiffs) != 1 {
		t.Fatalf("expected 1 account diff, got %d", len(diff.AccountDiffs))
	}

	ad := diff.AccountDiffs[0]
	if ad.BalanceChange == nil {
		t.Error("expected balance change")
	}
	if ad.NonceChange == nil {
		t.Error("expected nonce change")
	}
	if ad.CodeChange == nil {
		t.Error("expected code change")
	}
	if len(ad.StorageChanges) != 1 {
		t.Error("expected 1 storage change")
	}
}

func TestBalanceChangeCopiesValues(t *testing.T) {
	b := NewStateDiffBuilder(40, hash(0x40))
	from := big.NewInt(100)
	to := big.NewInt(200)
	b.RecordBalanceChange(addr(0x60), from, to)

	// Mutate original values.
	from.SetInt64(999)
	to.SetInt64(888)

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if ad.BalanceChange.From.Cmp(big.NewInt(100)) != 0 {
		t.Error("From balance should be 100, not affected by mutation")
	}
	if ad.BalanceChange.To.Cmp(big.NewInt(200)) != 0 {
		t.Error("To balance should be 200, not affected by mutation")
	}
}

func TestConcurrentStateDiffBuilder(t *testing.T) {
	b := NewStateDiffBuilder(50, hash(0x50))
	var wg sync.WaitGroup

	// Concurrent balance changes to different addresses.
	for i := byte(0); i < 100; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			b.RecordBalanceChange(addr(idx), big.NewInt(0), big.NewInt(int64(idx)))
		}(i)
	}

	// Concurrent nonce changes.
	for i := byte(0); i < 50; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			b.RecordNonceChange(addr(idx), 0, uint64(idx))
		}(i)
	}

	// Concurrent storage changes.
	for i := byte(0); i < 30; i++ {
		wg.Add(1)
		go func(idx byte) {
			defer wg.Done()
			b.RecordStorageChange(addr(idx), hash(idx), hash(0x00), hash(idx))
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.IsEmpty()
			b.AffectedAddresses()
		}()
	}

	wg.Wait()

	diff := b.Build()
	if len(diff.AccountDiffs) != 100 {
		t.Errorf("expected 100 account diffs, got %d", len(diff.AccountDiffs))
	}
}

func TestBuildSortedStorageChanges(t *testing.T) {
	b := NewStateDiffBuilder(60, hash(0x60))
	a := addr(0x70)
	// Insert in reverse order.
	b.RecordStorageChange(a, hash(0x05), hash(0x00), hash(0x01))
	b.RecordStorageChange(a, hash(0x03), hash(0x00), hash(0x01))
	b.RecordStorageChange(a, hash(0x01), hash(0x00), hash(0x01))
	b.RecordStorageChange(a, hash(0x04), hash(0x00), hash(0x01))
	b.RecordStorageChange(a, hash(0x02), hash(0x00), hash(0x01))

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	for i := 1; i < len(ad.StorageChanges); i++ {
		if !hashLess(ad.StorageChanges[i-1].Key, ad.StorageChanges[i].Key) {
			t.Errorf("storage changes not sorted at index %d", i)
		}
	}
}

func TestHashLess(t *testing.T) {
	a := hash(0x01)
	b := hash(0x02)
	if !hashLess(a, b) {
		t.Error("0x01 should be less than 0x02")
	}
	if hashLess(b, a) {
		t.Error("0x02 should not be less than 0x01")
	}
	if hashLess(a, a) {
		t.Error("equal hashes should not be less")
	}
}

func TestEmptyCodeChange(t *testing.T) {
	b := NewStateDiffBuilder(70, hash(0x70))
	b.RecordCodeChange(addr(0x80), nil, nil)

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if ad.CodeChange == nil {
		t.Fatal("CodeChange should not be nil")
	}
	if len(ad.CodeChange.From) != 0 {
		t.Error("From code should be empty")
	}
	if len(ad.CodeChange.To) != 0 {
		t.Error("To code should be empty")
	}
}

func TestLargeBalanceValues(t *testing.T) {
	b := NewStateDiffBuilder(80, hash(0x80))

	// Use values larger than uint64 max (Ethereum balances are 256-bit).
	large := new(big.Int)
	large.SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)
	b.RecordBalanceChange(addr(0x90), big.NewInt(0), large)

	diff := b.Build()
	ad := diff.AccountDiffs[0]
	if ad.BalanceChange.To.Cmp(large) != 0 {
		t.Error("large balance value not preserved correctly")
	}
}

func TestBuildMultipleTimes(t *testing.T) {
	b := NewStateDiffBuilder(90, hash(0x90))
	b.RecordBalanceChange(addr(0xa0), big.NewInt(0), big.NewInt(100))

	diff1 := b.Build()
	diff2 := b.Build()

	if len(diff1.AccountDiffs) != len(diff2.AccountDiffs) {
		t.Error("multiple Build calls should produce consistent results")
	}
}

func TestIsEmptyAfterRecording(t *testing.T) {
	b := NewStateDiffBuilder(0, types.Hash{})
	if !b.IsEmpty() {
		t.Error("should be empty initially")
	}

	b.RecordStorageChange(addr(0x01), hash(0x01), hash(0x00), hash(0x01))
	if b.IsEmpty() {
		t.Error("should not be empty after recording a storage change")
	}
}
