// Package eth implements the eth wire protocol handler.
//
// peer_set.go provides ETH-protocol-level peer management with capability
// negotiation, peer scoring integration, best peer selection, and lifecycle
// event hooks.
package eth

import (
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/p2p"
)

// Peer set errors.
var (
	ErrPeerSetFull    = errors.New("eth: peer set at capacity")
	ErrPeerSetClosed  = errors.New("eth: peer set closed")
	ErrPeerExists     = errors.New("eth: peer already registered")
	ErrPeerMissing    = errors.New("eth: peer not registered")
	ErrMinVersion     = errors.New("eth: peer below minimum protocol version")
)

// PeerEvent describes a peer lifecycle event type.
type PeerEvent int

const (
	PeerEventRegistered   PeerEvent = iota // Peer successfully registered.
	PeerEventUnregistered                  // Peer removed from set.
	PeerEventScoreChanged                  // Peer's score was updated.
)

// PeerEventData carries information about a peer lifecycle event.
type PeerEventData struct {
	PeerID  string
	Event   PeerEvent
	Time    time.Time
	Version uint32  // Protocol version (for Registered events).
	Score   float64 // Current score (for ScoreChanged events).
}

// PeerEventFunc is a callback invoked on peer lifecycle events.
type PeerEventFunc func(PeerEventData)

// scoredPeer wraps an EthPeer with scoring and capability metadata.
type scoredPeer struct {
	ethPeer  *EthPeer
	p2pPeer  *p2p.Peer
	score    *p2p.PeerScore
	version  uint32
	head     types.Hash
	td       *big.Int
	joinedAt time.Time
}

// EthPeerSet manages the set of active ETH protocol peers with capacity
// limits, scoring, capability negotiation, and lifecycle hooks. Thread-safe.
type EthPeerSet struct {
	mu       sync.RWMutex
	peers    map[string]*scoredPeer
	maxPeers int
	minVer   uint32 // Minimum acceptable ETH protocol version.
	closed   bool

	// Optional event callback for peer lifecycle notifications.
	eventFn PeerEventFunc
}

// NewEthPeerSet creates a peer set with the given capacity and minimum
// protocol version. Peers below minVersion are rejected during registration.
func NewEthPeerSet(maxPeers int, minVersion uint32) *EthPeerSet {
	return &EthPeerSet{
		peers:    make(map[string]*scoredPeer),
		maxPeers: maxPeers,
		minVer:   minVersion,
	}
}

// SetEventHandler registers a callback for peer lifecycle events.
func (ps *EthPeerSet) SetEventHandler(fn PeerEventFunc) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.eventFn = fn
}

// Register adds a peer to the set after validating its capabilities.
// The peer must advertise a protocol version at or above the configured
// minimum. Returns an error if the set is full, closed, or the peer fails
// capability negotiation.
func (ps *EthPeerSet) Register(ep *EthPeer, version uint32) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return ErrPeerSetClosed
	}
	id := ep.ID()
	if _, ok := ps.peers[id]; ok {
		return ErrPeerExists
	}
	if len(ps.peers) >= ps.maxPeers {
		return ErrPeerSetFull
	}
	if version < ps.minVer {
		return ErrMinVersion
	}

	sp := &scoredPeer{
		ethPeer:  ep,
		p2pPeer:  ep.Peer(),
		score:    p2p.NewPeerScore(),
		version:  version,
		td:       new(big.Int),
		joinedAt: time.Now(),
	}
	ps.peers[id] = sp

	// Fire registration event.
	if ps.eventFn != nil {
		ps.eventFn(PeerEventData{
			PeerID:  id,
			Event:   PeerEventRegistered,
			Time:    sp.joinedAt,
			Version: version,
		})
	}
	return nil
}

// Unregister removes a peer from the set and fires the unregistered event.
func (ps *EthPeerSet) Unregister(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return ErrPeerSetClosed
	}
	if _, ok := ps.peers[id]; !ok {
		return ErrPeerMissing
	}
	delete(ps.peers, id)

	if ps.eventFn != nil {
		ps.eventFn(PeerEventData{
			PeerID: id,
			Event:  PeerEventUnregistered,
			Time:   time.Now(),
		})
	}
	return nil
}

// Get returns the EthPeer for the given ID, or nil if not found.
func (ps *EthPeerSet) Get(id string) *EthPeer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if sp, ok := ps.peers[id]; ok {
		return sp.ethPeer
	}
	return nil
}

