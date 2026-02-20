// committee_selection.go implements deterministic committee selection per the
// Ethereum beacon chain spec. Includes swap-or-not shuffle, proposer index
// computation, and beacon committee formation.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Committee selection spec constants.
const (
	// ShuffleRoundCountCS is the number of rounds in the swap-or-not shuffle.
	ShuffleRoundCountCS = 90
	// TargetCommitteeSize is the ideal number of validators per committee.
	TargetCommitteeSize = 128
	// maxCommitteesCS mirrors MaxCommitteesPerSlot for committee selection.
	maxCommitteesCS = 64
	// MaxEffBalForSelection is the max effective balance used in proposer selection.
	MaxEffBalForSelection = 32_000_000_000
	// CSDomainAttester is the domain type for attestation seed computation (uint32).
	CSDomainAttester uint32 = 0x01000000
	// CSDomainProposer is the domain type for proposer seed computation (uint32).
	CSDomainProposer uint32 = 0x00000000
)

// Committee selection errors.
var (
	ErrCSNilState        = errors.New("committee_selection: nil state")
	ErrCSNoValidators    = errors.New("committee_selection: no active validators")
	ErrCSInvalidIndex    = errors.New("committee_selection: index out of range")
	ErrCSInvalidSeed     = errors.New("committee_selection: invalid seed")
	ErrCSZeroIndexCount  = errors.New("committee_selection: zero index count")
	ErrCSInvalidCommIdx  = errors.New("committee_selection: committee index out of range")
)

// ComputeShuffledIndex implements the swap-or-not shuffle from the beacon
// chain spec. Given an index, total count, and seed, it returns the shuffled
// position. This is used for committee assignment and proposer selection.
func ComputeShuffledIndex(index, indexCount uint64, seed [32]byte) (uint64, error) {
	if indexCount == 0 {
		return 0, ErrCSZeroIndexCount
	}
	if index >= indexCount {
		return 0, fmt.Errorf("%w: %d >= %d", ErrCSInvalidIndex, index, indexCount)
	}
	if indexCount == 1 {
		return 0, nil
	}

	cur := index
	for round := uint64(0); round < ShuffleRoundCountCS; round++ {
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

		// Check the bit at position%256.
		byteIdx := (position % 256) / 8
		bitIdx := position % 8
		if (source[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur, nil
}

// GetSeed computes the seed for a given epoch and domain type from the
// beacon state's RANDAO mixes. The seed is hash(domain || epoch || mix).
func GetSeed(state *BeaconStateV2, epoch Epoch, domainType uint32) ([32]byte, error) {
	if state == nil {
		return [32]byte{}, ErrCSNilState
	}
	mix := state.RandaoMixes[uint64(epoch)%EpochsPerHistoricalVector]
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[:4], domainType)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:], mix[:20])
	result := sha256.Sum256(buf[:])
	return result, nil
}

// GetActiveValidatorIndices returns the indices of all validators active
// at the given epoch.
func GetActiveValidatorIndices(state *BeaconStateV2, epoch Epoch) ([]ValidatorIndex, error) {
	if state == nil {
		return nil, ErrCSNilState
	}
	raw := state.GetActiveValidatorIndices(epoch)
	if len(raw) == 0 {
		return nil, ErrCSNoValidators
	}
	result := make([]ValidatorIndex, len(raw))
	for i, idx := range raw {
		result[i] = ValidatorIndex(idx)
	}
	return result, nil
}

// GetCommitteeCountPerSlot returns the number of committees per slot
// based on the active validator count. This follows the spec formula:
// max(1, min(MAX_COMMITTEES_PER_SLOT, active_count / SLOTS_PER_EPOCH / TARGET_COMMITTEE_SIZE)).
func GetCommitteeCountPerSlot(state *BeaconStateV2, epoch Epoch) (uint64, error) {
	indices, err := GetActiveValidatorIndices(state, epoch)
	if err != nil {
		return 0, err
	}
	return computeCommitteeCount(len(indices), state.SlotsPerEpoch), nil
}

