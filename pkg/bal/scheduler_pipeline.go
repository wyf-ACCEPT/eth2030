// scheduler_pipeline.go implements pipeline-based transaction scheduling for
// parallel execution using Block Access Lists (EIP-7928). It provides worker
// pool assignment with load balancing, dependency-aware pipeline stages,
// and conflict-free batch construction.
package bal

import (
	"errors"
	"sort"
)

// Pipeline errors.
var (
	ErrPipelineEmpty   = errors.New("pipeline: no transactions to pipeline")
	ErrPipelineWorkers = errors.New("pipeline: worker count must be positive")
)

// PipelineStage represents a set of transactions that execute in a single
// pipeline stage. All transactions in a stage are conflict-free.
type PipelineStage struct {
	StageID int
	Batches []PipelineBatch
}

// PipelineBatch groups transactions assigned to a single worker within a stage.
type PipelineBatch struct {
	WorkerID int
	Tasks    []PipelineTask
	GasSum   uint64
}

// PipelineTask is a transaction scheduled for pipeline execution.
type PipelineTask struct {
	TxIndex  int
	GasLimit uint64
	Depth    int // dependency depth (0 = no dependencies)
}

// PipelinePlan is the complete execution plan for a block.
type PipelinePlan struct {
	Stages       []PipelineStage
	WorkerCount  int
	TotalTx      int
	MaxStageSize int
}

// PipelineScheduler creates pipeline execution plans from BAL analysis.
type PipelineScheduler struct {
	workers  int
	detector *BALConflictDetector
}

// NewPipelineScheduler creates a scheduler with the given worker count
// and conflict detector.
func NewPipelineScheduler(workers int, detector *BALConflictDetector) (*PipelineScheduler, error) {
	if workers < 1 {
		return nil, ErrPipelineWorkers
	}
	return &PipelineScheduler{
		workers:  workers,
		detector: detector,
	}, nil
}

