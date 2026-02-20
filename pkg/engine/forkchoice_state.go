// forkchoice_state.go implements fork choice state management for the Engine API.
//
// This provides the execution-layer's view of the consensus fork choice:
//   - Justified and finalized checkpoint tracking
//   - Head block determination with safe/unsafe distinction
//   - Proposer boost accounting per the LMD-GHOST fork choice rule
//   - Reorg detection and notification to subscribers
//
// The ForkchoiceStateManager sits between the Engine API and the block store,
// maintaining a consistent view of the canonical chain head as the CL sends
// forkchoiceUpdated calls. It detects reorgs by comparing the new head against
// the previous head and notifies registered listeners.
//
// Reference: consensus-specs/specs/phase0/fork-choice.md, execution-apis
package engine

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Fork choice state errors.
var (
	ErrFCStateNilUpdate       = errors.New("forkchoice_state: nil update")
	ErrFCStateZeroHead        = errors.New("forkchoice_state: head block hash is zero")
	ErrFCStateFinalizedAhead  = errors.New("forkchoice_state: finalized hash ahead of justified")
	ErrFCStateHeadNotFound    = errors.New("forkchoice_state: head block not found in store")
	ErrFCStateSafeNotAncestor = errors.New("forkchoice_state: safe block not ancestor of head")
)

// Checkpoint represents a justified or finalized checkpoint.
type Checkpoint struct {
	// Epoch is the checkpoint epoch.
	Epoch uint64

	// Root is the block root at the checkpoint boundary.
	Root types.Hash
}

// ProposerBoost holds the current proposer boost state per LMD-GHOST.
// The proposer of the current slot receives a boost to its fork-choice
// weight for a limited duration to prevent short-range reorgs.
type ProposerBoost struct {
	// Slot is the slot for which the boost is active.
	Slot uint64

	// BlockRoot is the root of the block receiving the boost.
	BlockRoot types.Hash

	// BoostWeight is the additional weight applied (committee weight * 40/100).
	BoostWeight uint64
}

// ReorgEvent describes a chain reorganization detected by the fork choice.
type ReorgEvent struct {
	// Slot is the slot at which the reorg was detected.
	Slot uint64

	// OldHead is the previous head block hash.
	OldHead types.Hash

	// NewHead is the new head block hash after the reorg.
	NewHead types.Hash

	// Depth is the number of blocks reorganized (distance to common ancestor).
	Depth uint64

	// OldHeadNumber is the block number of the old head.
	OldHeadNumber uint64

	// NewHeadNumber is the block number of the new head.
	NewHeadNumber uint64
}

// ReorgListener is a callback invoked when a chain reorg is detected.
type ReorgListener func(event ReorgEvent)

// BlockInfo stores minimal block metadata needed for fork choice.
type BlockInfo struct {
	Hash       types.Hash
	ParentHash types.Hash
	Number     uint64
	Slot       uint64
}

// ForkchoiceStateManager manages the fork choice state on the execution layer.
// It tracks justified/finalized checkpoints, maintains the head/safe/finalized
// block distinction, accounts for proposer boost, and detects reorgs.
//
// All public methods are safe for concurrent use.
type ForkchoiceStateManager struct {
	mu sync.RWMutex

	// Current fork choice pointers.
	headHash      types.Hash
	safeHash      types.Hash
	finalizedHash types.Hash

	// Checkpoint tracking.
	justifiedCheckpoint Checkpoint
	finalizedCheckpoint Checkpoint

	// Proposer boost state.
	currentBoost *ProposerBoost

	// Block metadata store for ancestry checks and reorg depth.
	blocks map[types.Hash]*BlockInfo

	// Reorg detection.
	reorgListeners []ReorgListener

	// Statistics.
	updateCount uint64
	reorgCount  uint64
}

// NewForkchoiceStateManager creates a new fork choice state manager.
// If genesis is non-nil, it is used as the initial head/safe/finalized block.
func NewForkchoiceStateManager(genesis *BlockInfo) *ForkchoiceStateManager {
	m := &ForkchoiceStateManager{
		blocks: make(map[types.Hash]*BlockInfo),
	}
	if genesis != nil {
		m.blocks[genesis.Hash] = genesis
		m.headHash = genesis.Hash
		m.safeHash = genesis.Hash
		m.finalizedHash = genesis.Hash
		m.justifiedCheckpoint = Checkpoint{Root: genesis.Hash}
		m.finalizedCheckpoint = Checkpoint{Root: genesis.Hash}
	}
	return m
}

