// proof_queue.go implements an async proof validation queue with worker pool
// and priority scheduling for the mandatory 3-of-5 proof requirement (K+ spec).
// The queue accepts proof submissions, validates them concurrently, and tracks
// which blocks have met the mandatory proof threshold.
package proofs

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/metrics"
)

// Queue-specific proof type constants. These represent the 5 proof categories
// required by the mandatory 3-of-5 rule.
type QueueProofType uint8

const (
	StateProof     QueueProofType = iota // Account/balance state proof
	StorageProof                         // Storage trie proof
	ExecutionTrace                       // Full execution trace proof
	WitnessProof                         // Stateless witness proof
	ReceiptProof                         // Receipt trie proof
)

// String returns the name of the proof type.
func (pt QueueProofType) String() string {
	switch pt {
	case StateProof:
		return "StateProof"
	case StorageProof:
		return "StorageProof"
	case ExecutionTrace:
		return "ExecutionProof"
	case WitnessProof:
		return "WitnessProof"
	case ReceiptProof:
		return "ReceiptProof"
	default:
		return "UnknownProof"
	}
}

// AllQueueProofTypes lists all 5 proof types for the mandatory requirement.
var AllQueueProofTypes = []QueueProofType{
	StateProof, StorageProof, ExecutionTrace, WitnessProof, ReceiptProof,
}

// MandatoryThreshold is the number of distinct proof types required (3 of 5).
const MandatoryThreshold = 3

// Queue errors.
var (
	ErrQueueClosed           = errors.New("proof_queue: queue is closed")
	ErrQueueFull             = errors.New("proof_queue: queue is full")
	ErrProofDataEmpty        = errors.New("proof_queue: proof data is empty")
	ErrBlockHashZero         = errors.New("proof_queue: block hash is zero")
	ErrDeadlineExceeded      = errors.New("proof_queue: proof deadline exceeded")
	ErrInvalidQueueProofType = errors.New("proof_queue: invalid proof type")
)

// ProofResult is the outcome of validating a single proof.
type ProofResult struct {
	// BlockHash identifies the block this proof covers.
	BlockHash [32]byte

	// ProofType identifies what kind of proof was validated.
	ProofType QueueProofType

	// IsValid indicates whether the proof passed validation.
	IsValid bool

	// Duration is how long validation took.
	Duration time.Duration

	// Error is set if validation encountered an error (distinct from invalid proof).
	Error error
}

// ProofQueueConfig configures the proof validation queue.
type ProofQueueConfig struct {
	// Workers is the number of concurrent validation workers.
	Workers int

	// QueueSize is the maximum number of pending proof submissions.
	QueueSize int

	// DefaultDeadline is the default time allowed for proof validation.
	DefaultDeadline time.Duration
}

// DefaultProofQueueConfig returns a config with sensible defaults.
func DefaultProofQueueConfig() ProofQueueConfig {
	return ProofQueueConfig{
		Workers:         4,
		QueueSize:       1024,
		DefaultDeadline: 30 * time.Second,
	}
}

// proofJob is an internal work item for the worker pool.
type proofJob struct {
	blockHash [32]byte
	proofType QueueProofType
	data      []byte
	result    chan ProofResult
	deadline  time.Time
}

// ProofQueue is an async proof validation queue with a bounded worker pool.
// Proofs are submitted via Submit() and validated concurrently by workers.
// Thread-safe.
type ProofQueue struct {
	config  ProofQueueConfig
	jobs    chan proofJob
	closed  atomic.Bool
	wg      sync.WaitGroup
	tracker *MandatoryProofTracker

	// Metrics.
	proofsValidated *metrics.Counter
	proofsFailed    *metrics.Counter
	proofsTimedOut  *metrics.Counter
}

// NewProofQueue creates and starts a new proof validation queue.
func NewProofQueue(config ProofQueueConfig) *ProofQueue {
	if config.Workers <= 0 {
		config.Workers = 4
	}
	if config.QueueSize <= 0 {
		config.QueueSize = 1024
	}
	if config.DefaultDeadline <= 0 {
		config.DefaultDeadline = 30 * time.Second
	}

	q := &ProofQueue{
		config:          config,
		jobs:            make(chan proofJob, config.QueueSize),
		tracker:         NewMandatoryProofTracker(),
		proofsValidated: metrics.NewCounter("proofs_validated"),
		proofsFailed:    metrics.NewCounter("proofs_failed"),
		proofsTimedOut:  metrics.NewCounter("proofs_timed_out"),
	}

	// Start worker goroutines.
	q.wg.Add(config.Workers)
	for i := 0; i < config.Workers; i++ {
		go q.worker()
	}

	return q
}

