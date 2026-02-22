package consensus

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// SSFRoundPhase represents the current phase within an SSF round.
type SSFRoundPhase uint8

const (
	// PhasePropose is the initial phase where a block is proposed.
	PhasePropose SSFRoundPhase = iota
	// PhaseAttest is the phase where validators submit attestations.
	PhaseAttest
	// PhaseAggregate is the phase where votes are aggregated.
	PhaseAggregate
	// PhaseFinalize is the terminal phase where finality is determined.
	PhaseFinalize
)

// String returns a human-readable name for the phase.
func (p SSFRoundPhase) String() string {
	switch p {
	case PhasePropose:
		return "Propose"
	case PhaseAttest:
		return "Attest"
	case PhaseAggregate:
		return "Aggregate"
	case PhaseFinalize:
		return "Finalize"
	default:
		return "Unknown"
	}
}

// SSF round engine errors.
var (
	ErrRoundAlreadyExists   = errors.New("ssf-round: round already exists for slot")
	ErrRoundNotFound        = errors.New("ssf-round: no round for slot")
	ErrRoundWrongPhase      = errors.New("ssf-round: operation invalid in current phase")
	ErrRoundAlreadyFinalized = errors.New("ssf-round: round already finalized")
	ErrRoundDuplicateVote   = errors.New("ssf-round: duplicate vote from validator")
	ErrRoundEquivocation    = errors.New("ssf-round: equivocation detected")
	ErrRoundNoBlockRoot     = errors.New("ssf-round: no block root proposed")
	ErrRoundNilConfig       = errors.New("ssf-round: nil configuration")
	ErrRoundZeroTotalStake  = errors.New("ssf-round: total stake is zero")
)

// SSFRoundVote represents a single validator's vote within an SSF round.
type SSFRoundVote struct {
	// ValidatorPubkeyHash is the keccak256 hash of the validator's BLS
	// public key, used as a compact identifier.
	ValidatorPubkeyHash types.Hash

	// Slot is the slot this vote targets.
	Slot uint64

	// BlockRoot is the block root this vote attests to.
	BlockRoot types.Hash

	// Signature is a placeholder for the attestation BLS signature.
	Signature [96]byte

	// Stake is the validator's effective balance weight.
	Stake uint64
}

// SSFRound tracks the state of a single SSF finality round.
type SSFRound struct {
	Slot      uint64
	Phase     SSFRoundPhase
	BlockRoot types.Hash
	Votes     map[types.Hash]*SSFRoundVote // keyed by ValidatorPubkeyHash
	StakeByRoot map[types.Hash]uint64
	TotalVoteStake uint64
	AggregatedSig  [96]byte
	Finalized bool
	StartedAt time.Time
	FinalizedAt time.Time
}

// SSFRoundEngineConfig configures the round engine.
type SSFRoundEngineConfig struct {
	// FinalityThresholdNum/Den express the supermajority fraction.
	// Default: 2/3.
	FinalityThresholdNum uint64
	FinalityThresholdDen uint64

	// TotalStake is the total active stake in the system.
	TotalStake uint64

	// MaxRoundHistory is the number of completed rounds to retain.
	MaxRoundHistory int
}

// DefaultSSFRoundEngineConfig returns sensible defaults.
func DefaultSSFRoundEngineConfig() SSFRoundEngineConfig {
	return SSFRoundEngineConfig{
		FinalityThresholdNum: 2,
		FinalityThresholdDen: 3,
		TotalStake:           32_000_000 * GweiPerETH,
		MaxRoundHistory:      256,
	}
}

// SSFRoundStats tracks aggregate statistics for the round engine.
type SSFRoundStats struct {
	RoundsCompleted      uint64
	TotalFinalityTimeMs  uint64
	EquivocationsDetected uint64
}

// AverageFinalityMs returns the average finality time in milliseconds.
// Returns 0 if no rounds have been completed.
func (s SSFRoundStats) AverageFinalityMs() uint64 {
	if s.RoundsCompleted == 0 {
		return 0
	}
	return s.TotalFinalityTimeMs / s.RoundsCompleted
}

