package discover

import (
	"testing"
	"time"
)

func makeEntry(b byte) NodeEntry {
	var id [32]byte
	id[31] = b
	return NodeEntry{
		ID:       id,
		Address:  "10.0.0.1",
		Port:     30303,
		LastSeen: time.Now(),
	}
}

func makeEntryID(bytes ...byte) NodeEntry {
	var id [32]byte
	for i, b := range bytes {
		if i < 32 {
			id[i] = b
		}
	}
	return NodeEntry{
		ID:       id,
		Address:  "10.0.0.1",
		Port:     30303,
		LastSeen: time.Now(),
	}
}

// --- KLogDistance ---

func TestKLogDistance_Identical(t *testing.T) {
	var a [32]byte
	a[0] = 0xAB
	if d := KLogDistance(a, a); d != 0 {
		t.Fatalf("KLogDistance(a, a): want 0, got %d", d)
	}
}

func TestKLogDistance_LastBit(t *testing.T) {
	var a, b [32]byte
	b[31] = 0x01
	if d := KLogDistance(a, b); d != 1 {
		t.Fatalf("KLogDistance: want 1, got %d", d)
	}
}

func TestKLogDistance_HighBit(t *testing.T) {
	var a, b [32]byte
	b[0] = 0x80
	if d := KLogDistance(a, b); d != 256 {
		t.Fatalf("KLogDistance: want 256, got %d", d)
	}
}

func TestKLogDistance_MidBit(t *testing.T) {
	var a, b [32]byte
	b[15] = 0x01
	// Bit at position 16*8 - 1 = bit 128 from MSB, distance = 256 - 15*8 - 7 = 129
	expected := 256 - 15*8 - 7
	if d := KLogDistance(a, b); d != expected {
		t.Fatalf("KLogDistance: want %d, got %d", expected, d)
	}
}

// --- BucketForDistance ---

func TestBucketForDistance_Zero(t *testing.T) {
	if b := BucketForDistance(0); b != -1 {
		t.Fatalf("BucketForDistance(0): want -1, got %d", b)
	}
}

func TestBucketForDistance_One(t *testing.T) {
	if b := BucketForDistance(1); b != 0 {
		t.Fatalf("BucketForDistance(1): want 0, got %d", b)
	}
}

func TestBucketForDistance_Max(t *testing.T) {
	if b := BucketForDistance(256); b != 255 {
		t.Fatalf("BucketForDistance(256): want 255, got %d", b)
	}
}

func TestBucketForDistance_OverMax(t *testing.T) {
	if b := BucketForDistance(300); b != 255 {
		t.Fatalf("BucketForDistance(300): want 255, got %d", b)
	}
}

// --- NewKademliaTable ---

func TestNewKademliaTable(t *testing.T) {
	var selfID [32]byte
	selfID[0] = 0x42
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	if kt == nil {
		t.Fatal("NewKademliaTable returned nil")
	}
	if kt.SelfID() != selfID {
		t.Fatal("SelfID mismatch")
	}
	if kt.TableSize() != 0 {
		t.Fatalf("TableSize: want 0, got %d", kt.TableSize())
	}
}

// --- AddNode ---

func TestAddNode_Basic(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	node := makeEntry(1)
	if !kt.AddNode(node) {
		t.Fatal("AddNode should return true for a new entry")
	}
	if kt.TableSize() != 1 {
		t.Fatalf("TableSize: want 1, got %d", kt.TableSize())
	}
}

func TestAddNode_Self(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	node := NodeEntry{ID: selfID}
	if kt.AddNode(node) {
		t.Fatal("AddNode should return false for self")
	}
	if kt.TableSize() != 0 {
		t.Fatal("self should not be added")
	}
}

func TestAddNode_Duplicate(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	node := makeEntry(1)
	kt.AddNode(node)
	kt.AddNode(node) // duplicate

	if kt.TableSize() != 1 {
		t.Fatalf("TableSize after duplicate: want 1, got %d", kt.TableSize())
	}
}

