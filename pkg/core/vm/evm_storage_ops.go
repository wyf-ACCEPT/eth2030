// evm_storage_ops.go implements storage operation logic for the EVM:
// SLOAD with EIP-2929 cold/warm access costs, SSTORE with EIP-2200 net gas
// metering and refund calculation, TLOAD/TSTORE transient storage (EIP-1153),
// and integrated access list warmth tracking.
package vm

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// Storage operation errors.
var (
	ErrStorageReadOnly       = errors.New("storage: write in read-only context")
	ErrStorageNoState        = errors.New("storage: no state database available")
	ErrStorageSlotGasExhaust = errors.New("storage: insufficient gas for slot access")
)

// SlotAccessCosts holds the gas costs for storage slot access per EIP-2929.
type SlotAccessCosts struct {
	ColdSloadGas        uint64 // first access to a slot in a tx: 2100
	WarmStorageReadGas  uint64 // subsequent accesses: 100
	SstoreSetGas        uint64 // zero -> non-zero: 20000
	SstoreResetGas      uint64 // non-zero -> non-zero: 2900
	SstoreClearsRefund  uint64 // refund for clearing: 4800
}

// DefaultSlotAccessCosts returns post-London/Cancun slot access costs.
func DefaultSlotAccessCosts() SlotAccessCosts {
	return SlotAccessCosts{
		ColdSloadGas:       ColdSloadCost,                // 2100
		WarmStorageReadGas: WarmStorageReadCost,           // 100
		SstoreSetGas:       GasSstoreSet,                  // 20000
		SstoreResetGas:     GasSstoreReset,                // 2900
		SstoreClearsRefund: SstoreClearsScheduleRefund,    // 4800
	}
}

// StorageOpHandler executes storage operations with integrated access list
// tracking and gas metering. It provides a higher-level interface than the
// raw opcode implementations, suitable for testing and standalone execution.
type StorageOpHandler struct {
	costs   SlotAccessCosts
	tracker *AccessListTracker
}

// NewStorageOpHandler creates a handler with default costs and a fresh tracker.
func NewStorageOpHandler() *StorageOpHandler {
	return &StorageOpHandler{
		costs:   DefaultSlotAccessCosts(),
		tracker: NewAccessListTracker(),
	}
}

// NewStorageOpHandlerWithTracker creates a handler with a pre-existing tracker.
func NewStorageOpHandlerWithTracker(tracker *AccessListTracker) *StorageOpHandler {
	return &StorageOpHandler{
		costs:   DefaultSlotAccessCosts(),
		tracker: tracker,
	}
}

// Tracker returns the underlying AccessListTracker for inspection.
func (h *StorageOpHandler) Tracker() *AccessListTracker {
	return h.tracker
}

// SloadGas computes the gas cost for an SLOAD operation. It warms the slot
// if cold and returns the total gas cost (warm read + cold surcharge).
//
// Per EIP-2929:
//   - Warm slot: WarmStorageReadGas (100)
//   - Cold slot: ColdSloadGas (2100) and the slot becomes warm
func (h *StorageOpHandler) SloadGas(addr types.Address, slot types.Hash) uint64 {
	_, slotWarm := h.tracker.TouchSlot(addr, slot)
	if slotWarm {
		return h.costs.WarmStorageReadGas
	}
	return h.costs.ColdSloadGas
}

// SstoreGasCost computes the gas cost and refund delta for an SSTORE operation.
// It implements the EIP-2200 net gas metering schedule with EIP-3529 reduced
// refunds.
//
// Parameters:
//   - addr: contract address
//   - slot: storage slot key
//   - current: current value at slot (in-memory state)
//   - original: value at start of transaction (committed state)
//   - newVal: value being written
//
// Returns (gasCost, refundDelta).
// refundDelta is positive when refund should be added, negative when subtracted.
func (h *StorageOpHandler) SstoreGasCost(
	addr types.Address,
	slot types.Hash,
	current, original, newVal [32]byte,
) (uint64, int64) {
	// Determine cold/warm status and apply cold surcharge.
	_, slotWarm := h.tracker.TouchSlot(addr, slot)
	var coldCost uint64
	if !slotWarm {
		coldCost = h.costs.ColdSloadGas
	}

	// No-op case: writing the same value that already exists.
	if current == newVal {
		return h.costs.WarmStorageReadGas + coldCost, 0
	}

	var gas uint64
	var refund int64

	if original == current {
		// Clean slot: the in-memory value matches the committed value.
		if isAllZero(original) {
			// Creating a new slot: 0 -> non-zero.
			gas = h.costs.SstoreSetGas
		} else {
			// Updating an existing slot: non-zero -> different non-zero.
			gas = h.costs.SstoreResetGas
			if isAllZero(newVal) {
				// Clearing: non-zero -> zero. Add refund.
				refund = int64(h.costs.SstoreClearsRefund)
			}
		}
		return gas + coldCost, refund
	}

	// Dirty slot: original != current (slot was already modified in this tx).
	gas = h.costs.WarmStorageReadGas

	// Refund adjustments for dirty slots per EIP-2200/3529.
	if !isAllZero(original) {
		if isAllZero(current) && !isAllZero(newVal) {
			// Undoing a previous clear: subtract the refund that was given.
			refund -= int64(h.costs.SstoreClearsRefund)
		} else if !isAllZero(current) && isAllZero(newVal) {
			// Clearing a dirty non-zero slot: add refund.
			refund += int64(h.costs.SstoreClearsRefund)
		}
	}
	if original == newVal {
		// Restoring the slot to its original value.
		if isAllZero(original) {
			// Was 0, set to X, now back to 0.
			if h.costs.SstoreSetGas > h.costs.WarmStorageReadGas {
				refund += int64(h.costs.SstoreSetGas - h.costs.WarmStorageReadGas)
			}
		} else {
			// Was X, changed to Y, now back to X.
			if h.costs.SstoreResetGas > h.costs.WarmStorageReadGas {
				refund += int64(h.costs.SstoreResetGas - h.costs.WarmStorageReadGas)
			}
		}
	}

	return gas + coldCost, refund
}

