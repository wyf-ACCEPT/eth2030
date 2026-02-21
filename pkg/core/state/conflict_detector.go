// conflict_detector.go implements read-write conflict detection for parallel
// transaction execution. Tracks per-transaction read/write sets for accounts
// and storage slots, detects WW/RW/WR conflicts that violate serializability.
// Designed for the BAL-based parallel execution engine.
package state

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Conflict detection errors.
var (
	ErrCDTxNotFound       = errors.New("conflict_detector: transaction not found")
	ErrCDTxAlreadyExists  = errors.New("conflict_detector: transaction already registered")
	ErrCDNegativeTxIndex  = errors.New("conflict_detector: negative transaction index")
	ErrCDAlreadyFinalized = errors.New("conflict_detector: detector already finalized")
)

// ConflictKind classifies the type of conflict between two transactions.
type ConflictKind uint8

const (
	ConflictWriteWrite ConflictKind = iota // Two transactions write the same location
	ConflictReadWrite                      // Tx reads a location that another tx writes
	ConflictWriteRead                      // Tx writes a location that another tx reads
)

// String returns a human-readable label for the conflict kind.
func (ck ConflictKind) String() string {
	switch ck {
	case ConflictWriteWrite:
		return "write-write"
	case ConflictReadWrite:
		return "read-write"
	case ConflictWriteRead:
		return "write-read"
	default:
		return "unknown"
	}
}

// LocationKind distinguishes between account-level and storage-level locations.
type LocationKind uint8

const (
	LocationAccount LocationKind = iota // Account balance/nonce/code
	LocationStorage                     // Storage slot
)

// String returns a human-readable label for the location kind.
func (lk LocationKind) String() string {
	switch lk {
	case LocationAccount:
		return "account"
	case LocationStorage:
		return "storage"
	default:
		return "unknown"
	}
}

// ConflictLocation identifies a specific state location (account or storage slot).
type ConflictLocation struct {
	Kind    LocationKind
	Address types.Address
	Slot    types.Hash // only for LocationStorage
}

// String returns a human-readable description of the location.
func (cl ConflictLocation) String() string {
	if cl.Kind == LocationStorage {
		return fmt.Sprintf("storage(%s, %s)", cl.Address.Hex(), cl.Slot.Hex())
	}
	return fmt.Sprintf("account(%s)", cl.Address.Hex())
}

// Conflict describes a detected conflict between two transactions.
type Conflict struct {
	Kind     ConflictKind
	Location ConflictLocation
	TxA      int // first transaction index
	TxB      int // second transaction index
}

// String returns a human-readable description of the conflict.
func (c Conflict) String() string {
	return fmt.Sprintf("%s conflict at %s between tx[%d] and tx[%d]",
		c.Kind, c.Location, c.TxA, c.TxB)
}

// TxReadWriteSet tracks the read and write sets for a single transaction.
type TxReadWriteSet struct {
	TxIndex       int
	AccountReads  map[types.Address]struct{}
	AccountWrites map[types.Address]struct{}
	StorageReads  map[types.Address]map[types.Hash]struct{}
	StorageWrites map[types.Address]map[types.Hash]struct{}
}

// newTxReadWriteSet creates a new empty read-write set for the given tx index.
func newTxReadWriteSet(txIndex int) *TxReadWriteSet {
	return &TxReadWriteSet{
		TxIndex:       txIndex,
		AccountReads:  make(map[types.Address]struct{}),
		AccountWrites: make(map[types.Address]struct{}),
		StorageReads:  make(map[types.Address]map[types.Hash]struct{}),
		StorageWrites: make(map[types.Address]map[types.Hash]struct{}),
	}
}

// ReadAccount marks an account as read.
func (rw *TxReadWriteSet) ReadAccount(addr types.Address) { rw.AccountReads[addr] = struct{}{} }

// WriteAccount marks an account as written.
func (rw *TxReadWriteSet) WriteAccount(addr types.Address) { rw.AccountWrites[addr] = struct{}{} }

// ReadStorage marks a storage slot as read by this transaction.
func (rw *TxReadWriteSet) ReadStorage(addr types.Address, slot types.Hash) {
	if _, ok := rw.StorageReads[addr]; !ok {
		rw.StorageReads[addr] = make(map[types.Hash]struct{})
	}
	rw.StorageReads[addr][slot] = struct{}{}
}

// WriteStorage marks a storage slot as written by this transaction.
func (rw *TxReadWriteSet) WriteStorage(addr types.Address, slot types.Hash) {
	if _, ok := rw.StorageWrites[addr]; !ok {
		rw.StorageWrites[addr] = make(map[types.Hash]struct{})
	}
	rw.StorageWrites[addr][slot] = struct{}{}
}

// AccountReadCount returns the number of accounts read.
func (rw *TxReadWriteSet) AccountReadCount() int { return len(rw.AccountReads) }

// AccountWriteCount returns the number of accounts written.
func (rw *TxReadWriteSet) AccountWriteCount() int { return len(rw.AccountWrites) }

