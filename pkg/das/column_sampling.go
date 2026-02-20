// column_sampling.go implements DAS column-level sampling for PeerDAS validators.
//
// This file provides per-validator column index selection, custody subnet
// assignment tracking, column download state management, sample verification
// against column roots, and column availability scoring for data availability
// determination.
//
// Per EIP-7594, each validator selects columns to sample each slot based on
// a deterministic function of its node ID and the slot number. The validator
// must download and verify these columns to attest to data availability.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/crypto/sha3"
)

// Column sampling errors.
var (
	ErrColSamplingSlotZero       = errors.New("das/colsampling: slot must be > 0")
	ErrColSamplingColumnOOB      = errors.New("das/colsampling: column index out of range")
	ErrColSamplingProofMismatch  = errors.New("das/colsampling: sample proof does not match column root")
	ErrColSamplingNotAssigned    = errors.New("das/colsampling: column not in validator's assignment")
	ErrColSamplingAlreadyTracked = errors.New("das/colsampling: column already tracked for slot")
)

// ColumnSample represents a single downloaded and verified column sample.
type ColumnSample struct {
	// Slot is the beacon slot this sample belongs to.
	Slot uint64

	// ColumnIndex is the column in [0, NumberOfColumns).
	ColumnIndex ColumnIndex

	// Verified is true if the sample was verified against the column root.
	Verified bool

	// DataSize is the byte size of the column data received.
	DataSize int
}

// ColumnAvailability reports per-slot column availability for a validator.
type ColumnAvailability struct {
	// Slot is the beacon slot.
	Slot uint64

	// RequiredColumns is the set of columns the validator must sample.
	RequiredColumns []ColumnIndex

	// DownloadedColumns is the set of columns successfully downloaded.
	DownloadedColumns []ColumnIndex

	// VerifiedColumns is the subset of downloaded columns that passed
	// proof verification.
	VerifiedColumns []ColumnIndex

	// Score is the availability score: verified / required (0.0 to 1.0).
	Score float64

	// Available is true if all required columns are verified.
	Available bool
}

// ColumnSamplerConfig configures the column sampler.
type ColumnSamplerConfig struct {
	// SamplesPerSlot is the number of random columns to sample per slot.
	SamplesPerSlot int

	// NumberOfColumns is the total column count in the extended matrix.
	NumberOfColumns int

	// CustodyGroupCount is how many custody groups this validator serves.
	CustodyGroupCount uint64

	// TrackSlots is how many recent slots to keep tracking state for.
	TrackSlots int
}

// DefaultColumnSamplerConfig returns production defaults from the Fulu spec.
func DefaultColumnSamplerConfig() ColumnSamplerConfig {
	return ColumnSamplerConfig{
		SamplesPerSlot:    SamplesPerSlot,
		NumberOfColumns:   NumberOfColumns,
		CustodyGroupCount: CustodyRequirement,
		TrackSlots:        64,
	}
}

// slotTracker holds per-slot column download and verification state.
type slotTracker struct {
	required   map[ColumnIndex]bool
	downloaded map[ColumnIndex]bool
	verified   map[ColumnIndex]bool
	samples    []ColumnSample
}

// ColumnSampler manages per-validator column sampling across slots. It
// selects which columns to sample, tracks download progress, verifies
// samples against column roots, and computes availability scores.
//
// All public methods are safe for concurrent use.
type ColumnSampler struct {
	mu     sync.RWMutex
	config ColumnSamplerConfig

	// nodeID is the validator's 32-byte identifier for deterministic selection.
	nodeID [32]byte

	// custodyColumns is the pre-computed set of columns this node custodies.
	custodyColumns map[ColumnIndex]bool

	// slots maps slot numbers to their tracking state.
	slots map[uint64]*slotTracker
}

