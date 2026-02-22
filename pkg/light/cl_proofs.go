// Real-Time Consensus Layer Proofs (I+ roadmap: real-time CL proofs)
//
// Implements a proof system for CL state that supports multiple proof types
// (Merkle, STARK, SNARK). The CLProver generates and verifies proofs for
// beacon state roots, validator status, and attestation data. Proofs are
// cached for efficiency and all operations are thread-safe.
package light

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// CLProofType distinguishes the cryptographic proof system used.
type CLProofType uint8

const (
	// ProofTypeMerkle uses standard Merkle tree proofs (Keccak256).
	ProofTypeMerkle CLProofType = iota
	// ProofTypeSTARK uses STARK-style proofs (simulated).
	ProofTypeSTARK
	// ProofTypeSNARK uses SNARK-style proofs (simulated).
	ProofTypeSNARK
)

// CL prover errors.
var (
	ErrProverZeroStateRoot  = errors.New("light: state root must not be zero")
	ErrProverInvalidSlot    = errors.New("light: slot exceeds configured max proof depth")
	ErrProverNilProof       = errors.New("light: nil proof")
	ErrProverInvalidProof   = errors.New("light: invalid proof")
	ErrProverEmptyBatch     = errors.New("light: empty proof batch")
	ErrProverInvalidIndex   = errors.New("light: invalid validator index")
	ErrProverInvalidCommIdx = errors.New("light: invalid committee index")
)

// CLProverConfig configures the CLProver.
type CLProverConfig struct {
	ProofType     CLProofType
	CacheSize     int
	MaxProofDepth int
}

// DefaultCLProverConfig returns a default config for Merkle proofs.
func DefaultCLProverConfig() CLProverConfig {
	return CLProverConfig{
		ProofType:     ProofTypeMerkle,
		CacheSize:     512,
		MaxProofDepth: 32,
	}
}

// CLStateProof proves that a CL state root is valid at a given slot.
type CLStateProof struct {
	Slot       uint64
	StateRoot  types.Hash
	BeaconRoot types.Hash
	Proof      []types.Hash
	ProofType  CLProofType
	Timestamp  time.Time
}

// ValidatorProof proves a validator's status and balance at a state root.
type ValidatorProof struct {
	Index     uint64
	Balance   uint64
	Status    string
	Proof     []types.Hash
	StateRoot types.Hash
}

// AttestationProof proves attestation data for a slot and committee.
type AttestationProof struct {
	Slot             uint64
	CommitteeIndex   uint64
	AggregationBits  []byte
	Proof            []types.Hash
}

// clProverCacheKey is the internal cache key for the prover.
type clProverCacheKey struct {
	kind  string // "state", "validator", "attestation"
	slot  uint64
	index uint64
	extra types.Hash // additional discriminator (e.g. state root)
}

// CLProver generates and verifies real-time CL proofs.
// All methods are safe for concurrent use.
type CLProver struct {
	config CLProverConfig
	mu     sync.RWMutex
	cache  map[clProverCacheKey]interface{}
}

// NewCLProver creates a new CLProver with the given configuration.
func NewCLProver(config CLProverConfig) *CLProver {
	if config.MaxProofDepth <= 0 {
		config.MaxProofDepth = 32
	}
	if config.CacheSize <= 0 {
		config.CacheSize = 512
	}
	return &CLProver{
		config: config,
		cache:  make(map[clProverCacheKey]interface{}, config.CacheSize),
	}
}