// Submit enqueues a proof for async validation and returns a channel that
// will receive the validation result. The channel is buffered (size 1) so
// the caller does not need to read it immediately.
func (q *ProofQueue) Submit(blockHash [32]byte, proofType QueueProofType, proof []byte) (<-chan ProofResult, error) {
	if q.closed.Load() {
		return nil, ErrQueueClosed
	}
	if blockHash == ([32]byte{}) {
		return nil, ErrBlockHashZero
	}
	if len(proof) == 0 {
		return nil, ErrProofDataEmpty
	}
	if proofType > ReceiptProof {
		return nil, ErrInvalidQueueProofType
	}

	resultCh := make(chan ProofResult, 1)
	job := proofJob{
		blockHash: blockHash,
		proofType: proofType,
		data:      append([]byte(nil), proof...),
		result:    resultCh,
		deadline:  time.Now().Add(q.config.DefaultDeadline),
	}

	select {
	case q.jobs <- job:
		return resultCh, nil
	default:
		return nil, ErrQueueFull
	}
}

// Close shuts down the queue and waits for all workers to finish.
func (q *ProofQueue) Close() {
	if q.closed.CompareAndSwap(false, true) {
		close(q.jobs)
		q.wg.Wait()
	}
}

// Tracker returns the mandatory proof tracker for querying block proof status.
func (q *ProofQueue) Tracker() *MandatoryProofTracker {
	return q.tracker
}

// Metrics returns the current metric values: validated, failed, timed_out.
func (q *ProofQueue) Metrics() (validated, failed, timedOut int64) {
	return q.proofsValidated.Value(), q.proofsFailed.Value(), q.proofsTimedOut.Value()
}

// worker is the main loop for a validation worker goroutine.
func (q *ProofQueue) worker() {
	defer q.wg.Done()

	for job := range q.jobs {
		start := time.Now()

		// Check deadline before starting validation.
		if time.Now().After(job.deadline) {
			q.proofsTimedOut.Inc()
			job.result <- ProofResult{
				BlockHash: job.blockHash,
				ProofType: job.proofType,
				IsValid:   false,
				Duration:  time.Since(start),
				Error:     ErrDeadlineExceeded,
			}
			continue
		}

		// Validate the proof.
		valid := validateProof(job.blockHash, job.proofType, job.data)
		duration := time.Since(start)

		if valid {
			q.proofsValidated.Inc()
			// Record in the mandatory proof tracker.
			q.tracker.RecordProof(job.blockHash, job.proofType)
		} else {
			q.proofsFailed.Inc()
		}

		job.result <- ProofResult{
			BlockHash: job.blockHash,
			ProofType: job.proofType,
			IsValid:   valid,
			Duration:  duration,
			Error:     nil,
		}
	}
}

// validateProof performs the actual proof validation. The proof is valid if
// H(blockHash || proofType || data) has its first byte matching
// (proofType XOR len(data)%256). This allows deterministic test construction
// while simulating real cryptographic verification.
func validateProof(blockHash [32]byte, proofType QueueProofType, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	h := crypto.Keccak256(blockHash[:], []byte{byte(proofType)}, data)
	expected := byte(proofType) ^ byte(len(data)%256)
	return h[0] == expected
}

// MandatoryProofTracker tracks which proof types have been received and
// validated for each block. It determines whether the mandatory 3-of-5
// threshold has been met.
type MandatoryProofTracker struct {
	mu     sync.RWMutex
	blocks map[[32]byte]*blockProofRecord
}

// blockProofRecord holds the set of validated proof types for a single block.
type blockProofRecord struct {
	proofTypes map[QueueProofType]bool
	firstSeen  time.Time
}

// NewMandatoryProofTracker creates a new tracker.
func NewMandatoryProofTracker() *MandatoryProofTracker {
	return &MandatoryProofTracker{
		blocks: make(map[[32]byte]*blockProofRecord),
	}
}

// RecordProof records that a proof of the given type has been validated
// for the specified block.
func (t *MandatoryProofTracker) RecordProof(blockHash [32]byte, proofType QueueProofType) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record, ok := t.blocks[blockHash]
	if !ok {
		record = &blockProofRecord{
			proofTypes: make(map[QueueProofType]bool),
			firstSeen:  time.Now(),
		}
		t.blocks[blockHash] = record
	}
	record.proofTypes[proofType] = true
}

