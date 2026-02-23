package main

import (
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/eth2030/eth2030/geth"
)

// precompileInjector manages ETH2030 custom precompile injection into geth
// EVM instances. Custom precompiles only activate at future fork timestamps
// (Glamsterdam, Hogota, I+), so for current mainnet this is a no-op.
type precompileInjector struct {
	glamsterdamTime *uint64
	hogotaTime      *uint64
	iPlusTime       *uint64
}

// newPrecompileInjector creates an injector configured for the given fork schedule.
func newPrecompileInjector(glamsterdam, hogota, iPlus *uint64) *precompileInjector {
	return &precompileInjector{
		glamsterdamTime: glamsterdam,
		hogotaTime:      hogota,
		iPlusTime:       iPlus,
	}
}

// forkLevelAtTime determines the ETH2030 fork level active at the given block time.
func (pi *precompileInjector) forkLevelAtTime(time uint64) geth.Eth2028ForkLevel {
	if pi.iPlusTime != nil && time >= *pi.iPlusTime {
		return geth.ForkLevelIPlus
	}
	if pi.hogotaTime != nil && time >= *pi.hogotaTime {
		return geth.ForkLevelHogota
	}
	if pi.glamsterdamTime != nil && time >= *pi.glamsterdamTime {
		return geth.ForkLevelGlamsterdam
	}
	return geth.ForkLevelPrague
}

// InjectIntoEVM sets custom precompiles on a go-ethereum EVM instance
// if the block time indicates a future ETH2030 fork is active.
func (pi *precompileInjector) InjectIntoEVM(evm *gethvm.EVM, rules params.Rules, blockTime uint64) {
	forkLevel := pi.forkLevelAtTime(blockTime)
	if forkLevel > geth.ForkLevelPrague {
		precompiles := geth.InjectCustomPrecompiles(rules, forkLevel)
		evm.SetPrecompiles(precompiles)
	}
}

// CustomAddresses returns the precompile addresses active at the given block time.
func (pi *precompileInjector) CustomAddresses(blockTime uint64) []gethcommon.Address {
	forkLevel := pi.forkLevelAtTime(blockTime)
	return geth.CustomPrecompileAddresses(forkLevel)
}
