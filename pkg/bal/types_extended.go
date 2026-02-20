// types_extended.go adds detailed access entries with read/write tracking,
// conflict matrix, dependency graph construction, and parallel execution
// scheduling types for Block Access Lists (EIP-7928).
package bal

import (
	"sort"

	"github.com/eth2028/eth2028/core/types"
)

// AccessMode classifies whether a state access is a read or a write.
type AccessMode uint8

const (
	// AccessRead indicates a storage slot was only read.
	AccessRead AccessMode = 0
	// AccessWrite indicates a storage slot was written.
	AccessWrite AccessMode = 1
	// AccessReadWrite indicates both a read and write on the same slot.
	AccessReadWrite AccessMode = 2
)

// String returns a human-readable label for the access mode.
func (m AccessMode) String() string {
	switch m {
	case AccessRead:
		return "read"
	case AccessWrite:
		return "write"
	case AccessReadWrite:
		return "read-write"
	default:
		return "unknown"
	}
}

// DetailedAccess tracks a single (address, slot) access with its mode.
type DetailedAccess struct {
	Address types.Address
	Slot    types.Hash
	Mode    AccessMode
}

// DetailedAccessEntry extends AccessEntry with read/write mode tracking per
// slot, enabling finer-grained conflict analysis.
type DetailedAccessEntry struct {
	TxIndex  int
	Accesses []DetailedAccess
}

// BuildDetailedEntries converts a BlockAccessList into per-transaction
// DetailedAccessEntry slices. Each slot gets a mode: read-only, write-only,
// or read-write (if both a read and a change exist for the same slot).
func BuildDetailedEntries(bal *BlockAccessList) []DetailedAccessEntry {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}

	// Group by tx index.
	txMap := make(map[int]map[slotLocation]AccessMode)
	for _, entry := range bal.Entries {
		if entry.AccessIndex == 0 {
			continue
		}
		txIdx := int(entry.AccessIndex) - 1

		modes, ok := txMap[txIdx]
		if !ok {
			modes = make(map[slotLocation]AccessMode)
			txMap[txIdx] = modes
		}

		for _, sr := range entry.StorageReads {
			loc := slotLocation{Addr: entry.Address, Slot: sr.Slot}
			if existing, exists := modes[loc]; exists && existing == AccessWrite {
				modes[loc] = AccessReadWrite
			} else if !exists {
				modes[loc] = AccessRead
			}
		}
		for _, sc := range entry.StorageChanges {
			loc := slotLocation{Addr: entry.Address, Slot: sc.Slot}
			if existing, exists := modes[loc]; exists && existing == AccessRead {
				modes[loc] = AccessReadWrite
			} else if !exists {
				modes[loc] = AccessWrite
			}
		}
		// Account-level changes (balance, nonce, code) use sentinel zero slot.
		if entry.BalanceChange != nil || entry.NonceChange != nil || entry.CodeChange != nil {
			loc := slotLocation{Addr: entry.Address}
			if _, exists := modes[loc]; !exists {
				modes[loc] = AccessWrite
			} else {
				modes[loc] = AccessReadWrite
			}
		}
	}

	// Collect sorted tx indices.
	indices := make([]int, 0, len(txMap))
	for idx := range txMap {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	result := make([]DetailedAccessEntry, 0, len(indices))
	for _, idx := range indices {
		modes := txMap[idx]
		accesses := make([]DetailedAccess, 0, len(modes))
		for loc, mode := range modes {
			accesses = append(accesses, DetailedAccess{
				Address: loc.Addr,
				Slot:    loc.Slot,
				Mode:    mode,
			})
		}
		// Sort for determinism.
		sort.Slice(accesses, func(i, j int) bool {
			if accesses[i].Address != accesses[j].Address {
				return addrLess(accesses[i].Address, accesses[j].Address)
			}
			return hashLess(accesses[i].Slot, accesses[j].Slot)
		})
		result = append(result, DetailedAccessEntry{TxIndex: idx, Accesses: accesses})
	}
	return result
}

// ConflictMatrix is a symmetric boolean matrix where cell [i][j] indicates
// whether transaction i conflicts with transaction j.
type ConflictMatrix struct {
	size int
	data []bool // flattened symmetric matrix
}

// NewConflictMatrix creates a new n x n conflict matrix.
func NewConflictMatrix(n int) *ConflictMatrix {
	return &ConflictMatrix{
		size: n,
		data: make([]bool, n*n),
	}
}

// Set marks a conflict between transactions i and j (symmetric).
func (m *ConflictMatrix) Set(i, j int) {
	if i >= 0 && i < m.size && j >= 0 && j < m.size {
		m.data[i*m.size+j] = true
		m.data[j*m.size+i] = true
	}
}

// Get returns whether transactions i and j conflict.
func (m *ConflictMatrix) Get(i, j int) bool {
	if i < 0 || i >= m.size || j < 0 || j >= m.size {
		return false
	}
	return m.data[i*m.size+j]
}