// HasMandatoryProofs returns true if the block has at least MandatoryThreshold
// (3) distinct proof types validated.
func (t *MandatoryProofTracker) HasMandatoryProofs(blockHash [32]byte) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, ok := t.blocks[blockHash]
	if !ok {
		return false
	}
	return len(record.proofTypes) >= MandatoryThreshold
}

// ProofCount returns the number of distinct validated proof types for a block.
func (t *MandatoryProofTracker) ProofCount(blockHash [32]byte) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, ok := t.blocks[blockHash]
	if !ok {
		return 0
	}
	return len(record.proofTypes)
}

// ValidatedTypes returns the list of proof types that have been validated
// for the given block.
func (t *MandatoryProofTracker) ValidatedTypes(blockHash [32]byte) []QueueProofType {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record, ok := t.blocks[blockHash]
	if !ok {
		return nil
	}

	result := make([]QueueProofType, 0, len(record.proofTypes))
	for pt := range record.proofTypes {
		result = append(result, pt)
	}
	return result
}

// MissingTypes returns the proof types not yet validated for the given block.
func (t *MandatoryProofTracker) MissingTypes(blockHash [32]byte) []QueueProofType {
	t.mu.RLock()
	defer t.mu.RUnlock()

	record := t.blocks[blockHash]
	var missing []QueueProofType
	for _, pt := range AllQueueProofTypes {
		if record == nil || !record.proofTypes[pt] {
			missing = append(missing, pt)
		}
	}
	return missing
}

// ProofDeadline tracks proof submission deadlines per block.
type ProofDeadline struct {
	mu        sync.RWMutex
	deadlines map[[32]byte]time.Time
	duration  time.Duration
}

// NewProofDeadline creates a new deadline tracker with the given duration.
func NewProofDeadline(duration time.Duration) *ProofDeadline {
	if duration <= 0 {
		duration = 30 * time.Second
	}
	return &ProofDeadline{
		deadlines: make(map[[32]byte]time.Time),
		duration:  duration,
	}
}

// SetDeadline sets the proof submission deadline for a block.
// The deadline is calculated as now + configured duration.
func (d *ProofDeadline) SetDeadline(blockHash [32]byte) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadlines[blockHash] = time.Now().Add(d.duration)
}

// SetDeadlineAt sets a specific deadline time for a block.
func (d *ProofDeadline) SetDeadlineAt(blockHash [32]byte, deadline time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deadlines[blockHash] = deadline
}

// IsExpired returns true if the proof deadline for the block has passed.
func (d *ProofDeadline) IsExpired(blockHash [32]byte) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	deadline, ok := d.deadlines[blockHash]
	if !ok {
		return false // no deadline set means not expired
	}
	return time.Now().After(deadline)
}

// TimeRemaining returns the time remaining before the deadline expires.
// Returns zero if the deadline has passed or is not set.
func (d *ProofDeadline) TimeRemaining(blockHash [32]byte) time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()

	deadline, ok := d.deadlines[blockHash]
	if !ok {
		return 0
	}
	remaining := time.Until(deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Prune removes deadlines for blocks that have expired beyond a grace period.
func (d *ProofDeadline) Prune(gracePeriod time.Duration) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-gracePeriod)
	pruned := 0
	for hash, deadline := range d.deadlines {
		if deadline.Before(cutoff) {
			delete(d.deadlines, hash)
			pruned++
		}
	}
	return pruned
}

// Count returns the number of tracked deadlines.
func (d *ProofDeadline) Count() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.deadlines)
}

// MakeValidProof constructs proof data that will pass validateProof for the
// given block hash and proof type. This is a test helper.
func MakeValidProof(blockHash [32]byte, proofType QueueProofType) []byte {
	// We need data such that:
	// H(blockHash || byte(proofType) || data)[0] == byte(proofType) ^ byte(len(data)%256)
	// Try different data payloads.
	base := crypto.Keccak256(blockHash[:], []byte{byte(proofType)})
	for nonce := 0; nonce < 65536; nonce++ {
		data := make([]byte, len(base)+2)
		copy(data, base)
		data[len(base)] = byte(nonce >> 8)
		data[len(base)+1] = byte(nonce)
		h := crypto.Keccak256(blockHash[:], []byte{byte(proofType)}, data)
		expected := byte(proofType) ^ byte(len(data)%256)
		if h[0] == expected {
			return data
		}
	}
	return base // fallback
}
