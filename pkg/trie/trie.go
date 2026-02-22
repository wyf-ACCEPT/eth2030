package trie

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

var (
	// ErrNotFound is returned when a key is not found in the trie.
	ErrNotFound = errors.New("trie: key not found")
)

// emptyRoot is the root hash of an empty trie: Keccak256(RLP("")).
// RLP("") = 0x80, so emptyRoot = Keccak256([]byte{0x80}).
var emptyRoot = crypto.Keccak256Hash(func() []byte {
	b, _ := rlp.EncodeToBytes([]byte{})
	return b
}())

// Trie is a Merkle Patricia Trie.
type Trie struct {
	root node
}

// New creates a new, empty Merkle Patricia Trie.
func New() *Trie {
	return &Trie{}
}

// Get retrieves the value associated with the given key.
// Returns ErrNotFound if the key does not exist.
func (t *Trie) Get(key []byte) ([]byte, error) {
	value, found := t.get(t.root, keybytesToHex(key), 0)
	if !found {
		return nil, ErrNotFound
	}
	return value, nil
}

func (t *Trie) get(n node, key []byte, pos int) ([]byte, bool) {
	switch n := n.(type) {
	case nil:
		return nil, false
	case valueNode:
		return []byte(n), true
	case *shortNode:
		if len(key)-pos < len(n.Key) || !keysEqual(n.Key, key[pos:pos+len(n.Key)]) {
			return nil, false
		}
		return t.get(n.Val, key, pos+len(n.Key))
	case *fullNode:
		if pos >= len(key) {
			return t.get(n.Children[16], key, pos)
		}
		return t.get(n.Children[key[pos]], key, pos+1)
	case hashNode:
		// In a full implementation, we would resolve the hash from storage.
		// For this in-memory trie, hashNodes should not appear during lookups
		// on a freshly built trie.
		return nil, false
	default:
		return nil, false
	}
}

// Put inserts or updates a key-value pair in the trie.
// If value is empty/nil, the key is deleted instead.
func (t *Trie) Put(key, value []byte) error {
	if len(value) == 0 {
		return t.Delete(key)
	}
	k := keybytesToHex(key)
	n, err := t.insert(t.root, nil, k, valueNode(value))
	if err != nil {
		return err
	}
	t.root = n
	return nil
}

func (t *Trie) insert(n node, prefix, key []byte, value node) (node, error) {
	if len(key) == 0 {
		if v, ok := n.(valueNode); ok {
			if keysEqual(v, value.(valueNode)) {
				return v, nil
			}
		}
		return value, nil
	}

	switch n := n.(type) {
	case nil:
		// Empty slot: create a new leaf node.
		return &shortNode{Key: key, Val: value, flags: nodeFlag{dirty: true}}, nil

	case *shortNode:
		matchLen := prefixLen(key, n.Key)
		// If the entire key matches, update the value.
		if matchLen == len(n.Key) {
			nn, err := t.insert(n.Val, append(prefix, key[:matchLen]...), key[matchLen:], value)
			if err != nil {
				return nil, err
			}
			return &shortNode{Key: n.Key, Val: nn, flags: nodeFlag{dirty: true}}, nil
		}
		// Otherwise, we need to split: create a branch node.
		branch := &fullNode{flags: nodeFlag{dirty: true}}
		// Insert existing child at the appropriate nibble.
		var err error
		existingChild, err := t.insert(nil, append(prefix, n.Key[:matchLen+1]...), n.Key[matchLen+1:], n.Val)
		if err != nil {
			return nil, err
		}
		branch.Children[n.Key[matchLen]] = existingChild
		// Insert new value at the appropriate nibble.
		newChild, err := t.insert(nil, append(prefix, key[:matchLen+1]...), key[matchLen+1:], value)
		if err != nil {
			return nil, err
		}
		branch.Children[key[matchLen]] = newChild
		// If the match length is > 0, wrap the branch in an extension node.
		if matchLen > 0 {
			return &shortNode{Key: key[:matchLen], Val: branch, flags: nodeFlag{dirty: true}}, nil
		}
		return branch, nil

	case *fullNode:
		nn := n.copy()
		nn.flags = nodeFlag{dirty: true}
		child, err := t.insert(n.Children[key[0]], append(prefix, key[0]), key[1:], value)
		if err != nil {
			return nil, err
		}
		nn.Children[key[0]] = child
		return nn, nil

	case hashNode:
		// In a full implementation, we would resolve the hash node first.
		return nil, errors.New("trie: cannot insert into hash node (no database)")

	default:
		return nil, errors.New("trie: unknown node type")
	}
}

