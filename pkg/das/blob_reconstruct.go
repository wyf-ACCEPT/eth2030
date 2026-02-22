// Package das - blob_reconstruct.go implements a full blob reconstruction engine
// for PeerDAS local blob reconstruction. It uses Reed-Solomon erasure coding
// recovery from partial cell samples via Lagrange interpolation over the
// BLS12-381 scalar field.
//
// This builds on the skeletal reconstruction.go by adding a higher-level
// BlobReconstructor that supports parallel multi-blob reconstruction,
// sample validation, minimum threshold checking, and metrics tracking.
package das

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Reconstruction errors.
var (
	ErrNilSample            = errors.New("das: nil sample")
	ErrInvalidSampleIndex   = errors.New("das: sample cell index out of range")
	ErrSampleBlobOutOfRange = errors.New("das: sample blob index out of range")
	ErrNoSamplesForBlob     = errors.New("das: no samples for blob")
	ErrReconstructionFailed = errors.New("das: reconstruction failed")
)

// Sample represents a single cell sample received from the network,
// tagged with its blob index and cell index for reconstruction.
type Sample struct {
	// BlobIndex identifies which blob in the block this sample belongs to.
	BlobIndex uint64
	// CellIndex is the column position within the extended blob [0, CellsPerExtBlob).
	CellIndex uint64
	// Data is the cell content.
	Data Cell
}

// ValidateSample checks that a sample has valid indices.
func ValidateSample(s *Sample, blobCount int) error {
	if s == nil {
		return ErrNilSample
	}
	if s.CellIndex >= CellsPerExtBlob {
		return fmt.Errorf("%w: cell %d >= %d", ErrInvalidSampleIndex, s.CellIndex, CellsPerExtBlob)
	}
	if blobCount > 0 && s.BlobIndex >= uint64(blobCount) {
		return fmt.Errorf("%w: blob %d >= %d", ErrSampleBlobOutOfRange, s.BlobIndex, blobCount)
	}
	return nil
}

// ReconstructionMetrics tracks statistics about blob reconstruction operations.
type ReconstructionMetrics struct {
	// SuccessCount is the total number of successful reconstructions.
	SuccessCount atomic.Int64
	// FailureCount is the total number of failed reconstructions.
	FailureCount atomic.Int64
	// TotalLatencyNs accumulates latency in nanoseconds for averaging.
	TotalLatencyNs atomic.Int64
	// LastLatencyNs is the latency of the most recent reconstruction.
	LastLatencyNs atomic.Int64
	// BlobsReconstructed is the total number of individual blobs recovered.
	BlobsReconstructed atomic.Int64
	// InsufficientSamples counts times reconstruction was skipped due to too few samples.
	InsufficientSamples atomic.Int64
}

// AvgLatencyMs returns the average reconstruction latency in milliseconds.
func (m *ReconstructionMetrics) AvgLatencyMs() float64 {
	total := m.TotalLatencyNs.Load()
	count := m.SuccessCount.Load() + m.FailureCount.Load()
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count) / 1e6
}

// ValidateReconstructionInput checks that reconstruction inputs are valid:
// minimum fragment count, cell index validity, and no nil cells.
func ValidateReconstructionInput(samples []Sample, totalCells int) error {
	if totalCells <= 0 {
		return errors.New("das: total cells must be > 0")
	}
	if len(samples) == 0 {
		return ErrNoSamplesForBlob
	}
	if len(samples) < ReconstructionThreshold {
		return fmt.Errorf("%w: have %d, need %d", ErrInsufficientCells, len(samples), ReconstructionThreshold)
	}
	seen := make(map[uint64]struct{}, len(samples))
	for _, s := range samples {
		if s.CellIndex >= uint64(totalCells) {
			return fmt.Errorf("%w: cell %d >= %d", ErrInvalidSampleIndex, s.CellIndex, totalCells)
		}
		seen[s.CellIndex] = struct{}{}
	}
	if len(seen) < ReconstructionThreshold {
		return fmt.Errorf("%w: only %d unique cells, need %d",
			ErrInsufficientCells, len(seen), ReconstructionThreshold)
	}
	return nil
}

