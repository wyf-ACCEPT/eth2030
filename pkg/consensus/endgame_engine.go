package consensus

// Endgame Engine implements fast sub-second finality for the M+ era (2029+).
// Unlike the EndgameFinalityTracker which builds on epochs/sub-slots,
// EndgameEngine provides a standalone, thread-safe engine that collects
// weighted votes per slot and determines finality using configurable
// thresholds and optimistic confirmation.

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

var (
	ErrDuplicateVote     = errors.New("endgame: duplicate vote from validator")
	ErrInvalidSlot       = errors.New("endgame: vote slot too old or too far ahead")
	ErrZeroWeight        = errors.New("endgame: vote weight must be > 0")
	ErrUnknownValidator  = errors.New("endgame: unknown validator index")
	ErrInvalidSignature  = errors.New("endgame: BLS signature verification failed")
	ErrBlockExecFailed   = errors.New("endgame: block execution verification failed")
)

// EndgameEngineConfig holds the configuration for the endgame finality engine.
type EndgameEngineConfig struct {
	// FinalityThreshold is the fraction of total weight required for
	// finalization (e.g. 0.667 for 2/3 supermajority).
	FinalityThreshold float64

	// MaxSlotHistory is how many slots of history to retain before pruning.
	MaxSlotHistory uint64

	// TargetFinalityMs is the target time (in ms) to achieve finality
	// within a slot. Used as a reference for TimeToFinality calculations.
	TargetFinalityMs uint64

	// OptimisticThreshold is the fraction of weight needed for optimistic
	// (pre-finality) confirmation (e.g. 0.5 for 50%).
	OptimisticThreshold float64
}

// DefaultEndgameEngineConfig returns sane defaults for endgame finality.
func DefaultEndgameEngineConfig() EndgameEngineConfig {
	return EndgameEngineConfig{
		FinalityThreshold:   0.667,
		MaxSlotHistory:      64,
		TargetFinalityMs:    500,
		OptimisticThreshold: 0.5,
	}
}

// EndgameVote represents a single finality vote from a validator.
type EndgameVote struct {
	Slot           uint64
	ValidatorIndex uint64
	BlockHash      types.Hash
	Weight         uint64
	Timestamp      uint64                      // unix timestamp in ms when the vote was cast
	Signature      [crypto.BLSSignatureSize]byte // BLS signature over slot||block_root
}

// EndgameResult is the outcome of a finality check for a given slot.
type EndgameResult struct {
	Slot           uint64
	IsFinalized    bool
	FinalizedHash  types.Hash
	TotalWeight    uint64
	Threshold      uint64
	TimeToFinality uint64 // ms from first vote to finalization, 0 if not finalized
}

// OptimisticResult is the outcome of an optimistic confirmation check.
type OptimisticResult struct {
	Confirmed  bool
	Confidence float64
	TimeMs     uint64
}

// slotState tracks per-slot voting state.
type slotState struct {
	votes       map[uint64]*EndgameVote // validatorIndex -> vote
	hashWeights map[types.Hash]uint64   // blockHash -> accumulated weight
	totalVoted  uint64                  // total weight that has voted
	finalized   bool
	finalHash   types.Hash
	firstVoteTs uint64 // timestamp of first vote (ms)
	finalizedTs uint64 // timestamp when finalized (ms)
}

func newSlotState() *slotState {
	return &slotState{
		votes:       make(map[uint64]*EndgameVote),
		hashWeights: make(map[types.Hash]uint64),
	}
}

// EndgameEngine is the core engine for sub-second endgame finality.
// It is fully thread-safe.
type EndgameEngine struct {
	mu               sync.RWMutex
	config           EndgameEngineConfig
	slots            map[uint64]*slotState
	validatorWeights map[uint64]uint64                      // validator index -> weight
	validatorPubkeys map[uint64][crypto.BLSPubkeySize]byte  // validator index -> BLS pubkey
	totalWeight      uint64                                 // sum of all validator weights
	finalizedChain   []types.Hash                           // ordered list of finalized block hashes
	latestFinalized  uint64                                 // highest finalized slot
	blockExecutor    func(root [32]byte) bool               // optional block execution verifier
	FinalizedCallback func(slot uint64, root [32]byte)      // called when finality is reached
}

// NewEndgameEngine creates a new endgame finality engine.
func NewEndgameEngine(config EndgameEngineConfig) *EndgameEngine {
	return &EndgameEngine{
		config:           config,
		slots:            make(map[uint64]*slotState),
		validatorWeights: make(map[uint64]uint64),
		finalizedChain:   make([]types.Hash, 0),
	}
}