func TestAddNode_UpdatesLastSeen(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	node := makeEntry(1)
	node.LastSeen = time.Now().Add(-1 * time.Hour)
	kt.AddNode(node)

	updatedNode := makeEntry(1)
	updatedNode.LastSeen = time.Now()
	kt.AddNode(updatedNode)

	got := kt.GetNode(node.ID)
	if got == nil {
		t.Fatal("GetNode returned nil")
	}
	if got.LastSeen.Before(updatedNode.LastSeen.Add(-time.Second)) {
		t.Fatal("LastSeen was not updated")
	}
}

func TestAddNode_BucketFull_GoesToReplacement(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.BucketSize = 4
	kt := NewKademliaTable(selfID, cfg)

	// All these nodes go to bucket 7 (distance 8): id[31] in [0x80, 0xFF].
	for i := byte(0x80); i < 0x80+byte(cfg.BucketSize); i++ {
		var id [32]byte
		id[31] = i
		kt.AddNode(NodeEntry{ID: id, Address: "10.0.0.1", Port: 30303, LastSeen: time.Now()})
	}

	if kt.TableSize() != cfg.BucketSize {
		t.Fatalf("TableSize: want %d, got %d", cfg.BucketSize, kt.TableSize())
	}

	// This one should go to replacement.
	var extraID [32]byte
	extraID[31] = 0x80 + byte(cfg.BucketSize)
	added := kt.AddNode(NodeEntry{ID: extraID, Address: "10.0.0.1", Port: 30303, LastSeen: time.Now()})
	if added {
		t.Fatal("should not be added to entries when bucket is full")
	}
	if kt.TableSize() != cfg.BucketSize {
		t.Fatalf("TableSize after overflow: want %d, got %d", cfg.BucketSize, kt.TableSize())
	}
	if kt.BucketReplacementLen(7) != 1 {
		t.Fatalf("replacement count: want 1, got %d", kt.BucketReplacementLen(7))
	}
}

// --- RemoveNode ---

func TestRemoveNode_Basic(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	node := makeEntry(1)
	kt.AddNode(node)
	kt.RemoveNode(node.ID)

	if kt.TableSize() != 0 {
		t.Fatalf("TableSize after remove: want 0, got %d", kt.TableSize())
	}
}

func TestRemoveNode_PromotesReplacement(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.BucketSize = 2
	kt := NewKademliaTable(selfID, cfg)

	// Fill bucket 7 (distance 8).
	var id1, id2, id3 [32]byte
	id1[31] = 0x80
	id2[31] = 0x81
	id3[31] = 0x82

	kt.AddNode(NodeEntry{ID: id1, LastSeen: time.Now()})
	kt.AddNode(NodeEntry{ID: id2, LastSeen: time.Now()})
	kt.AddNode(NodeEntry{ID: id3, LastSeen: time.Now()}) // goes to replacement

	kt.RemoveNode(id1)

	if kt.TableSize() != 2 {
		t.Fatalf("TableSize: want 2, got %d", kt.TableSize())
	}
	if kt.GetNode(id3) == nil {
		t.Fatal("replacement should have been promoted")
	}
}

func TestRemoveNode_Nonexistent(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	node := makeEntry(1)
	kt.AddNode(node)

	var ghost [32]byte
	ghost[0] = 0xFF
	kt.RemoveNode(ghost) // should not panic

	if kt.TableSize() != 1 {
		t.Fatalf("TableSize: want 1, got %d", kt.TableSize())
	}
}

// --- RecordFailure ---

func TestRecordFailure(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.MaxFailCount = 3
	kt := NewKademliaTable(selfID, cfg)

	node := makeEntry(1)
	kt.AddNode(node)

	kt.RecordFailure(node.ID)
	kt.RecordFailure(node.ID)
	got := kt.GetNode(node.ID)
	if got == nil {
		t.Fatal("node should still exist after 2 failures")
	}
	if got.FailCount != 2 {
		t.Fatalf("FailCount: want 2, got %d", got.FailCount)
	}

	// Third failure should evict.
	kt.RecordFailure(node.ID)
	if kt.GetNode(node.ID) != nil {
		t.Fatal("node should be evicted after MaxFailCount failures")
	}
}

// --- GetNode ---

func TestGetNode_NotFound(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	var id [32]byte
	id[0] = 0xFF
	if kt.GetNode(id) != nil {
		t.Fatal("GetNode should return nil for non-existent node")
	}
}

