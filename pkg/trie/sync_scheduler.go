// sync_scheduler.go implements a trie sync scheduling engine for snap sync
// and state healing.
//
// During snap sync, the client downloads state trie nodes from peers. The
// scheduler tracks which nodes are missing, deduplicates requests, orders
// them by priority (shallower nodes first to enable early verification), and
// records arrivals so the healing phase can fill remaining gaps.
//
// The scheduler is concurrent-safe and designed to be driven by the sync
// pipeline: the downloader calls AddRoot/AddHash to seed work, the
// scheduler produces batched requests, and the downloader feeds back
// arrived nodes.
package trie

import (
	"errors"
	"sync"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// SyncPriority encodes the importance of a trie node request. Lower numeric
// values indicate higher priority.
type SyncPriority int

const (
	// PriorityRoot is the highest priority, for root nodes.
	PriorityRoot SyncPriority = 0
	// PriorityShallow is for nodes in the top 4 levels of the trie.
	PriorityShallow SyncPriority = 1
	// PriorityMedium is for nodes at depth 5-16.
	PriorityMedium SyncPriority = 2
	// PriorityDeep is for nodes deeper than 16.
	PriorityDeep SyncPriority = 3
	// PriorityHeal is for healing requests that fill gaps after snap sync.
	PriorityHeal SyncPriority = 4
)

// priorityForDepth returns the sync priority for a node at the given depth.
func priorityForDepth(depth int) SyncPriority {
	switch {
	case depth == 0:
		return PriorityRoot
	case depth <= 4:
		return PriorityShallow
	case depth <= 16:
		return PriorityMedium
	default:
		return PriorityDeep
	}
}

// SyncRequest represents a request for a single trie node.
type SyncRequest struct {
	// Hash is the expected keccak256 hash of the node's RLP encoding.
	Hash types.Hash
	// Path is the hex-nibble path from the root to this node, used for
	// request deduplication and ordering.
	Path []byte
	// Depth is the depth in the trie (0 = root).
	Depth int
	// Priority determines the scheduling order.
	Priority SyncPriority
	// IsHeal marks this request as a healing request (post-snap-sync gap fill).
	IsHeal bool
	// Parent is the hash of the parent node, used for dependency tracking.
	Parent types.Hash
}

// SyncScheduler manages pending, in-flight, and completed trie node requests.
type SyncScheduler struct {
	mu sync.Mutex

	// pending holds requests that have not yet been dispatched, keyed by
	// node hash. Duplicate requests for the same hash are deduplicated.
	pending map[types.Hash]*SyncRequest

	// inflight tracks hashes that have been dispatched but not yet received.
	inflight map[types.Hash]struct{}

	// done tracks hashes that have been successfully received and verified.
	done map[types.Hash]struct{}

	// nodeDB is the target database where arrived nodes are stored.
	nodeDB *NodeDatabase

	// priorityQueues holds pending requests bucketed by priority level.
	// Index corresponds to SyncPriority values.
	priorityQueues [5][]*SyncRequest

	// Stats.
	totalRequested uint64
	totalReceived  uint64
	totalDuplicate uint64
	healRequested  uint64
}

// NewSyncScheduler creates a new trie sync scheduler that stores received
// nodes in the given NodeDatabase.
func NewSyncScheduler(nodeDB *NodeDatabase) *SyncScheduler {
	return &SyncScheduler{
		pending:  make(map[types.Hash]*SyncRequest),
		inflight: make(map[types.Hash]struct{}),
		done:     make(map[types.Hash]struct{}),
		nodeDB:   nodeDB,
	}
}

// AddRoot seeds the scheduler with a root hash to sync. This is typically
// the state root of the target block.
func (s *SyncScheduler) AddRoot(root types.Hash) {
	s.AddHash(root, nil, 0, false)
}

// AddHash schedules a trie node for retrieval. If the hash is already
// pending, in-flight, or done, the request is deduplicated.
func (s *SyncScheduler) AddHash(hash types.Hash, path []byte, depth int, isHeal bool) {
	if hash == (types.Hash{}) {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already known.
	if _, ok := s.done[hash]; ok {
		s.totalDuplicate++
		return
	}
	if _, ok := s.inflight[hash]; ok {
		s.totalDuplicate++
		return
	}
	if _, ok := s.pending[hash]; ok {
		s.totalDuplicate++
		return
	}

	// Check if the node is already in the database.
	if _, err := s.nodeDB.Node(hash); err == nil {
		s.done[hash] = struct{}{}
		s.totalDuplicate++
		return
	}

	priority := priorityForDepth(depth)
	if isHeal {
		priority = PriorityHeal
	}

	req := &SyncRequest{
		Hash:     hash,
		Path:     copyBytes(path),
		Depth:    depth,
		Priority: priority,
		IsHeal:   isHeal,
	}

	s.pending[hash] = req
	s.priorityQueues[priority] = append(s.priorityQueues[priority], req)
	s.totalRequested++
	if isHeal {
		s.healRequested++
	}
}

// AddHealHash schedules a healing request for a missing node discovered
// during post-snap-sync trie verification.
func (s *SyncScheduler) AddHealHash(hash types.Hash, path []byte, depth int) {
	s.AddHash(hash, path, depth, true)
}

// Pending returns the total number of pending (not yet dispatched) requests.
func (s *SyncScheduler) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending)
}

