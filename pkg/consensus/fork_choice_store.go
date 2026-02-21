// fork_choice_store.go implements an extended fork choice store with
// LMD-GHOST using latest-message-driven protocol. Tracks the block tree,
// validator latest messages, justified/finalized checkpoints, and provides
// on_block / on_attestation handlers with old branch pruning.
// Complements forkchoice.go (basic weight accumulation) and
// fork_choice_lmd.go (explicit vote store) by combining both approaches
// with checkpoint-aware pruning and head caching.
package consensus

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// Fork choice store errors.
var (
	ErrFCSEmptyStore       = errors.New("fcs: empty store")
	ErrFCSUnknownBlock     = errors.New("fcs: unknown block")
	ErrFCSUnknownParent    = errors.New("fcs: unknown parent block")
	ErrFCSDuplicateBlock   = errors.New("fcs: duplicate block")
	ErrFCSStaleAttestation = errors.New("fcs: stale attestation")
	ErrFCSFutureSlot       = errors.New("fcs: block slot is in the future")
	ErrFCSInvalidCheckpoint = errors.New("fcs: invalid checkpoint")
)

// FCSBlockNode represents a block in the fork choice store tree.
type FCSBlockNode struct {
	Root       types.Hash
	ParentRoot types.Hash
	Slot       uint64
	StateRoot  types.Hash
	Children   []types.Hash
	// Justified and finalized checkpoints at this block.
	JustifiedEpoch Epoch
	FinalizedEpoch Epoch
}

// FCSLatestMessage records a validator's latest attestation target.
type FCSLatestMessage struct {
	ValidatorIndex ValidatorIndex
	TargetRoot     types.Hash
	TargetEpoch    Epoch
	Weight         uint64
}

// FCSCheckpoint is a checkpoint within the fork choice store.
type FCSCheckpoint struct {
	Epoch Epoch
	Root  types.Hash
}

// ForkChoiceStoreV3 is an extended fork choice store implementing LMD-GHOST
// with latest-message-driven protocol, checkpoint tracking, and pruning.
// Thread-safe.
type ForkChoiceStoreV3 struct {
	mu sync.RWMutex

	// Block tree.
	blocks map[types.Hash]*FCSBlockNode

	// Latest messages per validator.
	latestMessages map[ValidatorIndex]*FCSLatestMessage

	// Checkpoints.
	justifiedCheckpoint FCSCheckpoint
	finalizedCheckpoint FCSCheckpoint
	bestJustified       FCSCheckpoint

	// Current slot for validation.
	currentSlot uint64

	// Slots per epoch for epoch computation.
	slotsPerEpoch uint64

	// Cached head root.
	cachedHead     types.Hash
	headCacheValid bool
}

// ForkChoiceStoreV3Config configures the fork choice store.
type ForkChoiceStoreV3Config struct {
	JustifiedCheckpoint FCSCheckpoint
	FinalizedCheckpoint FCSCheckpoint
	CurrentSlot         uint64
	SlotsPerEpoch       uint64
}

// NewForkChoiceStoreV3 creates a new fork choice store.
func NewForkChoiceStoreV3(cfg ForkChoiceStoreV3Config) *ForkChoiceStoreV3 {
	spe := cfg.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	return &ForkChoiceStoreV3{
		blocks:              make(map[types.Hash]*FCSBlockNode),
		latestMessages:      make(map[ValidatorIndex]*FCSLatestMessage),
		justifiedCheckpoint: cfg.JustifiedCheckpoint,
		finalizedCheckpoint: cfg.FinalizedCheckpoint,
		bestJustified:       cfg.JustifiedCheckpoint,
		currentSlot:         cfg.CurrentSlot,
		slotsPerEpoch:       spe,
	}
}

// OnBlock handles a new block being added to the store. Validates the
// parent exists (unless it is the first block), updates the tree, and
// invalidates the head cache.
func (fcs *ForkChoiceStoreV3) OnBlock(
	root, parentRoot, stateRoot types.Hash,
	slot uint64,
	justifiedEpoch, finalizedEpoch Epoch,
) error {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()

	if _, dup := fcs.blocks[root]; dup {
		return ErrFCSDuplicateBlock
	}

	// Parent must exist unless tree is empty.
	if len(fcs.blocks) > 0 {
		parent, ok := fcs.blocks[parentRoot]
		if !ok {
			return fmt.Errorf("%w: %s", ErrFCSUnknownParent, parentRoot.Hex())
		}
		parent.Children = append(parent.Children, root)
	}

	fcs.blocks[root] = &FCSBlockNode{
		Root:           root,
		ParentRoot:     parentRoot,
		Slot:           slot,
		StateRoot:      stateRoot,
		JustifiedEpoch: justifiedEpoch,
		FinalizedEpoch: finalizedEpoch,
	}

	// Update best justified checkpoint if this block has a higher justified epoch.
	if justifiedEpoch > fcs.bestJustified.Epoch {
		fcs.bestJustified = FCSCheckpoint{Epoch: justifiedEpoch, Root: root}
	}

	fcs.headCacheValid = false
	return nil
}

