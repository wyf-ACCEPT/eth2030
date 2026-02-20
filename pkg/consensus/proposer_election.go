// proposer_election.go implements block proposer selection per the Ethereum
// beacon chain spec. Selects proposers using effective balance weighting
// and RANDAO-based randomness.
//
// Key functions:
//   - ComputeProposerIndexElection: balance-weighted proposer selection
//   - GetBeaconProposerElection: returns the proposer for a slot
//   - ProposerBoostScore: EIP-7732 proposer boost for fork choice
//   - ElectProposersForEpoch: batch election for an entire epoch
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// Proposer election constants.
const (
	// PEMaxEffectiveBalance is the max effective balance used in proposer
	// selection (32 ETH in Gwei, per phase0 spec).
	PEMaxEffectiveBalance uint64 = 32_000_000_000

	// PEShuffleRounds is the number of swap-or-not shuffle rounds for
	// proposer candidate selection.
	PEShuffleRounds = 90

	// PEProposerBoostFraction is the committee weight fraction for
	// proposer boost in fork choice (40% per EIP-7732).
	PEProposerBoostFraction uint64 = 40

	// PEProposerBoostDenominator is the denominator for boost fraction.
	PEProposerBoostDenominator uint64 = 100

	// PEMaxSelectionAttempts caps the number of selection attempts to
	// prevent infinite loops with very low balance sets.
	PEMaxSelectionAttempts uint64 = 10000

	// PEDomainProposer is the domain type for proposer seed computation.
	PEDomainProposer uint32 = 0x00000000
)

// Proposer election errors.
var (
	ErrPENilState      = errors.New("proposer_election: nil beacon state")
	ErrPENoValidators  = errors.New("proposer_election: no active validators")
	ErrPENoProposer    = errors.New("proposer_election: failed to select proposer")
	ErrPEInvalidSlot   = errors.New("proposer_election: invalid slot")
)

// ProposerElectionConfig configures the proposer election process.
type ProposerElectionConfig struct {
	SlotsPerEpoch      uint64
	MaxEffectiveBalance uint64
	ShuffleRounds      uint64
	ProposerBoostPct   uint64 // proposer boost percentage (0-100)
}

// DefaultProposerElectionConfig returns mainnet default config.
func DefaultProposerElectionConfig() *ProposerElectionConfig {
	return &ProposerElectionConfig{
		SlotsPerEpoch:      32,
		MaxEffectiveBalance: PEMaxEffectiveBalance,
		ShuffleRounds:      PEShuffleRounds,
		ProposerBoostPct:   PEProposerBoostFraction,
	}
}

// ProposerElection manages proposer selection for the beacon chain.
type ProposerElection struct {
	config *ProposerElectionConfig
}

// NewProposerElection creates a new proposer election engine.
func NewProposerElection(cfg *ProposerElectionConfig) *ProposerElection {
	if cfg == nil {
		cfg = DefaultProposerElectionConfig()
	}
	return &ProposerElection{config: cfg}
}

// ComputeProposerIndexElection selects a block proposer from the active
// validator set using effective balance weighting and RANDAO-based
// randomness per the beacon chain spec.
//
// Algorithm:
//  1. Iterate through shuffled active validator indices.
//  2. For each candidate, compute a random byte from the seed.
//  3. Accept the candidate if:
//     effective_balance * MAX_RANDOM_BYTE >= MAX_EFFECTIVE_BALANCE * random_byte
//  4. This gives higher-balance validators a proportionally higher chance.
func (pe *ProposerElection) ComputeProposerIndexElection(
	activeIndices []ValidatorIndex,
	balances map[ValidatorIndex]uint64,
	seed [32]byte,
) (ValidatorIndex, error) {
	if len(activeIndices) == 0 {
		return 0, ErrPENoValidators
	}

	total := uint64(len(activeIndices))
	maxEB := pe.config.MaxEffectiveBalance

	var buf [40]byte
	for i := uint64(0); i < PEMaxSelectionAttempts; i++ {
		// Compute shuffled index for this iteration.
		shuffledPos := peComputeShuffled(i%total, total, seed)
		candidate := activeIndices[shuffledPos]

		// Compute random byte for acceptance.
		copy(buf[:32], seed[:])
		binary.LittleEndian.PutUint64(buf[32:], i/32)
		randHash := sha256.Sum256(buf[:])
		randByte := uint64(randHash[i%32])

		// Accept candidate based on effective balance weight.
		effBal, ok := balances[candidate]
		if !ok {
			continue
		}
		if effBal*255 >= maxEB*randByte {
			return candidate, nil
		}
	}

	// Fallback: return first active validator.
	return activeIndices[0], nil
}

// GetBeaconProposerElection returns the elected proposer for a given slot
// using the beacon state's RANDAO mixes.
func (pe *ProposerElection) GetBeaconProposerElection(
	state *BeaconStateV2,
	slot uint64,
) (ValidatorIndex, error) {
	if state == nil {
		return 0, ErrPENilState
	}

	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = pe.config.SlotsPerEpoch
	}
	epoch := Epoch(slot / spe)

	state.mu.RLock()
	rawIndices := state.activeIndices(epoch)
	validators := state.Validators
	randaoMix := state.RandaoMixes[uint64(epoch)%EpochsPerHistoricalVector]
	state.mu.RUnlock()

	if len(rawIndices) == 0 {
		return 0, ErrPENoValidators
	}

	// Convert raw indices and collect balances.
	activeIndices := make([]ValidatorIndex, len(rawIndices))
	balances := make(map[ValidatorIndex]uint64, len(rawIndices))
	for i, idx := range rawIndices {
		vi := ValidatorIndex(idx)
		activeIndices[i] = vi
		balances[vi] = validators[idx].EffectiveBalance
	}

	// Compute proposer seed: hash(randao_mix || slot).
	seed := computeProposerSeed(randaoMix, slot)

	return pe.ComputeProposerIndexElection(activeIndices, balances, seed)
}

