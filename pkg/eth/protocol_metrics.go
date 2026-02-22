// protocol_metrics.go tracks ETH protocol message statistics and per-peer
// performance metrics. These metrics are useful for monitoring network
// health, identifying slow or misbehaving peers, and tuning protocol
// parameters.
package eth

import (
	"sort"
	"sync"
	"time"
)

// PeerProtocolMetrics holds aggregated message statistics for a single peer.
type PeerProtocolMetrics struct {
	PeerID         string
	TotalMessages  uint64
	TotalBytes     uint64
	AvgLatencyMs   float64
	MessagesByType map[uint64]uint64
	LastSeen       int64 // unix timestamp

	// Internal accumulators for computing averages.
	totalLatencyMs int64
}

// MessageTypeStats holds aggregated statistics for a single message type
// across all peers. Named to avoid collision with the MsgType constants.
type MessageTypeStats struct {
	MsgType      uint64
	Count        uint64
	TotalBytes   uint64
	AvgSize      float64
	AvgLatencyMs float64

	// Internal accumulators.
	totalLatencyMs int64
}

// GlobalProtocolMetrics holds aggregate protocol statistics across all peers.
type GlobalProtocolMetrics struct {
	TotalMessages uint64
	TotalBytes    uint64
	ActivePeers   uint64
	AvgLatencyMs  float64
	StartedAt     int64 // unix timestamp when metrics collection began
}

// peerState holds internal per-peer tracking state.
type peerState struct {
	totalMessages  uint64
	totalBytes     uint64
	totalLatencyMs int64
	messagesByType map[uint64]uint64
	lastSeen       int64
}

// msgTypeState holds internal per-message-type tracking state.
type msgTypeState struct {
	count          uint64
	totalBytes     uint64
	totalLatencyMs int64
}

// ProtocolMetrics tracks per-peer and per-message-type statistics for the
// ETH wire protocol. All methods are thread-safe.
type ProtocolMetrics struct {
	mu        sync.RWMutex
	peers     map[string]*peerState
	msgTypes  map[uint64]*msgTypeState
	startedAt int64

	// Global accumulators.
	totalMessages  uint64
	totalBytes     uint64
	totalLatencyMs int64
}

// NewProtocolMetrics creates a new metrics tracker.
func NewProtocolMetrics() *ProtocolMetrics {
	return &ProtocolMetrics{
		peers:     make(map[string]*peerState),
		msgTypes:  make(map[uint64]*msgTypeState),
		startedAt: time.Now().Unix(),
	}
}

// RecordMessage records a protocol message from a peer. Parameters:
//   - peerID: unique identifier of the sending/receiving peer
//   - msgType: ETH message code (e.g., MsgStatus, MsgBlockHeaders)
//   - size: message size in bytes
//   - latencyMs: processing or round-trip latency in milliseconds
func (pm *ProtocolMetrics) RecordMessage(peerID string, msgType uint64, size int, latencyMs int64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Update peer state.
	ps, ok := pm.peers[peerID]
	if !ok {
		ps = &peerState{
			messagesByType: make(map[uint64]uint64),
		}
		pm.peers[peerID] = ps
	}
	ps.totalMessages++
	ps.totalBytes += uint64(size)
	ps.totalLatencyMs += latencyMs
	ps.messagesByType[msgType]++
	ps.lastSeen = time.Now().Unix()

	// Update message type state.
	mts, ok := pm.msgTypes[msgType]
	if !ok {
		mts = &msgTypeState{}
		pm.msgTypes[msgType] = mts
	}
	mts.count++
	mts.totalBytes += uint64(size)
	mts.totalLatencyMs += latencyMs

	// Update globals.
	pm.totalMessages++
	pm.totalBytes += uint64(size)
	pm.totalLatencyMs += latencyMs
}

