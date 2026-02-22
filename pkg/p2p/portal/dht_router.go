// dht_router.go implements Portal network DHT routing for content-addressed
// lookups. It bridges the Kademlia DHT table from the discover package with
// the Portal network's content ID space and radius-based content placement.
//
// Reference: https://github.com/ethereum/portal-network-specs
package portal

import (
	"errors"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/p2p/discover"
)

// DHT routing errors.
var (
	ErrDHTNoNodes        = errors.New("portal/dht: no nodes available for lookup")
	ErrDHTContentTimeout = errors.New("portal/dht: content lookup timed out")
	ErrDHTLookupFailed   = errors.New("portal/dht: iterative lookup failed")
	ErrDHTOfferRejected  = errors.New("portal/dht: all peers rejected offer")
	ErrDHTInvalidContent = errors.New("portal/dht: invalid content ID")
)

// DHTRouterConfig configures the DHT router behavior.
type DHTRouterConfig struct {
	// MaxLookupIterations caps the number of iterative query rounds.
	// Default: 10.
	MaxLookupIterations int

	// LookupAlpha is the concurrency factor for parallel queries.
	// Default: 3.
	LookupAlpha int

	// LookupResultSize is the number of closest nodes to track.
	// Default: 16.
	LookupResultSize int

	// MaxOfferPeers is the maximum number of peers to offer content to.
	// Default: 8.
	MaxOfferPeers int

	// MinRadiusPercent is the minimum radius as a percentage of max (0-100).
	// Default: 1 (meaning 1% of max radius).
	MinRadiusPercent int
}

// DefaultDHTRouterConfig returns default configuration values.
func DefaultDHTRouterConfig() DHTRouterConfig {
	return DHTRouterConfig{
		MaxLookupIterations: 10,
		LookupAlpha:         3,
		LookupResultSize:    16,
		MaxOfferPeers:       8,
		MinRadiusPercent:     1,
	}
}

func (c *DHTRouterConfig) applyDefaults() {
	if c.MaxLookupIterations <= 0 {
		c.MaxLookupIterations = 10
	}
	if c.LookupAlpha <= 0 {
		c.LookupAlpha = 3
	}
	if c.LookupResultSize <= 0 {
		c.LookupResultSize = 16
	}
	if c.MaxOfferPeers <= 0 {
		c.MaxOfferPeers = 8
	}
	if c.MinRadiusPercent <= 0 {
		c.MinRadiusPercent = 1
	}
}

// ContentResponse holds the result of a content lookup from the DHT.
type ContentResponse struct {
	// Data is the raw content bytes.
	Data []byte
	// Proof is an optional proof (e.g., Merkle proof) for the content.
	Proof []byte
	// FoundNode is the node that provided the content.
	FoundNode discover.NodeEntry
}

// DHTQueryFunc is called to query a remote node for content or closer nodes.
// It returns content data if the node has it, or a list of closer node entries.
type DHTQueryFunc func(node discover.NodeEntry, contentID [32]byte) (
	data []byte, proof []byte, closerNodes []discover.NodeEntry, err error,
)

// DHTRouter manages content-addressed routing in the Portal network DHT.
// It uses a KademliaTable from the discover package for node management and
// adds content radius tracking for Portal-specific content placement.
type DHTRouter struct {
	mu            sync.RWMutex
	table         *discover.KademliaTable
	nodeID        [32]byte
	contentRadius *big.Int
	config        DHTRouterConfig
}

// NewDHTRouter creates a new DHT router with the given Kademlia table and config.
func NewDHTRouter(table *discover.KademliaTable, config DHTRouterConfig) *DHTRouter {
	config.applyDefaults()
	return &DHTRouter{
		table:         table,
		nodeID:        table.SelfID(),
		contentRadius: MaxRadius().Raw,
		config:        config,
	}
}

// Table returns the underlying Kademlia table.
func (r *DHTRouter) Table() *discover.KademliaTable {
	return r.table
}

// NodeID returns the local node's identifier.
func (r *DHTRouter) NodeID() [32]byte {
	return r.nodeID
}

// ContentRadius returns the current content radius.
func (r *DHTRouter) ContentRadius() *big.Int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return new(big.Int).Set(r.contentRadius)
}

// ComputeContentDistance computes the XOR distance between a node ID and
// a content ID. Both are 32-byte identifiers; the distance is the big-endian
// interpretation of their bitwise XOR.
func ComputeContentDist(nodeID, contentID [32]byte) *big.Int {
	var xored [32]byte
	for i := 0; i < 32; i++ {
		xored[i] = nodeID[i] ^ contentID[i]
	}
	return new(big.Int).SetBytes(xored[:])
}

