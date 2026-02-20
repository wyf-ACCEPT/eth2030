package vm

// gas_table_ext.go defines extended per-fork gas cost tables. GasTableExt
// encapsulates the gas parameters for major EIPs:
//   - EIP-2929: cold/warm storage access costs (Berlin+)
//   - EIP-2200: SSTORE gas metering with dirty-slot tracking
//   - EIP-3860: initcode size limits for CREATE/CREATE2
//   - EIP-8037/8038: Glamsterdam state access repricing
//
// Each fork's gas table is immutable once created, enabling safe concurrent
// use by multiple EVM instances.

// GasTableExt holds per-fork gas cost parameters for state access operations.
// It extends the base gas constants with fork-specific overrides.
type GasTableExt struct {
	// Fork identification.
	ForkName string

	// EIP-2929: cold/warm access costs (Berlin+).
	WarmStorageRead   uint64 // cost of reading a warm storage slot
	ColdSload         uint64 // cost of SLOAD on a cold slot
	ColdAccountAccess uint64 // cost of accessing a cold account

	// EIP-2200: SSTORE gas metering.
	SstoreSet          uint64 // cost: zero -> non-zero
	SstoreReset        uint64 // cost: non-zero -> non-zero (different)
	SstoreClearsRefund uint64 // refund for clearing a slot (non-zero -> zero)
	SstoreNoopCost     uint64 // cost when current == new (no-op)

	// EIP-3860: CREATE/CREATE2 initcode limits.
	CreateBaseCost    uint64 // base gas for CREATE
	InitCodeWordCost  uint64 // per-word gas for initcode
	MaxInitCodeBytes  uint64 // maximum allowed initcode size

	// Code deposit cost (per byte of deployed contract code).
	CodeDepositPerByte uint64

	// EIP-150: 63/64 call gas fraction.
	CallGasFrac uint64

	// CALL value transfer and new account costs.
	CallValueTransfer uint64
	CallNewAccount    uint64
}

// GasTableBerlin returns the gas table for Berlin and later forks up to
// (but not including) Glamsterdam. This covers Berlin, London, Merge,
// Shanghai, Cancun, and Prague.
func GasTableBerlin() *GasTableExt {
	return &GasTableExt{
		ForkName:           "Berlin",
		WarmStorageRead:    WarmStorageReadCost,   // 100
		ColdSload:          ColdSloadCost,         // 2100
		ColdAccountAccess:  ColdAccountAccessCost, // 2600
		SstoreSet:          GasSstoreSet,          // 20000
		SstoreReset:        GasSstoreReset,        // 2900
		SstoreClearsRefund: SstoreClearsScheduleRefund, // 4800
		SstoreNoopCost:     WarmStorageReadCost,   // 100
		CreateBaseCost:     GasCreate,             // 32000
		InitCodeWordCost:   InitCodeWordGas,       // 2
		MaxInitCodeBytes:   uint64(MaxInitCodeSize), // 49152
		CodeDepositPerByte: CreateDataGas,         // 200
		CallGasFrac:        CallGasFraction,       // 64
		CallValueTransfer:  CallValueTransferGas,  // 9000
		CallNewAccount:     CallNewAccountGas,     // 25000
	}
}

// GasTableGlamsterdam returns the gas table for the Glamsterdam fork with
// EIP-8037/8038 repricing for state access costs.
func GasTableGlamsterdam() *GasTableExt {
	return &GasTableExt{
		ForkName:           "Glamsterdam",
		WarmStorageRead:    WarmStorageReadGlamst,         // 150
		ColdSload:          ColdSloadGlamst,               // 2800
		ColdAccountAccess:  ColdAccountAccessGlamst,       // 3500
		SstoreSet:          GasSstoreSetGlamsterdam,       // 24084
		SstoreReset:        GasSstoreReset,                // 2900 (unchanged)
		SstoreClearsRefund: SstoreClearsRefundGlam,        // 6400
		SstoreNoopCost:     WarmStorageReadGlamst,         // 150
		CreateBaseCost:     GasCreateGlamsterdam,          // 83144
		InitCodeWordCost:   InitCodeWordGas,               // 2 (unchanged)
		MaxInitCodeBytes:   uint64(MaxInitCodeSize),       // 49152
		CodeDepositPerByte: GasCodeDepositGlamsterdam,     // 662
		CallGasFrac:        CallGasFraction,               // 64 (unchanged)
		CallValueTransfer:  CallValueTransferGlamst,       // 2000
		CallNewAccount:     CallNewAccountGlamst,          // 26000
	}
}

