// witness_accumulator.go implements VOPS witness accumulation during block
// execution. It tracks all accessed state elements (accounts, storage slots,
// code) and builds minimal witnesses for stateless verification.
package vops

import (
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// AccessType classifies the kind of state access.
type AccessType uint8

const (
	// AccessTypeAccount indicates an account read (nonce, balance, etc.).
	AccessTypeAccount AccessType = iota
	// AccessTypeStorage indicates a storage slot read or write.
	AccessTypeStorage
	// AccessTypeCode indicates a contract code read.
	AccessTypeCode
)

// StateAccess records a single state element access during block execution.
type StateAccess struct {
	Address types.Address
	Type    AccessType
	Slot    types.Hash // only set for AccessTypeStorage
}

// WitnessEntry contains the data for a single witnessed state element.
type WitnessEntry struct {
	Address     types.Address
	Type        AccessType
	Slot        types.Hash   // for storage accesses
	Nonce       uint64       // for account accesses
	Balance     *big.Int     // for account accesses
	CodeHash    types.Hash   // for account/code accesses
	StorageRoot types.Hash   // for account accesses
	Value       types.Hash   // for storage accesses
	Code        []byte       // for code accesses
}

// AccumulatedWitness contains all state accesses and their values,
// sufficient for stateless block verification.
type AccumulatedWitness struct {
	StateRoot types.Hash
	Entries   []WitnessEntry
	Hash      types.Hash // commitment over all entries
}

// WitnessAccumulator tracks state accesses during block execution and
// builds a minimal witness for stateless verification.
type WitnessAccumulator struct {
	mu          sync.Mutex
	stateRoot   types.Hash
	accounts    map[types.Address]*accountWitness
	storage     map[storageKey]types.Hash
	code        map[types.Address][]byte
	accessOrder []StateAccess // ordered access log
}

// accountWitness stores witnessed account data.
type accountWitness struct {
	Nonce       uint64
	Balance     *big.Int
	CodeHash    types.Hash
	StorageRoot types.Hash
}

// storageKey identifies a storage slot.
type storageKey struct {
	Addr types.Address
	Slot types.Hash
}

// NewWitnessAccumulator creates an accumulator for the given state root.
func NewWitnessAccumulator(stateRoot types.Hash) *WitnessAccumulator {
	return &WitnessAccumulator{
		stateRoot: stateRoot,
		accounts:  make(map[types.Address]*accountWitness),
		storage:   make(map[storageKey]types.Hash),
		code:      make(map[types.Address][]byte),
	}
}

// RecordAccountAccess records an account state read.
func (wa *WitnessAccumulator) RecordAccountAccess(addr types.Address, nonce uint64, balance *big.Int, codeHash, storageRoot types.Hash) {
	wa.mu.Lock()
	defer wa.mu.Unlock()

	if _, exists := wa.accounts[addr]; !exists {
		var bal *big.Int
		if balance != nil {
			bal = new(big.Int).Set(balance)
		}
		wa.accounts[addr] = &accountWitness{
			Nonce:       nonce,
			Balance:     bal,
			CodeHash:    codeHash,
			StorageRoot: storageRoot,
		}
		wa.accessOrder = append(wa.accessOrder, StateAccess{
			Address: addr,
			Type:    AccessTypeAccount,
		})
	}
}

// RecordStorageAccess records a storage slot access.
func (wa *WitnessAccumulator) RecordStorageAccess(addr types.Address, slot, value types.Hash) {
	wa.mu.Lock()
	defer wa.mu.Unlock()

	key := storageKey{Addr: addr, Slot: slot}
	if _, exists := wa.storage[key]; !exists {
		wa.storage[key] = value
		wa.accessOrder = append(wa.accessOrder, StateAccess{
			Address: addr,
			Type:    AccessTypeStorage,
			Slot:    slot,
		})
	}
}

// RecordCodeAccess records a contract code access.
func (wa *WitnessAccumulator) RecordCodeAccess(addr types.Address, code []byte) {
	wa.mu.Lock()
	defer wa.mu.Unlock()

	if _, exists := wa.code[addr]; !exists {
		cp := make([]byte, len(code))
		copy(cp, code)
		wa.code[addr] = cp
		wa.accessOrder = append(wa.accessOrder, StateAccess{
			Address: addr,
			Type:    AccessTypeCode,
		})
	}
}

// Build constructs the AccumulatedWitness from all recorded accesses.
// Entries are sorted for deterministic output.
func (wa *WitnessAccumulator) Build() *AccumulatedWitness {
	wa.mu.Lock()
	defer wa.mu.Unlock()

	var entries []WitnessEntry

	// Collect account entries sorted by address.
	addrs := make([]types.Address, 0, len(wa.accounts))
	for addr := range wa.accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addressLess(addrs[i], addrs[j])
	})

	for _, addr := range addrs {
		acct := wa.accounts[addr]
		entries = append(entries, WitnessEntry{
			Address:     addr,
			Type:        AccessTypeAccount,
			Nonce:       acct.Nonce,
			Balance:     acct.Balance,
			CodeHash:    acct.CodeHash,
			StorageRoot: acct.StorageRoot,
		})
	}

	// Collect storage entries sorted by (address, slot).
	skeys := make([]storageKey, 0, len(wa.storage))
	for key := range wa.storage {
		skeys = append(skeys, key)
	}
	sort.Slice(skeys, func(i, j int) bool {
		if skeys[i].Addr != skeys[j].Addr {
			return addressLess(skeys[i].Addr, skeys[j].Addr)
		}
		return hashLessThan(skeys[i].Slot, skeys[j].Slot)
	})

	for _, key := range skeys {
		entries = append(entries, WitnessEntry{
			Address: key.Addr,
			Type:    AccessTypeStorage,
			Slot:    key.Slot,
			Value:   wa.storage[key],
		})
	}

	// Collect code entries sorted by address.
	codeAddrs := make([]types.Address, 0, len(wa.code))
	for addr := range wa.code {
		codeAddrs = append(codeAddrs, addr)
	}
	sort.Slice(codeAddrs, func(i, j int) bool {
		return addressLess(codeAddrs[i], codeAddrs[j])
	})

	for _, addr := range codeAddrs {
		entries = append(entries, WitnessEntry{
			Address:  addr,
			Type:     AccessTypeCode,
			CodeHash: crypto.Keccak256Hash(wa.code[addr]),
			Code:     wa.code[addr],
		})
	}

	hash := computeWitnessHash(wa.stateRoot, entries)

	return &AccumulatedWitness{
		StateRoot: wa.stateRoot,
		Entries:   entries,
		Hash:      hash,
	}
}