// Delete removes a key from the trie.
// If the key does not exist, Delete is a no-op and returns nil.
func (t *Trie) Delete(key []byte) error {
	k := keybytesToHex(key)
	n, err := t.delete(t.root, nil, k)
	if err != nil {
		return err
	}
	t.root = n
	return nil
}

func (t *Trie) delete(n node, prefix, key []byte) (node, error) {
	switch n := n.(type) {
	case nil:
		return nil, nil

	case *shortNode:
		matchLen := prefixLen(key, n.Key)
		if matchLen < len(n.Key) {
			// Key doesn't exist in this subtree.
			return n, nil
		}
		if matchLen == len(key) {
			// Exact match: remove this node entirely.
			return nil, nil
		}
		// Key continues past this node's key. Delete from the child.
		child, err := t.delete(n.Val, append(prefix, key[:len(n.Key)]...), key[len(n.Key):])
		if err != nil {
			return nil, err
		}
		switch child := child.(type) {
		case nil:
			// Child was deleted; remove this extension/leaf too.
			return nil, nil
		case *shortNode:
			// Child collapsed to a short node; merge keys.
			mergedKey := concat(n.Key, child.Key)
			return &shortNode{Key: mergedKey, Val: child.Val, flags: nodeFlag{dirty: true}}, nil
		default:
			return &shortNode{Key: n.Key, Val: child, flags: nodeFlag{dirty: true}}, nil
		}

	case *fullNode:
		nn := n.copy()
		nn.flags = nodeFlag{dirty: true}
		child, err := t.delete(n.Children[key[0]], append(prefix, key[0]), key[1:])
		if err != nil {
			return nil, err
		}
		nn.Children[key[0]] = child
		// Check how many children remain.
		remaining := -1
		for i := 0; i < 17; i++ {
			if nn.Children[i] != nil {
				if remaining >= 0 {
					// More than one child remains: keep the branch.
					return nn, nil
				}
				remaining = i
			}
		}
		if remaining < 0 {
			// No children remain (shouldn't happen with valid trie).
			return nil, nil
		}
		// Only one child remains: collapse the branch.
		if remaining == 16 {
			// The remaining "child" is the value at this branch.
			// Wrap it in a leaf with the terminator.
			return &shortNode{
				Key:   []byte{terminatorByte},
				Val:   nn.Children[16],
				flags: nodeFlag{dirty: true},
			}, nil
		}
		child = nn.Children[remaining]
		if cnode, ok := child.(*shortNode); ok {
			// Merge the nibble with the child's key.
			mergedKey := concat([]byte{byte(remaining)}, cnode.Key)
			return &shortNode{Key: mergedKey, Val: cnode.Val, flags: nodeFlag{dirty: true}}, nil
		}
		// Child is a full node or value node; create a new short node.
		return &shortNode{
			Key:   []byte{byte(remaining)},
			Val:   child,
			flags: nodeFlag{dirty: true},
		}, nil

	case valueNode:
		if len(key) == 0 {
			return nil, nil
		}
		return n, nil

	case hashNode:
		return nil, errors.New("trie: cannot delete from hash node (no database)")

	default:
		return nil, errors.New("trie: unknown node type")
	}
}

// Hash computes the Keccak-256 root hash of the trie.
// An empty trie returns the hash of the RLP encoding of the empty string.
func (t *Trie) Hash() types.Hash {
	if t.root == nil {
		return emptyRoot
	}
	h := newHasher()
	hashed, cached := h.hash(t.root, true)
	t.root = cached
	switch n := hashed.(type) {
	case hashNode:
		return types.BytesToHash(n)
	default:
		// If the root is too small to be hashed (< 32 bytes RLP), we
		// forced the hash, so this shouldn't happen. But handle it
		// by encoding and hashing.
		enc, _ := encodeNode(hashed)
		return crypto.Keccak256Hash(enc)
	}
}

// Len returns the number of key-value pairs stored in the trie.
// This traverses the entire trie, so it is O(n).
func (t *Trie) Len() int {
	return countValues(t.root)
}

// Empty returns true if the trie has no entries.
func (t *Trie) Empty() bool {
	return t.root == nil
}

// countValues recursively counts the number of value nodes in the trie.
func countValues(n node) int {
	switch n := n.(type) {
	case nil:
		return 0
	case valueNode:
		return 1
	case *shortNode:
		return countValues(n.Val)
	case *fullNode:
		count := 0
		for i := 0; i < 17; i++ {
			count += countValues(n.Children[i])
		}
		return count
	case hashNode:
		return 0 // cannot count through unresolved hash nodes
	default:
		return 0
	}
}

// keysEqual returns true if two byte slices are equal.
func keysEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// concat concatenates two byte slices into a new slice.
func concat(a, b []byte) []byte {
	r := make([]byte, len(a)+len(b))
	copy(r, a)
	copy(r[len(a):], b)
	return r
}
