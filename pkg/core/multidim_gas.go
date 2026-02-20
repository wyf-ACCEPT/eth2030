// multidim_gas.go implements a comprehensive 5-dimensional gas pricing engine
// for the Ethereum 2028 roadmap. It extends the existing 3-dimensional model
// (compute, calldata, blob) with two additional dimensions (storage, witness)
// and provides full EIP-1559-style independent base fee adjustment per dimension.
//
// Each dimension tracks its own base fee history, target usage, and elasticity
// parameters. The engine is thread-safe and suitable for concurrent access
// during block building and validation.
package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
)

// GasDimension identifies one of the five gas dimensions tracked by the
// multidimensional pricing engine.
type GasDimension int

const (
	// DimCompute is execution/compute gas (traditional EVM gas).
	DimCompute GasDimension = iota
	// DimStorage is state storage gas (SSTORE, SLOAD, state access).
	DimStorage
	// DimBandwidth is calldata/bandwidth gas (data availability on L1).
	DimBandwidth
	// DimBlob is blob gas (EIP-4844 data blobs).
	DimBlob
	// DimWitness is witness gas (EIP-6800 stateless execution witnesses).
	DimWitness

	// NumGasDimensions is the total number of gas dimensions.
	NumGasDimensions = 5
)

// String returns the human-readable name for a gas dimension.
func (d GasDimension) String() string {
	switch d {
	case DimCompute:
		return "compute"
	case DimStorage:
		return "storage"
	case DimBandwidth:
		return "bandwidth"
	case DimBlob:
		return "blob"
	case DimWitness:
		return "witness"
	default:
		return fmt.Sprintf("unknown(%d)", int(d))
	}
}

// AllGasDimensions returns a slice of all five gas dimensions.
func AllGasDimensions() []GasDimension {
	return []GasDimension{DimCompute, DimStorage, DimBandwidth, DimBlob, DimWitness}
}

// Multidimensional gas pricing errors.
var (
	ErrMDGasInvalidConfig   = errors.New("mdgas: invalid config")
	ErrMDGasInvalidDim      = errors.New("mdgas: invalid dimension")
	ErrMDGasHistoryEmpty    = errors.New("mdgas: base fee history is empty")
	ErrMDGasUsageExceedsMax = errors.New("mdgas: usage exceeds max gas")
)

// DimGasConfig holds the EIP-1559-style fee adjustment parameters for one
// gas dimension. Each dimension can have its own target, max, elasticity,
// and adjustment speed.
type DimGasConfig struct {
	Target               uint64   // target gas usage per block
	MaxGas               uint64   // maximum gas per block (limit)
	ElasticityMul        uint64   // limit / target ratio
	BaseFeeChangeDenom   uint64   // controls adjustment speed (higher = slower)
	MinBaseFee           *big.Int // floor for the base fee
	MaxBaseFee           *big.Int // ceiling for the base fee (nil = unlimited)
}

// validate checks that the config is self-consistent.
func (c *DimGasConfig) validate() error {
	if c.Target == 0 {
		return fmt.Errorf("%w: target must be > 0", ErrMDGasInvalidConfig)
	}
	if c.MaxGas == 0 {
		return fmt.Errorf("%w: max gas must be > 0", ErrMDGasInvalidConfig)
	}
	if c.BaseFeeChangeDenom == 0 {
		return fmt.Errorf("%w: base fee change denominator must be > 0", ErrMDGasInvalidConfig)
	}
	if c.MinBaseFee == nil || c.MinBaseFee.Sign() <= 0 {
		return fmt.Errorf("%w: min base fee must be > 0", ErrMDGasInvalidConfig)
	}
	return nil
}

// MultidimGasConfig holds configuration for all five gas dimensions.
type MultidimGasConfig struct {
	Dims         [NumGasDimensions]DimGasConfig
	HistoryLimit int // max number of historical base fee entries per dimension
}

// DefaultMultidimGasConfig returns production-like defaults for all dimensions.
func DefaultMultidimGasConfig() MultidimGasConfig {
	return MultidimGasConfig{
		Dims: [NumGasDimensions]DimGasConfig{
			{ // Compute
				Target:             15_000_000,
				MaxGas:             30_000_000,
				ElasticityMul:      2,
				BaseFeeChangeDenom: 8,
				MinBaseFee:         big.NewInt(7),
			},
			{ // Storage
				Target:             5_000_000,
				MaxGas:             10_000_000,
				ElasticityMul:      2,
				BaseFeeChangeDenom: 8,
				MinBaseFee:         big.NewInt(1),
			},
			{ // Bandwidth (calldata)
				Target:             1_875_000,
				MaxGas:             7_500_000,
				ElasticityMul:      4,
				BaseFeeChangeDenom: 8,
				MinBaseFee:         big.NewInt(1),
			},
			{ // Blob
				Target:             393_216,
				MaxGas:             786_432,
				ElasticityMul:      2,
				BaseFeeChangeDenom: 3,
				MinBaseFee:         big.NewInt(1),
			},
			{ // Witness
				Target:             2_000_000,
				MaxGas:             4_000_000,
				ElasticityMul:      2,
				BaseFeeChangeDenom: 6,
				MinBaseFee:         big.NewInt(1),
			},
		},
		HistoryLimit: 256,
	}
}

