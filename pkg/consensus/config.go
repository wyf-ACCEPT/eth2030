package consensus

import "fmt"

// ConsensusConfig holds consensus-layer parameters.
type ConsensusConfig struct {
	SecondsPerSlot   uint64 // slot duration in seconds
	SlotsPerEpoch    uint64 // number of slots per epoch
	MinGenesisTime   uint64 // minimum genesis timestamp
	EpochsForFinality uint64 // epochs required for finalization (2 = Casper FFG, 1 = single-epoch)
}

// DefaultConfig returns the standard Ethereum mainnet consensus config.
func DefaultConfig() *ConsensusConfig {
	return &ConsensusConfig{
		SecondsPerSlot:   12,
		SlotsPerEpoch:    32,
		MinGenesisTime:   0,
		EpochsForFinality: 2,
	}
}

// QuickSlotsConfig returns the config for the quick-slots + 1-epoch finality upgrade.
func QuickSlotsConfig() *ConsensusConfig {
	return &ConsensusConfig{
		SecondsPerSlot:   6,
		SlotsPerEpoch:    4,
		MinGenesisTime:   0,
		EpochsForFinality: 1,
	}
}

// Validate checks config constraints and returns an error if invalid.
func (c *ConsensusConfig) Validate() error {
	if c.SecondsPerSlot == 0 {
		return fmt.Errorf("consensus: SecondsPerSlot must be > 0")
	}
	if c.SlotsPerEpoch == 0 {
		return fmt.Errorf("consensus: SlotsPerEpoch must be > 0")
	}
	if c.EpochsForFinality == 0 {
		return fmt.Errorf("consensus: EpochsForFinality must be > 0")
	}
	return nil
}

// EpochDuration returns the total duration of one epoch in seconds.
func (c *ConsensusConfig) EpochDuration() uint64 {
	return c.SecondsPerSlot * c.SlotsPerEpoch
}

// IsSingleEpochFinality returns true if the config uses 1-epoch finality.
func (c *ConsensusConfig) IsSingleEpochFinality() bool {
	return c.EpochsForFinality == 1
}
