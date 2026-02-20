// net_api.go provides a standalone Net namespace API with its own backend
// interface for network information. This complements the net_ methods
// already dispatched through EthAPI (in api.go) by providing a separate,
// composable API struct with richer network info access.
package rpc

import (
	"errors"
	"fmt"
)

// NetBackend provides access to network status information.
type NetBackend interface {
	// NetworkID returns the network identifier (e.g. 1 for mainnet).
	NetworkID() uint64

	// IsListening returns whether the node is currently accepting
	// inbound connections.
	IsListening() bool

	// PeerCount returns the number of currently connected peers.
	PeerCount() int

	// MaxPeers returns the configured maximum number of peers.
	MaxPeers() int
}

// NetAPI implements the net_ namespace JSON-RPC methods.
type NetAPI struct {
	backend NetBackend
}

// NewNetAPI creates a new net API service.
func NewNetAPI(backend NetBackend) *NetAPI {
	return &NetAPI{backend: backend}
}

// HandleNetRequest dispatches a net_ namespace JSON-RPC request.
func (n *NetAPI) HandleNetRequest(req *Request) *Response {
	switch req.Method {
	case "net_version":
		return n.netVersionFull(req)
	case "net_listening":
		return n.netListeningFull(req)
	case "net_peerCount":
		return n.netPeerCountFull(req)
	case "net_maxPeers":
		return n.netMaxPeers(req)
	default:
		return errorResponse(req.ID, ErrCodeMethodNotFound,
			fmt.Sprintf("method %q not found in net namespace", req.Method))
	}
}

// netVersionFull returns the network ID as a decimal string.
func (n *NetAPI) netVersionFull(req *Request) *Response {
	if n.backend == nil {
		return errorResponse(req.ID, ErrCodeInternal, "net backend not available")
	}
	id := n.backend.NetworkID()
	return successResponse(req.ID, fmt.Sprintf("%d", id))
}

// netListeningFull returns whether the node is listening for connections.
func (n *NetAPI) netListeningFull(req *Request) *Response {
	if n.backend == nil {
		return errorResponse(req.ID, ErrCodeInternal, "net backend not available")
	}
	return successResponse(req.ID, n.backend.IsListening())
}

// netPeerCountFull returns the connected peer count as a hex string.
func (n *NetAPI) netPeerCountFull(req *Request) *Response {
	if n.backend == nil {
		return errorResponse(req.ID, ErrCodeInternal, "net backend not available")
	}
	count := n.backend.PeerCount()
	return successResponse(req.ID, encodeUint64(uint64(count)))
}

// netMaxPeers returns the max peer count as a hex string.
func (n *NetAPI) netMaxPeers(req *Request) *Response {
	if n.backend == nil {
		return errorResponse(req.ID, ErrCodeInternal, "net backend not available")
	}
	max := n.backend.MaxPeers()
	return successResponse(req.ID, encodeUint64(uint64(max)))
}

// --- Direct Go-typed API methods (for programmatic / internal use) ---

// ErrNetBackendNil is returned when the net backend is nil.
var ErrNetBackendNil = errors.New("net backend not available")

// Version returns the network ID as a decimal string.
func (n *NetAPI) Version() (string, error) {
	if n.backend == nil {
		return "", ErrNetBackendNil
	}
	return fmt.Sprintf("%d", n.backend.NetworkID()), nil
}

// Listening returns whether the node is accepting connections.
func (n *NetAPI) Listening() (bool, error) {
	if n.backend == nil {
		return false, ErrNetBackendNil
	}
	return n.backend.IsListening(), nil
}

// PeerCount returns the connected peer count.
func (n *NetAPI) PeerCount() (int, error) {
	if n.backend == nil {
		return 0, ErrNetBackendNil
	}
	return n.backend.PeerCount(), nil
}
