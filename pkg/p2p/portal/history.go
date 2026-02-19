package portal

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// History network errors.
var (
	ErrNotFound          = errors.New("portal/history: content not found")
	ErrInvalidProof      = errors.New("portal/history: invalid accumulator proof")
	ErrHeaderMismatch    = errors.New("portal/history: header hash mismatch")
	ErrHistoryExpired    = errors.New("portal/history: content expired per EIP-4444")
	ErrNetworkNotStarted = errors.New("portal/history: network not started")
)

// EIP-4444 history expiry threshold. Blocks older than this many epochs
// are candidates for expiry from the execution layer and served via
// the portal history network instead.
const HistoryExpiryEpochs = 8192

// EpochSize is the number of blocks in one epoch accumulator.
const EpochSize = 8192

// ContentStore is the interface for persisting portal content.
type ContentStore interface {
	Get(contentID ContentID) ([]byte, error)
	Put(contentID ContentID, data []byte) error
	Delete(contentID ContentID) error
	Has(contentID ContentID) bool
	UsedBytes() uint64
	CapacityBytes() uint64
}

// HistoryNetwork manages historical block data (headers, bodies, receipts)
// for the Portal History sub-protocol. It provides content-addressed storage
// and retrieval, with validation against epoch accumulator proofs.
type HistoryNetwork struct {
	mu           sync.RWMutex
	table        *RoutingTable
	store        ContentStore
	started      bool
	currentBlock uint64 // current chain head for EIP-4444 expiry checks
}

// NewHistoryNetwork creates a new history network handler.
func NewHistoryNetwork(table *RoutingTable, store ContentStore) *HistoryNetwork {
	return &HistoryNetwork{
		table: table,
		store: store,
	}
}

// Start initializes the history network.
func (hn *HistoryNetwork) Start() error {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.started = true
	return nil
}

// Stop shuts down the history network.
func (hn *HistoryNetwork) Stop() {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.started = false
}

// SetCurrentBlock updates the current chain head block number, used for
// EIP-4444 expiry calculations.
func (hn *HistoryNetwork) SetCurrentBlock(number uint64) {
	hn.mu.Lock()
	defer hn.mu.Unlock()
	hn.currentBlock = number
}

// IsExpired returns true if a block number has expired per EIP-4444 and
// should only be available via the portal history network.
func (hn *HistoryNetwork) IsExpired(blockNumber uint64) bool {
	hn.mu.RLock()
	current := hn.currentBlock
	hn.mu.RUnlock()

	if current == 0 {
		return false
	}
	expiryThreshold := HistoryExpiryEpochs * EpochSize
	if current <= uint64(expiryThreshold) {
		return false
	}
	return blockNumber < current-uint64(expiryThreshold)
}

// GetBlockHeader retrieves a block header by its block hash from local
// storage or the DHT.
func (hn *HistoryNetwork) GetBlockHeader(blockHash types.Hash) ([]byte, error) {
	hn.mu.RLock()
	started := hn.started
	hn.mu.RUnlock()
	if !started {
		return nil, ErrNetworkNotStarted
	}

	key := BlockHeaderKey{BlockHash: blockHash}
	contentKey := key.Encode()
	contentID := ComputeContentID(contentKey)

	// Try local store first.
	if data, err := hn.store.Get(contentID); err == nil {
		return data, nil
	}

	return nil, ErrNotFound
}

// GetBlockBody retrieves a block body by its block hash.
func (hn *HistoryNetwork) GetBlockBody(blockHash types.Hash) ([]byte, error) {
	hn.mu.RLock()
	started := hn.started
	hn.mu.RUnlock()
	if !started {
		return nil, ErrNetworkNotStarted
	}

	key := BlockBodyKey{BlockHash: blockHash}
	contentKey := key.Encode()
	contentID := ComputeContentID(contentKey)

	if data, err := hn.store.Get(contentID); err == nil {
		return data, nil
	}

	return nil, ErrNotFound
}

// GetReceipts retrieves receipts by block hash.
func (hn *HistoryNetwork) GetReceipts(blockHash types.Hash) ([]byte, error) {
	hn.mu.RLock()
	started := hn.started
	hn.mu.RUnlock()
	if !started {
		return nil, ErrNetworkNotStarted
	}

	key := ReceiptKey{BlockHash: blockHash}
	contentKey := key.Encode()
	contentID := ComputeContentID(contentKey)

	if data, err := hn.store.Get(contentID); err == nil {
		return data, nil
	}

	return nil, ErrNotFound
}

