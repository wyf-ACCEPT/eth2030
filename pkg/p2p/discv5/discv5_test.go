package discv5

import (
	"net"
	"sync"
	"testing"
	"time"
)

// mockTransport implements Transport for testing.
type mockTransport struct {
	mu       sync.Mutex
	pingOK   bool
	findResp map[NodeID][]*NodeRecord // keyed by target node ID
	enrResp  map[NodeID]*NodeRecord
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		pingOK:   true,
		findResp: make(map[NodeID][]*NodeRecord),
		enrResp:  make(map[NodeID]*NodeRecord),
	}
}

func (m *mockTransport) Ping(_ *NodeRecord) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pingOK
}

func (m *mockTransport) FindNode(target *NodeRecord, _ int) []*NodeRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findResp[target.NodeID]
}

func (m *mockTransport) RequestENR(node *NodeRecord) (*NodeRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.enrResp[node.NodeID]; ok {
		return r, nil
	}
	return nil, ErrNodeNotFound
}

func (m *mockTransport) setPingOK(ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingOK = ok
}

// makeID creates a NodeID with the given byte value in the first position.
func makeID(b byte) NodeID {
	var id NodeID
	id[0] = b
	return id
}

// makeRecord creates a NodeRecord with the given byte value.
func makeRecord(b byte) *NodeRecord {
	return &NodeRecord{
		NodeID:  makeID(b),
		IP:      net.IPv4(127, 0, 0, 1),
		UDPPort: 30303,
		TCPPort: 30303,
	}
}

func TestDistance(t *testing.T) {
	a := NodeID{}
	b := NodeID{}

	// Identical nodes have distance 0.
	if d := Distance(a, b); d != 0 {
		t.Errorf("Distance(a, a) = %d, want 0", d)
	}

	// Differ in last bit.
	b[31] = 1
	if d := Distance(a, b); d != 1 {
		t.Errorf("Distance(a, b) = %d, want 1", d)
	}

	// Differ in most significant bit.
	b = NodeID{}
	b[0] = 0x80
	if d := Distance(a, b); d != 256 {
		t.Errorf("Distance(a, b) = %d, want 256", d)
	}

	// Differ in second-most significant bit.
	b = NodeID{}
	b[0] = 0x40
	if d := Distance(a, b); d != 255 {
		t.Errorf("Distance(a, b) = %d, want 255", d)
	}
}

func TestAddNode(t *testing.T) {
	selfID := makeID(0)
	d := New(selfID, Config{}, nil)

	// Cannot add self.
	selfRec := makeRecord(0)
	if err := d.AddNode(selfRec); err != ErrSelf {
		t.Errorf("AddNode(self) = %v, want ErrSelf", err)
	}

	// Add a node.
	rec := makeRecord(1)
	if err := d.AddNode(rec); err != nil {
		t.Fatalf("AddNode: %v", err)
	}
	if d.Len() != 1 {
		t.Errorf("Len() = %d, want 1", d.Len())
	}

	// Adding the same node again should succeed (update last-seen).
	if err := d.AddNode(rec); err != nil {
		t.Errorf("AddNode(duplicate) = %v, want nil", err)
	}
	if d.Len() != 1 {
		t.Errorf("Len() = %d after duplicate, want 1", d.Len())
	}
}

func TestRemoveNode(t *testing.T) {
	selfID := makeID(0)
	d := New(selfID, Config{}, nil)

	rec := makeRecord(1)
	d.AddNode(rec)
	if d.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", d.Len())
	}

	d.RemoveNode(rec.NodeID)
	if d.Len() != 0 {
		t.Errorf("Len() = %d after remove, want 0", d.Len())
	}
}

func TestGetNode(t *testing.T) {
	selfID := makeID(0)
	d := New(selfID, Config{}, nil)

	rec := makeRecord(1)
	d.AddNode(rec)

	got := d.GetNode(rec.NodeID)
	if got == nil {
		t.Fatal("GetNode returned nil")
	}
	if got.NodeID != rec.NodeID {
		t.Errorf("GetNode NodeID = %v, want %v", got.NodeID, rec.NodeID)
	}

	// Unknown node.
	if got := d.GetNode(makeID(99)); got != nil {
		t.Error("GetNode(unknown) returned non-nil")
	}
}

func TestBucketFull(t *testing.T) {
	selfID := NodeID{}
	cfg := Config{BucketSize: 4}
	d := New(selfID, cfg, nil)

	// All nodes with high bit set in byte 0 will be at distance 256,
	// bucket index 255.
	for i := 0; i < 4; i++ {
		var id NodeID
		id[0] = 0x80
		id[31] = byte(i + 1) // distinct IDs
		rec := &NodeRecord{NodeID: id, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303}
		if err := d.AddNode(rec); err != nil {
			t.Fatalf("AddNode(%d): %v", i, err)
		}
	}

	if d.BucketLen(255) != 4 {
		t.Fatalf("BucketLen(255) = %d, want 4", d.BucketLen(255))
	}

	// 5th node should fail.
	var extra NodeID
	extra[0] = 0x80
	extra[31] = 0xFF
	rec := &NodeRecord{NodeID: extra, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303}
	if err := d.AddNode(rec); err != ErrBucketFull {
		t.Errorf("AddNode(5th) = %v, want ErrBucketFull", err)
	}
}