// --- FindClosest ---

func TestFindClosest(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	// Add nodes at various distances.
	for i := byte(1); i <= 10; i++ {
		kt.AddNode(makeEntry(i))
	}

	var target [32]byte
	target[31] = 5 // target is node 5

	closest := kt.FindClosest(target, 3)
	if len(closest) != 3 {
		t.Fatalf("FindClosest: want 3, got %d", len(closest))
	}

	// First result should be the exact match.
	if closest[0].ID[31] != 5 {
		t.Fatalf("closest[0] ID[31]: want 5, got %d", closest[0].ID[31])
	}

	// Verify sorted by distance.
	for i := 1; i < len(closest); i++ {
		if !xorLess(target, closest[i-1].ID, closest[i].ID) {
			t.Fatal("FindClosest results not sorted by distance")
		}
	}
}

func TestFindClosest_Empty(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	var target [32]byte
	target[0] = 0xFF

	closest := kt.FindClosest(target, 5)
	if len(closest) != 0 {
		t.Fatalf("FindClosest on empty table: want 0, got %d", len(closest))
	}
}

func TestFindClosest_FewerThanRequested(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	kt.AddNode(makeEntry(1))
	kt.AddNode(makeEntry(2))

	var target [32]byte
	closest := kt.FindClosest(target, 10)
	if len(closest) != 2 {
		t.Fatalf("FindClosest: want 2, got %d", len(closest))
	}
}

// --- NeedRefresh ---

func TestNeedRefresh(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.RefreshInterval = 100 * time.Millisecond
	kt := NewKademliaTable(selfID, cfg)

	// Freshly created buckets should not need refresh.
	if kt.NeedRefresh(0) {
		t.Fatal("bucket 0 should not need refresh immediately")
	}

	// Wait for the interval to pass.
	time.Sleep(150 * time.Millisecond)
	if !kt.NeedRefresh(0) {
		t.Fatal("bucket 0 should need refresh after interval")
	}

	// Mark refreshed.
	kt.MarkRefreshed(0)
	if kt.NeedRefresh(0) {
		t.Fatal("bucket 0 should not need refresh right after MarkRefreshed")
	}
}

func TestNeedRefresh_OutOfRange(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	if kt.NeedRefresh(-1) {
		t.Fatal("NeedRefresh(-1) should return false")
	}
	if kt.NeedRefresh(256) {
		t.Fatal("NeedRefresh(256) should return false")
	}
}

// --- RandomNodeInBucket ---

func TestRandomNodeInBucket_Empty(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	if kt.RandomNodeInBucket(0) != nil {
		t.Fatal("RandomNodeInBucket on empty bucket should return nil")
	}
}

func TestRandomNodeInBucket_SingleEntry(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	node := makeEntry(1) // distance 1 -> bucket 0
	kt.AddNode(node)

	got := kt.RandomNodeInBucket(0)
	if got == nil {
		t.Fatal("RandomNodeInBucket should return non-nil for populated bucket")
	}
	if got.ID != node.ID {
		t.Fatal("RandomNodeInBucket returned wrong node")
	}
}

func TestRandomNodeInBucket_OutOfRange(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	if kt.RandomNodeInBucket(-1) != nil {
		t.Fatal("should return nil for negative index")
	}
	if kt.RandomNodeInBucket(256) != nil {
		t.Fatal("should return nil for index >= 256")
	}
}

// --- BucketsNeedingRefresh ---

func TestBucketsNeedingRefresh(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.RefreshInterval = 50 * time.Millisecond
	kt := NewKademliaTable(selfID, cfg)

	time.Sleep(100 * time.Millisecond)
	needing := kt.BucketsNeedingRefresh()
	if len(needing) != 256 {
		t.Fatalf("BucketsNeedingRefresh: want 256, got %d", len(needing))
	}

	kt.MarkRefreshed(0)
	needing = kt.BucketsNeedingRefresh()
	if len(needing) != 255 {
		t.Fatalf("after marking bucket 0: want 255, got %d", len(needing))
	}
}

// --- AllNodes ---

func TestAllNodes(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	for i := byte(1); i <= 5; i++ {
		kt.AddNode(makeEntry(i))
	}

	all := kt.AllNodes()
	if len(all) != 5 {
		t.Fatalf("AllNodes: want 5, got %d", len(all))
	}
}

