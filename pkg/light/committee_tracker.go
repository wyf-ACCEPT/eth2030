// committee_tracker.go implements sync committee tracking with period-based
// rotation, signature aggregation threshold checking, and committee update
// verification for the light client protocol.
//
// The tracker maintains a sliding window of committees across periods,
// validates committee transitions, and monitors participation rates to
// ensure the light client is following a sufficiently attested chain.
//
// Part of the CL roadmap: light client sync committee verification.
package light

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Committee tracker errors.
var (
	ErrTrackerNotInitialized   = errors.New("committee_tracker: tracker not initialized")
	ErrTrackerAlreadyTracked   = errors.New("committee_tracker: period already tracked")
	ErrTrackerPeriodGap        = errors.New("committee_tracker: non-sequential period transition")
	ErrTrackerNoCommittee      = errors.New("committee_tracker: no committee for period")
	ErrTrackerThresholdNotMet  = errors.New("committee_tracker: aggregation threshold not met")
	ErrTrackerInvalidUpdate    = errors.New("committee_tracker: invalid committee update")
	ErrTrackerRootMismatch     = errors.New("committee_tracker: committee root mismatch")
	ErrTrackerWindowFull       = errors.New("committee_tracker: retention window full")
	ErrTrackerNilCommittee     = errors.New("committee_tracker: nil committee provided")
)

// CommitteeTrackerConfig configures the committee tracker behavior.
type CommitteeTrackerConfig struct {
	// MaxRetainedPeriods is the maximum number of past periods to retain.
	MaxRetainedPeriods int

	// MinParticipationRate is the minimum fraction (0-100) of committee
	// members that must sign for an update to be accepted.
	MinParticipationRate int

	// VerifyRoots enables committee root verification on transitions.
	VerifyRoots bool
}

// DefaultCommitteeTrackerConfig returns a config with sensible defaults.
func DefaultCommitteeTrackerConfig() CommitteeTrackerConfig {
	return CommitteeTrackerConfig{
		MaxRetainedPeriods:   8,
		MinParticipationRate: 67, // 2/3 threshold
		VerifyRoots:          true,
	}
}

// TrackedCommittee holds a sync committee along with tracking metadata.
type TrackedCommittee struct {
	Committee      *SyncCommittee
	Period         uint64
	Root           types.Hash
	PeakSigners    int
	UpdateCount    uint64
	LastAttestSlot uint64
}

// ParticipationRecord records a signature aggregation event for analysis.
type ParticipationRecord struct {
	Period     uint64
	Slot       uint64
	Signers    int
	Total      int
	Sufficient bool
}

// CommitteeTracker manages sync committee lifecycle across periods.
// It tracks current and recent committees, validates transitions, and
// monitors participation rates. Thread-safe.
type CommitteeTracker struct {
	mu     sync.RWMutex
	config CommitteeTrackerConfig

	// committees maps period -> tracked committee.
	committees map[uint64]*TrackedCommittee

	// currentPeriod is the most recent period being tracked.
	currentPeriod uint64

	// initialized indicates whether the tracker has been bootstrapped.
	initialized bool

	// participation records the last N participation events.
	participation []ParticipationRecord
	maxRecords    int

	// stats for monitoring.
	totalUpdates  uint64
	totalFailures uint64
}

// NewCommitteeTracker creates a new committee tracker with the given config.
func NewCommitteeTracker(config CommitteeTrackerConfig) *CommitteeTracker {
	if config.MaxRetainedPeriods <= 0 {
		config.MaxRetainedPeriods = 8
	}
	if config.MinParticipationRate <= 0 || config.MinParticipationRate > 100 {
		config.MinParticipationRate = 67
	}
	return &CommitteeTracker{
		config:     config,
		committees: make(map[uint64]*TrackedCommittee),
		maxRecords: config.MaxRetainedPeriods * 32,
	}
}