// ExecSload performs a full SLOAD: computes gas, reads the value from state.
// Returns the loaded value and the gas cost.
func (h *StorageOpHandler) ExecSload(
	stateDB StateDB,
	addr types.Address,
	slot types.Hash,
) (types.Hash, uint64, error) {
	if stateDB == nil {
		return types.Hash{}, 0, ErrStorageNoState
	}
	gas := h.SloadGas(addr, slot)
	val := stateDB.GetState(addr, slot)
	return val, gas, nil
}

// ExecSstore performs a full SSTORE: computes gas/refund, writes the value.
// Returns (gasCost, refundDelta, error).
func (h *StorageOpHandler) ExecSstore(
	stateDB StateDB,
	addr types.Address,
	slot types.Hash,
	newVal types.Hash,
	readOnly bool,
) (uint64, int64, error) {
	if readOnly {
		return 0, 0, ErrStorageReadOnly
	}
	if stateDB == nil {
		return 0, 0, ErrStorageNoState
	}

	current := stateDB.GetState(addr, slot)
	original := stateDB.GetCommittedState(addr, slot)

	var currentBytes, originalBytes, newBytes [32]byte
	copy(currentBytes[:], current[:])
	copy(originalBytes[:], original[:])
	copy(newBytes[:], newVal[:])

	gas, refund := h.SstoreGasCost(addr, slot, currentBytes, originalBytes, newBytes)

	// Apply refund to state.
	if refund > 0 {
		stateDB.AddRefund(uint64(refund))
	} else if refund < 0 {
		stateDB.SubRefund(uint64(-refund))
	}

	// Write the new value.
	stateDB.SetState(addr, slot, newVal)
	return gas, refund, nil
}

// TransientStorageHandler provides TLOAD/TSTORE operations (EIP-1153).
// Transient storage is automatically cleared at the end of each transaction.
type TransientStorageHandler struct {
	gasTload  uint64
	gasTstore uint64
}

// NewTransientStorageHandler creates a handler with standard gas costs.
func NewTransientStorageHandler() *TransientStorageHandler {
	return &TransientStorageHandler{
		gasTload:  GasTload,  // 100
		gasTstore: GasTstore, // 100
	}
}

// ExecTload reads a value from transient storage. Returns value and gas cost.
func (th *TransientStorageHandler) ExecTload(
	stateDB StateDB,
	addr types.Address,
	slot types.Hash,
) (types.Hash, uint64, error) {
	if stateDB == nil {
		return types.Hash{}, 0, ErrStorageNoState
	}
	val := stateDB.GetTransientState(addr, slot)
	return val, th.gasTload, nil
}

// ExecTstore writes a value to transient storage. Returns gas cost.
func (th *TransientStorageHandler) ExecTstore(
	stateDB StateDB,
	addr types.Address,
	slot types.Hash,
	value types.Hash,
	readOnly bool,
) (uint64, error) {
	if readOnly {
		return 0, ErrStorageReadOnly
	}
	if stateDB == nil {
		return 0, ErrStorageNoState
	}
	stateDB.SetTransientState(addr, slot, value)
	return th.gasTstore, nil
}

// SlotWarmthTracker provides per-slot warmth query and tracking utilities.
// It wraps an AccessListTracker to provide specialized storage operations.
type SlotWarmthTracker struct {
	tracker *AccessListTracker
}

// NewSlotWarmthTracker creates a tracker wrapping the given access list.
func NewSlotWarmthTracker(tracker *AccessListTracker) *SlotWarmthTracker {
	return &SlotWarmthTracker{tracker: tracker}
}

