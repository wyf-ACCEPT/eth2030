// Verkle trie node types with Pedersen commitment support.
//
// Implements VerkleInternalNode (256-child branch), VerkleLeafNode (stem
// + 256 value slots), and VerkleEmptyNode. Uses PedersenConfig for real
// Banderwagon curve commitments. The trie is navigated by the 31-byte
// stem (key bytes 0..30), with the final byte selecting a value slot.

package verkle

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/crypto"
)

// VerkleNodeType distinguishes the three node types in the trie.
type VerkleNodeType byte

const (
	VerkleNodeInternal VerkleNodeType = iota
	VerkleNodeLeaf
	VerkleNodeEmpty
)

// VerkleNode is the interface for all Verkle trie nodes.
type VerkleNode interface {
	NodeCommitment() [32]byte
	Insert(key, value []byte) error
	GetValue(key []byte) ([]byte, error)
	NodeType() VerkleNodeType
	IsDirty() bool
}

var (
	ErrInvalidNodeKey   = errors.New("verkle_node: key must be 32 bytes")
	ErrInvalidNodeValue = errors.New("verkle_node: value must be 32 bytes")
	ErrInsertIntoEmpty  = errors.New("verkle_node: cannot insert into empty node")
	ErrMaxDepthExceeded = errors.New("verkle_node: max tree depth exceeded")
)

// --- VerkleEmptyNode (singleton) ---

type VerkleEmptyNode struct{}

var emptyNodeInstance = &VerkleEmptyNode{}

func EmptyVerkleNode() *VerkleEmptyNode              { return emptyNodeInstance }
func (n *VerkleEmptyNode) NodeCommitment() [32]byte   { return [32]byte{} }
func (n *VerkleEmptyNode) NodeType() VerkleNodeType    { return VerkleNodeEmpty }
func (n *VerkleEmptyNode) IsDirty() bool               { return false }
func (n *VerkleEmptyNode) Insert(key, value []byte) error { return ErrInsertIntoEmpty }
func (n *VerkleEmptyNode) GetValue(key []byte) ([]byte, error) { return nil, nil }

// --- VerkleLeafNode ---

// VerkleLeafNode stores up to 256 values under a 31-byte stem.
// Commitment: C = stem_scalar * G_0 + sum(values[i] * G_{i+1}).
type VerkleLeafNode struct {
	stem       [StemSize]byte
	values     [NodeWidth][ValueSize]byte
	present    [NodeWidth]bool
	commitment [32]byte
	commitPt   *crypto.BanderPoint
	dirty      bool
	config     *PedersenConfig
}

func NewVerkleLeafNode(stem [StemSize]byte, config *PedersenConfig) *VerkleLeafNode {
	return &VerkleLeafNode{stem: stem, dirty: true, config: config}
}

func (n *VerkleLeafNode) NodeType() VerkleNodeType { return VerkleNodeLeaf }
func (n *VerkleLeafNode) IsDirty() bool            { return n.dirty }
func (n *VerkleLeafNode) Stem() [StemSize]byte     { return n.stem }
func (n *VerkleLeafNode) HasValue(suffix byte) bool { return n.present[suffix] }

func (n *VerkleLeafNode) ValueAt(suffix byte) *[ValueSize]byte {
	if !n.present[suffix] {
		return nil
	}
	v := n.values[suffix]
	return &v
}

func (n *VerkleLeafNode) SetValue(suffix byte, value [ValueSize]byte) {
	n.values[suffix] = value
	n.present[suffix] = true
	n.dirty = true
}

func (n *VerkleLeafNode) DeleteValue(suffix byte) {
	n.values[suffix] = [ValueSize]byte{}
	n.present[suffix] = false
	n.dirty = true
}

func (n *VerkleLeafNode) ValueCount() int {
	c := 0
	for _, p := range n.present {
		if p { c++ }
	}
	return c
}

func (n *VerkleLeafNode) Insert(key, value []byte) error {
	if len(key) != KeySize { return ErrInvalidNodeKey }
	if len(value) != ValueSize { return ErrInvalidNodeValue }
	var stem [StemSize]byte
	copy(stem[:], key[:StemSize])
	if stem != n.stem {
		return errors.New("verkle_node: key stem does not match leaf stem")
	}
	var val [ValueSize]byte
	copy(val[:], value)
	n.SetValue(key[StemSize], val)
	return nil
}

