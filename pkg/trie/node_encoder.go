// node_encoder.go implements trie node encoding for the Merkle Patricia Trie.
// It provides RLP encoding for branch, extension, and leaf nodes, compact
// hex-prefix encoding, key-nibble conversions, node hash computation with
// small-node inlining, and dirty node tracking for commit optimization.
package trie

import (
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// NodeEncKind identifies the type of an encoded trie node.
type NodeEncKind byte

const (
	NodeEncBranch    NodeEncKind = 0
	NodeEncExtension NodeEncKind = 1
	NodeEncLeaf      NodeEncKind = 2
	NodeEncHash      NodeEncKind = 3
	NodeEncEmpty     NodeEncKind = 4
)

// NodeEncResult holds the result of encoding a trie node: the raw RLP bytes,
// the keccak256 hash of those bytes, and whether the node was inlined.
type NodeEncResult struct {
	RLP    []byte     // raw RLP encoding
	Hash   types.Hash // keccak256(RLP), zero if inlined
	Inline bool       // true if len(RLP) < 32 and node was not force-hashed
	Kind   NodeEncKind
}

// NodeEncTracker tracks dirty nodes during trie modifications and collects
// encoded nodes during commit, enabling efficient incremental hashing.
type NodeEncTracker struct {
	dirty  map[types.Hash]struct{} // set of hashes that need re-encoding
	cached map[types.Hash]*NodeEncResult
	count  int // total nodes tracked
}

// NewNodeEncTracker creates a new dirty node tracker.
func NewNodeEncTracker() *NodeEncTracker {
	return &NodeEncTracker{
		dirty:  make(map[types.Hash]struct{}),
		cached: make(map[types.Hash]*NodeEncResult),
	}
}

// MarkDirty marks a node hash as needing re-encoding.
func (t *NodeEncTracker) MarkDirty(hash types.Hash) {
	t.dirty[hash] = struct{}{}
	delete(t.cached, hash)
}

// IsDirty returns true if the node is dirty (needs re-encoding).
func (t *NodeEncTracker) IsDirty(hash types.Hash) bool {
	_, ok := t.dirty[hash]
	return ok
}

// ClearDirty removes a hash from the dirty set.
func (t *NodeEncTracker) ClearDirty(hash types.Hash) {
	delete(t.dirty, hash)
}

// DirtyCount returns the number of dirty nodes.
func (t *NodeEncTracker) DirtyCount() int {
	return len(t.dirty)
}

// CacheResult stores an encoding result for a node hash.
func (t *NodeEncTracker) CacheResult(hash types.Hash, result *NodeEncResult) {
	t.cached[hash] = result
	delete(t.dirty, hash)
	t.count++
}

// GetCached returns the cached encoding result for a hash, or nil if not cached.
func (t *NodeEncTracker) GetCached(hash types.Hash) *NodeEncResult {
	return t.cached[hash]
}

// CachedCount returns the number of cached encoding results.
func (t *NodeEncTracker) CachedCount() int {
	return len(t.cached)
}

// TotalTracked returns the total number of nodes that have been tracked.
func (t *NodeEncTracker) TotalTracked() int {
	return t.count
}

// Reset clears all tracking state.
func (t *NodeEncTracker) Reset() {
	t.dirty = make(map[types.Hash]struct{})
	t.cached = make(map[types.Hash]*NodeEncResult)
	t.count = 0
}

// EncBranchNode RLP-encodes a branch node (17 children). Each child is either
// a 32-byte hash reference, an inline RLP encoding (< 32 bytes), or empty.
// The 17th element is the optional value stored at this branch point.
func EncBranchNode(children [17][]byte) ([]byte, error) {
	var payload []byte
	for i := 0; i < 17; i++ {
		enc, err := encBranchChild(children[i])
		if err != nil {
			return nil, err
		}
		payload = append(payload, enc...)
	}
	return wrapListPayload(payload), nil
}

// encBranchChild encodes a single child reference for a branch node.
// nil or empty => RLP empty string (0x80)
// otherwise    => RLP string of the data
func encBranchChild(child []byte) ([]byte, error) {
	if len(child) == 0 {
		return []byte{0x80}, nil
	}
	return rlp.EncodeToBytes(child)
}

// EncExtensionNode RLP-encodes an extension node as a 2-element list:
// [compact_encoded_key, child_reference].
// The key must be in hex-nibble form (without terminator); it will be
// compact-encoded internally.
func EncExtensionNode(hexKey []byte, childRef []byte) ([]byte, error) {
	compact := hexToCompact(hexKey)
	keyEnc, err := rlp.EncodeToBytes(compact)
	if err != nil {
		return nil, err
	}
	var childEnc []byte
	if len(childRef) == 0 {
		childEnc = []byte{0x80}
	} else {
		childEnc, err = rlp.EncodeToBytes(childRef)
		if err != nil {
			return nil, err
		}
	}
	payload := append(keyEnc, childEnc...)
	return wrapListPayload(payload), nil
}

// EncLeafNode RLP-encodes a leaf node as a 2-element list:
// [compact_encoded_key_with_terminator, value].
// The key must be in hex-nibble form; a terminator (0x10) is appended before
// compact encoding to mark it as a leaf.
func EncLeafNode(hexKey []byte, value []byte) ([]byte, error) {
	// Append terminator to mark as leaf, then compact-encode.
	keyWithTerm := make([]byte, len(hexKey)+1)
	copy(keyWithTerm, hexKey)
	keyWithTerm[len(hexKey)] = terminatorByte
	compact := hexToCompact(keyWithTerm)

	keyEnc, err := rlp.EncodeToBytes(compact)
	if err != nil {
		return nil, err
	}
	valEnc, err := rlp.EncodeToBytes(value)
	if err != nil {
		return nil, err
	}
	payload := append(keyEnc, valEnc...)
	return wrapListPayload(payload), nil
}

// EncNodeHash computes the keccak256 hash of an RLP-encoded node.
// If the encoding is less than 32 bytes, the raw encoding is returned
// as-is (inlining optimization), unless force is true.
func EncNodeHash(rlpData []byte, force bool) NodeEncResult {
	if len(rlpData) < 32 && !force {
		return NodeEncResult{
			RLP:    rlpData,
			Inline: true,
			Kind:   classifyNodeEnc(rlpData),
		}
	}
	hash := crypto.Keccak256Hash(rlpData)
	return NodeEncResult{
		RLP:  rlpData,
		Hash: hash,
		Kind: classifyNodeEnc(rlpData),
	}
}

// classifyNodeEnc determines the kind of node from its RLP encoding
// by examining the number of elements in the top-level list.
func classifyNodeEnc(data []byte) NodeEncKind {
	if len(data) == 0 {
		return NodeEncEmpty
	}
	items, err := decodeRLPList(data)
	if err != nil {
		if len(data) == 32 {
			return NodeEncHash
		}
		return NodeEncEmpty
	}
	switch len(items) {
	case 17:
		return NodeEncBranch
	case 2:
		key := compactToHex(items[0])
		if hasTerm(key) {
			return NodeEncLeaf
		}
		return NodeEncExtension
	default:
		return NodeEncEmpty
	}
}

// NibblesToKey converts a hex-nibble sequence (without terminator) to a
// packed byte key. The nibble sequence must have even length.
func NibblesToKey(nibbles []byte) []byte {
	// Strip terminator if present.
	if hasTerm(nibbles) {
		nibbles = nibbles[:len(nibbles)-1]
	}
	if len(nibbles)%2 != 0 {
		// Odd-length nibble sequences cannot be cleanly packed; pad with zero.
		padded := make([]byte, len(nibbles)+1)
		copy(padded[1:], nibbles)
		nibbles = padded
	}
	key := make([]byte, len(nibbles)/2)
	for i := 0; i < len(nibbles); i += 2 {
		key[i/2] = nibbles[i]<<4 | nibbles[i+1]
	}
	return key
}

// KeyToNibbles converts a packed byte key to a hex-nibble sequence.
// The returned sequence does not include a terminator.
func KeyToNibbles(key []byte) []byte {
	nibbles := make([]byte, len(key)*2)
	for i, b := range key {
		nibbles[i*2] = b >> 4
		nibbles[i*2+1] = b & 0x0f
	}
	return nibbles
}

// KeyToNibblesWithTerm converts a packed byte key to a hex-nibble sequence
// with terminator appended (for leaf nodes).
func KeyToNibblesWithTerm(key []byte) []byte {
	nibbles := make([]byte, len(key)*2+1)
	for i, b := range key {
		nibbles[i*2] = b >> 4
		nibbles[i*2+1] = b & 0x0f
	}
	nibbles[len(nibbles)-1] = terminatorByte
	return nibbles
}

// CompactEncodeHex performs hex-prefix (HP) encoding on a nibble sequence.
// If isLeaf is true, the terminator flag is set in the compact prefix.
func CompactEncodeHex(nibbles []byte, isLeaf bool) []byte {
	// Build a sequence with optional terminator.
	if isLeaf {
		withTerm := make([]byte, len(nibbles)+1)
		copy(withTerm, nibbles)
		withTerm[len(nibbles)] = terminatorByte
		return hexToCompact(withTerm)
	}
	return hexToCompact(nibbles)
}

// CompactDecodeHex decodes a hex-prefix encoded byte sequence back to
// nibbles and a leaf flag.
func CompactDecodeHex(compact []byte) (nibbles []byte, isLeaf bool) {
	hex := compactToHex(compact)
	if hasTerm(hex) {
		return hex[:len(hex)-1], true
	}
	return hex, false
}

// SharedNibblePrefix returns the length of the common nibble prefix between
// two nibble sequences.
func SharedNibblePrefix(a, b []byte) int {
	return prefixLen(a, b)
}

// EncNodeInline determines whether an RLP-encoded node should be inlined
// (embedded directly in its parent) rather than stored by hash reference.
// The threshold is 32 bytes, matching Ethereum's MPT spec.
func EncNodeInline(rlpData []byte) bool {
	return len(rlpData) < 32
}

// EncNodeRef returns a reference to a node: either its 32-byte hash or
// the inline RLP encoding, depending on the encoding size.
func EncNodeRef(rlpData []byte) []byte {
	if EncNodeInline(rlpData) {
		return rlpData
	}
	return crypto.Keccak256(rlpData)
}

// EncCollapseBranch encodes a full node by replacing each child with its
// hash reference or inline encoding. Returns the RLP of the collapsed branch.
func EncCollapseBranch(fn *fullNode) ([]byte, error) {
	var children [17][]byte
	for i := 0; i < 16; i++ {
		if fn.Children[i] == nil {
			continue
		}
		ref, err := encCollapseChild(fn.Children[i])
		if err != nil {
			return nil, err
		}
		children[i] = ref
	}
	// Value at index 16.
	if fn.Children[16] != nil {
		if v, ok := fn.Children[16].(valueNode); ok {
			children[16] = []byte(v)
		}
	}
	return EncBranchNode(children)
}

// EncCollapseShort encodes a short node by collapsing its child to a hash
// reference or inline encoding. The key is compact-encoded.
func EncCollapseShort(sn *shortNode) ([]byte, error) {
	if hasTerm(sn.Key) {
		// Leaf node.
		if v, ok := sn.Val.(valueNode); ok {
			// Strip terminator for nibble path.
			nibbles := sn.Key[:len(sn.Key)-1]
			return EncLeafNode(nibbles, []byte(v))
		}
	}
	// Extension node.
	childRef, err := encCollapseChild(sn.Val)
	if err != nil {
		return nil, err
	}
	return EncExtensionNode(sn.Key, childRef)
}

// encCollapseChild encodes a child node to its reference form (hash or inline).
func encCollapseChild(n node) ([]byte, error) {
	switch n := n.(type) {
	case nil:
		return nil, nil
	case hashNode:
		return []byte(n), nil
	case valueNode:
		return []byte(n), nil
	case *shortNode:
		enc, err := EncCollapseShort(n)
		if err != nil {
			return nil, err
		}
		return EncNodeRef(enc), nil
	case *fullNode:
		enc, err := EncCollapseBranch(n)
		if err != nil {
			return nil, err
		}
		return EncNodeRef(enc), nil
	default:
		return nil, nil
	}
}

// NodeEncBatch collects multiple encoded nodes for batch database insertion.
type NodeEncBatch struct {
	entries []nodeEncEntry
	size    int
}

type nodeEncEntry struct {
	hash types.Hash
	data []byte
}

// NewNodeEncBatch creates a new batch collector.
func NewNodeEncBatch() *NodeEncBatch {
	return &NodeEncBatch{}
}

// Add stores an encoded node in the batch.
func (b *NodeEncBatch) Add(hash types.Hash, data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	b.entries = append(b.entries, nodeEncEntry{hash: hash, data: cp})
	b.size += len(data)
}

// Len returns the number of entries in the batch.
func (b *NodeEncBatch) Len() int {
	return len(b.entries)
}

// Size returns the total byte size of all entries.
func (b *NodeEncBatch) Size() int {
	return b.size
}

// FlushTo writes all entries to the given NodeDatabase.
func (b *NodeEncBatch) FlushTo(db *NodeDatabase) {
	for _, e := range b.entries {
		db.InsertNode(e.hash, e.data)
	}
}

// Reset clears the batch.
func (b *NodeEncBatch) Reset() {
	b.entries = b.entries[:0]
	b.size = 0
}

// Entries returns a copy of all (hash, data) pairs in the batch.
func (b *NodeEncBatch) Entries() []nodeEncEntry {
	result := make([]nodeEncEntry, len(b.entries))
	copy(result, b.entries)
	return result
}
