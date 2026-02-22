// validator_set.go implements a comprehensive validator registry with index
// lookups, committee assignment caching, activation/exit queue processing,
// effective balance tracking, and churn limit calculations.
//
// It builds on the existing ValidatorBalance, ValidatorSet, and ValidatorV2
// types to provide a higher-level registry suitable for beacon state integration
// with the epoch processor and sync committee manager.
package consensus

import (
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/crypto"
)

// ValidatorRegistry errors.
var (
	ErrRegistryFull          = errors.New("validator_registry: registry at capacity")
	ErrRegistryDuplicatePubkey = errors.New("validator_registry: duplicate pubkey")
	ErrRegistryIndexMismatch = errors.New("validator_registry: index mismatch")
	ErrRegistryEmpty         = errors.New("validator_registry: no validators in registry")
)

// ValidatorRegistryConfig configures the validator registry.
type ValidatorRegistryConfig struct {
	// MaxValidators is the maximum number of validators (default: 2^22).
	MaxValidators int

	// SlotsPerEpoch is the number of slots per epoch.
	SlotsPerEpoch uint64

	// TargetCommitteeSize is the target committee size per slot.
	TargetCommitteeSize int

	// MaxCommitteesPerSlot is the maximum number of committees per slot.
	MaxCommitteesPerSlot int
}

// DefaultValidatorRegistryConfig returns mainnet-like defaults.
func DefaultValidatorRegistryConfig() ValidatorRegistryConfig {
	return ValidatorRegistryConfig{
		MaxValidators:        1 << 22, // ~4M
		SlotsPerEpoch:        32,
		TargetCommitteeSize:  128,
		MaxCommitteesPerSlot: 64,
	}
}

// ValidatorEntry represents a single validator in the registry with full
// lifecycle fields and a sequential index.
type ValidatorEntry struct {
	Index                ValidatorIndex
	Pubkey               [48]byte
	EffectiveBalance     uint64
	Balance              uint64
	Slashed              bool
	ActivationEligible   Epoch
	ActivationEpoch      Epoch
	ExitEpoch            Epoch
	WithdrawableEpoch    Epoch
}

// IsActiveAt returns true if the validator is active at the given epoch.
func (ve *ValidatorEntry) IsActiveAt(epoch Epoch) bool {
	return ve.ActivationEpoch <= epoch && epoch < ve.ExitEpoch
}

// IsExiting returns true if the validator has initiated exit but not yet exited.
func (ve *ValidatorEntry) IsExiting(epoch Epoch) bool {
	return ve.ExitEpoch != FarFutureEpoch && epoch < ve.ExitEpoch && ve.ActivationEpoch <= epoch
}

// CommitteeAssignmentEntry maps a validator to a specific committee and slot.
type CommitteeAssignmentEntry struct {
	ValidatorIndex ValidatorIndex
	Slot           Slot
	CommitteeIdx   uint64
	PositionInComm int
}

// ValidatorRegistry is a comprehensive validator set with index lookups,
// committee caching, and lifecycle management. Thread-safe.
type ValidatorRegistry struct {
	mu sync.RWMutex

	config     ValidatorRegistryConfig
	validators []*ValidatorEntry
	byPubkey   map[[48]byte]ValidatorIndex

	// Committee cache: epoch -> slot -> committee index -> validator indices.
	committeeCache map[Epoch]map[Slot]map[uint64][]ValidatorIndex
}

// NewValidatorRegistry creates a new empty validator registry.
func NewValidatorRegistry(config ValidatorRegistryConfig) *ValidatorRegistry {
	return &ValidatorRegistry{
		config:         config,
		validators:     make([]*ValidatorEntry, 0),
		byPubkey:       make(map[[48]byte]ValidatorIndex),
		committeeCache: make(map[Epoch]map[Slot]map[uint64][]ValidatorIndex),
	}
}

