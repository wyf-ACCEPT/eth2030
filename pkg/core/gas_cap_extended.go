package core

import (
	"errors"
	"fmt"

	"github.com/eth2028/eth2028/core/types"
)

// EIP-7825 extended gas cap validation with block gas limit checks,
// per-dimension caps, and dynamic gas limit adjustment logic.

// Extended gas cap errors.
var (
	ErrTxGasLimitZero          = errors.New("transaction gas limit is zero")
	ErrBlockGasLimitTooLow     = errors.New("block gas limit below minimum")
	ErrBlockGasLimitTooHigh    = errors.New("block gas limit exceeds maximum")
	ErrBlockGasLimitDelta      = errors.New("block gas limit change exceeds allowed delta")
	ErrCalldataGasCapExceeded  = errors.New("transaction calldata gas exceeds cap")
	ErrBlobCountCapExceeded    = errors.New("transaction blob count exceeds per-tx cap")
	ErrTotalBlockGasExceeded   = errors.New("transactions exceed block gas limit")
)

// Gas cap constants for multi-dimensional caps.
const (
	// MaxTxCalldataGas is the maximum calldata gas a single transaction may
	// consume. Derived from MaxTransactionGas / CalldataGasLimitRatio (EIP-7706).
	MaxTxCalldataGas uint64 = MaxTransactionGas / types.CalldataGasLimitRatio

	// MaxBlobsPerTx is the maximum number of blobs a single transaction may carry.
	// Per EIP-4844, this is 6 for Cancun.
	MaxBlobsPerTx = 6

	// DefaultMinBlockGasLimit is the minimum block gas limit for validation.
	DefaultMinBlockGasLimit uint64 = 5000

	// DefaultMaxBlockGasLimit is the maximum block gas limit (2^63 - 1).
	DefaultMaxBlockGasLimit uint64 = 1<<63 - 1
)

// GasCapConfig holds configuration for gas cap validation, allowing
// different settings per fork.
type GasCapConfig struct {
	// MaxTxGas is the maximum gas limit for a single transaction.
	MaxTxGas uint64
	// MaxTxCalldataGas is the maximum calldata gas for a single transaction.
	MaxTxCalldataGas uint64
	// MaxBlobsPerTx is the maximum blobs per transaction.
	MaxBlobsPerTx int
	// MinBlockGasLimit is the minimum block gas limit.
	MinBlockGasLimit uint64
	// MaxBlockGasLimit is the maximum block gas limit.
	MaxBlockGasLimit uint64
	// GasLimitBoundDivisor controls the max per-block gas limit change.
	GasLimitBoundDivisor uint64
}

// DefaultGasCapConfig returns the default gas cap configuration for Prague.
func DefaultGasCapConfig() GasCapConfig {
	return GasCapConfig{
		MaxTxGas:             MaxTransactionGas,
		MaxTxCalldataGas:     MaxTxCalldataGas,
		MaxBlobsPerTx:        MaxBlobsPerTx,
		MinBlockGasLimit:     DefaultMinBlockGasLimit,
		MaxBlockGasLimit:     DefaultMaxBlockGasLimit,
		GasLimitBoundDivisor: GasLimitBoundDivisor,
	}
}

// GasCapConfigForFork returns the appropriate gas cap configuration for
// the given chain config and block timestamp.
func GasCapConfigForFork(config *ChainConfig, time uint64) GasCapConfig {
	cfg := DefaultGasCapConfig()
	if config == nil {
		return cfg
	}

	// Post-Prague: EIP-7825 tx gas cap is active.
	if config.IsPrague(time) {
		cfg.MaxTxGas = MaxTransactionGas
	}

	// Post-Prague: update max blobs per tx based on blob schedule.
	sched := GetBlobSchedule(config, time)
	cfg.MaxBlobsPerTx = int(sched.Max)

	return cfg
}

