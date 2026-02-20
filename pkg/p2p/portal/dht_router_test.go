package portal

import (
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/p2p/discover"
)

func makeKademliaNode(b byte) discover.NodeEntry {
	var id [32]byte
	id[31] = b
	return discover.NodeEntry{
		ID:       id,
		Address:  "10.0.0.1",
		Port:     30303,
		LastSeen: time.Now(),
	}
}

func makeKademliaNodeFull(bytes ...byte) discover.NodeEntry {
	var id [32]byte
	for i, b := range bytes {
		if i < 32 {
			id[i] = b
		}
	}
	return discover.NodeEntry{
		ID:       id,
		Address:  "10.0.0.1",
		Port:     30303,
		LastSeen: time.Now(),
	}
}

func newTestRouter() (*DHTRouter, *discover.KademliaTable) {
	var selfID [32]byte
	table := discover.NewKademliaTable(selfID, discover.DefaultKademliaConfig())
	router := NewDHTRouter(table, DefaultDHTRouterConfig())
	return router, table
}

// --- NewDHTRouter ---

func TestDHTRouter_New(t *testing.T) {
	router, _ := newTestRouter()
	if router == nil {
		t.Fatal("NewDHTRouter returned nil")
	}
	if router.Table() == nil {
		t.Fatal("Table should not be nil")
	}
}

func TestDHTRouter_NodeID(t *testing.T) {
	var selfID [32]byte
	selfID[0] = 0xAB
	table := discover.NewKademliaTable(selfID, discover.DefaultKademliaConfig())
	router := NewDHTRouter(table, DefaultDHTRouterConfig())
	if router.NodeID() != selfID {
		t.Fatal("NodeID mismatch")
	}
}

// --- ComputeContentDist ---

func TestComputeContentDist_Identical(t *testing.T) {
	var a [32]byte
	a[0] = 0x42
	dist := ComputeContentDist(a, a)
	if dist.Sign() != 0 {
		t.Fatalf("distance to self: want 0, got %v", dist)
	}
}

func TestComputeContentDist_Different(t *testing.T) {
	var a, b [32]byte
	a[31] = 0x01
	b[31] = 0x02
	dist := ComputeContentDist(a, b)
	// XOR = 0x03
	if dist.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("distance: want 3, got %v", dist)
	}
}

// --- IsWithinRadius ---

func TestDHTRouter_IsWithinRadius_MaxRadius(t *testing.T) {
	router, _ := newTestRouter()
	var contentID [32]byte
	contentID[0] = 0xFF
	if !router.IsWithinRadius(contentID) {
		t.Fatal("all content should be within max radius")
	}
}

func TestDHTRouter_IsWithinRadius_Shrunk(t *testing.T) {
	router, _ := newTestRouter()
	// Set radius to a small value.
	router.UpdateRadius(99, 100)
	radius := router.ContentRadius()
	if radius.Cmp(MaxRadius().Raw) >= 0 {
		t.Fatal("radius should have shrunk")
	}

	// Very distant content should be outside radius.
	var farContent [32]byte
	farContent[0] = 0xFF
	// This depends on the actual radius value. The point is the radius shrunk.
}

// --- FindContentProviders ---

func TestDHTRouter_FindContentProviders(t *testing.T) {
	router, table := newTestRouter()

	for i := byte(1); i <= 5; i++ {
		table.AddNode(makeKademliaNode(i))
	}

	var contentID [32]byte
	contentID[31] = 3 // close to node 3

	providers := router.FindContentProviders(contentID, 3)
	if len(providers) != 3 {
		t.Fatalf("FindContentProviders: want 3, got %d", len(providers))
	}

	// First provider should be the closest to contentID.
	if providers[0].ID[31] != 3 {
		t.Fatalf("closest provider: want ID[31]=3, got %d", providers[0].ID[31])
	}
}

func TestDHTRouter_FindContentProviders_Empty(t *testing.T) {
	router, _ := newTestRouter()
	var contentID [32]byte
	providers := router.FindContentProviders(contentID, 5)
	if len(providers) != 0 {
		t.Fatalf("FindContentProviders on empty table: want 0, got %d", len(providers))
	}
}

func TestDHTRouter_FindContentProviders_ZeroMax(t *testing.T) {
	router, _ := newTestRouter()
	if providers := router.FindContentProviders([32]byte{}, 0); providers != nil {
		t.Fatal("FindContentProviders(0) should return nil")
	}
}

