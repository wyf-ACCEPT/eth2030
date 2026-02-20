// state_witness.go implements a stateless execution witness builder that
// collects accessed state (accounts, storage, code) during EVM execution
// and produces a compact witness for verification.
//
// Unlike the WitnessCollector (which wraps a StateDB), the
// StateWitnessBuilder operates as a standalone accumulator that can be
// fed access events from any source. It supports deduplication, size
// estimation, compaction, and produces a verifiable StateWitness.
//
// Per EIP-6800/EIP-8025, the witness must contain all pre-state data
// needed to re-execute a block without the full state trie.
package witness

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// StateWitness builder errors.
var (
	ErrStateWitnessFinalized = errors.New("witness: state witness already finalized")
	ErrStateWitnessEmpty     = errors.New("witness: state witness has no entries")
	ErrStateWitnessNotReady  = errors.New("witness: state witness not yet finalized")
)

// StateWitness is a compact, verifiable execution witness containing all
// accessed state for stateless block verification.
type StateWitness struct {
	// BlockNumber is the block this witness covers.
	BlockNumber uint64
	// StateRoot is the pre-state root before execution.
	StateRoot types.Hash
	// Accounts maps addresses to their witnessed pre-state.
	Accounts map[types.Address]*StateWitnessAccount
	// Codes maps code hashes to bytecode.
	Codes map[types.Hash][]byte
	// AccessedSlots tracks total number of storage slots in the witness.
	AccessedSlots int
	// WitnessHash is the deterministic hash of the witness content.
	WitnessHash types.Hash
}

// StateWitnessAccount holds an account's pre-state in the witness.
type StateWitnessAccount struct {
	Nonce    uint64
	Balance  *big.Int
	CodeHash types.Hash
	Storage  map[types.Hash]types.Hash
	Exists   bool
}

// StateWitnessBuilder collects state accesses and produces a StateWitness.
// All methods are safe for concurrent use.
type StateWitnessBuilder struct {
	mu sync.Mutex

	blockNum  uint64
	stateRoot types.Hash
	finalized bool

	accounts   map[types.Address]*stateWitnessEntry
	codes      map[types.Hash][]byte
	slotCount  int
	accessLog  []StateAccessRecord
}

// stateWitnessEntry tracks one account during collection.
type stateWitnessEntry struct {
	nonce    uint64
	balance  *big.Int
	codeHash types.Hash
	storage  map[types.Hash]types.Hash
	exists   bool
	recorded bool // true after first account-level capture
}

// StateAccessRecord is a structured log entry for a single state access.
type StateAccessRecord struct {
	Address types.Address
	Type    StateAccessType
	Key     types.Hash // storage key, if applicable
}

// StateAccessType categorizes the type of access.
type StateAccessType uint8

const (
	StateAccessAccount StateAccessType = iota
	StateAccessStorage
	StateAccessCode
)

// NewStateWitnessBuilder creates a builder for the given block context.
func NewStateWitnessBuilder(blockNum uint64, stateRoot types.Hash) *StateWitnessBuilder {
	return &StateWitnessBuilder{
		blockNum:  blockNum,
		stateRoot: stateRoot,
		accounts:  make(map[types.Address]*stateWitnessEntry),
		codes:     make(map[types.Hash][]byte),
	}
}

// RecordAccount records an account's pre-state. Only the first call for
// each address captures the values; subsequent calls are no-ops.
func (b *StateWitnessBuilder) RecordAccount(
	addr types.Address,
	exists bool,
	nonce uint64,
	balance *big.Int,
	codeHash types.Hash,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finalized {
		return ErrStateWitnessFinalized
	}

	entry := b.getOrCreate(addr)
	if !entry.recorded {
		entry.exists = exists
		entry.nonce = nonce
		if balance != nil {
			entry.balance = new(big.Int).Set(balance)
		}
		entry.codeHash = codeHash
		entry.recorded = true
	}
	b.accessLog = append(b.accessLog, StateAccessRecord{
		Address: addr,
		Type:    StateAccessAccount,
	})
	return nil
}

