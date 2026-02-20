// endgame_pipeline.go wires block execution into the endgame finality path,
// providing a high-level pipeline that processes blocks through execution,
// attestation collection, BLS aggregate verification, and sub-second
// finalization. Targets the M+ era (2029+) fast L1 finality goal.
package consensus

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// Pipeline errors.
var (
	ErrPipelineNilBlock       = errors.New("endgame_pipeline: nil block")
	ErrPipelineNilParentState = errors.New("endgame_pipeline: nil parent state")
	ErrPipelineExecutionFail  = errors.New("endgame_pipeline: block execution failed")
	ErrPipelineNoAttestations = errors.New("endgame_pipeline: no attestations collected")
	ErrPipelineTimeout        = errors.New("endgame_pipeline: finality attempt timed out")
	ErrPipelineQuorumNotMet   = errors.New("endgame_pipeline: quorum not met")
	ErrPipelineNilConfig      = errors.New("endgame_pipeline: nil config")
	ErrPipelineZeroStake      = errors.New("endgame_pipeline: total stake is zero")
	ErrPipelineAlreadyFinal   = errors.New("endgame_pipeline: slot already finalized")
)

// PipelineStage represents a discrete stage in the endgame finality pipeline.
type PipelineStage uint8

const (
	// StageValidateBlock validates the block header and structure.
	StageValidateBlock PipelineStage = iota
	// StageExecuteBlock runs the execution engine on the block.
	StageExecuteBlock
	// StageCollectVotes collects attestation votes from the validator set.
	StageCollectVotes
	// StageVerifyBLS verifies the aggregate BLS signature over votes.
	StageVerifyBLS
	// StageFinalize marks the block as finalized if quorum is met.
	StageFinalize
)

// String returns a human-readable name for the pipeline stage.
func (s PipelineStage) String() string {
	switch s {
	case StageValidateBlock:
		return "ValidateBlock"
	case StageExecuteBlock:
		return "ExecuteBlock"
	case StageCollectVotes:
		return "CollectVotes"
	case StageVerifyBLS:
		return "VerifyBLS"
	case StageFinalize:
		return "Finalize"
	default:
		return "Unknown"
	}
}

// PipelineFinalityResult captures the outcome of a finality pipeline execution.
type PipelineFinalityResult struct {
	// Finalized indicates whether the block achieved finality.
	Finalized bool

	// LatencyMs is the total time from pipeline start to finality (or timeout).
	LatencyMs int64

	// ParticipantCount is the number of validators that voted.
	ParticipantCount int

	// AggregateSignature is the aggregated BLS signature over all votes.
	AggregateSignature [96]byte

	// VotingStake is the total stake weight that voted for the block.
	VotingStake uint64

	// BlockRoot is the root of the block that was processed.
	BlockRoot types.Hash

	// Stage is the last completed pipeline stage.
	Stage PipelineStage
}

// ExecutionEngine is the interface the pipeline uses to execute blocks.
type ExecutionEngine interface {
	// ExecuteBlock runs the state transition for a block against parent state.
	// Returns the new state root and any error.
	ExecuteBlock(block *SignedBeaconBlock, parentStateRoot types.Hash) (types.Hash, error)
}

// BLSVerifier is the interface for verifying aggregate BLS signatures.
type BLSVerifier interface {
	// VerifyAggregate checks an aggregate signature against multiple pubkeys
	// and a shared message digest. Returns true if valid.
	VerifyAggregate(pubkeys [][48]byte, message []byte, aggSig [96]byte) bool
}

// PipelineConfig holds configuration for the endgame finality pipeline.
type PipelineConfig struct {
	// TargetFinalityMs is the target time in milliseconds to achieve finality.
	// If exceeded, the pipeline returns a timeout error.
	TargetFinalityMs int64

	// FinalityThresholdNum and FinalityThresholdDen express the supermajority
	// fraction required for finality (default: 2/3).
	FinalityThresholdNum uint64
	FinalityThresholdDen uint64

	// TotalStake is the total active validator stake used for quorum checks.
	TotalStake uint64

	// MaxVoters limits the number of votes to aggregate per slot.
	MaxVoters int
}

