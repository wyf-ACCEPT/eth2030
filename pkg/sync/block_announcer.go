// block_announcer.go implements BlkAnnounce, the block announcement handler.
// It processes NewBlockHashes messages from peers, deduplicates known blocks,
// tracks per-peer announcements, schedules fetches with latency-based priority,
// handles batch fetching, and provides metrics on the announcement pipeline.
package sync

import (
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// BlkAnnounce tuning constants.
const (
	// BlkAnnounceMaxPending is the max announced blocks to hold before dropping.
	BlkAnnounceMaxPending = 256

	// BlkAnnounceFetchBatch is the max blocks to fetch in a single request.
	BlkAnnounceFetchBatch = 16

	// BlkAnnouncePeerTimeout is the timeout for a single fetch request.
	BlkAnnouncePeerTimeout = 10 * time.Second

	// BlkAnnounceMaxPerPeer is the max tracked announcements per peer.
	BlkAnnounceMaxPerPeer = 128

	// BlkAnnounceMaxPeerAge is how long to keep peer announcement records.
	BlkAnnounceMaxPeerAge = 5 * time.Minute
)

// BlkAnnounce errors.
var (
	ErrBlkAnnounceKnown      = errors.New("block announcer: block already known")
	ErrBlkAnnounceFull       = errors.New("block announcer: pending queue full")
	ErrBlkAnnounceNoPeer     = errors.New("block announcer: no peers available")
	ErrBlkAnnouncePeerBusy   = errors.New("block announcer: peer has pending fetch")
	ErrBlkAnnounceTimeout    = errors.New("block announcer: fetch timed out")
	ErrBlkAnnounceNotPending = errors.New("block announcer: block not in pending set")
)

// BlkAnnouncePeer is the interface for a peer that can serve block fetches.
type BlkAnnouncePeer interface {
	ID() string
	FetchBlocks(hashes []types.Hash) ([]*types.Block, error)
	Latency() time.Duration
}

// BlkAnnounceMetrics holds announcement pipeline metrics.
type BlkAnnounceMetrics struct {
	AnnouncementsReceived uint64
	AnnouncementsDupes    uint64
	FetchesStarted        uint64
	FetchesCompleted      uint64
	FetchesFailed         uint64
	BlocksFetched         uint64
	TimeoutsTotal         uint64
}

// blkAnnouncement represents a single block announcement from a peer.
type blkAnnouncement struct {
	hash       types.Hash
	number     uint64
	peerID     string
	receivedAt time.Time
}

// blkPeerInfo tracks announcement state for a single peer.
type blkPeerInfo struct {
	id           string
	announced    map[types.Hash]uint64 // hash -> block number
	fetching     bool                   // currently fetching from this peer
	lastFetch    time.Time
	latency      time.Duration
	announcedAt  []time.Time // for age-based pruning
}

// BlkAnnounce processes NewBlockHashes messages, deduplicates known blocks,
// and schedules fetch requests prioritized by peer latency.
type BlkAnnounce struct {
	mu sync.Mutex

	// Known blocks: blocks we already have or have fetched.
	known map[types.Hash]struct{}

	// Pending blocks: announced but not yet fetched.
	pending map[types.Hash]*blkAnnouncement

	// Per-peer tracking.
	peers map[string]*blkPeerInfo

	// Fetch callback: checks whether we have a block locally.
	hasBlock func(hash types.Hash) bool

	// Metrics tracked with atomics for concurrent reads.
	metrics BlkAnnounceMetrics

	// Counters for atomic access.
	announcementsReceived atomic.Uint64
	announcementsDupes    atomic.Uint64
	fetchesStarted        atomic.Uint64
	fetchesCompleted      atomic.Uint64
	fetchesFailed         atomic.Uint64
	blocksFetched         atomic.Uint64
	timeoutsTotal         atomic.Uint64
}

// NewBlkAnnounce creates a new block announcement handler.
// The hasBlock callback should return true if the block is already in our chain.
func NewBlkAnnounce(hasBlock func(hash types.Hash) bool) *BlkAnnounce {
	return &BlkAnnounce{
		known:    make(map[types.Hash]struct{}),
		pending:  make(map[types.Hash]*blkAnnouncement),
		peers:    make(map[string]*blkPeerInfo),
		hasBlock: hasBlock,
	}
}

// HandleAnnouncement processes a NewBlockHashes announcement from a peer.
// It records the announcement, deduplicates against known blocks, and
// queues unknown blocks for fetching.
func (ba *BlkAnnounce) HandleAnnouncement(peerID string, hashes []types.Hash, numbers []uint64) int {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	added := 0
	now := time.Now()

	// Ensure peer tracking exists.
	peer := ba.getOrCreatePeer(peerID)

	for i, hash := range hashes {
		ba.announcementsReceived.Add(1)

		var number uint64
		if i < len(numbers) {
			number = numbers[i]
		}

		// Record that this peer has this block.
		peer.announced[hash] = number
		if len(peer.announcedAt) < BlkAnnounceMaxPerPeer {
			peer.announcedAt = append(peer.announcedAt, now)
		}

		// Skip if already known or in our local chain.
		if _, known := ba.known[hash]; known {
			ba.announcementsDupes.Add(1)
			continue
		}
		if ba.hasBlock != nil && ba.hasBlock(hash) {
			ba.known[hash] = struct{}{}
			ba.announcementsDupes.Add(1)
			continue
		}

		// Skip if already pending.
		if _, pending := ba.pending[hash]; pending {
			ba.announcementsDupes.Add(1)
			continue
		}

		// Drop if pending queue is full.
		if len(ba.pending) >= BlkAnnounceMaxPending {
			continue
		}

		ba.pending[hash] = &blkAnnouncement{
			hash:       hash,
			number:     number,
			peerID:     peerID,
			receivedAt: now,
		}
		added++
	}

	return added
}

// FetchPending selects the best peer and fetches a batch of pending blocks.
// It prefers the peer with the lowest latency that is not currently fetching.
// Returns the fetched blocks and any error.
func (ba *BlkAnnounce) FetchPending(peers []BlkAnnouncePeer) ([]*types.Block, error) {
	ba.mu.Lock()
	if len(ba.pending) == 0 {
		ba.mu.Unlock()
		return nil, nil
	}

	// Select blocks to fetch (oldest announcements first).
	toFetch := ba.selectFetchBatch()
	if len(toFetch) == 0 {
		ba.mu.Unlock()
		return nil, nil
	}

	// Select the best peer.
	peer := ba.selectBestPeer(peers)
	if peer == nil {
		ba.mu.Unlock()
		return nil, ErrBlkAnnounceNoPeer
	}

	peerInfo := ba.getOrCreatePeer(peer.ID())
	peerInfo.fetching = true
	peerInfo.lastFetch = time.Now()
	ba.fetchesStarted.Add(1)
	ba.mu.Unlock()

	// Execute the fetch with timeout.
	hashes := make([]types.Hash, len(toFetch))
	for i, ann := range toFetch {
		hashes[i] = ann.hash
	}

	type fetchResult struct {
		blocks []*types.Block
		err    error
	}
	resultCh := make(chan fetchResult, 1)
	go func() {
		blocks, err := peer.FetchBlocks(hashes)
		resultCh <- fetchResult{blocks, err}
	}()

	var result fetchResult
	select {
	case result = <-resultCh:
	case <-time.After(BlkAnnouncePeerTimeout):
		result = fetchResult{nil, ErrBlkAnnounceTimeout}
		ba.timeoutsTotal.Add(1)
	}

	// Process result.
	ba.mu.Lock()
	defer ba.mu.Unlock()

	peerInfo.fetching = false

	if result.err != nil {
		ba.fetchesFailed.Add(1)
		return nil, result.err
	}

	ba.fetchesCompleted.Add(1)
	ba.blocksFetched.Add(uint64(len(result.blocks)))

	// Mark fetched blocks as known and remove from pending.
	for _, block := range result.blocks {
		hash := block.Hash()
		ba.known[hash] = struct{}{}
		delete(ba.pending, hash)
	}

	// Also remove requested hashes from pending (even if not returned).
	for _, hash := range hashes {
		delete(ba.pending, hash)
	}

	return result.blocks, nil
}

// selectFetchBatch returns the oldest pending announcements up to the batch limit.
// Caller must hold ba.mu.
func (ba *BlkAnnounce) selectFetchBatch() []*blkAnnouncement {
	if len(ba.pending) == 0 {
		return nil
	}

	// Collect all pending announcements.
	anns := make([]*blkAnnouncement, 0, len(ba.pending))
	for _, ann := range ba.pending {
		anns = append(anns, ann)
	}

	// Sort by receive time (oldest first).
	sort.Slice(anns, func(i, j int) bool {
		return anns[i].receivedAt.Before(anns[j].receivedAt)
	})

	if len(anns) > BlkAnnounceFetchBatch {
		anns = anns[:BlkAnnounceFetchBatch]
	}
	return anns
}

// selectBestPeer selects the peer with the lowest latency that is not busy.
// Caller must hold ba.mu.
func (ba *BlkAnnounce) selectBestPeer(peers []BlkAnnouncePeer) BlkAnnouncePeer {
	// Sort peers by latency (lowest first).
	sorted := make([]BlkAnnouncePeer, len(peers))
	copy(sorted, peers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Latency() < sorted[j].Latency()
	})

	for _, peer := range sorted {
		info, exists := ba.peers[peer.ID()]
		if !exists || !info.fetching {
			return peer
		}
	}

	// All peers are busy: use the one with lowest latency anyway.
	if len(sorted) > 0 {
		return sorted[0]
	}
	return nil
}

