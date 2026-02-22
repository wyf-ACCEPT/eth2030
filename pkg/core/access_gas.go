// access_gas.go implements separate gas accounting for state access operations.
// Part of the I+ era roadmap: multidimensional gas pricing separates compute
// gas from state access gas, enabling independent limits and pricing for
// SLOAD/SSTORE operations vs pure computation.
package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Default gas costs for state access operations.
const (
	// DefaultReadGas is the gas charged per cold storage read (SLOAD).
	DefaultReadGas uint64 = 2100

	// DefaultWriteGas is the gas charged per cold storage write (SSTORE).
	DefaultWriteGas uint64 = 5000

	// WarmReadGas is the reduced cost for warm slot reads.
	WarmReadGas uint64 = 100

	// WarmWriteGas is the reduced cost for warm slot writes.
	WarmWriteGas uint64 = 100

	// DefaultAccessGasLimit is the default per-block access gas limit (I+ era).
	DefaultAccessGasLimit uint64 = 20_000_000

	// DefaultComputeGasLimit is the default per-block compute gas limit.
	DefaultComputeGasLimit uint64 = 30_000_000
)

// Access gas errors.
var (
	ErrAccessGasExceeded  = errors.New("access_gas: access gas limit exceeded")
	ErrComputeGasExceeded = errors.New("access_gas: compute gas limit exceeded")
	ErrNilCounter         = errors.New("access_gas: nil counter")
)

// accessKey uniquely identifies a state access (address + storage slot).
type accessKey struct {
	Addr types.Address
	Slot types.Hash
}

// AccessGasConfig holds the gas limits for compute and access dimensions.
type AccessGasConfig struct {
	// AccessGasLimit is the maximum total access gas per block.
	AccessGasLimit uint64
	// ComputeGasLimit is the maximum total compute gas per block.
	ComputeGasLimit uint64
	// ReadGasCost is the gas cost per cold state read.
	ReadGasCost uint64
	// WriteGasCost is the gas cost per cold state write.
	WriteGasCost uint64
	// WarmReadCost is the gas cost for a warm slot read.
	WarmReadCost uint64
	// WarmWriteCost is the gas cost for a warm slot write.
	WarmWriteCost uint64
}

// DefaultAccessGasConfig returns a config with sensible defaults for the I+ era.
func DefaultAccessGasConfig() AccessGasConfig {
	return AccessGasConfig{
		AccessGasLimit:  DefaultAccessGasLimit,
		ComputeGasLimit: DefaultComputeGasLimit,
		ReadGasCost:     DefaultReadGas,
		WriteGasCost:    DefaultWriteGas,
		WarmReadCost:    WarmReadGas,
		WarmWriteCost:   WarmWriteGas,
	}
}

// AccessRecord captures a single state access for auditing.
type AccessRecord struct {
	Addr    types.Address
	Slot    types.Hash
	IsWrite bool
	GasUsed uint64
	Warm    bool
}

// AccessGasCounter tracks gas consumption for state access operations
// independently from compute gas. This enables multidimensional gas pricing
// where read/write access is metered separately from execution gas.
type AccessGasCounter struct {
	mu sync.RWMutex

	config AccessGasConfig

	// Cumulative gas consumed.
	readGasUsed  uint64
	writeGasUsed uint64

	// Warm tracking: slots already accessed in this transaction context.
	warmSlots map[accessKey]bool

	// Access records for tracing/debugging.
	records []AccessRecord
}

// NewAccessGasCounter creates a new counter with the given config.
func NewAccessGasCounter(config AccessGasConfig) *AccessGasCounter {
	return &AccessGasCounter{
		config:    config,
		warmSlots: make(map[accessKey]bool),
	}
}

// TrackRead charges access gas for a storage read (SLOAD). Cold reads are
// charged the full ReadGasCost; warm reads are charged WarmReadCost.
// Returns the gas actually charged and any error if the limit is exceeded.
func (c *AccessGasCounter) TrackRead(addr types.Address, slot types.Hash) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := accessKey{Addr: addr, Slot: slot}
	warm := c.warmSlots[key]

	var cost uint64
	if warm {
		cost = c.config.WarmReadCost
	} else {
		cost = c.config.ReadGasCost
		c.warmSlots[key] = true
	}

	if c.readGasUsed+c.writeGasUsed+cost > c.config.AccessGasLimit {
		return 0, fmt.Errorf("%w: need %d, available %d",
			ErrAccessGasExceeded, cost, c.config.AccessGasLimit-c.readGasUsed-c.writeGasUsed)
	}

	c.readGasUsed += cost
	c.records = append(c.records, AccessRecord{
		Addr:    addr,
		Slot:    slot,
		IsWrite: false,
		GasUsed: cost,
		Warm:    warm,
	})

	return cost, nil
}

