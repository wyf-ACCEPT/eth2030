// Package verkle implements Verkle tree node types and key derivation
// for EIP-6800 (Ethereum state stored in Verkle trees).
//
// A Verkle tree uses vector commitments (e.g., Pedersen commitments over
// Bandersnatch) to replace the MPT. Each internal node has 256 children.
// Leaf nodes store 256 values under a common 31-byte "stem".
package verkle

import (
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Tree width: each node has 256 children (1 byte per level).
const (
	NodeWidth    = 256
	StemSize     = 31
	KeySize      = 32
	ValueSize    = 32
	CommitSize   = 32
	MaxDepth     = 31 // Maximum tree depth (one level per stem byte)
)

// NodeType distinguishes between internal nodes and leaf nodes.
type NodeType byte

const (
	InternalNodeType NodeType = 0x01
	LeafNodeType     NodeType = 0x02
	EmptyNodeType    NodeType = 0x00
)

// Commitment represents a 32-byte Verkle tree commitment (Pedersen hash).
// In a production implementation this would be a Banderwagon point.
type Commitment [CommitSize]byte

// IsZero returns true if the commitment is all zeros.
func (c Commitment) IsZero() bool {
	for _, b := range c {
		if b != 0 {
			return false
		}
	}
	return true
}

// Node is the interface for all Verkle tree nodes.
type Node interface {
	// Commit computes or returns the cached commitment for this node.
	Commit() Commitment

	// Hash returns the hash of the node for serialization.
	Hash() types.Hash

	// Type returns the node type.
	Type() NodeType

	// Serialize encodes the node for storage.
	Serialize() []byte
}

// InternalNode represents a Verkle tree internal (branch) node.
// It has up to 256 children, each identified by a single byte.
type InternalNode struct {
	children   [NodeWidth]Node
	commitment Commitment
	depth      int
	dirty      bool
}

// NewInternalNode creates a new empty internal node.
func NewInternalNode(depth int) *InternalNode {
	return &InternalNode{
		depth: depth,
		dirty: true,
	}
}

func (n *InternalNode) Type() NodeType { return InternalNodeType }

func (n *InternalNode) Depth() int { return n.depth }

// Child returns the child at the given index.
func (n *InternalNode) Child(index byte) Node {
	return n.children[index]
}

// SetChild sets the child at the given index and marks the node dirty.
func (n *InternalNode) SetChild(index byte, child Node) {
	n.children[index] = child
	n.dirty = true
}

// ChildCount returns the number of non-nil children.
func (n *InternalNode) ChildCount() int {
	count := 0
	for _, c := range n.children {
		if c != nil {
			count++
		}
	}
	return count
}

// Commit computes the Pedersen vector commitment over child commitments.
// C = sum(childHash[i] * G_i) for each child, where childHash is the
// map-to-field value of the child's commitment point.
func (n *InternalNode) Commit() Commitment {
	if !n.dirty {
		return n.commitment
	}

	values := make([]*big.Int, NodeWidth)
	for i := 0; i < NodeWidth; i++ {
		if n.children[i] != nil {
			c := n.children[i].Commit()
			values[i] = new(big.Int).SetBytes(c[:])
		} else {
			values[i] = new(big.Int)
		}
	}

	point := crypto.PedersenCommit(values)
	n.commitment = crypto.BanderMapToBytes(point)
	n.dirty = false
	return n.commitment
}

func (n *InternalNode) Hash() types.Hash {
	c := n.Commit()
	var h types.Hash
	copy(h[:], c[:])
	return h
}

func (n *InternalNode) Serialize() []byte {
	// Format: [type(1)] [depth(1)] [bitmap(32)] [child_commitments...]
	// Bitmap: which children are present (256 bits = 32 bytes)
	buf := make([]byte, 2+32)
	buf[0] = byte(InternalNodeType)
	buf[1] = byte(n.depth)

	for i := 0; i < NodeWidth; i++ {
		if n.children[i] != nil {
			buf[2+i/8] |= 1 << (uint(i) % 8)
			c := n.children[i].Commit()
			buf = append(buf, c[:]...)
		}
	}
	return buf
}

// LeafNode represents a Verkle tree leaf node.
// It stores up to 256 values under a common 31-byte stem.
type LeafNode struct {
	stem       [StemSize]byte
	values     [NodeWidth]*[ValueSize]byte
	commitment Commitment
	dirty      bool
}

// NewLeafNode creates a new leaf node with the given stem.
func NewLeafNode(stem [StemSize]byte) *LeafNode {
	return &LeafNode{
		stem:  stem,
		dirty: true,
	}
}

func (n *LeafNode) Type() NodeType { return LeafNodeType }

// Stem returns the 31-byte stem of this leaf.
func (n *LeafNode) Stem() [StemSize]byte { return n.stem }

// Get returns the value at the given suffix index, or nil if empty.
func (n *LeafNode) Get(suffix byte) *[ValueSize]byte {
	return n.values[suffix]
}

// Set stores a value at the given suffix index.
func (n *LeafNode) Set(suffix byte, value [ValueSize]byte) {
	v := value
	n.values[suffix] = &v
	n.dirty = true
}

// Delete removes the value at the given suffix index.
func (n *LeafNode) Delete(suffix byte) {
	n.values[suffix] = nil
	n.dirty = true
}

// ValueCount returns the number of non-nil values.
func (n *LeafNode) ValueCount() int {
	count := 0
	for _, v := range n.values {
		if v != nil {
			count++
		}
	}
	return count
}

// Commit computes the leaf Pedersen commitment.
// The commitment encodes the stem and all stored values:
//   C = stem_hash * G_0 + sum(value[i] * G_{i+1})
// where stem_hash maps the stem bytes to a scalar.
func (n *LeafNode) Commit() Commitment {
	if !n.dirty {
		return n.commitment
	}

	values := make([]*big.Int, NodeWidth)
	// Slot 0: stem encoded as a scalar (first 31 bytes, big-endian).
	values[0] = new(big.Int).SetBytes(n.stem[:])
	for i := 0; i < NodeWidth-1; i++ {
		if n.values[i] != nil {
			values[i+1] = new(big.Int).SetBytes(n.values[i][:])
		} else {
			values[i+1] = new(big.Int)
		}
	}

	point := crypto.PedersenCommit(values)
	n.commitment = crypto.BanderMapToBytes(point)
	n.dirty = false
	return n.commitment
}

func (n *LeafNode) Hash() types.Hash {
	c := n.Commit()
	var h types.Hash
	copy(h[:], c[:])
	return h
}

func (n *LeafNode) Serialize() []byte {
	// Format: [type(1)] [stem(31)] [bitmap(32)] [values...]
	buf := make([]byte, 1+StemSize+32)
	buf[0] = byte(LeafNodeType)
	copy(buf[1:1+StemSize], n.stem[:])

	for i := 0; i < NodeWidth; i++ {
		if n.values[i] != nil {
			buf[1+StemSize+i/8] |= 1 << (uint(i) % 8)
			buf = append(buf, n.values[i][:]...)
		}
	}
	return buf
}

// EmptyNode represents an empty slot in the tree.
type EmptyNode struct{}

func (n *EmptyNode) Type() NodeType    { return EmptyNodeType }
func (n *EmptyNode) Commit() Commitment { return Commitment{} }
func (n *EmptyNode) Hash() types.Hash   { return types.Hash{} }
func (n *EmptyNode) Serialize() []byte  { return []byte{byte(EmptyNodeType)} }

// Tree is a simplified Verkle tree implementation.
type Tree struct {
	root *InternalNode
}

// NewTree creates a new empty Verkle tree.
func NewTree() *Tree {
	return &Tree{
		root: NewInternalNode(0),
	}
}

// Root returns the root node.
func (t *Tree) Root() *InternalNode { return t.root }

// RootCommitment returns the root commitment (state root).
func (t *Tree) RootCommitment() Commitment {
	return t.root.Commit()
}

// Get retrieves a value by its 32-byte key.
func (t *Tree) Get(key [KeySize]byte) (*[ValueSize]byte, error) {
	stem, suffix := splitKey(key)
	leaf := t.getLeaf(stem)
	if leaf == nil {
		return nil, nil
	}
	return leaf.Get(suffix), nil
}

// Put stores a value at the given 32-byte key.
func (t *Tree) Put(key [KeySize]byte, value [ValueSize]byte) error {
	stem, suffix := splitKey(key)
	leaf := t.getOrCreateLeaf(stem)
	leaf.Set(suffix, value)
	return nil
}

// Delete removes a value at the given 32-byte key.
func (t *Tree) Delete(key [KeySize]byte) error {
	stem, suffix := splitKey(key)
	leaf := t.getLeaf(stem)
	if leaf == nil {
		return nil
	}
	leaf.Delete(suffix)
	return nil
}

// getLeaf traverses the tree to find the leaf for the given stem.
func (t *Tree) getLeaf(stem [StemSize]byte) *LeafNode {
	node := t.root
	for depth := 0; depth < StemSize; depth++ {
		child := node.Child(stem[depth])
		if child == nil {
			return nil
		}
		switch c := child.(type) {
		case *LeafNode:
			if c.stem == stem {
				return c
			}
			return nil
		case *InternalNode:
			node = c
		default:
			return nil
		}
	}
	return nil
}

// getOrCreateLeaf traverses the tree, creating nodes as needed.
func (t *Tree) getOrCreateLeaf(stem [StemSize]byte) *LeafNode {
	node := t.root
	for depth := 0; depth < StemSize; depth++ {
		child := node.Child(stem[depth])

		if child == nil {
			// No child here: insert a new leaf.
			leaf := NewLeafNode(stem)
			node.SetChild(stem[depth], leaf)
			return leaf
		}

		switch c := child.(type) {
		case *LeafNode:
			if c.stem == stem {
				return c
			}
			// Collision: need to split by creating an internal node.
			newInternal := NewInternalNode(depth + 1)
			node.SetChild(stem[depth], newInternal)

			// Reinsert the existing leaf.
			if depth+1 < StemSize {
				newInternal.SetChild(c.stem[depth+1], c)
			}

			// Continue walking to insert our new leaf.
			node = newInternal
			continue

		case *InternalNode:
			node = c
			continue

		default:
			// Unknown node type, create leaf.
			leaf := NewLeafNode(stem)
			node.SetChild(stem[depth], leaf)
			return leaf
		}
	}

	// Should not reach here in a well-formed tree.
	return nil
}

// splitKey splits a 32-byte key into a 31-byte stem and 1-byte suffix.
func splitKey(key [KeySize]byte) ([StemSize]byte, byte) {
	var stem [StemSize]byte
	copy(stem[:], key[:StemSize])
	return stem, key[StemSize]
}

// Errors for tree operations.
var (
	ErrKeyNotFound = errors.New("key not found in verkle tree")
)
