// gas_scheduler.go implements multi-dimensional gas accounting for the gigagas
// execution target. Instead of a single gas pool, each resource dimension
// (compute, storage reads, storage writes, bandwidth, state growth) has its
// own gas budget and pricing, following the EIP-4844 style multidimensional
// fee market extended to all execution resources.
//
// This enables fairer pricing: a compute-heavy transaction no longer competes
// for the same budget as a storage-heavy one, preventing one dimension from
// monopolizing block capacity.
package vm

import (
	"errors"
	"fmt"
	"sync"
)

// ResourceType identifies a gas resource dimension.
type ResourceType uint8

const (
	// Compute covers CPU-bound opcode execution (arithmetic, hashing, etc).
	Compute ResourceType = iota
	// StorageRead covers state/storage read operations (SLOAD, BALANCE, etc).
	StorageRead
	// StorageWrite covers state/storage write operations (SSTORE, CREATE, etc).
	StorageWrite
	// Bandwidth covers calldata and transaction envelope size costs.
	Bandwidth
	// StateGrowth covers new account/storage slot creation (state expansion).
	StateGrowth

	resourceTypeCount = 5
)

// resourceTypeNames maps ResourceType values to human-readable names.
var resourceTypeNames = [resourceTypeCount]string{
	"Compute",
	"StorageRead",
	"StorageWrite",
	"Bandwidth",
	"StateGrowth",
}

// String returns the name of the resource type.
func (r ResourceType) String() string {
	if int(r) < len(resourceTypeNames) {
		return resourceTypeNames[r]
	}
	return "Unknown"
}

// Gas scheduler errors.
var (
	ErrResourceExhausted   = errors.New("gas_scheduler: resource gas exhausted")
	ErrInvalidResourceType = errors.New("gas_scheduler: invalid resource type")
	ErrSchedulerClosed     = errors.New("gas_scheduler: scheduler has been reset")
)

// ResourceConfig defines the gas budget and pricing for a single dimension.
type ResourceConfig struct {
	// Limit is the maximum gas for this dimension within the block.
	Limit uint64
	// TargetUsage is the target utilization (for EIP-1559-style adjustment).
	// Expressed as a fraction of Limit (e.g., 50% target = Limit/2).
	TargetUsage uint64
	// BaseFeeMultiplier scales the base fee for this resource dimension.
	// For example, storage writes may have a 2x multiplier relative to compute.
	BaseFeeMultiplier uint64
	// MinGasPrice is the floor price per gas unit for this dimension.
	MinGasPrice uint64
}

// GasSchedulerConfig holds configuration for all resource dimensions.
type GasSchedulerConfig struct {
	// BlockGasLimit is the overall block gas limit (used for validation).
	BlockGasLimit uint64
	// Resources maps each ResourceType to its configuration.
	Resources [resourceTypeCount]ResourceConfig
}

// DefaultGasSchedulerConfig returns a sensible default configuration for
// gigagas-targeted blocks (30M gas limit as baseline, scaled per dimension).
func DefaultGasSchedulerConfig() GasSchedulerConfig {
	blockGasLimit := uint64(30_000_000)
	return GasSchedulerConfig{
		BlockGasLimit: blockGasLimit,
		Resources: [resourceTypeCount]ResourceConfig{
			Compute: {
				Limit:             blockGasLimit,
				TargetUsage:       blockGasLimit / 2,
				BaseFeeMultiplier: 1,
				MinGasPrice:       1,
			},
			StorageRead: {
				Limit:             blockGasLimit / 4,      // 25% of block for reads
				TargetUsage:       blockGasLimit / 8,      // 50% of read limit
				BaseFeeMultiplier: 2,                      // reads cost 2x compute
				MinGasPrice:       1,
			},
			StorageWrite: {
				Limit:             blockGasLimit / 8,      // 12.5% of block for writes
				TargetUsage:       blockGasLimit / 16,     // 50% of write limit
				BaseFeeMultiplier: 5,                      // writes cost 5x compute
				MinGasPrice:       1,
			},
			Bandwidth: {
				Limit:             blockGasLimit / 4,      // 25% for calldata/bandwidth
				TargetUsage:       blockGasLimit / 8,
				BaseFeeMultiplier: 1,
				MinGasPrice:       1,
			},
			StateGrowth: {
				Limit:             blockGasLimit / 16,     // 6.25% for state growth
				TargetUsage:       blockGasLimit / 32,
				BaseFeeMultiplier: 10,                     // state growth is expensive
				MinGasPrice:       1,
			},
		},
	}
}

// resourceState tracks gas usage for a single dimension.
type resourceState struct {
	used  uint64
	limit uint64
}

// GasScheduler tracks per-resource gas usage within a block and computes
// per-resource gas prices. Thread-safe for concurrent transaction execution.
type GasScheduler struct {
	mu     sync.Mutex
	config GasSchedulerConfig
	state  [resourceTypeCount]resourceState
}

// NewGasScheduler creates a scheduler from the given config.
func NewGasScheduler(config GasSchedulerConfig) *GasScheduler {
	gs := &GasScheduler{config: config}
	for i := 0; i < resourceTypeCount; i++ {
		gs.state[i] = resourceState{
			used:  0,
			limit: config.Resources[i].Limit,
		}
	}
	return gs
}

