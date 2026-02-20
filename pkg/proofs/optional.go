// optional.go implements an optional proof submission system for the eth2028 client.
// This is from EL Throughput: "optional proofs" (before mandatory 3-of-5).
// Validators and provers can voluntarily submit proofs for blocks, building a
// reputation and earning rewards before the mandatory proof requirement activates.
package proofs

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Optional proof system errors.
var (
	ErrOptionalNilSubmission    = errors.New("optional: nil proof submission")
	ErrOptionalEmptyProofData   = errors.New("optional: empty proof data")
	ErrOptionalEmptyProofType   = errors.New("optional: empty proof type")
	ErrOptionalZeroBlockHash    = errors.New("optional: zero block hash")
	ErrOptionalEmptySubmitter   = errors.New("optional: empty submitter address")
	ErrOptionalProofTypeNotAccepted = errors.New("optional: proof type not accepted by policy")
	ErrOptionalDuplicateProof   = errors.New("optional: duplicate proof from same submitter for same block")
)

// OptionalProofSubmission represents a voluntarily submitted proof for a block.
// Unlike ProofSubmission used by the mandatory system, this includes a submitter
// address (instead of a prover hash) and uses wall-clock timestamps.
type OptionalProofSubmission struct {
	BlockHash types.Hash
	ProofType string
	ProofData []byte
	Submitter types.Address
	Timestamp time.Time
}

// OptionalProofPolicy configures which proof types are accepted in the
// optional proof submission system.
type OptionalProofPolicy struct {
	mu            sync.RWMutex
	acceptedTypes map[string]bool
}

// NewOptionalProofPolicy creates a policy that accepts the given proof types.
// If no types are provided, all proof types are accepted.
func NewOptionalProofPolicy(accepted []string) *OptionalProofPolicy {
	p := &OptionalProofPolicy{
		acceptedTypes: make(map[string]bool),
	}
	for _, t := range accepted {
		p.acceptedTypes[t] = true
	}
	return p
}

// IsAccepted returns true if the given proof type is accepted by the policy.
// If no explicit types were configured, all types are accepted.
func (p *OptionalProofPolicy) IsAccepted(proofType string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.acceptedTypes) == 0 {
		return true
	}
	return p.acceptedTypes[proofType]
}

// AddAcceptedType adds a proof type to the policy.
func (p *OptionalProofPolicy) AddAcceptedType(proofType string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acceptedTypes[proofType] = true
}

// AcceptedTypes returns a copy of all accepted proof type names.
func (p *OptionalProofPolicy) AcceptedTypes() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]string, 0, len(p.acceptedTypes))
	for t := range p.acceptedTypes {
		result = append(result, t)
	}
	return result
}

// OptionalProofStore tracks optional proof submissions per block.
// Thread-safe.
type OptionalProofStore struct {
	mu     sync.RWMutex
	policy *OptionalProofPolicy
	// proofs maps blockHash -> list of submissions.
	proofs map[types.Hash][]*OptionalProofSubmission
	// seen tracks submitter+block pairs to prevent duplicates.
	seen map[types.Hash]map[types.Address]bool
}

// NewOptionalProofStore creates a new optional proof store with the given policy.
func NewOptionalProofStore(policy *OptionalProofPolicy) *OptionalProofStore {
	if policy == nil {
		policy = NewOptionalProofPolicy(nil)
	}
	return &OptionalProofStore{
		policy: policy,
		proofs: make(map[types.Hash][]*OptionalProofSubmission),
		seen:   make(map[types.Hash]map[types.Address]bool),
	}
}

