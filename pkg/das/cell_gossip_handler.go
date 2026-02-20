// Package das - cell_gossip_handler.go implements a cell-level gossip protocol
// handler for PeerDAS data availability sampling. It manages receiving,
// validating, storing, and broadcasting individual cell messages, tracks
// reconstruction readiness per blob, and provides a pluggable validation
// interface for cell integrity checks.
//
// The handler sits between the network gossip layer and the reconstruction
// engine: it collects cells from gossip, validates them, tracks which cells
// are still missing, and signals when enough cells are available for
// Reed-Solomon reconstruction.
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/crypto"
)

// Cell gossip handler errors.
var (
	ErrGossipHandlerClosed     = errors.New("das/gossip: handler is closed")
	ErrGossipCellDuplicate     = errors.New("das/gossip: duplicate cell received")
	ErrGossipCellValidation    = errors.New("das/gossip: cell validation failed")
	ErrGossipCellNilMessage    = errors.New("das/gossip: nil cell message")
	ErrGossipBlobNotTracked    = errors.New("das/gossip: blob index not tracked")
	ErrGossipBroadcastNoPeers  = errors.New("das/gossip: no peers available for broadcast")
)

// CellGossipMessage is an extended cell message carrying a KZG proof and slot
// information for gossip-level handling.
type CellGossipMessage struct {
	// BlobIndex identifies which blob in the block this cell belongs to.
	BlobIndex int

	// CellIndex identifies the column position of this cell (0..CellsPerExtBlob-1).
	CellIndex int

	// Data is the raw cell data (up to BytesPerCell bytes).
	Data []byte

	// KZGProof is the 48-byte KZG proof for this cell.
	KZGProof [48]byte

	// Slot is the slot number this cell belongs to.
	Slot uint64
}

// CellValidator is an interface for validating cells received via gossip.
// Implementations can perform KZG proof verification, size checks, hash
// verification, or any other integrity checks.
type CellValidator interface {
	// ValidateCell returns true if the cell message passes validation.
	// The implementation should check data integrity, proof validity,
	// and index bounds as needed.
	ValidateCell(msg CellGossipMessage) bool
}

// SimpleCellValidator implements basic validation for cell gossip messages:
// size checks, index bounds, and hash verification of the cell data.
type SimpleCellValidator struct {
	// MaxBlobIndex is the maximum valid blob index (exclusive).
	MaxBlobIndex int

	// MaxCellIndex is the maximum valid cell index (exclusive).
	MaxCellIndex int

	// MaxDataSize is the maximum cell data size in bytes.
	MaxDataSize int

	// ExpectedHashes optionally maps (blobIndex, cellIndex) -> expected hash.
	// If set, the cell data hash is verified against the expected value.
	// If nil, hash verification is skipped.
	ExpectedHashes map[cellKey][32]byte
}

// cellKey is a compound key for (blobIndex, cellIndex) lookups.
type cellKey struct {
	blob int
	cell int
}

// NewSimpleCellValidator creates a validator with PeerDAS default parameters.
func NewSimpleCellValidator() *SimpleCellValidator {
	return &SimpleCellValidator{
		MaxBlobIndex: MaxBlobCommitmentsPerBlock,
		MaxCellIndex: CellsPerExtBlob,
		MaxDataSize:  BytesPerCell,
	}
}

// ValidateCell performs basic structural and integrity checks on a cell message.
func (v *SimpleCellValidator) ValidateCell(msg CellGossipMessage) bool {
	// Index bounds.
	if msg.BlobIndex < 0 || msg.BlobIndex >= v.MaxBlobIndex {
		return false
	}
	if msg.CellIndex < 0 || msg.CellIndex >= v.MaxCellIndex {
		return false
	}

	// Data size check.
	if len(msg.Data) == 0 || len(msg.Data) > v.MaxDataSize {
		return false
	}

	// Hash verification (if expected hashes are provided).
	if v.ExpectedHashes != nil {
		key := cellKey{blob: msg.BlobIndex, cell: msg.CellIndex}
		if expectedHash, ok := v.ExpectedHashes[key]; ok {
			actualHash := crypto.Keccak256Hash(msg.Data)
			if actualHash != expectedHash {
				return false
			}
		}
	}

	return true
}

// CellGossipCallback is called when a gossip event occurs.
type CellGossipCallback func(blobIndex int, event string)

// blobCellState tracks the cells received for a single blob.
type blobCellState struct {
	cells       map[int]CellGossipMessage // cell index -> message
	ready       bool                       // true when reconstruction threshold met
	reconstructed bool                     // true when blob has been reconstructed
}

