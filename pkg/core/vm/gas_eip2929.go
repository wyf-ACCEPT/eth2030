// gas_eip2929.go implements the EIP-2929 gas cost model for state access.
// EIP-2929 (Berlin fork) introduced warm/cold access patterns where the first
// access to an address or storage slot in a transaction costs significantly
// more than subsequent accesses.
//
// Cold account access: 2600 gas
// Warm account access: 100 gas
// Cold storage slot access: 2100 gas
// Warm storage slot access: 100 gas
//
// This file also implements EIP-2930 access list pre-warming, where a
// transaction can declare addresses and storage slots it will access,
// paying a flat fee upfront to avoid cold access surcharges.
package vm

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-2929 gas accounting errors.
var (
	ErrEIP2929NoState      = errors.New("eip2929: no state database for access tracking")
	ErrEIP2929GasExhausted = errors.New("eip2929: insufficient gas for state access")
	ErrEIP2929InvalidAddr  = errors.New("eip2929: invalid address")
)

// EIP2929GasParams holds all gas cost parameters for the EIP-2929 model.
type EIP2929GasParams struct {
	ColdAccountAccess uint64 // full cold account access cost: 2600
	WarmAccountAccess uint64 // warm account access cost: 100
	ColdSlotAccess    uint64 // full cold storage slot cost: 2100
	WarmSlotAccess    uint64 // warm storage slot cost: 100
}

// DefaultEIP2929GasParams returns the standard post-Berlin gas parameters.
func DefaultEIP2929GasParams() EIP2929GasParams {
	return EIP2929GasParams{
		ColdAccountAccess: ColdAccountAccessCost, // 2600
		WarmAccountAccess: WarmStorageReadCost,   // 100
		ColdSlotAccess:    ColdSloadCost,         // 2100
		WarmSlotAccess:    WarmStorageReadCost,   // 100
	}
}

// GlamsterdamEIP2929Params returns the Glamsterdam gas parameters (EIP-8038).
func GlamsterdamEIP2929Params() EIP2929GasParams {
	return EIP2929GasParams{
		ColdAccountAccess: ColdAccountAccessGlamst, // 3500
		WarmAccountAccess: WarmStorageReadGlamst,   // 150
		ColdSlotAccess:    ColdSloadGlamst,         // 2800
		WarmSlotAccess:    WarmStorageReadGlamst,   // 150
	}
}

// EIP2929GasCalculator computes gas costs for all opcodes affected by EIP-2929
// warm/cold access patterns. It tracks which addresses and slots have been
// accessed during the transaction.
type EIP2929GasCalculator struct {
	params  EIP2929GasParams
	tracker *AccessListTracker
}

// NewEIP2929GasCalculator creates a calculator with default parameters.
func NewEIP2929GasCalculator() *EIP2929GasCalculator {
	return &EIP2929GasCalculator{
		params:  DefaultEIP2929GasParams(),
		tracker: NewAccessListTracker(),
	}
}

// NewEIP2929GasCalculatorWithParams creates a calculator with custom params.
func NewEIP2929GasCalculatorWithParams(params EIP2929GasParams) *EIP2929GasCalculator {
	return &EIP2929GasCalculator{
		params:  params,
		tracker: NewAccessListTracker(),
	}
}

// NewEIP2929GasCalculatorWithTracker creates a calculator with an existing tracker.
func NewEIP2929GasCalculatorWithTracker(
	params EIP2929GasParams,
	tracker *AccessListTracker,
) *EIP2929GasCalculator {
	return &EIP2929GasCalculator{
		params:  params,
		tracker: tracker,
	}
}

// Tracker returns the underlying AccessListTracker.
func (c *EIP2929GasCalculator) Tracker() *AccessListTracker {
	return c.tracker
}

// Params returns the gas parameters in use.
func (c *EIP2929GasCalculator) Params() EIP2929GasParams {
	return c.params
}

// AccountAccessGas returns the gas cost for accessing an account.
// If the account is cold, it is warmed and the cold cost is returned.
// If warm, the warm cost is returned.
func (c *EIP2929GasCalculator) AccountAccessGas(addr types.Address) uint64 {
	if c.tracker.TouchAddress(addr) {
		return c.params.WarmAccountAccess
	}
	return c.params.ColdAccountAccess
}

// AccountAccessDynamicGas returns the dynamic gas surcharge for cold account
// access. The constant gas for the opcode covers the warm cost; this function
// returns the extra cost for cold access (cold - warm), or 0 if warm.
func (c *EIP2929GasCalculator) AccountAccessDynamicGas(addr types.Address) uint64 {
	if c.tracker.TouchAddress(addr) {
		return 0 // already warm, no surcharge
	}
	return c.params.ColdAccountAccess - c.params.WarmAccountAccess
}

// SlotAccessGas returns the gas cost for accessing a storage slot.
// If the slot is cold, it is warmed and the cold cost is returned.
// If warm, the warm cost is returned.
func (c *EIP2929GasCalculator) SlotAccessGas(addr types.Address, slot types.Hash) uint64 {
	_, slotWarm := c.tracker.TouchSlot(addr, slot)
	if slotWarm {
		return c.params.WarmSlotAccess
	}
	return c.params.ColdSlotAccess
}

