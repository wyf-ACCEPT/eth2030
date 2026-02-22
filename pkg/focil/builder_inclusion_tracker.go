// builder_inclusion_tracker.go implements a per-builder compliance tracker
// that monitors how well builders include FOCIL inclusion list transactions
// over time. Per EIP-7805, builders must include transactions from the merged
// inclusion list or risk attestation penalties.
//
// Unlike ComplianceTracker (which tracks validator-level IL submission compliance)
// and ComplianceEngine (which evaluates individual blocks), the
// BuilderInclusionTracker focuses on longitudinal builder behavior: historical
// compliance rates, non-compliant builder identification, and per-slot
// compliance detail.
//
// The tracker records per-slot what was required vs what was included, and
// provides APIs for querying builder compliance rates, identifying
// non-compliant builders, and pruning old history.
package focil

import (
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// builderSlotRecord stores per-slot inclusion data for a builder.
type builderSlotRecord struct {
	slot      uint64
	builderID string
	included  map[types.Hash]bool
	required  map[types.Hash]bool
}

// SlotComplianceResult contains the compliance evaluation for a specific slot.
type SlotComplianceResult struct {
	// Slot is the evaluated slot number.
	Slot uint64

	// BuilderID is the builder responsible for the slot.
	BuilderID string

	// IncludedCount is how many required transactions were included.
	IncludedCount int

	// RequiredCount is the total required transactions from inclusion lists.
	RequiredCount int

	// MissingTxs lists transaction hashes required but not included.
	MissingTxs []types.Hash

	// CompliancePercent is the inclusion rate as a percentage (0.0 to 100.0).
	CompliancePercent float64
}

// BuilderInclusionTracker monitors builder compliance with FOCIL inclusion
// lists across multiple slots. All public methods are safe for concurrent use.
type BuilderInclusionTracker struct {
	mu sync.RWMutex

	// Per-slot records.
	slotRecords map[uint64]*builderSlotRecord

	// Per-builder: list of slots they were responsible for.
	builderSlots map[string][]uint64
}

// NewBuilderInclusionTracker creates a new builder inclusion tracker.
func NewBuilderInclusionTracker() *BuilderInclusionTracker {
	return &BuilderInclusionTracker{
		slotRecords:  make(map[uint64]*builderSlotRecord),
		builderSlots: make(map[string][]uint64),
	}
}

// RecordSlot records what was included vs what was required for a slot.
// This is the primary data ingestion method. If the slot already has a
// record, it is overwritten.
func (bt *BuilderInclusionTracker) RecordSlot(slot uint64, builderID string, included []types.Hash, required []types.Hash) {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	includedSet := make(map[types.Hash]bool, len(included))
	for _, h := range included {
		includedSet[h] = true
	}
	requiredSet := make(map[types.Hash]bool, len(required))
	for _, h := range required {
		requiredSet[h] = true
	}

	// Check if this slot already has a record to avoid duplicate builder slot entries.
	_, existed := bt.slotRecords[slot]

	bt.slotRecords[slot] = &builderSlotRecord{
		slot:      slot,
		builderID: builderID,
		included:  includedSet,
		required:  requiredSet,
	}

	// Track the builder's slots, avoiding duplicates.
	if !existed {
		bt.builderSlots[builderID] = append(bt.builderSlots[builderID], slot)
	} else {
		// If the builder changed, update the slot assignment.
		bt.rebuildBuilderSlots()
	}
}

// GetComplianceRate returns the historical compliance rate for a builder
// across all recorded slots (0.0 to 1.0). If the builder has no recorded
// slots, returns 0.0. Slots with zero required transactions count as 1.0
// (fully compliant).
func (bt *BuilderInclusionTracker) GetComplianceRate(builderID string) float64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	slots, ok := bt.builderSlots[builderID]
	if !ok || len(slots) == 0 {
		return 0.0
	}

	var totalRate float64
	var count int
	for _, slot := range slots {
		rec, ok := bt.slotRecords[slot]
		if !ok {
			continue
		}
		rate := bt.computeSlotRate(rec)
		totalRate += rate
		count++
	}

	if count == 0 {
		return 0.0
	}
	return totalRate / float64(count)
}

// GetSlotCompliance returns the compliance details for a specific slot.
// Returns nil if the slot is not tracked.
func (bt *BuilderInclusionTracker) GetSlotCompliance(slot uint64) *SlotComplianceResult {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	rec, ok := bt.slotRecords[slot]
	if !ok {
		return nil
	}

	return bt.buildSlotResult(rec)
}

// IsCompliant checks whether a builder's historical compliance rate meets
// the given threshold (0.0 to 1.0). A builder with no history is considered
// non-compliant.
func (bt *BuilderInclusionTracker) IsCompliant(builderID string, threshold float64) bool {
	rate := bt.GetComplianceRate(builderID)
	return rate >= threshold
}

// GetNonCompliantBuilders returns the IDs of all builders whose historical
// compliance rate falls below the given threshold. Results are sorted
// alphabetically for determinism.
func (bt *BuilderInclusionTracker) GetNonCompliantBuilders(threshold float64) []string {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	var nonCompliant []string
	for builderID, slots := range bt.builderSlots {
		if len(slots) == 0 {
			continue
		}
		rate := bt.computeBuilderRate(builderID)
		if rate < threshold {
			nonCompliant = append(nonCompliant, builderID)
		}
	}

	sort.Strings(nonCompliant)
	return nonCompliant
}

