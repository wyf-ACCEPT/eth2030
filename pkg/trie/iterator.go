package trie

// Iterator iterates over all key-value pairs in a Merkle Patricia Trie
// in lexicographic order of the keys. This is useful for snap sync,
// state dumping, and trie comparison.
//
// Usage:
//
//	it := NewIterator(trie)
//	for it.Next() {
//	    key := it.Key   // hex-encoded nibble key (without terminator)
//	    value := it.Value
//	    path := it.Path // raw byte key (original key before hex encoding)
//	}
//	if err := it.Err(); err != nil {
//	    // handle error
//	}
type Iterator struct {
	trie  *Trie
	Key   []byte // current key in raw bytes (not hex nibbles)
	Value []byte // current value

	// stack tracks our position in the trie for depth-first traversal.
	stack []iterFrame
	err   error
	started bool
}

// iterFrame represents one level in the trie traversal stack.
type iterFrame struct {
	node  node
	path  []byte // hex nibble path accumulated so far
	index int    // for fullNode: next child index to visit (0-16); for shortNode: 0=not visited, 1=visited
}

// NewIterator creates a new iterator for the given trie. The iterator
// starts before the first element; call Next() to advance.
func NewIterator(t *Trie) *Iterator {
	it := &Iterator{trie: t}
	if t.root != nil {
		it.stack = []iterFrame{{node: t.root, path: nil, index: 0}}
	}
	return it
}

// Next advances the iterator to the next key-value pair.
// Returns true if a new pair is available, false when iteration is complete.
func (it *Iterator) Next() bool {
	for len(it.stack) > 0 {
		top := &it.stack[len(it.stack)-1]

		switch n := top.node.(type) {
		case *shortNode:
			if top.index > 0 {
				// Already visited this short node; pop it.
				it.stack = it.stack[:len(it.stack)-1]
				continue
			}
			top.index = 1
			childPath := concat(top.path, n.Key)

			if v, ok := n.Val.(valueNode); ok {
				// Leaf node: emit the key-value pair.
				// The key has a terminator nibble; strip it for the raw key.
				if hasTerm(childPath) {
					it.Key = hexToKeybytes(childPath[:len(childPath)-1])
				} else {
					it.Key = hexToKeybytes(childPath)
				}
				it.Value = make([]byte, len(v))
				copy(it.Value, v)
				return true
			}
			// Extension node: push the child.
			it.stack = append(it.stack, iterFrame{
				node:  n.Val,
				path:  childPath,
				index: 0,
			})

		case *fullNode:
			// Visit order: value at branch (Children[16]) first (shorter key
			// precedes any extension), then children 0-15.
			// index 0 = value slot (Children[16]), index 1-16 = children 0-15.
			found := false
			for top.index <= 16 {
				idx := top.index
				top.index++

				if idx == 0 {
					// Check for value at this branch point.
					if v, ok := n.Children[16].(valueNode); ok {
						if len(top.path)%2 == 0 {
							it.Key = hexToKeybytes(top.path)
						} else {
							// Odd-length path shouldn't happen for valid keys.
							continue
						}
						it.Value = make([]byte, len(v))
						copy(it.Value, v)
						return true
					}
					continue
				}

				childIdx := idx - 1 // map index 1-16 to children 0-15
				child := n.Children[childIdx]
				if child == nil {
					continue
				}

				childPath := concat(top.path, []byte{byte(childIdx)})
				it.stack = append(it.stack, iterFrame{
					node:  child,
					path:  childPath,
					index: 0,
				})
				found = true
				break
			}
			if !found {
				// All children visited; pop this node.
				it.stack = it.stack[:len(it.stack)-1]
			}

		case valueNode:
			// Direct value node (shouldn't normally appear as stack root).
			it.stack = it.stack[:len(it.stack)-1]
			if hasTerm(top.path) {
				it.Key = hexToKeybytes(top.path[:len(top.path)-1])
			} else if len(top.path)%2 == 0 {
				it.Key = hexToKeybytes(top.path)
			} else {
				it.stack = it.stack[:0]
				continue
			}
			it.Value = make([]byte, len(n))
			copy(it.Value, n)
			return true

		case hashNode:
			// Cannot iterate over unresolved hash nodes in a plain trie.
			// For ResolvableTrie, use ResolvableIterator instead.
			it.err = ErrNotFound
			it.stack = it.stack[:0]
			return false

		default:
			it.stack = it.stack[:len(it.stack)-1]
		}
	}
	return false
}

