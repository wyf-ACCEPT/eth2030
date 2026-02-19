package rpc

import (
	"errors"
	"fmt"
)

// AdminBackend provides access to node administration data.
type AdminBackend interface {
	NodeInfo() NodeInfoData
	Peers() []PeerInfoData
	AddPeer(url string) error
	RemovePeer(url string) error
	ChainID() uint64
	DataDir() string
}

// NodeInfoData contains information about the running node.
type NodeInfoData struct {
	Name       string                 `json:"name"`
	ID         string                 `json:"id"`
	Enode      string                 `json:"enode"`
	ListenAddr string                 `json:"listenAddr"`
	Protocols  map[string]interface{} `json:"protocols"`
}

// PeerInfoData contains information about a connected peer.
type PeerInfoData struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	RemoteAddr string   `json:"remoteAddr"`
	Caps       []string `json:"caps"`
	Static     bool     `json:"static"`
	Trusted    bool     `json:"trusted"`
}

// AdminAPI implements the admin namespace JSON-RPC methods.
type AdminAPI struct {
	backend AdminBackend
}

// NewAdminAPI creates a new admin API service.
func NewAdminAPI(backend AdminBackend) *AdminAPI {
	return &AdminAPI{backend: backend}
}

// AdminNodeInfo returns information about the running node.
func (api *AdminAPI) AdminNodeInfo() (*NodeInfoData, error) {
	if api.backend == nil {
		return nil, errors.New("admin backend not available")
	}
	info := api.backend.NodeInfo()
	return &info, nil
}

// AdminPeers returns information about connected peers.
func (api *AdminAPI) AdminPeers() ([]PeerInfoData, error) {
	if api.backend == nil {
		return nil, errors.New("admin backend not available")
	}
	peers := api.backend.Peers()
	if peers == nil {
		return []PeerInfoData{}, nil
	}
	return peers, nil
}

// AdminAddPeer requests adding a new remote peer.
func (api *AdminAPI) AdminAddPeer(url string) (bool, error) {
	if api.backend == nil {
		return false, errors.New("admin backend not available")
	}
	if url == "" {
		return false, errors.New("empty peer URL")
	}
	if err := api.backend.AddPeer(url); err != nil {
		return false, err
	}
	return true, nil
}

// AdminRemovePeer requests disconnection from a remote peer.
func (api *AdminAPI) AdminRemovePeer(url string) (bool, error) {
	if api.backend == nil {
		return false, errors.New("admin backend not available")
	}
	if url == "" {
		return false, errors.New("empty peer URL")
	}
	if err := api.backend.RemovePeer(url); err != nil {
		return false, err
	}
	return true, nil
}

// AdminDataDir returns the data directory of the node.
func (api *AdminAPI) AdminDataDir() (string, error) {
	if api.backend == nil {
		return "", errors.New("admin backend not available")
	}
	return api.backend.DataDir(), nil
}

// AdminStartRPC starts the HTTP RPC listener (stub).
func (api *AdminAPI) AdminStartRPC(host string, port int) (bool, error) {
	if api.backend == nil {
		return false, errors.New("admin backend not available")
	}
	if host == "" {
		return false, errors.New("empty host")
	}
	if port <= 0 || port > 65535 {
		return false, errors.New("invalid port number")
	}
	// Stub: in a full implementation this would start the HTTP listener.
	return true, nil
}

// AdminStopRPC stops the HTTP RPC listener (stub).
func (api *AdminAPI) AdminStopRPC() (bool, error) {
	if api.backend == nil {
		return false, errors.New("admin backend not available")
	}
	// Stub: in a full implementation this would stop the HTTP listener.
	return true, nil
}

// AdminChainID returns the chain ID as a hex string.
func (api *AdminAPI) AdminChainID() (string, error) {
	if api.backend == nil {
		return "", errors.New("admin backend not available")
	}
	id := api.backend.ChainID()
	return fmt.Sprintf("0x%x", id), nil
}
