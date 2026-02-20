// fork_choice_lmd.go implements a standalone LMD-GHOST fork choice that
// tracks individual validator votes, computes the canonical head via
// latest-message-driven greedy heaviest observed subtree, and supports
// reorg detection and tree pruning.
//
// Unlike forkchoice.go (weight accumulation) and fork_choice_v2.go
// (proto-array), this implementation maintains an explicit VoteStore
// that maps each validator to their latest target, recalculating
// subtree weights on demand from the full vote set.
package consensus

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// LMD-GHOST specific errors.
var (
	ErrLMDUnknownBlock     = errors.New("lmd-ghost: unknown block")
	ErrLMDUnknownParent    = errors.New("lmd-ghost: unknown parent block")
	ErrLMDDuplicateBlock   = errors.New("lmd-ghost: duplicate block")
	ErrLMDEmptyTree        = errors.New("lmd-ghost: empty block tree")
	ErrLMDStaleAttestation = errors.New("lmd-ghost: attestation for stale epoch")
	ErrLMDInvalidValidator = errors.New("lmd-ghost: invalid validator index")
)

// LMDBlockNode represents a block in the LMD-GHOST tree. It holds
// parent/child links plus the slot for pruning decisions.
type LMDBlockNode struct {
	Root       types.Hash
	ParentRoot types.Hash
	Slot       uint64
	Children   []types.Hash
}

// LMDVote records a single validator's latest fork choice vote.
type LMDVote struct {
	ValidatorIndex ValidatorIndex
	TargetRoot     types.Hash
	TargetEpoch    Epoch
	Weight         uint64 // effective balance
}

// VoteStore tracks every validator's latest attestation target. Only the
// latest (by epoch) vote per validator is retained.
type VoteStore struct {
	mu    sync.RWMutex
	votes map[ValidatorIndex]*LMDVote
}

// NewVoteStore creates an empty vote store.
func NewVoteStore() *VoteStore {
	return &VoteStore{votes: make(map[ValidatorIndex]*LMDVote)}
}

// RecordVote upserts a vote. If the validator already has a vote for an
// equal or newer epoch the call is a no-op and returns false.
func (vs *VoteStore) RecordVote(vote *LMDVote) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	existing, ok := vs.votes[vote.ValidatorIndex]
	if ok && existing.TargetEpoch > vote.TargetEpoch {
		return false
	}
	vs.votes[vote.ValidatorIndex] = vote
	return true
}

// GetVote returns the current vote for a validator, or nil.
func (vs *VoteStore) GetVote(idx ValidatorIndex) *LMDVote {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.votes[idx]
}

// AllVotes returns a snapshot of all current votes.
func (vs *VoteStore) AllVotes() []*LMDVote {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	result := make([]*LMDVote, 0, len(vs.votes))
	for _, v := range vs.votes {
		result = append(result, v)
	}
	return result
}

// Len returns the number of validators with recorded votes.
func (vs *VoteStore) Len() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.votes)
}

// RemoveVote removes a validator's vote. Returns true if a vote was removed.
func (vs *VoteStore) RemoveVote(idx ValidatorIndex) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	_, ok := vs.votes[idx]
	if ok {
		delete(vs.votes, idx)
	}
	return ok
}

// LMDGhostForkChoice implements the Latest-Message-Driven Greedy Heaviest
// Observed SubTree (LMD-GHOST) fork choice rule. It maintains a block tree
// and a vote store, recomputing the canonical head on demand.
//
// Thread-safe: all public methods are protected by a mutex.
type LMDGhostForkChoice struct {
	mu sync.RWMutex

	// Block tree indexed by root hash.
	blocks map[types.Hash]*LMDBlockNode

	// Vote tracking.
	votes *VoteStore

	// Anchor state: justified root starts the head computation.
	justifiedRoot  types.Hash
	justifiedEpoch Epoch
	finalizedRoot  types.Hash
	finalizedEpoch Epoch

	// slotsPerEpoch for slot-to-epoch conversion.
	slotsPerEpoch uint64

	// prevHead tracks the last computed head for reorg detection.
	prevHead types.Hash
}

