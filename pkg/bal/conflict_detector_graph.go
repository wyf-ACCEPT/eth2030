// conflict_detector_graph.go extends BAL conflict detection with graph-based
// analysis for parallel transaction execution. It builds on the existing
// BALConflictDetector and DependencyGraph types to provide:
//   - Per-transaction-pair independence checks
//   - Parallel group identification using topological layering
//   - Speedup estimation for parallel execution
//   - Critical path analysis for scheduling optimization
//
// This complements conflict_detector.go (pair-wise conflict detection) and
// conflict_detector_advanced.go (clustering, scoring, reordering) by adding
// graph-algorithmic features needed by the parallel execution engine.
package bal

import (
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// GraphConflictAnalyzer provides graph-based conflict analysis for BALs.
// It wraps a BALConflictDetector and extends it with dependency graph
// operations, parallel group computation, and scheduling analysis.
type GraphConflictAnalyzer struct {
	detector *BALConflictDetector
}

// NewGraphConflictAnalyzer creates a graph analyzer wrapping the given detector.
func NewGraphConflictAnalyzer(detector *BALConflictDetector) *GraphConflictAnalyzer {
	return &GraphConflictAnalyzer{detector: detector}
}

// Detector returns the underlying conflict detector.
func (g *GraphConflictAnalyzer) Detector() *BALConflictDetector {
	return g.detector
}

// DetectConflictsFromBALs analyzes individual transaction BALs (one per tx)
// for pairwise conflicts. Each BAL in the slice represents a single tx's
// access list with AccessIndex=1 for all entries.
func (g *GraphConflictAnalyzer) DetectConflictsFromBALs(txBALs []*BlockAccessList) []Conflict {
	if len(txBALs) == 0 {
		return nil
	}

	// Extract per-tx read/write sets from each individual BAL.
	sets := make(map[int]*txRWSet, len(txBALs))
	for i, bal := range txBALs {
		if bal == nil {
			continue
		}
		rw := &txRWSet{
			reads:         make(map[slotLocation]struct{}),
			writes:        make(map[slotLocation]struct{}),
			accountWrites: make(map[types.Address]struct{}),
		}
		for _, entry := range bal.Entries {
			for _, sr := range entry.StorageReads {
				rw.reads[slotLocation{Addr: entry.Address, Slot: sr.Slot}] = struct{}{}
			}
			for _, sc := range entry.StorageChanges {
				rw.writes[slotLocation{Addr: entry.Address, Slot: sc.Slot}] = struct{}{}
			}
			if entry.BalanceChange != nil || entry.NonceChange != nil || entry.CodeChange != nil {
				rw.accountWrites[entry.Address] = struct{}{}
			}
		}
		sets[i] = rw
	}

	if len(sets) < 2 {
		return nil
	}

	indices := make([]int, 0, len(sets))
	for idx := range sets {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	var conflicts []Conflict
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			txA, txB := indices[i], indices[j]
			found := g.detector.detectPairConflicts(txA, txB, sets[txA], sets[txB])
			conflicts = append(conflicts, found...)
		}
	}
	return conflicts
}

// BuildDependencyGraphFromBALs creates a DependencyGraph from individual
// per-transaction BALs. Each BAL represents one transaction's state accesses.
func (g *GraphConflictAnalyzer) BuildDependencyGraphFromBALs(txBALs []*BlockAccessList) *DependencyGraph {
	conflicts := g.DetectConflictsFromBALs(txBALs)
	numTx := len(txBALs)
	return BuildDependencyGraphFromConflicts(conflicts, numTx)
}

// CanParallelize checks whether two transactions (identified by their BALs)
// can execute in parallel. Two transactions are independent if they have
// no read-write, write-read, write-write, or account-level conflicts.
func (g *GraphConflictAnalyzer) CanParallelize(bal1, bal2 *BlockAccessList) bool {
	if bal1 == nil || bal2 == nil {
		return true // nil BAL means no accesses, so no conflicts.
	}

	rw1 := extractSingleBALRWSet(bal1)
	rw2 := extractSingleBALRWSet(bal2)

	if rw1 == nil || rw2 == nil {
		return true
	}

	// Check write-write.
	for loc := range rw1.writes {
		if _, ok := rw2.writes[loc]; ok {
			return false
		}
	}
	// Check read-write (rw1 reads, rw2 writes).
	for loc := range rw1.reads {
		if _, ok := rw2.writes[loc]; ok {
			return false
		}
	}
	// Check write-read (rw1 writes, rw2 reads).
	for loc := range rw1.writes {
		if _, ok := rw2.reads[loc]; ok {
			return false
		}
	}
	// Check account-level.
	for addr := range rw1.accountWrites {
		if _, ok := rw2.accountWrites[addr]; ok {
			return false
		}
	}
	return true
}

