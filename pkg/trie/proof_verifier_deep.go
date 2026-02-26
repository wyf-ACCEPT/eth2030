// proof_verifier_deep.go extends proof verification with proof size estimation,
// compact proof encoding (shared prefix compression), batch binary proof
// verification, and cross-trie proof comparison utilities.
package trie

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Extended proof verifier errors.
var (
	ErrCompactProofEmpty   = errors.New("compact_proof: empty proof data")
	ErrCompactProofCorrupt = errors.New("compact_proof: corrupted encoding")
	ErrBatchProofEmpty     = errors.New("batch_proof: no proofs to verify")
	ErrBatchProofRootNil   = errors.New("batch_proof: nil root hash")
	ErrProofSizeNegative   = errors.New("proof_size: negative estimate")
	ErrCrossTrieMismatch   = errors.New("cross_trie: proof results differ")
)

// ProofSizeEstimator estimates the proof size for a key in a given trie type
// without actually generating the proof. This is useful for bandwidth
// planning and DoS protection.
type ProofSizeEstimator struct {
	mu sync.Mutex
}

// NewProofSizeEstimator creates a new estimator.
func NewProofSizeEstimator() *ProofSizeEstimator {
	return &ProofSizeEstimator{}
}

// EstimateMPTProofSize estimates the byte size of an MPT proof for a given
// key based on the trie depth. MPT proofs consist of RLP-encoded nodes
// along the path. Average node size is ~200 bytes, depth ~7-10.
func (e *ProofSizeEstimator) EstimateMPTProofSize(trieDepth int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if trieDepth <= 0 {
		return 0
	}
	// Average MPT node RLP size ~200 bytes, plus 32 bytes key encoding.
	return trieDepth*200 + 32
}

// EstimateBinaryProofSize estimates the byte size of a binary trie proof.
// Binary proofs contain one 32-byte sibling hash per level, plus key and value.
func (e *ProofSizeEstimator) EstimateBinaryProofSize(trieDepth, valueSize int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if trieDepth <= 0 {
		return 32 + valueSize // just key + value
	}
	// 32 bytes per sibling + 32 bytes key + value size.
	return trieDepth*32 + 32 + valueSize
}

// EstimateIPAProofSize estimates the byte size of an IPA-based proof.
// IPA proofs contain commitments (32 bytes each) plus IPA proof data.
func (e *ProofSizeEstimator) EstimateIPAProofSize(pathDepth int) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if pathDepth <= 0 {
		return 0
	}
	// Each commitment is 32 bytes, IPA proof is ~544 bytes (17 * 32).
	return pathDepth*32 + 544 + 32 // commitments + IPA + key
}

// CompactProof is an encoded proof that uses shared prefix compression
// to reduce size when multiple proof nodes share common prefixes.
type CompactProof struct {
	// EncodedData holds the compact-encoded proof data.
	EncodedData []byte
	// NumNodes is the number of original proof nodes.
	NumNodes int
	// OriginalSize is the uncompressed size for comparison.
	OriginalSize int
}

// CompactProofEncoder encodes and decodes proofs in a compact format
// that eliminates redundant prefix data shared between sibling nodes.
type CompactProofEncoder struct{}

// NewCompactProofEncoder creates a new encoder.
func NewCompactProofEncoder() *CompactProofEncoder {
	return &CompactProofEncoder{}
}

// Encode compresses an MPT proof by using length-prefixed encoding
// with shared-prefix elimination.
func (enc *CompactProofEncoder) Encode(proof [][]byte) (*CompactProof, error) {
	if len(proof) == 0 {
		return nil, ErrCompactProofEmpty
	}

	originalSize := 0
	for _, n := range proof {
		originalSize += len(n)
	}

	// Simple compact encoding: for each node, store the length of shared
	// prefix with the previous node, then the unique suffix.
	var buf bytes.Buffer

	// Write number of nodes as 2-byte big-endian.
	buf.WriteByte(byte(len(proof) >> 8))
	buf.WriteByte(byte(len(proof)))

	prevNode := []byte{}
	for _, node := range proof {
		// Find shared prefix length with previous node.
		shared := 0
		for shared < len(prevNode) && shared < len(node) && prevNode[shared] == node[shared] {
			shared++
		}
		if shared > 0xFFFF {
			shared = 0xFFFF
		}

		// Write: shared prefix length (2 bytes) + suffix length (2 bytes) + suffix data.
		suffixLen := len(node) - shared
		buf.WriteByte(byte(shared >> 8))
		buf.WriteByte(byte(shared))
		buf.WriteByte(byte(suffixLen >> 8))
		buf.WriteByte(byte(suffixLen))
		buf.Write(node[shared:])

		prevNode = node
	}

	return &CompactProof{
		EncodedData:  buf.Bytes(),
		NumNodes:     len(proof),
		OriginalSize: originalSize,
	}, nil
}

// Decode restores an MPT proof from compact encoding.
func (enc *CompactProofEncoder) Decode(cp *CompactProof) ([][]byte, error) {
	if cp == nil || len(cp.EncodedData) < 2 {
		return nil, ErrCompactProofCorrupt
	}

	data := cp.EncodedData
	numNodes := int(data[0])<<8 | int(data[1])
	pos := 2

	proof := make([][]byte, numNodes)
	prevNode := []byte{}

	for i := 0; i < numNodes; i++ {
		if pos+4 > len(data) {
			return nil, ErrCompactProofCorrupt
		}

		shared := int(data[pos])<<8 | int(data[pos+1])
		suffixLen := int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4

		if pos+suffixLen > len(data) {
			return nil, ErrCompactProofCorrupt
		}
		if shared > len(prevNode) {
			return nil, ErrCompactProofCorrupt
		}

		node := make([]byte, shared+suffixLen)
		copy(node[:shared], prevNode[:shared])
		copy(node[shared:], data[pos:pos+suffixLen])
		pos += suffixLen

		proof[i] = node
		prevNode = node
	}

	return proof, nil
}

