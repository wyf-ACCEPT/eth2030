// diff_tracker.go provides a state diff accumulator for tracking changes
// between trie states. It collects inserted, updated, and deleted keys as
// two tries are compared, producing a structured diff that can be used for
// state sync, change notification, and rollback operations.
//
// This complements the DiffIterator in node_iterator.go by providing a
// higher-level API that categorizes changes and supports accumulation
// across multiple comparisons.
package trie

import (
	"bytes"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// DiffEntry represents a single change between two trie states.
type DiffEntry struct {
	Key      []byte // the raw key that changed
	OldValue []byte // previous value (nil if inserted)
	NewValue []byte // new value (nil if deleted)
}

// IsInsert returns true if this entry represents a new key.
func (d *DiffEntry) IsInsert() bool {
	return d.OldValue == nil && d.NewValue != nil
}

// IsDelete returns true if this entry represents a removed key.
func (d *DiffEntry) IsDelete() bool {
	return d.OldValue != nil && d.NewValue == nil
}

// IsUpdate returns true if this entry represents a value change.
func (d *DiffEntry) IsUpdate() bool {
	return d.OldValue != nil && d.NewValue != nil
}

// DiffTracker accumulates state differences between trie snapshots.
// It is safe for concurrent use.
type DiffTracker struct {
	mu      sync.RWMutex
	entries map[string]*DiffEntry // keyed by string(key)
}

// NewDiffTracker creates a new empty DiffTracker.
func NewDiffTracker() *DiffTracker {
	return &DiffTracker{
		entries: make(map[string]*DiffEntry),
	}
}

// RecordInsert records that a key was inserted with the given value.
func (dt *DiffTracker) RecordInsert(key, value []byte) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	k := string(key)
	if existing, ok := dt.entries[k]; ok {
		// If there was a prior delete of the same key, this becomes an update.
		existing.NewValue = cloneBytes(value)
	} else {
		dt.entries[k] = &DiffEntry{
			Key:      cloneBytes(key),
			NewValue: cloneBytes(value),
		}
	}
}

// RecordDelete records that a key was deleted. oldValue is the value that
// was removed (may be nil if unknown).
func (dt *DiffTracker) RecordDelete(key, oldValue []byte) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	k := string(key)
	if existing, ok := dt.entries[k]; ok {
		// If there was a prior insert, and now it's deleted, they cancel out.
		if existing.OldValue == nil {
			delete(dt.entries, k)
			return
		}
		// Prior update becomes a delete.
		existing.NewValue = nil
	} else {
		dt.entries[k] = &DiffEntry{
			Key:      cloneBytes(key),
			OldValue: cloneBytes(oldValue),
		}
	}
}

// RecordUpdate records that a key's value changed from old to new.
func (dt *DiffTracker) RecordUpdate(key, oldValue, newValue []byte) {
	dt.mu.Lock()
	defer dt.mu.Unlock()

	k := string(key)
	if existing, ok := dt.entries[k]; ok {
		existing.NewValue = cloneBytes(newValue)
	} else {
		dt.entries[k] = &DiffEntry{
			Key:      cloneBytes(key),
			OldValue: cloneBytes(oldValue),
			NewValue: cloneBytes(newValue),
		}
	}
}

// Entries returns all diff entries sorted by key.
func (dt *DiffTracker) Entries() []*DiffEntry {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	result := make([]*DiffEntry, 0, len(dt.entries))
	for _, e := range dt.entries {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Key, result[j].Key) < 0
	})
	return result
}

// Inserts returns only the insertion entries, sorted by key.
func (dt *DiffTracker) Inserts() []*DiffEntry {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	var result []*DiffEntry
	for _, e := range dt.entries {
		if e.IsInsert() {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Key, result[j].Key) < 0
	})
	return result
}

// Deletes returns only the deletion entries, sorted by key.
func (dt *DiffTracker) Deletes() []*DiffEntry {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	var result []*DiffEntry
	for _, e := range dt.entries {
		if e.IsDelete() {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Key, result[j].Key) < 0
	})
	return result
}

// Updates returns only the update entries, sorted by key.
func (dt *DiffTracker) Updates() []*DiffEntry {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	var result []*DiffEntry
	for _, e := range dt.entries {
		if e.IsUpdate() {
			result = append(result, e)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].Key, result[j].Key) < 0
	})
	return result
}

// Len returns the total number of tracked changes.
func (dt *DiffTracker) Len() int {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return len(dt.entries)
}

// Has returns true if the tracker has a diff entry for the given key.
func (dt *DiffTracker) Has(key []byte) bool {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	_, ok := dt.entries[string(key)]
	return ok
}

// Get returns the diff entry for the given key, or nil if not tracked.
func (dt *DiffTracker) Get(key []byte) *DiffEntry {
	dt.mu.RLock()
	defer dt.mu.RUnlock()
	return dt.entries[string(key)]
}