// StorageReadCount returns the total number of storage slots read.
func (rw *TxReadWriteSet) StorageReadCount() int {
	c := 0
	for _, s := range rw.StorageReads {
		c += len(s)
	}
	return c
}

// StorageWriteCount returns the total number of storage slots written.
func (rw *TxReadWriteSet) StorageWriteCount() int {
	c := 0
	for _, s := range rw.StorageWrites {
		c += len(s)
	}
	return c
}

// ConflictDetectionResult holds the results of conflict detection.
type ConflictDetectionResult struct {
	Conflicts    []Conflict
	TxCount      int
	HasConflicts bool
	WWCount      int
	RWCount      int
	WRCount      int
}

// ConflictDetector detects read-write conflicts between parallel transactions.
type ConflictDetector struct {
	mu        sync.RWMutex
	txSets    map[int]*TxReadWriteSet
	txOrder   []int
	finalized bool
}

// NewConflictDetector creates a new conflict detector.
func NewConflictDetector() *ConflictDetector {
	return &ConflictDetector{
		txSets: make(map[int]*TxReadWriteSet),
	}
}

// RegisterTx registers a new transaction for tracking.
func (cd *ConflictDetector) RegisterTx(txIndex int) (*TxReadWriteSet, error) {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	if cd.finalized {
		return nil, ErrCDAlreadyFinalized
	}
	if txIndex < 0 {
		return nil, ErrCDNegativeTxIndex
	}
	if _, exists := cd.txSets[txIndex]; exists {
		return nil, ErrCDTxAlreadyExists
	}

	rw := newTxReadWriteSet(txIndex)
	cd.txSets[txIndex] = rw
	cd.txOrder = append(cd.txOrder, txIndex)
	return rw, nil
}

// GetTxSet returns the read-write set for the given transaction index.
func (cd *ConflictDetector) GetTxSet(txIndex int) (*TxReadWriteSet, error) {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	rw, ok := cd.txSets[txIndex]
	if !ok {
		return nil, ErrCDTxNotFound
	}
	return rw, nil
}

// Detect runs conflict detection across all registered transactions.
func (cd *ConflictDetector) Detect() *ConflictDetectionResult {
	cd.mu.Lock()
	defer cd.mu.Unlock()

	cd.finalized = true

	result := &ConflictDetectionResult{
		TxCount: len(cd.txSets),
	}

	// Sort tx indices for deterministic conflict ordering.
	indices := make([]int, len(cd.txOrder))
	copy(indices, cd.txOrder)
	sort.Ints(indices)

	// Check all pairs of transactions.
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			txA := cd.txSets[indices[i]]
			txB := cd.txSets[indices[j]]
			cd.detectPairConflicts(txA, txB, result)
		}
	}

	result.HasConflicts = len(result.Conflicts) > 0
	return result
}

// detectPairConflicts detects conflicts between two transactions.
func (cd *ConflictDetector) detectPairConflicts(
	txA, txB *TxReadWriteSet,
	result *ConflictDetectionResult,
) {
	// Check account-level WW conflicts.
	for addr := range txA.AccountWrites {
		if _, ok := txB.AccountWrites[addr]; ok {
			result.Conflicts = append(result.Conflicts, Conflict{
				Kind:     ConflictWriteWrite,
				Location: ConflictLocation{Kind: LocationAccount, Address: addr},
				TxA:      txA.TxIndex,
				TxB:      txB.TxIndex,
			})
			result.WWCount++
		}
	}

	// Check account-level RW conflicts (txA reads, txB writes).
	for addr := range txA.AccountReads {
		if _, ok := txB.AccountWrites[addr]; ok {
			result.Conflicts = append(result.Conflicts, Conflict{
				Kind:     ConflictReadWrite,
				Location: ConflictLocation{Kind: LocationAccount, Address: addr},
				TxA:      txA.TxIndex,
				TxB:      txB.TxIndex,
			})
			result.RWCount++
		}
	}

	// Check account-level WR conflicts (txA writes, txB reads).
	for addr := range txA.AccountWrites {
		if _, ok := txB.AccountReads[addr]; ok {
			result.Conflicts = append(result.Conflicts, Conflict{
				Kind:     ConflictWriteRead,
				Location: ConflictLocation{Kind: LocationAccount, Address: addr},
				TxA:      txA.TxIndex,
				TxB:      txB.TxIndex,
			})
			result.WRCount++
		}
	}

	// Check storage-level WW conflicts.
	for addr, slotsA := range txA.StorageWrites {
		if slotsB, ok := txB.StorageWrites[addr]; ok {
			for slot := range slotsA {
				if _, dup := slotsB[slot]; dup {
					result.Conflicts = append(result.Conflicts, Conflict{
						Kind:     ConflictWriteWrite,
						Location: ConflictLocation{Kind: LocationStorage, Address: addr, Slot: slot},
						TxA:      txA.TxIndex,
						TxB:      txB.TxIndex,
					})
					result.WWCount++
				}
			}
		}
	}

	// Check storage-level RW conflicts (txA reads, txB writes).
	for addr, slotsA := range txA.StorageReads {
		if slotsB, ok := txB.StorageWrites[addr]; ok {
			for slot := range slotsA {
				if _, dup := slotsB[slot]; dup {
					result.Conflicts = append(result.Conflicts, Conflict{
						Kind:     ConflictReadWrite,
						Location: ConflictLocation{Kind: LocationStorage, Address: addr, Slot: slot},
						TxA:      txA.TxIndex,
						TxB:      txB.TxIndex,
					})
					result.RWCount++
				}
			}
		}
	}

	// Check storage-level WR conflicts (txA writes, txB reads).
	for addr, slotsA := range txA.StorageWrites {
		if slotsB, ok := txB.StorageReads[addr]; ok {
			for slot := range slotsA {
				if _, dup := slotsB[slot]; dup {
					result.Conflicts = append(result.Conflicts, Conflict{
						Kind:     ConflictWriteRead,
						Location: ConflictLocation{Kind: LocationStorage, Address: addr, Slot: slot},
						TxA:      txA.TxIndex,
						TxB:      txB.TxIndex,
					})
					result.WRCount++
				}
			}
		}
	}
}

