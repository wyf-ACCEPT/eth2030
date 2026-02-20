package portal

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- Routing table creation ---

func TestRouting_NewRoutingTable(t *testing.T) {
	var self [32]byte
	self[0] = 0x42
	rt := NewRoutingTable(self)
	if rt == nil {
		t.Fatal("NewRoutingTable returned nil")
	}
	if rt.Self() != self {
		t.Fatal("Self() mismatch")
	}
	if rt.Len() != 0 {
		t.Fatalf("empty table Len: want 0, got %d", rt.Len())
	}
}

// --- Add self is a no-op ---

func TestRouting_AddSelfIgnored(t *testing.T) {
	var self [32]byte
	self[0] = 0x99
	rt := NewRoutingTable(self)
	rt.AddPeer(&PeerInfo{NodeID: self})
	if rt.Len() != 0 {
		t.Fatal("adding self should be ignored")
	}
}

// --- Add and count ---

func TestRouting_AddPeersAndLen(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	for i := byte(1); i <= 10; i++ {
		var id [32]byte
		id[0] = i
		rt.AddPeer(&PeerInfo{NodeID: id, Radius: MaxRadius()})
	}

	if rt.Len() != 10 {
		t.Fatalf("Len: want 10, got %d", rt.Len())
	}
}

// --- Add duplicate updates in place ---

func TestRouting_AddDuplicateUpdate(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id [32]byte
	id[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: id, Radius: MaxRadius()})
	// Update with zero radius.
	rt.AddPeer(&PeerInfo{NodeID: id, Radius: ZeroRadius()})

	if rt.Len() != 1 {
		t.Fatalf("Len after duplicate: want 1, got %d", rt.Len())
	}
	p := rt.GetPeer(id)
	if p == nil {
		t.Fatal("GetPeer returned nil")
	}
	if p.Radius.Raw.Sign() != 0 {
		t.Fatal("peer radius should have been updated to zero")
	}
}

// --- Remove peer ---

func TestRouting_RemovePeer(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id1, id2 [32]byte
	id1[0] = 0x10
	id2[0] = 0x20
	rt.AddPeer(&PeerInfo{NodeID: id1})
	rt.AddPeer(&PeerInfo{NodeID: id2})

	rt.RemovePeer(id1)
	if rt.Len() != 1 {
		t.Fatalf("Len after remove: want 1, got %d", rt.Len())
	}
	if rt.GetPeer(id1) != nil {
		t.Fatal("removed peer still found")
	}
	if rt.GetPeer(id2) == nil {
		t.Fatal("remaining peer not found")
	}
}

// --- Remove non-existent peer (no-op) ---

func TestRouting_RemoveNonexistent(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	var id [32]byte
	id[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: id})

	var ghost [32]byte
	ghost[0] = 0xFF
	rt.RemovePeer(ghost) // should not panic

	if rt.Len() != 1 {
		t.Fatalf("Len after removing non-existent: want 1, got %d", rt.Len())
	}
}

// --- Replacement promotion ---

func TestRouting_ReplacementPromotion(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Fill a bucket plus one replacement.
	var ids [BucketSize + 1][32]byte
	for i := 0; i <= BucketSize; i++ {
		ids[i][0] = 0x80 // same high bit -> same bucket
		ids[i][31] = byte(i + 1)
		rt.AddPeer(&PeerInfo{NodeID: ids[i], Radius: MaxRadius()})
	}

	if rt.Len() != BucketSize {
		t.Fatalf("Len: want %d, got %d", BucketSize, rt.Len())
	}

	// Remove first entry; replacement should be promoted.
	rt.RemovePeer(ids[0])
	if rt.Len() != BucketSize {
		t.Fatalf("Len after promote: want %d, got %d", BucketSize, rt.Len())
	}
	if rt.GetPeer(ids[BucketSize]) == nil {
		t.Fatal("replacement was not promoted")
	}
}

// --- ClosestPeers ---

