// binary_iterator_extended.go adds seek-to-key, prefix iteration, proof
// generation during iteration, deletion during iteration, and count.
package trie

import (
	"bytes"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// SeekBinaryIterator is a BinaryIterator with seek, prefix, delete, and
// proof generation capabilities.
type SeekBinaryIterator struct {
	trie  *BinaryTrie
	stack []*seekIterFrame
	key   []byte
	value []byte
	done  bool
}

type seekIterFrame struct {
	node  *binaryNode
	state int    // 0=not visited, 1=visited left, 2=visited right
	bits  []byte // accumulated path bits from root to this node
}

// NewSeekBinaryIterator creates a new seekable iterator for the binary trie.
func NewSeekBinaryIterator(t *BinaryTrie) *SeekBinaryIterator {
	it := &SeekBinaryIterator{trie: t}
	if t.root != nil {
		it.stack = []*seekIterFrame{{node: t.root, bits: nil}}
	}
	return it
}

// Next advances the iterator to the next key-value pair in sorted (left-first)
// order. Returns true if a new pair is available.
func (it *SeekBinaryIterator) Next() bool {
	if it.done {
		return false
	}
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
				childBits := append(cloneBits(top.bits), 0)
				it.stack = append(it.stack, &seekIterFrame{
					node: top.node.left,
					bits: childBits,
				})
			}
		case 1:
			top.state = 2
			if top.node.right != nil {
				childBits := append(cloneBits(top.bits), 1)
				it.stack = append(it.stack, &seekIterFrame{
					node: top.node.right,
					bits: childBits,
				})
			}
		case 2:
			it.stack = it.stack[:len(it.stack)-1]
		}
	}
	it.done = true
	return false
}

// Key returns the current key.
func (it *SeekBinaryIterator) Key() []byte { return it.key }

// Value returns the current value.
func (it *SeekBinaryIterator) Value() []byte { return it.value }

// Seek positions the iterator at the first key >= the given target key.
// Subsequent calls to Next() will return keys starting from this position.
func (it *SeekBinaryIterator) Seek(target types.Hash) bool {
	it.stack = nil
	it.done = false

	if it.trie.root == nil {
		return false
	}

	// Rebuild the stack by traversing toward the target.
	it.stack = []*seekIterFrame{{node: it.trie.root, bits: nil}}

	for it.Next() {
		var currentKey types.Hash
		copy(currentKey[:], it.key)
		if bytes.Compare(currentKey[:], target[:]) >= 0 {
			return true
		}
	}
	return false
}

// PrefixIterator returns all key-value pairs whose hashed key starts with
// the given prefix bits. prefix is a slice of 0s and 1s representing bit
// positions from the MSB.
func PrefixIterator(t *BinaryTrie, prefix []byte) *PrefixBinaryIterator {
	return &PrefixBinaryIterator{
		trie:   t,
		prefix: prefix,
	}
}

// PrefixBinaryIterator iterates over keys matching a bit prefix.
type PrefixBinaryIterator struct {
	trie   *BinaryTrie
	prefix []byte
	inner  *BinaryIterator
	key    []byte
	value  []byte
	inited bool
}

// Next advances to the next key-value pair matching the prefix.
func (it *PrefixBinaryIterator) Next() bool {
	if !it.inited {
		it.inner = NewBinaryIterator(it.trie)
		it.inited = true
	}
	for it.inner.Next() {
		k := it.inner.Key()
		if matchesPrefix(k, it.prefix) {
			it.key = k
			it.value = it.inner.Value()
			return true
		}
	}
	return false
}

// Key returns the current key.
func (it *PrefixBinaryIterator) Key() []byte { return it.key }

// Value returns the current value.
func (it *PrefixBinaryIterator) Value() []byte { return it.value }

// matchesPrefix checks if the key's leading bits match the given prefix bits.
func matchesPrefix(key []byte, prefix []byte) bool {
	if len(key) < 32 {
		return false
	}
	for i, bit := range prefix {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		if byteIdx >= len(key) {
			return false
		}
		actual := (key[byteIdx] >> uint(bitIdx)) & 1
		if actual != bit {
			return false
		}
	}
	return true
}

// ProofCollectingIterator generates Merkle proofs for each key during
// iteration.
type ProofCollectingIterator struct {
	trie   *BinaryTrie
	inner  *BinaryIterator
	key    []byte
	value  []byte
	proofs []IterBinaryProof
}

// IterBinaryProof is a Merkle proof for a key in the binary trie,
// generated during iteration.
type IterBinaryProof struct {
	Key      types.Hash
	Value    []byte
	Siblings []types.Hash
}

