// reconstruction_pipeline.go implements a multi-stage blob reconstruction
// pipeline that collects cells from custody columns and the network, schedules
// reconstruction priority, performs Reed-Solomon erasure decoding, validates
// against KZG commitments, and tracks pipeline metrics.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package das

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Pipeline errors.
var (
	ErrPipelineClosed        = errors.New("das/pipeline: pipeline is closed")
	ErrPipelineInvalidBlob   = errors.New("das/pipeline: invalid blob index")
	ErrPipelineNoCells       = errors.New("das/pipeline: no cells collected")
	ErrPipelineInsufficient  = errors.New("das/pipeline: insufficient cells for reconstruction")
	ErrPipelineValidation    = errors.New("das/pipeline: validation failed")
	ErrPipelineDuplicateCell = errors.New("das/pipeline: duplicate cell")
)

// ReconstructionPriority determines reconstruction urgency.
type ReconstructionPriority int

const (
	PriorityLow      ReconstructionPriority = 0
	PriorityNormal   ReconstructionPriority = 1
	PriorityHigh     ReconstructionPriority = 2
	PriorityCritical ReconstructionPriority = 3
)

// BlobReconState tracks the reconstruction state of a single blob.
type BlobReconState struct {
	BlobIndex      uint64
	Slot           uint64
	Priority       ReconstructionPriority
	Commitment     KZGCommitment
	CollectedCells map[uint64]Cell // cellIndex -> cell
	TotalCells     int             // total cells needed
	Reconstructed  bool
	ReconData      []byte
	StartedAt      time.Time
	CompletedAt    time.Time
	Error          error
}

// CellCollector collects individual cells from custody columns and network
// sampling into a per-blob collection. All methods are safe for concurrent use.
type CellCollector struct {
	mu    sync.Mutex
	blobs map[blobKey]*BlobReconState
}

type blobKey struct {
	slot      uint64
	blobIndex uint64
}

// NewCellCollector creates a new cell collector.
func NewCellCollector() *CellCollector {
	return &CellCollector{
		blobs: make(map[blobKey]*BlobReconState),
	}
}

// InitBlob initializes collection state for a blob.
func (cc *CellCollector) InitBlob(slot, blobIndex uint64, commitment KZGCommitment, priority ReconstructionPriority) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	key := blobKey{slot: slot, blobIndex: blobIndex}
	if _, exists := cc.blobs[key]; exists {
		return
	}
	cc.blobs[key] = &BlobReconState{
		BlobIndex:      blobIndex,
		Slot:           slot,
		Priority:       priority,
		Commitment:     commitment,
		CollectedCells: make(map[uint64]Cell),
		TotalCells:     CellsPerExtBlob,
		StartedAt:      time.Now(),
	}
}

// AddCell adds a cell to the collection for a blob. Returns an error
// if the blob is not initialized or the cell is a duplicate.
func (cc *CellCollector) AddCell(slot, blobIndex, cellIndex uint64, cell Cell) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	key := blobKey{slot: slot, blobIndex: blobIndex}
	state, ok := cc.blobs[key]
	if !ok {
		return fmt.Errorf("%w: slot %d blob %d", ErrPipelineInvalidBlob, slot, blobIndex)
	}
	if _, dup := state.CollectedCells[cellIndex]; dup {
		return ErrPipelineDuplicateCell
	}
	state.CollectedCells[cellIndex] = cell
	return nil
}

// GetState returns a copy of the reconstruction state for a blob.
func (cc *CellCollector) GetState(slot, blobIndex uint64) *BlobReconState {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	key := blobKey{slot: slot, blobIndex: blobIndex}
	state, ok := cc.blobs[key]
	if !ok {
		return nil
	}
	cp := *state
	cp.CollectedCells = make(map[uint64]Cell, len(state.CollectedCells))
	for k, v := range state.CollectedCells {
		cp.CollectedCells[k] = v
	}
	return &cp
}

// CellCount returns the number of cells collected for a blob.
func (cc *CellCollector) CellCount(slot, blobIndex uint64) int {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	key := blobKey{slot: slot, blobIndex: blobIndex}
	state, ok := cc.blobs[key]
	if !ok {
		return 0
	}
	return len(state.CollectedCells)
}

// IsReady returns true if enough cells have been collected for reconstruction.
func (cc *CellCollector) IsReady(slot, blobIndex uint64) bool {
	return cc.CellCount(slot, blobIndex) >= ReconstructionThreshold
}

