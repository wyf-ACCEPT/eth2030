// custody_subnet.go implements custody subnet management per the PeerDAS spec.
// It manages which subnets a node is responsible for, provides peer discovery
// by custody column, and validates that a node has all required custody columns.
package das

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Custody subnet errors.
var (
	ErrCustodyGroupCountExceeded = errors.New("das/custody: group count exceeds maximum")
	ErrMissingCustodyColumn      = errors.New("das/custody: missing required custody column")
	ErrColumnOutOfRange          = errors.New("das/custody: column index out of range")
	ErrNoPeersForColumn          = errors.New("das/custody: no peers found for column")
)

// CustodyConfig holds the configurable parameters for custody subnet management.
type CustodyConfig struct {
	// CustodyRequirement is the minimum number of custody groups an honest
	// node custodies and serves samples from (default: 4).
	CustodyRequirement uint64

	// NumberOfColumns is the total number of columns in the extended data
	// matrix (default: 128).
	NumberOfColumns uint64

	// DataColumnSidecarSubnetCount is the number of data column sidecar
	// subnets used in the gossipsub protocol (default: 128).
	DataColumnSidecarSubnetCount uint64

	// NumberOfCustodyGroups is the number of custody groups available for
	// nodes to custody (default: 128).
	NumberOfCustodyGroups uint64
}

// DefaultCustodyConfig returns the default custody configuration from the
// Fulu consensus spec.
func DefaultCustodyConfig() CustodyConfig {
	return CustodyConfig{
		CustodyRequirement:           CustodyRequirement,
		NumberOfColumns:              NumberOfColumns,
		DataColumnSidecarSubnetCount: DataColumnSidecarSubnetCount,
		NumberOfCustodyGroups:        NumberOfCustodyGroups,
	}
}

// SubnetAssignment describes which subnets and columns a node is responsible
// for custodying.
type SubnetAssignment struct {
	// NodeID is the 32-byte identifier of the node.
	NodeID [32]byte

	// CustodyGroups lists the custody group indices assigned to this node.
	CustodyGroups []CustodyGroup

	// SubnetIDs lists the subnet IDs this node must subscribe to.
	SubnetIDs []uint64

	// ColumnIndices lists all column indices this node must custody.
	ColumnIndices []uint64
}

// PeerInfo stores information about a peer's custody assignment used for
// peer discovery.
type PeerInfo struct {
	// NodeID is the peer's 32-byte identifier.
	NodeID [32]byte

	// CustodyGroupCount is the number of custody groups the peer advertises.
	CustodyGroupCount uint64

	// CustodiedColumns is the sorted set of columns the peer custodies.
	CustodiedColumns []uint64
}

// CustodySubnetManager manages data availability custody subnets per PeerDAS.
// It tracks which subnets a node is responsible for, provides methods to
// compute custody assignments, and supports peer discovery for specific columns.
// All methods are safe for concurrent use.
type CustodySubnetManager struct {
	mu     sync.RWMutex
	config CustodyConfig

	// localAssignment is the local node's custody assignment.
	localAssignment *SubnetAssignment

	// peers maps node IDs to their peer information.
	peers map[[32]byte]*PeerInfo

	// columnToPeers maps column indices to the set of peer node IDs that
	// custody that column. Used for fast peer discovery.
	columnToPeers map[uint64][][32]byte
}

// NewCustodySubnetManager creates a new custody subnet manager with the
// given configuration.
func NewCustodySubnetManager(config CustodyConfig) *CustodySubnetManager {
	return &CustodySubnetManager{
		config:        config,
		peers:         make(map[[32]byte]*PeerInfo),
		columnToPeers: make(map[uint64][][32]byte),
	}
}