// IsWithinRadius reports whether a content ID falls within this router's
// content storage radius.
func (r *DHTRouter) IsWithinRadius(contentID [32]byte) bool {
	dist := ComputeContentDist(r.nodeID, contentID)
	r.mu.RLock()
	defer r.mu.RUnlock()
	return dist.Cmp(r.contentRadius) <= 0
}

// FindContentProviders locates nodes from the routing table that should have
// the specified content based on XOR distance and content radius heuristics.
// Returns up to maxProviders nodes sorted by distance to the content ID.
func (r *DHTRouter) FindContentProviders(contentID [32]byte, maxProviders int) []discover.NodeEntry {
	if maxProviders <= 0 {
		return nil
	}

	// Find the closest nodes to the content ID.
	candidates := r.table.FindClosest(contentID, maxProviders*2)

	// Filter and sort by distance.
	type distEntry struct {
		node discover.NodeEntry
		dist *big.Int
	}
	var entries []distEntry
	for _, c := range candidates {
		dist := ComputeContentDist(c.ID, contentID)
		entries = append(entries, distEntry{node: c, dist: dist})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].dist.Cmp(entries[j].dist) < 0
	})

	result := make([]discover.NodeEntry, 0, maxProviders)
	for _, e := range entries {
		result = append(result, e.node)
		if len(result) >= maxProviders {
			break
		}
	}
	return result
}

// RouteContentRequest performs a content lookup by querying nodes iteratively.
// It starts with the closest known nodes and queries progressively closer nodes
// until the content is found or no closer nodes are available.
func (r *DHTRouter) RouteContentRequest(contentID [32]byte, queryFn DHTQueryFunc) (*ContentResponse, error) {
	closest := r.table.FindClosest(contentID, r.config.LookupResultSize)
	if len(closest) == 0 {
		return nil, ErrDHTNoNodes
	}

	asked := make(map[[32]byte]bool)
	asked[r.nodeID] = true

	for round := 0; round < r.config.MaxLookupIterations; round++ {
		// Select up to Alpha un-asked nodes from the closest set.
		var toAsk []discover.NodeEntry
		for _, n := range closest {
			if !asked[n.ID] {
				toAsk = append(toAsk, n)
				if len(toAsk) >= r.config.LookupAlpha {
					break
				}
			}
		}
		if len(toAsk) == 0 {
			break
		}

		improved := false
		for _, n := range toAsk {
			asked[n.ID] = true

			data, proof, closerNodes, err := queryFn(n, contentID)
			if err != nil {
				r.table.RecordFailure(n.ID)
				continue
			}

			// Content found.
			if data != nil {
				return &ContentResponse{
					Data:      data,
					Proof:     proof,
					FoundNode: n,
				}, nil
			}

			// Merge closer nodes into our candidate set.
			for _, cn := range closerNodes {
				if asked[cn.ID] || cn.ID == r.nodeID {
					continue
				}
				r.table.AddNode(cn)
				closest = insertNodeSorted(closest, cn, contentID)
				if len(closest) > r.config.LookupResultSize {
					closest = closest[:r.config.LookupResultSize]
				}
				improved = true
			}
		}

		if !improved {
			break
		}
	}

	return nil, ErrDHTLookupFailed
}

// IterativeNodeLookup performs an iterative Kademlia node lookup for the
// given target ID. Unlike RouteContentRequest, this looks for nodes rather
// than content. Returns the closest nodes found.
func (r *DHTRouter) IterativeNodeLookup(target [32]byte, queryFn func(discover.NodeEntry) []discover.NodeEntry) []discover.NodeEntry {
	closest := r.table.FindClosest(target, r.config.LookupResultSize)
	if len(closest) == 0 {
		return nil
	}

	asked := make(map[[32]byte]bool)
	asked[r.nodeID] = true

	for round := 0; round < r.config.MaxLookupIterations; round++ {
		var toAsk []discover.NodeEntry
		for _, n := range closest {
			if !asked[n.ID] {
				toAsk = append(toAsk, n)
				if len(toAsk) >= r.config.LookupAlpha {
					break
				}
			}
		}
		if len(toAsk) == 0 {
			break
		}

		improved := false
		for _, n := range toAsk {
			asked[n.ID] = true
			results := queryFn(n)
			for _, rn := range results {
				if asked[rn.ID] || rn.ID == r.nodeID {
					continue
				}
				r.table.AddNode(rn)
				closest = insertNodeSorted(closest, rn, target)
				if len(closest) > r.config.LookupResultSize {
					closest = closest[:r.config.LookupResultSize]
				}
				improved = true
			}
		}

		if !improved {
			break
		}
	}

	return closest
}

