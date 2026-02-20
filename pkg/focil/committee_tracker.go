// committee_tracker.go implements FOCIL committee tracking for inclusion list
// committee selection, member rotation, duty verification, and quorum checking.
//
// Per EIP-7805, each slot has an IL committee of IL_COMMITTEE_SIZE validators
// selected pseudo-randomly. The committee members construct inclusion lists,
// and blocks must satisfy a 2/3 quorum of these lists to be considered valid
// by the fork-choice rule.
//
// The CommitteeTracker provides:
//   - Deterministic committee selection per slot via shuffled validator indices
//   - Committee member rotation tracking across slots
//   - Duty assignment lookup and caching for fast repeated queries
//   - Quorum verification (2/3 of committee members must submit lists)
//   - Signed list aggregation and committee root computation
package focil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/sha3"
)

// Committee tracker errors.
var (
	ErrTrackerNoValidators     = errors.New("focil/committee: no active validators")
	ErrTrackerSlotZero         = errors.New("focil/committee: slot must be > 0")
	ErrTrackerNotCommittee     = errors.New("focil/committee: validator not in committee")
	ErrTrackerQuorumNotReached = errors.New("focil/committee: quorum not reached")
	ErrTrackerDuplicateList    = errors.New("focil/committee: duplicate list from validator")
	ErrTrackerSlotMismatch     = errors.New("focil/committee: list slot does not match")
)

// QuorumNumerator and QuorumDenominator define the 2/3 quorum threshold.
const (
	QuorumNumerator   = 2
	QuorumDenominator = 3
)

// CommitteeDuty represents a single validator's committee assignment for a slot.
type CommitteeDuty struct {
	// Slot is the beacon slot for this duty.
	Slot uint64

	// ValidatorIndex is the validator's index in the global validator set.
	ValidatorIndex uint64

	// CommitteePosition is the validator's position within the IL committee.
	CommitteePosition int
}

// SlotCommittee holds the full committee for a single slot.
type SlotCommittee struct {
	// Slot is the beacon slot.
	Slot uint64

	// Members is the ordered list of validator indices in the committee.
	Members []uint64

	// Root is the Merkle root commitment of the committee members.
	Root types.Hash
}

// QuorumStatus reports the inclusion list quorum state for a slot.
type QuorumStatus struct {
	// Slot is the beacon slot.
	Slot uint64

	// CommitteeSize is the total number of committee members.
	CommitteeSize int

	// ListsReceived is the number of valid inclusion lists received.
	ListsReceived int

	// QuorumThreshold is the minimum lists needed (ceil(2/3 * committee)).
	QuorumThreshold int

	// QuorumReached is true if ListsReceived >= QuorumThreshold.
	QuorumReached bool

	// SubmittedBy lists the validator indices that submitted lists.
	SubmittedBy []uint64
}

// CommitteeTrackerConfig configures the committee tracker.
type CommitteeTrackerConfig struct {
	// CommitteeSize is the number of validators per IL committee.
	CommitteeSize int

	// CacheSlots is the number of recent slots to cache committee assignments.
	CacheSlots int
}

// DefaultCommitteeTrackerConfig returns sensible defaults.
func DefaultCommitteeTrackerConfig() CommitteeTrackerConfig {
	return CommitteeTrackerConfig{
		CommitteeSize: IL_COMMITTEE_SIZE,
		CacheSlots:    64,
	}
}

// CommitteeTracker tracks FOCIL inclusion list committees across slots.
// It supports deterministic committee selection, duty lookup, and quorum
// checking. All public methods are safe for concurrent use.
type CommitteeTracker struct {
	mu     sync.RWMutex
	config CommitteeTrackerConfig

	// activeValidators is the set of active validator indices.
	activeValidators []uint64

	// committeeCache maps slot -> computed committee.
	committeeCache map[uint64]*SlotCommittee

	// receivedLists tracks which validators submitted lists per slot.
	// slot -> set of validator indices.
	receivedLists map[uint64]map[uint64]bool
}

