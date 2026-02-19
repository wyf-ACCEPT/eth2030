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

// Glamsterdam gas repricing constants.

// EIP-2780: Reduce intrinsic transaction gas.
const (
	TxBaseGlamsterdam         uint64 = 4500  // TX_BASE_COST (was 21000)
	GasNewAccount             uint64 = 25000 // surcharge for value-transfer to non-existent account
	StateUpdate               uint64 = 1000  // one account-leaf write
	ColdAccountCostNoCode     uint64 = 500   // cold touch of account without code
	ColdAccountCostCode       uint64 = 2600  // cold touch of account with code
	CallValueTransferGlamst   uint64 = 2000  // EIP-2780: 2 * STATE_UPDATE (was 9000)
	CallNewAccountGlamst      uint64 = 26000 // EIP-2780: STATE_UPDATE + GAS_NEW_ACCOUNT
)

// EIP-8037: State Creation Gas Increase (simplified for 60M gas limit).
// cost_per_state_byte = ceil((gas_limit * 2_628_000) / (2 * TARGET_STATE_GROWTH_PER_YEAR))
// At 60M: ceil((60_000_000 * 2_628_000) / (2 * 107_374_182_400)) = 734 (raw)
// Quantized with 5 significant bits + offset 9578: cpsb = 662
const (
	CostPerStateByte uint64 = 662 // at 60M gas limit

	// EIP-8037: GAS_CREATE = 112 * cpsb (state) + 9000 (regular)
	GasCreateGlamsterdam uint64 = 112*CostPerStateByte + 9000 // 83,144

	// EIP-8037: GAS_CODE_DEPOSIT = cpsb per byte (state gas)
	GasCodeDepositGlamsterdam uint64 = CostPerStateByte // 662 per byte (was 200)

	// EIP-8037: GAS_STORAGE_SET = 32 * cpsb + 2900
	GasSstoreSetGlamsterdam uint64 = 32*CostPerStateByte + 2900 // 24,084

	// EIP-8037: GAS_NEW_ACCOUNT = 112 * cpsb (state gas component)
	GasNewAccountState uint64 = 112 * CostPerStateByte // 74,144
)

// EIP-8038: State Access Gas Increase.
// The spec has TBD values. We use conservative increases based on the
// rationale that state has grown ~2x since EIP-2929. The EXT* family
// gets an additional WarmStorageReadCost for the second DB read.
const (
	ColdAccountAccessGlamst uint64 = 3500 // was 2600
	ColdSloadGlamst         uint64 = 2800 // was 2100
	WarmStorageReadGlamst   uint64 = 150  // was 100
	SstoreClearsRefundGlam  uint64 = 6400 // was 4800, = SstoreReset + AccessListStorageKeyCostGlam
	AccessListAddressGlamst uint64 = 3200 // was 2400
	AccessListStorageGlamst uint64 = 2500 // was 1900
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

// --- Pre-Berlin dynamic gas functions for CALL-family opcodes ---

// gasCallFrontier calculates dynamic gas for CALL in pre-Berlin forks.
// Charges memory expansion + value transfer gas (9000) when value > 0,
// plus new account gas (25000) when sending value to a non-existent account.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallFrontier(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	var gas uint64
	transfersValue := stack.Back(2).Sign() != 0
	if transfersValue {
		gas = safeAdd(gas, CallValueTransferGas)
		// Sending value to a non-existent account costs extra.
		addr := types.BytesToAddress(stack.Back(1).Bytes())
		if evm.StateDB != nil && !evm.StateDB.Exist(addr) {
			gas = safeAdd(gas, CallNewAccountGas)
		}
	}
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasCallCodeFrontier calculates dynamic gas for CALLCODE in pre-Berlin forks.
// Charges memory expansion + value transfer gas (9000) when value > 0.
// CALLCODE does NOT charge new account gas since it runs in the caller's context.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func gasCallCodeFrontier(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	var gas uint64
	if stack.Back(2).Sign() != 0 {
		gas = safeAdd(gas, CallValueTransferGas)
	}
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasSelfdestructFrontier calculates dynamic gas for SELFDESTRUCT in pre-Berlin forks.
// Charges CreateBySelfdestructGas (25000) when sending balance to a non-existent account.
func gasSelfdestructFrontier(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	if evm.StateDB != nil {
		if !evm.StateDB.Exist(addr) && evm.StateDB.GetBalance(contract.Address).Sign() != 0 {
			return CreateBySelfdestructGas
		}
	}
	return 0
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

// --- Glamsterdam gas functions (EIP-8038, EIP-2780, EIP-7778) ---

// gasEIP8038AccountCheck is the Glamsterdam version of gasEIP2929AccountCheck
// with increased cold/warm costs per EIP-8038.
func gasEIP8038AccountCheck(evm *EVM, addr types.Address) uint64 {
	if evm.StateDB == nil {
		return 0
	}
	if evm.StateDB.AddressInAccessList(addr) {
		return 0
	}
	evm.StateDB.AddAddressToAccessList(addr)
	return ColdAccountAccessGlamst - WarmStorageReadGlamst
}

// gasEIP8038SlotCheck is the Glamsterdam version of gasEIP2929SlotCheck
// with increased cold/warm costs per EIP-8038.
func gasEIP8038SlotCheck(evm *EVM, addr types.Address, slot types.Hash) uint64 {
	if evm.StateDB == nil {
		return 0
	}
	_, slotWarm := evm.StateDB.SlotInAccessList(addr, slot)
	if slotWarm {
		return 0
	}
	evm.StateDB.AddSlotToAccessList(addr, slot)
	return ColdSloadGlamst - WarmStorageReadGlamst
}

// gasSloadGlamst charges warm/cold gas for SLOAD under Glamsterdam (EIP-8038).
func gasSloadGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	loc := stack.Back(0)
	slot := bigToHash(loc)
	return gasEIP8038SlotCheck(evm, contract.Address, slot)
}

// gasBalanceGlamst charges warm/cold gas for BALANCE under Glamsterdam (EIP-8038).
func gasBalanceGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasEIP8038AccountCheck(evm, addr)
}