// CompressionRatio returns the compression ratio of a compact proof
// (compressed / original). Lower is better.
func (cp *CompactProof) CompressionRatio() float64 {
	if cp.OriginalSize == 0 {
		return 1.0
	}
	return float64(len(cp.EncodedData)) / float64(cp.OriginalSize)
}

// BatchBinaryProofResult holds the results of batch binary proof verification.
type BatchBinaryProofResult struct {
	Results []BinaryProofResult
	// Valid is the count of successfully verified proofs.
	Valid int
	// Invalid is the count of failed proofs.
	Invalid int
}

// VerifyBinaryProofBatch verifies multiple binary trie proofs against the
// same root hash. Returns results for each proof and a summary.
func VerifyBinaryProofBatch(rootHash types.Hash, proofs []*BinaryProof) (*BatchBinaryProofResult, error) {
	if len(proofs) == 0 {
		return nil, ErrBatchProofEmpty
	}

	result := &BatchBinaryProofResult{
		Results: make([]BinaryProofResult, len(proofs)),
	}

	for i, proof := range proofs {
		if proof == nil {
			result.Results[i] = BinaryProofResult{Exists: false}
			result.Invalid++
			continue
		}

		r, err := VerifyBinaryProof(rootHash, proof)
		if err != nil {
			result.Results[i] = BinaryProofResult{Key: proof.Key, Exists: false}
			result.Invalid++
			continue
		}
		result.Results[i] = *r
		result.Valid++
	}

	return result, nil
}

// CrossTrieProofResult holds the comparison between an MPT proof and a
// binary trie proof for the same key.
type CrossTrieProofResult struct {
	Key          []byte
	MPTExists    bool
	MPTValue     []byte
	BinaryExists bool
	BinaryValue  []byte
	Match        bool // true if both proofs agree on existence and value
}

// CompareCrossTrieProofs verifies an MPT proof and a binary proof for the
// same key and checks they agree on existence and value.
func CompareCrossTrieProofs(
	mptRoot types.Hash,
	mptKey []byte,
	mptProof [][]byte,
	binaryRoot types.Hash,
	binaryProof *BinaryProof,
) (*CrossTrieProofResult, error) {
	if mptKey == nil {
		return nil, ErrProofNilInput
	}

	result := &CrossTrieProofResult{Key: mptKey}

	// Verify MPT proof.
	mptResult, err := VerifyMPTProof(mptRoot, mptKey, mptProof)
	if err != nil {
		return nil, fmt.Errorf("cross_trie: MPT proof failed: %w", err)
	}
	result.MPTExists = mptResult.Exists
	result.MPTValue = mptResult.Value

	// Verify binary proof.
	if binaryProof != nil {
		binaryResult, err := VerifyBinaryProof(binaryRoot, binaryProof)
		if err != nil {
			return nil, fmt.Errorf("cross_trie: binary proof failed: %w", err)
		}
		result.BinaryExists = binaryResult.Exists
		result.BinaryValue = binaryResult.Value
	}

	// Compare results.
	result.Match = result.MPTExists == result.BinaryExists &&
		bytes.Equal(result.MPTValue, result.BinaryValue)

	return result, nil
}

// ProofCacheEntry stores a cached proof for a specific key and root.
type ProofCacheEntry struct {
	Root  types.Hash
	Key   types.Hash
	Proof [][]byte
	Size  int
}

// ProofCache is an LRU-like cache for recently verified proofs.
type ProofCache struct {
	mu      sync.Mutex
	entries map[types.Hash]*ProofCacheEntry
	maxSize int
}

// NewProofCache creates a proof cache with the given maximum number of entries.
func NewProofCache(maxSize int) *ProofCache {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &ProofCache{
		entries: make(map[types.Hash]*ProofCacheEntry),
		maxSize: maxSize,
	}
}

// Put adds a proof to the cache.
func (pc *ProofCache) Put(root types.Hash, key []byte, proof [][]byte) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	hk := crypto.Keccak256Hash(key)
	// Simple cache key: hash of root + key.
	cacheKey := crypto.Keccak256Hash(append(root[:], hk[:]...))

	if len(pc.entries) >= pc.maxSize {
		// Evict one entry (simple FIFO: remove any one).
		for k := range pc.entries {
			delete(pc.entries, k)
			break
		}
	}

	totalSize := 0
	for _, n := range proof {
		totalSize += len(n)
	}

	pc.entries[cacheKey] = &ProofCacheEntry{
		Root:  root,
		Key:   hk,
		Proof: proof,
		Size:  totalSize,
	}
}

// Get retrieves a cached proof. Returns nil if not found.
func (pc *ProofCache) Get(root types.Hash, key []byte) [][]byte {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	hk := crypto.Keccak256Hash(key)
	cacheKey := crypto.Keccak256Hash(append(root[:], hk[:]...))

	entry, ok := pc.entries[cacheKey]
	if !ok {
		return nil
	}
	return entry.Proof
}

// Len returns the number of cached proofs.
func (pc *ProofCache) Len() int {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return len(pc.entries)
}

// Clear removes all entries from the cache.
func (pc *ProofCache) Clear() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.entries = make(map[types.Hash]*ProofCacheEntry)
}