// SetValidatorSet replaces the validator weight map and recomputes total weight.
func (e *EndgameEngine) SetValidatorSet(weights map[uint64]uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.validatorWeights = make(map[uint64]uint64, len(weights))
	e.totalWeight = 0
	for idx, w := range weights {
		e.validatorWeights[idx] = w
		e.totalWeight += w
	}
}

// SetValidatorPubkeys sets the BLS public keys for validators. When pubkeys
// are set, SubmitVote will verify BLS signatures on incoming votes.
func (e *EndgameEngine) SetValidatorPubkeys(pubkeys map[uint64][crypto.BLSPubkeySize]byte) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.validatorPubkeys = make(map[uint64][crypto.BLSPubkeySize]byte, len(pubkeys))
	for idx, pk := range pubkeys {
		e.validatorPubkeys[idx] = pk
	}
}

// SetBlockExecutor registers a block execution verifier function. When set,
// the engine calls this function with the candidate block root before
// declaring finality. If the function returns false, finality is not declared.
func (e *EndgameEngine) SetBlockExecutor(executor func(root [32]byte) bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.blockExecutor = executor
}

// VoteMessage constructs the canonical message that validators sign for a
// finality vote: the 8-byte big-endian slot concatenated with the 32-byte
// block root.
func VoteMessage(slot uint64, blockRoot [32]byte) []byte {
	msg := make([]byte, 8+32)
	binary.BigEndian.PutUint64(msg[:8], slot)
	copy(msg[8:], blockRoot[:])
	return msg
}