// Reset clears all tracked diffs.
func (dt *DiffTracker) Reset() {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.entries = make(map[string]*DiffEntry)
}

// DiffSummary provides aggregate statistics about tracked changes.
type DiffSummary struct {
	Inserts    int
	Deletes    int
	Updates    int
	TotalBytes int64 // sum of all new value sizes
}

// Summary returns aggregate statistics about the tracked diffs.
func (dt *DiffTracker) Summary() DiffSummary {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	var s DiffSummary
	for _, e := range dt.entries {
		if e.IsInsert() {
			s.Inserts++
		} else if e.IsDelete() {
			s.Deletes++
		} else if e.IsUpdate() {
			s.Updates++
		}
		if e.NewValue != nil {
			s.TotalBytes += int64(len(e.NewValue))
		}
	}
	return s
}

// ComputeTrieDiff compares two in-memory tries and returns a DiffTracker
// containing all key-value differences. Keys present in b but not a are
// inserts; keys in a but not b are deletes; keys in both with different
// values are updates.
func ComputeTrieDiff(a, b *Trie) *DiffTracker {
	dt := NewDiffTracker()

	// Collect all entries from both tries.
	aEntries := collectTrieEntries(a)
	bEntries := collectTrieEntries(b)

	// Compare: find inserts and updates.
	for k, bVal := range bEntries {
		if aVal, exists := aEntries[k]; exists {
			if !bytes.Equal(aVal, bVal) {
				dt.RecordUpdate([]byte(k), aVal, bVal)
			}
		} else {
			dt.RecordInsert([]byte(k), bVal)
		}
	}

	// Find deletes: keys in a but not in b.
	for k, aVal := range aEntries {
		if _, exists := bEntries[k]; !exists {
			dt.RecordDelete([]byte(k), aVal)
		}
	}

	return dt
}

// ComputeTrieDiffFromDB compares two committed tries (referenced by root
// hash) stored in a NodeDatabase. Returns a DiffTracker with all differences.
func ComputeTrieDiffFromDB(rootA, rootB types.Hash, db *NodeDatabase) (*DiffTracker, error) {
	dt := NewDiffTracker()

	// Build node maps from the database.
	nodesA := extractNodesForRoot(rootA, db)
	nodesB := extractNodesForRoot(rootB, db)

	iterA := NewTrieIterator(rootA, nodesA)
	iterB := NewTrieIterator(rootB, nodesB)

	aEntries := make(map[string][]byte)
	for iterA.Next() {
		key := make([]byte, len(iterA.Key()))
		copy(key, iterA.Key())
		val := make([]byte, len(iterA.Value()))
		copy(val, iterA.Value())
		aEntries[string(key)] = val
	}
	if err := iterA.Error(); err != nil {
		return nil, err
	}

	bEntries := make(map[string][]byte)
	for iterB.Next() {
		key := make([]byte, len(iterB.Key()))
		copy(key, iterB.Key())
		val := make([]byte, len(iterB.Value()))
		copy(val, iterB.Value())
		bEntries[string(key)] = val
	}
	if err := iterB.Error(); err != nil {
		return nil, err
	}

	// Compute diff.
	for k, bVal := range bEntries {
		if aVal, exists := aEntries[k]; exists {
			if !bytes.Equal(aVal, bVal) {
				dt.RecordUpdate([]byte(k), aVal, bVal)
			}
		} else {
			dt.RecordInsert([]byte(k), bVal)
		}
	}
	for k, aVal := range aEntries {
		if _, exists := bEntries[k]; !exists {
			dt.RecordDelete([]byte(k), aVal)
		}
	}

	return dt, nil
}

// collectTrieEntries iterates an in-memory trie and returns a map of key->value.
func collectTrieEntries(t *Trie) map[string][]byte {
	entries := make(map[string][]byte)
	if t == nil || t.root == nil {
		return entries
	}
	it := NewIterator(t)
	for it.Next() {
		k := make([]byte, len(it.Key))
		copy(k, it.Key)
		v := make([]byte, len(it.Value))
		copy(v, it.Value)
		entries[string(k)] = v
	}
	return entries
}

// extractNodesForRoot builds a node map from the NodeDatabase's dirty cache.
func extractNodesForRoot(root types.Hash, db *NodeDatabase) map[types.Hash][]byte {
	nodeMap := make(map[types.Hash][]byte)
	if root == (types.Hash{}) || root == emptyRoot {
		return nodeMap
	}

	db.mu.RLock()
	defer db.mu.RUnlock()
	for h, d := range db.dirty {
		cp := make([]byte, len(d))
		copy(cp, d)
		nodeMap[h] = cp
	}
	return nodeMap
}

// cloneBytes returns a copy of a byte slice, handling nil.
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
