// gas_cache.go implements a gas calculation cache for repeated opcode gas
// lookups. It caches dynamically computed gas costs for operations like
// SLOAD warm/cold, SSTORE net metering, and CALL with value transfer.
// The cache supports per-block invalidation, tracks hit/miss ratios,
// integrates with EIP-2929 warm/cold tracking, and provides gas budget
// tracking with early abort for speculative parallel execution.
package vm

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// OpGasCache constants.
const (
	// OpGasCacheDefaultSize is the default max entries per block.
	OpGasCacheDefaultSize = 16384

	// GasBudgetUnlimited indicates no gas budget limit.
	GasBudgetUnlimited uint64 = 0
)

// OpGasCacheConfig configures the OpGasCache.
type OpGasCacheConfig struct {
	MaxEntries int  // Maximum cache entries per block.
	EnableSpec bool // Enable speculative gas cost caching.
}

// DefaultOpGasCacheConfig returns sensible defaults.
func DefaultOpGasCacheConfig() OpGasCacheConfig {
	return OpGasCacheConfig{
		MaxEntries: OpGasCacheDefaultSize,
		EnableSpec: true,
	}
}

// gasCacheKey identifies a cached gas computation.
type gasCacheKey struct {
	Op   OpCode
	Addr types.Address
	Slot types.Hash
}

// GasCacheEntry holds a cached gas cost with metadata.
type GasCacheEntry struct {
	Op       OpCode
	Addr     types.Address
	Slot     types.Hash
	GasCost  uint64
	IsWarm   bool   // Whether the access was warm at time of caching.
	BlockNum uint64 // Block number when this entry was cached.
}

// OpGasCacheStats tracks cache performance metrics.
type OpGasCacheStats struct {
	Hits      atomic.Uint64
	Misses    atomic.Uint64
	Evictions atomic.Uint64
	Inserts   atomic.Uint64
	Resets    atomic.Uint64
}

// HitRate returns the cache hit rate (0.0 to 1.0).
func (s *OpGasCacheStats) HitRate() float64 {
	hits := s.Hits.Load()
	total := hits + s.Misses.Load()
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// Snapshot returns an immutable copy of the stats.
func (s *OpGasCacheStats) Snapshot() OpGasCacheStatsSnapshot {
	return OpGasCacheStatsSnapshot{
		Hits:      s.Hits.Load(),
		Misses:    s.Misses.Load(),
		Evictions: s.Evictions.Load(),
		Inserts:   s.Inserts.Load(),
		Resets:    s.Resets.Load(),
	}
}

// OpGasCacheStatsSnapshot is an immutable snapshot of stats.
type OpGasCacheStatsSnapshot struct {
	Hits      uint64
	Misses    uint64
	Evictions uint64
	Inserts   uint64
	Resets    uint64
}

// String returns a human-readable stats summary.
func (s OpGasCacheStatsSnapshot) String() string {
	total := s.Hits + s.Misses
	var rate float64
	if total > 0 {
		rate = float64(s.Hits) / float64(total) * 100
	}
	return fmt.Sprintf("hits=%d misses=%d rate=%.1f%% evictions=%d inserts=%d resets=%d",
		s.Hits, s.Misses, rate, s.Evictions, s.Inserts, s.Resets)
}

// OpGasCache caches dynamically computed gas costs for EVM opcodes.
// It is designed for per-block use: all entries are invalidated when the
// block number changes. Safe for concurrent use.
type OpGasCache struct {
	mu       sync.RWMutex
	config   OpGasCacheConfig
	entries  map[gasCacheKey]GasCacheEntry
	blockNum uint64
	stats    *OpGasCacheStats
}

// NewOpGasCache creates an OpGasCache with the given config.
func NewOpGasCache(config OpGasCacheConfig) *OpGasCache {
	if config.MaxEntries <= 0 {
		config.MaxEntries = OpGasCacheDefaultSize
	}
	return &OpGasCache{
		config:  config,
		entries: make(map[gasCacheKey]GasCacheEntry),
		stats:   &OpGasCacheStats{},
	}
}

// Lookup retrieves a cached gas cost. Returns the cost and whether it
// was found. If the block number has changed since caching, the entry
// is treated as a miss.
func (c *OpGasCache) Lookup(op OpCode, addr types.Address, slot types.Hash, blockNum uint64) (uint64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.blockNum != blockNum {
		c.stats.Misses.Add(1)
		return 0, false
	}

	key := gasCacheKey{Op: op, Addr: addr, Slot: slot}
	entry, ok := c.entries[key]
	if !ok {
		c.stats.Misses.Add(1)
		return 0, false
	}
	c.stats.Hits.Add(1)
	return entry.GasCost, true
}

// Store caches a gas cost for a specific opcode/address/slot combination.
// If the block number has changed, the cache is invalidated first.
func (c *OpGasCache) Store(op OpCode, addr types.Address, slot types.Hash, gasCost uint64, isWarm bool, blockNum uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Invalidate on block transition.
	if c.blockNum != blockNum {
		c.entries = make(map[gasCacheKey]GasCacheEntry)
		c.blockNum = blockNum
		c.stats.Resets.Add(1)
	}

	key := gasCacheKey{Op: op, Addr: addr, Slot: slot}

	// Evict if at capacity.
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.config.MaxEntries {
		c.evictOneLocked()
	}

	c.entries[key] = GasCacheEntry{
		Op:       op,
		Addr:     addr,
		Slot:     slot,
		GasCost:  gasCost,
		IsWarm:   isWarm,
		BlockNum: blockNum,
	}
	c.stats.Inserts.Add(1)
}

// Invalidate removes a specific entry from the cache. This is called
// when a storage write changes the warm/cold status or gas cost of a slot.
func (c *OpGasCache) Invalidate(op OpCode, addr types.Address, slot types.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := gasCacheKey{Op: op, Addr: addr, Slot: slot}
	delete(c.entries, key)
}

// InvalidateSlot removes all cached entries for a specific storage slot
// across all opcodes. Used when SSTORE modifies a slot.
func (c *OpGasCache) InvalidateSlot(addr types.Address, slot types.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.Addr == addr && key.Slot == slot {
			delete(c.entries, key)
		}
	}
}