// Register adds a new validator to the registry and returns its assigned index.
func (vr *ValidatorRegistry) Register(pubkey [48]byte, effectiveBalance, balance uint64) (ValidatorIndex, error) {
	vr.mu.Lock()
	defer vr.mu.Unlock()

	if len(vr.validators) >= vr.config.MaxValidators {
		return 0, ErrRegistryFull
	}
	if _, exists := vr.byPubkey[pubkey]; exists {
		return 0, ErrRegistryDuplicatePubkey
	}

	idx := ValidatorIndex(len(vr.validators))
	entry := &ValidatorEntry{
		Index:              idx,
		Pubkey:             pubkey,
		EffectiveBalance:   effectiveBalance,
		Balance:            balance,
		ActivationEligible: FarFutureEpoch,
		ActivationEpoch:    FarFutureEpoch,
		ExitEpoch:          FarFutureEpoch,
		WithdrawableEpoch:  FarFutureEpoch,
	}

	vr.validators = append(vr.validators, entry)
	vr.byPubkey[pubkey] = idx
	return idx, nil
}

// LookupByPubkey returns the validator index for the given public key.
func (vr *ValidatorRegistry) LookupByPubkey(pubkey [48]byte) (ValidatorIndex, bool) {
	vr.mu.RLock()
	defer vr.mu.RUnlock()
	idx, ok := vr.byPubkey[pubkey]
	return idx, ok
}

// GetByIndex returns a copy of the validator entry at the given index.
func (vr *ValidatorRegistry) GetByIndex(idx ValidatorIndex) (*ValidatorEntry, error) {
	vr.mu.RLock()
	defer vr.mu.RUnlock()
	if int(idx) >= len(vr.validators) {
		return nil, ErrValidatorNotFound
	}
	v := *vr.validators[idx]
	return &v, nil
}

// Size returns the total number of validators in the registry.
func (vr *ValidatorRegistry) Size() int {
	vr.mu.RLock()
	defer vr.mu.RUnlock()
	return len(vr.validators)
}

// ActiveIndicesAt returns the sorted indices of all active validators at the
// given epoch.
func (vr *ValidatorRegistry) ActiveIndicesAt(epoch Epoch) []ValidatorIndex {
	vr.mu.RLock()
	defer vr.mu.RUnlock()

	var indices []ValidatorIndex
	for _, v := range vr.validators {
		if v.IsActiveAt(epoch) {
			indices = append(indices, v.Index)
		}
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}

// ActiveCountAt returns the number of active validators at the given epoch.
func (vr *ValidatorRegistry) ActiveCountAt(epoch Epoch) int {
	return len(vr.ActiveIndicesAt(epoch))
}

// TotalEffectiveBalanceAt returns the sum of effective balances for all
// active validators at the given epoch. Returns at least
// EffectiveBalanceIncrement to prevent division by zero.
func (vr *ValidatorRegistry) TotalEffectiveBalanceAt(epoch Epoch) uint64 {
	vr.mu.RLock()
	defer vr.mu.RUnlock()

	var total uint64
	for _, v := range vr.validators {
		if v.IsActiveAt(epoch) {
			total += v.EffectiveBalance
		}
	}
	if total < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return total
}

// EffectiveBalances returns a slice of effective balances indexed by validator
// index. Used for proposer duty computation.
func (vr *ValidatorRegistry) EffectiveBalances() []uint64 {
	vr.mu.RLock()
	defer vr.mu.RUnlock()

	balances := make([]uint64, len(vr.validators))
	for i, v := range vr.validators {
		balances[i] = v.EffectiveBalance
	}
	return balances
}

// ComputeChurnLimit returns the maximum number of validators that can activate
// or exit per epoch, based on the active validator count:
//
//	max(MIN_PER_EPOCH_CHURN_LIMIT, active_count / CHURN_LIMIT_QUOTIENT)
func (vr *ValidatorRegistry) ComputeChurnLimit(epoch Epoch) uint64 {
	activeCount := uint64(vr.ActiveCountAt(epoch))
	churn := activeCount / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		return MinPerEpochChurnLimit
	}
	return churn
}

