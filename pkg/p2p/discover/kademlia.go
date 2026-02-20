// kademlia.go implements a standalone Kademlia DHT table with configurable
// bucket size, alpha concurrency, stale-node eviction, bucket refresh tracking,
// and random node selection for table maintenance.
package discover

import (
	"crypto/rand"
	"math/bits"
	"sort"
	"sync"
	"time"
)

// KademliaConfig controls the behavior of the Kademlia DHT table.
type KademliaConfig struct {
	// BucketSize is the maximum number of entries per k-bucket (k).
	// Default: 16 (standard Kademlia).
	BucketSize int

	// Alpha is the concurrency factor for parallel lookups.
	// Default: 3 (standard Kademlia).
	Alpha int

	// MaxTableSize caps the total number of nodes across all buckets.
	// 0 means unlimited (bounded only by BucketSize * 256).
	MaxTableSize int

	// RefreshInterval is the minimum time between bucket refreshes.
	// Default: 1 hour.
	RefreshInterval time.Duration

	// StaleTimeout is the maximum time a node can go without being seen
	// before it is considered stale and eligible for eviction.
	// Default: 24 hours.
	StaleTimeout time.Duration

	// MaxFailCount is the number of consecutive failures before a node
	// is removed from the table. Default: 5.
	MaxFailCount int

	// MaxReplacements is the maximum number of replacement entries per bucket.
	// Default: 10.
	MaxReplacements int
}

// DefaultKademliaConfig returns a KademliaConfig with standard defaults.
func DefaultKademliaConfig() KademliaConfig {
	return KademliaConfig{
		BucketSize:      16,
		Alpha:           3,
		MaxTableSize:    0,
		RefreshInterval: 1 * time.Hour,
		StaleTimeout:    24 * time.Hour,
		MaxFailCount:    5,
		MaxReplacements: 10,
	}
}

func (c *KademliaConfig) applyDefaults() {
	if c.BucketSize <= 0 {
		c.BucketSize = 16
	}
	if c.Alpha <= 0 {
		c.Alpha = 3
	}
	if c.RefreshInterval <= 0 {
		c.RefreshInterval = 1 * time.Hour
	}
	if c.StaleTimeout <= 0 {
		c.StaleTimeout = 24 * time.Hour
	}
	if c.MaxFailCount <= 0 {
		c.MaxFailCount = 5
	}
	if c.MaxReplacements <= 0 {
		c.MaxReplacements = 10
	}
}

// NodeEntry represents a single node in the Kademlia routing table.
type NodeEntry struct {
	// ID is the 32-byte node identifier used for XOR distance computation.
	ID [32]byte

	// Address is the network address (IP or hostname) of the node.
	Address string

	// Port is the UDP port for discovery protocol messages.
	Port int

	// LastSeen records the last time this node was observed to be alive.
	LastSeen time.Time

	// FailCount tracks consecutive communication failures.
	FailCount int
}

// KBucket holds entries and replacements at a given XOR log distance.
type KBucket struct {
	entries      []NodeEntry
	replacements []NodeEntry
	lastRefresh  time.Time
}

// KademliaTable is a Kademlia DHT routing table organized as 256 k-buckets
// indexed by XOR log distance from the local node ID.
type KademliaTable struct {
	mu      sync.RWMutex
	selfID  [32]byte
	buckets [256]*KBucket
	config  KademliaConfig
}

// NewKademliaTable creates a new Kademlia routing table for the given local
// node ID with the specified configuration.
func NewKademliaTable(selfID [32]byte, config KademliaConfig) *KademliaTable {
	config.applyDefaults()
	kt := &KademliaTable{
		selfID: selfID,
		config: config,
	}
	now := time.Now()
	for i := range kt.buckets {
		kt.buckets[i] = &KBucket{
			lastRefresh: now,
		}
	}
	return kt
}

// SelfID returns the local node's 32-byte identifier.
func (kt *KademliaTable) SelfID() [32]byte {
	return kt.selfID
}

// Config returns a copy of the table's configuration.
func (kt *KademliaTable) Config() KademliaConfig {
	return kt.config
}

