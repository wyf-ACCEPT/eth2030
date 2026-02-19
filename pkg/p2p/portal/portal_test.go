package portal

import (
	"bytes"
	"crypto/sha256"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- Content key encoding/decoding ---

func TestBlockHeaderKeyEncode(t *testing.T) {
	hash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	key := BlockHeaderKey{BlockHash: hash}
	encoded := key.Encode()

	if len(encoded) != 1+types.HashLength {
		t.Fatalf("encoded length = %d, want %d", len(encoded), 1+types.HashLength)
	}
	if encoded[0] != ContentKeyBlockHeader {
		t.Fatalf("type selector = 0x%02x, want 0x%02x", encoded[0], ContentKeyBlockHeader)
	}
	if !bytes.Equal(encoded[1:], hash[:]) {
		t.Fatal("hash portion does not match")
	}
}

func TestBlockBodyKeyEncode(t *testing.T) {
	hash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	key := BlockBodyKey{BlockHash: hash}
	encoded := key.Encode()

	if encoded[0] != ContentKeyBlockBody {
		t.Fatalf("type selector = 0x%02x, want 0x%02x", encoded[0], ContentKeyBlockBody)
	}
	if !bytes.Equal(encoded[1:], hash[:]) {
		t.Fatal("hash portion does not match")
	}
}

func TestReceiptKeyEncode(t *testing.T) {
	hash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	key := ReceiptKey{BlockHash: hash}
	encoded := key.Encode()

	if encoded[0] != ContentKeyReceipt {
		t.Fatalf("type selector = 0x%02x, want 0x%02x", encoded[0], ContentKeyReceipt)
	}
}

func TestEpochAccumulatorKeyEncode(t *testing.T) {
	hash := types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")
	key := EpochAccumulatorKey{EpochHash: hash}
	encoded := key.Encode()

	if encoded[0] != ContentKeyEpochAccumulator {
		t.Fatalf("type selector = 0x%02x, want 0x%02x", encoded[0], ContentKeyEpochAccumulator)
	}
}

func TestDecodeContentKey(t *testing.T) {
	hash := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	tests := []struct {
		name     string
		key      ContentKeyEncoder
		wantType byte
	}{
		{"header", BlockHeaderKey{BlockHash: hash}, ContentKeyBlockHeader},
		{"body", BlockBodyKey{BlockHash: hash}, ContentKeyBlockBody},
		{"receipt", ReceiptKey{BlockHash: hash}, ContentKeyReceipt},
		{"epoch", EpochAccumulatorKey{EpochHash: hash}, ContentKeyEpochAccumulator},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := tt.key.Encode()
			decoded, err := DecodeContentKey(encoded)
			if err != nil {
				t.Fatalf("DecodeContentKey: %v", err)
			}
			reEncoded := decoded.Encode()
			if !bytes.Equal(encoded, reEncoded) {
				t.Fatal("round-trip encode/decode mismatch")
			}
		})
	}
}

func TestDecodeContentKeyErrors(t *testing.T) {
	// Too short.
	if _, err := DecodeContentKey([]byte{0x00}); err != ErrInvalidContentKey {
		t.Fatalf("short key: got %v, want %v", err, ErrInvalidContentKey)
	}

	// Unknown type.
	bad := make([]byte, 1+types.HashLength)
	bad[0] = 0xFF
	if _, err := DecodeContentKey(bad); err != ErrUnknownKeyType {
		t.Fatalf("unknown type: got %v, want %v", err, ErrUnknownKeyType)
	}
}

// --- Content ID computation ---

func TestComputeContentID(t *testing.T) {
	hash := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	key := BlockHeaderKey{BlockHash: hash}
	encoded := key.Encode()

	id := ComputeContentID(encoded)
	expected := sha256.Sum256(encoded)

	if id != expected {
		t.Fatalf("content ID mismatch:\n  got  %x\n  want %x", id, expected)
	}
}

