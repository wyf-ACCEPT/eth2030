package consensus

// Endgame Finality V2 implements voting aggregation and finality enforcement
// for the M+ era (2029+). Unlike EndgameEngine which uses weighted votes
// and a pre-set validator set, V2 uses a simpler validator-count model
// with explicit finality rounds, optimistic fast-path finalization, and
// configurable round retention for historical queries.

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

var (
	ErrRoundNotStarted  = errors.New("finality-v2: round not started for slot")
	ErrDuplicateVoteV2  = errors.New("finality-v2: duplicate vote from validator")
	ErrInvalidValidator = errors.New("finality-v2: validator index out of range")
	ErrRoundFinalized   = errors.New("finality-v2: round already finalized")
	ErrSlotMismatch     = errors.New("finality-v2: vote slot does not match round")
)

// FinalityV2Config configures the endgame finality V2 engine.
type FinalityV2Config struct {
	// SupermajorityPct is the percentage threshold for optimistic
	// (fast-path) finalization. Default: 90.
	SupermajorityPct int

	// RetainRounds is the number of finalized rounds to keep in history
	// before pruning. Default: 64.
	RetainRounds int
}

// DefaultFinalityV2Config returns the default V2 configuration.
func DefaultFinalityV2Config() FinalityV2Config {
	return FinalityV2Config{
		SupermajorityPct: 90,
		RetainRounds:     64,
	}
}

// FinalityVote represents a single validator's finality vote.
type FinalityVote struct {
	ValidatorIndex uint64
	Slot           uint64
	BlockHash      types.Hash
	Signature      [96]byte // BLS signature placeholder
	Timestamp      uint64   // unix timestamp in seconds
}

// FinalityRound tracks the state of a single finality round for a slot.
type FinalityRound struct {
	Slot           uint64
	ValidatorCount int
	Threshold      int // 2/3 + 1 of validators
	Finalized      bool
	FinalizedHash  types.Hash

	// votes tracks which validators have voted and for which hash.
	votes map[uint64]types.Hash

	// hashVotes counts votes per block hash.
	hashVotes map[types.Hash]int

	// totalVotes is the number of votes cast so far.
	totalVotes int

	// optimisticThreshold is the count for fast-path finality.
	optimisticThreshold int
}

// newFinalityRound creates a new round for the given slot and validator count.
func newFinalityRound(slot uint64, validatorCount int, supermajorityPct int) *FinalityRound {
	threshold := (validatorCount*2)/3 + 1
	if threshold > validatorCount {
		threshold = validatorCount
	}

	optimistic := (validatorCount * supermajorityPct) / 100
	if optimistic < threshold {
		optimistic = threshold
	}

	return &FinalityRound{
		Slot:                slot,
		ValidatorCount:      validatorCount,
		Threshold:           threshold,
		votes:               make(map[uint64]types.Hash),
		hashVotes:           make(map[types.Hash]int),
		optimisticThreshold: optimistic,
	}
}

// VoteCount returns the total number of votes cast in this round.
func (r *FinalityRound) VoteCount() int {
	return r.totalVotes
}

// HashVotes returns the vote count for a specific block hash.
func (r *FinalityRound) HashVotes(h types.Hash) int {
	return r.hashVotes[h]
}

// LeadingHash returns the block hash with the most votes and its count.
func (r *FinalityRound) LeadingHash() (types.Hash, int) {
	var best types.Hash
	bestCount := 0
	for h, c := range r.hashVotes {
		if c > bestCount {
			best = h
			bestCount = c
		}
	}
	return best, bestCount
}

// EndgameFinalityV2 manages finality rounds with voting aggregation.
// It is fully thread-safe.
type EndgameFinalityV2 struct {
	mu     sync.RWMutex
	config FinalityV2Config

	// rounds maps slot -> FinalityRound.
	rounds map[uint64]*FinalityRound

	// finalized keeps an ordered record of finalized slots for history.
	finalized []uint64

	// latestFinalized is the highest finalized slot.
	latestFinalized uint64
}

// NewEndgameFinalityV2 creates a new V2 finality engine.
func NewEndgameFinalityV2(config FinalityV2Config) *EndgameFinalityV2 {
	if config.SupermajorityPct <= 0 || config.SupermajorityPct > 100 {
		config.SupermajorityPct = 90
	}
	if config.RetainRounds <= 0 {
		config.RetainRounds = 64
	}
	return &EndgameFinalityV2{
		config: config,
		rounds: make(map[uint64]*FinalityRound),
	}
}

// StartRound initializes a new finality round for the given slot.
// validatorCount is the total number of validators eligible to vote.
// If a round already exists for this slot, it is returned unchanged.
func (ef *EndgameFinalityV2) StartRound(slot uint64, validatorCount int) *FinalityRound {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	if existing, ok := ef.rounds[slot]; ok {
		return existing
	}

	round := newFinalityRound(slot, validatorCount, ef.config.SupermajorityPct)
	ef.rounds[slot] = round
	return round
}