// Initialize bootstraps the tracker with an initial committee for the given period.
func (ct *CommitteeTracker) Initialize(committee *SyncCommittee, period uint64) error {
	if committee == nil {
		return ErrTrackerNilCommittee
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()

	root := ComputeCommitteeRoot(committee.Pubkeys)
	ct.committees[period] = &TrackedCommittee{
		Committee: committee,
		Period:    period,
		Root:      root,
	}
	ct.currentPeriod = period
	ct.initialized = true
	return nil
}

// IsInitialized returns whether the tracker has been bootstrapped.
func (ct *CommitteeTracker) IsInitialized() bool {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.initialized
}

// CurrentPeriod returns the most recently tracked period.
func (ct *CommitteeTracker) CurrentPeriod() uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.currentPeriod
}

// GetCommittee returns the tracked committee for the given period.
func (ct *CommitteeTracker) GetCommittee(period uint64) (*TrackedCommittee, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	if !ct.initialized {
		return nil, ErrTrackerNotInitialized
	}
	tc, ok := ct.committees[period]
	if !ok {
		return nil, ErrTrackerNoCommittee
	}
	return tc, nil
}

// AdvancePeriod processes a committee rotation to the next period. The new
// committee must be for currentPeriod+1. If VerifyRoots is enabled, the
// provided root is validated against the computed committee root.
func (ct *CommitteeTracker) AdvancePeriod(next *SyncCommittee, expectedRoot types.Hash) error {
	if next == nil {
		return ErrTrackerNilCommittee
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if !ct.initialized {
		return ErrTrackerNotInitialized
	}

	expectedPeriod := ct.currentPeriod + 1
	if next.Period != expectedPeriod {
		ct.totalFailures++
		return ErrTrackerPeriodGap
	}

	// Verify committee root if enabled.
	computedRoot := ComputeCommitteeRoot(next.Pubkeys)
	if ct.config.VerifyRoots && !expectedRoot.IsZero() {
		if computedRoot != expectedRoot {
			ct.totalFailures++
			return ErrTrackerRootMismatch
		}
	}

	// Check if already tracked.
	if _, exists := ct.committees[expectedPeriod]; exists {
		return ErrTrackerAlreadyTracked
	}

	// Add new committee.
	ct.committees[expectedPeriod] = &TrackedCommittee{
		Committee: next,
		Period:    expectedPeriod,
		Root:      computedRoot,
	}
	ct.currentPeriod = expectedPeriod
	ct.totalUpdates++

	// Evict old periods.
	ct.evictOldPeriods()
	return nil
}

// CheckAggregationThreshold validates that a signature has sufficient
// participation for the given period. Returns nil if the threshold is met.
func (ct *CommitteeTracker) CheckAggregationThreshold(
	period uint64,
	committeeBits []byte,
	slot uint64,
) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	if !ct.initialized {
		return ErrTrackerNotInitialized
	}

	tc, ok := ct.committees[period]
	if !ok {
		return ErrTrackerNoCommittee
	}

	signers := countBits(committeeBits)
	total := len(tc.Committee.Pubkeys)

	// Record participation.
	sufficient := ct.meetsThreshold(signers, total)
	record := ParticipationRecord{
		Period:     period,
		Slot:       slot,
		Signers:    signers,
		Total:      total,
		Sufficient: sufficient,
	}
	ct.addParticipationRecord(record)

	// Update peak signers.
	if signers > tc.PeakSigners {
		tc.PeakSigners = signers
	}
	tc.UpdateCount++
	if slot > tc.LastAttestSlot {
		tc.LastAttestSlot = slot
	}

	if !sufficient {
		ct.totalFailures++
		return ErrTrackerThresholdNotMet
	}
	return nil
}

