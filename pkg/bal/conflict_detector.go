// conflict_detector.go implements BAL-level conflict detection for parallel
// transaction execution. It provides read-write set conflict analysis,
// dependency graph construction from BlockAccessList entries, conflict
// resolution strategies, and conflict metrics collection.
//
// This is distinct from the vm-level ConflictDetector (which tracks live
// storage accesses during optimistic execution). The BAL conflict detector
// works on pre-declared access lists to determine execution feasibility
// before any transaction runs.
package bal

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// ConflictType classifies the kind of conflict between two transactions.
type ConflictType uint8

const (
	// ConflictReadWrite means tx A reads a slot that tx B writes.
	ConflictReadWrite ConflictType = iota
	// ConflictWriteRead means tx A writes a slot that tx B reads.
	ConflictWriteRead
	// ConflictWriteWrite means both tx A and tx B write the same slot.
	ConflictWriteWrite
	// ConflictAccountLevel means both txs modify the same account's
	// balance, nonce, or code.
	ConflictAccountLevel
)

// String returns a human-readable label for the conflict type.
func (ct ConflictType) String() string {
	switch ct {
	case ConflictReadWrite:
		return "read-write"
	case ConflictWriteRead:
		return "write-read"
	case ConflictWriteWrite:
		return "write-write"
	case ConflictAccountLevel:
		return "account-level"
	default:
		return "unknown"
	}
}

// Conflict records a single conflict between two transactions identified
// by their BAL access indices. TxA always has the lower index.
type Conflict struct {
	TxA      int
	TxB      int
	Type     ConflictType
	Address  types.Address
	Slot     types.Hash // zero for account-level conflicts
}

// ResolutionStrategy determines how to handle detected conflicts.
type ResolutionStrategy uint8

const (
	// StrategySerialize forces conflicting transactions to execute sequentially
	// in their original block order.
	StrategySerialize ResolutionStrategy = iota
	// StrategyAbort aborts the later transaction and marks it for re-inclusion
	// in a future block.
	StrategyAbort
	// StrategyRetry re-executes the later transaction after the earlier one
	// completes, using updated state.
	StrategyRetry
)

// String returns a human-readable label for the resolution strategy.
func (rs ResolutionStrategy) String() string {
	switch rs {
	case StrategySerialize:
		return "serialize"
	case StrategyAbort:
		return "abort"
	case StrategyRetry:
		return "retry"
	default:
		return "unknown"
	}
}

// ConflictMetrics collects statistics about conflict detection runs.
type ConflictMetrics struct {
	TotalPairs       atomic.Uint64 // total tx pairs analyzed
	ConflictsFound   atomic.Uint64 // total conflicts detected
	ReadWriteCount   atomic.Uint64
	WriteReadCount   atomic.Uint64
	WriteWriteCount  atomic.Uint64
	AccountConflicts atomic.Uint64
	ParallelFeasible atomic.Uint64 // runs where parallel execution is feasible
	SerialRequired   atomic.Uint64 // runs where all txs must serialize
}

// Snapshot returns a copy of the current metric values.
func (cm *ConflictMetrics) Snapshot() ConflictMetricsSnapshot {
	return ConflictMetricsSnapshot{
		TotalPairs:       cm.TotalPairs.Load(),
		ConflictsFound:   cm.ConflictsFound.Load(),
		ReadWriteCount:   cm.ReadWriteCount.Load(),
		WriteReadCount:   cm.WriteReadCount.Load(),
		WriteWriteCount:  cm.WriteWriteCount.Load(),
		AccountConflicts: cm.AccountConflicts.Load(),
		ParallelFeasible: cm.ParallelFeasible.Load(),
		SerialRequired:   cm.SerialRequired.Load(),
	}
}

// ConflictMetricsSnapshot is an immutable snapshot of conflict metrics.
type ConflictMetricsSnapshot struct {
	TotalPairs       uint64
	ConflictsFound   uint64
	ReadWriteCount   uint64
	WriteReadCount   uint64
	WriteWriteCount  uint64
	AccountConflicts uint64
	ParallelFeasible uint64
	SerialRequired   uint64
}

// slotLocation uniquely identifies a storage slot across addresses.
type slotLocation struct {
	Addr types.Address
	Slot types.Hash
}

// txRWSet holds the read set, write set, and account-level write flag
// for a single transaction extracted from BlockAccessList entries.
type txRWSet struct {
	reads         map[slotLocation]struct{}
	writes        map[slotLocation]struct{}
	accountWrites map[types.Address]struct{} // balance/nonce/code changes
}