// PeerMetrics returns aggregated metrics for a specific peer. Returns nil
// if the peer has no recorded messages.
func (pm *ProtocolMetrics) PeerMetrics(peerID string) *PeerProtocolMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	ps, ok := pm.peers[peerID]
	if !ok {
		return nil
	}

	avgLatency := float64(0)
	if ps.totalMessages > 0 {
		avgLatency = float64(ps.totalLatencyMs) / float64(ps.totalMessages)
	}

	// Copy the message type map.
	byType := make(map[uint64]uint64, len(ps.messagesByType))
	for k, v := range ps.messagesByType {
		byType[k] = v
	}

	return &PeerProtocolMetrics{
		PeerID:         peerID,
		TotalMessages:  ps.totalMessages,
		TotalBytes:     ps.totalBytes,
		AvgLatencyMs:   avgLatency,
		MessagesByType: byType,
		LastSeen:       ps.lastSeen,
	}
}

// MessageTypeMetrics returns aggregated metrics for a specific message type.
// Returns nil if no messages of that type have been recorded.
func (pm *ProtocolMetrics) MessageTypeMetrics(msgType uint64) *MessageTypeStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	mts, ok := pm.msgTypes[msgType]
	if !ok {
		return nil
	}

	avgSize := float64(0)
	avgLatency := float64(0)
	if mts.count > 0 {
		avgSize = float64(mts.totalBytes) / float64(mts.count)
		avgLatency = float64(mts.totalLatencyMs) / float64(mts.count)
	}

	return &MessageTypeStats{
		MsgType:      msgType,
		Count:        mts.count,
		TotalBytes:   mts.totalBytes,
		AvgSize:      avgSize,
		AvgLatencyMs: avgLatency,
	}
}

// ActivePeers returns the IDs of all peers that have recorded messages.
// The order is not guaranteed.
func (pm *ProtocolMetrics) ActivePeers() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]string, 0, len(pm.peers))
	for id := range pm.peers {
		peers = append(peers, id)
	}
	// Sort for deterministic output.
	sort.Strings(peers)
	return peers
}

// TopPeersByVolume returns the top n peers ranked by total bytes transferred.
// If there are fewer than n peers, all peers are returned.
func (pm *ProtocolMetrics) TopPeersByVolume(n int) []*PeerProtocolMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	type peerEntry struct {
		id    string
		state *peerState
	}

	entries := make([]peerEntry, 0, len(pm.peers))
	for id, ps := range pm.peers {
		entries = append(entries, peerEntry{id: id, state: ps})
	}

	// Sort by total bytes descending.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].state.totalBytes > entries[j].state.totalBytes
	})

	if n > len(entries) {
		n = len(entries)
	}

	result := make([]*PeerProtocolMetrics, n)
	for i := 0; i < n; i++ {
		ps := entries[i].state
		avgLatency := float64(0)
		if ps.totalMessages > 0 {
			avgLatency = float64(ps.totalLatencyMs) / float64(ps.totalMessages)
		}
		byType := make(map[uint64]uint64, len(ps.messagesByType))
		for k, v := range ps.messagesByType {
			byType[k] = v
		}
		result[i] = &PeerProtocolMetrics{
			PeerID:         entries[i].id,
			TotalMessages:  ps.totalMessages,
			TotalBytes:     ps.totalBytes,
			AvgLatencyMs:   avgLatency,
			MessagesByType: byType,
			LastSeen:       ps.lastSeen,
		}
	}
	return result
}

// GlobalMetrics returns a snapshot of aggregate protocol statistics.
func (pm *ProtocolMetrics) GlobalMetrics() *GlobalProtocolMetrics {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	avgLatency := float64(0)
	if pm.totalMessages > 0 {
		avgLatency = float64(pm.totalLatencyMs) / float64(pm.totalMessages)
	}

	return &GlobalProtocolMetrics{
		TotalMessages: pm.totalMessages,
		TotalBytes:    pm.totalBytes,
		ActivePeers:   uint64(len(pm.peers)),
		AvgLatencyMs:  avgLatency,
		StartedAt:     pm.startedAt,
	}
}

// PrunePeer removes all recorded data for a specific peer. This is
// useful when a peer disconnects and its metrics are no longer relevant.
func (pm *ProtocolMetrics) PrunePeer(peerID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.peers, peerID)
}

// Reset clears all recorded metrics and resets the start time.
func (pm *ProtocolMetrics) Reset() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.peers = make(map[string]*peerState)
	pm.msgTypes = make(map[uint64]*msgTypeState)
	pm.totalMessages = 0
	pm.totalBytes = 0
	pm.totalLatencyMs = 0
	pm.startedAt = time.Now().Unix()
}
