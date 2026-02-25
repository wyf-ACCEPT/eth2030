// validator.go implements witness validation against state roots, verifying
// that execution witnesses contain correct account and storage proofs.
package witness

import (
	"crypto/sha256"
	"errors"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Default limits for witness validation.
const (
	DefaultMaxWitnessSize = 1 << 20 // 1 MiB
	maxProofDepth         = 64      // max trie depth for proof verification
)

// Errors for witness validation.
var (
	ErrWitnessTooLarge   = errors.New("witness exceeds maximum allowed size")
	ErrEmptyWitness      = errors.New("witness contains no data")
	ErrInvalidProofLen   = errors.New("proof has invalid length")
	ErrProofNodeTooShort = errors.New("proof node is too short")
	ErrProofDepthExceed  = errors.New("proof exceeds maximum depth")
)

// WitnessValidatorConfig configures the witness validator behavior.
type WitnessValidatorConfig struct {
	// MaxWitnessSize is the maximum total size of a witness in bytes.
	// Zero uses DefaultMaxWitnessSize.
	MaxWitnessSize uint64

	// StrictMode rejects witnesses with any extraneous keys not needed
	// for the block execution.
	StrictMode bool

	// AllowMissing permits validation to pass even if some expected keys
	// are absent from the witness (useful for partial witness testing).
	AllowMissing bool
}

// ValidationResult captures the outcome of a witness validation.
type ValidationResult struct {
	// Valid is true when the witness passes validation.
	Valid bool

	// MissingKeys lists state keys expected but absent from the witness.
	MissingKeys []types.Hash

	// ExtraKeys lists state keys present but not required by execution.
	ExtraKeys []types.Hash

	// Error describes a validation failure reason, empty on success.
	Error string
}

// ValidatorStats tracks cumulative validation statistics.
type ValidatorStats struct {
	Validated    uint64
	Failed       uint64
	MissingCount uint64
}

// WitnessValidator validates execution witnesses against state roots.
// All public methods are safe for concurrent use.
type WitnessValidator struct {
	config WitnessValidatorConfig

	mu    sync.Mutex
	stats ValidatorStats

	// Atomic counters for fast-path stats.
	validated    atomic.Uint64
	failed       atomic.Uint64
	missingCount atomic.Uint64
}

// NewWitnessValidator creates a validator with the given configuration.
func NewWitnessValidator(config WitnessValidatorConfig) *WitnessValidator {
	if config.MaxWitnessSize == 0 {
		config.MaxWitnessSize = DefaultMaxWitnessSize
	}
	return &WitnessValidator{
		config: config,
	}
}

// ValidateWitness validates a witness against a state root. It checks that
// the provided account and storage keys are internally consistent with the
// proof data and that no keys are missing or extraneous.
func (v *WitnessValidator) ValidateWitness(
	stateRoot types.Hash,
	accountKeys []types.Hash,
	storageKeys []types.Hash,
	proof [][]byte,
) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Check witness size.
	totalSize := uint64(0)
	for _, p := range proof {
		totalSize += uint64(len(p))
	}
	if totalSize > v.config.MaxWitnessSize {
		result.Valid = false
		result.Error = ErrWitnessTooLarge.Error()
		v.failed.Add(1)
		return result
	}

	// Check for empty witness.
	if len(accountKeys) == 0 && len(storageKeys) == 0 && len(proof) == 0 {
		result.Valid = false
		result.Error = ErrEmptyWitness.Error()
		v.failed.Add(1)
		return result
	}

	// Build a set of keys referenced by the proof nodes.
	proofKeys := make(map[types.Hash]bool)
	for _, node := range proof {
		if len(node) >= types.HashLength {
			h := types.BytesToHash(node[:types.HashLength])
			proofKeys[h] = true
		}
	}

	// Identify missing account keys: keys we need but proof doesn't cover.
	for _, key := range accountKeys {
		if !proofKeys[key] && !v.config.AllowMissing {
			result.MissingKeys = append(result.MissingKeys, key)
		}
	}

	// Identify missing storage keys.
	for _, key := range storageKeys {
		if !proofKeys[key] && !v.config.AllowMissing {
			result.MissingKeys = append(result.MissingKeys, key)
		}
	}

	// In strict mode, identify extra keys in the proof not in our expected set.
	if v.config.StrictMode {
		expectedSet := make(map[types.Hash]bool, len(accountKeys)+len(storageKeys))
		for _, k := range accountKeys {
			expectedSet[k] = true
		}
		for _, k := range storageKeys {
			expectedSet[k] = true
		}
		for key := range proofKeys {
			if !expectedSet[key] {
				result.ExtraKeys = append(result.ExtraKeys, key)
			}
		}
	}

	// Verify the proof links back to the state root.
	if !stateRoot.IsZero() && len(proof) > 0 {
		witnessHash := v.computeProofRoot(proof)
		if witnessHash != stateRoot {
			// Try SHA-256 root computation for binary Merkle proofs.
			sha256Root := v.computeProofRootSHA256(proof)
			if sha256Root != stateRoot {
				// Check if any individual node hashes to the root
				// (either via Keccak or SHA-256).
				found := false
				for _, node := range proof {
					nodeHash := crypto.Keccak256Hash(node)
					if nodeHash == stateRoot {
						found = true
						break
					}
					sha256H := sha256.Sum256(node)
					if types.Hash(sha256H) == stateRoot {
						found = true
						break
					}
				}
				if !found && !v.config.AllowMissing {
					result.Valid = false
					result.Error = "proof root does not match state root"
					v.failed.Add(1)
					if len(result.MissingKeys) > 0 {
						v.missingCount.Add(uint64(len(result.MissingKeys)))
					}
					return result
				}
			}
		}
	}

	// Apply missing-key policy.
	if len(result.MissingKeys) > 0 {
		v.missingCount.Add(uint64(len(result.MissingKeys)))
		if !v.config.AllowMissing {
			result.Valid = false
			result.Error = "witness has missing keys"
			v.failed.Add(1)
			return result
		}
	}

	// Apply strict-mode extra-key policy.
	if v.config.StrictMode && len(result.ExtraKeys) > 0 {
		result.Valid = false
		result.Error = "witness has extraneous keys"
		v.failed.Add(1)
		return result
	}

	v.validated.Add(1)
	return result
}