// SyncSchedulerStats holds scheduler statistics.
type SyncSchedulerStats struct {
	Pending        int
	Inflight       int
	Done           int
	TotalRequested uint64
	TotalReceived  uint64
	TotalDuplicate uint64
	HealRequested  uint64
}

// Stats returns a snapshot of the scheduler's statistics.
func (s *SyncScheduler) Stats() SyncSchedulerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SyncSchedulerStats{
		Pending:        len(s.pending),
		Inflight:       len(s.inflight),
		Done:           len(s.done),
		TotalRequested: s.totalRequested,
		TotalReceived:  s.totalReceived,
		TotalDuplicate: s.totalDuplicate,
		HealRequested:  s.healRequested,
	}
}

// PopRequests returns up to maxCount pending requests in priority order
// (highest priority first) and marks them as in-flight.
func (s *SyncScheduler) PopRequests(maxCount int) []*SyncRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []*SyncRequest
	remaining := maxCount

	for pri := 0; pri < len(s.priorityQueues) && remaining > 0; pri++ {
		queue := s.priorityQueues[pri]
		take := remaining
		if take > len(queue) {
			take = len(queue)
		}

		// Filter out already-resolved requests (can happen if a node arrived
		// between scheduling and dispatching).
		filtered := queue[:0]
		for _, req := range queue {
			if _, ok := s.done[req.Hash]; ok {
				delete(s.pending, req.Hash)
				continue
			}
			if _, ok := s.inflight[req.Hash]; ok {
				delete(s.pending, req.Hash)
				continue
			}
			filtered = append(filtered, req)
		}
		s.priorityQueues[pri] = filtered

		take = remaining
		if take > len(filtered) {
			take = len(filtered)
		}

		for i := 0; i < take; i++ {
			req := filtered[i]
			s.inflight[req.Hash] = struct{}{}
			delete(s.pending, req.Hash)
			result = append(result, req)
		}

		// Remove dispatched entries from the queue.
		s.priorityQueues[pri] = filtered[take:]
		remaining -= take
	}

	return result
}

// NodeArrived is called when a trie node has been received from a peer.
// It verifies that the data matches the expected hash, stores it in the
// node database, and marks the request as done. Returns an error if the
// hash does not match.
func (s *SyncScheduler) NodeArrived(hash types.Hash, data []byte) error {
	// Verify the data hashes to the expected value.
	computed := types.BytesToHash(crypto.Keccak256(data))
	if computed != hash {
		return errors.New("sync: node hash mismatch")
	}

	// Store in the node database.
	s.nodeDB.InsertNode(hash, data)

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.inflight, hash)
	delete(s.pending, hash)
	s.done[hash] = struct{}{}
	s.totalReceived++
	return nil
}

// NodeFailed is called when a node request has failed (e.g., peer
// disconnected, timeout). The request is moved back to pending for retry.
func (s *SyncScheduler) NodeFailed(hash types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.inflight[hash]; !ok {
		return
	}
	delete(s.inflight, hash)

	// Re-enqueue if not already done or pending.
	if _, ok := s.done[hash]; ok {
		return
	}
	if _, ok := s.pending[hash]; ok {
		return
	}

	req := &SyncRequest{
		Hash:     hash,
		Priority: PriorityMedium, // default priority for retries
	}
	s.pending[hash] = req
	s.priorityQueues[PriorityMedium] = append(s.priorityQueues[PriorityMedium], req)
}

// IsDone returns true if all requested nodes have been received
// (pending + inflight == 0).
func (s *SyncScheduler) IsDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) == 0 && len(s.inflight) == 0
}

// Reset clears all scheduler state, discarding pending, in-flight, and
// completed requests. The underlying NodeDatabase is not affected.
func (s *SyncScheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending = make(map[types.Hash]*SyncRequest)
	s.inflight = make(map[types.Hash]struct{})
	s.done = make(map[types.Hash]struct{})
	for i := range s.priorityQueues {
		s.priorityQueues[i] = nil
	}
	s.totalRequested = 0
	s.totalReceived = 0
	s.totalDuplicate = 0
	s.healRequested = 0
}