func TestClosestNodes(t *testing.T) {
	selfID := NodeID{}
	d := New(selfID, Config{}, nil)

	// Add nodes at various distances.
	for i := 1; i <= 10; i++ {
		var id NodeID
		id[31] = byte(i)
		d.AddNode(&NodeRecord{NodeID: id, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303})
	}

	target := NodeID{}
	target[31] = 3

	closest := d.ClosestNodes(target, 5)
	if len(closest) != 5 {
		t.Fatalf("ClosestNodes returned %d, want 5", len(closest))
	}

	// The first result should be the exact match (distance 0 to target).
	if closest[0].NodeID != target {
		t.Errorf("closest[0] = %v, want %v", closest[0].NodeID, target)
	}

	// Verify sorted order.
	for i := 1; i < len(closest); i++ {
		d1 := Distance(target, closest[i-1].NodeID)
		d2 := Distance(target, closest[i].NodeID)
		if d1 > d2 {
			t.Errorf("closest[%d] dist %d > closest[%d] dist %d", i-1, d1, i, d2)
		}
	}
}

func TestPing(t *testing.T) {
	selfID := makeID(0)
	tr := newMockTransport()
	d := New(selfID, Config{}, tr)

	rec := makeRecord(1)
	d.AddNode(rec)

	// Successful ping.
	if !d.Ping(rec) {
		t.Error("Ping returned false, want true")
	}

	// Failed ping.
	tr.setPingOK(false)
	if d.Ping(rec) {
		t.Error("Ping returned true, want false")
	}
}

func TestFindNode(t *testing.T) {
	selfID := makeID(0)
	tr := newMockTransport()
	d := New(selfID, Config{}, tr)

	target := makeRecord(1)
	d.AddNode(target)

	neighbor := makeRecord(2)
	tr.findResp[target.NodeID] = []*NodeRecord{neighbor}

	results := d.FindNode(target, 1)
	if len(results) != 1 {
		t.Fatalf("FindNode returned %d results, want 1", len(results))
	}
	if results[0].NodeID != neighbor.NodeID {
		t.Errorf("FindNode[0] = %v, want %v", results[0].NodeID, neighbor.NodeID)
	}

	// The neighbor should also be added to the table.
	if d.GetNode(neighbor.NodeID) == nil {
		t.Error("neighbor not added to table")
	}
}

func TestRequestENR(t *testing.T) {
	selfID := makeID(0)
	tr := newMockTransport()
	d := New(selfID, Config{}, tr)

	node := makeRecord(1)
	updated := &NodeRecord{
		NodeID:    node.NodeID,
		IP:        net.IPv4(10, 0, 0, 1),
		UDPPort:   9000,
		SeqNumber: 5,
	}
	tr.enrResp[node.NodeID] = updated

	got, err := d.RequestENR(node)
	if err != nil {
		t.Fatalf("RequestENR: %v", err)
	}
	if got.SeqNumber != 5 {
		t.Errorf("SeqNumber = %d, want 5", got.SeqNumber)
	}
}

func TestLookup(t *testing.T) {
	selfID := NodeID{}
	tr := newMockTransport()
	d := New(selfID, Config{BucketSize: 16}, tr)

	// Seed the table with a few nodes.
	for i := 1; i <= 5; i++ {
		var id NodeID
		id[31] = byte(i)
		d.AddNode(&NodeRecord{NodeID: id, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303})
	}

	// When node 1 is queried, it returns nodes 6 and 7.
	node1ID := makeID(1)
	var id6, id7 NodeID
	id6[31] = 6
	id7[31] = 7
	tr.findResp[node1ID] = []*NodeRecord{
		{NodeID: id6, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303},
		{NodeID: id7, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303},
	}

	target := NodeID{}
	target[31] = 3

	results := d.Lookup(target)
	if len(results) == 0 {
		t.Fatal("Lookup returned no results")
	}

	// Should include the target itself and newly discovered nodes.
	found := make(map[NodeID]bool)
	for _, r := range results {
		found[r.NodeID] = true
	}
	if !found[target] {
		t.Error("target not in lookup results")
	}
}