// LMDGhostConfig configures the LMD-GHOST fork choice.
type LMDGhostConfig struct {
	JustifiedRoot  types.Hash
	JustifiedEpoch Epoch
	FinalizedRoot  types.Hash
	FinalizedEpoch Epoch
	SlotsPerEpoch  uint64
}

// NewLMDGhost creates a new LMD-GHOST fork choice with the given config.
// The justified root must be added via OnBlock before calling GetHead.
func NewLMDGhost(cfg LMDGhostConfig) *LMDGhostForkChoice {
	spe := cfg.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	return &LMDGhostForkChoice{
		blocks:         make(map[types.Hash]*LMDBlockNode),
		votes:          NewVoteStore(),
		justifiedRoot:  cfg.JustifiedRoot,
		justifiedEpoch: cfg.JustifiedEpoch,
		finalizedRoot:  cfg.FinalizedRoot,
		finalizedEpoch: cfg.FinalizedEpoch,
		slotsPerEpoch:  spe,
	}
}

// OnBlock registers a new block in the fork choice tree. The parent must
// already exist unless this is the first block added.
func (fc *LMDGhostForkChoice) OnBlock(root, parentRoot types.Hash, slot uint64) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, dup := fc.blocks[root]; dup {
		return ErrLMDDuplicateBlock
	}

	if len(fc.blocks) > 0 {
		parent, ok := fc.blocks[parentRoot]
		if !ok {
			return ErrLMDUnknownParent
		}
		parent.Children = append(parent.Children, root)
	}

	fc.blocks[root] = &LMDBlockNode{
		Root:       root,
		ParentRoot: parentRoot,
		Slot:       slot,
	}
	return nil
}

// ProcessAttestation records a validator's attestation for a block root.
// Stale attestations (epoch < existing vote's epoch) are rejected.
func (fc *LMDGhostForkChoice) ProcessAttestation(validatorIdx ValidatorIndex, targetRoot types.Hash, epoch Epoch, weight uint64) error {
	fc.mu.RLock()
	_, known := fc.blocks[targetRoot]
	fc.mu.RUnlock()

	if !known {
		return fmt.Errorf("%w: %s", ErrLMDUnknownBlock, targetRoot.Hex())
	}

	vote := &LMDVote{
		ValidatorIndex: validatorIdx,
		TargetRoot:     targetRoot,
		TargetEpoch:    epoch,
		Weight:         weight,
	}
	if !fc.votes.RecordVote(vote) {
		return ErrLMDStaleAttestation
	}
	return nil
}

// GetHead computes the canonical head by walking the tree from the justified
// root, at each fork selecting the child with the highest accumulated vote
// weight. Returns the head root and whether a reorg occurred since the last
// call.
func (fc *LMDGhostForkChoice) GetHead() (types.Hash, bool, error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if len(fc.blocks) == 0 {
		return types.Hash{}, false, ErrLMDEmptyTree
	}

	start := fc.justifiedRoot
	if _, ok := fc.blocks[start]; !ok {
		// Fall back to any root node.
		start = fc.findAnyRoot()
		if start == (types.Hash{}) {
			return types.Hash{}, false, ErrLMDEmptyTree
		}
	}

	// Build weight map from votes.
	weights := fc.computeSubtreeWeights()

	current := start
	for {
		node, ok := fc.blocks[current]
		if !ok || len(node.Children) == 0 {
			break
		}

		best := node.Children[0]
		bestW := weights[best]
		for _, child := range node.Children[1:] {
			w := weights[child]
			if w > bestW || (w == bestW && hashLess(child, best)) {
				best = child
				bestW = w
			}
		}
		current = best
	}

	reorg := fc.prevHead != (types.Hash{}) && fc.prevHead != current
	fc.prevHead = current
	return current, reorg, nil
}