// ProcessActivationQueue activates eligible validators up to the churn limit.
// Validators are sorted by activation eligibility epoch (then index) and
// activated in order. Returns the indices of newly activated validators.
func (vr *ValidatorRegistry) ProcessActivationQueue(epoch Epoch, finalizedEpoch Epoch) []ValidatorIndex {
	vr.mu.Lock()
	defer vr.mu.Unlock()

	// Count active for churn limit.
	activeCount := 0
	for _, v := range vr.validators {
		if v.IsActiveAt(epoch) {
			activeCount++
		}
	}
	churn := uint64(activeCount) / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		churn = MinPerEpochChurnLimit
	}

	// Collect eligible candidates.
	type candidate struct {
		idx   ValidatorIndex
		eligE Epoch
	}
	var candidates []candidate
	for _, v := range vr.validators {
		if v.ActivationEligible != FarFutureEpoch &&
			v.ActivationEpoch == FarFutureEpoch &&
			v.ActivationEligible <= finalizedEpoch &&
			!v.Slashed &&
			v.EffectiveBalance >= MinActivationBalance {
			candidates = append(candidates, candidate{v.Index, v.ActivationEligible})
		}
	}

	// Sort by eligibility epoch, then by index.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].eligE != candidates[j].eligE {
			return candidates[i].eligE < candidates[j].eligE
		}
		return candidates[i].idx < candidates[j].idx
	})

	// Activate up to churn limit.
	activationEpoch := Epoch(uint64(epoch) + 1 + MaxSeedLookahead)
	limit := int(churn)
	if limit > len(candidates) {
		limit = len(candidates)
	}

	activated := make([]ValidatorIndex, 0, limit)
	for i := 0; i < limit; i++ {
		v := vr.validators[candidates[i].idx]
		v.ActivationEpoch = activationEpoch
		activated = append(activated, v.Index)
	}
	return activated
}

// ProcessExitQueue computes exit epochs for validators requesting exit,
// respecting the churn limit. It finds the earliest available exit epoch
// by examining the exit queue.
func (vr *ValidatorRegistry) ProcessExitQueue(indices []ValidatorIndex, epoch Epoch) {
	vr.mu.Lock()
	defer vr.mu.Unlock()

	// Count active for churn limit.
	activeCount := 0
	for _, v := range vr.validators {
		if v.IsActiveAt(epoch) {
			activeCount++
		}
	}
	churn := uint64(activeCount) / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		churn = MinPerEpochChurnLimit
	}

	// Find the current exit queue epoch and count.
	exitQueueEpoch := Epoch(uint64(epoch) + 1 + MaxSeedLookahead)
	exitQueueChurn := uint64(0)
	for _, v := range vr.validators {
		if v.ExitEpoch != FarFutureEpoch {
			if v.ExitEpoch > exitQueueEpoch {
				exitQueueEpoch = v.ExitEpoch
				exitQueueChurn = 1
			} else if v.ExitEpoch == exitQueueEpoch {
				exitQueueChurn++
			}
		}
	}

	for _, idx := range indices {
		if int(idx) >= len(vr.validators) {
			continue
		}
		v := vr.validators[idx]
		if v.ExitEpoch != FarFutureEpoch || !v.IsActiveAt(epoch) {
			continue
		}

		if exitQueueChurn >= churn {
			exitQueueEpoch++
			exitQueueChurn = 0
		}

		v.ExitEpoch = exitQueueEpoch
		v.WithdrawableEpoch = Epoch(uint64(v.ExitEpoch) + MinValidatorWithdrawDelay)
		exitQueueChurn++
	}
}

// UpdateEffectiveBalances recomputes effective balances for all validators
// using hysteresis to prevent oscillation. This should be called at each
// epoch boundary.
func (vr *ValidatorRegistry) UpdateEffectiveBalances() {
	vr.mu.Lock()
	defer vr.mu.Unlock()

	for _, v := range vr.validators {
		v.EffectiveBalance = ComputeEffectiveBalance(v.Balance, v.EffectiveBalance)
	}
}

