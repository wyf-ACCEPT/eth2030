package core

import "math/big"

// ChainConfig holds chain-level configuration for fork scheduling.
// Post-merge, all forks are activated by timestamp.
type ChainConfig struct {
	ChainID       *big.Int
	ShanghaiTime  *uint64
	CancunTime    *uint64
	PragueTime    *uint64
	AmsterdamTime *uint64
}

func isTimestampForked(forkTime *uint64, blockTime uint64) bool {
	if forkTime == nil {
		return false
	}
	return *forkTime <= blockTime
}

// IsShanghai returns whether the given block time is at or past the Shanghai fork.
func (c *ChainConfig) IsShanghai(time uint64) bool {
	return isTimestampForked(c.ShanghaiTime, time)
}

// IsCancun returns whether the given block time is at or past the Cancun fork.
func (c *ChainConfig) IsCancun(time uint64) bool {
	return isTimestampForked(c.CancunTime, time)
}

// IsPrague returns whether the given block time is at or past the Prague fork.
func (c *ChainConfig) IsPrague(time uint64) bool {
	return isTimestampForked(c.PragueTime, time)
}

// IsAmsterdam returns whether the given block time is at or past the Amsterdam fork.
func (c *ChainConfig) IsAmsterdam(time uint64) bool {
	return isTimestampForked(c.AmsterdamTime, time)
}

func newUint64(v uint64) *uint64 { return &v }

// MainnetConfig is the chain config for Ethereum mainnet.
var MainnetConfig = &ChainConfig{
	ChainID:       big.NewInt(1),
	ShanghaiTime:  newUint64(1681338455),
	CancunTime:    newUint64(1710338135),
	PragueTime:    nil, // not yet scheduled
	AmsterdamTime: nil, // not yet scheduled
}

// TestConfig is a chain config with all forks active at genesis (time 0).
var TestConfig = &ChainConfig{
	ChainID:       big.NewInt(1337),
	ShanghaiTime:  newUint64(0),
	CancunTime:    newUint64(0),
	PragueTime:    newUint64(0),
	AmsterdamTime: newUint64(0),
}