func TestComputeContentIDDifferentKeys(t *testing.T) {
	hash := types.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	headerID := ComputeContentID(BlockHeaderKey{BlockHash: hash}.Encode())
	bodyID := ComputeContentID(BlockBodyKey{BlockHash: hash}.Encode())
	receiptID := ComputeContentID(ReceiptKey{BlockHash: hash}.Encode())

	// Same hash but different key types must produce different content IDs.
	if headerID == bodyID {
		t.Fatal("header and body content IDs should differ")
	}
	if headerID == receiptID {
		t.Fatal("header and receipt content IDs should differ")
	}
	if bodyID == receiptID {
		t.Fatal("body and receipt content IDs should differ")
	}
}

func TestContentIDDeterministic(t *testing.T) {
	key := BlockHeaderKey{BlockHash: types.HexToHash("0xabcd")}
	id1 := ComputeContentID(key.Encode())
	id2 := ComputeContentID(key.Encode())
	if id1 != id2 {
		t.Fatal("content ID should be deterministic")
	}
}

// --- Distance function ---

func TestDistanceSelf(t *testing.T) {
	var a [32]byte
	a[0] = 0x42
	d := Distance(a, a)
	if d.Sign() != 0 {
		t.Fatalf("distance to self = %v, want 0", d)
	}
}

func TestDistanceSymmetric(t *testing.T) {
	var a, b [32]byte
	a[0] = 0x01
	b[0] = 0xFF
	d1 := Distance(a, b)
	d2 := Distance(b, a)
	if d1.Cmp(d2) != 0 {
		t.Fatalf("distance not symmetric: %v vs %v", d1, d2)
	}
}

func TestDistanceXOR(t *testing.T) {
	var a, b [32]byte
	a[31] = 0x01 // ...0001
	b[31] = 0x03 // ...0011
	// XOR = ...0010 = 2
	d := Distance(a, b)
	if d.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("distance = %v, want 2", d)
	}
}

func TestDistanceLarger(t *testing.T) {
	var a, b [32]byte
	a[0] = 0x80 // highest bit set
	b[0] = 0x00
	d := Distance(a, b)
	// XOR has bit 255 set, so distance = 2^255.
	expected := new(big.Int).Lsh(big.NewInt(1), 255)
	// Actually a[0]=0x80 XOR b[0]=0x00 = 0x80 = bit 7 of byte 0 = bit 255 of the 256-bit number.
	if d.Cmp(expected) != 0 {
		t.Fatalf("distance = %v, want 2^255", d)
	}
}

func TestLogDistance(t *testing.T) {
	var a, b [32]byte
	a[31] = 0x01
	b[31] = 0x03
	// XOR = 0x02 = binary 10, log2 = 2
	ld := LogDistance(a, b)
	if ld != 2 {
		t.Fatalf("log distance = %d, want 2", ld)
	}
}

func TestLogDistanceSelf(t *testing.T) {
	var a [32]byte
	a[0] = 0x55
	ld := LogDistance(a, a)
	if ld != 0 {
		t.Fatalf("log distance to self = %d, want 0", ld)
	}
}

func TestXORBytes(t *testing.T) {
	var a, b [32]byte
	a[0] = 0xFF
	b[0] = 0x0F
	result := XORBytes(a, b)
	if result[0] != 0xF0 {
		t.Fatalf("XOR byte 0 = 0x%02x, want 0xF0", result[0])
	}
	for i := 1; i < 32; i++ {
		if result[i] != 0 {
			t.Fatalf("XOR byte %d = 0x%02x, want 0", i, result[i])
		}
	}
}

// --- Radius ---

func TestMaxRadius(t *testing.T) {
	max := MaxRadius()
	// 2^256 - 1
	expected := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if max.Raw.Cmp(expected) != 0 {
		t.Fatal("MaxRadius not 2^256 - 1")
	}
}

func TestZeroRadius(t *testing.T) {
	zero := ZeroRadius()
	if zero.Raw.Sign() != 0 {
		t.Fatal("ZeroRadius not zero")
	}
}

func TestRadiusContains(t *testing.T) {
	// Max radius contains everything.
	max := MaxRadius()
	var nodeID, contentID [32]byte
	nodeID[0] = 0x01
	contentID[0] = 0xFF
	if !max.Contains(nodeID, contentID) {
		t.Fatal("MaxRadius should contain all content")
	}

	// Zero radius contains nothing (except distance-zero, which is the same ID).
	zero := ZeroRadius()
	if zero.Contains(nodeID, contentID) {
		t.Fatal("ZeroRadius should not contain distant content")
	}

	// Distance-zero: same ID should be in zero radius.
	if !zero.Contains(nodeID, nodeID) {
		t.Fatal("ZeroRadius should contain same-ID content")
	}
}

