package consensus

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

var (
	ErrUnknownParent = errors.New("forkchoice: unknown parent block")
	ErrDuplicateBlock = errors.New("forkchoice: duplicate block")
	ErrUnknownBlock  = errors.New("forkchoice: unknown block")
)

// ForkChoiceConfig holds the initial configuration for a fork choice store.
type ForkChoiceConfig struct {
	JustifiedEpoch    uint64
	FinalizedEpoch    uint64
	ProposerBoostRoot types.Hash
}

// BlockNode represents a block in the fork choice tree.
type BlockNode struct {
	Hash       types.Hash
	ParentHash types.Hash
	Slot       uint64
	Weight     uint64
	Children   []types.Hash
}

// ForkChoiceStore implements LMD-GHOST fork choice. It maintains a tree of
// blocks and their accumulated attestation weights, and computes the
// canonical head by greedily selecting the heaviest subtree at each fork.
type ForkChoiceStore struct {
	mu sync.RWMutex

	nodes map[types.Hash]*BlockNode

	justifiedEpoch uint64
	justifiedRoot  types.Hash
	finalizedEpoch uint64
	finalizedRoot  types.Hash

	proposerBoostRoot types.Hash
}

// NewForkChoiceStore returns a new empty ForkChoiceStore with the given config.
func NewForkChoiceStore(config ForkChoiceConfig) *ForkChoiceStore {
	return &ForkChoiceStore{
		nodes:             make(map[types.Hash]*BlockNode),
		justifiedEpoch:    config.JustifiedEpoch,
		finalizedEpoch:    config.FinalizedEpoch,
		proposerBoostRoot: config.ProposerBoostRoot,
	}
}

// AddBlock inserts a new block into the fork choice tree. The parent must
// already be present (or the tree must be empty, in which case the block
// becomes the root). Returns ErrDuplicateBlock if the hash already exists
// and ErrUnknownParent if the parent is not found.
func (fc *ForkChoiceStore) AddBlock(hash, parentHash types.Hash, slot uint64) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, exists := fc.nodes[hash]; exists {
		return ErrDuplicateBlock
	}

	// Allow the first block to have a missing parent (genesis / justified root).
	if len(fc.nodes) > 0 {
		parent, ok := fc.nodes[parentHash]
		if !ok {
			return ErrUnknownParent
		}
		parent.Children = append(parent.Children, hash)
	}

	fc.nodes[hash] = &BlockNode{
		Hash:       hash,
		ParentHash: parentHash,
		Slot:       slot,
	}
	return nil
}

// AddAttestation adds vote weight to a block. The weight propagates up
// toward the root automatically during head computation via subtreeWeight.
func (fc *ForkChoiceStore) AddAttestation(hash types.Hash, weight uint64) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if node, ok := fc.nodes[hash]; ok {
		node.Weight += weight
	}
}

// GetHead computes the head of the chain using LMD-GHOST. Starting from
// the justified root, it greedily picks the child with the highest
// subtree weight at each fork until reaching a leaf.
func (fc *ForkChoiceStore) GetHead() types.Hash {
	fc.mu.RLock()
	defer fc.mu.RUnlock()

	// Find the starting root. Prefer the justified root if it exists in the
	// tree; otherwise fall back to any root (node whose parent is absent).
	start := fc.justifiedRoot
	if _, ok := fc.nodes[start]; !ok {
		start = fc.findRoot()
		if start == (types.Hash{}) {
			return types.Hash{}
		}
	}

	current := start
	for {
		node, ok := fc.nodes[current]
		if !ok {
			return current
		}
		if len(node.Children) == 0 {
			return current
		}
		// Pick the child subtree with the most weight, breaking ties by
		// hash (deterministic).
		best := node.Children[0]
		bestWeight := fc.subtreeWeight(best)
		for _, child := range node.Children[1:] {
			w := fc.subtreeWeight(child)
			if w > bestWeight || (w == bestWeight && hashLess(child, best)) {
				best = child
				bestWeight = w
			}
		}
		current = best
	}
}

// subtreeWeight returns the total attestation weight of a block and all
// its descendants. Must be called with at least a read lock held.
func (fc *ForkChoiceStore) subtreeWeight(hash types.Hash) uint64 {
	node, ok := fc.nodes[hash]
	if !ok {
		return 0
	}
	total := node.Weight
	for _, child := range node.Children {
		total += fc.subtreeWeight(child)
	}
	return total
}

// findRoot returns the hash of a root node (one whose parent is not in the
// tree). Must be called with at least a read lock held. Returns the zero
// hash if the tree is empty.
func (fc *ForkChoiceStore) findRoot() types.Hash {
	for h, node := range fc.nodes {
		if _, ok := fc.nodes[node.ParentHash]; !ok {
			return h
		}
	}
	return types.Hash{}
}

// hashLess returns true if a < b in lexicographic (byte) order.
func hashLess(a, b types.Hash) bool {
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

// SetJustified sets the justified epoch and root.
func (fc *ForkChoiceStore) SetJustified(epoch uint64, root types.Hash) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.justifiedEpoch = epoch
	fc.justifiedRoot = root
}

// SetFinalized sets the finalized epoch and root.
func (fc *ForkChoiceStore) SetFinalized(epoch uint64, root types.Hash) {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.finalizedEpoch = epoch
	fc.finalizedRoot = root
}

// GetJustifiedRoot returns the current justified block root.
func (fc *ForkChoiceStore) GetJustifiedRoot() types.Hash {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.justifiedRoot
}

// GetFinalizedRoot returns the current finalized block root.
func (fc *ForkChoiceStore) GetFinalizedRoot() types.Hash {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.finalizedRoot
}

// Prune removes all blocks that are not descendants of finalizedRoot.
// After pruning, finalizedRoot becomes the new root of the tree.
func (fc *ForkChoiceStore) Prune(finalizedRoot types.Hash) {
	fc.mu.Lock()
	defer fc.mu.Unlock()

	if _, ok := fc.nodes[finalizedRoot]; !ok {
		return
	}

	// Collect all descendants of finalizedRoot (including itself).
	keep := make(map[types.Hash]bool)
	fc.collectDescendants(finalizedRoot, keep)

	// Remove all nodes not in the keep set.
	for h := range fc.nodes {
		if !keep[h] {
			delete(fc.nodes, h)
		}
	}

	// Clear the finalized root's parent reference and remove stale children
	// links that pointed to pruned nodes.
	if node, ok := fc.nodes[finalizedRoot]; ok {
		node.ParentHash = types.Hash{}
	}
}

// collectDescendants adds hash and all its descendants to the keep set.
// Must be called with the write lock held.
func (fc *ForkChoiceStore) collectDescendants(hash types.Hash, keep map[types.Hash]bool) {
	keep[hash] = true
	node, ok := fc.nodes[hash]
	if !ok {
		return
	}
	for _, child := range node.Children {
		fc.collectDescendants(child, keep)
	}
}

// HasBlock returns whether a block with the given hash exists in the store.
func (fc *ForkChoiceStore) HasBlock(hash types.Hash) bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	_, ok := fc.nodes[hash]
	return ok
}

// BlockCount returns the number of blocks in the store.
func (fc *ForkChoiceStore) BlockCount() int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.nodes)
}
