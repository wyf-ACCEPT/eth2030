package vm

import (
	"math/big"

	"github.com/eth2028/eth2028/core/types"
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

// --- EIP-2929 dynamic gas functions ---

// gasSloadEIP2929 charges warm/cold gas for SLOAD.
// The constant gas for the opcode is WarmStorageReadCost (100).
// If the slot is cold, this function adds the extra (ColdSloadCost - WarmStorageReadCost).
func gasSloadEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	loc := stack.Back(0)
	slot := bigToHash(loc)
	return gasEIP2929SlotCheck(evm, contract.Address, slot)
}

// gasBalanceEIP2929 charges warm/cold gas for BALANCE.
// The constant gas is WarmStorageReadCost (100).
// If the address is cold, this adds (ColdAccountAccessCost - WarmStorageReadCost).
func gasBalanceEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasEIP2929AccountCheck(evm, addr)
}

// gasExtCodeSizeEIP2929 charges warm/cold gas for EXTCODESIZE.
func gasExtCodeSizeEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasEIP2929AccountCheck(evm, addr)
}

// gasExtCodeCopyEIP2929 charges warm/cold gas for EXTCODECOPY, plus memory expansion.
func gasExtCodeCopyEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas += gasMemExpansion(evm, contract, stack, mem, memorySize)
	return gas
}

// gasExtCodeHashEIP2929 charges warm/cold gas for EXTCODEHASH.
func gasExtCodeHashEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasEIP2929AccountCheck(evm, addr)
}

// gasCallEIP2929 charges warm/cold gas for CALL, plus memory expansion.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas += gasMemExpansion(evm, contract, stack, mem, memorySize)
	return gas
}

// gasCallCodeEIP2929 charges warm/cold gas for CALLCODE, plus memory expansion.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallCodeEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas += gasMemExpansion(evm, contract, stack, mem, memorySize)
	return gas
}

// gasDelegateCallEIP2929 charges warm/cold gas for DELEGATECALL, plus memory expansion.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength
func gasDelegateCallEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas += gasMemExpansion(evm, contract, stack, mem, memorySize)
	return gas
}

// gasStaticCallEIP2929 charges warm/cold gas for STATICCALL, plus memory expansion.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength
func gasStaticCallEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas += gasMemExpansion(evm, contract, stack, mem, memorySize)
	return gas
}
