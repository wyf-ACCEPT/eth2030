// fork_choice_v2.go implements a proto-array based LMD GHOST + Casper FFG
// fork choice rule per the Ethereum consensus specification.
//
// Proto-array stores the block tree as a flat array with parent pointers,
// enabling O(n) weight propagation from leaves to root. This is an optimized
// alternative to the recursive tree walk in forkchoice.go.
package consensus

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Fork choice v2 errors.
var (
	ErrV2UnknownBlock     = errors.New("fork_choice_v2: unknown block root")
	ErrV2UnknownParent    = errors.New("fork_choice_v2: unknown parent root")
	ErrV2DuplicateBlock   = errors.New("fork_choice_v2: duplicate block root")
	ErrV2InvalidEpoch     = errors.New("fork_choice_v2: invalid epoch for attestation")
	ErrV2NoViableHead     = errors.New("fork_choice_v2: no viable head found")
	ErrV2InvalidValidator = errors.New("fork_choice_v2: invalid validator index")
)

// invalidIndex is a sentinel for unset parent/child/descendant indices.
const invalidIndex = ^uint64(0)

// ProtoNode is a single node in the proto-array. Each node corresponds to a
// block and stores its ancestry, checkpoint epochs, accumulated weight, and
// best-descendant pointers for O(1) head lookup after weight propagation.
type ProtoNode struct {
	Slot            uint64
	Root            types.Hash
	Parent          uint64 // index in ProtoArray.nodes, or invalidIndex
	JustifiedEpoch  Epoch
	FinalizedEpoch  Epoch
	Weight          uint64 // direct attestation weight on this node
	BestChild       uint64 // index of the best child, or invalidIndex
	BestDescendant  uint64 // index of the best leaf descendant, or invalidIndex
}

// ProtoArray is the backing store for the proto-array fork choice.
// Nodes are stored in insertion order; indices grow monotonically.
type ProtoArray struct {
	nodes      []ProtoNode
	indexMap   map[types.Hash]uint64 // root -> index in nodes
	pruneCount uint64                // cumulative pruned node count (index offset)
}

// newProtoArray creates an empty proto-array.
func newProtoArray() *ProtoArray {
	return &ProtoArray{
		nodes:    make([]ProtoNode, 0, 256),
		indexMap: make(map[types.Hash]uint64, 256),
	}
}

// ForkChoiceV2 implements LMD GHOST + Casper FFG using a proto-array.
// All public methods are thread-safe.
type ForkChoiceV2 struct {
	mu sync.RWMutex

	protoArray *ProtoArray

	// Validator vote tracking: validatorIndex -> latest vote.
	votes map[ValidatorIndex]*fcVote

	// Checkpoint tracking.
	justifiedCheckpoint Checkpoint
	finalizedCheckpoint Checkpoint

	// Validator balances for weight computation.
	balances map[ValidatorIndex]uint64
}

// fcVote tracks a validator's latest fork choice vote.
type fcVote struct {
	currentRoot types.Hash
	currentEpoch Epoch
	nextRoot     types.Hash
	nextEpoch    Epoch
}

// NewForkChoiceV2 creates a new proto-array fork choice store seeded with the
// given justified and finalized checkpoints.
func NewForkChoiceV2(justified, finalized Checkpoint) *ForkChoiceV2 {
	return &ForkChoiceV2{
		protoArray:          newProtoArray(),
		votes:               make(map[ValidatorIndex]*fcVote),
		justifiedCheckpoint: justified,
		finalizedCheckpoint: finalized,
		balances:            make(map[ValidatorIndex]uint64),
	}
}

// SetBalance sets the effective balance for a validator, used during weight
// propagation.
func (fc *ForkChoiceV2) SetBalance(idx ValidatorIndex, balance uint64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.balances[idx] = balance
}

// OnBlock processes a new block. It adds a node to the proto-array and updates
// best-child/best-descendant pointers. The parent must already exist in the
// array (except for the first block which becomes the root).
func (fc *ForkChoiceV2) OnBlock(slot uint64, root, parentRoot types.Hash, justifiedEpoch, finalizedEpoch Epoch) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, exists := fc.protoArray.indexMap[root]; exists {
		return ErrV2DuplicateBlock
	}

	parentIdx := invalidIndex
	if len(fc.protoArray.nodes) > 0 {
		pi, ok := fc.protoArray.indexMap[parentRoot]
		if !ok {
			return ErrV2UnknownParent
		}
		parentIdx = pi
	}

	nodeIdx := uint64(len(fc.protoArray.nodes)) + fc.protoArray.pruneCount
	node := ProtoNode{
		Slot:           slot,
		Root:           root,
		Parent:         parentIdx,
		JustifiedEpoch: justifiedEpoch,
		FinalizedEpoch: finalizedEpoch,
		Weight:         0,
		BestChild:      invalidIndex,
		BestDescendant: invalidIndex,
	}
	fc.protoArray.nodes = append(fc.protoArray.nodes, node)
	fc.protoArray.indexMap[root] = nodeIdx

	// Update parent's best-child/best-descendant if applicable.
	if parentIdx != invalidIndex {
		fc.maybeUpdateBestChildAndDescendant(parentIdx, nodeIdx)
	}

	return nil
}

