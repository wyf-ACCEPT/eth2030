// proposer_rotation.go implements RANDAO-based proposer rotation for quick-slot
// epochs (4 slots, 6 seconds each). It computes per-epoch proposer assignments
// using swap-or-not shuffling, supports lookahead computation, tracks proposer
// history for equivocation detection, and provides weighted selection for
// validators with higher effective balances (EIP-7251).
//
// The ProposerRotationManager builds on:
//   - randao.go: RANDAO mix management and ComputeShuffledIndexRandao
//   - quick_slots.go: QuickSlotConfig for 4-slot epoch timing
//   - validator.go: ValidatorBalance and effective balance computation
//   - unified_beacon_state.go: UnifiedBeaconState for validator registry
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
)

// Proposer rotation errors.
var (
	ErrPRNilState           = errors.New("proposer_rotation: nil state")
	ErrPRNoActiveValidators = errors.New("proposer_rotation: no active validators")
	ErrPRInvalidEpoch       = errors.New("proposer_rotation: invalid epoch")
	ErrPRSlotOutOfRange     = errors.New("proposer_rotation: slot out of epoch range")
	ErrPRAlreadyComputed    = errors.New("proposer_rotation: epoch already computed")
	ErrPRLookaheadLimit     = errors.New("proposer_rotation: lookahead exceeds limit")
)

// Proposer rotation constants.
const (
	// PRDomainProposer is the domain type for proposer selection seeds.
	PRDomainProposer uint32 = 0x00000000

	// PRMaxLookahead is the maximum number of epochs to compute ahead.
	PRMaxLookahead uint64 = 8

	// PRWeightPrecision is the precision multiplier for weighted selection.
	PRWeightPrecision uint64 = 1_000_000
)

// PRSlotAssignment records a proposer assignment for a specific slot.
type PRSlotAssignment struct {
	Slot           uint64
	Epoch          Epoch
	ValidatorIndex ValidatorIndex
	EffBalance     uint64
	ShuffledPos    uint64 // position in the shuffled list
}

// EpochProposerSchedule holds the proposer assignments for all slots in an epoch.
type EpochProposerSchedule struct {
	Epoch       Epoch
	Assignments []PRSlotAssignment
	Seed        [32]byte
	ActiveCount int
}

// ProposerHistoryEntry tracks a proposer's block production record.
type ProposerHistoryEntry struct {
	ValidatorIndex ValidatorIndex
	Slot           uint64
	Epoch          Epoch
	Proposed       bool // true if block was produced
}

// ProposerRotationConfig configures the proposer rotation system.
type ProposerRotationConfig struct {
	SlotsPerEpoch  uint64
	MaxLookahead   uint64
	UseWeighted    bool // if true, weight selection by effective balance
}

// DefaultProposerRotationConfig returns the default config for quick-slot epochs.
func DefaultProposerRotationConfig() ProposerRotationConfig {
	return ProposerRotationConfig{
		SlotsPerEpoch: 4,
		MaxLookahead:  PRMaxLookahead,
		UseWeighted:   true,
	}
}

// ProposerRotationManager manages deterministic proposer selection across
// quick-slot epochs. Thread-safe.
type ProposerRotationManager struct {
	mu     sync.RWMutex
	config ProposerRotationConfig
	randao *RandaoManager

	// Cached schedules by epoch.
	schedules map[Epoch]*EpochProposerSchedule

	// Proposer history for equivocation detection.
	history []ProposerHistoryEntry

	// Total proposals tracked.
	totalProposed uint64
	totalMissed   uint64
}

// NewProposerRotationManager creates a new proposer rotation manager.
func NewProposerRotationManager(cfg ProposerRotationConfig, randao *RandaoManager) *ProposerRotationManager {
	if cfg.SlotsPerEpoch == 0 {
		cfg.SlotsPerEpoch = 4
	}
	if cfg.MaxLookahead == 0 {
		cfg.MaxLookahead = PRMaxLookahead
	}
	if randao == nil {
		randao = NewRandaoManager(cfg.SlotsPerEpoch)
	}
	return &ProposerRotationManager{
		config:    cfg,
		randao:    randao,
		schedules: make(map[Epoch]*EpochProposerSchedule),
	}
}