// BuildPlan analyzes the BAL and produces a PipelinePlan with stages
// and worker assignments. Each stage contains only conflict-free
// transactions. Within each stage, transactions are distributed across
// workers using gas-balanced assignment.
func (ps *PipelineScheduler) BuildPlan(bal *BlockAccessList, gasLimits map[int]uint64) (*PipelinePlan, error) {
	if bal == nil || len(bal.Entries) == 0 {
		return nil, ErrPipelineEmpty
	}

	conflicts := ps.detector.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	txIndices := make([]int, 0, len(sets))
	for idx := range sets {
		txIndices = append(txIndices, idx)
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	sort.Ints(txIndices)

	if len(txIndices) == 0 {
		return nil, ErrPipelineEmpty
	}

	// Build dependency graph and compute depths.
	graph := BuildDependencyGraphFromConflicts(conflicts, numTx)
	depthMap := computeDepths(graph, numTx)

	// Group transactions into stages by depth.
	stageMap := make(map[int][]int) // depth -> tx indices
	for _, idx := range txIndices {
		d := depthMap[idx]
		stageMap[d] = append(stageMap[d], idx)
	}

	stageDepths := make([]int, 0, len(stageMap))
	for d := range stageMap {
		stageDepths = append(stageDepths, d)
	}
	sort.Ints(stageDepths)

	stages := make([]PipelineStage, len(stageDepths))
	maxStageSize := 0
	for i, d := range stageDepths {
		txs := stageMap[d]
		sort.Ints(txs)

		tasks := make([]PipelineTask, len(txs))
		for j, idx := range txs {
			gas := uint64(21000) // default
			if g, ok := gasLimits[idx]; ok {
				gas = g
			}
			tasks[j] = PipelineTask{
				TxIndex:  idx,
				GasLimit: gas,
				Depth:    d,
			}
		}

		batches := ps.assignWorkers(tasks)
		stages[i] = PipelineStage{
			StageID: i,
			Batches: batches,
		}

		if len(txs) > maxStageSize {
			maxStageSize = len(txs)
		}
	}

	return &PipelinePlan{
		Stages:       stages,
		WorkerCount:  ps.workers,
		TotalTx:      len(txIndices),
		MaxStageSize: maxStageSize,
	}, nil
}

// assignWorkers distributes tasks across workers using a greedy
// gas-balanced assignment. The worker with the lowest current gas sum
// receives the next task.
func (ps *PipelineScheduler) assignWorkers(tasks []PipelineTask) []PipelineBatch {
	if len(tasks) == 0 {
		return nil
	}

	batches := make([]PipelineBatch, ps.workers)
	for i := range batches {
		batches[i].WorkerID = i
	}

	for _, task := range tasks {
		// Find worker with minimum gas load.
		minIdx := 0
		for i := 1; i < len(batches); i++ {
			if batches[i].GasSum < batches[minIdx].GasSum {
				minIdx = i
			}
		}
		batches[minIdx].Tasks = append(batches[minIdx].Tasks, task)
		batches[minIdx].GasSum += task.GasLimit
	}

	// Remove empty batches (when workers > tasks).
	var nonEmpty []PipelineBatch
	for _, b := range batches {
		if len(b.Tasks) > 0 {
			nonEmpty = append(nonEmpty, b)
		}
	}
	return nonEmpty
}

// computeDepths computes the dependency depth for each transaction.
// Depth 0 means no dependencies; higher values mean deeper chains.
func computeDepths(graph *DependencyGraph, numTx int) map[int]int {
	depths := make(map[int]int)
	for i := 0; i < numTx; i++ {
		computeDepth(i, graph, depths)
	}
	return depths
}

// computeDepth recursively computes the depth of a single node.
func computeDepth(n int, g *DependencyGraph, depths map[int]int) int {
	if d, ok := depths[n]; ok {
		return d
	}
	maxDep := -1
	for _, dep := range g.Dependencies(n) {
		d := computeDepth(dep, g, depths)
		if d > maxDep {
			maxDep = d
		}
	}
	depths[n] = maxDep + 1
	return depths[n]
}

// BuildConflictFreeBatches creates batches of transactions that have zero
// conflicts with each other, suitable for fully parallel execution.
// Uses greedy graph coloring.
func BuildConflictFreeBatches(bal *BlockAccessList) [][]int {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}

	detector := NewBALConflictDetector(StrategySerialize)
	conflicts := detector.DetectConflicts(bal)
	sets := extractRWSets(bal)
	numTx := 0
	txIndices := make([]int, 0, len(sets))
	for idx := range sets {
		txIndices = append(txIndices, idx)
		if idx+1 > numTx {
			numTx = idx + 1
		}
	}
	sort.Ints(txIndices)

	if len(txIndices) == 0 {
		return nil
	}

	// Build adjacency.
	matrix := BuildConflictMatrix(conflicts, numTx)

	// Greedy coloring.
	color := make(map[int]int)
	for _, idx := range txIndices {
		usedColors := make(map[int]struct{})
		for _, other := range txIndices {
			if c, assigned := color[other]; assigned && matrix.Get(idx, other) {
				usedColors[c] = struct{}{}
			}
		}
		c := 0
		for {
			if _, used := usedColors[c]; !used {
				break
			}
			c++
		}
		color[idx] = c
	}

	// Group by color.
	groupMap := make(map[int][]int)
	for _, idx := range txIndices {
		c := color[idx]
		groupMap[c] = append(groupMap[c], idx)
	}

	colorIDs := make([]int, 0, len(groupMap))
	for c := range groupMap {
		colorIDs = append(colorIDs, c)
	}
	sort.Ints(colorIDs)

	batches := make([][]int, len(colorIDs))
	for i, c := range colorIDs {
		batches[i] = groupMap[c]
	}
	return batches
}

// GasBalanceRatio returns the ratio of the lightest worker's gas to the
// heaviest worker's gas within a stage. A value of 1.0 means perfect
// balance; lower values indicate imbalance.
func GasBalanceRatio(stage PipelineStage) float64 {
	if len(stage.Batches) <= 1 {
		return 1.0
	}
	var minGas, maxGas uint64
	minGas = ^uint64(0)
	for _, b := range stage.Batches {
		if b.GasSum < minGas {
			minGas = b.GasSum
		}
		if b.GasSum > maxGas {
			maxGas = b.GasSum
		}
	}
	if maxGas == 0 {
		return 1.0
	}
	return float64(minGas) / float64(maxGas)
}

// PipelineEfficiency returns the ratio of total transactions to the
// number of stages. Higher values indicate more parallelism.
func PipelineEfficiency(plan *PipelinePlan) float64 {
	if plan == nil || len(plan.Stages) == 0 || plan.TotalTx == 0 {
		return 0.0
	}
	return float64(plan.TotalTx) / float64(len(plan.Stages))
}
