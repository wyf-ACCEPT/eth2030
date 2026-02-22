// block_finalization_engine.go links block execution to the endgame finality
// pipeline with a sub-second finalization target. It bridges the gap between
// the EndgameEngine (weighted vote collection) and real block execution by
// tracking proposed blocks, collecting finality votes with stake weighting,
// computing aggregate finality proofs, and measuring proposal-to-finality
// latency. This addresses Gap #6 (Fast L1 Finality) by integrating block
// execution validity into the finality pipeline.
package consensus

import (
	"errors"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Block finalization engine errors.
var (
	ErrBFEBlockNotExecutionValid = errors.New("bfe: block execution not valid")
	ErrBFESlotAlreadyFinalized   = errors.New("bfe: slot already finalized")
	ErrBFEDuplicateVote          = errors.New("bfe: duplicate vote from validator")
	ErrBFEWrongSlot              = errors.New("bfe: vote slot does not match any pending block")
	ErrBFEUnknownValidator       = errors.New("bfe: unknown validator index")
	ErrBFENilBlock               = errors.New("bfe: nil block")
	ErrBFENilVote                = errors.New("bfe: nil vote")
	ErrBFENilConfig              = errors.New("bfe: nil config")
	ErrBFESlotRegression         = errors.New("bfe: slot regression not allowed")
)

// FinalizationConfig holds the parameters for the block finalization engine.
type FinalizationConfig struct {
	// TargetLatencyMs is the target proposal-to-finality time in ms.
	// Default: 500.
	TargetLatencyMs uint64

	// VotingWindowMs is the time window in which votes are accepted
	// after a block is proposed. Default: 400.
	VotingWindowMs uint64

	// AggregationWindowMs is the time window for signature aggregation
	// after voting closes. Default: 100.
	AggregationWindowMs uint64

	// MinParticipation is the minimum fraction of total stake required
	// for finality (e.g. 0.667 for 2/3 supermajority).
	MinParticipation float64

	// MaxValidators is the maximum number of validators tracked.
	MaxValidators uint64
}

// DefaultFinalizationConfig returns sensible defaults targeting sub-second
// finality.
func DefaultFinalizationConfig() *FinalizationConfig {
	return &FinalizationConfig{
		TargetLatencyMs:     500,
		VotingWindowMs:      400,
		AggregationWindowMs: 100,
		MinParticipation:    0.667,
		MaxValidators:       131072, // 128K
	}
}

// FinalizationBlock represents a block proposed for finalization.
type FinalizationBlock struct {
	Slot           uint64
	ProposerIndex  uint64
	StateRoot      types.Hash
	ParentRoot     types.Hash
	Body           []byte
	ExecutionValid bool
}

// BlockFinalityVote represents a validator's finality vote for a block.
type BlockFinalityVote struct {
	Slot           uint64
	BlockRoot      types.Hash
	ValidatorIndex uint64
	Signature      []byte
	Timestamp      int64 // unix timestamp in ms
}

// BlockFinalityProof represents proof that a slot achieved finality.
type BlockFinalityProof struct {
	Slot                uint64
	BlockRoot           types.Hash
	AggregateSignature  []byte
	ParticipantBitfield []byte
	TotalStake          uint64
	FinalizedAt         int64 // unix ms
}

// FinalizationMetrics tracks proposal-to-finality latency statistics.
type FinalizationMetrics struct {
	AvgLatencyMs    float64
	P95LatencyMs    float64
	FinalizedBlocks uint64
	MissedSlots     uint64
}

// validatorRecord holds registered validator info.
type validatorRecord struct {
	index  uint64
	pubkey []byte
	stake  uint64
}

// bfeSlotState tracks per-slot finalization state.
type bfeSlotState struct {
	block       *FinalizationBlock
	blockRoot   types.Hash
	proposedAt  int64 // unix ms
	votes       map[uint64]*BlockFinalityVote
	stakeByRoot map[types.Hash]uint64
	totalVoted  uint64
	finalized   bool
	finalRoot   types.Hash
	finalizedAt int64
}

func newBFESlotState(block *FinalizationBlock, root types.Hash, now int64) *bfeSlotState {
	return &bfeSlotState{
		block:       block,
		blockRoot:   root,
		proposedAt:  now,
		votes:       make(map[uint64]*BlockFinalityVote),
		stakeByRoot: make(map[types.Hash]uint64),
	}
}

// BlockFinalizationEngine links block execution to the endgame finality
// pipeline. It tracks proposed blocks, collects votes, checks finality
// thresholds using stake weighting, and measures latency. All methods
// are thread-safe.
type BlockFinalizationEngine struct {
	mu sync.RWMutex

	config     *FinalizationConfig
	validators map[uint64]*validatorRecord
	totalStake uint64

	slots           map[uint64]*bfeSlotState
	latestFinalized uint64
	finalizedRoots  []types.Hash

	// Latency tracking for metrics.
	latencies []float64 // ms values for finalized blocks
	missed    uint64
}

// NewBlockFinalizationEngine creates a new block finalization engine.
func NewBlockFinalizationEngine(config *FinalizationConfig) *BlockFinalizationEngine {
	if config == nil {
		config = DefaultFinalizationConfig()
	}
	return &BlockFinalizationEngine{
		config:         config,
		validators:     make(map[uint64]*validatorRecord),
		slots:          make(map[uint64]*bfeSlotState),
		finalizedRoots: make([]types.Hash, 0),
		latencies:      make([]float64, 0),
	}
}

// RegisterValidator registers a validator with the given index, pubkey, and
// stake. If the validator is already registered, their stake is updated.
func (e *BlockFinalizationEngine) RegisterValidator(index uint64, pubkey []byte, stake uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	existing, ok := e.validators[index]
	if ok {
		e.totalStake -= existing.stake
	}

	pk := make([]byte, len(pubkey))
	copy(pk, pubkey)
	e.validators[index] = &validatorRecord{
		index:  index,
		pubkey: pk,
		stake:  stake,
	}
	e.totalStake += stake
}

// ActiveValidatorSet returns the validator indices active at the given slot.
// Currently returns all registered validators.
func (e *BlockFinalizationEngine) ActiveValidatorSet(slot uint64) []uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	indices := make([]uint64, 0, len(e.validators))
	for idx := range e.validators {
		indices = append(indices, idx)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	return indices
}

// ProposeBlock starts the finalization timer for a proposed block.
// The block must have ExecutionValid == true.
func (e *BlockFinalizationEngine) ProposeBlock(block *FinalizationBlock) error {
	if block == nil {
		return ErrBFENilBlock
	}
	if !block.ExecutionValid {
		return ErrBFEBlockNotExecutionValid
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, finalized := e.slots[block.Slot]; finalized {
		ss := e.slots[block.Slot]
		if ss != nil && ss.finalized {
			return ErrBFESlotAlreadyFinalized
		}
	}

	root := computeBlockRoot(block)
	now := time.Now().UnixMilli()
	e.slots[block.Slot] = newBFESlotState(block, root, now)
	return nil
}

// ReceiveVote accumulates a finality vote and checks the threshold.
func (e *BlockFinalizationEngine) ReceiveVote(vote *BlockFinalityVote) error {
	if vote == nil {
		return ErrBFENilVote
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	ss, ok := e.slots[vote.Slot]
	if !ok {
		return ErrBFEWrongSlot
	}
	if ss.finalized {
		return ErrBFESlotAlreadyFinalized
	}

	// Verify the validator is registered.
	vr, ok := e.validators[vote.ValidatorIndex]
	if !ok {
		return ErrBFEUnknownValidator
	}

	// Reject duplicate votes.
	if _, exists := ss.votes[vote.ValidatorIndex]; exists {
		return ErrBFEDuplicateVote
	}

	ss.votes[vote.ValidatorIndex] = vote
	ss.stakeByRoot[vote.BlockRoot] += vr.stake
	ss.totalVoted += vr.stake

	// Check finality threshold.
	e.checkBFEFinality(vote.Slot, ss)
	return nil
}

// checkBFEFinality checks if any block root in the slot has reached the
// finality threshold. Caller must hold e.mu.
func (e *BlockFinalizationEngine) checkBFEFinality(slot uint64, ss *bfeSlotState) {
	if e.totalStake == 0 {
		return
	}

	threshold := uint64(float64(e.totalStake) * e.config.MinParticipation)
	if threshold == 0 {
		threshold = 1
	}

	for root, stake := range ss.stakeByRoot {
		if stake >= threshold {
			ss.finalized = true
			ss.finalRoot = root
			ss.finalizedAt = time.Now().UnixMilli()

			if slot > e.latestFinalized {
				e.latestFinalized = slot
				e.finalizedRoots = append(e.finalizedRoots, root)
			}

			// Record latency.
			latency := float64(ss.finalizedAt - ss.proposedAt)
			e.latencies = append(e.latencies, latency)
			return
		}
	}
}

// CheckFinality returns the finality proof for a slot, or an error if
// finality has not been reached.
func (e *BlockFinalizationEngine) CheckFinality(slot uint64) (*BlockFinalityProof, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ss, ok := e.slots[slot]
	if !ok {
		return nil, ErrBFEWrongSlot
	}
	if !ss.finalized {
		return nil, nil
	}

	// Build participant bitfield.
	bitfield := e.buildBitfield(ss)

	// Build aggregate signature (concatenate for now; real BLS aggregation
	// would be done via the FinalityBLSAdapter).
	var aggSig []byte
	for _, v := range ss.votes {
		if v.BlockRoot == ss.finalRoot {
			aggSig = append(aggSig, v.Signature...)
		}
	}

	return &BlockFinalityProof{
		Slot:                slot,
		BlockRoot:           ss.finalRoot,
		AggregateSignature:  aggSig,
		ParticipantBitfield: bitfield,
		TotalStake:          ss.totalVoted,
		FinalizedAt:         ss.finalizedAt,
	}, nil
}

// buildBitfield creates a bitfield of participating validators.
// Caller must hold e.mu (at least RLock).
func (e *BlockFinalizationEngine) buildBitfield(ss *bfeSlotState) []byte {
	if len(e.validators) == 0 {
		return nil
	}

	// Find max validator index.
	var maxIdx uint64
	for idx := range e.validators {
		if idx > maxIdx {
			maxIdx = idx
		}
	}

	bitfieldLen := (maxIdx + 8) / 8
	bitfield := make([]byte, bitfieldLen)
	for idx := range ss.votes {
		if ss.votes[idx].BlockRoot == ss.finalRoot {
			bitfield[idx/8] |= 1 << (idx % 8)
		}
	}
	return bitfield
}

// LatencyMetrics computes and returns finalization latency statistics.
func (e *BlockFinalizationEngine) LatencyMetrics() *FinalizationMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()

	m := &FinalizationMetrics{
		FinalizedBlocks: uint64(len(e.latencies)),
		MissedSlots:     e.missed,
	}

	if len(e.latencies) == 0 {
		return m
	}

	// Average.
	var sum float64
	for _, l := range e.latencies {
		sum += l
	}
	m.AvgLatencyMs = sum / float64(len(e.latencies))

	// P95.
	sorted := make([]float64, len(e.latencies))
	copy(sorted, e.latencies)
	sort.Float64s(sorted)
	p95Idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}
	if p95Idx >= len(sorted) {
		p95Idx = len(sorted) - 1
	}
	m.P95LatencyMs = sorted[p95Idx]

	return m
}

// RecordMissedSlot increments the missed slot counter. Called when a slot
// passes without reaching finality.
func (e *BlockFinalizationEngine) RecordMissedSlot() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.missed++
}

