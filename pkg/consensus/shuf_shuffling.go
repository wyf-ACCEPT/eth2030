// shuf_shuffling.go implements beacon chain committee shuffling per the
// Ethereum consensus spec. Provides the swap-or-not shuffle algorithm,
// proposer index computation with effective balance weighting, committee
// computation, and full committee retrieval for a given slot and index.
//
// This complements committee_assignment.go and beacon_state_v2.go by
// providing functions that operate directly on BsnBeaconState.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// Shuffling constants.
const (
	ShufShuffleRoundCount           = 90
	ShufMaxCommitteesPerSlot        = 64
	ShufTargetCommitteeSize         = 128
	ShufMaxRandomByte        uint64 = 255
	ShufDomainBeaconAttester        = 0x01000000
	ShufDomainBeaconProposer        = 0x00000000
)

// Shuffling errors.
var (
	ErrShufIndexOutOfRange  = errors.New("shuffling: index >= count")
	ErrShufZeroCount        = errors.New("shuffling: count is zero")
	ErrShufNoActiveVals     = errors.New("shuffling: no active validators")
	ErrShufInvalidSlot      = errors.New("shuffling: slot out of range for epoch")
	ErrShufInvalidCommIdx   = errors.New("shuffling: committee index out of range")
	ErrShufProposerNotFound = errors.New("shuffling: proposer not found after max iterations")
)

