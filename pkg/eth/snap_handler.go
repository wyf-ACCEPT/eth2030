// snap_handler.go implements SnapProtocolHandler, which handles Snap/1
// sync protocol messages for state synchronization. It provides account
// range, storage range, bytecode, and trie node retrieval with response
// size limiting and per-peer request throttling.
package eth

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Snap protocol errors.
var (
	ErrSnapHandlerStopped   = errors.New("snap handler: stopped")
	ErrSnapHandlerNoPeer    = errors.New("snap handler: peer not found")
	ErrSnapResponseTooLarge = errors.New("snap handler: response too large")
	ErrSnapRequestThrottled = errors.New("snap handler: request throttled")
	ErrSnapInvalidRange     = errors.New("snap handler: invalid hash range")
	ErrSnapNoData           = errors.New("snap handler: no data found")
)

// Snap protocol limits.
const (
	// MaxAccountRangeResponse is the maximum number of accounts in a
	// GetAccountRange response.
	MaxAccountRangeResponse = 4096

	// MaxStorageRangeResponse is the maximum number of storage slots
	// returned per account.
	MaxStorageRangeResponse = 4096

	// MaxByteCodesResponse is the maximum number of bytecodes returned.
	MaxByteCodesResponse = 1024

	// MaxTrieNodesResponse is the maximum number of trie nodes returned.
	MaxTrieNodesResponse = 4096

	// MaxSnapResponseBytes is the maximum response size in bytes (2 MiB).
	MaxSnapResponseBytes = 2 * 1024 * 1024
)

// SnapAccountEntry represents an account in a range response.
type SnapAccountEntry struct {
	Hash    types.Hash
	Address types.Address
	Nonce   uint64
	Balance []byte // big-endian encoded balance
	Root    types.Hash
	CodeHash types.Hash
}

// SnapStorageEntry represents a storage slot in a range response.
type SnapStorageEntry struct {
	Hash  types.Hash
	Key   types.Hash
	Value types.Hash
}

// SnapByteCodeEntry represents a contract bytecode response.
type SnapByteCodeEntry struct {
	Hash types.Hash
	Code []byte
}

// SnapTrieNodeEntry represents a trie node response.
type SnapTrieNodeEntry struct {
	Path []byte
	Data []byte
}

// AccountRangeResult is the response for GetAccountRange.
type AccountRangeResult struct {
	Accounts []SnapAccountEntry
	Proof    [][]byte // Merkle proof for range boundary
	More     bool     // true if there are more accounts in range
}

// StorageRangeResult is the response for GetStorageRanges.
type StorageRangeResult struct {
	Slots []SnapStorageEntry
	Proof [][]byte
	More  bool
}

// ByteCodesResult is the response for GetByteCodes.
type ByteCodesResult struct {
	Codes   []SnapByteCodeEntry
	Missing []types.Hash
}

// TrieNodesResult is the response for GetTrieNodes.
type TrieNodesResult struct {
	Nodes   []SnapTrieNodeEntry
	Missing int
}

// ResponseSizer tracks the size of a response being built, enforcing
// the maximum response size limit.
type ResponseSizer struct {
	currentSize int
	maxSize     int
}

// NewResponseSizer creates a sizer with the given limit.
func NewResponseSizer(maxSize int) *ResponseSizer {
	return &ResponseSizer{maxSize: maxSize}
}

// Add attempts to add bytes to the response. Returns false if the
// addition would exceed the maximum size.
func (rs *ResponseSizer) Add(size int) bool {
	if rs.currentSize+size > rs.maxSize {
		return false
	}
	rs.currentSize += size
	return true
}

// CurrentSize returns the current response size.
func (rs *ResponseSizer) CurrentSize() int {
	return rs.currentSize
}

// Remaining returns how many bytes can still be added.
func (rs *ResponseSizer) Remaining() int {
	r := rs.maxSize - rs.currentSize
	if r < 0 {
		return 0
	}
	return r
}

// Reset resets the sizer to zero.
func (rs *ResponseSizer) Reset() {
	rs.currentSize = 0
}

// RequestThrottler provides per-peer request rate limiting.
type RequestThrottler struct {
	mu          sync.Mutex
	peerWindows map[string]*throttleWindow
	maxRequests int
	window      time.Duration
}

// throttleWindow tracks request counts per peer.
type throttleWindow struct {
	count    int
	windowStart time.Time
}

// NewRequestThrottler creates a throttler allowing maxRequests per window.
func NewRequestThrottler(maxRequests int, window time.Duration) *RequestThrottler {
	return &RequestThrottler{
		peerWindows: make(map[string]*throttleWindow),
		maxRequests: maxRequests,
		window:      window,
	}
}

// Allow checks whether a request from the given peer should be allowed.
// Returns true if allowed, false if throttled.
func (rt *RequestThrottler) Allow(peerID string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	now := time.Now()
	w, ok := rt.peerWindows[peerID]
	if !ok {
		rt.peerWindows[peerID] = &throttleWindow{
			count:       1,
			windowStart: now,
		}
		return true
	}

	// Reset window if expired.
	if now.Sub(w.windowStart) >= rt.window {
		w.count = 1
		w.windowStart = now
		return true
	}

	w.count++
	return w.count <= rt.maxRequests
}

// PeerRequestCount returns the number of requests in the current window
// for a given peer.
func (rt *RequestThrottler) PeerRequestCount(peerID string) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	w, ok := rt.peerWindows[peerID]
	if !ok {
		return 0
	}
	return w.count
}

