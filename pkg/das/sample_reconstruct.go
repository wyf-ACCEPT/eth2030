// Package das - sample_reconstruct.go implements a sample-level blob
// reconstruction engine that collects individual cell samples, tracks
// reconstruction progress per blob, and reconstructs full blobs from partial
// samples using Reed-Solomon erasure coding.
//
// SampleReconstructor provides a higher-level API on top of the raw
// ReconstructBlob function, adding per-cell sample management, progress
// tracking, verification hooks, and integration with the erasure package
// for the simpler XOR-based recovery path.
//
// Reference: EIP-7594 PeerDAS specification, local blob reconstruction
package das

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/das/erasure"
)

// SampleReconstructor errors.
var (
	ErrReconstructorClosed    = errors.New("das/reconstruct: reconstructor is closed")
	ErrBlobAlreadyComplete    = errors.New("das/reconstruct: blob already reconstructed")
	ErrInvalidSamplePayload   = errors.New("das/reconstruct: invalid sample data size")
	ErrCannotReconstruct      = errors.New("das/reconstruct: insufficient samples for reconstruction")
	ErrErasureRecoveryFailed  = errors.New("das/reconstruct: erasure recovery failed")
)

// SampleReconstructorConfig configures the sample reconstructor.
type SampleReconstructorConfig struct {
	// MaxBlobs is the maximum number of blobs per block.
	MaxBlobs int

	// CellsPerBlob is the total number of cells in the extended blob.
	CellsPerBlob int

	// DataCells is the minimum number of cells needed for reconstruction (50%).
	DataCells int

	// UseErasureCoding enables the simple XOR-based erasure recovery path
	// from the erasure package as a fallback or alternative.
	UseErasureCoding bool
}

// DefaultSampleReconstructorConfig returns the default configuration.
func DefaultSampleReconstructorConfig() SampleReconstructorConfig {
	return SampleReconstructorConfig{
		MaxBlobs:         MaxBlobCommitmentsPerBlock,
		CellsPerBlob:    CellsPerExtBlob,
		DataCells:        ReconstructionThreshold,
		UseErasureCoding: false,
	}
}

// CellSample represents a single cell received from the network.
type CellSample struct {
	BlobIdx int
	CellIdx int
	Data    []byte
}

// BlobProgress tracks the reconstruction progress for a single blob.
type BlobProgress struct {
	// BlobIdx is the blob index.
	BlobIdx int

	// Collected is the number of unique cells collected so far.
	Collected int

	// Needed is the minimum number of cells needed for reconstruction.
	Needed int

	// Complete is true if the blob has been fully reconstructed.
	Complete bool

	// CellMap tracks which cell indices have been received.
	CellMap map[int]bool
}

// ReconstructorMetrics tracks sample reconstruction statistics.
type ReconstructorMetrics struct {
	SamplesReceived    atomic.Int64
	SamplesDuplicate   atomic.Int64
	SamplesInvalid     atomic.Int64
	BlobsComplete      atomic.Int64
	BlobsFailed        atomic.Int64
	ReconstructionNs   atomic.Int64
	LastReconstructNs  atomic.Int64
	ErasureRecoveries  atomic.Int64
}

// AvgReconstructMs returns the average reconstruction latency in milliseconds.
func (m *ReconstructorMetrics) AvgReconstructMs() float64 {
	total := m.BlobsComplete.Load() + m.BlobsFailed.Load()
	if total == 0 {
		return 0
	}
	return float64(m.ReconstructionNs.Load()) / float64(total) / 1e6
}

// blobState holds the internal state for a blob being reconstructed.
type blobState struct {
	cells    map[int]Cell // cell index -> cell data
	complete bool
	result   []byte // reconstructed blob data
}

// SampleReconstructor collects individual cell samples and reconstructs
// full blobs from partial data using Reed-Solomon erasure coding.
type SampleReconstructor struct {
	mu      sync.Mutex
	config  SampleReconstructorConfig
	blobs   map[int]*blobState
	closed  bool
	Metrics ReconstructorMetrics
}