// BlobReconstructor manages blob reconstruction from partial cell samples.
// It groups samples by blob index, validates them, and performs parallel
// Reed-Solomon erasure coding recovery.
type BlobReconstructor struct {
	// mu protects the pending samples map.
	mu sync.RWMutex
	// pending maps blob index to collected samples.
	pending map[uint64][]Sample
	// maxBlobs is the maximum expected blob count per block.
	maxBlobs int
	// Metrics tracks reconstruction statistics.
	Metrics ReconstructionMetrics
}

// NewBlobReconstructor creates a new BlobReconstructor.
// maxBlobs sets the maximum expected blob count; use 0 for no limit.
func NewBlobReconstructor(maxBlobs int) *BlobReconstructor {
	if maxBlobs <= 0 {
		maxBlobs = MaxBlobCommitmentsPerBlock
	}
	return &BlobReconstructor{
		pending:  make(map[uint64][]Sample),
		maxBlobs: maxBlobs,
	}
}

// AddSample adds a cell sample to the pending set for its blob.
// It validates the sample and deduplicates by cell index.
func (br *BlobReconstructor) AddSample(s Sample) error {
	if err := ValidateSample(&s, br.maxBlobs); err != nil {
		return err
	}

	br.mu.Lock()
	defer br.mu.Unlock()

	existing := br.pending[s.BlobIndex]
	// Check for duplicate cell index.
	for _, e := range existing {
		if e.CellIndex == s.CellIndex {
			// Silently ignore duplicates.
			return nil
		}
	}
	br.pending[s.BlobIndex] = append(existing, s)
	return nil
}

// AddSamples adds multiple samples, returning the first error encountered.
func (br *BlobReconstructor) AddSamples(samples []Sample) error {
	for i := range samples {
		if err := br.AddSample(samples[i]); err != nil {
			return fmt.Errorf("sample %d: %w", i, err)
		}
	}
	return nil
}

// SampleCount returns the number of samples collected for a given blob.
func (br *BlobReconstructor) SampleCount(blobIndex uint64) int {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return len(br.pending[blobIndex])
}

// CanReconstructBlob returns true if enough samples have been collected
// for the given blob to attempt reconstruction.
func (br *BlobReconstructor) CanReconstructBlob(blobIndex uint64) bool {
	return br.SampleCount(blobIndex) >= ReconstructionThreshold
}

// ReadyBlobs returns the blob indices that have sufficient samples for
// reconstruction.
func (br *BlobReconstructor) ReadyBlobs() []uint64 {
	br.mu.RLock()
	defer br.mu.RUnlock()

	var ready []uint64
	for blobIdx, samples := range br.pending {
		if len(samples) >= ReconstructionThreshold {
			ready = append(ready, blobIdx)
		}
	}
	return ready
}

// Reconstruct recovers blob data from the collected samples for a single blob.
// It requires at least ReconstructionThreshold (64) unique cell samples.
// The totalCells parameter specifies the expected total number of cells
// in the extended blob (normally CellsPerExtBlob = 128).
func (br *BlobReconstructor) Reconstruct(samples []Sample, totalCells int) ([]byte, error) {
	start := time.Now()

	if totalCells <= 0 {
		totalCells = CellsPerExtBlob
	}

	// Validate and deduplicate samples.
	seen := make(map[uint64]bool, len(samples))
	var cells []Cell
	var indices []uint64

	for i := range samples {
		if samples[i].CellIndex >= uint64(totalCells) {
			br.Metrics.FailureCount.Add(1)
			latency := time.Since(start).Nanoseconds()
			br.Metrics.TotalLatencyNs.Add(latency)
			br.Metrics.LastLatencyNs.Store(latency)
			return nil, fmt.Errorf("%w: cell index %d >= %d",
				ErrInvalidSampleIndex, samples[i].CellIndex, totalCells)
		}
		if seen[samples[i].CellIndex] {
			continue // Skip duplicates.
		}
		seen[samples[i].CellIndex] = true
		cells = append(cells, samples[i].Data)
		indices = append(indices, samples[i].CellIndex)
	}

	if len(cells) < ReconstructionThreshold {
		br.Metrics.FailureCount.Add(1)
		br.Metrics.InsufficientSamples.Add(1)
		latency := time.Since(start).Nanoseconds()
		br.Metrics.TotalLatencyNs.Add(latency)
		br.Metrics.LastLatencyNs.Store(latency)
		return nil, fmt.Errorf("%w: have %d unique cells, need %d",
			ErrInsufficientCells, len(cells), ReconstructionThreshold)
	}

	// Delegate to the low-level ReconstructBlob from reconstruction.go.
	result, err := ReconstructBlob(cells, indices)

	latency := time.Since(start).Nanoseconds()
	br.Metrics.TotalLatencyNs.Add(latency)
	br.Metrics.LastLatencyNs.Store(latency)

	if err != nil {
		br.Metrics.FailureCount.Add(1)
		return nil, fmt.Errorf("%w: %v", ErrReconstructionFailed, err)
	}

	br.Metrics.SuccessCount.Add(1)
	br.Metrics.BlobsReconstructed.Add(1)
	return result, nil
}

