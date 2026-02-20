package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewStatePrefetcher(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 0)
	defer sp.Stop()

	// Default should be 4 workers.
	if sp.workers != 4 {
		t.Fatalf("expected 4 workers, got %d", sp.workers)
	}
}

func TestNewStatePrefetcher_CustomWorkers(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 8)
	defer sp.Stop()

	if sp.workers != 8 {
		t.Fatalf("expected 8 workers, got %d", sp.workers)
	}
}

func TestStatePrefetcher_PrefetchAddress(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)
	defer sp.Stop()

	addr := types.HexToAddress("0xdead")
	sp.PrefetchAddress(addr)
	sp.Wait()

	if !sp.IsWarm(addr) {
		t.Fatal("address should be warm after prefetch")
	}

	stats := sp.Stats()
	if stats.Requested == 0 {
		t.Fatal("expected at least 1 request")
	}
	if stats.Completed == 0 {
		t.Fatal("expected at least 1 completion")
	}
}

func TestStatePrefetcher_PrefetchSlots(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)
	defer sp.Stop()

	addr := types.HexToAddress("0xbeef")
	slot1 := types.HexToHash("0x01")
	slot2 := types.HexToHash("0x02")

	sp.PrefetchSlots(addr, []types.Hash{slot1, slot2})
	sp.Wait()

	if !sp.IsWarm(addr) {
		t.Fatal("address should be warm")
	}
	if !sp.IsSlotWarm(addr, slot1) {
		t.Fatal("slot1 should be warm")
	}
	if !sp.IsSlotWarm(addr, slot2) {
		t.Fatal("slot2 should be warm")
	}
}

func TestStatePrefetcher_PrefetchState_Transactions(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 4)
	defer sp.Stop()

	to := types.HexToAddress("0xdead")
	data := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0x01, 0x02}
	txs := []*types.Transaction{
		types.NewTransaction(&types.LegacyTx{
			Gas:      100_000,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(0),
			To:       &to,
			Data:     data,
		}),
	}

	sp.PrefetchState(txs)
	sp.Wait()

	if !sp.IsWarm(to) {
		t.Fatal("recipient should be warm after PrefetchState")
	}

	accounts, slots := sp.CacheSize()
	if accounts == 0 {
		t.Fatal("expected at least 1 warmed account")
	}
	if slots == 0 {
		t.Fatal("expected at least 1 warmed slot (from calldata prediction)")
	}
}

func TestStatePrefetcher_PrefetchState_WithAccessList(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)
	defer sp.Stop()

	to := types.HexToAddress("0xdead")
	alAddr := types.HexToAddress("0xfeed")
	alSlot := types.HexToHash("0xaa")

	txs := []*types.Transaction{
		types.NewTransaction(&types.AccessListTx{
			Gas:      100_000,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(0),
			To:       &to,
			AccessList: types.AccessList{
				{Address: alAddr, StorageKeys: []types.Hash{alSlot}},
			},
		}),
	}

	sp.PrefetchState(txs)
	sp.Wait()

	// Access list slot should be predicted.
	if !sp.IsWarm(to) {
		t.Fatal("recipient should be warm")
	}
}

func TestStatePrefetcher_Reset(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)
	defer sp.Stop()

	addr := types.HexToAddress("0x01")
	sp.PrefetchAddress(addr)
	sp.Wait()

	if !sp.IsWarm(addr) {
		t.Fatal("should be warm before reset")
	}

	sp.Reset()

	if sp.IsWarm(addr) {
		t.Fatal("should not be warm after reset")
	}

	stats := sp.Stats()
	if stats.Requested != 0 || stats.Completed != 0 {
		t.Fatal("stats should be zero after reset")
	}
}

func TestStatePrefetcher_CacheHits(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)
	defer sp.Stop()

	addr := types.HexToAddress("0xaa")

	// First prefetch: miss.
	sp.PrefetchAddress(addr)
	sp.Wait()

	// Second prefetch: hit.
	sp.PrefetchAddress(addr)
	sp.Wait()

	stats := sp.Stats()
	if stats.CacheHits == 0 {
		t.Fatal("expected at least 1 cache hit on second prefetch")
	}
}

func TestStatePrefetcher_StopIdempotent(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)

	// Stop should be idempotent.
	sp.Stop()
	sp.Stop()
	sp.Stop()
}

func TestStatePrefetcher_CacheSize(t *testing.T) {
	state := newParallelMockStateDB()
	sp := NewStatePrefetcher(state, 2)
	defer sp.Stop()

	accounts, slots := sp.CacheSize()
	if accounts != 0 || slots != 0 {
		t.Fatalf("expected empty cache, got accounts=%d slots=%d", accounts, slots)
	}

	sp.PrefetchAddress(types.HexToAddress("0x01"))
	sp.PrefetchAddress(types.HexToAddress("0x02"))
	sp.PrefetchSlots(types.HexToAddress("0x03"), []types.Hash{types.HexToHash("0x10")})
	sp.Wait()

	accounts, slots = sp.CacheSize()
	if accounts < 2 {
		t.Fatalf("expected at least 2 accounts, got %d", accounts)
	}
	if slots < 1 {
		t.Fatalf("expected at least 1 slot, got %d", slots)
	}
}