// SSFRoundEngine manages single-slot finality rounds with explicit phase
// transitions: Propose -> Attest -> Aggregate -> Finalize.
//
// Thread-safe: all public methods are protected by a mutex.
type SSFRoundEngine struct {
	mu sync.Mutex

	config SSFRoundEngineConfig
	stats  SSFRoundStats

	// Active rounds keyed by slot.
	rounds map[uint64]*SSFRound

	// History of completed rounds (bounded by MaxRoundHistory).
	history     []uint64
	historyMap  map[uint64]*SSFRound

	// Equivocation tracking: validator pubkey hash -> first block root voted.
	// Per-slot to detect validators voting for different roots.
	equivocations map[uint64]map[types.Hash]types.Hash
}

// NewSSFRoundEngine creates a new round engine.
// Returns nil if the config is invalid.
func NewSSFRoundEngine(config SSFRoundEngineConfig) *SSFRoundEngine {
	if config.FinalityThresholdNum == 0 || config.FinalityThresholdDen == 0 {
		return nil
	}
	if config.MaxRoundHistory <= 0 {
		config.MaxRoundHistory = 256
	}
	return &SSFRoundEngine{
		config:        config,
		rounds:        make(map[uint64]*SSFRound),
		history:       make([]uint64, 0),
		historyMap:    make(map[uint64]*SSFRound),
		equivocations: make(map[uint64]map[types.Hash]types.Hash),
	}
}

// NewRound creates a new SSF round for the given slot.
// The round starts in the Propose phase.
func (e *SSFRoundEngine) NewRound(slot uint64) (*SSFRound, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.rounds[slot]; exists {
		return nil, fmt.Errorf("%w: slot %d", ErrRoundAlreadyExists, slot)
	}
	if _, exists := e.historyMap[slot]; exists {
		return nil, fmt.Errorf("%w: slot %d (in history)", ErrRoundAlreadyFinalized, slot)
	}

	round := &SSFRound{
		Slot:        slot,
		Phase:       PhasePropose,
		Votes:       make(map[types.Hash]*SSFRoundVote),
		StakeByRoot: make(map[types.Hash]uint64),
		StartedAt:   time.Now(),
	}
	e.rounds[slot] = round
	e.equivocations[slot] = make(map[types.Hash]types.Hash)

	return round, nil
}

// ProposeBlock sets the proposed block root for a round and transitions
// from Propose to Attest phase.
func (e *SSFRoundEngine) ProposeBlock(slot uint64, root types.Hash) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	round, ok := e.rounds[slot]
	if !ok {
		return fmt.Errorf("%w: slot %d", ErrRoundNotFound, slot)
	}
	if round.Finalized {
		return fmt.Errorf("%w: slot %d", ErrRoundAlreadyFinalized, slot)
	}
	if round.Phase != PhasePropose {
		return fmt.Errorf("%w: expected Propose, got %s", ErrRoundWrongPhase, round.Phase)
	}

	round.BlockRoot = root
	round.Phase = PhaseAttest
	return nil
}

