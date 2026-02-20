package p2p

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// Errors returned by AdvPeerManager operations.
var (
	ErrTooManyInbound  = errors.New("p2p: max inbound peers reached")
	ErrTooManyOutbound = errors.New("p2p: max outbound peers reached")
	ErrPeerBanned      = errors.New("p2p: peer is banned")
	ErrPeerExists      = errors.New("p2p: peer already connected")
	ErrPeerUnknown     = errors.New("p2p: peer not found")
)

// PeerManagerConfig configures the AdvPeerManager.
type PeerManagerConfig struct {
	MaxInbound    int // Maximum inbound connections allowed.
	MaxOutbound   int // Maximum outbound connections allowed.
	MinPeers      int // Minimum number of peers to maintain.
	PruneInterval int // Interval in seconds between pruning low-reputation peers.
	BanDuration   int // Duration in seconds that a ban lasts.
}

// AdvPeerInfo holds metadata about a connected peer managed by AdvPeerManager.
type AdvPeerInfo struct {
	ID          string   // Unique peer identifier.
	RemoteAddr  string   // Remote network address (ip:port).
	Protocols   []string // Supported protocol names.
	Inbound     bool     // True if the peer initiated the connection.
	ConnectedAt int64    // Unix timestamp of connection establishment.
	BytesIn     uint64   // Total bytes received from this peer.
	BytesOut    uint64   // Total bytes sent to this peer.
	Latency     int64    // Latest measured latency in milliseconds.
	Reputation  int      // Reputation score (higher is better).
}

// banEntry records a ban with its reason and expiry.
type banEntry struct {
	reason  string
	expires time.Time
}

// AdvPeerManager tracks peers with connection limits, banning, reputation,
// protocol filtering, and bandwidth accounting. All methods are thread-safe.
type AdvPeerManager struct {
	mu     sync.RWMutex
	config PeerManagerConfig
	peers  map[string]*AdvPeerInfo
	bans   map[string]*banEntry
}

// NewAdvPeerManager creates a new AdvPeerManager with the given config.
func NewAdvPeerManager(config PeerManagerConfig) *AdvPeerManager {
	return &AdvPeerManager{
		config: config,
		peers:  make(map[string]*AdvPeerInfo),
		bans:   make(map[string]*banEntry),
	}
}

// AddPeer registers a new peer. It enforces inbound/outbound limits
// and rejects banned peers. Returns an error if the peer cannot be added.
func (m *AdvPeerManager) AddPeer(info AdvPeerInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check ban status (clean expired bans inline).
	if ban, ok := m.bans[info.ID]; ok {
		if time.Now().Before(ban.expires) {
			return ErrPeerBanned
		}
		// Ban expired, remove it.
		delete(m.bans, info.ID)
	}

	if _, exists := m.peers[info.ID]; exists {
		return ErrPeerExists
	}

	// Enforce connection limits.
	if info.Inbound && m.inboundCountLocked() >= m.config.MaxInbound {
		return ErrTooManyInbound
	}
	if !info.Inbound && m.outboundCountLocked() >= m.config.MaxOutbound {
		return ErrTooManyOutbound
	}

	// Copy protocols to prevent external mutation.
	protos := make([]string, len(info.Protocols))
	copy(protos, info.Protocols)
	info.Protocols = protos

	m.peers[info.ID] = &info
	return nil
}

// RemovePeer removes a peer by ID. No error is returned if the peer
// does not exist (idempotent).
func (m *AdvPeerManager) RemovePeer(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, id)
}

// GetPeer returns a copy of the AdvPeerInfo for the given ID, or nil.
func (m *AdvPeerManager) GetPeer(id string) *AdvPeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.peers[id]
	if !ok {
		return nil
	}
	// Return a defensive copy.
	cp := *p
	cp.Protocols = make([]string, len(p.Protocols))
	copy(cp.Protocols, p.Protocols)
	return &cp
}

// BanPeer bans a peer for the configured BanDuration and removes it
// from the active peer set.
func (m *AdvPeerManager) BanPeer(id string, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dur := time.Duration(m.config.BanDuration) * time.Second
	m.bans[id] = &banEntry{
		reason:  reason,
		expires: time.Now().Add(dur),
	}
	delete(m.peers, id)
}

// IsBanned returns true if the peer is currently banned (not expired).
func (m *AdvPeerManager) IsBanned(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ban, ok := m.bans[id]
	if !ok {
		return false
	}
	return time.Now().Before(ban.expires)
}

// UpdateReputation adjusts a peer's reputation score by delta.
// Positive delta improves reputation, negative worsens it.
// No-op if the peer is not tracked.
func (m *AdvPeerManager) UpdateReputation(id string, delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.peers[id]; ok {
		p.Reputation += delta
	}
}

// BestPeers returns up to count peers sorted by reputation (descending).
func (m *AdvPeerManager) BestPeers(count int) []AdvPeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]AdvPeerInfo, 0, len(m.peers))
	for _, p := range m.peers {
		list = append(list, *p)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Reputation > list[j].Reputation
	})

	if count > len(list) {
		count = len(list)
	}
	return list[:count]
}

// PeersByProtocol returns all peers that support the given protocol.
func (m *AdvPeerManager) PeersByProtocol(protocol string) []AdvPeerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []AdvPeerInfo
	for _, p := range m.peers {
		for _, proto := range p.Protocols {
			if proto == protocol {
				result = append(result, *p)
				break
			}
		}
	}
	return result
}

// InboundCount returns the number of inbound peers.
func (m *AdvPeerManager) InboundCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.inboundCountLocked()
}

// OutboundCount returns the number of outbound peers.
func (m *AdvPeerManager) OutboundCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.outboundCountLocked()
}

// RecordBytes records bandwidth usage for a peer. Both in and out are
// added atomically to the peer's running totals.
func (m *AdvPeerManager) RecordBytes(id string, in, out uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p, ok := m.peers[id]; ok {
		p.BytesIn += in
		p.BytesOut += out
	}
}

// PeerCount returns the total number of connected peers.
func (m *AdvPeerManager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// inboundCountLocked returns the number of inbound peers.
// Caller must hold at least a read lock on m.mu.
func (m *AdvPeerManager) inboundCountLocked() int {
	count := 0
	for _, p := range m.peers {
		if p.Inbound {
			count++
		}
	}
	return count
}

// outboundCountLocked returns the number of outbound peers.
// Caller must hold at least a read lock on m.mu.
func (m *AdvPeerManager) outboundCountLocked() int {
	count := 0
	for _, p := range m.peers {
		if !p.Inbound {
			count++
		}
	}
	return count
}
