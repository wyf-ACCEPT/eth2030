package consensus

import (
	"errors"
	"sync"
)

// EIP-7251: Increase the MAX_EFFECTIVE_BALANCE.
// Allows validators to have larger effective balances while maintaining
// the 32 ETH lower bound for activation.

const (
	// GweiPerETH is the number of Gwei in one ETH.
	GweiPerETH uint64 = 1_000_000_000

	// MinActivationBalance is the minimum stake required to activate a validator (32 ETH).
	MinActivationBalance uint64 = 32 * GweiPerETH

	// MaxEffectiveBalance is the maximum effective balance a validator can have (2048 ETH).
	// This is the EIP-7251 increased limit (previously 32 ETH).
	MaxEffectiveBalance uint64 = 2048 * GweiPerETH

	// EffectiveBalanceIncrement is the granularity of effective balance changes.
	EffectiveBalanceIncrement uint64 = 1 * GweiPerETH

	// HysteresisQuotient prevents effective balance from oscillating.
	HysteresisQuotient uint64 = 4

	// HysteresisDownwardMultiplier controls downward hysteresis threshold.
	HysteresisDownwardMultiplier uint64 = 1

	// HysteresisUpwardMultiplier controls upward hysteresis threshold.
	HysteresisUpwardMultiplier uint64 = 5

	// FarFutureEpoch is a sentinel value for validators not yet exited.
	FarFutureEpoch Epoch = ^Epoch(0)

	// CompoundingWithdrawalPrefix marks a validator for compounding rewards (0x02).
	CompoundingWithdrawalPrefix byte = 0x02
)

var (
	ErrValidatorNotFound     = errors.New("validator not found")
	ErrValidatorAlreadyAdded = errors.New("validator already exists")
	ErrValidatorNotActive    = errors.New("validator not active")
	ErrValidatorSlashed      = errors.New("validator is slashed")
)

// ValidatorBalance represents a consensus-layer validator with EIP-7251 fields.
type ValidatorBalance struct {
	Pubkey               [48]byte
	WithdrawalCredentials [32]byte
	EffectiveBalance     uint64 // in Gwei
	Slashed              bool
	ActivationEpoch      Epoch
	ExitEpoch            Epoch
}

// IsActive returns true if the validator is active at the given epoch.
func (v *ValidatorBalance) IsActive(epoch Epoch) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}

// IsEligibleForActivation returns true if the validator can be activated.
// Requires: not yet activated, not slashed, effective balance >= MinActivationBalance.
func (v *ValidatorBalance) IsEligibleForActivation() bool {
	return v.ActivationEpoch == FarFutureEpoch &&
		!v.Slashed &&
		v.EffectiveBalance >= MinActivationBalance
}

// HasCompoundingCredentials returns true if the validator has the 0x02
// compounding withdrawal prefix.
func (v *ValidatorBalance) HasCompoundingCredentials() bool {
	return len(v.WithdrawalCredentials) > 0 &&
		v.WithdrawalCredentials[0] == CompoundingWithdrawalPrefix
}

// ValidatorSet is a thread-safe collection of validators indexed by public key.
type ValidatorSet struct {
	mu         sync.RWMutex
	validators map[[48]byte]*ValidatorBalance
}

// NewValidatorSet creates an empty validator set.
func NewValidatorSet() *ValidatorSet {
	return &ValidatorSet{
		validators: make(map[[48]byte]*ValidatorBalance),
	}
}

// Add inserts a validator into the set.
func (vs *ValidatorSet) Add(v *ValidatorBalance) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if _, exists := vs.validators[v.Pubkey]; exists {
		return ErrValidatorAlreadyAdded
	}
	vs.validators[v.Pubkey] = v
	return nil
}

// Remove deletes a validator from the set.
func (vs *ValidatorSet) Remove(pubkey [48]byte) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if _, exists := vs.validators[pubkey]; !exists {
		return ErrValidatorNotFound
	}
	delete(vs.validators, pubkey)
	return nil
}

// Get returns the validator with the given public key.
func (vs *ValidatorSet) Get(pubkey [48]byte) (*ValidatorBalance, error) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	v, exists := vs.validators[pubkey]
	if !exists {
		return nil, ErrValidatorNotFound
	}
	return v, nil
}

// ActiveCount returns the number of active validators at the given epoch.
func (vs *ValidatorSet) ActiveCount(epoch Epoch) int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	count := 0
	for _, v := range vs.validators {
		if v.IsActive(epoch) {
			count++
		}
	}
	return count
}

// Len returns the total number of validators in the set.
func (vs *ValidatorSet) Len() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.validators)
}

// ComputeEffectiveBalance calculates the effective balance for a validator
// given its actual balance, applying hysteresis to prevent oscillation.
// Per EIP-7251, the effective balance is capped at MaxEffectiveBalance.
//
// The hysteresis works as follows:
//   - Downward threshold: currentEffective - downwardAdjust
//     If balance < threshold, reduce effective balance.
//   - Upward threshold: currentEffective + upwardAdjust
//     If balance >= threshold, increase effective balance.
func ComputeEffectiveBalance(balance uint64, currentEffective uint64) uint64 {
	halfIncrement := EffectiveBalanceIncrement / HysteresisQuotient
	downwardAdjust := halfIncrement * HysteresisDownwardMultiplier
	upwardAdjust := halfIncrement * HysteresisUpwardMultiplier

	// Cap the maximum.
	maxEB := MaxEffectiveBalance

	if balance+downwardAdjust < currentEffective || currentEffective+upwardAdjust < balance {
		// Snap to nearest increment, capped at max.
		newEffective := (balance / EffectiveBalanceIncrement) * EffectiveBalanceIncrement
		if newEffective > maxEB {
			newEffective = maxEB
		}
		return newEffective
	}
	return currentEffective
}

// UpdateEffectiveBalance updates a validator's effective balance in place
// based on its actual balance.
func UpdateEffectiveBalance(v *ValidatorBalance, balance uint64) {
	v.EffectiveBalance = ComputeEffectiveBalance(balance, v.EffectiveBalance)
}