// Size returns the matrix dimension.
func (m *ConflictMatrix) Size() int {
	return m.size
}

// ConflictCount returns the number of conflicts for transaction i.
func (m *ConflictMatrix) ConflictCount(i int) int {
	if i < 0 || i >= m.size {
		return 0
	}
	count := 0
	for j := 0; j < m.size; j++ {
		if i != j && m.data[i*m.size+j] {
			count++
		}
	}
	return count
}

// TotalConflicts returns the total number of unique conflict pairs.
func (m *ConflictMatrix) TotalConflicts() int {
	count := 0
	for i := 0; i < m.size; i++ {
		for j := i + 1; j < m.size; j++ {
			if m.data[i*m.size+j] {
				count++
			}
		}
	}
	return count
}

// BuildConflictMatrix constructs a ConflictMatrix from a set of conflicts
// and the total number of transactions.
func BuildConflictMatrix(conflicts []Conflict, numTx int) *ConflictMatrix {
	cm := NewConflictMatrix(numTx)
	for _, c := range conflicts {
		if c.TxA < numTx && c.TxB < numTx {
			cm.Set(c.TxA, c.TxB)
		}
	}
	return cm
}

// DependencyGraph maps each transaction index to its set of predecessor
// transactions that must complete before it can start.
type DependencyGraph struct {
	deps map[int][]int
}

// NewDependencyGraph creates an empty dependency graph.
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{deps: make(map[int][]int)}
}

// AddNode ensures a transaction exists in the graph (even with no deps).
func (g *DependencyGraph) AddNode(tx int) {
	if _, ok := g.deps[tx]; !ok {
		g.deps[tx] = nil
	}
}

// AddEdge adds a dependency: tx depends on dep (dep must finish first).
func (g *DependencyGraph) AddEdge(tx, dep int) {
	g.AddNode(dep)
	g.deps[tx] = append(g.deps[tx], dep)
}

// Dependencies returns the direct predecessors of a transaction.
func (g *DependencyGraph) Dependencies(tx int) []int {
	return g.deps[tx]
}

// Nodes returns all transaction indices in sorted order.
func (g *DependencyGraph) Nodes() []int {
	nodes := make([]int, 0, len(g.deps))
	for n := range g.deps {
		nodes = append(nodes, n)
	}
	sort.Ints(nodes)
	return nodes
}

// IndependentNodes returns transactions with zero dependencies.
func (g *DependencyGraph) IndependentNodes() []int {
	var result []int
	for n, deps := range g.deps {
		if len(deps) == 0 {
			result = append(result, n)
		}
	}
	sort.Ints(result)
	return result
}

// BuildDependencyGraphFromConflicts constructs a DependencyGraph from
// conflicts. For each conflict, the later transaction depends on the earlier.
func BuildDependencyGraphFromConflicts(conflicts []Conflict, numTx int) *DependencyGraph {
	g := NewDependencyGraph()
	for i := 0; i < numTx; i++ {
		g.AddNode(i)
	}

	seen := make(map[[2]int]struct{})
	for _, c := range conflicts {
		edge := [2]int{c.TxA, c.TxB}
		if _, ok := seen[edge]; ok {
			continue
		}
		seen[edge] = struct{}{}
		g.AddEdge(c.TxB, c.TxA)
	}
	return g
}

// ScheduleSlot represents a transaction assigned to a specific execution slot.
type ScheduleSlot struct {
	TxIndex int
	WaveID  int
}

// ScheduleFromGraph produces an execution schedule from a dependency graph.
// Returns a list of ScheduleSlots, one per transaction, indicating which
// wave it should execute in. Returns nil if the graph is nil or empty.
func ScheduleFromGraph(g *DependencyGraph) []ScheduleSlot {
	if g == nil || len(g.deps) == 0 {
		return nil
	}

	// Assign each node a level = max(deps levels) + 1.
	level := make(map[int]int)
	nodes := g.Nodes()

	for _, n := range nodes {
		computeLevel(n, g, level)
	}

	slots := make([]ScheduleSlot, 0, len(nodes))
	for _, n := range nodes {
		slots = append(slots, ScheduleSlot{TxIndex: n, WaveID: level[n]})
	}
	return slots
}

// computeLevel recursively computes the execution wave for a node.
func computeLevel(n int, g *DependencyGraph, level map[int]int) int {
	if l, ok := level[n]; ok {
		return l
	}
	maxDep := -1
	for _, dep := range g.Dependencies(n) {
		l := computeLevel(dep, g, level)
		if l > maxDep {
			maxDep = l
		}
	}
	level[n] = maxDep + 1
	return level[n]
}

// WaveCount returns the number of execution waves needed.
func WaveCount(slots []ScheduleSlot) int {
	if len(slots) == 0 {
		return 0
	}
	max := 0
	for _, s := range slots {
		if s.WaveID > max {
			max = s.WaveID
		}
	}
	return max + 1
}