// BALConflictDetector analyzes BlockAccessList entries to detect conflicts,
// build dependency graphs, and determine parallel execution feasibility.
type BALConflictDetector struct {
	mu       sync.RWMutex
	strategy ResolutionStrategy
	metrics  ConflictMetrics
}

// NewBALConflictDetector creates a detector with the given resolution strategy.
func NewBALConflictDetector(strategy ResolutionStrategy) *BALConflictDetector {
	return &BALConflictDetector{
		strategy: strategy,
	}
}

// Strategy returns the current conflict resolution strategy.
func (d *BALConflictDetector) Strategy() ResolutionStrategy {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.strategy
}

// SetStrategy updates the conflict resolution strategy.
func (d *BALConflictDetector) SetStrategy(s ResolutionStrategy) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.strategy = s
}

// Metrics returns a reference to the detector's metrics collector.
func (d *BALConflictDetector) Metrics() *ConflictMetrics {
	return &d.metrics
}

// extractRWSets builds per-transaction read/write sets from a BlockAccessList.
// Only entries with AccessIndex >= 1 (actual transactions) are included.
func extractRWSets(bal *BlockAccessList) map[int]*txRWSet {
	sets := make(map[int]*txRWSet)
	for _, entry := range bal.Entries {
		if entry.AccessIndex == 0 {
			continue
		}
		txIdx := int(entry.AccessIndex) - 1

		rw, ok := sets[txIdx]
		if !ok {
			rw = &txRWSet{
				reads:         make(map[slotLocation]struct{}),
				writes:        make(map[slotLocation]struct{}),
				accountWrites: make(map[types.Address]struct{}),
			}
			sets[txIdx] = rw
		}

		for _, sr := range entry.StorageReads {
			loc := slotLocation{Addr: entry.Address, Slot: sr.Slot}
			rw.reads[loc] = struct{}{}
		}
		for _, sc := range entry.StorageChanges {
			loc := slotLocation{Addr: entry.Address, Slot: sc.Slot}
			rw.writes[loc] = struct{}{}
		}
		if entry.BalanceChange != nil || entry.NonceChange != nil || entry.CodeChange != nil {
			rw.accountWrites[entry.Address] = struct{}{}
		}
	}
	return sets
}

// DetectConflicts analyzes a BlockAccessList and returns all conflicts
// between transactions. Conflicts are returned sorted by (TxA, TxB).
func (d *BALConflictDetector) DetectConflicts(bal *BlockAccessList) []Conflict {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}

	sets := extractRWSets(bal)
	if len(sets) == 0 {
		return nil
	}

	// Collect and sort transaction indices for deterministic ordering.
	indices := make([]int, 0, len(sets))
	for idx := range sets {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var conflicts []Conflict

	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			txA, txB := indices[i], indices[j]
			setA, setB := sets[txA], sets[txB]
			d.metrics.TotalPairs.Add(1)

			found := d.detectPairConflicts(txA, txB, setA, setB)
			conflicts = append(conflicts, found...)
		}
	}

	return conflicts
}

// detectPairConflicts finds all conflicts between two transaction RW sets.
func (d *BALConflictDetector) detectPairConflicts(
	txA, txB int, setA, setB *txRWSet,
) []Conflict {
	var conflicts []Conflict

	// Write-write conflicts on storage slots.
	for loc := range setA.writes {
		if _, ok := setB.writes[loc]; ok {
			conflicts = append(conflicts, Conflict{
				TxA: txA, TxB: txB,
				Type:    ConflictWriteWrite,
				Address: loc.Addr,
				Slot:    loc.Slot,
			})
			d.metrics.WriteWriteCount.Add(1)
			d.metrics.ConflictsFound.Add(1)
		}
	}

	// Read-write conflicts: A reads, B writes.
	for loc := range setA.reads {
		if _, ok := setB.writes[loc]; ok {
			conflicts = append(conflicts, Conflict{
				TxA: txA, TxB: txB,
				Type:    ConflictReadWrite,
				Address: loc.Addr,
				Slot:    loc.Slot,
			})
			d.metrics.ReadWriteCount.Add(1)
			d.metrics.ConflictsFound.Add(1)
		}
	}

	// Write-read conflicts: A writes, B reads.
	for loc := range setA.writes {
		if _, ok := setB.reads[loc]; ok {
			conflicts = append(conflicts, Conflict{
				TxA: txA, TxB: txB,
				Type:    ConflictWriteRead,
				Address: loc.Addr,
				Slot:    loc.Slot,
			})
			d.metrics.WriteReadCount.Add(1)
			d.metrics.ConflictsFound.Add(1)
		}
	}

	// Account-level conflicts: both modify the same account.
	for addr := range setA.accountWrites {
		if _, ok := setB.accountWrites[addr]; ok {
			conflicts = append(conflicts, Conflict{
				TxA: txA, TxB: txB,
				Type:    ConflictAccountLevel,
				Address: addr,
			})
			d.metrics.AccountConflicts.Add(1)
			d.metrics.ConflictsFound.Add(1)
		}
	}

	return conflicts
}

