// Package trie implements a Merkle Patricia Trie as defined in the Ethereum Yellow Paper.
package trie

// node is the interface implemented by all trie node types.
type node interface {
	// cache returns the cached hash and dirty flag for this node.
	cache() (hashNode, bool)
}

// fullNode is a branch node with 16 children (one per hex nibble) plus an optional value.
// Children[16] is unused; the value field holds the embedded value at this branch point.
type fullNode struct {
	Children [17]node // 0-15: children indexed by nibble, 16: value slot
	flags    nodeFlag
}

// shortNode is an extension or leaf node. If the key has the terminator flag
// (indicated via HP encoding), it is a leaf node; otherwise it is an extension node.
type shortNode struct {
	Key   []byte // hex-encoded nibble key (may include terminator 0x10)
	Val   node   // child node (for extension) or valueNode (for leaf)
	flags nodeFlag
}

// hashNode is a 32-byte hash reference to a node stored elsewhere.
type hashNode []byte

// valueNode is raw value data stored in a leaf node.
type valueNode []byte

// nodeFlag contains caching information for a node.
type nodeFlag struct {
	hash  hashNode // cached hash of the node
	dirty bool     // whether the node has been modified since last hashing
}

func (n *fullNode) cache() (hashNode, bool)  { return n.flags.hash, n.flags.dirty }
func (n *shortNode) cache() (hashNode, bool) { return n.flags.hash, n.flags.dirty }
func (n hashNode) cache() (hashNode, bool)   { return nil, true }
func (n valueNode) cache() (hashNode, bool)  { return nil, true }

// copy returns a shallow copy of the fullNode.
func (n *fullNode) copy() *fullNode {
	cp := *n
	return &cp
}

// copy returns a copy of the shortNode.
func (n *shortNode) copy() *shortNode {
	cp := *n
	return &cp
}