// OnAttestation handles a new attestation, updating the validator's latest
// message. Only the latest attestation (by epoch) is kept per validator.
func (fcs *ForkChoiceStoreV3) OnAttestation(
	validatorIdx ValidatorIndex,
	targetRoot types.Hash,
	targetEpoch Epoch,
	weight uint64,
) error {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()

	// Target block must exist.
	if _, ok := fcs.blocks[targetRoot]; !ok {
		return fmt.Errorf("%w: %s", ErrFCSUnknownBlock, targetRoot.Hex())
	}

	// Only accept if newer than existing vote.
	if existing, ok := fcs.latestMessages[validatorIdx]; ok {
		if targetEpoch <= existing.TargetEpoch {
			return ErrFCSStaleAttestation
		}
	}

	fcs.latestMessages[validatorIdx] = &FCSLatestMessage{
		ValidatorIndex: validatorIdx,
		TargetRoot:     targetRoot,
		TargetEpoch:    targetEpoch,
		Weight:         weight,
	}

	fcs.headCacheValid = false
	return nil
}

// GetHead computes the canonical head using LMD-GHOST from the justified
// checkpoint root, caching the result until the tree or votes change.
func (fcs *ForkChoiceStoreV3) GetHead() (types.Hash, error) {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()

	if len(fcs.blocks) == 0 {
		return types.Hash{}, ErrFCSEmptyStore
	}

	if fcs.headCacheValid {
		return fcs.cachedHead, nil
	}

	start := fcs.justifiedCheckpoint.Root
	if _, ok := fcs.blocks[start]; !ok {
		start = fcs.findRoot()
		if start == (types.Hash{}) {
			return types.Hash{}, ErrFCSEmptyStore
		}
	}

	// Build subtree weights from latest messages.
	weights := fcs.computeWeights()

	current := start
	for {
		node, ok := fcs.blocks[current]
		if !ok || len(node.Children) == 0 {
			break
		}

		// Filter children to only those descended from finalized checkpoint.
		validChildren := fcs.filterViableChildren(node.Children)
		if len(validChildren) == 0 {
			break
		}

		best := validChildren[0]
		bestW := weights[best]
		for _, child := range validChildren[1:] {
			w := weights[child]
			if w > bestW || (w == bestW && fcsHashLess(child, best)) {
				best = child
				bestW = w
			}
		}
		current = best
	}

	fcs.cachedHead = current
	fcs.headCacheValid = true
	return current, nil
}

// computeWeights builds a map of block root -> subtree weight by
// propagating vote weights up from leaves to root.
// Must be called with write lock held.
func (fcs *ForkChoiceStoreV3) computeWeights() map[types.Hash]uint64 {
	weights := make(map[types.Hash]uint64, len(fcs.blocks))

	// Direct votes.
	for _, msg := range fcs.latestMessages {
		if _, ok := fcs.blocks[msg.TargetRoot]; ok {
			weights[msg.TargetRoot] += msg.Weight
		}
	}

	// Sort blocks by descending slot for bottom-up propagation.
	ordered := make([]types.Hash, 0, len(fcs.blocks))
	for h := range fcs.blocks {
		ordered = append(ordered, h)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return fcs.blocks[ordered[i]].Slot > fcs.blocks[ordered[j]].Slot
	})

	for _, h := range ordered {
		node := fcs.blocks[h]
		if _, ok := fcs.blocks[node.ParentRoot]; ok {
			weights[node.ParentRoot] += weights[h]
		}
	}

	return weights
}

// filterViableChildren returns children that are descendants of the
// finalized checkpoint (or are the finalized root itself).
// In this simplified version, all children are viable if the finalized
// root is an ancestor.
func (fcs *ForkChoiceStoreV3) filterViableChildren(children []types.Hash) []types.Hash {
	if fcs.finalizedCheckpoint.Root == (types.Hash{}) {
		return children
	}
	// If finalized root is not in the tree, accept all children.
	if _, ok := fcs.blocks[fcs.finalizedCheckpoint.Root]; !ok {
		return children
	}
	var viable []types.Hash
	for _, child := range children {
		if fcs.isDescendantOf(child, fcs.finalizedCheckpoint.Root) || child == fcs.finalizedCheckpoint.Root {
			viable = append(viable, child)
		}
	}
	if len(viable) == 0 {
		return children
	}
	return viable
}

