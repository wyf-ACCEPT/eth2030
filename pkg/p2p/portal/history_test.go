package portal

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- History network creation ---

func TestHistory_NewHistoryNetwork(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	if hn == nil {
		t.Fatal("NewHistoryNetwork returned nil")
	}
}

// --- Start/stop ---

func TestHistory_StartAndStop(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	if err := hn.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	hn.Stop()
	// Double stop should not panic.
	hn.Stop()
}

// --- Not-started errors ---

func TestHistory_NotStartedGetBlockHeader(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	hash := types.HexToHash("0x1234")
	_, err := hn.GetBlockHeader(hash)
	if err != ErrNetworkNotStarted {
		t.Fatalf("want ErrNetworkNotStarted, got %v", err)
	}
}

func TestHistory_NotStartedGetBlockBody(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	_, err := hn.GetBlockBody(types.HexToHash("0xaa"))
	if err != ErrNetworkNotStarted {
		t.Fatalf("want ErrNetworkNotStarted, got %v", err)
	}
}

func TestHistory_NotStartedGetReceipts(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	_, err := hn.GetReceipts(types.HexToHash("0xbb"))
	if err != ErrNetworkNotStarted {
		t.Fatalf("want ErrNetworkNotStarted, got %v", err)
	}
}

// --- Store and retrieve ---

