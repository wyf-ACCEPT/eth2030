package engine

import (
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// PayloadCacheConfig holds configuration for the payload cache.
type PayloadCacheConfig struct {
	// MaxPayloads is the maximum number of cached payloads before LRU eviction.
	MaxPayloads int
	// PayloadTTL is how long a payload lives before expiry.
	PayloadTTL time.Duration
	// MaxPayloadSize is the maximum size in bytes for a single payload.
	MaxPayloadSize int
}

// DefaultPayloadCacheConfig returns a PayloadCacheConfig with sensible defaults.
func DefaultPayloadCacheConfig() PayloadCacheConfig {
	return PayloadCacheConfig{
		MaxPayloads:    32,
		PayloadTTL:     120 * time.Second,
		MaxPayloadSize: 10 * 1024 * 1024, // 10 MB
	}
}

// CachedPayload represents a payload stored in the cache.
type CachedPayload struct {
	ID           types.Hash
	ParentHash   types.Hash
	FeeRecipient types.Address
	Timestamp    uint64
	GasLimit     uint64
	BaseFee      *big.Int
	Transactions [][]byte
	CreatedAt    time.Time
	Size         int
}

// payloadEntry wraps a CachedPayload with LRU ordering metadata.
type payloadEntry struct {
	payload    *CachedPayload
	accessTime time.Time
}

// PayloadCache is an LRU cache for execution payloads with TTL-based expiry.
type PayloadCache struct {
	mu      sync.RWMutex
	config  PayloadCacheConfig
	entries map[types.Hash]*payloadEntry
}

// NewPayloadCache creates a new PayloadCache with the given configuration.
func NewPayloadCache(config PayloadCacheConfig) *PayloadCache {
	return &PayloadCache{
		config:  config,
		entries: make(map[types.Hash]*payloadEntry),
	}
}

// Store adds a payload to the cache. If the cache is at capacity, the
// least-recently-used entry is evicted first.
func (pc *PayloadCache) Store(payload *CachedPayload) error {
	if payload == nil {
		return errors.New("nil payload")
	}
	if payload.Size > pc.config.MaxPayloadSize {
		return errors.New("payload exceeds maximum size")
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Evict LRU entry if at capacity and this is a new key.
	if _, exists := pc.entries[payload.ID]; !exists && len(pc.entries) >= pc.config.MaxPayloads {
		pc.evictOldest()
	}

	pc.entries[payload.ID] = &payloadEntry{
		payload:    payload,
		accessTime: time.Now(),
	}
	return nil
}

// Get retrieves a payload by ID. Returns the payload and true if found,
// nil and false otherwise. Accessing a payload updates its LRU timestamp.
func (pc *PayloadCache) Get(id types.Hash) (*CachedPayload, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	entry, ok := pc.entries[id]
	if !ok {
		return nil, false
	}
	entry.accessTime = time.Now()
	return entry.payload, true
}

// Delete removes a payload from the cache.
func (pc *PayloadCache) Delete(id types.Hash) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.entries, id)
}

// Prune removes all payloads that have exceeded their TTL.
func (pc *PayloadCache) Prune() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	now := time.Now()
	for id, entry := range pc.entries {
		if now.Sub(entry.payload.CreatedAt) > pc.config.PayloadTTL {
			delete(pc.entries, id)
		}
	}
}

// Size returns the number of cached payloads.
func (pc *PayloadCache) Size() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return len(pc.entries)
}

// TotalBytes returns the total estimated memory usage of all cached payloads.
func (pc *PayloadCache) TotalBytes() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	total := 0
	for _, entry := range pc.entries {
		total += entry.payload.Size
	}
	return total
}

// evictOldest removes the least-recently-accessed entry. Caller must hold mu.
func (pc *PayloadCache) evictOldest() {
	var oldestID types.Hash
	var oldestTime time.Time
	first := true

	for id, entry := range pc.entries {
		if first || entry.accessTime.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.accessTime
			first = false
		}
	}
	if !first {
		delete(pc.entries, oldestID)
	}
}