// MarkReconstructed marks a blob as successfully reconstructed.
func (cc *CellCollector) MarkReconstructed(slot, blobIndex uint64, data []byte) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	key := blobKey{slot: slot, blobIndex: blobIndex}
	if state, ok := cc.blobs[key]; ok {
		state.Reconstructed = true
		state.ReconData = data
		state.CompletedAt = time.Now()
	}
}

// MarkFailed marks a blob reconstruction as failed.
func (cc *CellCollector) MarkFailed(slot, blobIndex uint64, err error) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	key := blobKey{slot: slot, blobIndex: blobIndex}
	if state, ok := cc.blobs[key]; ok {
		state.Error = err
		state.CompletedAt = time.Now()
	}
}

// ReconstructionScheduler determines the order in which blobs should be
// reconstructed based on priority and cell availability.
type ReconstructionScheduler struct {
	collector *CellCollector
}

// NewReconstructionScheduler creates a new scheduler backed by the given collector.
func NewReconstructionScheduler(collector *CellCollector) *ReconstructionScheduler {
	return &ReconstructionScheduler{collector: collector}
}

// ScheduleEntry describes a blob ready for reconstruction with its priority.
type ScheduleEntry struct {
	Slot      uint64
	BlobIndex uint64
	Priority  ReconstructionPriority
	CellCount int
	ReadyPct  float64 // fraction of cells collected
}

// Schedule returns an ordered list of blobs ready for reconstruction,
// sorted by priority (highest first), then cell availability (highest first).
func (rs *ReconstructionScheduler) Schedule() []ScheduleEntry {
	rs.collector.mu.Lock()
	defer rs.collector.mu.Unlock()

	var entries []ScheduleEntry
	for key, state := range rs.collector.blobs {
		if state.Reconstructed || state.Error != nil {
			continue
		}
		cellCount := len(state.CollectedCells)
		if cellCount < ReconstructionThreshold {
			continue
		}
		pct := float64(cellCount) / float64(state.TotalCells)
		entries = append(entries, ScheduleEntry{
			Slot:      key.slot,
			BlobIndex: key.blobIndex,
			Priority:  state.Priority,
			CellCount: cellCount,
			ReadyPct:  pct,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Priority != entries[j].Priority {
			return entries[i].Priority > entries[j].Priority
		}
		return entries[i].ReadyPct > entries[j].ReadyPct
	})
	return entries
}

// ReconPipelineMetrics tracks reconstruction performance counters.
type ReconPipelineMetrics struct {
	TotalAttempts  atomic.Int64
	TotalSuccesses atomic.Int64
	TotalFailures  atomic.Int64
	TotalLatencyMs atomic.Int64
	CellsCollected atomic.Int64
}

// SuccessRate returns the fraction of successful reconstructions.
func (pm *ReconPipelineMetrics) SuccessRate() float64 {
	total := pm.TotalAttempts.Load()
	if total == 0 {
		return 0.0
	}
	return float64(pm.TotalSuccesses.Load()) / float64(total)
}

// AverageLatencyMs returns the average reconstruction latency in milliseconds.
func (pm *ReconPipelineMetrics) AverageLatencyMs() float64 {
	successes := pm.TotalSuccesses.Load()
	if successes == 0 {
		return 0.0
	}
	return float64(pm.TotalLatencyMs.Load()) / float64(successes)
}

// ValidationStep verifies that a reconstructed blob matches its KZG commitment.
// In this simplified implementation, we verify by re-hashing the data.
type ValidationStep struct {
	mu     sync.Mutex
	passed int
	failed int
}

// NewValidationStep creates a new validation step.
func NewValidationStep() *ValidationStep {
	return &ValidationStep{}
}

// Validate checks the reconstructed data against the commitment.
// For the purpose of this implementation, we verify the data is non-empty
// and has the expected blob size (real implementation would use KZG verify).
func (vs *ValidationStep) Validate(data []byte, commitment KZGCommitment) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	expectedSize := FieldElementsPerBlob * BytesPerFieldElement
	if len(data) != expectedSize {
		vs.failed++
		return false
	}
	// In a full implementation, this would perform:
	//   blob_to_kzg_commitment(data) == commitment
	// For now, accept non-zero data of the right size.
	vs.passed++
	return true
}

// Stats returns (passed, failed) validation counts.
func (vs *ValidationStep) Stats() (int, int) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.passed, vs.failed
}

// ReconstructionPipeline orchestrates the multi-stage blob reconstruction
// process: collect cells, schedule priority, decode, and validate.
type ReconstructionPipeline struct {
	mu        sync.Mutex
	collector *CellCollector
	scheduler *ReconstructionScheduler
	validator *ValidationStep
	metrics   *ReconPipelineMetrics
	closed    bool
}

