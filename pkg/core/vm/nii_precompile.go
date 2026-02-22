// Enhanced NII (Non-Interactive Inclusion) precompile supporting batch inclusion
// proofs and multi-path verification. Part of the EL Cryptography roadmap track.
//
// This precompile provides accelerated Merkle inclusion proof verification
// for state proofs, rollup data, and cross-chain bridges.
package vm

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/crypto"
)

// Errors for the enhanced NII precompile.
var (
	ErrNIIInvalidProof       = errors.New("nii_enhanced: invalid inclusion proof")
	ErrNIIBatchTooLarge      = errors.New("nii_enhanced: batch size exceeds maximum")
	ErrNIIProofDepthExceeded = errors.New("nii_enhanced: proof depth exceeds maximum")
	ErrNIIEmptyKey           = errors.New("nii_enhanced: key is empty")
	ErrNIIEmptyRoot          = errors.New("nii_enhanced: root is empty")
)

// Gas cost constants for the enhanced NII precompile.
const (
	niiEnhancedBaseGas    = 100 // base gas for any verification
	niiEnhancedPerStepGas = 50  // gas per proof step (hash computation)
	niiEnhancedBatchBonus = 80  // percentage discount for batching (80% of individual)
)

// NIIEnhancedConfig configures the enhanced NII precompile.
type NIIEnhancedConfig struct {
	MaxBatchSize  int // maximum number of items in a batch verification
	MaxProofDepth int // maximum depth of a Merkle proof path
	CacheSize     int // maximum number of cached proof results
}

// NIIBatchProof holds the result of a batch inclusion verification.
type NIIBatchProof struct {
	Proofs        []bool // per-item verification results
	BatchRoot     []byte // the root hash used for all verifications
	TotalGas      uint64 // total gas consumed for the batch
	VerifiedCount int    // number of items that verified successfully
}

// NIIInclusionItem represents a single inclusion proof item for batch verification.
type NIIInclusionItem struct {
	Key       []byte   // the key (leaf hash or identifier)
	Value     []byte   // the value to verify at this leaf
	ProofPath [][]byte // siblings along the Merkle path
	LeafIndex uint64   // position of the leaf in the tree
}

// proofCacheEntry stores a cached proof verification result.
type proofCacheEntry struct {
	valid bool
}

// NIIEnhancedPrecompile provides batch Merkle inclusion proof verification
// with caching. It is thread-safe.
type NIIEnhancedPrecompile struct {
	mu        sync.RWMutex
	config    NIIEnhancedConfig
	cache     map[string]proofCacheEntry
	cacheKeys []string // insertion-order for LRU eviction
}

// NewNIIEnhancedPrecompile creates a new enhanced NII precompile.
func NewNIIEnhancedPrecompile(config NIIEnhancedConfig) *NIIEnhancedPrecompile {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 256
	}
	if config.MaxProofDepth <= 0 {
		config.MaxProofDepth = 64
	}
	if config.CacheSize <= 0 {
		config.CacheSize = 1024
	}
	return &NIIEnhancedPrecompile{
		config:    config,
		cache:     make(map[string]proofCacheEntry),
		cacheKeys: make([]string, 0),
	}
}

// VerifyInclusion verifies a single Merkle inclusion proof.
// Returns (valid, gasUsed, error).
//
// The verification walks the proof path from leaf to root:
//  1. Compute leaf hash = keccak256(key || value)
//  2. For each sibling in proofPath, combine and hash:
//     - If leafIndex bit is 0: hash(current || sibling)
//     - If leafIndex bit is 1: hash(sibling || current)
//  3. Compare final hash against root.
func (p *NIIEnhancedPrecompile) VerifyInclusion(key, value, root []byte, proofPath [][]byte) (bool, uint64, error) {
	if len(key) == 0 {
		return false, 0, ErrNIIEmptyKey
	}
	if len(root) == 0 {
		return false, 0, ErrNIIEmptyRoot
	}
	if len(proofPath) > p.config.MaxProofDepth {
		return false, 0, ErrNIIProofDepthExceeded
	}

	gas := niiEnhancedBaseGas + uint64(len(proofPath))*niiEnhancedPerStepGas

	// Compute leaf hash.
	leafData := append(key, value...)
	current := crypto.Keccak256(leafData)

	// Walk proof path toward root.
	leafIdx := uint64(0)
	if len(proofPath) > 0 {
		// Derive leaf index from key for deterministic path direction.
		if len(key) >= 8 {
			for i := 0; i < 8; i++ {
				leafIdx |= uint64(key[i]) << uint(8*i)
			}
		} else {
			for i := 0; i < len(key); i++ {
				leafIdx |= uint64(key[i]) << uint(8*i)
			}
		}
	}

	for i, sibling := range proofPath {
		if len(sibling) == 0 {
			return false, gas, ErrNIIInvalidProof
		}
		bit := (leafIdx >> uint(i)) & 1
		if bit == 0 {
			combined := append(current, sibling...)
			current = crypto.Keccak256(combined)
		} else {
			combined := append(sibling, current...)
			current = crypto.Keccak256(combined)
		}
	}

	// Compare computed root with expected root.
	if len(current) != len(root) {
		return false, gas, nil
	}
	valid := true
	for i := range current {
		if current[i] != root[i] {
			valid = false
			break
		}
	}

	return valid, gas, nil
}

