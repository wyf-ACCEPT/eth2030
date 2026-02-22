// Package consensus - sync committee management per Altair spec.
//
// Implements sync committee selection (effective-balance-weighted sampling),
// aggregate processing, participation tracking, reward distribution, and
// period-boundary rotation.
package consensus

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Sync committee constants per Altair spec.
const (
	// SyncCommitteeSize is the number of validators in a sync committee.
	SyncCommitteeSize = 512

	// EpochsPerSyncCommitteePeriod is the duration of one sync committee period.
	EpochsPerSyncCommitteePeriod = 256

	// SyncRewardWeight is the weight assigned to sync rewards (out of 64 total).
	SyncRewardWeight = 2

	// WeightDenominator is the total weight denominator for reward calculation.
	WeightDenominator = 64

	// ProposerWeight is the proposer reward weight.
	ProposerWeight = 8

	// SyncAggregateBitfieldSize is the byte-length of the sync committee bitfield.
	// 512 bits = 64 bytes.
	SyncAggregateBitfieldSize = SyncCommitteeSize / 8

	// maxRandomByte is 2^8 - 1, used in effective-balance-weighted sampling.
	maxRandomByte = 255
)

// Sync committee errors.
var (
	ErrSyncNoValidators      = errors.New("sync: no active validators for committee selection")
	ErrSyncBitfieldLength    = errors.New("sync: invalid sync aggregate bitfield length")
	ErrSyncNilState          = errors.New("sync: nil beacon state")
	ErrSyncCommitteeNotReady = errors.New("sync: current committee not initialized")
)

// SyncCommittee represents a sync committee with member pubkeys and an
// aggregate public key, per the Altair SyncCommittee container.
type SyncCommittee struct {
	Pubkeys      [SyncCommitteeSize]types.Hash
	AggregatePubkey types.Hash
}

// SyncAggregate represents the sync aggregate included in a beacon block body.
type SyncAggregate struct {
	SyncCommitteeBits      [SyncAggregateBitfieldSize]byte
	SyncCommitteeSignature types.Hash
}

// SyncCommitteeConfig holds configuration for the sync committee manager.
type SyncCommitteeConfig struct {
	SlotsPerEpoch    uint64
	CommitteeSize    int
	PeriodLength     uint64 // epochs per sync committee period
	BaseRewardPerInc uint64 // base reward per increment in Gwei
}

// DefaultSyncCommitteeConfig returns the mainnet default configuration.
func DefaultSyncCommitteeConfig() *SyncCommitteeConfig {
	return &SyncCommitteeConfig{
		SlotsPerEpoch:    32,
		CommitteeSize:    SyncCommitteeSize,
		PeriodLength:     EpochsPerSyncCommitteePeriod,
		BaseRewardPerInc: 64, // simplified base reward
	}
}

// SyncCommitteeManager manages current and next sync committees, processes
// sync aggregates, tracks participation, and distributes rewards.
// All public methods are thread-safe.
type SyncCommitteeManager struct {
	mu sync.RWMutex

	config  *SyncCommitteeConfig
	current *SyncCommittee
	next    *SyncCommittee

	// currentIndices maps position in the committee to validator index.
	currentIndices [SyncCommitteeSize]ValidatorIndex

	// Participation tracking for the current period.
	totalBitsSet   uint64 // total participation bits across all processed aggregates
	totalBitSlots  uint64 // total bit-slots (aggregates * committee size)

	// Reward tracking: accumulated rewards per validator index.
	rewards map[ValidatorIndex]uint64

	// Current period number (epoch / PeriodLength).
	currentPeriod uint64
}

// NewSyncCommitteeManager creates a new sync committee manager.
func NewSyncCommitteeManager(cfg *SyncCommitteeConfig) *SyncCommitteeManager {
	if cfg == nil {
		cfg = DefaultSyncCommitteeConfig()
	}
	return &SyncCommitteeManager{
		config:  cfg,
		rewards: make(map[ValidatorIndex]uint64),
	}
}

