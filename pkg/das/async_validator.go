// Package das - async_validator.go implements an asynchronous DAS proof
// validation engine with a bounded worker pool, priority queue, timeout
// handling, and metrics tracking.
//
// The AsyncValidator accepts proof validation requests via SubmitProof, which
// returns a channel that will receive the ValidationResult when processing
// completes. Proofs are prioritized: custody proofs take precedence over
// random sampling proofs. A configurable timeout ensures that proofs taking
// too long are marked invalid rather than blocking the pipeline.
//
// Reference: EIP-7594 PeerDAS specification
package das

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Async validator errors.
var (
	ErrValidatorStopped = errors.New("das/async_validator: validator stopped")
	ErrProofTimeout     = errors.New("das/async_validator: proof validation timed out")
	ErrNilProof         = errors.New("das/async_validator: nil proof")
	ErrQueueFull        = errors.New("das/async_validator: work queue is full")
)

// ProofPriority indicates the priority level of a DAS proof.
type ProofPriority int

const (
	// PriorityCustody is the highest priority: custody proofs must be
	// validated promptly to respond to challenges.
	PriorityCustody ProofPriority = iota

	// PriorityRandom is lower priority: random sampling proofs for
	// data availability verification.
	PriorityRandom
)

// DASProof encapsulates a data availability proof to be validated.
type DASProof struct {
	// Slot is the slot for which this proof is relevant.
	Slot uint64

	// ColumnIndex is the column in the extended data matrix.
	ColumnIndex uint64

	// Data is the column data being proven.
	Data []byte

	// Proof is the proof bytes (e.g., keccak256 commitment).
	Proof []byte

	// Priority determines queue ordering.
	Priority ProofPriority
}

// ValidationResult holds the outcome of an async proof validation.
type ValidationResult struct {
	// IsValid is true if the proof passed validation.
	IsValid bool

	// Error is non-nil if validation failed or timed out.
	Error error

	// Duration is how long validation took.
	Duration time.Duration

	// BlobIdx is the blob/column index that was validated.
	BlobIdx int

	// Priority is the priority of the validated proof.
	Priority ProofPriority
}

// AsyncValidatorConfig configures the async validator.
type AsyncValidatorConfig struct {
	// Workers is the number of concurrent validation workers.
	Workers int

	// QueueSize is the maximum number of pending proof validations.
	QueueSize int

	// Timeout is the maximum duration for a single proof validation.
	Timeout time.Duration

	// ColumnCount is the total number of columns for validation range checks.
	ColumnCount int
}

// DefaultAsyncValidatorConfig returns a default configuration.
func DefaultAsyncValidatorConfig() AsyncValidatorConfig {
	return AsyncValidatorConfig{
		Workers:     4,
		QueueSize:   256,
		Timeout:     5 * time.Second,
		ColumnCount: NumberOfColumns,
	}
}

// ValidatorMetrics tracks async validation statistics.
type ValidatorMetrics struct {
	Submitted       atomic.Int64
	Validated       atomic.Int64
	Succeeded       atomic.Int64
	Failed          atomic.Int64
	Timeouts        atomic.Int64
	QueueDepth      atomic.Int64
	TotalLatencyNs  atomic.Int64
	CustodyProofs   atomic.Int64
	RandomProofs    atomic.Int64
}

// AvgLatencyMs returns the average validation latency in milliseconds.
func (m *ValidatorMetrics) AvgLatencyMs() float64 {
	count := m.Validated.Load()
	if count == 0 {
		return 0
	}
	return float64(m.TotalLatencyNs.Load()) / float64(count) / 1e6
}

// SuccessRate returns the fraction of successful validations.
func (m *ValidatorMetrics) SuccessRate() float64 {
	total := m.Validated.Load()
	if total == 0 {
		return 0
	}
	return float64(m.Succeeded.Load()) / float64(total)
}

// workItem is an internal work queue entry.
type workItem struct {
	blobIdx  int
	proof    DASProof
	resultCh chan ValidationResult
}

// AsyncValidator validates DAS proofs asynchronously using a bounded worker
// pool with priority scheduling and timeout handling.
type AsyncValidator struct {
	config  AsyncValidatorConfig
	cancel  context.CancelFunc
	ctx     context.Context
	wg      sync.WaitGroup
	stopped atomic.Bool

	// Two priority channels: custody (high) and random (low).
	custodyQueue chan workItem
	randomQueue  chan workItem

	// Metrics for monitoring.
	Metrics ValidatorMetrics
}

// NewAsyncValidator creates a new async validator with the given configuration.
func NewAsyncValidator(config AsyncValidatorConfig) *AsyncValidator {
	if config.Workers <= 0 {
		config.Workers = 1
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 64
	}
	if config.Timeout <= 0 {
		config.Timeout = 5 * time.Second
	}
	if config.ColumnCount <= 0 {
		config.ColumnCount = NumberOfColumns
	}

	ctx, cancel := context.WithCancel(context.Background())

	av := &AsyncValidator{
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		custodyQueue: make(chan workItem, config.QueueSize),
		randomQueue:  make(chan workItem, config.QueueSize),
	}

	// Start worker goroutines.
	for i := 0; i < config.Workers; i++ {
		av.wg.Add(1)
		go av.worker()
	}

	return av
}

