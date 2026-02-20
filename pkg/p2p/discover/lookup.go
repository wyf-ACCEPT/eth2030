// lookup.go implements an enhanced iterative Kademlia lookup with alpha
// concurrent queries, XOR distance tracking, response deduplication,
// and lookup path recording for diagnostics.
package discover

import (
	"sort"
	"sync"

	"github.com/eth2028/eth2028/p2p/enode"
)

// LookupConfig controls the behavior of an iterative lookup.
type LookupConfig struct {
	// Alpha is the number of concurrent queries per round. Default: 3.
	Alpha int
	// ResultSize is the max number of closest nodes to return. Default: BucketSize (16).
	ResultSize int
	// MaxRounds caps the total number of query rounds. 0 = unlimited.
	MaxRounds int
}

func (c *LookupConfig) defaults() {
	if c.Alpha <= 0 {
		c.Alpha = Alpha
	}
	if c.ResultSize <= 0 {
		c.ResultSize = BucketSize
	}
}

// QueryFunc is called to query a remote node for neighbors of the target.
// It should return the list of nodes the remote knows about near the target.
type QueryFunc func(n *enode.Node) []*enode.Node

// LookupResult holds the outcome of an iterative Kademlia lookup.
type LookupResult struct {
	// Target is the ID that was looked up.
	Target enode.NodeID
	// Closest contains up to ResultSize nodes nearest to the target, sorted
	// by ascending XOR distance.
	Closest []*enode.Node
	// Path records each query hop: which node was queried and what it returned.
	Path []LookupHop
	// QueriedCount is the number of remote nodes that were actually queried.
	QueriedCount int
	// Rounds is the number of iterative query rounds performed.
	Rounds int
}

// LookupHop records a single query step in the lookup.
type LookupHop struct {
	Queried  enode.NodeID   // the node that was queried
	Returned []enode.NodeID // IDs of nodes returned by the query
	Round    int            // round in which this hop occurred
}

// closestSet maintains a bounded, sorted set of nodes by distance to a target.
// It deduplicates by NodeID.
type closestSet struct {
	target enode.NodeID
	nodes  []*enode.Node
	seen   map[enode.NodeID]bool
	limit  int
}

// newClosestSet creates a closest-set tracker.
func newClosestSet(target enode.NodeID, limit int) *closestSet {
	return &closestSet{
		target: target,
		nodes:  make([]*enode.Node, 0, limit),
		seen:   make(map[enode.NodeID]bool),
		limit:  limit,
	}
}

// push adds a node to the set if it is not a duplicate and is close enough.
// Returns true if the node was actually inserted (i.e. it improved the set).
func (cs *closestSet) push(n *enode.Node) bool {
	if cs.seen[n.ID] {
		return false
	}
	cs.seen[n.ID] = true

	// If not full yet, always insert.
	if len(cs.nodes) < cs.limit {
		cs.insertSorted(n)
		return true
	}

	// Check if n is closer than the farthest node in the set.
	farthest := cs.nodes[len(cs.nodes)-1]
	if enode.DistCmp(cs.target, n.ID, farthest.ID) >= 0 {
		return false // not closer
	}

	// Replace the farthest node.
	cs.nodes = cs.nodes[:len(cs.nodes)-1]
	cs.insertSorted(n)
	return true
}

// insertSorted adds a node in distance-sorted order.
func (cs *closestSet) insertSorted(n *enode.Node) {
	i := sort.Search(len(cs.nodes), func(i int) bool {
		return enode.DistCmp(cs.target, n.ID, cs.nodes[i].ID) < 0
	})
	cs.nodes = append(cs.nodes, nil)
	copy(cs.nodes[i+1:], cs.nodes[i:])
	cs.nodes[i] = n
}

// result returns a copy of the sorted closest nodes.
func (cs *closestSet) result() []*enode.Node {
	out := make([]*enode.Node, len(cs.nodes))
	copy(out, cs.nodes)
	return out
}

// IterativeLookup performs a Kademlia iterative lookup from the routing table
// with alpha concurrent queries per round. It records the full lookup path
// and deduplicates responses.
func (t *Table) IterativeLookup(target enode.NodeID, queryFn QueryFunc, cfg LookupConfig) *LookupResult {
	cfg.defaults()

	result := &LookupResult{Target: target}
	closest := newClosestSet(target, cfg.ResultSize)
	asked := make(map[enode.NodeID]bool)
	asked[t.self] = true

	// Seed with the closest known nodes from the local table.
	seeds := t.FindNode(target, cfg.ResultSize)
	for _, s := range seeds {
		closest.push(s)
	}
	if len(closest.nodes) == 0 {
		return result
	}

	round := 0
	for {
		round++
		if cfg.MaxRounds > 0 && round > cfg.MaxRounds {
			break
		}

		// Select up to alpha un-asked nodes from the current closest set.
		var toAsk []*enode.Node
		for _, n := range closest.nodes {
			if !asked[n.ID] {
				toAsk = append(toAsk, n)
				if len(toAsk) >= cfg.Alpha {
					break
				}
			}
		}
		if len(toAsk) == 0 {
			break
		}

		// Query alpha nodes concurrently.
		type queryResult struct {
			queried enode.NodeID
			nodes   []*enode.Node
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		results := make([]queryResult, 0, len(toAsk))

		for _, n := range toAsk {
			asked[n.ID] = true
			wg.Add(1)
			go func(node *enode.Node) {
				defer wg.Done()
				resp := queryFn(node)
				mu.Lock()
				results = append(results, queryResult{
					queried: node.ID,
					nodes:   resp,
				})
				mu.Unlock()
			}(n)
		}
		wg.Wait()

		result.QueriedCount += len(toAsk)

		// Process results.
		improved := false
		for _, qr := range results {
			hop := LookupHop{
				Queried: qr.queried,
				Round:   round,
			}
			for _, r := range qr.nodes {
				if r.ID == t.self || asked[r.ID] {
					hop.Returned = append(hop.Returned, r.ID)
					continue
				}
				hop.Returned = append(hop.Returned, r.ID)
				// Add to routing table for future lookups.
				t.AddNode(r)
				if closest.push(r) {
					improved = true
				}
			}
			result.Path = append(result.Path, hop)
		}

		if !improved {
			break
		}
	}

	result.Closest = closest.result()
	result.Rounds = round
	return result
}

// XORDistance computes the raw XOR distance between two node IDs as a
// 32-byte big-endian value. This is useful for fine-grained distance
// comparisons beyond log distance.
func XORDistance(a, b enode.NodeID) [32]byte {
	var dist [32]byte
	for i := 0; i < 32; i++ {
		dist[i] = a[i] ^ b[i]
	}
	return dist
}

// CompareXORDistance compares XOR(a, target) vs XOR(b, target) and returns
// -1 if a is closer, +1 if b is closer, 0 if equal. This is a convenience
// wrapper around enode.DistCmp.
func CompareXORDistance(target, a, b enode.NodeID) int {
	return enode.DistCmp(target, a, b)
}

// LogDistance returns the XOR log distance between two node IDs (1-256),
// or 0 if they are identical. This is a convenience wrapper around
// enode.Distance.
func LogDistance(a, b enode.NodeID) int {
	return enode.Distance(a, b)
}