// KLogDistance computes the XOR log distance between two 32-byte identifiers.
// Returns 0 if a == b; otherwise returns 1..256 where the value represents
// the position of the highest differing bit.
func KLogDistance(a, b [32]byte) int {
	for i := 0; i < 32; i++ {
		x := a[i] ^ b[i]
		if x != 0 {
			// The highest set bit in x gives the distance.
			// Leading zeros in the entire XOR so far: i*8 bytes were zero.
			lz := bits.LeadingZeros8(x)
			return 256 - (i*8 + lz)
		}
	}
	return 0
}

// BucketForDistance returns the bucket index for a given XOR log distance.
// Distance 0 (self) returns -1. Distance 1..256 maps to bucket 0..255.
func BucketForDistance(distance int) int {
	if distance <= 0 {
		return -1
	}
	if distance > 256 {
		return 255
	}
	return distance - 1
}

// bucketIndex returns the bucket index for a given node ID relative to selfID.
func (kt *KademliaTable) bucketIndex(id [32]byte) int {
	dist := KLogDistance(kt.selfID, id)
	return BucketForDistance(dist)
}

// AddNode adds a node to the appropriate bucket. If the bucket is full and
// the node is new, it is placed in the replacement cache. If the node already
// exists, its LastSeen time is updated and FailCount is reset.
// Returns true if the node was added to the main bucket entries.
func (kt *KademliaTable) AddNode(node NodeEntry) bool {
	if node.ID == kt.selfID {
		return false
	}
	idx := kt.bucketIndex(node.ID)
	if idx < 0 {
		return false
	}

	kt.mu.Lock()
	defer kt.mu.Unlock()

	b := kt.buckets[idx]

	// Check if already present in entries; if so, update it.
	for i, e := range b.entries {
		if e.ID == node.ID {
			b.entries[i].LastSeen = node.LastSeen
			b.entries[i].FailCount = 0
			if node.Address != "" {
				b.entries[i].Address = node.Address
			}
			if node.Port != 0 {
				b.entries[i].Port = node.Port
			}
			return true
		}
	}

	// Check total table size cap.
	if kt.config.MaxTableSize > 0 && kt.tableSizeLocked() >= kt.config.MaxTableSize {
		// Table is at capacity. Try to evict a stale node first.
		if !kt.evictStaleLocked() {
			// No stale node to evict; add to replacements instead.
			kt.addReplacementLocked(b, node)
			return false
		}
		// After eviction, re-check the bucket (the evicted node may not
		// have been in this bucket, so this bucket may still be full).
	}

	// If bucket has room, insert directly.
	if len(b.entries) < kt.config.BucketSize {
		b.entries = append(b.entries, node)
		return true
	}

	// Bucket is full. Check if the tail entry is stale and can be evicted.
	if kt.isNodeStaleLocked(b.entries[len(b.entries)-1]) {
		b.entries[len(b.entries)-1] = node
		return true
	}

	// Bucket full, no stale node: add to replacement cache.
	kt.addReplacementLocked(b, node)
	return false
}

// addReplacementLocked adds a node to the replacement cache. Caller must hold kt.mu.
func (kt *KademliaTable) addReplacementLocked(b *KBucket, node NodeEntry) {
	// Update existing replacement.
	for i, r := range b.replacements {
		if r.ID == node.ID {
			b.replacements[i] = node
			return
		}
	}
	if len(b.replacements) < kt.config.MaxReplacements {
		b.replacements = append(b.replacements, node)
	}
}

// isNodeStaleLocked checks if a node is stale based on fail count or last seen time.
func (kt *KademliaTable) isNodeStaleLocked(n NodeEntry) bool {
	if n.FailCount >= kt.config.MaxFailCount {
		return true
	}
	if !n.LastSeen.IsZero() && time.Since(n.LastSeen) > kt.config.StaleTimeout {
		return true
	}
	return false
}

// evictStaleLocked removes a single stale node from any bucket. Returns true
// if a node was evicted.
func (kt *KademliaTable) evictStaleLocked() bool {
	for _, b := range kt.buckets {
		for i, e := range b.entries {
			if kt.isNodeStaleLocked(e) {
				b.entries = append(b.entries[:i], b.entries[i+1:]...)
				// Promote a replacement if available.
				if len(b.replacements) > 0 {
					b.entries = append(b.entries, b.replacements[0])
					b.replacements = b.replacements[1:]
				}
				return true
			}
		}
	}
	return false
}