// Validate checks that all dimension configs are valid.
func (cfg *MultidimGasConfig) Validate() error {
	for i, dim := range cfg.Dims {
		if err := dim.validate(); err != nil {
			return fmt.Errorf("dimension %s: %w", GasDimension(i), err)
		}
	}
	if cfg.HistoryLimit <= 0 {
		return fmt.Errorf("%w: history limit must be > 0", ErrMDGasInvalidConfig)
	}
	return nil
}

// DimensionalGasPrice holds the per-dimension base fee state and history.
type DimensionalGasPrice struct {
	baseFee *big.Int
	history []*big.Int // most recent at the end
}

// MultidimPricingEngine tracks 5 independent gas dimensions with their own
// EIP-1559-style base fee adjustments, usage targets, and price history.
// All methods are thread-safe.
type MultidimPricingEngine struct {
	mu     sync.RWMutex
	config MultidimGasConfig
	prices [NumGasDimensions]*DimensionalGasPrice
}

// NewMultidimPricingEngine creates a new engine with the given config.
// Base fees are initialized to each dimension's minimum.
func NewMultidimPricingEngine(cfg MultidimGasConfig) (*MultidimPricingEngine, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	e := &MultidimPricingEngine{config: cfg}
	for i := 0; i < NumGasDimensions; i++ {
		e.prices[i] = &DimensionalGasPrice{
			baseFee: new(big.Int).Set(cfg.Dims[i].MinBaseFee),
			history: make([]*big.Int, 0, cfg.HistoryLimit),
		}
	}
	return e, nil
}

// NewMultidimPricingEngineWithFees creates an engine with explicit initial base
// fees for each dimension.
func NewMultidimPricingEngineWithFees(cfg MultidimGasConfig, fees map[GasDimension]*big.Int) (*MultidimPricingEngine, error) {
	e, err := NewMultidimPricingEngine(cfg)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for dim, fee := range fees {
		if dim >= 0 && int(dim) < NumGasDimensions && fee != nil {
			e.prices[dim].baseFee = new(big.Int).Set(fee)
		}
	}
	return e, nil
}

// BaseFee returns the current base fee for the specified dimension.
// Returns a defensive copy.
func (e *MultidimPricingEngine) BaseFee(dim GasDimension) *big.Int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if dim < 0 || int(dim) >= NumGasDimensions {
		return new(big.Int)
	}
	return new(big.Int).Set(e.prices[dim].baseFee)
}

// AllBaseFees returns a snapshot of the current base fees for all dimensions.
func (e *MultidimPricingEngine) AllBaseFees() map[GasDimension]*big.Int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make(map[GasDimension]*big.Int, NumGasDimensions)
	for i := 0; i < NumGasDimensions; i++ {
		result[GasDimension(i)] = new(big.Int).Set(e.prices[i].baseFee)
	}
	return result
}

// UpdateBaseFees adjusts base fees for all dimensions based on actual block
// usage. Each dimension is adjusted independently using the EIP-1559 formula:
//
//	newBase = oldBase * (1 + (used - target) / target / denominator)
//
// Fees are clamped to the configured minimum (and maximum if set).
// The previous base fees are recorded in the history ring buffer.
func (e *MultidimPricingEngine) UpdateBaseFees(
	blockUsage map[GasDimension]uint64,
	targets map[GasDimension]uint64,
) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i := 0; i < NumGasDimensions; i++ {
		dim := GasDimension(i)
		price := e.prices[i]
		cfg := e.config.Dims[i]

		// Record current base fee in history before updating.
		e.appendHistory(i, price.baseFee)

		// Determine the target for this block. If custom targets are provided
		// and contain this dimension, use them; otherwise use the config default.
		target := cfg.Target
		if targets != nil {
			if t, ok := targets[dim]; ok && t > 0 {
				target = t
			}
		}

		used := uint64(0)
		if blockUsage != nil {
			used = blockUsage[dim]
		}

		price.baseFee = e.adjustDimBaseFee(price.baseFee, used, target, cfg)
	}
}