// AssignCustodyForNode computes the full custody assignment for a node,
// including custody groups, subnet IDs, and column indices. The assignment
// is deterministic for a given (nodeID, custodyGroupCount) pair.
func (m *CustodySubnetManager) AssignCustodyForNode(nodeID [32]byte, custodyGroupCount uint64) (SubnetAssignment, error) {
	if custodyGroupCount > m.config.NumberOfCustodyGroups {
		return SubnetAssignment{}, fmt.Errorf("%w: %d > %d",
			ErrCustodyGroupCountExceeded, custodyGroupCount, m.config.NumberOfCustodyGroups)
	}

	// Clamp to minimum.
	if custodyGroupCount < m.config.CustodyRequirement {
		custodyGroupCount = m.config.CustodyRequirement
	}

	groups, err := GetCustodyGroups(nodeID, custodyGroupCount)
	if err != nil {
		return SubnetAssignment{}, err
	}

	columns := m.ComputeCustodyColumns(nodeID, custodyGroupCount, m.columnsPerGroup())

	// Compute subnet IDs from columns.
	subnetSet := make(map[uint64]bool)
	for _, col := range columns {
		subnet := m.SubnetForColumn(col)
		subnetSet[subnet] = true
	}
	subnets := make([]uint64, 0, len(subnetSet))
	for s := range subnetSet {
		subnets = append(subnets, s)
	}
	sort.Slice(subnets, func(i, j int) bool { return subnets[i] < subnets[j] })

	return SubnetAssignment{
		NodeID:        nodeID,
		CustodyGroups: groups,
		SubnetIDs:     subnets,
		ColumnIndices: columns,
	}, nil
}

// SetLocalNode sets the local node's custody assignment. This is used to
// determine which columns the local node must custody.
func (m *CustodySubnetManager) SetLocalNode(nodeID [32]byte, custodyGroupCount uint64) error {
	assignment, err := m.AssignCustodyForNode(nodeID, custodyGroupCount)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.localAssignment = &assignment
	return nil
}

// LocalAssignment returns a copy of the local node's current custody assignment.
// Returns nil if no local node has been set.
func (m *CustodySubnetManager) LocalAssignment() *SubnetAssignment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.localAssignment == nil {
		return nil
	}
	cp := *m.localAssignment
	return &cp
}

// ComputeCustodyColumns computes all column indices that a node should custody,
// given its node ID, custody group count, and columns per group. The result
// is sorted in ascending order.
func (m *CustodySubnetManager) ComputeCustodyColumns(nodeID [32]byte, custodyGroupCount uint64, columnsPerGroup int) []uint64 {
	if custodyGroupCount < m.config.CustodyRequirement {
		custodyGroupCount = m.config.CustodyRequirement
	}
	if custodyGroupCount > m.config.NumberOfCustodyGroups {
		custodyGroupCount = m.config.NumberOfCustodyGroups
	}

	groups, err := GetCustodyGroups(nodeID, custodyGroupCount)
	if err != nil {
		return nil
	}

	var columns []uint64
	for _, g := range groups {
		cols, err := ComputeColumnsForCustodyGroup(g)
		if err != nil {
			continue
		}
		for _, c := range cols {
			columns = append(columns, uint64(c))
		}
	}

	sort.Slice(columns, func(i, j int) bool { return columns[i] < columns[j] })
	return columns
}

// IsInCustody returns true if the given column index is in the specified
// node's custody set. It uses the default custody requirement.
func (m *CustodySubnetManager) IsInCustody(nodeID [32]byte, columnIndex uint64) bool {
	if columnIndex >= m.config.NumberOfColumns {
		return false
	}
	columns := m.ComputeCustodyColumns(nodeID, m.config.CustodyRequirement, m.columnsPerGroup())
	for _, c := range columns {
		if c == columnIndex {
			return true
		}
	}
	return false
}

// SubnetForColumn returns the subnet ID for a given column index.
// subnet_id = column_index % DATA_COLUMN_SIDECAR_SUBNET_COUNT
func (m *CustodySubnetManager) SubnetForColumn(columnIndex uint64) uint64 {
	if m.config.DataColumnSidecarSubnetCount == 0 {
		return 0
	}
	return columnIndex % m.config.DataColumnSidecarSubnetCount
}