// NewSampleReconstructor creates a new sample reconstructor.
func NewSampleReconstructor(config SampleReconstructorConfig) *SampleReconstructor {
	if config.MaxBlobs <= 0 {
		config.MaxBlobs = MaxBlobCommitmentsPerBlock
	}
	if config.CellsPerBlob <= 0 {
		config.CellsPerBlob = CellsPerExtBlob
	}
	if config.DataCells <= 0 {
		config.DataCells = ReconstructionThreshold
	}
	return &SampleReconstructor{
		config: config,
		blobs:  make(map[int]*blobState),
	}
}

// AddSample adds a single cell sample for reconstruction. It validates the
// sample, deduplicates by cell index, and returns nil on success.
func (sr *SampleReconstructor) AddSample(blobIdx, cellIdx int, data []byte) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if sr.closed {
		return ErrReconstructorClosed
	}

	if blobIdx < 0 || blobIdx >= sr.config.MaxBlobs {
		sr.Metrics.SamplesInvalid.Add(1)
		return fmt.Errorf("%w: blob index %d out of range [0, %d)",
			ErrSampleBlobOutOfRange, blobIdx, sr.config.MaxBlobs)
	}
	if cellIdx < 0 || cellIdx >= sr.config.CellsPerBlob {
		sr.Metrics.SamplesInvalid.Add(1)
		return fmt.Errorf("%w: cell index %d out of range [0, %d)",
			ErrInvalidSampleIndex, cellIdx, sr.config.CellsPerBlob)
	}
	if len(data) == 0 {
		sr.Metrics.SamplesInvalid.Add(1)
		return ErrInvalidSamplePayload
	}

	sr.Metrics.SamplesReceived.Add(1)

	state, ok := sr.blobs[blobIdx]
	if !ok {
		state = &blobState{
			cells: make(map[int]Cell),
		}
		sr.blobs[blobIdx] = state
	}

	if state.complete {
		return ErrBlobAlreadyComplete
	}

	// Check for duplicate.
	if _, exists := state.cells[cellIdx]; exists {
		sr.Metrics.SamplesDuplicate.Add(1)
		return nil
	}

	// Convert data to Cell (zero-pad or truncate to BytesPerCell).
	var cell Cell
	copyLen := len(data)
	if copyLen > BytesPerCell {
		copyLen = BytesPerCell
	}
	copy(cell[:copyLen], data[:copyLen])
	state.cells[cellIdx] = cell

	return nil
}

// CanReconstruct reports whether enough samples have been collected for
// the specified blob to attempt reconstruction.
func (sr *SampleReconstructor) CanReconstruct(blobIdx int) bool {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	state, ok := sr.blobs[blobIdx]
	if !ok {
		return false
	}
	if state.complete {
		return true
	}
	return len(state.cells) >= sr.config.DataCells
}

// ReconstructionStatus returns the number of cells collected and needed
// for a given blob index.
func (sr *SampleReconstructor) ReconstructionStatus(blobIdx int) (collected, needed int) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	state, ok := sr.blobs[blobIdx]
	if !ok {
		return 0, sr.config.DataCells
	}
	return len(state.cells), sr.config.DataCells
}

// Reconstruct attempts to reconstruct the full blob data from collected
// samples. Returns the reconstructed blob or an error if insufficient
// samples are available.
func (sr *SampleReconstructor) Reconstruct(blobIdx int) ([]byte, error) {
	sr.mu.Lock()

	if sr.closed {
		sr.mu.Unlock()
		return nil, ErrReconstructorClosed
	}

	state, ok := sr.blobs[blobIdx]
	if !ok {
		sr.mu.Unlock()
		return nil, fmt.Errorf("%w: no samples for blob %d", ErrCannotReconstruct, blobIdx)
	}

	// Return cached result if already complete.
	if state.complete {
		result := make([]byte, len(state.result))
		copy(result, state.result)
		sr.mu.Unlock()
		return result, nil
	}

	if len(state.cells) < sr.config.DataCells {
		sr.mu.Unlock()
		return nil, fmt.Errorf("%w: have %d cells, need %d",
			ErrCannotReconstruct, len(state.cells), sr.config.DataCells)
	}

	// Snapshot cells for reconstruction (release lock during computation).
	cells := make([]Cell, 0, len(state.cells))
	indices := make([]uint64, 0, len(state.cells))
	for idx, cell := range state.cells {
		cells = append(cells, cell)
		indices = append(indices, uint64(idx))
	}
	sr.mu.Unlock()

	start := time.Now()

	var result []byte
	var err error

	if sr.config.UseErasureCoding {
		result, err = sr.reconstructWithErasure(cells, indices)
	} else {
		result, err = ReconstructBlob(cells, indices)
	}

	latency := time.Since(start).Nanoseconds()
	sr.Metrics.ReconstructionNs.Add(latency)
	sr.Metrics.LastReconstructNs.Store(latency)

	if err != nil {
		sr.Metrics.BlobsFailed.Add(1)
		return nil, fmt.Errorf("%w: %v", ErrErasureRecoveryFailed, err)
	}

	sr.Metrics.BlobsComplete.Add(1)

	// Cache the result.
	sr.mu.Lock()
	if st, ok := sr.blobs[blobIdx]; ok {
		st.complete = true
		st.result = make([]byte, len(result))
		copy(st.result, result)
	}
	sr.mu.Unlock()

	return result, nil
}

