package core

// glamsterdam_repricing.go implements gas repricing rules for the Glamsterdam
// fork (2026). This covers opcode gas cost adjustments, EIP-7623 calldata
// floor pricing, and intrinsic gas calculations specific to Glamsterdam.
//
// The Glamsterdam fork introduces several gas changes:
//   - SLOAD cold access reduced from 2100 to 800 (storage read optimization)
//   - SSTORE set reduced from 20000 to 5000 (storage write optimization)
//   - CALL cold access reduced from 2600 to 100 (call optimization)
//   - BALANCE cold access reduced from 2600 to 400 (balance check optimization)
//   - CREATE reduced from 32000 to 10000 (contract creation optimization)
//   - EIP-7623 calldata floor pricing with increased token cost

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// GlamsterdamGasTable holds the repriced gas costs for opcodes in the
// Glamsterdam fork. Each field represents the new gas cost for the
// corresponding operation.
type GlamsterdamGasTable struct {
	SloadCold   uint64 // SLOAD cold access: 2100 -> 800
	SloadWarm   uint64 // SLOAD warm access: unchanged at 100
	SstoreSet   uint64 // SSTORE from zero to non-zero: 20000 -> 5000
	SstoreReset uint64 // SSTORE from non-zero to non-zero: 2900 -> 1500
	CallCold    uint64 // CALL cold access: 2600 -> 100
	CallWarm    uint64 // CALL warm access: unchanged at 100
	BalanceCold uint64 // BALANCE cold access: 2600 -> 400
	BalanceWarm uint64 // BALANCE warm access: unchanged at 100
	Create      uint64 // CREATE base cost: 32000 -> 10000
	ExtCodeSize uint64 // EXTCODESIZE cold: 2600 -> 400
	ExtCodeCopy uint64 // EXTCODECOPY cold: 2600 -> 400
	ExtCodeHash uint64 // EXTCODEHASH cold: 2600 -> 400
	Selfdestruct uint64 // SELFDESTRUCT: 5000 -> 5000 (unchanged)
	Log         uint64 // LOG base: 375 -> 375 (unchanged)
	Keccak256   uint64 // KECCAK256 base: 30 -> 45 (increased, EIP-7904)
}

// DefaultGlamsterdamGasTable returns the gas table with Glamsterdam repricing
// applied. These values reflect the consensus-specified gas costs for the
// Glamsterdam fork.
func DefaultGlamsterdamGasTable() *GlamsterdamGasTable {
	return &GlamsterdamGasTable{
		SloadCold:    800,
		SloadWarm:    100,
		SstoreSet:    5000,
		SstoreReset:  1500,
		CallCold:     100,
		CallWarm:     100,
		BalanceCold:  400,
		BalanceWarm:  100,
		Create:       10000,
		ExtCodeSize:  400,
		ExtCodeCopy:  400,
		ExtCodeHash:  400,
		Selfdestruct: 5000,
		Log:          375,
		Keccak256:    45,
	}
}

// GasTableEntry represents a single opcode gas cost entry that can be
// modified by repricing.
type GasTableEntry struct {
	Opcode  byte
	OldCost uint64
	NewCost uint64
}

// ApplyGlamsterdamRepricing modifies the given gas table in-place with
// Glamsterdam gas costs. It applies all the repricing rules and returns
// the table for chaining.
func ApplyGlamsterdamRepricing(table *GlamsterdamGasTable) *GlamsterdamGasTable {
	glamst := DefaultGlamsterdamGasTable()
	table.SloadCold = glamst.SloadCold
	table.SloadWarm = glamst.SloadWarm
	table.SstoreSet = glamst.SstoreSet
	table.SstoreReset = glamst.SstoreReset
	table.CallCold = glamst.CallCold
	table.CallWarm = glamst.CallWarm
	table.BalanceCold = glamst.BalanceCold
	table.BalanceWarm = glamst.BalanceWarm
	table.Create = glamst.Create
	table.ExtCodeSize = glamst.ExtCodeSize
	table.ExtCodeCopy = glamst.ExtCodeCopy
	table.ExtCodeHash = glamst.ExtCodeHash
	table.Selfdestruct = glamst.Selfdestruct
	table.Log = glamst.Log
	table.Keccak256 = glamst.Keccak256
	return table
}

// GlamsterdamRepricingEntries returns the list of gas cost changes applied
// in the Glamsterdam fork. Useful for logging and auditing.
func GlamsterdamRepricingEntries() []GasTableEntry {
	return []GasTableEntry{
		{Opcode: 0x54, OldCost: 2100, NewCost: 800},   // SLOAD cold
		{Opcode: 0x55, OldCost: 20000, NewCost: 5000},  // SSTORE set
		{Opcode: 0xF1, OldCost: 2600, NewCost: 100},    // CALL cold
		{Opcode: 0x31, OldCost: 2600, NewCost: 400},    // BALANCE cold
		{Opcode: 0xF0, OldCost: 32000, NewCost: 10000}, // CREATE
		{Opcode: 0x3B, OldCost: 2600, NewCost: 400},    // EXTCODESIZE cold
		{Opcode: 0x3C, OldCost: 2600, NewCost: 400},    // EXTCODECOPY cold
		{Opcode: 0x3F, OldCost: 2600, NewCost: 400},    // EXTCODEHASH cold
		{Opcode: 0x20, OldCost: 30, NewCost: 45},       // KECCAK256 (EIP-7904)
	}
}

