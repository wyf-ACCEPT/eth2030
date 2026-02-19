package trie

import (
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Binary Merkle tree for state representation. Planned for the I*/J* phase
// as a ZK-proof-friendly alternative to MPT. Keys are 32-byte keccak256
// hashes; path traversal walks bits MSB-first (bit 0 = left, bit 1 = right).

// zeroHash is the hash of an empty subtree.
var zeroHash = types.Hash{}

// binaryNode is either a leaf or a branch in the binary trie.
type binaryNode struct {
	// Branch fields (used when isLeaf == false).
	left  *binaryNode
	right *binaryNode

	// Leaf fields (used when isLeaf == true).
	isLeaf bool
	key    types.Hash // full 32-byte hashed key for disambiguation
	value  []byte

	// Cached hash; zero means not yet computed.
	hash  types.Hash
	dirty bool
}

// BinaryTrie is a binary Merkle tree keyed by 32-byte hashed keys.
type BinaryTrie struct {
	root *binaryNode
}

// NewBinaryTrie creates a new, empty binary Merkle trie.
func NewBinaryTrie() *BinaryTrie {
	return &BinaryTrie{}
}

// Get retrieves the value associated with key. The key is hashed with
// keccak256 to produce the 32-byte trie key. Returns ErrNotFound if absent.
func (t *BinaryTrie) Get(key []byte) ([]byte, error) {
	hk := crypto.Keccak256Hash(key)
	return t.GetHashed(hk)
}

// GetHashed retrieves a value by its pre-hashed 32-byte key.
func (t *BinaryTrie) GetHashed(hk types.Hash) ([]byte, error) {
	n := t.root
	for depth := 0; n != nil; depth++ {
		if n.isLeaf {
			if n.key == hk {
				return n.value, nil
			}
			return nil, ErrNotFound
		}
		if getBit(hk, depth) == 0 {
			n = n.left
		} else {
			n = n.right
		}
	}
	return nil, ErrNotFound
}

// Put inserts or updates a key-value pair. The key is hashed with keccak256.
// If value is nil or empty, the key is deleted.
func (t *BinaryTrie) Put(key, value []byte) error {
	if len(value) == 0 {
		return t.Delete(key)
	}
	hk := crypto.Keccak256Hash(key)
	return t.PutHashed(hk, value)
}

// PutHashed inserts a value by its pre-hashed 32-byte key.
func (t *BinaryTrie) PutHashed(hk types.Hash, value []byte) error {
	if len(value) == 0 {
		return t.DeleteHashed(hk)
	}
	t.root = insertBinary(t.root, hk, value, 0)
	return nil
}

func insertBinary(n *binaryNode, key types.Hash, value []byte, depth int) *binaryNode {
	if n == nil {
		return &binaryNode{
			isLeaf: true,
			key:    key,
			value:  copyBytes(value),
			dirty:  true,
		}
	}

	if n.isLeaf {
		if n.key == key {
			// Update existing leaf.
			n.value = copyBytes(value)
			n.dirty = true
			return n
		}
		// Split: create a branch to separate the two leaves.
		return splitLeaf(n, key, value, depth)
	}

	// Branch node: descend.
	n.dirty = true
	if getBit(key, depth) == 0 {
		n.left = insertBinary(n.left, key, value, depth+1)
	} else {
		n.right = insertBinary(n.right, key, value, depth+1)
	}
	return n
}

// splitLeaf creates branch nodes until existing and new keys diverge,
// then places each as a leaf.
func splitLeaf(existing *binaryNode, newKey types.Hash, newValue []byte, depth int) *binaryNode {
	existBit := getBit(existing.key, depth)
	newBit := getBit(newKey, depth)

	if existBit == newBit {
		// Same direction: create a branch with a single child and recurse.
		child := splitLeaf(existing, newKey, newValue, depth+1)
		branch := &binaryNode{dirty: true}
		if existBit == 0 {
			branch.left = child
		} else {
			branch.right = child
		}
		return branch
	}

	// Different direction: create a branch with both leaves.
	newLeaf := &binaryNode{
		isLeaf: true,
		key:    newKey,
		value:  copyBytes(newValue),
		dirty:  true,
	}
	existing.dirty = true
	branch := &binaryNode{dirty: true}
	if existBit == 0 {
		branch.left = existing
		branch.right = newLeaf
	} else {
		branch.left = newLeaf
		branch.right = existing
	}
	return branch
}

// Delete removes a key from the trie. No-op if key doesn't exist.
func (t *BinaryTrie) Delete(key []byte) error {
	hk := crypto.Keccak256Hash(key)
	return t.DeleteHashed(hk)
}

// DeleteHashed removes a value by its pre-hashed 32-byte key.
func (t *BinaryTrie) DeleteHashed(hk types.Hash) error {
	t.root = deleteBinary(t.root, hk, 0)
	return nil
}

func deleteBinary(n *binaryNode, key types.Hash, depth int) *binaryNode {
	if n == nil {
		return nil
	}

	if n.isLeaf {
		if n.key == key {
			return nil
		}
		return n
	}

	// Branch node.
	if getBit(key, depth) == 0 {
		n.left = deleteBinary(n.left, key, depth+1)
	} else {
		n.right = deleteBinary(n.right, key, depth+1)
	}
	n.dirty = true

	// Collapse: if only one child remains and it's a leaf, promote it.
	if n.left == nil && n.right == nil {
		return nil
	}
	if n.left == nil && n.right != nil && n.right.isLeaf {
		return n.right
	}
	if n.right == nil && n.left != nil && n.left.isLeaf {
		return n.left
	}
	return n
}

// Hash computes the keccak256 Merkle root of the trie.
// An empty trie returns the zero hash.
func (t *BinaryTrie) Hash() types.Hash {
	if t.root == nil {
		return zeroHash
	}
	return hashBinaryNode(t.root)
}

func hashBinaryNode(n *binaryNode) types.Hash {
	if n == nil {
		return zeroHash
	}
	if !n.dirty && n.hash != zeroHash {
		return n.hash
	}

	var h types.Hash
	if n.isLeaf {
		// leaf hash = keccak256(0x00 || key || value)
		buf := make([]byte, 1+32+len(n.value))
		buf[0] = 0x00 // leaf prefix
		copy(buf[1:33], n.key[:])
		copy(buf[33:], n.value)
		h = crypto.Keccak256Hash(buf)
	} else {
		// branch hash = keccak256(0x01 || left_hash || right_hash)
		leftHash := hashBinaryNode(n.left)
		rightHash := hashBinaryNode(n.right)
		buf := make([]byte, 1+32+32)
		buf[0] = 0x01 // branch prefix
		copy(buf[1:33], leftHash[:])
		copy(buf[33:65], rightHash[:])
		h = crypto.Keccak256Hash(buf)
	}

	n.hash = h
	n.dirty = false
	return h
}

// Len returns the number of key-value pairs in the trie.
func (t *BinaryTrie) Len() int {
	return countBinaryLeaves(t.root)
}

func countBinaryLeaves(n *binaryNode) int {
	if n == nil {
		return 0
	}
	if n.isLeaf {
		return 1
	}
	return countBinaryLeaves(n.left) + countBinaryLeaves(n.right)
}

// Empty returns true if the trie has no entries.
func (t *BinaryTrie) Empty() bool {
	return t.root == nil
}

// getBit returns the bit at position pos in a 32-byte hash (MSB first).
// pos 0 is the most significant bit of byte 0.
func getBit(h types.Hash, pos int) byte {
	byteIdx := pos / 8
	bitIdx := 7 - (pos % 8)
	if byteIdx >= 32 {
		return 0
	}
	return (h[byteIdx] >> uint(bitIdx)) & 1
}

func copyBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
