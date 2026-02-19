package trie

import (
	"bytes"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// NodeIterator is an interface for iterating over trie nodes.
type NodeIterator interface {
	// Next advances to the next node. Returns false when done.
	Next() bool
	// Key returns the current key.
	Key() []byte
	// Value returns the current value.
	Value() []byte
	// Hash returns the hash of the current node.
	Hash() types.Hash
	// Leaf returns true if the current node is a leaf.
	Leaf() bool
	// Path returns the current traversal path in the trie.
	Path() []byte
	// Error returns any error encountered during iteration.
	Error() error
}

// TrieIterator implements NodeIterator for in-memory tries backed
// by a flat map of hash -> RLP-encoded node data. It walks the trie
// from the root, decoding nodes on the fly, and yields leaf entries
// in sorted key order.
type TrieIterator struct {
	nodes map[types.Hash][]byte
	root  types.Hash

	entries []leafEntry
	pos     int // -1 before first Next()
	err     error
}

// leafEntry holds a single leaf found during trie traversal.
type leafEntry struct {
	key   []byte
	value []byte
	hash  types.Hash
	path  []byte
}

// NewTrieIterator creates a TrieIterator that walks leaves from the given
// root and node map. The map stores RLP-encoded trie nodes keyed by hash.
func NewTrieIterator(root types.Hash, nodes map[types.Hash][]byte) *TrieIterator {
	it := &TrieIterator{
		nodes: nodes,
		root:  root,
		pos:   -1,
	}
	it.collectAll()
	return it
}

// collectAll decodes the trie from the root and collects all leaves.
func (it *TrieIterator) collectAll() {
	if it.root == (types.Hash{}) || it.nodes == nil {
		return
	}
	data, ok := it.nodes[it.root]
	if !ok {
		return
	}
	it.walkRLP(data, nil)
	sortEntries(it.entries)
}

// walkRLP decodes an RLP-encoded trie node and recursively walks children.
func (it *TrieIterator) walkRLP(data []byte, path []byte) {
	if len(data) == 0 {
		return
	}
	nodeHash := crypto.Keccak256Hash(data)

	// Use the existing decoder from decoder.go.
	items, err := decodeRLPList(data)
	if err != nil {
		return
	}

	switch len(items) {
	case 17:
		it.walkBranchRLP(items, path, nodeHash)
	case 2:
		it.walkShortRLP(items, path, nodeHash)
	}
}

// walkBranchRLP handles a 17-element branch node.
func (it *TrieIterator) walkBranchRLP(items [][]byte, path []byte, nodeHash types.Hash) {
	// Value at index 16.
	if len(items[16]) > 0 && len(path)%2 == 0 {
		key := hexToKeybytes(path)
		it.entries = append(it.entries, leafEntry{
			key:   key,
			value: cloneSlice(items[16]),
			hash:  nodeHash,
			path:  cloneSlice(path),
		})
	}

	// Children 0-15.
	for i := 0; i < 16; i++ {
		child := items[i]
		if len(child) == 0 {
			continue
		}
		childPath := append(cloneSlice(path), byte(i))
		it.resolveAndWalk(child, childPath)
	}
}

// walkShortRLP handles a 2-element extension or leaf node.
func (it *TrieIterator) walkShortRLP(items [][]byte, path []byte, nodeHash types.Hash) {
	if len(items[0]) == 0 {
		return
	}

	nibbles := compactToHex(items[0])
	isLeaf := hasTerm(nibbles)

	if isLeaf {
		nibblePath := nibbles[:len(nibbles)-1]
		fullPath := append(cloneSlice(path), nibblePath...)
		if len(fullPath)%2 == 0 {
			key := hexToKeybytes(fullPath)
			it.entries = append(it.entries, leafEntry{
				key:   key,
				value: cloneSlice(items[1]),
				hash:  nodeHash,
				path:  cloneSlice(fullPath),
			})
		}
	} else {
		// Extension: follow child.
		extPath := append(cloneSlice(path), nibbles...)
		it.resolveAndWalk(items[1], extPath)
	}
}

// resolveAndWalk follows a child reference: 32-byte hash or inline node.
func (it *TrieIterator) resolveAndWalk(child []byte, path []byte) {
	if len(child) == 32 {
		var h types.Hash
		copy(h[:], child)
		if data, ok := it.nodes[h]; ok {
			it.walkRLP(data, path)
		}
	} else {
		it.walkRLP(child, path)
	}
}

// Next advances to the next leaf node.
func (it *TrieIterator) Next() bool {
	it.pos++
	return it.pos < len(it.entries)
}

// Key returns the current key.
func (it *TrieIterator) Key() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].key
}

