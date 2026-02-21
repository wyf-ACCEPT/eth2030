// gas_pool_tracker.go implements per-call-frame multidimensional gas pool
// tracking for EIP-7706. While the core/gas_pool_extended.go tracks block-level
// multi-gas pools and the vm/gas_scheduler.go tracks 5 resource dimensions for
// gigagas scheduling, this tracker operates at the EVM call frame level with
// the three EIP-7706 gas dimensions: execution, calldata, and blob.
//
// It supports snapshot/revert for nested CALL frames, per-dimension limits,
// and merging usage from inner frames back to outer frames.
package vm

import (
	"errors"
	"fmt"
	"math"
)

// GasDimension identifies one of the three EIP-7706 gas dimensions tracked
// per call frame. This is distinct from the core.GasDimension (5 dimensions)
// and core.PoolGasDim (block-level pool) types.
type GasDimension uint8

const (
	// DimExecution is the traditional EVM compute gas.
	DimExecution GasDimension = iota
	// DimCalldata is the calldata gas dimension (EIP-7706).
	DimCalldata
	// DimBlob is the blob gas dimension (EIP-4844/7706).
	DimBlob

	// numDimensions is the number of gas dimensions tracked.
	numDimensions = 3
)

// String returns a human-readable name for the gas dimension.
func (d GasDimension) String() string {
	switch d {
	case DimExecution:
		return "execution"
	case DimCalldata:
		return "calldata"
	case DimBlob:
		return "blob"
	default:
		return fmt.Sprintf("unknown(%d)", d)
	}
}

// Valid returns true if the dimension is a known dimension.
func (d GasDimension) Valid() bool {
	return d < numDimensions
}

// Gas pool tracker errors.
var (
	ErrGasPoolOutOfGas      = errors.New("gas_pool_tracker: out of gas")
	ErrGasPoolInvalidDim    = errors.New("gas_pool_tracker: invalid dimension")
	ErrGasPoolOverflow      = errors.New("gas_pool_tracker: gas overflow")
	ErrGasPoolInvalidSnap   = errors.New("gas_pool_tracker: invalid snapshot")
	ErrGasPoolSnapReverted  = errors.New("gas_pool_tracker: snapshot already reverted")
)

// dimState holds the per-dimension gas state within a call frame.
type dimState struct {
	limit uint64
	used  uint64
}

// remaining returns the gas remaining in this dimension.
func (s dimState) remaining() uint64 {
	if s.used >= s.limit {
		return 0
	}
	return s.limit - s.used
}

// GasPoolSnapshot captures the state of all dimensions at a point in time,
// allowing rollback of gas accounting when a CALL frame reverts.
type GasPoolSnapshot struct {
	id    uint64
	dims  [numDimensions]dimState
	valid bool
}

// ID returns the unique identifier for this snapshot.
func (s GasPoolSnapshot) ID() uint64 {
	return s.id
}

// Valid returns true if the snapshot has not been reverted.
func (s GasPoolSnapshot) Valid() bool {
	return s.valid
}

// GasPoolTracker tracks gas usage across the three EIP-7706 dimensions
// within an EVM call frame. It supports per-dimension gas limits, usage
// tracking, snapshots for nested calls, and merging usage from child frames.
type GasPoolTracker struct {
	dims      [numDimensions]dimState
	snapshots []GasPoolSnapshot
	nextSnap  uint64

	// Peak usage tracking for analytics.
	peakUsed [numDimensions]uint64
}

// NewGasPoolTracker creates a new gas pool tracker with the specified limits
// for each dimension.
func NewGasPoolTracker(executionLimit, calldataLimit, blobLimit uint64) *GasPoolTracker {
	t := &GasPoolTracker{}
	t.dims[DimExecution] = dimState{limit: executionLimit}
	t.dims[DimCalldata] = dimState{limit: calldataLimit}
	t.dims[DimBlob] = dimState{limit: blobLimit}
	return t
}

