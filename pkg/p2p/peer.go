package p2p

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

var (
	// ErrPeerAlreadyRegistered is returned when attempting to register a peer
	// that already exists in the peer set.
	ErrPeerAlreadyRegistered = errors.New("p2p: peer already registered")

	// ErrPeerNotRegistered is returned when attempting to unregister a peer
	// that is not in the peer set.
	ErrPeerNotRegistered = errors.New("p2p: peer not registered")
)

// Cap represents a peer capability (protocol name and version).
type Cap struct {
	Name    string
	Version uint
}

// Peer represents a connected remote node in the eth protocol.
type Peer struct {
	id         string     // Unique peer identifier (e.g., enode ID).
	remoteAddr string     // Remote network address (ip:port).
	caps       []Cap      // Negotiated capabilities.
	head       types.Hash // Hash of the peer's best known block.
	td         *big.Int   // Total difficulty of the peer's best known block.
	version    uint32     // Negotiated eth protocol version.
	headNumber uint64     // Best known block number.

	// Handler state: last responses and delivered request-correlated responses.
	lastResponses      map[uint64]interface{}
	deliveredResponses map[uint64]interface{}

	mu sync.RWMutex
}

// NewPeer creates a new Peer with the given identity and address.
func NewPeer(id, remoteAddr string, caps []Cap) *Peer {
	capsCopy := make([]Cap, len(caps))
	copy(capsCopy, caps)
	return &Peer{
		id:         id,
		remoteAddr: remoteAddr,
		caps:       capsCopy,
		td:         new(big.Int),
	}
}

// ID returns the peer's unique identifier.
func (p *Peer) ID() string {
	return p.id
}

// RemoteAddr returns the peer's remote network address.
func (p *Peer) RemoteAddr() string {
	return p.remoteAddr
}

// Caps returns the peer's advertised capabilities.
func (p *Peer) Caps() []Cap {
	p.mu.RLock()
	defer p.mu.RUnlock()
	c := make([]Cap, len(p.caps))
	copy(c, p.caps)
	return c
}

// Head returns the hash of the peer's best known block.
func (p *Peer) Head() types.Hash {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.head
}

// TD returns the total difficulty of the peer's best known block.
func (p *Peer) TD() *big.Int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return new(big.Int).Set(p.td)
}

// Version returns the negotiated eth protocol version.
func (p *Peer) Version() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.version
}

// SetHead updates the peer's known head block hash and total difficulty.
func (p *Peer) SetHead(hash types.Hash, td *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.head = hash
	if td != nil {
		p.td = new(big.Int).Set(td)
	}
}

// SetVersion sets the negotiated protocol version for this peer.
func (p *Peer) SetVersion(v uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.version = v
}

// PeerSet is a thread-safe collection of peers.
type PeerSet struct {
	mu    sync.RWMutex
	peers map[string]*Peer
}

// NewPeerSet creates an empty PeerSet.
func NewPeerSet() *PeerSet {
	return &PeerSet{
		peers: make(map[string]*Peer),
	}
}

// Register adds a peer to the set. Returns ErrPeerAlreadyRegistered if
// a peer with the same ID already exists.
func (ps *PeerSet) Register(p *Peer) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, exists := ps.peers[p.id]; exists {
		return ErrPeerAlreadyRegistered
	}
	ps.peers[p.id] = p
	return nil
}

// Unregister removes a peer from the set. Returns ErrPeerNotRegistered if
// the peer is not found.
func (ps *PeerSet) Unregister(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, exists := ps.peers[id]; !exists {
		return ErrPeerNotRegistered
	}
	delete(ps.peers, id)
	return nil
}

// Peer returns the peer with the given ID, or nil if not found.
func (ps *PeerSet) Peer(id string) *Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.peers[id]
}

// Len returns the number of peers in the set.
func (ps *PeerSet) Len() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}

// BestPeer returns the peer with the highest total difficulty.
// Returns nil if the set is empty.
func (ps *PeerSet) BestPeer() *Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	var best *Peer
	var bestTD *big.Int

	for _, p := range ps.peers {
		td := p.TD()
		if bestTD == nil || td.Cmp(bestTD) > 0 {
			best = p
			bestTD = td
		}
	}
	return best
}

// Peers returns a snapshot of all peers in the set.
func (ps *PeerSet) Peers() []*Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	list := make([]*Peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		list = append(list, p)
	}
	return list
}