// isDescendantOf returns true if node is a descendant of ancestor.
func (fcs *ForkChoiceStoreV3) isDescendantOf(node, ancestor types.Hash) bool {
	current := node
	visited := make(map[types.Hash]bool)
	for {
		if current == ancestor {
			return true
		}
		if visited[current] {
			return false
		}
		visited[current] = true
		block, ok := fcs.blocks[current]
		if !ok {
			return false
		}
		current = block.ParentRoot
	}
}

// findRoot returns any root node (no parent in tree). Must hold lock.
func (fcs *ForkChoiceStoreV3) findRoot() types.Hash {
	for h, node := range fcs.blocks {
		if _, ok := fcs.blocks[node.ParentRoot]; !ok {
			return h
		}
	}
	return types.Hash{}
}

// SetJustifiedCheckpoint updates the justified checkpoint.
func (fcs *ForkChoiceStoreV3) SetJustifiedCheckpoint(cp FCSCheckpoint) {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()
	fcs.justifiedCheckpoint = cp
	fcs.headCacheValid = false
}

// SetFinalizedCheckpoint updates the finalized checkpoint.
func (fcs *ForkChoiceStoreV3) SetFinalizedCheckpoint(cp FCSCheckpoint) {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()
	fcs.finalizedCheckpoint = cp
	fcs.headCacheValid = false
}

// GetJustifiedCheckpoint returns the current justified checkpoint.
func (fcs *ForkChoiceStoreV3) GetJustifiedCheckpoint() FCSCheckpoint {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	return fcs.justifiedCheckpoint
}

// GetFinalizedCheckpoint returns the current finalized checkpoint.
func (fcs *ForkChoiceStoreV3) GetFinalizedCheckpoint() FCSCheckpoint {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	return fcs.finalizedCheckpoint
}

// AdvanceSlot advances the current slot.
func (fcs *ForkChoiceStoreV3) AdvanceSlot(slot uint64) {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()
	fcs.currentSlot = slot
}

// PruneBeforeFinalized removes all blocks not descended from the
// finalized checkpoint. Returns the number of blocks pruned.
func (fcs *ForkChoiceStoreV3) PruneBeforeFinalized() int {
	fcs.mu.Lock()
	defer fcs.mu.Unlock()

	finalRoot := fcs.finalizedCheckpoint.Root
	if _, ok := fcs.blocks[finalRoot]; !ok {
		return 0
	}

	keep := make(map[types.Hash]bool)
	fcs.collectDescendantsV3(finalRoot, keep)

	pruned := 0
	for h := range fcs.blocks {
		if !keep[h] {
			delete(fcs.blocks, h)
			pruned++
		}
	}

	// Clear parent on new root.
	if node, ok := fcs.blocks[finalRoot]; ok {
		node.ParentRoot = types.Hash{}
	}

	fcs.headCacheValid = false
	return pruned
}

// collectDescendantsV3 recursively collects root and its descendants.
func (fcs *ForkChoiceStoreV3) collectDescendantsV3(root types.Hash, keep map[types.Hash]bool) {
	keep[root] = true
	node, ok := fcs.blocks[root]
	if !ok {
		return
	}
	for _, child := range node.Children {
		fcs.collectDescendantsV3(child, keep)
	}
}

// HasBlock returns whether the store contains a block.
func (fcs *ForkChoiceStoreV3) HasBlock(root types.Hash) bool {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	_, ok := fcs.blocks[root]
	return ok
}

// BlockCount returns the number of blocks in the store.
func (fcs *ForkChoiceStoreV3) BlockCount() int {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	return len(fcs.blocks)
}

// MessageCount returns the number of latest messages stored.
func (fcs *ForkChoiceStoreV3) MessageCount() int {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	return len(fcs.latestMessages)
}

// GetBlock returns the block node for a given root.
func (fcs *ForkChoiceStoreV3) GetBlock(root types.Hash) *FCSBlockNode {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	node := fcs.blocks[root]
	if node == nil {
		return nil
	}
	cp := *node
	return &cp
}

// GetLatestMessage returns the latest message for a validator.
func (fcs *ForkChoiceStoreV3) GetLatestMessage(idx ValidatorIndex) *FCSLatestMessage {
	fcs.mu.RLock()
	defer fcs.mu.RUnlock()
	msg := fcs.latestMessages[idx]
	if msg == nil {
		return nil
	}
	cp := *msg
	return &cp
}

// fcsHashLess returns true if a < b lexicographically.
func fcsHashLess(a, b types.Hash) bool {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// computeFCSBlockRoot derives a deterministic hash for a block.
func computeFCSBlockRoot(slot uint64, parentRoot types.Hash) types.Hash {
	data := make([]byte, 0, 8+32)
	s := slot
	data = append(data, byte(s), byte(s>>8), byte(s>>16), byte(s>>24),
		byte(s>>32), byte(s>>40), byte(s>>48), byte(s>>56))
	data = append(data, parentRoot[:]...)
	return crypto.Keccak256Hash(data)
}
