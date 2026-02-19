package core

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// EIP-7706: Separate gas type for calldata.
//
// This module implements the calldata gas dimension from EIP-7706, which
// creates a separate base fee, gas limit, and gas accounting for calldata.
// The mechanism mirrors the EIP-4844 blob gas approach: an EIP-1559-style
// exponential fee adjustment based on excess gas.

// EIP-7706 constants.
const (
	// CalldataBaseFeeUpdateFraction controls the exponential update speed.
	// Roughly matches EIP-4844 parameters.
	CalldataBaseFeeUpdateFraction = 8

	// CalldataTargetRatio is the ratio of gas limit to target for calldata.
	// A higher ratio (4 vs 2 for execution) reduces scenarios where blocks
	// hit the calldata limit, mitigating economic impact.
	CalldataTargetRatio uint64 = 4

	// MinCalldataBaseFee is the minimum base fee per calldata gas (1 wei).
	MinCalldataBaseFee = 1
)

// CalcCalldataGasLimit derives the calldata gas limit from the execution gas limit.
// Per EIP-7706: calldata_gas_limit = execution_gas_limit / CALLDATA_GAS_LIMIT_RATIO
func CalcCalldataGasLimit(executionGasLimit uint64) uint64 {
	return executionGasLimit / types.CalldataGasLimitRatio
}

// CalcCalldataGasTarget computes the calldata gas target for a block.
// target = calldata_gas_limit / CalldataTargetRatio
func CalcCalldataGasTarget(calldataGasLimit uint64) uint64 {
	return calldataGasLimit / CalldataTargetRatio
}

// CalcCalldataExcessGas calculates the excess calldata gas for the next block,
// following the same pattern as EIP-4844 blob excess gas.
// excess = max(0, parent_excess + parent_used - target)
func CalcCalldataExcessGas(parentExcess, parentUsed, parentGasLimit uint64) uint64 {
	calldataGasLimit := CalcCalldataGasLimit(parentGasLimit)
	target := CalcCalldataGasTarget(calldataGasLimit)
	sum := parentExcess + parentUsed
	if sum < target {
		return 0
	}
	return sum - target
}

// CalcCalldataBaseFee computes the calldata base fee from the excess calldata gas.
// Uses the fake_exponential formula: MIN_BASE_FEE * e^(excess / (target * UPDATE_FRACTION))
func CalcCalldataBaseFee(excessCalldataGas uint64, calldataGasLimit uint64) *big.Int {
	target := CalcCalldataGasTarget(calldataGasLimit)
	if target == 0 {
		return big.NewInt(MinCalldataBaseFee)
	}
	denominator := new(big.Int).SetUint64(target * CalldataBaseFeeUpdateFraction)
	return fakeExponential(
		big.NewInt(MinCalldataBaseFee),
		new(big.Int).SetUint64(excessCalldataGas),
		denominator,
	)
}

// CalcCalldataBaseFeeFromHeader computes the calldata base fee from a parent header.
// Returns MinCalldataBaseFee if the header lacks EIP-7706 fields.
func CalcCalldataBaseFeeFromHeader(parent *types.Header) *big.Int {
	if parent.CalldataExcessGas == nil {
		return big.NewInt(MinCalldataBaseFee)
	}
	calldataGasLimit := CalcCalldataGasLimit(parent.GasLimit)
	return CalcCalldataBaseFee(*parent.CalldataExcessGas, calldataGasLimit)
}

// GetCalldataFees computes calldata gas fees for a transaction.
// Returns (calldataGas, calldataBaseFee) where:
//   - calldataGas: the calldata gas consumed by this transaction
//   - calldataBaseFee: the per-gas base fee for calldata
func GetCalldataFees(tx *types.Transaction, header *types.Header) (calldataGas uint64, calldataBaseFee *big.Int) {
	calldataGas = tx.CalldataGas()
	calldataBaseFee = CalcCalldataBaseFeeFromHeader(header)
	return
}

// CalldataGasCost computes the total wei cost for a transaction's calldata gas.
// cost = calldata_gas * calldata_base_fee
func CalldataGasCost(calldataGas uint64, calldataBaseFee *big.Int) *big.Int {
	return new(big.Int).Mul(
		calldataBaseFee,
		new(big.Int).SetUint64(calldataGas),
	)
}
