package vm

import (
	"math/big"
)

// Gas cost constants for EIP-2929 (cold/warm access), EIP-3529 (reduced refunds),
// and EIP-1559 gas metering.
const (
	ColdAccountAccessCost uint64 = 2600
	ColdSloadCost         uint64 = 2100
	WarmStorageReadCost   uint64 = 100
	CallStipend           uint64 = 2300 // free gas for CALL with value
	MaxCallDepth          int    = 1024

	// Memory expansion costs.
	MemoryGasCostPerWord uint64 = 3

	// EIP-3529: max gas refund is gasUsed/5 (was gasUsed/2 before London).
	MaxRefundQuotient uint64 = 5

	// SELFDESTRUCT gas.
	SelfdestructGas    uint64 = 5000
	CreateDataGas      uint64 = 200 // per byte of created contract code
	MaxCodeSize        int    = 24576 // EIP-170: max contract size
	MaxInitCodeSize    int    = 49152 // EIP-3860: max init code size (2 * MaxCodeSize)

	// EIP-3860: initcode word gas.
	InitCodeWordGas uint64 = 2

	// CALL gas: 63/64 rule (EIP-150).
	CallGasFraction uint64 = 64
)

// MemoryGasCost calculates the gas cost for memory expansion.
// Gas for memory = 3 * numWords + numWords^2 / 512
func MemoryGasCost(memSize uint64) uint64 {
	if memSize == 0 {
		return 0
	}
	words := toWordSize(memSize)
	linear := words * MemoryGasCostPerWord
	quadratic := words * words / 512
	return linear + quadratic
}

// MemoryExpansionGas returns the gas cost for expanding memory from oldSize to newSize.
func MemoryExpansionGas(oldSize, newSize uint64) uint64 {
	if newSize <= oldSize {
		return 0
	}
	return MemoryGasCost(newSize) - MemoryGasCost(oldSize)
}

// toWordSize rounds up to the next 32-byte word.
func toWordSize(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	return (size + 31) / 32
}

// CallGas computes the gas available for a CALL-family opcode per the 63/64 rule (EIP-150).
// The caller gets to keep 1/64 of its remaining gas.
func CallGas(availableGas, requestedGas uint64) uint64 {
	maxGas := availableGas - availableGas/CallGasFraction
	if requestedGas > maxGas {
		return maxGas
	}
	return requestedGas
}

// SstoreGas computes the gas cost and refund for an SSTORE operation.
// Per EIP-2200 / EIP-3529 (post-London):
//   - If current == new: WarmStorageReadCost (100 gas, no-op)
//   - If current != new:
//     - If original == current: SstoreSet (20000) or SstoreReset (2900)
//     - If original != current: WarmStorageReadCost (100)
//   - Refund logic handled separately.
func SstoreGas(original, current, newVal [32]byte, cold bool) (gas uint64, refund int64) {
	if cold {
		gas += ColdSloadCost
	}

	if current == newVal {
		// No-op.
		gas += WarmStorageReadCost
		return gas, 0
	}

	if original == current {
		if isZero(original) {
			// 0 -> non-zero.
			gas += GasSstoreSet
			return gas, 0
		}
		// non-zero -> non-zero (different value).
		gas += GasSstoreReset
		if isZero(newVal) {
			// non-zero -> zero: refund.
			refund = int64(GasSstoreReset) + int64(ColdSloadCost)
		}
		return gas, refund
	}

	// original != current (already dirty slot).
	gas += WarmStorageReadCost

	// Calculate refund adjustments.
	if !isZero(original) {
		if isZero(current) && !isZero(newVal) {
			// Undid a previous clear.
			refund -= int64(GasSstoreReset) + int64(ColdSloadCost)
		} else if !isZero(current) && isZero(newVal) {
			// Clearing a dirty slot.
			refund += int64(GasSstoreReset) + int64(ColdSloadCost)
		}
	}
	if original == newVal {
		// Restoring to original value.
		if isZero(original) {
			refund += int64(GasSstoreSet) - int64(WarmStorageReadCost)
		} else {
			refund += int64(GasSstoreReset) - int64(WarmStorageReadCost)
		}
	}
	return gas, refund
}

// LogGas computes the gas cost for a LOG operation.
func LogGas(numTopics uint64, dataSize uint64) uint64 {
	return GasLog + numTopics*GasLogTopic + dataSize*GasLogData
}

// Sha3Gas computes the gas cost for a SHA3/KECCAK256 operation.
func Sha3Gas(dataSize uint64) uint64 {
	words := toWordSize(dataSize)
	return GasKeccak256 + words*GasKeccak256Word
}

// ExpGas computes the gas cost for the EXP operation.
// 10 gas + 50 gas per byte of the exponent.
func ExpGas(exponent *big.Int) uint64 {
	if exponent.Sign() == 0 {
		return GasSlowStep
	}
	byteLen := uint64((exponent.BitLen() + 7) / 8)
	return GasSlowStep + 50*byteLen
}

// CopyGas computes the gas cost for a copy operation (CALLDATACOPY, CODECOPY, etc.).
func CopyGas(size uint64) uint64 {
	return GasCopy * toWordSize(size)
}

// isZero returns true if all bytes are zero.
func isZero(val [32]byte) bool {
	for _, b := range val {
		if b != 0 {
			return false
		}
	}
	return true
}
