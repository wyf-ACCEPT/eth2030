// forkchoice_engine.go implements the full forkchoice processing engine for the
// Engine API. It manages head/safe/finalized block tracking, validates forkchoice
// state transitions, determines when to build new payloads, and maintains a
// payload cache for in-progress payload construction.
//
// This sits between the JSON-RPC handler and the ForkchoiceStateManager,
// providing the core orchestration logic for engine_forkchoiceUpdated calls.
package engine

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Forkchoice engine errors.
var (
	ErrFCEHeadUnknown            = errors.New("forkchoice_engine: head block hash is unknown")
	ErrFCESafeUnknown            = errors.New("forkchoice_engine: safe block hash is unknown")
	ErrFCEFinalizedUnknown       = errors.New("forkchoice_engine: finalized block hash is unknown")
	ErrFCEHeadZero               = errors.New("forkchoice_engine: head block hash is zero")
	ErrFCESafeNotAncestorOfHead  = errors.New("forkchoice_engine: safe block is not ancestor of head")
	ErrFCEFinalNotAncestorOfSafe = errors.New("forkchoice_engine: finalized block is not ancestor of safe")
	ErrFCEInvalidTransition      = errors.New("forkchoice_engine: invalid chain transition")
	ErrFCEAlreadyFinalized       = errors.New("forkchoice_engine: block already finalized")
	ErrFCEPayloadBuildFailed     = errors.New("forkchoice_engine: failed to start payload build")
	ErrFCENilAttributes          = errors.New("forkchoice_engine: nil payload attributes")
	ErrFCETimestampRegression    = errors.New("forkchoice_engine: timestamp regression in attributes")
)

// ForkchoiceState holds the three block hashes defining the forkchoice view.
// These are set by the consensus layer via engine_forkchoiceUpdated.
type ForkchoiceState struct {
	// HeadBlockHash is the block the CL considers as the current chain head.
	HeadBlockHash [32]byte

	// SafeBlockHash is the highest block that has been justified (safe to build on).
	SafeBlockHash [32]byte

	// FinalizedBlockHash is the highest finalized block (irreversible).
	FinalizedBlockHash [32]byte
}

// ForkchoiceResponse is the result of processing a forkchoice update.
type ForkchoiceResponse struct {
	// PayloadStatus indicates the validity of the head block.
	PayloadStatus PayloadStatusV1

	// PayloadID is set when payload building was started (non-zero).
	PayloadID PayloadID
}

// BlockLookup provides block information for forkchoice validation.
// The forkchoice engine uses this to verify block existence and ancestry.
type BlockLookup interface {
	// HasBlock returns true if the block with the given hash is known.
	HasBlock(hash [32]byte) bool

	// GetBlockNumber returns the block number for a given hash, or 0 and false if unknown.
	GetBlockNumber(hash [32]byte) (uint64, bool)

	// GetParentHash returns the parent hash of the block, or zero and false if unknown.
	GetParentHash(hash [32]byte) ([32]byte, bool)

	// GetBlockTimestamp returns the timestamp of the block, or 0 and false if unknown.
	GetBlockTimestamp(hash [32]byte) (uint64, bool)
}

// ForkchoiceEngine manages forkchoice state and processes forkchoice updates
// from the consensus layer. It validates state transitions, tracks the
// canonical chain pointers, and initiates payload building.
type ForkchoiceEngine struct {
	mu sync.RWMutex

	// Current forkchoice pointers.
	headBlockHash      [32]byte
	safeBlockHash      [32]byte
	finalizedBlockHash [32]byte

	// Block lookup for validation.
	blocks BlockLookup

	// payloadCache stores recently built payload IDs.
	payloadCache map[PayloadID]bool

	// syncing indicates whether the node is currently syncing.
	syncing bool

	// Statistics.
	updateCount   uint64
	buildCount    uint64
	invalidCount  uint64
}

// NewForkchoiceEngine creates a new forkchoice engine with the given block lookup.
func NewForkchoiceEngine(blocks BlockLookup) *ForkchoiceEngine {
	return &ForkchoiceEngine{
		blocks:       blocks,
		payloadCache: make(map[PayloadID]bool),
	}
}

// SetSyncing sets the syncing state of the engine. When syncing, forkchoice
// updates return SYNCING status instead of processing the update.
func (e *ForkchoiceEngine) SetSyncing(syncing bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.syncing = syncing
}

// IsSyncing returns whether the engine is in syncing state.
func (e *ForkchoiceEngine) IsSyncing() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.syncing
}

