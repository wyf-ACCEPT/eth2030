// checkpoint_store.go implements a trusted checkpoint store and sync
// orchestrator for fast initial synchronization. It manages verified
// checkpoints from multiple sources, header range requests for batch
// downloads, and detailed sync state transitions.
package sync

import (
	"encoding/binary"
	"errors"
	"fmt"
	gosync "sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Checkpoint store errors.
var (
	ErrStoreCheckpointExists  = errors.New("checkpoint store: checkpoint already exists")
	ErrStoreCheckpointUnknown = errors.New("checkpoint store: checkpoint not found")
	ErrStoreEmpty             = errors.New("checkpoint store: no checkpoints registered")
	ErrStoreSyncActive        = errors.New("checkpoint store: sync already active")
	ErrStoreSyncInactive      = errors.New("checkpoint store: no active sync")
	ErrStoreInvalidRange      = errors.New("checkpoint store: invalid header range")
	ErrStoreRangeOverlap      = errors.New("checkpoint store: range request overlaps pending")
	ErrStoreTooManyPending    = errors.New("checkpoint store: too many pending range requests")
)

// SyncState represents the current phase of checkpoint-based sync.
type SyncState uint32

const (
	StateCheckpointIdle               SyncState = iota
	StateCheckpointDownloadingHeaders
	StateCheckpointDownloadingBodies
	StateCheckpointDownloadingReceipts
	StateCheckpointProcessing
	StateCheckpointComplete
)

// String returns a human-readable name for the sync state.
func (s SyncState) String() string {
	names := [...]string{"idle", "downloading_headers", "downloading_bodies",
		"downloading_receipts", "processing", "complete"}
	if int(s) < len(names) {
		return names[s]
	}
	return fmt.Sprintf("unknown(%d)", s)
}

// TrustedCheckpoint identifies a verified point in the chain suitable
// for starting sync.
type TrustedCheckpoint struct {
	BlockNumber uint64
	BlockHash   types.Hash
	StateRoot   types.Hash
	Epoch       uint64
	Source      string    // Origin: "embedded", "api", "manual", "beacon".
	AddedAt     time.Time // When this checkpoint was registered.
}

// CheckpointID returns a deterministic hash identifying this checkpoint.
func (tc TrustedCheckpoint) CheckpointID() types.Hash {
	buf := make([]byte, 0, 16+64)
	buf = binary.BigEndian.AppendUint64(buf, tc.Epoch)
	buf = binary.BigEndian.AppendUint64(buf, tc.BlockNumber)
	buf = append(buf, tc.BlockHash[:]...)
	buf = append(buf, tc.StateRoot[:]...)
	return crypto.Keccak256Hash(buf)
}

// Validate checks that the checkpoint fields are internally consistent.
func (tc TrustedCheckpoint) Validate() error {
	if tc.BlockHash.IsZero() {
		return errors.New("checkpoint: block hash must not be zero")
	}
	if tc.StateRoot.IsZero() {
		return errors.New("checkpoint: state root must not be zero")
	}
	if tc.BlockNumber == 0 {
		return errors.New("checkpoint: block number must not be zero")
	}
	if tc.Epoch > 0 && tc.BlockNumber < tc.Epoch*32 {
		return fmt.Errorf("checkpoint: block %d < epoch %d * 32", tc.BlockNumber, tc.Epoch)
	}
	return nil
}

// CheckpointSyncProgress reports checkpoint sync state and progress.
type CheckpointSyncProgress struct {
	State        SyncState
	CurrentBlock uint64
	HighestBlock uint64
	StartedAt    time.Time
	ETA          time.Duration
	Percentage   float64
	HeadersDown  uint64
	BodiesDown   uint64
	ReceiptsDown uint64
}

// HeaderRangeRequest describes a batch header download request.
type HeaderRangeRequest struct {
	ID        uint64
	From      uint64
	To        uint64
	PeerID    string
	IssuedAt  time.Time
	Completed bool
	Headers   int
}

// Validate checks that the range request has valid bounds.
func (r HeaderRangeRequest) Validate() error {
	if r.From == 0 {
		return fmt.Errorf("%w: from must be > 0", ErrStoreInvalidRange)
	}
	if r.To < r.From {
		return fmt.Errorf("%w: to (%d) < from (%d)", ErrStoreInvalidRange, r.To, r.From)
	}
	return nil
}

// Count returns the number of blocks in this range (inclusive).
func (r HeaderRangeRequest) Count() uint64 { return r.To - r.From + 1 }

// CheckpointStoreConfig configures the checkpoint store.
type CheckpointStoreConfig struct {
	MaxCheckpoints   int
	MaxPendingRanges int
	HeaderBatchSize  int
}

// DefaultCheckpointStoreConfig returns sensible defaults.
func DefaultCheckpointStoreConfig() CheckpointStoreConfig {
	return CheckpointStoreConfig{MaxCheckpoints: 256, MaxPendingRanges: 16, HeaderBatchSize: 192}
}

// CheckpointStore manages trusted checkpoints and orchestrates
// checkpoint-based sync with range request tracking.
type CheckpointStore struct {
	config      CheckpointStoreConfig
	mu          gosync.RWMutex
	checkpoints map[types.Hash]*TrustedCheckpoint
	order       []types.Hash
	active      *TrustedCheckpoint
	state       atomic.Uint32
	progress    CheckpointSyncProgress
	ranges      map[uint64]*HeaderRangeRequest
	nextRangeID uint64
}

// NewCheckpointStore creates a new checkpoint store.
func NewCheckpointStore(config CheckpointStoreConfig) *CheckpointStore {
	if config.MaxPendingRanges <= 0 {
		config.MaxPendingRanges = 16
	}
	if config.HeaderBatchSize <= 0 {
		config.HeaderBatchSize = 192
	}
	return &CheckpointStore{
		config:      config,
		checkpoints: make(map[types.Hash]*TrustedCheckpoint),
		ranges:      make(map[uint64]*HeaderRangeRequest),
	}
}

// RegisterCheckpoint adds a verified checkpoint to the store.
func (cs *CheckpointStore) RegisterCheckpoint(cp TrustedCheckpoint) error {
	if err := cp.Validate(); err != nil {
		return err
	}
	id := cp.CheckpointID()
	if cp.AddedAt.IsZero() {
		cp.AddedAt = time.Now()
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if _, exists := cs.checkpoints[id]; exists {
		return ErrStoreCheckpointExists
	}
	if cs.config.MaxCheckpoints > 0 && len(cs.checkpoints) >= cs.config.MaxCheckpoints {
		if len(cs.order) > 0 {
			delete(cs.checkpoints, cs.order[0])
			cs.order = cs.order[1:]
		}
	}
	stored := cp
	cs.checkpoints[id] = &stored
	cs.order = append(cs.order, id)
	return nil
}

// GetCheckpoint retrieves a checkpoint by its ID hash.
func (cs *CheckpointStore) GetCheckpoint(id types.Hash) (*TrustedCheckpoint, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	cp, ok := cs.checkpoints[id]
	if !ok {
		return nil, ErrStoreCheckpointUnknown
	}
	result := *cp
	return &result, nil
}

// GetLatestCheckpoint returns the most recently registered checkpoint.
func (cs *CheckpointStore) GetLatestCheckpoint() *TrustedCheckpoint {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if len(cs.order) == 0 {
		return nil
	}
	latest := cs.checkpoints[cs.order[len(cs.order)-1]]
	if latest == nil {
		return nil
	}
	result := *latest
	return &result
}

// GetHighestCheckpoint returns the checkpoint with the highest block number.
func (cs *CheckpointStore) GetHighestCheckpoint() *TrustedCheckpoint {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var best *TrustedCheckpoint
	for _, cp := range cs.checkpoints {
		if best == nil || cp.BlockNumber > best.BlockNumber {
			best = cp
		}
	}
	if best == nil {
		return nil
	}
	result := *best
	return &result
}

// CheckpointCount returns the number of stored checkpoints.
func (cs *CheckpointStore) CheckpointCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.checkpoints)
}