// TxCount returns the number of registered transactions.
func (cd *ConflictDetector) TxCount() int {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return len(cd.txSets)
}

// IsFinalized returns true if Detect has been called.
func (cd *ConflictDetector) IsFinalized() bool {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return cd.finalized
}

// Reset clears all state, allowing the detector to be reused.
func (cd *ConflictDetector) Reset() {
	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.txSets = make(map[int]*TxReadWriteSet)
	cd.txOrder = cd.txOrder[:0]
	cd.finalized = false
}

// ConflictFreeGroups partitions transactions into conflict-free parallel groups.
func (cd *ConflictDetector) ConflictFreeGroups() [][]int {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	// Sort indices.
	indices := make([]int, len(cd.txOrder))
	copy(indices, cd.txOrder)
	sort.Ints(indices)

	if len(indices) == 0 {
		return nil
	}

	// Build a conflict graph.
	conflictsWith := make(map[int]map[int]bool)
	for _, idx := range indices {
		conflictsWith[idx] = make(map[int]bool)
	}

	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			txA := cd.txSets[indices[i]]
			txB := cd.txSets[indices[j]]
			if cd.hasPairConflict(txA, txB) {
				conflictsWith[indices[i]][indices[j]] = true
				conflictsWith[indices[j]][indices[i]] = true
			}
		}
	}

	// Greedy graph coloring to partition into conflict-free groups.
	var groups [][]int
	for _, idx := range indices {
		placed := false
		for gi, group := range groups {
			conflict := false
			for _, member := range group {
				if conflictsWith[idx][member] {
					conflict = true
					break
				}
			}
			if !conflict {
				groups[gi] = append(groups[gi], idx)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, []int{idx})
		}
	}

	return groups
}

// hasPairConflict checks if two transactions have any conflict.
func (cd *ConflictDetector) hasPairConflict(txA, txB *TxReadWriteSet) bool {
	// Account WW.
	for addr := range txA.AccountWrites {
		if _, ok := txB.AccountWrites[addr]; ok {
			return true
		}
	}
	// Account RW.
	for addr := range txA.AccountReads {
		if _, ok := txB.AccountWrites[addr]; ok {
			return true
		}
	}
	// Account WR.
	for addr := range txA.AccountWrites {
		if _, ok := txB.AccountReads[addr]; ok {
			return true
		}
	}
	// Storage WW.
	for addr, sA := range txA.StorageWrites {
		if sB, ok := txB.StorageWrites[addr]; ok {
			for slot := range sA {
				if _, dup := sB[slot]; dup {
					return true
				}
			}
		}
	}
	// Storage RW.
	for addr, sA := range txA.StorageReads {
		if sB, ok := txB.StorageWrites[addr]; ok {
			for slot := range sA {
				if _, dup := sB[slot]; dup {
					return true
				}
			}
		}
	}
	// Storage WR.
	for addr, sA := range txA.StorageWrites {
		if sB, ok := txB.StorageReads[addr]; ok {
			for slot := range sA {
				if _, dup := sB[slot]; dup {
					return true
				}
			}
		}
	}
	return false
}

// TouchedAddresses returns all addresses touched across all transactions.
func (cd *ConflictDetector) TouchedAddresses() map[types.Address]struct{} {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	addrs := make(map[types.Address]struct{})
	for _, rw := range cd.txSets {
		for addr := range rw.AccountReads {
			addrs[addr] = struct{}{}
		}
		for addr := range rw.AccountWrites {
			addrs[addr] = struct{}{}
		}
		for addr := range rw.StorageReads {
			addrs[addr] = struct{}{}
		}
		for addr := range rw.StorageWrites {
			addrs[addr] = struct{}{}
		}
	}
	return addrs
}
