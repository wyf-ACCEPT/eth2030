// dynamic_gas.go implements configurable dynamic gas calculations for EVM
// opcodes. It provides a DynamicGasCalculator struct that encapsulates pricing
// rules for EIP-150 (63/64th rule), EIP-2200 (SSTORE), EIP-3860 (initcode),
// and various other dynamic gas computations.
package vm

import (
	"errors"
	"math"
)

// Errors returned by dynamic gas calculation functions.
var (
	ErrGasOverflow       = errors.New("dynamic gas: overflow")
	ErrInvalidTopicCount = errors.New("dynamic gas: invalid topic count (0-4)")
	ErrInitCodeTooLarge  = errors.New("dynamic gas: initcode exceeds max size")
)

// GasPricingRules holds configurable gas pricing parameters for a specific
// hard fork. All fields default to Cancun/post-London values.
type GasPricingRules struct {
	// SSTORE costs (EIP-2200 / EIP-3529).
	SstoreSetGas     uint64 // zero -> non-zero (default 20000)
	SstoreResetGas   uint64 // non-zero -> non-zero (default 2900)
	WarmReadGas      uint64 // warm storage read (default 100)
	ColdSloadGas     uint64 // cold SLOAD cost (default 2100)
	SstoreClearsRef  uint64 // refund for clearing a slot (default 4800)

	// EXP costs.
	ExpBaseCost    uint64 // base cost of EXP (default 10)
	ExpByteCost    uint64 // per-byte cost of exponent (default 50)

	// LOG costs.
	LogBaseCost   uint64 // per LOG operation (default 375)
	LogTopicCost  uint64 // per topic (default 375)
	LogDataCost   uint64 // per byte of data (default 8)

	// KECCAK256 (SHA3) costs.
	Keccak256BaseCost uint64 // base cost (default 30)
	Keccak256WordCost uint64 // per 32-byte word (default 6)

	// Copy operation costs.
	CopyCostPerWord uint64 // per 32-byte word (default 3)

	// CREATE/CREATE2 costs.
	CreateBaseCost      uint64 // base CREATE gas (default 32000)
	InitCodeWordCost    uint64 // per word of initcode (default 2)
	Create2HashWordCost uint64 // keccak per word for CREATE2 (default 6)
	MaxInitCodeSize     uint64 // max initcode size, EIP-3860 (default 49152)

	// SELFDESTRUCT costs.
	SelfDestructBaseCost     uint64 // base cost (default 5000)
	SelfDestructNewAcctCost  uint64 // creating beneficiary account (default 25000)
	SelfDestructColdAcctCost uint64 // cold access to beneficiary (default 2600)

	// CALL 63/64th rule.
	CallGasFraction uint64 // denominator for EIP-150 (default 64)
}

// DefaultPricingRules returns post-London/Cancun gas pricing rules.
func DefaultPricingRules() GasPricingRules {
	return GasPricingRules{
		SstoreSetGas:     GasSstoreSet,
		SstoreResetGas:   GasSstoreReset,
		WarmReadGas:      WarmStorageReadCost,
		ColdSloadGas:     ColdSloadCost,
		SstoreClearsRef:  SstoreClearsScheduleRefund,

		ExpBaseCost:    GasHigh,
		ExpByteCost:    50,

		LogBaseCost:   GasLog,
		LogTopicCost:  GasLogTopic,
		LogDataCost:   GasLogData,

		Keccak256BaseCost: GasKeccak256,
		Keccak256WordCost: GasKeccak256Word,

		CopyCostPerWord: GasCopy,

		CreateBaseCost:      GasCreate,
		InitCodeWordCost:    InitCodeWordGas,
		Create2HashWordCost: GasKeccak256Word,
		MaxInitCodeSize:     uint64(MaxInitCodeSize),

		SelfDestructBaseCost:     SelfdestructGas,
		SelfDestructNewAcctCost:  CreateBySelfdestructGas,
		SelfDestructColdAcctCost: ColdAccountAccessCost,

		CallGasFraction: CallGasFraction,
	}
}

// GlamsterdamPricingRules returns Glamsterdam gas pricing rules with increased
// state access costs per EIP-8037 and EIP-8038.
func GlamsterdamPricingRules() GasPricingRules {
	r := DefaultPricingRules()
	r.SstoreSetGas = GasSstoreSetGlamsterdam
	r.WarmReadGas = WarmStorageReadGlamst
	r.ColdSloadGas = ColdSloadGlamst
	r.SstoreClearsRef = SstoreClearsRefundGlam
	r.SelfDestructColdAcctCost = ColdAccountAccessGlamst
	r.Keccak256BaseCost = GasKeccak256Glamsterdan
	return r
}

// DynamicGasCalculator performs dynamic gas calculations using configurable
// pricing rules. It is safe to use concurrently because it holds no mutable
// state; all methods are pure functions of the input and the immutable rules.
type DynamicGasCalculator struct {
	Rules GasPricingRules
}

