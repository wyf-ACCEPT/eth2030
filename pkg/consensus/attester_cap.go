package consensus

// Attester stake cap limits the maximum effective balance that counts toward
// attestation weight. This prevents large validators from having outsized
// influence and targets ~128K active attesters with 16M ETH staked.
// Part of the 2029+ Consensus Layer accessibility roadmap.

import "errors"

// Attester cap errors.
var (
	ErrCapBelowMinStake    = errors.New("attester cap: MaxAttesterBalance must be >= 32 ETH")
	ErrCapEpochNotPositive = errors.New("attester cap: CapEpoch must be positive or zero")
)

const (
	// DefaultAttesterCap is 128 ETH in Gwei. With 16M ETH staked this yields
	// ~125,000 "virtual" attesters, keeping the attestation committee manageable.
	DefaultAttesterCap uint64 = 128 * GweiPerETH

	// MinStakeGwei is the minimum stake required for a validator (32 ETH in Gwei).
	MinStakeGwei uint64 = 32 * GweiPerETH
)

// AttesterCapConfig holds the configuration for the attester stake cap.
type AttesterCapConfig struct {
	MaxAttesterBalance uint64 // maximum effective balance for attestation (Gwei)
	CapEpoch           Epoch  // epoch at which the cap activates
}

// DefaultAttesterCapConfig returns the default attester cap configuration.
func DefaultAttesterCapConfig() *AttesterCapConfig {
	return &AttesterCapConfig{
		MaxAttesterBalance: DefaultAttesterCap,
		CapEpoch:           0,
	}
}

// IsCapActive returns true if the attester cap is active at the given epoch.
func IsCapActive(epoch Epoch, config *AttesterCapConfig) bool {
	return epoch >= config.CapEpoch
}

// CapEffectiveBalance returns the effective balance capped at maxCap.
// If balance <= maxCap, returns balance unchanged.
func CapEffectiveBalance(balance, maxCap uint64) uint64 {
	if balance > maxCap {
		return maxCap
	}
	return balance
}

// ApplyAttesterCap caps the effective balance of all active validators in
// the set at the given epoch. Only validators active at the epoch are affected.
// The cap is applied to the EffectiveBalance field.
func ApplyAttesterCap(validators *ValidatorSet, config *AttesterCapConfig, epoch Epoch) {
	if !IsCapActive(epoch, config) {
		return
	}

	validators.mu.Lock()
	defer validators.mu.Unlock()

	for _, v := range validators.validators {
		if v.IsActive(epoch) {
			v.EffectiveBalance = CapEffectiveBalance(v.EffectiveBalance, config.MaxAttesterBalance)
		}
	}
}

// ValidateAttesterCapConfig checks that an attester cap config is sane.
// Returns an error if MaxAttesterBalance is below the minimum stake (32 ETH).
func ValidateAttesterCapConfig(config *AttesterCapConfig) error {
	if config == nil {
		return errors.New("attester cap: config is nil")
	}
	if config.MaxAttesterBalance < MinStakeGwei {
		return ErrCapBelowMinStake
	}
	return nil
}

// SupermajorityThresholds holds the recomputed supermajority thresholds after
// cap activation.
type SupermajorityThresholds struct {
	TotalWeight     uint64 // total capped weight
	TwoThirdsWeight uint64 // 2/3 threshold for finality
	OneThirdWeight  uint64 // 1/3 threshold for liveness
}

// MigrateSupermajorityThresholds recomputes supermajority thresholds based on
// capped effective balances. This should be called after the attester cap
// activates to ensure finality thresholds reflect capped weights.
func MigrateSupermajorityThresholds(validators *ValidatorSet, config *AttesterCapConfig, epoch Epoch) *SupermajorityThresholds {
	totalWeight := TotalCappedWeight(validators, config, epoch)
	return &SupermajorityThresholds{
		TotalWeight:     totalWeight,
		TwoThirdsWeight: (totalWeight * 2) / 3,
		OneThirdWeight:  totalWeight / 3,
	}
}

// TotalCappedWeight returns the total effective balance of all active validators
// after applying the attester cap. Useful for computing supermajority thresholds.
func TotalCappedWeight(validators *ValidatorSet, config *AttesterCapConfig, epoch Epoch) uint64 {
	validators.mu.RLock()
	defer validators.mu.RUnlock()

	var total uint64
	for _, v := range validators.validators {
		if v.IsActive(epoch) {
			bal := v.EffectiveBalance
			if IsCapActive(epoch, config) {
				bal = CapEffectiveBalance(bal, config.MaxAttesterBalance)
			}
			total += bal
		}
	}
	return total
}
