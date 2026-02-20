package consensus

import (
	"errors"
	"fmt"
)

// Extended config errors.
var (
	ErrConfigInvalidSlotDuration = errors.New("config: slot duration must be > 0")
	ErrConfigInvalidEpochLength  = errors.New("config: slots per epoch must be > 0")
	ErrConfigInvalidCommittee    = errors.New("config: committee size invalid")
	ErrConfigForkMismatch        = errors.New("config: fork schedule inconsistency")
)

// Reward and penalty constants (in Gwei) for the extended config.
const (
	// ExtBaseRewardFactor is used to compute the base reward per validator.
	ExtBaseRewardFactor uint64 = 64

	// ExtBaseRewardPerEpochDivisor divides total active balance for reward calc.
	ExtBaseRewardPerEpochDivisor uint64 = 8

	// ExtProposerRewardQuotient is the divisor for the proposer's share.
	ExtProposerRewardQuotient uint64 = 8

	// ExtWhistleblowerRewardQuotient is the divisor for whistleblower rewards.
	ExtWhistleblowerRewardQuotient uint64 = 512

	// ExtInactivityPenaltyQuotient for mainnet Deneb.
	ExtInactivityPenaltyQuotient uint64 = 1 << 26 // 67,108,864

	// ExtMinSlashingPenaltyQuotient for mainnet Deneb.
	ExtMinSlashingPenaltyQuotient uint64 = 32

	// ExtProportionalSlashingMultiplier for mainnet Deneb.
	ExtProportionalSlashingMultiplier uint64 = 3
)

// Committee and validator constants for the extended config.
const (
	// ExtTargetCommitteeSize is the desired committee size.
	ExtTargetCommitteeSize uint64 = 128

	// ExtMaxCommitteesPerSlotDefault is the default max committees per slot.
	ExtMaxCommitteesPerSlotDefault uint64 = 64

	// ExtMaxValidatorsPerCommittee is the maximum number of validators per committee.
	ExtMaxValidatorsPerCommittee uint64 = 2048

	// ExtMaxValidatorsDefault is the effective validator limit.
	ExtMaxValidatorsDefault uint64 = 1 << 22 // 4,194,304

	// ExtShuffleRoundCount is the number of rounds for the shuffle algorithm.
	ExtShuffleRoundCount uint64 = 90

	// ExtTargetAggregatorsPerCommittee is the target for random aggregator selection.
	ExtTargetAggregatorsPerCommittee uint64 = 16
)

// Fork identifiers.
type ForkID string

const (
	ExtForkPhase0       ForkID = "phase0"
	ExtForkAltair       ForkID = "altair"
	ExtForkBellatrix    ForkID = "bellatrix"
	ExtForkCapella      ForkID = "capella"
	ExtForkDeneb        ForkID = "deneb"
	ExtForkElectra      ForkID = "electra"
	ExtForkGlamsterdam  ForkID = "glamsterdam"
	ExtForkHogota       ForkID = "hogota"
)

// ForkScheduleEntry records the activation epoch for a fork.
type ForkScheduleEntry struct {
	Name  ForkID
	Epoch Epoch
}

// ExtConsensusConfig is the full consensus chain configuration with fork
// timestamps, slot durations, epoch lengths, committee sizes, max validators,
// reward constants, and the fork schedule.
type ExtConsensusConfig struct {
	// Timing.
	SecondsPerSlot    uint64
	SlotsPerEpoch     uint64
	MinGenesisTime    uint64
	EpochsForFinality uint64

	// Committee.
	TargetCommitteeSize        uint64
	MaxCommitteesPerSlot       uint64
	MaxValidatorsPerCommittee  uint64
	MaxValidators              uint64
	ShuffleRounds              uint64
	TargetAggregatorsPerCommit uint64

	// Rewards and penalties.
	BaseRewardFactor              uint64
	ProposerRewardQuotient        uint64
	WhistleblowerRewardQuotient   uint64
	InactivityPenaltyQuotient     uint64
	MinSlashingPenaltyQuotient    uint64
	ProportionalSlashingMultiplier uint64

	// Fork schedule.
	ForkSchedule []ForkScheduleEntry
}

// DefaultExtConsensusConfig returns the standard Ethereum mainnet configuration.
func DefaultExtConsensusConfig() *ExtConsensusConfig {
	return &ExtConsensusConfig{
		SecondsPerSlot:                 12,
		SlotsPerEpoch:                  32,
		MinGenesisTime:                 0,
		EpochsForFinality:              2,
		TargetCommitteeSize:            ExtTargetCommitteeSize,
		MaxCommitteesPerSlot:           ExtMaxCommitteesPerSlotDefault,
		MaxValidatorsPerCommittee:      ExtMaxValidatorsPerCommittee,
		MaxValidators:                  ExtMaxValidatorsDefault,
		ShuffleRounds:                  ExtShuffleRoundCount,
		TargetAggregatorsPerCommit:     ExtTargetAggregatorsPerCommittee,
		BaseRewardFactor:               ExtBaseRewardFactor,
		ProposerRewardQuotient:         ExtProposerRewardQuotient,
		WhistleblowerRewardQuotient:    ExtWhistleblowerRewardQuotient,
		InactivityPenaltyQuotient:      ExtInactivityPenaltyQuotient,
		MinSlashingPenaltyQuotient:     ExtMinSlashingPenaltyQuotient,
		ProportionalSlashingMultiplier: ExtProportionalSlashingMultiplier,
		ForkSchedule: []ForkScheduleEntry{
			{Name: ExtForkPhase0, Epoch: 0},
			{Name: ExtForkAltair, Epoch: 74240},
			{Name: ExtForkBellatrix, Epoch: 144896},
			{Name: ExtForkCapella, Epoch: 194048},
			{Name: ExtForkDeneb, Epoch: 269568},
		},
	}
}