// computeSubtreeWeights aggregates vote weights through the tree.
// Each block accumulates its direct votes plus all descendant weights.
// Must be called with the write lock held.
func (fc *LMDGhostForkChoice) computeSubtreeWeights() map[types.Hash]uint64 {
	weights := make(map[types.Hash]uint64, len(fc.blocks))

	// Accumulate direct vote weights.
	for _, vote := range fc.votes.AllVotes() {
		if _, ok := fc.blocks[vote.TargetRoot]; ok {
			weights[vote.TargetRoot] += vote.Weight
		}
	}

	// Propagate weights bottom-up. We process nodes in reverse-slot order
	// so children are processed before parents.
	ordered := make([]types.Hash, 0, len(fc.blocks))
	for h := range fc.blocks {
		ordered = append(ordered, h)
	}
	// Sort descending by slot so children come first.
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			if fc.blocks[ordered[i]].Slot < fc.blocks[ordered[j]].Slot {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}

	for _, h := range ordered {
		node := fc.blocks[h]
		if parent, ok := fc.blocks[node.ParentRoot]; ok {
			_ = parent // just confirm parent exists
			weights[node.ParentRoot] += weights[h]
		}
	}

	return weights
}

// findAnyRoot returns the root of the tree (a block whose parent is not
// in the tree). Must be called with the lock held.
func (fc *LMDGhostForkChoice) findAnyRoot() types.Hash {
	for h, node := range fc.blocks {
		if _, ok := fc.blocks[node.ParentRoot]; !ok {
			return h
		}
	}
	return types.Hash{}
}

// HasBlock returns whether a block is in the fork choice tree.
func (fc *LMDGhostForkChoice) HasBlock(root types.Hash) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	_, ok := fc.blocks[root]
	return ok
}

// BlockCount returns the number of blocks in the tree.
func (fc *LMDGhostForkChoice) BlockCount() int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.blocks)
}

// VoteCount returns the number of recorded validator votes.
func (fc *LMDGhostForkChoice) VoteCount() int {
	return fc.votes.Len()
}

// SetJustified updates the justified checkpoint root and epoch.
func (fc *LMDGhostForkChoice) SetJustified(root types.Hash, epoch Epoch) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.justifiedRoot = root
	fc.justifiedEpoch = epoch
}

// SetFinalized updates the finalized checkpoint root and epoch.
func (fc *LMDGhostForkChoice) SetFinalized(root types.Hash, epoch Epoch) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.finalizedRoot = root
	fc.finalizedEpoch = epoch
}

// Prune removes all blocks that are not descendants of the given root
// (and not the root itself). After pruning, the root becomes the new
// tree root. Returns the number of blocks removed.
func (fc *LMDGhostForkChoice) Prune(root types.Hash) int {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, ok := fc.blocks[root]; !ok {
		return 0
	}

	// Collect descendants of root.
	keep := make(map[types.Hash]bool)
	fc.collectDescs(root, keep)

	pruned := 0
	for h := range fc.blocks {
		if !keep[h] {
			delete(fc.blocks, h)
			pruned++
		}
	}

	// Clear parent reference on the new root.
	if node, ok := fc.blocks[root]; ok {
		node.ParentRoot = types.Hash{}
	}

	return pruned
}

// collectDescs recursively collects root and all its descendants.
func (fc *LMDGhostForkChoice) collectDescs(root types.Hash, keep map[types.Hash]bool) {
	keep[root] = true
	node, ok := fc.blocks[root]
	if !ok {
		return
	}
	for _, child := range node.Children {
		fc.collectDescs(child, keep)
	}
}

// GetBlock returns the block node for a given root, or nil.
func (fc *LMDGhostForkChoice) GetBlock(root types.Hash) *LMDBlockNode {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.blocks[root]
}

// Votes returns the underlying vote store for direct access.
func (fc *LMDGhostForkChoice) Votes() *VoteStore {
	return fc.votes
}