// SlotAccessDynamicGas returns the dynamic gas surcharge for cold slot access.
// Returns (cold - warm) if cold, 0 if warm.
func (c *EIP2929GasCalculator) SlotAccessDynamicGas(addr types.Address, slot types.Hash) uint64 {
	_, slotWarm := c.tracker.TouchSlot(addr, slot)
	if slotWarm {
		return 0
	}
	return c.params.ColdSlotAccess - c.params.WarmSlotAccess
}

// BalanceGas returns the gas cost for the BALANCE opcode.
// Total = WarmAccountAccess (constant) + cold surcharge (dynamic).
func (c *EIP2929GasCalculator) BalanceGas(addr types.Address) uint64 {
	return c.AccountAccessGas(addr)
}

// ExtCodeSizeGas returns the gas cost for the EXTCODESIZE opcode.
func (c *EIP2929GasCalculator) ExtCodeSizeGas(addr types.Address) uint64 {
	return c.AccountAccessGas(addr)
}

// ExtCodeHashGas returns the gas cost for the EXTCODEHASH opcode.
func (c *EIP2929GasCalculator) ExtCodeHashGas(addr types.Address) uint64 {
	return c.AccountAccessGas(addr)
}

// ExtCodeCopyGas returns the gas cost for EXTCODECOPY, including the account
// access cost, per-word copy cost, and memory expansion cost.
//
// Total = accountAccess + copyGas + memExpansion
// where copyGas = GasCopy * ceil(length / 32)
func (c *EIP2929GasCalculator) ExtCodeCopyGas(
	addr types.Address,
	length uint64,
	currentMemSize, requiredMemSize uint64,
) uint64 {
	gas := c.AccountAccessGas(addr)

	// Copy cost: 3 per word.
	words := toWordSize(length)
	gas = safeAdd(gas, safeMul(GasCopy, words))

	// Memory expansion.
	if requiredMemSize > currentMemSize {
		gas = safeAdd(gas, MemoryExpansionGas(currentMemSize, requiredMemSize))
	}

	return gas
}

// CallGasEIP2929 returns the gas cost for a CALL opcode with EIP-2929.
// This includes the account access cost. Value transfer and new account
// costs are computed separately.
func (c *EIP2929GasCalculator) CallGasEIP2929(addr types.Address) uint64 {
	return c.AccountAccessGas(addr)
}

// StaticCallGasEIP2929 returns the gas cost for STATICCALL with EIP-2929.
func (c *EIP2929GasCalculator) StaticCallGasEIP2929(addr types.Address) uint64 {
	return c.AccountAccessGas(addr)
}

// DelegateCallGasEIP2929 returns the gas cost for DELEGATECALL with EIP-2929.
func (c *EIP2929GasCalculator) DelegateCallGasEIP2929(addr types.Address) uint64 {
	return c.AccountAccessGas(addr)
}

// SelfDestructGasEIP2929 returns the gas cost for SELFDESTRUCT with EIP-2929.
// The beneficiary address is subject to cold/warm accounting. If the beneficiary
// does not exist and the contract has a non-zero balance, an additional
// CreateBySelfdestructGas (25000) is charged.
func (c *EIP2929GasCalculator) SelfDestructGasEIP2929(
	beneficiary types.Address,
	beneficiaryExists bool,
	hasBalance bool,
) uint64 {
	gas := c.AccountAccessGas(beneficiary)
	if !beneficiaryExists && hasBalance {
		gas = safeAdd(gas, CreateBySelfdestructGas)
	}
	return gas
}

// AccessListPreWarmer handles EIP-2930 access list pre-warming at the start
// of a transaction. Each pre-warmed address costs 2400 gas and each
// pre-warmed storage slot costs 1900 gas.
type AccessListPreWarmer struct {
	calc *EIP2929GasCalculator

	// Per EIP-2930 gas costs.
	addressCost uint64 // 2400 per address
	storageCost uint64 // 1900 per storage key
}

// EIP-2930 access list gas costs.
const (
	AccessListAddressCost uint64 = 2400
	AccessListStorageCost uint64 = 1900
)

// NewAccessListPreWarmer creates a pre-warmer tied to the given calculator.
func NewAccessListPreWarmer(calc *EIP2929GasCalculator) *AccessListPreWarmer {
	return &AccessListPreWarmer{
		calc:        calc,
		addressCost: AccessListAddressCost,
		storageCost: AccessListStorageCost,
	}
}

// PreWarm processes an EIP-2930 access list and adds all addresses and slots
// to the warm set. Returns the total gas cost for the access list.
func (w *AccessListPreWarmer) PreWarm(
	sender types.Address,
	to *types.Address,
	accessList types.AccessList,
) uint64 {
	// Pre-populate the tracker (sender, to, precompiles, access list entries).
	w.calc.tracker.PrePopulate(sender, to, accessList)

	// Calculate the gas cost: per EIP-2930, the access list gas is charged
	// upfront as part of the intrinsic gas.
	var gas uint64
	for _, tuple := range accessList {
		gas = safeAdd(gas, w.addressCost)
		gas = safeAdd(gas, safeMul(uint64(len(tuple.StorageKeys)), w.storageCost))
	}
	return gas
}

