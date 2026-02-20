package core

import (
	"errors"
	"fmt"
	"sync"
)

// Multi-dimensional gas pool with sub-pools for execution, blob, and calldata
// gas per EIP-7706. Supports gas reservation, release, and atomic operations.

// Sub-pool errors.
var (
	ErrBlobGasPoolExhausted       = errors.New("blob gas pool exhausted")
	ErrExecutionGasPoolExhausted  = errors.New("execution gas pool exhausted")
	ErrGasReservationNotFound     = errors.New("gas reservation not found")
	ErrGasReservationAlreadyExists = errors.New("gas reservation already exists")
)

// PoolGasDim identifies a dimension in multi-dimensional gas pool accounting.
type PoolGasDim int

const (
	PoolDimExecution PoolGasDim = iota
	PoolDimCalldata
	PoolDimBlob
)

// String returns the name of the pool gas dimension.
func (d PoolGasDim) String() string {
	switch d {
	case PoolDimExecution:
		return "execution"
	case PoolDimCalldata:
		return "calldata"
	case PoolDimBlob:
		return "blob"
	default:
		return "unknown"
	}
}

// GasReservation tracks gas reserved by a specific transaction before execution.
// Reservations ensure that the gas pool accounting is consistent even when
// transactions are processed out of order or speculatively.
type GasReservation struct {
	ID        uint64 // unique reservation identifier
	Execution uint64
	Calldata  uint64
	Blob      uint64
}

// Total returns the total gas reserved across all dimensions.
func (r *GasReservation) Total() uint64 {
	return r.Execution + r.Calldata + r.Blob
}

// MultiGasPool tracks gas availability across three dimensions (execution,
// calldata, blob) during block processing. It supports atomic sub/add operations,
// named reservations for speculative execution, and usage tracking.
type MultiGasPool struct {
	mu sync.Mutex

	// Per-dimension capacity and usage.
	executionCap  uint64
	calldataCap   uint64
	blobCap       uint64
	executionUsed uint64
	calldataUsed  uint64
	blobUsed      uint64

	// Reservations map: reservation ID -> reservation.
	reservations map[uint64]*GasReservation
	nextReservID uint64

	// Peak usage tracking for analytics.
	peakExecution uint64
	peakCalldata  uint64
	peakBlob      uint64
}

// NewMultiGasPool creates a multi-dimensional gas pool with the specified
// capacity for each dimension.
func NewMultiGasPool(executionGas, calldataGas, blobGas uint64) *MultiGasPool {
	return &MultiGasPool{
		executionCap: executionGas,
		calldataCap:  calldataGas,
		blobCap:      blobGas,
		reservations: make(map[uint64]*GasReservation),
	}
}

// NewMultiGasPoolFromDimensions creates a multi-dimensional gas pool from a
// GasDimensions struct.
func NewMultiGasPoolFromDimensions(dims GasDimensions) *MultiGasPool {
	return NewMultiGasPool(dims.Compute, dims.Calldata, dims.Blob)
}

// SubGasMulti atomically subtracts gas across all three dimensions.
// If any dimension has insufficient gas, no deduction occurs (all-or-nothing).
func (p *MultiGasPool) SubGasMulti(execution, calldata, blob uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.subGasLocked(execution, calldata, blob)
}

// subGasLocked performs the subtraction under an existing lock.
func (p *MultiGasPool) subGasLocked(execution, calldata, blob uint64) error {
	execAvail := p.executionCap - p.executionUsed
	if execution > execAvail {
		return fmt.Errorf("%w: need %d, have %d", ErrExecutionGasPoolExhausted, execution, execAvail)
	}
	cdAvail := p.calldataCap - p.calldataUsed
	if calldata > cdAvail {
		return fmt.Errorf("%w: need %d, have %d", ErrCalldataGasPoolExhausted, calldata, cdAvail)
	}
	blobAvail := p.blobCap - p.blobUsed
	if blob > blobAvail {
		return fmt.Errorf("%w: need %d, have %d", ErrBlobGasPoolExhausted, blob, blobAvail)
	}

	p.executionUsed += execution
	p.calldataUsed += calldata
	p.blobUsed += blob

	// Track peak usage.
	if p.executionUsed > p.peakExecution {
		p.peakExecution = p.executionUsed
	}
	if p.calldataUsed > p.peakCalldata {
		p.peakCalldata = p.calldataUsed
	}
	if p.blobUsed > p.peakBlob {
		p.peakBlob = p.blobUsed
	}
	return nil
}

// AddGasMulti returns gas to the pool across all three dimensions.
// This is used for gas refunds after transaction execution.
func (p *MultiGasPool) AddGasMulti(execution, calldata, blob uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if execution > p.executionUsed {
		p.executionUsed = 0
	} else {
		p.executionUsed -= execution
	}
	if calldata > p.calldataUsed {
		p.calldataUsed = 0
	} else {
		p.calldataUsed -= calldata
	}
	if blob > p.blobUsed {
		p.blobUsed = 0
	} else {
		p.blobUsed -= blob
	}
}