// reconstructWithErasure uses the simpler XOR-based erasure coding from the
// erasure package as an alternative reconstruction path.
func (sr *SampleReconstructor) reconstructWithErasure(cells []Cell, indices []uint64) ([]byte, error) {
	sr.Metrics.ErasureRecoveries.Add(1)

	dataShards := sr.config.CellsPerBlob / 2
	parityShards := sr.config.CellsPerBlob / 2
	totalShards := dataShards + parityShards

	// Build shard array with nil for missing shards.
	shards := make([][]byte, totalShards)
	for i, idx := range indices {
		if int(idx) < totalShards {
			shards[idx] = cells[i][:]
		}
	}

	result, err := erasure.Decode(shards, dataShards, parityShards)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Progress returns the reconstruction progress for all tracked blobs.
func (sr *SampleReconstructor) Progress() []BlobProgress {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var progress []BlobProgress
	for blobIdx, state := range sr.blobs {
		cellMap := make(map[int]bool, len(state.cells))
		for idx := range state.cells {
			cellMap[idx] = true
		}

		progress = append(progress, BlobProgress{
			BlobIdx:   blobIdx,
			Collected: len(state.cells),
			Needed:    sr.config.DataCells,
			Complete:  state.complete,
			CellMap:   cellMap,
		})
	}
	return progress
}

// Reset clears all state for a new block.
func (sr *SampleReconstructor) Reset() {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.blobs = make(map[int]*blobState)
}

// Close marks the reconstructor as closed, preventing further operations.
func (sr *SampleReconstructor) Close() {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	sr.closed = true
}

// BlobCount returns the number of blobs with at least one sample.
func (sr *SampleReconstructor) BlobCount() int {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return len(sr.blobs)
}

// CompletedBlobs returns the blob indices that have been fully reconstructed.
func (sr *SampleReconstructor) CompletedBlobs() []int {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var completed []int
	for idx, state := range sr.blobs {
		if state.complete {
			completed = append(completed, idx)
		}
	}
	return completed
}

// PendingBlobs returns blob indices that have samples but are not yet complete.
func (sr *SampleReconstructor) PendingBlobs() []int {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var pending []int
	for idx, state := range sr.blobs {
		if !state.complete {
			pending = append(pending, idx)
		}
	}
	return pending
}

// ReadyBlobs returns blob indices that have enough samples for reconstruction
// but have not yet been reconstructed.
func (sr *SampleReconstructor) ReadyBlobs() []int {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	var ready []int
	for idx, state := range sr.blobs {
		if !state.complete && len(state.cells) >= sr.config.DataCells {
			ready = append(ready, idx)
		}
	}
	return ready
}

// ReconstructAllReady reconstructs all blobs that have sufficient samples.
// Returns a map of blob index to reconstructed data.
func (sr *SampleReconstructor) ReconstructAllReady() (map[int][]byte, error) {
	ready := sr.ReadyBlobs()
	if len(ready) == 0 {
		return nil, nil
	}

	results := make(map[int][]byte)
	var firstErr error

	for _, blobIdx := range ready {
		data, err := sr.Reconstruct(blobIdx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		results[blobIdx] = data
	}

	return results, firstErr
}
