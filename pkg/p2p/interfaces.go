// Package p2p implements devp2p networking for the Ethereum execution layer.
//
// Key interfaces for protocol handler integration:
//   - PeerHandler: called when peers connect/disconnect
//   - MsgReadWriter: per-protocol message I/O
//   - PeerInfo: read-only peer metadata
//   - NodeDiscovery: node table access
package p2p

// MsgReadWriter combines message reading and writing for a single sub-protocol.
// Protocol handlers receive this interface to exchange messages with a peer.
type MsgReadWriter interface {
	// ReadMsg reads the next message for this protocol. The message code is
	// relative to the protocol (starts at 0). Blocks until a message arrives
	// or the connection is closed.
	ReadMsg() (Msg, error)

	// WriteMsg sends a message to the remote peer. The message code should be
	// relative to the protocol.
	WriteMsg(msg Msg) error
}

// PeerHandler is the callback interface for protocol-level peer lifecycle events.
// Implementations are registered via Server configuration and called when peers
// complete the handshake or disconnect.
type PeerHandler interface {
	// HandlePeer is called when a new peer connects with a compatible protocol.
	// The handler should exchange messages using rw and return when done.
	// Returning an error disconnects the peer.
	HandlePeer(peer *Peer, rw MsgReadWriter) error
}

// PeerHandlerFunc is an adapter to allow use of ordinary functions as PeerHandler.
type PeerHandlerFunc func(peer *Peer, rw MsgReadWriter) error

// HandlePeer calls f(peer, rw).
func (f PeerHandlerFunc) HandlePeer(peer *Peer, rw MsgReadWriter) error {
	return f(peer, rw)
}

// PeerInfo provides read-only information about a connected peer.
// Used by higher-level code (sync, tx pool) to query peer state.
type PeerInfo interface {
	// ID returns the peer's unique identifier.
	ID() string

	// RemoteAddr returns the peer's remote network address.
	RemoteAddr() string

	// Caps returns the peer's advertised capabilities.
	Caps() []Cap

	// Version returns the negotiated protocol version.
	Version() uint32
}

// PeerSet provides read-only access to the set of connected peers.
// This interface allows the eth protocol handler to iterate and query peers
// without managing the underlying connection lifecycle.
type PeerSetReader interface {
	// Peer returns the peer with the given ID, or nil.
	Peer(id string) *Peer

	// Len returns the number of connected peers.
	Len() int

	// Peers returns a snapshot of all connected peers.
	Peers() []*Peer

	// BestPeer returns the peer with the highest total difficulty.
	BestPeer() *Peer
}

// NodeDiscovery provides access to the node table for protocol-level
// peer management (e.g., requesting specific peers for sync).
type NodeDiscovery interface {
	// AllNodes returns all known nodes.
	AllNodes() []*Node

	// StaticNodes returns permanently configured nodes.
	StaticNodes() []*Node

	// AddNode adds a discovered node to the table.
	AddNode(n *Node) error

	// Remove removes a node from the table.
	Remove(id NodeID)
}

// Verify interface compliance at compile time.
var _ PeerHandler = PeerHandlerFunc(nil)
var _ PeerInfo = (*Peer)(nil)
var _ PeerSetReader = (*PeerSet)(nil)
var _ NodeDiscovery = (*NodeTable)(nil)
