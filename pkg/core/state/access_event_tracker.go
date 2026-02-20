// access_event_tracker.go provides a higher-level address/storage/code access
// tracker for EIP-4762 statelessness gas accounting. It wraps the lower-level
// AccessEvents with convenience methods for tracking cold/warm transitions,
// branch-level access counting, and gas charge accumulation.
package state

import (
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Gas cost constants for the access event tracker. These mirror EIP-2929/4762
// gas costs and are used by the tracker to compute gas charges.
const (
	TrackerWarmStorageReadCost    uint64 = 100
	TrackerColdAccountAccessCost uint64 = 2600
	TrackerColdSloadCost         uint64 = 2100
	TrackerColdCodeAccessCost    uint64 = 2600
)

// AccessEventTracker tracks address, storage slot, and code accesses for
// EIP-4762 statelessness gas accounting. It maintains warm/cold state for
// each unique access target and computes appropriate gas charges.
type AccessEventTracker struct {
	mu             sync.Mutex
	addresses      map[types.Address]*addressAccessState
	witness        *AccessWitness
	branchCounters map[branchCounterKey]*BranchAccessCounter
}

// addressAccessState tracks the warm/cold state for a single address
// and its associated storage and code accesses.
type addressAccessState struct {
	readWarm  bool
	writeWarm bool
	slots     map[types.Hash]*slotAccessState
	codeWarm  bool
}

// slotAccessState tracks warm/cold state for a storage slot.
type slotAccessState struct {
	readWarm  bool
	writeWarm bool
}

// branchCounterKey identifies a Verkle tree branch for access counting.
type branchCounterKey struct {
	addr      types.Address
	treeIndex uint64
}

// BranchAccessCounter counts per-branch accesses for Verkle tree gas
// calculation. Each unique branch in the tree contributes to the witness
// size when accessed for the first time in a transaction.
type BranchAccessCounter struct {
	ReadCount  uint64
	WriteCount uint64
	Warm       bool
}

// AccessWitness accumulates gas charges for state accesses within a
// transaction. It tracks total gas consumed and the number of unique
// cold and warm accesses.
type AccessWitness struct {
	TotalGas    uint64
	ColdReads   uint64
	WarmReads   uint64
	ColdWrites  uint64
	WarmWrites  uint64
}

// NewAccessEventTracker creates a new tracker with empty state.
func NewAccessEventTracker() *AccessEventTracker {
	return &AccessEventTracker{
		addresses:      make(map[types.Address]*addressAccessState),
		witness:        &AccessWitness{},
		branchCounters: make(map[branchCounterKey]*BranchAccessCounter),
	}
}

// getOrCreateAddrState returns the access state for an address, creating
// it if it does not yet exist.
func (t *AccessEventTracker) getOrCreateAddrState(addr types.Address) *addressAccessState {
	s, ok := t.addresses[addr]
	if !ok {
		s = &addressAccessState{
			slots: make(map[types.Hash]*slotAccessState),
		}
		t.addresses[addr] = s
	}
	return s
}

// TouchAddress records an address access and returns the gas cost.
// The first access is cold (2600 gas); subsequent accesses are warm (100 gas).
// If isWrite is true, the write flag is also set.
func (t *AccessEventTracker) TouchAddress(addr types.Address, isWrite bool) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.getOrCreateAddrState(addr)
	var gas uint64

	if !s.readWarm {
		// Cold access.
		gas = TrackerColdAccountAccessCost
		s.readWarm = true
		t.witness.ColdReads++
	} else {
		gas = TrackerWarmStorageReadCost
		t.witness.WarmReads++
	}

	if isWrite && !s.writeWarm {
		s.writeWarm = true
		t.witness.ColdWrites++
	} else if isWrite {
		t.witness.WarmWrites++
	}

	t.witness.TotalGas += gas
	return gas
}