func TestRouting_ClosestPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var near, mid, far [32]byte
	near[31] = 0x01 // distance = 1
	mid[31] = 0x08  // distance = 8
	far[0] = 0xFF   // distance = 2^255+

	rt.AddPeer(&PeerInfo{NodeID: near, Radius: MaxRadius()})
	rt.AddPeer(&PeerInfo{NodeID: mid, Radius: MaxRadius()})
	rt.AddPeer(&PeerInfo{NodeID: far, Radius: MaxRadius()})

	closest := rt.ClosestPeers(self, 2)
	if len(closest) != 2 {
		t.Fatalf("ClosestPeers: want 2, got %d", len(closest))
	}
	if closest[0].NodeID != near {
		t.Fatal("first closest should be 'near'")
	}
	if closest[1].NodeID != mid {
		t.Fatal("second closest should be 'mid'")
	}
}

func TestRouting_ClosestPeersEmpty(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	closest := rt.ClosestPeers(self, 5)
	if len(closest) != 0 {
		t.Fatalf("ClosestPeers on empty table: want 0, got %d", len(closest))
	}
}

// --- AllPeers ---

func TestRouting_AllPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id1, id2 [32]byte
	id1[0] = 0x10
	id2[0] = 0x20
	rt.AddPeer(&PeerInfo{NodeID: id1})
	rt.AddPeer(&PeerInfo{NodeID: id2})

	all := rt.AllPeers()
	if len(all) != 2 {
		t.Fatalf("AllPeers: want 2, got %d", len(all))
	}
}

// --- BucketIndex ---

func TestRouting_BucketIndexSelf(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	if rt.BucketIndex(self) != -1 {
		t.Fatal("BucketIndex(self) should be -1")
	}
}

func TestRouting_BucketIndexNear(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var near [32]byte
	near[31] = 0x01 // log distance = 1, bucket = 0
	if idx := rt.BucketIndex(near); idx != 0 {
		t.Fatalf("BucketIndex: want 0, got %d", idx)
	}
}

// --- BucketEntries ---

func TestRouting_BucketEntriesOutOfRange(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	if entries := rt.BucketEntries(-1); entries != nil {
		t.Fatal("BucketEntries(-1) should be nil")
	}
	if entries := rt.BucketEntries(256); entries != nil {
		t.Fatal("BucketEntries(256) should be nil")
	}
}

func TestRouting_BucketEntriesValid(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id [32]byte
	id[31] = 0x01 // bucket 0
	rt.AddPeer(&PeerInfo{NodeID: id})

	entries := rt.BucketEntries(0)
	if len(entries) != 1 {
		t.Fatalf("BucketEntries(0): want 1, got %d", len(entries))
	}
}

// --- PeersInRadius ---

func TestRouting_PeersInRadius(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var closeID [32]byte
	closeID[31] = 0x01 // distance = 1
	var farID [32]byte
	farID[0] = 0xFF // distance huge

	rt.AddPeer(&PeerInfo{NodeID: closeID, Radius: MaxRadius()})
	rt.AddPeer(&PeerInfo{NodeID: farID, Radius: MaxRadius()})

	smallRadius := NodeRadius{Raw: big.NewInt(256)}
	inRadius := rt.PeersInRadius(self, smallRadius)
	if len(inRadius) != 1 {
		t.Fatalf("PeersInRadius (small): want 1, got %d", len(inRadius))
	}
	if inRadius[0].NodeID != closeID {
		t.Fatal("expected close peer in small radius")
	}

	inRadius = rt.PeersInRadius(self, MaxRadius())
	if len(inRadius) != 2 {
		t.Fatalf("PeersInRadius (max): want 2, got %d", len(inRadius))
	}
}

// --- RadiusUpdate ---

func TestRouting_RadiusUpdateEmpty(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	rt.RadiusUpdate(0, 1000)
	r := rt.Radius()
	if r.Raw.Cmp(MaxRadius().Raw) != 0 {
		t.Fatal("empty storage should give max radius")
	}
}

