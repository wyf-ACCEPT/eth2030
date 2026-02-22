package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestPrefetcherAddresses(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Before prefetch, addresses should not exist.
	if db.Exist(addr1) {
		t.Fatal("addr1 should not exist before prefetch")
	}
	if db.Exist(addr2) {
		t.Fatal("addr2 should not exist before prefetch")
	}

	pf.PrefetchAddresses([]types.Address{addr1, addr2})

	// After prefetch, addresses should exist with default state.
	if !db.Exist(addr1) {
		t.Fatal("addr1 should exist after prefetch")
	}
	if !db.Exist(addr2) {
		t.Fatal("addr2 should exist after prefetch")
	}

	// Prefetched accounts should have zero balance and nonce.
	if db.GetBalance(addr1).Sign() != 0 {
		t.Fatal("prefetched account should have zero balance")
	}
	if db.GetNonce(addr1) != 0 {
		t.Fatal("prefetched account should have zero nonce")
	}
}

func TestPrefetcherDoesNotOverwriteExisting(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	// Set up existing account with state.
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1000))
	db.SetNonce(addr, 42)

	// Prefetch should not overwrite the existing state.
	pf.PrefetchAddresses([]types.Address{addr})

	if db.GetBalance(addr).Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("prefetch should not overwrite balance: got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 42 {
		t.Fatalf("prefetch should not overwrite nonce: got %d", db.GetNonce(addr))
	}
}

func TestPrefetcherIsPrefetched(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	if pf.IsPrefetched(addr) {
		t.Fatal("should not be prefetched initially")
	}

	pf.PrefetchAddresses([]types.Address{addr})

	if !pf.IsPrefetched(addr) {
		t.Fatal("should be prefetched after PrefetchAddresses")
	}
}

func TestPrefetcherStorageSlots(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	key := types.HexToHash("0x01")

	pf.PrefetchStorageSlots(addr, []types.Hash{key})

	// The address should exist after prefetching storage.
	if !db.Exist(addr) {
		t.Fatal("address should exist after prefetching storage slots")
	}
}

func TestPrefetcherTransaction(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	receiver := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	accessAddr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	pf.PrefetchTransaction(
		sender,
		&receiver,
		[]types.Address{accessAddr},
		map[types.Address][]types.Hash{
			accessAddr: {types.HexToHash("0x01")},
		},
	)

	if !db.Exist(sender) {
		t.Fatal("sender should exist after transaction prefetch")
	}
	if !db.Exist(receiver) {
		t.Fatal("receiver should exist after transaction prefetch")
	}
	if !db.Exist(accessAddr) {
		t.Fatal("access list address should exist after transaction prefetch")
	}
}

func TestPrefetcherTransactionNilReceiver(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	// nil receiver represents contract creation.
	pf.PrefetchTransaction(sender, nil, nil, nil)

	if !db.Exist(sender) {
		t.Fatal("sender should exist after transaction prefetch")
	}
}

func TestPrefetcherEmptyList(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewStatePrefetcher(db)

	// Should be a no-op, no panic.
	pf.PrefetchAddresses(nil)
	pf.PrefetchAddresses([]types.Address{})
	pf.PrefetchStorageSlots(types.HexToAddress("0x01"), nil)
}

func TestMemoryStateDBPrefetch(t *testing.T) {
	db := NewMemoryStateDB()

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	db.Prefetch([]types.Address{addr1, addr2})

	if !db.Exist(addr1) {
		t.Fatal("addr1 should exist after Prefetch")
	}
	if !db.Exist(addr2) {
		t.Fatal("addr2 should exist after Prefetch")
	}
}

func TestMemoryStateDBPrefetchStorage(t *testing.T) {
	db := NewMemoryStateDB()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	keys := []types.Hash{types.HexToHash("0x01"), types.HexToHash("0x02")}

	db.PrefetchStorage(addr, keys)

	if !db.Exist(addr) {
		t.Fatal("address should exist after PrefetchStorage")
	}
}