// ShufComputeShuffledIndex computes the shuffled position for a given index
// using the swap-or-not network. Implements the full spec shuffling algorithm
// with 90 rounds of SHA-256 based randomness.
func ShufComputeShuffledIndex(index, indexCount uint64, seed [32]byte) (uint64, error) {
	if indexCount == 0 {
		return 0, ErrShufZeroCount
	}
	if index >= indexCount {
		return 0, ErrShufIndexOutOfRange
	}
	if indexCount == 1 {
		return 0, nil
	}

	cur := index
	for round := uint64(0); round < ShufShuffleRoundCount; round++ {
		// Compute pivot = hash(seed || round_byte) mod count.
		var pivotInput [33]byte
		copy(pivotInput[:32], seed[:])
		pivotInput[32] = byte(round)
		pivotHash := sha256.Sum256(pivotInput[:])
		pivot := binary.LittleEndian.Uint64(pivotHash[:8]) % indexCount

		// Compute flip index: (pivot + count - cur) mod count.
		flip := (pivot + indexCount - cur) % indexCount

		// Position is max(cur, flip) for the source hash lookup.
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

		// Check the bit at position % 256 in the source hash.
		byteIdx := (position % 256) / 8
		bitIdx := position % 8
		if (source[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur, nil
}

// ShufUnshuffleIndex computes the original index from a shuffled position.
// This is the inverse of ShufComputeShuffledIndex, running rounds in reverse.
func ShufUnshuffleIndex(shuffledIndex, indexCount uint64, seed [32]byte) (uint64, error) {
	if indexCount == 0 {
		return 0, ErrShufZeroCount
	}
	if shuffledIndex >= indexCount {
		return 0, ErrShufIndexOutOfRange
	}
	if indexCount == 1 {
		return 0, nil
	}

	cur := shuffledIndex
	for r := int(ShufShuffleRoundCount) - 1; r >= 0; r-- {
		round := uint64(r)

		var pivotInput [33]byte
		copy(pivotInput[:32], seed[:])
		pivotInput[32] = byte(round)
		pivotHash := sha256.Sum256(pivotInput[:])
		pivot := binary.LittleEndian.Uint64(pivotHash[:8]) % indexCount

		flip := (pivot + indexCount - cur) % indexCount

		position := flip
		if cur > flip {
			position = cur
		}

		var srcInput [37]byte
		copy(srcInput[:32], seed[:])
		srcInput[32] = byte(round)
		binary.LittleEndian.PutUint32(srcInput[33:], uint32(position/256))
		source := sha256.Sum256(srcInput[:])

		byteIdx := (position % 256) / 8
		bitIdx := position % 8
		if (source[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur, nil
}

// ShufComputeProposerIndex computes the beacon chain proposer for a given
// slot. Uses effective-balance-weighted random selection:
//   - Shuffle active validators using the epoch seed mixed with the slot
//   - Select a candidate; accept with probability proportional to
//     effective_balance / MAX_EFFECTIVE_BALANCE
func ShufComputeProposerIndex(
	activeIndices []uint64,
	effectiveBalances map[uint64]uint64,
	seed [32]byte,
) (uint64, error) {
	if len(activeIndices) == 0 {
		return 0, ErrShufNoActiveVals
	}

	total := uint64(len(activeIndices))
	maxEB := uint64(MaxEffectiveBalanceV2)

	// The proposer selection loop: try up to total * 100 candidates.
	var buf [40]byte
	for i := uint64(0); i < total*100; i++ {
		// Shuffle the candidate index.
		candidateIdx := i % total
		shuffled, err := ShufComputeShuffledIndex(candidateIdx, total, seed)
		if err != nil {
			return 0, err
		}
		candidate := activeIndices[shuffled]

		// Compute random byte for probability check.
		copy(buf[:32], seed[:])
		binary.LittleEndian.PutUint64(buf[32:], i/32)
		randHash := sha256.Sum256(buf[:])
		randByte := uint64(randHash[i%32])

		// Accept candidate with probability effective_balance / max_effective_balance.
		eb := effectiveBalances[candidate]
		if eb*ShufMaxRandomByte >= maxEB*randByte {
			return candidate, nil
		}
	}
	return 0, ErrShufProposerNotFound
}

// ShufComputeCommitteeCount returns the number of committees per slot for a
// given active validator count.
// Formula: max(1, min(MAX_COMMITTEES_PER_SLOT, active / slots_per_epoch / TARGET_COMMITTEE_SIZE))
func ShufComputeCommitteeCount(activeCount uint64, slotsPerEpoch uint64) uint64 {
	if slotsPerEpoch == 0 {
		slotsPerEpoch = 32
	}
	count := activeCount / slotsPerEpoch / ShufTargetCommitteeSize
	if count == 0 {
		count = 1
	}
	if count > ShufMaxCommitteesPerSlot {
		count = ShufMaxCommitteesPerSlot
	}
	return count
}

// ShufComputeCommittee computes the committee members for a given committee
// position within the epoch's shuffled validator set.
//
// Parameters:
//   - indices: sorted list of active validator indices for the epoch
//   - seed: the epoch seed for shuffling
//   - idx: the committee's global position in the epoch (slot_offset * committees_per_slot + committee_index)
//   - totalCommittees: total number of committees in the epoch (slots_per_epoch * committees_per_slot)
func ShufComputeCommittee(
	indices []uint64,
	seed [32]byte,
	idx uint64,
	totalCommittees uint64,
) ([]uint64, error) {
	count := uint64(len(indices))
	if count == 0 {
		return nil, ErrShufNoActiveVals
	}
	if totalCommittees == 0 {
		return nil, ErrShufZeroCount
	}

	// Compute the slice of the shuffled list for this committee.
	start := count * idx / totalCommittees
	end := count * (idx + 1) / totalCommittees

	members := make([]uint64, 0, end-start)
	for i := start; i < end; i++ {
		shuffled, err := ShufComputeShuffledIndex(i, count, seed)
		if err != nil {
			return nil, err
		}
		members = append(members, indices[shuffled])
	}
	return members, nil
}

// ShufGetBeaconCommittee returns the beacon committee for a given slot and
// committee index, using the provided BsnBeaconState.
func ShufGetBeaconCommittee(
	state *BsnBeaconState,
	slot uint64,
	committeeIndex uint64,
) ([]uint64, error) {
	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	epoch := Epoch(slot / spe)
	activeIndices := state.BsnGetActiveValidatorIndices(epoch)
	if len(activeIndices) == 0 {
		return nil, ErrShufNoActiveVals
	}

	committeesPerSlot := ShufComputeCommitteeCount(uint64(len(activeIndices)), spe)
	if committeeIndex >= committeesPerSlot {
		return nil, ErrShufInvalidCommIdx
	}

	totalCommittees := spe * committeesPerSlot
	slotOffset := slot % spe
	globalIdx := slotOffset*committeesPerSlot + committeeIndex

	// Compute the epoch seed: hash(domain || epoch || randao_mix).
	seed := shufComputeEpochSeed(epoch, state.BsnGetRandaoMix(epoch), ShufDomainBeaconAttester)

	return ShufComputeCommittee(activeIndices, seed, globalIdx, totalCommittees)
}

// ShufGetBeaconProposerIndex computes the proposer for the given slot
// using the BsnBeaconState.
func ShufGetBeaconProposerIndex(state *BsnBeaconState, slot uint64) (uint64, error) {
	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	epoch := Epoch(slot / spe)
	activeIndices := state.BsnGetActiveValidatorIndices(epoch)
	if len(activeIndices) == 0 {
		return 0, ErrShufNoActiveVals
	}

	// Build effective balance map.
	state.mu.RLock()
	balances := make(map[uint64]uint64, len(activeIndices))
	for _, idx := range activeIndices {
		if idx < uint64(len(state.Validators)) {
			balances[idx] = state.Validators[idx].EffectiveBalance
		}
	}
	state.mu.RUnlock()

	// Derive proposer seed: hash(domain || epoch || randao_mix) then mix with slot.
	epochSeed := shufComputeEpochSeed(epoch, state.BsnGetRandaoMix(epoch), ShufDomainBeaconProposer)
	var slotBuf [40]byte
	copy(slotBuf[:32], epochSeed[:])
	binary.LittleEndian.PutUint64(slotBuf[32:], slot)
	proposerSeed := sha256.Sum256(slotBuf[:])

	return ShufComputeProposerIndex(activeIndices, balances, proposerSeed)
}

// shufComputeEpochSeed builds a seed from the domain, epoch, and RANDAO mix.
// seed = sha256(domain_4bytes || epoch_8bytes || mix[:20])
func shufComputeEpochSeed(epoch Epoch, mix [32]byte, domain uint32) [32]byte {
	var buf [40]byte
	binary.LittleEndian.PutUint32(buf[:4], domain)
	binary.LittleEndian.PutUint64(buf[4:12], uint64(epoch))
	copy(buf[12:32], mix[:20])
	return sha256.Sum256(buf[:])
}

// ShufShuffleList applies the swap-or-not shuffle to produce a complete
// shuffled permutation of the input indices.
func ShufShuffleList(indices []uint64, seed [32]byte) ([]uint64, error) {
	n := uint64(len(indices))
	if n == 0 {
		return nil, ErrShufNoActiveVals
	}
	result := make([]uint64, n)
	for i := uint64(0); i < n; i++ {
		shuffled, err := ShufComputeShuffledIndex(i, n, seed)
		if err != nil {
			return nil, err
		}
		result[i] = indices[shuffled]
	}
	return result, nil
}

// ShufGetCommitteeCountPerSlot returns the number of committees for a slot
// given the state.
func ShufGetCommitteeCountPerSlot(state *BsnBeaconState, epoch Epoch) uint64 {
	activeIndices := state.BsnGetActiveValidatorIndices(epoch)
	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	return ShufComputeCommitteeCount(uint64(len(activeIndices)), spe)
}