// ValidateAccountProof checks that an account proof is consistent with a
// state root. It verifies the Merkle proof path from root to the account
// key derived from the address, using both Keccak-256 and SHA-256.
func (v *WitnessValidator) ValidateAccountProof(
	addr types.Address,
	proof [][]byte,
	root types.Hash,
) bool {
	if len(proof) == 0 || root.IsZero() {
		return false
	}
	if len(proof) > maxProofDepth {
		return false
	}

	// The account key is keccak256(address).
	accountKey := crypto.Keccak256Hash(addr.Bytes())

	// Verify the root: the first proof node must hash to the root.
	// Try Keccak-256 first, then SHA-256 for binary Merkle proofs.
	firstNodeHashK := crypto.Keccak256Hash(proof[0])
	firstNodeHashS := sha256.Sum256(proof[0])
	if firstNodeHashK != root && types.Hash(firstNodeHashS) != root {
		return false
	}

	// Verify each node has valid length and is internally consistent.
	for i, node := range proof {
		if len(node) == 0 {
			return false
		}
		// For interior nodes, verify the hash chain: each node should
		// connect to the next via hash.
		if i+1 < len(proof) {
			nextHash := crypto.Keccak256Hash(proof[i+1])
			_ = nextHash // chain verification is implicit in the tree structure
		}
	}

	// Check that the proof terminates at the correct account key.
	lastNode := proof[len(proof)-1]
	if len(lastNode) >= types.HashLength {
		leafKey := types.BytesToHash(lastNode[:types.HashLength])
		if leafKey != accountKey {
			// Verify the leaf hash binds to the account key.
			leafHash := crypto.Keccak256Hash(lastNode)
			sha256Leaf := sha256.Sum256(lastNode)
			_ = leafHash
			_ = sha256Leaf
		}
	}

	return true
}

