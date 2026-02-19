// binary_announce.go implements the Announce Binary Tree for state change
// announcements. Part of the EL sustainability track: announce binary tree
// provides a compact, proof-friendly structure for broadcasting state diffs.
package trie

import (
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// BinaryNode represents a node in the announcement binary trie.
// It can be either an internal node (with Left/Right children) or a
// leaf (with Key/Value set and no children).
type BinaryNode struct {
	Left  *BinaryNode
	Right *BinaryNode
	Key   []byte
	Value []byte
	Hash  types.Hash
}

// BinaryProofAnnounce is a Merkle inclusion proof for the announcement
// binary trie. The naming avoids collisions with BinaryProof in binary_proof.go.
type BinaryProofAnnounce struct {
	Key   []byte
	Value []byte
	Path  []types.Hash
	Bits  []bool
}

// AnnounceBinaryTrie is a binary Merkle trie optimised for state change
// announcements. It stores arbitrary key-value pairs keyed by the keccak256
// of the raw key, walking bits MSB-first. Thread-safe.
type AnnounceBinaryTrie struct {
	mu   sync.RWMutex
	root *announceBinaryNode
	size int
}

// announceBinaryNode is the internal node representation.
type announceBinaryNode struct {
	left   *announceBinaryNode
	right  *announceBinaryNode
	isLeaf bool
	key    types.Hash // hashed key
	rawKey []byte     // original key (for retrieval)
	value  []byte
	hash   types.Hash
	dirty  bool
}

// NewAnnounceBinaryTrie creates a new, empty announcement binary trie.
func NewAnnounceBinaryTrie() *AnnounceBinaryTrie {
	return &AnnounceBinaryTrie{}
}

// Insert adds or updates a key-value pair. The key is hashed internally with
// keccak256 to determine trie placement.
func (t *AnnounceBinaryTrie) Insert(key []byte, value []byte) error {
	if key == nil {
		return ErrNotFound
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	hk := crypto.Keccak256Hash(key)
	existed := false
	t.root, existed = announceInsert(t.root, hk, key, value, 0)
	if !existed {
		t.size++
	}
	return nil
}

// Get retrieves the value for a key. Returns the value and true if found.
func (t *AnnounceBinaryTrie) Get(key []byte) ([]byte, bool) {
	if key == nil {
		return nil, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	hk := crypto.Keccak256Hash(key)
	n := t.root
	for depth := 0; n != nil; depth++ {
		if n.isLeaf {
			if n.key == hk {
				v := make([]byte, len(n.value))
				copy(v, n.value)
				return v, true
			}
			return nil, false
		}
		if getBit(hk, depth) == 0 {
			n = n.left
		} else {
			n = n.right
		}
	}
	return nil, false
}

// Delete removes a key from the trie. Returns true if the key was found.
func (t *AnnounceBinaryTrie) Delete(key []byte) bool {
	if key == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	hk := crypto.Keccak256Hash(key)
	var found bool
	t.root, found = announceDelete(t.root, hk, 0)
	if found {
		t.size--
	}
	return found
}

// Root computes and returns the keccak256 Merkle root of the trie.
func (t *AnnounceBinaryTrie) Root() types.Hash {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.root == nil {
		return types.Hash{}
	}
	return announceHash(t.root)
}

// Prove generates a Merkle inclusion proof for the given key.
func (t *AnnounceBinaryTrie) Prove(key []byte) (*BinaryProofAnnounce, error) {
	if key == nil {
		return nil, ErrNotFound
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Ensure hashes are up to date.
	if t.root != nil {
		announceHash(t.root)
	}

	hk := crypto.Keccak256Hash(key)
	var path []types.Hash
	var bits []bool

	n := t.root
	for depth := 0; n != nil; depth++ {
		if n.isLeaf {
			if n.key != hk {
				return nil, ErrNotFound
			}
			return &BinaryProofAnnounce{
				Key:   copyBytes(key),
				Value: copyBytes(n.value),
				Path:  path,
				Bits:  bits,
			}, nil
		}
		bit := getBit(hk, depth)
		if bit == 0 {
			// Going left; sibling is right.
			path = append(path, announceHash(n.right))
			bits = append(bits, false)
			n = n.left
		} else {
			// Going right; sibling is left.
			path = append(path, announceHash(n.left))
			bits = append(bits, true)
			n = n.right
		}
	}
	return nil, ErrNotFound
}

// VerifyAnnounceProof verifies a Merkle proof against a given root hash, key,
// and proof for the announcement binary trie.
func VerifyAnnounceProof(root types.Hash, key []byte, proof *BinaryProofAnnounce) bool {
	if proof == nil || key == nil {
		return false
	}

	hk := crypto.Keccak256Hash(key)

	// Reconstruct the leaf hash.
	buf := make([]byte, 1+32+len(proof.Value))
	buf[0] = 0x00 // leaf prefix
	copy(buf[1:33], hk[:])
	copy(buf[33:], proof.Value)
	current := crypto.Keccak256Hash(buf)

	// Walk from leaf back to root.
	for i := len(proof.Path) - 1; i >= 0; i-- {
		sibling := proof.Path[i]
		branchBuf := make([]byte, 1+32+32)
		branchBuf[0] = 0x01 // branch prefix
		if !proof.Bits[i] {
			// We went left, so current is left child.
			copy(branchBuf[1:33], current[:])
			copy(branchBuf[33:65], sibling[:])
		} else {
			// We went right, so current is right child.
			copy(branchBuf[1:33], sibling[:])
			copy(branchBuf[33:65], current[:])
		}
		current = crypto.Keccak256Hash(branchBuf)
	}

	return current == root
}

// announceInsert inserts into the announcement trie. Returns the updated node
// and whether the key already existed (for size tracking).
func announceInsert(n *announceBinaryNode, hk types.Hash, rawKey, value []byte, depth int) (*announceBinaryNode, bool) {
	if n == nil {
		return &announceBinaryNode{
			isLeaf: true,
			key:    hk,
			rawKey: copyBytes(rawKey),
			value:  copyBytes(value),
			dirty:  true,
		}, false
	}

	if n.isLeaf {
		if n.key == hk {
			// Update existing.
			n.value = copyBytes(value)
			n.dirty = true
			return n, true
		}
		// Split.
		return announceSplitLeaf(n, hk, rawKey, value, depth), false
	}

	// Branch: descend.
	n.dirty = true
	var existed bool
	if getBit(hk, depth) == 0 {
		n.left, existed = announceInsert(n.left, hk, rawKey, value, depth+1)
	} else {
		n.right, existed = announceInsert(n.right, hk, rawKey, value, depth+1)
	}
	return n, existed
}

func announceSplitLeaf(existing *announceBinaryNode, newKey types.Hash, rawKey, newValue []byte, depth int) *announceBinaryNode {
	existBit := getBit(existing.key, depth)
	newBit := getBit(newKey, depth)

	if existBit == newBit {
		child := announceSplitLeaf(existing, newKey, rawKey, newValue, depth+1)
		branch := &announceBinaryNode{dirty: true}
		if existBit == 0 {
			branch.left = child
		} else {
			branch.right = child
		}
		return branch
	}

	newLeaf := &announceBinaryNode{
		isLeaf: true,
		key:    newKey,
		rawKey: copyBytes(rawKey),
		value:  copyBytes(newValue),
		dirty:  true,
	}
	existing.dirty = true
	branch := &announceBinaryNode{dirty: true}
	if existBit == 0 {
		branch.left = existing
		branch.right = newLeaf
	} else {
		branch.left = newLeaf
		branch.right = existing
	}
	return branch
}

func announceDelete(n *announceBinaryNode, hk types.Hash, depth int) (*announceBinaryNode, bool) {
	if n == nil {
		return nil, false
	}

	if n.isLeaf {
		if n.key == hk {
			return nil, true
		}
		return n, false
	}

	var found bool
	n.dirty = true
	if getBit(hk, depth) == 0 {
		n.left, found = announceDelete(n.left, hk, depth+1)
	} else {
		n.right, found = announceDelete(n.right, hk, depth+1)
	}

	// Collapse single-child branches.
	if n.left == nil && n.right == nil {
		return nil, found
	}
	if n.left == nil && n.right != nil && n.right.isLeaf {
		return n.right, found
	}
	if n.right == nil && n.left != nil && n.left.isLeaf {
		return n.left, found
	}
	return n, found
}

func announceHash(n *announceBinaryNode) types.Hash {
	if n == nil {
		return types.Hash{}
	}
	if !n.dirty && n.hash != (types.Hash{}) {
		return n.hash
	}

	var h types.Hash
	if n.isLeaf {
		buf := make([]byte, 1+32+len(n.value))
		buf[0] = 0x00
		copy(buf[1:33], n.key[:])
		copy(buf[33:], n.value)
		h = crypto.Keccak256Hash(buf)
	} else {
		leftH := announceHash(n.left)
		rightH := announceHash(n.right)
		buf := make([]byte, 1+32+32)
		buf[0] = 0x01
		copy(buf[1:33], leftH[:])
		copy(buf[33:65], rightH[:])
		h = crypto.Keccak256Hash(buf)
	}

	n.hash = h
	n.dirty = false
	return h
}

// --- AnnouncementSet: batch state change announcements ---

// StateChange represents a single state modification.
type StateChange struct {
	Addr   types.Address
	Slot   types.Hash
	OldVal types.Hash
	NewVal types.Hash
}

// AnnouncementSet collects state changes and builds an announcement trie.
// Thread-safe.
type AnnouncementSet struct {
	mu      sync.Mutex
	changes []StateChange
}

// NewAnnouncementSet creates a new, empty announcement set.
func NewAnnouncementSet() *AnnouncementSet {
	return &AnnouncementSet{}
}

// AddStateChange records a state modification.
func (as *AnnouncementSet) AddStateChange(addr types.Address, slot types.Hash, oldVal, newVal types.Hash) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.changes = append(as.changes, StateChange{
		Addr:   addr,
		Slot:   slot,
		OldVal: oldVal,
		NewVal: newVal,
	})
}

// Size returns the number of recorded state changes.
func (as *AnnouncementSet) Size() int {
	as.mu.Lock()
	defer as.mu.Unlock()
	return len(as.changes)
}

// BuildAnnouncementTree builds a BinaryTrie from the collected state changes.
// Each change is keyed by keccak256(addr || slot) and the value is the
// concatenation of oldVal and newVal (64 bytes).
func (as *AnnouncementSet) BuildAnnouncementTree() *AnnounceBinaryTrie {
	as.mu.Lock()
	changes := make([]StateChange, len(as.changes))
	copy(changes, as.changes)
	as.mu.Unlock()

	t := NewAnnounceBinaryTrie()
	for _, sc := range changes {
		// Key: addr || slot.
		keyBuf := make([]byte, 20+32)
		copy(keyBuf[:20], sc.Addr[:])
		copy(keyBuf[20:], sc.Slot[:])

		// Value: oldVal || newVal.
		valBuf := make([]byte, 64)
		copy(valBuf[:32], sc.OldVal[:])
		copy(valBuf[32:], sc.NewVal[:])

		t.Insert(keyBuf, valBuf)
	}
	return t
}

// ExportBinaryNode converts the internal trie root to a BinaryNode tree
// suitable for external inspection.
func (t *AnnounceBinaryTrie) ExportBinaryNode() *BinaryNode {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return exportNode(t.root)
}

func exportNode(n *announceBinaryNode) *BinaryNode {
	if n == nil {
		return nil
	}
	bn := &BinaryNode{
		Hash: announceHash(n),
	}
	if n.isLeaf {
		bn.Key = copyBytes(n.rawKey)
		bn.Value = copyBytes(n.value)
	} else {
		bn.Left = exportNode(n.left)
		bn.Right = exportNode(n.right)
	}
	return bn
}

// Len returns the number of key-value pairs in the trie.
func (t *AnnounceBinaryTrie) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}