// ComputeSyncCommittee selects a sync committee from the beacon state for
// the given epoch using effective-balance-weighted sampling without replacement.
//
// The algorithm follows the Altair spec: iterate through shuffled validator
// indices and accept candidates probabilistically based on their effective
// balance relative to MaxEffectiveBalance.
func (m *SyncCommitteeManager) ComputeSyncCommittee(
	state *FullBeaconState,
	epoch Epoch,
) (*SyncCommittee, [SyncCommitteeSize]ValidatorIndex, error) {
	if state == nil {
		return nil, [SyncCommitteeSize]ValidatorIndex{}, ErrSyncNilState
	}

	state.mu.RLock()
	activeIndices := getActiveValidatorIndices(state, epoch)
	validators := state.Validators
	state.mu.RUnlock()

	if len(activeIndices) == 0 {
		return nil, [SyncCommitteeSize]ValidatorIndex{}, ErrSyncNoValidators
	}

	// Build the seed from epoch.
	seed := computeSyncSeed(epoch)

	activeCount := uint64(len(activeIndices))
	committee := &SyncCommittee{}
	var indices [SyncCommitteeSize]ValidatorIndex
	selected := 0
	i := uint64(0)

	for selected < SyncCommitteeSize {
		// Compute shuffled index (simplified: hash-based permutation).
		shuffledIdx := computeShuffledIndex(i%activeCount, activeCount, seed)
		candidateIndex := activeIndices[shuffledIdx]

		// Get random byte for acceptance probability.
		randomByte := computeRandomByte(seed, i)

		// Effective-balance-weighted acceptance.
		effBalance := validators[candidateIndex].EffectiveBalance
		if effBalance*maxRandomByte >= MaxEffectiveBalance*uint64(randomByte) {
			committee.Pubkeys[selected] = pubkeyToHash(validators[candidateIndex].Pubkey)
			indices[selected] = candidateIndex
			selected++
		}
		i++
	}

	// Compute aggregate pubkey by hashing all member pubkeys together.
	committee.AggregatePubkey = computeAggregatePubkey(committee)

	return committee, indices, nil
}

// InitializeCommittees sets up current and next committees from the beacon state.
func (m *SyncCommitteeManager) InitializeCommittees(
	state *FullBeaconState,
	currentEpoch Epoch,
) error {
	if state == nil {
		return ErrSyncNilState
	}

	current, currentIdx, err := m.ComputeSyncCommittee(state, currentEpoch)
	if err != nil {
		return err
	}

	nextEpoch := Epoch(uint64(currentEpoch) + m.config.PeriodLength)
	next, _, err := m.ComputeSyncCommittee(state, nextEpoch)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.current = current
	m.next = next
	m.currentIndices = currentIdx
	m.currentPeriod = uint64(currentEpoch) / m.config.PeriodLength
	m.totalBitsSet = 0
	m.totalBitSlots = 0
	m.rewards = make(map[ValidatorIndex]uint64)

	return nil
}

// ProcessSyncAggregate processes a sync aggregate from a beacon block.
// It verifies the bitfield, tracks participation, and computes rewards.
func (m *SyncCommitteeManager) ProcessSyncAggregate(
	bits [SyncAggregateBitfieldSize]byte,
	signature types.Hash,
) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == nil {
		return ErrSyncCommitteeNotReady
	}

	// Count participating members and distribute rewards.
	participantCount := uint64(0)
	for i := 0; i < SyncCommitteeSize; i++ {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if bits[byteIdx]&(1<<bitIdx) != 0 {
			participantCount++
			// Reward the participating validator.
			validatorIdx := m.currentIndices[i]
			m.rewards[validatorIdx] += m.config.BaseRewardPerInc
		}
	}

	m.totalBitsSet += participantCount
	m.totalBitSlots += SyncCommitteeSize

	return nil
}