// Reserve creates a named reservation for the specified gas amounts across
// all dimensions. The gas is deducted from the available pool immediately.
// The reservation can later be committed (finalized) or released (returned).
func (p *MultiGasPool) Reserve(execution, calldata, blob uint64) (uint64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.subGasLocked(execution, calldata, blob); err != nil {
		return 0, fmt.Errorf("reservation failed: %w", err)
	}

	id := p.nextReservID
	p.nextReservID++
	p.reservations[id] = &GasReservation{
		ID:        id,
		Execution: execution,
		Calldata:  calldata,
		Blob:      blob,
	}
	return id, nil
}

// Commit finalizes a reservation, removing it from tracking. The gas remains
// consumed; this confirms that the reserved gas was actually used.
func (p *MultiGasPool) Commit(reservationID uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.reservations[reservationID]; !ok {
		return fmt.Errorf("%w: id=%d", ErrGasReservationNotFound, reservationID)
	}
	delete(p.reservations, reservationID)
	return nil
}

// Release cancels a reservation and returns the reserved gas to the pool.
func (p *MultiGasPool) Release(reservationID uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	r, ok := p.reservations[reservationID]
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrGasReservationNotFound, reservationID)
	}

	// Return gas to pool.
	if r.Execution > p.executionUsed {
		p.executionUsed = 0
	} else {
		p.executionUsed -= r.Execution
	}
	if r.Calldata > p.calldataUsed {
		p.calldataUsed = 0
	} else {
		p.calldataUsed -= r.Calldata
	}
	if r.Blob > p.blobUsed {
		p.blobUsed = 0
	} else {
		p.blobUsed -= r.Blob
	}
	delete(p.reservations, reservationID)
	return nil
}

// PartialCommit finalizes a reservation but only consumes the actual amounts
// used. Any difference between the reserved and actual amounts is returned.
func (p *MultiGasPool) PartialCommit(reservationID uint64, actualExec, actualCD, actualBlob uint64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	r, ok := p.reservations[reservationID]
	if !ok {
		return fmt.Errorf("%w: id=%d", ErrGasReservationNotFound, reservationID)
	}

	// Return any unused gas.
	if r.Execution > actualExec {
		refund := r.Execution - actualExec
		if refund > p.executionUsed {
			p.executionUsed = 0
		} else {
			p.executionUsed -= refund
		}
	}
	if r.Calldata > actualCD {
		refund := r.Calldata - actualCD
		if refund > p.calldataUsed {
			p.calldataUsed = 0
		} else {
			p.calldataUsed -= refund
		}
	}
	if r.Blob > actualBlob {
		refund := r.Blob - actualBlob
		if refund > p.blobUsed {
			p.blobUsed = 0
		} else {
			p.blobUsed -= refund
		}
	}

	delete(p.reservations, reservationID)
	return nil
}

// Available returns the remaining gas in each dimension.
func (p *MultiGasPool) Available() (execution, calldata, blob uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executionCap - p.executionUsed,
		p.calldataCap - p.calldataUsed,
		p.blobCap - p.blobUsed
}

// Used returns the consumed gas in each dimension.
func (p *MultiGasPool) Used() (execution, calldata, blob uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executionUsed, p.calldataUsed, p.blobUsed
}

// Capacity returns the total capacity of each dimension.
func (p *MultiGasPool) Capacity() (execution, calldata, blob uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executionCap, p.calldataCap, p.blobCap
}

// PeakUsage returns the peak gas usage observed in each dimension.
func (p *MultiGasPool) PeakUsage() (execution, calldata, blob uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peakExecution, p.peakCalldata, p.peakBlob
}

// Utilization returns the utilization ratio for each dimension as a fraction
// in the range [0.0, 1.0].
func (p *MultiGasPool) Utilization() (execution, calldata, blob float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.executionCap > 0 {
		execution = float64(p.executionUsed) / float64(p.executionCap)
	}
	if p.calldataCap > 0 {
		calldata = float64(p.calldataUsed) / float64(p.calldataCap)
	}
	if p.blobCap > 0 {
		blob = float64(p.blobUsed) / float64(p.blobCap)
	}
	return
}

// ReservationCount returns the number of active reservations.
func (p *MultiGasPool) ReservationCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.reservations)
}

// GetReservation returns a copy of a reservation by its ID.
func (p *MultiGasPool) GetReservation(id uint64) (*GasReservation, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.reservations[id]
	if !ok {
		return nil, false
	}
	// Return a copy.
	cp := *r
	return &cp, true
}

// Reset restores the pool to its initial state with the given capacities.
func (p *MultiGasPool) Reset(executionGas, calldataGas, blobGas uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.executionCap = executionGas
	p.calldataCap = calldataGas
	p.blobCap = blobGas
	p.executionUsed = 0
	p.calldataUsed = 0
	p.blobUsed = 0
	p.reservations = make(map[uint64]*GasReservation)
	p.nextReservID = 0
	p.peakExecution = 0
	p.peakCalldata = 0
	p.peakBlob = 0
}
