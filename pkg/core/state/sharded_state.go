// sharded_state.go implements a sharded StateDB wrapper that partitions state
// access by address prefix for concurrent gigagas execution. Each shard covers
// a nibble of the address space (16 shards), with its own RWMutex to minimize
// contention during parallel transaction processing.
package state

import (
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

const (
	// numShards is the number of address-space shards (first nibble: 0x0..0xF).
	numShards = 16
)

// shardIndex returns the shard number for a given address (first nibble).
func shardIndex(addr types.Address) int {
	return int(addr[0] >> 4)
}

// TxAccessRecord tracks reads and writes per transaction for conflict detection.
type TxAccessRecord struct {
	TxIndex int
	Reads   map[types.Address]map[types.Hash]struct{}
	Writes  map[types.Address]map[types.Hash]struct{}
}

// NewTxAccessRecord creates an empty access record.
func NewTxAccessRecord(txIndex int) *TxAccessRecord {
	return &TxAccessRecord{
		TxIndex: txIndex,
		Reads:   make(map[types.Address]map[types.Hash]struct{}),
		Writes:  make(map[types.Address]map[types.Hash]struct{}),
	}
}

// AddRead records a read of addr+key.
func (r *TxAccessRecord) AddRead(addr types.Address, key types.Hash) {
	if r.Reads[addr] == nil {
		r.Reads[addr] = make(map[types.Hash]struct{})
	}
	r.Reads[addr][key] = struct{}{}
}

// AddWrite records a write to addr+key.
func (r *TxAccessRecord) AddWrite(addr types.Address, key types.Hash) {
	if r.Writes[addr] == nil {
		r.Writes[addr] = make(map[types.Hash]struct{})
	}
	r.Writes[addr][key] = struct{}{}
}

// ConflictsWith returns true if this record conflicts with another.
// A conflict exists when one writes an address+key that the other reads or writes.
func (r *TxAccessRecord) ConflictsWith(other *TxAccessRecord) bool {
	// Check if our writes overlap with their reads or writes.
	for addr, keys := range r.Writes {
		if otherKeys, ok := other.Reads[addr]; ok {
			for k := range keys {
				if _, found := otherKeys[k]; found {
					return true
				}
			}
		}
		if otherKeys, ok := other.Writes[addr]; ok {
			for k := range keys {
				if _, found := otherKeys[k]; found {
					return true
				}
			}
		}
	}
	// Check if their writes overlap with our reads.
	for addr, keys := range other.Writes {
		if ourKeys, ok := r.Reads[addr]; ok {
			for k := range keys {
				if _, found := ourKeys[k]; found {
					return true
				}
			}
		}
	}
	return false
}

// shard holds the state and lock for one address-space partition.
type shard struct {
	mu       sync.RWMutex
	balances map[types.Address]*big.Int
	nonces   map[types.Address]uint64
	storage  map[types.Address]map[types.Hash]types.Hash
}

func newShard() *shard {
	return &shard{
		balances: make(map[types.Address]*big.Int),
		nonces:   make(map[types.Address]uint64),
		storage:  make(map[types.Address]map[types.Hash]types.Hash),
	}
}

// ShardedStateDB is a concurrent state wrapper sharded by address prefix.
// It is NOT a full StateDB implementation; it wraps a subset of state
// operations needed for parallel execution and provides Merge and conflict
// detection capabilities.
type ShardedStateDB struct {
	shards [numShards]*shard
}

// NewShardedStateDB creates a new sharded state wrapper.
func NewShardedStateDB() *ShardedStateDB {
	s := &ShardedStateDB{}
	for i := range s.shards {
		s.shards[i] = newShard()
	}
	return s
}

// getShard returns the shard for the given address.
func (s *ShardedStateDB) getShard(addr types.Address) *shard {
	return s.shards[shardIndex(addr)]
}

// GetBalance returns the balance for addr, or zero if not set.
func (s *ShardedStateDB) GetBalance(addr types.Address) *big.Int {
	sh := s.getShard(addr)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if bal, ok := sh.balances[addr]; ok {
		return new(big.Int).Set(bal)
	}
	return new(big.Int)
}

// SetBalance sets the balance for addr.
func (s *ShardedStateDB) SetBalance(addr types.Address, amount *big.Int) {
	sh := s.getShard(addr)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.balances[addr] = new(big.Int).Set(amount)
}

// GetNonce returns the nonce for addr.
func (s *ShardedStateDB) GetNonce(addr types.Address) uint64 {
	sh := s.getShard(addr)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	return sh.nonces[addr]
}

// SetNonce sets the nonce for addr.
func (s *ShardedStateDB) SetNonce(addr types.Address, nonce uint64) {
	sh := s.getShard(addr)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.nonces[addr] = nonce
}

// GetState returns the storage value at addr+key.
func (s *ShardedStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	sh := s.getShard(addr)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if slots, ok := sh.storage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

// SetState writes a storage value at addr+key.
func (s *ShardedStateDB) SetState(addr types.Address, key, value types.Hash) {
	sh := s.getShard(addr)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.storage[addr] == nil {
		sh.storage[addr] = make(map[types.Hash]types.Hash)
	}
	sh.storage[addr][key] = value
}

// Merge applies all state from src into this ShardedStateDB.
// Each shard is merged independently, so two non-overlapping Merge calls
// can proceed in parallel.
func (s *ShardedStateDB) Merge(src *ShardedStateDB) {
	for i := 0; i < numShards; i++ {
		dst := s.shards[i]
		srcSh := src.shards[i]

		dst.mu.Lock()
		srcSh.mu.RLock()

		for addr, bal := range srcSh.balances {
			dst.balances[addr] = new(big.Int).Set(bal)
		}
		for addr, nonce := range srcSh.nonces {
			dst.nonces[addr] = nonce
		}
		for addr, slots := range srcSh.storage {
			if dst.storage[addr] == nil {
				dst.storage[addr] = make(map[types.Hash]types.Hash, len(slots))
			}
			for k, v := range slots {
				dst.storage[addr][k] = v
			}
		}

		srcSh.mu.RUnlock()
		dst.mu.Unlock()
	}
}

// MergeParallel applies state from src, merging all shards concurrently.
func (s *ShardedStateDB) MergeParallel(src *ShardedStateDB) {
	var wg sync.WaitGroup
	for i := 0; i < numShards; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			dst := s.shards[idx]
			srcSh := src.shards[idx]

			dst.mu.Lock()
			srcSh.mu.RLock()

			for addr, bal := range srcSh.balances {
				dst.balances[addr] = new(big.Int).Set(bal)
			}
			for addr, nonce := range srcSh.nonces {
				dst.nonces[addr] = nonce
			}
			for addr, slots := range srcSh.storage {
				if dst.storage[addr] == nil {
					dst.storage[addr] = make(map[types.Hash]types.Hash, len(slots))
				}
				for k, v := range slots {
					dst.storage[addr][k] = v
				}
			}

			srcSh.mu.RUnlock()
			dst.mu.Unlock()
		}(i)
	}
	wg.Wait()
}

// DetectConflicts checks whether two TxAccessRecords have overlapping
// read/write sets. Returns true if there is a conflict.
func DetectShardedConflicts(a, b *TxAccessRecord) bool {
	return a.ConflictsWith(b)
}

// ShardCount returns the number of address-space shards.
func (s *ShardedStateDB) ShardCount() int { return numShards }

// AddressCount returns the total number of addresses with balance or nonce set.
func (s *ShardedStateDB) AddressCount() int {
	total := 0
	for i := 0; i < numShards; i++ {
		sh := s.shards[i]
		sh.mu.RLock()
		// Count unique addresses across balances and nonces.
		addrs := make(map[types.Address]struct{})
		for a := range sh.balances {
			addrs[a] = struct{}{}
		}
		for a := range sh.nonces {
			addrs[a] = struct{}{}
		}
		total += len(addrs)
		sh.mu.RUnlock()
	}
	return total
}