// RecordStorage records a storage slot read. Only the first value for
// each (address, key) pair is captured.
func (b *StateWitnessBuilder) RecordStorage(
	addr types.Address,
	key types.Hash,
	value types.Hash,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finalized {
		return ErrStateWitnessFinalized
	}

	entry := b.getOrCreate(addr)
	if _, seen := entry.storage[key]; !seen {
		entry.storage[key] = value
		b.slotCount++
	}
	b.accessLog = append(b.accessLog, StateAccessRecord{
		Address: addr,
		Type:    StateAccessStorage,
		Key:     key,
	})
	return nil
}

// RecordCode records contract bytecode. Deduplicated by code hash.
func (b *StateWitnessBuilder) RecordCode(
	codeHash types.Hash,
	code []byte,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finalized {
		return ErrStateWitnessFinalized
	}

	if codeHash == types.EmptyCodeHash || codeHash.IsZero() {
		return nil
	}
	if _, exists := b.codes[codeHash]; !exists {
		cp := make([]byte, len(code))
		copy(cp, code)
		b.codes[codeHash] = cp
	}
	return nil
}

// AccountCount returns the number of unique accounts recorded.
func (b *StateWitnessBuilder) AccountCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.accounts)
}

// SlotCount returns the number of unique storage slots recorded.
func (b *StateWitnessBuilder) SlotCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.slotCount
}

// CodeCount returns the number of unique code entries recorded.
func (b *StateWitnessBuilder) CodeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.codes)
}

// AccessLogLen returns the number of access records logged.
func (b *StateWitnessBuilder) AccessLogLen() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.accessLog)
}

// EstimateSize estimates the witness size in bytes before finalization.
func (b *StateWitnessBuilder) EstimateSize() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Base: block number (8) + state root (32)
	size := 40
	// Accounts: addr(20) + nonce(8) + balance(32) + codeHash(32) + exists(1)
	size += len(b.accounts) * (20 + 8 + 32 + 32 + 1)
	// Storage: key(32) + value(32) per slot
	size += b.slotCount * 64
	// Codes: hash(32) + length(4) + data
	for _, code := range b.codes {
		size += 36 + len(code)
	}
	return size
}

// Finalize builds the final StateWitness from the collected data. After
// finalization, no more records can be added.
func (b *StateWitnessBuilder) Finalize() (*StateWitness, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.finalized {
		return nil, ErrStateWitnessFinalized
	}
	if len(b.accounts) == 0 {
		return nil, ErrStateWitnessEmpty
	}
	b.finalized = true

	sw := &StateWitness{
		BlockNumber:   b.blockNum,
		StateRoot:     b.stateRoot,
		Accounts:      make(map[types.Address]*StateWitnessAccount, len(b.accounts)),
		Codes:         make(map[types.Hash][]byte, len(b.codes)),
		AccessedSlots: b.slotCount,
	}

	// Copy accounts.
	for addr, entry := range b.accounts {
		acc := &StateWitnessAccount{
			Nonce:    entry.nonce,
			Balance:  new(big.Int),
			CodeHash: entry.codeHash,
			Exists:   entry.exists,
			Storage:  make(map[types.Hash]types.Hash, len(entry.storage)),
		}
		if entry.balance != nil {
			acc.Balance.Set(entry.balance)
		}
		for k, v := range entry.storage {
			acc.Storage[k] = v
		}
		sw.Accounts[addr] = acc
	}

	// Copy codes.
	for h, code := range b.codes {
		cp := make([]byte, len(code))
		copy(cp, code)
		sw.Codes[h] = cp
	}

	// Compute deterministic witness hash.
	sw.WitnessHash = computeStateWitnessHash(sw)
	return sw, nil
}

// IsFinalized reports whether the builder has been finalized.
func (b *StateWitnessBuilder) IsFinalized() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.finalized
}

