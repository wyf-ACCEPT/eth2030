package core

// hogota_repricing.go implements gas repricing rules for the Hogota fork
// (2026-2027). Hogota follows Glamsterdam and introduces further reductions
// in state access costs, plus a separate blob base fee pricing mechanism.
//
// Key changes in Hogota:
//   - SLOAD cold further reduced to 200 (from Glamsterdam's 800)
//   - SSTORE cold reduced to 2500 (from Glamsterdam's 5000)
//   - SSTORE warm reduced to 100 (warm write optimization)
//   - Separate blob gas market with its own base fee calculation
//   - Payload shrinking optimizations reflected in gas costs

import (
	"math"
	"math/big"
)

// HogotaGasTable holds the repriced gas costs for opcodes in the Hogota fork.
// It further refines Glamsterdam costs based on real-world benchmarking data
// and the goal of increasing L1 throughput.
type HogotaGasTable struct {
	SloadCold   uint64 // SLOAD cold: 800 -> 200
	SloadWarm   uint64 // SLOAD warm: unchanged at 100
	SstoreCold  uint64 // SSTORE cold (set): 5000 -> 2500
	SstoreWarm  uint64 // SSTORE warm write: 2900 -> 100
	CallCold    uint64 // CALL cold: unchanged at 100
	CallWarm    uint64 // CALL warm: unchanged at 100
	BalanceCold uint64 // BALANCE cold: 400 -> 200
	BalanceWarm uint64 // BALANCE warm: unchanged at 100
	Create      uint64 // CREATE: 10000 -> 8000
	ExtCodeSize uint64 // EXTCODESIZE cold: 400 -> 200
	ExtCodeCopy uint64 // EXTCODECOPY cold: 400 -> 200
	ExtCodeHash uint64 // EXTCODEHASH cold: 400 -> 200
	Log         uint64 // LOG base: 375 -> 300
	LogData     uint64 // LOG per data byte: 8 -> 6
}

// StateAccessRepricing holds the specific state access gas costs that are
// modified in the Hogota fork. These are the most impactful changes for
// EVM execution throughput.
type StateAccessRepricing struct {
	SloadCold   uint64 // SLOAD cold access cost
	SstoreCold  uint64 // SSTORE cold write cost
	SstoreWarm  uint64 // SSTORE warm write cost
	BalanceCold uint64 // BALANCE cold access cost
}

// DefaultStateAccessRepricing returns the Hogota state access repricing values.
func DefaultStateAccessRepricing() StateAccessRepricing {
	return StateAccessRepricing{
		SloadCold:   200,
		SstoreCold:  2500,
		SstoreWarm:  100,
		BalanceCold: 200,
	}
}

// DefaultHogotaGasTable returns the gas table with Hogota repricing applied.
func DefaultHogotaGasTable() *HogotaGasTable {
	return &HogotaGasTable{
		SloadCold:   200,
		SloadWarm:   100,
		SstoreCold:  2500,
		SstoreWarm:  100,
		CallCold:    100,
		CallWarm:    100,
		BalanceCold: 200,
		BalanceWarm: 100,
		Create:      8000,
		ExtCodeSize: 200,
		ExtCodeCopy: 200,
		ExtCodeHash: 200,
		Log:         300,
		LogData:     6,
	}
}

// ApplyHogotaRepricing modifies the given gas table in-place with Hogota
// gas costs. Returns the table for chaining.
func ApplyHogotaRepricing(table *HogotaGasTable) *HogotaGasTable {
	hogota := DefaultHogotaGasTable()
	table.SloadCold = hogota.SloadCold
	table.SloadWarm = hogota.SloadWarm
	table.SstoreCold = hogota.SstoreCold
	table.SstoreWarm = hogota.SstoreWarm
	table.CallCold = hogota.CallCold
	table.CallWarm = hogota.CallWarm
	table.BalanceCold = hogota.BalanceCold
	table.BalanceWarm = hogota.BalanceWarm
	table.Create = hogota.Create
	table.ExtCodeSize = hogota.ExtCodeSize
	table.ExtCodeCopy = hogota.ExtCodeCopy
	table.ExtCodeHash = hogota.ExtCodeHash
	table.Log = hogota.Log
	table.LogData = hogota.LogData
	return table
}

// HogotaRepricingDeltas returns the gas cost differences between Glamsterdam
// and Hogota for each modified opcode. Positive values indicate reductions.
func HogotaRepricingDeltas() map[string]int64 {
	return map[string]int64{
		"SLOAD_COLD":    800 - 200,    // 600 reduction
		"SSTORE_COLD":   5000 - 2500,  // 2500 reduction
		"SSTORE_WARM":   1500 - 100,   // 1400 reduction
		"BALANCE_COLD":  400 - 200,    // 200 reduction
		"CREATE":        10000 - 8000, // 2000 reduction
		"EXTCODESIZE":   400 - 200,    // 200 reduction
		"EXTCODECOPY":   400 - 200,    // 200 reduction
		"EXTCODEHASH":   400 - 200,    // 200 reduction
		"LOG_BASE":      375 - 300,    // 75 reduction
		"LOG_DATA_BYTE": 8 - 6,        // 2 reduction per byte
	}
}

// BlobBaseFeePricing implements the separate blob gas market for Hogota.
// In Hogota, blob gas has its own base fee that adjusts independently
// from execution gas, allowing more efficient pricing of blob data.
type BlobBaseFeePricing struct {
	// MinBlobBaseFee is the floor for the blob base fee (in wei).
	MinBlobBaseFee uint64

	// BlobBaseFeeUpdateFraction controls the speed of exponential update.
	// Higher values make the fee more stable.
	BlobBaseFeeUpdateFraction uint64

	// TargetBlobsPerBlock is the target number of blobs per block.
	TargetBlobsPerBlock uint64

	// MaxBlobsPerBlock is the maximum number of blobs per block.
	MaxBlobsPerBlock uint64
}

