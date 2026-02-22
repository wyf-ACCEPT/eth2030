// column_custody.go implements a ColumnCustodyManager that manages which data
// columns a node is responsible for, provides deterministic custody assignment
// based on node ID (CUSTODY_REQUIREMENT=4), persistent column storage with
// expiry, per-epoch custody rotation, custody proof response generation, and
// network sampling of random columns from peers.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/crypto/sha3"
)

// Column custody errors.
var (
	ErrCustodyManagerClosed   = errors.New("das/custody-mgr: manager is closed")
	ErrColumnNotInCustody     = errors.New("das/custody-mgr: column not in custody set")
	ErrColumnExpired          = errors.New("das/custody-mgr: column data expired")
	ErrColumnAlreadyStored    = errors.New("das/custody-mgr: column already stored")
	ErrSamplingNoPeers        = errors.New("das/custody-mgr: no peers available for sampling")
	ErrSamplingTimeout        = errors.New("das/custody-mgr: sampling request timed out")
	ErrInvalidEpoch           = errors.New("das/custody-mgr: invalid epoch number")
	ErrCustodyRotationPending = errors.New("das/custody-mgr: rotation already in progress")
)

// CustodyManagerParams holds the configurable parameters for the custody manager.
type CustodyManagerParams struct {
	// NumberOfColumns is the total columns in the extended data matrix.
	NumberOfColumns uint64

	// CustodyRequirement is the minimum number of custody groups.
	CustodyRequirement uint64

	// SamplesPerSlot is how many random columns to sample per slot.
	SamplesPerSlot int

	// ColumnExpiryEpochs is how many epochs to keep column data before expiry.
	ColumnExpiryEpochs uint64

	// MaxStoredColumns is the maximum number of stored column entries.
	MaxStoredColumns int
}

// DefaultCustodyManagerParams returns the default params from the Fulu spec.
func DefaultCustodyManagerParams() CustodyManagerParams {
	return CustodyManagerParams{
		NumberOfColumns:    NumberOfColumns,
		CustodyRequirement: CustodyRequirement,
		SamplesPerSlot:     SamplesPerSlot,
		ColumnExpiryEpochs: 256,
		MaxStoredColumns:   16384,
	}
}

// CustodyAssignment describes which columns a node is assigned for a given epoch.
type CustodyAssignment struct {
	NodeID        [32]byte
	Epoch         uint64
	ColumnIndices []uint64
	GroupIndices  []CustodyGroup
}

// StoredColumn represents a custody column stored locally with expiry metadata.
type StoredColumn struct {
	Index     uint64
	Epoch     uint64
	Slot      uint64
	Data      []byte
	StoredAt  time.Time
	ExpiresAt time.Time
}

// CustodyRotation tracks the custody rotation state across epochs.
type CustodyRotation struct {
	PreviousEpoch    uint64
	CurrentEpoch     uint64
	PreviousColumns  []uint64
	CurrentColumns   []uint64
	RotatedAt        time.Time
	PendingMigration bool
}

// CustodyProofResponse contains the data needed to respond to a custody challenge.
type CustodyProofResponse struct {
	NodeID      [32]byte
	Epoch       uint64
	Column      uint64
	CellData    []byte
	ProofHash   []byte
	GeneratedAt time.Time
}

// NetworkSamplingRequest represents a request to sample a column from peers.
type NetworkSamplingRequest struct {
	ColumnIndex uint64
	Slot        uint64
	PeerID      [32]byte
	Timeout     time.Duration
}

// NetworkSamplingResult contains the result of a network sampling request.
type NetworkSamplingResult struct {
	ColumnIndex uint64
	Slot        uint64
	PeerID      [32]byte
	Data        []byte
	Success     bool
	Latency     time.Duration
	Error       error
}

// ColumnCustodyManager manages which data columns this node is responsible for.
// It handles deterministic assignment, persistent storage, epoch rotation,
// proof generation, and network sampling. All methods are safe for concurrent use.
type ColumnCustodyManager struct {
	mu     sync.RWMutex
	params CustodyManagerParams
	nodeID [32]byte

	// current is the active custody assignment.
	current *CustodyAssignment

	// rotation tracks the last epoch rotation.
	rotation *CustodyRotation

	// store maps (epoch, columnIndex) to stored column data.
	store map[columnStoreKey]*StoredColumn

	// storeCount tracks the number of entries in the store.
	storeCount int

	// closed indicates the manager has been shut down.
	closed bool
}

