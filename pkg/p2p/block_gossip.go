// Package p2p implements block gossip protocol for P2P block propagation.
//
// BlockGossipHandler manages block announcement receipt and propagation
// to connected peers using sqrt(n) fanout for efficient distribution.
package p2p

import (
	"errors"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// Block gossip errors.
var (
	ErrBlockGossipNilHash    = errors.New("block gossip: nil block hash")
	ErrBlockGossipNoPeers    = errors.New("block gossip: no peers connected")
	ErrBlockGossipDuplicate  = errors.New("block gossip: duplicate announcement")
	ErrBlockGossipEmptyPeer  = errors.New("block gossip: empty peer ID")
	ErrBlockGossipPeerExists = errors.New("block gossip: peer already registered")
)

// BlockGossipConfig configures the block gossip handler.
type BlockGossipConfig struct {
	MaxBlockSize     uint64        // maximum accepted block size in bytes
	PropagationDelay time.Duration // delay before propagating to peers
	MaxPeers         int           // maximum tracked peers
	FilterDuplicates bool          // whether to filter duplicate announcements
}

// DefaultBlockGossipConfig returns sensible defaults for block gossip.
func DefaultBlockGossipConfig() BlockGossipConfig {
	return BlockGossipConfig{
		MaxBlockSize:     10 * 1024 * 1024, // 10 MiB
		PropagationDelay: 100 * time.Millisecond,
		MaxPeers:         50,
		FilterDuplicates: true,
	}
}

// BlockAnnouncement represents a block announcement received from a peer.
type BlockAnnouncement struct {
	Hash   types.Hash // block hash
	Number uint64     // block number
	TD     *big.Int   // total difficulty at this block
	PeerID string     // peer that sent the announcement
}

// GossipStats tracks block gossip statistics.
type GossipStats struct {
	Announced  uint64 // total announcements received
	Propagated uint64 // total propagations sent
	Duplicates uint64 // duplicate announcements filtered
	Peers      int    // current peer count
}

// BlockGossipHandler manages block gossip for P2P block propagation.
// All methods are safe for concurrent use.
type BlockGossipHandler struct {
	mu     sync.RWMutex
	config BlockGossipConfig

	// Seen blocks cache for deduplication (hash -> true).
	seen map[types.Hash]bool

	// Connected peers (peerID -> true).
	peers map[string]bool

	// Recent announcements in chronological order.
	announcements []BlockAnnouncement

	// Statistics.
	stats GossipStats
}

// NewBlockGossipHandler creates a new block gossip handler with the given config.
func NewBlockGossipHandler(config BlockGossipConfig) *BlockGossipHandler {
	return &BlockGossipHandler{
		config:        config,
		seen:          make(map[types.Hash]bool),
		peers:         make(map[string]bool),
		announcements: make([]BlockAnnouncement, 0),
	}
}

// HandleAnnouncement processes a block announcement from a peer.
// Returns ErrBlockGossipDuplicate if the block was already seen and
// duplicate filtering is enabled.
func (h *BlockGossipHandler) HandleAnnouncement(ann BlockAnnouncement) error {
	if ann.Hash.IsZero() {
		return ErrBlockGossipNilHash
	}
	if ann.PeerID == "" {
		return ErrBlockGossipEmptyPeer
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check for duplicates.
	if h.config.FilterDuplicates && h.seen[ann.Hash] {
		h.stats.Duplicates++
		return ErrBlockGossipDuplicate
	}

	// Mark as seen and record.
	h.seen[ann.Hash] = true
	h.stats.Announced++
	h.announcements = append(h.announcements, ann)

	return nil
}

// PropagateBlock propagates a block hash to sqrt(n) peers for efficient
// distribution. Returns the list of peer IDs that were selected.
func (h *BlockGossipHandler) PropagateBlock(hash types.Hash, number uint64) []string {
	if hash.IsZero() {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	n := len(h.peers)
	if n == 0 {
		return nil
	}

	// Select sqrt(n) peers, minimum 1.
	fanout := int(math.Ceil(math.Sqrt(float64(n))))
	if fanout > n {
		fanout = n
	}

	// Collect all peer IDs.
	allPeers := make([]string, 0, n)
	for pid := range h.peers {
		allPeers = append(allPeers, pid)
	}

	// Select the first fanout peers. In production this would use
	// a random or scoring-based selection; here we use deterministic
	// ordering for testability.
	selected := allPeers[:fanout]

	h.stats.Propagated += uint64(len(selected))
	h.seen[hash] = true

	return selected
}

// AddPeer registers a new peer for block gossip.
func (h *BlockGossipHandler) AddPeer(peerID string) {
	if peerID == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.config.MaxPeers > 0 && len(h.peers) >= h.config.MaxPeers {
		return
	}

	h.peers[peerID] = true
	h.stats.Peers = len(h.peers)
}

// RemovePeer unregisters a peer from block gossip.
func (h *BlockGossipHandler) RemovePeer(peerID string) {
	if peerID == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.peers, peerID)
	h.stats.Peers = len(h.peers)
}

// SeenBlock returns true if the block hash has been seen before.
func (h *BlockGossipHandler) SeenBlock(hash types.Hash) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.seen[hash]
}

// PeerCount returns the number of connected peers.
func (h *BlockGossipHandler) PeerCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.peers)
}

// Stats returns a snapshot of the gossip statistics.
func (h *BlockGossipHandler) Stats() GossipStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.stats
}

// RecentAnnouncements returns the most recent block announcements,
// up to the specified limit. Returns announcements in chronological order
// (oldest first).
func (h *BlockGossipHandler) RecentAnnouncements(limit int) []BlockAnnouncement {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if limit <= 0 {
		return nil
	}

	total := len(h.announcements)
	if total == 0 {
		return nil
	}

	start := 0
	if total > limit {
		start = total - limit
	}

	// Return a copy to avoid data races.
	result := make([]BlockAnnouncement, total-start)
	copy(result, h.announcements[start:])
	return result
}