// GlamsterdamConfig returns the config for the Glamsterdam upgrade with
// fast confirmation, ePBS, FOCIL, peerDAS, native AA, and BALS.
func GlamsterdamConfig() *ExtConsensusConfig {
	cfg := DefaultExtConsensusConfig()
	cfg.SecondsPerSlot = 12
	cfg.SlotsPerEpoch = 32
	cfg.EpochsForFinality = 2
	cfg.ForkSchedule = append(cfg.ForkSchedule, ForkScheduleEntry{
		Name:  ExtForkGlamsterdam,
		Epoch: 300000, // placeholder
	})
	return cfg
}

// HogotaConfig returns the config for the Hogota upgrade with blob
// throughput increases, local blob reconstruction, and repricing.
func HogotaConfig() *ExtConsensusConfig {
	cfg := GlamsterdamConfig()
	cfg.ForkSchedule = append(cfg.ForkSchedule, ForkScheduleEntry{
		Name:  ExtForkHogota,
		Epoch: 350000, // placeholder
	})
	return cfg
}

// QuickSlotsExtConfig returns the config for 6-second slots and 4-slot epochs
// with 1-epoch finality.
func QuickSlotsExtConfig() *ExtConsensusConfig {
	cfg := DefaultExtConsensusConfig()
	cfg.SecondsPerSlot = 6
	cfg.SlotsPerEpoch = 4
	cfg.EpochsForFinality = 1
	return cfg
}

// Validate checks all config parameters for correctness.
func (c *ExtConsensusConfig) Validate() error {
	if c.SecondsPerSlot == 0 {
		return ErrConfigInvalidSlotDuration
	}
	if c.SlotsPerEpoch == 0 {
		return ErrConfigInvalidEpochLength
	}
	if c.EpochsForFinality == 0 {
		return fmt.Errorf("config: epochs for finality must be > 0")
	}
	if c.TargetCommitteeSize == 0 {
		return ErrConfigInvalidCommittee
	}
	if c.MaxCommitteesPerSlot == 0 {
		return ErrConfigInvalidCommittee
	}

	// Validate fork schedule is in ascending epoch order.
	for i := 1; i < len(c.ForkSchedule); i++ {
		if c.ForkSchedule[i].Epoch < c.ForkSchedule[i-1].Epoch {
			return fmt.Errorf("%w: fork %s epoch %d < %s epoch %d",
				ErrConfigForkMismatch,
				c.ForkSchedule[i].Name, c.ForkSchedule[i].Epoch,
				c.ForkSchedule[i-1].Name, c.ForkSchedule[i-1].Epoch)
		}
	}
	return nil
}

// EpochDuration returns the total duration of one epoch in seconds.
func (c *ExtConsensusConfig) EpochDuration() uint64 {
	return c.SecondsPerSlot * c.SlotsPerEpoch
}

// IsSingleEpochFinality returns true if using 1-epoch finality.
func (c *ExtConsensusConfig) IsSingleEpochFinality() bool {
	return c.EpochsForFinality == 1
}

// ForkAtEpoch returns the highest fork active at the given epoch.
func (c *ExtConsensusConfig) ForkAtEpoch(epoch Epoch) ForkID {
	var active ForkID
	for _, entry := range c.ForkSchedule {
		if epoch >= entry.Epoch {
			active = entry.Name
		}
	}
	return active
}

// IsForkActive returns true if the named fork is active at the given epoch.
func (c *ExtConsensusConfig) IsForkActive(fork ForkID, epoch Epoch) bool {
	for _, entry := range c.ForkSchedule {
		if entry.Name == fork {
			return epoch >= entry.Epoch
		}
	}
	return false
}

// ForkEpoch returns the activation epoch for a given fork, or FarFutureEpoch
// if the fork is not in the schedule.
func (c *ExtConsensusConfig) ForkEpoch(fork ForkID) Epoch {
	for _, entry := range c.ForkSchedule {
		if entry.Name == fork {
			return entry.Epoch
		}
	}
	return FarFutureEpoch
}

// ToBasicConfig converts to the basic ConsensusConfig type.
func (c *ExtConsensusConfig) ToBasicConfig() *ConsensusConfig {
	return &ConsensusConfig{
		SecondsPerSlot:    c.SecondsPerSlot,
		SlotsPerEpoch:     c.SlotsPerEpoch,
		MinGenesisTime:    c.MinGenesisTime,
		EpochsForFinality: c.EpochsForFinality,
	}
}