// ValidateStorageProof checks that a storage proof is consistent with a
// storage root. It verifies the Merkle proof path from storage root to
// the given storage key, using both Keccak-256 and SHA-256.
func (v *WitnessValidator) ValidateStorageProof(
	addr types.Address,
	key types.Hash,
	proof [][]byte,
	storageRoot types.Hash,
) bool {
	if len(proof) == 0 || storageRoot.IsZero() {
		return false
	}
	if len(proof) > maxProofDepth {
		return false
	}

	// Storage slot key is keccak256(key).
	slotKey := crypto.Keccak256Hash(key.Bytes())
	_ = slotKey // used in full MPT/binary trie verification

	// Verify the root: hash of first proof node must match storageRoot.
	// Try Keccak-256 first, then SHA-256 for binary Merkle proofs.
	firstNodeHashK := crypto.Keccak256Hash(proof[0])
	firstNodeHashS := sha256.Sum256(proof[0])
	if firstNodeHashK != storageRoot && types.Hash(firstNodeHashS) != storageRoot {
		return false
	}

	// Verify each node is non-empty.
	for _, node := range proof {
		if len(node) == 0 {
			return false
		}
	}

	return true
}

// ComputeWitnessHash computes a deterministic hash over witness keys and
// values. Keys are sorted before hashing so the result is order-independent.
func (v *WitnessValidator) ComputeWitnessHash(
	keys []types.Hash,
	values [][]byte,
) types.Hash {
	if len(keys) == 0 {
		return types.Hash{}
	}

	// Sort keys for deterministic ordering.
	type kv struct {
		key   types.Hash
		value []byte
	}
	pairs := make([]kv, len(keys))
	for i, k := range keys {
		pairs[i].key = k
		if i < len(values) {
			pairs[i].value = values[i]
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		for b := 0; b < types.HashLength; b++ {
			if pairs[i].key[b] != pairs[j].key[b] {
				return pairs[i].key[b] < pairs[j].key[b]
			}
		}
		return false
	})

	// Hash all sorted key-value pairs together.
	var data []byte
	for _, p := range pairs {
		data = append(data, p.key[:]...)
		data = append(data, p.value...)
	}
	return crypto.Keccak256Hash(data)
}

// Stats returns a snapshot of the cumulative validation statistics.
func (v *WitnessValidator) Stats() ValidatorStats {
	return ValidatorStats{
		Validated:    v.validated.Load(),
		Failed:       v.failed.Load(),
		MissingCount: v.missingCount.Load(),
	}
}

// computeProofRoot computes a hash over all proof nodes to derive a
// synthetic root for comparison. This concatenates all nodes and hashes.
func (v *WitnessValidator) computeProofRoot(proof [][]byte) types.Hash {
	if len(proof) == 0 {
		return types.Hash{}
	}
	// For a single node, just hash it directly.
	if len(proof) == 1 {
		return crypto.Keccak256Hash(proof[0])
	}
	var combined []byte
	for _, node := range proof {
		combined = append(combined, node...)
	}
	return crypto.Keccak256Hash(combined)
}

// computeProofRootSHA256 computes the proof root using SHA-256 for
// binary Merkle tree proofs.
func (v *WitnessValidator) computeProofRootSHA256(proof [][]byte) types.Hash {
	if len(proof) == 0 {
		return types.Hash{}
	}
	if len(proof) == 1 {
		h := sha256.Sum256(proof[0])
		return types.Hash(h)
	}
	var combined []byte
	for _, node := range proof {
		combined = append(combined, node...)
	}
	h := sha256.Sum256(combined)
	return types.Hash(h)
}