// AddBlock registers a block in the fork choice store. This must be called
// for all blocks the node knows about so ancestry lookups work.
func (m *ForkchoiceStateManager) AddBlock(info *BlockInfo) {
	if info == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blocks[info.Hash] = info
}

// ProcessForkchoiceUpdate applies a forkchoice state update from the CL.
// It validates the update, detects reorgs, updates checkpoints, and
// notifies registered listeners of any reorg.
func (m *ForkchoiceStateManager) ProcessForkchoiceUpdate(update ForkchoiceStateV1) error {
	if update.HeadBlockHash == (types.Hash{}) {
		return ErrFCStateZeroHead
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.updateCount++

	// Verify the head block is known.
	headInfo, headKnown := m.blocks[update.HeadBlockHash]
	if !headKnown {
		return fmt.Errorf("%w: %s", ErrFCStateHeadNotFound, update.HeadBlockHash.Hex())
	}

	// Detect reorg: if the new head differs from the old head, and the new
	// head is not a direct descendant of the old head, it is a reorg.
	oldHead := m.headHash
	var reorgEvent *ReorgEvent
	if oldHead != (types.Hash{}) && oldHead != update.HeadBlockHash {
		oldInfo := m.blocks[oldHead]
		if oldInfo != nil && !m.isAncestorLocked(oldHead, update.HeadBlockHash) {
			depth := m.reorgDepthLocked(oldHead, update.HeadBlockHash)
			reorgEvent = &ReorgEvent{
				Slot:          headInfo.Slot,
				OldHead:       oldHead,
				NewHead:       update.HeadBlockHash,
				Depth:         depth,
				OldHeadNumber: oldInfo.Number,
				NewHeadNumber: headInfo.Number,
			}
			m.reorgCount++
		}
	}

	// Update fork choice pointers.
	m.headHash = update.HeadBlockHash
	m.safeHash = update.SafeBlockHash
	m.finalizedHash = update.FinalizedBlockHash

	// Update finalized checkpoint if the finalized hash changed.
	if update.FinalizedBlockHash != (types.Hash{}) {
		if finInfo, ok := m.blocks[update.FinalizedBlockHash]; ok {
			m.finalizedCheckpoint = Checkpoint{
				Epoch: finInfo.Slot / 32, // slots per epoch
				Root:  update.FinalizedBlockHash,
			}
		}
	}

	// Update justified checkpoint from safe hash (safe ~ justified in PoS).
	if update.SafeBlockHash != (types.Hash{}) {
		if safeInfo, ok := m.blocks[update.SafeBlockHash]; ok {
			m.justifiedCheckpoint = Checkpoint{
				Epoch: safeInfo.Slot / 32,
				Root:  update.SafeBlockHash,
			}
		}
	}

	// Notify reorg listeners outside the critical data updates (but still
	// under lock to ensure ordering; listeners should not block).
	if reorgEvent != nil {
		for _, listener := range m.reorgListeners {
			listener(*reorgEvent)
		}
	}

	return nil
}

// Head returns the current head block hash.
func (m *ForkchoiceStateManager) Head() types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.headHash
}

// SafeHead returns the current safe (justified) block hash.
func (m *ForkchoiceStateManager) SafeHead() types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.safeHash
}

// FinalizedHead returns the current finalized block hash.
func (m *ForkchoiceStateManager) FinalizedHead() types.Hash {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.finalizedHash
}

// HeadInfo returns full block info for the current head, or nil if unknown.
func (m *ForkchoiceStateManager) HeadInfo() *BlockInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	info := m.blocks[m.headHash]
	if info == nil {
		return nil
	}
	cp := *info
	return &cp
}

// JustifiedCheckpoint returns the current justified checkpoint.
func (m *ForkchoiceStateManager) JustifiedCheckpoint() Checkpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.justifiedCheckpoint
}

// FinalizedCheckpoint returns the current finalized checkpoint.
func (m *ForkchoiceStateManager) FinalizedCheckpoint() Checkpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.finalizedCheckpoint
}

// SetProposerBoost sets the proposer boost for the current slot.
// This gives extra fork-choice weight to the timely block.
func (m *ForkchoiceStateManager) SetProposerBoost(slot uint64, blockRoot types.Hash, weight uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentBoost = &ProposerBoost{
		Slot:        slot,
		BlockRoot:   blockRoot,
		BoostWeight: weight,
	}
}

