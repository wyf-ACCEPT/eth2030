package consensus

import (
	"encoding/binary"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Attester-Proposer Separation (APS) implements the post-2029 consensus
// upgrade where validators are assigned dedicated roles per epoch: either
// attesting or proposing, but not both. This reduces validator bandwidth
// and enables specialized hardware for block building.

// APSConfig holds the configuration for attester-proposer separation.
type APSConfig struct {
	SeparationEpoch Epoch   // epoch at which APS activates
	AttesterWeight  float64 // fraction of validators assigned to attest (e.g. 0.9)
	ProposerWeight  float64 // fraction of validators assigned to propose (e.g. 0.1)
}

// DefaultAPSConfig returns the default APS configuration.
func DefaultAPSConfig() *APSConfig {
	return &APSConfig{
		SeparationEpoch: 0,
		AttesterWeight:  0.9,
		ProposerWeight:  0.1,
	}
}

// IsAPSActive returns true if APS is active at the given epoch.
func (c *APSConfig) IsAPSActive(epoch Epoch) bool {
	return epoch >= c.SeparationEpoch
}

// AttesterDuty represents a single attestation duty assignment.
type AttesterDuty struct {
	ValidatorIndex uint64
	Slot           Slot
	CommitteeIndex uint64
}

// ProposerDuty represents a single proposal duty assignment.
type ProposerDuty struct {
	ValidatorIndex uint64
	Slot           Slot
}

// DutyScheduler computes attester and proposer duties for each epoch.
type DutyScheduler struct {
	config       *APSConfig
	consensusCfg *ConsensusConfig
}

// NewDutyScheduler creates a new duty scheduler.
func NewDutyScheduler(apsCfg *APSConfig, consensusCfg *ConsensusConfig) *DutyScheduler {
	return &DutyScheduler{
		config:       apsCfg,
		consensusCfg: consensusCfg,
	}
}

// ShuffleValidators performs a Fisher-Yates shuffle on the validator indices
// using a deterministic seed with domain separation.
func ShuffleValidators(validators []uint64, seed types.Hash) []uint64 {
	n := len(validators)
	if n <= 1 {
		return validators
	}

	// Work on a copy to avoid mutating the input.
	shuffled := make([]uint64, n)
	copy(shuffled, validators)

	for i := n - 1; i > 0; i-- {
		// Domain-separated hash: H(seed || "shuffle" || round_index)
		buf := make([]byte, 32+7+8) // seed + "shuffle" + uint64
		copy(buf, seed[:])
		copy(buf[32:], []byte("shuffle"))
		binary.BigEndian.PutUint64(buf[39:], uint64(i))

		h := crypto.Keccak256(buf)
		// Use the first 8 bytes as a random index mod (i+1).
		j := binary.BigEndian.Uint64(h[:8]) % uint64(i+1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	return shuffled
}

// SplitDuties splits a shuffled validator list into attester and proposer sets
// based on the APS config weights.
func SplitDuties(validators []uint64, config *APSConfig) (attesters, proposers []uint64) {
	n := len(validators)
	if n == 0 {
		return nil, nil
	}

	// Compute the split point: at least 1 proposer if there are validators.
	proposerCount := int(float64(n) * config.ProposerWeight)
	if proposerCount < 1 && n > 0 {
		proposerCount = 1
	}
	if proposerCount > n {
		proposerCount = n
	}

	attesterCount := n - proposerCount
	attesters = validators[:attesterCount]
	proposers = validators[attesterCount:]
	return attesters, proposers
}

// ComputeAttesterDuties assigns attestation duties for an epoch.
// Each attester is assigned to a slot and committee within that epoch.
func ComputeAttesterDuties(
	epoch Epoch,
	validatorIndices []uint64,
	randaoMix types.Hash,
	config *APSConfig,
	consensusCfg *ConsensusConfig,
) []AttesterDuty {
	if len(validatorIndices) == 0 {
		return nil
	}

	// Build the epoch seed with domain separation.
	epochSeed := computeEpochSeed(epoch, randaoMix, []byte("attester"))

	shuffled := ShuffleValidators(validatorIndices, epochSeed)

	// If APS is active, split duties and only use the attester subset.
	var attesters []uint64
	if config.IsAPSActive(epoch) {
		attesters, _ = SplitDuties(shuffled, config)
	} else {
		attesters = shuffled
	}

	slotsPerEpoch := consensusCfg.SlotsPerEpoch
	startSlot := EpochStartSlot(epoch, slotsPerEpoch)

	duties := make([]AttesterDuty, len(attesters))
	for i, valIdx := range attesters {
		slotOffset := uint64(i) % slotsPerEpoch
		committeeIdx := uint64(i) / slotsPerEpoch

		duties[i] = AttesterDuty{
			ValidatorIndex: valIdx,
			Slot:           Slot(uint64(startSlot) + slotOffset),
			CommitteeIndex: committeeIdx,
		}
	}
	return duties
}

// ComputeProposerDuties assigns proposal duties for an epoch.
// One proposer is selected per slot from the proposer subset.
func ComputeProposerDuties(
	epoch Epoch,
	validatorIndices []uint64,
	randaoMix types.Hash,
	config *APSConfig,
	consensusCfg *ConsensusConfig,
) []ProposerDuty {
	if len(validatorIndices) == 0 {
		return nil
	}

	epochSeed := computeEpochSeed(epoch, randaoMix, []byte("proposer"))

	shuffled := ShuffleValidators(validatorIndices, epochSeed)

	// If APS is active, use the proposer subset only.
	var proposers []uint64
	if config.IsAPSActive(epoch) {
		_, proposers = SplitDuties(shuffled, config)
	} else {
		proposers = shuffled
	}

	slotsPerEpoch := consensusCfg.SlotsPerEpoch
	startSlot := EpochStartSlot(epoch, slotsPerEpoch)

	duties := make([]ProposerDuty, slotsPerEpoch)
	for i := uint64(0); i < slotsPerEpoch; i++ {
		// Round-robin among proposers.
		proposerIdx := proposers[i%uint64(len(proposers))]
		duties[i] = ProposerDuty{
			ValidatorIndex: proposerIdx,
			Slot:           Slot(uint64(startSlot) + i),
		}
	}
	return duties
}

// computeEpochSeed derives a deterministic seed for the given epoch and domain.
func computeEpochSeed(epoch Epoch, randaoMix types.Hash, domain []byte) types.Hash {
	buf := make([]byte, 32+8+len(domain))
	copy(buf, randaoMix[:])
	binary.BigEndian.PutUint64(buf[32:], uint64(epoch))
	copy(buf[40:], domain)
	return crypto.Keccak256Hash(buf)
}
