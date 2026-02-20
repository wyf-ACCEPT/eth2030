// Package shared implements cross-node transaction propagation with dedup
// and priority relay. SharedMempool provides a bloom-filter-based dedup layer
// on top of a peer-aware transaction cache, complementing the shard-oriented
// SharedPool that handles cross-validator coordination.
package shared

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Shared mempool errors.
var (
	ErrSharedMempoolFull = errors.New("shared mempool: cache is full")
	ErrSharedTxDuplicate = errors.New("shared mempool: duplicate transaction")
	ErrSharedTxInvalid   = errors.New("shared mempool: invalid transaction")
)

// SharedMempoolConfig configures the cross-node shared mempool.
type SharedMempoolConfig struct {
	MaxPeers        int           // maximum connected peers
	MaxCacheSize    int           // maximum number of cached transactions
	RelayInterval   time.Duration // interval between relay rounds
	BloomFilterSize uint32        // bloom filter bit count
}

// DefaultSharedMempoolConfig returns sensible defaults for production use.
func DefaultSharedMempoolConfig() SharedMempoolConfig {
	return SharedMempoolConfig{
		MaxPeers:        50,
		MaxCacheSize:    4096,
		RelayInterval:   500 * time.Millisecond,
		BloomFilterSize: 65536,
	}
}

// SharedMempoolTx represents a transaction in the shared mempool with
// metadata for cross-node propagation and priority ordering.
type SharedMempoolTx struct {
	Hash         types.Hash // transaction hash
	Sender       types.Address
	Nonce        uint64
	GasPrice     uint64
	Data         []byte
	ReceivedFrom string    // peer ID the tx was received from
	ReceivedAt   time.Time // time the tx was added
}

// relayRecord tracks which peers a transaction has been relayed to.
type relayRecord struct {
	peers map[string]struct{}
}

// simpleBloom is a lightweight bloom filter for fast dedup checks.
// It uses multiple hash positions derived from the input hash bytes.
type simpleBloom struct {
	bits []byte
	size uint32
}

func newSimpleBloom(size uint32) *simpleBloom {
	if size == 0 {
		size = 65536
	}
	// Round up to nearest byte.
	byteSize := (size + 7) / 8
	return &simpleBloom{
		bits: make([]byte, byteSize),
		size: size,
	}
}

// add marks a hash in the bloom filter using 3 independent positions.
func (b *simpleBloom) add(h types.Hash) {
	for i := 0; i < 3; i++ {
		pos := b.position(h, i)
		b.bits[pos/8] |= 1 << (pos % 8)
	}
}

// contains checks if a hash might be in the bloom filter.
func (b *simpleBloom) contains(h types.Hash) bool {
	for i := 0; i < 3; i++ {
		pos := b.position(h, i)
		if b.bits[pos/8]&(1<<(pos%8)) == 0 {
			return false
		}
	}
	return true
}

// position computes the i-th hash position from the hash bytes.
func (b *simpleBloom) position(h types.Hash, i int) uint32 {
	// Use different pairs of bytes for each hash function.
	offset := i * 4
	if offset+3 >= types.HashLength {
		offset = 0
	}
	val := uint32(h[offset])<<24 | uint32(h[offset+1])<<16 |
		uint32(h[offset+2])<<8 | uint32(h[offset+3])
	return val % b.size
}

// peerConn represents a connected peer in the shared mempool.
type peerConn struct {
	id        string
	connected time.Time
}

// SharedMempool manages cross-node transaction propagation with dedup
// and priority relay. It is safe for concurrent use.
type SharedMempool struct {
	mu     sync.RWMutex
	config SharedMempoolConfig

	txCache map[types.Hash]*SharedMempoolTx // all cached transactions
	bloom   *simpleBloom                    // fast dedup filter
	relays  map[types.Hash]*relayRecord     // relay tracking per tx
	peers   map[string]*peerConn            // connected peers
}

// NewSharedMempool creates a new shared mempool with the given configuration.
func NewSharedMempool(config SharedMempoolConfig) *SharedMempool {
	return &SharedMempool{
		config:  config,
		txCache: make(map[types.Hash]*SharedMempoolTx),
		bloom:   newSimpleBloom(config.BloomFilterSize),
		relays:  make(map[types.Hash]*relayRecord),
		peers:   make(map[string]*peerConn),
	}
}