// OnAttestation records an attestation vote from a validator. The vote is
// buffered and applied during the next GetHead call via weight propagation.
func (fc *ForkChoiceV2) OnAttestation(validatorIndex ValidatorIndex, root types.Hash, epoch Epoch) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	vote, exists := fc.votes[validatorIndex]
	if !exists {
		fc.votes[validatorIndex] = &fcVote{
			nextRoot:  root,
			nextEpoch: epoch,
		}
		return
	}

	// Only update if the attestation is for a newer or equal epoch.
	if epoch >= vote.nextEpoch {
		vote.nextRoot = root
		vote.nextEpoch = epoch
	}
}

// GetHead computes the current head block root using LMD GHOST, filtered by
// Casper FFG finalization constraints.
func (fc *ForkChoiceV2) GetHead() (types.Hash, error) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if len(fc.protoArray.nodes) == 0 {
		return types.Hash{}, ErrV2NoViableHead
	}

	// Apply pending votes as weight deltas and propagate.
	fc.applyVoteDeltas()
	fc.propagateWeights()

	// Start from justified root.
	justifiedIdx, ok := fc.protoArray.indexMap[fc.justifiedCheckpoint.Root]
	if !ok {
		return types.Hash{}, ErrV2NoViableHead
	}

	bestIdx := justifiedIdx
	for {
		node := fc.nodeAt(bestIdx)
		if node == nil {
			return types.Hash{}, ErrV2NoViableHead
		}

		if node.BestDescendant != invalidIndex {
			desc := fc.nodeAt(node.BestDescendant)
			if desc != nil && fc.isNodeViable(desc) {
				bestIdx = node.BestDescendant
				return desc.Root, nil
			}
		}

		// If the best child is set, walk down the best-child chain.
		if node.BestChild == invalidIndex {
			return node.Root, nil
		}

		child := fc.nodeAt(node.BestChild)
		if child == nil || !fc.isNodeViable(child) {
			return node.Root, nil
		}

		bestIdx = node.BestChild
	}
}

// GetJustifiedCheckpoint returns the current justified checkpoint.
func (fc *ForkChoiceV2) GetJustifiedCheckpoint() Checkpoint {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.justifiedCheckpoint
}

// GetFinalizedCheckpoint returns the current finalized checkpoint.
func (fc *ForkChoiceV2) GetFinalizedCheckpoint() Checkpoint {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.finalizedCheckpoint
}

// UpdateJustifiedCheckpoint updates the justified checkpoint if the new one
// has a higher epoch.
func (fc *ForkChoiceV2) UpdateJustifiedCheckpoint(cp Checkpoint) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if cp.Epoch > fc.justifiedCheckpoint.Epoch {
		fc.justifiedCheckpoint = cp
	}
}

// UpdateFinalizedCheckpoint updates the finalized checkpoint if the new one
// has a higher epoch.
func (fc *ForkChoiceV2) UpdateFinalizedCheckpoint(cp Checkpoint) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if cp.Epoch > fc.finalizedCheckpoint.Epoch {
		fc.finalizedCheckpoint = cp
	}
}

// Prune removes nodes below the finalized slot from the proto-array.
func (fc *ForkChoiceV2) Prune(finalizedRoot types.Hash) int {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	finalizedIdx, ok := fc.protoArray.indexMap[finalizedRoot]
	if !ok {
		return 0
	}

	// Determine how many nodes to prune (all nodes before the finalized node).
	localIdx := fc.localIndex(finalizedIdx)
	if localIdx == 0 {
		return 0
	}

	pruneCount := localIdx

	// Remove pruned roots from the index map.
	for i := uint64(0); i < pruneCount; i++ {
		delete(fc.protoArray.indexMap, fc.protoArray.nodes[i].Root)
	}

	// Slice the nodes array.
	fc.protoArray.nodes = fc.protoArray.nodes[pruneCount:]
	fc.protoArray.pruneCount += pruneCount

	// Fix the finalized node's parent.
	if len(fc.protoArray.nodes) > 0 {
		fc.protoArray.nodes[0].Parent = invalidIndex
	}

	return int(pruneCount)
}

// HasBlock returns whether the proto-array contains a block with the given root.
func (fc *ForkChoiceV2) HasBlock(root types.Hash) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	_, ok := fc.protoArray.indexMap[root]
	return ok
}

// BlockCount returns the number of blocks in the proto-array.
func (fc *ForkChoiceV2) BlockCount() int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.protoArray.nodes)
}

// nodeAt returns a pointer to the node at the given absolute index, or nil if
// out of bounds. Must be called with the lock held.
func (fc *ForkChoiceV2) nodeAt(absIdx uint64) *ProtoNode {
	li := fc.localIndex(absIdx)
	if li >= uint64(len(fc.protoArray.nodes)) {
		return nil
	}
	return &fc.protoArray.nodes[li]
}

