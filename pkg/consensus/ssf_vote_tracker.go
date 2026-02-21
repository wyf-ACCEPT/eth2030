// ssf_vote_tracker.go implements single-slot finality vote tracking with
// supermajority detection, equivocation (double-vote) detection, BLS
// signature batching placeholders, vote expiry, and finality events.
// Complements ssf.go (basic SSF state) and ssf_vote_aggregator.go (quorum
// tracking) by adding equivocation detection and finality event emission.
package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// SSF vote tracker errors.
var (
	ErrVTSlotFinalized    = errors.New("vote_tracker: slot already finalized")
	ErrVTZeroTotalStake   = errors.New("vote_tracker: total active stake is zero")
	ErrVTInvalidValidator = errors.New("vote_tracker: invalid validator index")
	ErrVTNilVote          = errors.New("vote_tracker: nil vote")
	ErrVTOldSlot          = errors.New("vote_tracker: vote for expired slot")
)

// SSFVoteTrackerConfig configures the vote tracker.
type SSFVoteTrackerConfig struct {
	// TotalActiveStake is the sum of all active validator effective balances.
	TotalActiveStake uint64
	// SupermajorityNum and SupermajorityDen define the threshold fraction
	// (typically 2/3).
	SupermajorityNum uint64
	SupermajorityDen uint64
	// MaxSlotRetention is the number of past slots to keep before expiry.
	MaxSlotRetention uint64
}

// DefaultSSFVoteTrackerConfig returns sensible defaults.
func DefaultSSFVoteTrackerConfig() SSFVoteTrackerConfig {
	return SSFVoteTrackerConfig{
		TotalActiveStake: 32_000_000 * GweiPerETH,
		SupermajorityNum: 2,
		SupermajorityDen: 3,
		MaxSlotRetention: 64,
	}
}

// TrackedVote represents a single validator's SSF vote.
type TrackedVote struct {
	ValidatorIndex ValidatorIndex
	Slot           uint64
	BlockRoot      types.Hash
	Stake          uint64
	Signature      [96]byte
}

// EquivocationRecord records a double-vote by a validator in one slot.
type EquivocationRecord struct {
	ValidatorIndex ValidatorIndex
	Slot           uint64
	Root1          types.Hash
	Root2          types.Hash
}

// FinalityEvent is emitted when a slot reaches the supermajority threshold.
type FinalityEvent struct {
	Slot          uint64
	BlockRoot     types.Hash
	TotalStake    uint64
	VotingStake   uint64
	VoteCount     int
	Participation float64 // 0.0 to 100.0
}

// BatchSignatureData aggregates BLS signature data for batch verification.
type BatchSignatureData struct {
	Slot       uint64
	BlockRoot  types.Hash
	Signatures [][96]byte
	Pubkeys    [][48]byte
	Count      int
}

// slotVoteTrack holds all per-slot vote data for the tracker.
type slotVoteTrack struct {
	votes       map[ValidatorIndex]*TrackedVote
	stakeByRoot map[types.Hash]uint64
	totalStake  uint64
	finalized   bool
	finalRoot   types.Hash
}

func newSlotVoteTrack() *slotVoteTrack {
	return &slotVoteTrack{
		votes:       make(map[ValidatorIndex]*TrackedVote),
		stakeByRoot: make(map[types.Hash]uint64),
	}
}

// SSFVoteTracker tracks validator votes per slot, detects equivocation,
// checks supermajority, and emits finality events. Thread-safe.
type SSFVoteTracker struct {
	mu     sync.RWMutex
	config SSFVoteTrackerConfig

	slots          map[uint64]*slotVoteTrack
	equivocations  []*EquivocationRecord
	finalityEvents []*FinalityEvent

	// highestSlot is the highest slot for which a vote has been received.
	highestSlot uint64
}

// NewSSFVoteTracker creates a new vote tracker with the given config.
func NewSSFVoteTracker(cfg SSFVoteTrackerConfig) *SSFVoteTracker {
	if cfg.SupermajorityDen == 0 {
		cfg.SupermajorityDen = 3
	}
	if cfg.SupermajorityNum == 0 {
		cfg.SupermajorityNum = 2
	}
	if cfg.MaxSlotRetention == 0 {
		cfg.MaxSlotRetention = 64
	}
	return &SSFVoteTracker{
		config: cfg,
		slots:  make(map[uint64]*slotVoteTrack),
	}
}