// IsSlotWarm returns true if the given storage slot has been previously accessed
// in this transaction.
func (sw *SlotWarmthTracker) IsSlotWarm(addr types.Address, slot types.Hash) bool {
	_, slotWarm := sw.tracker.ContainsSlot(addr, slot)
	return slotWarm
}

// IsAddressWarm returns true if the given address has been previously accessed.
func (sw *SlotWarmthTracker) IsAddressWarm(addr types.Address) bool {
	return sw.tracker.ContainsAddress(addr)
}

// WarmSlot warms a storage slot and returns whether it was already warm.
func (sw *SlotWarmthTracker) WarmSlot(addr types.Address, slot types.Hash) bool {
	_, slotWarm := sw.tracker.TouchSlot(addr, slot)
	return slotWarm
}

// WarmAddress warms an address and returns whether it was already warm.
func (sw *SlotWarmthTracker) WarmAddress(addr types.Address) bool {
	return sw.tracker.TouchAddress(addr)
}

// PreWarmAccessList loads an EIP-2930 access list into the warmth tracker.
// This pre-pays for cold accesses at the start of the transaction.
func (sw *SlotWarmthTracker) PreWarmAccessList(
	sender types.Address,
	to *types.Address,
	accessList types.AccessList,
) {
	sw.tracker.PrePopulate(sender, to, accessList)
}

// SstoreRefundExplainer breaks down the refund calculation for an SSTORE
// operation, useful for debugging and analysis.
type SstoreRefundExplainer struct {
	costs SlotAccessCosts
}

// NewSstoreRefundExplainer creates an explainer with default costs.
func NewSstoreRefundExplainer() *SstoreRefundExplainer {
	return &SstoreRefundExplainer{costs: DefaultSlotAccessCosts()}
}

// SstoreRefundBreakdown describes the gas and refund components of an SSTORE.
type SstoreRefundBreakdown struct {
	IsCold       bool   // whether this was a cold access
	ColdCost     uint64 // cold surcharge (0 or ColdSloadGas)
	BaseCost     uint64 // base gas cost (set/reset/warm read)
	TotalGas     uint64 // total gas consumed
	RefundDelta  int64  // net refund change
	Category     string // description of the SSTORE category
}

// Explain returns a detailed breakdown of the SSTORE gas and refund.
func (e *SstoreRefundExplainer) Explain(
	cold bool,
	current, original, newVal [32]byte,
) SstoreRefundBreakdown {
	bd := SstoreRefundBreakdown{IsCold: cold}
	if cold {
		bd.ColdCost = e.costs.ColdSloadGas
	}

	if current == newVal {
		bd.BaseCost = e.costs.WarmStorageReadGas
		bd.Category = "no-op (current == new)"
		bd.TotalGas = bd.BaseCost + bd.ColdCost
		return bd
	}

	if original == current {
		if isAllZero(original) {
			bd.BaseCost = e.costs.SstoreSetGas
			bd.Category = "create (0 -> non-zero)"
		} else if isAllZero(newVal) {
			bd.BaseCost = e.costs.SstoreResetGas
			bd.RefundDelta = int64(e.costs.SstoreClearsRefund)
			bd.Category = "delete (non-zero -> 0)"
		} else {
			bd.BaseCost = e.costs.SstoreResetGas
			bd.Category = "update (non-zero -> different non-zero)"
		}
		bd.TotalGas = bd.BaseCost + bd.ColdCost
		return bd
	}

	// Dirty slot.
	bd.BaseCost = e.costs.WarmStorageReadGas
	bd.Category = fmt.Sprintf("dirty slot (original != current)")

	if !isAllZero(original) {
		if isAllZero(current) && !isAllZero(newVal) {
			bd.RefundDelta -= int64(e.costs.SstoreClearsRefund)
		} else if !isAllZero(current) && isAllZero(newVal) {
			bd.RefundDelta += int64(e.costs.SstoreClearsRefund)
		}
	}
	if original == newVal {
		if isAllZero(original) {
			if e.costs.SstoreSetGas > e.costs.WarmStorageReadGas {
				bd.RefundDelta += int64(e.costs.SstoreSetGas - e.costs.WarmStorageReadGas)
			}
		} else {
			if e.costs.SstoreResetGas > e.costs.WarmStorageReadGas {
				bd.RefundDelta += int64(e.costs.SstoreResetGas - e.costs.WarmStorageReadGas)
			}
		}
		bd.Category = "restore to original"
	}

	bd.TotalGas = bd.BaseCost + bd.ColdCost
	return bd
}

// isAllZero returns true if all 32 bytes are zero.
func isAllZero(val [32]byte) bool {
	for _, b := range val {
		if b != 0 {
			return false
		}
	}
	return true
}
