package core

// conversion_repricing.go implements gas conversion repricing for the
// post-Hogota era (EL Throughput track). This module provides a systematic
// way to apply proportional gas cost adjustments across all opcodes,
// enabling smooth transitions between gas schedules as L1 throughput
// targets increase.
//
// The conversion approach uses a numerator/denominator factor to scale
// gas costs, avoiding the need for per-opcode specification in each fork.
// This supports the EL throughput roadmap goal of reaching 1 Ggas/sec
// through progressive repricing.

import (
	"math"
)

// ConversionFactor represents a rational scaling factor (numerator/denominator)
// applied to gas costs. A factor of {1, 2} halves all costs; {3, 4} reduces
// them by 25%.
type ConversionFactor struct {
	Numerator   uint64
	Denominator uint64
}

// NewConversionFactor creates a new ConversionFactor. Panics if denominator
// is zero.
func NewConversionFactor(numerator, denominator uint64) ConversionFactor {
	if denominator == 0 {
		panic("core: conversion factor denominator must be > 0")
	}
	return ConversionFactor{
		Numerator:   numerator,
		Denominator: denominator,
	}
}

// Apply scales a gas value by the conversion factor.
// result = gas * numerator / denominator, with a minimum of 1 to prevent
// any opcode from becoming free.
func (f ConversionFactor) Apply(gas uint64) uint64 {
	if gas == 0 {
		return 0
	}
	// Use 128-bit multiplication to avoid overflow.
	result := safeMulDiv(gas, f.Numerator, f.Denominator)
	if result == 0 {
		return 1 // minimum gas cost of 1
	}
	return result
}

// Ratio returns the factor as a float64 for display purposes.
func (f ConversionFactor) Ratio() float64 {
	if f.Denominator == 0 {
		return 0
	}
	return float64(f.Numerator) / float64(f.Denominator)
}

// IsReduction returns true if the factor reduces gas costs (ratio < 1).
func (f ConversionFactor) IsReduction() bool {
	return f.Numerator < f.Denominator
}

// IsIncrease returns true if the factor increases gas costs (ratio > 1).
func (f ConversionFactor) IsIncrease() bool {
	return f.Numerator > f.Denominator
}

// IsIdentity returns true if the factor has no effect (ratio == 1).
func (f ConversionFactor) IsIdentity() bool {
	return f.Numerator == f.Denominator
}

// GasOp represents a single gas operation for batch conversion.
type GasOp struct {
	Opcode byte   // EVM opcode
	Gas    uint64 // original gas cost
}

// ConvertedGasOp represents a gas operation with both original and converted costs.
type ConvertedGasOp struct {
	Opcode       byte
	OriginalGas  uint64
	ConvertedGas uint64
}

// ConversionTable maps opcode gas costs from old values to new values.
// It supports both explicit per-opcode mappings and a default conversion
// factor for unmapped opcodes.
type ConversionTable struct {
	// explicit maps specific opcodes to their new gas costs.
	explicit map[byte]uint64

	// defaultFactor is applied to all opcodes not in the explicit map.
	defaultFactor ConversionFactor

	// minGas is the minimum gas cost for any opcode after conversion.
	minGas uint64
}

// NewConversionTable creates a new conversion table with the given default
// factor and explicit overrides. The minGas parameter sets the floor gas
// cost for any converted operation (typically 1).
func NewConversionTable(defaultFactor ConversionFactor, minGas uint64) *ConversionTable {
	return &ConversionTable{
		explicit:      make(map[byte]uint64),
		defaultFactor: defaultFactor,
		minGas:        minGas,
	}
}

// SetExplicit adds an explicit gas cost override for a specific opcode.
func (ct *ConversionTable) SetExplicit(opcode byte, newGas uint64) {
	ct.explicit[opcode] = newGas
}

// GetExplicit returns the explicit gas cost for an opcode, or 0 and false
// if no explicit override exists.
func (ct *ConversionTable) GetExplicit(opcode byte) (uint64, bool) {
	gas, ok := ct.explicit[opcode]
	return gas, ok
}

// ConvertGas converts a single opcode's gas cost using the conversion table.
// If an explicit mapping exists for the opcode, it is used. Otherwise, the
// default conversion factor is applied.
func ConvertGas(table *ConversionTable, opcode byte, oldGas uint64) uint64 {
	// Check explicit mapping first.
	if newGas, ok := table.explicit[opcode]; ok {
		return newGas
	}

	// Apply default factor.
	result := table.defaultFactor.Apply(oldGas)

	// Enforce minimum.
	if result < table.minGas && oldGas > 0 {
		return table.minGas
	}
	return result
}

// BatchConvert converts a slice of gas operations using the conversion table.
// Returns a slice of converted gas values in the same order.
func BatchConvert(table *ConversionTable, operations []GasOp) []uint64 {
	results := make([]uint64, len(operations))
	for i, op := range operations {
		results[i] = ConvertGas(table, op.Opcode, op.Gas)
	}
	return results
}

// BatchConvertDetailed converts a slice of gas operations and returns
// detailed results including both original and converted costs.
func BatchConvertDetailed(table *ConversionTable, operations []GasOp) []ConvertedGasOp {
	results := make([]ConvertedGasOp, len(operations))
	for i, op := range operations {
		results[i] = ConvertedGasOp{
			Opcode:       op.Opcode,
			OriginalGas:  op.Gas,
			ConvertedGas: ConvertGas(table, op.Opcode, op.Gas),
		}
	}
	return results
}