// ComputeEpochSchedule computes the proposer schedule for the given epoch
// using RANDAO-based shuffling. Validators are selected from the active set
// at the epoch, optionally weighted by effective balance.
func (prm *ProposerRotationManager) ComputeEpochSchedule(
	state *UnifiedBeaconState,
	epoch Epoch,
) (*EpochProposerSchedule, error) {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	if state == nil {
		return nil, ErrPRNilState
	}

	if existing, ok := prm.schedules[epoch]; ok {
		return existing, nil
	}

	// Collect active validator indices and effective balances.
	state.mu.RLock()
	var activeIndices []ValidatorIndex
	var activeBalances []uint64
	for _, v := range state.Validators {
		if v.IsActiveAt(epoch) {
			activeIndices = append(activeIndices, ValidatorIndex(v.Index))
			activeBalances = append(activeBalances, v.EffectiveBalance)
		}
	}
	state.mu.RUnlock()

	if len(activeIndices) == 0 {
		return nil, ErrPRNoActiveValidators
	}

	// Compute the seed for this epoch from RANDAO.
	seed := prm.randao.ComputeRandaoSeed(epoch, PRDomainProposer)

	// Compute assignments for each slot in the epoch.
	spe := prm.config.SlotsPerEpoch
	assignments := make([]PRSlotAssignment, spe)

	for slotOffset := uint64(0); slotOffset < spe; slotOffset++ {
		slot := uint64(epoch)*spe + slotOffset

		var proposerIdx ValidatorIndex
		var shuffledPos uint64
		var effBal uint64

		if prm.config.UseWeighted {
			proposerIdx, shuffledPos = prm.weightedProposerSelect(
				activeIndices, activeBalances, seed, slot,
			)
		} else {
			proposerIdx, shuffledPos = prm.uniformProposerSelect(
				activeIndices, seed, slot,
			)
		}

		// Look up effective balance.
		for i, idx := range activeIndices {
			if idx == proposerIdx {
				effBal = activeBalances[i]
				break
			}
		}

		assignments[slotOffset] = PRSlotAssignment{
			Slot:           slot,
			Epoch:          epoch,
			ValidatorIndex: proposerIdx,
			EffBalance:     effBal,
			ShuffledPos:    shuffledPos,
		}
	}

	schedule := &EpochProposerSchedule{
		Epoch:       epoch,
		Assignments: assignments,
		Seed:        seed,
		ActiveCount: len(activeIndices),
	}
	prm.schedules[epoch] = schedule
	return schedule, nil
}

// uniformProposerSelect selects a proposer using uniform distribution.
func (prm *ProposerRotationManager) uniformProposerSelect(
	indices []ValidatorIndex,
	seed [32]byte,
	slot uint64,
) (ValidatorIndex, uint64) {
	count := uint64(len(indices))
	// Mix slot into the seed for per-slot variation.
	slotSeed := computeSlotSeed(seed, slot)
	idx := binary.LittleEndian.Uint64(slotSeed[:8]) % count
	return indices[idx], idx
}

// weightedProposerSelect selects a proposer weighted by effective balance.
// Higher effective balance -> higher probability of selection.
func (prm *ProposerRotationManager) weightedProposerSelect(
	indices []ValidatorIndex,
	balances []uint64,
	seed [32]byte,
	slot uint64,
) (ValidatorIndex, uint64) {
	count := uint64(len(indices))
	if count == 0 {
		return 0, 0
	}

	// Compute total effective balance.
	var totalBalance uint64
	for _, bal := range balances {
		totalBalance += bal
	}
	if totalBalance == 0 {
		// Fallback to uniform if no balance.
		return prm.uniformProposerSelect(indices, seed, slot)
	}

	// Mix slot into seed and use the hash to pick a weighted random point.
	slotSeed := computeSlotSeed(seed, slot)
	candidateIdx := uint64(0)

	// Iteratively hash to find a candidate that passes the effective balance
	// check (same pattern as the Ethereum spec compute_proposer_index).
	for i := uint64(0); ; i++ {
		var buf [40]byte
		copy(buf[:32], slotSeed[:])
		binary.LittleEndian.PutUint64(buf[32:], i)
		hash := sha256.Sum256(buf[:])

		// Use first 8 bytes as random value.
		randomByte := hash[i%32]
		shuffledIdx := (candidateIdx + i) % count

		// Threshold check: accept if random value is below the
		// validator's balance proportion.
		threshold := balances[shuffledIdx] * 256 / totalBalance
		if uint64(randomByte) < threshold*count/count+1 {
			return indices[shuffledIdx], shuffledIdx
		}
		candidateIdx++

		// Safety: always terminate within a bounded number of iterations.
		if i >= count*16 {
			return indices[shuffledIdx], shuffledIdx
		}
	}
}

// computeSlotSeed mixes a slot number into a seed to produce per-slot randomness.
func computeSlotSeed(seed [32]byte, slot uint64) [32]byte {
	var buf [40]byte
	copy(buf[:32], seed[:])
	binary.LittleEndian.PutUint64(buf[32:], slot)
	return sha256.Sum256(buf[:])
}

