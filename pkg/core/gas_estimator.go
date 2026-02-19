package core

import (
	"errors"
	"math"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// Gas estimation errors.
var (
	ErrGasEstimationFailed = errors.New("gas estimation failed: execution always fails")
	ErrIntrinsicGasTooHigh = errors.New("intrinsic gas exceeds block gas limit")
	ErrGasUint64Overflow   = errors.New("gas uint64 overflow")
)

// Additional intrinsic gas constants used by the gas estimator.
// TxGas, TxDataZeroGas, TxDataNonZeroGas, and TxCreateGas are in processor.go.
const (
	// TxGasContractCreation is the total base gas for contract creation (21000 + 32000).
	TxGasContractCreation uint64 = TxGas + TxCreateGas

	// TxAccessListAddressGas is the gas cost per access list address entry.
	TxAccessListAddressGas uint64 = 2400

	// TxAccessListStorageKeyGas is the gas cost per access list storage key.
	TxAccessListStorageKeyGas uint64 = 1900

	// AccessListTxGas is the flat surcharge for access list transaction types.
	AccessListTxGas uint64 = 2600
)

// CallMsg contains parameters for a simulated call or gas estimation.
type CallMsg struct {
	From     types.Address
	To       *types.Address // nil for contract creation
	Gas      uint64
	GasPrice *big.Int
	Value    *big.Int
	Data     []byte
}

// GasEstimatorConfig holds tunable parameters for the gas estimator.
type GasEstimatorConfig struct {
	// DefaultGasLimit is the block gas limit used as upper bound.
	DefaultGasLimit uint64
	// MaxIterations is the maximum number of binary search steps.
	MaxIterations int
	// GasCapMultiplier is a safety margin applied to the estimated gas.
	GasCapMultiplier float64
}

// DefaultGasEstimatorConfig returns a GasEstimatorConfig with sensible defaults.
func DefaultGasEstimatorConfig() GasEstimatorConfig {
	return GasEstimatorConfig{
		DefaultGasLimit:  30_000_000,
		MaxIterations:    20,
		GasCapMultiplier: 1.2,
	}
}

// GasEstimator estimates the gas required for transaction execution.
type GasEstimator struct {
	config GasEstimatorConfig

	// executor is the function used to simulate transaction execution.
	// It receives a CallMsg with a specific gas limit and returns whether
	// execution succeeded (true) or failed due to out-of-gas (false).
	executor func(msg CallMsg, gas uint64) (bool, uint64, error)
}

// NewGasEstimator creates a new GasEstimator with the given configuration.
// If the executor is nil, a default one is used that always succeeds
// (useful for intrinsic gas calculations only).
func NewGasEstimator(config GasEstimatorConfig) *GasEstimator {
	if config.DefaultGasLimit == 0 {
		config.DefaultGasLimit = 30_000_000
	}
	if config.MaxIterations == 0 {
		config.MaxIterations = 20
	}
	if config.GasCapMultiplier == 0 {
		config.GasCapMultiplier = 1.2
	}
	return &GasEstimator{
		config: config,
		executor: func(msg CallMsg, gas uint64) (bool, uint64, error) {
			// Default executor: assume success and full gas usage.
			return true, gas, nil
		},
	}
}

// SetExecutor sets the execution function used for gas estimation.
// The function takes a CallMsg and gas limit, and returns (success, gasUsed, error).
func (ge *GasEstimator) SetExecutor(exec func(CallMsg, uint64) (bool, uint64, error)) {
	ge.executor = exec
}

// IntrinsicGas calculates the intrinsic gas cost for a transaction.
// This is the minimum gas required before any EVM execution occurs.
func IntrinsicGas(data []byte, isContractCreation bool, isAccessList bool) (uint64, error) {
	var gas uint64

	// Base transaction gas.
	if isContractCreation {
		gas = TxGasContractCreation
	} else {
		gas = TxGas
	}

	// Data gas: 4 per zero byte, 16 per non-zero byte.
	if len(data) > 0 {
		var zeros uint64
		for _, b := range data {
			if b == 0 {
				zeros++
			}
		}
		nonZeros := uint64(len(data)) - zeros

		// Check for overflow on non-zero gas.
		if nonZeros > 0 {
			nzGas := nonZeros * TxDataNonZeroGas
			if nzGas/TxDataNonZeroGas != nonZeros {
				return 0, ErrGasUint64Overflow
			}
			gas += nzGas
			if gas < nzGas {
				return 0, ErrGasUint64Overflow
			}
		}

		// Add zero-byte gas.
		zGas := zeros * TxDataZeroGas
		if zeros > 0 && zGas/TxDataZeroGas != zeros {
			return 0, ErrGasUint64Overflow
		}
		gas += zGas
		if gas < zGas {
			return 0, ErrGasUint64Overflow
		}
	}

	// Access list tx type surcharge.
	if isAccessList {
		gas += AccessListTxGas
		if gas < AccessListTxGas {
			return 0, ErrGasUint64Overflow
		}
	}

	return gas, nil
}

// IntrinsicGasWithAccessList calculates the intrinsic gas including per-entry
// access list costs.
func IntrinsicGasWithAccessList(data []byte, isContractCreation bool, accessList types.AccessList) (uint64, error) {
	gas, err := IntrinsicGas(data, isContractCreation, len(accessList) > 0)
	if err != nil {
		return 0, err
	}

	// Per-entry access list gas.
	for _, tuple := range accessList {
		gas += TxAccessListAddressGas
		if gas < TxAccessListAddressGas {
			return 0, ErrGasUint64Overflow
		}
		keyGas := uint64(len(tuple.StorageKeys)) * TxAccessListStorageKeyGas
		if len(tuple.StorageKeys) > 0 && keyGas/TxAccessListStorageKeyGas != uint64(len(tuple.StorageKeys)) {
			return 0, ErrGasUint64Overflow
		}
		gas += keyGas
		if gas < keyGas {
			return 0, ErrGasUint64Overflow
		}
	}

	return gas, nil
}

// EstimateGas estimates the gas required to execute the given call message.
// It uses binary search between the intrinsic gas floor and the block gas limit.
func (ge *GasEstimator) EstimateGas(msg CallMsg) (uint64, error) {
	isContractCreation := msg.To == nil

	// Calculate intrinsic gas as lower bound.
	intrinsic, err := IntrinsicGas(msg.Data, isContractCreation, false)
	if err != nil {
		return 0, err
	}

	// If caller specified a gas cap, use it; otherwise use block gas limit.
	hi := ge.config.DefaultGasLimit
	if msg.Gas > 0 && msg.Gas < hi {
		hi = msg.Gas
	}
	lo := intrinsic

	if lo > hi {
		return 0, ErrIntrinsicGasTooHigh
	}

	// First check: does execution succeed at the upper bound?
	ok, _, err := ge.executor(msg, hi)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrGasEstimationFailed
	}

	// Binary search for the minimum gas that succeeds.
	for i := 0; i < ge.config.MaxIterations; i++ {
		if lo >= hi {
			break
		}
		mid := lo + (hi-lo)/2

		ok, _, err := ge.executor(msg, mid)
		if err != nil {
			return 0, err
		}
		if ok {
			hi = mid
		} else {
			lo = mid + 1
		}
	}

	// Apply safety margin.
	estimated := float64(hi) * ge.config.GasCapMultiplier
	if estimated > float64(math.MaxUint64) {
		estimated = float64(ge.config.DefaultGasLimit)
	}
	result := uint64(estimated)

	// Cap at block gas limit.
	if result > ge.config.DefaultGasLimit {
		result = ge.config.DefaultGasLimit
	}

	return result, nil
}

// EstimateAccessListGas estimates gas for a transaction that uses an access list.
// The returned value includes the access list overhead.
func (ge *GasEstimator) EstimateAccessListGas(msg CallMsg, accessList []types.Address) uint64 {
	isContractCreation := msg.To == nil

	// Start with intrinsic gas for access list transactions.
	gas, err := IntrinsicGas(msg.Data, isContractCreation, true)
	if err != nil {
		return ge.config.DefaultGasLimit
	}

	// Add per-address cost from the access list.
	gas += uint64(len(accessList)) * TxAccessListAddressGas

	return gas
}
