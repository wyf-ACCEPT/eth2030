package core

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// EIP-1559 constants.
const (
	// InitialBaseFee is the initial base fee for EIP-1559 (1 Gwei).
	InitialBaseFee = 1_000_000_000

	// MinBaseFee is the minimum base fee (7 wei, EIP-4844 era minimum).
	// This prevents the base fee from reaching zero during periods of low
	// network activity, ensuring that a minimum cost is always imposed.
	MinBaseFee = 7
)

// CalcBaseFee calculates the base fee for the next block based on the
// parent's gas usage, following EIP-1559 rules.
//
// Rules:
//   - If parent gas used == target (limit/2): base fee unchanged
//   - If parent gas used > target: increase proportionally (max 12.5%)
//   - If parent gas used < target: decrease proportionally (max 12.5%)
//   - Minimum base fee: 7 wei (EIP-4844 era)
//
// Constants: ElasticityMultiplier=2, BaseFeeChangeDenominator=8
func CalcBaseFee(parent *types.Header) *big.Int {
	if parent.BaseFee == nil {
		return big.NewInt(InitialBaseFee)
	}

	parentGasTarget := parent.GasLimit / ElasticityMultiplier

	// Exactly at target: base fee unchanged.
	if parent.GasUsed == parentGasTarget {
		return new(big.Int).Set(parent.BaseFee)
	}

	if parent.GasUsed > parentGasTarget {
		// Gas used above target: increase base fee.
		gasUsedDelta := parent.GasUsed - parentGasTarget
		baseFeeDelta := new(big.Int).Mul(parent.BaseFee, new(big.Int).SetUint64(gasUsedDelta))
		baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(parentGasTarget))
		baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(BaseFeeChangeDenominator))

		// Ensure minimum increase of 1.
		if baseFeeDelta.Sign() == 0 {
			baseFeeDelta.SetInt64(1)
		}
		return new(big.Int).Add(parent.BaseFee, baseFeeDelta)
	}

	// Gas used below target: decrease base fee.
	gasUsedDelta := parentGasTarget - parent.GasUsed
	baseFeeDelta := new(big.Int).Mul(parent.BaseFee, new(big.Int).SetUint64(gasUsedDelta))
	baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(parentGasTarget))
	baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(BaseFeeChangeDenominator))

	baseFee := new(big.Int).Sub(parent.BaseFee, baseFeeDelta)

	// Enforce minimum base fee of 7 wei (EIP-4844 era).
	minFee := big.NewInt(MinBaseFee)
	if baseFee.Cmp(minFee) < 0 {
		baseFee.Set(minFee)
	}
	return baseFee
}