// extractSingleBALRWSet extracts a read/write set from a single-tx BAL.
func extractSingleBALRWSet(bal *BlockAccessList) *txRWSet {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}
	rw := &txRWSet{
		reads:         make(map[slotLocation]struct{}),
		writes:        make(map[slotLocation]struct{}),
		accountWrites: make(map[types.Address]struct{}),
	}
	for _, entry := range bal.Entries {
		for _, sr := range entry.StorageReads {
			rw.reads[slotLocation{Addr: entry.Address, Slot: sr.Slot}] = struct{}{}
		}
		for _, sc := range entry.StorageChanges {
			rw.writes[slotLocation{Addr: entry.Address, Slot: sc.Slot}] = struct{}{}
		}
		if entry.BalanceChange != nil || entry.NonceChange != nil || entry.CodeChange != nil {
			rw.accountWrites[entry.Address] = struct{}{}
		}
	}
	return rw
}

// FindParallelGroups identifies groups of transactions that can execute
// simultaneously. It uses topological layering of the dependency graph:
// all transactions in the same layer have no dependencies on each other
// and can be scheduled for parallel execution.
//
// Returns a slice of groups, where each group is a slice of tx indices.
// Groups are ordered by dependency depth (group 0 has no dependencies).
func (g *GraphConflictAnalyzer) FindParallelGroups(graph *DependencyGraph) [][]int {
	if graph == nil || len(graph.deps) == 0 {
		return nil
	}

	slots := ScheduleFromGraph(graph)
	if len(slots) == 0 {
		return nil
	}

	// Group by wave ID.
	waveMap := make(map[int][]int)
	maxWave := 0
	for _, s := range slots {
		waveMap[s.WaveID] = append(waveMap[s.WaveID], s.TxIndex)
		if s.WaveID > maxWave {
			maxWave = s.WaveID
		}
	}

	groups := make([][]int, 0, maxWave+1)
	for w := 0; w <= maxWave; w++ {
		txs := waveMap[w]
		if len(txs) == 0 {
			continue
		}
		sort.Ints(txs)
		groups = append(groups, txs)
	}
	return groups
}

// FindParallelGroupsFromBAL is a convenience method that detects conflicts,
// builds the dependency graph, and returns parallel groups in one call.
func (g *GraphConflictAnalyzer) FindParallelGroupsFromBAL(bal *BlockAccessList) [][]int {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}

	conflicts := g.detector.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	for idx := range sets {
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	if numTx == 0 {
		return nil
	}

	graph := BuildDependencyGraphFromConflicts(conflicts, numTx)
	return g.FindParallelGroups(graph)
}

// EstimateSpeedup estimates the parallel execution speedup factor given a
// set of parallel groups. The speedup is the ratio of total transactions
// to the number of sequential waves needed.
//
// A speedup of 1.0 means fully serial execution (each group has 1 tx).
// Higher values indicate more parallelism. The theoretical maximum is
// totalTx (all txs in one group).
func (g *GraphConflictAnalyzer) EstimateSpeedup(groups [][]int) float64 {
	if len(groups) == 0 {
		return 1.0
	}

	totalTx := 0
	for _, group := range groups {
		totalTx += len(group)
	}
	if totalTx == 0 {
		return 1.0
	}

	// Speedup = total work / critical path length.
	// Critical path = number of sequential groups (waves).
	waves := len(groups)
	if waves == 0 {
		return 1.0
	}

	return float64(totalTx) / float64(waves)
}

// EstimateSpeedupWeighted estimates speedup considering per-transaction
// gas costs. Heavier transactions in the critical path reduce the actual
// speedup compared to the unweighted estimate.
//
// gasCosts maps tx index to estimated gas cost. Missing entries default to 1.
func (g *GraphConflictAnalyzer) EstimateSpeedupWeighted(groups [][]int, gasCosts map[int]uint64) float64 {
	if len(groups) == 0 {
		return 1.0
	}

	var totalGas uint64
	var criticalPathGas uint64

	for _, group := range groups {
		var maxGroupGas uint64
		for _, txIdx := range group {
			gas := uint64(1) // default weight
			if g, ok := gasCosts[txIdx]; ok && g > 0 {
				gas = g
			}
			totalGas += gas
			if gas > maxGroupGas {
				maxGroupGas = gas
			}
		}
		// The critical path through this wave is the slowest tx.
		criticalPathGas += maxGroupGas
	}

	if criticalPathGas == 0 {
		return 1.0
	}
	return float64(totalGas) / float64(criticalPathGas)
}