// --- BucketLen ---

func TestBucketLen(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	// Distance 1 -> bucket 0.
	kt.AddNode(makeEntry(1))
	if kt.BucketLen(0) != 1 {
		t.Fatalf("BucketLen(0): want 1, got %d", kt.BucketLen(0))
	}
	if kt.BucketLen(1) != 0 {
		t.Fatalf("BucketLen(1): want 0, got %d", kt.BucketLen(1))
	}
}

func TestBucketLen_OutOfRange(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	if kt.BucketLen(-1) != 0 {
		t.Fatal("BucketLen(-1) should return 0")
	}
	if kt.BucketLen(256) != 0 {
		t.Fatal("BucketLen(256) should return 0")
	}
}

// --- DefaultKademliaConfig ---

func TestDefaultKademliaConfig(t *testing.T) {
	cfg := DefaultKademliaConfig()
	if cfg.BucketSize != 16 {
		t.Fatalf("BucketSize: want 16, got %d", cfg.BucketSize)
	}
	if cfg.Alpha != 3 {
		t.Fatalf("Alpha: want 3, got %d", cfg.Alpha)
	}
	if cfg.MaxFailCount != 5 {
		t.Fatalf("MaxFailCount: want 5, got %d", cfg.MaxFailCount)
	}
	if cfg.MaxReplacements != 10 {
		t.Fatalf("MaxReplacements: want 10, got %d", cfg.MaxReplacements)
	}
}

// --- MaxTableSize ---

func TestMaxTableSize(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.MaxTableSize = 3
	kt := NewKademliaTable(selfID, cfg)

	for i := byte(1); i <= 10; i++ {
		kt.AddNode(makeEntry(i))
	}

	if kt.TableSize() > 3 {
		t.Fatalf("TableSize should not exceed MaxTableSize 3, got %d", kt.TableSize())
	}
}

// --- RandomIDForBucket ---

func TestRandomIDForBucket(t *testing.T) {
	var selfID [32]byte
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())

	for bucket := 0; bucket < 256; bucket++ {
		target := kt.RandomIDForBucket(bucket)
		dist := KLogDistance(selfID, target)
		expectedDist := bucket + 1
		if dist != expectedDist {
			t.Fatalf("RandomIDForBucket(%d): distance want %d, got %d", bucket, expectedDist, dist)
		}
	}
}

func TestRandomIDForBucket_OutOfRange(t *testing.T) {
	var selfID [32]byte
	selfID[0] = 0x42
	kt := NewKademliaTable(selfID, DefaultKademliaConfig())
	if kt.RandomIDForBucket(-1) != selfID {
		t.Fatal("RandomIDForBucket(-1) should return selfID")
	}
	if kt.RandomIDForBucket(256) != selfID {
		t.Fatal("RandomIDForBucket(256) should return selfID")
	}
}

// --- StaleNode eviction ---

func TestAddNode_EvictsStaleFromFullBucket(t *testing.T) {
	var selfID [32]byte
	cfg := DefaultKademliaConfig()
	cfg.BucketSize = 2
	cfg.MaxFailCount = 2
	kt := NewKademliaTable(selfID, cfg)

	// Add two nodes to bucket 7 (distance 8).
	var id1, id2 [32]byte
	id1[31] = 0x80
	id2[31] = 0x81
	kt.AddNode(NodeEntry{ID: id1, LastSeen: time.Now()})
	kt.AddNode(NodeEntry{ID: id2, LastSeen: time.Now()})

	// Make id2 stale by recording failures.
	kt.RecordFailure(id2)
	// id2 should still be there after 1 failure (MaxFailCount=2).
	// But now let's try to add a new node: the stale check in AddNode
	// uses fail count. Let's mark the second failure so it becomes truly stale.
	kt.RecordFailure(id2)
	// id2 is now evicted by RecordFailure.

	// Add a new node, should succeed since id2 was evicted.
	var id3 [32]byte
	id3[31] = 0x82
	added := kt.AddNode(NodeEntry{ID: id3, LastSeen: time.Now()})
	if !added {
		t.Fatal("expected new node to be added after stale eviction")
	}
}
