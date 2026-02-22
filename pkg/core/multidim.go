package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-7706/7999: Multidimensional gas pricing.
//
// This module provides the GasDimensions abstraction that unifies compute,
// calldata, and blob gas into a single 3-dimensional gas vector. It builds
// on the existing EIP-7706 calldata gas primitives in calldata_gas.go and
// adds the higher-level tracking and validation needed for multidimensional
// block processing.

// Multidimensional gas errors.
var (
	ErrCalldataGasPoolExhausted = errors.New("calldata gas pool exhausted")
	ErrInvalidCalldataBaseFee   = errors.New("invalid calldata base fee")
	ErrInvalidCalldataGasLimit  = errors.New("invalid calldata gas limit")
)

// EIP-7706/7999 constants re-exported for clarity in the multidimensional context.
const (
	// CalldataGasPerTokenMultidim matches CalldataGasPerToken (4).
	CalldataGasPerTokenMultidim = types.CalldataGasPerToken

	// TokensPerNonZeroByteMultidim matches CalldataTokensPerNonZeroByte (4).
	TokensPerNonZeroByteMultidim = types.CalldataTokensPerNonZeroByte

	// CalldataGasLimitRatioMultidim matches CalldataGasLimitRatio (4).
	CalldataGasLimitRatioMultidim = types.CalldataGasLimitRatio

	// LimitTargetRatioCompute is the ratio of compute gas limit to target (EIP-1559).
	LimitTargetRatioCompute uint64 = 2

	// LimitTargetRatioBlob is the ratio of blob gas limit to target (EIP-4844).
	LimitTargetRatioBlob uint64 = 2

	// LimitTargetRatioCalldata is the ratio of calldata gas limit to target (EIP-7706).
	LimitTargetRatioCalldata uint64 = 4
)

// GasDimensions tracks gas usage across three dimensions: compute, calldata,
// and blob gas. This is the core data structure for multidimensional gas
// accounting per EIP-7706/7999.
type GasDimensions struct {
	Compute  uint64
	Calldata uint64
	Blob     uint64
}

// Add returns a new GasDimensions that is the element-wise sum of g and other.
func (g GasDimensions) Add(other GasDimensions) GasDimensions {
	return GasDimensions{
		Compute:  g.Compute + other.Compute,
		Calldata: g.Calldata + other.Calldata,
		Blob:     g.Blob + other.Blob,
	}
}

// Sub returns a new GasDimensions that is the element-wise subtraction (clamped at zero).
func (g GasDimensions) Sub(other GasDimensions) GasDimensions {
	var result GasDimensions
	if g.Compute >= other.Compute {
		result.Compute = g.Compute - other.Compute
	}
	if g.Calldata >= other.Calldata {
		result.Calldata = g.Calldata - other.Calldata
	}
	if g.Blob >= other.Blob {
		result.Blob = g.Blob - other.Blob
	}
	return result
}

// FitsWithin returns true if each dimension of g is less than or equal to the
// corresponding dimension in limits.
func (g GasDimensions) FitsWithin(limits GasDimensions) bool {
	return g.Compute <= limits.Compute &&
		g.Calldata <= limits.Calldata &&
		g.Blob <= limits.Blob
}

// IsZero returns true if all dimensions are zero.
func (g GasDimensions) IsZero() bool {
	return g.Compute == 0 && g.Calldata == 0 && g.Blob == 0
}

// TotalCost computes the total wei cost across all dimensions given per-gas prices.
// cost = compute*computePrice + calldata*calldataPrice + blob*blobPrice
func (g GasDimensions) TotalCost(computePrice, calldataPrice, blobPrice *big.Int) *big.Int {
	total := new(big.Int)
	if g.Compute > 0 && computePrice != nil {
		total.Add(total, new(big.Int).Mul(computePrice, new(big.Int).SetUint64(g.Compute)))
	}
	if g.Calldata > 0 && calldataPrice != nil {
		total.Add(total, new(big.Int).Mul(calldataPrice, new(big.Int).SetUint64(g.Calldata)))
	}
	if g.Blob > 0 && blobPrice != nil {
		total.Add(total, new(big.Int).Mul(blobPrice, new(big.Int).SetUint64(g.Blob)))
	}
	return total
}

// BlockGasLimits computes the per-dimension gas limits for a block.
// Per EIP-7706:
//   - Compute: header.GasLimit
//   - Calldata: header.GasLimit / CalldataGasLimitRatio
//   - Blob: MaxBlobGasPerBlock (from EIP-4844)
func BlockGasLimits(header *types.Header) GasDimensions {
	return GasDimensions{
		Compute:  header.GasLimit,
		Calldata: CalcCalldataGasLimit(header.GasLimit),
		Blob:     types.MaxBlobGasPerBlock,
	}
}

// BlockGasTargets computes the per-dimension gas targets for a block.
func BlockGasTargets(header *types.Header) GasDimensions {
	limits := BlockGasLimits(header)
	return GasDimensions{
		Compute:  limits.Compute / LimitTargetRatioCompute,
		Calldata: limits.Calldata / LimitTargetRatioCalldata,
		Blob:     limits.Blob / LimitTargetRatioBlob,
	}
}