func TestHistory_StoreAndRetrieveHeader(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	blockHash := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	contentKey := BlockHeaderKey{BlockHash: blockHash}.Encode()
	data := []byte("mock header data for history_test")

	if err := hn.StoreContent(contentKey, data); err != nil {
		t.Fatalf("StoreContent: %v", err)
	}

	got, err := hn.GetBlockHeader(blockHash)
	if err != nil {
		t.Fatalf("GetBlockHeader: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("retrieved header data mismatch")
	}
}

func TestHistory_StoreAndRetrieveBody(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	blockHash := types.HexToHash("0xbody123")
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

func TestHistory_StoreAndRetrieveReceipts(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	blockHash := types.HexToHash("0xreceipt456")
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

// --- Not found ---

func TestHistory_GetNotFound(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	_, err := hn.GetBlockHeader(types.HexToHash("0xnonexistent"))
	if err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// --- StoreContent errors ---

func TestHistory_StoreContentEmptyKey(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	if err := hn.StoreContent(nil, []byte("data")); err != ErrEmptyPayload {
		t.Fatalf("nil key: want ErrEmptyPayload, got %v", err)
	}
}

func TestHistory_StoreContentEmptyData(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	if err := hn.StoreContent([]byte{0x00}, nil); err != ErrEmptyPayload {
		t.Fatalf("nil data: want ErrEmptyPayload, got %v", err)
	}
}

// --- Validation ---

func TestHistory_ValidateHeaderCorrect(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	headerData := []byte("some header rlp bytes")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	if err := hn.ValidateContent(contentKey, headerData); err != nil {
		t.Fatalf("ValidateContent (matching): %v", err)
	}
}

func TestHistory_ValidateHeaderMismatch(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	headerData := []byte("correct header data")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	wrongData := []byte("wrong header data")
	if err := hn.ValidateContent(contentKey, wrongData); err != ErrHeaderMismatch {
		t.Fatalf("want ErrHeaderMismatch, got %v", err)
	}
}

func TestHistory_ValidateEmptyPayload(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	hash := types.HexToHash("0xabc")
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	if err := hn.ValidateContent(contentKey, nil); err != ErrEmptyPayload {
		t.Fatalf("want ErrEmptyPayload, got %v", err)
	}
	if err := hn.ValidateContent(contentKey, []byte{}); err != ErrEmptyPayload {
		t.Fatalf("want ErrEmptyPayload for empty slice, got %v", err)
	}
}

// --- EIP-4444 expiry ---

func TestHistory_ExpiryConstants(t *testing.T) {
	if HistoryExpiryEpochs != 8192 {
		t.Fatalf("HistoryExpiryEpochs: want 8192, got %d", HistoryExpiryEpochs)
	}
	if EpochSize != 8192 {
		t.Fatalf("EpochSize: want 8192, got %d", EpochSize)
	}
}

func TestHistory_IsExpiredWithZeroCurrent(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	// current=0: nothing is expired.
	if hn.IsExpired(0) {
		t.Fatal("nothing should be expired with current=0")
	}
	if hn.IsExpired(100) {
		t.Fatal("nothing should be expired with current=0")
	}
}

func TestHistory_IsExpiredCalculation(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	hn.SetCurrentBlock(100_000_000)
	threshold := uint64(100_000_000) - uint64(HistoryExpiryEpochs*EpochSize)

	if !hn.IsExpired(0) {
		t.Fatal("block 0 should be expired")
	}
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

// --- LookupContent local first ---

func TestHistory_LookupContentLocalFirst(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	hash := types.HexToHash("0xlocal123")
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()
	data := []byte("already stored locally")
	hn.StoreContent(contentKey, data)

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

// --- LookupContent from DHT ---

func TestHistory_LookupContentDHT(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)
	hn.Start()
	defer hn.Stop()

	headerData := []byte("valid header for DHT lookup test")
	hash := crypto.Keccak256Hash(headerData)
	contentKey := BlockHeaderKey{BlockHash: hash}.Encode()

	var peerID [32]byte
	peerID[0] = 0x07
	rt.AddPeer(&PeerInfo{NodeID: peerID, Radius: MaxRadius()})

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		return headerData, nil, nil
	}

	got, err := hn.LookupContent(contentKey, queryFn)
	if err != nil {
		t.Fatalf("LookupContent: %v", err)
	}
	if !bytes.Equal(got, headerData) {
		t.Fatal("DHT content mismatch")
	}

	// Content should be cached locally.
	cached, err := hn.GetBlockHeader(hash)
	if err != nil {
		t.Fatalf("GetBlockHeader (cached): %v", err)
	}
	if !bytes.Equal(cached, headerData) {
		t.Fatal("cached content mismatch")
	}
}

// --- LookupContent not started ---

func TestHistory_LookupContentNotStarted(t *testing.T) {
	var self [32]byte
	rt := NewRoutingTable(self)
	store := NewMemoryStore(1 << 20)
	hn := NewHistoryNetwork(rt, store)

	queryFn := func(peer *PeerInfo, key []byte) ([]byte, []*PeerInfo, error) {
		return nil, nil, nil
	}

	_, err := hn.LookupContent([]byte{0x00}, queryFn)
	if err != ErrNetworkNotStarted {
		t.Fatalf("want ErrNetworkNotStarted, got %v", err)
	}
}

// --- MemoryStore operations ---

func TestHistory_MemoryStoreGetPutDelete(t *testing.T) {
	store := NewMemoryStore(1 << 20)
	id := ComputeContentID([]byte("history test key"))

	// Not found.
	if _, err := store.Get(id); err != ErrNotFound {
		t.Fatalf("Get non-existent: want ErrNotFound, got %v", err)
	}
	if store.Has(id) {
		t.Fatal("Has should be false for non-existent")
	}

	// Put and Get.
	data := []byte("history test data")
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
		t.Fatalf("UsedBytes: want %d, got %d", len(data), store.UsedBytes())
	}

	// Delete.
	store.Delete(id)
	if store.Has(id) {
		t.Fatal("Has should be false after Delete")
	}
	if store.UsedBytes() != 0 {
		t.Fatalf("UsedBytes after delete: want 0, got %d", store.UsedBytes())
	}
}

func TestHistory_MemoryStoreOverwrite(t *testing.T) {
	store := NewMemoryStore(1 << 20)
	id := ComputeContentID([]byte("overwrite key"))

	store.Put(id, []byte("abc"))
	if store.UsedBytes() != 3 {
		t.Fatalf("used: want 3, got %d", store.UsedBytes())
	}

	store.Put(id, []byte("defgh"))
	if store.UsedBytes() != 5 {
		t.Fatalf("used after overwrite: want 5, got %d", store.UsedBytes())
	}
}

func TestHistory_MemoryStoreCapacity(t *testing.T) {
	store := NewMemoryStore(1024)
	if store.CapacityBytes() != 1024 {
		t.Fatalf("capacity: want 1024, got %d", store.CapacityBytes())
	}
}