// SubmitAttestation records a validator's vote for the current round.
// Must be in Attest phase. Detects equivocation (same validator, different root).
//
// Optimistic path: if after this vote the 2/3+1 threshold is met, the
// round transitions directly to Finalize, skipping Aggregate.
func (e *SSFRoundEngine) SubmitAttestation(vote SSFRoundVote) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	round, ok := e.rounds[vote.Slot]
	if !ok {
		return fmt.Errorf("%w: slot %d", ErrRoundNotFound, vote.Slot)
	}
	if round.Finalized {
		return fmt.Errorf("%w: slot %d", ErrRoundAlreadyFinalized, vote.Slot)
	}
	if round.Phase != PhaseAttest {
		return fmt.Errorf("%w: expected Attest, got %s", ErrRoundWrongPhase, round.Phase)
	}

	// Check for duplicate votes.
	if existing, dup := round.Votes[vote.ValidatorPubkeyHash]; dup {
		// If same validator votes for a different root, it's equivocation.
		if existing.BlockRoot != vote.BlockRoot {
			e.stats.EquivocationsDetected++
			slotEquiv := e.equivocations[vote.Slot]
			slotEquiv[vote.ValidatorPubkeyHash] = vote.BlockRoot
			return fmt.Errorf("%w: validator %s voted for %s then %s",
				ErrRoundEquivocation,
				vote.ValidatorPubkeyHash.Hex(),
				existing.BlockRoot.Hex(),
				vote.BlockRoot.Hex())
		}
		return fmt.Errorf("%w: validator %s at slot %d",
			ErrRoundDuplicateVote, vote.ValidatorPubkeyHash.Hex(), vote.Slot)
	}

	// Record the vote.
	voteCopy := vote
	round.Votes[vote.ValidatorPubkeyHash] = &voteCopy
	round.StakeByRoot[vote.BlockRoot] += vote.Stake
	round.TotalVoteStake += vote.Stake

	// Optimistic path: check if threshold met, skip to Finalize.
	if e.meetsThreshold(round.StakeByRoot[vote.BlockRoot]) {
		round.Phase = PhaseFinalize
	}

	return nil
}

// AggregateVotes transitions from Attest to Aggregate phase and
// computes the aggregated signature placeholder. If the threshold
// is already met, transitions directly to Finalize.
func (e *SSFRoundEngine) AggregateVotes(slot uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	round, ok := e.rounds[slot]
	if !ok {
		return fmt.Errorf("%w: slot %d", ErrRoundNotFound, slot)
	}
	if round.Finalized {
		return fmt.Errorf("%w: slot %d", ErrRoundAlreadyFinalized, slot)
	}
	// Allow aggregation from Attest phase (normal) or if already at
	// Finalize (optimistic path already moved it).
	if round.Phase == PhaseFinalize {
		return nil // already at or past aggregate
	}
	if round.Phase != PhaseAttest {
		return fmt.Errorf("%w: expected Attest, got %s", ErrRoundWrongPhase, round.Phase)
	}

	// Placeholder aggregation: XOR all vote signatures.
	var aggSig [96]byte
	for _, v := range round.Votes {
		for i := range aggSig {
			aggSig[i] ^= v.Signature[i]
		}
	}
	round.AggregatedSig = aggSig

	// Check if any root meets the threshold.
	thresholdMet := false
	for _, stake := range round.StakeByRoot {
		if e.meetsThreshold(stake) {
			thresholdMet = true
			break
		}
	}

	if thresholdMet {
		round.Phase = PhaseFinalize
	} else {
		round.Phase = PhaseAggregate
	}

	return nil
}

// Finalize completes the round if the 2/3+ threshold is met.
// Can be called from Aggregate or Finalize phase (optimistic path).
// Returns the finalized block root.
func (e *SSFRoundEngine) Finalize(slot uint64) (types.Hash, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.config.TotalStake == 0 {
		return types.Hash{}, ErrRoundZeroTotalStake
	}

	round, ok := e.rounds[slot]
	if !ok {
		return types.Hash{}, fmt.Errorf("%w: slot %d", ErrRoundNotFound, slot)
	}
	if round.Finalized {
		return round.BlockRoot, nil
	}
	if round.Phase != PhaseAggregate && round.Phase != PhaseFinalize {
		return types.Hash{}, fmt.Errorf("%w: expected Aggregate or Finalize, got %s",
			ErrRoundWrongPhase, round.Phase)
	}

	// Find the root with the most stake that meets the threshold.
	var bestRoot types.Hash
	var bestStake uint64
	for root, stake := range round.StakeByRoot {
		if e.meetsThreshold(stake) && stake > bestStake {
			bestRoot = root
			bestStake = stake
		}
	}

	if bestStake == 0 {
		return types.Hash{}, fmt.Errorf("ssf-round: no root meets finality threshold at slot %d", slot)
	}

	round.Finalized = true
	round.BlockRoot = bestRoot
	round.Phase = PhaseFinalize
	round.FinalizedAt = time.Now()

	// Update statistics.
	e.stats.RoundsCompleted++
	elapsed := round.FinalizedAt.Sub(round.StartedAt)
	e.stats.TotalFinalityTimeMs += uint64(elapsed.Milliseconds())

	// Move to history.
	e.historyMap[slot] = round
	e.history = append(e.history, slot)
	delete(e.rounds, slot)
	delete(e.equivocations, slot)

	// Evict oldest entries if history exceeds max.
	for len(e.history) > e.config.MaxRoundHistory {
		oldest := e.history[0]
		e.history = e.history[1:]
		delete(e.historyMap, oldest)
	}

	return bestRoot, nil
}

