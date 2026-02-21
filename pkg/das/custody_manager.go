// custody_manager.go implements a high-level data custody manager for PeerDAS
// (EIP-7594). It manages which columns a node is responsible for, tracks
// custody groups, handles custody rotation on epoch boundaries, validates
// custody proofs, and monitors custody completeness. The manager coordinates
// between the lower-level ColumnCustodyManager and CustodySubnetManager.
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

// Custody manager errors.
var (
	ErrCustodyMgrClosed        = errors.New("das/custody-manager: manager is closed")
	ErrCustodyMgrNotInitialized = errors.New("das/custody-manager: not initialized")
	ErrCustodyMgrEpochZero     = errors.New("das/custody-manager: epoch must be > 0")
	ErrCustodyMgrColumnOOB     = errors.New("das/custody-manager: column index out of range")
	ErrCustodyMgrSlotOOB       = errors.New("das/custody-manager: slot out of epoch range")
	ErrCustodyMgrIncomplete    = errors.New("das/custody-manager: custody set incomplete")
	ErrCustodyMgrProofInvalid  = errors.New("das/custody-manager: custody proof invalid")
	ErrCustodyMgrAlreadyStored = errors.New("das/custody-manager: column data already stored")
	ErrCustodyMgrRotationBusy  = errors.New("das/custody-manager: rotation in progress")
)

// CustodyManagerConfig configures the CustodyManager.
type CustodyManagerConfig struct {
	// CustodyRequirement is the minimum number of custody groups.
	CustodyRequirement uint64

	// NumberOfColumns is the total columns in the extended data matrix.
	NumberOfColumns uint64

	// NumberOfCustodyGroups is the total number of custody groups.
	NumberOfCustodyGroups uint64

	// SlotsPerEpoch is the number of slots in an epoch.
	SlotsPerEpoch uint64

	// RetentionEpochs is how many epochs to retain column data.
	RetentionEpochs uint64

	// MaxTrackedSlots is the maximum number of slots to track completeness.
	MaxTrackedSlots int
}

// DefaultCustodyManagerConfig returns production defaults from the Fulu spec.
func DefaultCustodyManagerConfig() CustodyManagerConfig {
	return CustodyManagerConfig{
		CustodyRequirement:    CustodyRequirement,
		NumberOfColumns:       NumberOfColumns,
		NumberOfCustodyGroups: NumberOfCustodyGroups,
		SlotsPerEpoch:         32,
		RetentionEpochs:       256,
		MaxTrackedSlots:       1024,
	}
}

// CustodyEpochState captures the custody state for a single epoch.
type CustodyEpochState struct {
	Epoch         uint64
	Groups        []CustodyGroup
	Columns       []uint64
	ActivatedAt   time.Time
	DeactivatedAt time.Time
	Active        bool
}

// SlotCompleteness tracks which required columns have been received for a slot.
type SlotCompleteness struct {
	Slot            uint64
	Epoch           uint64
	RequiredColumns []uint64
	ReceivedColumns map[uint64]bool
	Complete        bool
	Timestamp       time.Time
}

// CustodyRotationEvent records an epoch rotation transition.
type CustodyRotationEvent struct {
	FromEpoch    uint64
	ToEpoch      uint64
	AddedColumns []uint64
	DroppedColumns []uint64
	Timestamp    time.Time
}

// CustodyProofRequest is a request to prove custody of a column.
type CustodyProofRequest struct {
	NodeID [32]byte
	Epoch  uint64
	Column uint64
	Slot   uint64
}

// CustodyProofResult is the result of verifying a custody proof.
type CustodyProofResult struct {
	Valid     bool
	Column    uint64
	Epoch     uint64
	ProofHash [32]byte
	Reason    string
}

// CustodyManager provides high-level custody management for PeerDAS. It
// coordinates column assignment, epoch rotation, data tracking, proof
// validation, and completeness monitoring. All methods are safe for
// concurrent use.
type CustodyManager struct {
	mu     sync.RWMutex
	config CustodyManagerConfig
	nodeID [32]byte

	// current epoch state
	currentEpoch uint64
	epochStates  map[uint64]*CustodyEpochState

	// column data store: maps (epoch, slot, column) -> data hash
	columnStore map[custodyDataKey][32]byte

	// slot completeness tracking
	slotTracker map[uint64]*SlotCompleteness

	// rotation history (most recent first)
	rotationHistory []*CustodyRotationEvent

	// closed signals shutdown
	closed bool
}

// custodyDataKey uniquely identifies a stored column.
type custodyDataKey struct {
	epoch  uint64
	slot   uint64
	column uint64
}