// ValidateTransactionGasCaps performs comprehensive gas cap validation on a
// transaction, checking execution gas, calldata gas, and blob count limits.
func ValidateTransactionGasCaps(tx *types.Transaction, config GasCapConfig) error {
	// Zero gas limit is always invalid.
	if tx.Gas() == 0 {
		return ErrTxGasLimitZero
	}

	// EIP-7825: check execution gas cap.
	if tx.Gas() > config.MaxTxGas {
		return fmt.Errorf("%w: gas %d exceeds max %d",
			ErrTxGasLimitExceeded, tx.Gas(), config.MaxTxGas)
	}

	// EIP-7706: check calldata gas cap.
	calldataGas := tx.CalldataGas()
	if calldataGas > config.MaxTxCalldataGas {
		return fmt.Errorf("%w: calldata gas %d exceeds max %d",
			ErrCalldataGasCapExceeded, calldataGas, config.MaxTxCalldataGas)
	}

	// EIP-4844: check blob count cap.
	if blobHashes := tx.BlobHashes(); len(blobHashes) > config.MaxBlobsPerTx {
		return fmt.Errorf("%w: %d blobs exceeds max %d",
			ErrBlobCountCapExceeded, len(blobHashes), config.MaxBlobsPerTx)
	}

	return nil
}

// ValidateBlockGasLimit validates the block gas limit against consensus rules:
//   - The gas limit must be within [MinBlockGasLimit, MaxBlockGasLimit].
//   - The gas limit change from parent to child must not exceed parent/GasLimitBoundDivisor.
func ValidateBlockGasLimit(parent, header *types.Header, config GasCapConfig) error {
	if header.GasLimit < config.MinBlockGasLimit {
		return fmt.Errorf("%w: %d < %d", ErrBlockGasLimitTooLow, header.GasLimit, config.MinBlockGasLimit)
	}
	if header.GasLimit > config.MaxBlockGasLimit {
		return fmt.Errorf("%w: %d > %d", ErrBlockGasLimitTooHigh, header.GasLimit, config.MaxBlockGasLimit)
	}

	// Check the 1/GasLimitBoundDivisor bound on gas limit change.
	divisor := config.GasLimitBoundDivisor
	if divisor == 0 {
		divisor = 1024
	}
	maxDelta := parent.GasLimit / divisor
	if maxDelta == 0 {
		maxDelta = 1
	}

	var diff uint64
	if header.GasLimit > parent.GasLimit {
		diff = header.GasLimit - parent.GasLimit
	} else {
		diff = parent.GasLimit - header.GasLimit
	}

	if diff > maxDelta {
		return fmt.Errorf("%w: diff %d exceeds max delta %d (parent gas limit %d)",
			ErrBlockGasLimitDelta, diff, maxDelta, parent.GasLimit)
	}

	return nil
}

// ValidateBlockGasUsage checks that the cumulative gas used by transactions
// in a block does not exceed the block gas limit.
func ValidateBlockGasUsage(header *types.Header, txs []*types.Transaction) error {
	var totalGas uint64
	for _, tx := range txs {
		totalGas += tx.Gas()
		if totalGas > header.GasLimit {
			return fmt.Errorf("%w: cumulative %d > limit %d",
				ErrTotalBlockGasExceeded, totalGas, header.GasLimit)
		}
	}
	return nil
}

// DynamicGasLimitAdjustment computes the target gas limit for the next block
// based on the parent's utilization, implementing a smooth adjustment toward
// a target utilization of 50% (EIP-1559 elasticity).
//
// The adjustment rate is bounded by parent.GasLimit / 1024 per block, ensuring
// a gradual transition even under sudden load changes.
func DynamicGasLimitAdjustment(parentGasLimit, parentGasUsed, targetGasLimit uint64) uint64 {
	// If we have a specific target (from the schedule), move toward it.
	if targetGasLimit > 0 {
		return moveTowardTarget(parentGasLimit, targetGasLimit)
	}

	// Otherwise, use utilization-based adjustment.
	target := parentGasLimit / ElasticityMultiplier
	maxDelta := parentGasLimit / GasLimitBoundDivisor
	if maxDelta == 0 {
		maxDelta = 1
	}

	if parentGasUsed > target {
		// Over-utilized: increase gas limit.
		delta := (parentGasUsed - target) * maxDelta / target
		if delta > maxDelta {
			delta = maxDelta
		}
		if delta == 0 {
			delta = 1
		}
		return parentGasLimit + delta
	} else if parentGasUsed < target {
		// Under-utilized: decrease gas limit.
		delta := (target - parentGasUsed) * maxDelta / target
		if delta > maxDelta {
			delta = maxDelta
		}
		if delta == 0 {
			delta = 1
		}
		if parentGasLimit > delta+MinGasLimit {
			return parentGasLimit - delta
		}
		return MinGasLimit
	}
	return parentGasLimit
}