// ProcessForkchoiceUpdate processes a forkchoice state update from the consensus
// layer. It validates the state, updates the chain pointers, and optionally starts
// building a new payload if attributes are provided.
//
// Per the Engine API spec:
//   - The forkchoice state is validated first.
//   - If the head block is unknown, SYNCING is returned.
//   - If attributes are provided and valid, a new payload build is started.
//   - The payload ID is returned for subsequent engine_getPayload calls.
func (e *ForkchoiceEngine) ProcessForkchoiceUpdate(
	state ForkchoiceState,
	attrs *PayloadAttributesV3,
) (*ForkchoiceResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.updateCount++

	// Validate basic forkchoice state structure.
	if err := e.validateForkchoiceStateLocked(state); err != nil {
		return nil, err
	}

	// If we are syncing and the head is unknown, return SYNCING.
	if e.syncing && !e.blocks.HasBlock(state.HeadBlockHash) {
		return &ForkchoiceResponse{
			PayloadStatus: PayloadStatusV1{
				Status: StatusSyncing,
			},
		}, nil
	}

	// Verify the head block is known.
	if !e.blocks.HasBlock(state.HeadBlockHash) {
		return &ForkchoiceResponse{
			PayloadStatus: PayloadStatusV1{
				Status: StatusSyncing,
			},
		}, nil
	}

	// Validate the chain: safe must be ancestor of head, finalized ancestor of safe.
	if state.SafeBlockHash != ([32]byte{}) && state.SafeBlockHash != state.HeadBlockHash {
		if !e.isAncestorLocked(state.SafeBlockHash, state.HeadBlockHash) {
			return nil, ErrFCESafeNotAncestorOfHead
		}
	}
	if state.FinalizedBlockHash != ([32]byte{}) && state.FinalizedBlockHash != state.SafeBlockHash {
		if !e.isAncestorLocked(state.FinalizedBlockHash, state.SafeBlockHash) {
			return nil, ErrFCEFinalNotAncestorOfSafe
		}
	}

	// Update chain pointers.
	e.headBlockHash = state.HeadBlockHash
	if state.SafeBlockHash != ([32]byte{}) {
		e.safeBlockHash = state.SafeBlockHash
	}
	if state.FinalizedBlockHash != ([32]byte{}) {
		e.finalizedBlockHash = state.FinalizedBlockHash
	}

	headHash := types.Hash(state.HeadBlockHash)
	resp := &ForkchoiceResponse{
		PayloadStatus: PayloadStatusV1{
			Status:          StatusValid,
			LatestValidHash: &headHash,
		},
	}

	// If payload attributes are provided, start building a new payload.
	if attrs != nil {
		if err := e.validatePayloadAttributesLocked(state, attrs); err != nil {
			return nil, err
		}

		payloadID := e.generatePayloadID(state.HeadBlockHash, attrs)
		e.payloadCache[payloadID] = true
		e.buildCount++
		resp.PayloadID = payloadID
	}

	return resp, nil
}

// validateForkchoiceStateLocked validates the structural integrity of a
// ForkchoiceState. Caller must hold e.mu.
func (e *ForkchoiceEngine) validateForkchoiceStateLocked(state ForkchoiceState) error {
	if state.HeadBlockHash == ([32]byte{}) {
		return ErrFCEHeadZero
	}
	return nil
}

// ValidateForkchoiceState validates a ForkchoiceState without applying it.
// This is a public wrapper for validation without side effects.
func (e *ForkchoiceEngine) ValidateForkchoiceState(state ForkchoiceState) error {
	if state.HeadBlockHash == ([32]byte{}) {
		return ErrFCEHeadZero
	}
	if !e.blocks.HasBlock(state.HeadBlockHash) {
		return ErrFCEHeadUnknown
	}
	if state.SafeBlockHash != ([32]byte{}) && !e.blocks.HasBlock(state.SafeBlockHash) {
		return ErrFCESafeUnknown
	}
	if state.FinalizedBlockHash != ([32]byte{}) && !e.blocks.HasBlock(state.FinalizedBlockHash) {
		return ErrFCEFinalizedUnknown
	}
	return nil
}