// TouchStorageSlot records a storage slot access and returns the gas cost.
// Cold slot access costs 2100 gas; warm costs 100 gas.
func (t *AccessEventTracker) TouchStorageSlot(addr types.Address, slot types.Hash, isWrite bool) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	addrState := t.getOrCreateAddrState(addr)
	ss, ok := addrState.slots[slot]
	if !ok {
		ss = &slotAccessState{}
		addrState.slots[slot] = ss
	}

	var gas uint64
	if !ss.readWarm {
		gas = TrackerColdSloadCost
		ss.readWarm = true
		t.witness.ColdReads++
	} else {
		gas = TrackerWarmStorageReadCost
		t.witness.WarmReads++
	}

	if isWrite && !ss.writeWarm {
		ss.writeWarm = true
		t.witness.ColdWrites++
	} else if isWrite {
		t.witness.WarmWrites++
	}

	t.witness.TotalGas += gas
	return gas
}

// TouchCode records a code access for an address and returns the gas cost.
// Cold code access costs 2600 gas; warm costs 100 gas.
func (t *AccessEventTracker) TouchCode(addr types.Address, isWrite bool) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.getOrCreateAddrState(addr)
	var gas uint64

	if !s.codeWarm {
		gas = TrackerColdCodeAccessCost
		s.codeWarm = true
		t.witness.ColdReads++
	} else {
		gas = TrackerWarmStorageReadCost
		t.witness.WarmReads++
	}

	if isWrite {
		t.witness.ColdWrites++
	}

	t.witness.TotalGas += gas
	return gas
}

// IsAddressWarm reports whether the given address has already been accessed.
func (t *AccessEventTracker) IsAddressWarm(addr types.Address) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.addresses[addr]
	return ok && s.readWarm
}

// IsSlotWarm reports whether the given storage slot has already been accessed.
func (t *AccessEventTracker) IsSlotWarm(addr types.Address, slot types.Hash) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.addresses[addr]
	if !ok {
		return false
	}
	ss, ok := s.slots[slot]
	return ok && ss.readWarm
}

// IsCodeWarm reports whether the code for the given address has been accessed.
func (t *AccessEventTracker) IsCodeWarm(addr types.Address) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.addresses[addr]
	return ok && s.codeWarm
}

// Witness returns the current accumulated witness gas charges.
func (t *AccessEventTracker) Witness() AccessWitness {
	t.mu.Lock()
	defer t.mu.Unlock()
	return *t.witness
}

// TouchBranch records a branch-level access for Verkle tree gas counting.
// Returns the gas cost: cold branch costs WitnessBranchReadCost, warm is free.
func (t *AccessEventTracker) TouchBranch(addr types.Address, treeIndex uint64, isWrite bool) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := branchCounterKey{addr: addr, treeIndex: treeIndex}
	bc, ok := t.branchCounters[key]
	if !ok {
		bc = &BranchAccessCounter{}
		t.branchCounters[key] = bc
	}

	var gas uint64
	if !bc.Warm {
		gas = WitnessBranchReadCost
		bc.Warm = true
	}

	if isWrite {
		bc.WriteCount++
	} else {
		bc.ReadCount++
	}

	t.witness.TotalGas += gas
	return gas
}

// GetBranchCounter returns the access counter for a specific branch.
// Returns nil if the branch has not been accessed.
func (t *AccessEventTracker) GetBranchCounter(addr types.Address, treeIndex uint64) *BranchAccessCounter {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := branchCounterKey{addr: addr, treeIndex: treeIndex}
	return t.branchCounters[key]
}

// Reset clears all tracked state, returning the tracker to its initial state.
func (t *AccessEventTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.addresses = make(map[types.Address]*addressAccessState)
	t.witness = &AccessWitness{}
	t.branchCounters = make(map[branchCounterKey]*BranchAccessCounter)
}

// AddressCount returns the number of unique addresses that have been accessed.
func (t *AccessEventTracker) AddressCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.addresses)
}

// SlotCount returns the total number of unique storage slots accessed
// across all addresses.
func (t *AccessEventTracker) SlotCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	count := 0
	for _, s := range t.addresses {
		count += len(s.slots)
	}
	return count
}

// BranchCount returns the number of unique branches accessed.
func (t *AccessEventTracker) BranchCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.branchCounters)
}