// ListCheckpoints returns all stored checkpoints in insertion order.
func (cs *CheckpointStore) ListCheckpoints() []TrustedCheckpoint {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	result := make([]TrustedCheckpoint, 0, len(cs.order))
	for _, id := range cs.order {
		if cp, ok := cs.checkpoints[id]; ok {
			result = append(result, *cp)
		}
	}
	return result
}

// CheckpointsBySource returns all checkpoints from the given source.
func (cs *CheckpointStore) CheckpointsBySource(source string) []TrustedCheckpoint {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var result []TrustedCheckpoint
	for _, cp := range cs.checkpoints {
		if cp.Source == source {
			result = append(result, *cp)
		}
	}
	return result
}

// StartSync begins checkpoint sync from cp toward targetBlock.
func (cs *CheckpointStore) StartSync(cp TrustedCheckpoint, targetBlock uint64) error {
	if err := cp.Validate(); err != nil {
		return err
	}
	if !cs.state.CompareAndSwap(uint32(StateCheckpointIdle), uint32(StateCheckpointDownloadingHeaders)) {
		return ErrStoreSyncActive
	}
	cs.mu.Lock()
	stored := cp
	cs.active = &stored
	cs.progress = CheckpointSyncProgress{
		State: StateCheckpointDownloadingHeaders, CurrentBlock: cp.BlockNumber,
		HighestBlock: targetBlock, StartedAt: time.Now(),
	}
	if targetBlock <= cp.BlockNumber {
		cs.progress.State = StateCheckpointComplete
		cs.progress.HighestBlock = cp.BlockNumber
		cs.progress.Percentage = 100.0
		cs.mu.Unlock()
		cs.state.Store(uint32(StateCheckpointComplete))
		return nil
	}
	cs.mu.Unlock()
	return nil
}

// VerifyCheckpoint validates and registers a checkpoint.
func (cs *CheckpointStore) VerifyCheckpoint(cp TrustedCheckpoint) error {
	if err := cp.Validate(); err != nil {
		return err
	}
	return cs.RegisterCheckpoint(cp)
}

