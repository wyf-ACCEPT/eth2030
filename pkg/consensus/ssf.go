package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Single-Slot Finality (SSF) enables blocks to be finalized within a single
// slot instead of the traditional 2-epoch (12.8 minute) Casper FFG finality.
// Validators cast SSF votes during the slot, and once 2/3+ of the total stake
// has voted for the same block root, the slot is marked finalized.

// SSF errors.
var (
	ErrSSFDuplicateVote   = errors.New("ssf: validator already voted for this slot")
	ErrSSFSlotAlreadyFinal = errors.New("ssf: slot is already finalized")
	ErrSSFSlotMismatch    = errors.New("ssf: vote slot does not match target slot")
	ErrSSFZeroStake       = errors.New("ssf: total stake is zero")
	ErrSSFInvalidVoter    = errors.New("ssf: voter index exceeds voter limit")
)

// SSFConfig holds parameters for single-slot finality.
type SSFConfig struct {
	// SlotDuration is the duration of a slot in seconds.
	SlotDuration uint64

	// VoterLimit is the maximum number of validators that can vote per slot.
	// This bounds the committee size for scalability.
	VoterLimit uint64

	// FinalityThreshold is the fraction of total stake required for finality,
	// expressed as numerator/denominator. Standard is 2/3 (supermajority).
	FinalityThresholdNum uint64
	FinalityThresholdDen uint64

	// TotalStake is the total active stake in the system (in Gwei).
	// Used to determine when the finality threshold is met.
	TotalStake uint64
}

// DefaultSSFConfig returns production defaults for SSF.
// Uses 12s slot duration with 2/3+ finality threshold and a voter limit
// suitable for the initial SSF deployment.
func DefaultSSFConfig() *SSFConfig {
	return &SSFConfig{
		SlotDuration:         12,
		VoterLimit:           8192,
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           32_000_000 * GweiPerETH, // ~32M ETH staked
	}
}

// meetsThreshold returns true if voteStake meets or exceeds the 2/3+
// supermajority of totalStake. Uses integer arithmetic to avoid
// floating-point: voteStake * den >= totalStake * num.
func (c *SSFConfig) meetsThreshold(voteStake uint64) bool {
	if c.TotalStake == 0 {
		return false
	}
	// voteStake * 3 >= totalStake * 2 (for 2/3 threshold)
	return voteStake*c.FinalityThresholdDen >= c.TotalStake*c.FinalityThresholdNum
}

// SSFVote represents a single validator's finality vote for a slot.
type SSFVote struct {
	ValidatorIndex ValidatorIndex
	Slot           uint64
	BlockRoot      types.Hash
	Signature      [96]byte
	// Stake is the effective balance (weight) of this validator's vote.
	Stake uint64
}

// slotVotes tracks all votes for a single slot, keyed by block root.
type slotVotes struct {
	// votes maps validator index to their vote for de-duplication.
	votes map[ValidatorIndex]*SSFVote
	// stakeByRoot accumulates stake per block root.
	stakeByRoot map[types.Hash]uint64
	// totalVoteStake is the sum of all vote stakes for the slot.
	totalVoteStake uint64
}

func newSlotVotes() *slotVotes {
	return &slotVotes{
		votes:       make(map[ValidatorIndex]*SSFVote),
		stakeByRoot: make(map[types.Hash]uint64),
	}
}

// SSFState tracks the current single-slot finalization state.
type SSFState struct {
	mu sync.RWMutex

	config *SSFConfig

	// FinalizedSlot is the most recently finalized slot.
	FinalizedSlot uint64
	// FinalizedRoot is the block root of the most recently finalized slot.
	FinalizedRoot types.Hash

	// PendingVotes maps slot number to the accumulated votes for that slot.
	PendingVotes map[uint64]*slotVotes
}

// NewSSFState creates a new SSF state with the given config.
func NewSSFState(config *SSFConfig) *SSFState {
	if config == nil {
		config = DefaultSSFConfig()
	}
	return &SSFState{
		config:       config,
		PendingVotes: make(map[uint64]*slotVotes),
	}
}

// CastVote records a validator vote for the current slot.
// Returns an error if the validator has already voted for this slot
// or the vote is otherwise invalid.
func (s *SSFState) CastVote(vote SSFVote) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate voter index against limit.
	if uint64(vote.ValidatorIndex) >= s.config.VoterLimit {
		return fmt.Errorf("%w: index %d >= limit %d",
			ErrSSFInvalidVoter, vote.ValidatorIndex, s.config.VoterLimit)
	}

	// Check if this slot is already finalized.
	if vote.Slot <= s.FinalizedSlot && s.FinalizedSlot > 0 {
		return fmt.Errorf("%w: slot %d", ErrSSFSlotAlreadyFinal, vote.Slot)
	}

	// Get or create the vote tracker for this slot.
	sv, ok := s.PendingVotes[vote.Slot]
	if !ok {
		sv = newSlotVotes()
		s.PendingVotes[vote.Slot] = sv
	}

	// Reject duplicate votes from the same validator for the same slot.
	if _, exists := sv.votes[vote.ValidatorIndex]; exists {
		return fmt.Errorf("%w: validator %d at slot %d",
			ErrSSFDuplicateVote, vote.ValidatorIndex, vote.Slot)
	}

	// Record the vote.
	sv.votes[vote.ValidatorIndex] = &vote
	sv.stakeByRoot[vote.BlockRoot] += vote.Stake
	sv.totalVoteStake += vote.Stake

	return nil
}

// CheckFinality checks if the given block root at the given slot has
// achieved 2/3+ of total stake. Returns true if finality is met.
func (s *SSFState) CheckFinality(slot uint64, root types.Hash) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.config.TotalStake == 0 {
		return false, ErrSSFZeroStake
	}

	sv, ok := s.PendingVotes[slot]
	if !ok {
		return false, nil
	}

	rootStake := sv.stakeByRoot[root]
	return s.config.meetsThreshold(rootStake), nil
}

// FinalizeSlot marks a slot as finalized with the given block root.
// This is called after CheckFinality confirms the threshold is met.
func (s *SSFState) FinalizeSlot(slot uint64, root types.Hash) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify the slot is not regressing.
	if slot < s.FinalizedSlot {
		return fmt.Errorf("ssf: cannot finalize slot %d before current finalized slot %d",
			slot, s.FinalizedSlot)
	}

	s.FinalizedSlot = slot
	s.FinalizedRoot = root

	// Remove votes for the finalized slot (no longer needed).
	delete(s.PendingVotes, slot)

	return nil
}

// GetFinalizedHead returns the latest finalized slot and block root.
func (s *SSFState) GetFinalizedHead() (uint64, types.Hash) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FinalizedSlot, s.FinalizedRoot
}

// PruneVotes garbage collects votes for slots up to and including upToSlot.
// This is used to bound memory usage by removing old pending votes
// that are no longer needed.
func (s *SSFState) PruneVotes(upToSlot uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for slot := range s.PendingVotes {
		if slot <= upToSlot {
			delete(s.PendingVotes, slot)
		}
	}
}

// VoteCount returns the number of votes recorded for a given slot.
func (s *SSFState) VoteCount(slot uint64) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sv, ok := s.PendingVotes[slot]
	if !ok {
		return 0
	}
	return len(sv.votes)
}

// StakeForRoot returns the accumulated stake for a given block root at a slot.
func (s *SSFState) StakeForRoot(slot uint64, root types.Hash) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sv, ok := s.PendingVotes[slot]
	if !ok {
		return 0
	}
	return sv.stakeByRoot[root]
}