// RemovePeer removes tracking state for a disconnected peer.
func (rt *RequestThrottler) RemovePeer(peerID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.peerWindows, peerID)
}

// StateProvider provides state data for snap sync responses.
type StateProvider interface {
	GetAccountRange(root types.Hash, start, limit types.Hash, maxResults int) ([]SnapAccountEntry, bool)
	GetStorageRanges(root types.Hash, accounts []types.Address, start, limit types.Hash, maxResults int) ([]SnapStorageEntry, bool)
	GetByteCodes(hashes []types.Hash) []SnapByteCodeEntry
	GetTrieNodes(root types.Hash, paths [][]byte) []SnapTrieNodeEntry
}

// SnapProtocolHandler handles Snap/1 sync protocol messages.
type SnapProtocolHandler struct {
	mu        sync.RWMutex
	state     StateProvider
	throttler *RequestThrottler
	stopped   bool
}

// NewSnapProtocolHandler creates a new snap protocol handler.
func NewSnapProtocolHandler(state StateProvider, throttler *RequestThrottler) *SnapProtocolHandler {
	return &SnapProtocolHandler{
		state:     state,
		throttler: throttler,
	}
}

// HandleGetAccountRange processes a GetAccountRange request. Returns
// accounts within the hash range [start, limit] from the given state root.
func (h *SnapProtocolHandler) HandleGetAccountRange(peerID string, root, start, limit types.Hash, maxResults int) (*AccountRangeResult, error) {
	if h.isStopped() {
		return nil, ErrSnapHandlerStopped
	}
	if h.throttler != nil && !h.throttler.Allow(peerID) {
		return nil, ErrSnapRequestThrottled
	}

	if maxResults > MaxAccountRangeResponse {
		maxResults = MaxAccountRangeResponse
	}
	if maxResults <= 0 {
		maxResults = MaxAccountRangeResponse
	}

	if h.state == nil {
		return &AccountRangeResult{}, nil
	}

	accounts, more := h.state.GetAccountRange(root, start, limit, maxResults)
	return &AccountRangeResult{
		Accounts: accounts,
		More:     more,
	}, nil
}

// HandleGetStorageRanges processes a GetStorageRanges request. Returns
// storage slots for the given accounts within the hash range.
func (h *SnapProtocolHandler) HandleGetStorageRanges(peerID string, root types.Hash, accounts []types.Address, start, limit types.Hash, maxResults int) (*StorageRangeResult, error) {
	if h.isStopped() {
		return nil, ErrSnapHandlerStopped
	}
	if h.throttler != nil && !h.throttler.Allow(peerID) {
		return nil, ErrSnapRequestThrottled
	}

	if maxResults > MaxStorageRangeResponse {
		maxResults = MaxStorageRangeResponse
	}
	if maxResults <= 0 {
		maxResults = MaxStorageRangeResponse
	}

	if h.state == nil {
		return &StorageRangeResult{}, nil
	}

	slots, more := h.state.GetStorageRanges(root, accounts, start, limit, maxResults)
	return &StorageRangeResult{
		Slots: slots,
		More:  more,
	}, nil
}

// HandleGetByteCodes processes a GetByteCodes request. Returns the
// bytecodes for the requested code hashes.
func (h *SnapProtocolHandler) HandleGetByteCodes(peerID string, hashes []types.Hash) (*ByteCodesResult, error) {
	if h.isStopped() {
		return nil, ErrSnapHandlerStopped
	}
	if h.throttler != nil && !h.throttler.Allow(peerID) {
		return nil, ErrSnapRequestThrottled
	}

	if len(hashes) > MaxByteCodesResponse {
		hashes = hashes[:MaxByteCodesResponse]
	}

	result := &ByteCodesResult{}
	if h.state == nil {
		result.Missing = hashes
		return result, nil
	}

	sizer := NewResponseSizer(MaxSnapResponseBytes)
	codes := h.state.GetByteCodes(hashes)
	receivedMap := make(map[types.Hash]bool, len(codes))

	for _, code := range codes {
		if !sizer.Add(len(code.Code)) {
			break // response size limit reached
		}
		result.Codes = append(result.Codes, code)
		receivedMap[code.Hash] = true
	}

	for _, hash := range hashes {
		if !receivedMap[hash] {
			result.Missing = append(result.Missing, hash)
		}
	}
	return result, nil
}

// HandleGetTrieNodes processes a GetTrieNodes request. Returns the trie
// nodes for the given paths.
func (h *SnapProtocolHandler) HandleGetTrieNodes(peerID string, root types.Hash, paths [][]byte) (*TrieNodesResult, error) {
	if h.isStopped() {
		return nil, ErrSnapHandlerStopped
	}
	if h.throttler != nil && !h.throttler.Allow(peerID) {
		return nil, ErrSnapRequestThrottled
	}

	if len(paths) > MaxTrieNodesResponse {
		paths = paths[:MaxTrieNodesResponse]
	}

	result := &TrieNodesResult{}
	if h.state == nil {
		result.Missing = len(paths)
		return result, nil
	}

	sizer := NewResponseSizer(MaxSnapResponseBytes)
	nodes := h.state.GetTrieNodes(root, paths)

	for _, node := range nodes {
		if !sizer.Add(len(node.Data)) {
			break
		}
		result.Nodes = append(result.Nodes, node)
	}
	result.Missing = len(paths) - len(result.Nodes)
	return result, nil
}

// Stop marks the handler as stopped.
func (h *SnapProtocolHandler) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
}

func (h *SnapProtocolHandler) isStopped() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stopped
}
