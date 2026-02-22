// parallel_executor_deep.go extends the parallel executor with EIP-7928 BAL
// integration, dependency graph analysis, execution grouping, speculative
// execution with rollback, and state merge for gigagas throughput.
package vm

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Extended parallel executor errors.
var (
	ErrDepGraphEmpty       = errors.New("depgraph: no transactions provided")
	ErrDepGraphNilTx       = errors.New("depgraph: nil transaction in slice")
	ErrExecGroupEmpty      = errors.New("execgroup: empty execution group")
	ErrSpeculativeRollback = errors.New("speculative: execution rolled back due to conflict")
	ErrMergeStateNil       = errors.New("merge: nil state database")
	ErrMergeConflict       = errors.New("merge: conflicting writes during merge")
)

// AccessPair represents a single address+slot that a transaction accesses.
type AccessPair struct {
	Address types.Address
	Slot    types.Hash
	IsWrite bool
}

// TxAccessProfile summarizes the read/write access set for a transaction,
// derived from EIP-7928 Block Access Lists.
type TxAccessProfile struct {
	TxIndex int
	Reads   []AccessPair
	Writes  []AccessPair
}

// DependencyEdge represents a dependency from one tx to another.
type DependencyEdge struct {
	From int    // tx index that must execute first
	To   int    // tx index that depends on From
	Kind string // "RAW", "WAW", or "WAR"
}

// DependencyGraph represents the dependency relationships between transactions.
type DependencyGraph struct {
	NumTxs int
	Edges  []DependencyEdge
	// Adjacency list: tx -> list of txs that depend on it.
	Successors map[int][]int
	// InDegree tracks how many txs each tx depends on.
	InDegree []int
}

// DependencyAnalyzer builds a dependency graph from transaction access profiles.
type DependencyAnalyzer struct {
	mu       sync.Mutex
	profiles []TxAccessProfile
}

// NewDependencyAnalyzer creates a new analyzer.
func NewDependencyAnalyzer() *DependencyAnalyzer {
	return &DependencyAnalyzer{}
}

// AddProfile adds a transaction's access profile.
func (da *DependencyAnalyzer) AddProfile(p TxAccessProfile) {
	da.mu.Lock()
	da.profiles = append(da.profiles, p)
	da.mu.Unlock()
}

// ProfileCount returns the number of registered profiles.
func (da *DependencyAnalyzer) ProfileCount() int {
	da.mu.Lock()
	defer da.mu.Unlock()
	return len(da.profiles)
}

// BuildGraph builds the dependency graph from all registered profiles.
// It identifies RAW (read-after-write), WAW (write-after-write), and
// WAR (write-after-read) dependencies between transactions.
func (da *DependencyAnalyzer) BuildGraph() (*DependencyGraph, error) {
	da.mu.Lock()
	defer da.mu.Unlock()

	n := len(da.profiles)
	if n == 0 {
		return nil, ErrDepGraphEmpty
	}

	graph := &DependencyGraph{
		NumTxs:     n,
		Successors: make(map[int][]int),
		InDegree:   make([]int, n),
	}

	type slotKey struct {
		addr types.Address
		slot types.Hash
	}

	// Build maps: slot -> list of (txIndex, isWrite).
	type access struct {
		txIdx   int
		isWrite bool
	}
	slotAccess := make(map[slotKey][]access)

	for _, p := range da.profiles {
		for _, r := range p.Reads {
			sk := slotKey{addr: r.Address, slot: r.Slot}
			slotAccess[sk] = append(slotAccess[sk], access{txIdx: p.TxIndex, isWrite: false})
		}
		for _, w := range p.Writes {
			sk := slotKey{addr: w.Address, slot: w.Slot}
			slotAccess[sk] = append(slotAccess[sk], access{txIdx: p.TxIndex, isWrite: true})
		}
	}

	// Deduplicate edges.
	edgeSet := make(map[[2]int]string)

	for _, accesses := range slotAccess {
		for i := 0; i < len(accesses); i++ {
			for j := i + 1; j < len(accesses); j++ {
				a, b := accesses[i], accesses[j]
				if a.txIdx == b.txIdx {
					continue
				}
				// Ensure ordering: earlier tx is "from", later tx is "to".
				from, to := a, b
				if from.txIdx > to.txIdx {
					from, to = to, from
				}

				if !from.isWrite && !to.isWrite {
					continue // RR: no dependency.
				}

				key := [2]int{from.txIdx, to.txIdx}
				var kind string
				if from.isWrite && !to.isWrite {
					kind = "RAW"
				} else if from.isWrite && to.isWrite {
					kind = "WAW"
				} else {
					kind = "WAR"
				}

				// Keep the strongest dependency kind.
				if existing, ok := edgeSet[key]; ok {
					if kind == "RAW" || (kind == "WAW" && existing == "WAR") {
						edgeSet[key] = kind
					}
				} else {
					edgeSet[key] = kind
				}
			}
		}
	}

	for key, kind := range edgeSet {
		edge := DependencyEdge{From: key[0], To: key[1], Kind: kind}
		graph.Edges = append(graph.Edges, edge)
		graph.Successors[key[0]] = append(graph.Successors[key[0]], key[1])
		graph.InDegree[key[1]]++
	}

	return graph, nil
}

