// sync_committee_tracker.go tracks sync committee rotations and validates
// sync committee signatures across periods for the light client protocol.
//
// Unlike the existing CommitteeTracker (which tracks SyncCommittee objects
// with pubkey slices), this tracker works with CommitteeMember structs that
// carry validator index, typed pubkey, and weight. It is designed for
// higher-level light client logic that needs per-member metadata.
//
// Part of the CL roadmap: light client sync committee verification.
package light

import (
	"errors"
	"sort"
	"sync"
)

// SyncCommitteeTracker-specific errors.
var (
	ErrSCTrackerPeriodNotFound  = errors.New("sync_committee_tracker: period not found")
	ErrSCTrackerEmptyCommittee  = errors.New("sync_committee_tracker: empty committee")
	ErrSCTrackerNilMembers      = errors.New("sync_committee_tracker: nil members slice")
	ErrSCTrackerInvalidUpdate   = errors.New("sync_committee_tracker: invalid sync update")
	ErrSCTrackerNoFinality      = errors.New("sync_committee_tracker: update does not advance finality")
	ErrSCTrackerInsufficientSig = errors.New("sync_committee_tracker: insufficient signature participation")
)

// CommitteeMember represents a single member of a sync committee with
// validator index, BLS public key, and weight.
type CommitteeMember struct {
	// ValidatorIndex is the beacon chain validator index.
	ValidatorIndex uint64

	// Pubkey is the 48-byte BLS12-381 public key.
	Pubkey [48]byte

	// Weight is the effective balance or voting weight of this member.
	Weight uint64
}

// SyncUpdate carries the data for a sync committee update that the tracker
// must validate. It includes the attested and finalized slots, participation
// bits, aggregate signature, and the next committee if a rotation occurs.
type SyncUpdate struct {
	// AttestedSlot is the slot of the header attested by the sync committee.
	AttestedSlot uint64

	// FinalizedSlot is the slot of the finalized header referenced by
	// the update.
	FinalizedSlot uint64

	// SyncCommitteeBits is the participation bitfield indicating which
	// committee members signed.
	SyncCommitteeBits []byte

	// Signature is the 96-byte aggregate BLS signature.
	Signature [96]byte

	// NextCommittee is the next sync committee if a rotation occurs.
	// May be nil if no rotation is included in this update.
	NextCommittee []*CommitteeMember
}

// CommitteeStats aggregates statistics about tracked committees.
type CommitteeStats struct {
	// Periods is the number of periods currently tracked.
	Periods int

	// TotalMembers is the total number of committee members across all
	// tracked periods.
	TotalMembers int

	// AvgParticipation is the average participation rate across all
	// periods with recorded participation data (0.0 to 1.0).
	AvgParticipation float64
}

// trackedPeriod stores committee members and participation data for a period.
type trackedPeriod struct {
	members         []*CommitteeMember
	participantBits []byte
	totalWeight     uint64
}

// SyncCommitteeTracker tracks sync committee members across periods,
// validates sync updates, and monitors participation rates.
// All public methods are safe for concurrent use.
type SyncCommitteeTracker struct {
	mu         sync.RWMutex
	maxPeriods int
	periods    map[uint64]*trackedPeriod
	current    uint64
}

// NewSyncCommitteeTracker creates a new tracker that retains at most
// maxPeriods committee periods. If maxPeriods < 1, defaults to 8.
func NewSyncCommitteeTracker(maxPeriods int) *SyncCommitteeTracker {
	if maxPeriods < 1 {
		maxPeriods = 8
	}
	return &SyncCommitteeTracker{
		maxPeriods: maxPeriods,
		periods:    make(map[uint64]*trackedPeriod),
	}
}

// RegisterCommittee registers a set of committee members for a given period.
// If the period already exists, its members are overwritten. The period is
// set as the current period if it is >= the existing current period.
func (t *SyncCommitteeTracker) RegisterCommittee(period uint64, members []*CommitteeMember) error {
	if members == nil {
		return ErrSCTrackerNilMembers
	}
	if len(members) == 0 {
		return ErrSCTrackerEmptyCommittee
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Copy members to avoid external mutation.
	membersCopy := make([]*CommitteeMember, len(members))
	var totalWeight uint64
	for i, m := range members {
		cp := *m
		membersCopy[i] = &cp
		totalWeight += m.Weight
	}

	t.periods[period] = &trackedPeriod{
		members:     membersCopy,
		totalWeight: totalWeight,
	}

	if period >= t.current {
		t.current = period
	}

	// Evict old periods if over capacity.
	t.evictOldPeriods()

	return nil
}

// GetCommittee returns the committee members for the given period.
func (t *SyncCommitteeTracker) GetCommittee(period uint64) ([]*CommitteeMember, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tp, ok := t.periods[period]
	if !ok {
		return nil, ErrSCTrackerPeriodNotFound
	}

	// Return a copy.
	result := make([]*CommitteeMember, len(tp.members))
	for i, m := range tp.members {
		cp := *m
		result[i] = &cp
	}
	return result, nil
}

// CurrentPeriod returns the most recently registered period number.
func (t *SyncCommitteeTracker) CurrentPeriod() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.current
}

