package portal

import (
	"math/big"
	"sort"
	"sync"
)

// Routing table constants for the portal content-addressed DHT.
const (
	BucketSize      = 16  // max entries per k-bucket
	NumBuckets      = 256 // one bucket per XOR log distance
	LookupAlpha     = 3   // concurrent lookup parallelism
	MaxReplacements = 10  // replacement cache per bucket
)

// PeerInfo holds metadata about a portal peer in the routing table.
type PeerInfo struct {
	// NodeID is the 32-byte identifier used for XOR distance calculation.
	NodeID [32]byte
	// ENR is the serialized Ethereum Node Record (optional, may be nil).
	ENR []byte
	// Radius is the advertised content radius.
	Radius NodeRadius
	// DataRadius tracks the last known radius for content distribution.
	DataRadius *big.Int
}

// Bucket holds peers at a given XOR log distance from the local node.
type Bucket struct {
	entries      []*PeerInfo
	replacements []*PeerInfo
}

// RoutingTable is a Kademlia-like k-bucket structure specialized for the
// Portal content-addressed DHT overlay.
type RoutingTable struct {
	mu      sync.RWMutex
	self    [32]byte
	buckets [NumBuckets]Bucket
	radius  NodeRadius
}

// NewRoutingTable creates a routing table for the given local node ID.
func NewRoutingTable(self [32]byte) *RoutingTable {
	return &RoutingTable{
		self:   self,
		radius: MaxRadius(),
	}
}

// Self returns the local node ID.
func (rt *RoutingTable) Self() [32]byte {
	return rt.self
}

// Radius returns the local node's advertised content radius.
func (rt *RoutingTable) Radius() NodeRadius {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.radius
}

// BucketIndex returns the bucket index for a given node ID.
// Returns -1 for the local node (distance 0).
func (rt *RoutingTable) BucketIndex(id [32]byte) int {
	dist := LogDistance(rt.self, id)
	if dist == 0 {
		return -1
	}
	return dist - 1
}

// AddPeer adds a peer to the appropriate bucket. If the bucket is full,
// the peer goes into the replacement cache.
func (rt *RoutingTable) AddPeer(peer *PeerInfo) {
	if peer.NodeID == rt.self {
		return
	}
	idx := rt.BucketIndex(peer.NodeID)
	if idx < 0 {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	b := &rt.buckets[idx]

	// Update existing entry.
	for i, e := range b.entries {
		if e.NodeID == peer.NodeID {
			b.entries[i] = peer
			return
		}
	}

	if len(b.entries) < BucketSize {
		b.entries = append(b.entries, peer)
		return
	}

	// Bucket full: add to replacements.
	for i, e := range b.replacements {
		if e.NodeID == peer.NodeID {
			b.replacements[i] = peer
			return
		}
	}
	if len(b.replacements) < MaxReplacements {
		b.replacements = append(b.replacements, peer)
	}
}

// RemovePeer removes a peer by ID and promotes a replacement if available.
func (rt *RoutingTable) RemovePeer(id [32]byte) {
	idx := rt.BucketIndex(id)
	if idx < 0 {
		return
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	b := &rt.buckets[idx]
	for i, e := range b.entries {
		if e.NodeID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			if len(b.replacements) > 0 {
				b.entries = append(b.entries, b.replacements[0])
				b.replacements = b.replacements[1:]
			}
			return
		}
	}
}

// GetPeer returns the PeerInfo for the given node ID, or nil if not found.
func (rt *RoutingTable) GetPeer(id [32]byte) *PeerInfo {
	idx := rt.BucketIndex(id)
	if idx < 0 {
		return nil
	}

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	for _, e := range rt.buckets[idx].entries {
		if e.NodeID == id {
			return e
		}
	}
	return nil
}

// ClosestPeers returns up to count peers closest to the target by XOR distance.
func (rt *RoutingTable) ClosestPeers(target [32]byte, count int) []*PeerInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var all []*PeerInfo
	for i := range rt.buckets {
		all = append(all, rt.buckets[i].entries...)
	}

	sort.Slice(all, func(i, j int) bool {
		di := Distance(target, all[i].NodeID)
		dj := Distance(target, all[j].NodeID)
		return di.Cmp(dj) < 0
	})

	if len(all) > count {
		all = all[:count]
	}
	return all
}

// PeersInRadius returns peers whose node IDs are within the given radius of target.
func (rt *RoutingTable) PeersInRadius(target [32]byte, radius NodeRadius) []*PeerInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var result []*PeerInfo
	for i := range rt.buckets {
		for _, p := range rt.buckets[i].entries {
			dist := Distance(target, p.NodeID)
			if dist.Cmp(radius.Raw) <= 0 {
				result = append(result, p)
			}
		}
	}
	return result
}

// Len returns the total number of peers in the table.
func (rt *RoutingTable) Len() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	count := 0
	for i := range rt.buckets {
		count += len(rt.buckets[i].entries)
	}
	return count
}

// AllPeers returns a snapshot of all peers in the table.
func (rt *RoutingTable) AllPeers() []*PeerInfo {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	var all []*PeerInfo
	for i := range rt.buckets {
		all = append(all, rt.buckets[i].entries...)
	}
	return all
}