// NewDynamicGasCalculator creates a calculator with the given pricing rules.
func NewDynamicGasCalculator(rules GasPricingRules) *DynamicGasCalculator {
	return &DynamicGasCalculator{Rules: rules}
}

// NewDefaultGasCalculator creates a calculator with post-London defaults.
func NewDefaultGasCalculator() *DynamicGasCalculator {
	return NewDynamicGasCalculator(DefaultPricingRules())
}

// CalcCallGas computes the gas available for a CALL-family opcode per the
// EIP-150 63/64th rule. The caller retains 1/64th of its remaining gas.
//
// Parameters:
//   - availableGas: remaining gas after deducting base costs
//   - requestedGas: gas explicitly requested by the CALL
//   - codeSize: size of the callee's code (unused for basic 63/64 rule
//     but included for future pricing models)
//
// Returns the gas to forward to the callee.
func (c *DynamicGasCalculator) CalcCallGas(availableGas, requestedGas, codeSize uint64) (uint64, error) {
	if c.Rules.CallGasFraction == 0 {
		return 0, ErrGasOverflow
	}
	maxGas := availableGas - availableGas/c.Rules.CallGasFraction
	if requestedGas > maxGas {
		return maxGas, nil
	}
	return requestedGas, nil
}

// CalcExpGas computes the gas cost for the EXP instruction.
// Cost = ExpBaseCost + ExpByteCost * byteLength(exponent).
// The exponentLen is the number of significant bytes in the exponent.
func (c *DynamicGasCalculator) CalcExpGas(exponentLen uint64) (uint64, error) {
	if exponentLen == 0 {
		return c.Rules.ExpBaseCost, nil
	}
	byteCost := dgSafeMul(c.Rules.ExpByteCost, exponentLen)
	total := dgSafeAdd(c.Rules.ExpBaseCost, byteCost)
	if total == math.MaxUint64 && exponentLen > 0 {
		return 0, ErrGasOverflow
	}
	return total, nil
}

// CalcSStoreGas computes the gas cost and refund for an SSTORE operation per
// EIP-2200 / EIP-3529 (post-London). Returns (gasCost, refund, error).
//
// Parameters:
//   - current: the current value of the storage slot
//   - original: the value at the start of the transaction (committed state)
//   - newVal: the value being written
//   - coldAccess: true if this is a cold slot access
func (c *DynamicGasCalculator) CalcSStoreGas(current, original, newVal [32]byte, coldAccess bool) (uint64, uint64, error) {
	var gas uint64

	// Cold access surcharge.
	if coldAccess {
		gas = dgSafeAdd(gas, c.Rules.ColdSloadGas)
	}

	// No-op: current == new.
	if current == newVal {
		gas = dgSafeAdd(gas, c.Rules.WarmReadGas)
		return gas, 0, nil
	}

	var refund uint64

	if original == current {
		// Clean slot: original matches current.
		if dgIsZero(original) {
			// Create slot: 0 -> non-zero.
			gas = dgSafeAdd(gas, c.Rules.SstoreSetGas)
			return gas, 0, nil
		}
		// Update slot: original == current != new.
		gas = dgSafeAdd(gas, c.Rules.SstoreResetGas)
		if dgIsZero(newVal) {
			// Delete slot: non-zero -> zero. Refund per EIP-3529.
			refund = c.Rules.SstoreClearsRef
		}
		return gas, refund, nil
	}

	// Dirty slot: original != current (already modified in this tx).
	gas = dgSafeAdd(gas, c.Rules.WarmReadGas)

	// Calculate refund adjustments for dirty slots.
	if !dgIsZero(original) {
		if dgIsZero(current) && !dgIsZero(newVal) {
			// Undo a previous clear: subtract the refund that was given.
			// We return 0 here; the caller must handle negative refund tracking.
		} else if !dgIsZero(current) && dgIsZero(newVal) {
			// Clear a dirty non-zero slot: add refund.
			refund = dgSafeAdd(refund, c.Rules.SstoreClearsRef)
		}
	}
	if original == newVal {
		// Restoring to original value.
		if dgIsZero(original) {
			// Was 0, set to X, now back to 0: refund set cost minus warm read.
			if c.Rules.SstoreSetGas > c.Rules.WarmReadGas {
				refund = dgSafeAdd(refund, c.Rules.SstoreSetGas-c.Rules.WarmReadGas)
			}
		} else {
			// Was X, changed to Y, now back to X: refund reset cost minus warm read.
			if c.Rules.SstoreResetGas > c.Rules.WarmReadGas {
				refund = dgSafeAdd(refund, c.Rules.SstoreResetGas-c.Rules.WarmReadGas)
			}
		}
	}

	return gas, refund, nil
}