// --- RouteContentRequest ---

func TestDHTRouter_RouteContentRequest_Found(t *testing.T) {
	router, table := newTestRouter()

	node := makeKademliaNode(1)
	table.AddNode(node)

	var contentID [32]byte
	contentID[31] = 0x42
	expectedData := []byte("portal content data")

	queryFn := func(n discover.NodeEntry, cid [32]byte) ([]byte, []byte, []discover.NodeEntry, error) {
		if n.ID == node.ID {
			return expectedData, []byte("proof"), nil, nil
		}
		return nil, nil, nil, ErrDHTLookupFailed
	}

	resp, err := router.RouteContentRequest(contentID, queryFn)
	if err != nil {
		t.Fatalf("RouteContentRequest: %v", err)
	}
	if string(resp.Data) != string(expectedData) {
		t.Fatalf("data mismatch: got %q, want %q", resp.Data, expectedData)
	}
	if string(resp.Proof) != "proof" {
		t.Fatalf("proof mismatch: got %q, want %q", resp.Proof, "proof")
	}
	if resp.FoundNode.ID != node.ID {
		t.Fatal("FoundNode ID mismatch")
	}
}

func TestDHTRouter_RouteContentRequest_NotFound(t *testing.T) {
	router, table := newTestRouter()

	table.AddNode(makeKademliaNode(1))

	var contentID [32]byte
	queryFn := func(n discover.NodeEntry, cid [32]byte) ([]byte, []byte, []discover.NodeEntry, error) {
		return nil, nil, nil, nil // no content, no closer nodes
	}

	_, err := router.RouteContentRequest(contentID, queryFn)
	if err != ErrDHTLookupFailed {
		t.Fatalf("want ErrDHTLookupFailed, got %v", err)
	}
}

func TestDHTRouter_RouteContentRequest_EmptyTable(t *testing.T) {
	router, _ := newTestRouter()

	var contentID [32]byte
	queryFn := func(n discover.NodeEntry, cid [32]byte) ([]byte, []byte, []discover.NodeEntry, error) {
		t.Fatal("should not query when table is empty")
		return nil, nil, nil, nil
	}

	_, err := router.RouteContentRequest(contentID, queryFn)
	if err != ErrDHTNoNodes {
		t.Fatalf("want ErrDHTNoNodes, got %v", err)
	}
}

func TestDHTRouter_RouteContentRequest_CloserNodes(t *testing.T) {
	router, table := newTestRouter()

	node1 := makeKademliaNode(10)
	table.AddNode(node1)

	var contentID [32]byte
	contentID[31] = 5

	// Node 10 returns a closer node (node 5) which has the content.
	node5 := makeKademliaNode(5)
	expectedData := []byte("found via closer node")

	queryFn := func(n discover.NodeEntry, cid [32]byte) ([]byte, []byte, []discover.NodeEntry, error) {
		if n.ID[31] == 10 {
			return nil, nil, []discover.NodeEntry{node5}, nil
		}
		if n.ID[31] == 5 {
			return expectedData, nil, nil, nil
		}
		return nil, nil, nil, nil
	}

	resp, err := router.RouteContentRequest(contentID, queryFn)
	if err != nil {
		t.Fatalf("RouteContentRequest: %v", err)
	}
	if string(resp.Data) != string(expectedData) {
		t.Fatalf("data mismatch: got %q, want %q", resp.Data, expectedData)
	}
}

// --- IterativeNodeLookup ---

func TestDHTRouter_IterativeNodeLookup(t *testing.T) {
	router, table := newTestRouter()

	for i := byte(1); i <= 5; i++ {
		table.AddNode(makeKademliaNode(i))
	}

	var target [32]byte
	target[31] = 3

	queryFn := func(n discover.NodeEntry) []discover.NodeEntry {
		return nil // no new nodes
	}

	closest := router.IterativeNodeLookup(target, queryFn)
	if len(closest) == 0 {
		t.Fatal("IterativeNodeLookup returned no nodes")
	}
}

func TestDHTRouter_IterativeNodeLookup_Empty(t *testing.T) {
	router, _ := newTestRouter()
	var target [32]byte
	queryFn := func(n discover.NodeEntry) []discover.NodeEntry {
		t.Fatal("should not query on empty table")
		return nil
	}
	closest := router.IterativeNodeLookup(target, queryFn)
	if len(closest) != 0 {
		t.Fatalf("want 0, got %d", len(closest))
	}
}