// CastVote submits a finality vote. The round must already be started.
// Returns nil on success. If the vote causes finalization (standard 2/3+1
// or optimistic supermajority), the round is marked finalized.
func (ef *EndgameFinalityV2) CastVote(vote *FinalityVote) error {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	round, ok := ef.rounds[vote.Slot]
	if !ok {
		return ErrRoundNotStarted
	}

	if round.Finalized {
		return ErrRoundFinalized
	}

	if vote.ValidatorIndex >= uint64(round.ValidatorCount) {
		return ErrInvalidValidator
	}

	if _, exists := round.votes[vote.ValidatorIndex]; exists {
		return ErrDuplicateVoteV2
	}

	// Record the vote.
	round.votes[vote.ValidatorIndex] = vote.BlockHash
	round.hashVotes[vote.BlockHash]++
	round.totalVotes++

	// Check for optimistic finality first (supermajority fast-path).
	if round.hashVotes[vote.BlockHash] >= round.optimisticThreshold {
		ef.finalizeRound(round, vote.BlockHash)
		return nil
	}

	// Check standard 2/3+1 threshold.
	if round.hashVotes[vote.BlockHash] >= round.Threshold {
		ef.finalizeRound(round, vote.BlockHash)
		return nil
	}

	return nil
}

// finalizeRound marks the round as finalized. Caller must hold ef.mu.
func (ef *EndgameFinalityV2) finalizeRound(round *FinalityRound, hash types.Hash) {
	round.Finalized = true
	round.FinalizedHash = hash

	if round.Slot > ef.latestFinalized {
		ef.latestFinalized = round.Slot
	}
	ef.finalized = append(ef.finalized, round.Slot)

	// Prune old rounds.
	ef.pruneOldRounds()
}

// pruneOldRounds removes rounds beyond the retention window.
// Caller must hold ef.mu.
func (ef *EndgameFinalityV2) pruneOldRounds() {
	if ef.latestFinalized <= uint64(ef.config.RetainRounds) {
		return
	}
	cutoff := ef.latestFinalized - uint64(ef.config.RetainRounds)
	for slot := range ef.rounds {
		if slot < cutoff {
			delete(ef.rounds, slot)
		}
	}

	// Also trim finalized history.
	trimIdx := 0
	for trimIdx < len(ef.finalized) && ef.finalized[trimIdx] < cutoff {
		trimIdx++
	}
	if trimIdx > 0 {
		ef.finalized = ef.finalized[trimIdx:]
	}
}

// IsFinalized returns true if the given slot has been finalized.
func (ef *EndgameFinalityV2) IsFinalized(slot uint64) bool {
	ef.mu.RLock()
	defer ef.mu.RUnlock()

	round, ok := ef.rounds[slot]
	if !ok {
		return false
	}
	return round.Finalized
}

// FinalizedHash returns the finalized block hash for a slot.
// Returns the zero hash if the slot is not finalized.
func (ef *EndgameFinalityV2) FinalizedHash(slot uint64) types.Hash {
	ef.mu.RLock()
	defer ef.mu.RUnlock()

	round, ok := ef.rounds[slot]
	if !ok {
		return types.Hash{}
	}
	if !round.Finalized {
		return types.Hash{}
	}
	return round.FinalizedHash
}

// GetRound returns a snapshot of the finality round for a given slot.
// Returns nil if no round exists for the slot.
func (ef *EndgameFinalityV2) GetRound(slot uint64) *FinalityRound {
	ef.mu.RLock()
	defer ef.mu.RUnlock()

	round, ok := ef.rounds[slot]
	if !ok {
		return nil
	}
	// Return a shallow copy to prevent external mutation.
	cpy := *round
	return &cpy
}

// LatestFinalizedSlot returns the highest finalized slot number.
func (ef *EndgameFinalityV2) LatestFinalizedSlot() uint64 {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return ef.latestFinalized
}

// FinalizedSlots returns a copy of the ordered list of finalized slot numbers.
func (ef *EndgameFinalityV2) FinalizedSlots() []uint64 {
	ef.mu.RLock()
	defer ef.mu.RUnlock()

	out := make([]uint64, len(ef.finalized))
	copy(out, ef.finalized)
	return out
}

// ActiveRounds returns the number of currently tracked rounds.
func (ef *EndgameFinalityV2) ActiveRounds() int {
	ef.mu.RLock()
	defer ef.mu.RUnlock()
	return len(ef.rounds)
}

// Progress returns the vote progress for a slot as a fraction (0.0 - 1.0)
// relative to the 2/3+1 threshold. Returns 0 if the round doesn't exist.
func (ef *EndgameFinalityV2) Progress(slot uint64) float64 {
	ef.mu.RLock()
	defer ef.mu.RUnlock()

	round, ok := ef.rounds[slot]
	if !ok || round.Threshold == 0 {
		return 0
	}

	_, bestCount := round.LeadingHash()
	p := float64(bestCount) / float64(round.Threshold)
	if p > 1.0 {
		p = 1.0
	}
	return p
}

// Cleanup removes all non-finalized rounds older than the given slot.
// This can be called periodically to free memory for stale rounds that
// never reached finality.
func (ef *EndgameFinalityV2) Cleanup(beforeSlot uint64) int {
	ef.mu.Lock()
	defer ef.mu.Unlock()

	removed := 0
	for slot, round := range ef.rounds {
		if slot < beforeSlot && !round.Finalized {
			delete(ef.rounds, slot)
			removed++
		}
	}
	return removed
}