// GetRound returns a snapshot of the round for the given slot.
// Checks both active rounds and history.
func (e *SSFRoundEngine) GetRound(slot uint64) (*SSFRound, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if round, ok := e.rounds[slot]; ok {
		return e.copyRound(round), true
	}
	if round, ok := e.historyMap[slot]; ok {
		return e.copyRound(round), true
	}
	return nil, false
}

// GetPhase returns the current phase of the round for the given slot.
func (e *SSFRoundEngine) GetPhase(slot uint64) (SSFRoundPhase, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if round, ok := e.rounds[slot]; ok {
		return round.Phase, true
	}
	if round, ok := e.historyMap[slot]; ok {
		return round.Phase, true
	}
	return 0, false
}

// VoteCount returns the number of votes for a given slot.
func (e *SSFRoundEngine) VoteCount(slot uint64) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if round, ok := e.rounds[slot]; ok {
		return len(round.Votes)
	}
	if round, ok := e.historyMap[slot]; ok {
		return len(round.Votes)
	}
	return 0
}

// IsFinalized returns true if the round for the given slot is finalized.
func (e *SSFRoundEngine) IsFinalized(slot uint64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	if round, ok := e.historyMap[slot]; ok && round.Finalized {
		return true
	}
	if round, ok := e.rounds[slot]; ok && round.Finalized {
		return true
	}
	return false
}

// Stats returns a copy of the current statistics.
func (e *SSFRoundEngine) Stats() SSFRoundStats {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stats
}

// EquivocationCount returns the number of equivocations detected for a slot.
func (e *SSFRoundEngine) EquivocationCount(slot uint64) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	if equivs, ok := e.equivocations[slot]; ok {
		return len(equivs)
	}
	return 0
}

// --- internal helpers ---

// meetsThreshold checks if stake meets the supermajority requirement.
func (e *SSFRoundEngine) meetsThreshold(stake uint64) bool {
	if e.config.TotalStake == 0 {
		return false
	}
	return stake*e.config.FinalityThresholdDen >=
		e.config.TotalStake*e.config.FinalityThresholdNum
}

// copyRound creates a shallow copy of a round suitable for external use.
func (e *SSFRoundEngine) copyRound(r *SSFRound) *SSFRound {
	cp := &SSFRound{
		Slot:           r.Slot,
		Phase:          r.Phase,
		BlockRoot:      r.BlockRoot,
		TotalVoteStake: r.TotalVoteStake,
		AggregatedSig:  r.AggregatedSig,
		Finalized:      r.Finalized,
		StartedAt:      r.StartedAt,
		FinalizedAt:    r.FinalizedAt,
		Votes:          make(map[types.Hash]*SSFRoundVote, len(r.Votes)),
		StakeByRoot:    make(map[types.Hash]uint64, len(r.StakeByRoot)),
	}
	for k, v := range r.Votes {
		voteCopy := *v
		cp.Votes[k] = &voteCopy
	}
	for k, v := range r.StakeByRoot {
		cp.StakeByRoot[k] = v
	}
	return cp
}