// AddTransaction adds a transaction to the shared mempool with dedup check.
// Returns ErrSharedTxDuplicate if the transaction is already known,
// ErrSharedMempoolFull if the cache is at capacity, or ErrSharedTxInvalid
// if the transaction hash is zero.
func (sm *SharedMempool) AddTransaction(tx SharedMempoolTx) error {
	if tx.Hash.IsZero() {
		return ErrSharedTxInvalid
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check exact dedup via map.
	if _, exists := sm.txCache[tx.Hash]; exists {
		return ErrSharedTxDuplicate
	}

	// Check capacity.
	if sm.config.MaxCacheSize > 0 && len(sm.txCache) >= sm.config.MaxCacheSize {
		return ErrSharedMempoolFull
	}

	if tx.ReceivedAt.IsZero() {
		tx.ReceivedAt = time.Now()
	}

	txCopy := tx
	sm.txCache[tx.Hash] = &txCopy
	sm.bloom.add(tx.Hash)
	return nil
}

// GetPendingTxs returns up to limit transactions, ordered by gas price
// descending (highest priority first). If limit <= 0, returns all.
func (sm *SharedMempool) GetPendingTxs(limit int) []SharedMempoolTx {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make([]SharedMempoolTx, 0, len(sm.txCache))
	for _, tx := range sm.txCache {
		result = append(result, *tx)
	}

	// Sort by gas price descending, then by received time ascending.
	sort.Slice(result, func(i, j int) bool {
		if result[i].GasPrice != result[j].GasPrice {
			return result[i].GasPrice > result[j].GasPrice
		}
		return result[i].ReceivedAt.Before(result[j].ReceivedAt)
	})

	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result
}

// MarkRelayed records that a transaction has been relayed to a specific peer.
func (sm *SharedMempool) MarkRelayed(txHash types.Hash, peerID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	rec, ok := sm.relays[txHash]
	if !ok {
		rec = &relayRecord{peers: make(map[string]struct{})}
		sm.relays[txHash] = rec
	}
	rec.peers[peerID] = struct{}{}
}

// IsRelayedTo checks whether a transaction has been relayed to a peer.
func (sm *SharedMempool) IsRelayedTo(txHash types.Hash, peerID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	rec, ok := sm.relays[txHash]
	if !ok {
		return false
	}
	_, found := rec.peers[peerID]
	return found
}

// IsKnown performs a fast bloom filter check to determine if a transaction
// hash might already be in the mempool. May return false positives but
// never false negatives for transactions that were added.
func (sm *SharedMempool) IsKnown(txHash types.Hash) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.bloom.contains(txHash)
}

// AddPeer registers a peer connection. Returns false if the peer limit
// is reached or the peer is already connected.
func (sm *SharedMempool) AddPeer(peerID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, exists := sm.peers[peerID]; exists {
		return false
	}
	if sm.config.MaxPeers > 0 && len(sm.peers) >= sm.config.MaxPeers {
		return false
	}
	sm.peers[peerID] = &peerConn{
		id:        peerID,
		connected: time.Now(),
	}
	return true
}

// RemovePeer removes a peer connection.
func (sm *SharedMempool) RemovePeer(peerID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.peers, peerID)
}

// PeerCount returns the number of connected peers.
func (sm *SharedMempool) PeerCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.peers)
}

// TxCount returns the number of cached transactions.
func (sm *SharedMempool) TxCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.txCache)
}

// EvictStale removes transactions older than maxAge seconds from the cache.
// Returns the number of transactions evicted.
func (sm *SharedMempool) EvictStale(maxAge int64) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-time.Duration(maxAge) * time.Second)
	evicted := 0

	for hash, tx := range sm.txCache {
		if tx.ReceivedAt.Before(cutoff) {
			delete(sm.txCache, hash)
			delete(sm.relays, hash)
			evicted++
		}
	}
	return evicted
}

// GetTransaction retrieves a transaction by hash from the cache.
// Returns nil if not found.
func (sm *SharedMempool) GetTransaction(hash types.Hash) *SharedMempoolTx {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	tx, ok := sm.txCache[hash]
	if !ok {
		return nil
	}
	// Return a copy.
	txCopy := *tx
	return &txCopy
}

// RemoveTransaction removes a single transaction from the cache.
// Returns true if the transaction was found and removed.
func (sm *SharedMempool) RemoveTransaction(hash types.Hash) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if _, ok := sm.txCache[hash]; !ok {
		return false
	}
	delete(sm.txCache, hash)
	delete(sm.relays, hash)
	return true
}
