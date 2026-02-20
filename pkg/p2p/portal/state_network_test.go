package portal

import (
	"bytes"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// --- StateContentKeyV2 encoding/decoding ---

func TestAccountTrieNodeKeyV2Encode(t *testing.T) {
	addr := types.HexToAddress("0xdead")
	path := []byte{0x01, 0x02, 0x03}
	key := MakeAccountTrieNodeKey(addr, path)

	encoded := key.Encode()
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}
	if encoded[0] != StateKeyAccountTrieNode {
		t.Fatalf("type = 0x%02x, want 0x%02x", encoded[0], StateKeyAccountTrieNode)
	}
	if len(encoded) != 1+types.HashLength+len(path) {
		t.Fatalf("encoded len = %d, want %d", len(encoded), 1+types.HashLength+len(path))
	}
}

func TestContractStorageTrieNodeKeyV2Encode(t *testing.T) {
	addr := types.HexToAddress("0xbeef")
	path := []byte{0x0a, 0x0b}
	key := MakeContractStorageTrieNodeKey(addr, path)

	encoded := key.Encode()
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}
	if encoded[0] != StateKeyContractStorageTrieNode {
		t.Fatalf("type = 0x%02x, want 0x%02x", encoded[0], StateKeyContractStorageTrieNode)
	}
}

func TestContractBytecodeKeyV2Encode(t *testing.T) {
	addr := types.HexToAddress("0xcafe")
	codeHash := types.HexToHash("0xc0de")
	key := MakeContractBytecodeKey(addr, codeHash)

	encoded := key.Encode()
	if encoded == nil {
		t.Fatal("Encode returned nil")
	}
	if encoded[0] != StateKeyContractBytecode {
		t.Fatalf("type = 0x%02x, want 0x%02x", encoded[0], StateKeyContractBytecode)
	}
	if len(encoded) != 1+types.HashLength+types.HashLength {
		t.Fatalf("encoded len = %d, want %d", len(encoded), 1+2*types.HashLength)
	}
}

func TestStateContentKeyV2EncodeUnknownType(t *testing.T) {
	key := StateContentKeyV2{Type: 0xFF}
	if key.Encode() != nil {
		t.Fatal("unknown type should encode to nil")
	}
}

func TestDecodeStateContentKeyV2(t *testing.T) {
	addr := types.HexToAddress("0xdead")
	path := []byte{0x01, 0x02}
	original := MakeAccountTrieNodeKey(addr, path)
	encoded := original.Encode()

	decoded, err := DecodeStateContentKeyV2(encoded)
	if err != nil {
		t.Fatalf("DecodeStateContentKeyV2: %v", err)
	}
	if decoded.Type != StateKeyAccountTrieNode {
		t.Fatalf("decoded type = 0x%02x, want 0x%02x", decoded.Type, StateKeyAccountTrieNode)
	}
	if !bytes.Equal(decoded.Path, path) {
		t.Fatalf("decoded path = %x, want %x", decoded.Path, path)
	}
}

func TestDecodeStateContentKeyV2Errors(t *testing.T) {
	// Too short.
	if _, err := DecodeStateContentKeyV2([]byte{0x20}); err != ErrInvalidStateContent {
		t.Fatalf("short data: got %v, want ErrInvalidStateContent", err)
	}
	// Unknown type.
	bad := make([]byte, 1+types.HashLength)
	bad[0] = 0xFF
	if _, err := DecodeStateContentKeyV2(bad); err != ErrInvalidStateContent {
		t.Fatalf("unknown type: got %v, want ErrInvalidStateContent", err)
	}
}

// --- StateContentID ---

func TestStateContentID(t *testing.T) {
	addr := types.HexToAddress("0xdead")
	key := MakeAccountTrieNodeKey(addr, []byte{0x01})

	id := StateContentID(key)
	if id.IsZero() {
		t.Fatal("content ID should not be zero")
	}

	// Same key should produce same ID.
	id2 := StateContentID(key)
	if id != id2 {
		t.Fatal("content ID should be deterministic")
	}
}

func TestStateContentIDDifferentTypes(t *testing.T) {
	addr := types.HexToAddress("0xdead")
	path := []byte{0x01}

	id1 := StateContentID(MakeAccountTrieNodeKey(addr, path))
	id2 := StateContentID(MakeContractStorageTrieNodeKey(addr, path))
	id3 := StateContentID(MakeContractBytecodeKey(addr, types.HexToHash("0x01")))

	if id1 == id2 {
		t.Fatal("account and storage content IDs should differ")
	}
	if id1 == id3 {
		t.Fatal("account and bytecode content IDs should differ")
	}
}

// --- StateNetworkClient lifecycle ---

func newTestClient() (*StateNetworkClient, *MemoryStore) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20) // 1 MiB
	client := NewStateNetworkClient(rt, store)
	return client, store
}

