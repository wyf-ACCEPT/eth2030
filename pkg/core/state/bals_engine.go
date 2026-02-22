// Package state provides the BALS (Block Access Lists) parallel execution engine.
// This file implements conflict detection, dependency graph construction, and
// parallel transaction execution using block access lists (EIP-7928).
// Part of the EL Sustainability roadmap: BALS.
package state

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

var (
	ErrEmptyAccessLists    = errors.New("bals: empty access lists")
	ErrNilTransaction      = errors.New("bals: nil transaction")
	ErrCyclicGraph         = errors.New("bals: dependency graph contains a cycle")
	ErrBALSIndexOutOfRange = errors.New("bals: transaction index out of range")
)

// BALSTx represents a transaction with its index in the block and metadata
// needed for parallel execution.
type BALSTx struct {
	Index    int
	GasLimit uint64
	Data     []byte
}

// BALSAccessList declares the storage slots a transaction reads and writes.
// This information is used to detect conflicts and build a dependency graph
// for parallel execution.
type BALSAccessList struct {
	TxIndex  int
	ReadSet  []types.Hash
	WriteSet []types.Hash
}

// ConflictEdge represents a conflict between two transactions.
// TxA has a lower index than TxB.
type ConflictEdge struct {
	TxA    int
	TxB    int
	Reason string // "write-write", "read-write", or "write-read"
}

// DependencyGraph maps each transaction index to the set of transaction
// indices it depends on (must execute after).
type DependencyGraph map[int][]int

// ExecutionResult holds the outcome of parallel transaction execution.
type ExecutionResult struct {
	TxIndex    int
	GasUsed    uint64
	Success    bool
	OutputData []byte
}

// BALSEngine manages parallel transaction execution using block access lists.
type BALSEngine struct {
	mu          sync.RWMutex
	maxParallel int
}

// NewBALSEngine creates a new BALS execution engine.
// maxParallel controls the maximum degree of parallelism.
func NewBALSEngine(maxParallel int) *BALSEngine {
	if maxParallel < 1 {
		maxParallel = 1
	}
	return &BALSEngine{
		maxParallel: maxParallel,
	}
}

// ExecuteParallel executes transactions in parallel where possible, based on
// their access lists. Non-conflicting transactions run concurrently; dependent
// transactions execute in topological order. Returns results for each transaction.
func (e *BALSEngine) ExecuteParallel(txs []BALSTx, accessLists []BALSAccessList) ([]ExecutionResult, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	if len(accessLists) == 0 {
		return nil, ErrEmptyAccessLists
	}

	// Build dependency graph from access lists.
	graph := BuildDependencyGraph(accessLists)

	// Get execution order.
	order, err := TopologicalSort(graph, len(txs))
	if err != nil {
		return nil, err
	}

	// Group transactions into levels (each level can run in parallel).
	levels := buildExecutionLevels(order, graph)

	results := make([]ExecutionResult, len(txs))

	// Execute each level in parallel.
	for _, level := range levels {
		var wg sync.WaitGroup
		sem := make(chan struct{}, e.maxParallel)

		for _, txIdx := range level {
			if txIdx < 0 || txIdx >= len(txs) {
				continue
			}
			wg.Add(1)
			sem <- struct{}{}

			go func(idx int) {
				defer wg.Done()
				defer func() { <-sem }()

				tx := txs[idx]
				// Simulate execution: gas used is proportional to data length.
				gasUsed := uint64(21000) // base gas
				if len(tx.Data) > 0 {
					gasUsed += uint64(len(tx.Data)) * 16 // calldata cost
				}
				if gasUsed > tx.GasLimit {
					gasUsed = tx.GasLimit
				}

				results[idx] = ExecutionResult{
					TxIndex:    idx,
					GasUsed:    gasUsed,
					Success:    gasUsed <= tx.GasLimit,
					OutputData: tx.Data,
				}
			}(txIdx)
		}
		wg.Wait()
	}

	return results, nil
}

// DetectConflicts analyzes access lists and returns all conflict edges.
// Two transactions conflict if:
//   - Both write to the same slot (write-write conflict)
//   - One reads and the other writes the same slot (read-write / write-read)
func DetectConflicts(accessLists []BALSAccessList) []ConflictEdge {
	var conflicts []ConflictEdge

	for i := 0; i < len(accessLists); i++ {
		for j := i + 1; j < len(accessLists); j++ {
			a := accessLists[i]
			b := accessLists[j]

			txA := a.TxIndex
			txB := b.TxIndex
			if txA > txB {
				txA, txB = txB, txA
			}

			// Write-write conflicts.
			if hasOverlap(a.WriteSet, b.WriteSet) {
				conflicts = append(conflicts, ConflictEdge{
					TxA: txA, TxB: txB, Reason: "write-write",
				})
				continue // one conflict edge per pair
			}

			// Read-write conflicts (a reads, b writes).
			if hasOverlap(a.ReadSet, b.WriteSet) {
				conflicts = append(conflicts, ConflictEdge{
					TxA: txA, TxB: txB, Reason: "read-write",
				})
				continue
			}

			// Write-read conflicts (a writes, b reads).
			if hasOverlap(a.WriteSet, b.ReadSet) {
				conflicts = append(conflicts, ConflictEdge{
					TxA: txA, TxB: txB, Reason: "write-read",
				})
			}
		}
	}

	return conflicts
}

