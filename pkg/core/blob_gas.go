package core

import "math/big"

// Enhanced blob gas constants from EIP-7762, EIP-7918, and EIP-7691
// for the Fusaka (Fulu+Osaka) upgrade. These exist alongside the
// original EIP-4844 constants in blob_validation.go.
const (
	// MinBaseFeePerBlobGas is the minimum base fee per blob gas (EIP-7762).
	// Increased from 1 to 2^25 to speed up price discovery on blob space.
	MinBaseFeePerBlobGas = 1 << 25 // 33554432

	// BlobBaseCost is the execution gas cost constant used for the reserve
	// price calculation in EIP-7918. The reserve blob base fee is
	// BLOB_BASE_COST * base_fee_per_gas / GAS_PER_BLOB.
	BlobBaseCost = 1 << 13 // 8192

	// FusakaMaxBlobsPerBlock is the maximum number of blobs per block (EIP-7691).
	// Increased from 6 to 9.
	FusakaMaxBlobsPerBlock = 9

	// FusakaTargetBlobsPerBlock is the target number of blobs per block (EIP-7691).
	// Increased from 3 to 6.
	FusakaTargetBlobsPerBlock = 6

	// FusakaMaxBlobGasPerBlock is the maximum blob gas per block with updated
	// blob counts: FusakaMaxBlobsPerBlock * GasPerBlob = 9 * 131072 = 1179648.
	FusakaMaxBlobGasPerBlock = FusakaMaxBlobsPerBlock * GasPerBlob

	// FusakaTargetBlobGasPerBlock is the target blob gas per block:
	// FusakaTargetBlobsPerBlock * GasPerBlob = 6 * 131072 = 786432.
	FusakaTargetBlobGasPerBlock = FusakaTargetBlobsPerBlock * GasPerBlob

	// FusakaBlobBaseFeeUpdateFraction is the update fraction for the increased
	// blob target. Scaled from the EIP-4844 value to account for the new
	// target: 5376681 (chosen to maintain similar price elasticity).
	FusakaBlobBaseFeeUpdateFraction = 5376681
)

// CalcBlobBaseFeeV2 calculates the blob base fee from excess blob gas,
// incorporating the EIP-7762 minimum floor and the EIP-7918 execution
// cost bound.
//
// Parameters:
//   - excessBlobGas: the parent's accumulated excess blob gas
//   - baseFeePerGas: the current block's execution base fee (for EIP-7918)
//
// Returns the effective blob base fee as max(computed_fee, reserve_price).
func CalcBlobBaseFeeV2(excessBlobGas uint64, baseFeePerGas *big.Int) *big.Int {
	// Compute base fee from excess via fake exponential.
	computedFee := fakeExponentialV2(
		big.NewInt(MinBaseFeePerBlobGas),
		new(big.Int).SetUint64(excessBlobGas),
		big.NewInt(FusakaBlobBaseFeeUpdateFraction),
	)

	// Apply EIP-7918 reserve price floor: BLOB_BASE_COST * base_fee_per_gas / GAS_PER_BLOB.
	if baseFeePerGas != nil && baseFeePerGas.Sign() > 0 {
		reservePrice := new(big.Int).Mul(big.NewInt(BlobBaseCost), baseFeePerGas)
		reservePrice.Div(reservePrice, big.NewInt(GasPerBlob))

		if reservePrice.Cmp(computedFee) > 0 {
			return reservePrice
		}
	}

	return computedFee
}

// CalcExcessBlobGasV2 calculates the excess blob gas for the next block,
// implementing the updated logic from EIP-7918.
//
// When the blob base fee is below the reserve price (BLOB_BASE_COST * base_fee
// > GAS_PER_BLOB * blob_base_fee), excess increases without subtracting
// target_blob_gas.
func CalcExcessBlobGasV2(parentExcessBlobGas, parentBlobGasUsed uint64, parentBaseFeePerGas *big.Int) uint64 {
	targetBlobGas := uint64(FusakaTargetBlobGasPerBlock)

	if parentExcessBlobGas+parentBlobGasUsed < targetBlobGas {
		return 0
	}

	// Check if we're in the execution-fee-led pricing regime (EIP-7918).
	if parentBaseFeePerGas != nil && parentBaseFeePerGas.Sign() > 0 {
		blobBaseFee := fakeExponentialV2(
			big.NewInt(MinBaseFeePerBlobGas),
			new(big.Int).SetUint64(parentExcessBlobGas),
			big.NewInt(FusakaBlobBaseFeeUpdateFraction),
		)

		// BLOB_BASE_COST * base_fee_per_gas > GAS_PER_BLOB * blob_base_fee
		lhs := new(big.Int).Mul(big.NewInt(BlobBaseCost), parentBaseFeePerGas)
		rhs := new(big.Int).Mul(big.NewInt(GasPerBlob), blobBaseFee)

		if lhs.Cmp(rhs) > 0 {
			// Execution-fee-led: increase without subtracting target.
			// excess = parent_excess + blob_gas_used * (max - target) / max
			increase := parentBlobGasUsed * (FusakaMaxBlobsPerBlock - FusakaTargetBlobsPerBlock) / FusakaMaxBlobsPerBlock
			return parentExcessBlobGas + increase
		}
	}

	// Normal case: subtract target.
	return parentExcessBlobGas + parentBlobGasUsed - targetBlobGas
}

// fakeExponentialV2 approximates factor * e^(numerator / denominator)
// using a Taylor expansion, same algorithm as EIP-4844.
func fakeExponentialV2(factor, numerator, denominator *big.Int) *big.Int {
	i := new(big.Int).SetUint64(1)
	output := new(big.Int)
	numeratorAccum := new(big.Int).Mul(factor, denominator)
	tmp := new(big.Int)
	denom := new(big.Int)
	for numeratorAccum.Sign() > 0 {
		output.Add(output, numeratorAccum)
		tmp.Mul(numeratorAccum, numerator)
		denom.Mul(denominator, i)
		numeratorAccum.Div(tmp, denom)
		i.Add(i, big.NewInt(1))
	}
	output.Div(output, denominator)
	return output
}
