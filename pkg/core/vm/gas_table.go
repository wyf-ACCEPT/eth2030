package vm

import (
	"math"
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

	// EIP-3529: SSTORE_CLEARS_SCHEDULE refund = SSTORE_RESET_GAS + ACCESS_LIST_STORAGE_KEY_COST.
	// SSTORE_RESET_GAS = 5000 - COLD_SLOAD_COST = 2900
	// ACCESS_LIST_STORAGE_KEY_COST = 1900
	SstoreClearsScheduleRefund uint64 = 4800

	// SELFDESTRUCT gas.
	SelfdestructGas          uint64 = 5000
	CreateBySelfdestructGas  uint64 = 25000 // sending to a new account
	CreateDataGas            uint64 = 200   // per byte of created contract code
	MaxCodeSize              int    = 24576 // EIP-170: max contract size
	MaxInitCodeSize          int    = 49152 // EIP-3860: max init code size (2 * MaxCodeSize)

	// EIP-3860: initcode word gas.
	InitCodeWordGas uint64 = 2

	// CALL gas constants.
	CallGasFraction      uint64 = 64    // 63/64 rule (EIP-150)
	CallValueTransferGas uint64 = 9000  // paid for non-zero value transfer
	CallNewAccountGas    uint64 = 25000 // paid when calling a non-existent account
)