// DefaultBlobBaseFeePricing returns the default blob fee pricing parameters
// for the Hogota fork.
func DefaultBlobBaseFeePricing() *BlobBaseFeePricing {
	return &BlobBaseFeePricing{
		MinBlobBaseFee:            1 << 25, // 33554432 (EIP-7762)
		BlobBaseFeeUpdateFraction: 5376681, // Fusaka value
		TargetBlobsPerBlock:       6,
		MaxBlobsPerBlock:          9,
	}
}

// ComputeHogotaBlobFee calculates the total blob fee for a given amount of
// excess blob gas and number of blobs. The fee is:
//
//	blob_base_fee = min_fee * e^(excess / fraction)
//	total_fee = blob_base_fee * blob_count * GAS_PER_BLOB
//
// This implements the separate blob gas market where blob fees adjust
// independently from execution gas fees.
func ComputeHogotaBlobFee(excessBlobGas uint64, blobCount uint64) uint64 {
	if blobCount == 0 {
		return 0
	}

	pricing := DefaultBlobBaseFeePricing()

	// Calculate blob base fee via fake exponential.
	blobBaseFee := hogotaFakeExponential(
		pricing.MinBlobBaseFee,
		excessBlobGas,
		pricing.BlobBaseFeeUpdateFraction,
	)

	// Total gas for the given number of blobs.
	totalBlobGas := blobCount * GasPerBlob

	// Fee = base_fee * total_blob_gas.
	// Cap at MaxUint64 to prevent overflow.
	if blobBaseFee > 0 && totalBlobGas > math.MaxUint64/blobBaseFee {
		return math.MaxUint64
	}
	return blobBaseFee * totalBlobGas
}

// ComputeHogotaBlobBaseFee returns just the per-gas blob base fee for the
// given excess blob gas, without multiplying by blob count.
func ComputeHogotaBlobBaseFee(excessBlobGas uint64) uint64 {
	pricing := DefaultBlobBaseFeePricing()
	return hogotaFakeExponential(
		pricing.MinBlobBaseFee,
		excessBlobGas,
		pricing.BlobBaseFeeUpdateFraction,
	)
}

// ComputeHogotaExcessBlobGas calculates the excess blob gas for the next
// block given the parent's values.
func ComputeHogotaExcessBlobGas(parentExcess, parentBlobGasUsed uint64) uint64 {
	pricing := DefaultBlobBaseFeePricing()
	targetBlobGas := pricing.TargetBlobsPerBlock * GasPerBlob

	sum := parentExcess + parentBlobGasUsed
	if sum < targetBlobGas {
		return 0
	}
	return sum - targetBlobGas
}

// hogotaFakeExponential approximates factor * e^(numerator / denominator)
// using integer arithmetic with a Taylor series expansion. This is the same
// algorithm as EIP-4844 but operates on uint64 for efficiency.
//
// For very large exponents (numerator/denominator > ~44), the result will
// exceed MaxUint64 and the function returns MaxUint64.
func hogotaFakeExponential(factor, numerator, denominator uint64) uint64 {
	if denominator == 0 {
		return math.MaxUint64
	}
	// Early overflow check: if the exponent is large enough that factor * e^(n/d)
	// would overflow uint64, return MaxUint64. e^44 > 2^63.
	if factor > 0 && numerator/denominator > 60 {
		return math.MaxUint64
	}

	// Use big.Int internally to avoid overflow during intermediate steps.
	factorBig := new(big.Int).SetUint64(factor)
	numBig := new(big.Int).SetUint64(numerator)
	denomBig := new(big.Int).SetUint64(denominator)

	i := new(big.Int).SetUint64(1)
	output := new(big.Int)
	numeratorAccum := new(big.Int).Mul(factorBig, denomBig)
	tmp := new(big.Int)
	denom := new(big.Int)
	maxU64 := new(big.Int).SetUint64(math.MaxUint64)

	for numeratorAccum.Sign() > 0 {
		output.Add(output, numeratorAccum)
		// Early exit if output already exceeds MaxUint64.
		if output.Cmp(new(big.Int).Mul(maxU64, denomBig)) > 0 {
			return math.MaxUint64
		}
		tmp.Mul(numeratorAccum, numBig)
		denom.Mul(denomBig, i)
		numeratorAccum.Div(tmp, denom)
		i.Add(i, big.NewInt(1))
	}
	output.Div(output, denomBig)

	// Cap at MaxUint64.
	if output.Cmp(maxU64) > 0 {
		return math.MaxUint64
	}
	return output.Uint64()
}

// IsHogotaActive returns true if the Hogota fork is active at the given
// block number.
func IsHogotaActive(blockNum *big.Int, forkBlock *big.Int) bool {
	if forkBlock == nil || blockNum == nil {
		return false
	}
	return blockNum.Cmp(forkBlock) >= 0
}

// HogotaGasReduction returns the total gas reduction from Glamsterdam to
// Hogota for a typical DeFi transaction pattern (approximate).
// Pattern: 5 SLOADs, 2 SSTOREs, 1 CALL, 2 BALANCEs.
func HogotaGasReduction() uint64 {
	// Glamsterdam costs.
	glamst := uint64(5*800 + 2*5000 + 1*100 + 2*400)
	// Hogota costs.
	hogota := uint64(5*200 + 2*2500 + 1*100 + 2*200)
	return glamst - hogota
}
