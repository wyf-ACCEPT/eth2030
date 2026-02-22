// blob_sync.go implements a blob sync protocol handler that manages
// downloading and verifying blobs during snap sync. It tracks pending
// blob requests, verified blobs, and per-peer download statistics.
package sync

import (
	"errors"
	"fmt"
	"sort"
	gosync "sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Blob sync errors.
var (
	ErrBlobSyncSlotAlreadyComplete = errors.New("blob sync: slot already marked complete")
	ErrBlobSyncDuplicateBlob       = errors.New("blob sync: duplicate blob for slot/index")
	ErrBlobSyncSlotNotFound        = errors.New("blob sync: slot not found")
	ErrBlobSyncInvalidIndex        = errors.New("blob sync: blob index not in requested set")
	ErrBlobSyncEmptyBlob           = errors.New("blob sync: empty blob data")
	ErrBlobSyncNoIndices           = errors.New("blob sync: no indices specified")
	ErrBlobSyncInconsistent        = errors.New("blob sync: blobs are inconsistent")
)

// BlobSyncConfig holds configuration for the blob sync manager.
type BlobSyncConfig struct {
	MaxPendingBlobs int // max number of pending blob requests
	BlobTimeout     int // timeout in seconds for blob requests
	RetryLimit      int // max retries per blob request
	PeerTimeout     int // timeout in seconds for peer responses
}

// DefaultBlobSyncConfig returns sensible defaults for blob sync.
func DefaultBlobSyncConfig() BlobSyncConfig {
	return BlobSyncConfig{
		MaxPendingBlobs: 128,
		BlobTimeout:     30,
		RetryLimit:      3,
		PeerTimeout:     15,
	}
}

// blobSlotState tracks the state of blob requests for a single slot.
type blobSlotState struct {
	requestedIndices map[uint64]bool   // indices that were requested
	blobs            map[uint64][]byte // index -> blob data
	verified         bool
	complete         bool
	peerID           string // peer assigned to this slot
}

// BlobSyncManager manages downloading and verifying blobs during snap sync.
// It is safe for concurrent use.
type BlobSyncManager struct {
	mu        gosync.RWMutex
	config    BlobSyncConfig
	slots     map[uint64]*blobSlotState // slot -> state
	peerStats map[string]int            // peerID -> download count
}

// NewBlobSyncManager creates a new BlobSyncManager with the given config.
func NewBlobSyncManager(config BlobSyncConfig) *BlobSyncManager {
	if config.MaxPendingBlobs <= 0 {
		config.MaxPendingBlobs = 128
	}
	if config.BlobTimeout <= 0 {
		config.BlobTimeout = 30
	}
	if config.RetryLimit <= 0 {
		config.RetryLimit = 3
	}
	if config.PeerTimeout <= 0 {
		config.PeerTimeout = 15
	}
	return &BlobSyncManager{
		config:    config,
		slots:     make(map[uint64]*blobSlotState),
		peerStats: make(map[string]int),
	}
}

// RequestBlobs queues blob requests for the given slot and indices.
// Returns an error if the slot is already complete or no indices are given.
func (m *BlobSyncManager) RequestBlobs(slot uint64, indices []uint64) error {
	if len(indices) == 0 {
		return ErrBlobSyncNoIndices
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if state, ok := m.slots[slot]; ok && state.complete {
		return ErrBlobSyncSlotAlreadyComplete
	}

	state, ok := m.slots[slot]
	if !ok {
		state = &blobSlotState{
			requestedIndices: make(map[uint64]bool),
			blobs:            make(map[uint64][]byte),
		}
		m.slots[slot] = state
	}

	for _, idx := range indices {
		state.requestedIndices[idx] = true
	}

	return nil
}

// ProcessBlobResponse handles an incoming blob for the given slot and index.
// It validates the response and stores the blob data. The optional peerID
// is used for per-peer download statistics.
func (m *BlobSyncManager) ProcessBlobResponse(slot uint64, index uint64, blob []byte) error {
	return m.ProcessBlobResponseFromPeer(slot, index, blob, "")
}

// ProcessBlobResponseFromPeer handles an incoming blob and tracks the peer
// that provided it.
func (m *BlobSyncManager) ProcessBlobResponseFromPeer(slot uint64, index uint64, blob []byte, peerID string) error {
	if len(blob) == 0 {
		return ErrBlobSyncEmptyBlob
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.slots[slot]
	if !ok {
		return ErrBlobSyncSlotNotFound
	}
	if state.complete {
		return ErrBlobSyncSlotAlreadyComplete
	}
	if !state.requestedIndices[index] {
		return fmt.Errorf("%w: index %d at slot %d", ErrBlobSyncInvalidIndex, index, slot)
	}
	if _, exists := state.blobs[index]; exists {
		return fmt.Errorf("%w: index %d at slot %d", ErrBlobSyncDuplicateBlob, index, slot)
	}

	// Store the blob data (copy to avoid aliasing).
	stored := make([]byte, len(blob))
	copy(stored, blob)
	state.blobs[index] = stored

	if peerID != "" {
		state.peerID = peerID
		m.peerStats[peerID]++
	}

	return nil
}

// VerifyBlobConsistency checks whether all requested blobs for a slot have
// been received and verifies their consistency using Keccak256 hashes.
// Returns true if all blobs are present and consistent, false otherwise.
func (m *BlobSyncManager) VerifyBlobConsistency(slot uint64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.slots[slot]
	if !ok {
		return false, ErrBlobSyncSlotNotFound
	}

	// Check that all requested indices have blobs.
	for idx := range state.requestedIndices {
		if _, exists := state.blobs[idx]; !exists {
			return false, nil
		}
	}

	// Verify consistency: compute a content hash for each blob and ensure
	// no two blobs share the same hash (they should be distinct data).
	hashes := make(map[types.Hash]uint64)
	for idx, blob := range state.blobs {
		h := crypto.Keccak256Hash(blob)
		if prevIdx, dup := hashes[h]; dup {
			return false, fmt.Errorf("%w: indices %d and %d have identical content",
				ErrBlobSyncInconsistent, prevIdx, idx)
		}
		hashes[h] = idx
	}

	state.verified = true
	return true, nil
}

// GetPendingSlots returns a sorted list of slots that still have pending
// (incomplete) blob requests.
func (m *BlobSyncManager) GetPendingSlots() []uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []uint64
	for slot, state := range m.slots {
		if !state.complete {
			pending = append(pending, slot)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i] < pending[j] })
	return pending
}

// GetVerifiedBlobs returns the verified blobs for a slot, ordered by index.
// Returns nil if the slot is not found or not verified.
func (m *BlobSyncManager) GetVerifiedBlobs(slot uint64) [][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.slots[slot]
	if !ok || !state.verified {
		return nil
	}

	// Collect and sort indices for deterministic ordering.
	indices := make([]uint64, 0, len(state.blobs))
	for idx := range state.blobs {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })

	blobs := make([][]byte, len(indices))
	for i, idx := range indices {
		b := state.blobs[idx]
		out := make([]byte, len(b))
		copy(out, b)
		blobs[i] = out
	}
	return blobs
}

// MarkSlotComplete marks a slot as fully synced. Once complete, no more
// blobs can be added to it.
func (m *BlobSyncManager) MarkSlotComplete(slot uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.slots[slot]
	if !ok {
		return
	}
	state.complete = true
}

// PeerStats returns a copy of the per-peer download statistics.
func (m *BlobSyncManager) PeerStats() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]int, len(m.peerStats))
	for k, v := range m.peerStats {
		stats[k] = v
	}
	return stats
}

// SlotBlobCount returns the number of blobs received for a slot.
// Returns 0 if the slot is not found.
func (m *BlobSyncManager) SlotBlobCount(slot uint64) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.slots[slot]
	if !ok {
		return 0
	}
	return len(state.blobs)
}

// IsSlotComplete returns whether a slot has been marked complete.
func (m *BlobSyncManager) IsSlotComplete(slot uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.slots[slot]
	if !ok {
		return false
	}
	return state.complete
}

// IsSlotVerified returns whether a slot's blobs have been verified.
func (m *BlobSyncManager) IsSlotVerified(slot uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.slots[slot]
	if !ok {
		return false
	}
	return state.verified
}