// UpdateHead sets the canonical chain head to the given block hash.
func (e *ForkchoiceEngine) UpdateHead(blockHash [32]byte) error {
	if !e.blocks.HasBlock(blockHash) {
		return ErrFCEHeadUnknown
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.headBlockHash = blockHash
	return nil
}

// UpdateSafe sets the safe (justified) block hash.
func (e *ForkchoiceEngine) UpdateSafe(blockHash [32]byte) error {
	if blockHash != ([32]byte{}) && !e.blocks.HasBlock(blockHash) {
		return ErrFCESafeUnknown
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.safeBlockHash = blockHash
	return nil
}

// UpdateFinalized sets the finalized block hash.
func (e *ForkchoiceEngine) UpdateFinalized(blockHash [32]byte) error {
	if blockHash != ([32]byte{}) && !e.blocks.HasBlock(blockHash) {
		return ErrFCEFinalizedUnknown
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.finalizedBlockHash = blockHash
	return nil
}

// HeadBlock returns the current head block hash.
func (e *ForkchoiceEngine) HeadBlock() [32]byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.headBlockHash
}

// SafeBlock returns the current safe block hash.
func (e *ForkchoiceEngine) SafeBlock() [32]byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.safeBlockHash
}

// FinalizedBlock returns the current finalized block hash.
func (e *ForkchoiceEngine) FinalizedBlock() [32]byte {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.finalizedBlockHash
}

// IsValidTransition checks whether a transition from parentHash to blockHash
// represents a valid chain link (blockHash has parentHash as its parent).
func (e *ForkchoiceEngine) IsValidTransition(parentHash, blockHash [32]byte) bool {
	if !e.blocks.HasBlock(blockHash) {
		return false
	}
	actualParent, ok := e.blocks.GetParentHash(blockHash)
	if !ok {
		return false
	}
	return actualParent == parentHash
}

// ShouldBuildPayload determines whether a new payload should be built given
// the forkchoice state and optional attributes. Payload building occurs only
// when attributes are provided, the head block is known, and the attributes
// contain a valid timestamp.
func (e *ForkchoiceEngine) ShouldBuildPayload(
	state ForkchoiceState,
	attrs *PayloadAttributesV3,
) bool {
	if attrs == nil {
		return false
	}
	if state.HeadBlockHash == ([32]byte{}) {
		return false
	}
	if !e.blocks.HasBlock(state.HeadBlockHash) {
		return false
	}
	if attrs.Timestamp == 0 {
		return false
	}
	// Ensure timestamp progresses beyond the head block.
	headTimestamp, ok := e.blocks.GetBlockTimestamp(state.HeadBlockHash)
	if ok && attrs.Timestamp <= headTimestamp {
		return false
	}
	return true
}

// Stats returns forkchoice engine statistics.
func (e *ForkchoiceEngine) Stats() (updates, builds, invalids uint64) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.updateCount, e.buildCount, e.invalidCount
}

// GetState returns the current forkchoice state.
func (e *ForkchoiceEngine) GetState() ForkchoiceState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return ForkchoiceState{
		HeadBlockHash:      e.headBlockHash,
		SafeBlockHash:      e.safeBlockHash,
		FinalizedBlockHash: e.finalizedBlockHash,
	}
}

// HasPayload returns whether a payload ID is known to the engine.
func (e *ForkchoiceEngine) HasPayload(id PayloadID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.payloadCache[id]
}

// isAncestorLocked walks the parent chain from descendant to see if ancestor
// is in the chain. Limited to 1024 steps. Caller must hold e.mu.
func (e *ForkchoiceEngine) isAncestorLocked(ancestor, descendant [32]byte) bool {
	current := descendant
	for i := 0; i < 1024; i++ {
		if current == ancestor {
			return true
		}
		parent, ok := e.blocks.GetParentHash(current)
		if !ok {
			return false
		}
		if parent == current {
			// Self-referencing (genesis or broken).
			return false
		}
		current = parent
	}
	return false
}

// validatePayloadAttributesLocked validates payload attributes before starting
// a payload build. Caller must hold e.mu.
func (e *ForkchoiceEngine) validatePayloadAttributesLocked(
	state ForkchoiceState,
	attrs *PayloadAttributesV3,
) error {
	if attrs.Timestamp == 0 {
		return fmt.Errorf("%w: timestamp is zero", ErrFCENilAttributes)
	}
	// Check timestamp progression.
	headTimestamp, ok := e.blocks.GetBlockTimestamp(state.HeadBlockHash)
	if ok && attrs.Timestamp <= headTimestamp {
		return fmt.Errorf("%w: attrs timestamp %d <= head timestamp %d",
			ErrFCETimestampRegression, attrs.Timestamp, headTimestamp)
	}
	// Beacon root must be provided for V3+ attributes.
	if attrs.ParentBeaconBlockRoot == (types.Hash{}) {
		return ErrInvalidPayloadAttributes
	}
	return nil
}

// generatePayloadID creates a deterministic-ish payload ID from the head hash
// and payload attributes. In production this would use a proper derivation;
// here we combine head hash bytes with timestamp and some randomness.
func (e *ForkchoiceEngine) generatePayloadID(headHash [32]byte, attrs *PayloadAttributesV3) PayloadID {
	var id PayloadID
	// Mix head hash and timestamp.
	copy(id[:4], headHash[:4])
	binary.BigEndian.PutUint32(id[4:], uint32(attrs.Timestamp))
	// Add some randomness to avoid collisions.
	var rb [4]byte
	rand.Read(rb[:])
	id[0] ^= rb[0]
	id[1] ^= rb[1]
	return id
}
