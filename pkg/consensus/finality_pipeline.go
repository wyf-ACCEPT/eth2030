// finality_pipeline.go wires the EndgameEngine, BLSBackend (from
// crypto/bls_integration.go), and block execution together into a unified
// fast finality pipeline. It implements optimistic finality with real BLS
// signature verification, block execution triggering, EIP-8025 execution
// proof validation, and timing metrics.
//
// The pipeline supports two paths:
//   - Fast path (< 500ms): 2/3 votes collected quickly, finality declared
//     without execution proofs.
//   - Slow path (>= 500ms): execution proof required for finality.
//
// This addresses gap #6 (Fast L1 finality) by connecting real BLS verification
// and block execution to the endgame engine's vote collection.
package consensus

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// FP (Finality Pipeline) errors.
var (
	ErrFPNilConfig     = errors.New("finality_pipeline: nil config")
	ErrFPNilEngine     = errors.New("finality_pipeline: nil endgame engine")
	ErrFPNilBLS        = errors.New("finality_pipeline: nil BLS backend")
	ErrFPNilExecutor   = errors.New("finality_pipeline: nil block executor")
	ErrFPInvalidVote   = errors.New("finality_pipeline: invalid vote")
	ErrFPBLSFailed     = errors.New("finality_pipeline: BLS verification failed")
	ErrFPExecFailed    = errors.New("finality_pipeline: block execution failed")
	ErrFPProofFailed   = errors.New("finality_pipeline: execution proof validation failed")
	ErrFPSlotFinalized = errors.New("finality_pipeline: slot already finalized")
	ErrFPNoBlockRoot   = errors.New("finality_pipeline: vote has zero block root")
	ErrFPNilVote       = errors.New("finality_pipeline: nil vote")
	ErrFPStopped       = errors.New("finality_pipeline: pipeline is stopped")
)

// FPBlockExecutor is the interface for executing blocks upon finality.
// Implementations wire into the engine backend's ProcessBlock path.
type FPBlockExecutor interface {
	// ExecuteBlock runs block execution for the finalized block root.
	// Returns the post-execution state root and any error.
	ExecuteBlock(slot uint64, blockRoot types.Hash) (stateRoot types.Hash, err error)
}

// FPProofValidator validates EIP-8025 execution proofs. When a proof
// is available, it is verified against the expected state root.
type FPProofValidator interface {
	// ValidateProof checks an execution proof for the given block.
	// Returns true if the proof is valid.
	ValidateProof(blockRoot types.Hash, stateRoot types.Hash, proofData []byte) bool
}

// FPFinalityResult captures the outcome of the finality pipeline for a slot.
type FPFinalityResult struct {
	Slot           uint64
	BlockRoot      types.Hash
	FinalizedAt    time.Time
	ExecutionValid bool
	ProofValid     bool
	FastPath       bool // true if finalized within TargetFinalityMs
	StateRoot      types.Hash
	VoteCount      int
	TotalWeight    uint64
	Threshold      uint64
}

// FPTimingMetrics tracks latency statistics for the finality pipeline.
type FPTimingMetrics struct {
	VoteCollectionLatencyMs int64 // time from first vote to 2/3 threshold
	FinalityLatencyMs       int64 // time from first vote to finality declared
	ExecutionLatencyMs      int64 // time spent in block execution
	ProofLatencyMs          int64 // time spent in proof validation
	TotalLatencyMs          int64 // total pipeline latency
}

// FPConfig configures the finality pipeline.
type FPConfig struct {
	// TargetFinalityMs is the threshold for fast path (default 500ms).
	TargetFinalityMs int64

	// RequireProofOnSlowPath controls whether execution proofs are required
	// when finality exceeds TargetFinalityMs.
	RequireProofOnSlowPath bool

	// SkipExecution disables block execution (useful for testing).
	SkipExecution bool

	// MaxConcurrentSlots limits the number of slots being processed.
	MaxConcurrentSlots int
}