// OfferContent offers a set of content keys to nodes that are close to each
// content ID. It sends OFFER messages to nearby nodes and returns the number
// of nodes that accepted.
func (r *DHTRouter) OfferContent(contentKeys [][32]byte, offerFn func(node discover.NodeEntry, keys [][32]byte) ([]bool, error)) (int, error) {
	if len(contentKeys) == 0 {
		return 0, nil
	}

	// Use the first content key's ID to find nearby nodes.
	providers := r.FindContentProviders(contentKeys[0], r.config.MaxOfferPeers)
	if len(providers) == 0 {
		return 0, ErrDHTOfferRejected
	}

	accepted := 0
	for _, p := range providers {
		results, err := offerFn(p, contentKeys)
		if err != nil {
			continue
		}
		for _, ok := range results {
			if ok {
				accepted++
				break // count each peer only once
			}
		}
	}

	if accepted == 0 {
		return 0, ErrDHTOfferRejected
	}
	return accepted, nil
}

// UpdateRadius adjusts the content radius based on current storage utilization.
// When storage is near capacity, the radius shrinks so the node takes responsibility
// for less content. When storage is mostly free, the radius expands.
func (r *DHTRouter) UpdateRadius(totalStored, maxStorage uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if maxStorage == 0 {
		r.contentRadius = new(big.Int)
		return
	}

	if totalStored == 0 {
		r.contentRadius = MaxRadius().Raw
		return
	}

	if totalStored >= maxStorage {
		// Apply minimum radius.
		minRadius := computeMinRadius(r.config.MinRadiusPercent)
		r.contentRadius = minRadius
		return
	}

	// Scale radius linearly: maxRadius * (remaining / capacity).
	maxRad := MaxRadius().Raw
	remaining := maxStorage - totalStored
	numerator := new(big.Int).Mul(maxRad, new(big.Int).SetUint64(remaining))
	newRadius := new(big.Int).Div(numerator, new(big.Int).SetUint64(maxStorage))

	// Enforce minimum radius.
	minRadius := computeMinRadius(r.config.MinRadiusPercent)
	if newRadius.Cmp(minRadius) < 0 {
		newRadius = minRadius
	}

	r.contentRadius = newRadius
}

// computeMinRadius returns the minimum radius as a fraction of the max radius.
func computeMinRadius(percent int) *big.Int {
	if percent <= 0 {
		return new(big.Int)
	}
	if percent >= 100 {
		return MaxRadius().Raw
	}
	maxRad := MaxRadius().Raw
	result := new(big.Int).Mul(maxRad, big.NewInt(int64(percent)))
	result.Div(result, big.NewInt(100))
	return result
}

// insertNodeSorted inserts a node entry into a sorted (by XOR distance to
// target) slice, maintaining sort order and deduplicating by ID.
func insertNodeSorted(nodes []discover.NodeEntry, n discover.NodeEntry, target [32]byte) []discover.NodeEntry {
	for _, existing := range nodes {
		if existing.ID == n.ID {
			return nodes
		}
	}

	nDist := ComputeContentDist(target, n.ID)
	i := sort.Search(len(nodes), func(i int) bool {
		iDist := ComputeContentDist(target, nodes[i].ID)
		return nDist.Cmp(iDist) < 0
	})

	nodes = append(nodes, discover.NodeEntry{})
	copy(nodes[i+1:], nodes[i:])
	nodes[i] = n
	return nodes
}

// NodeDistanceInfo holds information about a node's distance to content.
type NodeDistanceInfo struct {
	Node     discover.NodeEntry
	Distance *big.Int
}

// RankedProviders returns nodes ranked by distance to the given content ID,
// along with their distances. This is useful for diagnostics and provider selection.
func (r *DHTRouter) RankedProviders(contentID [32]byte, count int) []NodeDistanceInfo {
	candidates := r.table.FindClosest(contentID, count)
	result := make([]NodeDistanceInfo, len(candidates))
	for i, c := range candidates {
		result[i] = NodeDistanceInfo{
			Node:     c,
			Distance: ComputeContentDist(c.ID, contentID),
		}
	}
	return result
}