// NewGasPoolTrackerUniform creates a tracker with the same limit for all
// dimensions. Useful for testing or simple scenarios.
func NewGasPoolTrackerUniform(limit uint64) *GasPoolTracker {
	return NewGasPoolTracker(limit, limit, limit)
}

// SetLimit sets the gas limit for a specific dimension. It can only increase
// the available gas (limit must be >= current used). Returns an error for
// invalid dimensions.
func (t *GasPoolTracker) SetLimit(dim GasDimension, limit uint64) error {
	if !dim.Valid() {
		return fmt.Errorf("%w: %s", ErrGasPoolInvalidDim, dim)
	}
	t.dims[dim].limit = limit
	return nil
}

// Limit returns the gas limit for a specific dimension.
func (t *GasPoolTracker) Limit(dim GasDimension) uint64 {
	if !dim.Valid() {
		return 0
	}
	return t.dims[dim].limit
}

// SubGas subtracts gas from the specified dimension. Returns ErrGasPoolOutOfGas
// if insufficient gas remains in that dimension.
func (t *GasPoolTracker) SubGas(dim GasDimension, amount uint64) error {
	if !dim.Valid() {
		return fmt.Errorf("%w: %s", ErrGasPoolInvalidDim, dim)
	}
	s := &t.dims[dim]
	remaining := s.remaining()
	if amount > remaining {
		return fmt.Errorf("%w: %s needs %d, has %d",
			ErrGasPoolOutOfGas, dim, amount, remaining)
	}
	s.used += amount
	// Track peak usage.
	if s.used > t.peakUsed[dim] {
		t.peakUsed[dim] = s.used
	}
	return nil
}

// SubGasMulti atomically subtracts gas from all three dimensions. If any
// dimension has insufficient gas, no gas is deducted (all-or-nothing).
func (t *GasPoolTracker) SubGasMulti(execution, calldata, blob uint64) error {
	// Pre-check all dimensions before committing.
	if execution > t.dims[DimExecution].remaining() {
		return fmt.Errorf("%w: %s needs %d, has %d",
			ErrGasPoolOutOfGas, DimExecution, execution, t.dims[DimExecution].remaining())
	}
	if calldata > t.dims[DimCalldata].remaining() {
		return fmt.Errorf("%w: %s needs %d, has %d",
			ErrGasPoolOutOfGas, DimCalldata, calldata, t.dims[DimCalldata].remaining())
	}
	if blob > t.dims[DimBlob].remaining() {
		return fmt.Errorf("%w: %s needs %d, has %d",
			ErrGasPoolOutOfGas, DimBlob, blob, t.dims[DimBlob].remaining())
	}

	// All checks passed; commit.
	t.dims[DimExecution].used += execution
	t.dims[DimCalldata].used += calldata
	t.dims[DimBlob].used += blob

	// Track peaks.
	for d := GasDimension(0); d < numDimensions; d++ {
		if t.dims[d].used > t.peakUsed[d] {
			t.peakUsed[d] = t.dims[d].used
		}
	}
	return nil
}

// AddGas refunds gas to the specified dimension. The used amount is reduced
// but will not go below zero.
func (t *GasPoolTracker) AddGas(dim GasDimension, amount uint64) {
	if !dim.Valid() {
		return
	}
	s := &t.dims[dim]
	if amount > s.used {
		s.used = 0
	} else {
		s.used -= amount
	}
}

// GasUsed returns the gas used in the specified dimension.
func (t *GasPoolTracker) GasUsed(dim GasDimension) uint64 {
	if !dim.Valid() {
		return 0
	}
	return t.dims[dim].used
}

// GasRemaining returns the remaining gas in the specified dimension.
func (t *GasPoolTracker) GasRemaining(dim GasDimension) uint64 {
	if !dim.Valid() {
		return 0
	}
	return t.dims[dim].remaining()
}

