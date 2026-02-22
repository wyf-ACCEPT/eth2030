// Package discv5 implements the Ethereum Node Discovery Protocol v5.
// It manages a Kademlia DHT routing table, performs iterative lookups,
// and provides table maintenance via periodic pings and bucket refreshes.
package discv5

import (
	"crypto/rand"
	"errors"
	"net"
	"sort"
	"sync"
	"time"
)

// Kademlia DHT constants.
const (
	NumBuckets            = 256 // one bucket per possible log distance (1-256)
	BucketSize            = 16  // max nodes per bucket
	DefaultMaxPeers       = 256
	DefaultRefreshSeconds = 30
	DefaultPingTimeout    = 5 * time.Second
)

// Errors returned by DiscV5.
var (
	ErrClosed       = errors.New("discv5: closed")
	ErrSelf         = errors.New("discv5: cannot add self")
	ErrBucketFull   = errors.New("discv5: bucket full")
	ErrNodeNotFound = errors.New("discv5: node not found")
)

// NodeID is a 32-byte identifier for a node, typically keccak256(pubkey).
type NodeID [32]byte

// IsZero returns true if the ID is all zeros.
func (id NodeID) IsZero() bool { return id == NodeID{} }

// NodeRecord holds addressing and identity information for a network node.
type NodeRecord struct {
	NodeID    NodeID
	IP        net.IP
	UDPPort   uint16
	TCPPort   uint16
	SeqNumber uint64
	ENRRecord []byte // raw ENR bytes (optional)
}

// bucketEntry wraps a NodeRecord with discovery metadata.
type bucketEntry struct {
	Record   *NodeRecord
	LastSeen time.Time
	Failed   int // consecutive ping failures
}

// KBucket holds nodes at a specific XOR log distance.
type KBucket struct {
	entries []*bucketEntry
}

// Config controls DiscV5 behavior.
type Config struct {
	BucketSize      int           // max entries per bucket (default 16)
	MaxPeers        int           // max total peers tracked
	RefreshInterval time.Duration // interval between random lookups
	PingTimeout     time.Duration // how long to wait for a pong
}

func (c *Config) defaults() {
	if c.BucketSize <= 0 {
		c.BucketSize = BucketSize
	}
	if c.MaxPeers <= 0 {
		c.MaxPeers = DefaultMaxPeers
	}
	if c.RefreshInterval <= 0 {
		c.RefreshInterval = time.Duration(DefaultRefreshSeconds) * time.Second
	}
	if c.PingTimeout <= 0 {
		c.PingTimeout = DefaultPingTimeout
	}
}

// Transport abstracts the underlying UDP I/O for testing.
type Transport interface {
	// Ping sends a ping to the node and returns true if a pong is received.
	Ping(node *NodeRecord) bool
	// FindNode queries a node for neighbors at the given log distance (0-256).
	FindNode(target *NodeRecord, distance int) []*NodeRecord
	// RequestENR requests the current ENR from a node.
	RequestENR(node *NodeRecord) (*NodeRecord, error)
}

// DiscV5 manages node discovery via a Kademlia DHT.
type DiscV5 struct {
	mu        sync.RWMutex
	self      NodeID
	buckets   [NumBuckets]KBucket
	config    Config
	transport Transport
	closed    bool
	closeCh   chan struct{}
	wg        sync.WaitGroup
}

// New creates a new DiscV5 instance. The selfID identifies the local node.
// If transport is nil, network operations (Ping, FindNode) will be no-ops.
func New(selfID NodeID, cfg Config, transport Transport) *DiscV5 {
	cfg.defaults()
	return &DiscV5{
		self:      selfID,
		config:    cfg,
		transport: transport,
		closeCh:   make(chan struct{}),
	}
}

// Self returns the local node ID.
func (d *DiscV5) Self() NodeID { return d.self }

// Start begins background table maintenance (periodic refresh).
func (d *DiscV5) Start() {
	d.wg.Add(1)
	go d.maintainLoop()
}

// Stop shuts down background goroutines.
func (d *DiscV5) Stop() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	close(d.closeCh)
	d.mu.Unlock()
	d.wg.Wait()
}

// Distance returns the XOR log distance between two node IDs.
// Returns 0 if a == b. Range is 1-256 for distinct IDs.
func Distance(a, b NodeID) int {
	lz := 0
	for i := 0; i < 32; i++ {
		x := a[i] ^ b[i]
		if x == 0 {
			lz += 8
			continue
		}
		// Count leading zeros in the byte.
		for bit := 7; bit >= 0; bit-- {
			if x&(1<<uint(bit)) != 0 {
				return 256 - lz
			}
			lz++
		}
	}
	return 0 // identical
}