// DefaultFPConfig returns sensible defaults for the finality pipeline.
func DefaultFPConfig() *FPConfig {
	return &FPConfig{
		TargetFinalityMs:       500,
		RequireProofOnSlowPath: true,
		SkipExecution:          false,
		MaxConcurrentSlots:     64,
	}
}

// fpSlotState tracks per-slot pipeline state.
type fpSlotState struct {
	firstVoteTime time.Time
	thresholdTime time.Time
	finalizedTime time.Time
	voteCount     int
	totalWeight   uint64
	finalized     bool
	result        *FPFinalityResult
	metrics       *FPTimingMetrics
	verifiedVotes map[uint64]bool // validator index -> BLS verified
}

func newFPSlotState() *fpSlotState {
	return &fpSlotState{
		verifiedVotes: make(map[uint64]bool),
		metrics:       &FPTimingMetrics{},
	}
}

// FPVote extends EndgameVote with a BLS signature for verification.
type FPVote struct {
	Slot           uint64
	ValidatorIndex uint64
	BlockHash      types.Hash
	Weight         uint64
	Pubkey         [48]byte
	Signature      [96]byte
	// SigningData is the message that was signed (domain + slot + root).
	SigningData []byte
}

// FinalityPipeline orchestrates the full finality flow: vote collection with
// BLS verification, threshold detection, block execution, and proof validation.
// It uses the BLSBackend from crypto/bls_integration.go for signature ops.
// Thread-safe.
type FinalityPipeline struct {
	mu       sync.Mutex
	config   *FPConfig
	engine   *EndgameEngine
	bls      crypto.BLSBackend
	executor FPBlockExecutor
	prover   FPProofValidator

	slots   map[uint64]*fpSlotState
	results map[uint64]*FPFinalityResult

	stopped  atomic.Bool
	finCount atomic.Int64

	// Callback invoked when a slot is finalized.
	onFinality func(result *FPFinalityResult)
}

// NewFinalityPipeline creates a new finality pipeline. The BLS backend,
// endgame engine, and block executor are required. The execution proof
// validator is optional (nil disables proof checks).
func NewFinalityPipeline(
	config *FPConfig,
	engine *EndgameEngine,
	bls crypto.BLSBackend,
	executor FPBlockExecutor,
	prover FPProofValidator,
) (*FinalityPipeline, error) {
	if config == nil {
		return nil, ErrFPNilConfig
	}
	if engine == nil {
		return nil, ErrFPNilEngine
	}
	if bls == nil {
		return nil, ErrFPNilBLS
	}
	if executor == nil {
		return nil, ErrFPNilExecutor
	}

	return &FinalityPipeline{
		config:   config,
		engine:   engine,
		bls:      bls,
		executor: executor,
		prover:   prover,
		slots:    make(map[uint64]*fpSlotState),
		results:  make(map[uint64]*FPFinalityResult),
	}, nil
}

// SetOnFinality registers a callback invoked when a slot achieves finality.
func (p *FinalityPipeline) SetOnFinality(fn func(*FPFinalityResult)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onFinality = fn
}

