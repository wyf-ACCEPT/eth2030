package state

import (
	"math/big"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// CacheStats holds hit/miss statistics for AccountCache.
type CacheStats struct {
	Hits   uint64
	Misses uint64
}

// lruEntry is a doubly-linked list node for the LRU cache.
type lruEntry struct {
	key  types.Address
	acct *types.Account
	prev *lruEntry
	next *lruEntry
}

// AccountCache provides fast in-memory LRU caching of account state.
// It is safe for concurrent use.
type AccountCache struct {
	mu      sync.Mutex
	maxSize int
	entries map[types.Address]*lruEntry

	// Doubly-linked list for LRU ordering.
	// head is the most recently used; tail is the least recently used.
	head *lruEntry
	tail *lruEntry

	hits   atomic.Uint64
	misses atomic.Uint64
}

// NewAccountCache creates a new AccountCache with LRU eviction at maxSize entries.
// maxSize must be at least 1; if less, it is set to 1.
func NewAccountCache(maxSize int) *AccountCache {
	if maxSize < 1 {
		maxSize = 1
	}
	return &AccountCache{
		maxSize: maxSize,
		entries: make(map[types.Address]*lruEntry, maxSize),
	}
}

// Get retrieves an account from the cache. Returns a deep copy of the account
// and true if found, or nil and false if not in the cache.
// The accessed entry is promoted to the front (most recently used).
func (c *AccountCache) Get(addr types.Address) (*types.Account, bool) {
	c.mu.Lock()
	entry, ok := c.entries[addr]
	if !ok {
		c.mu.Unlock()
		c.misses.Add(1)
		return nil, false
	}
	c.moveToFront(entry)
	acct := copyAccount(entry.acct)
	c.mu.Unlock()
	c.hits.Add(1)
	return acct, true
}

// Put stores an account in the cache. If the cache is full, the least recently
// used entry is evicted. The account is deep-copied before storage.
func (c *AccountCache) Put(addr types.Address, acct *types.Account) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[addr]; ok {
		// Update existing entry.
		entry.acct = copyAccount(acct)
		c.moveToFront(entry)
		return
	}

	// Evict if at capacity.
	if len(c.entries) >= c.maxSize {
		c.removeTail()
	}

	entry := &lruEntry{
		key:  addr,
		acct: copyAccount(acct),
	}
	c.entries[addr] = entry
	c.pushFront(entry)
}

// Delete removes an account from the cache.
func (c *AccountCache) Delete(addr types.Address) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[addr]
	if !ok {
		return
	}
	c.removeEntry(entry)
	delete(c.entries, addr)
}

// Len returns the number of entries currently in the cache.
func (c *AccountCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Clear removes all entries from the cache and resets statistics.
func (c *AccountCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[types.Address]*lruEntry, c.maxSize)
	c.head = nil
	c.tail = nil
	c.hits.Store(0)
	c.misses.Store(0)
}

// Stats returns a snapshot of the cache hit/miss statistics.
func (c *AccountCache) Stats() CacheStats {
	return CacheStats{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
	}
}

// moveToFront moves an existing entry to the front of the LRU list.
// Caller must hold c.mu.
func (c *AccountCache) moveToFront(entry *lruEntry) {
	if c.head == entry {
		return
	}
	c.removeEntry(entry)
	c.pushFront(entry)
}

// pushFront inserts an entry at the front of the LRU list.
// Caller must hold c.mu.
func (c *AccountCache) pushFront(entry *lruEntry) {
	entry.prev = nil
	entry.next = c.head
	if c.head != nil {
		c.head.prev = entry
	}
	c.head = entry
	if c.tail == nil {
		c.tail = entry
	}
}

// removeEntry detaches an entry from the LRU list without removing it from the map.
// Caller must hold c.mu.
func (c *AccountCache) removeEntry(entry *lruEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		c.head = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		c.tail = entry.prev
	}
	entry.prev = nil
	entry.next = nil
}

// removeTail evicts the least recently used entry.
// Caller must hold c.mu.
func (c *AccountCache) removeTail() {
	if c.tail == nil {
		return
	}
	evicted := c.tail
	c.removeEntry(evicted)
	delete(c.entries, evicted.key)
}

// copyAccount returns a deep copy of an account so mutations to the returned
// value do not affect the cached copy.
func copyAccount(acct *types.Account) *types.Account {
	if acct == nil {
		return nil
	}
	cp := &types.Account{
		Nonce: acct.Nonce,
		Root:  acct.Root,
	}
	if acct.Balance != nil {
		cp.Balance = new(big.Int).Set(acct.Balance)
	}
	if acct.CodeHash != nil {
		cp.CodeHash = make([]byte, len(acct.CodeHash))
		copy(cp.CodeHash, acct.CodeHash)
	}
	return cp
}
