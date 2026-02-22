package core

import (
	"errors"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Chain reorg errors.
var (
	ErrReorgTooDeep      = errors.New("chain reorg: exceeds max depth")
	ErrReorgZeroHash     = errors.New("chain reorg: zero block hash")
	ErrReorgUnknownBlock = errors.New("chain reorg: unknown block number")
)

// ReorgConfig configures the chain reorganization handler.
type ReorgConfig struct {
	MaxReorgDepth uint64 // maximum allowed reorg depth (0 = unlimited)
	NotifyOnReorg bool   // whether to record reorg events
	TrackMetrics  bool   // whether to track reorg metrics
}

// DefaultReorgConfig returns sensible defaults for reorg handling.
func DefaultReorgConfig() ReorgConfig {
	return ReorgConfig{
		MaxReorgDepth: 64,
		NotifyOnReorg: true,
		TrackMetrics:  true,
	}
}

// ReorgEvent records details of a chain reorganization.
type ReorgEvent struct {
	OldHead   types.Hash // hash of the old canonical head
	NewHead   types.Hash // hash of the new canonical head
	OldBlocks []uint64   // block numbers removed from canonical chain
	NewBlocks []uint64   // block numbers added to canonical chain
	Depth     uint64     // depth of the reorganization
	Timestamp int64      // unix timestamp when reorg was detected
}

// ChainReorgHandler detects and records chain reorganizations by tracking
// the canonical chain state. All methods are safe for concurrent use.
type ChainReorgHandler struct {
	mu     sync.RWMutex
	config ReorgConfig

	// Canonical chain: block number -> hash.
	canonical map[uint64]types.Hash

	// Current chain head.
	headNumber uint64
	headHash   types.Hash

	// Reorg history.
	reorgHistory []ReorgEvent
	reorgCount   int
	maxDepthSeen uint64
}

// NewChainReorgHandler creates a new chain reorg handler with the given config.
func NewChainReorgHandler(config ReorgConfig) *ChainReorgHandler {
	return &ChainReorgHandler{
		config:       config,
		canonical:    make(map[uint64]types.Hash),
		reorgHistory: make([]ReorgEvent, 0),
	}
}

// ProcessNewHead processes a new block head and detects reorganizations.
// If a reorg is detected, a ReorgEvent is returned describing the change.
// Returns nil event for normal chain extensions (no reorg).
func (h *ChainReorgHandler) ProcessNewHead(number uint64, hash types.Hash, parentHash types.Hash) (*ReorgEvent, error) {
	if hash.IsZero() {
		return nil, ErrReorgZeroHash
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// First block ever: just set it as canonical.
	if h.headNumber == 0 && h.headHash.IsZero() {
		h.headNumber = number
		h.headHash = hash
		h.canonical[number] = hash
		return nil, nil
	}

	// Normal chain extension: new block's parent is current head.
	if parentHash == h.headHash && number == h.headNumber+1 {
		h.headNumber = number
		h.headHash = hash
		h.canonical[number] = hash
		return nil, nil
	}

	// Reorg detected: new block doesn't extend current head.
	return h.handleReorg(number, hash, parentHash)
}

// handleReorg processes a chain reorganization. Caller must hold h.mu.
func (h *ChainReorgHandler) handleReorg(newNumber uint64, newHash types.Hash, newParentHash types.Hash) (*ReorgEvent, error) {
	oldHead := h.headHash
	oldNumber := h.headNumber

	// Find the fork point by walking back from both chains.
	// The fork point is where the new chain diverges from the canonical chain.
	var forkPoint uint64
	found := false

	// Walk back on the canonical chain to find a common ancestor.
	// The new block's parent should be at newNumber-1.
	if newNumber > 0 {
		// Check if parentHash matches canonical at newNumber-1.
		if canonHash, ok := h.canonical[newNumber-1]; ok && canonHash == newParentHash {
			forkPoint = newNumber - 1
			found = true
		}
	}

	// If we could not find a direct match, search backwards.
	if !found {
		// Walk back to find where canonical and new chain diverge.
		// Start from the minimum of old and new head.
		searchFrom := oldNumber
		if newNumber < searchFrom {
			searchFrom = newNumber
		}
		for n := searchFrom; n > 0; n-- {
			if _, ok := h.canonical[n]; ok {
				forkPoint = n - 1
				found = true
				break
			}
		}
		if !found {
			forkPoint = 0
		}
	}

	// Compute reorg depth.
	depth := oldNumber - forkPoint
	if newNumber > oldNumber && newNumber-forkPoint > depth {
		depth = newNumber - forkPoint
	}

	// Check max reorg depth.
	if h.config.MaxReorgDepth > 0 && depth > h.config.MaxReorgDepth {
		return nil, ErrReorgTooDeep
	}

	// Collect old blocks being removed (from forkPoint+1 to oldNumber).
	var oldBlocks []uint64
	for n := forkPoint + 1; n <= oldNumber; n++ {
		oldBlocks = append(oldBlocks, n)
	}

	// Collect new blocks being added (from forkPoint+1 to newNumber).
	var newBlocks []uint64
	for n := forkPoint + 1; n <= newNumber; n++ {
		newBlocks = append(newBlocks, n)
	}

	// Remove old canonical entries above fork point.
	for n := forkPoint + 1; n <= oldNumber; n++ {
		delete(h.canonical, n)
	}

	// Set new canonical head.
	h.canonical[newNumber] = newHash
	h.headNumber = newNumber
	h.headHash = newHash

	event := &ReorgEvent{
		OldHead:   oldHead,
		NewHead:   newHash,
		OldBlocks: oldBlocks,
		NewBlocks: newBlocks,
		Depth:     depth,
		Timestamp: time.Now().Unix(),
	}

	// Track metrics.
	if h.config.TrackMetrics {
		h.reorgCount++
		if depth > h.maxDepthSeen {
			h.maxDepthSeen = depth
		}
	}

	// Record event if configured.
	if h.config.NotifyOnReorg {
		h.reorgHistory = append(h.reorgHistory, *event)
	}

	return event, nil
}

// GetCanonicalHash returns the canonical block hash at the given height.
// Returns a zero hash if the height is not in the canonical chain.
func (h *ChainReorgHandler) GetCanonicalHash(number uint64) types.Hash {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.canonical[number]
}

// SetCanonicalHash sets the canonical hash for the given block number.
func (h *ChainReorgHandler) SetCanonicalHash(number uint64, hash types.Hash) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.canonical[number] = hash
}

// ChainHead returns the current head block number and hash.
func (h *ChainReorgHandler) ChainHead() (uint64, types.Hash) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.headNumber, h.headHash
}

// ReorgHistory returns the most recent reorg events, up to the given limit.
// Returns events in chronological order (oldest first).
func (h *ChainReorgHandler) ReorgHistory(limit int) []ReorgEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if limit <= 0 {
		return nil
	}

	total := len(h.reorgHistory)
	if total == 0 {
		return nil
	}

	start := 0
	if total > limit {
		start = total - limit
	}

	result := make([]ReorgEvent, total-start)
	copy(result, h.reorgHistory[start:])
	return result
}

// ReorgCount returns the total number of reorgs detected.
func (h *ChainReorgHandler) ReorgCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.reorgCount
}

// MaxReorgDepthSeen returns the depth of the deepest reorg ever observed.
func (h *ChainReorgHandler) MaxReorgDepthSeen() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.maxDepthSeen
}

// IsCanonical returns true if the given block number has the given hash
// in the canonical chain.
func (h *ChainReorgHandler) IsCanonical(number uint64, hash types.Hash) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	canonHash, ok := h.canonical[number]
	return ok && canonHash == hash
}
