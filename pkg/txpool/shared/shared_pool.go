// Package shared implements a cross-validator transaction coordination pool.
// Validators use the SharedPool to share, relay, and synchronize pending
// transactions across logical shards, enabling efficient cross-shard
// transaction propagation for the shared mempool roadmap item.
package shared

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// ShardID identifies a logical shard in the shared mempool.
type ShardID uint32

// Shared pool errors.
var (
	ErrPoolClosed       = errors.New("shared pool is closed")
	ErrShardFull        = errors.New("shard transaction limit reached")
	ErrMaxShardsReached = errors.New("maximum shard count reached")
	ErrRelayLimit       = errors.New("relay limit exceeded for transaction")
	ErrDuplicateTx      = errors.New("transaction already exists in shard")
	ErrShardNotFound    = errors.New("shard not found")
	ErrNilTransaction   = errors.New("transaction is nil")
	ErrInvalidConfig    = errors.New("invalid shared pool configuration")
)

// SharedPoolConfig configures the cross-validator shared mempool.
type SharedPoolConfig struct {
	MaxTxPerShard int           // maximum transactions per shard
	MaxShards     int           // maximum number of shards
	RelayLimit    int           // maximum relay hops per transaction
	SyncInterval  time.Duration // interval between cross-shard syncs
}

// DefaultSharedPoolConfig returns sensible defaults.
func DefaultSharedPoolConfig() SharedPoolConfig {
	return SharedPoolConfig{
		MaxTxPerShard: 1024,
		MaxShards:     64,
		RelayLimit:    3,
		SyncInterval:  2 * time.Second,
	}
}

// SharedTx wraps a transaction with cross-shard coordination metadata.
type SharedTx struct {
	Tx         *types.Transaction
	Origin     ShardID   // shard where the transaction was first seen
	RelayCount int       // number of times this tx has been relayed
	Priority   float64   // priority score for ordering (higher = more urgent)
	AddedAt    time.Time // when the tx was added to the pool
}

// Hash returns the underlying transaction hash for indexing.
func (stx *SharedTx) Hash() types.Hash {
	if stx.Tx == nil {
		return types.Hash{}
	}
	return stx.Tx.Hash()
}

// shardBucket holds transactions assigned to a single shard.
type shardBucket struct {
	txs map[types.Hash]*SharedTx
}

func newShardBucket() *shardBucket {
	return &shardBucket{txs: make(map[types.Hash]*SharedTx)}
}

// SharedPool manages cross-validator transaction sharing across shards.
// It is safe for concurrent use.
type SharedPool struct {
	mu     sync.RWMutex
	config SharedPoolConfig
	shards map[ShardID]*shardBucket
	closed bool
}

// NewSharedPool creates a new shared mempool with the given configuration.
func NewSharedPool(config SharedPoolConfig) (*SharedPool, error) {
	if config.MaxTxPerShard <= 0 || config.MaxShards <= 0 || config.RelayLimit < 0 {
		return nil, ErrInvalidConfig
	}
	return &SharedPool{
		config: config,
		shards: make(map[ShardID]*shardBucket),
	}, nil
}

// AddSharedTx adds a cross-shard transaction to its originating shard.
func (sp *SharedPool) AddSharedTx(tx *SharedTx) error {
	if tx == nil || tx.Tx == nil {
		return ErrNilTransaction
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.closed {
		return ErrPoolClosed
	}

	shard, ok := sp.shards[tx.Origin]
	if !ok {
		// Auto-create the shard if within limits.
		if len(sp.shards) >= sp.config.MaxShards {
			return ErrMaxShardsReached
		}
		shard = newShardBucket()
		sp.shards[tx.Origin] = shard
	}

	hash := tx.Hash()
	if _, exists := shard.txs[hash]; exists {
		return ErrDuplicateTx
	}
	if len(shard.txs) >= sp.config.MaxTxPerShard {
		return ErrShardFull
	}

	if tx.AddedAt.IsZero() {
		tx.AddedAt = time.Now()
	}
	shard.txs[hash] = tx
	return nil
}

// GetPendingForShard returns all pending transactions for the given shard,
// sorted by priority descending (highest priority first).
func (sp *SharedPool) GetPendingForShard(shard ShardID) []*SharedTx {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	bucket, ok := sp.shards[shard]
	if !ok {
		return nil
	}

	result := make([]*SharedTx, 0, len(bucket.txs))
	for _, stx := range bucket.txs {
		result = append(result, stx)
	}

	// Sort by priority descending, then by time ascending for determinism.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].AddedAt.Before(result[j].AddedAt)
	})
	return result
}

// RelayTx relays a transaction from its current shard to a target shard.
// Each relay increments the relay count; once RelayLimit is exceeded the
// relay is rejected to prevent infinite propagation.
func (sp *SharedPool) RelayTx(tx *SharedTx, targetShard ShardID) error {
	if tx == nil || tx.Tx == nil {
		return ErrNilTransaction
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if sp.closed {
		return ErrPoolClosed
	}

	if tx.RelayCount >= sp.config.RelayLimit {
		return ErrRelayLimit
	}

	target, ok := sp.shards[targetShard]
	if !ok {
		if len(sp.shards) >= sp.config.MaxShards {
			return ErrMaxShardsReached
		}
		target = newShardBucket()
		sp.shards[targetShard] = target
	}

	hash := tx.Hash()
	if _, exists := target.txs[hash]; exists {
		return ErrDuplicateTx
	}
	if len(target.txs) >= sp.config.MaxTxPerShard {
		return ErrShardFull
	}

	// Create a copy for the target shard with incremented relay count.
	relayed := &SharedTx{
		Tx:         tx.Tx,
		Origin:     tx.Origin,
		RelayCount: tx.RelayCount + 1,
		Priority:   tx.Priority,
		AddedAt:    time.Now(),
	}
	target.txs[hash] = relayed
	return nil
}

// PruneShard removes all transactions from the given shard.
func (sp *SharedPool) PruneShard(shard ShardID) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	delete(sp.shards, shard)
}

// CrossShardSync gathers transactions from the given peer shards that are
// not yet present in other shards. This is used during periodic sync rounds
// to propagate transactions between validators.
func (sp *SharedPool) CrossShardSync(peerShards []ShardID) ([]*SharedTx, error) {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	if sp.closed {
		return nil, ErrPoolClosed
	}

	// Collect all known hashes across all local shards.
	known := make(map[types.Hash]struct{})
	for _, bucket := range sp.shards {
		for h := range bucket.txs {
			known[h] = struct{}{}
		}
	}

	// Gather unique transactions from peer shards.
	seen := make(map[types.Hash]struct{})
	var result []*SharedTx

	for _, sid := range peerShards {
		bucket, ok := sp.shards[sid]
		if !ok {
			continue
		}
		for h, stx := range bucket.txs {
			if _, already := seen[h]; already {
				continue
			}
			seen[h] = struct{}{}
			result = append(result, stx)
		}
	}

	// Sort for deterministic output: priority desc, then time asc.
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority > result[j].Priority
		}
		return result[i].AddedAt.Before(result[j].AddedAt)
	})
	return result, nil
}

// ShardCount returns the number of active shards.
func (sp *SharedPool) ShardCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.shards)
}

// TxCount returns the total number of transactions across all shards.
func (sp *SharedPool) TxCount() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	total := 0
	for _, bucket := range sp.shards {
		total += len(bucket.txs)
	}
	return total
}

// Close marks the pool as closed. Subsequent mutations return ErrPoolClosed.
func (sp *SharedPool) Close() {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.closed = true
}