// ValidateCustody verifies that the given set of data columns contains all
// columns required by the local node's custody assignment. Returns an error
// listing the first missing column if any required column is absent.
func (m *CustodySubnetManager) ValidateCustody(columns []DataColumn) error {
	m.mu.RLock()
	local := m.localAssignment
	m.mu.RUnlock()

	if local == nil {
		// No local assignment set; no columns required.
		return nil
	}

	// Build a set of column indices we have.
	have := make(map[uint64]bool, len(columns))
	for _, col := range columns {
		idx := uint64(col.Index)
		if idx >= m.config.NumberOfColumns {
			return fmt.Errorf("%w: %d >= %d", ErrColumnOutOfRange, idx, m.config.NumberOfColumns)
		}
		have[idx] = true
	}

	// Check every required column is present.
	for _, required := range local.ColumnIndices {
		if !have[required] {
			return fmt.Errorf("%w: column %d", ErrMissingCustodyColumn, required)
		}
	}
	return nil
}

// RegisterPeer registers a peer's custody information for peer discovery.
// The peer's custodied columns are computed deterministically from its
// node ID and advertised custody group count.
func (m *CustodySubnetManager) RegisterPeer(nodeID [32]byte, custodyGroupCount uint64) {
	if custodyGroupCount < m.config.CustodyRequirement {
		custodyGroupCount = m.config.CustodyRequirement
	}
	if custodyGroupCount > m.config.NumberOfCustodyGroups {
		custodyGroupCount = m.config.NumberOfCustodyGroups
	}

	columns := m.ComputeCustodyColumns(nodeID, custodyGroupCount, m.columnsPerGroup())

	info := &PeerInfo{
		NodeID:            nodeID,
		CustodyGroupCount: custodyGroupCount,
		CustodiedColumns:  columns,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove old entries if peer is being re-registered.
	if old, exists := m.peers[nodeID]; exists {
		m.removePeerColumnsLocked(nodeID, old.CustodiedColumns)
	}

	m.peers[nodeID] = info
	for _, col := range columns {
		m.columnToPeers[col] = append(m.columnToPeers[col], nodeID)
	}
}

// UnregisterPeer removes a peer from the peer registry.
func (m *CustodySubnetManager) UnregisterPeer(nodeID [32]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, exists := m.peers[nodeID]
	if !exists {
		return
	}
	m.removePeerColumnsLocked(nodeID, info.CustodiedColumns)
	delete(m.peers, nodeID)
}

// removePeerColumnsLocked removes a peer from all column-to-peer mappings.
// Caller must hold m.mu write lock.
func (m *CustodySubnetManager) removePeerColumnsLocked(nodeID [32]byte, columns []uint64) {
	for _, col := range columns {
		peers := m.columnToPeers[col]
		filtered := peers[:0]
		for _, p := range peers {
			if p != nodeID {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			delete(m.columnToPeers, col)
		} else {
			m.columnToPeers[col] = filtered
		}
	}
}

// FindPeersForColumn returns the node IDs of peers that custody the given
// column index. This is used for targeted sample requests.
func (m *CustodySubnetManager) FindPeersForColumn(columnIndex uint64) ([][32]byte, error) {
	if columnIndex >= m.config.NumberOfColumns {
		return nil, fmt.Errorf("%w: %d >= %d", ErrColumnOutOfRange, columnIndex, m.config.NumberOfColumns)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	peers := m.columnToPeers[columnIndex]
	if len(peers) == 0 {
		return nil, fmt.Errorf("%w: column %d", ErrNoPeersForColumn, columnIndex)
	}

	// Return a copy to prevent races.
	result := make([][32]byte, len(peers))
	copy(result, peers)
	return result, nil
}

// PeerCount returns the number of registered peers.
func (m *CustodySubnetManager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// columnsPerGroup returns NUMBER_OF_COLUMNS / NUMBER_OF_CUSTODY_GROUPS.
func (m *CustodySubnetManager) columnsPerGroup() int {
	if m.config.NumberOfCustodyGroups == 0 {
		return 0
	}
	return int(m.config.NumberOfColumns / m.config.NumberOfCustodyGroups)
}