// RemoveNode removes a node by its ID from all buckets. If a replacement
// node is available in the same bucket, it is promoted to the main entries.
func (kt *KademliaTable) RemoveNode(id [32]byte) {
	idx := kt.bucketIndex(id)
	if idx < 0 {
		return
	}

	kt.mu.Lock()
	defer kt.mu.Unlock()

	b := kt.buckets[idx]
	for i, e := range b.entries {
		if e.ID == id {
			b.entries = append(b.entries[:i], b.entries[i+1:]...)
			// Promote replacement.
			if len(b.replacements) > 0 {
				b.entries = append(b.entries, b.replacements[0])
				b.replacements = b.replacements[1:]
			}
			return
		}
	}

	// Also remove from replacements if present.
	for i, r := range b.replacements {
		if r.ID == id {
			b.replacements = append(b.replacements[:i], b.replacements[i+1:]...)
			return
		}
	}
}

// RecordFailure increments the fail count for a node. If the fail count
// exceeds MaxFailCount, the node is removed from the table.
func (kt *KademliaTable) RecordFailure(id [32]byte) {
	idx := kt.bucketIndex(id)
	if idx < 0 {
		return
	}

	kt.mu.Lock()
	defer kt.mu.Unlock()

	b := kt.buckets[idx]
	for i, e := range b.entries {
		if e.ID == id {
			b.entries[i].FailCount++
			if b.entries[i].FailCount >= kt.config.MaxFailCount {
				b.entries = append(b.entries[:i], b.entries[i+1:]...)
				if len(b.replacements) > 0 {
					b.entries = append(b.entries, b.replacements[0])
					b.replacements = b.replacements[1:]
				}
			}
			return
		}
	}
}

// GetNode returns the NodeEntry for the given ID, or nil if not found.
func (kt *KademliaTable) GetNode(id [32]byte) *NodeEntry {
	idx := kt.bucketIndex(id)
	if idx < 0 {
		return nil
	}

	kt.mu.RLock()
	defer kt.mu.RUnlock()

	for _, e := range kt.buckets[idx].entries {
		if e.ID == id {
			cp := e
			return &cp
		}
	}
	return nil
}

// FindClosest returns up to count nodes closest to the target by XOR distance,
// sorted in ascending distance order.
func (kt *KademliaTable) FindClosest(target [32]byte, count int) []NodeEntry {
	kt.mu.RLock()
	defer kt.mu.RUnlock()

	var all []NodeEntry
	for _, b := range kt.buckets {
		all = append(all, b.entries...)
	}

	// Sort by XOR distance to target.
	sort.Slice(all, func(i, j int) bool {
		return xorLess(target, all[i].ID, all[j].ID)
	})

	if len(all) > count {
		all = all[:count]
	}
	return all
}

// xorLess returns true if a is closer to target than b by XOR distance.
func xorLess(target, a, b [32]byte) bool {
	for i := 0; i < 32; i++ {
		da := target[i] ^ a[i]
		db := target[i] ^ b[i]
		if da != db {
			return da < db
		}
	}
	return false
}

// NeedRefresh returns true if the bucket at the given index has not been
// refreshed within the configured RefreshInterval.
func (kt *KademliaTable) NeedRefresh(bucketIndex int) bool {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return false
	}

	kt.mu.RLock()
	defer kt.mu.RUnlock()

	return time.Since(kt.buckets[bucketIndex].lastRefresh) > kt.config.RefreshInterval
}

// MarkRefreshed updates the lastRefresh timestamp for the given bucket.
func (kt *KademliaTable) MarkRefreshed(bucketIndex int) {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return
	}

	kt.mu.Lock()
	defer kt.mu.Unlock()

	kt.buckets[bucketIndex].lastRefresh = time.Now()
}

// RandomNodeInBucket returns a random node from the specified bucket, or nil
// if the bucket is empty. Useful for generating refresh lookup targets.
func (kt *KademliaTable) RandomNodeInBucket(bucketIndex int) *NodeEntry {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return nil
	}

	kt.mu.RLock()
	defer kt.mu.RUnlock()

	entries := kt.buckets[bucketIndex].entries
	if len(entries) == 0 {
		return nil
	}

	// Pick a random index.
	var buf [1]byte
	rand.Read(buf[:])
	idx := int(buf[0]) % len(entries)

	cp := entries[idx]
	return &cp
}

