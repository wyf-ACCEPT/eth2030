// Package enode implements Ethereum node identification and enode:// URL parsing.
// NodeID is a 32-byte identifier derived from the keccak256 hash of the node's
// compressed secp256k1 public key.
package enode

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"strconv"
	"strings"

	"github.com/eth2030/eth2030/p2p/enr"
)

// NodeID is a 32-byte unique identifier for a node (keccak256 of compressed pubkey).
type NodeID [32]byte

// String returns the hex-encoded node ID.
func (id NodeID) String() string {
	return hex.EncodeToString(id[:])
}

// IsZero reports whether the ID is all zeros.
func (id NodeID) IsZero() bool {
	return id == NodeID{}
}

// HexID converts a hex string to a NodeID. Panics if invalid.
func HexID(s string) NodeID {
	id, err := ParseID(s)
	if err != nil {
		panic("invalid node ID: " + err.Error())
	}
	return id
}

// ParseID parses a hex-encoded node ID. The "0x" prefix is optional.
func ParseID(s string) (NodeID, error) {
	s = strings.TrimPrefix(s, "0x")
	b, err := hex.DecodeString(s)
	if err != nil {
		return NodeID{}, err
	}
	if len(b) != 32 {
		return NodeID{}, fmt.Errorf("enode: wrong ID length %d, want 32", len(b))
	}
	var id NodeID
	copy(id[:], b)
	return id, nil
}

// Node represents a network node with its identification, network endpoints,
// and optional ENR record.
type Node struct {
	ID     NodeID
	IP     net.IP
	TCP    uint16
	UDP    uint16
	Record *enr.Record
	Pubkey []byte // compressed secp256k1 public key (33 bytes)
}

// NewNode creates a Node with the given ID and network endpoints.
func NewNode(id NodeID, ip net.IP, tcp, udp uint16) *Node {
	return &Node{
		ID:  id,
		IP:  ip,
		TCP: tcp,
		UDP: udp,
	}
}

// String returns the enode:// URL representation.
// Format: enode://<hex-pubkey-or-id>@<ip>:<tcp-port>?discport=<udp-port>
func (n *Node) String() string {
	id := hex.EncodeToString(n.Pubkey)
	if len(n.Pubkey) == 0 {
		id = n.ID.String()
	}
	ip := "127.0.0.1"
	if n.IP != nil {
		ip = n.IP.String()
	}
	s := fmt.Sprintf("enode://%s@%s:%d", id, ip, n.TCP)
	if n.UDP != 0 && n.UDP != n.TCP {
		s += fmt.Sprintf("?discport=%d", n.UDP)
	}
	return s
}

// Addr returns the UDP address of the node.
func (n *Node) Addr() net.UDPAddr {
	return net.UDPAddr{
		IP:   n.IP,
		Port: int(n.UDP),
	}
}

// TCPAddr returns the TCP address of the node.
func (n *Node) TCPAddr() net.TCPAddr {
	return net.TCPAddr{
		IP:   n.IP,
		Port: int(n.TCP),
	}
}

// ParseNode parses an enode:// URL into a Node.
// Format: enode://<hex-node-id>@<ip>:<tcp-port>[?discport=<udp-port>]
func ParseNode(rawurl string) (*Node, error) {
	if !strings.HasPrefix(rawurl, "enode://") {
		return nil, errors.New("enode: missing enode:// prefix")
	}
	rest := rawurl[len("enode://"):]

	// Split at '@'.
	atIdx := strings.Index(rest, "@")
	if atIdx < 0 {
		return nil, errors.New("enode: missing @ separator")
	}
	hexID := rest[:atIdx]
	hostPort := rest[atIdx+1:]

	// Parse the hex ID (could be 64-byte pubkey or 32-byte ID).
	idBytes, err := hex.DecodeString(hexID)
	if err != nil {
		return nil, fmt.Errorf("enode: invalid hex node ID: %w", err)
	}
	if len(idBytes) != 32 && len(idBytes) != 33 && len(idBytes) != 64 && len(idBytes) != 65 {
		return nil, fmt.Errorf("enode: invalid node ID length %d", len(idBytes))
	}

	// Split query parameters.
	var hostPortPart, queryPart string
	if qIdx := strings.Index(hostPort, "?"); qIdx >= 0 {
		hostPortPart = hostPort[:qIdx]
		queryPart = hostPort[qIdx+1:]
	} else {
		hostPortPart = hostPort
	}

	// Parse host:port.
	host, portStr, err := net.SplitHostPort(hostPortPart)
	if err != nil {
		return nil, fmt.Errorf("enode: invalid host:port: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("enode: invalid IP address %q", host)
	}
	tcpPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("enode: invalid TCP port: %w", err)
	}

	// UDP defaults to TCP port.
	udpPort := tcpPort

	// Parse query params for discport.
	if queryPart != "" {
		for _, param := range strings.Split(queryPart, "&") {
			kv := strings.SplitN(param, "=", 2)
			if len(kv) == 2 && kv[0] == "discport" {
				dp, err := strconv.ParseUint(kv[1], 10, 16)
				if err != nil {
					return nil, fmt.Errorf("enode: invalid discport: %w", err)
				}
				udpPort = dp
			}
		}
	}

	node := &Node{
		IP:  ip,
		TCP: uint16(tcpPort),
		UDP: uint16(udpPort),
	}

	// Set ID and pubkey based on the hex length.
	switch len(idBytes) {
	case 32:
		copy(node.ID[:], idBytes)
	case 33:
		// Compressed pubkey.
		node.Pubkey = idBytes
		// Derive NodeID = keccak256(compressed).
		node.ID = enrNodeID(idBytes)
	case 64, 65:
		// Uncompressed pubkey (with or without 0x04 prefix).
		node.Pubkey = idBytes
		copy(node.ID[:], idBytes[:32]) // simplified: take first 32 bytes
	}

	return node, nil
}

// enrNodeID computes keccak256 of compressed pubkey. This avoids importing crypto
// directly by delegating to the caller or using the ENR record's NodeID.
// For enode parsing with raw bytes, we compute directly.
func enrNodeID(compressed []byte) NodeID {
	// We create a temporary record to compute the ID using the ENR approach.
	r := &enr.Record{}
	r.Set(enr.KeySecp256k1, compressed)
	return NodeID(r.NodeID())
}

// Distance returns the XOR log distance between two NodeIDs: log2(a XOR b).
// Returns 0 if a == b.
func Distance(a, b NodeID) int {
	lz := 0
	for i := 0; i < len(a); i += 8 {
		ai := binary.BigEndian.Uint64(a[i : i+8])
		bi := binary.BigEndian.Uint64(b[i : i+8])
		x := ai ^ bi
		if x == 0 {
			lz += 64
		} else {
			lz += bits.LeadingZeros64(x)
			break
		}
	}
	return len(a)*8 - lz
}

// DistCmp compares distances a->target and b->target.
// Returns -1 if a is closer to target, 1 if b is closer, 0 if equal.
func DistCmp(target, a, b NodeID) int {
	for i := 0; i < len(target); i += 8 {
		tn := binary.BigEndian.Uint64(target[i : i+8])
		da := tn ^ binary.BigEndian.Uint64(a[i:i+8])
		db := tn ^ binary.BigEndian.Uint64(b[i:i+8])
		if da > db {
			return 1
		} else if da < db {
			return -1
		}
	}
	return 0
}