// NewColumnSampler creates a new column sampler for the given validator node ID.
func NewColumnSampler(config ColumnSamplerConfig, nodeID [32]byte) *ColumnSampler {
	if config.SamplesPerSlot <= 0 {
		config.SamplesPerSlot = SamplesPerSlot
	}
	if config.NumberOfColumns <= 0 {
		config.NumberOfColumns = NumberOfColumns
	}
	if config.CustodyGroupCount == 0 {
		config.CustodyGroupCount = CustodyRequirement
	}
	if config.TrackSlots <= 0 {
		config.TrackSlots = 64
	}

	// Pre-compute custody columns.
	custodyCols := make(map[ColumnIndex]bool)
	cols, err := GetCustodyColumns(nodeID, config.CustodyGroupCount)
	if err == nil {
		for _, c := range cols {
			custodyCols[c] = true
		}
	}

	return &ColumnSampler{
		config:         config,
		nodeID:         nodeID,
		custodyColumns: custodyCols,
		slots:          make(map[uint64]*slotTracker),
	}
}

// SelectColumns returns the deterministic set of column indices this validator
// should sample for the given slot. The result is sorted and always the same
// for a given (nodeID, slot) pair.
func (cs *ColumnSampler) SelectColumns(slot uint64) ([]ColumnIndex, error) {
	if slot == 0 {
		return nil, ErrColSamplingSlotZero
	}
	return selectSampleColumns(cs.nodeID, slot, cs.config.SamplesPerSlot, cs.config.NumberOfColumns), nil
}

// CustodyColumns returns the set of columns this validator custodies.
func (cs *ColumnSampler) CustodyColumns() []ColumnIndex {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	cols := make([]ColumnIndex, 0, len(cs.custodyColumns))
	for c := range cs.custodyColumns {
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool { return cols[i] < cols[j] })
	return cols
}

// IsCustodyColumn returns true if the given column is in this node's custody set.
func (cs *ColumnSampler) IsCustodyColumn(col ColumnIndex) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.custodyColumns[col]
}

// CustodySubnet returns the subnet ID for a given column index.
func (cs *ColumnSampler) CustodySubnet(col ColumnIndex) SubnetID {
	return SubnetID(uint64(col) % DataColumnSidecarSubnetCount)
}