// NewProofCollectingIterator creates an iterator that also generates proofs.
func NewProofCollectingIterator(t *BinaryTrie) *ProofCollectingIterator {
	return &ProofCollectingIterator{
		trie:  t,
		inner: NewBinaryIterator(t),
	}
}

// Next advances and generates a proof for the current key.
func (it *ProofCollectingIterator) Next() bool {
	if !it.inner.Next() {
		return false
	}
	it.key = it.inner.Key()
	it.value = it.inner.Value()

	var hk types.Hash
	copy(hk[:], it.key)
	siblings := collectBinarySiblings(it.trie.root, hk, 0)
	proof := IterBinaryProof{
		Key:      hk,
		Value:    it.value,
		Siblings: siblings,
	}
	it.proofs = append(it.proofs, proof)
	return true
}

// Key returns the current key.
func (it *ProofCollectingIterator) Key() []byte { return it.key }

// Value returns the current value.
func (it *ProofCollectingIterator) Value() []byte { return it.value }

// Proofs returns all collected proofs so far.
func (it *ProofCollectingIterator) Proofs() []IterBinaryProof { return it.proofs }

// collectBinarySiblings walks the tree collecting sibling hashes for a proof.
func collectBinarySiblings(n *binaryNode, key types.Hash, depth int) []types.Hash {
	if n == nil {
		return nil
	}
	if n.isLeaf {
		return nil
	}

	bit := getBit(key, depth)
	var siblingHash types.Hash
	if bit == 0 {
		siblingHash = hashBinaryNode(n.right)
		deeper := collectBinarySiblings(n.left, key, depth+1)
		return append([]types.Hash{siblingHash}, deeper...)
	}
	siblingHash = hashBinaryNode(n.left)
	deeper := collectBinarySiblings(n.right, key, depth+1)
	return append([]types.Hash{siblingHash}, deeper...)
}

// VerifyIterBinaryProof verifies a binary trie Merkle proof (from iteration) against a root hash.
func VerifyIterBinaryProof(root types.Hash, proof IterBinaryProof) bool {
	// Reconstruct the leaf hash.
	buf := make([]byte, 1+32+len(proof.Value))
	buf[0] = 0x00
	copy(buf[1:33], proof.Key[:])
	copy(buf[33:], proof.Value)
	current := crypto.Keccak256Hash(buf)

	// Walk from leaf to root, combining with siblings.
	for i := len(proof.Siblings) - 1; i >= 0; i-- {
		depth := i
		bit := getBit(proof.Key, depth)

		combineBuf := make([]byte, 1+32+32)
		combineBuf[0] = 0x01
		if bit == 0 {
			copy(combineBuf[1:33], current[:])
			copy(combineBuf[33:65], proof.Siblings[i][:])
		} else {
			copy(combineBuf[1:33], proof.Siblings[i][:])
			copy(combineBuf[33:65], current[:])
		}
		current = crypto.Keccak256Hash(combineBuf)
	}

	return current == root
}

// DeletingIterator allows deletion of keys during iteration.
type DeletingIterator struct {
	trie    *BinaryTrie
	inner   *BinaryIterator
	key     []byte
	value   []byte
	deleted int
}

// NewDeletingIterator creates an iterator that supports Delete().
func NewDeletingIterator(t *BinaryTrie) *DeletingIterator {
	return &DeletingIterator{
		trie:  t,
		inner: NewBinaryIterator(t),
	}
}

// Next advances to the next key.
func (it *DeletingIterator) Next() bool {
	if !it.inner.Next() {
		return false
	}
	it.key = it.inner.Key()
	it.value = it.inner.Value()
	return true
}

// Key returns the current key.
func (it *DeletingIterator) Key() []byte { return it.key }

// Value returns the current value.
func (it *DeletingIterator) Value() []byte { return it.value }

// Delete removes the current key from the trie. The iterator continues
// from the next key on subsequent Next() calls.
func (it *DeletingIterator) Delete() {
	if it.key == nil {
		return
	}
	var hk types.Hash
	copy(hk[:], it.key)
	it.trie.DeleteHashed(hk)
	it.deleted++
}

// DeletedCount returns the number of keys deleted during iteration.
func (it *DeletingIterator) DeletedCount() int { return it.deleted }

// CountBinaryIterator counts the number of key-value pairs without
// allocating storage for all of them.
func CountBinaryIterator(t *BinaryTrie) int {
	it := NewBinaryIterator(t)
	count := 0
	for it.Next() {
		count++
	}
	return count
}

func cloneBits(bits []byte) []byte {
	if bits == nil {
		return nil
	}
	cp := make([]byte, len(bits))
	copy(cp, bits)
	return cp
}