// Value returns the current value.
func (it *TrieIterator) Value() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].value
}

// Hash returns the hash of the current node.
func (it *TrieIterator) Hash() types.Hash {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return types.Hash{}
	}
	return it.entries[it.pos].hash
}

// Leaf returns true (all items from this iterator are leaves).
func (it *TrieIterator) Leaf() bool {
	return it.pos >= 0 && it.pos < len(it.entries)
}

// Path returns the current traversal path (hex nibbles).
func (it *TrieIterator) Path() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].path
}

// Error returns any error encountered during iteration.
func (it *TrieIterator) Error() error {
	return it.err
}

// DiffIterator yields nodes in b but not in a (or with changed values).
// Both iterators must yield keys in sorted order.
type DiffIterator struct {
	a, b   NodeIterator
	aKey   []byte
	bKey   []byte
	aValid bool
	bValid bool
	err    error
}

// NewDiffIterator creates a DiffIterator.
func NewDiffIterator(a, b NodeIterator) *DiffIterator {
	d := &DiffIterator{a: a, b: b}
	d.aValid = a.Next()
	d.bValid = b.Next()
	if d.aValid {
		d.aKey = a.Key()
	}
	if d.bValid {
		d.bKey = b.Key()
	}
	return d
}

// Next advances to the next diff entry.
func (d *DiffIterator) Next() bool {
	for d.bValid {
		if !d.aValid {
			return true
		}
		cmp := bytes.Compare(d.bKey, d.aKey)
		if cmp < 0 {
			return true
		}
		if cmp == 0 {
			if !bytes.Equal(d.b.Value(), d.a.Value()) {
				return true
			}
			d.aValid = d.a.Next()
			if d.aValid {
				d.aKey = d.a.Key()
			}
			d.bValid = d.b.Next()
			if d.bValid {
				d.bKey = d.b.Key()
			}
			continue
		}
		d.aValid = d.a.Next()
		if d.aValid {
			d.aKey = d.a.Key()
		}
	}
	return false
}

// Key returns the current key from b.
func (d *DiffIterator) Key() []byte {
	if !d.bValid {
		return nil
	}
	return d.b.Key()
}

// Value returns the current value from b.
func (d *DiffIterator) Value() []byte {
	if !d.bValid {
		return nil
	}
	return d.b.Value()
}

// Hash returns the hash of the current node from b.
func (d *DiffIterator) Hash() types.Hash {
	if !d.bValid {
		return types.Hash{}
	}
	return d.b.Hash()
}

// Leaf returns true if the current node in b is a leaf.
func (d *DiffIterator) Leaf() bool {
	if !d.bValid {
		return false
	}
	return d.b.Leaf()
}

// Path returns the current path from b.
func (d *DiffIterator) Path() []byte {
	if !d.bValid {
		return nil
	}
	return d.b.Path()
}

// Error returns any error from either underlying iterator.
func (d *DiffIterator) Error() error {
	if d.err != nil {
		return d.err
	}
	if err := d.a.Error(); err != nil {
		return err
	}
	return d.b.Error()
}

// Advance moves the b pointer forward after processing a diff entry.
func (d *DiffIterator) Advance() {
	d.bValid = d.b.Next()
	if d.bValid {
		d.bKey = d.b.Key()
	}
}

// IteratorStats tracks statistics about trie iteration.
type IteratorStats struct {
	NodesVisited int
	LeavesFound  int
	BytesRead    int
	Duration     time.Duration
}

// CollectLeaves walks all entries from a NodeIterator and returns
// the collected keys and values.
func CollectLeaves(iter NodeIterator) ([][]byte, [][]byte, error) {
	var keys, values [][]byte
	for iter.Next() {
		k := iter.Key()
		v := iter.Value()
		kc := make([]byte, len(k))
		copy(kc, k)
		vc := make([]byte, len(v))
		copy(vc, v)
		keys = append(keys, kc)
		values = append(values, vc)
	}
	return keys, values, iter.Error()
}

// --- helpers (with names that do not conflict with existing package funcs) ---

// cloneSlice returns a copy of a byte slice, handling nil.
func cloneSlice(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// sortEntries sorts leaf entries by key using insertion sort.
func sortEntries(entries []leafEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && bytes.Compare(entries[j].key, entries[j-1].key) < 0; j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}