// RecordVote records a validator's vote for a slot. Returns true if this
// vote caused the slot to reach the supermajority threshold.
// If the validator has already voted for a different root, an equivocation
// record is created. If the validator voted for the same root, it is a no-op.
func (vt *SSFVoteTracker) RecordVote(vote *TrackedVote) (bool, error) {
	if vote == nil {
		return false, ErrVTNilVote
	}
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if vt.config.TotalActiveStake == 0 {
		return false, ErrVTZeroTotalStake
	}

	// Check if slot is expired.
	if vt.highestSlot > vt.config.MaxSlotRetention &&
		vote.Slot < vt.highestSlot-vt.config.MaxSlotRetention {
		return false, fmt.Errorf("%w: slot %d (current high %d)",
			ErrVTOldSlot, vote.Slot, vt.highestSlot)
	}

	if vote.Slot > vt.highestSlot {
		vt.highestSlot = vote.Slot
	}

	sv, ok := vt.slots[vote.Slot]
	if !ok {
		sv = newSlotVoteTrack()
		vt.slots[vote.Slot] = sv
	}

	// Slot already finalized.
	if sv.finalized {
		return false, fmt.Errorf("%w: slot %d", ErrVTSlotFinalized, vote.Slot)
	}

	// Check for equivocation (same validator, same slot, different root).
	if existing, dup := sv.votes[vote.ValidatorIndex]; dup {
		if existing.BlockRoot != vote.BlockRoot {
			vt.equivocations = append(vt.equivocations, &EquivocationRecord{
				ValidatorIndex: vote.ValidatorIndex,
				Slot:           vote.Slot,
				Root1:          existing.BlockRoot,
				Root2:          vote.BlockRoot,
			})
		}
		// Do not double-count the stake.
		return false, nil
	}

	// Record the vote.
	sv.votes[vote.ValidatorIndex] = vote
	sv.stakeByRoot[vote.BlockRoot] += vote.Stake
	sv.totalStake += vote.Stake

	// Check supermajority for this root.
	rootStake := sv.stakeByRoot[vote.BlockRoot]
	if vt.meetsSupermajority(rootStake) && !sv.finalized {
		sv.finalized = true
		sv.finalRoot = vote.BlockRoot

		participation := float64(sv.totalStake) / float64(vt.config.TotalActiveStake) * 100.0
		evt := &FinalityEvent{
			Slot:          vote.Slot,
			BlockRoot:     vote.BlockRoot,
			TotalStake:    vt.config.TotalActiveStake,
			VotingStake:   sv.totalStake,
			VoteCount:     len(sv.votes),
			Participation: participation,
		}
		vt.finalityEvents = append(vt.finalityEvents, evt)
		return true, nil
	}

	return false, nil
}

// meetsSupermajority returns true if rootStake >= totalStake * num / den.
// Uses integer arithmetic: rootStake * den >= totalStake * num.
func (vt *SSFVoteTracker) meetsSupermajority(rootStake uint64) bool {
	return rootStake*vt.config.SupermajorityDen >=
		vt.config.TotalActiveStake*vt.config.SupermajorityNum
}

// IsFinalized returns whether a slot has reached finality.
func (vt *SSFVoteTracker) IsFinalized(slot uint64) bool {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	sv, ok := vt.slots[slot]
	if !ok {
		return false
	}
	return sv.finalized
}

// FinalizedRoot returns the finalized block root for a slot, or zero hash.
func (vt *SSFVoteTracker) FinalizedRoot(slot uint64) types.Hash {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	sv, ok := vt.slots[slot]
	if !ok {
		return types.Hash{}
	}
	if !sv.finalized {
		return types.Hash{}
	}
	return sv.finalRoot
}

// VoteCount returns the number of votes for a slot.
func (vt *SSFVoteTracker) VoteCount(slot uint64) int {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	sv, ok := vt.slots[slot]
	if !ok {
		return 0
	}
	return len(sv.votes)
}

// StakeForRoot returns the accumulated stake for a root at a slot.
func (vt *SSFVoteTracker) StakeForRoot(slot uint64, root types.Hash) uint64 {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	sv, ok := vt.slots[slot]
	if !ok {
		return 0
	}
	return sv.stakeByRoot[root]
}

// TotalVotingStake returns the total voting stake for a slot.
func (vt *SSFVoteTracker) TotalVotingStake(slot uint64) uint64 {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	sv, ok := vt.slots[slot]
	if !ok {
		return 0
	}
	return sv.totalStake
}

