package core

import "math/big"

// EIP-7691: Blob Throughput Increase
//
// This file provides a fork-aware blob schedule abstraction that maps
// fork names to their blob parameters (target, max, update fraction).
// It complements the existing blob_gas.go by offering named schedule
// entries for Dencun and Prague/Electra, plus calculation helpers that
// accept a schedule parameter.

// BlobScheduleEntry holds the blob parameters for a specific fork.
type BlobScheduleEntry struct {
	Target                uint64 // target blobs per block
	Max                   uint64 // maximum blobs per block
	BaseFeeUpdateFraction uint64 // blob base fee update fraction
}

// Named blob schedules per fork.
var (
	// DencunBlobSchedule: EIP-4844 original parameters (Cancun/Deneb).
	DencunBlobSchedule = BlobScheduleEntry{
		Target:                3,
		Max:                   6,
		BaseFeeUpdateFraction: 3338477,
	}

	// PragueElectraBlobSchedule: EIP-7691 increased blob throughput (Prague/Electra).
	// Target increased from 3 to 6, max from 6 to 9.
	// Update fraction from EIP-7691: 5007716.
	PragueElectraBlobSchedule = BlobScheduleEntry{
		Target:                6,
		Max:                   9,
		BaseFeeUpdateFraction: 5007716,
	}
)

// GetBlobScheduleEntry returns the active BlobScheduleEntry for the given
// config and timestamp. This mirrors GetBlobSchedule from blob_gas.go but
// returns the EIP-7691-style entry type.
func GetBlobScheduleEntry(config *ChainConfig, time uint64) BlobScheduleEntry {
	if config.IsPrague(time) {
		return PragueElectraBlobSchedule
	}
	return DencunBlobSchedule
}

// CalcBlobBaseFeeWithSchedule computes the blob base fee from excess blob gas
// using the given schedule's update fraction. Uses the EIP-4844 fake exponential.
func CalcBlobBaseFeeWithSchedule(parentExcessGas uint64, schedule BlobScheduleEntry) *big.Int {
	return fakeExponentialV2(
		big.NewInt(1), // MIN_BASE_FEE_PER_BLOB_GAS from EIP-4844
		new(big.Int).SetUint64(parentExcessGas),
		new(big.Int).SetUint64(schedule.BaseFeeUpdateFraction),
	)
}

// CalcExcessBlobGasWithSchedule computes excess blob gas for the next block
// using the given schedule's target. This is the simple pre-7918 formula from
// EIP-4844 / EIP-7691.
func CalcExcessBlobGasWithSchedule(parentExcessGas, parentBlobsUsed uint64, schedule BlobScheduleEntry) uint64 {
	parentBlobGasUsed := parentBlobsUsed * GasPerBlob
	targetBlobGas := schedule.Target * GasPerBlob

	if parentExcessGas+parentBlobGasUsed < targetBlobGas {
		return 0
	}
	return parentExcessGas + parentBlobGasUsed - targetBlobGas
}