// BucketEntries returns a copy of entries in a specific bucket.
func (rt *RoutingTable) BucketEntries(idx int) []*PeerInfo {
	if idx < 0 || idx >= NumBuckets {
		return nil
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	entries := make([]*PeerInfo, len(rt.buckets[idx].entries))
	copy(entries, rt.buckets[idx].entries)
	return entries
}

// ContentLookupResult is returned from a content lookup operation.
type ContentLookupResult struct {
	// Found is true if the content was located.
	Found bool
	// Content holds the raw content data if found.
	Content []byte
	// ClosestPeers holds the closest peers encountered during lookup.
	ClosestPeers []*PeerInfo
	// Source is the peer that provided the content.
	Source *PeerInfo
}

// ContentQueryFn is called to query a peer for content. It returns the content
// data if the peer has it, or a list of closer peers otherwise.
type ContentQueryFn func(peer *PeerInfo, contentKey []byte) (content []byte, closerPeers []*PeerInfo, err error)

// ContentLookup performs an iterative content lookup in the DHT.
// It queries progressively closer peers until content is found or no
// closer peers remain.
func (rt *RoutingTable) ContentLookup(contentKey []byte, queryFn ContentQueryFn) ContentLookupResult {
	contentID := ComputeContentID(contentKey)

	// Seed with the closest known peers.
	closest := rt.ClosestPeers(contentID, BucketSize)
	if len(closest) == 0 {
		return ContentLookupResult{}
	}

	asked := make(map[[32]byte]bool)
	asked[rt.self] = true

	for {
		// Pick up to Alpha unasked peers from closest set.
		var toAsk []*PeerInfo
		for _, p := range closest {
			if !asked[p.NodeID] {
				toAsk = append(toAsk, p)
				if len(toAsk) >= LookupAlpha {
					break
				}
			}
		}
		if len(toAsk) == 0 {
			break
		}

		improved := false
		for _, p := range toAsk {
			asked[p.NodeID] = true
			content, closerPeers, err := queryFn(p, contentKey)
			if err != nil {
				continue
			}
			if content != nil {
				return ContentLookupResult{
					Found:        true,
					Content:      content,
					ClosestPeers: closest,
					Source:       p,
				}
			}
			// Merge closer peers into the closest set.
			for _, cp := range closerPeers {
				if asked[cp.NodeID] {
					continue
				}
				rt.AddPeer(cp)
				closest = insertPeerSorted(closest, cp, contentID)
				if len(closest) > BucketSize {
					closest = closest[:BucketSize]
				}
				improved = true
			}
		}
		if !improved {
			break
		}
	}

	return ContentLookupResult{
		ClosestPeers: closest,
	}
}

// insertPeerSorted inserts a peer into a sorted slice, maintaining sort by
// distance to target. Deduplicates by NodeID.
func insertPeerSorted(peers []*PeerInfo, p *PeerInfo, target ContentID) []*PeerInfo {
	for _, existing := range peers {
		if existing.NodeID == p.NodeID {
			return peers
		}
	}
	i := sort.Search(len(peers), func(i int) bool {
		dp := Distance(target, p.NodeID)
		di := Distance(target, peers[i].NodeID)
		return dp.Cmp(di) < 0
	})
	peers = append(peers, nil)
	copy(peers[i+1:], peers[i:])
	peers[i] = p
	return peers
}

// FindContentPeers returns peers that should store the given content based on
// content ID proximity and their advertised radius.
func (rt *RoutingTable) FindContentPeers(contentKey []byte, count int) []*PeerInfo {
	contentID := ComputeContentID(contentKey)
	closest := rt.ClosestPeers(contentID, count*2)

	var result []*PeerInfo
	for _, p := range closest {
		if p.Radius.Contains(p.NodeID, contentID) {
			result = append(result, p)
			if len(result) >= count {
				break
			}
		}
	}
	return result
}

// OfferResult records which peers accepted offered content keys.
type OfferResult struct {
	PeerID   [32]byte
	Accepted []bool // per content key
}

// OfferFn is called to offer content keys to a peer. Returns the acceptance bitfield.
type OfferFn func(peer *PeerInfo, contentKeys [][]byte) (accepted []bool, err error)

// OfferContent distributes content keys to the closest peers that should
// store them. Returns which peers accepted which keys.
func (rt *RoutingTable) OfferContent(contentKeys [][]byte, offerFn OfferFn) []OfferResult {
	if len(contentKeys) == 0 {
		return nil
	}

	// Find peers close to the first content key as a heuristic.
	contentID := ComputeContentID(contentKeys[0])
	peers := rt.ClosestPeers(contentID, BucketSize)

	var results []OfferResult
	for _, p := range peers {
		accepted, err := offerFn(p, contentKeys)
		if err != nil {
			continue
		}
		results = append(results, OfferResult{
			PeerID:   p.NodeID,
			Accepted: accepted,
		})
	}
	return results
}

// RadiusUpdate adjusts the local node radius based on current storage usage.
// storageUsed and storageCapacity are in bytes. The radius shrinks as storage
// fills up, reducing the content this node is responsible for.
func (rt *RoutingTable) RadiusUpdate(storageUsed, storageCapacity uint64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if storageCapacity == 0 {
		rt.radius = ZeroRadius()
		return
	}

	if storageUsed == 0 {
		rt.radius = MaxRadius()
		return
	}

	// Scale: ratio = (capacity - used) / capacity
	// New radius = maxRadius * ratio
	// This linearly shrinks the radius as storage fills.
	maxRadius := MaxRadius().Raw
	remaining := storageCapacity - storageUsed
	if remaining > storageCapacity {
		// Overflow: used > capacity.
		rt.radius = ZeroRadius()
		return
	}

	// newRadius = maxRadius * remaining / capacity
	numerator := new(big.Int).Mul(maxRadius, new(big.Int).SetUint64(remaining))
	newRadius := new(big.Int).Div(numerator, new(big.Int).SetUint64(storageCapacity))
	rt.radius = NodeRadius{Raw: newRadius}
}
