package bal

import (
	"sort"

	"github.com/eth2030/eth2030/core/types"
)

// ExecutionGroup represents a set of transactions that can execute in parallel.
type ExecutionGroup struct {
	TxIndices []int
}

// addrSlot uniquely identifies a storage location.
type addrSlot struct {
	Addr types.Address
	Slot types.Hash
}

// txAccessSet tracks read and write sets for a single transaction.
type txAccessSet struct {
	reads  map[addrSlot]struct{}
	writes map[addrSlot]struct{}
}

// ComputeParallelSets analyzes a BlockAccessList and groups transactions
// into sets that can execute in parallel. Two transactions conflict if they
// access the same (address, slot) pair and at least one of them writes.
func ComputeParallelSets(bal *BlockAccessList) []ExecutionGroup {
	if bal == nil || len(bal.Entries) == 0 {
		return nil
	}

	// Collect per-tx read and write sets. Only consider entries with
	// AccessIndex >= 1 (actual transactions, not pre/post execution).
	txMap := make(map[int]*txAccessSet)

	for _, entry := range bal.Entries {
		if entry.AccessIndex == 0 {
			continue // pre-execution
		}
		txIdx := int(entry.AccessIndex) - 1 // convert to 0-based tx index

		ta, ok := txMap[txIdx]
		if !ok {
			ta = &txAccessSet{
				reads:  make(map[addrSlot]struct{}),
				writes: make(map[addrSlot]struct{}),
			}
			txMap[txIdx] = ta
		}

		for _, sr := range entry.StorageReads {
			ta.reads[addrSlot{Addr: entry.Address, Slot: sr.Slot}] = struct{}{}
		}
		for _, sc := range entry.StorageChanges {
			ta.writes[addrSlot{Addr: entry.Address, Slot: sc.Slot}] = struct{}{}
		}

		// Balance, nonce, and code changes also use the address as conflict key.
		// We use a zero-hash sentinel slot to represent account-level conflicts.
		if entry.BalanceChange != nil || entry.NonceChange != nil || entry.CodeChange != nil {
			sentinel := addrSlot{Addr: entry.Address}
			ta.writes[sentinel] = struct{}{}
		}
	}

	if len(txMap) == 0 {
		return nil
	}

	// Extract sorted tx indices.
	txIndices := make([]int, 0, len(txMap))
	for idx := range txMap {
		txIndices = append(txIndices, idx)
	}
	sort.Ints(txIndices)

	// Build conflict adjacency: two txs conflict if they share an addrSlot
	// and at least one writes to it.
	conflicts := make(map[int]map[int]struct{})
	for i := 0; i < len(txIndices); i++ {
		for j := i + 1; j < len(txIndices); j++ {
			a := txMap[txIndices[i]]
			b := txMap[txIndices[j]]
			if txsConflict(a, b) {
				ai, bi := txIndices[i], txIndices[j]
				if conflicts[ai] == nil {
					conflicts[ai] = make(map[int]struct{})
				}
				if conflicts[bi] == nil {
					conflicts[bi] = make(map[int]struct{})
				}
				conflicts[ai][bi] = struct{}{}
				conflicts[bi][ai] = struct{}{}
			}
		}
	}

	// Greedy graph coloring to find independent groups.
	color := make(map[int]int) // tx index -> group id
	for _, idx := range txIndices {
		usedColors := make(map[int]struct{})
		if neighbors, ok := conflicts[idx]; ok {
			for n := range neighbors {
				if c, assigned := color[n]; assigned {
					usedColors[c] = struct{}{}
				}
			}
		}
		// Find smallest unused color.
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

	// Build sorted result.
	groupIDs := make([]int, 0, len(groupMap))
	for gid := range groupMap {
		groupIDs = append(groupIDs, gid)
	}
	sort.Ints(groupIDs)

	groups := make([]ExecutionGroup, len(groupIDs))
	for i, gid := range groupIDs {
		groups[i] = ExecutionGroup{TxIndices: groupMap[gid]}
	}
	return groups
}

// MaxParallelism returns the maximum number of transactions that can
// execute in parallel, which is the size of the largest execution group.
func MaxParallelism(bal *BlockAccessList) int {
	groups := ComputeParallelSets(bal)
	max := 0
	for _, g := range groups {
		if len(g.TxIndices) > max {
			max = len(g.TxIndices)
		}
	}
	return max
}

// txsConflict returns true if two transactions have a read-write or
// write-write conflict on any shared storage location.
func txsConflict(a, b *txAccessSet) bool {
	// Check if a writes to any slot that b reads or writes.
	for slot := range a.writes {
		if _, ok := b.reads[slot]; ok {
			return true
		}
		if _, ok := b.writes[slot]; ok {
			return true
		}
	}
	// Check if b writes to any slot that a reads.
	for slot := range b.writes {
		if _, ok := a.reads[slot]; ok {
			return true
		}
	}
	return false
}
