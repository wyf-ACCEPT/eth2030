package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Fork choice errors.
var (
	ErrFinalizedBlockUnknown = errors.New("finalized block not found")
	ErrSafeBlockUnknown      = errors.New("safe block not found")
	ErrHeadBlockUnknown      = errors.New("head block not found")
	ErrReorgPastFinalized    = errors.New("reorg would revert past finalized block")
	ErrCommonAncestorNotFound = errors.New("common ancestor not found")
	ErrInvalidFinalizedChain = errors.New("finalized block not in head's ancestry")
	ErrInvalidSafeChain      = errors.New("safe block not in head's ancestry")
	ErrSafeNotFinalized      = errors.New("safe block number is below finalized block number")
)

// ForkChoice tracks the consensus layer's view of the chain: head, safe,
// and finalized block pointers. It coordinates with the Blockchain to
// perform chain reorganizations when the CL updates the fork choice.
type ForkChoice struct {
	mu sync.RWMutex
	bc *Blockchain

	// Head is the latest validated block the CL considers canonical.
	head *types.Block

	// Safe is the latest block that is safe from re-orgs (enough attestations).
	safe *types.Block

	// Finalized is the latest block that can never be reverted.
	finalized *types.Block
}

// NewForkChoice creates a new ForkChoice tracker backed by the given blockchain.
// It initializes head, safe, and finalized to the current blockchain head.
func NewForkChoice(bc *Blockchain) *ForkChoice {
	head := bc.CurrentBlock()
	return &ForkChoice{
		bc:        bc,
		head:      head,
		safe:      head,
		finalized: head,
	}
}

// Head returns the current head block.
func (fc *ForkChoice) Head() *types.Block {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.head
}

// Safe returns the current safe block.
func (fc *ForkChoice) Safe() *types.Block {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.safe
}

// Finalized returns the current finalized block.
func (fc *ForkChoice) Finalized() *types.Block {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.finalized
}

// ForkchoiceUpdate processes a forkchoice state update from the consensus
// layer. Per the Engine API spec (engine_forkchoiceUpdated):
//
//  1. Validate that headBlockHash, safeBlockHash, and finalizedBlockHash
//     all refer to known blocks.
//  2. Validate that finalizedBlockHash is an ancestor of headBlockHash.
//  3. Validate that safeBlockHash is an ancestor of headBlockHash and
//     its block number >= finalized block number.
//  4. If headBlockHash differs from the current canonical head, trigger
//     a chain reorg (unless it would revert past finalized).
//  5. Update the head, safe, and finalized pointers.
func (fc *ForkChoice) ForkchoiceUpdate(headHash, safeHash, finalizedHash types.Hash) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	// Look up the head block.
	headBlock := fc.bc.GetBlock(headHash)
	if headBlock == nil {
		return fmt.Errorf("%w: %v", ErrHeadBlockUnknown, headHash)
	}

	// Look up the finalized block. A zero hash means "no finalized block yet",
	// so we keep the current finalized pointer.
	var finalizedBlock *types.Block
	if finalizedHash == (types.Hash{}) {
		finalizedBlock = fc.finalized
	} else {
		finalizedBlock = fc.bc.GetBlock(finalizedHash)
		if finalizedBlock == nil {
			return fmt.Errorf("%w: %v", ErrFinalizedBlockUnknown, finalizedHash)
		}
	}

	// Look up the safe block. A zero hash means "no safe block yet".
	var safeBlock *types.Block
	if safeHash == (types.Hash{}) {
		safeBlock = fc.safe
	} else {
		safeBlock = fc.bc.GetBlock(safeHash)
		if safeBlock == nil {
			return fmt.Errorf("%w: %v", ErrSafeBlockUnknown, safeHash)
		}
	}

	// Validate: safe block number must not be below finalized block number.
	if safeBlock.NumberU64() < finalizedBlock.NumberU64() {
		return fmt.Errorf("%w: safe=%d < finalized=%d",
			ErrSafeNotFinalized, safeBlock.NumberU64(), finalizedBlock.NumberU64())
	}

	// Validate: finalized block must be in the head block's ancestry.
	if finalizedBlock.NumberU64() > 0 || finalizedHash != (types.Hash{}) {
		if !fc.isAncestor(finalizedBlock, headBlock) {
			return fmt.Errorf("%w: finalized=%v not ancestor of head=%v",
				ErrInvalidFinalizedChain, finalizedHash, headHash)
		}
	}

	// Validate: safe block must be in the head block's ancestry.
	if safeHash != (types.Hash{}) {
		if !fc.isAncestor(safeBlock, headBlock) {
			return fmt.Errorf("%w: safe=%v not ancestor of head=%v",
				ErrInvalidSafeChain, safeHash, headHash)
		}
	}

	// Determine the effective finalized boundary: finalization is monotonic,
	// so use the higher of the existing and incoming finalized blocks.
	effectiveFinalized := fc.finalized
	if finalizedBlock.NumberU64() > effectiveFinalized.NumberU64() {
		effectiveFinalized = finalizedBlock
	}

	// If the head is changing, we may need a reorg.
	currentHead := fc.bc.CurrentBlock()
	if headBlock.Hash() != currentHead.Hash() {
		// Check that the reorg doesn't revert past the effective finalized block.
		if effectiveFinalized.NumberU64() > 0 {
			ancestor := FindCommonAncestor(fc.bc, currentHead, headBlock)
			if ancestor != nil && ancestor.NumberU64() < effectiveFinalized.NumberU64() {
				return fmt.Errorf("%w: common ancestor at %d, finalized at %d",
					ErrReorgPastFinalized, ancestor.NumberU64(), effectiveFinalized.NumberU64())
			}
		}

		// Perform the reorg.
		if err := fc.bc.Reorg(headBlock); err != nil {
			return fmt.Errorf("reorg to %v: %w", headHash, err)
		}
	}

	// Update pointers.
	fc.head = headBlock
	fc.safe = safeBlock
	fc.finalized = finalizedBlock

	return nil
}