// moveTowardTarget adjusts the gas limit toward the target, bounded by 1/1024.
func moveTowardTarget(current, target uint64) uint64 {
	maxDelta := current / GasLimitBoundDivisor
	if maxDelta == 0 {
		maxDelta = 1
	}

	if target > current {
		diff := target - current
		if diff > maxDelta {
			diff = maxDelta
		}
		return current + diff
	} else if target < current {
		diff := current - target
		if diff > maxDelta {
			diff = maxDelta
		}
		if current > diff && current-diff >= MinGasLimit {
			return current - diff
		}
		return MinGasLimit
	}
	return current
}

// ValidateTransactionGasWithFork is a convenience function that validates a
// transaction's gas caps based on the active fork rules. It combines the
// EIP-7825 tx gas cap check with calldata and blob cap checks.
func ValidateTransactionGasWithFork(tx *types.Transaction, config *ChainConfig, time uint64) error {
	// Pre-Prague: only check that gas limit is positive.
	if config == nil || !config.IsPrague(time) {
		if tx.Gas() == 0 {
			return ErrTxGasLimitZero
		}
		return nil
	}

	capCfg := GasCapConfigForFork(config, time)
	return ValidateTransactionGasCaps(tx, capCfg)
}

// ValidateGasCapInvariant checks the invariant that a transaction's gas limit
// does not exceed the block gas limit AND the per-tx cap (whichever is smaller).
func ValidateGasCapInvariant(tx *types.Transaction, blockGasLimit uint64, config GasCapConfig) error {
	effectiveCap := config.MaxTxGas
	if blockGasLimit < effectiveCap {
		effectiveCap = blockGasLimit
	}

	if tx.Gas() > effectiveCap {
		return fmt.Errorf("tx gas %d exceeds effective cap %d (block limit %d, tx cap %d)",
			tx.Gas(), effectiveCap, blockGasLimit, config.MaxTxGas)
	}
	return nil
}

// EstimateBlockTxCapacity estimates how many transactions of the given average
// gas cost can fit in a block with the specified gas limit.
func EstimateBlockTxCapacity(blockGasLimit, avgTxGas uint64) uint64 {
	if avgTxGas == 0 {
		return 0
	}
	return blockGasLimit / avgTxGas
}

// GasCapCheckResult holds the results of a multi-check gas cap validation,
// useful for returning all violations at once instead of stopping at the first.
type GasCapCheckResult struct {
	Errors []error
}

// Ok returns true if no errors were found.
func (r *GasCapCheckResult) Ok() bool {
	return len(r.Errors) == 0
}

// Error returns all errors joined as a single error, or nil if no errors.
func (r *GasCapCheckResult) Error() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return errors.Join(r.Errors...)
}

// ValidateAllGasCaps performs all gas cap checks and collects all violations.
func ValidateAllGasCaps(tx *types.Transaction, blockGasLimit uint64, config GasCapConfig) *GasCapCheckResult {
	result := &GasCapCheckResult{}

	if tx.Gas() == 0 {
		result.Errors = append(result.Errors, ErrTxGasLimitZero)
	}
	if tx.Gas() > config.MaxTxGas {
		result.Errors = append(result.Errors, fmt.Errorf("%w: %d > %d", ErrTxGasLimitExceeded, tx.Gas(), config.MaxTxGas))
	}
	if tx.Gas() > blockGasLimit {
		result.Errors = append(result.Errors, fmt.Errorf("%w: tx gas %d > block limit %d", ErrGasLimitExceeded, tx.Gas(), blockGasLimit))
	}

	calldataGas := tx.CalldataGas()
	if calldataGas > config.MaxTxCalldataGas {
		result.Errors = append(result.Errors, fmt.Errorf("%w: %d > %d", ErrCalldataGasCapExceeded, calldataGas, config.MaxTxCalldataGas))
	}

	if blobHashes := tx.BlobHashes(); len(blobHashes) > config.MaxBlobsPerTx {
		result.Errors = append(result.Errors, fmt.Errorf("%w: %d > %d", ErrBlobCountCapExceeded, len(blobHashes), config.MaxBlobsPerTx))
	}

	return result
}