// AccessListGas computes the gas cost for an EIP-2930 access list without
// actually warming any slots. Useful for intrinsic gas calculation.
func AccessListGas(accessList types.AccessList) uint64 {
	var gas uint64
	for _, tuple := range accessList {
		gas = safeAdd(gas, AccessListAddressCost)
		gas = safeAdd(gas, safeMul(uint64(len(tuple.StorageKeys)), AccessListStorageCost))
	}
	return gas
}

// EIP2929OpGasCosts bundles gas cost functions for all opcodes affected by
// EIP-2929. Each function returns the total gas (constant + dynamic).
type EIP2929OpGasCosts struct {
	calc *EIP2929GasCalculator
}

// NewEIP2929OpGasCosts creates a cost calculator.
func NewEIP2929OpGasCosts(calc *EIP2929GasCalculator) *EIP2929OpGasCosts {
	return &EIP2929OpGasCosts{calc: calc}
}

// SloadGas returns the total gas for SLOAD.
func (g *EIP2929OpGasCosts) SloadGas(addr types.Address, slot types.Hash) uint64 {
	return g.calc.SlotAccessGas(addr, slot)
}

// BalanceGas returns the total gas for BALANCE.
func (g *EIP2929OpGasCosts) BalanceGas(addr types.Address) uint64 {
	return g.calc.BalanceGas(addr)
}

// ExtCodeSizeGas returns the total gas for EXTCODESIZE.
func (g *EIP2929OpGasCosts) ExtCodeSizeGas(addr types.Address) uint64 {
	return g.calc.ExtCodeSizeGas(addr)
}

// ExtCodeHashGas returns the total gas for EXTCODEHASH.
func (g *EIP2929OpGasCosts) ExtCodeHashGas(addr types.Address) uint64 {
	return g.calc.ExtCodeHashGas(addr)
}

// ExtCodeCopyGas returns the total gas for EXTCODECOPY.
func (g *EIP2929OpGasCosts) ExtCodeCopyGas(
	addr types.Address,
	length uint64,
	currentMemSize, requiredMemSize uint64,
) uint64 {
	return g.calc.ExtCodeCopyGas(addr, length, currentMemSize, requiredMemSize)
}

// CallGas returns the account access gas for CALL.
func (g *EIP2929OpGasCosts) CallGas(addr types.Address) uint64 {
	return g.calc.CallGasEIP2929(addr)
}

// StaticCallGas returns the account access gas for STATICCALL.
func (g *EIP2929OpGasCosts) StaticCallGas(addr types.Address) uint64 {
	return g.calc.StaticCallGasEIP2929(addr)
}

// DelegateCallGas returns the account access gas for DELEGATECALL.
func (g *EIP2929OpGasCosts) DelegateCallGas(addr types.Address) uint64 {
	return g.calc.DelegateCallGasEIP2929(addr)
}

// SelfDestructGas returns the gas for SELFDESTRUCT including beneficiary access.
func (g *EIP2929OpGasCosts) SelfDestructGas(
	beneficiary types.Address,
	beneficiaryExists, hasBalance bool,
) uint64 {
	return g.calc.SelfDestructGasEIP2929(beneficiary, beneficiaryExists, hasBalance)
}

// WarmColdReport describes the result of a single address or slot access.
type WarmColdReport struct {
	Address types.Address
	Slot    *types.Hash // nil for address-only access
	WasCold bool
	GasCost uint64
	GasType string // "account" or "slot"
}

// TraceAccountAccess records and returns the warm/cold status of an account access.
func (c *EIP2929GasCalculator) TraceAccountAccess(addr types.Address) WarmColdReport {
	wasCold := !c.tracker.ContainsAddress(addr)
	gas := c.AccountAccessGas(addr)
	return WarmColdReport{
		Address: addr,
		WasCold: wasCold,
		GasCost: gas,
		GasType: "account",
	}
}

// TraceSlotAccess records and returns the warm/cold status of a slot access.
func (c *EIP2929GasCalculator) TraceSlotAccess(addr types.Address, slot types.Hash) WarmColdReport {
	_, wasCold := c.tracker.ContainsSlot(addr, slot)
	gas := c.SlotAccessGas(addr, slot)
	slotCopy := slot
	return WarmColdReport{
		Address: addr,
		Slot:    &slotCopy,
		WasCold: !wasCold,
		GasCost: gas,
		GasType: "slot",
	}
}

// String returns a human-readable description of the warm/cold report.
func (r WarmColdReport) String() string {
	temp := "warm"
	if r.WasCold {
		temp = "cold"
	}
	if r.Slot != nil {
		return fmt.Sprintf("%s access %s slot %s: %d gas",
			temp, r.Address.Hex(), r.Slot.Hex(), r.GasCost)
	}
	return fmt.Sprintf("%s access %s: %d gas", temp, r.Address.Hex(), r.GasCost)
}
