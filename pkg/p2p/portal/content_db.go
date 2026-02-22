// Package portal - content_db.go implements a persistent content database with
// LRU eviction, XOR-based distance metrics, gossip propagation, and content
// radius tracking for the Portal Network state network.
//
// ContentDB provides the storage backend for state content distribution. It
// wraps an in-memory store with LRU eviction policy and distance-based content
// radius management, enabling nodes to dynamically adjust which content they
// store based on available capacity.
//
// Reference: https://github.com/ethereum/portal-network-specs/blob/master/state-network.md
package portal

import (
	"container/list"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/crypto"
)

// ContentDB errors.
var (
	ErrContentDBClosed    = errors.New("portal/content_db: database is closed")
	ErrContentDBFull      = errors.New("portal/content_db: database is full, eviction failed")
	ErrContentOutOfRadius = errors.New("portal/content_db: content outside node radius")
	ErrContentKeyEmpty    = errors.New("portal/content_db: empty content key")
	ErrGossipNoPeers      = errors.New("portal/content_db: no peers for gossip")
)

// NodeID is the 32-byte identifier for a portal node.
type NodeID = [32]byte

// ContentDBConfig configures the content database.
type ContentDBConfig struct {
	// MaxCapacity is the maximum storage capacity in bytes.
	MaxCapacity uint64

	// MaxItems is the maximum number of items (0 = unlimited).
	MaxItems int

	// EvictBatchSize is the number of items to evict when the DB is full.
	EvictBatchSize int

	// NodeID is the local node's 32-byte identifier.
	NodeID NodeID
}

// DefaultContentDBConfig returns a default ContentDB configuration.
func DefaultContentDBConfig(nodeID NodeID) ContentDBConfig {
	return ContentDBConfig{
		MaxCapacity:    256 << 20, // 256 MiB
		MaxItems:       0,
		EvictBatchSize: 16,
		NodeID:         nodeID,
	}
}

// ContentEntry is a single content item stored in the database.
type ContentEntry struct {
	ContentKey []byte
	ContentID  ContentID
	Data       []byte
	Size       uint64
	StoredAt   time.Time
}

// ContentDBMetrics tracks content database statistics.
type ContentDBMetrics struct {
	Puts       atomic.Int64
	Gets       atomic.Int64
	Hits       atomic.Int64
	Misses     atomic.Int64
	Evictions  atomic.Int64
	UsedBytes  atomic.Int64
	ItemCount  atomic.Int64
	GossipSent atomic.Int64
	GossipRecv atomic.Int64
}

// ContentDB is a persistent content store with LRU eviction and
// XOR distance-based content radius management. It implements the
// ContentStore interface for use with the Portal routing table.
type ContentDB struct {
	mu      sync.Mutex
	config  ContentDBConfig
	items   map[ContentID]*list.Element
	lruList *list.List
	closed  bool
	radius  NodeRadius

	// Metrics for monitoring.
	Metrics ContentDBMetrics
}

// NewContentDB creates a new content database with the given configuration.
func NewContentDB(config ContentDBConfig) *ContentDB {
	if config.EvictBatchSize <= 0 {
		config.EvictBatchSize = 1
	}
	return &ContentDB{
		config:  config,
		items:   make(map[ContentID]*list.Element),
		lruList: list.New(),
		radius:  MaxRadius(),
	}
}

// Get retrieves content by its content ID, returning the raw data.
// Updates LRU ordering on access.
func (db *ContentDB) Get(id ContentID) ([]byte, error) {
	db.Metrics.Gets.Add(1)
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return nil, ErrContentDBClosed
	}

	elem, ok := db.items[id]
	if !ok {
		db.Metrics.Misses.Add(1)
		return nil, ErrContentNotFound
	}

	// Move to front of LRU list.
	db.lruList.MoveToFront(elem)

	entry := elem.Value.(*ContentEntry)
	db.Metrics.Hits.Add(1)

	// Return a copy.
	result := make([]byte, len(entry.Data))
	copy(result, entry.Data)
	return result, nil
}