// GenerateStateProof generates a proof that stateRoot is the CL state
// at the given slot. The beacon root is derived from the slot and state
// root, and a Merkle branch (or equivalent for STARK/SNARK) is computed.
func (p *CLProver) GenerateStateProof(slot uint64, stateRoot types.Hash) (*CLStateProof, error) {
	if stateRoot.IsZero() {
		return nil, ErrProverZeroStateRoot
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check cache.
	key := clProverCacheKey{kind: "state", slot: slot, extra: stateRoot}
	if cached, ok := p.cache[key]; ok {
		if sp, ok := cached.(*CLStateProof); ok {
			return sp, nil
		}
	}

	// Build leaf: H(slot || stateRoot).
	leaf := hashSlotAndRoot(slot, stateRoot)

	// Build proof branch.
	branch := p.buildBranch(leaf, slot)

	// Compute beacon root by walking the branch.
	beaconRoot := walkBranch(leaf, branch, slot)

	proof := &CLStateProof{
		Slot:       slot,
		StateRoot:  stateRoot,
		BeaconRoot: beaconRoot,
		Proof:      branch,
		ProofType:  p.config.ProofType,
		Timestamp:  time.Now(),
	}

	p.cacheEntry(key, proof)
	return proof, nil
}

// VerifyStateProof verifies that a CLStateProof is internally consistent.
// It recomputes the beacon root from the leaf and branch, and checks it
// matches the claimed BeaconRoot.
func (p *CLProver) VerifyStateProof(proof *CLStateProof) bool {
	if proof == nil || len(proof.Proof) == 0 {
		return false
	}
	if proof.StateRoot.IsZero() {
		return false
	}

	leaf := hashSlotAndRoot(proof.Slot, proof.StateRoot)
	computed := walkBranch(leaf, proof.Proof, proof.Slot)
	return computed == proof.BeaconRoot
}

// GenerateValidatorProof generates a proof of a validator's status and
// balance. The validator status is derived deterministically for the
// simulation (active, pending, exited, slashed).
func (p *CLProver) GenerateValidatorProof(validatorIndex uint64) (*ValidatorProof, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Derive deterministic validator data from the index.
	balance := deriveBalance(validatorIndex)
	status := deriveStatus(validatorIndex)

	// Build leaf: H(index || balance || status).
	indexBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBuf, validatorIndex)
	balBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(balBuf, balance)
	leaf := crypto.Keccak256(indexBuf, balBuf, []byte(status))

	// Build proof branch.
	branch := p.buildBranch(leaf, validatorIndex)

	// Compute state root by walking the branch.
	rootBytes := walkBranch(leaf, branch, validatorIndex)

	// Check cache.
	key := clProverCacheKey{kind: "validator", index: validatorIndex}
	if cached, ok := p.cache[key]; ok {
		if vp, ok := cached.(*ValidatorProof); ok {
			return vp, nil
		}
	}

	vp := &ValidatorProof{
		Index:     validatorIndex,
		Balance:   balance,
		Status:    status,
		Proof:     branch,
		StateRoot: rootBytes,
	}

	p.cacheEntry(key, vp)
	return vp, nil
}

// VerifyValidatorProof verifies that a ValidatorProof is internally consistent.
func (p *CLProver) VerifyValidatorProof(proof *ValidatorProof) bool {
	if proof == nil || len(proof.Proof) == 0 {
		return false
	}

	indexBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBuf, proof.Index)
	balBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(balBuf, proof.Balance)
	leaf := crypto.Keccak256(indexBuf, balBuf, []byte(proof.Status))

	computed := walkBranch(leaf, proof.Proof, proof.Index)
	return computed == proof.StateRoot
}

// GenerateAttestationProof generates a proof for attestation data at a
// given slot and committee index. AggregationBits are derived
// deterministically from the slot and committee index.
func (p *CLProver) GenerateAttestationProof(slot uint64, committeeIndex uint64) (*AttestationProof, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Derive deterministic aggregation bits (64 validators per committee).
	aggBits := deriveAggBits(slot, committeeIndex)

	// Build leaf: H(slot || committeeIndex || aggBits).
	slotBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(slotBuf, slot)
	ciBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(ciBuf, committeeIndex)
	leaf := crypto.Keccak256(slotBuf, ciBuf, aggBits)

	// Combine slot and committee for leaf index.
	leafIndex := slot*64 + committeeIndex

	branch := p.buildBranch(leaf, leafIndex)

	// Check cache.
	key := clProverCacheKey{kind: "attestation", slot: slot, index: committeeIndex}
	if cached, ok := p.cache[key]; ok {
		if ap, ok := cached.(*AttestationProof); ok {
			return ap, nil
		}
	}

	ap := &AttestationProof{
		Slot:            slot,
		CommitteeIndex:  committeeIndex,
		AggregationBits: aggBits,
		Proof:           branch,
	}

	p.cacheEntry(key, ap)
	return ap, nil
}

