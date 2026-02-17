package p2p

import (
	"errors"
	"sync"
)

var (
	// ErrMaxPeers is returned when the peer set is full.
	ErrMaxPeers = errors.New("p2p: max peers reached")

	// ErrPeerSetClosed is returned when operating on a closed peer set.
	ErrPeerSetClosed = errors.New("p2p: peer set closed")
)

// ManagedPeerSet is a concurrent peer set with a configurable maximum capacity.
// It extends the basic PeerSet with max peers enforcement and close semantics.
type ManagedPeerSet struct {
	mu       sync.RWMutex
	peers    map[string]*Peer
	maxPeers int
	closed   bool
}

// NewManagedPeerSet creates a peer set with the given maximum capacity.
func NewManagedPeerSet(maxPeers int) *ManagedPeerSet {
	return &ManagedPeerSet{
		peers:    make(map[string]*Peer),
		maxPeers: maxPeers,
	}
}

// Add adds a peer to the set. Returns ErrMaxPeers if the set is full,
// ErrPeerAlreadyRegistered if the peer already exists.
func (ps *ManagedPeerSet) Add(p *Peer) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return ErrPeerSetClosed
	}
	if _, exists := ps.peers[p.id]; exists {
		return ErrPeerAlreadyRegistered
	}
	if len(ps.peers) >= ps.maxPeers {
		return ErrMaxPeers
	}
	ps.peers[p.id] = p
	return nil
}

// Remove removes a peer by ID.
func (ps *ManagedPeerSet) Remove(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.closed {
		return ErrPeerSetClosed
	}
	if _, exists := ps.peers[id]; !exists {
		return ErrPeerNotRegistered
	}
	delete(ps.peers, id)
	return nil
}

// Get returns the peer with the given ID, or nil.
func (ps *ManagedPeerSet) Get(id string) *Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.peers[id]
}

// Len returns the number of peers.
func (ps *ManagedPeerSet) Len() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}

// Peers returns a snapshot of all peers.
func (ps *ManagedPeerSet) Peers() []*Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	list := make([]*Peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		list = append(list, p)
	}
	return list
}

// Close marks the set as closed. Further Add calls will return ErrPeerSetClosed.
func (ps *ManagedPeerSet) Close() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.closed = true
	// Clear the map.
	for k := range ps.peers {
		delete(ps.peers, k)
	}
}