// TxGasDimensions computes the 3-dimensional gas usage for a transaction.
// Per EIP-7706: [execution_gas, blob_gas, calldata_gas]
func TxGasDimensions(tx *types.Transaction) GasDimensions {
	return GasDimensions{
		Compute:  tx.Gas(),
		Calldata: tx.CalldataGas(),
		Blob:     tx.BlobGas(),
	}
}

// BlockBaseFees computes the per-dimension base fees from a parent header.
// Uses the fake_exponential for all three dimensions.
func BlockBaseFees(parent *types.Header) (compute, calldata, blob *big.Int) {
	compute = CalcBaseFee(parent)
	calldata = CalcCalldataBaseFeeFromHeader(parent)
	if parent.ExcessBlobGas != nil {
		blob = calcBlobBaseFee(*parent.ExcessBlobGas)
	} else {
		blob = big.NewInt(1)
	}
	return
}

// CalcCalldataGasUsed computes the calldata gas consumed by raw data bytes.
// This wraps CalldataTokenGas from the types package.
// tokens = zero_bytes + nonzero_bytes * TOKENS_PER_NONZERO_BYTE
// gas = tokens * CALLDATA_GAS_PER_TOKEN
func CalcCalldataGasUsed(data []byte) uint64 {
	return types.CalldataTokenGas(data)
}

// ValidateCalldataGasFields validates the calldata gas header fields against
// the parent header. This is an alias for ValidateCalldataGas in block_validator.go
// that provides a clearer name for the multidimensional context.
func ValidateCalldataGasFields(header, parent *types.Header) error {
	return ValidateCalldataGas(header, parent)
}

// CalldataGasPool tracks available calldata gas during block execution.
// It mirrors the GasPool type but operates on the calldata dimension.
type CalldataGasPool struct {
	gas uint64
}

// NewCalldataGasPool creates a new calldata gas pool with the given capacity.
func NewCalldataGasPool(gas uint64) *CalldataGasPool {
	return &CalldataGasPool{gas: gas}
}

// AddGas adds gas to the calldata gas pool.
func (p *CalldataGasPool) AddGas(amount uint64) *CalldataGasPool {
	p.gas += amount
	return p
}

// SubGas subtracts gas from the calldata pool, returning an error if insufficient.
func (p *CalldataGasPool) SubGas(amount uint64) error {
	if p.gas < amount {
		return fmt.Errorf("%w: have %d, want %d", ErrCalldataGasPoolExhausted, p.gas, amount)
	}
	p.gas -= amount
	return nil
}

// Gas returns the remaining calldata gas in the pool.
func (p *CalldataGasPool) Gas() uint64 {
	return p.gas
}

// MultidimGasPool tracks gas across all three dimensions during block execution.
type MultidimGasPool struct {
	Compute  *GasPool
	Calldata *CalldataGasPool
}

// NewMultidimGasPool creates a gas pool from block gas limits.
func NewMultidimGasPool(limits GasDimensions) *MultidimGasPool {
	computePool := new(GasPool).AddGas(limits.Compute)
	return &MultidimGasPool{
		Compute:  computePool,
		Calldata: NewCalldataGasPool(limits.Calldata),
	}
}

// SubGas subtracts gas across all dimensions, returning an error if any pool is exhausted.
func (m *MultidimGasPool) SubGas(dims GasDimensions) error {
	if err := m.Compute.SubGas(dims.Compute); err != nil {
		return fmt.Errorf("compute gas: %w", err)
	}
	if err := m.Calldata.SubGas(dims.Calldata); err != nil {
		// Roll back compute gas deduction.
		m.Compute.AddGas(dims.Compute)
		return fmt.Errorf("calldata gas: %w", err)
	}
	return nil
}

// AddGas returns gas across all dimensions (for refunds).
func (m *MultidimGasPool) AddGas(dims GasDimensions) {
	m.Compute.AddGas(dims.Compute)
	m.Calldata.AddGas(dims.Calldata)
}

// Remaining returns the remaining gas in each dimension.
func (m *MultidimGasPool) Remaining() GasDimensions {
	return GasDimensions{
		Compute:  m.Compute.Gas(),
		Calldata: m.Calldata.Gas(),
	}
}

// CalcExcessGasDimensions computes the excess gas vector for the next block from
// the parent's excess and used gas across all dimensions.
func CalcExcessGasDimensions(parent *types.Header) GasDimensions {
	targets := BlockGasTargets(parent)

	var computeExcess uint64
	if parent.GasUsed+0 > targets.Compute {
		// For compute gas, the excess is not tracked in the header the same way.
		// The EIP-1559 basefee handles compute gas adjustments. This field is
		// primarily for future use when compute transitions to the exponential model.
		computeExcess = 0
	}

	var calldataExcess uint64
	if parent.CalldataExcessGas != nil && parent.CalldataGasUsed != nil {
		calldataExcess = CalcCalldataExcessGas(*parent.CalldataExcessGas, *parent.CalldataGasUsed, parent.GasLimit)
	}

	var blobExcess uint64
	if parent.ExcessBlobGas != nil && parent.BlobGasUsed != nil {
		blobExcess = types.CalcExcessBlobGas(*parent.ExcessBlobGas, *parent.BlobGasUsed)
	}

	return GasDimensions{
		Compute:  computeExcess,
		Calldata: calldataExcess,
		Blob:     blobExcess,
	}
}
