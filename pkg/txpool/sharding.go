package txpool

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Sharded mempool errors.
var (
	ErrShardFull    = errors.New("shard is full")
	ErrShardInvalid = errors.New("invalid shard configuration")
)

// ShardConfig configures the sharded mempool.
type ShardConfig struct {
	NumShards         uint32 // number of shards to distribute transactions across
	ShardCapacity     int    // maximum transactions per shard
	ReplicationFactor uint32 // number of shards each tx is replicated to (1 = no replication)
}

// DefaultShardConfig returns sensible defaults for a sharded pool.
func DefaultShardConfig() ShardConfig {
	return ShardConfig{
		NumShards:         16,
		ShardCapacity:     1024,
		ReplicationFactor: 1,
	}
}

// ShardStats holds per-shard metrics.
type ShardStats struct {
	ID          uint32
	TxCount     int
	Utilization float64 // fraction of capacity used (0.0 - 1.0)
}

// TxShard holds transactions assigned to a single shard.
type TxShard struct {
	id           uint32
	transactions map[types.Hash]*types.Transaction
	mu           sync.RWMutex
}

func newTxShard(id uint32) *TxShard {
	return &TxShard{
		id:           id,
		transactions: make(map[types.Hash]*types.Transaction),
	}
}

// ShardedPool distributes transactions across multiple shards using consistent
// hashing on the transaction hash. This enables parallel access to disjoint
// transaction sets and reduces lock contention at high throughput.
type ShardedPool struct {
	shards []*TxShard
	config ShardConfig
}

// NewShardedPool creates a new sharded transaction pool.
func NewShardedPool(config ShardConfig) *ShardedPool {
	if config.NumShards == 0 {
		config.NumShards = 1
	}
	if config.ReplicationFactor == 0 {
		config.ReplicationFactor = 1
	}
	shards := make([]*TxShard, config.NumShards)
	for i := uint32(0); i < config.NumShards; i++ {
		shards[i] = newTxShard(i)
	}
	return &ShardedPool{
		shards: shards,
		config: config,
	}
}

// ShardForTx returns the shard index for a given transaction hash
// using consistent hashing (first 4 bytes mod numShards).
func (sp *ShardedPool) ShardForTx(hash types.Hash) uint32 {
	// Use the first 4 bytes of the hash as a uint32.
	v := uint32(hash[0])<<24 | uint32(hash[1])<<16 | uint32(hash[2])<<8 | uint32(hash[3])
	return v % sp.config.NumShards
}

// ShardForAddress returns the shard index for a given sender address.
func (sp *ShardedPool) ShardForAddress(addr types.Address) uint32 {
	v := uint32(addr[0])<<24 | uint32(addr[1])<<16 | uint32(addr[2])<<8 | uint32(addr[3])
	return v % sp.config.NumShards
}

// AddTx routes a transaction to the appropriate shard and inserts it.
func (sp *ShardedPool) AddTx(tx *types.Transaction) error {
	hash := tx.Hash()
	shardID := sp.ShardForTx(hash)
	shard := sp.shards[shardID]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if sp.config.ShardCapacity > 0 && len(shard.transactions) >= sp.config.ShardCapacity {
		return ErrShardFull
	}
	shard.transactions[hash] = tx
	return nil
}

// RemoveTx removes a transaction from the pool by hash.
// Returns true if the transaction was found and removed.
func (sp *ShardedPool) RemoveTx(hash types.Hash) bool {
	shardID := sp.ShardForTx(hash)
	shard := sp.shards[shardID]

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, ok := shard.transactions[hash]; ok {
		delete(shard.transactions, hash)
		return true
	}
	return false
}

// GetTx retrieves a transaction by hash from the appropriate shard.
func (sp *ShardedPool) GetTx(hash types.Hash) *types.Transaction {
	shardID := sp.ShardForTx(hash)
	shard := sp.shards[shardID]

	shard.mu.RLock()
	defer shard.mu.RUnlock()

	return shard.transactions[hash]
}