// ApplyConversion modifies a HogotaGasTable in-place by applying the
// conversion factor to all entries. Explicit overrides in the table take
// precedence. Returns the modified table for chaining.
func ApplyConversion(table *HogotaGasTable, factor ConversionFactor) *HogotaGasTable {
	table.SloadCold = applyWithMin(table.SloadCold, factor)
	table.SloadWarm = applyWithMin(table.SloadWarm, factor)
	table.SstoreCold = applyWithMin(table.SstoreCold, factor)
	table.SstoreWarm = applyWithMin(table.SstoreWarm, factor)
	table.CallCold = applyWithMin(table.CallCold, factor)
	table.CallWarm = applyWithMin(table.CallWarm, factor)
	table.BalanceCold = applyWithMin(table.BalanceCold, factor)
	table.BalanceWarm = applyWithMin(table.BalanceWarm, factor)
	table.Create = applyWithMin(table.Create, factor)
	table.ExtCodeSize = applyWithMin(table.ExtCodeSize, factor)
	table.ExtCodeCopy = applyWithMin(table.ExtCodeCopy, factor)
	table.ExtCodeHash = applyWithMin(table.ExtCodeHash, factor)
	table.Log = applyWithMin(table.Log, factor)
	table.LogData = applyWithMin(table.LogData, factor)
	return table
}

// applyWithMin applies a conversion factor with a minimum of 1.
func applyWithMin(gas uint64, factor ConversionFactor) uint64 {
	result := factor.Apply(gas)
	if result == 0 && gas > 0 {
		return 1
	}
	return result
}

// PreDefinedConversionFactors contains standard conversion factors for
// progressive throughput increases.
var PreDefinedConversionFactors = map[string]ConversionFactor{
	// Half: reduce all gas costs by 50% (2x throughput).
	"half": {Numerator: 1, Denominator: 2},

	// Quarter: reduce all gas costs by 75% (4x throughput).
	"quarter": {Numerator: 1, Denominator: 4},

	// ThreeQuarters: reduce all gas costs by 25% (~1.33x throughput).
	"three_quarters": {Numerator: 3, Denominator: 4},

	// Tenth: reduce all gas costs by 90% (10x throughput).
	"tenth": {Numerator: 1, Denominator: 10},

	// Identity: no change (useful as a default/no-op).
	"identity": {Numerator: 1, Denominator: 1},
}

// TotalGasSavings calculates the total gas saved across a set of operations
// after applying conversion.
func TotalGasSavings(table *ConversionTable, operations []GasOp) uint64 {
	var totalOriginal, totalConverted uint64
	for _, op := range operations {
		totalOriginal += op.Gas
		totalConverted += ConvertGas(table, op.Opcode, op.Gas)
	}
	if totalOriginal <= totalConverted {
		return 0
	}
	return totalOriginal - totalConverted
}

// ConversionSummary provides a summary of a conversion table's effects.
type ConversionSummary struct {
	DefaultRatio  float64
	ExplicitCount int
	MinGas        uint64
	IsReduction   bool
}

// Summarize returns a summary of the conversion table's configuration.
func (ct *ConversionTable) Summarize() ConversionSummary {
	return ConversionSummary{
		DefaultRatio:  ct.defaultFactor.Ratio(),
		ExplicitCount: len(ct.explicit),
		MinGas:        ct.minGas,
		IsReduction:   ct.defaultFactor.IsReduction(),
	}
}

// safeMulDiv computes (a * b) / c without overflow using big-number intermediate.
// Returns MaxUint64 if the result overflows uint64.
func safeMulDiv(a, b, c uint64) uint64 {
	if c == 0 {
		return math.MaxUint64
	}
	if a == 0 || b == 0 {
		return 0
	}

	// Check if a * b fits in uint64.
	if a <= math.MaxUint64/b {
		return (a * b) / c
	}

	// Use 128-bit intermediate: split into high and low 64-bit words.
	// a * b = (aH * 2^32 + aL) * (bH * 2^32 + bL)
	aH, aL := a>>32, a&0xFFFFFFFF
	bH, bL := b>>32, b&0xFFFFFFFF

	// Cross products.
	mid1 := aH * bL
	mid2 := aL * bH
	low := aL * bL
	high := aH * bH

	// Add cross products.
	mid := mid1 + mid2
	if mid < mid1 {
		high += 1 << 32 // carry
	}

	// Combine into high:low 128-bit number.
	high += mid >> 32
	lowCarry := (mid & 0xFFFFFFFF) << 32
	newLow := low + lowCarry
	if newLow < low {
		high++ // carry
	}

	// Divide high:newLow by c.
	if high == 0 {
		return newLow / c
	}

	// Full 128-bit / 64-bit division. For simplicity, use iterative approach.
	// This is rare in practice (only for very large gas * factor values).
	if high >= c {
		return math.MaxUint64 // overflow
	}

	// Knuth's algorithm D (simplified for single-word divisor).
	remainder := high
	result := uint64(0)
	for i := 63; i >= 0; i-- {
		remainder = (remainder << 1) | ((newLow >> uint(i)) & 1)
		if remainder >= c {
			remainder -= c
			result |= 1 << uint(i)
		}
	}
	return result
}
