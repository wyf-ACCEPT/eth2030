package txpool

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeTestTx(nonce uint64) *types.Transaction {
	addr := types.Address{0x01, 0x02, 0x03}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &addr,
		Value:    big.NewInt(100),
	})
	return tx
}

func makeTestTxWithSender(nonce uint64, sender types.Address) *types.Transaction {
	addr := types.Address{0x01, 0x02, 0x03}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1000),
		Gas:      21000,
		To:       &addr,
		Value:    big.NewInt(100),
	})
	tx.SetSender(sender)
	return tx
}

func TestShardedPool_AddRemove(t *testing.T) {
	sp := NewShardedPool(ShardConfig{
		NumShards:     4,
		ShardCapacity: 100,
	})

	tx1 := makeTestTx(0)
	tx2 := makeTestTx(1)
	tx3 := makeTestTx(2)

	// Add transactions.
	if err := sp.AddTx(tx1); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if err := sp.AddTx(tx2); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if err := sp.AddTx(tx3); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}

	if sp.Count() != 3 {
		t.Fatalf("expected 3 txs, got %d", sp.Count())
	}

	// Get transaction.
	got := sp.GetTx(tx1.Hash())
	if got == nil {
		t.Fatal("expected to find tx1")
	}
	if got.Hash() != tx1.Hash() {
		t.Fatal("hash mismatch")
	}

	// Remove transaction.
	if !sp.RemoveTx(tx2.Hash()) {
		t.Fatal("expected RemoveTx to return true")
	}
	if sp.Count() != 2 {
		t.Fatalf("expected 2 txs after removal, got %d", sp.Count())
	}
	if sp.GetTx(tx2.Hash()) != nil {
		t.Fatal("expected tx2 to be removed")
	}

	// Remove non-existent.
	if sp.RemoveTx(tx2.Hash()) {
		t.Fatal("expected RemoveTx to return false for already-removed tx")
	}
}

func TestShardedPool_ConsistentHashing(t *testing.T) {
	sp := NewShardedPool(ShardConfig{NumShards: 8, ShardCapacity: 100})

	tx := makeTestTx(42)
	hash := tx.Hash()

	// ShardForTx should be deterministic.
	shard1 := sp.ShardForTx(hash)
	shard2 := sp.ShardForTx(hash)
	if shard1 != shard2 {
		t.Fatalf("inconsistent shard assignment: %d vs %d", shard1, shard2)
	}

	// Must be in range.
	if shard1 >= 8 {
		t.Fatalf("shard %d out of range [0, 8)", shard1)
	}
}

func TestShardedPool_ShardForAddress(t *testing.T) {
	sp := NewShardedPool(ShardConfig{NumShards: 4, ShardCapacity: 100})

	addr := types.Address{0xAB, 0xCD, 0xEF, 0x01}
	s1 := sp.ShardForAddress(addr)
	s2 := sp.ShardForAddress(addr)
	if s1 != s2 {
		t.Fatalf("inconsistent address shard: %d vs %d", s1, s2)
	}
	if s1 >= 4 {
		t.Fatalf("shard %d out of range [0, 4)", s1)
	}
}

func TestShardedPool_ShardCapacity(t *testing.T) {
	sp := NewShardedPool(ShardConfig{NumShards: 1, ShardCapacity: 2})

	tx1 := makeTestTx(0)
	tx2 := makeTestTx(1)
	tx3 := makeTestTx(2)

	if err := sp.AddTx(tx1); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if err := sp.AddTx(tx2); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if err := sp.AddTx(tx3); err != ErrShardFull {
		t.Fatalf("expected ErrShardFull, got %v", err)
	}
}

func TestShardedPool_CrossShardQuery(t *testing.T) {
	sp := NewShardedPool(ShardConfig{NumShards: 4, ShardCapacity: 100})

	sender := types.Address{0xAA, 0xBB, 0xCC}
	tx1 := makeTestTxWithSender(0, sender)
	tx2 := makeTestTxWithSender(1, sender)
	tx3 := makeTestTxWithSender(2, types.Address{0x11, 0x22, 0x33}) // different sender

	sp.AddTx(tx1)
	sp.AddTx(tx2)
	sp.AddTx(tx3)

	results := sp.PendingByAddress(sender)
	if len(results) != 2 {
		t.Fatalf("expected 2 txs from sender, got %d", len(results))
	}
}