// SelectGasTableExt returns the gas table for the given fork rules.
func SelectGasTableExt(rules ForkRules) *GasTableExt {
	if rules.IsGlamsterdan {
		return GasTableGlamsterdam()
	}
	return GasTableBerlin()
}

// SloadGas returns the total gas cost for an SLOAD operation, including
// the cold access surcharge if the slot is cold.
func (gt *GasTableExt) SloadGas(isCold bool) uint64 {
	if isCold {
		return gt.ColdSload
	}
	return gt.WarmStorageRead
}

// SstoreGasEIP2200 computes the gas cost for an SSTORE operation per
// EIP-2200 / EIP-3529 (post-London) using this table's parameters.
// Returns (gasCost, refund).
func (gt *GasTableExt) SstoreGasEIP2200(original, current, newVal [32]byte, isCold bool) (uint64, int64) {
	var gas uint64
	if isCold {
		gas += gt.ColdSload
	}

	// No-op: current == new.
	if current == newVal {
		gas += gt.SstoreNoopCost
		return gas, 0
	}

	var refund int64

	if original == current {
		if gtIsZero(original) {
			// Create slot: 0 -> non-zero.
			gas += gt.SstoreSet
			return gas, 0
		}
		// Update slot: original == current != new.
		gas += gt.SstoreReset
		if gtIsZero(newVal) {
			// Delete slot: non-zero -> zero.
			refund = int64(gt.SstoreClearsRefund)
		}
		return gas, refund
	}

	// Dirty slot: original != current.
	gas += gt.SstoreNoopCost

	if !gtIsZero(original) {
		if gtIsZero(current) && !gtIsZero(newVal) {
			// Undo a previous clear.
			refund -= int64(gt.SstoreClearsRefund)
		} else if !gtIsZero(current) && gtIsZero(newVal) {
			// Clear a dirty non-zero slot.
			refund += int64(gt.SstoreClearsRefund)
		}
	}
	if original == newVal {
		// Restoring to original value.
		if gtIsZero(original) {
			if gt.SstoreSet > gt.SstoreNoopCost {
				refund += int64(gt.SstoreSet - gt.SstoreNoopCost)
			}
		} else {
			if gt.SstoreReset > gt.SstoreNoopCost {
				refund += int64(gt.SstoreReset - gt.SstoreNoopCost)
			}
		}
	}

	return gas, refund
}

// CreateGasEIP3860 computes the gas for CREATE/CREATE2 including
// EIP-3860 initcode word charging. Returns the gas cost and whether the
// initcode exceeds the maximum size.
func (gt *GasTableExt) CreateGasEIP3860(initCodeSize uint64, isCreate2 bool) (uint64, bool) {
	if initCodeSize > gt.MaxInitCodeBytes {
		return 0, false
	}
	gas := gt.CreateBaseCost
	words := gtToWordSize(initCodeSize)
	gas = gtSafeAdd(gas, gtSafeMul(gt.InitCodeWordCost, words))
	if isCreate2 {
		// CREATE2 also hashes the init code for address derivation.
		gas = gtSafeAdd(gas, gtSafeMul(GasKeccak256Word, words))
	}
	return gas, true
}

// AccountAccessGas returns the gas cost for an account access (BALANCE,
// EXTCODESIZE, EXTCODEHASH, etc.) based on cold/warm status.
func (gt *GasTableExt) AccountAccessGas(isCold bool) uint64 {
	if isCold {
		return gt.ColdAccountAccess
	}
	return gt.WarmStorageRead
}

// --- helper functions with unique prefixes to avoid redeclaration ---

// gtIsZero returns true if all bytes in val are zero.
func gtIsZero(val [32]byte) bool {
	for _, b := range val {
		if b != 0 {
			return false
		}
	}
	return true
}

// gtSafeAdd returns a+b, capping at max uint64 on overflow.
func gtSafeAdd(a, b uint64) uint64 {
	sum := a + b
	if sum < a {
		return ^uint64(0)
	}
	return sum
}

// gtSafeMul returns a*b, capping at max uint64 on overflow.
func gtSafeMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > (^uint64(0))/b {
		return ^uint64(0)
	}
	return a * b
}

// gtToWordSize rounds up to the next 32-byte word boundary.
func gtToWordSize(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	if size > (^uint64(0))-31 {
		return (^uint64(0))/32 + 1
	}
	return (size + 31) / 32
}