// bucketIndex returns the bucket index (0-255) for a given node ID.
// Returns -1 for the local node (distance 0).
func (d *DiscV5) bucketIndex(id NodeID) int {
	dist := Distance(d.self, id)
	if dist == 0 {
		return -1
	}
	return dist - 1
}

// AddNode inserts a node into the appropriate bucket. If the bucket is full,
// the node is silently dropped. Returns an error only for self-insertion.
func (d *DiscV5) AddNode(rec *NodeRecord) error {
	if rec.NodeID == d.self {
		return ErrSelf
	}
	idx := d.bucketIndex(rec.NodeID)
	if idx < 0 {
		return ErrSelf
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	b := &d.buckets[idx]

	// Check duplicate.
	for _, e := range b.entries {
		if e.Record.NodeID == rec.NodeID {
			e.LastSeen = time.Now()
			return nil
		}
	}

	if len(b.entries) >= d.config.BucketSize {
		return ErrBucketFull
	}

	b.entries = append(b.entries, &bucketEntry{
		Record:   rec,
		LastSeen: time.Now(),
	})
	return nil
}

// RemoveNode removes a node from the table.
func (d *DiscV5) RemoveNode(id NodeID) {
	idx := d.bucketIndex(id)
	if idx < 0 {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	b := &d.buckets[idx]
	for i, e := range b.entries {
		if e.Record.NodeID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			return
		}
	}
}

// GetNode returns the NodeRecord for a given ID, or nil if not found.
func (d *DiscV5) GetNode(id NodeID) *NodeRecord {
	idx := d.bucketIndex(id)
	if idx < 0 {
		return nil
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, e := range d.buckets[idx].entries {
		if e.Record.NodeID == id {
			return e.Record
		}
	}
	return nil
}

// Len returns the total number of nodes in all buckets.
func (d *DiscV5) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for i := range d.buckets {
		count += len(d.buckets[i].entries)
	}
	return count
}

// BucketLen returns the number of entries in the given bucket (0-255).
func (d *DiscV5) BucketLen(idx int) int {
	if idx < 0 || idx >= NumBuckets {
		return 0
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.buckets[idx].entries)
}

// Ping pings a node using the transport and updates its last-seen time
// or increments its failure count. Returns true if the pong was received.
func (d *DiscV5) Ping(node *NodeRecord) bool {
	if d.transport == nil {
		return false
	}
	ok := d.transport.Ping(node)

	d.mu.Lock()
	defer d.mu.Unlock()

	idx := d.bucketIndex(node.NodeID)
	if idx < 0 {
		return ok
	}
	for _, e := range d.buckets[idx].entries {
		if e.Record.NodeID == node.NodeID {
			if ok {
				e.LastSeen = time.Now()
				e.Failed = 0
			} else {
				e.Failed++
			}
			break
		}
	}
	return ok
}

// FindNode queries the target node for neighbors at a specific log distance
// using the transport. Results are added to the local table.
func (d *DiscV5) FindNode(target *NodeRecord, distance int) []*NodeRecord {
	if d.transport == nil {
		return nil
	}
	results := d.transport.FindNode(target, distance)
	for _, r := range results {
		d.AddNode(r) // best-effort insert
	}
	return results
}

// RequestENR requests the current ENR from a node via the transport.
func (d *DiscV5) RequestENR(node *NodeRecord) (*NodeRecord, error) {
	if d.transport == nil {
		return nil, errors.New("discv5: no transport")
	}
	return d.transport.RequestENR(node)
}

// ClosestNodes returns up to count nodes closest to the target ID from the
// local routing table, sorted by XOR distance.
func (d *DiscV5) ClosestNodes(target NodeID, count int) []*NodeRecord {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var all []*NodeRecord
	for i := range d.buckets {
		for _, e := range d.buckets[i].entries {
			all = append(all, e.Record)
		}
	}

	sort.Slice(all, func(i, j int) bool {
		di := Distance(target, all[i].NodeID)
		dj := Distance(target, all[j].NodeID)
		return di < dj
	})

	if len(all) > count {
		all = all[:count]
	}
	return all
}

// Lookup performs an iterative Kademlia lookup for the target, querying the
// closest known nodes and iterating until no closer nodes are found.
// Returns up to BucketSize closest nodes.
func (d *DiscV5) Lookup(target NodeID) []*NodeRecord {
	const alpha = 3 // concurrency factor

	closest := d.ClosestNodes(target, d.config.BucketSize)
	if len(closest) == 0 {
		return nil
	}

	asked := make(map[NodeID]bool)
	asked[d.self] = true

	for {
		// Pick up to alpha un-asked nodes from the closest set.
		var toAsk []*NodeRecord
		for _, n := range closest {
			if !asked[n.NodeID] {
				toAsk = append(toAsk, n)
				if len(toAsk) >= alpha {
					break
				}
			}
		}
		if len(toAsk) == 0 {
			break
		}

		improved := false
		for _, n := range toAsk {
			asked[n.NodeID] = true

			// Query the node for neighbors at the target distance.
			dist := Distance(d.self, target)
			if dist == 0 {
				dist = 1
			}
			results := d.FindNode(n, dist)
			for _, r := range results {
				if asked[r.NodeID] || r.NodeID == d.self {
					continue
				}
				// Check if this node is closer than our furthest.
				if len(closest) < d.config.BucketSize {
					closest = insertSorted(closest, r, target)
					improved = true
				} else {
					lastDist := Distance(target, closest[len(closest)-1].NodeID)
					newDist := Distance(target, r.NodeID)
					if newDist < lastDist {
						closest = insertSorted(closest, r, target)
						if len(closest) > d.config.BucketSize {
							closest = closest[:d.config.BucketSize]
						}
						improved = true
					}
				}
			}
		}
		if !improved {
			break
		}
	}
	return closest
}

// RandomLookup discovers random nodes for table maintenance by performing
// a lookup on a randomly generated target ID.
func (d *DiscV5) RandomLookup() []*NodeRecord {
	var target NodeID
	rand.Read(target[:])
	return d.Lookup(target)
}

// EvictStale removes nodes that have exceeded maxFailures consecutive
// ping failures. Returns the number of nodes evicted.
func (d *DiscV5) EvictStale(maxFailures int) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	evicted := 0
	for i := range d.buckets {
		b := &d.buckets[i]
		j := 0
		for _, e := range b.entries {
			if e.Failed < maxFailures {
				b.entries[j] = e
				j++
			} else {
				evicted++
			}
		}
		b.entries = b.entries[:j]
	}
	return evicted
}

