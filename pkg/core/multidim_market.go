// multidim_market.go implements a full 3-dimensional fee market for the
// Ethereum 2028 roadmap. It manages independent EIP-1559-style base fee
// adjustments for three gas dimensions: execution gas, blob gas, and
// access gas (from the multidimensional pricing track).
//
// Each dimension has its own target, limit, elasticity multiplier, and
// base fee change denominator, allowing independent price discovery for
// compute, data availability, and state access.
package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
)

// DimensionType identifies a gas dimension in the multidimensional fee market.
type DimensionType int

const (
	ExecutionGas DimensionType = iota
	BlobGas
	AccessGas
)

// String returns the human-readable name of the dimension.
func (d DimensionType) String() string {
	switch d {
	case ExecutionGas:
		return "execution"
	case BlobGas:
		return "blob"
	case AccessGas:
		return "access"
	default:
		return "unknown"
	}
}

// Multidimensional fee market errors.
var (
	ErrFeeMarketInvalidConfig  = errors.New("fee_market: invalid config")
	ErrFeeBelowBaseFee         = errors.New("fee_market: max fee below base fee")
	ErrBlobFeeBelowBaseFee     = errors.New("fee_market: max blob fee below base fee")
	ErrAccessFeeBelowBaseFee   = errors.New("fee_market: max access fee below base fee")
)

// DimensionConfig holds the parameters for a single gas dimension's
// EIP-1559-style fee adjustment mechanism.
type DimensionConfig struct {
	TargetGas                uint64 // target gas per block
	MaxGas                   uint64 // maximum gas per block (limit)
	ElasticityMultiplier     uint64 // limit / target ratio
	BaseFeeChangeDenominator uint64 // controls adjustment speed
	MinBaseFee               int64  // floor for the base fee (wei)
}

// FeeMarketConfig holds configuration for all three dimensions.
type FeeMarketConfig struct {
	Execution DimensionConfig
	Blob      DimensionConfig
	Access    DimensionConfig
}

// DefaultFeeMarketConfig returns a config with production-like defaults:
//   - Execution: 15M target, 30M limit, elasticity 2, denominator 8, min 7 wei
//   - Blob: ~393K target, ~786K limit, elasticity 2, denominator 3, min 1 wei
//   - Access: 3.75M target, 7.5M limit, elasticity 2, denominator 8, min 1 wei
func DefaultFeeMarketConfig() FeeMarketConfig {
	return FeeMarketConfig{
		Execution: DimensionConfig{
			TargetGas:                15_000_000,
			MaxGas:                   30_000_000,
			ElasticityMultiplier:     2,
			BaseFeeChangeDenominator: 8,
			MinBaseFee:               7,
		},
		Blob: DimensionConfig{
			TargetGas:                393_216,
			MaxGas:                   786_432,
			ElasticityMultiplier:     2,
			BaseFeeChangeDenominator: 3,
			MinBaseFee:               1,
		},
		Access: DimensionConfig{
			TargetGas:                3_750_000,
			MaxGas:                   7_500_000,
			ElasticityMultiplier:     2,
			BaseFeeChangeDenominator: 8,
			MinBaseFee:               1,
		},
	}
}

// validateConfig checks that the config dimensions are self-consistent.
func validateConfig(cfg FeeMarketConfig) error {
	for _, dc := range []struct {
		name string
		dim  DimensionConfig
	}{
		{"execution", cfg.Execution},
		{"blob", cfg.Blob},
		{"access", cfg.Access},
	} {
		if dc.dim.TargetGas == 0 {
			return fmt.Errorf("%w: %s target gas must be > 0", ErrFeeMarketInvalidConfig, dc.name)
		}
		if dc.dim.MaxGas == 0 {
			return fmt.Errorf("%w: %s max gas must be > 0", ErrFeeMarketInvalidConfig, dc.name)
		}
		if dc.dim.BaseFeeChangeDenominator == 0 {
			return fmt.Errorf("%w: %s denominator must be > 0", ErrFeeMarketInvalidConfig, dc.name)
		}
	}
	return nil
}

// FeeMarket manages 3-dimensional fee pricing with independent EIP-1559-style
// base fee adjustments for execution gas, blob gas, and access gas. Thread-safe.
type FeeMarket struct {
	mu     sync.RWMutex
	config FeeMarketConfig

	executionBaseFee *big.Int
	blobBaseFee      *big.Int
	accessBaseFee    *big.Int
}