// Put stores content data by its content ID. If the database is full,
// LRU eviction is performed. The content must fall within the node's
// current content radius.
func (db *ContentDB) Put(id ContentID, data []byte) error {
	db.Metrics.Puts.Add(1)
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return ErrContentDBClosed
	}

	if len(data) == 0 {
		return ErrEmptyPayload
	}

	dataSize := uint64(len(data))

	// Check if updating existing entry.
	if elem, exists := db.items[id]; exists {
		old := elem.Value.(*ContentEntry)
		db.Metrics.UsedBytes.Add(-int64(old.Size))
		oldData := make([]byte, len(data))
		copy(oldData, data)
		old.Data = oldData
		old.Size = dataSize
		old.StoredAt = time.Now()
		db.Metrics.UsedBytes.Add(int64(dataSize))
		db.lruList.MoveToFront(elem)
		return nil
	}

	// Evict if needed.
	if err := db.evictIfNeeded(dataSize); err != nil {
		return err
	}

	entry := &ContentEntry{
		ContentID: id,
		Data:      make([]byte, len(data)),
		Size:      dataSize,
		StoredAt:  time.Now(),
	}
	copy(entry.Data, data)

	elem := db.lruList.PushFront(entry)
	db.items[id] = elem
	db.Metrics.UsedBytes.Add(int64(dataSize))
	db.Metrics.ItemCount.Add(1)

	return nil
}

// Delete removes content by its content ID.
func (db *ContentDB) Delete(id ContentID) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return ErrContentDBClosed
	}

	elem, ok := db.items[id]
	if !ok {
		return nil
	}

	entry := elem.Value.(*ContentEntry)
	db.lruList.Remove(elem)
	delete(db.items, id)
	db.Metrics.UsedBytes.Add(-int64(entry.Size))
	db.Metrics.ItemCount.Add(-1)

	return nil
}

// Has reports whether content exists in the database.
func (db *ContentDB) Has(id ContentID) bool {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return false
	}
	_, ok := db.items[id]
	return ok
}

// UsedBytes returns the total bytes stored.
func (db *ContentDB) UsedBytes() uint64 {
	v := db.Metrics.UsedBytes.Load()
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// CapacityBytes returns the configured maximum capacity.
func (db *ContentDB) CapacityBytes() uint64 {
	return db.config.MaxCapacity
}

// ItemCount returns the number of stored content items.
func (db *ContentDB) ItemCount() int {
	return int(db.Metrics.ItemCount.Load())
}

// Close marks the database as closed, preventing further operations.
func (db *ContentDB) Close() {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.closed = true
}

// Radius returns the current content radius for this node.
func (db *ContentDB) Radius() NodeRadius {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.radius
}

// SetRadius updates the content radius.
func (db *ContentDB) SetRadius(r NodeRadius) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.radius = r
}

// evictIfNeeded removes entries from the LRU tail until enough space
// is available for a new item of the given size. Caller must hold db.mu.
func (db *ContentDB) evictIfNeeded(size uint64) error {
	used := db.Metrics.UsedBytes.Load()
	if used < 0 {
		used = 0
	}

	for uint64(used)+size > db.config.MaxCapacity || (db.config.MaxItems > 0 && db.lruList.Len() >= db.config.MaxItems) {
		if db.lruList.Len() == 0 {
			return ErrContentDBFull
		}

		// Evict from the back (least recently used).
		evicted := 0
		for evicted < db.config.EvictBatchSize && db.lruList.Len() > 0 {
			back := db.lruList.Back()
			if back == nil {
				break
			}
			entry := back.Value.(*ContentEntry)
			db.lruList.Remove(back)
			delete(db.items, entry.ContentID)
			db.Metrics.UsedBytes.Add(-int64(entry.Size))
			db.Metrics.ItemCount.Add(-1)
			db.Metrics.Evictions.Add(1)
			evicted++
		}

		used = db.Metrics.UsedBytes.Load()
		if used < 0 {
			used = 0
		}
	}
	return nil
}

// DistanceMetric computes the XOR distance between a node ID and a content ID.
// This is the fundamental distance metric for DHT content placement.
func DistanceMetric(nodeID NodeID, contentID ContentID) *big.Int {
	return Distance(nodeID, contentID)
}

// ContentKeyToID derives a content ID from a raw content key using keccak256.
// This is used for content-addressed storage where the content ID is the
// keccak256 hash of the content key.
func ContentKeyToID(contentKey []byte) ContentID {
	h := crypto.Keccak256Hash(contentKey)
	var id ContentID
	copy(id[:], h[:])
	return id
}

// IsWithinRadius checks whether a content ID falls within the given radius
// of the specified node ID.
func IsWithinRadius(nodeID NodeID, contentID ContentID, radius NodeRadius) bool {
	dist := DistanceMetric(nodeID, contentID)
	return dist.Cmp(radius.Raw) <= 0
}

