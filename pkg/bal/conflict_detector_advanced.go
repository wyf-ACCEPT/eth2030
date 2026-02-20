// conflict_detector_advanced.go provides advanced parallel execution conflict
// analysis using Block Access Lists (EIP-7928). It extends the base
// BALConflictDetector with dependency graph computation, conflict clustering,
// transaction reordering suggestions, and parallelism scoring.
package bal

import (
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// ConflictCluster groups transactions that are transitively connected through
// conflicts. Transactions in different clusters are fully independent.
type ConflictCluster struct {
	TxIndices []int
	Conflicts []Conflict
}

// ReorderSuggestion recommends a new transaction ordering that maximizes
// parallelism by placing independent transactions adjacent to each other.
type ReorderSuggestion struct {
	OriginalOrder  []int
	SuggestedOrder []int
	WavesBefore    int
	WavesAfter     int
}

// ParallelismScore captures quantitative metrics about how parallelizable
// a block's transactions are.
type ParallelismScore struct {
	TotalTx          int
	IndependentPairs int
	ConflictingPairs int
	ClusterCount     int
	MaxClusterSize   int
	WaveCount        int
	Score            float64 // 0.0 (fully serial) to 1.0 (fully parallel)
}

// AdvancedConflictAnalyzer extends BALConflictDetector with graph analysis,
// clustering, and reordering heuristics.
type AdvancedConflictAnalyzer struct {
	detector *BALConflictDetector
}

// NewAdvancedConflictAnalyzer creates an analyzer wrapping the given detector.
func NewAdvancedConflictAnalyzer(detector *BALConflictDetector) *AdvancedConflictAnalyzer {
	return &AdvancedConflictAnalyzer{detector: detector}
}

// AnalyzeConflicts detects all conflicts and returns them along with
// the conflict matrix. Returns nil conflicts and matrix for nil/empty BAL.
func (a *AdvancedConflictAnalyzer) AnalyzeConflicts(bal *BlockAccessList) ([]Conflict, *ConflictMatrix) {
	if bal == nil || len(bal.Entries) == 0 {
		return nil, nil
	}
	conflicts := a.detector.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	for idx := range sets {
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	if numTx == 0 {
		return nil, nil
	}
	matrix := BuildConflictMatrix(conflicts, numTx)
	return conflicts, matrix
}

// ComputeClusters finds connected components of transactions via transitive
// conflict relationships. Transactions in different clusters can execute
// fully independently.
func (a *AdvancedConflictAnalyzer) ComputeClusters(bal *BlockAccessList) []ConflictCluster {
	conflicts, matrix := a.AnalyzeConflicts(bal)
	if matrix == nil || matrix.Size() == 0 {
		return nil
	}

	n := matrix.Size()
	visited := make([]bool, n)
	var clusters []ConflictCluster

	for i := 0; i < n; i++ {
		if visited[i] {
			continue
		}
		// BFS to find the connected component.
		var component []int
		queue := []int{i}
		visited[i] = true
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			component = append(component, node)
			for j := 0; j < n; j++ {
				if !visited[j] && matrix.Get(node, j) {
					visited[j] = true
					queue = append(queue, j)
				}
			}
		}
		sort.Ints(component)

		// Collect conflicts within this cluster.
		compSet := make(map[int]struct{}, len(component))
		for _, idx := range component {
			compSet[idx] = struct{}{}
		}
		var clusterConflicts []Conflict
		for _, c := range conflicts {
			if _, okA := compSet[c.TxA]; okA {
				if _, okB := compSet[c.TxB]; okB {
					clusterConflicts = append(clusterConflicts, c)
				}
			}
		}

		clusters = append(clusters, ConflictCluster{
			TxIndices: component,
			Conflicts: clusterConflicts,
		})
	}
	return clusters
}

// SuggestReorder computes a reordering of transactions that places
// independent transactions together, improving wave-based scheduling.
// It sorts by dependency depth (shallower first), then by cluster ID.
func (a *AdvancedConflictAnalyzer) SuggestReorder(bal *BlockAccessList) *ReorderSuggestion {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}
	conflicts := a.detector.DetectConflicts(bal)
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

	original := make([]int, numTx)
	for i := range original {
		original[i] = i
	}

	// Build dependency graph and compute depths.
	graph := BuildDependencyGraphFromConflicts(conflicts, numTx)
	slots := ScheduleFromGraph(graph)
	wavesBefore := WaveCount(slots)

	// Compute depth per tx.
	depth := make(map[int]int)
	for _, s := range slots {
		depth[s.TxIndex] = s.WaveID
	}

	// Assign cluster IDs.
	matrix := BuildConflictMatrix(conflicts, numTx)
	clusterID := make(map[int]int)
	visited := make([]bool, numTx)
	cid := 0
	for i := 0; i < numTx; i++ {
		if visited[i] {
			continue
		}
		queue := []int{i}
		visited[i] = true
		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			clusterID[node] = cid
			for j := 0; j < numTx; j++ {
				if !visited[j] && matrix.Get(node, j) {
					visited[j] = true
					queue = append(queue, j)
				}
			}
		}
		cid++
	}

	// Sort by (depth, clusterID, original index).
	suggested := make([]int, numTx)
	copy(suggested, original)
	sort.Slice(suggested, func(i, j int) bool {
		di, dj := depth[suggested[i]], depth[suggested[j]]
		if di != dj {
			return di < dj
		}
		ci, cj := clusterID[suggested[i]], clusterID[suggested[j]]
		if ci != cj {
			return ci < cj
		}
		return suggested[i] < suggested[j]
	})

	// Compute waves after reordering. Since wave assignment is based on
	// dependency structure (which doesn't change with reordering), the
	// wave count stays the same. However, the reorder helps batch
	// formation within waves.
	wavesAfter := wavesBefore

	return &ReorderSuggestion{
		OriginalOrder:  original,
		SuggestedOrder: suggested,
		WavesBefore:    wavesBefore,
		WavesAfter:     wavesAfter,
	}
}