// DrainEquivocations returns all detected equivocations and clears the buffer.
func (vt *SSFVoteTracker) DrainEquivocations() []*EquivocationRecord {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	evs := vt.equivocations
	vt.equivocations = nil
	return evs
}

// PeekEquivocations returns equivocations without consuming them.
func (vt *SSFVoteTracker) PeekEquivocations() []*EquivocationRecord {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	out := make([]*EquivocationRecord, len(vt.equivocations))
	copy(out, vt.equivocations)
	return out
}

// DrainFinalityEvents returns all finality events and clears the buffer.
func (vt *SSFVoteTracker) DrainFinalityEvents() []*FinalityEvent {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	evs := vt.finalityEvents
	vt.finalityEvents = nil
	return evs
}

// PeekFinalityEvents returns finality events without consuming them.
func (vt *SSFVoteTracker) PeekFinalityEvents() []*FinalityEvent {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	out := make([]*FinalityEvent, len(vt.finalityEvents))
	copy(out, vt.finalityEvents)
	return out
}

// ExpireOldSlots removes vote data for slots older than the retention window.
// Returns the number of slots expired.
func (vt *SSFVoteTracker) ExpireOldSlots() int {
	vt.mu.Lock()
	defer vt.mu.Unlock()

	if vt.highestSlot <= vt.config.MaxSlotRetention {
		return 0
	}
	cutoff := vt.highestSlot - vt.config.MaxSlotRetention
	expired := 0
	for slot := range vt.slots {
		if slot < cutoff {
			delete(vt.slots, slot)
			expired++
		}
	}
	return expired
}

// TrackedSlotCount returns the number of slots currently being tracked.
func (vt *SSFVoteTracker) TrackedSlotCount() int {
	vt.mu.RLock()
	defer vt.mu.RUnlock()
	return len(vt.slots)
}

// CollectBatchSignatures collects all signatures for a given slot and root
// for BLS batch verification. Returns nil if no matching votes are found.
func (vt *SSFVoteTracker) CollectBatchSignatures(slot uint64, root types.Hash) *BatchSignatureData {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	sv, ok := vt.slots[slot]
	if !ok {
		return nil
	}

	batch := &BatchSignatureData{
		Slot:      slot,
		BlockRoot: root,
	}
	for _, vote := range sv.votes {
		if vote.BlockRoot == root {
			batch.Signatures = append(batch.Signatures, vote.Signature)
			// Derive a placeholder pubkey from validator index for testing.
			var pk [48]byte
			pk[0] = byte(vote.ValidatorIndex)
			pk[1] = byte(vote.ValidatorIndex >> 8)
			batch.Pubkeys = append(batch.Pubkeys, pk)
			batch.Count++
		}
	}
	if batch.Count == 0 {
		return nil
	}
	return batch
}

// Participation returns the percentage of total stake that has voted for
// a given slot across all roots. Range 0.0 to 100.0.
func (vt *SSFVoteTracker) Participation(slot uint64) float64 {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	if vt.config.TotalActiveStake == 0 {
		return 0
	}
	sv, ok := vt.slots[slot]
	if !ok {
		return 0
	}
	return float64(sv.totalStake) / float64(vt.config.TotalActiveStake) * 100.0
}

// LeadingRoot returns the root with the most accumulated stake for a slot.
func (vt *SSFVoteTracker) LeadingRoot(slot uint64) (types.Hash, uint64) {
	vt.mu.RLock()
	defer vt.mu.RUnlock()

	sv, ok := vt.slots[slot]
	if !ok {
		return types.Hash{}, 0
	}
	var bestRoot types.Hash
	var bestStake uint64
	for root, stake := range sv.stakeByRoot {
		if stake > bestStake {
			bestRoot = root
			bestStake = stake
		}
	}
	return bestRoot, bestStake
}

// computeVoteDigest computes a hash of a vote for deduplication/verification.
func computeVoteDigest(validatorIdx ValidatorIndex, slot uint64, root types.Hash) types.Hash {
	data := make([]byte, 0, 8+8+32)
	vi := uint64(validatorIdx)
	data = append(data, byte(vi), byte(vi>>8), byte(vi>>16), byte(vi>>24),
		byte(vi>>32), byte(vi>>40), byte(vi>>48), byte(vi>>56))
	data = append(data, byte(slot), byte(slot>>8), byte(slot>>16), byte(slot>>24),
		byte(slot>>32), byte(slot>>40), byte(slot>>48), byte(slot>>56))
	data = append(data, root[:]...)
	return crypto.Keccak256Hash(data)
}