type columnStoreKey struct {
	epoch  uint64
	column uint64
}

// NewColumnCustodyManager creates a new custody manager for the given node.
func NewColumnCustodyManager(params CustodyManagerParams, nodeID [32]byte) *ColumnCustodyManager {
	if params.NumberOfColumns == 0 {
		params.NumberOfColumns = NumberOfColumns
	}
	if params.CustodyRequirement == 0 {
		params.CustodyRequirement = CustodyRequirement
	}
	if params.SamplesPerSlot == 0 {
		params.SamplesPerSlot = SamplesPerSlot
	}
	if params.ColumnExpiryEpochs == 0 {
		params.ColumnExpiryEpochs = 256
	}
	if params.MaxStoredColumns == 0 {
		params.MaxStoredColumns = 16384
	}
	return &ColumnCustodyManager{
		params: params,
		nodeID: nodeID,
		store:  make(map[columnStoreKey]*StoredColumn),
	}
}

// ComputeAssignment deterministically computes the custody assignment for a
// given epoch. The assignment is derived from the node ID and epoch number,
// ensuring determinism across all nodes.
func (m *ColumnCustodyManager) ComputeAssignment(epoch uint64) (*CustodyAssignment, error) {
	if epoch == 0 {
		return nil, ErrInvalidEpoch
	}

	// Derive epoch-specific node ID: keccak256(nodeID || epoch).
	epochNodeID := deriveEpochNodeID(m.nodeID, epoch)

	groups, err := GetCustodyGroups(epochNodeID, m.params.CustodyRequirement)
	if err != nil {
		return nil, fmt.Errorf("das/custody-mgr: failed to get custody groups: %w", err)
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

	return &CustodyAssignment{
		NodeID:        m.nodeID,
		Epoch:         epoch,
		ColumnIndices: columns,
		GroupIndices:  groups,
	}, nil
}

// SetEpoch updates the manager to the given epoch, computing and storing
// the new custody assignment and rotating from the previous epoch.
func (m *ColumnCustodyManager) SetEpoch(epoch uint64) error {
	if epoch == 0 {
		return ErrInvalidEpoch
	}

	assignment, err := m.ComputeAssignment(epoch)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrCustodyManagerClosed
	}

	var previousColumns []uint64
	var previousEpoch uint64
	if m.current != nil {
		previousColumns = m.current.ColumnIndices
		previousEpoch = m.current.Epoch
	}

	m.current = assignment
	m.rotation = &CustodyRotation{
		PreviousEpoch:    previousEpoch,
		CurrentEpoch:     epoch,
		PreviousColumns:  previousColumns,
		CurrentColumns:   assignment.ColumnIndices,
		RotatedAt:        time.Now(),
		PendingMigration: previousEpoch > 0 && previousEpoch != epoch,
	}

	// Expire old columns.
	m.expireColumnsLocked(epoch)

	return nil
}

// CurrentAssignment returns a copy of the current custody assignment.
func (m *ColumnCustodyManager) CurrentAssignment() *CustodyAssignment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return nil
	}
	cp := *m.current
	cols := make([]uint64, len(m.current.ColumnIndices))
	copy(cols, m.current.ColumnIndices)
	cp.ColumnIndices = cols
	return &cp
}

// IsInCustody returns true if the given column index is in the current
// custody assignment.
func (m *ColumnCustodyManager) IsInCustody(columnIndex uint64) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.current == nil {
		return false
	}
	for _, c := range m.current.ColumnIndices {
		if c == columnIndex {
			return true
		}
	}
	return false
}

// StoreColumn persists a custody column for a given epoch and slot.
func (m *ColumnCustodyManager) StoreColumn(epoch, slot, columnIndex uint64, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrCustodyManagerClosed
	}

	key := columnStoreKey{epoch: epoch, column: columnIndex}
	if _, exists := m.store[key]; exists {
		return ErrColumnAlreadyStored
	}

	// Enforce max capacity by evicting oldest entries.
	if m.storeCount >= m.params.MaxStoredColumns {
		m.evictOldestLocked()
	}

	now := time.Now()
	expiryDuration := time.Duration(m.params.ColumnExpiryEpochs*32*12) * time.Second
	m.store[key] = &StoredColumn{
		Index:     columnIndex,
		Epoch:     epoch,
		Slot:      slot,
		Data:      append([]byte(nil), data...),
		StoredAt:  now,
		ExpiresAt: now.Add(expiryDuration),
	}
	m.storeCount++
	return nil
}