// InitSlot initializes tracking for a slot by computing the required columns.
// This should be called at the beginning of each slot.
func (cs *ColumnSampler) InitSlot(slot uint64) error {
	if slot == 0 {
		return ErrColSamplingSlotZero
	}

	cols, err := cs.SelectColumns(slot)
	if err != nil {
		return err
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if _, exists := cs.slots[slot]; exists {
		// Already initialized; no-op.
		return nil
	}

	required := make(map[ColumnIndex]bool, len(cols))
	for _, c := range cols {
		required[c] = true
	}

	cs.slots[slot] = &slotTracker{
		required:   required,
		downloaded: make(map[ColumnIndex]bool),
		verified:   make(map[ColumnIndex]bool),
		samples:    make([]ColumnSample, 0),
	}

	cs.evictOldSlotsLocked(slot)
	return nil
}

// RecordDownload records that a column was successfully downloaded for a slot.
func (cs *ColumnSampler) RecordDownload(slot uint64, col ColumnIndex, dataSize int) error {
	if uint64(col) >= uint64(cs.config.NumberOfColumns) {
		return fmt.Errorf("%w: %d >= %d", ErrColSamplingColumnOOB, col, cs.config.NumberOfColumns)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	tracker := cs.getOrCreateSlotLocked(slot)
	tracker.downloaded[col] = true
	tracker.samples = append(tracker.samples, ColumnSample{
		Slot:        slot,
		ColumnIndex: col,
		Verified:    false,
		DataSize:    dataSize,
	})
	return nil
}

// VerifySample verifies a column sample against an expected column root.
// The root is computed as keccak256(slot || columnIndex || data).
// On success, the column is marked as verified.
func (cs *ColumnSampler) VerifySample(slot uint64, col ColumnIndex, data []byte, expectedRoot [32]byte) error {
	if uint64(col) >= uint64(cs.config.NumberOfColumns) {
		return fmt.Errorf("%w: %d >= %d", ErrColSamplingColumnOOB, col, cs.config.NumberOfColumns)
	}

	// Compute the proof and compare.
	proof := computeColumnRoot(slot, uint64(col), data)
	if proof != expectedRoot {
		return fmt.Errorf("%w: column %d at slot %d", ErrColSamplingProofMismatch, col, slot)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	tracker := cs.getOrCreateSlotLocked(slot)
	tracker.verified[col] = true
	tracker.downloaded[col] = true
	return nil
}

// GetAvailability computes the column availability status for a slot.
func (cs *ColumnSampler) GetAvailability(slot uint64) (*ColumnAvailability, error) {
	if slot == 0 {
		return nil, ErrColSamplingSlotZero
	}

	cols, err := cs.SelectColumns(slot)
	if err != nil {
		return nil, err
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	tracker, exists := cs.slots[slot]
	if !exists {
		return &ColumnAvailability{
			Slot:            slot,
			RequiredColumns: cols,
			Score:           0.0,
			Available:       false,
		}, nil
	}

	var downloaded, verified []ColumnIndex
	for _, c := range cols {
		if tracker.downloaded[c] {
			downloaded = append(downloaded, c)
		}
		if tracker.verified[c] {
			verified = append(verified, c)
		}
	}

	score := 0.0
	if len(cols) > 0 {
		score = float64(len(verified)) / float64(len(cols))
	}

	return &ColumnAvailability{
		Slot:              slot,
		RequiredColumns:   cols,
		DownloadedColumns: downloaded,
		VerifiedColumns:   verified,
		Score:             score,
		Available:         len(verified) == len(cols),
	}, nil
}

// IsAvailable returns true if all required columns for the slot are verified.
func (cs *ColumnSampler) IsAvailable(slot uint64) bool {
	avail, err := cs.GetAvailability(slot)
	if err != nil {
		return false
	}
	return avail.Available
}

// PruneBefore removes tracking state for slots before the given slot.
func (cs *ColumnSampler) PruneBefore(slot uint64) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	pruned := 0
	for s := range cs.slots {
		if s < slot {
			delete(cs.slots, s)
			pruned++
		}
	}
	return pruned
}

// --- internal helpers ---

// getOrCreateSlotLocked returns the tracker for a slot, creating it if needed.
// Caller must hold cs.mu write lock.
func (cs *ColumnSampler) getOrCreateSlotLocked(slot uint64) *slotTracker {
	if t, ok := cs.slots[slot]; ok {
		return t
	}
	t := &slotTracker{
		required:   make(map[ColumnIndex]bool),
		downloaded: make(map[ColumnIndex]bool),
		verified:   make(map[ColumnIndex]bool),
		samples:    make([]ColumnSample, 0),
	}
	cs.slots[slot] = t
	return t
}

// evictOldSlotsLocked removes slots older than TrackSlots from the current.
// Caller must hold cs.mu write lock.
func (cs *ColumnSampler) evictOldSlotsLocked(currentSlot uint64) {
	if len(cs.slots) <= cs.config.TrackSlots {
		return
	}
	cutoff := currentSlot - uint64(cs.config.TrackSlots)
	for s := range cs.slots {
		if s < cutoff {
			delete(cs.slots, s)
		}
	}
}

// selectSampleColumns deterministically selects sample columns for a
// (nodeID, slot) pair using a hash-chain approach.
func selectSampleColumns(nodeID [32]byte, slot uint64, count int, totalColumns int) []ColumnIndex {
	if count <= 0 || totalColumns <= 0 {
		return nil
	}

	// Seed = keccak256(nodeID || slot).
	h := sha3.NewLegacyKeccak256()
	h.Write(nodeID[:])
	var slotBuf [8]byte
	binary.LittleEndian.PutUint64(slotBuf[:], slot)
	h.Write(slotBuf[:])
	seed := h.Sum(nil)

	seen := make(map[ColumnIndex]bool, count)
	result := make([]ColumnIndex, 0, count)

	for counter := uint64(0); len(result) < count; counter++ {
		sh := sha3.NewLegacyKeccak256()
		sh.Write(seed)
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		sh.Write(cBuf[:])
		digest := sh.Sum(nil)

		val := binary.LittleEndian.Uint64(digest[:8])
		col := ColumnIndex(val % uint64(totalColumns))

		if !seen[col] {
			seen[col] = true
			result = append(result, col)
		}
	}

	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// computeColumnRoot computes keccak256(slot || columnIndex || data) as
// a 32-byte root for column verification.
func computeColumnRoot(slot uint64, columnIndex uint64, data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], slot)
	binary.LittleEndian.PutUint64(buf[8:], columnIndex)
	h.Write(buf[:])
	h.Write(data)
	var root [32]byte
	h.Sum(root[:0])
	return root
}
