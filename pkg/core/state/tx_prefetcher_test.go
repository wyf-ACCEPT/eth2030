package state

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

func TestTxPrefetcher_PrefetchAddress(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	if db.Exist(addr) {
		t.Fatal("addr should not exist before prefetch")
	}

	pf.PrefetchAddress(addr)
	pf.Wait()

	if !db.Exist(addr) {
		t.Fatal("addr should exist after PrefetchAddress")
	}
}

func TestTxPrefetcher_PrefetchStorage(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")
	keys := []types.Hash{types.HexToHash("0x01"), types.HexToHash("0x02")}

	pf.PrefetchStorage(addr, keys)
	pf.Wait()

	if !db.Exist(addr) {
		t.Fatal("addr should exist after PrefetchStorage")
	}
}

func TestTxPrefetcher_PrefetchTransactions(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 4)
	defer pf.Close()

	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	accessAddr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")

	tx := types.NewTransaction(&types.DynamicFeeTx{
		ChainID:   big.NewInt(1),
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(100),
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(0),
		AccessList: types.AccessList{
			{Address: accessAddr, StorageKeys: []types.Hash{types.HexToHash("0xaa")}},
		},
	})

	pf.Prefetch([]*types.Transaction{tx})
	pf.Wait()

	if !db.Exist(to) {
		t.Fatal("receiver should exist after Prefetch")
	}
	if !db.Exist(accessAddr) {
		t.Fatal("access list address should exist after Prefetch")
	}
}

func TestTxPrefetcher_PrefetchWithSender(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	sender := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	to := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	tx.SetSender(sender)

	pf.Prefetch([]*types.Transaction{tx})
	pf.Wait()

	if !db.Exist(sender) {
		t.Fatal("sender should exist after Prefetch")
	}
	if !db.Exist(to) {
		t.Fatal("receiver should exist after Prefetch")
	}
}

func TestTxPrefetcher_DoesNotOverwrite(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(500))
	db.SetNonce(addr, 10)

	pf.PrefetchAddress(addr)
	pf.Wait()

	if db.GetBalance(addr).Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("prefetch should not overwrite balance: got %s", db.GetBalance(addr))
	}
	if db.GetNonce(addr) != 10 {
		t.Fatalf("prefetch should not overwrite nonce: got %d", db.GetNonce(addr))
	}
}

func TestTxPrefetcher_Stats(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	stats := pf.TxPrefetcherStats()
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Fatal("initial stats should be zero")
	}

	addr1 := types.HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := types.HexToAddress("0x2222222222222222222222222222222222222222")

	pf.PrefetchAddress(addr1)
	pf.PrefetchAddress(addr2)
	pf.Wait()

	stats = pf.TxPrefetcherStats()
	if stats.Misses != 2 {
		t.Fatalf("misses: want 2, got %d", stats.Misses)
	}

	// Prefetch again: addr1 now exists, so it should be a hit.
	pf.PrefetchAddress(addr1)
	pf.Wait()

	stats = pf.TxPrefetcherStats()
	if stats.Hits != 1 {
		t.Fatalf("hits: want 1, got %d", stats.Hits)
	}
}

func TestTxPrefetcher_Close(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)

	pf.Close()

	// After Close, Prefetch should be a no-op and not panic.
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	pf.PrefetchAddress(addr)

	if db.Exist(addr) {
		t.Fatal("prefetch after Close should be a no-op")
	}

	// Double close should not panic.
	pf.Close()
}

func TestTxPrefetcher_CloseIdempotent(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)

	// Close multiple times should not panic.
	for i := 0; i < 5; i++ {
		pf.Close()
	}
}

func TestTxPrefetcher_ConcurrentPrefetches(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 4)
	defer pf.Close()

	const count = 50
	addrs := make([]types.Address, count)
	for i := range addrs {
		addrs[i] = types.BytesToAddress([]byte{byte(i + 1)})
	}

	for _, addr := range addrs {
		pf.PrefetchAddress(addr)
	}
	pf.Wait()

	for i, addr := range addrs {
		if !db.Exist(addr) {
			t.Fatalf("addr %d should exist after concurrent prefetch", i)
		}
	}

	stats := pf.TxPrefetcherStats()
	if stats.Misses != count {
		t.Fatalf("misses: want %d, got %d", count, stats.Misses)
	}
}

func TestTxPrefetcher_IsPrefetched(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	if pf.IsPrefetched(addr) {
		t.Fatal("should not be prefetched before any operation")
	}

	pf.PrefetchAddress(addr)
	pf.Wait()

	if !pf.IsPrefetched(addr) {
		t.Fatal("should be prefetched after PrefetchAddress")
	}
}

func TestTxPrefetcher_EmptyTransaction(t *testing.T) {
	db := NewMemoryStateDB()
	pf := NewTxPrefetcher(db, 2)
	defer pf.Close()

	// Prefetch with nil/empty list should be a no-op, no panic.
	pf.Prefetch(nil)
	pf.Prefetch([]*types.Transaction{})

	// Contract creation (nil receiver).
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       nil,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	pf.Prefetch([]*types.Transaction{tx})
	pf.Wait()
}

func TestTxPrefetcher_DefaultWorkers(t *testing.T) {
	db := NewMemoryStateDB()
	// Workers <= 0 should default to 4 without panic.
	pf := NewTxPrefetcher(db, 0)
	defer pf.Close()

	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")
	pf.PrefetchAddress(addr)

	// Give workers time to process.
	deadline := time.Now().Add(2 * time.Second)
	for !pf.IsPrefetched(addr) {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for prefetch with default workers")
		}
		time.Sleep(time.Millisecond)
	}
}