// NewCommitteeTracker creates a new committee tracker.
func NewCommitteeTracker(config CommitteeTrackerConfig, validators []uint64) *CommitteeTracker {
	if config.CommitteeSize <= 0 {
		config.CommitteeSize = IL_COMMITTEE_SIZE
	}
	if config.CacheSlots <= 0 {
		config.CacheSlots = 64
	}
	vals := make([]uint64, len(validators))
	copy(vals, validators)
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })

	return &CommitteeTracker{
		config:           config,
		activeValidators: vals,
		committeeCache:   make(map[uint64]*SlotCommittee),
		receivedLists:    make(map[uint64]map[uint64]bool),
	}
}

// GetCommittee returns the IL committee for a given slot. The result is
// deterministic: the same slot and validator set always produces the same
// committee. Results are cached for repeated lookups.
func (ct *CommitteeTracker) GetCommittee(slot uint64) (*SlotCommittee, error) {
	if slot == 0 {
		return nil, ErrTrackerSlotZero
	}

	ct.mu.RLock()
	if cached, ok := ct.committeeCache[slot]; ok {
		ct.mu.RUnlock()
		return cached, nil
	}
	ct.mu.RUnlock()

	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Double-check after acquiring write lock.
	if cached, ok := ct.committeeCache[slot]; ok {
		return cached, nil
	}

	if len(ct.activeValidators) == 0 {
		return nil, ErrTrackerNoValidators
	}

	committee := ct.selectCommittee(slot)
	root := computeCommitteeRoot(slot, committee)

	sc := &SlotCommittee{
		Slot:    slot,
		Members: committee,
		Root:    root,
	}

	ct.committeeCache[slot] = sc
	ct.evictOldCacheLocked(slot)

	return sc, nil
}

// IsCommitteeMember returns true if the given validator is in the IL
// committee for the specified slot.
func (ct *CommitteeTracker) IsCommitteeMember(validatorIndex uint64, slot uint64) bool {
	committee, err := ct.GetCommittee(slot)
	if err != nil {
		return false
	}
	for _, idx := range committee.Members {
		if idx == validatorIndex {
			return true
		}
	}
	return false
}

// GetDuty returns the committee duty for a specific validator at a slot.
// Returns ErrTrackerNotCommittee if the validator is not in the committee.
func (ct *CommitteeTracker) GetDuty(validatorIndex uint64, slot uint64) (*CommitteeDuty, error) {
	committee, err := ct.GetCommittee(slot)
	if err != nil {
		return nil, err
	}
	for pos, idx := range committee.Members {
		if idx == validatorIndex {
			return &CommitteeDuty{
				Slot:              slot,
				ValidatorIndex:    validatorIndex,
				CommitteePosition: pos,
			}, nil
		}
	}
	return nil, fmt.Errorf("%w: validator %d at slot %d", ErrTrackerNotCommittee, validatorIndex, slot)
}