// Len returns the number of registered peers.
func (ps *EthPeerSet) Len() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}

// Capacity returns the maximum number of peers allowed.
func (ps *EthPeerSet) Capacity() int {
	return ps.maxPeers
}

// HasCapability returns true if the peer with the given ID supports at least
// the specified protocol version.
func (ps *EthPeerSet) HasCapability(id string, minVersion uint32) bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	sp, ok := ps.peers[id]
	if !ok {
		return false
	}
	return sp.version >= minVersion
}

// PeerVersion returns the negotiated protocol version for a peer, or 0
// if the peer is not registered.
func (ps *EthPeerSet) PeerVersion(id string) uint32 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if sp, ok := ps.peers[id]; ok {
		return sp.version
	}
	return 0
}

// PeerScore returns the current score for a peer, or 0 if not found.
func (ps *EthPeerSet) PeerScore(id string) float64 {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if sp, ok := ps.peers[id]; ok {
		return sp.score.Value()
	}
	return 0
}

// RecordGoodResponse records a valid response from the peer.
func (ps *EthPeerSet) RecordGoodResponse(id string) {
	ps.mu.RLock()
	sp, ok := ps.peers[id]
	fn := ps.eventFn
	ps.mu.RUnlock()
	if !ok {
		return
	}
	sp.score.GoodResponse()
	if fn != nil {
		fn(PeerEventData{PeerID: id, Event: PeerEventScoreChanged, Time: time.Now(), Score: sp.score.Value()})
	}
}

// RecordBadResponse records an invalid or useless response from the peer.
func (ps *EthPeerSet) RecordBadResponse(id string) {
	ps.mu.RLock()
	sp, ok := ps.peers[id]
	fn := ps.eventFn
	ps.mu.RUnlock()
	if !ok {
		return
	}
	sp.score.BadResponse()
	if fn != nil {
		fn(PeerEventData{PeerID: id, Event: PeerEventScoreChanged, Time: time.Now(), Score: sp.score.Value()})
	}
}

// RecordTimeout records a request timeout for the peer.
func (ps *EthPeerSet) RecordTimeout(id string) {
	ps.mu.RLock()
	sp, ok := ps.peers[id]
	ps.mu.RUnlock()
	if !ok {
		return
	}
	sp.score.Timeout()
}

// BestPeer returns the peer with the highest total difficulty. If multiple
// peers share the same TD, the one with the higher score wins. Returns nil
// if the set is empty.
func (ps *EthPeerSet) BestPeer() *EthPeer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var best *scoredPeer
	for _, sp := range ps.peers {
		if best == nil {
			best = sp
			continue
		}
		td := sp.p2pPeer.TD()
		bestTD := best.p2pPeer.TD()
		cmp := td.Cmp(bestTD)
		if cmp > 0 || (cmp == 0 && sp.score.Value() > best.score.Value()) {
			best = sp
		}
	}
	if best == nil {
		return nil
	}
	return best.ethPeer
}

// PeersAboveScore returns all peers whose score is strictly above the
// threshold, sorted by score descending.
func (ps *EthPeerSet) PeersAboveScore(threshold float64) []*EthPeer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	type scored struct {
		ep    *EthPeer
		score float64
	}
	var candidates []scored
	for _, sp := range ps.peers {
		s := sp.score.Value()
		if s > threshold {
			candidates = append(candidates, scored{ep: sp.ethPeer, score: s})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	result := make([]*EthPeer, len(candidates))
	for i, c := range candidates {
		result[i] = c.ep
	}
	return result
}

// PeersWithVersion returns all peers supporting at least the given version.
func (ps *EthPeerSet) PeersWithVersion(minVersion uint32) []*EthPeer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var result []*EthPeer
	for _, sp := range ps.peers {
		if sp.version >= minVersion {
			result = append(result, sp.ethPeer)
		}
	}
	return result
}

// ShouldDisconnect returns the IDs of peers whose scores have fallen below
// the disconnect threshold.
func (ps *EthPeerSet) ShouldDisconnect() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var ids []string
	for id, sp := range ps.peers {
		if sp.score.ShouldDisconnect() {
			ids = append(ids, id)
		}
	}
	return ids
}

// Close marks the set as closed, preventing new registrations and clearing
// all peers.
func (ps *EthPeerSet) Close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.closed = true
	ps.peers = make(map[string]*scoredPeer)
}

// IsClosed returns true if the peer set has been shut down.
func (ps *EthPeerSet) IsClosed() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.closed
}