// ReconstructBlobs performs parallel reconstruction of multiple blobs from
// the pending sample set. It returns a map of blob index to recovered blob
// data. Blobs without sufficient samples are skipped (not returned).
func (br *BlobReconstructor) ReconstructBlobs(blobCount int) (map[uint64][]byte, error) {
	if blobCount <= 0 {
		return nil, fmt.Errorf("das: invalid blob count %d", blobCount)
	}

	br.mu.RLock()
	// Take a snapshot of pending samples.
	snapshot := make(map[uint64][]Sample, len(br.pending))
	for idx, samples := range br.pending {
		if idx >= uint64(blobCount) {
			continue
		}
		cp := make([]Sample, len(samples))
		copy(cp, samples)
		snapshot[idx] = cp
	}
	br.mu.RUnlock()

	type result struct {
		blobIndex uint64
		data      []byte
		err       error
	}

	var wg sync.WaitGroup
	results := make(chan result, len(snapshot))

	for blobIdx, samples := range snapshot {
		if len(samples) < ReconstructionThreshold {
			br.Metrics.InsufficientSamples.Add(1)
			continue
		}

		wg.Add(1)
		go func(idx uint64, s []Sample) {
			defer wg.Done()
			data, err := br.Reconstruct(s, CellsPerExtBlob)
			results <- result{blobIndex: idx, data: data, err: err}
		}(blobIdx, samples)
	}

	// Close channel after all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	blobs := make(map[uint64][]byte)
	var firstErr error
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("blob %d: %w", r.blobIndex, r.err)
			}
			continue
		}
		blobs[r.blobIndex] = r.data
	}

	return blobs, firstErr
}

// ReconstructPending attempts reconstruction for all blobs that have
// sufficient samples in the pending set. Successfully reconstructed
// blobs are removed from the pending set.
func (br *BlobReconstructor) ReconstructPending() (map[uint64][]byte, error) {
	ready := br.ReadyBlobs()
	if len(ready) == 0 {
		return nil, nil
	}

	// Find the highest blob index to determine blob count.
	maxIdx := uint64(0)
	for _, idx := range ready {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	blobs, err := br.ReconstructBlobs(int(maxIdx) + 1)

	// Remove successfully reconstructed blobs from pending.
	br.mu.Lock()
	for idx := range blobs {
		delete(br.pending, idx)
	}
	br.mu.Unlock()

	return blobs, err
}

// Reset clears all pending samples and resets the state for a new block.
func (br *BlobReconstructor) Reset() {
	br.mu.Lock()
	br.pending = make(map[uint64][]Sample)
	br.mu.Unlock()
}

// PendingBlobCount returns the number of blobs with at least one sample.
func (br *BlobReconstructor) PendingBlobCount() int {
	br.mu.RLock()
	defer br.mu.RUnlock()
	return len(br.pending)
}

// Status returns a summary of the current reconstruction state.
type ReconstructionStatus struct {
	PendingBlobs int
	ReadyBlobs   int
	TotalSamples int
}

// Status returns the current reconstruction status.
func (br *BlobReconstructor) Status() ReconstructionStatus {
	br.mu.RLock()
	defer br.mu.RUnlock()

	status := ReconstructionStatus{
		PendingBlobs: len(br.pending),
	}
	for _, samples := range br.pending {
		status.TotalSamples += len(samples)
		if len(samples) >= ReconstructionThreshold {
			status.ReadyBlobs++
		}
	}
	return status
}