// ExecutionGroup represents a set of transactions that can execute in parallel
// because they have no mutual dependencies.
type ExecutionGroup struct {
	Level     int   // execution level (0 = first batch, 1 = second, etc.)
	TxIndices []int // transaction indices in this group
}

// BuildExecutionGroups uses topological sorting on the dependency graph to
// produce ordered groups of non-conflicting transactions. Each group can
// be executed fully in parallel, and groups are executed sequentially.
func BuildExecutionGroups(graph *DependencyGraph) ([]ExecutionGroup, error) {
	if graph == nil || graph.NumTxs == 0 {
		return nil, ErrExecGroupEmpty
	}

	inDegree := make([]int, graph.NumTxs)
	copy(inDegree, graph.InDegree)

	var groups []ExecutionGroup
	level := 0
	remaining := graph.NumTxs

	for remaining > 0 {
		// Collect all txs with zero in-degree.
		var ready []int
		for i := 0; i < graph.NumTxs; i++ {
			if inDegree[i] == 0 {
				ready = append(ready, i)
			}
		}

		if len(ready) == 0 {
			// Cycle detected or all processed.
			break
		}

		groups = append(groups, ExecutionGroup{
			Level:     level,
			TxIndices: ready,
		})

		// Remove processed txs from the graph.
		for _, idx := range ready {
			inDegree[idx] = -1 // mark as processed
			remaining--
			for _, succ := range graph.Successors[idx] {
				if inDegree[succ] > 0 {
					inDegree[succ]--
				}
			}
		}
		level++
	}

	return groups, nil
}

// SpeculativeResult holds the result of speculative execution along with
// a snapshot ID for potential rollback.
type SpeculativeResult struct {
	TxIndex    int
	Receipt    *types.Receipt
	GasUsed    uint64
	ReadSet    []StorageAccess
	WriteSet   []StorageAccess
	SnapshotID int
	RolledBack bool
	Err        error
}

// SpeculativeExecutor runs transactions optimistically against snapshot-capable
// state, detecting conflicts and rolling back as needed.
type SpeculativeExecutor struct {
	mu       sync.Mutex
	detector *ConflictDetector
	results  map[int]*SpeculativeResult
}

// NewSpeculativeExecutor creates a new speculative executor.
func NewSpeculativeExecutor() *SpeculativeExecutor {
	return &SpeculativeExecutor{
		detector: NewConflictDetector(),
		results:  make(map[int]*SpeculativeResult),
	}
}