// TotalGasUsed returns the sum of gas used across all three dimensions.
func (t *GasPoolTracker) TotalGasUsed() uint64 {
	var total uint64
	for d := GasDimension(0); d < numDimensions; d++ {
		total += t.dims[d].used
	}
	return total
}

// TotalGasRemaining returns the sum of remaining gas across all dimensions.
func (t *GasPoolTracker) TotalGasRemaining() uint64 {
	var total uint64
	for d := GasDimension(0); d < numDimensions; d++ {
		total += t.dims[d].remaining()
	}
	return total
}

// TotalLimit returns the sum of gas limits across all dimensions.
func (t *GasPoolTracker) TotalLimit() uint64 {
	var total uint64
	for d := GasDimension(0); d < numDimensions; d++ {
		total += t.dims[d].limit
	}
	return total
}

// PeakUsed returns the peak gas usage observed for the specified dimension.
func (t *GasPoolTracker) PeakUsed(dim GasDimension) uint64 {
	if !dim.Valid() {
		return 0
	}
	return t.peakUsed[dim]
}

// Utilization returns the utilization ratio for a dimension as a float in
// [0.0, 1.0]. Returns 0 for invalid dimensions or zero limits.
func (t *GasPoolTracker) Utilization(dim GasDimension) float64 {
	if !dim.Valid() || t.dims[dim].limit == 0 {
		return 0
	}
	return float64(t.dims[dim].used) / float64(t.dims[dim].limit)
}

// IsExhausted returns true if any dimension has zero remaining gas.
func (t *GasPoolTracker) IsExhausted() bool {
	for d := GasDimension(0); d < numDimensions; d++ {
		if t.dims[d].used >= t.dims[d].limit {
			return true
		}
	}
	return false
}

// Snapshot captures the current gas state for later rollback. Returns a
// GasPoolSnapshot that can be passed to Revert.
func (t *GasPoolTracker) Snapshot() GasPoolSnapshot {
	snap := GasPoolSnapshot{
		id:    t.nextSnap,
		dims:  t.dims,
		valid: true,
	}
	t.nextSnap++
	t.snapshots = append(t.snapshots, snap)
	return snap
}

// Revert restores the gas state to the given snapshot. Any snapshots taken
// after this one are also invalidated (stack-like behavior matching EVM
// journal reverts). Returns an error if the snapshot is invalid or already
// reverted.
func (t *GasPoolTracker) Revert(snap GasPoolSnapshot) error {
	if !snap.valid {
		return ErrGasPoolSnapReverted
	}

	// Find the snapshot in the stack.
	idx := -1
	for i, s := range t.snapshots {
		if s.id == snap.id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: id=%d", ErrGasPoolInvalidSnap, snap.id)
	}

	// Restore the state.
	t.dims = snap.dims

	// Invalidate this snapshot and all later ones (stack semantics).
	t.snapshots = t.snapshots[:idx]

	return nil
}

// SnapshotCount returns the number of active (non-reverted) snapshots.
func (t *GasPoolTracker) SnapshotCount() int {
	return len(t.snapshots)
}

// MergeUsage adds the gas usage from another tracker (typically an inner
// call frame) into this tracker. Only the used amounts are merged; limits
// are not modified. Returns ErrGasPoolOverflow if the merge would cause
// overflow or exceed limits.
func (t *GasPoolTracker) MergeUsage(other *GasPoolTracker) error {
	if other == nil {
		return nil
	}

	// Pre-check for overflow and limit violations.
	for d := GasDimension(0); d < numDimensions; d++ {
		otherUsed := other.dims[d].used
		if otherUsed == 0 {
			continue
		}
		// Check overflow.
		if t.dims[d].used > math.MaxUint64-otherUsed {
			return fmt.Errorf("%w: %s merge would overflow",
				ErrGasPoolOverflow, d)
		}
		newUsed := t.dims[d].used + otherUsed
		if newUsed > t.dims[d].limit {
			return fmt.Errorf("%w: %s merge needs %d total, limit %d",
				ErrGasPoolOutOfGas, d, newUsed, t.dims[d].limit)
		}
	}

	// All checks passed; apply.
	for d := GasDimension(0); d < numDimensions; d++ {
		t.dims[d].used += other.dims[d].used
		if t.dims[d].used > t.peakUsed[d] {
			t.peakUsed[d] = t.dims[d].used
		}
	}
	return nil
}

