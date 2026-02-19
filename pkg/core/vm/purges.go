package vm

import (
	"math/big"
	"sync/atomic"

	"github.com/eth2028/eth2028/core/types"
)

// PurgeConfig controls EVM purge behavior.
type PurgeConfig struct {
	// EnableSELFDESTRUCTRestriction enables EIP-6780 restriction:
	// SELFDESTRUCT only works when called in the same transaction that created the contract.
	EnableSELFDESTRUCTRestriction bool
	// EnableEmptyAccountPurge enables removal of empty accounts
	// (zero balance, zero nonce, no code).
	EnableEmptyAccountPurge bool
	// PurgeGasCost is the gas cost per account purge operation.
	PurgeGasCost uint64
}

// DefaultPurgeConfig returns the default purge configuration.
func DefaultPurgeConfig() PurgeConfig {
	return PurgeConfig{
		EnableSELFDESTRUCTRestriction: true,
		EnableEmptyAccountPurge:       true,
		PurgeGasCost:                  5000,
	}
}

// PurgeResult holds statistics from a purge operation.
type PurgeResult struct {
	AccountsPurged     int
	StorageSlotsPurged int
	GasUsed            uint64
}

// PurgeManager manages state purge operations for the EVM.
type PurgeManager struct {
	config  PurgeConfig
	purged  atomic.Uint64
	checked atomic.Uint64
}

// NewPurgeManager creates a new purge manager with the given configuration.
func NewPurgeManager(config PurgeConfig) *PurgeManager {
	return &PurgeManager{
		config: config,
	}
}

// ShouldPurgeAccount returns true if the account is empty and should be purged.
// An account is empty when balance=0, nonce=0, and codeSize=0.
func (pm *PurgeManager) ShouldPurgeAccount(addr types.Address, balance *big.Int, nonce uint64, codeSize int) bool {
	pm.checked.Add(1)
	if !pm.config.EnableEmptyAccountPurge {
		return false
	}
	if balance != nil && balance.Sign() > 0 {
		return false
	}
	if nonce > 0 {
		return false
	}
	if codeSize > 0 {
		return false
	}
	return true
}

// PurgeEmptyAccounts identifies and purges empty accounts from the given set.
// It checks each account using the provided getter functions and returns
// the purge result with statistics.
func (pm *PurgeManager) PurgeEmptyAccounts(
	accounts map[types.Address]bool,
	getBalance func(types.Address) *big.Int,
	getNonce func(types.Address) uint64,
	getCodeSize func(types.Address) int,
) *PurgeResult {
	result := &PurgeResult{}

	for addr := range accounts {
		bal := getBalance(addr)
		nonce := getNonce(addr)
		codeSize := getCodeSize(addr)

		if pm.ShouldPurgeAccount(addr, bal, nonce, codeSize) {
			result.AccountsPurged++
			result.GasUsed += pm.config.PurgeGasCost
			pm.purged.Add(1)
		}
	}
	return result
}

// CanSelfDestruct returns whether an account is allowed to self-destruct.
// Under EIP-6780, SELFDESTRUCT only has effect when executed in the same
// transaction that created the contract.
func (pm *PurgeManager) CanSelfDestruct(addr types.Address, createdInCurrentTx bool) bool {
	if !pm.config.EnableSELFDESTRUCTRestriction {
		// Pre-EIP-6780: always allowed.
		return true
	}
	return createdInCurrentTx
}

// Stats returns the total number of accounts purged and checked.
func (pm *PurgeManager) Stats() (purged, checked uint64) {
	return pm.purged.Load(), pm.checked.Load()
}