// MarkKnown marks a block as known (already in our chain) to prevent re-fetch.
func (ba *BlkAnnounce) MarkKnown(hash types.Hash) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	ba.known[hash] = struct{}{}
	delete(ba.pending, hash)
}

// PendingCount returns the number of blocks waiting to be fetched.
func (ba *BlkAnnounce) PendingCount() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	return len(ba.pending)
}

// KnownCount returns the number of known block hashes.
func (ba *BlkAnnounce) KnownCount() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	return len(ba.known)
}

// PeerBlocks returns the block hashes announced by a specific peer.
func (ba *BlkAnnounce) PeerBlocks(peerID string) []types.Hash {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	info, exists := ba.peers[peerID]
	if !exists {
		return nil
	}

	hashes := make([]types.Hash, 0, len(info.announced))
	for hash := range info.announced {
		hashes = append(hashes, hash)
	}
	return hashes
}

// PeerCount returns the number of tracked peers.
func (ba *BlkAnnounce) PeerCount() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	return len(ba.peers)
}

// RemovePeer removes a peer's announcement tracking data.
func (ba *BlkAnnounce) RemovePeer(peerID string) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	delete(ba.peers, peerID)
}

// PrunePeers removes stale peer entries older than BlkAnnounceMaxPeerAge.
func (ba *BlkAnnounce) PrunePeers() int {
	ba.mu.Lock()
	defer ba.mu.Unlock()

	cutoff := time.Now().Add(-BlkAnnounceMaxPeerAge)
	pruned := 0

	for id, info := range ba.peers {
		if len(info.announcedAt) == 0 {
			continue
		}
		last := info.announcedAt[len(info.announcedAt)-1]
		if last.Before(cutoff) && !info.fetching {
			delete(ba.peers, id)
			pruned++
		}
	}
	return pruned
}