// ValidateUpdate validates a sync committee update. It checks that:
// - The finalized slot is less than or equal to the attested slot.
// - The participation bitfield has enough signers (supermajority).
// - The committee for the attested slot's period exists.
// - If a next committee is provided, it is non-empty.
func (t *SyncCommitteeTracker) ValidateUpdate(update *SyncUpdate) error {
	if update == nil {
		return ErrSCTrackerInvalidUpdate
	}
	if update.FinalizedSlot > update.AttestedSlot {
		return ErrSCTrackerNoFinality
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	// Determine which period the attested slot falls in.
	period := update.AttestedSlot / SlotsPerSyncCommitteePeriod

	tp, ok := t.periods[period]
	if !ok {
		return ErrSCTrackerPeriodNotFound
	}

	// Check participation.
	signerCount := countSCTrackerBits(update.SyncCommitteeBits)
	committeeSize := len(tp.members)
	if committeeSize == 0 || signerCount*3 < committeeSize*2 {
		return ErrSCTrackerInsufficientSig
	}

	// If a next committee is provided, it must be non-empty.
	if update.NextCommittee != nil && len(update.NextCommittee) == 0 {
		return ErrSCTrackerEmptyCommittee
	}

	return nil
}

// ParticipationRate returns the participation rate for a given period based
// on the recorded participation bits. Returns 0.0 if no participation has
// been recorded or the period is unknown.
func (t *SyncCommitteeTracker) ParticipationRate(period uint64) float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tp, ok := t.periods[period]
	if !ok || tp.participantBits == nil {
		return 0.0
	}

	committeeSize := len(tp.members)
	if committeeSize == 0 {
		return 0.0
	}

	signers := countSCTrackerBits(tp.participantBits)
	return float64(signers) / float64(committeeSize)
}

// UpdateParticipation records a participation bitfield for a given period.
// The bits are OR-ed with any existing participation data, so that
// cumulative participation is tracked.
func (t *SyncCommitteeTracker) UpdateParticipation(period uint64, participantBits []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()

	tp, ok := t.periods[period]
	if !ok {
		return
	}

	if tp.participantBits == nil {
		tp.participantBits = make([]byte, len(participantBits))
		copy(tp.participantBits, participantBits)
		return
	}

	// OR the bits together.
	minLen := len(tp.participantBits)
	if len(participantBits) < minLen {
		minLen = len(participantBits)
	}
	for i := 0; i < minLen; i++ {
		tp.participantBits[i] |= participantBits[i]
	}
	// If new bits are longer, extend.
	if len(participantBits) > len(tp.participantBits) {
		tp.participantBits = append(tp.participantBits, participantBits[len(tp.participantBits):]...)
	}
}

// PruneBefore removes all tracked periods with period number strictly less
// than the given period. Returns the number of periods removed.
func (t *SyncCommitteeTracker) PruneBefore(period uint64) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	pruned := 0
	for p := range t.periods {
		if p < period {
			delete(t.periods, p)
			pruned++
		}
	}
	return pruned
}

// Stats returns aggregate statistics about tracked committees.
func (t *SyncCommitteeTracker) Stats() *CommitteeStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := &CommitteeStats{
		Periods: len(t.periods),
	}

	var totalParticipation float64
	var participationCount int

	for _, tp := range t.periods {
		stats.TotalMembers += len(tp.members)

		if tp.participantBits != nil && len(tp.members) > 0 {
			signers := countSCTrackerBits(tp.participantBits)
			totalParticipation += float64(signers) / float64(len(tp.members))
			participationCount++
		}
	}

	if participationCount > 0 {
		stats.AvgParticipation = totalParticipation / float64(participationCount)
	}

	return stats
}

// PeriodCount returns the number of currently tracked periods.
func (t *SyncCommitteeTracker) PeriodCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.periods)
}

// --- Internal helpers ---

// evictOldPeriods removes the oldest periods if we exceed maxPeriods.
// Must be called with the write lock held.
func (t *SyncCommitteeTracker) evictOldPeriods() {
	if len(t.periods) <= t.maxPeriods {
		return
	}

	// Collect and sort periods.
	allPeriods := make([]uint64, 0, len(t.periods))
	for p := range t.periods {
		allPeriods = append(allPeriods, p)
	}
	sort.Slice(allPeriods, func(i, j int) bool { return allPeriods[i] < allPeriods[j] })

	// Remove oldest until within capacity.
	toRemove := len(allPeriods) - t.maxPeriods
	for i := 0; i < toRemove; i++ {
		delete(t.periods, allPeriods[i])
	}
}

// countSCTrackerBits counts set bits in a byte slice.
func countSCTrackerBits(data []byte) int {
	count := 0
	for _, b := range data {
		for i := 0; i < 8; i++ {
			if b&(1<<uint(i)) != 0 {
				count++
			}
		}
	}
	return count
}
