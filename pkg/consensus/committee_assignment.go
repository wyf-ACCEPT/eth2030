// committee_assignment.go implements beacon committee computation, sync committee
// selection, proposer selection, and the swap-or-not shuffling algorithm per
// the Ethereum beacon chain spec (Altair/Bellatrix).
//
// This complements committee_selection.go by providing a higher-level
// CommitteeAssigner that operates on ValidatorRegistryV2 and produces full
// epoch committee assignments with caching and sync committee support.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"sync"
)

// Committee assignment constants.
const (
	// CAShuffleRounds is the number of rounds in the swap-or-not shuffle.
	CAShuffleRounds = 90

	// CATargetCommitteeSize is the ideal committee size.
	CATargetCommitteeSize = 128

	// CAMaxCommitteesPerSlot limits the number of committees per slot.
	CAMaxCommitteesPerSlot = 64

	// CAMaxRandomByte is 2^8 - 1, used in proposer selection.
	CAMaxRandomByte uint64 = 255

	// CASyncCommitteeSize is the sync committee size.
	CASyncCommitteeSize = 512

	// CAEpochsPerSyncPeriod is the sync committee period length in epochs.
	CAEpochsPerSyncPeriod = 256

	// Domain types for seed computation.
	CADomainBeaconAttester uint32 = 0x01000000
	CADomainBeaconProposer uint32 = 0x00000000
	CADomainSyncCommittee  uint32 = 0x07000000
)

// Committee assignment errors.
var (
	ErrCANoValidators = errors.New("committee_assignment: no active validators")
	ErrCAInvalidSlot  = errors.New("committee_assignment: invalid slot")
	ErrCAInvalidIndex = errors.New("committee_assignment: invalid committee index")
	ErrCANilRegistry  = errors.New("committee_assignment: nil registry")
	ErrCAZeroCount    = errors.New("committee_assignment: zero index count")
)

// CommitteeAssignerConfig configures the committee assigner.
type CommitteeAssignerConfig struct {
	SlotsPerEpoch         uint64
	TargetCommitteeSize   uint64
	MaxCommitteesPerSlot  uint64
	SyncCommitteeSize     int
	EpochsPerSyncPeriod   uint64
	MaxEffectiveBalanceCA uint64 // cap for proposer selection
}

// DefaultCommitteeAssignerConfig returns mainnet defaults.
func DefaultCommitteeAssignerConfig() CommitteeAssignerConfig {
	return CommitteeAssignerConfig{
		SlotsPerEpoch:         32,
		TargetCommitteeSize:   CATargetCommitteeSize,
		MaxCommitteesPerSlot:  CAMaxCommitteesPerSlot,
		SyncCommitteeSize:     CASyncCommitteeSize,
		EpochsPerSyncPeriod:   CAEpochsPerSyncPeriod,
		MaxEffectiveBalanceCA: MaxEffectiveBalanceV2,
	}
}

// BeaconCommitteeResult holds the result of a beacon committee computation.
type BeaconCommitteeResult struct {
	Slot           Slot
	CommitteeIndex uint64
	Members        []ValidatorIndex
}

// ProposerAssignment holds a proposer selection result.
type ProposerAssignment struct {
	Slot     Slot
	Proposer ValidatorIndex
	Epoch    Epoch
}

// SyncCommitteeResult holds a sync committee selection result.
type SyncCommitteeResult struct {
	Period     uint64
	StartEpoch Epoch
	Members    []ValidatorIndex
}

// CommitteeAssigner computes beacon committees, sync committees, and proposer
// assignments. Thread-safe with internal caching.
type CommitteeAssigner struct {
	mu     sync.RWMutex
	config CommitteeAssignerConfig

	// Cache: epoch -> list of beacon committee results.
	committeeCache map[Epoch][]BeaconCommitteeResult

	// Cache: epoch -> proposer assignments per slot.
	proposerCache map[Epoch][]ProposerAssignment

	// Cache: period -> sync committee result.
	syncCache map[uint64]*SyncCommitteeResult
}