// NewCustodyManager creates a new custody manager for the given node.
func NewCustodyManager(config CustodyManagerConfig, nodeID [32]byte) *CustodyManager {
	if config.CustodyRequirement == 0 {
		config.CustodyRequirement = CustodyRequirement
	}
	if config.NumberOfColumns == 0 {
		config.NumberOfColumns = NumberOfColumns
	}
	if config.NumberOfCustodyGroups == 0 {
		config.NumberOfCustodyGroups = NumberOfCustodyGroups
	}
	if config.SlotsPerEpoch == 0 {
		config.SlotsPerEpoch = 32
	}
	if config.RetentionEpochs == 0 {
		config.RetentionEpochs = 256
	}
	if config.MaxTrackedSlots == 0 {
		config.MaxTrackedSlots = 1024
	}
	return &CustodyManager{
		config:      config,
		nodeID:      nodeID,
		epochStates: make(map[uint64]*CustodyEpochState),
		columnStore: make(map[custodyDataKey][32]byte),
		slotTracker: make(map[uint64]*SlotCompleteness),
	}
}

// Initialize sets up the manager for the given starting epoch.
func (cm *CustodyManager) Initialize(epoch uint64) error {
	if epoch == 0 {
		return ErrCustodyMgrEpochZero
	}

	groups, columns, err := cm.computeAssignment(epoch)
	if err != nil {
		return fmt.Errorf("das/custody-manager: init failed: %w", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.closed {
		return ErrCustodyMgrClosed
	}

	cm.currentEpoch = epoch
	cm.epochStates[epoch] = &CustodyEpochState{
		Epoch:       epoch,
		Groups:      groups,
		Columns:     columns,
		ActivatedAt: time.Now(),
		Active:      true,
	}
	return nil
}

// RotateEpoch transitions custody to a new epoch. It computes the new
// assignment, records added/dropped columns, and deactivates old state.
func (cm *CustodyManager) RotateEpoch(newEpoch uint64) (*CustodyRotationEvent, error) {
	if newEpoch == 0 {
		return nil, ErrCustodyMgrEpochZero
	}

	groups, columns, err := cm.computeAssignment(newEpoch)
	if err != nil {
		return nil, fmt.Errorf("das/custody-manager: rotate failed: %w", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.closed {
		return nil, ErrCustodyMgrClosed
	}

	oldEpoch := cm.currentEpoch
	var oldColumns []uint64
	if st, ok := cm.epochStates[oldEpoch]; ok {
		oldColumns = st.Columns
		st.Active = false
		st.DeactivatedAt = time.Now()
	}

	// Compute added and dropped columns.
	oldSet := toSet(oldColumns)
	newSet := toSet(columns)

	var added, dropped []uint64
	for _, c := range columns {
		if !oldSet[c] {
			added = append(added, c)
		}
	}
	for _, c := range oldColumns {
		if !newSet[c] {
			dropped = append(dropped, c)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i] < added[j] })
	sort.Slice(dropped, func(i, j int) bool { return dropped[i] < dropped[j] })

	event := &CustodyRotationEvent{
		FromEpoch:      oldEpoch,
		ToEpoch:        newEpoch,
		AddedColumns:   added,
		DroppedColumns: dropped,
		Timestamp:      time.Now(),
	}

	cm.currentEpoch = newEpoch
	cm.epochStates[newEpoch] = &CustodyEpochState{
		Epoch:       newEpoch,
		Groups:      groups,
		Columns:     columns,
		ActivatedAt: time.Now(),
		Active:      true,
	}

	cm.rotationHistory = append(cm.rotationHistory, event)
	// Keep only the last 64 rotation events.
	if len(cm.rotationHistory) > 64 {
		cm.rotationHistory = cm.rotationHistory[len(cm.rotationHistory)-64:]
	}

	// Expire old data.
	cm.expireOldDataLocked(newEpoch)

	return event, nil
}

// CurrentColumns returns the sorted column indices for the current epoch.
func (cm *CustodyManager) CurrentColumns() []uint64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	st, ok := cm.epochStates[cm.currentEpoch]
	if !ok {
		return nil
	}
	result := make([]uint64, len(st.Columns))
	copy(result, st.Columns)
	return result
}

// CurrentGroups returns the custody groups for the current epoch.
func (cm *CustodyManager) CurrentGroups() []CustodyGroup {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	st, ok := cm.epochStates[cm.currentEpoch]
	if !ok {
		return nil
	}
	result := make([]CustodyGroup, len(st.Groups))
	copy(result, st.Groups)
	return result
}

// CurrentEpoch returns the current epoch number.
func (cm *CustodyManager) CurrentEpoch() uint64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.currentEpoch
}

// IsColumnInCustody returns true if the given column is in the current
// custody set.
func (cm *CustodyManager) IsColumnInCustody(column uint64) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	st, ok := cm.epochStates[cm.currentEpoch]
	if !ok {
		return false
	}
	for _, c := range st.Columns {
		if c == column {
			return true
		}
	}
	return false
}