// CellGossipHandler manages cell-level gossip: receiving, validating, storing,
// and tracking cells for reconstruction readiness. Thread-safe.
type CellGossipHandler struct {
	mu sync.Mutex

	// validator performs cell validation on receipt.
	validator CellValidator

	// pendingCells tracks cells per blob.
	pendingCells map[int]*blobCellState

	// reconstructionThreshold is the minimum cells needed per blob.
	reconstructionThreshold int

	// maxBlobIndex is the maximum valid blob index.
	maxBlobIndex int

	// maxCellIndex is the maximum valid cell index.
	maxCellIndex int

	// callbacks is notified on gossip events.
	callbacks []CellGossipCallback

	// broadcastQueue holds cells that should be forwarded to peers.
	broadcastQueue []CellGossipMessage

	// maxBroadcastQueue limits the broadcast queue size.
	maxBroadcastQueue int

	// closed prevents further operations after shutdown.
	closed bool

	// stats tracks gossip statistics.
	stats GossipHandlerStats
}

// GossipHandlerStats holds gossip handler statistics.
type GossipHandlerStats struct {
	CellsReceived    int64
	CellsValidated   int64
	CellsRejected    int64
	CellsDuplicate   int64
	CellsBroadcast   int64
	BlobsReady       int64
}

// CellGossipHandlerConfig configures the gossip handler.
type CellGossipHandlerConfig struct {
	// Validator is the cell validation implementation.
	Validator CellValidator

	// ReconstructionThreshold is the minimum cells needed per blob.
	// Defaults to ReconstructionThreshold (64) if zero.
	ReconstructionThreshold int

	// MaxBlobIndex is the maximum blob index. Defaults to MaxBlobCommitmentsPerBlock.
	MaxBlobIndex int

	// MaxCellIndex is the maximum cell index. Defaults to CellsPerExtBlob.
	MaxCellIndex int

	// MaxBroadcastQueue limits the outgoing broadcast queue. Defaults to 1024.
	MaxBroadcastQueue int
}

// NewCellGossipHandler creates a new cell gossip handler.
func NewCellGossipHandler(cfg CellGossipHandlerConfig) *CellGossipHandler {
	if cfg.Validator == nil {
		cfg.Validator = NewSimpleCellValidator()
	}
	if cfg.ReconstructionThreshold <= 0 {
		cfg.ReconstructionThreshold = ReconstructionThreshold
	}
	if cfg.MaxBlobIndex <= 0 {
		cfg.MaxBlobIndex = MaxBlobCommitmentsPerBlock
	}
	if cfg.MaxCellIndex <= 0 {
		cfg.MaxCellIndex = CellsPerExtBlob
	}
	if cfg.MaxBroadcastQueue <= 0 {
		cfg.MaxBroadcastQueue = 1024
	}

	return &CellGossipHandler{
		validator:               cfg.Validator,
		pendingCells:            make(map[int]*blobCellState),
		reconstructionThreshold: cfg.ReconstructionThreshold,
		maxBlobIndex:            cfg.MaxBlobIndex,
		maxCellIndex:            cfg.MaxCellIndex,
		maxBroadcastQueue:       cfg.MaxBroadcastQueue,
	}
}

// OnCellReceived processes a cell received from gossip. It validates the cell,
// stores it if new, and checks whether the blob is ready for reconstruction.
// Returns nil on success, or an error if the cell is invalid or duplicate.
func (h *CellGossipHandler) OnCellReceived(msg CellGossipMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrGossipHandlerClosed
	}

	h.stats.CellsReceived++

	// Validate the cell.
	if !h.validator.ValidateCell(msg) {
		h.stats.CellsRejected++
		return fmt.Errorf("%w: blob=%d cell=%d", ErrGossipCellValidation, msg.BlobIndex, msg.CellIndex)
	}
	h.stats.CellsValidated++

	// Get or create blob state.
	state, ok := h.pendingCells[msg.BlobIndex]
	if !ok {
		state = &blobCellState{
			cells: make(map[int]CellGossipMessage),
		}
		h.pendingCells[msg.BlobIndex] = state
	}

	// Skip if blob is already reconstructed.
	if state.reconstructed {
		return nil
	}

	// Check for duplicate.
	if _, exists := state.cells[msg.CellIndex]; exists {
		h.stats.CellsDuplicate++
		return nil // Silently ignore duplicates (not an error in gossip).
	}

	// Store the cell.
	state.cells[msg.CellIndex] = msg

	// Check reconstruction readiness.
	if !state.ready && len(state.cells) >= h.reconstructionThreshold {
		state.ready = true
		h.stats.BlobsReady++
		h.notifyCallbacks(msg.BlobIndex, "ready")
	}

	return nil
}