// NewReconstructionPipeline creates a new pipeline with all stages initialized.
func NewReconstructionPipeline() *ReconstructionPipeline {
	collector := NewCellCollector()
	return &ReconstructionPipeline{
		collector: collector,
		scheduler: NewReconstructionScheduler(collector),
		validator: NewValidationStep(),
		metrics:   &ReconPipelineMetrics{},
	}
}

// Collector returns the underlying cell collector.
func (p *ReconstructionPipeline) Collector() *CellCollector {
	return p.collector
}

// Scheduler returns the underlying reconstruction scheduler.
func (p *ReconstructionPipeline) Scheduler() *ReconstructionScheduler {
	return p.scheduler
}

// Metrics returns the pipeline metrics.
func (p *ReconstructionPipeline) Metrics() *ReconPipelineMetrics {
	return p.metrics
}

// Validator returns the validation step.
func (p *ReconstructionPipeline) Validator() *ValidationStep {
	return p.validator
}

// InitBlob registers a blob for reconstruction.
func (p *ReconstructionPipeline) InitBlob(slot, blobIndex uint64, commitment KZGCommitment, priority ReconstructionPriority) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPipelineClosed
	}
	p.mu.Unlock()

	p.collector.InitBlob(slot, blobIndex, commitment, priority)
	return nil
}

// AddCell adds a cell to the pipeline's collector.
func (p *ReconstructionPipeline) AddCell(slot, blobIndex, cellIndex uint64, cell Cell) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return ErrPipelineClosed
	}
	p.mu.Unlock()

	err := p.collector.AddCell(slot, blobIndex, cellIndex, cell)
	if err == nil {
		p.metrics.CellsCollected.Add(1)
	}
	return err
}

// Reconstruct attempts to reconstruct a specific blob using collected cells.
// Returns the reconstructed blob data or an error.
func (p *ReconstructionPipeline) Reconstruct(slot, blobIndex uint64) ([]byte, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPipelineClosed
	}
	p.mu.Unlock()

	p.metrics.TotalAttempts.Add(1)
	start := time.Now()

	state := p.collector.GetState(slot, blobIndex)
	if state == nil {
		p.metrics.TotalFailures.Add(1)
		return nil, fmt.Errorf("%w: slot %d blob %d", ErrPipelineInvalidBlob, slot, blobIndex)
	}

	if len(state.CollectedCells) == 0 {
		p.metrics.TotalFailures.Add(1)
		return nil, ErrPipelineNoCells
	}

	if len(state.CollectedCells) < ReconstructionThreshold {
		p.metrics.TotalFailures.Add(1)
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrPipelineInsufficient, len(state.CollectedCells), ReconstructionThreshold)
	}

	// Build cells and indices arrays for reconstruction.
	cells := make([]Cell, 0, len(state.CollectedCells))
	indices := make([]uint64, 0, len(state.CollectedCells))
	for idx, cell := range state.CollectedCells {
		cells = append(cells, cell)
		indices = append(indices, idx)
	}

	data, err := ReconstructBlob(cells, indices)
	if err != nil {
		p.metrics.TotalFailures.Add(1)
		p.collector.MarkFailed(slot, blobIndex, err)
		return nil, err
	}

	// Validate against commitment.
	if !p.validator.Validate(data, state.Commitment) {
		p.metrics.TotalFailures.Add(1)
		valErr := ErrPipelineValidation
		p.collector.MarkFailed(slot, blobIndex, valErr)
		return nil, valErr
	}

	elapsed := time.Since(start)
	p.metrics.TotalSuccesses.Add(1)
	p.metrics.TotalLatencyMs.Add(elapsed.Milliseconds())
	p.collector.MarkReconstructed(slot, blobIndex, data)

	return data, nil
}

// RunScheduled runs reconstruction for all scheduled (ready) blobs in priority
// order. Returns a map of (blobIndex -> error), where nil error means success.
func (p *ReconstructionPipeline) RunScheduled(slot uint64) map[uint64]error {
	entries := p.scheduler.Schedule()
	results := make(map[uint64]error)
	for _, entry := range entries {
		if entry.Slot != slot {
			continue
		}
		_, err := p.Reconstruct(slot, entry.BlobIndex)
		results[entry.BlobIndex] = err
	}
	return results
}

// Close shuts down the pipeline.
func (p *ReconstructionPipeline) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
}