// DefaultPipelineConfig returns sensible defaults for the endgame pipeline.
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		TargetFinalityMs:     500,
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           32_000_000 * GweiPerETH,
		MaxVoters:            8192,
	}
}

// Validate checks the pipeline config for correctness.
func (c *PipelineConfig) Validate() error {
	if c == nil {
		return ErrPipelineNilConfig
	}
	if c.FinalityThresholdNum == 0 || c.FinalityThresholdDen == 0 {
		return errors.New("endgame_pipeline: invalid finality threshold")
	}
	if c.TotalStake == 0 {
		return ErrPipelineZeroStake
	}
	if c.TargetFinalityMs <= 0 {
		c.TargetFinalityMs = 500
	}
	return nil
}

// EndgamePipeline orchestrates the end-to-end finality flow: block validation,
// execution, vote collection, BLS verification, and finalization.
type EndgamePipeline struct {
	mu sync.Mutex

	config    *PipelineConfig
	execution ExecutionEngine
	verifier  BLSVerifier

	// finalized tracks which slots have been finalized.
	finalized map[uint64]types.Hash

	// stats tracks pipeline performance.
	totalRuns     uint64
	totalLatency  int64
	totalFinalized uint64
}

// NewEndgamePipeline creates a new pipeline. Returns nil if config is invalid.
func NewEndgamePipeline(cfg *PipelineConfig, exec ExecutionEngine, verifier BLSVerifier) *EndgamePipeline {
	if cfg == nil {
		cfg = DefaultPipelineConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil
	}
	return &EndgamePipeline{
		config:    cfg,
		execution: exec,
		verifier:  verifier,
		finalized: make(map[uint64]types.Hash),
	}
}

// ProcessBlock runs the full finality pipeline for a block against the given
// parent state root. It validates the block, executes it via the execution
// engine, then attempts fast finality with the provided attestations.
func (p *EndgamePipeline) ProcessBlock(
	block *SignedBeaconBlock,
	parentStateRoot types.Hash,
	attestations []SSFRoundVote,
) (*PipelineFinalityResult, error) {
	if block == nil || block.Block == nil {
		return nil, ErrPipelineNilBlock
	}

	start := time.Now()
	result := &PipelineFinalityResult{
		BlockRoot: block.Block.StateRoot,
	}

	// Stage 1: Validate block structure.
	if err := p.validateBlock(block); err != nil {
		result.Stage = StageValidateBlock
		result.LatencyMs = time.Since(start).Milliseconds()
		return result, err
	}
	result.Stage = StageValidateBlock

	// Stage 2: Execute the block through the execution engine.
	if p.execution != nil {
		newRoot, err := p.execution.ExecuteBlock(block, parentStateRoot)
		if err != nil {
			result.Stage = StageExecuteBlock
			result.LatencyMs = time.Since(start).Milliseconds()
			return result, ErrPipelineExecutionFail
		}
		result.BlockRoot = newRoot
	}
	result.Stage = StageExecuteBlock

	// Stage 3 & 4: Attempt fast finality with the attestations.
	slot := uint64(block.Block.Slot)
	finalized, err := p.AttemptFastFinality(slot, attestations)
	if err != nil {
		result.LatencyMs = time.Since(start).Milliseconds()
		return result, err
	}

	result.Finalized = finalized
	result.LatencyMs = time.Since(start).Milliseconds()
	result.ParticipantCount = len(attestations)

	if finalized {
		result.Stage = StageFinalize
	}

	return result, nil
}