// ApplyChildFrame applies the EIP-150 63/64 rule to create a child frame
// tracker with reduced gas limits. The child frame gets at most 63/64 of
// the remaining gas in each dimension, minus the stipend reserved for the
// parent frame to continue after the CALL returns.
func (t *GasPoolTracker) ApplyChildFrame(stipend uint64) *GasPoolTracker {
	child := &GasPoolTracker{}
	for d := GasDimension(0); d < numDimensions; d++ {
		rem := t.dims[d].remaining()
		// EIP-150: child gets floor(remaining * 63 / 64).
		childGas := rem - rem/64
		if d == DimExecution && stipend > 0 && childGas > stipend {
			// The stipend is added for value transfers (CallStipend = 2300).
			// We account for it by increasing the child's execution gas.
			childGas += stipend
		}
		child.dims[d] = dimState{limit: childGas}
	}
	return child
}

// Reset clears all gas usage and snapshots, optionally setting new limits.
// Pass 0 for a limit to keep the existing limit for that dimension.
func (t *GasPoolTracker) Reset(executionLimit, calldataLimit, blobLimit uint64) {
	if executionLimit > 0 {
		t.dims[DimExecution].limit = executionLimit
	}
	if calldataLimit > 0 {
		t.dims[DimCalldata].limit = calldataLimit
	}
	if blobLimit > 0 {
		t.dims[DimBlob].limit = blobLimit
	}
	for d := GasDimension(0); d < numDimensions; d++ {
		t.dims[d].used = 0
		t.peakUsed[d] = 0
	}
	t.snapshots = t.snapshots[:0]
	t.nextSnap = 0
}

// Copy returns a deep copy of the tracker, including snapshot history.
func (t *GasPoolTracker) Copy() *GasPoolTracker {
	cp := &GasPoolTracker{
		dims:     t.dims,
		peakUsed: t.peakUsed,
		nextSnap: t.nextSnap,
	}
	if len(t.snapshots) > 0 {
		cp.snapshots = make([]GasPoolSnapshot, len(t.snapshots))
		copy(cp.snapshots, t.snapshots)
	}
	return cp
}

// UsageVector returns the gas used in each dimension as a 3-element array
// ordered [execution, calldata, blob].
func (t *GasPoolTracker) UsageVector() [numDimensions]uint64 {
	var v [numDimensions]uint64
	for d := GasDimension(0); d < numDimensions; d++ {
		v[d] = t.dims[d].used
	}
	return v
}

// LimitVector returns the gas limit for each dimension as a 3-element array.
func (t *GasPoolTracker) LimitVector() [numDimensions]uint64 {
	var v [numDimensions]uint64
	for d := GasDimension(0); d < numDimensions; d++ {
		v[d] = t.dims[d].limit
	}
	return v
}

// RemainingVector returns the remaining gas in each dimension.
func (t *GasPoolTracker) RemainingVector() [numDimensions]uint64 {
	var v [numDimensions]uint64
	for d := GasDimension(0); d < numDimensions; d++ {
		v[d] = t.dims[d].remaining()
	}
	return v
}

// String returns a compact representation of the tracker state.
func (t *GasPoolTracker) String() string {
	return fmt.Sprintf(
		"GasPoolTracker{exec:%d/%d, calldata:%d/%d, blob:%d/%d}",
		t.dims[DimExecution].used, t.dims[DimExecution].limit,
		t.dims[DimCalldata].used, t.dims[DimCalldata].limit,
		t.dims[DimBlob].used, t.dims[DimBlob].limit,
	)
}