// localIndex converts an absolute index to a local slice index.
func (fc *ForkChoiceV2) localIndex(absIdx uint64) uint64 {
	if absIdx < fc.protoArray.pruneCount {
		return invalidIndex
	}
	return absIdx - fc.protoArray.pruneCount
}

// isNodeViable checks whether a node descends from the finalized checkpoint.
// A node is viable if its finalized_epoch matches the store's finalized epoch
// or is at the genesis epoch, and its justified_epoch is at least the store's
// justified epoch or is at the genesis epoch.
func (fc *ForkChoiceV2) isNodeViable(node *ProtoNode) bool {
	justOK := node.JustifiedEpoch == fc.justifiedCheckpoint.Epoch ||
		fc.justifiedCheckpoint.Epoch == 0
	finOK := node.FinalizedEpoch == fc.finalizedCheckpoint.Epoch ||
		fc.finalizedCheckpoint.Epoch == 0
	return justOK && finOK
}

// applyVoteDeltas processes buffered attestation votes, computing weight deltas
// between previous and current votes for each validator. Must be called with
// the lock held.
func (fc *ForkChoiceV2) applyVoteDeltas() {
	for _, vote := range fc.votes {
		if vote.currentRoot == vote.nextRoot && vote.currentEpoch == vote.nextEpoch {
			continue
		}
		vote.currentRoot = vote.nextRoot
		vote.currentEpoch = vote.nextEpoch
	}
}

// propagateWeights recomputes all node weights from validator votes. It first
// zeroes all weights, then adds vote weights bottom-up from leaves to root.
// Must be called with the lock held.
func (fc *ForkChoiceV2) propagateWeights() {
	nodes := fc.protoArray.nodes

	// Zero all weights.
	for i := range nodes {
		nodes[i].Weight = 0
	}

	// Accumulate direct vote weights.
	for valIdx, vote := range fc.votes {
		absIdx, ok := fc.protoArray.indexMap[vote.currentRoot]
		if !ok {
			continue
		}
		bal := fc.balances[valIdx]
		li := fc.localIndex(absIdx)
		if li < uint64(len(nodes)) {
			nodes[li].Weight += bal
		}
	}

	// Propagate from leaves to root (reverse order since parents always have
	// lower indices than children in the proto-array).
	for i := len(nodes) - 1; i >= 1; i-- {
		parentAbsIdx := nodes[i].Parent
		if parentAbsIdx == invalidIndex {
			continue
		}
		pli := fc.localIndex(parentAbsIdx)
		if pli < uint64(len(nodes)) {
			nodes[pli].Weight += nodes[i].Weight
		}
	}

	// Recompute best-child and best-descendant bottom-up.
	for i := len(nodes) - 1; i >= 0; i-- {
		nodes[i].BestChild = invalidIndex
		nodes[i].BestDescendant = invalidIndex
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		parentAbsIdx := nodes[i].Parent
		if parentAbsIdx == invalidIndex {
			continue
		}
		nodeAbsIdx := uint64(i) + fc.protoArray.pruneCount
		fc.maybeUpdateBestChildAndDescendant(parentAbsIdx, nodeAbsIdx)
	}
}

// maybeUpdateBestChildAndDescendant updates a parent's best-child and
// best-descendant pointers if the given child is better than the current best.
// "Better" means: higher weight, with ties broken by lexicographically higher
// root hash (matching the spec: max(weight, root)).
// Must be called with the lock held.
func (fc *ForkChoiceV2) maybeUpdateBestChildAndDescendant(parentAbsIdx, childAbsIdx uint64) {
	parent := fc.nodeAt(parentAbsIdx)
	child := fc.nodeAt(childAbsIdx)
	if parent == nil || child == nil {
		return
	}

	childViable := fc.isNodeViable(child)

	// Determine the child's best leaf: itself or its best descendant.
	childLeafIdx := childAbsIdx
	if child.BestDescendant != invalidIndex {
		desc := fc.nodeAt(child.BestDescendant)
		if desc != nil && fc.isNodeViable(desc) {
			childLeafIdx = child.BestDescendant
		} else if !childViable {
			// Neither child nor its best descendant is viable.
			return
		}
	} else if !childViable {
		return
	}

	if parent.BestChild == invalidIndex {
		// No current best child; this child wins.
		parent.BestChild = childAbsIdx
		parent.BestDescendant = childLeafIdx
		return
	}

	currentBest := fc.nodeAt(parent.BestChild)
	if currentBest == nil {
		parent.BestChild = childAbsIdx
		parent.BestDescendant = childLeafIdx
		return
	}

	// Compare weights, ties broken by higher root hash.
	childWeight := child.Weight
	bestWeight := currentBest.Weight

	if childWeight > bestWeight ||
		(childWeight == bestWeight && hashGreater(child.Root, currentBest.Root)) {
		parent.BestChild = childAbsIdx
		parent.BestDescendant = childLeafIdx
	}
}

// hashGreater returns true if a > b in lexicographic (byte) order.
func hashGreater(a, b types.Hash) bool {
	for i := 0; i < len(a); i++ {
		if a[i] > b[i] {
			return true
		}
		if a[i] < b[i] {
			return false
		}
	}
	return false
}
