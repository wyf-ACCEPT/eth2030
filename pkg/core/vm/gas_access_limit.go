// gas_access_limit.go implements per-block access gas limit enforcement for
// the multidimensional gas pricing roadmap (access gas dimension). This
// separates state access costs into a dedicated gas budget, preventing
// storage-heavy transactions from monopolizing blocks.
//
// The access gas dimension tracks cold/warm account and slot access with an
// independent per-block limit derived from the block gas limit.
package vm

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Access gas cost constants. These mirror the EIP-2929 costs but are tracked
// in a separate access gas dimension rather than the compute gas pool.
const (
	ColdAccountAccessGas uint64 = 2600
	ColdSloadGas         uint64 = 2100
	WarmAccessGas        uint64 = 100
	ColdSstoreGas        uint64 = 5000
)

// Errors returned by the access gas tracker.
var (
	ErrAccessGasExhausted = errors.New("access gas limit exhausted")
	ErrInvalidAccessRatio = errors.New("access gas ratio must be in (0, 1]")
)

// AccessGasConfig holds configurable parameters for the access gas dimension.
type AccessGasConfig struct {
	ColdAccountGas uint64  // gas for cold account access
	ColdSloadGas   uint64  // gas for cold storage load
	WarmAccessGas  uint64  // gas for warm access (account or slot)
	ColdSstoreGas  uint64  // gas for cold storage store
	AccessGasRatio float64 // fraction of block gas limit allocated to access gas
}

// DefaultAccessGasConfig returns a config with the standard cost constants
// and a default access gas ratio of 0.25 (25% of block gas limit).
func DefaultAccessGasConfig() AccessGasConfig {
	return AccessGasConfig{
		ColdAccountGas: ColdAccountAccessGas,
		ColdSloadGas:   ColdSloadGas,
		WarmAccessGas:  WarmAccessGas,
		ColdSstoreGas:  ColdSstoreGas,
		AccessGasRatio: 0.25,
	}
}

// slotKey is the composite key for tracking warm storage slots.
type slotKey struct {
	addr types.Address
	slot types.Hash
}

// AccessGasTracker tracks per-block access gas usage with warm/cold
// accounting for addresses and storage slots. Thread-safe.
type AccessGasTracker struct {
	mu sync.Mutex

	config    AccessGasConfig
	limit     uint64 // access gas limit for the current block
	used      uint64 // access gas consumed so far
	warmAddrs map[types.Address]struct{}
	warmSlots map[slotKey]struct{}
}

// NewAccessGasTracker creates a tracker for the given block gas limit.
// accessGasRatio specifies what fraction of blockGasLimit is allocated
// to the access gas dimension (e.g. 0.25 means 25%).
func NewAccessGasTracker(blockGasLimit uint64, accessGasRatio float64) *AccessGasTracker {
	if accessGasRatio <= 0 || accessGasRatio > 1 {
		accessGasRatio = 0.25
	}
	cfg := DefaultAccessGasConfig()
	cfg.AccessGasRatio = accessGasRatio
	limit := uint64(float64(blockGasLimit) * accessGasRatio)

	return &AccessGasTracker{
		config:    cfg,
		limit:     limit,
		warmAddrs: make(map[types.Address]struct{}),
		warmSlots: make(map[slotKey]struct{}),
	}
}

// NewAccessGasTrackerWithConfig creates a tracker with a custom config.
func NewAccessGasTrackerWithConfig(blockGasLimit uint64, config AccessGasConfig) (*AccessGasTracker, error) {
	if config.AccessGasRatio <= 0 || config.AccessGasRatio > 1 {
		return nil, ErrInvalidAccessRatio
	}
	limit := uint64(float64(blockGasLimit) * config.AccessGasRatio)
	return &AccessGasTracker{
		config:    config,
		limit:     limit,
		warmAddrs: make(map[types.Address]struct{}),
		warmSlots: make(map[slotKey]struct{}),
	}, nil
}

// ChargeAccessGas charges access gas for a storage access operation.
// It determines the cost based on whether the address/slot is warm or cold,
// and whether the operation is a write (SSTORE) or read (SLOAD / account access).
// Returns the gas charged and an error if the access gas limit is exceeded.
func (t *AccessGasTracker) ChargeAccessGas(addr types.Address, slot types.Hash, isWrite bool) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	cost := t.accessCost(addr, slot, isWrite)

	if t.used+cost > t.limit {
		return 0, fmt.Errorf("%w: need %d, remaining %d", ErrAccessGasExhausted, cost, t.limit-t.used)
	}

	t.used += cost

	// Warm the address and slot after charging.
	t.warmAddrs[addr] = struct{}{}
	if slot != (types.Hash{}) || isWrite {
		t.warmSlots[slotKey{addr: addr, slot: slot}] = struct{}{}
	}

	return cost, nil
}

// accessCost computes the access gas cost for a given operation.
func (t *AccessGasTracker) accessCost(addr types.Address, slot types.Hash, isWrite bool) uint64 {
	// Check if the slot is warm (for slot-level operations).
	isSlotOp := slot != (types.Hash{}) || isWrite
	if isSlotOp {
		sk := slotKey{addr: addr, slot: slot}
		if _, warm := t.warmSlots[sk]; warm {
			return t.config.WarmAccessGas
		}
		// Cold slot access.
		if isWrite {
			return t.config.ColdSstoreGas
		}
		return t.config.ColdSloadGas
	}

	// Address-level access (BALANCE, EXTCODESIZE, etc.).
	if _, warm := t.warmAddrs[addr]; warm {
		return t.config.WarmAccessGas
	}
	return t.config.ColdAccountGas
}

// RemainingAccessGas returns the remaining access gas budget for the block.
func (t *AccessGasTracker) RemainingAccessGas() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.used >= t.limit {
		return 0
	}
	return t.limit - t.used
}

// AccessGasUsed returns the total access gas consumed so far.
func (t *AccessGasTracker) AccessGasUsed() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used
}

// IsAccessExhausted returns true if the access gas limit has been reached.
func (t *AccessGasTracker) IsAccessExhausted() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used >= t.limit
}

// ResetForBlock resets the tracker for a new block with the given gas limit.
// Warm sets and usage counters are cleared.
func (t *AccessGasTracker) ResetForBlock(blockGasLimit uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.limit = uint64(float64(blockGasLimit) * t.config.AccessGasRatio)
	t.used = 0
	t.warmAddrs = make(map[types.Address]struct{})
	t.warmSlots = make(map[slotKey]struct{})
}

// WarmAddress marks an address as warm (reduced access cost on subsequent access).
func (t *AccessGasTracker) WarmAddress(addr types.Address) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.warmAddrs[addr] = struct{}{}
}

// WarmSlot marks a storage slot as warm. Also warms the address.
func (t *AccessGasTracker) WarmSlot(addr types.Address, slot types.Hash) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.warmAddrs[addr] = struct{}{}
	t.warmSlots[slotKey{addr: addr, slot: slot}] = struct{}{}
}

// IsWarm returns whether the address is in the warm set.
func (t *AccessGasTracker) IsWarm(addr types.Address) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.warmAddrs[addr]
	return ok
}

// IsSlotWarm returns whether a specific storage slot is in the warm set.
func (t *AccessGasTracker) IsSlotWarm(addr types.Address, slot types.Hash) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.warmSlots[slotKey{addr: addr, slot: slot}]
	return ok
}

// AccessGasLimit returns the access gas limit for the current block.
func (t *AccessGasTracker) AccessGasLimit() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.limit
}