// computeCommitteeCount calculates committee count from validator count
// and slots per epoch.
func computeCommitteeCount(activeCount int, slotsPerEpoch uint64) uint64 {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	count := uint64(activeCount) / slotsPerEpoch / TargetCommitteeSize
	if count == 0 {
		count = 1
	}
	if count > maxCommitteesCS {
		count = maxCommitteesCS
	}
	return count
}

// ComputeBeaconCommittee returns the committee for a given slot and committee
// index. It shuffles the active validator indices using the seed and carves
// out the appropriate slice.
func ComputeBeaconCommittee(
	state *BeaconStateV2,
	slot Slot,
	committeeIndex uint64,
	seed [32]byte,
) ([]ValidatorIndex, error) {
	if state == nil {
		return nil, ErrCSNilState
	}
	epoch := SlotToEpoch(slot, state.SlotsPerEpoch)
	indices, err := GetActiveValidatorIndices(state, epoch)
	if err != nil {
		return nil, err
	}

	committeesPerSlot := computeCommitteeCount(len(indices), state.SlotsPerEpoch)
	if committeeIndex >= committeesPerSlot {
		return nil, fmt.Errorf("%w: %d >= %d", ErrCSInvalidCommIdx, committeeIndex, committeesPerSlot)
	}

	// Compute the offset into the total epoch committees.
	slotOffset := uint64(slot) % state.SlotsPerEpoch
	totalCommittees := committeesPerSlot * state.SlotsPerEpoch
	globalIdx := slotOffset*committeesPerSlot + committeeIndex

	// Compute start and end index for this committee's slice of validators.
	count := uint64(len(indices))
	start := count * globalIdx / totalCommittees
	end := count * (globalIdx + 1) / totalCommittees

	committee := make([]ValidatorIndex, 0, end-start)
	for i := start; i < end; i++ {
		shuffled, err := ComputeShuffledIndex(i, count, seed)
		if err != nil {
			return nil, err
		}
		committee = append(committee, indices[shuffled])
	}
	return committee, nil
}

// ComputeProposerIndex computes the beacon block proposer for a given epoch
// and slot using the seed. It selects from active validators weighted by
// effective balance (swap-or-not shuffle + random sampling).
func ComputeProposerIndex(
	state *BeaconStateV2,
	epoch Epoch,
	seed [32]byte,
) (ValidatorIndex, error) {
	if state == nil {
		return 0, ErrCSNilState
	}
	indices, err := GetActiveValidatorIndices(state, epoch)
	if err != nil {
		return 0, err
	}

	total := uint64(len(indices))
	if total == 0 {
		return 0, ErrCSNoValidators
	}

	// Iterate through candidates, sampling with effective balance weighting.
	var buf [40]byte
	for i := uint64(0); i < total*100; i++ {
		shuffled, err := ComputeShuffledIndex(i%total, total, seed)
		if err != nil {
			return 0, err
		}
		candidate := indices[shuffled]

		// Random byte for proportional selection.
		copy(buf[:32], seed[:])
		binary.LittleEndian.PutUint64(buf[32:], i/32)
		randHash := sha256.Sum256(buf[:])
		randByte := randHash[i%32]

		// Check if candidate is selected proportional to balance.
		effBal := state.Validators[uint64(candidate)].EffectiveBalance
		if effBal*255 >= MaxEffBalForSelection*uint64(randByte) {
			return candidate, nil
		}
	}
	// Fallback: return first active validator.
	return indices[0], nil
}

// ShuffleValidatorsCS returns a fully shuffled copy of the given validator
// indices using the provided seed. Useful for testing and epoch transitions.
func ShuffleValidatorsCS(indices []ValidatorIndex, seed [32]byte) ([]ValidatorIndex, error) {
	if len(indices) == 0 {
		return nil, ErrCSNoValidators
	}
	count := uint64(len(indices))
	result := make([]ValidatorIndex, count)
	for i := uint64(0); i < count; i++ {
		shuffled, err := ComputeShuffledIndex(i, count, seed)
		if err != nil {
			return nil, err
		}
		result[i] = indices[shuffled]
	}
	return result, nil
}