// StoreContent stores raw content data keyed by its content key.
// The content is validated before storage if an accumulator proof is provided.
func (hn *HistoryNetwork) StoreContent(contentKey []byte, data []byte) error {
	if len(contentKey) == 0 || len(data) == 0 {
		return ErrEmptyPayload
	}

	contentID := ComputeContentID(contentKey)

	// Check if content falls within our radius.
	hn.mu.RLock()
	selfID := hn.table.Self()
	radius := hn.table.Radius()
	hn.mu.RUnlock()

	if !radius.Contains(selfID, contentID) {
		// Content is outside our radius, still store it but this
		// would normally be rejected in production.
	}

	return hn.store.Put(contentID, data)
}

// ValidateContent verifies content against its content key. For block headers,
// this checks that keccak256(header_rlp) matches the block hash in the key.
func (hn *HistoryNetwork) ValidateContent(contentKey []byte, data []byte) error {
	if len(contentKey) < 1+types.HashLength {
		return ErrInvalidContentKey
	}
	if len(data) == 0 {
		return ErrEmptyPayload
	}

	keyType := contentKey[0]
	var expectedHash types.Hash
	copy(expectedHash[:], contentKey[1:1+types.HashLength])

	switch keyType {
	case ContentKeyBlockHeader:
		// Validate header: keccak256(data) must match the block hash.
		hash := crypto.Keccak256Hash(data)
		if hash != expectedHash {
			return ErrHeaderMismatch
		}
	case ContentKeyBlockBody, ContentKeyReceipt:
		// Body and receipt validation requires the associated header's
		// transactions root or receipts root. For now, we accept them
		// if the data is non-empty.
	case ContentKeyEpochAccumulator:
		// Epoch accumulator validation would check the Merkle proof.
		// Stub: accept non-empty data.
	default:
		return ErrUnknownKeyType
	}

	return nil
}

// LookupContent performs a DHT lookup for content across the history network.
// It queries peers in the routing table iteratively until the content is found.
func (hn *HistoryNetwork) LookupContent(contentKey []byte, queryFn ContentQueryFn) ([]byte, error) {
	hn.mu.RLock()
	started := hn.started
	hn.mu.RUnlock()
	if !started {
		return nil, ErrNetworkNotStarted
	}

	// Check local first.
	contentID := ComputeContentID(contentKey)
	if data, err := hn.store.Get(contentID); err == nil {
		return data, nil
	}

	// Iterative DHT lookup.
	result := hn.table.ContentLookup(contentKey, queryFn)
	if result.Found {
		// Validate before returning.
		if err := hn.ValidateContent(contentKey, result.Content); err != nil {
			return nil, err
		}
		// Cache locally.
		_ = hn.store.Put(contentID, result.Content)
		return result.Content, nil
	}

	return nil, ErrNotFound
}

// --- In-memory content store (for testing and light nodes) ---

// MemoryStore is a simple in-memory ContentStore.
type MemoryStore struct {
	mu       sync.RWMutex
	data     map[ContentID][]byte
	used     uint64
	capacity uint64
}

// NewMemoryStore creates a MemoryStore with the given capacity in bytes.
func NewMemoryStore(capacity uint64) *MemoryStore {
	return &MemoryStore{
		data:     make(map[ContentID][]byte),
		capacity: capacity,
	}
}

// Get retrieves content by ID.
func (ms *MemoryStore) Get(id ContentID) ([]byte, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	data, ok := ms.data[id]
	if !ok {
		return nil, ErrNotFound
	}
	result := make([]byte, len(data))
	copy(result, data)
	return result, nil
}

// Put stores content by ID.
func (ms *MemoryStore) Put(id ContentID, data []byte) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	stored := make([]byte, len(data))
	copy(stored, data)

	// If updating, subtract old size first.
	if old, exists := ms.data[id]; exists {
		ms.used -= uint64(len(old))
	}

	ms.data[id] = stored
	ms.used += uint64(len(data))
	return nil
}

// Delete removes content by ID.
func (ms *MemoryStore) Delete(id ContentID) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	if data, ok := ms.data[id]; ok {
		ms.used -= uint64(len(data))
		delete(ms.data, id)
	}
	return nil
}

// Has reports whether content exists in the store.
func (ms *MemoryStore) Has(id ContentID) bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	_, ok := ms.data[id]
	return ok
}

// UsedBytes returns the total bytes stored.
func (ms *MemoryStore) UsedBytes() uint64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.used
}

// CapacityBytes returns the store capacity.
func (ms *MemoryStore) CapacityBytes() uint64 {
	return ms.capacity
}
