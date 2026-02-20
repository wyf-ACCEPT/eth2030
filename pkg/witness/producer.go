// producer.go implements the WitnessProducer which records state accesses
// during block execution and produces execution witnesses for stateless
// verification (EIP-6800/8025).
package witness

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Producer-specific errors. Note: ErrWitnessTooLarge is defined in validator.go.
var (
	ErrWitnessNotStarted = errors.New("witness production not started for any block")
	ErrWitnessNoAccess   = errors.New("no state accesses recorded")
)

// WitnessProducerConfig configures witness production behavior.
type WitnessProducerConfig struct {
	// MaxWitnessSize is the maximum allowed wire size of a produced witness
	// in bytes. Zero means no limit.
	MaxWitnessSize int

	// IncludeStorageProofs controls whether storage slot proofs are
	// included in the produced witness.
	IncludeStorageProofs bool

	// IncludeCode controls whether contract bytecode chunks are
	// included in the produced witness.
	IncludeCode bool
}

// DefaultProducerConfig returns a WitnessProducerConfig with sensible defaults.
func DefaultProducerConfig() WitnessProducerConfig {
	return WitnessProducerConfig{
		MaxWitnessSize:       DefaultMaxWitnessSize, // 1 MiB from validator.go
		IncludeStorageProofs: true,
		IncludeCode:          true,
	}
}

// AccessRecord tracks which account fields and storage keys were accessed
// for a particular address during block execution.
type AccessRecord struct {
	// Address is the account address.
	Address types.Address

	// Fields lists the account-level fields accessed (e.g. "nonce",
	// "balance", "code", "codehash").
	Fields map[string]bool

	// StorageKeys lists the storage slots accessed.
	StorageKeys map[types.Hash]bool

	// CodeAccessed indicates whether the full contract code was loaded.
	CodeAccessed bool
}

// ProducedWitness is the output of the witness production process. It
// contains all accessed accounts, storage proofs, code chunks, and the
// block context.
type ProducedWitness struct {
	// BlockNumber is the block for which this witness was produced.
	BlockNumber uint64

	// StateRoot is the pre-state root provided at BeginBlock.
	StateRoot types.Hash

	// AccessedAccounts maps addresses to their access records.
	AccessedAccounts map[types.Address]*AccessRecord

	// StorageProofs lists the storage keys that have proof data,
	// grouped by account address.
	StorageProofs map[types.Address][]types.Hash

	// CodeChunks maps addresses to a flag indicating code was accessed.
	CodeChunks map[types.Address]bool

	// AccountCount is the total number of distinct accounts accessed.
	AccountCount int

	// StorageKeyCount is the total number of distinct storage keys accessed.
	StorageKeyCount int
}

// WitnessProducer records state accesses during block execution and
// assembles a ProducedWitness. All public methods are safe for concurrent use.
type WitnessProducer struct {
	config WitnessProducerConfig

	mu          sync.RWMutex
	started     bool
	blockNumber uint64
	stateRoot   types.Hash
	records     map[types.Address]*AccessRecord
}

// NewWitnessProducer creates a new WitnessProducer with the given config.
func NewWitnessProducer(config WitnessProducerConfig) *WitnessProducer {
	return &WitnessProducer{
		config:  config,
		records: make(map[types.Address]*AccessRecord),
	}
}

// BeginBlock starts recording state accesses for the given block.
// It resets any previously recorded state.
func (wp *WitnessProducer) BeginBlock(blockNumber uint64, stateRoot types.Hash) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.started = true
	wp.blockNumber = blockNumber
	wp.stateRoot = stateRoot
	wp.records = make(map[types.Address]*AccessRecord)
}

// RecordAccountAccess records that specific fields of an account were read.
// Valid field names: "nonce", "balance", "code", "codehash".
func (wp *WitnessProducer) RecordAccountAccess(addr types.Address, fields []string) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	rec := wp.getOrCreateRecord(addr)
	for _, f := range fields {
		rec.Fields[f] = true
	}
}

// RecordStorageAccess records that a storage slot was read for the given
// account address.
func (wp *WitnessProducer) RecordStorageAccess(addr types.Address, key types.Hash) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	rec := wp.getOrCreateRecord(addr)
	rec.StorageKeys[key] = true
}

// RecordCodeAccess records that contract code was loaded for the given address.
func (wp *WitnessProducer) RecordCodeAccess(addr types.Address) {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	rec := wp.getOrCreateRecord(addr)
	rec.CodeAccessed = true
	rec.Fields["code"] = true
}