// RecordList records that a validator submitted an inclusion list for a slot.
// Returns an error if the validator is not a committee member or has already
// submitted a list for this slot.
func (ct *CommitteeTracker) RecordList(validatorIndex uint64, slot uint64) error {
	if slot == 0 {
		return ErrTrackerSlotZero
	}
	if !ct.IsCommitteeMember(validatorIndex, slot) {
		return fmt.Errorf("%w: validator %d at slot %d", ErrTrackerNotCommittee, validatorIndex, slot)
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	if ct.receivedLists[slot] == nil {
		ct.receivedLists[slot] = make(map[uint64]bool)
	}
	if ct.receivedLists[slot][validatorIndex] {
		return fmt.Errorf("%w: validator %d at slot %d", ErrTrackerDuplicateList, validatorIndex, slot)
	}
	ct.receivedLists[slot][validatorIndex] = true
	return nil
}

// GetQuorumStatus returns the current quorum status for a slot.
func (ct *CommitteeTracker) GetQuorumStatus(slot uint64) (*QuorumStatus, error) {
	committee, err := ct.GetCommittee(slot)
	if err != nil {
		return nil, err
	}

	ct.mu.RLock()
	defer ct.mu.RUnlock()

	threshold := quorumThreshold(len(committee.Members))
	received := ct.receivedLists[slot]
	listCount := len(received)

	var submitters []uint64
	for idx := range received {
		submitters = append(submitters, idx)
	}
	sort.Slice(submitters, func(i, j int) bool { return submitters[i] < submitters[j] })

	return &QuorumStatus{
		Slot:            slot,
		CommitteeSize:   len(committee.Members),
		ListsReceived:   listCount,
		QuorumThreshold: threshold,
		QuorumReached:   listCount >= threshold,
		SubmittedBy:     submitters,
	}, nil
}

// CheckQuorum returns nil if the 2/3 quorum is reached for the slot,
// or ErrTrackerQuorumNotReached otherwise.
func (ct *CommitteeTracker) CheckQuorum(slot uint64) error {
	status, err := ct.GetQuorumStatus(slot)
	if err != nil {
		return err
	}
	if !status.QuorumReached {
		return fmt.Errorf("%w: have %d/%d, need %d",
			ErrTrackerQuorumNotReached, status.ListsReceived, status.CommitteeSize, status.QuorumThreshold)
	}
	return nil
}

// PruneBefore removes cached data for slots before the given slot.
// Returns the number of entries removed.
func (ct *CommitteeTracker) PruneBefore(slot uint64) int {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	pruned := 0
	for s := range ct.committeeCache {
		if s < slot {
			delete(ct.committeeCache, s)
			pruned++
		}
	}
	for s := range ct.receivedLists {
		if s < slot {
			delete(ct.receivedLists, s)
		}
	}
	return pruned
}

// selectCommittee deterministically selects committee members for a slot.
// Uses a Fisher-Yates shuffle seeded by keccak256(slot) to pick members
// from the active validator set.
// Caller must hold ct.mu (at least read lock for activeValidators).
func (ct *CommitteeTracker) selectCommittee(slot uint64) []uint64 {
	n := len(ct.activeValidators)
	if n == 0 {
		return nil
	}

	size := ct.config.CommitteeSize
	if size > n {
		size = n
	}

	// Seed from slot.
	seed := slotSeed(slot)

	// Use a hash-chain shuffle to pick `size` unique indices.
	selected := make([]uint64, 0, size)
	used := make(map[int]bool, size)

	for counter := uint64(0); len(selected) < size; counter++ {
		h := sha3.NewLegacyKeccak256()
		h.Write(seed[:])
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		h.Write(cBuf[:])
		digest := h.Sum(nil)

		idx := int(binary.LittleEndian.Uint64(digest[:8]) % uint64(n))
		if !used[idx] {
			used[idx] = true
			selected = append(selected, ct.activeValidators[idx])
		}
	}

	sort.Slice(selected, func(i, j int) bool { return selected[i] < selected[j] })
	return selected
}

// evictOldCacheLocked removes the oldest cache entries if the cache exceeds
// the configured size. Caller must hold ct.mu write lock.
func (ct *CommitteeTracker) evictOldCacheLocked(currentSlot uint64) {
	if len(ct.committeeCache) <= ct.config.CacheSlots {
		return
	}
	cutoff := currentSlot - uint64(ct.config.CacheSlots)
	for s := range ct.committeeCache {
		if s < cutoff {
			delete(ct.committeeCache, s)
		}
	}
}

// quorumThreshold computes ceil(2/3 * committeeSize).
func quorumThreshold(committeeSize int) int {
	return (committeeSize*QuorumNumerator + QuorumDenominator - 1) / QuorumDenominator
}

// slotSeed computes keccak256(slot) as a 32-byte seed.
func slotSeed(slot uint64) [32]byte {
	h := sha3.NewLegacyKeccak256()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	var seed [32]byte
	h.Sum(seed[:0])
	return seed
}

// computeCommitteeRoot computes a Merkle-like root for the committee:
// root = keccak256(slot || member0 || member1 || ... || memberN)
func computeCommitteeRoot(slot uint64, members []uint64) types.Hash {
	h := sha3.NewLegacyKeccak256()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], slot)
	h.Write(buf[:])
	for _, m := range members {
		binary.LittleEndian.PutUint64(buf[:], m)
		h.Write(buf[:])
	}
	var root types.Hash
	h.Sum(root[:0])
	return root
}