// IsParallelFeasible returns true if at least two transactions in the BAL
// can execute in parallel (i.e., not every pair conflicts).
func (d *BALConflictDetector) IsParallelFeasible(bal *BlockAccessList) bool {
	if bal == nil {
		return false
	}
	sets := extractRWSets(bal)
	if len(sets) < 2 {
		d.metrics.SerialRequired.Add(1)
		return false
	}

	indices := make([]int, 0, len(sets))
	for idx := range sets {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	// Check if any pair is conflict-free.
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			pair := d.detectPairConflicts(indices[i], indices[j], sets[indices[i]], sets[indices[j]])
			if len(pair) == 0 {
				d.metrics.ParallelFeasible.Add(1)
				return true
			}
		}
	}

	d.metrics.SerialRequired.Add(1)
	return false
}

// BuildDependencyGraph constructs a directed acyclic graph from conflicts.
// Each edge goes from an earlier transaction to a later transaction that
// depends on it. The graph maps each tx index to its list of dependencies
// (predecessors that must finish before it can start).
func (d *BALConflictDetector) BuildDependencyGraph(bal *BlockAccessList) map[int][]int {
	conflicts := d.DetectConflicts(bal)
	sets := extractRWSets(bal)

	graph := make(map[int][]int)
	for idx := range sets {
		graph[idx] = nil
	}

	// Deduplicate edges: for each (TxA, TxB) pair, add at most one edge.
	seen := make(map[[2]int]struct{})
	for _, c := range conflicts {
		edge := [2]int{c.TxA, c.TxB}
		if _, ok := seen[edge]; ok {
			continue
		}
		seen[edge] = struct{}{}
		graph[c.TxB] = append(graph[c.TxB], c.TxA)
	}

	// Sort dependency lists for determinism.
	for idx := range graph {
		if len(graph[idx]) > 1 {
			sort.Ints(graph[idx])
		}
	}

	return graph
}

// ResolveConflicts applies the detector's resolution strategy to a set of
// conflicts and returns a mapping of transaction index to resolution action.
// Actions: "execute" means proceed normally, "serialize" means wait for deps,
// "abort" means skip this tx, "retry" means re-execute after dependencies.
func (d *BALConflictDetector) ResolveConflicts(conflicts []Conflict) map[int]string {
	d.mu.RLock()
	strategy := d.strategy
	d.mu.RUnlock()

	actions := make(map[int]string)

	// Collect all transaction indices involved in conflicts.
	conflicting := make(map[int]struct{})
	for _, c := range conflicts {
		conflicting[c.TxA] = struct{}{}
		conflicting[c.TxB] = struct{}{}
	}

	switch strategy {
	case StrategySerialize:
		// The later transaction in each conflict pair must wait.
		for _, c := range conflicts {
			actions[c.TxA] = "execute"
			actions[c.TxB] = "serialize"
		}
	case StrategyAbort:
		// The later transaction in each conflict pair is aborted.
		for _, c := range conflicts {
			actions[c.TxA] = "execute"
			actions[c.TxB] = "abort"
		}
	case StrategyRetry:
		// The later transaction is executed speculatively and retried on conflict.
		for _, c := range conflicts {
			actions[c.TxA] = "execute"
			actions[c.TxB] = "retry"
		}
	}

	return actions
}

// ConflictRate returns the fraction of tx pairs that have conflicts.
// Returns 0.0 if no pairs have been analyzed.
func (d *BALConflictDetector) ConflictRate() float64 {
	total := d.metrics.TotalPairs.Load()
	if total == 0 {
		return 0.0
	}
	return float64(d.metrics.ConflictsFound.Load()) / float64(total)
}