// Err returns any error encountered during iteration.
func (it *Iterator) Err() error {
	return it.err
}

// NodeCount returns the number of entries remaining on the traversal stack.
// This can be used to gauge iteration progress.
func (it *Iterator) NodeCount() int {
	return len(it.stack)
}

// ResolvableIterator iterates over all key-value pairs in a trie backed by
// a node database, resolving hash references on the fly.
type ResolvableIterator struct {
	trie  *ResolvableTrie
	Key   []byte
	Value []byte
	stack []iterFrame
	err   error
}

// NewResolvableIterator creates an iterator for a database-backed trie.
func NewResolvableIterator(t *ResolvableTrie) *ResolvableIterator {
	it := &ResolvableIterator{trie: t}
	if t.root != nil {
		it.stack = []iterFrame{{node: t.root, path: nil, index: 0}}
	}
	return it
}

// Next advances the resolvable iterator, resolving hash nodes from the database.
func (it *ResolvableIterator) Next() bool {
	for len(it.stack) > 0 {
		top := &it.stack[len(it.stack)-1]

		switch n := top.node.(type) {
		case *shortNode:
			if top.index > 0 {
				it.stack = it.stack[:len(it.stack)-1]
				continue
			}
			top.index = 1
			childPath := concat(top.path, n.Key)

			if v, ok := n.Val.(valueNode); ok {
				if hasTerm(childPath) {
					it.Key = hexToKeybytes(childPath[:len(childPath)-1])
				} else {
					it.Key = hexToKeybytes(childPath)
				}
				it.Value = make([]byte, len(v))
				copy(it.Value, v)
				return true
			}
			it.stack = append(it.stack, iterFrame{
				node:  n.Val,
				path:  childPath,
				index: 0,
			})

		case *fullNode:
			found := false
			for top.index <= 16 {
				idx := top.index
				top.index++

				if idx == 0 {
					if v, ok := n.Children[16].(valueNode); ok {
						if len(top.path)%2 == 0 {
							it.Key = hexToKeybytes(top.path)
						} else {
							continue
						}
						it.Value = make([]byte, len(v))
						copy(it.Value, v)
						return true
					}
					continue
				}

				childIdx := idx - 1
				child := n.Children[childIdx]
				if child == nil {
					continue
				}

				childPath := concat(top.path, []byte{byte(childIdx)})
				it.stack = append(it.stack, iterFrame{
					node:  child,
					path:  childPath,
					index: 0,
				})
				found = true
				break
			}
			if !found {
				it.stack = it.stack[:len(it.stack)-1]
			}

		case valueNode:
			it.stack = it.stack[:len(it.stack)-1]
			if hasTerm(top.path) {
				it.Key = hexToKeybytes(top.path[:len(top.path)-1])
			} else if len(top.path)%2 == 0 {
				it.Key = hexToKeybytes(top.path)
			} else {
				it.stack = it.stack[:0]
				continue
			}
			it.Value = make([]byte, len(n))
			copy(it.Value, n)
			return true

		case hashNode:
			// Resolve the hash node from the database.
			resolved, err := it.trie.resolveHash(n)
			if err != nil {
				it.err = err
				it.stack = it.stack[:0]
				return false
			}
			top.node = resolved

		default:
			it.stack = it.stack[:len(it.stack)-1]
		}
	}
	return false
}

// Err returns any error encountered during iteration.
func (it *ResolvableIterator) Err() error {
	return it.err
}