// BuildDependencyGraph constructs a DAG from the access lists.
// For each pair of conflicting transactions, the later one (by TxIndex)
// depends on the earlier one.
func BuildDependencyGraph(accessLists []BALSAccessList) DependencyGraph {
	graph := make(DependencyGraph)

	// Ensure all tx indices are in the graph.
	for _, al := range accessLists {
		if _, ok := graph[al.TxIndex]; !ok {
			graph[al.TxIndex] = nil
		}
	}

	conflicts := DetectConflicts(accessLists)
	for _, c := range conflicts {
		// TxB depends on TxA (TxA < TxB).
		graph[c.TxB] = append(graph[c.TxB], c.TxA)
	}

	return graph
}

// TopologicalSort produces a valid execution order respecting all dependencies
// using Kahn's algorithm. Returns ErrCyclicGraph if the graph has a cycle.
func TopologicalSort(graph DependencyGraph, numTxs int) ([]int, error) {
	// Compute in-degree for each node.
	inDegree := make(map[int]int)
	adjList := make(map[int][]int) // forward edges: dependency -> dependent

	// Initialize all nodes.
	for i := 0; i < numTxs; i++ {
		inDegree[i] = 0
	}
	for node := range graph {
		inDegree[node] = 0
	}

	// Build forward adjacency and count in-degrees.
	for node, deps := range graph {
		for _, dep := range deps {
			adjList[dep] = append(adjList[dep], node)
			inDegree[node]++
		}
	}

	// Initialize queue with zero in-degree nodes, sorted for determinism.
	var queue []int
	for node, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, node)
		}
	}
	sort.Ints(queue)

	var order []int
	for len(queue) > 0 {
		// Pop front.
		node := queue[0]
		queue = queue[1:]
		order = append(order, node)

		// Process neighbors.
		neighbors := adjList[node]
		sort.Ints(neighbors)
		for _, next := range neighbors {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
		// Re-sort to maintain deterministic order.
		sort.Ints(queue)
	}

	if len(order) != len(inDegree) {
		return nil, ErrCyclicGraph
	}

	return order, nil
}

// ComputeParallelGain estimates the speedup factor from parallel execution
// based on the conflict graph. Returns a value >= 1.0 where higher means
// more parallelism. 1.0 means fully serial execution.
func ComputeParallelGain(accessLists []BALSAccessList) float64 {
	if len(accessLists) <= 1 {
		return 1.0
	}

	graph := BuildDependencyGraph(accessLists)
	numTxs := len(accessLists)

	order, err := TopologicalSort(graph, numTxs)
	if err != nil {
		return 1.0
	}

	levels := buildExecutionLevels(order, graph)
	if len(levels) == 0 {
		return 1.0
	}

	// Speedup = total_txs / number_of_levels.
	// Fully parallel: numTxs levels = 1 -> speedup = numTxs.
	// Fully serial: numTxs levels -> speedup = 1.
	return float64(numTxs) / float64(len(levels))
}

// buildExecutionLevels groups transactions into levels where all transactions
// in a level can execute in parallel. A transaction is in the earliest level
// after all its dependencies have been placed.
func buildExecutionLevels(order []int, graph DependencyGraph) [][]int {
	if len(order) == 0 {
		return nil
	}

	// Compute the level of each node: level = max(level of deps) + 1.
	level := make(map[int]int)
	for _, node := range order {
		maxDepLevel := -1
		for _, dep := range graph[node] {
			if l, ok := level[dep]; ok && l > maxDepLevel {
				maxDepLevel = l
			}
		}
		level[node] = maxDepLevel + 1
	}

	// Group nodes by level.
	maxLevel := 0
	for _, l := range level {
		if l > maxLevel {
			maxLevel = l
		}
	}

	levels := make([][]int, maxLevel+1)
	for _, node := range order {
		l := level[node]
		levels[l] = append(levels[l], node)
	}

	return levels
}

// hasOverlap checks if two hash sets have any common element.
func hasOverlap(a, b []types.Hash) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	set := make(map[types.Hash]struct{}, len(a))
	for _, h := range a {
		set[h] = struct{}{}
	}
	for _, h := range b {
		if _, ok := set[h]; ok {
			return true
		}
	}
	return false
}

// MaxParallel returns the configured parallelism limit.
func (e *BALSEngine) MaxParallel() int {
	return e.maxParallel
}