func TestStateNetworkClientStartStop(t *testing.T) {
	client, _ := newTestClient()
	if client.IsStarted() {
		t.Fatal("should not be started initially")
	}
	client.Start()
	if !client.IsStarted() {
		t.Fatal("should be started after Start()")
	}
	client.Stop()
	if client.IsStarted() {
		t.Fatal("should not be started after Stop()")
	}
}

func TestFindContentNotStarted(t *testing.T) {
	client, _ := newTestClient()
	key := MakeAccountTrieNodeKey(types.HexToAddress("0x01"), []byte{0x00})
	_, err := client.FindContent(key)
	if err != ErrClientNotStarted {
		t.Fatalf("got %v, want ErrClientNotStarted", err)
	}
}

func TestFindContentFromLocalStore(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	addr := types.HexToAddress("0xdead")
	key := MakeAccountTrieNodeKey(addr, []byte{0x01})
	data := []byte("trie node data")

	// Store content first.
	if err := client.StoreContent(key, data); err != nil {
		t.Fatalf("StoreContent: %v", err)
	}

	// Find should return from local store.
	got, err := client.FindContent(key)
	if err != nil {
		t.Fatalf("FindContent: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data = %q, want %q", got, data)
	}
}

func TestFindContentMissing(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	key := MakeAccountTrieNodeKey(types.HexToAddress("0xdead"), []byte{0x01})
	_, err := client.FindContent(key)
	// No peers and not in local store.
	if err != ErrNoPeers {
		t.Fatalf("got %v, want ErrNoPeers", err)
	}
}

func TestFindContentInvalidKey(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	badKey := StateContentKeyV2{Type: 0xFF}
	_, err := client.FindContent(badKey)
	if err != ErrInvalidStateContent {
		t.Fatalf("got %v, want ErrInvalidStateContent", err)
	}
}

func TestStoreContentEmpty(t *testing.T) {
	client, _ := newTestClient()
	key := MakeAccountTrieNodeKey(types.HexToAddress("0x01"), []byte{0x00})
	if err := client.StoreContent(key, nil); err != ErrEmptyPayload {
		t.Fatalf("nil data: got %v, want ErrEmptyPayload", err)
	}
	if err := client.StoreContent(key, []byte{}); err != ErrEmptyPayload {
		t.Fatalf("empty data: got %v, want ErrEmptyPayload", err)
	}
}

func TestStoreContentInvalidKey(t *testing.T) {
	client, _ := newTestClient()
	badKey := StateContentKeyV2{Type: 0xFF}
	err := client.StoreContent(badKey, []byte("data"))
	if err != ErrInvalidStateContent {
		t.Fatalf("got %v, want ErrInvalidStateContent", err)
	}
}

// --- OfferContent ---

func TestOfferContentNotStarted(t *testing.T) {
	client, _ := newTestClient()
	key := MakeAccountTrieNodeKey(types.HexToAddress("0x01"), []byte{0x00})
	_, err := client.OfferContent(key, []byte("data"))
	if err != ErrClientNotStarted {
		t.Fatalf("got %v, want ErrClientNotStarted", err)
	}
}

func TestOfferContentEmptyPayload(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	key := MakeAccountTrieNodeKey(types.HexToAddress("0x01"), []byte{0x00})
	_, err := client.OfferContent(key, nil)
	if err != ErrEmptyPayload {
		t.Fatalf("got %v, want ErrEmptyPayload", err)
	}
}

func TestOfferContentWithPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	client := NewStateNetworkClient(rt, store)
	client.Start()
	defer client.Stop()

	// Add peer with max radius.
	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	key := MakeAccountTrieNodeKey(types.HexToAddress("0xdead"), []byte{0x01})
	accepted, err := client.OfferContent(key, []byte("trie data"))
	if err != nil {
		t.Fatalf("OfferContent: %v", err)
	}
	if accepted == 0 {
		t.Fatal("expected at least one peer to accept")
	}
}

func TestOfferContentRejectedNoPeers(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	key := MakeAccountTrieNodeKey(types.HexToAddress("0xdead"), []byte{0x01})
	_, err := client.OfferContent(key, []byte("data"))
	if err != ErrOfferRejected {
		t.Fatalf("got %v, want ErrOfferRejected", err)
	}
}

// --- ComputeContentDistance ---

func TestComputeContentDistance(t *testing.T) {
	var nodeID [32]byte
	nodeID[0] = 0x01

	var contentID ContentID
	contentID[0] = 0xFF

	dist := ComputeContentDistance(nodeID, contentID)
	if dist.Sign() == 0 {
		t.Fatal("distance should not be zero")
	}

	// Distance to self should be zero.
	distSelf := ComputeContentDistance(nodeID, ContentID(nodeID))
	if distSelf.Sign() != 0 {
		t.Fatalf("distance to self = %v, want 0", distSelf)
	}
}