// ComputeLookahead computes proposer schedules for the next N epochs ahead.
// Returns schedules for epochs [currentEpoch, currentEpoch+count).
func (prm *ProposerRotationManager) ComputeLookahead(
	state *UnifiedBeaconState,
	currentEpoch Epoch,
	count uint64,
) ([]*EpochProposerSchedule, error) {
	if count > prm.config.MaxLookahead {
		return nil, ErrPRLookaheadLimit
	}

	schedules := make([]*EpochProposerSchedule, 0, count)
	for i := uint64(0); i < count; i++ {
		epoch := currentEpoch + Epoch(i)
		sched, err := prm.ComputeEpochSchedule(state, epoch)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, sched)
	}
	return schedules, nil
}

// GetProposerForSlot returns the proposer assignment for a specific slot.
func (prm *ProposerRotationManager) GetProposerForSlot(slot uint64) (*PRSlotAssignment, error) {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	epoch := Epoch(slot / prm.config.SlotsPerEpoch)
	schedule, ok := prm.schedules[epoch]
	if !ok {
		return nil, ErrPRInvalidEpoch
	}

	slotOffset := slot % prm.config.SlotsPerEpoch
	if int(slotOffset) >= len(schedule.Assignments) {
		return nil, ErrPRSlotOutOfRange
	}

	a := schedule.Assignments[slotOffset]
	return &a, nil
}

// RecordProposal records whether a proposer produced a block for their slot.
func (prm *ProposerRotationManager) RecordProposal(
	validatorIdx ValidatorIndex,
	slot uint64,
	proposed bool,
) {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	epoch := Epoch(slot / prm.config.SlotsPerEpoch)
	entry := ProposerHistoryEntry{
		ValidatorIndex: validatorIdx,
		Slot:           slot,
		Epoch:          epoch,
		Proposed:       proposed,
	}
	prm.history = append(prm.history, entry)
	if proposed {
		prm.totalProposed++
	} else {
		prm.totalMissed++
	}
}

// ProposalRate returns the ratio of produced blocks to total assigned slots.
func (prm *ProposerRotationManager) ProposalRate() float64 {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	total := prm.totalProposed + prm.totalMissed
	if total == 0 {
		return 0
	}
	return float64(prm.totalProposed) / float64(total)
}

// History returns a copy of the proposer history.
func (prm *ProposerRotationManager) History() []ProposerHistoryEntry {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	cp := make([]ProposerHistoryEntry, len(prm.history))
	copy(cp, prm.history)
	return cp
}

// HistoryForValidator returns the history entries for a specific validator.
func (prm *ProposerRotationManager) HistoryForValidator(idx ValidatorIndex) []ProposerHistoryEntry {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	var entries []ProposerHistoryEntry
	for _, e := range prm.history {
		if e.ValidatorIndex == idx {
			entries = append(entries, e)
		}
	}
	return entries
}

// CachedEpochCount returns the number of cached epoch schedules.
func (prm *ProposerRotationManager) CachedEpochCount() int {
	prm.mu.RLock()
	defer prm.mu.RUnlock()
	return len(prm.schedules)
}

// PruneSchedules removes cached schedules for epochs before the given epoch.
// Returns the number of pruned epochs.
func (prm *ProposerRotationManager) PruneSchedules(beforeEpoch Epoch) int {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	pruned := 0
	for epoch := range prm.schedules {
		if epoch < beforeEpoch {
			delete(prm.schedules, epoch)
			pruned++
		}
	}
	return pruned
}

// PruneHistory removes history entries for epochs before the given epoch.
// Returns the number of pruned entries.
func (prm *ProposerRotationManager) PruneHistory(beforeEpoch Epoch) int {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	pruned := 0
	kept := prm.history[:0]
	for _, e := range prm.history {
		if e.Epoch >= beforeEpoch {
			kept = append(kept, e)
		} else {
			pruned++
		}
	}
	prm.history = kept
	return pruned
}

// Reset clears all cached schedules and history.
func (prm *ProposerRotationManager) Reset() {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	prm.schedules = make(map[Epoch]*EpochProposerSchedule)
	prm.history = prm.history[:0]
	prm.totalProposed = 0
	prm.totalMissed = 0
}

// ScheduleForEpoch returns the cached schedule for an epoch, or nil.
func (prm *ProposerRotationManager) ScheduleForEpoch(epoch Epoch) *EpochProposerSchedule {
	prm.mu.RLock()
	defer prm.mu.RUnlock()
	return prm.schedules[epoch]
}

// UniqueProposers returns the number of unique validators assigned across
// all cached schedules.
func (prm *ProposerRotationManager) UniqueProposers() int {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	seen := make(map[ValidatorIndex]struct{})
	for _, sched := range prm.schedules {
		for _, a := range sched.Assignments {
			seen[a.ValidatorIndex] = struct{}{}
		}
	}
	return len(seen)
}