// isAncestor checks whether 'ancestor' is in the ancestry chain of 'descendant'.
// It walks back from descendant to ancestor's block number and checks the hash.
func (fc *ForkChoice) isAncestor(ancestor, descendant *types.Block) bool {
	if ancestor.NumberU64() > descendant.NumberU64() {
		return false
	}
	if ancestor.Hash() == descendant.Hash() {
		return true
	}

	// Walk back from descendant to the ancestor's block number.
	current := descendant
	for current.NumberU64() > ancestor.NumberU64() {
		parent := fc.bc.GetBlock(current.ParentHash())
		if parent == nil {
			return false
		}
		current = parent
	}
	return current.Hash() == ancestor.Hash()
}

// FindCommonAncestor walks back both chains from oldHead and newHead until
// it finds a block that exists in both chains' ancestry. Returns nil if
// no common ancestor can be found (should not happen in a valid chain since
// genesis is always shared).
func FindCommonAncestor(bc *Blockchain, oldHead, newHead *types.Block) *types.Block {
	if oldHead == nil || newHead == nil {
		return nil
	}

	old := oldHead
	new := newHead

	// First, bring both chains to the same height by walking back the longer one.
	for old.NumberU64() > new.NumberU64() {
		parent := bc.GetBlock(old.ParentHash())
		if parent == nil {
			return nil
		}
		old = parent
	}
	for new.NumberU64() > old.NumberU64() {
		parent := bc.GetBlock(new.ParentHash())
		if parent == nil {
			return nil
		}
		new = parent
	}

	// Now walk both back in lockstep until hashes match.
	for old.Hash() != new.Hash() {
		if old.NumberU64() == 0 {
			// Reached genesis without matching -- should not happen in valid chains.
			return nil
		}
		oldParent := bc.GetBlock(old.ParentHash())
		newParent := bc.GetBlock(new.ParentHash())
		if oldParent == nil || newParent == nil {
			return nil
		}
		old = oldParent
		new = newParent
	}

	return old
}

// ReorgWithValidation is a higher-level reorg that validates against
// finalization before executing. It finds the common ancestor, verifies
// the fork point is not below the finalized block, then calls Blockchain.Reorg.
func (fc *ForkChoice) ReorgWithValidation(newHead *types.Block) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	currentHead := fc.bc.CurrentBlock()
	if currentHead.Hash() == newHead.Hash() {
		return nil // no-op
	}

	// Find common ancestor.
	ancestor := FindCommonAncestor(fc.bc, currentHead, newHead)
	if ancestor == nil {
		return ErrCommonAncestorNotFound
	}

	// Check against finalized block.
	if fc.finalized != nil && ancestor.NumberU64() < fc.finalized.NumberU64() {
		return fmt.Errorf("%w: fork point at %d, finalized at %d",
			ErrReorgPastFinalized, ancestor.NumberU64(), fc.finalized.NumberU64())
	}

	// Execute the reorg.
	if err := fc.bc.Reorg(newHead); err != nil {
		return err
	}

	// Update head to the new head (safe/finalized unchanged).
	fc.head = newHead
	return nil
}