// ElectProposersForEpoch returns the elected proposer for every slot in
// the given epoch.
func (pe *ProposerElection) ElectProposersForEpoch(
	state *BeaconStateV2,
	epoch Epoch,
) ([]ValidatorIndex, error) {
	if state == nil {
		return nil, ErrPENilState
	}

	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = pe.config.SlotsPerEpoch
	}

	proposers := make([]ValidatorIndex, spe)
	startSlot := uint64(epoch) * spe

	for i := uint64(0); i < spe; i++ {
		slot := startSlot + i
		proposer, err := pe.GetBeaconProposerElection(state, slot)
		if err != nil {
			return nil, err
		}
		proposers[i] = proposer
	}

	return proposers, nil
}

// ProposerBoostScore computes the proposer boost score for fork choice per
// EIP-7732. The boost is a fraction of the committee weight applied to the
// block from the current slot's proposer.
//
// boost = committee_weight * PROPOSER_BOOST_FRACTION / PROPOSER_BOOST_DENOMINATOR
//
// Parameters:
//   - committeeWeight: the total effective balance of the slot's committee
//
// Returns the additional score to add for the timely proposer's block.
func (pe *ProposerElection) ProposerBoostScore(committeeWeight uint64) uint64 {
	boostPct := pe.config.ProposerBoostPct
	if boostPct == 0 {
		boostPct = PEProposerBoostFraction
	}
	return committeeWeight * boostPct / PEProposerBoostDenominator
}

// IsEligibleProposer checks if a validator is eligible to propose:
// active, not slashed, and has sufficient effective balance.
func IsEligibleProposer(v *ValidatorV2, epoch Epoch) bool {
	if v == nil {
		return false
	}
	return v.IsActiveV2(epoch) && !v.Slashed && v.EffectiveBalance > 0
}

// ProposerDutyElection represents a proposer assignment for a slot,
// including the proposer's public key.
type ProposerDutyElection struct {
	Slot           Slot
	ValidatorIdx   ValidatorIndex
	Pubkey         [48]byte
}

// GetProposerDuties returns proposer duties for all slots in an epoch.
func (pe *ProposerElection) GetProposerDuties(
	state *BeaconStateV2,
	epoch Epoch,
) ([]ProposerDutyElection, error) {
	if state == nil {
		return nil, ErrPENilState
	}

	spe := state.SlotsPerEpoch
	if spe == 0 {
		spe = pe.config.SlotsPerEpoch
	}

	duties := make([]ProposerDutyElection, spe)
	startSlot := uint64(epoch) * spe

	for i := uint64(0); i < spe; i++ {
		slot := startSlot + i
		proposer, err := pe.GetBeaconProposerElection(state, slot)
		if err != nil {
			return nil, err
		}

		state.mu.RLock()
		var pk [48]byte
		if int(proposer) < len(state.Validators) {
			pk = state.Validators[uint64(proposer)].Pubkey
		}
		state.mu.RUnlock()

		duties[i] = ProposerDutyElection{
			Slot:         Slot(slot),
			ValidatorIdx: proposer,
			Pubkey:       pk,
		}
	}

	return duties, nil
}

// --- Internal helpers ---

// computeProposerSeed computes the proposer seed from the RANDAO mix and slot.
// seed = sha256(randao_mix || slot_bytes)
func computeProposerSeed(randaoMix [32]byte, slot uint64) [32]byte {
	var buf [40]byte
	copy(buf[:32], randaoMix[:])
	binary.LittleEndian.PutUint64(buf[32:], slot)
	return sha256.Sum256(buf[:])
}

// peComputeShuffled implements swap-or-not shuffle for proposer selection.
func peComputeShuffled(index, count uint64, seed [32]byte) uint64 {
	if count <= 1 {
		return index
	}
	cur := index
	for round := uint64(0); round < PEShuffleRounds; round++ {
		var pivotInput [33]byte
		copy(pivotInput[:32], seed[:])
		pivotInput[32] = byte(round)
		pivotHash := sha256.Sum256(pivotInput[:])
		pivot := binary.LittleEndian.Uint64(pivotHash[:8]) % count

		flip := (pivot + count - cur) % count
		pos := flip
		if cur > flip {
			pos = cur
		}

		var srcInput [37]byte
		copy(srcInput[:32], seed[:])
		srcInput[32] = byte(round)
		binary.LittleEndian.PutUint32(srcInput[33:], uint32(pos/256))
		src := sha256.Sum256(srcInput[:])

		byteIdx := (pos % 256) / 8
		bitIdx := pos % 8
		if (src[byteIdx]>>bitIdx)&1 != 0 {
			cur = flip
		}
	}
	return cur
}