func TestEncodeDecodeRadius(t *testing.T) {
	r := NodeRadius{Raw: big.NewInt(12345678)}
	encoded := EncodeRadius(r)
	decoded := DecodeRadius(encoded)
	if r.Raw.Cmp(decoded.Raw) != 0 {
		t.Fatalf("round-trip radius: got %v, want %v", decoded.Raw, r.Raw)
	}
}

func TestEncodeDecodeMaxRadius(t *testing.T) {
	r := MaxRadius()
	encoded := EncodeRadius(r)
	decoded := DecodeRadius(encoded)
	if r.Raw.Cmp(decoded.Raw) != 0 {
		t.Fatal("round-trip MaxRadius mismatch")
	}
}

// --- Routing table operations ---

func TestRoutingTableAddAndLen(t *testing.T) {
	var self [32]byte
	self[0] = 0x00
	rt := NewRoutingTable(self)

	if rt.Len() != 0 {
		t.Fatalf("empty table Len = %d, want 0", rt.Len())
	}

	// Add 5 peers at different distances.
	for i := byte(1); i <= 5; i++ {
		var id [32]byte
		id[0] = i
		rt.AddPeer(&PeerInfo{
			NodeID: id,
			Radius: MaxRadius(),
		})
	}

	if rt.Len() != 5 {
		t.Fatalf("table Len = %d, want 5", rt.Len())
	}
}

func TestRoutingTableAddSelf(t *testing.T) {
	var self [32]byte
	self[0] = 0x42
	rt := NewRoutingTable(self)
	rt.AddPeer(&PeerInfo{NodeID: self})
	if rt.Len() != 0 {
		t.Fatal("should not add self to routing table")
	}
}

func TestRoutingTableAddDuplicate(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id [32]byte
	id[0] = 0x01
	peer := &PeerInfo{NodeID: id, Radius: MaxRadius()}
	rt.AddPeer(peer)
	rt.AddPeer(peer)

	if rt.Len() != 1 {
		t.Fatalf("table Len = %d, want 1 (no duplicates)", rt.Len())
	}
}

func TestRoutingTableRemovePeer(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id1, id2 [32]byte
	id1[0] = 0x01
	id2[0] = 0x02
	rt.AddPeer(&PeerInfo{NodeID: id1, Radius: MaxRadius()})
	rt.AddPeer(&PeerInfo{NodeID: id2, Radius: MaxRadius()})

	rt.RemovePeer(id1)
	if rt.Len() != 1 {
		t.Fatalf("after remove: Len = %d, want 1", rt.Len())
	}
	if rt.GetPeer(id1) != nil {
		t.Fatal("removed peer still found")
	}
	if rt.GetPeer(id2) == nil {
		t.Fatal("remaining peer not found")
	}
}

func TestRoutingTableRemovePromotesReplacement(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Fill a bucket to capacity, then add one more (goes to replacements).
	// All peers at the same log distance (same bucket).
	var ids [BucketSize + 1][32]byte
	for i := 0; i <= BucketSize; i++ {
		// Set byte 31 to different values, keeping byte 0 the same so they're
		// in the same bucket (same leading bits differ from self=0x00).
		ids[i][0] = 0x80       // same high bit = same bucket
		ids[i][31] = byte(i + 1)
		rt.AddPeer(&PeerInfo{NodeID: ids[i], Radius: MaxRadius()})
	}

	if rt.Len() != BucketSize {
		t.Fatalf("Len = %d, want %d (bucket capacity)", rt.Len(), BucketSize)
	}

	// Remove the first entry; the replacement should be promoted.
	rt.RemovePeer(ids[0])
	if rt.Len() != BucketSize {
		t.Fatalf("after remove+promote: Len = %d, want %d", rt.Len(), BucketSize)
	}

	// The replacement (last added) should now be in the bucket.
	if rt.GetPeer(ids[BucketSize]) == nil {
		t.Fatal("replacement was not promoted")
	}
}

func TestRoutingTableClosestPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Add peers at varying distances.
	var near, mid, far [32]byte
	near[31] = 0x01 // distance = 1
	mid[31] = 0x08  // distance = 8
	far[0] = 0xFF   // distance = 2^255+
	rt.AddPeer(&PeerInfo{NodeID: near, Radius: MaxRadius()})
	rt.AddPeer(&PeerInfo{NodeID: mid, Radius: MaxRadius()})
	rt.AddPeer(&PeerInfo{NodeID: far, Radius: MaxRadius()})

	target := self // target = self, so nearest peer is 'near'
	closest := rt.ClosestPeers(target, 2)
	if len(closest) != 2 {
		t.Fatalf("ClosestPeers returned %d, want 2", len(closest))
	}
	if closest[0].NodeID != near {
		t.Fatal("closest peer should be 'near'")
	}
	if closest[1].NodeID != mid {
		t.Fatal("second closest should be 'mid'")
	}
}

func TestRoutingTableGetPeer(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id [32]byte
	id[0] = 0xAB
	rt.AddPeer(&PeerInfo{NodeID: id, Radius: MaxRadius()})

	p := rt.GetPeer(id)
	if p == nil {
		t.Fatal("GetPeer returned nil for existing peer")
	}
	if p.NodeID != id {
		t.Fatal("GetPeer returned wrong peer")
	}

	var unknown [32]byte
	unknown[0] = 0xCD
	if rt.GetPeer(unknown) != nil {
		t.Fatal("GetPeer should return nil for unknown peer")
	}
}

func TestRoutingTableBucketIndex(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Self should return -1.
	if rt.BucketIndex(self) != -1 {
		t.Fatal("BucketIndex(self) should be -1")
	}

	// Peer with only the lowest bit differing: log distance = 1, bucket = 0.
	var near [32]byte
	near[31] = 0x01
	idx := rt.BucketIndex(near)
	if idx != 0 {
		t.Fatalf("BucketIndex for distance-1 peer = %d, want 0", idx)
	}
}

// --- Content lookup with mock peers ---

func TestContentLookupFound(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Add a peer that will have the content.
	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xdead")}.Encode()
	expectedContent := []byte("block header data")

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
	if !bytes.Equal(result.Content, expectedContent) {
		t.Fatalf("content = %q, want %q", result.Content, expectedContent)
	}
	if result.Source.NodeID != peerID {
		t.Fatal("source should be the peer that had the content")
	}
}

func TestContentLookupNotFound(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xbeef")}.Encode()

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		return nil, nil, nil // no content, no closer peers
	}

	result := rt.ContentLookup(contentKey, queryFn)
	if result.Found {
		t.Fatal("content should not have been found")
	}
}

func TestContentLookupViaCloserPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Peer A knows about peer B, which has the content.
	var peerA, peerB [32]byte
	peerA[0] = 0x80
	peerB[0] = 0x01

	rt.AddPeer(&PeerInfo{NodeID: peerA, Radius: MaxRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xcafe")}.Encode()
	expectedContent := []byte("found via hop")

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		if peer.NodeID == peerA {
			// Return peerB as a closer peer.
			return nil, []*PeerInfo{{NodeID: peerB, Radius: MaxRadius()}}, nil
		}
		if peer.NodeID == peerB {
			return expectedContent, nil, nil
		}
		return nil, nil, nil
	}

	result := rt.ContentLookup(contentKey, queryFn)
	if !result.Found {
		t.Fatal("content should be found via multi-hop lookup")
	}
	if !bytes.Equal(result.Content, expectedContent) {
		t.Fatalf("content = %q, want %q", result.Content, expectedContent)
	}
}

func TestFindContentPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Peer with max radius (stores everything).
	var wideID [32]byte
	wideID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: wideID, Radius: MaxRadius()})

	// Peer with zero radius (stores nothing except self).
	var narrowID [32]byte
	narrowID[0] = 0x02
	rt.AddPeer(&PeerInfo{NodeID: narrowID, Radius: ZeroRadius()})

	contentKey := BlockHeaderKey{BlockHash: types.HexToHash("0xaaaa")}.Encode()
	peers := rt.FindContentPeers(contentKey, 10)

	// Only the wide-radius peer should be returned.
	if len(peers) != 1 {
		t.Fatalf("FindContentPeers returned %d, want 1", len(peers))
	}
	if peers[0].NodeID != wideID {
		t.Fatal("expected wide-radius peer")
	}
}