// RecordColumn records that a column has been received for a given slot.
// The data is hashed and stored. Returns an error if the column is not in
// the custody set or was already recorded.
func (cm *CustodyManager) RecordColumn(epoch, slot, column uint64, data []byte) error {
	if column >= cm.config.NumberOfColumns {
		return fmt.Errorf("%w: %d >= %d", ErrCustodyMgrColumnOOB, column, cm.config.NumberOfColumns)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.closed {
		return ErrCustodyMgrClosed
	}

	key := custodyDataKey{epoch: epoch, slot: slot, column: column}
	if _, exists := cm.columnStore[key]; exists {
		return ErrCustodyMgrAlreadyStored
	}

	// Hash the data for compact storage.
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var hash [32]byte
	copy(hash[:], h.Sum(nil))

	cm.columnStore[key] = hash

	// Update slot completeness tracker.
	cm.updateSlotTrackerLocked(epoch, slot, column)

	return nil
}

// CheckSlotCompleteness checks whether all required custody columns have
// been received for a given slot.
func (cm *CustodyManager) CheckSlotCompleteness(slot uint64) (*SlotCompleteness, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.currentEpoch == 0 {
		return nil, ErrCustodyMgrNotInitialized
	}

	tracker, ok := cm.slotTracker[slot]
	if !ok {
		// Build a fresh completeness check.
		epoch := cm.epochForSlot(slot)
		st, ok := cm.epochStates[epoch]
		if !ok {
			st = cm.epochStates[cm.currentEpoch]
		}
		if st == nil {
			return nil, ErrCustodyMgrNotInitialized
		}

		received := make(map[uint64]bool)
		for _, col := range st.Columns {
			key := custodyDataKey{epoch: epoch, slot: slot, column: col}
			if _, exists := cm.columnStore[key]; exists {
				received[col] = true
			}
		}

		complete := len(received) >= len(st.Columns)
		return &SlotCompleteness{
			Slot:            slot,
			Epoch:           epoch,
			RequiredColumns: st.Columns,
			ReceivedColumns: received,
			Complete:        complete,
			Timestamp:       time.Now(),
		}, nil
	}

	return tracker, nil
}

// ValidateCustodyProofRequest validates that a proof request is well-formed
// and that the requested column is within the node's custody assignment for
// the given epoch.
func (cm *CustodyManager) ValidateCustodyProofRequest(req *CustodyProofRequest) (*CustodyProofResult, error) {
	if req == nil {
		return nil, ErrCustodyMgrProofInvalid
	}
	if req.Epoch == 0 {
		return nil, ErrCustodyMgrEpochZero
	}
	if req.Column >= cm.config.NumberOfColumns {
		return nil, fmt.Errorf("%w: %d >= %d", ErrCustodyMgrColumnOOB, req.Column, cm.config.NumberOfColumns)
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.closed {
		return nil, ErrCustodyMgrClosed
	}

	// Verify the column is in custody for the requested epoch.
	st, ok := cm.epochStates[req.Epoch]
	if !ok {
		// Compute the assignment if we don't have the epoch state cached.
		_, columns, err := cm.computeAssignmentUnlocked(req.Epoch)
		if err != nil {
			return &CustodyProofResult{
				Valid:  false,
				Column: req.Column,
				Epoch:  req.Epoch,
				Reason: fmt.Sprintf("failed to compute assignment: %v", err),
			}, nil
		}
		inSet := false
		for _, c := range columns {
			if c == req.Column {
				inSet = true
				break
			}
		}
		if !inSet {
			return &CustodyProofResult{
				Valid:  false,
				Column: req.Column,
				Epoch:  req.Epoch,
				Reason: "column not in custody set for epoch",
			}, nil
		}
	} else {
		inSet := false
		for _, c := range st.Columns {
			if c == req.Column {
				inSet = true
				break
			}
		}
		if !inSet {
			return &CustodyProofResult{
				Valid:  false,
				Column: req.Column,
				Epoch:  req.Epoch,
				Reason: "column not in custody set for epoch",
			}, nil
		}
	}

	// Check if we have the data stored.
	key := custodyDataKey{epoch: req.Epoch, slot: req.Slot, column: req.Column}
	dataHash, hasData := cm.columnStore[key]
	if !hasData {
		return &CustodyProofResult{
			Valid:  false,
			Column: req.Column,
			Epoch:  req.Epoch,
			Reason: "column data not available",
		}, nil
	}

	// Compute proof hash.
	proofHash := cm.computeProofHash(req.NodeID, req.Epoch, req.Column, dataHash)

	return &CustodyProofResult{
		Valid:     true,
		Column:    req.Column,
		Epoch:     req.Epoch,
		ProofHash: proofHash,
		Reason:    "valid",
	}, nil
}

// RotationHistory returns the recent rotation events (most recent last).
func (cm *CustodyManager) RotationHistory() []*CustodyRotationEvent {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	result := make([]*CustodyRotationEvent, len(cm.rotationHistory))
	copy(result, cm.rotationHistory)
	return result
}

// StoredColumnCount returns the total number of stored column entries.
func (cm *CustodyManager) StoredColumnCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.columnStore)
}

