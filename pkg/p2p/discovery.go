package p2p

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNodeAlreadyKnown is returned when adding a node that already exists.
	ErrNodeAlreadyKnown = errors.New("p2p: node already known")

	// ErrInvalidEnode is returned when an enode URL is malformed.
	ErrInvalidEnode = errors.New("p2p: invalid enode URL")
)

// NodeID is a 64-byte public key identifying a node on the network.
// For now we use a hex string representation; in a full implementation this
// would be a [64]byte secp256k1 public key.
type NodeID string

// Node represents a known peer on the network with its addressing information.
type Node struct {
	ID   NodeID // Public key identifier.
	IP   net.IP // IPv4 or IPv6 address.
	TCP  uint16 // TCP listening port.
	UDP  uint16 // UDP discovery port.
	Name string // Human-readable name (optional).
}

// Addr returns the TCP address string (ip:port).
func (n *Node) Addr() string {
	return net.JoinHostPort(n.IP.String(), strconv.Itoa(int(n.TCP)))
}

// String returns a human-readable representation of the node.
func (n *Node) String() string {
	if n.Name != "" {
		return fmt.Sprintf("%s@%s", n.Name, n.Addr())
	}
	short := string(n.ID)
	if len(short) > 16 {
		short = short[:16] + "..."
	}
	return fmt.Sprintf("%s@%s", short, n.Addr())
}

// ParseEnode parses an enode URL of the form:
//
//	enode://<hex-node-id>@<ip>:<tcp-port>?discport=<udp-port>
//
// The discport parameter is optional (defaults to tcp-port).
func ParseEnode(rawurl string) (*Node, error) {
	if !strings.HasPrefix(rawurl, "enode://") {
		return nil, fmt.Errorf("%w: missing enode:// prefix", ErrInvalidEnode)
	}

	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnode, err)
	}

	if u.User == nil && u.Host == "" {
		return nil, fmt.Errorf("%w: missing host", ErrInvalidEnode)
	}

	// The node ID is in the user-info part.
	id := u.User.Username()
	if len(id) == 0 {
		return nil, fmt.Errorf("%w: empty node ID", ErrInvalidEnode)
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: bad host:port: %v", ErrInvalidEnode, err)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// Try to resolve hostname.
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return nil, fmt.Errorf("%w: cannot resolve host %q", ErrInvalidEnode, host)
		}
		ip = net.ParseIP(addrs[0])
	}

	tcpPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("%w: bad TCP port: %v", ErrInvalidEnode, err)
	}

	udpPort := tcpPort
	if dp := u.Query().Get("discport"); dp != "" {
		udpPort, err = strconv.ParseUint(dp, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("%w: bad discport: %v", ErrInvalidEnode, err)
		}
	}

	return &Node{
		ID:  NodeID(id),
		IP:  ip,
		TCP: uint16(tcpPort),
		UDP: uint16(udpPort),
	}, nil
}

// NodeTable manages a set of known nodes for discovery purposes.
// It provides static peer list management (v4/v5-style bootnodes)
// and basic node lifecycle tracking.
type NodeTable struct {
	mu    sync.RWMutex
	nodes map[NodeID]*nodeEntry
}

// nodeEntry wraps a Node with discovery metadata.
type nodeEntry struct {
	Node      *Node
	AddedAt   time.Time
	LastSeen  time.Time
	FailCount int  // Consecutive connection failures.
	Static    bool // Static nodes are never removed by eviction.
}

// NewNodeTable creates an empty node table.
func NewNodeTable() *NodeTable {
	return &NodeTable{
		nodes: make(map[NodeID]*nodeEntry),
	}
}

// AddStatic adds a node to the table as a static peer. Static peers are never
// evicted and are always candidates for reconnection.
func (nt *NodeTable) AddStatic(n *Node) error {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if _, exists := nt.nodes[n.ID]; exists {
		return ErrNodeAlreadyKnown
	}
	now := time.Now()
	nt.nodes[n.ID] = &nodeEntry{
		Node:     n,
		AddedAt:  now,
		LastSeen: now,
		Static:   true,
	}
	return nil
}

// AddNode adds a discovered (non-static) node to the table.
func (nt *NodeTable) AddNode(n *Node) error {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if _, exists := nt.nodes[n.ID]; exists {
		return ErrNodeAlreadyKnown
	}
	now := time.Now()
	nt.nodes[n.ID] = &nodeEntry{
		Node:     n,
		AddedAt:  now,
		LastSeen: now,
		Static:   false,
	}
	return nil
}

// Remove removes a node from the table.
func (nt *NodeTable) Remove(id NodeID) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	delete(nt.nodes, id)
}

// Get returns the node with the given ID, or nil.
func (nt *NodeTable) Get(id NodeID) *Node {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	if e, ok := nt.nodes[id]; ok {
		return e.Node
	}
	return nil
}

// Len returns the number of nodes in the table.
func (nt *NodeTable) Len() int {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	return len(nt.nodes)
}

// StaticNodes returns all nodes marked as static.
func (nt *NodeTable) StaticNodes() []*Node {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	var result []*Node
	for _, e := range nt.nodes {
		if e.Static {
			result = append(result, e.Node)
		}
	}
	return result
}

// AllNodes returns all nodes in the table.
func (nt *NodeTable) AllNodes() []*Node {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	result := make([]*Node, 0, len(nt.nodes))
	for _, e := range nt.nodes {
		result = append(result, e.Node)
	}
	return result
}

// MarkSeen updates the last-seen time for a node and resets its failure count.
func (nt *NodeTable) MarkSeen(id NodeID) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if e, ok := nt.nodes[id]; ok {
		e.LastSeen = time.Now()
		e.FailCount = 0
	}
}

// MarkFailed increments the failure count for a node.
func (nt *NodeTable) MarkFailed(id NodeID) {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if e, ok := nt.nodes[id]; ok {
		e.FailCount++
	}
}

// FailCount returns the consecutive failure count for a node.
func (nt *NodeTable) FailCount(id NodeID) int {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	if e, ok := nt.nodes[id]; ok {
		return e.FailCount
	}
	return 0
}

// IsStatic returns whether the given node is a static peer.
func (nt *NodeTable) IsStatic(id NodeID) bool {
	nt.mu.RLock()
	defer nt.mu.RUnlock()
	if e, ok := nt.nodes[id]; ok {
		return e.Static
	}
	return false
}

// Evict removes non-static nodes that have exceeded maxFails consecutive
// connection failures. Returns the number of nodes evicted.
func (nt *NodeTable) Evict(maxFails int) int {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	evicted := 0
	for id, e := range nt.nodes {
		if !e.Static && e.FailCount >= maxFails {
			delete(nt.nodes, id)
			evicted++
		}
	}
	return evicted
}