// --- History network store/retrieve ---

func TestHistoryNetworkStoreAndRetrieve(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20) // 1 MiB
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	blockHash := types.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	contentKey := BlockHeaderKey{BlockHash: blockHash}.Encode()
	data := []byte("mock header RLP data")

	if err := hn.StoreContent(contentKey, data); err != nil {
		t.Fatalf("StoreContent: %v", err)
	}

	got, err := hn.GetBlockHeader(blockHash)
	if err != nil {
		t.Fatalf("GetBlockHeader: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("retrieved = %q, want %q", got, data)
	}
}

func TestHistoryNetworkGetBlockBody(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	blockHash := types.HexToHash("0xbody")
	contentKey := BlockBodyKey{BlockHash: blockHash}.Encode()
	data := []byte("mock body data")
	hn.StoreContent(contentKey, data)

	got, err := hn.GetBlockBody(blockHash)
	if err != nil {
		t.Fatalf("GetBlockBody: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("body data mismatch")
	}
}

func TestHistoryNetworkGetReceipts(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	blockHash := types.HexToHash("0xreceipt")
	contentKey := ReceiptKey{BlockHash: blockHash}.Encode()
	data := []byte("mock receipt data")
	hn.StoreContent(contentKey, data)

	got, err := hn.GetReceipts(blockHash)
	if err != nil {
		t.Fatalf("GetReceipts: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("receipt data mismatch")
	}
}

func TestHistoryNetworkNotStarted(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	// Not started: all getters should fail.
	hash := types.HexToHash("0xaaa")
	if _, err := hn.GetBlockHeader(hash); err != ErrNetworkNotStarted {
		t.Fatalf("expected ErrNetworkNotStarted, got %v", err)
	}
	if _, err := hn.GetBlockBody(hash); err != ErrNetworkNotStarted {
		t.Fatalf("expected ErrNetworkNotStarted, got %v", err)
	}
	if _, err := hn.GetReceipts(hash); err != ErrNetworkNotStarted {
		t.Fatalf("expected ErrNetworkNotStarted, got %v", err)
	}
}

func TestHistoryNetworkNotFound(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	hash := types.HexToHash("0xnonexistent")
	if _, err := hn.GetBlockHeader(hash); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestHistoryNetworkValidateHeader(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	// Create fake header data whose keccak256 matches the content key.
	headerData := []byte("fake header rlp")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	// Validation should pass.
	if err := hn.ValidateContent(contentKey, headerData); err != nil {
		t.Fatalf("ValidateContent (matching): %v", err)
	}

	// Validation should fail with wrong data.
	wrongData := []byte("wrong data")
	if err := hn.ValidateContent(contentKey, wrongData); err != ErrHeaderMismatch {
		t.Fatalf("ValidateContent (mismatch): got %v, want ErrHeaderMismatch", err)
	}
}

func TestHistoryNetworkValidateEmptyPayload(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	hash := types.HexToHash("0xabc")
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	if err := hn.ValidateContent(contentKey, nil); err != ErrEmptyPayload {
		t.Fatalf("expected ErrEmptyPayload, got %v", err)
	}
	if err := hn.ValidateContent(contentKey, []byte{}); err != ErrEmptyPayload {
		t.Fatalf("expected ErrEmptyPayload for empty slice, got %v", err)
	}
}

func TestHistoryNetworkStoreContentErrors(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	if err := hn.StoreContent(nil, []byte("data")); err != ErrEmptyPayload {
		t.Fatalf("nil key: got %v, want ErrEmptyPayload", err)
	}
	if err := hn.StoreContent([]byte{0x00}, nil); err != ErrEmptyPayload {
		t.Fatalf("nil data: got %v, want ErrEmptyPayload", err)
	}
}

// --- EIP-4444 history expiry ---

func TestHistoryExpiry(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	// Before setting current block, nothing is expired.
	if hn.IsExpired(0) {
		t.Fatal("nothing should be expired with current=0")
	}

	// Set chain head high enough that old blocks are expired.
	// Expiry threshold = 8192 * 8192 = 67108864.
	hn.SetCurrentBlock(100_000_000)

	if !hn.IsExpired(0) {
		t.Fatal("block 0 should be expired")
	}
	if !hn.IsExpired(1_000_000) {
		t.Fatal("block 1M should be expired")
	}
	// Block just below the threshold.
	threshold := uint64(100_000_000) - uint64(HistoryExpiryEpochs*EpochSize)
	if !hn.IsExpired(threshold - 1) {
		t.Fatal("block at threshold-1 should be expired")
	}
	if hn.IsExpired(threshold) {
		t.Fatal("block at threshold should not be expired")
	}
	if hn.IsExpired(99_999_999) {
		t.Fatal("recent block should not be expired")
	}
}

// --- Radius adjustment ---

func TestRadiusUpdateFull(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Empty storage: max radius.
	rt.RadiusUpdate(0, 1000)
	r := rt.Radius()
	if r.Raw.Cmp(MaxRadius().Raw) != 0 {
		t.Fatal("empty storage should have max radius")
	}
}

func TestRadiusUpdatePartial(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Half full: radius should be ~half of max.
	rt.RadiusUpdate(500, 1000)
	r := rt.Radius()
	halfMax := new(big.Int).Div(MaxRadius().Raw, big.NewInt(2))
	if r.Raw.Cmp(halfMax) != 0 {
		t.Fatalf("half-full radius = %v, want %v", r.Raw, halfMax)
	}
}

func TestRadiusUpdateFull100(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Completely full: radius should be zero.
	rt.RadiusUpdate(1000, 1000)
	r := rt.Radius()
	if r.Raw.Sign() != 0 {
		t.Fatalf("full storage radius = %v, want 0", r.Raw)
	}
}

func TestRadiusUpdateZeroCapacity(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	rt.RadiusUpdate(0, 0)
	r := rt.Radius()
	if r.Raw.Sign() != 0 {
		t.Fatal("zero capacity should give zero radius")
	}
}

// --- MemoryStore ---

func TestMemoryStoreBasic(t *testing.T) {
	store := NewMemoryStore(1 << 20)
	id := ComputeContentID([]byte("test key"))

	// Get non-existent.
	if _, err := store.Get(id); err != ErrNotFound {
		t.Fatalf("Get non-existent: got %v, want ErrNotFound", err)
	}
	if store.Has(id) {
		t.Fatal("Has should be false for non-existent")
	}

	// Put and Get.
	data := []byte("test data")
	if err := store.Put(id, data); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !store.Has(id) {
		t.Fatal("Has should be true after Put")
	}
	got, err := store.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("Get data mismatch")
	}

	// UsedBytes.
	if store.UsedBytes() != uint64(len(data)) {
		t.Fatalf("UsedBytes = %d, want %d", store.UsedBytes(), len(data))
	}

	// Delete.
	store.Delete(id)
	if store.Has(id) {
		t.Fatal("Has should be false after Delete")
	}
	if store.UsedBytes() != 0 {
		t.Fatalf("UsedBytes after delete = %d, want 0", store.UsedBytes())
	}
}

func TestMemoryStoreUpdate(t *testing.T) {
	store := NewMemoryStore(1 << 20)
	id := ComputeContentID([]byte("key"))

	store.Put(id, []byte("aaa"))
	if store.UsedBytes() != 3 {
		t.Fatalf("used = %d, want 3", store.UsedBytes())
	}

	// Overwrite with larger data.
	store.Put(id, []byte("bbbbb"))
	if store.UsedBytes() != 5 {
		t.Fatalf("used after overwrite = %d, want 5", store.UsedBytes())
	}
}

// --- Offer/Accept ---

func TestOfferContent(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var peerID [32]byte
	peerID[0] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	keys := [][]byte{
		BlockHeaderKey{BlockHash: types.HexToHash("0x01")}.Encode(),
		BlockBodyKey{BlockHash: types.HexToHash("0x02")}.Encode(),
	}

	offerFn := func(peer *PeerInfo, contentKeys [][]byte) ([]bool, error) {
		// Accept first key, reject second.
		return []bool{true, false}, nil
	}

	results := rt.OfferContent(keys, offerFn)
	if len(results) == 0 {
		t.Fatal("expected at least one offer result")
	}
	if results[0].PeerID != peerID {
		t.Fatal("offer result peer mismatch")
	}
	if !results[0].Accepted[0] || results[0].Accepted[1] {
		t.Fatal("acceptance bitfield mismatch")
	}
}

// --- Uint16 encoding ---

func TestEncodeDecodeUint16(t *testing.T) {
	val := uint16(0xBEEF)
	encoded := EncodeUint16(val)
	decoded := DecodeUint16(encoded)
	if decoded != val {
		t.Fatalf("round-trip uint16: got 0x%04x, want 0x%04x", decoded, val)
	}
}

// --- DHT lookup with history network ---

func TestHistoryNetworkLookupContent(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	// Create content whose keccak256 matches the key (valid header).
	headerData := []byte("valid header rlp content for lookup")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	// Add a peer.
	var peerID [32]byte
	peerID[0] = 0x05
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		return headerData, nil, nil
	}

	got, err := hn.LookupContent(contentKey, queryFn)
	if err != nil {
		t.Fatalf("LookupContent: %v", err)
	}
	if !bytes.Equal(got, headerData) {
		t.Fatal("lookup content mismatch")
	}

	// Content should be cached locally now.
	cached, err := hn.GetBlockHeader(hash)
	if err != nil {
		t.Fatalf("GetBlockHeader (cached): %v", err)
	}
	if !bytes.Equal(cached, headerData) {
		t.Fatal("cached content mismatch")
	}
}

func TestHistoryNetworkLookupLocalFirst(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	hash := types.HexToHash("0xlocal")
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()
	data := []byte("already stored locally")
	hn.StoreContent(contentKey, data)

	// Query function should not be called.
	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		t.Fatal("query function should not be called for local content")
		return nil, nil, nil
	}

	got, err := hn.LookupContent(contentKey, queryFn)
	if err != nil {
		t.Fatalf("LookupContent: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("local content mismatch")
	}
}

// --- AllPeers and BucketEntries ---

func TestRoutingTableAllPeers(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	var id1, id2 [32]byte
	id1[0] = 0x10
	id2[0] = 0x20
	rt.AddPeer(&PeerInfo{NodeID: id1})
	rt.AddPeer(&PeerInfo{NodeID: id2})

	all := rt.AllPeers()
	if len(all) != 2 {
		t.Fatalf("AllPeers = %d, want 2", len(all))
	}
}

func TestRoutingTableBucketEntries(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Out of range.
	if entries := rt.BucketEntries(-1); entries != nil {
		t.Fatal("BucketEntries(-1) should be nil")
	}
	if entries := rt.BucketEntries(256); entries != nil {
		t.Fatal("BucketEntries(256) should be nil")
	}

	var id [32]byte
	id[31] = 0x01 // log distance = 1, bucket index = 0
	rt.AddPeer(&PeerInfo{NodeID: id})

	entries := rt.BucketEntries(0)
	if len(entries) != 1 {
		t.Fatalf("BucketEntries(0) = %d, want 1", len(entries))
	}
}

// --- PeersInRadius ---

func TestPeersInRadius(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)

	// Peer very close.
	var closeID [32]byte
	closeID[31] = 0x01
	rt.AddPeer(&PeerInfo{NodeID: closeID, Radius: MaxRadius()})

	// Peer very far.
	var farID [32]byte
	farID[0] = 0xFF
	rt.AddPeer(&PeerInfo{NodeID: farID, Radius: MaxRadius()})

	// Small radius: only close peer should match.
	smallRadius := NodeRadius{Raw: big.NewInt(256)}
	target := self
	inRadius := rt.PeersInRadius(target, smallRadius)
	if len(inRadius) != 1 {
		t.Fatalf("PeersInRadius (small) = %d, want 1", len(inRadius))
	}
	if inRadius[0].NodeID != closeID {
		t.Fatal("expected close peer in small radius")
	}

	// Max radius: both peers should match.
	inRadius = rt.PeersInRadius(target, MaxRadius())
	if len(inRadius) != 2 {
		t.Fatalf("PeersInRadius (max) = %d, want 2", len(inRadius))
	}
}