// VerifyCommitteeUpdate validates a committee update by checking the
// signature from the current committee over the next committee's root.
// Uses the same Keccak256 binding scheme as VerifySyncCommitteeSignature.
func (ct *CommitteeTracker) VerifyCommitteeUpdate(
	currentPeriod uint64,
	nextCommittee *SyncCommittee,
	committeeBits []byte,
	signature []byte,
) error {
	if nextCommittee == nil {
		return ErrTrackerNilCommittee
	}
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	if !ct.initialized {
		return ErrTrackerNotInitialized
	}

	tc, ok := ct.committees[currentPeriod]
	if !ok {
		return ErrTrackerNoCommittee
	}

	// The signing root is the root of the next committee.
	nextRoot := ComputeCommitteeRoot(nextCommittee.Pubkeys)

	// Verify signature using current committee.
	if err := VerifySyncCommitteeSignature(
		tc.Committee, nextRoot, committeeBits, signature,
	); err != nil {
		return ErrTrackerInvalidUpdate
	}
	return nil
}

// TrackedPeriods returns all currently tracked period numbers.
func (ct *CommitteeTracker) TrackedPeriods() []uint64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	periods := make([]uint64, 0, len(ct.committees))
	for p := range ct.committees {
		periods = append(periods, p)
	}
	return periods
}

// ParticipationHistory returns the recent participation records.
func (ct *CommitteeTracker) ParticipationHistory() []ParticipationRecord {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	result := make([]ParticipationRecord, len(ct.participation))
	copy(result, ct.participation)
	return result
}

// AverageParticipation computes the average participation rate across
// all recorded events. Returns 0 if no records exist.
func (ct *CommitteeTracker) AverageParticipation() float64 {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	if len(ct.participation) == 0 {
		return 0
	}
	var total float64
	for _, r := range ct.participation {
		if r.Total > 0 {
			total += float64(r.Signers) / float64(r.Total)
		}
	}
	return total / float64(len(ct.participation))
}

// Stats returns aggregate tracker statistics.
func (ct *CommitteeTracker) Stats() (updates, failures uint64) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return ct.totalUpdates, ct.totalFailures
}

// CommitteeRootForPeriod computes and returns the committee root for
// the given period, or an error if no committee exists for that period.
func (ct *CommitteeTracker) CommitteeRootForPeriod(period uint64) (types.Hash, error) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	tc, ok := ct.committees[period]
	if !ok {
		return types.Hash{}, ErrTrackerNoCommittee
	}
	return tc.Root, nil
}

// ComputeTransitionProof generates a Keccak256 proof binding two consecutive
// committee roots. Useful for proving committee continuity in light client
// bootstrap scenarios.
func ComputeTransitionProof(currentRoot, nextRoot types.Hash, period uint64) types.Hash {
	data := make([]byte, 0, 72)
	data = append(data, currentRoot[:]...)
	data = append(data, nextRoot[:]...)
	data = append(data, byte(period>>56), byte(period>>48), byte(period>>40),
		byte(period>>32), byte(period>>24), byte(period>>16),
		byte(period>>8), byte(period))
	return crypto.Keccak256Hash(data)
}

// meetsThreshold checks if signers/total meets the configured minimum rate.
func (ct *CommitteeTracker) meetsThreshold(signers, total int) bool {
	if total == 0 {
		return false
	}
	// signers * 100 >= total * minRate (avoid floating point)
	return signers*100 >= total*ct.config.MinParticipationRate
}

// addParticipationRecord appends a record and trims to max capacity.
func (ct *CommitteeTracker) addParticipationRecord(r ParticipationRecord) {
	ct.participation = append(ct.participation, r)
	if len(ct.participation) > ct.maxRecords {
		ct.participation = ct.participation[1:]
	}
}

// evictOldPeriods removes committees older than the retention window.
func (ct *CommitteeTracker) evictOldPeriods() {
	if len(ct.committees) <= ct.config.MaxRetainedPeriods {
		return
	}
	minPeriod := ct.currentPeriod - uint64(ct.config.MaxRetainedPeriods) + 1
	for p := range ct.committees {
		if p < minPeriod {
			delete(ct.committees, p)
		}
	}
}
