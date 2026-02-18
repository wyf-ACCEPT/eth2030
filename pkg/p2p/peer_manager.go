package p2p

import (
	"errors"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

var (
	// ErrPeerManagerClosed is returned when operating on a closed PeerManager.
	ErrPeerManagerClosed = errors.New("p2p: peer manager closed")
)

// PeerManager tracks connected peers and their status, providing methods
// for peer lifecycle management and message broadcasting.
type PeerManager struct {
	mu     sync.RWMutex
	peers  map[string]*managedPeer
	closed bool
}

// managedPeer wraps a Peer with its associated transport for sending messages.
type managedPeer struct {
	Peer      *Peer
	Transport Transport
}

// NewPeerManager creates a new PeerManager.
func NewPeerManager() *PeerManager {
	return &PeerManager{
		peers: make(map[string]*managedPeer),
	}
}

// AddPeer registers a peer and its transport with the manager.
// Returns ErrPeerAlreadyRegistered if the peer is already tracked.
func (pm *PeerManager) AddPeer(p *Peer, tr Transport) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.closed {
		return ErrPeerManagerClosed
	}
	if _, exists := pm.peers[p.ID()]; exists {
		return ErrPeerAlreadyRegistered
	}
	pm.peers[p.ID()] = &managedPeer{Peer: p, Transport: tr}
	return nil
}

// RemovePeer unregisters a peer from the manager.
// Returns ErrPeerNotRegistered if the peer is not tracked.
func (pm *PeerManager) RemovePeer(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.closed {
		return ErrPeerManagerClosed
	}
	if _, exists := pm.peers[id]; !exists {
		return ErrPeerNotRegistered
	}
	delete(pm.peers, id)
	return nil
}

// Peer returns the peer with the given ID, or nil if not found.
func (pm *PeerManager) Peer(id string) *Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if mp, ok := pm.peers[id]; ok {
		return mp.Peer
	}
	return nil
}

// Peers returns a snapshot of all managed peers.
func (pm *PeerManager) Peers() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	list := make([]*Peer, 0, len(pm.peers))
	for _, mp := range pm.peers {
		list = append(list, mp.Peer)
	}
	return list
}

// Len returns the number of managed peers.
func (pm *PeerManager) Len() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.peers)
}

// BestPeer returns the peer with the highest total difficulty, or nil if empty.
func (pm *PeerManager) BestPeer() *Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var best *Peer
	var bestTD *big.Int

	for _, mp := range pm.peers {
		td := mp.Peer.TD()
		if bestTD == nil || td.Cmp(bestTD) > 0 {
			best = mp.Peer
			bestTD = td
		}
	}
	return best
}

// BroadcastBlock sends a new block announcement to all peers except those
// listed in the exclude set. Each peer's head is updated with the new block.
func (pm *PeerManager) BroadcastBlock(block *types.Block, td *big.Int, exclude map[string]bool) []error {
	msg, err := EncodeMessage(NewBlockMsg, NewBlockData{
		Block: block,
		TD:    td,
	})
	if err != nil {
		return []error{err}
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var errs []error
	for id, mp := range pm.peers {
		if exclude != nil && exclude[id] {
			continue
		}
		wireMsg := Msg{
			Code:    msg.Code,
			Size:    msg.Size,
			Payload: msg.Payload,
		}
		if err := mp.Transport.WriteMsg(wireMsg); err != nil {
			errs = append(errs, err)
			continue
		}
		// Update peer's known head.
		mp.Peer.SetHead(block.Hash(), td)
	}
	return errs
}

// BroadcastTransactions sends a batch of transaction hashes (ETH/68 style)
// to all peers except those in the exclude set.
func (pm *PeerManager) BroadcastTransactions(txTypes []byte, txSizes []uint32, txHashes []types.Hash, exclude map[string]bool) []error {
	msg, err := EncodeMessage(NewPooledTransactionHashesMsg, NewPooledTransactionHashesPacket68{
		Types:  txTypes,
		Sizes:  txSizes,
		Hashes: txHashes,
	})
	if err != nil {
		return []error{err}
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var errs []error
	for id, mp := range pm.peers {
		if exclude != nil && exclude[id] {
			continue
		}
		wireMsg := Msg{
			Code:    msg.Code,
			Size:    msg.Size,
			Payload: msg.Payload,
		}
		if err := mp.Transport.WriteMsg(wireMsg); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// BroadcastBlockHashes sends block hash announcements to all peers except
// those in the exclude set.
func (pm *PeerManager) BroadcastBlockHashes(hashes []NewBlockHashesEntry, exclude map[string]bool) []error {
	msg, err := EncodeMessage(NewBlockHashesMsg, hashes)
	if err != nil {
		return []error{err}
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var errs []error
	for id, mp := range pm.peers {
		if exclude != nil && exclude[id] {
			continue
		}
		wireMsg := Msg{
			Code:    msg.Code,
			Size:    msg.Size,
			Payload: msg.Payload,
		}
		if err := mp.Transport.WriteMsg(wireMsg); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Close marks the manager as closed and clears all peers.
func (pm *PeerManager) Close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.closed = true
	for k := range pm.peers {
		delete(pm.peers, k)
	}
}