// TrackedSlotCount returns the number of tracked slots.
func (cm *CustodyManager) TrackedSlotCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.slotTracker)
}

// Close shuts down the custody manager.
func (cm *CustodyManager) Close() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.closed = true
}

// --- Internal helpers ---

// computeAssignment derives the custody groups and column indices for an epoch.
func (cm *CustodyManager) computeAssignment(epoch uint64) ([]CustodyGroup, []uint64, error) {
	return cm.computeAssignmentUnlocked(epoch)
}

// computeAssignmentUnlocked is the inner implementation, safe to call without
// the manager lock (it does not access mutable state).
func (cm *CustodyManager) computeAssignmentUnlocked(epoch uint64) ([]CustodyGroup, []uint64, error) {
	epochNodeID := deriveEpochCustodyID(cm.nodeID, epoch)

	groups, err := GetCustodyGroups(epochNodeID, cm.config.CustodyRequirement)
	if err != nil {
		return nil, nil, err
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

	return groups, columns, nil
}

// updateSlotTrackerLocked updates the completeness tracker for a slot.
// Caller must hold cm.mu write lock.
func (cm *CustodyManager) updateSlotTrackerLocked(epoch, slot, column uint64) {
	tracker, ok := cm.slotTracker[slot]
	if !ok {
		// Create a new tracker for this slot.
		st := cm.epochStates[epoch]
		if st == nil {
			st = cm.epochStates[cm.currentEpoch]
		}
		var required []uint64
		if st != nil {
			required = st.Columns
		}

		tracker = &SlotCompleteness{
			Slot:            slot,
			Epoch:           epoch,
			RequiredColumns: required,
			ReceivedColumns: make(map[uint64]bool),
			Timestamp:       time.Now(),
		}

		// Enforce max tracked slots.
		if len(cm.slotTracker) >= cm.config.MaxTrackedSlots {
			cm.evictOldestSlotLocked()
		}
		cm.slotTracker[slot] = tracker
	}

	tracker.ReceivedColumns[column] = true
	tracker.Complete = len(tracker.ReceivedColumns) >= len(tracker.RequiredColumns)
}

// evictOldestSlotLocked removes the slot with the smallest number.
// Caller must hold cm.mu write lock.
func (cm *CustodyManager) evictOldestSlotLocked() {
	var minSlot uint64
	found := false
	for slot := range cm.slotTracker {
		if !found || slot < minSlot {
			minSlot = slot
			found = true
		}
	}
	if found {
		delete(cm.slotTracker, minSlot)
	}
}

// expireOldDataLocked removes column data and epoch states older than
// RetentionEpochs. Caller must hold cm.mu write lock.
func (cm *CustodyManager) expireOldDataLocked(currentEpoch uint64) {
	if currentEpoch <= cm.config.RetentionEpochs {
		return
	}
	cutoff := currentEpoch - cm.config.RetentionEpochs

	for key := range cm.columnStore {
		if key.epoch < cutoff {
			delete(cm.columnStore, key)
		}
	}
	for epoch, st := range cm.epochStates {
		if epoch < cutoff && !st.Active {
			delete(cm.epochStates, epoch)
		}
	}
}

// epochForSlot returns the epoch number for a given slot.
func (cm *CustodyManager) epochForSlot(slot uint64) uint64 {
	if cm.config.SlotsPerEpoch == 0 {
		return 0
	}
	epoch := slot / cm.config.SlotsPerEpoch
	if epoch == 0 {
		epoch = 1
	}
	return epoch
}

// computeProofHash computes a proof hash for custody verification.
func (cm *CustodyManager) computeProofHash(nodeID [32]byte, epoch, column uint64, dataHash [32]byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(nodeID[:])
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], epoch)
	binary.LittleEndian.PutUint64(buf[8:], column)
	h.Write(buf[:])
	h.Write(dataHash[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// deriveEpochCustodyID derives an epoch-specific node ID for custody rotation.
func deriveEpochCustodyID(nodeID [32]byte, epoch uint64) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(nodeID[:])
	var epochBuf [8]byte
	binary.LittleEndian.PutUint64(epochBuf[:], epoch)
	h.Write(epochBuf[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// toSet converts a slice of uint64 to a set (map).
func toSet(vals []uint64) map[uint64]bool {
	m := make(map[uint64]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}