// SubmitProof submits a DAS proof for async validation. Returns a channel
// that will receive the ValidationResult when processing completes.
// Returns an error if the validator is stopped or the queue is full.
func (av *AsyncValidator) SubmitProof(blobIdx int, proof DASProof) (<-chan ValidationResult, error) {
	if av.stopped.Load() {
		return nil, ErrValidatorStopped
	}

	resultCh := make(chan ValidationResult, 1)
	item := workItem{
		blobIdx:  blobIdx,
		proof:    proof,
		resultCh: resultCh,
	}

	av.Metrics.Submitted.Add(1)

	// Route to priority queue.
	switch proof.Priority {
	case PriorityCustody:
		av.Metrics.CustodyProofs.Add(1)
		select {
		case av.custodyQueue <- item:
			av.Metrics.QueueDepth.Add(1)
		default:
			return nil, ErrQueueFull
		}
	default:
		av.Metrics.RandomProofs.Add(1)
		select {
		case av.randomQueue <- item:
			av.Metrics.QueueDepth.Add(1)
		default:
			return nil, ErrQueueFull
		}
	}

	return resultCh, nil
}

// Stop gracefully shuts down the validator, waiting for all workers to finish.
func (av *AsyncValidator) Stop() {
	if av.stopped.Swap(true) {
		return // already stopped
	}
	av.cancel()
	av.wg.Wait()
}

// IsStopped returns whether the validator has been stopped.
func (av *AsyncValidator) IsStopped() bool {
	return av.stopped.Load()
}

// QueueDepth returns the current number of pending items across both queues.
func (av *AsyncValidator) QueueDepth() int {
	return int(av.Metrics.QueueDepth.Load())
}

// worker is the main loop for a validation worker goroutine.
// It prioritizes custody proofs over random proofs using a select
// with priority: first try custody queue, then fall back to random.
func (av *AsyncValidator) worker() {
	defer av.wg.Done()

	for {
		var item workItem
		var ok bool

		// Priority select: try custody first.
		select {
		case <-av.ctx.Done():
			// Drain remaining items with cancellation errors.
			av.drainQueue()
			return
		case item, ok = <-av.custodyQueue:
			if !ok {
				return
			}
		default:
			// No custody proofs pending; try either queue.
			select {
			case <-av.ctx.Done():
				av.drainQueue()
				return
			case item, ok = <-av.custodyQueue:
				if !ok {
					return
				}
			case item, ok = <-av.randomQueue:
				if !ok {
					return
				}
			}
		}

		av.Metrics.QueueDepth.Add(-1)
		av.processItem(item)
	}
}

// processItem validates a single work item with timeout handling.
func (av *AsyncValidator) processItem(item workItem) {
	start := time.Now()

	// Create a timeout context for this validation.
	ctx, cancel := context.WithTimeout(av.ctx, av.config.Timeout)
	defer cancel()

	resultCh := make(chan ValidationResult, 1)

	go func() {
		result := av.validateProof(item.blobIdx, item.proof)
		resultCh <- result
	}()

	var result ValidationResult

	select {
	case result = <-resultCh:
		// Validation completed.
	case <-ctx.Done():
		if av.ctx.Err() != nil {
			// Parent context cancelled (shutdown).
			result = ValidationResult{
				IsValid:  false,
				Error:    ErrValidatorStopped,
				BlobIdx:  item.blobIdx,
				Priority: item.proof.Priority,
			}
		} else {
			// Timeout.
			av.Metrics.Timeouts.Add(1)
			result = ValidationResult{
				IsValid:  false,
				Error:    ErrProofTimeout,
				BlobIdx:  item.blobIdx,
				Priority: item.proof.Priority,
			}
		}
	}

	result.Duration = time.Since(start)
	av.Metrics.TotalLatencyNs.Add(result.Duration.Nanoseconds())
	av.Metrics.Validated.Add(1)

	if result.IsValid {
		av.Metrics.Succeeded.Add(1)
	} else {
		av.Metrics.Failed.Add(1)
	}

	// Deliver result.
	item.resultCh <- result
	close(item.resultCh)
}

// validateProof performs the actual proof validation.
func (av *AsyncValidator) validateProof(blobIdx int, proof DASProof) ValidationResult {
	result := ValidationResult{
		BlobIdx:  blobIdx,
		Priority: proof.Priority,
	}

	// Validate column index range.
	if proof.ColumnIndex >= uint64(av.config.ColumnCount) {
		result.Error = ErrInvalidColumnIdx
		return result
	}

	// Validate data and proof are present.
	if len(proof.Data) == 0 {
		result.Error = ErrInvalidColumnProof
		return result
	}
	if len(proof.Proof) == 0 {
		result.Error = ErrInvalidColumnProof
		return result
	}

	// Compute expected proof: keccak256(slot || columnIndex || data).
	expected := computeColumnProof(proof.Slot, proof.ColumnIndex, proof.Data)
	if len(expected) != len(proof.Proof) {
		result.Error = ErrInvalidColumnProof
		return result
	}

	for i := range expected {
		if expected[i] != proof.Proof[i] {
			result.Error = ErrInvalidColumnProof
			return result
		}
	}

	result.IsValid = true
	return result
}

// drainQueue drains remaining items from both queues, sending cancellation
// results. Called during shutdown.
func (av *AsyncValidator) drainQueue() {
	for {
		select {
		case item := <-av.custodyQueue:
			av.Metrics.QueueDepth.Add(-1)
			item.resultCh <- ValidationResult{
				IsValid:  false,
				Error:    ErrValidatorStopped,
				BlobIdx:  item.blobIdx,
				Priority: item.proof.Priority,
			}
			close(item.resultCh)
		case item := <-av.randomQueue:
			av.Metrics.QueueDepth.Add(-1)
			item.resultCh <- ValidationResult{
				IsValid:  false,
				Error:    ErrValidatorStopped,
				BlobIdx:  item.blobIdx,
				Priority: item.proof.Priority,
			}
			close(item.resultCh)
		default:
			return
		}
	}
}
