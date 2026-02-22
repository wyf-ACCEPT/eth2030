// Package discover implements the Ethereum Node Discovery v5 protocol.
// It maintains a Kademlia-like routing table organized in k-buckets
// indexed by XOR log distance.
package discover

import (
	"crypto/rand"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/p2p/enode"
)

// Kademlia table constants.
const (
	BucketSize = 16  // max entries per bucket
	NumBuckets = 256 // one bucket per possible log distance
	Alpha      = 3   // concurrency factor for lookups
	MaxReplacements = 10 // max replacement cache per bucket
)

// Bucket holds nodes at a given XOR log distance.
type Bucket struct {
	entries      []*enode.Node
	replacements []*enode.Node
}

// Table is a Kademlia-like routing table.
type Table struct {
	mu      sync.RWMutex
	self    enode.NodeID
	buckets [NumBuckets]Bucket
}

// NewTable creates a new routing table for the given local node ID.
func NewTable(self enode.NodeID) *Table {
	return &Table{self: self}
}

// Self returns the local node ID.
func (t *Table) Self() enode.NodeID {
	return t.self
}

// BucketIndex returns which bucket a node belongs in (0-255).
// Bucket 0 is for the largest distance (bit 255 differs),
// bucket 255 is for the smallest distance (bit 0 differs).
// Returns -1 for the local node (distance 0).
func (t *Table) BucketIndex(id enode.NodeID) int {
	dist := enode.Distance(t.self, id)
	if dist == 0 {
		return -1 // self
	}
	return dist - 1
}

// AddNode adds a node to the appropriate bucket. If the bucket is full,
// the node is added to the replacement cache instead.
func (t *Table) AddNode(n *enode.Node) {
	if n.ID == t.self {
		return
	}
	idx := t.BucketIndex(n.ID)
	if idx < 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	b := &t.buckets[idx]

	// Check if already present.
	for _, e := range b.entries {
		if e.ID == n.ID {
			return
		}
	}

	if len(b.entries) < BucketSize {
		b.entries = append(b.entries, n)
	} else {
		// Bucket full, add to replacements.
		for _, e := range b.replacements {
			if e.ID == n.ID {
				return
			}
		}
		if len(b.replacements) < MaxReplacements {
			b.replacements = append(b.replacements, n)
		}
	}
}

// RemoveNode removes a node from the table. If there are replacement nodes
// available, one is promoted.
func (t *Table) RemoveNode(id enode.NodeID) {
	idx := t.BucketIndex(id)
	if idx < 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	b := &t.buckets[idx]
	for i, e := range b.entries {
		if e.ID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			// Promote a replacement if available.
			if len(b.replacements) > 0 {
				b.entries = append(b.entries, b.replacements[0])
				b.replacements = b.replacements[1:]
			}
			return
		}
	}
}

// FindNode returns the count closest nodes to the target from the local table.
func (t *Table) FindNode(target enode.NodeID, count int) []*enode.Node {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Collect all nodes.
	var nodes []*enode.Node
	for i := range t.buckets {
		nodes = append(nodes, t.buckets[i].entries...)
	}

	// Sort by distance to target.
	sort.Slice(nodes, func(i, j int) bool {
		return enode.DistCmp(target, nodes[i].ID, nodes[j].ID) < 0
	})

	if len(nodes) > count {
		nodes = nodes[:count]
	}
	return nodes
}

// Lookup performs an iterative Kademlia lookup for the target node ID.
// It queries the Alpha closest known nodes, then iteratively queries nodes
// returned by those queries until no closer nodes are found.
// The network query function is provided as a callback.
func (t *Table) Lookup(target enode.NodeID, queryFn func(n *enode.Node) []*enode.Node) []*enode.Node {
	// Seed with the closest known nodes.
	closest := t.FindNode(target, BucketSize)
	if len(closest) == 0 {
		return nil
	}

	asked := make(map[enode.NodeID]bool)
	asked[t.self] = true

	// Iterative lookup.
	for {
		// Find Alpha nodes to query that haven't been asked.
		var toAsk []*enode.Node
		for _, n := range closest {
			if !asked[n.ID] {
				toAsk = append(toAsk, n)
				if len(toAsk) >= Alpha {
					break
				}
			}
		}
		if len(toAsk) == 0 {
			break
		}

		// Query in sequence (real implementation would parallelize).
		improved := false
		for _, n := range toAsk {
			asked[n.ID] = true
			results := queryFn(n)
			for _, r := range results {
				if asked[r.ID] {
					continue
				}
				// Add to table.
				t.AddNode(r)
				// Check if this node is closer than the current furthest.
				if len(closest) < BucketSize || enode.DistCmp(target, r.ID, closest[len(closest)-1].ID) < 0 {
					closest = insertSorted(closest, r, target)
					if len(closest) > BucketSize {
						closest = closest[:BucketSize]
					}
					improved = true
				}
			}
		}
		if !improved {
			break
		}
	}
	return closest
}

// insertSorted inserts a node into a sorted slice, maintaining sort by distance to target.
func insertSorted(nodes []*enode.Node, n *enode.Node, target enode.NodeID) []*enode.Node {
	// Check for duplicate.
	for _, existing := range nodes {
		if existing.ID == n.ID {
			return nodes
		}
	}
	i := sort.Search(len(nodes), func(i int) bool {
		return enode.DistCmp(target, n.ID, nodes[i].ID) < 0
	})
	nodes = append(nodes, nil)
	copy(nodes[i+1:], nodes[i:])
	nodes[i] = n
	return nodes
}

// Refresh picks a random target and performs a lookup to discover new nodes.
func (t *Table) Refresh(queryFn func(n *enode.Node) []*enode.Node) {
	var target enode.NodeID
	rand.Read(target[:])
	t.Lookup(target, queryFn)
}

// Len returns the total number of nodes in all buckets.
func (t *Table) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	count := 0
	for i := range t.buckets {
		count += len(t.buckets[i].entries)
	}
	return count
}

// Nodes returns all nodes in the table.
func (t *Table) Nodes() []*enode.Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var all []*enode.Node
	for i := range t.buckets {
		all = append(all, t.buckets[i].entries...)
	}
	return all
}

// BucketEntries returns the entries in a specific bucket.
func (t *Table) BucketEntries(idx int) []*enode.Node {
	if idx < 0 || idx >= NumBuckets {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	entries := make([]*enode.Node, len(t.buckets[idx].entries))
	copy(entries, t.buckets[idx].entries)
	return entries
}
