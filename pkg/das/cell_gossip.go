package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/sha3"
)

// Cell gossip errors.
var (
	ErrCellNotInCustody  = errors.New("das: cell not in node's custody subnet")
	ErrInvalidCellData   = errors.New("das: invalid cell data length")
	ErrInvalidBlobIndex  = errors.New("das: blob index out of range")
	ErrGossipCellIndex   = errors.New("das: cell index out of range for gossip")
	ErrNoBroadcastTarget = errors.New("das: no broadcast targets for cell")
)

// CellMessage is a gossip message carrying a single cell with its proof.
type CellMessage struct {
	// BlobIndex identifies which blob in the block this cell belongs to.
	BlobIndex uint64

	// CellIndex identifies the column position of this cell.
	CellIndex uint64

	// Data is the raw cell data.
	Data []byte

	// Proof is the KZG proof for this cell.
	Proof []byte
}

// SubnetConfig configures the gossip subnet topology.
type SubnetConfig struct {
	// NumSubnets is the total number of gossip subnets.
	NumSubnets uint64

	// SubnetsPerNode is how many subnets each node subscribes to.
	SubnetsPerNode uint64
}

// DefaultSubnetConfig returns the default subnet configuration based on
// PeerDAS parameters.
func DefaultSubnetConfig() SubnetConfig {
	return SubnetConfig{
		NumSubnets:     DataColumnSidecarSubnetCount, // 64
		SubnetsPerNode: CustodyRequirement,           // 4
	}
}

// GossipRouter routes cell messages to appropriate gossip subnets.
type GossipRouter struct {
	mu     sync.RWMutex
	config SubnetConfig

	// nodeSubnets caches the subnet assignments per node.
	nodeSubnets map[[32]byte][]uint64

	// subscribers tracks which nodes are subscribed to each subnet.
	subscribers map[uint64][][32]byte
}

// NewGossipRouter creates a new gossip router with the given configuration.
func NewGossipRouter(config SubnetConfig) *GossipRouter {
	return &GossipRouter{
		config:      config,
		nodeSubnets: make(map[[32]byte][]uint64),
		subscribers: make(map[uint64][][32]byte),
	}
}

// AssignSubnets deterministically assigns subnets to a node based on its ID.
// The assignment is stable: the same nodeID always gets the same subnets.
func AssignSubnets(nodeID [32]byte, config SubnetConfig) []uint64 {
	if config.SubnetsPerNode >= config.NumSubnets {
		subnets := make([]uint64, config.NumSubnets)
		for i := uint64(0); i < config.NumSubnets; i++ {
			subnets[i] = i
		}
		return subnets
	}

	subnets := make([]uint64, 0, config.SubnetsPerNode)
	seen := make(map[uint64]bool)

	// Hash-based deterministic assignment (similar to GetCustodyGroups).
	currentHash := nodeID
	for uint64(len(subnets)) < config.SubnetsPerNode {
		h := sha3.NewLegacyKeccak256()
		h.Write(currentHash[:])
		digest := h.Sum(nil)

		val := binary.LittleEndian.Uint64(digest[:8])
		subnet := val % config.NumSubnets

		if !seen[subnet] {
			seen[subnet] = true
			subnets = append(subnets, subnet)
		}

		copy(currentHash[:], digest[:32])
	}

	return subnets
}

// RegisterNode registers a node's subnet subscriptions with the router.
func (gr *GossipRouter) RegisterNode(nodeID [32]byte) []uint64 {
	gr.mu.Lock()
	defer gr.mu.Unlock()

	if existing, ok := gr.nodeSubnets[nodeID]; ok {
		return existing
	}

	subnets := AssignSubnets(nodeID, gr.config)
	gr.nodeSubnets[nodeID] = subnets

	for _, subnet := range subnets {
		gr.subscribers[subnet] = append(gr.subscribers[subnet], nodeID)
	}

	return subnets
}

// ShouldAccept returns true if the cell message belongs to one of the node's
// assigned custody subnets.
func (gr *GossipRouter) ShouldAccept(nodeID [32]byte, msg *CellMessage) bool {
	if msg == nil {
		return false
	}

	subnet := CellSubnet(msg.CellIndex, gr.config.NumSubnets)

	gr.mu.RLock()
	defer gr.mu.RUnlock()

	subnets, ok := gr.nodeSubnets[nodeID]
	if !ok {
		// Node not registered; compute on the fly.
		subnets = AssignSubnets(nodeID, gr.config)
	}

	for _, s := range subnets {
		if s == subnet {
			return true
		}
	}
	return false
}

// BroadcastCell determines which subnets should receive a cell message
// and returns the list of subscriber node IDs.
func (gr *GossipRouter) BroadcastCell(msg *CellMessage) ([][32]byte, error) {
	if msg == nil {
		return nil, ErrInvalidCellData
	}
	if msg.CellIndex >= NumberOfColumns {
		return nil, fmt.Errorf("%w: %d", ErrGossipCellIndex, msg.CellIndex)
	}

	subnet := CellSubnet(msg.CellIndex, gr.config.NumSubnets)

	gr.mu.RLock()
	defer gr.mu.RUnlock()

	nodes := gr.subscribers[subnet]
	if len(nodes) == 0 {
		return nil, ErrNoBroadcastTarget
	}

	// Return a copy to avoid data races.
	result := make([][32]byte, len(nodes))
	copy(result, nodes)
	return result, nil
}

// CellSubnet returns the subnet ID for a cell at the given column index.
func CellSubnet(cellIndex, numSubnets uint64) uint64 {
	if numSubnets == 0 {
		return 0
	}
	return cellIndex % numSubnets
}

// ValidateCellMessage performs basic validation on a cell gossip message.
func ValidateCellMessage(msg *CellMessage) error {
	if msg == nil {
		return ErrInvalidCellData
	}
	if msg.CellIndex >= NumberOfColumns {
		return fmt.Errorf("%w: cell index %d >= %d", ErrGossipCellIndex, msg.CellIndex, NumberOfColumns)
	}
	if msg.BlobIndex >= MaxBlobCommitmentsPerBlock {
		return fmt.Errorf("%w: blob index %d >= %d", ErrInvalidBlobIndex, msg.BlobIndex, MaxBlobCommitmentsPerBlock)
	}
	if len(msg.Data) == 0 || len(msg.Data) > BytesPerCell {
		return fmt.Errorf("%w: got %d bytes, max %d", ErrInvalidCellData, len(msg.Data), BytesPerCell)
	}
	return nil
}

// SubnetCount returns the number of registered nodes for a given subnet.
func (gr *GossipRouter) SubnetCount(subnet uint64) int {
	gr.mu.RLock()
	defer gr.mu.RUnlock()
	return len(gr.subscribers[subnet])
}