// ParticipationRate returns the fraction of committee members that have
// participated across all processed aggregates. Returns 0 if no aggregates
// have been processed.
func (m *SyncCommitteeManager) ParticipationRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.totalBitSlots == 0 {
		return 0
	}
	return float64(m.totalBitsSet) / float64(m.totalBitSlots)
}

// CurrentCommittee returns a copy of the current sync committee, or nil
// if not initialized.
func (m *SyncCommitteeManager) CurrentCommittee() *SyncCommittee {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.current == nil {
		return nil
	}
	cp := *m.current
	return &cp
}

// NextCommittee returns a copy of the next sync committee, or nil if not
// initialized.
func (m *SyncCommitteeManager) NextCommittee() *SyncCommittee {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.next == nil {
		return nil
	}
	cp := *m.next
	return &cp
}

// GetReward returns the accumulated reward for a validator index.
func (m *SyncCommitteeManager) GetReward(idx ValidatorIndex) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rewards[idx]
}

// RotateCommittees performs a period-boundary rotation: current becomes next,
// and a new next committee is computed from the beacon state.
func (m *SyncCommitteeManager) RotateCommittees(
	state *FullBeaconState,
	newPeriodEpoch Epoch,
) error {
	if state == nil {
		return ErrSyncNilState
	}

	// Compute the new next committee for the period after the upcoming one.
	futureEpoch := Epoch(uint64(newPeriodEpoch) + m.config.PeriodLength)
	newNext, _, err := m.ComputeSyncCommittee(state, futureEpoch)
	if err != nil {
		return err
	}

	// Recompute indices for what will become the current committee.
	_, newCurrentIdx, err := m.ComputeSyncCommittee(state, newPeriodEpoch)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.current = m.next
	m.next = newNext
	m.currentIndices = newCurrentIdx
	m.currentPeriod = uint64(newPeriodEpoch) / m.config.PeriodLength
	// Reset participation tracking for the new period.
	m.totalBitsSet = 0
	m.totalBitSlots = 0
	m.rewards = make(map[ValidatorIndex]uint64)

	return nil
}

// ShouldRotate returns true if the given epoch is at a sync committee period
// boundary.
func (m *SyncCommitteeManager) ShouldRotate(epoch Epoch) bool {
	return uint64(epoch)%m.config.PeriodLength == 0
}

// CurrentPeriod returns the current sync committee period number.
func (m *SyncCommitteeManager) CurrentPeriod() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentPeriod
}

// --- Helper functions ---

// getActiveValidatorIndices returns indices of validators active at the epoch.
// Caller must hold state.mu.RLock or state.mu.Lock.
func getActiveValidatorIndices(state *FullBeaconState, epoch Epoch) []ValidatorIndex {
	var indices []ValidatorIndex
	for i, v := range state.Validators {
		if v.IsActive(epoch) {
			indices = append(indices, ValidatorIndex(i))
		}
	}
	return indices
}

// computeSyncSeed derives a deterministic seed from the epoch.
func computeSyncSeed(epoch Epoch) types.Hash {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(epoch))
	// Domain prefix for sync committee selection.
	domain := []byte{0x07, 0x00, 0x00, 0x00}
	return crypto.Keccak256Hash(domain, buf[:])
}

// computeRandomByte returns a deterministic random byte from seed and counter.
func computeRandomByte(seed types.Hash, i uint64) byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], i/32)
	h := crypto.Keccak256(seed[:], buf[:])
	return h[i%32]
}

// pubkeyToHash converts a 48-byte pubkey to a 32-byte hash (truncating).
func pubkeyToHash(pubkey [48]byte) types.Hash {
	return crypto.Keccak256Hash(pubkey[:])
}

// computeAggregatePubkey computes a simplified aggregate of all committee
// member pubkeys by hashing them together.
func computeAggregatePubkey(committee *SyncCommittee) types.Hash {
	var data []byte
	for _, pk := range committee.Pubkeys {
		data = append(data, pk[:]...)
	}
	return crypto.Keccak256Hash(data)
}