// gasExtCodeSizeGlamst charges warm/cold gas for EXTCODESIZE under Glamsterdam.
// Per EIP-8038: adds extra WarmStorageReadGlamst for second DB read (code size).
func gasExtCodeSizeGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	gas := gasEIP8038AccountCheck(evm, addr)
	// EIP-8038: second read for code size costs warm access
	gas = safeAdd(gas, WarmStorageReadGlamst)
	return gas
}

// gasExtCodeCopyGlamst charges warm/cold gas for EXTCODECOPY under Glamsterdam.
// Per EIP-8038: adds extra WarmStorageReadGlamst for second DB read (code).
func gasExtCodeCopyGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	gas := gasEIP8038AccountCheck(evm, addr)
	// EIP-8038: second read for code costs warm access
	gas = safeAdd(gas, WarmStorageReadGlamst)
	// Copy gas: 3 per word. Size is at stack position 3.
	size := stack.Back(3).Uint64()
	gas = safeAdd(gas, safeMul(GasCopy, toWordSize(size)))
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasExtCodeHashGlamst charges warm/cold gas for EXTCODEHASH under Glamsterdam.
func gasExtCodeHashGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	return gasEIP8038AccountCheck(evm, addr)
}

// gasSstoreGlamst charges gas for SSTORE under Glamsterdam.
// EIP-7778: no refunds are issued (refund is always 0).
// EIP-8038: increased cold/warm costs.
// EIP-8037: increased storage set cost.
func gasSstoreGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	loc := stack.Back(0)
	slot := bigToHash(loc)

	// Check cold/warm with Glamsterdam costs.
	var coldGas uint64
	if evm.StateDB != nil {
		_, slotWarm := evm.StateDB.SlotInAccessList(contract.Address, slot)
		if !slotWarm {
			evm.StateDB.AddSlotToAccessList(contract.Address, slot)
			coldGas = ColdSloadGlamst
		}
	}

	if evm.StateDB == nil {
		return WarmStorageReadGlamst + coldGas
	}

	key := bigToHash(loc)
	current := evm.StateDB.GetState(contract.Address, key)
	original := evm.StateDB.GetCommittedState(contract.Address, key)
	val := bigToHash(stack.Back(1))

	if current == val {
		// No-op.
		return WarmStorageReadGlamst + coldGas
	}

	var currentBytes, originalBytes [32]byte
	copy(currentBytes[:], current[:])
	copy(originalBytes[:], original[:])

	if originalBytes == currentBytes {
		if isZero(originalBytes) {
			// Create slot: 0 -> non-zero. EIP-8037 increased cost.
			return GasSstoreSetGlamsterdam + coldGas
		}
		// Update slot: non-zero -> different non-zero.
		return GasSstoreReset + coldGas
	}

	// Dirty slot: original != current.
	// EIP-7778: no refunds, so just charge warm read.
	return WarmStorageReadGlamst + coldGas
}

// gasCallGlamst charges gas for CALL under Glamsterdam (EIP-8038/2780).
func gasCallGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP8038AccountCheck(evm, addr)
	transfersValue := stack.Back(2).Sign() != 0
	if transfersValue {
		// EIP-2780: CallValueTransferGas = 2 * STATE_UPDATE = 2000
		gas = safeAdd(gas, CallValueTransferGlamst)
		if evm.StateDB != nil && !evm.StateDB.Exist(addr) {
			// EIP-2780: STATE_UPDATE + GAS_NEW_ACCOUNT = 26000
			gas = safeAdd(gas, CallNewAccountGlamst)
		}
	}
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasCallCodeGlamst charges gas for CALLCODE under Glamsterdam.
func gasCallCodeGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP8038AccountCheck(evm, addr)
	if stack.Back(2).Sign() != 0 {
		gas = safeAdd(gas, CallValueTransferGlamst)
	}
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasDelegateCallGlamst charges gas for DELEGATECALL under Glamsterdam.
func gasDelegateCallGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP8038AccountCheck(evm, addr)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasStaticCallGlamst charges gas for STATICCALL under Glamsterdam.
func gasStaticCallGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	addr := types.BytesToAddress(stack.Back(1).Bytes())
	gas := gasEIP8038AccountCheck(evm, addr)
	gas = safeAdd(gas, gasMemExpansion(evm, contract, stack, mem, memorySize))
	return gas
}

// gasSelfdestructGlamst charges gas for SELFDESTRUCT under Glamsterdam.
func gasSelfdestructGlamst(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) uint64 {
	var gas uint64
	addr := types.BytesToAddress(stack.Back(0).Bytes())
	gas = safeAdd(gas, gasEIP8038AccountCheck(evm, addr))
	if evm.StateDB != nil {
		if !evm.StateDB.Exist(addr) && evm.StateDB.GetBalance(contract.Address).Sign() != 0 {
			gas = safeAdd(gas, CreateBySelfdestructGas)
		}
	}
	return gas
}