// GetShardStats returns per-shard metrics for all shards.
func (sp *ShardedPool) GetShardStats() []ShardStats {
	stats := make([]ShardStats, len(sp.shards))
	for i, shard := range sp.shards {
		shard.mu.RLock()
		count := len(shard.transactions)
		shard.mu.RUnlock()

		var util float64
		if sp.config.ShardCapacity > 0 {
			util = float64(count) / float64(sp.config.ShardCapacity)
		}
		stats[i] = ShardStats{
			ID:          shard.id,
			TxCount:     count,
			Utilization: util,
		}
	}
	return stats
}

// RebalanceShards moves transactions from overloaded shards to underloaded ones.
// This handles hotspot situations where consistent hashing produces uneven distribution.
func (sp *ShardedPool) RebalanceShards() {
	// Calculate average load.
	var totalTxs int
	for _, shard := range sp.shards {
		shard.mu.RLock()
		totalTxs += len(shard.transactions)
		shard.mu.RUnlock()
	}
	if totalTxs == 0 || len(sp.shards) == 0 {
		return
	}
	avgLoad := totalTxs / len(sp.shards)
	// Only rebalance if a shard exceeds 150% of the average.
	threshold := avgLoad + avgLoad/2
	if threshold < 1 {
		threshold = 1
	}

	// Collect overflow transactions from hot shards.
	var overflow []*types.Transaction
	for _, shard := range sp.shards {
		shard.mu.Lock()
		if len(shard.transactions) > threshold {
			excess := len(shard.transactions) - avgLoad
			for hash, tx := range shard.transactions {
				if excess <= 0 {
					break
				}
				overflow = append(overflow, tx)
				delete(shard.transactions, hash)
				excess--
			}
		}
		shard.mu.Unlock()
	}

	// Redistribute overflow to the least-loaded shards.
	for _, tx := range overflow {
		// Find the shard with the fewest transactions.
		var bestShard *TxShard
		bestCount := int(^uint(0) >> 1) // max int
		for _, shard := range sp.shards {
			shard.mu.RLock()
			c := len(shard.transactions)
			shard.mu.RUnlock()
			if c < bestCount {
				bestCount = c
				bestShard = shard
			}
		}
		if bestShard != nil {
			bestShard.mu.Lock()
			bestShard.transactions[tx.Hash()] = tx
			bestShard.mu.Unlock()
		}
	}
}

// PendingByAddress queries all shards to find transactions from a given address.
// This is a cross-shard query that iterates every shard.
func (sp *ShardedPool) PendingByAddress(addr types.Address) []*types.Transaction {
	var result []*types.Transaction
	for _, shard := range sp.shards {
		shard.mu.RLock()
		for _, tx := range shard.transactions {
			if from := tx.Sender(); from != nil && *from == addr {
				result = append(result, tx)
			}
		}
		shard.mu.RUnlock()
	}
	return result
}

// ValidateShardAssignment checks that a shard configuration is valid:
//   - NumShards must be > 0 and a power of two (for consistent hashing)
//   - ShardCapacity must be > 0
//   - ReplicationFactor must be >= 1 and <= NumShards
func ValidateShardAssignment(config ShardConfig) error {
	if config.NumShards == 0 {
		return errors.New("shard: NumShards must be > 0")
	}
	if config.NumShards&(config.NumShards-1) != 0 {
		return errors.New("shard: NumShards must be a power of two")
	}
	if config.ShardCapacity <= 0 {
		return errors.New("shard: ShardCapacity must be > 0")
	}
	if config.ReplicationFactor < 1 {
		return errors.New("shard: ReplicationFactor must be >= 1")
	}
	if config.ReplicationFactor > config.NumShards {
		return errors.New("shard: ReplicationFactor exceeds NumShards")
	}
	return nil
}

// Count returns the total number of transactions across all shards.
func (sp *ShardedPool) Count() int {
	total := 0
	for _, shard := range sp.shards {
		shard.mu.RLock()
		total += len(shard.transactions)
		shard.mu.RUnlock()
	}
	return total
}
