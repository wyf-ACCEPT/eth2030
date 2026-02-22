package state

import (
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// StatePrefetcher pre-loads account state and storage into an in-memory cache
// ahead of transaction execution. This enables parallel transaction processing
// by warming the state cache so EVM reads encounter fewer cold paths.
//
// For the in-memory MemoryStateDB, the prefetcher primarily ensures state
// objects exist in the map. In a disk-backed implementation, this would
// trigger asynchronous reads from the underlying database.
type StatePrefetcher struct {
	db *MemoryStateDB
	mu sync.Mutex
}

// NewStatePrefetcher creates a prefetcher that warms the given state database.
func NewStatePrefetcher(db *MemoryStateDB) *StatePrefetcher {
	return &StatePrefetcher{db: db}
}

// PrefetchAddresses pre-loads state for a batch of addresses concurrently.
// Each address's state object is ensured to exist in the state map. This is
// safe to call from a background goroutine before transaction execution begins.
func (p *StatePrefetcher) PrefetchAddresses(addrs []types.Address) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, addr := range addrs {
		if p.db.stateObjects[addr] == nil {
			p.db.stateObjects[addr] = newStateObject()
		}
	}
}

// PrefetchStorageSlots pre-loads specific storage slots for an address.
// For MemoryStateDB, this ensures the state object exists. In a disk-backed
// implementation, this would trigger reads of the specified slots.
func (p *StatePrefetcher) PrefetchStorageSlots(addr types.Address, keys []types.Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.db.stateObjects[addr] == nil {
		p.db.stateObjects[addr] = newStateObject()
	}
	// In a disk-backed implementation, iterate keys and trigger async reads:
	// for _, key := range keys {
	//     go p.db.readStorageFromDisk(addr, key)
	// }
}

// PrefetchTransaction pre-loads state for all addresses and storage slots
// that a transaction is expected to touch. The caller provides the sender,
// receiver, and any access list entries.
func (p *StatePrefetcher) PrefetchTransaction(
	sender types.Address,
	receiver *types.Address,
	accessListAddrs []types.Address,
	accessListSlots map[types.Address][]types.Hash,
) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure sender state object exists.
	if p.db.stateObjects[sender] == nil {
		p.db.stateObjects[sender] = newStateObject()
	}

	// Ensure receiver state object exists (if non-nil, i.e., not a contract creation).
	if receiver != nil {
		if p.db.stateObjects[*receiver] == nil {
			p.db.stateObjects[*receiver] = newStateObject()
		}
	}

	// Pre-load access list addresses.
	for _, addr := range accessListAddrs {
		if p.db.stateObjects[addr] == nil {
			p.db.stateObjects[addr] = newStateObject()
		}
	}

	// Pre-load access list storage slots.
	// In a disk-backed implementation, this would trigger async storage reads.
	for addr := range accessListSlots {
		if p.db.stateObjects[addr] == nil {
			p.db.stateObjects[addr] = newStateObject()
		}
	}
}

// IsPrefetched returns true if the given address already has a state object
// in the database (i.e., has been prefetched or previously accessed).
func (p *StatePrefetcher) IsPrefetched(addr types.Address) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.db.stateObjects[addr] != nil
}