// IsSlotFinalized returns true if the given slot has been finalized.
func (e *BlockFinalizationEngine) IsSlotFinalized(slot uint64) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ss, ok := e.slots[slot]
	if !ok {
		return false
	}
	return ss.finalized
}

// LatestFinalizedSlot returns the highest finalized slot number.
func (e *BlockFinalizationEngine) LatestFinalizedSlot() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.latestFinalized
}

// TotalStake returns the total registered validator stake.
func (e *BlockFinalizationEngine) TotalStake() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.totalStake
}

// ValidatorCount returns the number of registered validators.
func (e *BlockFinalizationEngine) ValidatorCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.validators)
}

// PruneSlots removes finalized slot state older than keepSlots before the
// latest finalized slot.
func (e *BlockFinalizationEngine) PruneSlots(keepSlots uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.latestFinalized <= keepSlots {
		return
	}
	cutoff := e.latestFinalized - keepSlots
	for s := range e.slots {
		if s < cutoff {
			delete(e.slots, s)
		}
	}
}

// computeBlockRoot produces a deterministic hash for a FinalizationBlock.
// Uses a simple XOR-fold of the block fields for compactness.
func computeBlockRoot(block *FinalizationBlock) types.Hash {
	var h types.Hash
	// Mix in slot.
	h[0] = byte(block.Slot)
	h[1] = byte(block.Slot >> 8)
	h[2] = byte(block.Slot >> 16)
	h[3] = byte(block.Slot >> 24)
	// Mix in proposer.
	h[4] = byte(block.ProposerIndex)
	h[5] = byte(block.ProposerIndex >> 8)
	// Mix in state root.
	for i := 0; i < 32; i++ {
		h[i] ^= block.StateRoot[i]
	}
	// Mix in parent root.
	for i := 0; i < 32; i++ {
		h[i] ^= block.ParentRoot[i]
	}
	return h
}