// TableSize returns the total number of nodes across all buckets.
func (kt *KademliaTable) TableSize() int {
	kt.mu.RLock()
	defer kt.mu.RUnlock()
	return kt.tableSizeLocked()
}

// tableSizeLocked returns the total node count without acquiring the lock.
func (kt *KademliaTable) tableSizeLocked() int {
	count := 0
	for _, b := range kt.buckets {
		count += len(b.entries)
	}
	return count
}

// BucketLen returns the number of entries in a specific bucket.
func (kt *KademliaTable) BucketLen(bucketIndex int) int {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return 0
	}

	kt.mu.RLock()
	defer kt.mu.RUnlock()

	return len(kt.buckets[bucketIndex].entries)
}

// BucketReplacementLen returns the number of replacement entries in a bucket.
func (kt *KademliaTable) BucketReplacementLen(bucketIndex int) int {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return 0
	}

	kt.mu.RLock()
	defer kt.mu.RUnlock()

	return len(kt.buckets[bucketIndex].replacements)
}

// BucketEntries returns a copy of entries in the specified bucket.
func (kt *KademliaTable) BucketEntries(bucketIndex int) []NodeEntry {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return nil
	}

	kt.mu.RLock()
	defer kt.mu.RUnlock()

	entries := make([]NodeEntry, len(kt.buckets[bucketIndex].entries))
	copy(entries, kt.buckets[bucketIndex].entries)
	return entries
}

// AllNodes returns a snapshot of all nodes in the table.
func (kt *KademliaTable) AllNodes() []NodeEntry {
	kt.mu.RLock()
	defer kt.mu.RUnlock()

	var all []NodeEntry
	for _, b := range kt.buckets {
		all = append(all, b.entries...)
	}
	return all
}

// BucketsNeedingRefresh returns the indices of buckets that need to be
// refreshed (i.e., their lastRefresh is older than RefreshInterval).
func (kt *KademliaTable) BucketsNeedingRefresh() []int {
	kt.mu.RLock()
	defer kt.mu.RUnlock()

	var result []int
	for i, b := range kt.buckets {
		if time.Since(b.lastRefresh) > kt.config.RefreshInterval {
			result = append(result, i)
		}
	}
	return result
}

// RandomIDForBucket generates a random 32-byte ID that would fall into the
// given bucket index relative to this node's selfID. This is useful for
// generating lookup targets during table refresh.
func (kt *KademliaTable) RandomIDForBucket(bucketIndex int) [32]byte {
	if bucketIndex < 0 || bucketIndex >= 256 {
		return kt.selfID
	}

	var target [32]byte
	rand.Read(target[:])

	// The distance (bucketIndex+1) means the highest differing bit is at
	// position (bucketIndex). We need to ensure XOR(selfID, target) has
	// its highest set bit at exactly bit (bucketIndex).
	//
	// Bit position (bucketIndex) corresponds to:
	//   byte index: (255 - bucketIndex) / 8
	//   bit within byte: (255 - bucketIndex) % 8   (counted from MSB)
	// But KLogDistance counts from the most significant end.
	//
	// Distance d means the first differing bit is at position (256-d) from
	// the MSB. So for bucket b (distance b+1), the first differing bit is
	// at byte (255-b)/8, bit 7 - ((255-b)%8).

	distance := bucketIndex + 1
	bitPos := 256 - distance // position from MSB where first diff should be

	// Start with selfID so XOR is zero.
	copy(target[:], kt.selfID[:])

	// Zero out all bits above bitPos in XOR space (they must match selfID).
	// Set the bit at bitPos (they must differ).
	byteIdx := bitPos / 8
	bitIdx := uint(7 - (bitPos % 8))

	// Flip the target bit at bitPos.
	target[byteIdx] ^= 1 << bitIdx

	// Randomize all lower bits (below bitPos).
	var randomBuf [32]byte
	rand.Read(randomBuf[:])
	// Zero out the bit at bitIdx and above in the same byte, keep random below.
	mask := byte((1 << bitIdx) - 1)
	target[byteIdx] = (target[byteIdx] & ^mask) | (randomBuf[byteIdx] & mask)
	// Randomize all subsequent bytes.
	for i := byteIdx + 1; i < 32; i++ {
		target[i] = randomBuf[i]
	}

	return target
}