func TestShardedPool_GetShardStats(t *testing.T) {
	sp := NewShardedPool(ShardConfig{NumShards: 4, ShardCapacity: 100})

	// Add some transactions.
	for i := 0; i < 10; i++ {
		sp.AddTx(makeTestTx(uint64(i)))
	}

	stats := sp.GetShardStats()
	if len(stats) != 4 {
		t.Fatalf("expected 4 shard stats, got %d", len(stats))
	}

	totalFromStats := 0
	for _, s := range stats {
		totalFromStats += s.TxCount
		if s.Utilization < 0 || s.Utilization > 1.0 {
			t.Fatalf("utilization out of range: %f", s.Utilization)
		}
	}
	if totalFromStats != 10 {
		t.Fatalf("expected 10 total txs from stats, got %d", totalFromStats)
	}
}

func TestShardedPool_Rebalance(t *testing.T) {
	// Use 1 shard so everything goes there, then add more shards and rebalance.
	sp := NewShardedPool(ShardConfig{NumShards: 4, ShardCapacity: 1000})

	// Add many transactions.
	for i := 0; i < 20; i++ {
		sp.AddTx(makeTestTx(uint64(i)))
	}

	// Record pre-rebalance state.
	before := sp.GetShardStats()
	totalBefore := 0
	for _, s := range before {
		totalBefore += s.TxCount
	}

	sp.RebalanceShards()

	// After rebalance, total count should be preserved.
	totalAfter := sp.Count()
	if totalAfter != totalBefore {
		t.Fatalf("rebalance changed total: %d -> %d", totalBefore, totalAfter)
	}
}

func TestShardedPool_ConcurrentAccess(t *testing.T) {
	sp := NewShardedPool(ShardConfig{NumShards: 8, ShardCapacity: 10000})

	var wg sync.WaitGroup
	const goroutines = 20
	const txsPerGoroutine = 50

	// Concurrent adds.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < txsPerGoroutine; i++ {
				tx := makeTestTx(uint64(offset*txsPerGoroutine + i))
				sp.AddTx(tx)
			}
		}(g)
	}
	wg.Wait()

	total := sp.Count()
	if total != goroutines*txsPerGoroutine {
		t.Fatalf("expected %d txs, got %d", goroutines*txsPerGoroutine, total)
	}

	// Concurrent reads.
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sp.GetShardStats()
			sp.PendingByAddress(types.Address{0x01})
		}()
	}
	wg.Wait()
}

func TestShardedPool_ZeroConfig(t *testing.T) {
	// Zero NumShards should default to 1.
	sp := NewShardedPool(ShardConfig{})
	if len(sp.shards) != 1 {
		t.Fatalf("expected 1 shard for zero config, got %d", len(sp.shards))
	}

	tx := makeTestTx(0)
	if err := sp.AddTx(tx); err != nil {
		t.Fatalf("AddTx failed: %v", err)
	}
	if sp.Count() != 1 {
		t.Fatalf("expected 1 tx, got %d", sp.Count())
	}
}

func TestValidateShardAssignment(t *testing.T) {
	// Zero shards.
	if err := ValidateShardAssignment(ShardConfig{NumShards: 0}); err == nil {
		t.Fatal("expected error for zero shards")
	}

	// Non-power-of-two.
	if err := ValidateShardAssignment(ShardConfig{NumShards: 3, ShardCapacity: 100, ReplicationFactor: 1}); err == nil {
		t.Fatal("expected error for non-power-of-two shards")
	}

	// ReplicationFactor > NumShards.
	if err := ValidateShardAssignment(ShardConfig{NumShards: 4, ShardCapacity: 100, ReplicationFactor: 8}); err == nil {
		t.Fatal("expected error when replication > shards")
	}

	// Valid config.
	if err := ValidateShardAssignment(DefaultShardConfig()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
