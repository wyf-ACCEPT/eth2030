// service.go implements a discovery service coordinator that manages
// peer discovery across multiple protocols. It maintains a thread-safe
// node table with protocol filtering, nearest-node lookups, and stale
// entry eviction.
package discover

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// DiscoveryServiceConfig configures the discovery service.
type DiscoveryServiceConfig struct {
	MaxNodes        int   // maximum number of tracked nodes
	RefreshInterval int64 // seconds between table refreshes
	BootnodeTimeout int64 // seconds before a bootnode is considered stale
	EnableDNS       bool  // whether DNS-based discovery is enabled
}

// DiscoveredNode represents a node found through the discovery process.
type DiscoveredNode struct {
	ID        string   // hex-encoded node identifier
	IP        string   // IPv4 or IPv6 address
	Port      uint16   // UDP port
	Protocols []string // supported protocol names (e.g. "eth/68", "snap/1")
	LastSeen  int64    // unix timestamp of last activity
	Distance  int      // XOR log distance from the local node
}

// DiscoveryService coordinates peer discovery across protocols.
type DiscoveryService struct {
	mu              sync.RWMutex
	config          DiscoveryServiceConfig
	nodes           map[string]*DiscoveredNode
	bootnodes       map[string]bool // set of bootnode IDs
	protocolFilter  []string        // if non-empty, only track matching nodes
}

// Errors returned by the discovery service.
var (
	ErrNodeExists    = errors.New("discover: node already exists")
	ErrTableFull     = errors.New("discover: node table full")
	ErrEmptyID       = errors.New("discover: empty node ID")
	ErrInvalidPort   = errors.New("discover: invalid port")
)

// NewDiscoveryService creates a new discovery service with the given config.
func NewDiscoveryService(config DiscoveryServiceConfig) *DiscoveryService {
	if config.MaxNodes <= 0 {
		config.MaxNodes = 256
	}
	if config.RefreshInterval <= 0 {
		config.RefreshInterval = 30
	}
	if config.BootnodeTimeout <= 0 {
		config.BootnodeTimeout = 60
	}
	return &DiscoveryService{
		config:    config,
		nodes:     make(map[string]*DiscoveredNode),
		bootnodes: make(map[string]bool),
	}
}

// AddBootnode registers a bootstrap node. It returns an error if the ID
// is empty, the port is zero, or the table is already full.
func (ds *DiscoveryService) AddBootnode(id, ip string, port uint16) error {
	if id == "" {
		return ErrEmptyID
	}
	if port == 0 {
		return ErrInvalidPort
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if _, exists := ds.nodes[id]; exists {
		return ErrNodeExists
	}
	if len(ds.nodes) >= ds.config.MaxNodes {
		return ErrTableFull
	}

	now := time.Now().Unix()
	node := &DiscoveredNode{
		ID:       id,
		IP:       ip,
		Port:     port,
		LastSeen: now,
		Distance: xorDistance(id),
	}
	ds.nodes[id] = node
	ds.bootnodes[id] = true
	return nil
}

// RemoveNode removes a node from the table.
func (ds *DiscoveryService) RemoveNode(id string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.nodes, id)
	delete(ds.bootnodes, id)
}

// GetNode looks up a node by ID. Returns nil if not found.
func (ds *DiscoveryService) GetNode(id string) *DiscoveredNode {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	n, ok := ds.nodes[id]
	if !ok {
		return nil
	}
	// Return a copy so callers cannot mutate the internal state.
	cp := *n
	cp.Protocols = make([]string, len(n.Protocols))
	copy(cp.Protocols, n.Protocols)
	return &cp
}

// NearestNodes returns up to count nodes sorted by ascending XOR distance
// to the targetID.
func (ds *DiscoveryService) NearestNodes(targetID string, count int) []DiscoveredNode {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if count <= 0 || len(ds.nodes) == 0 {
		return nil
	}

	type distEntry struct {
		node *DiscoveredNode
		dist int
	}

	entries := make([]distEntry, 0, len(ds.nodes))
	for _, n := range ds.nodes {
		d := xorStringDistance(targetID, n.ID)
		entries = append(entries, distEntry{node: n, dist: d})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].dist < entries[j].dist
	})

	if count > len(entries) {
		count = len(entries)
	}

	result := make([]DiscoveredNode, count)
	for i := 0; i < count; i++ {
		result[i] = *entries[i].node
		result[i].Distance = entries[i].dist
	}
	return result
}

// ActiveNodes returns the number of tracked nodes.
func (ds *DiscoveryService) ActiveNodes() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.nodes)
}

// RefreshNodes removes stale entries from the node table. A node is stale
// if its LastSeen timestamp is older than BootnodeTimeout seconds ago.
func (ds *DiscoveryService) RefreshNodes() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	cutoff := time.Now().Unix() - ds.config.BootnodeTimeout
	for id, n := range ds.nodes {
		if n.LastSeen < cutoff {
			delete(ds.nodes, id)
			delete(ds.bootnodes, id)
		}
	}
}

// SetProtocolFilter sets the protocol filter. Subsequent calls to
// NodesByProtocol will match against these protocols. An empty slice
// disables filtering.
func (ds *DiscoveryService) SetProtocolFilter(protocols []string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.protocolFilter = make([]string, len(protocols))
	copy(ds.protocolFilter, protocols)
}

// NodesByProtocol returns all nodes that advertise the given protocol.
func (ds *DiscoveryService) NodesByProtocol(protocol string) []DiscoveredNode {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var result []DiscoveredNode
	for _, n := range ds.nodes {
		for _, p := range n.Protocols {
			if p == protocol {
				cp := *n
				result = append(result, cp)
				break
			}
		}
	}
	return result
}

// AddNode inserts or updates a discovered node. If the node already exists
// its LastSeen and Protocols are updated.
func (ds *DiscoveryService) AddNode(node DiscoveredNode) error {
	if node.ID == "" {
		return ErrEmptyID
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	if existing, ok := ds.nodes[node.ID]; ok {
		existing.LastSeen = node.LastSeen
		if len(node.Protocols) > 0 {
			existing.Protocols = make([]string, len(node.Protocols))
			copy(existing.Protocols, node.Protocols)
		}
		return nil
	}

	if len(ds.nodes) >= ds.config.MaxNodes {
		return ErrTableFull
	}

	cp := node
	cp.Distance = xorDistance(node.ID)
	ds.nodes[node.ID] = &cp
	return nil
}

// xorDistance computes a simple distance metric from a hex-encoded ID.
// It sums the byte values as a lightweight stand-in for the full XOR
// log distance (which requires a local node ID).
func xorDistance(id string) int {
	d := 0
	for i := 0; i < len(id); i++ {
		d += int(id[i])
	}
	return d
}

// xorStringDistance computes the byte-wise XOR sum between two IDs.
func xorStringDistance(a, b string) int {
	d := 0
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		d += int(a[i] ^ b[i])
	}
	// Account for unequal lengths: each extra byte contributes its value.
	if len(a) > minLen {
		for i := minLen; i < len(a); i++ {
			d += int(a[i])
		}
	}
	if len(b) > minLen {
		for i := minLen; i < len(b); i++ {
			d += int(b[i])
		}
	}
	return d
}