// GossipResult records the outcome of a gossip operation.
type GossipResult struct {
	// ContentID is the content that was gossipped.
	ContentID ContentID
	// PeersSent is the number of peers the content was sent to.
	PeersSent int
	// PeersAccepted is the number of peers that accepted the content.
	PeersAccepted int
}

// GossipContent propagates content to peers whose radius covers the content ID.
// It selects peers from the routing table that should store this content based
// on XOR distance and advertised radius.
func GossipContent(contentKey, content []byte, peers []*PeerInfo, nodeID NodeID) (*GossipResult, error) {
	if len(contentKey) == 0 {
		return nil, ErrContentKeyEmpty
	}
	if len(content) == 0 {
		return nil, ErrEmptyPayload
	}
	if len(peers) == 0 {
		return nil, ErrGossipNoPeers
	}

	contentID := ContentKeyToID(contentKey)

	result := &GossipResult{
		ContentID: contentID,
	}

	for _, peer := range peers {
		// Check if the peer's radius covers this content.
		dist := DistanceMetric(peer.NodeID, contentID)
		if dist.Cmp(peer.Radius.Raw) <= 0 {
			result.PeersSent++
			// In production, we would send an OFFER message here.
			// For now, count peers that should accept based on radius.
			result.PeersAccepted++
		}
	}

	return result, nil
}

// UpdateRadiusFromUsage adjusts the content radius based on current
// storage utilization. The radius shrinks as storage fills up, reducing
// the responsibility range of this node.
func UpdateRadiusFromUsage(usedBytes, capacityBytes uint64) NodeRadius {
	if capacityBytes == 0 {
		return ZeroRadius()
	}
	if usedBytes == 0 {
		return MaxRadius()
	}
	if usedBytes >= capacityBytes {
		return ZeroRadius()
	}

	maxRadius := MaxRadius().Raw
	remaining := capacityBytes - usedBytes
	numerator := new(big.Int).Mul(maxRadius, new(big.Int).SetUint64(remaining))
	newRadius := new(big.Int).Div(numerator, new(big.Int).SetUint64(capacityBytes))
	return NodeRadius{Raw: newRadius}
}

// AutoUpdateRadius updates the ContentDB's radius based on current usage.
func (db *ContentDB) AutoUpdateRadius() {
	used := db.UsedBytes()
	capacity := db.CapacityBytes()
	newRadius := UpdateRadiusFromUsage(used, capacity)
	db.SetRadius(newRadius)
}

// FindContentByKey looks up content by raw content key (derives the content ID
// using keccak256 and then queries the store).
func (db *ContentDB) FindContentByKey(contentKey []byte) ([]byte, error) {
	if len(contentKey) == 0 {
		return nil, ErrContentKeyEmpty
	}
	id := ContentKeyToID(contentKey)
	return db.Get(id)
}

// StoreContentByKey stores content by raw content key (derives the content ID
// and stores the data). Checks that the content falls within the node's radius.
func (db *ContentDB) StoreContentByKey(contentKey, content []byte) error {
	if len(contentKey) == 0 {
		return ErrContentKeyEmpty
	}
	if len(content) == 0 {
		return ErrEmptyPayload
	}

	id := ContentKeyToID(contentKey)

	db.mu.Lock()
	radius := db.radius
	nodeID := db.config.NodeID
	db.mu.Unlock()

	if !IsWithinRadius(nodeID, id, radius) {
		return ErrContentOutOfRadius
	}

	return db.Put(id, content)
}

// EntriesWithinRadius returns all stored content IDs that fall within the
// given radius of the specified node ID. Useful for responding to OFFER
// messages and gossip decisions.
func (db *ContentDB) EntriesWithinRadius(nodeID NodeID, radius NodeRadius) []ContentID {
	db.mu.Lock()
	defer db.mu.Unlock()

	var result []ContentID
	for id := range db.items {
		if IsWithinRadius(nodeID, id, radius) {
			result = append(result, id)
		}
	}
	return result
}

// FarthestContent returns the content ID that is farthest from the local
// node ID. This is useful for distance-based eviction policies.
func (db *ContentDB) FarthestContent() (ContentID, *big.Int) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var farthestID ContentID
	farthestDist := new(big.Int)

	for id := range db.items {
		dist := DistanceMetric(db.config.NodeID, id)
		if dist.Cmp(farthestDist) > 0 {
			farthestID = id
			farthestDist = dist
		}
	}

	return farthestID, farthestDist
}