// SubmitVote validates a vote's BLS signature, submits it to the endgame
// engine, and checks whether finality has been reached. If finality is
// achieved, it triggers block execution and optional proof validation.
func (p *FinalityPipeline) SubmitVote(vote *FPVote) (*FPFinalityResult, error) {
	if p.stopped.Load() {
		return nil, ErrFPStopped
	}
	if vote == nil {
		return nil, ErrFPNilVote
	}
	if vote.BlockHash == (types.Hash{}) {
		return nil, ErrFPNoBlockRoot
	}
	if vote.Weight == 0 {
		return nil, ErrFPInvalidVote
	}

	// Step 1: Verify BLS signature using the BLSBackend.
	if !p.verifyVoteBLS(vote) {
		return nil, ErrFPBLSFailed
	}

	p.mu.Lock()

	// Check if slot already finalized.
	if existing, ok := p.results[vote.Slot]; ok {
		p.mu.Unlock()
		return existing, ErrFPSlotFinalized
	}

	// Get or create slot state.
	ss := p.getOrCreateSlotState(vote.Slot)

	// Track first vote time.
	now := time.Now()
	if ss.firstVoteTime.IsZero() {
		ss.firstVoteTime = now
	}

	// Record BLS-verified vote.
	ss.verifiedVotes[vote.ValidatorIndex] = true
	ss.voteCount++
	ss.totalWeight += vote.Weight

	// Submit to endgame engine (which tracks thresholds).
	endgameVote := &EndgameVote{
		Slot:           vote.Slot,
		ValidatorIndex: vote.ValidatorIndex,
		BlockHash:      vote.BlockHash,
		Weight:         vote.Weight,
		Timestamp:      uint64(now.UnixMilli()),
	}

	p.mu.Unlock()

	err := p.engine.SubmitVote(endgameVote)
	if err != nil {
		return nil, err
	}

	// Step 2: Check if finality threshold reached.
	result := p.engine.CheckFinality(vote.Slot)
	if !result.IsFinalized {
		return nil, nil // not yet finalized
	}

	// Step 3: Finality reached -- run execution pipeline.
	return p.executeFinalityFlow(vote.Slot, result, now)
}

// executeFinalityFlow runs block execution and proof validation after
// the 2/3 threshold is reached.
func (p *FinalityPipeline) executeFinalityFlow(
	slot uint64,
	endgameResult *EndgameResult,
	thresholdTime time.Time,
) (*FPFinalityResult, error) {
	p.mu.Lock()
	// Double-check finality hasn't been recorded by another goroutine.
	if existing, ok := p.results[slot]; ok {
		p.mu.Unlock()
		return existing, nil
	}
	ss := p.getOrCreateSlotState(slot)
	ss.thresholdTime = thresholdTime
	p.mu.Unlock()

	blockRoot := endgameResult.FinalizedHash

	// Determine path type.
	voteLatencyMs := int64(0)
	if !ss.firstVoteTime.IsZero() {
		voteLatencyMs = time.Since(ss.firstVoteTime).Milliseconds()
	}
	isFastPath := voteLatencyMs < p.config.TargetFinalityMs

	// Step 3a: Execute block.
	var stateRoot types.Hash
	var execValid bool
	execStart := time.Now()
	if !p.config.SkipExecution {
		var execErr error
		stateRoot, execErr = p.executor.ExecuteBlock(slot, blockRoot)
		if execErr != nil {
			return nil, ErrFPExecFailed
		}
		execValid = true
	} else {
		execValid = true
	}
	execLatency := time.Since(execStart).Milliseconds()

	// Step 3b: Validate EIP-8025 execution proof.
	var proofValid bool
	proofStart := time.Now()
	if p.prover != nil && (!isFastPath || !p.config.RequireProofOnSlowPath) {
		proofValid = p.prover.ValidateProof(blockRoot, stateRoot, nil)
	} else if isFastPath {
		// Fast path: no proof needed.
		proofValid = true
	} else if p.prover == nil && p.config.RequireProofOnSlowPath {
		// Slow path with no prover: proof considered invalid.
		proofValid = false
	} else {
		proofValid = true
	}
	proofLatency := time.Since(proofStart).Milliseconds()

	// On slow path with RequireProofOnSlowPath, fail if proof invalid.
	if !isFastPath && p.config.RequireProofOnSlowPath && !proofValid {
		return nil, ErrFPProofFailed
	}

	// Record result.
	finalizedAt := time.Now()
	totalLatency := int64(0)
	if !ss.firstVoteTime.IsZero() {
		totalLatency = finalizedAt.Sub(ss.firstVoteTime).Milliseconds()
	}

	pResult := &FPFinalityResult{
		Slot:           slot,
		BlockRoot:      blockRoot,
		FinalizedAt:    finalizedAt,
		ExecutionValid: execValid,
		ProofValid:     proofValid,
		FastPath:       isFastPath,
		StateRoot:      stateRoot,
		VoteCount:      ss.voteCount,
		TotalWeight:    endgameResult.TotalWeight,
		Threshold:      endgameResult.Threshold,
	}

	metrics := &FPTimingMetrics{
		VoteCollectionLatencyMs: voteLatencyMs,
		FinalityLatencyMs:       totalLatency - execLatency - proofLatency,
		ExecutionLatencyMs:      execLatency,
		ProofLatencyMs:          proofLatency,
		TotalLatencyMs:          totalLatency,
	}

	p.mu.Lock()
	ss.finalized = true
	ss.finalizedTime = finalizedAt
	ss.result = pResult
	ss.metrics = metrics
	p.results[slot] = pResult
	p.mu.Unlock()

	p.finCount.Add(1)

	// Invoke callback.
	if p.onFinality != nil {
		p.onFinality(pResult)
	}

	return pResult, nil
}