func TestRandomLookup(t *testing.T) {
	selfID := NodeID{}
	tr := newMockTransport()
	d := New(selfID, Config{}, tr)

	// Seed the table.
	for i := 1; i <= 3; i++ {
		var id NodeID
		id[31] = byte(i)
		d.AddNode(&NodeRecord{NodeID: id, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303})
	}

	// RandomLookup should not panic and should return results.
	results := d.RandomLookup()
	if len(results) == 0 {
		t.Error("RandomLookup returned no results")
	}
}

func TestEvictStale(t *testing.T) {
	selfID := makeID(0)
	tr := newMockTransport()
	d := New(selfID, Config{}, tr)

	rec := makeRecord(1)
	d.AddNode(rec)

	// Fail the node 3 times.
	tr.setPingOK(false)
	d.Ping(rec)
	d.Ping(rec)
	d.Ping(rec)

	evicted := d.EvictStale(3)
	if evicted != 1 {
		t.Errorf("EvictStale = %d, want 1", evicted)
	}
	if d.Len() != 0 {
		t.Errorf("Len() = %d after eviction, want 0", d.Len())
	}
}

func TestNodesAtDistance(t *testing.T) {
	selfID := NodeID{}
	d := New(selfID, Config{}, nil)

	// Node with high bit set => distance 256.
	var id NodeID
	id[0] = 0x80
	d.AddNode(&NodeRecord{NodeID: id, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303})

	nodes := d.NodesAtDistance(256)
	if len(nodes) != 1 {
		t.Errorf("NodesAtDistance(256) = %d, want 1", len(nodes))
	}

	// Distance 1: node with only the last bit set.
	var id2 NodeID
	id2[31] = 1
	d.AddNode(&NodeRecord{NodeID: id2, IP: net.IPv4(127, 0, 0, 1), UDPPort: 30303})

	nodes = d.NodesAtDistance(1)
	if len(nodes) != 1 {
		t.Errorf("NodesAtDistance(1) = %d, want 1", len(nodes))
	}

	// Invalid distances.
	if nodes := d.NodesAtDistance(0); nodes != nil {
		t.Error("NodesAtDistance(0) should be nil")
	}
	if nodes := d.NodesAtDistance(257); nodes != nil {
		t.Error("NodesAtDistance(257) should be nil")
	}
}

func TestStartStop(t *testing.T) {
	selfID := makeID(0)
	tr := newMockTransport()
	cfg := Config{RefreshInterval: 50 * time.Millisecond}
	d := New(selfID, cfg, tr)

	d.Start()
	// Let the maintenance loop run at least once.
	time.Sleep(100 * time.Millisecond)
	d.Stop()

	// Double-stop should be safe.
	d.Stop()
}

func TestConcurrentAccess(t *testing.T) {
	selfID := makeID(0)
	tr := newMockTransport()
	d := New(selfID, Config{}, tr)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := &NodeRecord{
				NodeID:  makeID(byte(i + 1)),
				IP:      net.IPv4(127, 0, 0, 1),
				UDPPort: uint16(30000 + i),
			}
			d.AddNode(rec)
			d.GetNode(rec.NodeID)
			d.Ping(rec)
			d.Len()
			d.RemoveNode(rec.NodeID)
		}(i)
	}
	wg.Wait()
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	cfg.defaults()

	if cfg.BucketSize != BucketSize {
		t.Errorf("BucketSize = %d, want %d", cfg.BucketSize, BucketSize)
	}
	if cfg.MaxPeers != DefaultMaxPeers {
		t.Errorf("MaxPeers = %d, want %d", cfg.MaxPeers, DefaultMaxPeers)
	}
	if cfg.RefreshInterval != time.Duration(DefaultRefreshSeconds)*time.Second {
		t.Errorf("RefreshInterval = %v, want %v", cfg.RefreshInterval, DefaultRefreshSeconds)
	}
	if cfg.PingTimeout != DefaultPingTimeout {
		t.Errorf("PingTimeout = %v, want %v", cfg.PingTimeout, DefaultPingTimeout)
	}
}

func TestDistanceSymmetry(t *testing.T) {
	a := makeID(0x42)
	b := makeID(0xAB)

	d1 := Distance(a, b)
	d2 := Distance(b, a)
	if d1 != d2 {
		t.Errorf("Distance not symmetric: %d != %d", d1, d2)
	}
}

func TestNilTransport(t *testing.T) {
	selfID := makeID(0)
	d := New(selfID, Config{}, nil)

	rec := makeRecord(1)
	d.AddNode(rec)

	// Ping with nil transport returns false.
	if d.Ping(rec) {
		t.Error("Ping with nil transport should return false")
	}

	// FindNode with nil transport returns nil.
	if results := d.FindNode(rec, 1); results != nil {
		t.Error("FindNode with nil transport should return nil")
	}

	// RequestENR with nil transport returns error.
	if _, err := d.RequestENR(rec); err == nil {
		t.Error("RequestENR with nil transport should return error")
	}
}