// SubmitVote adds a finality vote. Returns an error if the vote is invalid
// or a duplicate.
func (e *EndgameEngine) SubmitVote(vote *EndgameVote) error {
	if vote.Weight == 0 {
		return ErrZeroWeight
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Reject votes for slots that are too old (already pruned or finalized
	// and far behind).
	if e.latestFinalized > 0 && vote.Slot+e.config.MaxSlotHistory < e.latestFinalized {
		return ErrInvalidSlot
	}

	// Check that the validator is in the known set if we have one.
	if len(e.validatorWeights) > 0 {
		if _, ok := e.validatorWeights[vote.ValidatorIndex]; !ok {
			return ErrUnknownValidator
		}
	}

	// Verify BLS signature if validator pubkeys are configured.
	if len(e.validatorPubkeys) > 0 {
		pubkey, hasPK := e.validatorPubkeys[vote.ValidatorIndex]
		if hasPK {
			msg := VoteMessage(vote.Slot, vote.BlockHash)
			if !crypto.BLSVerify(pubkey, msg, vote.Signature) {
				return ErrInvalidSignature
			}
		}
	}

	ss := e.getOrCreateSlot(vote.Slot)

	// Reject duplicate votes from the same validator in the same slot.
	if _, exists := ss.votes[vote.ValidatorIndex]; exists {
		return ErrDuplicateVote
	}

	ss.votes[vote.ValidatorIndex] = vote
	ss.hashWeights[vote.BlockHash] += vote.Weight
	ss.totalVoted += vote.Weight

	if ss.firstVoteTs == 0 || vote.Timestamp < ss.firstVoteTs {
		ss.firstVoteTs = vote.Timestamp
	}

	// Check if any block hash has reached the finality threshold.
	if !ss.finalized {
		e.checkSlotFinality(vote.Slot, ss)
	}

	return nil
}

// getOrCreateSlot returns the slot state, creating it if needed.
// Caller must hold e.mu.
func (e *EndgameEngine) getOrCreateSlot(slot uint64) *slotState {
	ss, ok := e.slots[slot]
	if !ok {
		ss = newSlotState()
		e.slots[slot] = ss
	}
	return ss
}

// checkSlotFinality checks if any block in the slot has enough weight.
// Caller must hold e.mu.
func (e *EndgameEngine) checkSlotFinality(slot uint64, ss *slotState) {
	tw := e.totalWeight
	if tw == 0 {
		// If no validator set is configured, use totalVoted as denominator.
		// This allows the engine to work without a pre-set validator set.
		tw = ss.totalVoted
	}
	if tw == 0 {
		return
	}

	threshold := uint64(float64(tw) * e.config.FinalityThreshold)
	if threshold == 0 {
		threshold = 1
	}

	for hash, w := range ss.hashWeights {
		if w >= threshold {
			// If a block executor is set, verify block execution before
			// declaring finality.
			if e.blockExecutor != nil {
				if !e.blockExecutor(hash) {
					continue
				}
			}

			ss.finalized = true
			ss.finalHash = hash

			// Compute finality time from latest vote timestamp.
			latestTs := uint64(0)
			for _, v := range ss.votes {
				if v.Timestamp > latestTs {
					latestTs = v.Timestamp
				}
			}
			if latestTs > 0 && ss.firstVoteTs > 0 {
				ss.finalizedTs = latestTs
			}

			// Update finalized chain.
			if slot > e.latestFinalized {
				e.latestFinalized = slot
				e.finalizedChain = append(e.finalizedChain, hash)
			}

			// Invoke finality callback if set.
			if e.FinalizedCallback != nil {
				e.FinalizedCallback(slot, hash)
			}

			// Prune old slots.
			e.pruneOldSlots()
			return
		}
	}
}

// pruneOldSlots removes slots older than MaxSlotHistory from the latest
// finalized slot. Caller must hold e.mu.
func (e *EndgameEngine) pruneOldSlots() {
	if e.latestFinalized <= e.config.MaxSlotHistory {
		return
	}
	cutoff := e.latestFinalized - e.config.MaxSlotHistory
	for s := range e.slots {
		if s < cutoff {
			delete(e.slots, s)
		}
	}
}

// CheckFinality returns the finality status for a given slot.
func (e *EndgameEngine) CheckFinality(slot uint64) *EndgameResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	ss, ok := e.slots[slot]
	if !ok {
		return &EndgameResult{Slot: slot}
	}

	tw := e.totalWeight
	if tw == 0 {
		tw = ss.totalVoted
	}

	threshold := uint64(float64(tw) * e.config.FinalityThreshold)
	if threshold == 0 && tw > 0 {
		threshold = 1
	}

	result := &EndgameResult{
		Slot:        slot,
		IsFinalized: ss.finalized,
		TotalWeight: ss.totalVoted,
		Threshold:   threshold,
	}

	if ss.finalized {
		result.FinalizedHash = ss.finalHash
		if ss.finalizedTs > 0 && ss.firstVoteTs > 0 {
			result.TimeToFinality = ss.finalizedTs - ss.firstVoteTs
		}
	}

	return result
}

// ProcessSlotEnd performs end-of-slot finality processing. If the slot
// is not yet finalized, it performs a final check and returns the result.
func (e *EndgameEngine) ProcessSlotEnd(slot uint64) *EndgameResult {
	e.mu.Lock()

	ss, ok := e.slots[slot]
	if !ok {
		e.mu.Unlock()
		return &EndgameResult{Slot: slot}
	}

	if !ss.finalized {
		e.checkSlotFinality(slot, ss)
	}

	e.mu.Unlock()
	return e.CheckFinality(slot)
}

// OptimisticConfirmation checks if a block hash has sufficient weight for
// optimistic (fast, pre-finality) confirmation.
func (e *EndgameEngine) OptimisticConfirmation(blockHash types.Hash) *OptimisticResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := &OptimisticResult{}

	tw := e.totalWeight
	if tw == 0 {
		return result
	}

	// Search all active slots for this block hash.
	var bestWeight uint64
	var earliestTs, latestTs uint64

	for _, ss := range e.slots {
		w, ok := ss.hashWeights[blockHash]
		if !ok {
			continue
		}
		if w > bestWeight {
			bestWeight = w
		}
		// Track timing across all slots referencing this hash.
		for _, v := range ss.votes {
			if v.BlockHash == blockHash {
				if earliestTs == 0 || v.Timestamp < earliestTs {
					earliestTs = v.Timestamp
				}
				if v.Timestamp > latestTs {
					latestTs = v.Timestamp
				}
			}
		}
	}

	if tw > 0 {
		result.Confidence = float64(bestWeight) / float64(tw)
	}
	result.Confirmed = result.Confidence >= e.config.OptimisticThreshold

	if latestTs > 0 && earliestTs > 0 {
		result.TimeMs = latestTs - earliestTs
	}

	return result
}

// GetFinalizedChain returns a copy of the ordered finalized block hashes.
func (e *EndgameEngine) GetFinalizedChain() []types.Hash {
	e.mu.RLock()
	defer e.mu.RUnlock()

	chain := make([]types.Hash, len(e.finalizedChain))
	copy(chain, e.finalizedChain)
	return chain
}
