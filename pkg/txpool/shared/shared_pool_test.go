package shared

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// makeTx creates a minimal legacy transaction for testing.
func makeTx(nonce uint64) *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21000,
		Value:    big.NewInt(1),
		Data:     nil,
	})
}

func makeSharedTx(nonce uint64, shard ShardID, priority float64) *SharedTx {
	return &SharedTx{
		Tx:       makeTx(nonce),
		Origin:   shard,
		Priority: priority,
	}
}

func defaultPool(t *testing.T) *SharedPool {
	t.Helper()
	pool, err := NewSharedPool(DefaultSharedPoolConfig())
	if err != nil {
		t.Fatalf("NewSharedPool: %v", err)
	}
	return pool
}

func TestNewSharedPool(t *testing.T) {
	pool, err := NewSharedPool(DefaultSharedPoolConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("pool should not be nil")
	}
	if pool.ShardCount() != 0 {
		t.Errorf("expected 0 shards, got %d", pool.ShardCount())
	}
}

func TestNewSharedPool_InvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config SharedPoolConfig
	}{
		{"zero max tx", SharedPoolConfig{MaxTxPerShard: 0, MaxShards: 1, RelayLimit: 1}},
		{"zero max shards", SharedPoolConfig{MaxTxPerShard: 1, MaxShards: 0, RelayLimit: 1}},
		{"negative relay", SharedPoolConfig{MaxTxPerShard: 1, MaxShards: 1, RelayLimit: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSharedPool(tt.config)
			if err != ErrInvalidConfig {
				t.Errorf("expected ErrInvalidConfig, got %v", err)
			}
		})
	}
}

func TestAddSharedTx(t *testing.T) {
	pool := defaultPool(t)
	stx := makeSharedTx(1, ShardID(0), 10.0)

	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatalf("AddSharedTx: %v", err)
	}

	if pool.TxCount() != 1 {
		t.Errorf("expected 1 tx, got %d", pool.TxCount())
	}
	if pool.ShardCount() != 1 {
		t.Errorf("expected 1 shard, got %d", pool.ShardCount())
	}
}

func TestAddSharedTx_Nil(t *testing.T) {
	pool := defaultPool(t)

	if err := pool.AddSharedTx(nil); err != ErrNilTransaction {
		t.Errorf("expected ErrNilTransaction, got %v", err)
	}
	if err := pool.AddSharedTx(&SharedTx{Tx: nil}); err != ErrNilTransaction {
		t.Errorf("expected ErrNilTransaction for nil inner tx, got %v", err)
	}
}

func TestAddSharedTx_Duplicate(t *testing.T) {
	pool := defaultPool(t)
	stx := makeSharedTx(1, ShardID(0), 10.0)

	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(stx); err != ErrDuplicateTx {
		t.Errorf("expected ErrDuplicateTx, got %v", err)
	}
}