// --- AccessPatternPredictor tests ---

func TestAccessPatternPredictor_NilTx(t *testing.T) {
	p := NewAccessPatternPredictor()
	req := p.PredictAccess(nil)
	if req != nil {
		t.Fatal("expected nil for nil tx")
	}
}

func TestAccessPatternPredictor_SimpleTransfer(t *testing.T) {
	p := NewAccessPatternPredictor()
	to := types.HexToAddress("0xbeef")
	tx := types.NewTransaction(&types.LegacyTx{
		Gas:      21000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(100),
		To:       &to,
	})

	req := p.PredictAccess(tx)
	if req == nil {
		t.Fatal("expected non-nil request")
	}
	if req.Address != to {
		t.Fatalf("expected address %v, got %v", to, req.Address)
	}
	// Simple transfer with no calldata: no predicted storage keys.
	if len(req.StorageKeys) != 0 {
		t.Fatalf("expected 0 storage keys for simple transfer, got %d", len(req.StorageKeys))
	}
}

func TestAccessPatternPredictor_ContractCall(t *testing.T) {
	p := NewAccessPatternPredictor()
	to := types.HexToAddress("0xdead")
	// 4-byte selector + 32-byte address argument (left-padded).
	data := make([]byte, 36)
	data[0] = 0xa9
	data[1] = 0x05
	data[2] = 0x9c
	data[3] = 0xbb
	// Address argument at offset 4: first 12 bytes zero, then 20 bytes.
	data[16] = 0xde
	data[17] = 0xad
	data[35] = 0x01

	tx := types.NewTransaction(&types.LegacyTx{
		Gas:      100_000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(0),
		To:       &to,
		Data:     data,
	})

	req := p.PredictAccess(tx)
	if req == nil {
		t.Fatal("expected non-nil request")
	}
	// Should predict: selector slot + mapping slots + address balance slot.
	if len(req.StorageKeys) < 2 {
		t.Fatalf("expected at least 2 predicted storage keys, got %d", len(req.StorageKeys))
	}
}

func TestAccessPatternPredictor_WithAccessList(t *testing.T) {
	p := NewAccessPatternPredictor()
	to := types.HexToAddress("0xdead")
	alSlot := types.HexToHash("0xff")

	tx := types.NewTransaction(&types.AccessListTx{
		Gas:      100_000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(0),
		To:       &to,
		AccessList: types.AccessList{
			{Address: to, StorageKeys: []types.Hash{alSlot}},
		},
	})

	req := p.PredictAccess(tx)
	if req == nil {
		t.Fatal("expected non-nil request")
	}

	// Should include the access list slot.
	found := false
	for _, k := range req.StorageKeys {
		if k == alSlot {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected access list slot in predictions")
	}
}

// --- stateCache tests ---

func TestStateCache_WarmAccount(t *testing.T) {
	c := newStateCache()
	addr := types.HexToAddress("0x01")

	if c.isAccountWarm(addr) {
		t.Fatal("should not be warm initially")
	}

	wasWarm := c.warmAccount(addr)
	if wasWarm {
		t.Fatal("first warm should return false")
	}
	if !c.isAccountWarm(addr) {
		t.Fatal("should be warm now")
	}

	wasWarm = c.warmAccount(addr)
	if !wasWarm {
		t.Fatal("second warm should return true (already warm)")
	}
}

func TestStateCache_WarmSlot(t *testing.T) {
	c := newStateCache()
	addr := types.HexToAddress("0x01")
	key := types.HexToHash("0x10")

	if c.isSlotWarm(addr, key) {
		t.Fatal("should not be warm initially")
	}

	wasWarm := c.warmSlot(addr, key)
	if wasWarm {
		t.Fatal("first warm should return false")
	}
	if !c.isSlotWarm(addr, key) {
		t.Fatal("should be warm now")
	}

	wasWarm = c.warmSlot(addr, key)
	if !wasWarm {
		t.Fatal("second warm should return true")
	}
}

func TestStateCache_Reset(t *testing.T) {
	c := newStateCache()
	c.warmAccount(types.HexToAddress("0x01"))
	c.warmSlot(types.HexToAddress("0x02"), types.HexToHash("0x10"))

	c.reset()

	if c.accountCount() != 0 || c.slotCount() != 0 {
		t.Fatal("cache should be empty after reset")
	}
}