// AccessCount returns the total number of unique state accesses recorded.
func (wa *WitnessAccumulator) AccessCount() int {
	wa.mu.Lock()
	defer wa.mu.Unlock()
	return len(wa.accounts) + len(wa.storage) + len(wa.code)
}

// AccessLog returns an ordered copy of all recorded state accesses.
func (wa *WitnessAccumulator) AccessLog() []StateAccess {
	wa.mu.Lock()
	defer wa.mu.Unlock()
	cp := make([]StateAccess, len(wa.accessOrder))
	copy(cp, wa.accessOrder)
	return cp
}

// Reset clears all accumulated state accesses.
func (wa *WitnessAccumulator) Reset(newStateRoot types.Hash) {
	wa.mu.Lock()
	defer wa.mu.Unlock()
	wa.stateRoot = newStateRoot
	wa.accounts = make(map[types.Address]*accountWitness)
	wa.storage = make(map[storageKey]types.Hash)
	wa.code = make(map[types.Address][]byte)
	wa.accessOrder = nil
}

// WitnessSize returns the approximate byte size of the accumulated witness.
func (wa *WitnessAccumulator) WitnessSize() int {
	wa.mu.Lock()
	defer wa.mu.Unlock()

	size := 32 // state root
	// Account: 20 (addr) + 8 (nonce) + 32 (balance) + 32 (codeHash) + 32 (storageRoot)
	size += len(wa.accounts) * 124
	// Storage: 20 (addr) + 32 (slot) + 32 (value)
	size += len(wa.storage) * 84
	// Code
	for _, code := range wa.code {
		size += 20 + len(code) // addr + code
	}
	return size
}

// computeWitnessHash creates a deterministic hash commitment over the
// state root and all witness entries.
func computeWitnessHash(stateRoot types.Hash, entries []WitnessEntry) types.Hash {
	var data []byte
	data = append(data, stateRoot[:]...)

	for _, e := range entries {
		data = append(data, e.Address[:]...)
		data = append(data, byte(e.Type))
		switch e.Type {
		case AccessTypeAccount:
			data = append(data, e.CodeHash[:]...)
			data = append(data, e.StorageRoot[:]...)
		case AccessTypeStorage:
			data = append(data, e.Slot[:]...)
			data = append(data, e.Value[:]...)
		case AccessTypeCode:
			data = append(data, e.CodeHash[:]...)
		}
	}

	return crypto.Keccak256Hash(data)
}

// addressLess compares two addresses lexicographically.
func addressLess(a, b types.Address) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// hashLessThan compares two hashes lexicographically.
func hashLessThan(a, b types.Hash) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