// computeStateWitnessHash produces a deterministic hash of the witness
// by hashing sorted account and storage data.
func computeStateWitnessHash(sw *StateWitness) types.Hash {
	// Sort addresses for determinism.
	addrs := make([]types.Address, 0, len(sw.Accounts))
	for addr := range sw.Accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return stateWitnessAddrLess(addrs[i], addrs[j])
	})

	var data []byte
	// Mix in block number and state root.
	data = append(data, sw.StateRoot[:]...)
	buf := make([]byte, 8)
	buf[0] = byte(sw.BlockNumber >> 56)
	buf[1] = byte(sw.BlockNumber >> 48)
	buf[2] = byte(sw.BlockNumber >> 40)
	buf[3] = byte(sw.BlockNumber >> 32)
	buf[4] = byte(sw.BlockNumber >> 24)
	buf[5] = byte(sw.BlockNumber >> 16)
	buf[6] = byte(sw.BlockNumber >> 8)
	buf[7] = byte(sw.BlockNumber)
	data = append(data, buf...)

	for _, addr := range addrs {
		acc := sw.Accounts[addr]
		data = append(data, addr[:]...)
		// Include nonce.
		nonceBuf := make([]byte, 8)
		nonceBuf[0] = byte(acc.Nonce >> 56)
		nonceBuf[1] = byte(acc.Nonce >> 48)
		nonceBuf[2] = byte(acc.Nonce >> 40)
		nonceBuf[3] = byte(acc.Nonce >> 32)
		nonceBuf[4] = byte(acc.Nonce >> 24)
		nonceBuf[5] = byte(acc.Nonce >> 16)
		nonceBuf[6] = byte(acc.Nonce >> 8)
		nonceBuf[7] = byte(acc.Nonce)
		data = append(data, nonceBuf...)
		data = append(data, acc.CodeHash[:]...)
		if acc.Balance != nil {
			data = append(data, acc.Balance.Bytes()...)
		}
		// Include exists flag.
		if acc.Exists {
			data = append(data, 1)
		} else {
			data = append(data, 0)
		}

		// Sort storage keys.
		keys := make([]types.Hash, 0, len(acc.Storage))
		for k := range acc.Storage {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return stateWitnessHashLess(keys[i], keys[j])
		})
		for _, k := range keys {
			v := acc.Storage[k]
			data = append(data, k[:]...)
			data = append(data, v[:]...)
		}
	}

	return crypto.Keccak256Hash(data)
}

// VerifyStateWitnessHash checks that the witness hash matches its content.
func VerifyStateWitnessHash(sw *StateWitness) bool {
	if sw == nil {
		return false
	}
	computed := computeStateWitnessHash(sw)
	return computed == sw.WitnessHash
}

// StateWitnessSize returns the estimated byte size of a StateWitness.
func StateWitnessSize(sw *StateWitness) int {
	if sw == nil {
		return 0
	}
	size := 8 + types.HashLength + types.HashLength // blockNum + stateRoot + witnessHash
	for _, acc := range sw.Accounts {
		size += types.AddressLength + 8 + 32 + types.HashLength + 1
		size += len(acc.Storage) * (types.HashLength * 2)
	}
	for _, code := range sw.Codes {
		size += types.HashLength + len(code)
	}
	return size
}

// getOrCreate returns or creates a stateWitnessEntry for the address.
// Caller must hold b.mu.
func (b *StateWitnessBuilder) getOrCreate(addr types.Address) *stateWitnessEntry {
	if entry, ok := b.accounts[addr]; ok {
		return entry
	}
	entry := &stateWitnessEntry{
		balance: new(big.Int),
		storage: make(map[types.Hash]types.Hash),
	}
	b.accounts[addr] = entry
	return entry
}

func stateWitnessAddrLess(a, b types.Address) bool {
	for i := 0; i < types.AddressLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func stateWitnessHashLess(a, b types.Hash) bool {
	for i := 0; i < types.HashLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