// ScoreParallelism computes a ParallelismScore for the given BAL.
func (a *AdvancedConflictAnalyzer) ScoreParallelism(bal *BlockAccessList) ParallelismScore {
	if bal == nil || len(bal.Entries) == 0 {
		return ParallelismScore{}
	}
	conflicts := a.detector.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	for idx := range sets {
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	if numTx == 0 {
		return ParallelismScore{}
	}

	totalPairs := numTx * (numTx - 1) / 2
	// Deduplicate conflict pairs for counting.
	conflictPairs := make(map[[2]int]struct{})
	for _, c := range conflicts {
		pair := [2]int{c.TxA, c.TxB}
		conflictPairs[pair] = struct{}{}
	}
	conflictingPairs := len(conflictPairs)
	independentPairs := totalPairs - conflictingPairs

	clusters := a.ComputeClusters(bal)
	maxCluster := 0
	for _, cl := range clusters {
		if len(cl.TxIndices) > maxCluster {
			maxCluster = len(cl.TxIndices)
		}
	}

	graph := BuildDependencyGraphFromConflicts(conflicts, numTx)
	slots := ScheduleFromGraph(graph)
	waves := WaveCount(slots)

	// Score: ratio of independent pairs to total pairs.
	var score float64
	if totalPairs > 0 {
		score = float64(independentPairs) / float64(totalPairs)
	} else if numTx == 1 {
		score = 1.0 // single tx is trivially parallel
	}

	return ParallelismScore{
		TotalTx:          numTx,
		IndependentPairs: independentPairs,
		ConflictingPairs: conflictingPairs,
		ClusterCount:     len(clusters),
		MaxClusterSize:   maxCluster,
		WaveCount:        waves,
		Score:            score,
	}
}

// ConflictsByAddress groups conflicts by the address they affect.
func (a *AdvancedConflictAnalyzer) ConflictsByAddress(bal *BlockAccessList) map[types.Address][]Conflict {
	conflicts := a.detector.DetectConflicts(bal)
	if len(conflicts) == 0 {
		return nil
	}
	result := make(map[types.Address][]Conflict)
	for _, c := range conflicts {
		result[c.Address] = append(result[c.Address], c)
	}
	return result
}

// HotSpots returns the addresses that appear in the most conflicts,
// sorted by conflict count descending. The limit parameter caps the
// number of results (0 means return all).
func (a *AdvancedConflictAnalyzer) HotSpots(bal *BlockAccessList, limit int) []AddressConflictCount {
	byAddr := a.ConflictsByAddress(bal)
	if len(byAddr) == 0 {
		return nil
	}

	counts := make([]AddressConflictCount, 0, len(byAddr))
	for addr, cs := range byAddr {
		counts = append(counts, AddressConflictCount{
			Address: addr,
			Count:   len(cs),
		})
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].Count != counts[j].Count {
			return counts[i].Count > counts[j].Count
		}
		return addrLess(counts[i].Address, counts[j].Address)
	})

	if limit > 0 && limit < len(counts) {
		counts = counts[:limit]
	}
	return counts
}

// AddressConflictCount pairs an address with its conflict count.
type AddressConflictCount struct {
	Address types.Address
	Count   int
}