func TestRouting_RadiusUpdateHalf(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	rt.RadiusUpdate(500, 1000)
	r := rt.Radius()
	halfMax := new(big.Int).Div(MaxRadius().Raw, big.NewInt(2))
	if r.Raw.Cmp(halfMax) != 0 {
		t.Fatalf("half-full radius: want %v, got %v", halfMax, r.Raw)
	}
}

func TestRouting_RadiusUpdateFull(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	rt.RadiusUpdate(1000, 1000)
	r := rt.Radius()
	if r.Raw.Sign() != 0 {
		t.Fatalf("full storage radius: want 0, got %v", r.Raw)
	}
}

func TestRouting_RadiusUpdateZeroCapacity(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	rt.RadiusUpdate(0, 0)
	r := rt.Radius()
	if r.Raw.Sign() != 0 {
		t.Fatal("zero capacity should give zero radius")
	}
}

// --- ContentLookup ---

func TestRouting_ContentLookupFound(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xdead")}.Encode()
	expectedContent := []byte("routing test content")

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		if peer.NodeID == peerID {
			return expectedContent, nil, nil
		}
		return nil, nil, ErrNotFound
	}

	result := rt.ContentLookup(contentKey, queryFn)
	if !result.Found {
		t.Fatal("content should have been found")
	}
	if string(result.Content) != string(expectedContent) {
		t.Fatalf("content mismatch: got %q, want %q", result.Content, expectedContent)
	}
}

func TestRouting_ContentLookupNotFound(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xbeef")}.Encode()

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		return nil, nil, nil
	}

	result := rt.ContentLookup(contentKey, queryFn)
	if result.Found {
		t.Fatal("content should not have been found")
	}
}

func TestRouting_ContentLookupEmptyTable(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xfeed")}.Encode()
	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		t.Fatal("query should not be called on empty table")
		return nil, nil, nil
	}

	result := rt.ContentLookup(contentKey, queryFn)
	if result.Found {
		t.Fatal("should not find content in empty table")
	}
}

// --- FindContentPeers ---

func TestRouting_FindContentPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var wideID [32]byte
	wideID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: wideID, Radius: MaxRadius()})

	var narrowID [32]byte
	narrowID[0] = 0x02
	rt.AddPeer(&PeerInfo{NodeID: narrowID, Radius: ZeroRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xaaaa")}.Encode()
	peers := rt.FindContentPeers(contentKey, 10)

	if len(peers) != 1 {
		t.Fatalf("FindContentPeers: want 1, got %d", len(peers))
	}
	if peers[0].NodeID != wideID {
		t.Fatal("expected wide-radius peer")
	}
}

// --- OfferContent ---

func TestRouting_OfferContentEmpty(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	results := rt.OfferContent(nil, nil)
	if results != nil {
		t.Fatal("OfferContent(nil) should return nil")
	}

	results = rt.OfferContent([][]byte{}, nil)
	if results != nil {
		t.Fatal("OfferContent([]) should return nil")
	}
}

func TestRouting_OfferContentAccepted(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	keys := [][]byte{
		BlockHeaderKey{BlockHash: types.HexToHash("0x01")}.Encode(),
	}

	offerFn := func(peer *PeerInfo, contentKeys [][]byte) ([]bool, error) {
		return []bool{true}, nil
	}

	results := rt.OfferContent(keys, offerFn)
	if len(results) == 0 {
		t.Fatal("expected at least one offer result")
	}
	if !results[0].Accepted[0] {
		t.Fatal("first key should be accepted")
	}
}

// --- Constants ---

func TestRouting_Constants(t *testing.T) {
	if BucketSize != 16 {
		t.Fatalf("BucketSize: want 16, got %d", BucketSize)
	}
	if NumBuckets != 256 {
		t.Fatalf("NumBuckets: want 256, got %d", NumBuckets)
	}
	if LookupAlpha != 3 {
		t.Fatalf("LookupAlpha: want 3, got %d", LookupAlpha)
	}
	if MaxReplacements != 10 {
		t.Fatalf("MaxReplacements: want 10, got %d", MaxReplacements)
	}
}