// AttemptFastFinality tries to achieve sub-second finality for a slot using
// the provided attestations. It collects vote stakes, verifies quorum, and
// if the BLS verifier is available, validates the aggregate signature.
func (p *EndgamePipeline) AttemptFastFinality(
	slot uint64,
	attestations []SSFRoundVote,
) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if already finalized.
	if _, ok := p.finalized[slot]; ok {
		return true, ErrPipelineAlreadyFinal
	}

	if len(attestations) == 0 {
		return false, ErrPipelineNoAttestations
	}

	start := time.Now()

	// Collect voting stake per block root.
	stakeByRoot := make(map[types.Hash]uint64)
	var totalVotingStake uint64
	for i := range attestations {
		att := &attestations[i]
		stakeByRoot[att.BlockRoot] += att.Stake
		totalVotingStake += att.Stake
	}

	// Find the root with maximum stake.
	var bestRoot types.Hash
	var bestStake uint64
	for root, stake := range stakeByRoot {
		if stake > bestStake {
			bestRoot = root
			bestStake = stake
		}
	}

	// Check quorum.
	if !p.meetsThreshold(bestStake) {
		return false, ErrPipelineQuorumNotMet
	}

	// Check timeout.
	elapsed := time.Since(start)
	if elapsed.Milliseconds() > p.config.TargetFinalityMs {
		return false, ErrPipelineTimeout
	}

	// Verify BLS aggregate if verifier is available.
	if p.verifier != nil {
		var pubkeys [][48]byte
		var sigs [][96]byte
		for i := range attestations {
			if attestations[i].BlockRoot == bestRoot {
				// Use ValidatorPubkeyHash as a stand-in; real implementation
				// would look up the full pubkey from the validator registry.
				var pk [48]byte
				copy(pk[:], attestations[i].ValidatorPubkeyHash[:])
				pubkeys = append(pubkeys, pk)
				sigs = append(sigs, attestations[i].Signature)
			}
		}

		if len(pubkeys) > 0 {
			// Build the vote digest using the first matching vote.
			adapter := NewFinalityBLSAdapter()
			digest := adapter.VoteDigest(&attestations[0])
			aggSig := aggregateSigsForPipeline(sigs)
			if !p.verifier.VerifyAggregate(pubkeys, digest, aggSig) {
				return false, errors.New("endgame_pipeline: BLS aggregate verification failed")
			}
		}
	}

	// Mark as finalized.
	p.finalized[slot] = bestRoot
	p.totalRuns++
	p.totalFinalized++
	p.totalLatency += time.Since(start).Milliseconds()

	return true, nil
}

// ValidateQuorum checks whether the provided votes have sufficient stake to
// meet the 2/3 supermajority threshold.
func (p *EndgamePipeline) ValidateQuorum(votes []SSFRoundVote, totalStake uint64) bool {
	if totalStake == 0 || len(votes) == 0 {
		return false
	}
	var voteStake uint64
	for i := range votes {
		voteStake += votes[i].Stake
	}
	return voteStake*p.config.FinalityThresholdDen >= totalStake*p.config.FinalityThresholdNum
}

// meetsThreshold checks if the given stake meets the finality threshold
// against the configured total stake.
func (p *EndgamePipeline) meetsThreshold(stake uint64) bool {
	if p.config.TotalStake == 0 {
		return false
	}
	return stake*p.config.FinalityThresholdDen >= p.config.TotalStake*p.config.FinalityThresholdNum
}

// validateBlock performs basic structural validation of the beacon block.
func (p *EndgamePipeline) validateBlock(block *SignedBeaconBlock) error {
	if block.Block == nil {
		return ErrPipelineNilBlock
	}
	if block.Block.Body == nil {
		return errors.New("endgame_pipeline: nil block body")
	}
	return nil
}

// IsFinalized returns whether a slot has been finalized through the pipeline.
func (p *EndgamePipeline) IsFinalized(slot uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.finalized[slot]
	return ok
}

// FinalizedRoot returns the finalized root for a slot, or empty hash if not finalized.
func (p *EndgamePipeline) FinalizedRoot(slot uint64) types.Hash {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finalized[slot]
}

// PipelineStats returns aggregate statistics for the pipeline.
type PipelineStats struct {
	TotalRuns      uint64
	TotalFinalized uint64
	AvgLatencyMs   int64
}

// Stats returns a snapshot of the pipeline statistics.
func (p *EndgamePipeline) Stats() PipelineStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := PipelineStats{
		TotalRuns:      p.totalRuns,
		TotalFinalized: p.totalFinalized,
	}
	if p.totalRuns > 0 {
		stats.AvgLatencyMs = p.totalLatency / int64(p.totalRuns)
	}
	return stats
}

// aggregateSigsForPipeline XOR-aggregates signatures as a placeholder.
// In production, this would use real BLS signature aggregation.
func aggregateSigsForPipeline(sigs [][96]byte) [96]byte {
	var agg [96]byte
	for _, sig := range sigs {
		for i := range agg {
			agg[i] ^= sig[i]
		}
	}
	return agg
}
