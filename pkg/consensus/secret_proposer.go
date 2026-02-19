package consensus

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Secret proposer selection errors.
var (
	ErrSPNoCommitment   = errors.New("secret proposer: no commitment for slot")
	ErrSPWrongSecret    = errors.New("secret proposer: secret does not match commitment")
	ErrSPAlreadyRevealed = errors.New("secret proposer: already revealed")
	ErrSPZeroValidators = errors.New("secret proposer: zero validators")
)

// SecretProposerConfig configures the secret proposer selection mechanism.
type SecretProposerConfig struct {
	// LookaheadSlots is how far ahead proposers commit (default 32).
	LookaheadSlots uint64

	// CommitmentPeriod is the number of slots a commitment is valid (default 2).
	CommitmentPeriod uint64

	// RevealPeriod is the number of slots before the target slot that reveal is allowed (default 1).
	RevealPeriod uint64
}

// DefaultSecretProposerConfig returns the default configuration.
func DefaultSecretProposerConfig() *SecretProposerConfig {
	return &SecretProposerConfig{
		LookaheadSlots:   32,
		CommitmentPeriod: 2,
		RevealPeriod:     1,
	}
}

// ProposerCommitment represents a validator's commitment to propose a block
// at a specific slot. The commitment hash conceals the proposer identity
// until the reveal phase.
type ProposerCommitment struct {
	ValidatorIndex uint64
	Slot           uint64
	CommitHash     types.Hash
	RevealedAt     uint64
	Secret         []byte
}

// SecretProposerSelector manages secret proposer selection for MEV protection.
// Validators commit to proposer selection secrets in advance, then reveal
// them to claim their slot, preventing MEV-based proposer manipulation.
type SecretProposerSelector struct {
	config      *SecretProposerConfig
	seed        types.Hash
	commitments map[uint64]*ProposerCommitment // slot -> commitment
	mu          sync.RWMutex
}

// NewSecretProposerSelector creates a new selector with the given config and seed.
func NewSecretProposerSelector(config *SecretProposerConfig, seed types.Hash) *SecretProposerSelector {
	if config == nil {
		config = DefaultSecretProposerConfig()
	}
	return &SecretProposerSelector{
		config:      config,
		seed:        seed,
		commitments: make(map[uint64]*ProposerCommitment),
	}
}

// CommitProposer creates a commitment for proposer selection at the given slot.
// CommitHash = Keccak256(validatorIndex || slot || secret)
func (s *SecretProposerSelector) CommitProposer(validatorIndex uint64, slot uint64, secret []byte) (*ProposerCommitment, error) {
	commitHash := computeCommitHash(validatorIndex, slot, secret)

	commitment := &ProposerCommitment{
		ValidatorIndex: validatorIndex,
		Slot:           slot,
		CommitHash:     commitHash,
	}

	s.mu.Lock()
	s.commitments[slot] = commitment
	s.mu.Unlock()

	return commitment, nil
}

// RevealProposer reveals the proposer for a slot by providing the secret.
// Verifies the commitment exists and the secret matches, then returns
// the validator index.
func (s *SecretProposerSelector) RevealProposer(slot uint64, secret []byte) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	commitment, ok := s.commitments[slot]
	if !ok {
		return 0, ErrSPNoCommitment
	}

	// Recompute the commit hash and compare.
	expected := computeCommitHash(commitment.ValidatorIndex, slot, secret)
	if expected != commitment.CommitHash {
		return 0, ErrSPWrongSecret
	}

	commitment.Secret = make([]byte, len(secret))
	copy(commitment.Secret, secret)
	commitment.RevealedAt = slot

	return commitment.ValidatorIndex, nil
}

// IsCommitted returns whether a commitment exists for the given slot.
func (s *SecretProposerSelector) IsCommitted(slot uint64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.commitments[slot]
	return ok
}

// GetCommitment returns the commitment for the given slot, or nil if none.
func (s *SecretProposerSelector) GetCommitment(slot uint64) *ProposerCommitment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.commitments[slot]
}

// DetermineProposer returns a deterministic proposer index using a fallback
// mechanism when no commitment exists. The result is computed as:
// hash(slot || randaoMix) mod validatorCount
func DetermineProposer(slot uint64, validatorCount int, randaoMix types.Hash) uint64 {
	if validatorCount <= 0 {
		return 0
	}

	buf := make([]byte, 8+types.HashLength)
	binary.BigEndian.PutUint64(buf[:8], slot)
	copy(buf[8:], randaoMix[:])

	h := crypto.Keccak256Hash(buf)

	// Use the first 8 bytes of the hash as a uint64, then mod by validator count.
	idx := binary.BigEndian.Uint64(h[:8])
	return idx % uint64(validatorCount)
}

// computeCommitHash computes Keccak256(validatorIndex || slot || secret).
func computeCommitHash(validatorIndex uint64, slot uint64, secret []byte) types.Hash {
	buf := make([]byte, 8+8+len(secret))
	binary.BigEndian.PutUint64(buf[:8], validatorIndex)
	binary.BigEndian.PutUint64(buf[8:16], slot)
	copy(buf[16:], secret)
	return crypto.Keccak256Hash(buf)
}