// GetMissingTransactions returns the transaction hashes that were required
// but not included at a specific slot. Returns nil if the slot is not tracked.
// Results are sorted for determinism.
func (bt *BuilderInclusionTracker) GetMissingTransactions(slot uint64) []types.Hash {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	rec, ok := bt.slotRecords[slot]
	if !ok {
		return nil
	}

	return bt.computeMissing(rec)
}

// PruneHistory removes all records for slots strictly before the given slot.
// Returns the number of slot records pruned.
func (bt *BuilderInclusionTracker) PruneHistory(beforeSlot uint64) int {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	pruned := 0
	for s := range bt.slotRecords {
		if s < beforeSlot {
			delete(bt.slotRecords, s)
			pruned++
		}
	}

	// Clean up builder slot references.
	for builderID, slots := range bt.builderSlots {
		filtered := slots[:0]
		for _, s := range slots {
			if s >= beforeSlot {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			delete(bt.builderSlots, builderID)
		} else {
			bt.builderSlots[builderID] = filtered
		}
	}

	return pruned
}

// SlotCount returns the number of tracked slots.
func (bt *BuilderInclusionTracker) SlotCount() int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return len(bt.slotRecords)
}

// TrackedBuilderCount returns the number of builders with tracked slots.
func (bt *BuilderInclusionTracker) TrackedBuilderCount() int {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	return len(bt.builderSlots)
}

// GetBuilderSlots returns the list of slots recorded for a builder.
// Returns nil if the builder is not tracked.
func (bt *BuilderInclusionTracker) GetBuilderSlots(builderID string) []uint64 {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	slots, ok := bt.builderSlots[builderID]
	if !ok {
		return nil
	}
	result := make([]uint64, len(slots))
	copy(result, slots)
	return result
}

// GetAllSlotCompliance returns compliance results for all tracked slots,
// sorted by slot number ascending.
func (bt *BuilderInclusionTracker) GetAllSlotCompliance() []*SlotComplianceResult {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	results := make([]*SlotComplianceResult, 0, len(bt.slotRecords))
	for _, rec := range bt.slotRecords {
		results = append(results, bt.buildSlotResult(rec))
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Slot < results[j].Slot
	})
	return results
}

// GetBuilderComplianceHistory returns per-slot compliance results for a
// builder, sorted by slot ascending. Returns nil if the builder is not tracked.
func (bt *BuilderInclusionTracker) GetBuilderComplianceHistory(builderID string) []*SlotComplianceResult {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	slots, ok := bt.builderSlots[builderID]
	if !ok {
		return nil
	}

	var results []*SlotComplianceResult
	for _, slot := range slots {
		rec, ok := bt.slotRecords[slot]
		if !ok {
			continue
		}
		results = append(results, bt.buildSlotResult(rec))
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Slot < results[j].Slot
	})
	return results
}

// --- Internal helpers ---

// computeSlotRate computes the compliance rate for a single slot record.
// Returns 1.0 for slots with no required transactions (vacuously compliant).
// Must be called with at least a read lock held.
func (bt *BuilderInclusionTracker) computeSlotRate(rec *builderSlotRecord) float64 {
	if len(rec.required) == 0 {
		return 1.0
	}

	included := 0
	for h := range rec.required {
		if rec.included[h] {
			included++
		}
	}
	return float64(included) / float64(len(rec.required))
}

// computeBuilderRate computes the average compliance rate for a builder.
// Must be called with at least a read lock held.
func (bt *BuilderInclusionTracker) computeBuilderRate(builderID string) float64 {
	slots, ok := bt.builderSlots[builderID]
	if !ok || len(slots) == 0 {
		return 0.0
	}

	var totalRate float64
	var count int
	for _, slot := range slots {
		rec, ok := bt.slotRecords[slot]
		if !ok {
			continue
		}
		totalRate += bt.computeSlotRate(rec)
		count++
	}

	if count == 0 {
		return 0.0
	}
	return totalRate / float64(count)
}

// computeMissing returns sorted hashes of required but not included txs.
// Must be called with at least a read lock held.
func (bt *BuilderInclusionTracker) computeMissing(rec *builderSlotRecord) []types.Hash {
	var missing []types.Hash
	for h := range rec.required {
		if !rec.included[h] {
			missing = append(missing, h)
		}
	}

	// Sort for determinism.
	sort.Slice(missing, func(i, j int) bool {
		for k := 0; k < types.HashLength; k++ {
			if missing[i][k] != missing[j][k] {
				return missing[i][k] < missing[j][k]
			}
		}
		return false
	})
	return missing
}

// buildSlotResult constructs a SlotComplianceResult from a record.
// Must be called with at least a read lock held.
func (bt *BuilderInclusionTracker) buildSlotResult(rec *builderSlotRecord) *SlotComplianceResult {
	missing := bt.computeMissing(rec)
	rate := bt.computeSlotRate(rec)

	included := len(rec.required) - len(missing)
	return &SlotComplianceResult{
		Slot:              rec.slot,
		BuilderID:         rec.builderID,
		IncludedCount:     included,
		RequiredCount:     len(rec.required),
		MissingTxs:        missing,
		CompliancePercent: rate * 100.0,
	}
}

// rebuildBuilderSlots reconstructs the builderSlots index from slotRecords.
// Must be called with the write lock held.
func (bt *BuilderInclusionTracker) rebuildBuilderSlots() {
	bt.builderSlots = make(map[string][]uint64)
	for slot, rec := range bt.slotRecords {
		bt.builderSlots[rec.builderID] = append(bt.builderSlots[rec.builderID], slot)
	}
}
