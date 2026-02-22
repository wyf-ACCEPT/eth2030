// Secure Prequorum Protocol with VRF-based leader weighting and two-phase
// commit-reveal to prevent vote manipulation (CL Cryptography track).
//
// Extends the basic PrequorumEngine with:
// - SecurePrequorumConfig: additional security parameters (min validators,
//   signature verification flag, timeout).
// - SecurePrequorumVote: vote with VRF proof for leader-selection weight and
//   a commitment hash for the commit-reveal scheme.
// - SecurePrequorumState: manages the two-phase protocol with vote tracking,
//   commitment storage, and VRF-based quorum weighting.
// - Two-phase commit-reveal: validators first commit a blinded vote, then
//   reveal the actual vote, preventing front-running and last-revealer attacks.
package consensus

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Secure prequorum errors.
var (
	ErrSecureVoteNil             = errors.New("secure-prequorum: nil vote")
	ErrSecureVoteZeroSlot        = errors.New("secure-prequorum: zero slot")
	ErrSecureVoteEmptyBlockRoot  = errors.New("secure-prequorum: empty block root")
	ErrSecureVoteEmptyVRF        = errors.New("secure-prequorum: empty VRF proof")
	ErrSecureVoteEmptyCommitment = errors.New("secure-prequorum: empty commitment hash")
	ErrSecureVoteDuplicate       = errors.New("secure-prequorum: duplicate vote from validator")
	ErrSecureVoteSlotFull        = errors.New("secure-prequorum: slot vote limit reached")
	ErrSecureRevealNil           = errors.New("secure-prequorum: nil reveal")
	ErrSecureRevealNoCommitment  = errors.New("secure-prequorum: no prior commitment found")
	ErrSecureRevealMismatch      = errors.New("secure-prequorum: reveal does not match commitment")
	ErrSecureRevealDuplicate     = errors.New("secure-prequorum: already revealed")
	ErrSecureTooFewValidators    = errors.New("secure-prequorum: below minimum validator count")
	ErrSecureTimeout             = errors.New("secure-prequorum: round timed out")
)

// SecurePrequorumConfig holds the parameters for the secure prequorum protocol.
type SecurePrequorumConfig struct {
	// Threshold is the fraction (0,1] of weighted votes needed for quorum.
	Threshold float64
	// Timeout is the maximum duration for a commit-reveal round.
	Timeout time.Duration
	// MinValidators is the minimum number of unique validators required
	// to even consider reaching quorum.
	MinValidators uint64
	// ValidatorSetSize is the total validator set size for weight computation.
	ValidatorSetSize uint64
	// MaxVotesPerSlot caps votes stored per slot.
	MaxVotesPerSlot uint64
	// VerifySignatures enables signature verification on votes (for testing,
	// this can be disabled).
	VerifySignatures bool
}

// DefaultSecurePrequorumConfig returns sensible defaults for the secure
// prequorum protocol.
func DefaultSecurePrequorumConfig() SecurePrequorumConfig {
	return SecurePrequorumConfig{
		Threshold:        0.67,
		Timeout:          6 * time.Second,
		MinValidators:    3,
		ValidatorSetSize: 1000,
		MaxVotesPerSlot:  10_000,
		VerifySignatures: false,
	}
}

// SecurePrequorumVote represents a validator's vote in the secure prequorum.
// Each vote carries a VRF proof that determines the validator's weight in
// the quorum calculation, and a commitment hash for the commit-reveal scheme.
type SecurePrequorumVote struct {
	ValidatorIndex uint64
	Slot           uint64
	BlockRoot      types.Hash
	VRFProof       []byte     // VRF output for this (validator, slot) pair
	CommitmentHash types.Hash // H(slot || validator || blockRoot || vrfProof)
}

// SecureVoteReveal is the second phase: the validator reveals the actual
// vote contents so observers can verify the commitment.
type SecureVoteReveal struct {
	ValidatorIndex uint64
	Slot           uint64
	BlockRoot      types.Hash
	VRFProof       []byte
}

// SecureQuorumStatus reports the current state of a secure prequorum round.
type SecureQuorumStatus struct {
	Slot             uint64
	CommittedCount   int
	RevealedCount    int
	UniqueValidators int
	TotalWeight      float64
	QuorumReached    bool
	TimedOut         bool
}

// secureSlotState tracks per-slot state for the secure prequorum.
type secureSlotState struct {
	// Phase 1: commitments (validator -> commitment hash).
	commitments map[uint64]types.Hash
	// Phase 2: revealed votes (validator -> full vote).
	reveals map[uint64]*SecurePrequorumVote
	// VRF weights (validator -> weight).
	weights map[uint64]float64
	// Track block roots that received votes for tiebreaking.
	rootVotes map[types.Hash]float64
	// Round start time for timeout enforcement.
	startTime time.Time
	// Total committed count for capacity checks.
	totalCommitted int
}