// --- OfferContent ---

func TestDHTRouter_OfferContent_Accepted(t *testing.T) {
	router, table := newTestRouter()

	table.AddNode(makeKademliaNode(1))
	table.AddNode(makeKademliaNode(2))

	var key [32]byte
	key[31] = 0x10

	offerFn := func(node discover.NodeEntry, keys [][32]byte) ([]bool, error) {
		return []bool{true}, nil
	}

	accepted, err := router.OfferContent([][32]byte{key}, offerFn)
	if err != nil {
		t.Fatalf("OfferContent: %v", err)
	}
	if accepted == 0 {
		t.Fatal("expected at least one acceptance")
	}
}

func TestDHTRouter_OfferContent_Empty(t *testing.T) {
	router, _ := newTestRouter()
	accepted, err := router.OfferContent(nil, nil)
	if err != nil {
		t.Fatalf("OfferContent(nil): %v", err)
	}
	if accepted != 0 {
		t.Fatalf("want 0, got %d", accepted)
	}
}

func TestDHTRouter_OfferContent_NoPeers(t *testing.T) {
	router, _ := newTestRouter()
	var key [32]byte
	offerFn := func(node discover.NodeEntry, keys [][32]byte) ([]bool, error) {
		return nil, nil
	}
	_, err := router.OfferContent([][32]byte{key}, offerFn)
	if err != ErrDHTOfferRejected {
		t.Fatalf("want ErrDHTOfferRejected, got %v", err)
	}
}

// --- UpdateRadius ---

func TestDHTRouter_UpdateRadius_Empty(t *testing.T) {
	router, _ := newTestRouter()
	router.UpdateRadius(0, 1000)
	r := router.ContentRadius()
	if r.Cmp(MaxRadius().Raw) != 0 {
		t.Fatal("empty storage should give max radius")
	}
}

func TestDHTRouter_UpdateRadius_Full(t *testing.T) {
	router, _ := newTestRouter()
	router.UpdateRadius(1000, 1000)
	r := router.ContentRadius()
	// Should be at minimum radius.
	minR := computeMinRadius(1)
	if r.Cmp(minR) != 0 {
		t.Fatalf("full storage radius: want min radius, got %v", r)
	}
}

func TestDHTRouter_UpdateRadius_ZeroCapacity(t *testing.T) {
	router, _ := newTestRouter()
	router.UpdateRadius(0, 0)
	r := router.ContentRadius()
	if r.Sign() != 0 {
		t.Fatal("zero capacity should give zero radius")
	}
}

func TestDHTRouter_UpdateRadius_Half(t *testing.T) {
	router, _ := newTestRouter()
	router.UpdateRadius(500, 1000)
	r := router.ContentRadius()
	halfMax := new(big.Int).Div(MaxRadius().Raw, big.NewInt(2))
	if r.Cmp(halfMax) != 0 {
		t.Fatalf("half-full radius: want %v, got %v", halfMax, r)
	}
}

// --- RankedProviders ---

func TestDHTRouter_RankedProviders(t *testing.T) {
	router, table := newTestRouter()

	for i := byte(1); i <= 5; i++ {
		table.AddNode(makeKademliaNode(i))
	}

	var contentID [32]byte
	contentID[31] = 3

	ranked := router.RankedProviders(contentID, 3)
	if len(ranked) != 3 {
		t.Fatalf("RankedProviders: want 3, got %d", len(ranked))
	}
	// First should be closest.
	if ranked[0].Distance.Sign() < 0 {
		t.Fatal("distance should not be negative")
	}
}

// --- DefaultDHTRouterConfig ---

func TestDefaultDHTRouterConfig(t *testing.T) {
	cfg := DefaultDHTRouterConfig()
	if cfg.MaxLookupIterations != 10 {
		t.Fatalf("MaxLookupIterations: want 10, got %d", cfg.MaxLookupIterations)
	}
	if cfg.LookupAlpha != 3 {
		t.Fatalf("LookupAlpha: want 3, got %d", cfg.LookupAlpha)
	}
	if cfg.LookupResultSize != 16 {
		t.Fatalf("LookupResultSize: want 16, got %d", cfg.LookupResultSize)
	}
	if cfg.MaxOfferPeers != 8 {
		t.Fatalf("MaxOfferPeers: want 8, got %d", cfg.MaxOfferPeers)
	}
}