// TrackWrite charges access gas for a storage write (SSTORE). Cold writes are
// charged the full WriteGasCost; warm writes are charged WarmWriteCost.
// Returns the gas actually charged and any error if the limit is exceeded.
func (c *AccessGasCounter) TrackWrite(addr types.Address, slot types.Hash) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := accessKey{Addr: addr, Slot: slot}
	warm := c.warmSlots[key]

	var cost uint64
	if warm {
		cost = c.config.WarmWriteCost
	} else {
		cost = c.config.WriteGasCost
		c.warmSlots[key] = true
	}

	if c.readGasUsed+c.writeGasUsed+cost > c.config.AccessGasLimit {
		return 0, fmt.Errorf("%w: need %d, available %d",
			ErrAccessGasExceeded, cost, c.config.AccessGasLimit-c.readGasUsed-c.writeGasUsed)
	}

	c.writeGasUsed += cost
	c.records = append(c.records, AccessRecord{
		Addr:    addr,
		Slot:    slot,
		IsWrite: true,
		GasUsed: cost,
		Warm:    warm,
	})

	return cost, nil
}

// ReadGasUsed returns the total read gas consumed.
func (c *AccessGasCounter) ReadGasUsed() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.readGasUsed
}

// WriteGasUsed returns the total write gas consumed.
func (c *AccessGasCounter) WriteGasUsed() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.writeGasUsed
}

// TotalAccessGasUsed returns total access gas (read + write).
func (c *AccessGasCounter) TotalAccessGasUsed() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.readGasUsed + c.writeGasUsed
}

// IsAccessGasExceeded returns true if the cumulative access gas exceeds
// the configured access gas limit.
func (c *AccessGasCounter) IsAccessGasExceeded() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.readGasUsed+c.writeGasUsed > c.config.AccessGasLimit
}

// Remaining returns the amount of access gas still available.
func (c *AccessGasCounter) Remaining() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	used := c.readGasUsed + c.writeGasUsed
	if used >= c.config.AccessGasLimit {
		return 0
	}
	return c.config.AccessGasLimit - used
}

// Records returns a copy of all access records.
func (c *AccessGasCounter) Records() []AccessRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	recs := make([]AccessRecord, len(c.records))
	copy(recs, c.records)
	return recs
}

// WarmSlotCount returns how many unique slots have been warmed.
func (c *AccessGasCounter) WarmSlotCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.warmSlots)
}

// IsWarm returns true if the given (addr, slot) pair is warm.
func (c *AccessGasCounter) IsWarm(addr types.Address, slot types.Hash) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.warmSlots[accessKey{Addr: addr, Slot: slot}]
}

// MergeCounters merges another counter's gas usage and warm slots into this one.
// This is used to combine results from parallel execution contexts.
func (c *AccessGasCounter) MergeCounters(other *AccessGasCounter) error {
	if other == nil {
		return ErrNilCounter
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()

	c.readGasUsed += other.readGasUsed
	c.writeGasUsed += other.writeGasUsed

	for key, warm := range other.warmSlots {
		if warm {
			c.warmSlots[key] = true
		}
	}

	c.records = append(c.records, other.records...)
	return nil
}

// Reset clears all tracked state, setting gas counters to zero and clearing
// warm slots. The config is preserved.
func (c *AccessGasCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.readGasUsed = 0
	c.writeGasUsed = 0
	c.warmSlots = make(map[accessKey]bool)
	c.records = nil
}

// Config returns the current access gas configuration.
func (c *AccessGasCounter) Config() AccessGasConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// Summary returns a human-readable summary of gas usage.
func (c *AccessGasCounter) Summary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := c.readGasUsed + c.writeGasUsed
	return fmt.Sprintf("access_gas: read=%d write=%d total=%d/%d (%.1f%% used, %d warm slots)",
		c.readGasUsed, c.writeGasUsed, total, c.config.AccessGasLimit,
		float64(total)/float64(c.config.AccessGasLimit)*100, len(c.warmSlots))
}