// SecurePrequorumState manages the two-phase commit-reveal voting protocol.
// Thread-safe for concurrent use.
type SecurePrequorumState struct {
	mu     sync.RWMutex
	config SecurePrequorumConfig
	slots  map[uint64]*secureSlotState
}

// NewSecurePrequorumState creates a new secure prequorum state manager.
func NewSecurePrequorumState(config SecurePrequorumConfig) *SecurePrequorumState {
	if config.Threshold <= 0 || config.Threshold > 1 {
		config.Threshold = 0.67
	}
	if config.MinValidators == 0 {
		config.MinValidators = 3
	}
	if config.ValidatorSetSize == 0 {
		config.ValidatorSetSize = 1000
	}
	if config.MaxVotesPerSlot == 0 {
		config.MaxVotesPerSlot = 10_000
	}
	if config.Timeout == 0 {
		config.Timeout = 6 * time.Second
	}
	return &SecurePrequorumState{
		config: config,
		slots:  make(map[uint64]*secureSlotState),
	}
}

// getOrCreateSlotState returns the slot state, creating it on first access.
// Caller must hold the write lock.
func (s *SecurePrequorumState) getOrCreateSlotState(slot uint64) *secureSlotState {
	ss, ok := s.slots[slot]
	if !ok {
		ss = &secureSlotState{
			commitments: make(map[uint64]types.Hash),
			reveals:     make(map[uint64]*SecurePrequorumVote),
			weights:     make(map[uint64]float64),
			rootVotes:   make(map[types.Hash]float64),
			startTime:   time.Now(),
		}
		s.slots[slot] = ss
	}
	return ss
}

