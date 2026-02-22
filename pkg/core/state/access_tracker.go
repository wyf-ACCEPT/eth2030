// access_tracker.go implements EIP-2929/4762 warm/cold access tracking with
// per-transaction access sets, cross-transaction merging, access list
// preloading from EIP-2930 transactions, and witness charging integration for
// statelessness gas accounting.
//
// The AccessTracker maintains separate per-transaction access sets and can
// merge them into a block-level aggregate. It supports preloading access
// lists from EIP-2930 typed transactions, which warm addresses and storage
// slots before execution begins.
package state

import (
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Gas cost constants for EIP-2929 warm/cold access tracking.
const (
	AccessTrackerColdAccountCost uint64 = 2600
	AccessTrackerColdSloadCost   uint64 = 2100
	AccessTrackerWarmAccessCost  uint64 = 100
)

// AccessEntry represents a single access to an address or storage slot,
// recording whether it was a read, write, or both.
type AccessEntry struct {
	Address types.Address
	Slot    types.Hash
	IsSlot  bool // true if this entry refers to a storage slot
	Read    bool
	Write   bool
}

// TxAccessSet tracks which addresses and storage slots have been accessed
// within a single transaction. It records warm/cold state transitions and
// computes gas charges.
type TxAccessSet struct {
	addresses    map[types.Address]bool                    // true if warm
	slots        map[types.Address]map[types.Hash]bool     // addr -> slot -> warm
	gasCharged   uint64                                    // total gas charged for cold accesses
	coldAccounts int                                       // count of cold account accesses
	coldSlots    int                                       // count of cold slot accesses
	warmAccesses int                                       // count of warm accesses
}

// newTxAccessSet creates a new empty per-transaction access set.
func newTxAccessSet() *TxAccessSet {
	return &TxAccessSet{
		addresses: make(map[types.Address]bool),
		slots:     make(map[types.Address]map[types.Hash]bool),
	}
}

// ContainsAddress checks if the address is warm in this access set.
func (tas *TxAccessSet) ContainsAddress(addr types.Address) bool {
	return tas.addresses[addr]
}

// ContainsSlot checks if the address-slot pair is warm.
func (tas *TxAccessSet) ContainsSlot(addr types.Address, slot types.Hash) (addrWarm, slotWarm bool) {
	addrWarm = tas.addresses[addr]
	if slots, ok := tas.slots[addr]; ok {
		slotWarm = slots[slot]
	}
	return
}

// TouchAddress marks an address as warm and returns the gas cost.
// First access costs 2600 gas; subsequent accesses cost 100 gas.
func (tas *TxAccessSet) TouchAddress(addr types.Address) uint64 {
	if tas.addresses[addr] {
		tas.warmAccesses++
		return AccessTrackerWarmAccessCost
	}
	tas.addresses[addr] = true
	tas.coldAccounts++
	tas.gasCharged += AccessTrackerColdAccountCost
	return AccessTrackerColdAccountCost
}

// TouchSlot marks a storage slot as warm and returns the gas cost.
// First access costs 2100 gas; subsequent accesses cost 100 gas.
func (tas *TxAccessSet) TouchSlot(addr types.Address, slot types.Hash) uint64 {
	// Ensure address is warm.
	if !tas.addresses[addr] {
		tas.addresses[addr] = true
	}
	slots, ok := tas.slots[addr]
	if !ok {
		slots = make(map[types.Hash]bool)
		tas.slots[addr] = slots
	}
	if slots[slot] {
		tas.warmAccesses++
		return AccessTrackerWarmAccessCost
	}
	slots[slot] = true
	tas.coldSlots++
	tas.gasCharged += AccessTrackerColdSloadCost
	return AccessTrackerColdSloadCost
}

// GasCharged returns the total gas charged for cold accesses.
func (tas *TxAccessSet) GasCharged() uint64 {
	return tas.gasCharged
}

// AddressCount returns the number of unique addresses accessed.
func (tas *TxAccessSet) AddressCount() int {
	return len(tas.addresses)
}

// SlotCount returns the total number of unique slots accessed.
func (tas *TxAccessSet) SlotCount() int {
	count := 0
	for _, slots := range tas.slots {
		count += len(slots)
	}
	return count
}

// ColdAccountCount returns the number of cold account accesses.
func (tas *TxAccessSet) ColdAccountCount() int {
	return tas.coldAccounts
}

// ColdSlotCount returns the number of cold slot accesses.
func (tas *TxAccessSet) ColdSlotCount() int {
	return tas.coldSlots
}

// WarmAccessCount returns the number of warm accesses.
func (tas *TxAccessSet) WarmAccessCount() int {
	return tas.warmAccesses
}

// Entries returns all access entries in this set.
func (tas *TxAccessSet) Entries() []AccessEntry {
	var entries []AccessEntry
	for addr := range tas.addresses {
		entries = append(entries, AccessEntry{
			Address: addr,
			Read:    true,
		})
	}
	for addr, slots := range tas.slots {
		for slot := range slots {
			entries = append(entries, AccessEntry{
				Address: addr,
				Slot:    slot,
				IsSlot:  true,
				Read:    true,
			})
		}
	}
	return entries
}

// AccessTracker provides block-level warm/cold access tracking across
// multiple transactions. It maintains per-transaction access sets and
// supports merging them into a block-level aggregate for cross-tx
// warm/cold state propagation.
type AccessTracker struct {
	mu           sync.Mutex
	blockSet     *TxAccessSet   // block-level aggregated access set
	txSets       []*TxAccessSet // per-transaction access sets
	currentTx    *TxAccessSet   // current transaction's access set
	witnessGas   uint64         // total witness gas charged
	witnessAE    *AccessEvents  // optional: access events for EIP-4762
}

// NewAccessTracker creates a new block-level access tracker.
func NewAccessTracker() *AccessTracker {
	return &AccessTracker{
		blockSet: newTxAccessSet(),
	}
}

// NewAccessTrackerWithWitness creates an access tracker with EIP-4762
// witness gas integration.
func NewAccessTrackerWithWitness(ae *AccessEvents) *AccessTracker {
	return &AccessTracker{
		blockSet:  newTxAccessSet(),
		witnessAE: ae,
	}
}

// BeginTx starts tracking a new transaction. Returns the per-transaction
// access set for direct gas lookups.
func (at *AccessTracker) BeginTx() *TxAccessSet {
	at.mu.Lock()
	defer at.mu.Unlock()

	txSet := newTxAccessSet()
	// Pre-warm the transaction set with the block-level warm state so
	// that cross-tx warm accesses are recognized.
	for addr := range at.blockSet.addresses {
		txSet.addresses[addr] = true
	}
	for addr, slots := range at.blockSet.slots {
		txSlots := make(map[types.Hash]bool, len(slots))
		for slot := range slots {
			txSlots[slot] = true
		}
		txSet.slots[addr] = txSlots
	}

	at.currentTx = txSet
	return txSet
}

// EndTx finalizes the current transaction and merges its access set into
// the block-level aggregate.
func (at *AccessTracker) EndTx() {
	at.mu.Lock()
	defer at.mu.Unlock()

	if at.currentTx == nil {
		return
	}

	// Merge current tx accesses into block-level set.
	for addr := range at.currentTx.addresses {
		at.blockSet.addresses[addr] = true
	}
	for addr, slots := range at.currentTx.slots {
		if _, ok := at.blockSet.slots[addr]; !ok {
			at.blockSet.slots[addr] = make(map[types.Hash]bool)
		}
		for slot := range slots {
			at.blockSet.slots[addr][slot] = true
		}
	}

	at.txSets = append(at.txSets, at.currentTx)
	at.currentTx = nil
}

// PreloadAccessList warms addresses and storage slots from an EIP-2930
// access list before transaction execution.
func (at *AccessTracker) PreloadAccessList(addresses []types.Address, slots map[types.Address][]types.Hash) {
	at.mu.Lock()
	defer at.mu.Unlock()

	target := at.currentTx
	if target == nil {
		target = at.blockSet
	}

	for _, addr := range addresses {
		target.addresses[addr] = true
	}
	for addr, slotList := range slots {
		target.addresses[addr] = true
		if _, ok := target.slots[addr]; !ok {
			target.slots[addr] = make(map[types.Hash]bool)
		}
		for _, slot := range slotList {
			target.slots[addr][slot] = true
		}
	}
}

// TouchAddress records an address access in the current context (tx or block).
func (at *AccessTracker) TouchAddress(addr types.Address) uint64 {
	at.mu.Lock()
	defer at.mu.Unlock()

	target := at.currentTx
	if target == nil {
		target = at.blockSet
	}
	gas := target.TouchAddress(addr)

	// If witness tracking is enabled, charge witness gas.
	if at.witnessAE != nil && gas == AccessTrackerColdAccountCost {
		wGas := at.witnessAE.AddAccount(addr, false, gas)
		at.witnessGas += wGas
	}
	return gas
}

// TouchSlot records a storage slot access in the current context.
func (at *AccessTracker) TouchSlot(addr types.Address, slot types.Hash) uint64 {
	at.mu.Lock()
	defer at.mu.Unlock()

	target := at.currentTx
	if target == nil {
		target = at.blockSet
	}
	gas := target.TouchSlot(addr, slot)

	if at.witnessAE != nil && gas == AccessTrackerColdSloadCost {
		wGas := at.witnessAE.SlotGas(addr, slot, false, gas, false)
		at.witnessGas += wGas
	}
	return gas
}

// IsAddressWarm checks if an address is warm in the current context.
func (at *AccessTracker) IsAddressWarm(addr types.Address) bool {
	at.mu.Lock()
	defer at.mu.Unlock()

	if at.currentTx != nil {
		return at.currentTx.ContainsAddress(addr)
	}
	return at.blockSet.ContainsAddress(addr)
}

// IsSlotWarm checks if a storage slot is warm in the current context.
func (at *AccessTracker) IsSlotWarm(addr types.Address, slot types.Hash) bool {
	at.mu.Lock()
	defer at.mu.Unlock()

	if at.currentTx != nil {
		_, slotWarm := at.currentTx.ContainsSlot(addr, slot)
		return slotWarm
	}
	_, slotWarm := at.blockSet.ContainsSlot(addr, slot)
	return slotWarm
}

// BlockAddressCount returns the number of unique addresses accessed in the block.
func (at *AccessTracker) BlockAddressCount() int {
	at.mu.Lock()
	defer at.mu.Unlock()
	return at.blockSet.AddressCount()
}

// BlockSlotCount returns the number of unique slots accessed in the block.
func (at *AccessTracker) BlockSlotCount() int {
	at.mu.Lock()
	defer at.mu.Unlock()
	return at.blockSet.SlotCount()
}

// TxCount returns the number of completed transactions.
func (at *AccessTracker) TxCount() int {
	at.mu.Lock()
	defer at.mu.Unlock()
	return len(at.txSets)
}

// TxAccessSetAt returns the access set for a completed transaction by index.
// Returns nil if the index is out of range.
func (at *AccessTracker) TxAccessSetAt(idx int) *TxAccessSet {
	at.mu.Lock()
	defer at.mu.Unlock()
	if idx < 0 || idx >= len(at.txSets) {
		return nil
	}
	return at.txSets[idx]
}

// WitnessGas returns the total witness gas charged.
func (at *AccessTracker) WitnessGas() uint64 {
	at.mu.Lock()
	defer at.mu.Unlock()
	return at.witnessGas
}

// Reset clears all tracking state.
func (at *AccessTracker) Reset() {
	at.mu.Lock()
	defer at.mu.Unlock()

	at.blockSet = newTxAccessSet()
	at.txSets = at.txSets[:0]
	at.currentTx = nil
	at.witnessGas = 0
}