func TestComputeContentDistanceSymmetric(t *testing.T) {
	var a [32]byte
	a[0] = 0x42
	var b ContentID
	b[0] = 0xAB

	d1 := ComputeContentDistance(a, b)
	d2 := ComputeContentDistance([32]byte(b), ContentID(a))
	if d1.Cmp(d2) != 0 {
		t.Fatalf("distance not symmetric: %v vs %v", d1, d2)
	}
}

// --- RadiusFilter ---

func TestRadiusFilterMaxRadius(t *testing.T) {
	var nodeID [32]byte
	var contentID ContentID
	contentID[0] = 0xFF

	// Max radius should include everything.
	if !RadiusFilter(nodeID, contentID, MaxRadius()) {
		t.Fatal("MaxRadius should include all content")
	}
}

func TestRadiusFilterZeroRadius(t *testing.T) {
	var nodeID [32]byte
	var contentID ContentID
	contentID[0] = 0xFF

	// Zero radius should exclude distant content.
	if RadiusFilter(nodeID, contentID, ZeroRadius()) {
		t.Fatal("ZeroRadius should not include distant content")
	}
}

func TestRadiusFilterSameID(t *testing.T) {
	var nodeID [32]byte
	nodeID[0] = 0x42

	// Same ID is always within any radius (distance = 0).
	if !RadiusFilter(nodeID, ContentID(nodeID), ZeroRadius()) {
		t.Fatal("same ID should be within zero radius")
	}
}

func TestRadiusFilterSmallRadius(t *testing.T) {
	var nodeID [32]byte

	var nearContentID ContentID
	nearContentID[31] = 0x01 // distance = 1

	var farContentID ContentID
	farContentID[0] = 0xFF // distance very large

	smallRadius := NodeRadius{Raw: big.NewInt(256)}

	if !RadiusFilter(nodeID, nearContentID, smallRadius) {
		t.Fatal("near content should be within small radius")
	}
	if RadiusFilter(nodeID, farContentID, smallRadius) {
		t.Fatal("far content should not be within small radius")
	}
}

// --- FindContentWithQuery ---

func TestFindContentWithQueryLocal(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	key := MakeAccountTrieNodeKey(types.HexToAddress("0xdead"), []byte{0x01})
	data := []byte("cached trie node")
	client.StoreContent(key, data)

	// Query function should not be called since content is local.
	queryFn := func(peer *PeerInfo, k []byte) ([]byte, []*PeerInfo, error) {
		t.Fatal("queryFn should not be called for local content")
		return nil, nil, nil
	}

	got, err := client.FindContentWithQuery(key, queryFn)
	if err != nil {
		t.Fatalf("FindContentWithQuery: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("data mismatch")
	}
}

func TestFindContentWithQueryFromPeer(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	client := NewStateNetworkClient(rt, store)
	client.Start()
	defer client.Stop()

	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	key := MakeAccountTrieNodeKey(types.HexToAddress("0xdead"), []byte{0x01})
	expectedData := []byte("trie node from peer")

	queryFn := func(peer *PeerInfo, k []byte) ([]byte, []*PeerInfo, error) {
		return expectedData, nil, nil
	}

	got, err := client.FindContentWithQuery(key, queryFn)
	if err != nil {
		t.Fatalf("FindContentWithQuery: %v", err)
	}
	if !bytes.Equal(got, expectedData) {
		t.Fatal("data mismatch")
	}

	// Should be cached locally now.
	cached, err := client.FindContent(key)
	if err != nil {
		t.Fatalf("cached FindContent: %v", err)
	}
	if !bytes.Equal(cached, expectedData) {
		t.Fatal("cached data mismatch")
	}
}

// --- ContentIDFromRawKey ---

func TestContentIDFromRawKey(t *testing.T) {
	key := MakeAccountTrieNodeKey(types.HexToAddress("0xdead"), []byte{0x01})
	encoded := key.Encode()

	id1 := ContentIDFromRawKey(encoded)
	id2 := StateContentID(key)
	if id1 != id2 {
		t.Fatal("ContentIDFromRawKey should match StateContentID")
	}
}

// --- Concurrency ---

func TestStateNetworkClientConcurrency(t *testing.T) {
	client, _ := newTestClient()
	client.Start()
	defer client.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			addr := types.HexToAddress("0x01")
			path := []byte{byte(idx)}
			key := MakeAccountTrieNodeKey(addr, path)
			data := []byte{byte(idx), byte(idx + 1)}

			_ = client.StoreContent(key, data)
			_, _ = client.FindContent(key)
		}(i)
	}
	wg.Wait()
}