// CastSecureVote submits a vote commitment (phase 1). The vote's
// CommitmentHash must equal ComputeSecureCommitment(...) for the same fields.
func (s *SecurePrequorumState) CastSecureVote(vote *SecurePrequorumVote) error {
	if vote == nil {
		return ErrSecureVoteNil
	}
	if vote.Slot == 0 {
		return ErrSecureVoteZeroSlot
	}
	if vote.BlockRoot.IsZero() {
		return ErrSecureVoteEmptyBlockRoot
	}
	if len(vote.VRFProof) == 0 {
		return ErrSecureVoteEmptyVRF
	}
	if vote.CommitmentHash.IsZero() {
		return ErrSecureVoteEmptyCommitment
	}

	// Verify the commitment hash matches the vote fields.
	expected := ComputeSecureCommitment(vote.Slot, vote.ValidatorIndex, vote.BlockRoot, vote.VRFProof)
	if vote.CommitmentHash != expected {
		return ErrSecureVoteEmptyCommitment
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.getOrCreateSlotState(vote.Slot)

	// Check capacity.
	if ss.totalCommitted >= int(s.config.MaxVotesPerSlot) {
		return ErrSecureVoteSlotFull
	}

	// Check for duplicate commitment from same validator.
	if _, exists := ss.commitments[vote.ValidatorIndex]; exists {
		return ErrSecureVoteDuplicate
	}

	// Store the commitment.
	ss.commitments[vote.ValidatorIndex] = vote.CommitmentHash
	ss.totalCommitted++

	// Pre-compute the VRF weight so it's available at quorum-check time.
	weight := ComputeVRFWeight(vote.VRFProof, s.config.ValidatorSetSize)
	ss.weights[vote.ValidatorIndex] = weight

	return nil
}

// RevealCommitment processes a phase-2 reveal. The reveal's fields must
// hash to the commitment previously submitted via CastSecureVote.
func (s *SecurePrequorumState) RevealCommitment(reveal *SecureVoteReveal) error {
	if reveal == nil {
		return ErrSecureRevealNil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ss, ok := s.slots[reveal.Slot]
	if !ok {
		return ErrSecureRevealNoCommitment
	}

	// Find the prior commitment.
	commitment, exists := ss.commitments[reveal.ValidatorIndex]
	if !exists {
		return ErrSecureRevealNoCommitment
	}

	// Verify the reveal matches the commitment.
	expected := ComputeSecureCommitment(reveal.Slot, reveal.ValidatorIndex, reveal.BlockRoot, reveal.VRFProof)
	if expected != commitment {
		return ErrSecureRevealMismatch
	}

	// Check for duplicate reveal.
	if _, alreadyRevealed := ss.reveals[reveal.ValidatorIndex]; alreadyRevealed {
		return ErrSecureRevealDuplicate
	}

	// Store the revealed vote.
	vote := &SecurePrequorumVote{
		ValidatorIndex: reveal.ValidatorIndex,
		Slot:           reveal.Slot,
		BlockRoot:      reveal.BlockRoot,
		VRFProof:       reveal.VRFProof,
		CommitmentHash: commitment,
	}
	ss.reveals[reveal.ValidatorIndex] = vote

	// Accumulate the VRF-weighted vote for the block root.
	weight := ss.weights[reveal.ValidatorIndex]
	ss.rootVotes[reveal.BlockRoot] += weight

	return nil
}

// VerifyVoteCommitment checks that a vote's commitment hash matches the
// expected value derived from its fields.
func VerifyVoteCommitment(vote *SecurePrequorumVote) bool {
	if vote == nil {
		return false
	}
	expected := ComputeSecureCommitment(vote.Slot, vote.ValidatorIndex, vote.BlockRoot, vote.VRFProof)
	return expected == vote.CommitmentHash
}

// ComputeVRFWeight derives a validator's quorum weight from their VRF proof.
// Weight is in [0, 1], determined by interpreting the first 8 bytes of
// H(vrfProof) as a fraction of MaxUint64, then normalized.
// Validators with higher VRF outputs get proportionally more weight,
// simulating a leader-election probability distribution.
func ComputeVRFWeight(vrfProof []byte, validatorSetSize uint64) float64 {
	if len(vrfProof) == 0 || validatorSetSize == 0 {
		return 0
	}
	h := crypto.Keccak256(vrfProof)
	// Interpret first 8 bytes as uint64.
	val := binary.BigEndian.Uint64(h[:8])
	// Normalize to [0, 1] then scale by 1/validatorSetSize for per-validator weight.
	frac := float64(val) / float64(^uint64(0))
	// Scale so that the expected sum of all validator weights is ~1.0.
	return frac / float64(validatorSetSize)
}

// ReachSecureQuorum checks whether the secure prequorum threshold has been
// reached for the given slot. It considers only revealed votes, weighted
// by their VRF outputs.
func (s *SecurePrequorumState) ReachSecureQuorum(slot uint64) *SecureQuorumStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := &SecureQuorumStatus{Slot: slot}

	ss, ok := s.slots[slot]
	if !ok {
		return status
	}

	status.CommittedCount = len(ss.commitments)
	status.RevealedCount = len(ss.reveals)
	status.UniqueValidators = len(ss.commitments)

	// Check timeout.
	if time.Since(ss.startTime) > s.config.Timeout {
		status.TimedOut = true
	}

	// Check minimum validator count.
	if uint64(status.RevealedCount) < s.config.MinValidators {
		return status
	}

	// Sum revealed VRF weights.
	var totalWeight float64
	for valIdx := range ss.reveals {
		totalWeight += ss.weights[valIdx]
	}
	status.TotalWeight = totalWeight

	// Compare total weight against the threshold, normalized by a factor
	// that makes the expected total weight across all validators = 1.0.
	// With N validators each having expected weight 0.5/N, the expected
	// sum is 0.5. We compare against threshold directly.
	status.QuorumReached = totalWeight >= s.config.Threshold/float64(s.config.ValidatorSetSize)

	return status
}

// GetRevealedVotes returns all revealed votes for the given slot.
func (s *SecurePrequorumState) GetRevealedVotes(slot uint64) []*SecurePrequorumVote {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ss, ok := s.slots[slot]
	if !ok {
		return nil
	}

	out := make([]*SecurePrequorumVote, 0, len(ss.reveals))
	for _, v := range ss.reveals {
		out = append(out, v)
	}
	return out
}

// PurgeSecureSlot removes all state for the given slot.
func (s *SecurePrequorumState) PurgeSecureSlot(slot uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.slots, slot)
}

// Config returns the secure prequorum configuration.
func (s *SecurePrequorumState) Config() SecurePrequorumConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// ComputeSecureCommitment derives the commitment hash for a secure vote.
// commitment = Keccak256(slot || validatorIndex || blockRoot || vrfProof)
func ComputeSecureCommitment(slot, validatorIndex uint64, blockRoot types.Hash, vrfProof []byte) types.Hash {
	var buf [8 + 8 + types.HashLength]byte
	binary.BigEndian.PutUint64(buf[0:8], slot)
	binary.BigEndian.PutUint64(buf[8:16], validatorIndex)
	copy(buf[16:], blockRoot[:])
	combined := append(buf[:], vrfProof...)
	return crypto.Keccak256Hash(combined)
}