// NewCommitteeAssigner creates a new committee assigner.
func NewCommitteeAssigner(cfg CommitteeAssignerConfig) *CommitteeAssigner {
	return &CommitteeAssigner{
		config:         cfg,
		committeeCache: make(map[Epoch][]BeaconCommitteeResult),
		proposerCache:  make(map[Epoch][]ProposerAssignment),
		syncCache:      make(map[uint64]*SyncCommitteeResult),
	}
}

// SwapOrNotShuffle implements the spec's swap-or-not shuffling algorithm.
// Returns the shuffled position for the given index.
func SwapOrNotShuffle(index, indexCount uint64, seed [32]byte) (uint64, error) {
	if indexCount == 0 {
		return 0, ErrCAZeroCount
	}
	if index >= indexCount {
		return 0, ErrCAInvalidIndex
	}
	if indexCount == 1 {
		return 0, nil
	}

	cur := index
	for round := uint64(0); round < CAShuffleRounds; round++ {
		// Compute pivot: hash(seed || round_byte).
		var pivotInput [33]byte
		copy(pivotInput[:32], seed[:])
		pivotInput[32] = byte(round)
		pivotHash := sha256.Sum256(pivotInput[:])
		pivot := binary.LittleEndian.Uint64(pivotHash[:8]) % indexCount

		// Compute flip index.
		flip := (pivot + indexCount - cur) % indexCount

		// Position is max(cur, flip).
		position := flip
		if cur > flip {
			position = cur
		}

		// Compute source: hash(seed || round_byte || position/256).
		var srcInput [37]byte
		copy(srcInput[:32], seed[:])
		srcInput[32] = byte(round)
		binary.LittleEndian.PutUint32(srcInput[33:], uint32(position/256))
		source := sha256.Sum256(srcInput[:])

		// Check the bit at position%256 in the source hash.
		byteIdx := (position % 256) / 8
		bitIdx := position % 8
		if (source[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur, nil
}

// ShuffleIndices applies the swap-or-not shuffle to produce a complete
// shuffled permutation of the input indices.
func ShuffleIndices(indices []ValidatorIndex, seed [32]byte) ([]ValidatorIndex, error) {
	n := uint64(len(indices))
	if n == 0 {
		return nil, ErrCANoValidators
	}
	result := make([]ValidatorIndex, n)
	for i := uint64(0); i < n; i++ {
		shuffled, err := SwapOrNotShuffle(i, n, seed)
		if err != nil {
			return nil, err
		}
		result[i] = indices[shuffled]
	}
	return result, nil
}

// ComputeCommitteeCount calculates the number of committees per slot
// from the active validator count:
// max(1, min(MAX_COMMITTEES_PER_SLOT, active_count / SLOTS_PER_EPOCH / TARGET_COMMITTEE_SIZE))
func (ca *CommitteeAssigner) ComputeCommitteeCount(activeCount int) uint64 {
	spe := ca.config.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	count := uint64(activeCount) / spe / ca.config.TargetCommitteeSize
	if count == 0 {
		count = 1
	}
	if count > ca.config.MaxCommitteesPerSlot {
		count = ca.config.MaxCommitteesPerSlot
	}
	return count
}

// ComputeBeaconCommittees computes all beacon committees for the given epoch.
// Uses the provided active indices and seed. Results are cached.
func (ca *CommitteeAssigner) ComputeBeaconCommittees(
	epoch Epoch,
	activeIndices []ValidatorIndex,
	seed [32]byte,
) ([]BeaconCommitteeResult, error) {
	ca.mu.RLock()
	if cached, ok := ca.committeeCache[epoch]; ok {
		ca.mu.RUnlock()
		return cached, nil
	}
	ca.mu.RUnlock()

	if len(activeIndices) == 0 {
		return nil, ErrCANoValidators
	}

	spe := ca.config.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}

	committeesPerSlot := ca.ComputeCommitteeCount(len(activeIndices))
	totalCommittees := spe * committeesPerSlot
	count := uint64(len(activeIndices))

	var results []BeaconCommitteeResult
	startSlot := Slot(uint64(epoch) * spe)

	for s := uint64(0); s < spe; s++ {
		slot := Slot(uint64(startSlot) + s)
		for c := uint64(0); c < committeesPerSlot; c++ {
			globalIdx := s*committeesPerSlot + c
			start := count * globalIdx / totalCommittees
			end := count * (globalIdx + 1) / totalCommittees

			members := make([]ValidatorIndex, 0, end-start)
			for i := start; i < end; i++ {
				shuffled, err := SwapOrNotShuffle(i, count, seed)
				if err != nil {
					return nil, err
				}
				members = append(members, activeIndices[shuffled])
			}

			results = append(results, BeaconCommitteeResult{
				Slot:           slot,
				CommitteeIndex: c,
				Members:        members,
			})
		}
	}

	ca.mu.Lock()
	ca.committeeCache[epoch] = results
	ca.mu.Unlock()

	return results, nil
}

// ComputeProposerAssignments computes the proposer for each slot in the
// given epoch using effective-balance-weighted selection.
func (ca *CommitteeAssigner) ComputeProposerAssignments(
	epoch Epoch,
	activeIndices []ValidatorIndex,
	effectiveBalances map[ValidatorIndex]uint64,
	seed [32]byte,
) ([]ProposerAssignment, error) {
	ca.mu.RLock()
	if cached, ok := ca.proposerCache[epoch]; ok {
		ca.mu.RUnlock()
		return cached, nil
	}
	ca.mu.RUnlock()

	if len(activeIndices) == 0 {
		return nil, ErrCANoValidators
	}

	spe := ca.config.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}

	assignments := make([]ProposerAssignment, spe)
	startSlot := Slot(uint64(epoch) * spe)
	total := uint64(len(activeIndices))

	for s := uint64(0); s < spe; s++ {
		slot := Slot(uint64(startSlot) + s)

		// Derive per-slot seed.
		var slotBuf [40]byte
		copy(slotBuf[:32], seed[:])
		binary.LittleEndian.PutUint64(slotBuf[32:], uint64(slot))
		slotSeed := sha256.Sum256(slotBuf[:])

		proposer := ca.selectProposer(activeIndices, effectiveBalances, slotSeed, total)
		assignments[s] = ProposerAssignment{
			Slot:     slot,
			Proposer: proposer,
			Epoch:    epoch,
		}
	}

	ca.mu.Lock()
	ca.proposerCache[epoch] = assignments
	ca.mu.Unlock()

	return assignments, nil
}

// selectProposer selects a proposer weighted by effective balance using the
// spec's random sampling method.
func (ca *CommitteeAssigner) selectProposer(
	indices []ValidatorIndex,
	balances map[ValidatorIndex]uint64,
	seed [32]byte,
	total uint64,
) ValidatorIndex {
	maxEB := ca.config.MaxEffectiveBalanceCA
	if maxEB == 0 {
		maxEB = MaxEffectiveBalanceV2
	}

	var buf [40]byte
	for i := uint64(0); i < total*100; i++ {
		shuffled, err := SwapOrNotShuffle(i%total, total, seed)
		if err != nil {
			break
		}
		candidate := indices[shuffled]

		copy(buf[:32], seed[:])
		binary.LittleEndian.PutUint64(buf[32:], i/32)
		randHash := sha256.Sum256(buf[:])
		randByte := uint64(randHash[i%32])

		eb := balances[candidate]
		if eb*CAMaxRandomByte >= maxEB*randByte {
			return candidate
		}
	}
	// Fallback: return first active validator.
	return indices[0]
}

// ComputeSyncCommitteeCA selects a sync committee for the given period using
// effective-balance-weighted sampling.
func (ca *CommitteeAssigner) ComputeSyncCommitteeCA(
	periodStartEpoch Epoch,
	activeIndices []ValidatorIndex,
	effectiveBalances map[ValidatorIndex]uint64,
	seed [32]byte,
) (*SyncCommitteeResult, error) {
	period := uint64(periodStartEpoch) / ca.config.EpochsPerSyncPeriod

	ca.mu.RLock()
	if cached, ok := ca.syncCache[period]; ok {
		ca.mu.RUnlock()
		return cached, nil
	}
	ca.mu.RUnlock()

	if len(activeIndices) == 0 {
		return nil, ErrCANoValidators
	}

	committeeSize := ca.config.SyncCommitteeSize
	if committeeSize <= 0 {
		committeeSize = CASyncCommitteeSize
	}

	maxEB := ca.config.MaxEffectiveBalanceCA
	if maxEB == 0 {
		maxEB = MaxEffectiveBalanceV2
	}

	total := uint64(len(activeIndices))
	members := make([]ValidatorIndex, 0, committeeSize)
	selected := 0
	i := uint64(0)

	for selected < committeeSize {
		shuffled, err := SwapOrNotShuffle(i%total, total, seed)
		if err != nil {
			return nil, err
		}
		candidate := activeIndices[shuffled]

		randomByte := caRandomByte(seed, i)
		eb := effectiveBalances[candidate]
		if eb*CAMaxRandomByte >= maxEB*uint64(randomByte) {
			members = append(members, candidate)
			selected++
		}
		i++
		// Safety limit to prevent infinite loop with very low balances.
		if i > total*1000 {
			break
		}
	}

	result := &SyncCommitteeResult{
		Period:     period,
		StartEpoch: periodStartEpoch,
		Members:    members,
	}

	ca.mu.Lock()
	ca.syncCache[period] = result
	ca.mu.Unlock()

	return result, nil
}

// GetCommitteeForSlot returns the beacon committee members for the given
// slot and committee index. Requires that ComputeBeaconCommittees has been
// called for the containing epoch.
func (ca *CommitteeAssigner) GetCommitteeForSlot(
	epoch Epoch, slot Slot, committeeIdx uint64,
) ([]ValidatorIndex, error) {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	results, ok := ca.committeeCache[epoch]
	if !ok {
		return nil, ErrCAInvalidSlot
	}

	for _, r := range results {
		if r.Slot == slot && r.CommitteeIndex == committeeIdx {
			members := make([]ValidatorIndex, len(r.Members))
			copy(members, r.Members)
			return members, nil
		}
	}
	return nil, ErrCAInvalidIndex
}

// ClearCaches removes all cached committee, proposer, and sync results.
func (ca *CommitteeAssigner) ClearCaches() {
	ca.mu.Lock()
	ca.committeeCache = make(map[Epoch][]BeaconCommitteeResult)
	ca.proposerCache = make(map[Epoch][]ProposerAssignment)
	ca.syncCache = make(map[uint64]*SyncCommitteeResult)
	ca.mu.Unlock()
}

// ComputeAttesterSeed builds the attester seed for the given epoch and
// RANDAO mix. seed = sha256(domain || epoch || mix[:20]).
func ComputeAttesterSeed(epoch Epoch, randaoMix [32]byte) [32]byte {
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[:4], CADomainBeaconAttester)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:], randaoMix[:20])
	return sha256.Sum256(buf[:])
}

// ComputeProposerSeed builds the proposer seed for the given epoch.
func ComputeProposerSeed(epoch Epoch, randaoMix [32]byte) [32]byte {
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[:4], CADomainBeaconProposer)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:], randaoMix[:20])
	return sha256.Sum256(buf[:])
}

// ComputeSyncSeed builds the sync committee seed for the given epoch.
func ComputeSyncSeed(epoch Epoch, randaoMix [32]byte) [32]byte {
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[:4], CADomainSyncCommittee)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:], randaoMix[:20])
	return sha256.Sum256(buf[:])
}

// caRandomByte returns a deterministic random byte from seed and counter.
func caRandomByte(seed [32]byte, i uint64) byte {
	var buf [40]byte
	copy(buf[:32], seed[:])
	binary.LittleEndian.PutUint64(buf[32:], i/32)
	h := sha256.Sum256(buf[:])
	return h[i%32]
}

// SortedActiveIndices is a convenience to get sorted active indices from
// a list of ValidatorRecordV2 entries.
func SortedActiveIndices(validators []*ValidatorRecordV2, epoch Epoch) []ValidatorIndex {
	var out []ValidatorIndex
	for _, v := range validators {
		if v.IsActive(epoch) {
			out = append(out, v.Index)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