// GetMetrics returns the current announcement metrics.
func (ba *BlkAnnounce) GetMetrics() BlkAnnounceMetrics {
	return BlkAnnounceMetrics{
		AnnouncementsReceived: ba.announcementsReceived.Load(),
		AnnouncementsDupes:    ba.announcementsDupes.Load(),
		FetchesStarted:        ba.fetchesStarted.Load(),
		FetchesCompleted:      ba.fetchesCompleted.Load(),
		FetchesFailed:         ba.fetchesFailed.Load(),
		BlocksFetched:         ba.blocksFetched.Load(),
		TimeoutsTotal:         ba.timeoutsTotal.Load(),
	}
}

// getOrCreatePeer returns the peer info, creating it if needed.
// Caller must hold ba.mu.
func (ba *BlkAnnounce) getOrCreatePeer(peerID string) *blkPeerInfo {
	info, exists := ba.peers[peerID]
	if !exists {
		info = &blkPeerInfo{
			id:        peerID,
			announced: make(map[types.Hash]uint64),
		}
		ba.peers[peerID] = info
	}
	return info
}

// HasPending returns whether a specific block hash is in the pending set.
func (ba *BlkAnnounce) HasPending(hash types.Hash) bool {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	_, exists := ba.pending[hash]
	return exists
}

// IsKnown returns whether a specific block hash is in the known set.
func (ba *BlkAnnounce) IsKnown(hash types.Hash) bool {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	_, exists := ba.known[hash]
	return exists
}