// verifyVoteBLS checks the BLS signature on a pipeline vote using the
// configured BLSBackend.
func (p *FinalityPipeline) verifyVoteBLS(vote *FPVote) bool {
	if len(vote.SigningData) == 0 {
		return false
	}
	return p.bls.Verify(vote.Pubkey[:], vote.SigningData, vote.Signature[:])
}

// getOrCreateSlotState returns the pipeline state for a slot, creating it
// if needed. Caller must hold p.mu.
func (p *FinalityPipeline) getOrCreateSlotState(slot uint64) *fpSlotState {
	ss, ok := p.slots[slot]
	if !ok {
		ss = newFPSlotState()
		p.slots[slot] = ss
	}
	return ss
}

// GetFPResult returns the finality result for a slot, or nil if not finalized.
func (p *FinalityPipeline) GetFPResult(slot uint64) *FPFinalityResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.results[slot]
}

// GetFPMetrics returns the timing metrics for a finalized slot, or nil.
func (p *FinalityPipeline) GetFPMetrics(slot uint64) *FPTimingMetrics {
	p.mu.Lock()
	defer p.mu.Unlock()
	ss, ok := p.slots[slot]
	if !ok {
		return nil
	}
	return ss.metrics
}

// FPFinalizedCount returns the total number of slots finalized.
func (p *FinalityPipeline) FPFinalizedCount() int64 {
	return p.finCount.Load()
}

// FPStop prevents new votes from being submitted.
func (p *FinalityPipeline) FPStop() {
	p.stopped.Store(true)
}

// IsFPSlotFinalized returns whether a given slot has been finalized.
func (p *FinalityPipeline) IsFPSlotFinalized(slot uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.results[slot]
	return ok
}

// GetFPSlotVoteCount returns the number of BLS-verified votes for a slot.
func (p *FinalityPipeline) GetFPSlotVoteCount(slot uint64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	ss, ok := p.slots[slot]
	if !ok {
		return 0
	}
	return ss.voteCount
}

// FPPruneOldSlots removes pipeline state for slots older than the given cutoff.
func (p *FinalityPipeline) FPPruneOldSlots(cutoffSlot uint64) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	pruned := 0
	for slot := range p.slots {
		if slot < cutoffSlot {
			delete(p.slots, slot)
			delete(p.results, slot)
			pruned++
		}
	}
	return pruned
}

// FPBLSBackendName returns the name of the active BLS backend.
func (p *FinalityPipeline) FPBLSBackendName() string {
	return p.bls.Name()
}

// SubmitFPVoteBatch submits multiple votes and returns the first finality
// result encountered (if any). Stops on the first error.
func (p *FinalityPipeline) SubmitFPVoteBatch(votes []*FPVote) (*FPFinalityResult, error) {
	for _, vote := range votes {
		result, err := p.SubmitVote(vote)
		if err != nil {
			return result, err
		}
		if result != nil {
			return result, nil
		}
	}
	return nil, nil
}