// ComputeCommittees computes committee assignments for all slots in the given
// epoch using a seed-based shuffle. Results are cached.
func (vr *ValidatorRegistry) ComputeCommittees(epoch Epoch, seed [32]byte) {
	vr.mu.Lock()
	defer vr.mu.Unlock()

	// Check cache.
	if _, ok := vr.committeeCache[epoch]; ok {
		return
	}

	active := make([]ValidatorIndex, 0)
	for _, v := range vr.validators {
		if v.IsActiveAt(epoch) {
			active = append(active, v.Index)
		}
	}
	if len(active) == 0 {
		return
	}

	// Shuffle the active indices deterministically.
	shuffled := shuffleValidatorIndices(active, seed)

	spe := vr.config.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}

	// Compute committees per slot.
	committeesPerSlot := uint64(len(shuffled)) / (spe * uint64(vr.config.TargetCommitteeSize))
	if committeesPerSlot == 0 {
		committeesPerSlot = 1
	}
	if committeesPerSlot > uint64(vr.config.MaxCommitteesPerSlot) {
		committeesPerSlot = uint64(vr.config.MaxCommitteesPerSlot)
	}

	slotMap := make(map[Slot]map[uint64][]ValidatorIndex)
	startSlot := Slot(uint64(epoch) * spe)
	offset := 0

	totalCommittees := spe * committeesPerSlot
	membersPerCommittee := len(shuffled) / int(totalCommittees)
	remainder := len(shuffled) % int(totalCommittees)

	for s := uint64(0); s < spe; s++ {
		slot := Slot(uint64(startSlot) + s)
		slotMap[slot] = make(map[uint64][]ValidatorIndex)

		for c := uint64(0); c < committeesPerSlot; c++ {
			size := membersPerCommittee
			if int(s*committeesPerSlot+c) < remainder {
				size++
			}
			if offset+size > len(shuffled) {
				size = len(shuffled) - offset
			}
			if size <= 0 {
				continue
			}

			members := make([]ValidatorIndex, size)
			copy(members, shuffled[offset:offset+size])
			slotMap[slot][c] = members
			offset += size
		}
	}

	vr.committeeCache[epoch] = slotMap
}

// GetCommittee returns the committee members for the given slot and committee
// index. Returns nil if no committee is cached.
func (vr *ValidatorRegistry) GetCommittee(epoch Epoch, slot Slot, committeeIdx uint64) []ValidatorIndex {
	vr.mu.RLock()
	defer vr.mu.RUnlock()

	if epochSlots, ok := vr.committeeCache[epoch]; ok {
		if committees, ok := epochSlots[slot]; ok {
			if members, ok := committees[committeeIdx]; ok {
				result := make([]ValidatorIndex, len(members))
				copy(result, members)
				return result
			}
		}
	}
	return nil
}

// ClearCommitteeCache removes all cached committee assignments.
func (vr *ValidatorRegistry) ClearCommitteeCache() {
	vr.mu.Lock()
	vr.committeeCache = make(map[Epoch]map[Slot]map[uint64][]ValidatorIndex)
	vr.mu.Unlock()
}

// shuffleValidatorIndices performs a deterministic Fisher-Yates shuffle of
// validator indices using the given seed via Keccak256 hashing.
func shuffleValidatorIndices(indices []ValidatorIndex, seed [32]byte) []ValidatorIndex {
	n := len(indices)
	if n <= 1 {
		return indices
	}

	shuffled := make([]ValidatorIndex, n)
	copy(shuffled, indices)

	for i := n - 1; i > 0; i-- {
		var buf [40]byte
		copy(buf[:32], seed[:])
		binary.BigEndian.PutUint64(buf[32:], uint64(i))

		h := crypto.Keccak256(buf[:])
		j := binary.BigEndian.Uint64(h[:8]) % uint64(i+1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled
}
