package portal

import (
	"math/big"
	"sync"
	"testing"
)

// --- ContentDB creation ---

func TestNewContentDB(t *testing.T) {
	var nodeID NodeID
	nodeID[0] = 0x01
	cfg := DefaultContentDBConfig(nodeID)
	db := NewContentDB(cfg)
	if db == nil {
		t.Fatal("NewContentDB returned nil")
	}
	if db.ItemCount() != 0 {
		t.Fatalf("ItemCount = %d, want 0", db.ItemCount())
	}
	if db.UsedBytes() != 0 {
		t.Fatalf("UsedBytes = %d, want 0", db.UsedBytes())
	}
	if db.CapacityBytes() != cfg.MaxCapacity {
		t.Fatalf("CapacityBytes = %d, want %d", db.CapacityBytes(), cfg.MaxCapacity)
	}
}

func TestDefaultContentDBConfig(t *testing.T) {
	var nodeID NodeID
	cfg := DefaultContentDBConfig(nodeID)
	if cfg.MaxCapacity == 0 {
		t.Fatal("MaxCapacity should not be zero")
	}
	if cfg.EvictBatchSize <= 0 {
		t.Fatal("EvictBatchSize should be positive")
	}
}

// --- Put and Get ---

func TestContentDBPutAndGet(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0xAA
	data := []byte("test content data")

	if err := db.Put(id, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if db.ItemCount() != 1 {
		t.Fatalf("ItemCount = %d, want 1", db.ItemCount())
	}
	if db.UsedBytes() != uint64(len(data)) {
		t.Fatalf("UsedBytes = %d, want %d", db.UsedBytes(), len(data))
	}

	got, err := db.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestContentDBGetNotFound(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0xFF
	_, err := db.Get(id)
	if err != ErrContentNotFound {
		t.Fatalf("expected ErrContentNotFound, got %v", err)
	}
}

func TestContentDBPutEmptyData(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	if err := db.Put(id, nil); err != ErrEmptyPayload {
		t.Fatalf("expected ErrEmptyPayload, got %v", err)
	}
	if err := db.Put(id, []byte{}); err != ErrEmptyPayload {
		t.Fatalf("expected ErrEmptyPayload for empty slice, got %v", err)
	}
}

func TestContentDBPutUpdate(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0x01

	if err := db.Put(id, []byte("first")); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if db.UsedBytes() != 5 {
		t.Fatalf("UsedBytes = %d, want 5", db.UsedBytes())
	}

	if err := db.Put(id, []byte("second-longer")); err != nil {
		t.Fatalf("Put second: %v", err)
	}
	// Item count stays 1 (update, not insert).
	if db.ItemCount() != 1 {
		t.Fatalf("ItemCount = %d, want 1", db.ItemCount())
	}
	if db.UsedBytes() != 13 {
		t.Fatalf("UsedBytes = %d, want 13", db.UsedBytes())
	}

	got, err := db.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "second-longer" {
		t.Fatalf("got %q, want %q", got, "second-longer")
	}
}

// --- Delete ---

func TestContentDBDelete(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0x42
	db.Put(id, []byte("to delete"))

	if err := db.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if db.ItemCount() != 0 {
		t.Fatalf("ItemCount = %d, want 0", db.ItemCount())
	}
	if db.UsedBytes() != 0 {
		t.Fatalf("UsedBytes = %d, want 0", db.UsedBytes())
	}
	if db.Has(id) {
		t.Fatal("Has should be false after Delete")
	}
}

func TestContentDBDeleteNonExistent(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0xFF
	if err := db.Delete(id); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

// --- Has ---

func TestContentDBHas(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0x01
	if db.Has(id) {
		t.Fatal("Has should be false before Put")
	}
	db.Put(id, []byte("data"))
	if !db.Has(id) {
		t.Fatal("Has should be true after Put")
	}
}

// --- Close ---

func TestContentDBClosed(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))
	db.Close()

	var id ContentID
	if err := db.Put(id, []byte("data")); err != ErrContentDBClosed {
		t.Fatalf("Put on closed: got %v, want ErrContentDBClosed", err)
	}
	if _, err := db.Get(id); err != ErrContentDBClosed {
		t.Fatalf("Get on closed: got %v, want ErrContentDBClosed", err)
	}
	if err := db.Delete(id); err != ErrContentDBClosed {
		t.Fatalf("Delete on closed: got %v, want ErrContentDBClosed", err)
	}
	if db.Has(id) {
		t.Fatal("Has should return false on closed DB")
	}
}

// --- LRU eviction ---

func TestContentDBLRUEviction(t *testing.T) {
	var nodeID NodeID
	cfg := ContentDBConfig{
		MaxCapacity:    100,
		EvictBatchSize: 1,
		NodeID:         nodeID,
	}
	db := NewContentDB(cfg)

	// Insert items totaling 100 bytes.
	for i := 0; i < 10; i++ {
		var id ContentID
		id[0] = byte(i)
		if err := db.Put(id, make([]byte, 10)); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	if db.UsedBytes() != 100 {
		t.Fatalf("UsedBytes = %d, want 100", db.UsedBytes())
	}

	// Insert one more item. Should evict the least recently used (id[0]=0).
	var newID ContentID
	newID[0] = 0xFF
	if err := db.Put(newID, make([]byte, 10)); err != nil {
		t.Fatalf("Put overflow: %v", err)
	}

	// The first item should have been evicted.
	var firstID ContentID
	firstID[0] = 0
	if db.Has(firstID) {
		t.Fatal("first item should have been evicted")
	}
	if !db.Has(newID) {
		t.Fatal("new item should be present")
	}
}

func TestContentDBLRUAccessReorder(t *testing.T) {
	var nodeID NodeID
	cfg := ContentDBConfig{
		MaxCapacity:    30,
		EvictBatchSize: 1,
		NodeID:         nodeID,
	}
	db := NewContentDB(cfg)

	// Insert 3 items of 10 bytes each (fills to capacity).
	var id0, id1, id2 ContentID
	id0[0] = 0
	id1[0] = 1
	id2[0] = 2

	db.Put(id0, make([]byte, 10))
	db.Put(id1, make([]byte, 10))
	db.Put(id2, make([]byte, 10))

	// Access id0, making it most recently used.
	db.Get(id0)

	// Insert a new item. Should evict id1 (LRU after id0's access).
	var id3 ContentID
	id3[0] = 3
	db.Put(id3, make([]byte, 10))

	if db.Has(id1) {
		t.Fatal("id1 should have been evicted (LRU)")
	}
	if !db.Has(id0) {
		t.Fatal("id0 should still be present (recently accessed)")
	}
	if !db.Has(id3) {
		t.Fatal("id3 should be present")
	}
}

func TestContentDBMaxItems(t *testing.T) {
	var nodeID NodeID
	cfg := ContentDBConfig{
		MaxCapacity:    1 << 20, // large
		MaxItems:       3,
		EvictBatchSize: 1,
		NodeID:         nodeID,
	}
	db := NewContentDB(cfg)

	for i := 0; i < 3; i++ {
		var id ContentID
		id[0] = byte(i)
		db.Put(id, []byte{byte(i)})
	}

	if db.ItemCount() != 3 {
		t.Fatalf("ItemCount = %d, want 3", db.ItemCount())
	}

	// Insert 4th item: should evict one.
	var id3 ContentID
	id3[0] = 3
	db.Put(id3, []byte{3})

	if db.ItemCount() != 3 {
		t.Fatalf("ItemCount = %d, want 3 after eviction", db.ItemCount())
	}
}

// --- Metrics ---

func TestContentDBMetrics(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var id ContentID
	id[0] = 0x01
	data := []byte("metrics test")

	db.Put(id, data)
	if db.Metrics.Puts.Load() != 1 {
		t.Fatalf("Puts = %d, want 1", db.Metrics.Puts.Load())
	}

	db.Get(id)
	if db.Metrics.Gets.Load() != 1 {
		t.Fatalf("Gets = %d, want 1", db.Metrics.Gets.Load())
	}
	if db.Metrics.Hits.Load() != 1 {
		t.Fatalf("Hits = %d, want 1", db.Metrics.Hits.Load())
	}

	var missing ContentID
	missing[0] = 0xFF
	db.Get(missing)
	if db.Metrics.Misses.Load() != 1 {
		t.Fatalf("Misses = %d, want 1", db.Metrics.Misses.Load())
	}
}

// --- Radius ---

func TestContentDBRadius(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	r := db.Radius()
	if r.Raw.Cmp(MaxRadius().Raw) != 0 {
		t.Fatal("initial radius should be MaxRadius")
	}

	halfRadius := NodeRadius{Raw: new(big.Int).Div(MaxRadius().Raw, big.NewInt(2))}
	db.SetRadius(halfRadius)

	r = db.Radius()
	if r.Raw.Cmp(halfRadius.Raw) != 0 {
		t.Fatalf("radius = %v, want %v", r.Raw, halfRadius.Raw)
	}
}

func TestContentDBAutoUpdateRadius(t *testing.T) {
	var nodeID NodeID
	cfg := ContentDBConfig{
		MaxCapacity:    100,
		EvictBatchSize: 1,
		NodeID:         nodeID,
	}
	db := NewContentDB(cfg)

	// Empty: should be max radius.
	db.AutoUpdateRadius()
	if db.Radius().Raw.Cmp(MaxRadius().Raw) != 0 {
		t.Fatal("empty DB should have max radius")
	}

	// Fill halfway.
	var id ContentID
	id[0] = 0x01
	db.Put(id, make([]byte, 50))
	db.AutoUpdateRadius()

	expectedHalf := new(big.Int).Div(MaxRadius().Raw, big.NewInt(2))
	if db.Radius().Raw.Cmp(expectedHalf) != 0 {
		t.Fatalf("half-full radius = %v, want %v", db.Radius().Raw, expectedHalf)
	}
}

// --- DistanceMetric ---

func TestDistanceMetricFunc(t *testing.T) {
	var a NodeID
	a[0] = 0x01
	var b ContentID
	b[0] = 0xFF

	dist := DistanceMetric(a, b)
	if dist.Sign() == 0 {
		t.Fatal("distance should not be zero")
	}

	// Distance to self is zero.
	selfDist := DistanceMetric(a, ContentID(a))
	if selfDist.Sign() != 0 {
		t.Fatalf("self-distance = %v, want 0", selfDist)
	}
}

// --- ContentKeyToID ---

func TestContentKeyToID(t *testing.T) {
	key1 := []byte("key1")
	key2 := []byte("key2")

	id1 := ContentKeyToID(key1)
	id2 := ContentKeyToID(key2)

	if id1 == id2 {
		t.Fatal("different keys should produce different IDs")
	}

	// Deterministic.
	id1Again := ContentKeyToID(key1)
	if id1 != id1Again {
		t.Fatal("ContentKeyToID should be deterministic")
	}

	// Non-zero.
	if id1.IsZero() {
		t.Fatal("content ID should not be zero for non-empty key")
	}
}

// --- IsWithinRadius ---

func TestIsWithinRadius(t *testing.T) {
	var nodeID NodeID
	var contentID ContentID
	contentID[0] = 0xFF

	if !IsWithinRadius(nodeID, contentID, MaxRadius()) {
		t.Fatal("MaxRadius should contain all content")
	}
	if IsWithinRadius(nodeID, contentID, ZeroRadius()) {
		t.Fatal("ZeroRadius should not contain distant content")
	}
	if !IsWithinRadius(nodeID, ContentID(nodeID), ZeroRadius()) {
		t.Fatal("same ID should be within zero radius")
	}
}

// --- GossipContent ---

func TestGossipContentNoPeers(t *testing.T) {
	var nodeID NodeID
	_, err := GossipContent([]byte("key"), []byte("data"), nil, nodeID)
	if err != ErrGossipNoPeers {
		t.Fatalf("expected ErrGossipNoPeers, got %v", err)
	}
}

func TestGossipContentEmptyKey(t *testing.T) {
	var nodeID NodeID
	peers := []*PeerInfo{{Radius: MaxRadius()}}
	_, err := GossipContent(nil, []byte("data"), peers, nodeID)
	if err != ErrContentKeyEmpty {
		t.Fatalf("expected ErrContentKeyEmpty, got %v", err)
	}
}

func TestGossipContentEmptyData(t *testing.T) {
	var nodeID NodeID
	peers := []*PeerInfo{{Radius: MaxRadius()}}
	_, err := GossipContent([]byte("key"), nil, peers, nodeID)
	if err != ErrEmptyPayload {
		t.Fatalf("expected ErrEmptyPayload, got %v", err)
	}
}

func TestGossipContentWithPeers(t *testing.T) {
	var nodeID NodeID

	var peerID NodeID
	peerID[0] = 0x01
	peers := []*PeerInfo{
		{NodeID: peerID, Radius: MaxRadius()},
	}

	result, err := GossipContent([]byte("key"), []byte("data"), peers, nodeID)
	if err != nil {
		t.Fatalf("GossipContent: %v", err)
	}
	if result.PeersSent != 1 {
		t.Fatalf("PeersSent = %d, want 1", result.PeersSent)
	}
	if result.PeersAccepted != 1 {
		t.Fatalf("PeersAccepted = %d, want 1", result.PeersAccepted)
	}
}

func TestGossipContentPeerOutOfRadius(t *testing.T) {
	var nodeID NodeID

	var peerID NodeID
	peerID[0] = 0x01
	// Zero radius: the peer only stores content with distance 0 from itself.
	peers := []*PeerInfo{
		{NodeID: peerID, Radius: ZeroRadius()},
	}

	result, err := GossipContent([]byte("some-key"), []byte("data"), peers, nodeID)
	if err != nil {
		t.Fatalf("GossipContent: %v", err)
	}
	if result.PeersSent != 0 {
		t.Fatalf("PeersSent = %d, want 0 (peer out of radius)", result.PeersSent)
	}
}

// --- UpdateRadiusFromUsage ---

func TestUpdateRadiusFromUsage(t *testing.T) {
	r := UpdateRadiusFromUsage(0, 1000)
	if r.Raw.Cmp(MaxRadius().Raw) != 0 {
		t.Fatal("empty usage should give max radius")
	}

	r = UpdateRadiusFromUsage(500, 1000)
	expected := new(big.Int).Div(MaxRadius().Raw, big.NewInt(2))
	if r.Raw.Cmp(expected) != 0 {
		t.Fatalf("half usage: got %v, want %v", r.Raw, expected)
	}

	r = UpdateRadiusFromUsage(1000, 1000)
	if r.Raw.Sign() != 0 {
		t.Fatal("full usage should give zero radius")
	}

	r = UpdateRadiusFromUsage(0, 0)
	if r.Raw.Sign() != 0 {
		t.Fatal("zero capacity should give zero radius")
	}
}

// --- FindContentByKey / StoreContentByKey ---

func TestContentDBFindContentByKey(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	key := []byte("test-content-key")
	data := []byte("test-content-data")

	id := ContentKeyToID(key)
	db.Put(id, data)

	got, err := db.FindContentByKey(key)
	if err != nil {
		t.Fatalf("FindContentByKey: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestContentDBFindContentByKeyEmpty(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	_, err := db.FindContentByKey(nil)
	if err != ErrContentKeyEmpty {
		t.Fatalf("expected ErrContentKeyEmpty, got %v", err)
	}
}

func TestContentDBStoreContentByKey(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	key := []byte("store-key")
	data := []byte("store-data")

	if err := db.StoreContentByKey(key, data); err != nil {
		t.Fatalf("StoreContentByKey: %v", err)
	}

	got, err := db.FindContentByKey(key)
	if err != nil {
		t.Fatalf("FindContentByKey: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}

func TestContentDBStoreContentByKeyOutOfRadius(t *testing.T) {
	var nodeID NodeID
	cfg := DefaultContentDBConfig(nodeID)
	db := NewContentDB(cfg)

	// Set a zero radius so nothing is within range.
	db.SetRadius(ZeroRadius())

	key := []byte("far-away-key")
	data := []byte("data")

	err := db.StoreContentByKey(key, data)
	if err != ErrContentOutOfRadius {
		t.Fatalf("expected ErrContentOutOfRadius, got %v", err)
	}
}

// --- EntriesWithinRadius ---

func TestContentDBEntriesWithinRadius(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	// Add a few items.
	for i := 0; i < 5; i++ {
		var id ContentID
		id[0] = byte(i + 1)
		db.Put(id, []byte{byte(i)})
	}

	// Max radius: all entries.
	entries := db.EntriesWithinRadius(nodeID, MaxRadius())
	if len(entries) != 5 {
		t.Fatalf("entries within max radius = %d, want 5", len(entries))
	}

	// Zero radius: only entries at distance 0.
	entries = db.EntriesWithinRadius(nodeID, ZeroRadius())
	if len(entries) != 0 {
		t.Fatalf("entries within zero radius = %d, want 0", len(entries))
	}
}

// --- FarthestContent ---

func TestContentDBFarthestContent(t *testing.T) {
	var nodeID NodeID // all zeros
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var nearID ContentID
	nearID[31] = 0x01 // distance = 1

	var farID ContentID
	farID[0] = 0xFF // distance very large

	db.Put(nearID, []byte("near"))
	db.Put(farID, []byte("far"))

	farthest, dist := db.FarthestContent()
	if farthest != farID {
		t.Fatal("farthest content should be farID")
	}
	if dist.Sign() == 0 {
		t.Fatal("farthest distance should not be zero")
	}
}

func TestContentDBFarthestContentEmpty(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	_, dist := db.FarthestContent()
	if dist.Sign() != 0 {
		t.Fatal("farthest distance of empty DB should be zero")
	}
}

// --- Concurrency ---

func TestContentDBConcurrentAccess(t *testing.T) {
	var nodeID NodeID
	db := NewContentDB(DefaultContentDBConfig(nodeID))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var id ContentID
			id[0] = byte(idx)
			data := []byte{byte(idx)}

			db.Put(id, data)
			db.Get(id)
			db.Has(id)
		}(i)
	}
	wg.Wait()

	if db.ItemCount() != 50 {
		t.Fatalf("ItemCount = %d, want 50", db.ItemCount())
	}
}