// TransitionState moves the sync to the next state.
func (cs *CheckpointStore) TransitionState(next SyncState) error {
	current := SyncState(cs.state.Load())
	if current == StateCheckpointIdle {
		return ErrStoreSyncInactive
	}
	if current == StateCheckpointComplete {
		return errors.New("checkpoint store: sync already complete")
	}
	cs.state.Store(uint32(next))
	cs.mu.Lock()
	cs.progress.State = next
	cs.mu.Unlock()
	return nil
}

// UpdateProgress updates current block and recalculates percentage/ETA.
func (cs *CheckpointStore) UpdateProgress(currentBlock, headersDown, bodiesDown, receiptsDown uint64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.progress.CurrentBlock = currentBlock
	cs.progress.HeadersDown = headersDown
	cs.progress.BodiesDown = bodiesDown
	cs.progress.ReceiptsDown = receiptsDown
	if cs.active == nil {
		return
	}
	total := cs.progress.HighestBlock - cs.active.BlockNumber
	if total == 0 {
		cs.progress.Percentage = 100.0
		return
	}
	done := currentBlock - cs.active.BlockNumber
	if done > total {
		done = total
	}
	cs.progress.Percentage = float64(done) / float64(total) * 100.0
	if done > 0 && !cs.progress.StartedAt.IsZero() {
		elapsed := time.Since(cs.progress.StartedAt)
		cs.progress.ETA = time.Duration(float64(elapsed) * float64(total-done) / float64(done))
	}
	if currentBlock >= cs.progress.HighestBlock {
		cs.progress.State = StateCheckpointComplete
		cs.progress.Percentage = 100.0
		cs.state.Store(uint32(StateCheckpointComplete))
	}
}

// Progress returns the current sync progress.
func (cs *CheckpointStore) Progress() CheckpointSyncProgress {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.progress
}

// State returns the current sync state.
func (cs *CheckpointStore) State() SyncState { return SyncState(cs.state.Load()) }

// ActiveCheckpoint returns the currently active checkpoint, or nil.
func (cs *CheckpointStore) ActiveCheckpoint() *TrustedCheckpoint {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.active == nil {
		return nil
	}
	result := *cs.active
	return &result
}

// Reset clears sync state and returns to idle.
func (cs *CheckpointStore) Reset() {
	cs.state.Store(uint32(StateCheckpointIdle))
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.active = nil
	cs.progress = CheckpointSyncProgress{}
	cs.ranges = make(map[uint64]*HeaderRangeRequest)
	cs.nextRangeID = 0
}

// CreateRangeRequest creates a new header range request.
func (cs *CheckpointStore) CreateRangeRequest(from, to uint64, peerID string) (uint64, error) {
	req := HeaderRangeRequest{From: from, To: to, PeerID: peerID}
	if err := req.Validate(); err != nil {
		return 0, err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	pending := 0
	for _, r := range cs.ranges {
		if !r.Completed {
			pending++
		}
	}
	if pending >= cs.config.MaxPendingRanges {
		return 0, ErrStoreTooManyPending
	}
	for _, r := range cs.ranges {
		if !r.Completed && from <= r.To && to >= r.From {
			return 0, ErrStoreRangeOverlap
		}
	}
	cs.nextRangeID++
	req.ID = cs.nextRangeID
	req.IssuedAt = time.Now()
	cs.ranges[req.ID] = &req
	return req.ID, nil
}

// CompleteRangeRequest marks a range request as completed.
func (cs *CheckpointStore) CompleteRangeRequest(id uint64, headers int) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	req, ok := cs.ranges[id]
	if !ok {
		return fmt.Errorf("checkpoint store: range request %d not found", id)
	}
	req.Completed = true
	req.Headers = headers
	return nil
}

// PendingRangeRequests returns the number of incomplete range requests.
func (cs *CheckpointStore) PendingRangeRequests() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	count := 0
	for _, r := range cs.ranges {
		if !r.Completed {
			count++
		}
	}
	return count
}

// CompletedRangeRequests returns the number of completed range requests.
func (cs *CheckpointStore) CompletedRangeRequests() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	count := 0
	for _, r := range cs.ranges {
		if r.Completed {
			count++
		}
	}
	return count
}

// GetRangeRequest returns a range request by ID.
func (cs *CheckpointStore) GetRangeRequest(id uint64) (*HeaderRangeRequest, error) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	req, ok := cs.ranges[id]
	if !ok {
		return nil, fmt.Errorf("checkpoint store: range request %d not found", id)
	}
	result := *req
	return &result, nil
}

// RemoveCheckpoint removes a checkpoint by its ID.
func (cs *CheckpointStore) RemoveCheckpoint(id types.Hash) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if _, ok := cs.checkpoints[id]; !ok {
		return ErrStoreCheckpointUnknown
	}
	delete(cs.checkpoints, id)
	for i, oid := range cs.order {
		if oid == id {
			cs.order = append(cs.order[:i], cs.order[i+1:]...)
			break
		}
	}
	return nil
}