// ProduceWitness assembles a ProducedWitness from all recorded accesses.
// Returns ErrWitnessNotStarted if BeginBlock was not called, and
// ErrWitnessNoAccess if no state accesses were recorded.
func (wp *WitnessProducer) ProduceWitness() (*ProducedWitness, error) {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	if !wp.started {
		return nil, ErrWitnessNotStarted
	}

	if len(wp.records) == 0 {
		return nil, ErrWitnessNoAccess
	}

	pw := &ProducedWitness{
		BlockNumber:      wp.blockNumber,
		StateRoot:        wp.stateRoot,
		AccessedAccounts: make(map[types.Address]*AccessRecord, len(wp.records)),
		StorageProofs:    make(map[types.Address][]types.Hash),
		CodeChunks:       make(map[types.Address]bool),
	}

	totalStorageKeys := 0

	for addr, rec := range wp.records {
		// Deep copy the access record.
		copyRec := &AccessRecord{
			Address:      addr,
			Fields:       make(map[string]bool, len(rec.Fields)),
			StorageKeys:  make(map[types.Hash]bool, len(rec.StorageKeys)),
			CodeAccessed: rec.CodeAccessed,
		}
		for f, v := range rec.Fields {
			copyRec.Fields[f] = v
		}
		for k, v := range rec.StorageKeys {
			copyRec.StorageKeys[k] = v
		}
		pw.AccessedAccounts[addr] = copyRec

		// Build storage proofs list if configured.
		if wp.config.IncludeStorageProofs && len(rec.StorageKeys) > 0 {
			keys := make([]types.Hash, 0, len(rec.StorageKeys))
			for k := range rec.StorageKeys {
				keys = append(keys, k)
			}
			// Sort keys for deterministic output.
			sort.Slice(keys, func(i, j int) bool {
				return hashLess(keys[i], keys[j])
			})
			pw.StorageProofs[addr] = keys
			totalStorageKeys += len(keys)
		} else {
			totalStorageKeys += len(rec.StorageKeys)
		}

		// Record code access if configured.
		if wp.config.IncludeCode && rec.CodeAccessed {
			pw.CodeChunks[addr] = true
		}
	}

	pw.AccountCount = len(wp.records)
	pw.StorageKeyCount = totalStorageKeys

	// Check witness size limit.
	if wp.config.MaxWitnessSize > 0 {
		size := WitnessSize(pw)
		if size > wp.config.MaxWitnessSize {
			return nil, fmt.Errorf("%w: size %d exceeds max %d",
				ErrWitnessTooLarge, size, wp.config.MaxWitnessSize)
		}
	}

	return pw, nil
}

// WitnessSize calculates the approximate wire size of a ProducedWitness
// in bytes. The calculation accounts for addresses, field flags, storage
// keys, and overhead.
func WitnessSize(w *ProducedWitness) int {
	if w == nil {
		return 0
	}

	// Base: block number (8) + state root (32)
	size := 8 + types.HashLength

	for addr, rec := range w.AccessedAccounts {
		_ = addr
		// Address (20 bytes) + field count indicator (4 bytes)
		size += types.AddressLength + 4

		// Each field name stored as a short string (average ~7 bytes each).
		size += len(rec.Fields) * 8

		// Each storage key is 32 bytes.
		size += len(rec.StorageKeys) * types.HashLength
	}

	// Storage proofs: address (20) + key count (4) + keys (32 each)
	for _, keys := range w.StorageProofs {
		size += types.AddressLength + 4
		size += len(keys) * types.HashLength
	}

	// Code chunks: address (20) + flag (1)
	size += len(w.CodeChunks) * (types.AddressLength + 1)

	return size
}

// Reset clears all recording state. The producer can be reused for a new
// block after calling Reset followed by BeginBlock.
func (wp *WitnessProducer) Reset() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	wp.started = false
	wp.blockNumber = 0
	wp.stateRoot = types.Hash{}
	wp.records = make(map[types.Address]*AccessRecord)
}

// IsStarted reports whether BeginBlock has been called and recording is active.
func (wp *WitnessProducer) IsStarted() bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.started
}

// BlockNumber returns the current block number being recorded.
func (wp *WitnessProducer) BlockNumber() uint64 {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return wp.blockNumber
}

// AccountAccessCount returns the number of distinct accounts that have
// been accessed so far.
func (wp *WitnessProducer) AccountAccessCount() int {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	return len(wp.records)
}

// StorageAccessCount returns the total number of distinct storage keys
// accessed across all accounts.
func (wp *WitnessProducer) StorageAccessCount() int {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	count := 0
	for _, rec := range wp.records {
		count += len(rec.StorageKeys)
	}
	return count
}

// HasAccountAccess returns whether any access has been recorded for the
// given address.
func (wp *WitnessProducer) HasAccountAccess(addr types.Address) bool {
	wp.mu.RLock()
	defer wp.mu.RUnlock()
	_, ok := wp.records[addr]
	return ok
}

// getOrCreateRecord returns the AccessRecord for addr, creating one if
// it does not exist. Caller must hold wp.mu.
func (wp *WitnessProducer) getOrCreateRecord(addr types.Address) *AccessRecord {
	if rec, ok := wp.records[addr]; ok {
		return rec
	}
	rec := &AccessRecord{
		Address:     addr,
		Fields:      make(map[string]bool),
		StorageKeys: make(map[types.Hash]bool),
	}
	wp.records[addr] = rec
	return rec
}

// hashLess returns true if a < b (lexicographic byte comparison).
func hashLess(a, b types.Hash) bool {
	for i := 0; i < types.HashLength; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