// BroadcastCell queues a cell for broadcast to gossip peers.
// The cell is validated before queueing.
func (h *CellGossipHandler) BroadcastCell(msg CellGossipMessage) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrGossipHandlerClosed
	}

	if !h.validator.ValidateCell(msg) {
		return fmt.Errorf("%w: blob=%d cell=%d", ErrGossipCellValidation, msg.BlobIndex, msg.CellIndex)
	}

	if len(h.broadcastQueue) >= h.maxBroadcastQueue {
		// Drop oldest when queue is full.
		h.broadcastQueue = h.broadcastQueue[1:]
	}

	h.broadcastQueue = append(h.broadcastQueue, msg)
	h.stats.CellsBroadcast++
	return nil
}

// DrainBroadcastQueue returns and clears the pending broadcast queue.
func (h *CellGossipHandler) DrainBroadcastQueue() []CellGossipMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.broadcastQueue) == 0 {
		return nil
	}

	result := h.broadcastQueue
	h.broadcastQueue = nil
	return result
}

// CheckReconstructionReady returns true if enough cells have been received
// for the specified blob to attempt reconstruction.
func (h *CellGossipHandler) CheckReconstructionReady(blobIndex int) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.pendingCells[blobIndex]
	if !ok {
		return false
	}
	return len(state.cells) >= h.reconstructionThreshold
}

// GetMissingCells returns the cell indices that are still missing for a
// given blob. Returns nil if the blob is not being tracked.
func (h *CellGossipHandler) GetMissingCells(blobIndex int) []int {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.pendingCells[blobIndex]
	if !ok {
		return nil
	}

	missing := make([]int, 0)
	for i := 0; i < h.maxCellIndex; i++ {
		if _, exists := state.cells[i]; !exists {
			missing = append(missing, i)
		}
	}
	return missing
}

// GetReceivedCells returns all received cells for a blob, sorted by cell index.
func (h *CellGossipHandler) GetReceivedCells(blobIndex int) []CellGossipMessage {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.pendingCells[blobIndex]
	if !ok {
		return nil
	}

	cells := make([]CellGossipMessage, 0, len(state.cells))
	for _, msg := range state.cells {
		cells = append(cells, msg)
	}
	sort.Slice(cells, func(i, j int) bool {
		return cells[i].CellIndex < cells[j].CellIndex
	})
	return cells
}

// ReceivedCellCount returns the number of cells received for a blob.
func (h *CellGossipHandler) ReceivedCellCount(blobIndex int) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.pendingCells[blobIndex]
	if !ok {
		return 0
	}
	return len(state.cells)
}

// MarkReconstructed marks a blob as having been reconstructed, preventing
// further cell storage for this blob.
func (h *CellGossipHandler) MarkReconstructed(blobIndex int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.pendingCells[blobIndex]
	if !ok {
		return
	}
	state.reconstructed = true
}

// TrackedBlobs returns the blob indices currently being tracked.
func (h *CellGossipHandler) TrackedBlobs() []int {
	h.mu.Lock()
	defer h.mu.Unlock()

	blobs := make([]int, 0, len(h.pendingCells))
	for idx := range h.pendingCells {
		blobs = append(blobs, idx)
	}
	sort.Ints(blobs)
	return blobs
}

// ReadyBlobs returns blob indices that have enough cells for reconstruction
// but have not yet been reconstructed.
func (h *CellGossipHandler) ReadyBlobs() []int {
	h.mu.Lock()
	defer h.mu.Unlock()

	var ready []int
	for idx, state := range h.pendingCells {
		if state.ready && !state.reconstructed {
			ready = append(ready, idx)
		}
	}
	sort.Ints(ready)
	return ready
}

// RegisterCallback registers a callback for gossip events.
// Events: "ready" (blob has enough cells for reconstruction).
func (h *CellGossipHandler) RegisterCallback(cb CellGossipCallback) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callbacks = append(h.callbacks, cb)
}

// notifyCallbacks calls all registered callbacks. Must hold h.mu.
func (h *CellGossipHandler) notifyCallbacks(blobIndex int, event string) {
	for _, cb := range h.callbacks {
		cb(blobIndex, event)
	}
}

// Stats returns a snapshot of the handler's statistics.
func (h *CellGossipHandler) Stats() GossipHandlerStats {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stats
}

// Reset clears all state for a new slot/block.
func (h *CellGossipHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pendingCells = make(map[int]*blobCellState)
	h.broadcastQueue = nil
	h.stats = GossipHandlerStats{}
}

// Close shuts down the handler, preventing further operations.
func (h *CellGossipHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
}

// ComputeCellHash computes a Keccak256 hash of a cell gossip message,
// incorporating blob index, cell index, slot, and data for deduplication.
func ComputeCellHash(msg CellGossipMessage) [32]byte {
	var buf [16]byte
	binary.LittleEndian.PutUint32(buf[0:4], uint32(msg.BlobIndex))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(msg.CellIndex))
	binary.LittleEndian.PutUint64(buf[8:16], msg.Slot)

	h := crypto.Keccak256Hash(buf[:], msg.Data)
	return h
}