// GetColumn retrieves a stored custody column by epoch and column index.
func (m *ColumnCustodyManager) GetColumn(epoch, columnIndex uint64) (*StoredColumn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := columnStoreKey{epoch: epoch, column: columnIndex}
	col, ok := m.store[key]
	if !ok {
		return nil, ErrColumnNotInCustody
	}
	if time.Now().After(col.ExpiresAt) {
		return nil, ErrColumnExpired
	}
	return col, nil
}

// StoreCount returns the number of stored columns.
func (m *ColumnCustodyManager) StoreCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.storeCount
}

// GetRotation returns the current rotation state.
func (m *ColumnCustodyManager) GetRotation() *CustodyRotation {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.rotation == nil {
		return nil
	}
	cp := *m.rotation
	return &cp
}

// GenerateProofResponse generates a custody proof response for a challenge
// targeting a specific column and epoch.
func (m *ColumnCustodyManager) GenerateProofResponse(epoch, column uint64) (*CustodyProofResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return nil, ErrCustodyManagerClosed
	}

	key := columnStoreKey{epoch: epoch, column: column}
	stored, ok := m.store[key]
	if !ok {
		return nil, fmt.Errorf("%w: column %d epoch %d", ErrColumnNotInCustody, column, epoch)
	}
	if time.Now().After(stored.ExpiresAt) {
		return nil, ErrColumnExpired
	}

	// Compute proof hash: keccak256(nodeID || epoch || column || data).
	h := sha3.NewLegacyKeccak256()
	h.Write(m.nodeID[:])
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], epoch)
	binary.LittleEndian.PutUint64(buf[8:], column)
	h.Write(buf[:])
	h.Write(stored.Data)

	return &CustodyProofResponse{
		NodeID:      m.nodeID,
		Epoch:       epoch,
		Column:      column,
		CellData:    append([]byte(nil), stored.Data...),
		ProofHash:   h.Sum(nil),
		GeneratedAt: time.Now(),
	}, nil
}

// SelectSampleColumns selects random columns to sample from network peers
// for a given slot.
func (m *ColumnCustodyManager) SelectSampleColumns(slot uint64) []uint64 {
	h := sha3.NewLegacyKeccak256()
	h.Write(m.nodeID[:])
	var slotBuf [8]byte
	binary.LittleEndian.PutUint64(slotBuf[:], slot)
	h.Write(slotBuf[:])
	seed := h.Sum(nil)

	seen := make(map[uint64]bool)
	var result []uint64
	for counter := uint64(0); len(result) < m.params.SamplesPerSlot; counter++ {
		sh := sha3.NewLegacyKeccak256()
		sh.Write(seed)
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		sh.Write(cBuf[:])
		digest := sh.Sum(nil)
		val := binary.LittleEndian.Uint64(digest[:8])
		col := val % m.params.NumberOfColumns
		if !seen[col] {
			seen[col] = true
			result = append(result, col)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// Close shuts down the custody manager.
func (m *ColumnCustodyManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// expireColumnsLocked removes columns older than ColumnExpiryEpochs.
// Caller must hold m.mu.
func (m *ColumnCustodyManager) expireColumnsLocked(currentEpoch uint64) {
	if currentEpoch <= m.params.ColumnExpiryEpochs {
		return
	}
	cutoff := currentEpoch - m.params.ColumnExpiryEpochs
	for key := range m.store {
		if key.epoch < cutoff {
			delete(m.store, key)
			m.storeCount--
		}
	}
}

// evictOldestLocked removes the oldest stored column. Caller must hold m.mu.
func (m *ColumnCustodyManager) evictOldestLocked() {
	var oldestKey columnStoreKey
	var oldestTime time.Time
	found := false
	for key, col := range m.store {
		if !found || col.StoredAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = col.StoredAt
			found = true
		}
	}
	if found {
		delete(m.store, oldestKey)
		m.storeCount--
	}
}

// deriveEpochNodeID derives an epoch-specific node ID for custody rotation.
func deriveEpochNodeID(nodeID [32]byte, epoch uint64) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(nodeID[:])
	var epochBuf [8]byte
	binary.LittleEndian.PutUint64(epochBuf[:], epoch)
	h.Write(epochBuf[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}