// AccountGas charges gas for the specified resource type. Returns an error
// if the resource's gas limit would be exceeded.
func (gs *GasScheduler) AccountGas(resource ResourceType, amount uint64) error {
	if int(resource) >= resourceTypeCount {
		return fmt.Errorf("%w: %d", ErrInvalidResourceType, resource)
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()

	s := &gs.state[resource]
	if s.used+amount > s.limit {
		return fmt.Errorf("%w: %s needs %d, remaining %d",
			ErrResourceExhausted, resource.String(), amount, s.limit-s.used)
	}
	s.used += amount
	return nil
}

// RemainingGas returns the remaining gas for the specified resource.
func (gs *GasScheduler) RemainingGas(resource ResourceType) uint64 {
	if int(resource) >= resourceTypeCount {
		return 0
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()

	s := gs.state[resource]
	if s.used >= s.limit {
		return 0
	}
	return s.limit - s.used
}

// UsedGas returns the consumed gas for the specified resource.
func (gs *GasScheduler) UsedGas(resource ResourceType) uint64 {
	if int(resource) >= resourceTypeCount {
		return 0
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()
	return gs.state[resource].used
}

// GasPrice computes the effective gas price for a resource dimension given
// the current base fee. This implements an EIP-1559-style mechanism where
// the price adjusts based on utilization relative to the target:
//
//	effectivePrice = baseFee * multiplier * (1 + deviation)
//
// where deviation = (used - target) / target, clamped to [-0.5, 2.0].
// This encourages utilization near the target and penalizes excess usage.
func (gs *GasScheduler) GasPrice(resource ResourceType, baseFee uint64) uint64 {
	if int(resource) >= resourceTypeCount {
		return 0
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()

	cfg := gs.config.Resources[resource]
	s := gs.state[resource]

	// Base price = baseFee * multiplier.
	basePrice := baseFee * cfg.BaseFeeMultiplier
	if basePrice == 0 {
		basePrice = cfg.MinGasPrice
	}

	target := cfg.TargetUsage
	if target == 0 {
		return basePrice
	}

	// Compute adjustment factor based on utilization vs target.
	// Using integer arithmetic with 1000 as scale factor.
	const scale = 1000
	used := s.used

	var adjustedPrice uint64
	if used <= target {
		// Under-utilized: discount by up to 50%.
		// ratio = used * scale / target (range: 0..1000)
		ratio := used * scale / target
		// factor = 500 + ratio/2 (range: 500..1000)
		factor := 500 + ratio/2
		adjustedPrice = basePrice * factor / scale
	} else {
		// Over-utilized: premium of up to 3x.
		// excess = (used - target) * scale / target
		excess := (used - target) * scale / target
		if excess > 2000 {
			excess = 2000 // cap at 3x
		}
		factor := scale + excess
		adjustedPrice = basePrice * factor / scale
	}

	if adjustedPrice < cfg.MinGasPrice {
		adjustedPrice = cfg.MinGasPrice
	}
	return adjustedPrice
}

// TotalUsed returns the sum of gas used across all dimensions.
func (gs *GasScheduler) TotalUsed() uint64 {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	var total uint64
	for i := 0; i < resourceTypeCount; i++ {
		total += gs.state[i].used
	}
	return total
}

// Utilization returns the utilization ratio (0-100) for a resource dimension.
func (gs *GasScheduler) Utilization(resource ResourceType) uint64 {
	if int(resource) >= resourceTypeCount {
		return 0
	}
	gs.mu.Lock()
	defer gs.mu.Unlock()

	s := gs.state[resource]
	if s.limit == 0 {
		return 0
	}
	return s.used * 100 / s.limit
}

// IsExhausted returns true if any resource dimension has been fully consumed.
func (gs *GasScheduler) IsExhausted() bool {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	for i := 0; i < resourceTypeCount; i++ {
		if gs.state[i].used >= gs.state[i].limit {
			return true
		}
	}
	return false
}

// ResetForBlock resets all resource counters for a new block. Optionally
// accepts a new block gas limit to recalculate per-resource limits.
func (gs *GasScheduler) ResetForBlock(blockGasLimit uint64) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if blockGasLimit > 0 {
		gs.config.BlockGasLimit = blockGasLimit
		// Scale all resource limits proportionally if block gas limit changed.
		oldLimit := gs.config.BlockGasLimit
		if oldLimit > 0 && oldLimit != blockGasLimit {
			for i := 0; i < resourceTypeCount; i++ {
				gs.config.Resources[i].Limit = gs.config.Resources[i].Limit * blockGasLimit / oldLimit
				gs.config.Resources[i].TargetUsage = gs.config.Resources[i].TargetUsage * blockGasLimit / oldLimit
			}
		}
	}

	for i := 0; i < resourceTypeCount; i++ {
		gs.state[i].used = 0
		gs.state[i].limit = gs.config.Resources[i].Limit
	}
}

// ResourceSnapshot captures the current gas usage state for all dimensions.
type ResourceSnapshot struct {
	Used  [resourceTypeCount]uint64
	Limit [resourceTypeCount]uint64
}

// Snapshot returns the current gas usage state across all dimensions.
func (gs *GasScheduler) Snapshot() ResourceSnapshot {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	var snap ResourceSnapshot
	for i := 0; i < resourceTypeCount; i++ {
		snap.Used[i] = gs.state[i].used
		snap.Limit[i] = gs.state[i].limit
	}
	return snap
}

// RestoreSnapshot restores the gas usage state from a snapshot.
// Used for reverting state after a failed transaction.
func (gs *GasScheduler) RestoreSnapshot(snap ResourceSnapshot) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	for i := 0; i < resourceTypeCount; i++ {
		gs.state[i].used = snap.Used[i]
		gs.state[i].limit = snap.Limit[i]
	}
}