// NewFeeMarket creates a new fee market with the given config. Base fees
// are initialized to the configured minimums.
func NewFeeMarket(config FeeMarketConfig) *FeeMarket {
	if err := validateConfig(config); err != nil {
		// Fall back to defaults on invalid config.
		config = DefaultFeeMarketConfig()
	}
	return &FeeMarket{
		config:           config,
		executionBaseFee: big.NewInt(config.Execution.MinBaseFee),
		blobBaseFee:      big.NewInt(config.Blob.MinBaseFee),
		accessBaseFee:    big.NewInt(config.Access.MinBaseFee),
	}
}

// NewFeeMarketWithBaseFees creates a fee market with explicit initial base fees.
func NewFeeMarketWithBaseFees(config FeeMarketConfig, execFee, blobFee, accessFee *big.Int) *FeeMarket {
	if err := validateConfig(config); err != nil {
		config = DefaultFeeMarketConfig()
	}
	fm := &FeeMarket{config: config}
	fm.executionBaseFee = new(big.Int).Set(execFee)
	fm.blobBaseFee = new(big.Int).Set(blobFee)
	fm.accessBaseFee = new(big.Int).Set(accessFee)
	return fm
}

// BaseFee returns the current base fee for the specified dimension.
func (fm *FeeMarket) BaseFee(dim DimensionType) *big.Int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	switch dim {
	case ExecutionGas:
		return new(big.Int).Set(fm.executionBaseFee)
	case BlobGas:
		return new(big.Int).Set(fm.blobBaseFee)
	case AccessGas:
		return new(big.Int).Set(fm.accessBaseFee)
	default:
		return big.NewInt(0)
	}
}

// UpdateBaseFees adjusts base fees for all dimensions after a block based on
// actual gas usage. Each dimension adjusts independently using the EIP-1559
// algorithm: if usage > target, fee increases; if usage < target, fee decreases.
func (fm *FeeMarket) UpdateBaseFees(gasUsed, blobGasUsed, accessGasUsed uint64) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.executionBaseFee = adjustBaseFee(fm.executionBaseFee, gasUsed, fm.config.Execution)
	fm.blobBaseFee = adjustBaseFee(fm.blobBaseFee, blobGasUsed, fm.config.Blob)
	fm.accessBaseFee = adjustBaseFee(fm.accessBaseFee, accessGasUsed, fm.config.Access)
}

// adjustBaseFee implements the EIP-1559 base fee adjustment for one dimension.
func adjustBaseFee(currentFee *big.Int, gasUsed uint64, cfg DimensionConfig) *big.Int {
	target := cfg.TargetGas

	if gasUsed == target {
		return new(big.Int).Set(currentFee)
	}

	if gasUsed > target {
		// Usage above target: increase base fee.
		delta := gasUsed - target
		baseFeeDelta := new(big.Int).Mul(currentFee, new(big.Int).SetUint64(delta))
		baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(target))
		baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(cfg.BaseFeeChangeDenominator))
		// Ensure minimum increase of 1 when there is any excess.
		if baseFeeDelta.Sign() == 0 {
			baseFeeDelta.SetInt64(1)
		}
		return new(big.Int).Add(currentFee, baseFeeDelta)
	}

	// Usage below target: decrease base fee.
	delta := target - gasUsed
	baseFeeDelta := new(big.Int).Mul(currentFee, new(big.Int).SetUint64(delta))
	baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(target))
	baseFeeDelta.Div(baseFeeDelta, new(big.Int).SetUint64(cfg.BaseFeeChangeDenominator))

	newFee := new(big.Int).Sub(currentFee, baseFeeDelta)
	minFee := big.NewInt(cfg.MinBaseFee)
	if newFee.Cmp(minFee) < 0 {
		newFee.Set(minFee)
	}
	return newFee
}

// MultidimTx represents a transaction with multidimensional gas parameters.
type MultidimTx struct {
	GasLimit         uint64   // execution gas limit
	BlobCount        uint64   // number of blobs attached
	AccessListSize   uint64   // access gas budget requested
	MaxFeePerGas     *big.Int // max fee per execution gas (wei)
	MaxBlobFeePerGas *big.Int // max fee per blob gas (wei)
	MaxAccessFeePerGas *big.Int // max fee per access gas (wei)
}