// InvalidateAddress removes all cached entries for an address.
// Used on SELFDESTRUCT or CREATE.
func (c *OpGasCache) InvalidateAddress(addr types.Address) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if key.Addr == addr {
			delete(c.entries, key)
			c.stats.Evictions.Add(1)
		}
	}
}

// Reset clears all entries and resets the block number.
func (c *OpGasCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[gasCacheKey]GasCacheEntry)
	c.blockNum = 0
	c.stats.Resets.Add(1)
}

// Size returns the number of entries in the cache.
func (c *OpGasCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// BlockNumber returns the current block number the cache is valid for.
func (c *OpGasCache) BlockNumber() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.blockNum
}

// Stats returns the cache stats collector.
func (c *OpGasCache) Stats() *OpGasCacheStats {
	return c.stats
}

// Entries returns all cached entries (for debugging/testing).
func (c *OpGasCache) Entries() []GasCacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]GasCacheEntry, 0, len(c.entries))
	for _, e := range c.entries {
		result = append(result, e)
	}
	return result
}

// evictOneLocked removes one entry to make room. Must be called with mu held.
func (c *OpGasCache) evictOneLocked() {
	for key := range c.entries {
		delete(c.entries, key)
		c.stats.Evictions.Add(1)
		return
	}
}

// GasBudgetTracker tracks gas consumption during execution and supports
// early abort when a gas budget is exhausted. It integrates with the
// OpGasCache for speculative parallel execution where gas costs are
// pre-estimated from cache entries.
type GasBudgetTracker struct {
	mu        sync.Mutex
	budget    uint64 // Total gas budget (0 = unlimited).
	consumed  uint64 // Gas consumed so far.
	estimated uint64 // Gas estimated from cache (speculative).
	aborted   bool   // Whether execution was aborted due to budget.
	cache     *OpGasCache
}

// NewGasBudgetTracker creates a tracker with the given budget.
// A budget of 0 means unlimited.
func NewGasBudgetTracker(budget uint64, cache *OpGasCache) *GasBudgetTracker {
	return &GasBudgetTracker{
		budget: budget,
		cache:  cache,
	}
}

// Consume records actual gas consumption. Returns false if the budget
// is exhausted (caller should abort execution).
func (g *GasBudgetTracker) Consume(amount uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.aborted {
		return false
	}
	g.consumed += amount
	if g.budget > 0 && g.consumed > g.budget {
		g.aborted = true
		return false
	}
	return true
}

// Estimate records speculative gas from cache. Does not affect the
// actual consumed counter but tracks estimated costs for planning.
func (g *GasBudgetTracker) Estimate(amount uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.estimated += amount
}

// WouldExceedBudget returns true if consuming the given amount would
// exceed the budget. Does not actually consume gas.
func (g *GasBudgetTracker) WouldExceedBudget(amount uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.budget == 0 {
		return false
	}
	return g.consumed+amount > g.budget
}

// Remaining returns the remaining gas budget. Returns MaxUint64 for unlimited.
func (g *GasBudgetTracker) Remaining() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.budget == 0 {
		return ^uint64(0)
	}
	if g.consumed >= g.budget {
		return 0
	}
	return g.budget - g.consumed
}

// Consumed returns the total gas consumed so far.
func (g *GasBudgetTracker) Consumed() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.consumed
}

// Estimated returns the total speculative gas estimated from cache.
func (g *GasBudgetTracker) Estimated() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.estimated
}

// IsAborted returns whether the tracker has been aborted.
func (g *GasBudgetTracker) IsAborted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.aborted
}

// Budget returns the total gas budget.
func (g *GasBudgetTracker) Budget() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.budget
}

// Reset resets the tracker for reuse.
func (g *GasBudgetTracker) Reset(budget uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.budget = budget
	g.consumed = 0
	g.estimated = 0
	g.aborted = false
}

// Utilization returns the fraction of budget consumed (0.0-1.0).
// Returns 0.0 for unlimited budgets.
func (g *GasBudgetTracker) Utilization() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.budget == 0 {
		return 0.0
	}
	return float64(g.consumed) / float64(g.budget)
}

// LookupAndConsume combines a cache lookup with gas consumption.
// If the gas cost is found in cache, it is consumed. Returns the
// cost, whether it was a cache hit, and whether the budget was exceeded.
func (g *GasBudgetTracker) LookupAndConsume(op OpCode, addr types.Address, slot types.Hash, blockNum uint64) (cost uint64, hit bool, ok bool) {
	if g.cache == nil {
		return 0, false, true
	}
	cost, hit = g.cache.Lookup(op, addr, slot, blockNum)
	if !hit {
		return 0, false, true
	}
	ok = g.Consume(cost)
	return cost, true, ok
}