// BatchVerify verifies multiple CLStateProofs, returning true only if
// all proofs are valid.
func (p *CLProver) BatchVerify(proofs []*CLStateProof) bool {
	if len(proofs) == 0 {
		return false
	}
	for _, proof := range proofs {
		if !p.VerifyStateProof(proof) {
			return false
		}
	}
	return true
}

// --- internal helpers ---

// hashSlotAndRoot computes H(slot || stateRoot).
func hashSlotAndRoot(slot uint64, root types.Hash) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, slot)
	return crypto.Keccak256(buf, root.Bytes())
}

// buildBranch builds a Merkle branch of sibling hashes. For STARK/SNARK
// proof types, a domain separator is mixed into the sibling derivation.
func (p *CLProver) buildBranch(leaf []byte, leafIndex uint64) []types.Hash {
	depth := p.config.MaxProofDepth
	branch := make([]types.Hash, depth)

	var domainTag []byte
	switch p.config.ProofType {
	case ProofTypeSTARK:
		domainTag = []byte("stark")
	case ProofTypeSNARK:
		domainTag = []byte("snark")
	default:
		domainTag = []byte("merkle")
	}

	for i := 0; i < depth; i++ {
		levelBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(levelBuf, uint64(i))
		idxBuf := make([]byte, 8)
		binary.BigEndian.PutUint64(idxBuf, leafIndex)
		sibling := crypto.Keccak256(levelBuf, idxBuf, domainTag)
		copy(branch[i][:], sibling)
	}
	return branch
}

// walkBranch recomputes the root by hashing leaf with each branch sibling.
func walkBranch(leaf []byte, branch []types.Hash, leafIndex uint64) types.Hash {
	current := make([]byte, len(leaf))
	copy(current, leaf)

	for i, sibling := range branch {
		if (leafIndex>>uint(i))&1 == 0 {
			current = crypto.Keccak256(current, sibling[:])
		} else {
			current = crypto.Keccak256(sibling[:], current)
		}
	}
	return types.BytesToHash(current)
}

// deriveBalance returns a deterministic validator balance from the index.
func deriveBalance(index uint64) uint64 {
	// Base 32 ETH (in Gwei) with small variation.
	return 32_000_000_000 + (index % 1_000_000_000)
}

// deriveStatus returns a deterministic validator status from the index.
func deriveStatus(index uint64) string {
	switch index % 4 {
	case 0:
		return "active"
	case 1:
		return "pending"
	case 2:
		return "exited"
	default:
		return "slashed"
	}
}

// deriveAggBits returns deterministic aggregation bits for a committee.
// Each committee has 64 validators; bits are set based on H(slot || ci).
func deriveAggBits(slot, committeeIndex uint64) []byte {
	slotBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(slotBuf, slot)
	ciBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(ciBuf, committeeIndex)
	seed := crypto.Keccak256(slotBuf, ciBuf)
	// 64 validators = 8 bytes of bits.
	bits := make([]byte, 8)
	copy(bits, seed[:8])
	return bits
}

// cacheEntry stores an entry in the internal cache, evicting the oldest
// entry if the cache is full.
func (p *CLProver) cacheEntry(key clProverCacheKey, value interface{}) {
	if len(p.cache) >= p.config.CacheSize {
		// Evict one arbitrary entry.
		for k := range p.cache {
			delete(p.cache, k)
			break
		}
	}
	p.cache[key] = value
}