// BlobGasUsed returns the total blob gas for the transaction.
// Each blob uses BlobGasPerBlob (131072) gas.
func (tx *MultidimTx) BlobGasUsed() uint64 {
	return tx.BlobCount * 131072
}

// EffectiveFee calculates the total effective fee for a transaction given
// current base fees. The effective fee is:
//
//	gasLimit * min(maxFeePerGas, baseFee) +
//	blobGas * min(maxBlobFee, blobBaseFee) +
//	accessListSize * min(maxAccessFee, accessBaseFee)
func (fm *FeeMarket) EffectiveFee(tx *MultidimTx) *big.Int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	total := new(big.Int)

	// Execution dimension.
	if tx.GasLimit > 0 && tx.MaxFeePerGas != nil {
		execPrice := minBigInt(tx.MaxFeePerGas, fm.executionBaseFee)
		total.Add(total, new(big.Int).Mul(execPrice, new(big.Int).SetUint64(tx.GasLimit)))
	}

	// Blob dimension.
	blobGas := tx.BlobGasUsed()
	if blobGas > 0 && tx.MaxBlobFeePerGas != nil {
		blobPrice := minBigInt(tx.MaxBlobFeePerGas, fm.blobBaseFee)
		total.Add(total, new(big.Int).Mul(blobPrice, new(big.Int).SetUint64(blobGas)))
	}

	// Access dimension.
	if tx.AccessListSize > 0 && tx.MaxAccessFeePerGas != nil {
		accessPrice := minBigInt(tx.MaxAccessFeePerGas, fm.accessBaseFee)
		total.Add(total, new(big.Int).Mul(accessPrice, new(big.Int).SetUint64(tx.AccessListSize)))
	}

	return total
}

// ValidateFees checks that the transaction's max fees meet or exceed the
// current base fees in each dimension.
func (fm *FeeMarket) ValidateFees(tx *MultidimTx) error {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	// Execution gas fee check.
	if tx.GasLimit > 0 {
		if tx.MaxFeePerGas == nil || tx.MaxFeePerGas.Cmp(fm.executionBaseFee) < 0 {
			return fmt.Errorf("%w: have %v, need %v", ErrFeeBelowBaseFee,
				tx.MaxFeePerGas, fm.executionBaseFee)
		}
	}

	// Blob gas fee check.
	if tx.BlobCount > 0 {
		if tx.MaxBlobFeePerGas == nil || tx.MaxBlobFeePerGas.Cmp(fm.blobBaseFee) < 0 {
			return fmt.Errorf("%w: have %v, need %v", ErrBlobFeeBelowBaseFee,
				tx.MaxBlobFeePerGas, fm.blobBaseFee)
		}
	}

	// Access gas fee check.
	if tx.AccessListSize > 0 {
		if tx.MaxAccessFeePerGas == nil || tx.MaxAccessFeePerGas.Cmp(fm.accessBaseFee) < 0 {
			return fmt.Errorf("%w: have %v, need %v", ErrAccessFeeBelowBaseFee,
				tx.MaxAccessFeePerGas, fm.accessBaseFee)
		}
	}

	return nil
}

// EstimateTotalCost estimates the total cost for a transaction at current
// base fees. Unlike EffectiveFee, this uses the base fees directly (not the
// minimum of max fee and base fee) for a worst-case cost estimate.
func (fm *FeeMarket) EstimateTotalCost(tx *MultidimTx) *big.Int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	total := new(big.Int)

	// Execution cost.
	if tx.GasLimit > 0 {
		total.Add(total, new(big.Int).Mul(
			fm.executionBaseFee,
			new(big.Int).SetUint64(tx.GasLimit),
		))
	}

	// Blob cost.
	blobGas := tx.BlobGasUsed()
	if blobGas > 0 {
		total.Add(total, new(big.Int).Mul(
			fm.blobBaseFee,
			new(big.Int).SetUint64(blobGas),
		))
	}

	// Access cost.
	if tx.AccessListSize > 0 {
		total.Add(total, new(big.Int).Mul(
			fm.accessBaseFee,
			new(big.Int).SetUint64(tx.AccessListSize),
		))
	}

	return total
}

// minBigInt returns the smaller of two big.Int values.
func minBigInt(a, b *big.Int) *big.Int {
	if a.Cmp(b) < 0 {
		return a
	}
	return b
}