func (n *VerkleLeafNode) GetValue(key []byte) ([]byte, error) {
	if len(key) != KeySize { return nil, ErrInvalidNodeKey }
	var stem [StemSize]byte
	copy(stem[:], key[:StemSize])
	if stem != n.stem { return nil, nil }
	suffix := key[StemSize]
	if !n.present[suffix] { return nil, nil }
	out := make([]byte, ValueSize)
	copy(out, n.values[suffix][:])
	return out, nil
}

func (n *VerkleLeafNode) NodeCommitment() [32]byte {
	if !n.dirty { return n.commitment }
	n.recomputeCommitment()
	return n.commitment
}

func (n *VerkleLeafNode) CommitmentPoint() *crypto.BanderPoint {
	if n.dirty { n.recomputeCommitment() }
	return n.commitPt
}

func (n *VerkleLeafNode) recomputeCommitment() {
	scalars := make([]*big.Int, n.config.width)
	scalars[0] = new(big.Int).SetBytes(n.stem[:])
	for i := 0; i < n.config.width-1; i++ {
		if n.present[i] {
			scalars[i+1] = new(big.Int).SetBytes(n.values[i][:])
		} else {
			scalars[i+1] = new(big.Int)
		}
	}
	n.commitPt = crypto.BanderMSM(n.config.generators, scalars)
	n.commitment = crypto.BanderMapToBytes(n.commitPt)
	n.dirty = false
}

// --- VerkleInternalNode ---

// VerkleInternalNode is a 256-way branch node. Its commitment is the
// Pedersen commitment over child commitments.
type VerkleInternalNode struct {
	children   [NodeWidth]VerkleNode
	commitment [32]byte
	commitPt   *crypto.BanderPoint
	depth      int
	dirty      bool
	childCount int
	config     *PedersenConfig
}

func NewVerkleInternalNode(depth int, config *PedersenConfig) *VerkleInternalNode {
	return &VerkleInternalNode{depth: depth, dirty: true, config: config}
}

func (n *VerkleInternalNode) NodeType() VerkleNodeType { return VerkleNodeInternal }
func (n *VerkleInternalNode) IsDirty() bool            { return n.dirty }
func (n *VerkleInternalNode) Depth() int               { return n.depth }
func (n *VerkleInternalNode) ChildAt(index byte) VerkleNode { return n.children[index] }
func (n *VerkleInternalNode) ChildCount() int          { return n.childCount }

func (n *VerkleInternalNode) SetChildAt(index byte, child VerkleNode) {
	wasNil := n.children[index] == nil
	n.children[index] = child
	n.dirty = true
	if wasNil && child != nil { n.childCount++ }
	if !wasNil && child == nil { n.childCount-- }
}

func (n *VerkleInternalNode) Insert(key, value []byte) error {
	if len(key) != KeySize { return ErrInvalidNodeKey }
	if len(value) != ValueSize { return ErrInvalidNodeValue }
	if n.depth >= StemSize { return ErrMaxDepthExceeded }

	var stem [StemSize]byte
	copy(stem[:], key[:StemSize])
	childIdx := stem[n.depth]
	child := n.children[childIdx]

	if child == nil {
		leaf := NewVerkleLeafNode(stem, n.config)
		var val [ValueSize]byte
		copy(val[:], value)
		leaf.SetValue(key[StemSize], val)
		n.SetChildAt(childIdx, leaf)
		return nil
	}

	switch c := child.(type) {
	case *VerkleLeafNode:
		if c.stem == stem { return c.Insert(key, value) }
		return n.splitLeaf(childIdx, c, key, value)
	case *VerkleInternalNode:
		err := c.Insert(key, value)
		if err == nil { n.dirty = true }
		return err
	case *VerkleEmptyNode:
		leaf := NewVerkleLeafNode(stem, n.config)
		var val [ValueSize]byte
		copy(val[:], value)
		leaf.SetValue(key[StemSize], val)
		n.SetChildAt(childIdx, leaf)
		return nil
	default:
		return errors.New("verkle_node: unknown child node type")
	}
}