// EIP-7623 calldata floor pricing constants for Glamsterdam.
const (
	// GlamsterdamFloorTokenCost is the per-token floor cost under EIP-7623
	// as applied in the Glamsterdam fork. This is higher than the standard
	// calldata cost to incentivize blob usage.
	GlamsterdamFloorTokenCost uint64 = 16

	// GlamsterdamTxBase is the reduced base transaction gas for Glamsterdam
	// (EIP-2780 reduces from 21000 to 4500).
	GlamsterdamTxBase uint64 = 4500

	// GlamsterdamCalldataZeroGas is the gas cost per zero calldata byte.
	GlamsterdamCalldataZeroGas uint64 = 4

	// GlamsterdamCalldataNonZeroGas is the gas cost per non-zero calldata byte.
	GlamsterdamCalldataNonZeroGas uint64 = 16
)

// ComputeCalldataGas computes the EIP-7623 calldata gas cost for Glamsterdam.
// The calldata floor ensures a minimum gas charge based on data size:
//
//	tokens = zero_bytes * 1 + nonzero_bytes * 4
//	standard_gas = zero_bytes * 4 + nonzero_bytes * 16
//	floor_gas = GlamsterdamTxBase + tokens * GlamsterdamFloorTokenCost
//	result = max(standard_gas, floor_gas)
//
// This pricing incentivizes the use of blobs over calldata for large data.
func ComputeCalldataGas(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}

	var zeroBytes, nonZeroBytes uint64
	for _, b := range data {
		if b == 0 {
			zeroBytes++
		} else {
			nonZeroBytes++
		}
	}

	// Standard gas: traditional EIP-2028 pricing.
	standardGas := zeroBytes*GlamsterdamCalldataZeroGas + nonZeroBytes*GlamsterdamCalldataNonZeroGas

	// Floor gas: EIP-7623 floor pricing.
	tokens := zeroBytes + nonZeroBytes*4
	floorGas := GlamsterdamTxBase + tokens*GlamsterdamFloorTokenCost

	if floorGas > standardGas {
		return floorGas
	}
	return standardGas
}

// IntrinsicGasGlamsterdam computes the intrinsic gas for a transaction under
// Glamsterdam rules. This uses the reduced base gas (EIP-2780: 4500 instead
// of 21000) and applies the EIP-7623 calldata floor.
func IntrinsicGasGlamsterdam(data []byte, isContractCreation bool, accessList types.AccessList) uint64 {
	var gas uint64

	// Base transaction cost (EIP-2780: reduced from 21000 to 4500).
	gas = GlamsterdamTxBase

	// Contract creation surcharge.
	if isContractCreation {
		gas += TxCreateGas
	}

	// Calldata gas with EIP-7623 floor applied.
	calldataGas := ComputeCalldataGas(data)
	gas += calldataGas

	// Access list gas.
	for _, tuple := range accessList {
		gas += TxAccessListAddressGas
		gas += uint64(len(tuple.StorageKeys)) * TxAccessListStorageKeyGas
	}

	return gas
}

// IsGlamsterdamActive returns true if the Glamsterdam fork is active at the
// given block number. The fork is active when blockNum >= forkBlock.
// If forkBlock is nil, Glamsterdam is not configured and is never active.
func IsGlamsterdamActive(blockNum *big.Int, forkBlock *big.Int) bool {
	if forkBlock == nil || blockNum == nil {
		return false
	}
	return blockNum.Cmp(forkBlock) >= 0
}

// CalldataFloorDelta computes the difference between the EIP-7623 floor gas
// and the standard calldata gas. Returns 0 if the standard gas exceeds
// the floor (no delta needed).
func CalldataFloorDelta(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}

	var zeroBytes, nonZeroBytes uint64
	for _, b := range data {
		if b == 0 {
			zeroBytes++
		} else {
			nonZeroBytes++
		}
	}

	standardGas := zeroBytes*GlamsterdamCalldataZeroGas + nonZeroBytes*GlamsterdamCalldataNonZeroGas
	tokens := zeroBytes + nonZeroBytes*4
	floorGas := GlamsterdamTxBase + tokens*GlamsterdamFloorTokenCost

	if floorGas > standardGas {
		return floorGas - standardGas
	}
	return 0
}

// GlamsterdamSavings computes the gas savings for each repriced opcode
// compared to pre-Glamsterdam costs. Returns a map of opcode -> savings.
func GlamsterdamSavings() map[byte]uint64 {
	entries := GlamsterdamRepricingEntries()
	savings := make(map[byte]uint64, len(entries))
	for _, e := range entries {
		if e.OldCost > e.NewCost {
			savings[e.Opcode] = e.OldCost - e.NewCost
		}
	}
	return savings
}