// Execute runs a transaction speculatively against the state.
func (se *SpeculativeExecutor) Execute(
	txIndex int, tx *types.Transaction, stateDB StateDB,
) *SpeculativeResult {
	se.mu.Lock()
	defer se.mu.Unlock()

	snap := stateDB.Snapshot()

	tracker := newTrackingStateDB(stateDB, txIndex, se.detector)

	// Simulate execution.
	gasUsed := uint64(21_000)
	data := tx.Data()
	for _, b := range data {
		if b == 0 {
			gasUsed += 4
		} else {
			gasUsed += 16
		}
	}

	txGas := tx.Gas()
	var execErr error
	if gasUsed > txGas {
		gasUsed = txGas
		execErr = ErrOutOfGas
	}

	status := types.ReceiptStatusSuccessful
	if execErr != nil {
		status = types.ReceiptStatusFailed
	}
	receipt := types.NewReceipt(status, 0)
	receipt.GasUsed = gasUsed

	result := &SpeculativeResult{
		TxIndex:    txIndex,
		Receipt:    receipt,
		GasUsed:    gasUsed,
		ReadSet:    tracker.ReadSet(),
		WriteSet:   tracker.WriteSet(),
		SnapshotID: snap,
		Err:        execErr,
	}

	se.results[txIndex] = result
	return result
}

// Rollback reverts a speculative execution and marks it as rolled back.
func (se *SpeculativeExecutor) Rollback(txIndex int, stateDB StateDB) error {
	se.mu.Lock()
	defer se.mu.Unlock()

	result, ok := se.results[txIndex]
	if !ok {
		return fmt.Errorf("speculative: no result for tx %d", txIndex)
	}
	stateDB.RevertToSnapshot(result.SnapshotID)
	result.RolledBack = true
	return nil
}

// Results returns all speculative results.
func (se *SpeculativeExecutor) Results() map[int]*SpeculativeResult {
	se.mu.Lock()
	defer se.mu.Unlock()
	cp := make(map[int]*SpeculativeResult, len(se.results))
	for k, v := range se.results {
		cp[k] = v
	}
	return cp
}

// StateDelta represents accumulated state changes from parallel execution.
type StateDelta struct {
	mu     sync.Mutex
	writes map[storageKey]types.Hash
	count  int
}

// NewStateDelta creates an empty state delta.
func NewStateDelta() *StateDelta {
	return &StateDelta{
		writes: make(map[storageKey]types.Hash),
	}
}

// Record records a state write.
func (sd *StateDelta) Record(addr types.Address, key, val types.Hash) {
	sd.mu.Lock()
	sd.writes[storageKey{addr: addr, key: key}] = val
	sd.count++
	sd.mu.Unlock()
}

// Len returns the number of recorded writes.
func (sd *StateDelta) Len() int {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.count
}

// MergeResults applies a set of state deltas into a target StateDB.
// Returns an error if any write conflicts are detected between deltas.
func MergeResults(deltas []*StateDelta, target StateDB) error {
	if target == nil {
		return ErrMergeStateNil
	}

	// Check for write conflicts: same key written by different deltas.
	seen := make(map[storageKey]int) // key -> delta index
	for i, d := range deltas {
		d.mu.Lock()
		for sk := range d.writes {
			if prev, ok := seen[sk]; ok && prev != i {
				d.mu.Unlock()
				return fmt.Errorf("%w: key %v written by deltas %d and %d",
					ErrMergeConflict, sk, prev, i)
			}
			seen[sk] = i
		}
		d.mu.Unlock()
	}

	// Apply all writes to target.
	for _, d := range deltas {
		d.mu.Lock()
		for sk, val := range d.writes {
			target.SetState(sk.addr, sk.key, val)
		}
		d.mu.Unlock()
	}

	return nil
}

// AnalyzeParallelism computes the theoretical speedup from parallel execution
// given the dependency graph. Returns (maxParallelism, numLevels, speedup).
func AnalyzeParallelism(graph *DependencyGraph) (int, int, float64) {
	if graph == nil || graph.NumTxs == 0 {
		return 0, 0, 0
	}

	groups, err := BuildExecutionGroups(graph)
	if err != nil || len(groups) == 0 {
		return 1, 1, 1.0
	}

	maxPar := 0
	for _, g := range groups {
		if len(g.TxIndices) > maxPar {
			maxPar = len(g.TxIndices)
		}
	}

	numLevels := len(groups)
	speedup := float64(graph.NumTxs) / float64(numLevels)

	return maxPar, numLevels, speedup
}