// adjustDimBaseFee implements the EIP-1559 formula for a single dimension:
//
//	if used > target: newBase = oldBase + oldBase * (used - target) / target / denom
//	if used < target: newBase = oldBase - oldBase * (target - used) / target / denom
//	if used == target: newBase = oldBase
func (e *MultidimPricingEngine) adjustDimBaseFee(
	current *big.Int,
	used, target uint64,
	cfg DimGasConfig,
) *big.Int {
	if used == target {
		return new(big.Int).Set(current)
	}

	bigTarget := new(big.Int).SetUint64(target)
	bigDenom := new(big.Int).SetUint64(cfg.BaseFeeChangeDenom)

	if used > target {
		delta := new(big.Int).SetUint64(used - target)
		// baseFee * (used - target) / target / denominator
		adj := new(big.Int).Mul(current, delta)
		adj.Div(adj, bigTarget)
		adj.Div(adj, bigDenom)
		// Ensure minimum increase of 1.
		if adj.Sign() == 0 {
			adj.SetInt64(1)
		}
		newFee := new(big.Int).Add(current, adj)
		return e.clampFee(newFee, cfg)
	}

	// used < target
	delta := new(big.Int).SetUint64(target - used)
	adj := new(big.Int).Mul(current, delta)
	adj.Div(adj, bigTarget)
	adj.Div(adj, bigDenom)

	newFee := new(big.Int).Sub(current, adj)
	return e.clampFee(newFee, cfg)
}

// clampFee ensures the fee stays within [MinBaseFee, MaxBaseFee].
func (e *MultidimPricingEngine) clampFee(fee *big.Int, cfg DimGasConfig) *big.Int {
	if fee.Cmp(cfg.MinBaseFee) < 0 {
		return new(big.Int).Set(cfg.MinBaseFee)
	}
	if cfg.MaxBaseFee != nil && fee.Cmp(cfg.MaxBaseFee) > 0 {
		return new(big.Int).Set(cfg.MaxBaseFee)
	}
	return fee
}

// appendHistory adds a base fee entry to the history ring buffer for the
// given dimension, evicting the oldest entry if at capacity.
func (e *MultidimPricingEngine) appendHistory(dimIdx int, fee *big.Int) {
	price := e.prices[dimIdx]
	entry := new(big.Int).Set(fee)
	if len(price.history) >= e.config.HistoryLimit {
		// Shift left by 1 (evict oldest).
		copy(price.history, price.history[1:])
		price.history[len(price.history)-1] = entry
	} else {
		price.history = append(price.history, entry)
	}
}

// TotalGasCost computes the total wei cost for the given per-dimension gas
// usage at current base fees: sum(usage[dim] * baseFee[dim]).
func (e *MultidimPricingEngine) TotalGasCost(usage map[GasDimension]uint64) *big.Int {
	e.mu.RLock()
	defer e.mu.RUnlock()

	total := new(big.Int)
	for dim, amount := range usage {
		if dim < 0 || int(dim) >= NumGasDimensions || amount == 0 {
			continue
		}
		cost := new(big.Int).Mul(
			e.prices[dim].baseFee,
			new(big.Int).SetUint64(amount),
		)
		total.Add(total, cost)
	}
	return total
}

// EffectiveGasPrice computes the total effective gas cost for a transaction
// given its per-dimension gas usage. This is identical to TotalGasCost but
// named for clarity when working with transaction-level pricing.
func (e *MultidimPricingEngine) EffectiveGasPrice(usage map[GasDimension]uint64) *big.Int {
	return e.TotalGasCost(usage)
}

// BaseFeeHistory returns a copy of the base fee history for the given
// dimension, ordered oldest to newest.
func (e *MultidimPricingEngine) BaseFeeHistory(dim GasDimension) ([]*big.Int, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if dim < 0 || int(dim) >= NumGasDimensions {
		return nil, fmt.Errorf("%w: %d", ErrMDGasInvalidDim, dim)
	}

	src := e.prices[dim].history
	result := make([]*big.Int, len(src))
	for i, fee := range src {
		result[i] = new(big.Int).Set(fee)
	}
	return result, nil
}

// HistoryLen returns the number of historical base fee entries for a dimension.
func (e *MultidimPricingEngine) HistoryLen(dim GasDimension) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if dim < 0 || int(dim) >= NumGasDimensions {
		return 0
	}
	return len(e.prices[dim].history)
}

// Config returns a copy of the engine's configuration.
func (e *MultidimPricingEngine) Config() MultidimGasConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	// Copy the config. big.Int fields need deep copy.
	cfg := e.config
	for i := 0; i < NumGasDimensions; i++ {
		cfg.Dims[i].MinBaseFee = new(big.Int).Set(e.config.Dims[i].MinBaseFee)
		if e.config.Dims[i].MaxBaseFee != nil {
			cfg.Dims[i].MaxBaseFee = new(big.Int).Set(e.config.Dims[i].MaxBaseFee)
		}
	}
	return cfg
}

// ValidateUsage checks that none of the usage values exceed the configured
// max gas for their dimension.
func (e *MultidimPricingEngine) ValidateUsage(usage map[GasDimension]uint64) error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for dim, amount := range usage {
		if dim < 0 || int(dim) >= NumGasDimensions {
			return fmt.Errorf("%w: %d", ErrMDGasInvalidDim, dim)
		}
		if amount > e.config.Dims[dim].MaxGas {
			return fmt.Errorf("%w: %s used %d, max %d",
				ErrMDGasUsageExceedsMax, dim, amount, e.config.Dims[dim].MaxGas)
		}
	}
	return nil
}