func (n *VerkleInternalNode) splitLeaf(childIdx byte, existing *VerkleLeafNode, newKey, newValue []byte) error {
	var newStem [StemSize]byte
	copy(newStem[:], newKey[:StemSize])
	newDepth := n.depth + 1
	if newDepth >= StemSize { return ErrMaxDepthExceeded }

	internal := NewVerkleInternalNode(newDepth, n.config)
	existingIdx := existing.stem[newDepth]
	internal.SetChildAt(existingIdx, existing)

	newIdx := newStem[newDepth]
	if newIdx == existingIdx {
		// Still colliding: remove existing and reinsert both.
		internal.SetChildAt(existingIdx, nil)
		eKey := leafToKey(existing)
		eVal := leafFirstValue(existing)
		if err := internal.Insert(eKey, eVal); err != nil { return err }
		if err := internal.Insert(newKey, newValue); err != nil { return err }
	} else {
		newLeaf := NewVerkleLeafNode(newStem, n.config)
		var val [ValueSize]byte
		copy(val[:], newValue)
		newLeaf.SetValue(newKey[StemSize], val)
		internal.SetChildAt(newIdx, newLeaf)
	}

	n.SetChildAt(childIdx, internal)
	return nil
}

func leafToKey(leaf *VerkleLeafNode) []byte {
	key := make([]byte, KeySize)
	copy(key[:StemSize], leaf.stem[:])
	for i := 0; i < NodeWidth; i++ {
		if leaf.present[i] { key[StemSize] = byte(i); break }
	}
	return key
}

func leafFirstValue(leaf *VerkleLeafNode) []byte {
	for i := 0; i < NodeWidth; i++ {
		if leaf.present[i] {
			v := make([]byte, ValueSize)
			copy(v, leaf.values[i][:])
			return v
		}
	}
	return make([]byte, ValueSize)
}

func (n *VerkleInternalNode) GetValue(key []byte) ([]byte, error) {
	if len(key) != KeySize { return nil, ErrInvalidNodeKey }
	if n.depth >= StemSize { return nil, nil }
	child := n.children[key[n.depth]]
	if child == nil { return nil, nil }
	return child.GetValue(key)
}

func (n *VerkleInternalNode) NodeCommitment() [32]byte {
	if !n.dirty { return n.commitment }
	n.recomputeCommitment()
	return n.commitment
}

func (n *VerkleInternalNode) CommitmentPoint() *crypto.BanderPoint {
	if n.dirty { n.recomputeCommitment() }
	return n.commitPt
}

// Commit recursively commits and returns the root commitment.
func (n *VerkleInternalNode) Commit(config *PedersenConfig) [32]byte {
	return n.NodeCommitment()
}

func (n *VerkleInternalNode) recomputeCommitment() {
	scalars := make([]*big.Int, n.config.width)
	for i := 0; i < n.config.width; i++ {
		if n.children[i] != nil {
			c := n.children[i].NodeCommitment()
			scalars[i] = new(big.Int).SetBytes(c[:])
		} else {
			scalars[i] = new(big.Int)
		}
	}
	n.commitPt = crypto.BanderMSM(n.config.generators, scalars)
	n.commitment = crypto.BanderMapToBytes(n.commitPt)
	n.dirty = false
}

// --- VerkleTrie ---

// VerkleTrie wraps a root VerkleInternalNode with a PedersenConfig.
type VerkleTrie struct {
	root   *VerkleInternalNode
	config *PedersenConfig
}

func NewVerkleTrie(config *PedersenConfig) *VerkleTrie {
	if config == nil { config = DefaultPedersenConfig() }
	return &VerkleTrie{root: NewVerkleInternalNode(0, config), config: config}
}

func (vt *VerkleTrie) Root() *VerkleInternalNode { return vt.root }
func (vt *VerkleTrie) Insert(key, value []byte) error { return vt.root.Insert(key, value) }
func (vt *VerkleTrie) Get(key []byte) ([]byte, error) { return vt.root.GetValue(key) }
func (vt *VerkleTrie) RootCommitment() [32]byte { return vt.root.NodeCommitment() }

func (vt *VerkleTrie) NodeCount() int { return countVN(vt.root) }
func (vt *VerkleTrie) LeafCount() int { return countVL(vt.root) }

func countVN(node VerkleNode) int {
	if node == nil { return 0 }
	n, ok := node.(*VerkleInternalNode)
	if !ok { return 1 }
	count := 1
	for i := 0; i < NodeWidth; i++ {
		if n.children[i] != nil { count += countVN(n.children[i]) }
	}
	return count
}

func countVL(node VerkleNode) int {
	if node == nil { return 0 }
	switch n := node.(type) {
	case *VerkleInternalNode:
		count := 0
		for i := 0; i < NodeWidth; i++ {
			if n.children[i] != nil { count += countVL(n.children[i]) }
		}
		return count
	case *VerkleLeafNode:
		return 1
	default:
		return 0
	}
}