// ClearProposerBoost clears the current proposer boost (e.g., at slot boundary).
func (m *ForkchoiceStateManager) ClearProposerBoost() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentBoost = nil
}

// ProposerBoostFor returns the proposer boost for a given block root, or zero
// if no boost is active or the boost is for a different block.
func (m *ForkchoiceStateManager) ProposerBoostFor(blockRoot types.Hash) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentBoost != nil && m.currentBoost.BlockRoot == blockRoot {
		return m.currentBoost.BoostWeight
	}
	return 0
}

// GetCurrentBoost returns a copy of the current proposer boost, or nil.
func (m *ForkchoiceStateManager) GetCurrentBoost() *ProposerBoost {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentBoost == nil {
		return nil
	}
	cp := *m.currentBoost
	return &cp
}

// IsHeadSafe returns true if the current head equals the safe head.
func (m *ForkchoiceStateManager) IsHeadSafe() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.headHash == m.safeHash && m.headHash != (types.Hash{})
}

// IsHeadFinalized returns true if the current head equals the finalized head.
func (m *ForkchoiceStateManager) IsHeadFinalized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.headHash == m.finalizedHash && m.headHash != (types.Hash{})
}

// OnReorg registers a listener that is called whenever a chain reorg is detected.
func (m *ForkchoiceStateManager) OnReorg(listener ReorgListener) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reorgListeners = append(m.reorgListeners, listener)
}

// Stats returns fork choice statistics.
func (m *ForkchoiceStateManager) Stats() (updateCount, reorgCount uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.updateCount, m.reorgCount
}

// GetForkchoiceState returns the current fork choice state as a
// ForkchoiceStateV1 suitable for Engine API responses.
func (m *ForkchoiceStateManager) GetForkchoiceState() ForkchoiceStateV1 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ForkchoiceStateV1{
		HeadBlockHash:      m.headHash,
		SafeBlockHash:      m.safeHash,
		FinalizedBlockHash: m.finalizedHash,
	}
}

// PruneBeforeNumber removes block metadata for blocks with number < n.
// Does not remove blocks that are currently referenced as head/safe/finalized.
func (m *ForkchoiceStateManager) PruneBeforeNumber(n uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	pruned := 0
	for hash, info := range m.blocks {
		if info.Number < n {
			// Do not prune referenced blocks.
			if hash == m.headHash || hash == m.safeHash || hash == m.finalizedHash {
				continue
			}
			delete(m.blocks, hash)
			pruned++
		}
	}
	return pruned
}

// BlockCount returns the number of blocks in the store.
func (m *ForkchoiceStateManager) BlockCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.blocks)
}

// --- internal helpers ---

// isAncestorLocked checks if ancestorHash is an ancestor of descendantHash
// by walking the parent chain. Caller must hold at least m.mu read lock.
func (m *ForkchoiceStateManager) isAncestorLocked(ancestorHash, descendantHash types.Hash) bool {
	current := descendantHash
	// Walk up to 1024 blocks to find the ancestor (bounded to avoid infinite loops).
	for i := 0; i < 1024; i++ {
		if current == ancestorHash {
			return true
		}
		info, ok := m.blocks[current]
		if !ok {
			return false
		}
		if info.ParentHash == current {
			// Self-referencing (genesis or broken chain).
			return false
		}
		current = info.ParentHash
	}
	return false
}

// reorgDepthLocked computes the depth of a reorg by finding the common
// ancestor between oldHead and newHead. Returns 0 if no common ancestor
// is found within 1024 blocks. Caller must hold at least m.mu read lock.
func (m *ForkchoiceStateManager) reorgDepthLocked(oldHead, newHead types.Hash) uint64 {
	// Collect ancestors of oldHead.
	oldAncestors := make(map[types.Hash]uint64) // hash -> distance from oldHead
	current := oldHead
	for dist := uint64(0); dist < 1024; dist++ {
		oldAncestors[current] = dist
		info, ok := m.blocks[current]
		if !ok {
			break
		}
		if info.ParentHash == current {
			break
		}
		current = info.ParentHash
	}

	// Walk newHead's ancestors to find the first one in oldAncestors.
	current = newHead
	for dist := uint64(0); dist < 1024; dist++ {
		if oldDist, found := oldAncestors[current]; found {
			// Depth is the max of both distances to the common ancestor.
			if dist > oldDist {
				return dist
			}
			return oldDist
		}
		info, ok := m.blocks[current]
		if !ok {
			break
		}
		if info.ParentHash == current {
			break
		}
		current = info.ParentHash
	}

	return 0
}
