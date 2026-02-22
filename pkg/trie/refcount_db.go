// refcount_db.go provides a reference-counting trie database layer built on
// top of NodeDatabase. It tracks how many state roots reference each node and
// enables safe garbage collection of unreferenced nodes. This is the database
// abstraction used by the state manager to coordinate node lifecycle across
// multiple block states.
package trie

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

var (
	// ErrRefCountNegative is returned when a dereference causes a negative count.
	ErrRefCountNegative = errors.New("trie db: reference count went negative")

	// ErrDatabaseClosed is returned when operating on a closed database.
	ErrDatabaseClosed = errors.New("trie db: database is closed")
)

// RefCountDB wraps a NodeDatabase with reference counting for garbage
// collection. Each node's reference count tracks how many active trie roots
// reference it. When the count drops to zero, the node becomes eligible for
// removal. All methods are safe for concurrent use.
type RefCountDB struct {
	mu       sync.RWMutex
	inner    *NodeDatabase
	refs     map[types.Hash]int64 // reference counts per node hash
	size     int64                // total cached data size in bytes
	nodeSize map[types.Hash]int   // data size per node
	closed   bool
}

// NewRefCountDB creates a new reference-counting database layer backed by
// the given NodeDatabase.
func NewRefCountDB(inner *NodeDatabase) *RefCountDB {
	return &RefCountDB{
		inner:    inner,
		refs:     make(map[types.Hash]int64),
		nodeSize: make(map[types.Hash]int),
	}
}

// Node retrieves a trie node by hash, delegating to the inner database.
func (db *RefCountDB) Node(hash types.Hash) ([]byte, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return nil, ErrDatabaseClosed
	}
	return db.inner.Node(hash)
}

// InsertNode stores a trie node and initializes its reference count to zero
// if not already tracked. The node is not considered "alive" until Reference
// is called.
func (db *RefCountDB) InsertNode(hash types.Hash, data []byte) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return
	}

	db.inner.InsertNode(hash, data)
	if _, exists := db.refs[hash]; !exists {
		db.refs[hash] = 0
		db.nodeSize[hash] = len(data)
		db.size += int64(len(data))
	}
}

// Reference increments the reference count for a node hash. This is called
// when a new trie root is committed that includes this node.
func (db *RefCountDB) Reference(hash types.Hash) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return
	}
	db.refs[hash]++
}

// Dereference decrements the reference count for a node hash. Returns true
// if the reference count has reached zero (node is now unreferenced).
func (db *RefCountDB) Dereference(hash types.Hash) (bool, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return false, ErrDatabaseClosed
	}

	count, exists := db.refs[hash]
	if !exists {
		return false, nil
	}
	count--
	if count < 0 {
		return false, ErrRefCountNegative
	}
	db.refs[hash] = count
	return count == 0, nil
}

// DeleteNode removes a node from the database and its reference tracking.
// This should only be called for nodes with zero references.
func (db *RefCountDB) DeleteNode(hash types.Hash) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return
	}

	if sz, ok := db.nodeSize[hash]; ok {
		db.size -= int64(sz)
	}
	delete(db.refs, hash)
	delete(db.nodeSize, hash)
	// Note: NodeDatabase does not have a delete method, so we track
	// deletion at the RefCountDB layer for GC purposes.
}

// RefCount returns the current reference count for a node hash.
func (db *RefCountDB) RefCount(hash types.Hash) int64 {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.refs[hash]
}

// Size returns the total tracked data size in bytes.
func (db *RefCountDB) Size() int64 {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.size
}

// NodeCount returns the number of tracked nodes.
func (db *RefCountDB) NodeCount() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.refs)
}

// UnreferencedNodes returns all node hashes with a reference count of zero.
// These are candidates for garbage collection.
func (db *RefCountDB) UnreferencedNodes() []types.Hash {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var result []types.Hash
	for h, count := range db.refs {
		if count == 0 {
			result = append(result, h)
		}
	}
	return result
}

// CollectGarbage removes all nodes with zero reference counts. Returns the
// number of nodes removed and the total bytes freed.
func (db *RefCountDB) CollectGarbage() (int, int64) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return 0, 0
	}

	var removed int
	var freed int64
	for h, count := range db.refs {
		if count == 0 {
			if sz, ok := db.nodeSize[h]; ok {
				freed += int64(sz)
				db.size -= int64(sz)
			}
			delete(db.refs, h)
			delete(db.nodeSize, h)
			removed++
		}
	}
	return removed, freed
}

// ReferenceMany increments reference counts for multiple node hashes at once.
// This is more efficient than calling Reference individually when committing
// a trie that touches many nodes.
func (db *RefCountDB) ReferenceMany(hashes []types.Hash) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return
	}
	for _, h := range hashes {
		db.refs[h]++
	}
}

// DereferenceMany decrements reference counts for multiple node hashes.
// Returns the hashes that reached zero references.
func (db *RefCountDB) DereferenceMany(hashes []types.Hash) []types.Hash {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}

	var zeroed []types.Hash
	for _, h := range hashes {
		count, exists := db.refs[h]
		if !exists {
			continue
		}
		count--
		if count <= 0 {
			db.refs[h] = 0
			zeroed = append(zeroed, h)
		} else {
			db.refs[h] = count
		}
	}
	return zeroed
}

// Close marks the database as closed, preventing further operations.
func (db *RefCountDB) Close() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.closed = true
}

// Inner returns the underlying NodeDatabase for direct access when needed.
func (db *RefCountDB) Inner() *NodeDatabase {
	return db.inner
}

// RefCountStats holds statistics about the reference-counting database.
type RefCountStats struct {
	TotalNodes      int
	ReferencedNodes int
	UnreferencedCnt int
	TotalSize       int64
	MaxRefCount     int64
}

// Stats returns a snapshot of database statistics.
func (db *RefCountDB) Stats() RefCountStats {
	db.mu.RLock()
	defer db.mu.RUnlock()

	stats := RefCountStats{
		TotalNodes: len(db.refs),
		TotalSize:  db.size,
	}

	for _, count := range db.refs {
		if count > 0 {
			stats.ReferencedNodes++
		} else {
			stats.UnreferencedCnt++
		}
		if count > stats.MaxRefCount {
			stats.MaxRefCount = count
		}
	}
	return stats
}