// BatchVerify verifies multiple inclusion proofs against the same root.
// Returns a batch proof result with per-item results and aggregate stats.
func (p *NIIEnhancedPrecompile) BatchVerify(items []NIIInclusionItem, root []byte) (*NIIBatchProof, error) {
	if len(items) > p.config.MaxBatchSize {
		return nil, ErrNIIBatchTooLarge
	}
	if len(root) == 0 {
		return nil, ErrNIIEmptyRoot
	}
	if len(items) == 0 {
		return &NIIBatchProof{
			Proofs:        []bool{},
			BatchRoot:     root,
			TotalGas:      0,
			VerifiedCount: 0,
		}, nil
	}

	result := &NIIBatchProof{
		Proofs:    make([]bool, len(items)),
		BatchRoot: make([]byte, len(root)),
	}
	copy(result.BatchRoot, root)

	var totalGas uint64

	for i, item := range items {
		valid, gas, err := p.VerifyInclusion(item.Key, item.Value, root, item.ProofPath)
		if err != nil {
			// On error, mark as invalid and continue (batch does not abort).
			result.Proofs[i] = false
			totalGas += niiEnhancedBaseGas
			continue
		}
		result.Proofs[i] = valid
		if valid {
			result.VerifiedCount++
		}
		totalGas += gas
	}

	// Apply batch discount: 80% of individual cost.
	result.TotalGas = totalGas * niiEnhancedBatchBonus / 100

	return result, nil
}

// EstimateGas estimates the gas cost for a proof verification.
func (p *NIIEnhancedPrecompile) EstimateGas(proofDepth int, batchSize int) uint64 {
	if batchSize <= 0 {
		batchSize = 1
	}
	perItem := niiEnhancedBaseGas + uint64(proofDepth)*niiEnhancedPerStepGas
	total := perItem * uint64(batchSize)
	if batchSize > 1 {
		total = total * niiEnhancedBatchBonus / 100
	}
	return total
}

// CacheProof stores a verification result in the cache.
func (p *NIIEnhancedPrecompile) CacheProof(key []byte, result bool) {
	if len(key) == 0 {
		return
	}
	cacheKey := string(key)

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.cache[cacheKey]; !exists {
		// Evict oldest if at capacity.
		if len(p.cacheKeys) >= p.config.CacheSize {
			oldest := p.cacheKeys[0]
			p.cacheKeys = p.cacheKeys[1:]
			delete(p.cache, oldest)
		}
		p.cacheKeys = append(p.cacheKeys, cacheKey)
	}
	p.cache[cacheKey] = proofCacheEntry{valid: result}
}

// CacheHit checks if a proof result is cached. Returns (found, result).
func (p *NIIEnhancedPrecompile) CacheHit(key []byte) (bool, bool) {
	if len(key) == 0 {
		return false, false
	}
	cacheKey := string(key)

	p.mu.RLock()
	defer p.mu.RUnlock()

	entry, found := p.cache[cacheKey]
	if !found {
		return false, false
	}
	return true, entry.valid
}

// ClearCache removes all cached verification results.
func (p *NIIEnhancedPrecompile) ClearCache() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cache = make(map[string]proofCacheEntry)
	p.cacheKeys = p.cacheKeys[:0]
}

// CacheSize returns the current number of entries in the cache.
func (p *NIIEnhancedPrecompile) CacheSize() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.cache)
}