// SubmitOptionalProof validates and stores a voluntarily submitted proof.
func (s *OptionalProofStore) SubmitOptionalProof(submission *OptionalProofSubmission) error {
	if submission == nil {
		return ErrOptionalNilSubmission
	}
	if submission.BlockHash == (types.Hash{}) {
		return ErrOptionalZeroBlockHash
	}
	if submission.Submitter == (types.Address{}) {
		return ErrOptionalEmptySubmitter
	}
	if submission.ProofType == "" {
		return ErrOptionalEmptyProofType
	}
	if len(submission.ProofData) == 0 {
		return ErrOptionalEmptyProofData
	}
	if !s.policy.IsAccepted(submission.ProofType) {
		return ErrOptionalProofTypeNotAccepted
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate submission from the same submitter for the same block.
	if submitters, ok := s.seen[submission.BlockHash]; ok {
		if submitters[submission.Submitter] {
			return ErrOptionalDuplicateProof
		}
	}

	// Copy the submission to prevent caller mutation.
	sub := &OptionalProofSubmission{
		BlockHash: submission.BlockHash,
		ProofType: submission.ProofType,
		ProofData: make([]byte, len(submission.ProofData)),
		Submitter: submission.Submitter,
		Timestamp: submission.Timestamp,
	}
	copy(sub.ProofData, submission.ProofData)
	if sub.Timestamp.IsZero() {
		sub.Timestamp = time.Now()
	}

	s.proofs[submission.BlockHash] = append(s.proofs[submission.BlockHash], sub)

	if s.seen[submission.BlockHash] == nil {
		s.seen[submission.BlockHash] = make(map[types.Address]bool)
	}
	s.seen[submission.BlockHash][submission.Submitter] = true

	return nil
}

// GetProofsForBlock returns all optional proof submissions for a given block.
// Returns nil if no proofs exist for the block.
func (s *OptionalProofStore) GetProofsForBlock(blockHash types.Hash) []*OptionalProofSubmission {
	s.mu.RLock()
	defer s.mu.RUnlock()

	submissions := s.proofs[blockHash]
	if len(submissions) == 0 {
		return nil
	}

	// Return a copy to prevent external mutation.
	result := make([]*OptionalProofSubmission, len(submissions))
	copy(result, submissions)
	return result
}

// IsBlockVerified returns true if a block has at least minProofs optional proofs.
func (s *OptionalProofStore) IsBlockVerified(blockHash types.Hash, minProofs int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.proofs[blockHash]) >= minProofs
}

// ProofCount returns the number of optional proofs submitted for a block.
func (s *OptionalProofStore) ProofCount(blockHash types.Hash) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.proofs[blockHash])
}

// ProofRewardCalculator computes rewards for optional proof submitters.
// The first submitter for a block receives a bonus multiplier.
type ProofRewardCalculator struct {
	mu sync.RWMutex
	// BaseReward is the base reward per proof (in Gwei).
	BaseReward uint64
	// FirstSubmitterBonus is the multiplier for the first submitter (e.g. 2 = 2x).
	FirstSubmitterBonus uint64
	// rewardPool tracks total rewards distributed.
	rewardPool uint64
}

// NewProofRewardCalculator creates a new reward calculator with specified base
// reward and first-submitter bonus multiplier.
func NewProofRewardCalculator(baseReward, firstSubmitterBonus uint64) *ProofRewardCalculator {
	if baseReward == 0 {
		baseReward = 100 // 100 Gwei default
	}
	if firstSubmitterBonus == 0 {
		firstSubmitterBonus = 2 // 2x bonus for first submitter
	}
	return &ProofRewardCalculator{
		BaseReward:          baseReward,
		FirstSubmitterBonus: firstSubmitterBonus,
	}
}

// CalculateReward returns the reward in Gwei for a proof submission.
// proofType affects the base reward (e.g. ZK proofs get higher reward).
// isFirstSubmitter grants a bonus multiplier.
func (c *ProofRewardCalculator) CalculateReward(proofType string, isFirstSubmitter bool) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	reward := c.BaseReward

	// Proof type multiplier: ZK proofs are more expensive to produce.
	switch proofType {
	case "ZK-SNARK", "ZK-STARK":
		reward = reward * 3
	case "IPA":
		reward = reward * 2
	case "KZG":
		reward = reward * 2
	default:
		// Base reward for unknown types.
	}

	if isFirstSubmitter {
		reward = reward * c.FirstSubmitterBonus
	}

	c.rewardPool += reward
	return reward
}

// RewardPool returns the total rewards distributed so far in Gwei.
func (c *ProofRewardCalculator) RewardPool() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rewardPool
}