// CalcLogGas computes the gas cost for LOG0 through LOG4.
// Cost = LogBaseCost + topicCount*LogTopicCost + dataSize*LogDataCost.
func (c *DynamicGasCalculator) CalcLogGas(topicCount int, dataSize uint64) (uint64, error) {
	if topicCount < 0 || topicCount > 4 {
		return 0, ErrInvalidTopicCount
	}

	gas := c.Rules.LogBaseCost
	gas = dgSafeAdd(gas, dgSafeMul(uint64(topicCount), c.Rules.LogTopicCost))
	gas = dgSafeAdd(gas, dgSafeMul(dataSize, c.Rules.LogDataCost))

	if gas == math.MaxUint64 && dataSize > 0 {
		return 0, ErrGasOverflow
	}
	return gas, nil
}

// CalcKeccak256Gas computes the gas cost for the SHA3/KECCAK256 instruction.
// Cost = Keccak256BaseCost + ceil(dataSize/32)*Keccak256WordCost.
func (c *DynamicGasCalculator) CalcKeccak256Gas(dataSize uint64) (uint64, error) {
	words := dgToWordSize(dataSize)
	wordCost := dgSafeMul(words, c.Rules.Keccak256WordCost)
	total := dgSafeAdd(c.Rules.Keccak256BaseCost, wordCost)
	if total == math.MaxUint64 && dataSize > 0 {
		return 0, ErrGasOverflow
	}
	return total, nil
}

// CalcCopyGas computes the gas cost for copy operations (CALLDATACOPY,
// CODECOPY, RETURNDATACOPY, EXTCODECOPY).
// Cost = CopyCostPerWord * ceil(dataSize/32).
func (c *DynamicGasCalculator) CalcCopyGas(dataSize uint64) (uint64, error) {
	words := dgToWordSize(dataSize)
	total := dgSafeMul(c.Rules.CopyCostPerWord, words)
	if total == math.MaxUint64 && dataSize > 0 {
		return 0, ErrGasOverflow
	}
	return total, nil
}

// CalcCreateGas computes the gas cost for CREATE or CREATE2.
// Base cost = CreateBaseCost.
// InitCode cost = InitCodeWordCost * ceil(initCodeSize/32) per EIP-3860.
// For CREATE2, also adds Create2HashWordCost * ceil(initCodeSize/32) for
// keccak256 hashing of the init code.
//
// Returns an error if initCodeSize exceeds MaxInitCodeSize (EIP-3860).
func (c *DynamicGasCalculator) CalcCreateGas(initCodeSize uint64, isCreate2 bool) (uint64, error) {
	if c.Rules.MaxInitCodeSize > 0 && initCodeSize > c.Rules.MaxInitCodeSize {
		return 0, ErrInitCodeTooLarge
	}

	gas := c.Rules.CreateBaseCost
	words := dgToWordSize(initCodeSize)

	// EIP-3860: charge per word of initcode.
	initCost := dgSafeMul(c.Rules.InitCodeWordCost, words)
	gas = dgSafeAdd(gas, initCost)

	if isCreate2 {
		// CREATE2 must hash the initcode for the address derivation.
		hashCost := dgSafeMul(c.Rules.Create2HashWordCost, words)
		gas = dgSafeAdd(gas, hashCost)
	}

	if gas == math.MaxUint64 && initCodeSize > 0 {
		return 0, ErrGasOverflow
	}
	return gas, nil
}

// CalcSelfDestructGas computes the gas cost for SELFDESTRUCT.
//
// Parameters:
//   - targetExists: true if the beneficiary account already exists
//   - hasValue: true if the self-destructing contract has a non-zero balance
//   - coldAccess: true if the beneficiary address is cold
//
// Per EIP-2929/EIP-3529 (post-London):
//   - Base cost: SelfDestructBaseCost (5000) charged as constant gas
//   - Cold access: + SelfDestructColdAcctCost (2600)
//   - New account creation: + SelfDestructNewAcctCost (25000) when sending
//     value to a non-existent account
func (c *DynamicGasCalculator) CalcSelfDestructGas(targetExists, hasValue bool, coldAccess bool) (uint64, error) {
	gas := c.Rules.SelfDestructBaseCost

	if coldAccess {
		gas = dgSafeAdd(gas, c.Rules.SelfDestructColdAcctCost)
	}

	if !targetExists && hasValue {
		gas = dgSafeAdd(gas, c.Rules.SelfDestructNewAcctCost)
	}

	return gas, nil
}

// --- Helper functions ---

// dgIsZero returns true if all bytes are zero.
func dgIsZero(val [32]byte) bool {
	for _, b := range val {
		if b != 0 {
			return false
		}
	}
	return true
}

// dgSafeAdd returns a+b, capping at math.MaxUint64 on overflow.
func dgSafeAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// dgSafeMul returns a*b, capping at math.MaxUint64 on overflow.
func dgSafeMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}

// dgToWordSize rounds up to the next 32-byte word boundary.
func dgToWordSize(size uint64) uint64 {
	if size == 0 {
		return 0
	}
	if size > math.MaxUint64-31 {
		return math.MaxUint64/32 + 1
	}
	return (size + 31) / 32
}