// MemoryGasCost calculates the gas cost for memory expansion.
// Gas for memory = 3 * numWords + numWords^2 / 512
// Returns math.MaxUint64 on overflow to signal out-of-gas.
func MemoryGasCost(memSize uint64) uint64 {
	if memSize == 0 {
		return 0
	}
	words := toWordSize(memSize)
	// Overflow check: words * words could overflow for large memory sizes.
	// sqrt(MaxUint64) ~ 4.29e9, so if words > ~4.29 billion, words*words overflows.
	if words > 181_000 {
		// At 181_000 words (5.8 MB), gas cost is ~64 billion, well beyond any block
		// gas limit. Return MaxUint64 to signal out-of-gas.
		return math.MaxUint64
	}
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
	// Guard against overflow: if size > MaxUint64-31, size+31 wraps around.
	if size > math.MaxUint64-31 {
		return math.MaxUint64/32 + 1 // ceiling division result
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
//   - Refund logic per EIP-3529 (SstoreClearsScheduleRefund = 4800).
func SstoreGas(original, current, newVal [32]byte, cold bool) (gas uint64, refund int64) {
	if cold {
		gas += ColdSloadCost
	}

	if current == newVal {
		// No-op: current value equals new value.
		gas += WarmStorageReadCost
		return gas, 0
	}

	if original == current {
		if isZero(original) {
			// Create slot: 0 -> non-zero.
			gas += GasSstoreSet
			return gas, 0
		}
		// Update slot: original == current != new.
		gas += GasSstoreReset
		if isZero(newVal) {
			// Delete slot: non-zero -> zero. Refund per EIP-3529.
			refund = int64(SstoreClearsScheduleRefund)
		}
		return gas, refund
	}

	// Dirty slot: original != current (already modified in this transaction).
	gas += WarmStorageReadCost

	// Calculate refund adjustments for dirty slots.
	if !isZero(original) {
		if isZero(current) && !isZero(newVal) {
			// Undo a previous clear: subtract the refund that was previously given.
			refund -= int64(SstoreClearsScheduleRefund)
		} else if !isZero(current) && isZero(newVal) {
			// Clear a dirty non-zero slot: add refund.
			refund += int64(SstoreClearsScheduleRefund)
		}
	}
	if original == newVal {
		// Restoring to original value.
		if isZero(original) {
			// Was 0, set to X, now back to 0: refund the set cost minus the warm read.
			refund += int64(GasSstoreSet) - int64(WarmStorageReadCost)
		} else {
			// Was X, changed to Y, now back to X: refund the reset cost minus the warm read.
			refund += int64(GasSstoreReset) - int64(WarmStorageReadCost)
		}
	}
	return gas, refund
}

// LogGas computes the gas cost for a LOG operation.
// Returns: GasLog + numTopics*GasLogTopic + dataSize*GasLogData.
func LogGas(numTopics uint64, dataSize uint64) uint64 {
	gas := safeAdd(GasLog, safeMul(numTopics, GasLogTopic))
	return safeAdd(gas, safeMul(dataSize, GasLogData))
}

// Sha3Gas computes the gas cost for a SHA3/KECCAK256 operation.
// Returns: GasKeccak256 + ceil(dataSize/32)*GasKeccak256Word.
func Sha3Gas(dataSize uint64) uint64 {
	words := toWordSize(dataSize)
	return safeAdd(GasKeccak256, safeMul(words, GasKeccak256Word))
}

// ExpGas computes the gas cost for the EXP operation.
// Returns: GasSlowStep(10) + 50 * byte_length(exponent).
func ExpGas(exponent *big.Int) uint64 {
	if exponent.Sign() == 0 {
		return GasSlowStep
	}
	byteLen := uint64((exponent.BitLen() + 7) / 8)
	return safeAdd(GasSlowStep, safeMul(50, byteLen))
}

// CopyGas computes the gas cost for a copy operation (CALLDATACOPY, CODECOPY, etc.).
// Returns: GasCopy * ceil(size/32).
func CopyGas(size uint64) uint64 {
	return safeMul(GasCopy, toWordSize(size))
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

// safeAdd returns a+b, capping at math.MaxUint64 on overflow.
func safeAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// safeMul returns a*b, capping at math.MaxUint64 on overflow.
func safeMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}

// --- Dynamic gas functions for opcodes ---

// gasSha3 calculates dynamic gas for SHA3/KECCAK256: 6 per word + memory expansion.
func gasSha3(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	size := stack.Back(1).Uint64()
	words := toWordSize(size)
	gas := safeMul(words, GasKeccak256Word)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasExp calculates dynamic gas for EXP: 50 * byte_length(exponent).
// The constant gas (GasSlowStep = 10) is charged separately.
func gasExp(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	exp := stack.Back(1)
	if exp.Sign() == 0 {
		return 0
	}
	byteLen := uint64((exp.BitLen() + 7) / 8)
	return 50 * byteLen
}

// gasCopy calculates dynamic gas for copy opcodes (CALLDATACOPY, CODECOPY, RETURNDATACOPY).
// Charges GasCopy (3) per word of data copied, plus memory expansion.
// The size is at stack position 2 for these opcodes.
func gasCopy(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	size := stack.Back(2).Uint64()
	words := toWordSize(size)
	gas := safeMul(GasCopy, words)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasExtCodeCopyCopy calculates dynamic gas for EXTCODECOPY (pre-Berlin).
// Charges GasCopy per word + memory expansion. Size is at stack position 3.
func gasExtCodeCopyCopy(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	size := stack.Back(3).Uint64()
	words := toWordSize(size)
	gas := safeMul(GasCopy, words)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// makeGasLog returns a dynamic gas function for LOG0-LOG4.
// Charges GasLogTopic per topic + GasLogData per data byte + memory expansion.
// The constant gas (GasLog = 375) is charged separately.
func makeGasLog(n uint64) dynamicGasFunc {
	return func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
		dataSize := stack.Back(1).Uint64()
		gas := safeMul(n, GasLogTopic)
		gas = safeAdd(gas, safeMul(dataSize, GasLogData))
		gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
		return gas
	}
}

// gasCreateDynamic calculates dynamic gas for CREATE (EIP-3860).
// Charges InitCodeWordGas per word of init code + memory expansion.
func gasCreateDynamic(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	// Stack: value, offset, length
	size := stack.Back(2).Uint64()
	words := toWordSize(size)
	gas := safeMul(InitCodeWordGas, words)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasCreate2Dynamic calculates dynamic gas for CREATE2 (EIP-3860).
// Charges InitCodeWordGas + Keccak256WordGas per word (for hashing) + memory expansion.
func gasCreate2Dynamic(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	// Stack: value, offset, length, salt
	size := stack.Back(2).Uint64()
	words := toWordSize(size)
	// CREATE2 hashes the init code, so pay for keccak words + initcode words.
	gas := safeMul(InitCodeWordGas+GasKeccak256Word, words)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasSstoreEIP2929 charges warm/cold gas for SSTORE.
// The constant gas is 0 for SSTORE when using this dynamic gas function;
// all gas is computed dynamically based on the slot's current/original values.
//
// Per EIP-2929: if the slot is cold, charge ColdSloadCost (2100) and warm it.
// Then proceed with EIP-2200 gas calculation. Unlike SLOAD (where the constant
// gas covers WarmStorageReadCost), SSTORE's constant gas is 0, so the full
// ColdSloadCost is charged here as the cold penalty.
func gasSstoreEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	loc := stack.Back(0)
	slot := bigToHash(loc)

	// Check cold/warm. For SSTORE, the cold penalty is the full ColdSloadCost
	// because SSTORE has constantGas=0 (unlike SLOAD which has constantGas=WarmStorageReadCost).
	var coldGas uint64
	if evm.StateDB != nil {
		_, slotWarm := evm.StateDB.SlotInAccessList(contract.Address, slot)
		if !slotWarm {
			evm.StateDB.AddSlotToAccessList(contract.Address, slot)
			coldGas = ColdSloadCost
		}
	}

	if evm.StateDB == nil {
		return WarmStorageReadCost + coldGas
	}

	key := bigToHash(loc)
	current := evm.StateDB.GetState(contract.Address, key)
	original := evm.StateDB.GetCommittedState(contract.Address, key)
	val := bigToHash(stack.Back(1))

	var currentBytes, originalBytes, newBytes [32]byte
	copy(currentBytes[:], current[:])
	copy(originalBytes[:], original[:])
	copy(newBytes[:], val[:])

	gas, _ := SstoreGas(originalBytes, currentBytes, newBytes, false)
	return gas + coldGas
}

// gasSelfdestructEIP2929 charges gas for SELFDESTRUCT with EIP-2929 cold access.
// Post-London (EIP-3529): no refund is given for SELFDESTRUCT.
func gasSelfdestructEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	var gas uint64
	addr := types.BytesToAddress(stack.Back(0).Bytes())

	// Cold access cost for the beneficiary address.
	gas = safeAdd(gas, gasEIP2929AccountCheck(evm, addr))

	// If beneficiary doesn't exist and contract has balance, charge new account gas.
	if evm.StateDB != nil {
		if !evm.StateDB.Exist(addr) && evm.StateDB.GetBalance(contract.Address).Sign() != 0 {
			gas = safeAdd(gas, CreateBySelfdestructGas)
		}
	}

	return gas
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

// gasExtCodeCopyEIP2929 charges warm/cold gas for EXTCODECOPY, plus copy gas + memory expansion.
func gasExtCodeCopyEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	// Copy gas: 3 per word. Size is at stack position 3.
	size := stack.Back(3).Uint64()
	gas = safeAdd(gas, safeMul(GasCopy, toWordSize(size)))
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasExtCodeHashEIP2929 charges warm/cold gas for EXTCODEHASH.
func gasExtCodeHashEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasEIP2929AccountCheck(evm, addr)
}

// gasCallEIP2929 charges warm/cold gas for CALL, plus value transfer, new account, and memory expansion.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	// Value transfer gas.
	transfersValue := stack.Back(2).Sign() != 0
	if transfersValue {
		gas = safeAdd(gas, CallValueTransferGas)
		// Sending value to a non-existent account costs extra.
		if evm.StateDB != nil && !evm.StateDB.Exist(addr) {
			gas = safeAdd(gas, CallNewAccountGas)
		}
	}
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasCallCodeEIP2929 charges warm/cold gas for CALLCODE, plus value transfer and memory expansion.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallCodeEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	// Value transfer gas (CALLCODE doesn't create new accounts).
	if stack.Back(2).Sign() != 0 {
		gas = safeAdd(gas, CallValueTransferGas)
	}
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasDelegateCallEIP2929 charges warm/cold gas for DELEGATECALL, plus memory expansion.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength
func gasDelegateCallEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasStaticCallEIP2929 charges warm/cold gas for STATICCALL, plus memory expansion.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength
func gasStaticCallEIP2929(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP2929AccountCheck(evm, addr)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}
