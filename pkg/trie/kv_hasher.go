// kv_hasher.go provides a high-level TrieHasher that computes MPT root hashes
// from sorted key-value pairs without requiring a full trie build. This is
// useful for computing transaction trie roots, receipt trie roots, and other
// derived tries where the data is available as a flat list.
package trie

import (
	"sort"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
	"github.com/eth2028/eth2028/rlp"
)

// KeyValuePair represents a key-value pair for trie hashing.
type KeyValuePair struct {
	Key   []byte
	Value []byte
}

// TrieHasher computes MPT trie root hashes from key-value pairs.
// It builds an in-memory trie and computes the Keccak-256 root hash.
type TrieHasher struct{}

// NewTrieHasher creates a new TrieHasher.
func NewTrieHasher() *TrieHasher {
	return &TrieHasher{}
}

// HashChildren hashes a node's children recursively. If the RLP-encoded node
// is smaller than 32 bytes, the raw encoding is returned (short node
// optimization / inlining). Otherwise, the Keccak-256 hash of the encoding
// is returned.
func (th *TrieHasher) HashChildren(nodeData []byte) []byte {
	if len(nodeData) < 32 {
		// Short node optimization: nodes smaller than 32 bytes are inlined
		// rather than hashed.
		return nodeData
	}
	return crypto.Keccak256(nodeData)
}

// HashRoot computes the MPT root hash from a list of key-value pairs.
// The pairs are sorted by key before building the trie. Empty pairs
// return the empty trie root hash.
func (th *TrieHasher) HashRoot(pairs []KeyValuePair) types.Hash {
	if len(pairs) == 0 {
		return emptyRoot
	}

	// Sort pairs by key for deterministic ordering.
	sorted := make([]KeyValuePair, len(pairs))
	copy(sorted, pairs)
	sort.Slice(sorted, func(i, j int) bool {
		return compareBytesLess(sorted[i].Key, sorted[j].Key)
	})

	// Build a trie from the sorted pairs and compute its root hash.
	t := New()
	for _, p := range sorted {
		if len(p.Value) == 0 {
			continue
		}
		if err := t.Put(p.Key, p.Value); err != nil {
			// Should not happen with valid key-value pairs.
			return emptyRoot
		}
	}
	return t.Hash()
}

// HashSecureTrie computes a secure trie root hash where each key is first
// hashed with Keccak-256 before insertion. This is the standard approach
// for the Ethereum state trie and storage tries to prevent key-length
// extension attacks and ensure uniform key distribution.
func (th *TrieHasher) HashSecureTrie(pairs []KeyValuePair) types.Hash {
	if len(pairs) == 0 {
		return emptyRoot
	}

	// Hash each key with Keccak-256 before inserting into the trie.
	t := New()
	for _, p := range pairs {
		if len(p.Value) == 0 {
			continue
		}
		hashedKey := crypto.Keccak256(p.Key)
		if err := t.Put(hashedKey, p.Value); err != nil {
			return emptyRoot
		}
	}
	return t.Hash()
}

// EstimateTrieSize estimates the storage size in bytes for a trie with
// the given parameters. This provides a rough estimate useful for
// pre-allocating buffers or capacity planning.
//
// The estimate accounts for:
//   - Node overhead (pointers, flags, hash caches)
//   - RLP encoding overhead per node
//   - Branch nodes (one per 16-way split)
//   - Extension/leaf node key storage
//   - Value storage
func EstimateTrieSize(numKeys int, avgKeyLen, avgValLen int) int {
	if numKeys <= 0 {
		return 0
	}

	// Each leaf node stores: key (nibble-expanded) + value + node overhead.
	// Nibble-expanded key is roughly 2x the byte key length.
	leafSize := 2*avgKeyLen + avgValLen + 64 // 64 bytes for node overhead (flags, pointers, hash)

	// Estimate branch nodes: for uniformly distributed keys, we get roughly
	// numKeys/16 branch nodes at each level, with log16(numKeys) levels.
	// Each branch node has 17 slots (16 children + value).
	branchNodeSize := 17 * 32 // 17 hash references, 32 bytes each
	estimatedBranches := 0
	remaining := numKeys
	for remaining > 1 {
		branches := (remaining + 15) / 16
		estimatedBranches += branches
		remaining = branches
	}

	// RLP encoding overhead: ~3 bytes per node for list headers.
	rlpOverhead := 3

	totalLeaf := numKeys * (leafSize + rlpOverhead)
	totalBranch := estimatedBranches * (branchNodeSize + rlpOverhead)

	return totalLeaf + totalBranch
}

// DerivableList is an interface for types that can provide key-value pairs
// for trie root derivation (e.g., transaction lists, receipt lists).
type DerivableList interface {
	Len() int
	EncodeIndex(i int) ([]byte, error)
}

// DeriveSha computes the trie root hash from a DerivableList.
// Keys are RLP-encoded indices, values are the encoded list elements.
func DeriveSha(list DerivableList) types.Hash {
	th := NewTrieHasher()
	pairs := make([]KeyValuePair, list.Len())
	for i := 0; i < list.Len(); i++ {
		key, _ := rlp.EncodeToBytes(uint64(i))
		val, err := list.EncodeIndex(i)
		if err != nil {
			continue
		}
		pairs[i] = KeyValuePair{Key: key, Value: val}
	}
	return th.HashRoot(pairs)
}

// compareBytesLess returns true if a < b lexicographically.
func compareBytesLess(a, b []byte) bool {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return len(a) < len(b)
}