func TestAddSharedTx_ShardFull(t *testing.T) {
	cfg := SharedPoolConfig{
		MaxTxPerShard: 2,
		MaxShards:     10,
		RelayLimit:    3,
		SyncInterval:  time.Second,
	}
	pool, err := NewSharedPool(cfg)
	if err != nil {
		t.Fatal(err)
	}

	shard := ShardID(0)
	if err := pool.AddSharedTx(makeSharedTx(1, shard, 1.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(2, shard, 2.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(3, shard, 3.0)); err != ErrShardFull {
		t.Errorf("expected ErrShardFull, got %v", err)
	}
}

func TestAddSharedTx_MaxShards(t *testing.T) {
	cfg := SharedPoolConfig{
		MaxTxPerShard: 10,
		MaxShards:     2,
		RelayLimit:    3,
		SyncInterval:  time.Second,
	}
	pool, err := NewSharedPool(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := pool.AddSharedTx(makeSharedTx(1, ShardID(0), 1.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(2, ShardID(1), 1.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(3, ShardID(2), 1.0)); err != ErrMaxShardsReached {
		t.Errorf("expected ErrMaxShardsReached, got %v", err)
	}
}

func TestGetPendingForShard(t *testing.T) {
	pool := defaultPool(t)
	shard := ShardID(5)

	// Add several with different priorities.
	if err := pool.AddSharedTx(makeSharedTx(1, shard, 1.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(2, shard, 10.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(3, shard, 5.0)); err != nil {
		t.Fatal(err)
	}

	pending := pool.GetPendingForShard(shard)
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(pending))
	}

	// Verify priority ordering: highest first.
	if pending[0].Priority != 10.0 {
		t.Errorf("expected first priority 10.0, got %f", pending[0].Priority)
	}
	if pending[1].Priority != 5.0 {
		t.Errorf("expected second priority 5.0, got %f", pending[1].Priority)
	}
	if pending[2].Priority != 1.0 {
		t.Errorf("expected third priority 1.0, got %f", pending[2].Priority)
	}
}

func TestGetPendingForShard_Empty(t *testing.T) {
	pool := defaultPool(t)
	pending := pool.GetPendingForShard(ShardID(99))
	if pending != nil {
		t.Errorf("expected nil for unknown shard, got %v", pending)
	}
}

func TestRelayTx(t *testing.T) {
	pool := defaultPool(t)
	shard0 := ShardID(0)
	shard1 := ShardID(1)

	stx := makeSharedTx(1, shard0, 5.0)
	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}

	// Relay to shard 1.
	if err := pool.RelayTx(stx, shard1); err != nil {
		t.Fatalf("RelayTx: %v", err)
	}

	// Both shards should have the transaction.
	p0 := pool.GetPendingForShard(shard0)
	p1 := pool.GetPendingForShard(shard1)
	if len(p0) != 1 || len(p1) != 1 {
		t.Errorf("expected 1 tx in each shard, got shard0=%d shard1=%d", len(p0), len(p1))
	}

	// Relay count should increment in the target shard copy.
	if p1[0].RelayCount != 1 {
		t.Errorf("expected relay count 1 in target, got %d", p1[0].RelayCount)
	}
	// Origin copy should remain unchanged.
	if p0[0].RelayCount != 0 {
		t.Errorf("expected relay count 0 in origin, got %d", p0[0].RelayCount)
	}
}

func TestRelayTx_Nil(t *testing.T) {
	pool := defaultPool(t)
	if err := pool.RelayTx(nil, ShardID(0)); err != ErrNilTransaction {
		t.Errorf("expected ErrNilTransaction, got %v", err)
	}
}

func TestRelayTx_RelayLimitExceeded(t *testing.T) {
	cfg := SharedPoolConfig{
		MaxTxPerShard: 100,
		MaxShards:     100,
		RelayLimit:    2,
		SyncInterval:  time.Second,
	}
	pool, err := NewSharedPool(cfg)
	if err != nil {
		t.Fatal(err)
	}

	stx := makeSharedTx(1, ShardID(0), 5.0)
	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}

	// First relay: count 0 -> 1, OK.
	if err := pool.RelayTx(stx, ShardID(1)); err != nil {
		t.Fatal(err)
	}

	// Get the relayed copy.
	relayed := pool.GetPendingForShard(ShardID(1))[0]

	// Second relay: count 1 -> 2, OK.
	if err := pool.RelayTx(relayed, ShardID(2)); err != nil {
		t.Fatal(err)
	}

	// Get the second relayed copy.
	relayed2 := pool.GetPendingForShard(ShardID(2))[0]

	// Third relay: count 2 == RelayLimit, should fail.
	if err := pool.RelayTx(relayed2, ShardID(3)); err != ErrRelayLimit {
		t.Errorf("expected ErrRelayLimit, got %v", err)
	}
}

func TestRelayTx_DuplicateInTarget(t *testing.T) {
	pool := defaultPool(t)
	shard0 := ShardID(0)
	shard1 := ShardID(1)

	stx := makeSharedTx(1, shard0, 5.0)
	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}

	if err := pool.RelayTx(stx, shard1); err != nil {
		t.Fatal(err)
	}
	// Relaying again to same target should fail.
	if err := pool.RelayTx(stx, shard1); err != ErrDuplicateTx {
		t.Errorf("expected ErrDuplicateTx, got %v", err)
	}
}

func TestPruneShard(t *testing.T) {
	pool := defaultPool(t)
	shard := ShardID(7)

	if err := pool.AddSharedTx(makeSharedTx(1, shard, 1.0)); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddSharedTx(makeSharedTx(2, shard, 2.0)); err != nil {
		t.Fatal(err)
	}

	if pool.TxCount() != 2 {
		t.Fatalf("expected 2 txs before prune, got %d", pool.TxCount())
	}

	pool.PruneShard(shard)

	if pool.TxCount() != 0 {
		t.Errorf("expected 0 txs after prune, got %d", pool.TxCount())
	}
	if pool.ShardCount() != 0 {
		t.Errorf("expected 0 shards after prune, got %d", pool.ShardCount())
	}
}

func TestPruneShard_Nonexistent(t *testing.T) {
	pool := defaultPool(t)
	// Should not panic on nonexistent shard.
	pool.PruneShard(ShardID(999))
}

func TestCrossShardSync(t *testing.T) {
	pool := defaultPool(t)

	// Populate three shards.
	for i := 0; i < 3; i++ {
		shard := ShardID(i)
		for j := 0; j < 3; j++ {
			nonce := uint64(i*100 + j)
			priority := float64(j + 1)
			if err := pool.AddSharedTx(makeSharedTx(nonce, shard, priority)); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Sync shards 0 and 2.
	synced, err := pool.CrossShardSync([]ShardID{ShardID(0), ShardID(2)})
	if err != nil {
		t.Fatalf("CrossShardSync: %v", err)
	}

	if len(synced) != 6 {
		t.Errorf("expected 6 txs from shards 0+2, got %d", len(synced))
	}

	// Verify sorted by priority descending.
	for i := 1; i < len(synced); i++ {
		if synced[i].Priority > synced[i-1].Priority {
			t.Errorf("results not sorted by priority: idx %d (%f) > idx %d (%f)",
				i, synced[i].Priority, i-1, synced[i-1].Priority)
		}
	}
}

func TestCrossShardSync_EmptyPeers(t *testing.T) {
	pool := defaultPool(t)
	synced, err := pool.CrossShardSync(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(synced) != 0 {
		t.Errorf("expected 0 results, got %d", len(synced))
	}
}

func TestCrossShardSync_NonexistentShard(t *testing.T) {
	pool := defaultPool(t)
	synced, err := pool.CrossShardSync([]ShardID{ShardID(42)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(synced) != 0 {
		t.Errorf("expected 0 results for nonexistent shard, got %d", len(synced))
	}
}

func TestCrossShardSync_NoDuplicates(t *testing.T) {
	pool := defaultPool(t)
	stx := makeSharedTx(1, ShardID(0), 5.0)
	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}
	// Relay same tx to shard 1.
	if err := pool.RelayTx(stx, ShardID(1)); err != nil {
		t.Fatal(err)
	}

	// Sync both shards: should deduplicate.
	synced, err := pool.CrossShardSync([]ShardID{ShardID(0), ShardID(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(synced) != 1 {
		t.Errorf("expected 1 unique tx, got %d", len(synced))
	}
}

func TestClose(t *testing.T) {
	pool := defaultPool(t)
	pool.Close()

	if err := pool.AddSharedTx(makeSharedTx(1, ShardID(0), 1.0)); err != ErrPoolClosed {
		t.Errorf("expected ErrPoolClosed, got %v", err)
	}

	stx := makeSharedTx(2, ShardID(0), 1.0)
	if err := pool.RelayTx(stx, ShardID(1)); err != ErrPoolClosed {
		t.Errorf("expected ErrPoolClosed on relay, got %v", err)
	}

	_, err := pool.CrossShardSync([]ShardID{ShardID(0)})
	if err != ErrPoolClosed {
		t.Errorf("expected ErrPoolClosed on sync, got %v", err)
	}
}

func TestSharedTxHash_NilTx(t *testing.T) {
	stx := &SharedTx{Tx: nil}
	if h := stx.Hash(); !h.IsZero() {
		t.Errorf("expected zero hash for nil tx, got %s", h)
	}
}

func TestAddedAtAutoset(t *testing.T) {
	pool := defaultPool(t)
	stx := makeSharedTx(1, ShardID(0), 1.0)
	if !stx.AddedAt.IsZero() {
		t.Fatal("AddedAt should be zero before add")
	}

	before := time.Now()
	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}
	after := time.Now()

	pending := pool.GetPendingForShard(ShardID(0))
	if len(pending) != 1 {
		t.Fatalf("expected 1, got %d", len(pending))
	}
	addedAt := pending[0].AddedAt
	if addedAt.Before(before) || addedAt.After(after) {
		t.Errorf("AddedAt %v not between %v and %v", addedAt, before, after)
	}
}

func TestConcurrentAccess(t *testing.T) {
	pool := defaultPool(t)
	var wg sync.WaitGroup
	errs := make(chan error, 200)

	// Concurrent writers to different shards.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(shard int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				nonce := uint64(shard*1000 + j)
				stx := makeSharedTx(nonce, ShardID(shard), float64(j))
				if err := pool.AddSharedTx(stx); err != nil {
					errs <- err
				}
			}
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(shard int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				pool.GetPendingForShard(ShardID(shard))
				pool.TxCount()
				pool.ShardCount()
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if pool.TxCount() != 100 {
		t.Errorf("expected 100 txs, got %d", pool.TxCount())
	}
}

func TestRelayTx_ShardAutoCreate(t *testing.T) {
	pool := defaultPool(t)
	stx := makeSharedTx(1, ShardID(0), 5.0)
	if err := pool.AddSharedTx(stx); err != nil {
		t.Fatal(err)
	}

	// Relay to new shard that does not exist yet.
	if err := pool.RelayTx(stx, ShardID(99)); err != nil {
		t.Fatalf("relay to new shard: %v", err)
	}

	if pool.ShardCount() != 2 {
		t.Errorf("expected 2 shards after relay, got %d", pool.ShardCount())
	}
}

func TestMultipleShards(t *testing.T) {
	pool := defaultPool(t)

	for i := 0; i < 5; i++ {
		for j := 0; j < 4; j++ {
			nonce := uint64(i*100 + j)
			stx := makeSharedTx(nonce, ShardID(i), float64(j))
			if err := pool.AddSharedTx(stx); err != nil {
				t.Fatal(err)
			}
		}
	}

	if pool.ShardCount() != 5 {
		t.Errorf("expected 5 shards, got %d", pool.ShardCount())
	}
	if pool.TxCount() != 20 {
		t.Errorf("expected 20 txs, got %d", pool.TxCount())
	}

	// Check each shard has exactly 4.
	for i := 0; i < 5; i++ {
		pending := pool.GetPendingForShard(ShardID(i))
		if len(pending) != 4 {
			t.Errorf("shard %d: expected 4 txs, got %d", i, len(pending))
		}
	}
}