// CriticalPath returns the longest dependency chain in the graph.
// The result is a slice of tx indices from the root (no dependencies)
// to the deepest dependent transaction.
func (g *GraphConflictAnalyzer) CriticalPath(graph *DependencyGraph) []int {
	if graph == nil || len(graph.deps) == 0 {
		return nil
	}

	nodes := graph.Nodes()
	if len(nodes) == 0 {
		return nil
	}

	// Compute depth of each node.
	depth := make(map[int]int)
	parent := make(map[int]int)
	for _, n := range nodes {
		parent[n] = -1
	}

	var computeDepth func(n int) int
	computeDepth = func(n int) int {
		if d, ok := depth[n]; ok {
			return d
		}
		maxD := 0
		bestParent := -1
		for _, dep := range graph.Dependencies(n) {
			d := computeDepth(dep) + 1
			if d > maxD {
				maxD = d
				bestParent = dep
			}
		}
		depth[n] = maxD
		parent[n] = bestParent
		return maxD
	}

	for _, n := range nodes {
		computeDepth(n)
	}

	// Find the deepest node.
	deepest := nodes[0]
	for _, n := range nodes {
		if depth[n] > depth[deepest] {
			deepest = n
		}
	}

	// Trace back from deepest to root.
	var path []int
	current := deepest
	for current != -1 {
		path = append(path, current)
		current = parent[current]
	}

	// Reverse to get root-to-leaf order.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// GraphSummary holds summary statistics about a dependency graph.
type GraphSummary struct {
	TotalNodes      int
	TotalEdges      int
	IndependentTx   int     // transactions with no dependencies
	MaxDepth        int     // longest dependency chain
	ParallelGroups  int     // number of parallel execution waves
	CriticalPathLen int     // length of the critical path
	Speedup         float64 // estimated parallel speedup
}

// Summarize computes a GraphSummary from a dependency graph.
func (g *GraphConflictAnalyzer) Summarize(graph *DependencyGraph) GraphSummary {
	if graph == nil || len(graph.deps) == 0 {
		return GraphSummary{}
	}

	totalEdges := 0
	for _, deps := range graph.deps {
		totalEdges += len(deps)
	}

	groups := g.FindParallelGroups(graph)
	critPath := g.CriticalPath(graph)
	speedup := g.EstimateSpeedup(groups)

	maxDepth := 0
	if len(critPath) > 0 {
		maxDepth = len(critPath) - 1
	}

	return GraphSummary{
		TotalNodes:      len(graph.deps),
		TotalEdges:      totalEdges,
		IndependentTx:   len(graph.IndependentNodes()),
		MaxDepth:        maxDepth,
		ParallelGroups:  len(groups),
		CriticalPathLen: len(critPath),
		Speedup:         speedup,
	}
}

// TransitiveDeps returns all transitive dependencies of a transaction.
// This is the full set of transactions that must complete before tx can start.
func (g *GraphConflictAnalyzer) TransitiveDeps(graph *DependencyGraph, tx int) []int {
	if graph == nil {
		return nil
	}

	visited := make(map[int]struct{})
	var visit func(n int)
	visit = func(n int) {
		for _, dep := range graph.Dependencies(n) {
			if _, seen := visited[dep]; !seen {
				visited[dep] = struct{}{}
				visit(dep)
			}
		}
	}
	visit(tx)

	result := make([]int, 0, len(visited))
	for dep := range visited {
		result = append(result, dep)
	}
	sort.Ints(result)
	return result
}

// TransitiveDependents returns all transactions that transitively depend on tx.
// These are the transactions that cannot start until tx completes.
func (g *GraphConflictAnalyzer) TransitiveDependents(graph *DependencyGraph, tx int) []int {
	if graph == nil {
		return nil
	}

	// Build reverse adjacency map (tx -> transactions that depend on it).
	reverse := make(map[int][]int)
	for node, deps := range graph.deps {
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], node)
		}
	}

	visited := make(map[int]struct{})
	var visit func(n int)
	visit = func(n int) {
		for _, dependent := range reverse[n] {
			if _, seen := visited[dependent]; !seen {
				visited[dependent] = struct{}{}
				visit(dependent)
			}
		}
	}
	visit(tx)

	result := make([]int, 0, len(visited))
	for dep := range visited {
		result = append(result, dep)
	}
	sort.Ints(result)
	return result
}

// IndependentPairCount returns the number of transaction pairs that can
// execute in parallel (no conflicts between them).
func (g *GraphConflictAnalyzer) IndependentPairCount(graph *DependencyGraph) int {
	if graph == nil || len(graph.deps) == 0 {
		return 0
	}

	nodes := graph.Nodes()
	n := len(nodes)
	totalPairs := n * (n - 1) / 2

	// Build conflict set from dependencies.
	conflicting := make(map[[2]int]struct{})
	for node, deps := range graph.deps {
		for _, dep := range deps {
			a, b := dep, node
			if a > b {
				a, b = b, a
			}
			conflicting[[2]int{a, b}] = struct{}{}
		}
	}
	return totalPairs - len(conflicting)
}