// NodesAtDistance returns all nodes in the bucket corresponding to the
// given log distance (1-256). Distance 0 returns nil (self).
func (d *DiscV5) NodesAtDistance(distance int) []*NodeRecord {
	if distance <= 0 || distance > NumBuckets {
		return nil
	}
	idx := distance - 1

	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*NodeRecord, 0, len(d.buckets[idx].entries))
	for _, e := range d.buckets[idx].entries {
		result = append(result, e.Record)
	}
	return result
}

// maintainLoop periodically refreshes the routing table.
func (d *DiscV5) maintainLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.closeCh:
			return
		case <-ticker.C:
			d.refreshBuckets()
		}
	}
}

// refreshBuckets pings all nodes and evicts those with too many failures.
func (d *DiscV5) refreshBuckets() {
	// Collect a snapshot of all nodes.
	d.mu.RLock()
	var nodes []*NodeRecord
	for i := range d.buckets {
		for _, e := range d.buckets[i].entries {
			nodes = append(nodes, e.Record)
		}
	}
	d.mu.RUnlock()

	// Ping each node (outside the lock).
	for _, n := range nodes {
		d.Ping(n)
	}

	// Evict nodes with >= 3 consecutive failures.
	d.EvictStale(3)

	// Perform a random lookup to discover new nodes.
	d.RandomLookup()
}

// insertSorted inserts a record into a sorted slice, maintaining order by
// distance to the target. Deduplicates by NodeID.
func insertSorted(nodes []*NodeRecord, n *NodeRecord, target NodeID) []*NodeRecord {
	for _, existing := range nodes {
		if existing.NodeID == n.NodeID {
			return nodes
		}
	}
	dist := Distance(target, n.NodeID)
	i := sort.Search(len(nodes), func(i int) bool {
		return Distance(target, nodes[i].NodeID) > dist
	})
	nodes = append(nodes, nil)
	copy(nodes[i+1:], nodes[i:])
	nodes[i] = n
	return nodes
}
