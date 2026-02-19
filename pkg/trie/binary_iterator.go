package trie

// BinaryIterator iterates over all key-value pairs in a binary Merkle trie
// in depth-first order.
//
// Usage:
//
//	it := NewBinaryIterator(trie)
//	for it.Next() {
//	    key := it.Key()
//	    value := it.Value()
//	}
type BinaryIterator struct {
	stack []*binaryIterFrame
	key   []byte
	value []byte
}

type binaryIterFrame struct {
	node  *binaryNode
	state int // 0=not visited, 1=visited left, 2=visited right
}

// NewBinaryIterator creates a new iterator for the given binary trie.
// The iterator starts before the first element; call Next() to advance.
func NewBinaryIterator(t *BinaryTrie) *BinaryIterator {
	it := &BinaryIterator{}
	if t.root != nil {
		it.stack = []*binaryIterFrame{{node: t.root}}
	}
	return it
}

// Next advances the iterator to the next key-value pair. Returns true if
// a new pair is available, false when iteration is complete.
func (it *BinaryIterator) Next() bool {
	for len(it.stack) > 0 {
		top := it.stack[len(it.stack)-1]

		if top.node.isLeaf {
			it.key = top.node.key[:]
			it.value = top.node.value
			it.stack = it.stack[:len(it.stack)-1]
			return true
		}

		switch top.state {
		case 0:
			top.state = 1
			if top.node.left != nil {
				it.stack = append(it.stack, &binaryIterFrame{node: top.node.left})
			}
		case 1:
			top.state = 2
			if top.node.right != nil {
				it.stack = append(it.stack, &binaryIterFrame{node: top.node.right})
			}
		case 2:
			it.stack = it.stack[:len(it.stack)-1]
		}
	}
	return false
}

// Key returns the current 32-byte hashed key. Valid after Next() returns true.
func (it *BinaryIterator) Key() []byte {
	return it.key
}

// Value returns the current value. Valid after Next() returns true.
func (it *BinaryIterator) Value() []byte {
	return it.value
}
