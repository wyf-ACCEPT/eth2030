// inclusion_monitor.go monitors inclusion list compliance and tracks builder
// behavior per EIP-7805. It records per-slot required vs included items,
// computes compliance rates, and accumulates penalties for non-compliant
// builders.
//
// The monitor is complementary to the ComplianceEngine and ComplianceTracker:
// it focuses on tracking individual inclusion items and builder-level
// compliance across slots, whereas the engine evaluates blocks against ILs
// and the tracker manages validator-level compliance state.
package focil

import (
	"errors"
	"sort"
	"sync"
)

// Monitor-specific errors.
var (
	ErrMonitorSlotNotFound   = errors.New("inclusion_monitor: slot not found")
	ErrMonitorNoBuilders     = errors.New("inclusion_monitor: no builders registered")
	ErrMonitorBuilderUnknown = errors.New("inclusion_monitor: builder not registered for any slot")
)

// MonitorConfig configures the InclusionMonitor.
type MonitorConfig struct {
	// MaxTrackedSlots is the maximum number of slots to retain. Default: 256.
	MaxTrackedSlots uint64

	// ComplianceThreshold is the minimum inclusion rate to be considered
	// compliant (0.0 to 1.0). Default: 0.90.
	ComplianceThreshold float64

	// PenaltyPerMiss is the penalty points assessed per missed inclusion
	// item. Default: 1000.
	PenaltyPerMiss uint64
}

// DefaultMonitorConfig returns a MonitorConfig with production defaults.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		MaxTrackedSlots:     256,
		ComplianceThreshold: 0.90,
		PenaltyPerMiss:      1000,
	}
}

// InclusionItem represents a single transaction that should be included in a
// block per the FOCIL inclusion lists.
type InclusionItem struct {
	// TxHash is the keccak256 hash of the transaction.
	TxHash [32]byte

	// Sender is the 20-byte address of the transaction sender.
	Sender [20]byte

	// GasLimit is the gas limit of the transaction.
	GasLimit uint64

	// Priority is the priority fee or ordering hint.
	Priority uint64
}

// SlotComplianceReport records the compliance evaluation for a single slot.
type SlotComplianceReport struct {
	// Slot is the slot number.
	Slot uint64

	// RequiredCount is the number of items required by the inclusion lists.
	RequiredCount int

	// IncludedCount is the number of required items actually included.
	IncludedCount int

	// MissedCount is the number of required items that were not included.
	MissedCount int

	// ComplianceRate is IncludedCount / RequiredCount (0.0 to 1.0).
	ComplianceRate float64

	// MissedItems lists the items that were required but not included.
	MissedItems []InclusionItem
}

// slotRecord stores the raw data for a slot.
type slotRecord struct {
	slot     uint64
	required []InclusionItem
	included []InclusionItem
}

// builderRecord tracks a builder's slot assignments and compliance.
type builderRecord struct {
	slots []uint64
}

// InclusionMonitor tracks per-slot compliance and per-builder behavior.
// All public methods are safe for concurrent use.
type InclusionMonitor struct {
	mu       sync.RWMutex
	config   MonitorConfig
	slots    map[uint64]*slotRecord
	builders map[[20]byte]*builderRecord
}

// NewInclusionMonitor creates a new InclusionMonitor with the given config.
// Nil or zero-value fields in config are replaced with defaults.
func NewInclusionMonitor(config *MonitorConfig) *InclusionMonitor {
	cfg := DefaultMonitorConfig()
	if config != nil {
		if config.MaxTrackedSlots > 0 {
			cfg.MaxTrackedSlots = config.MaxTrackedSlots
		}
		if config.ComplianceThreshold > 0 && config.ComplianceThreshold <= 1.0 {
			cfg.ComplianceThreshold = config.ComplianceThreshold
		}
		if config.PenaltyPerMiss > 0 {
			cfg.PenaltyPerMiss = config.PenaltyPerMiss
		}
	}
	return &InclusionMonitor{
		config:   cfg,
		slots:    make(map[uint64]*slotRecord),
		builders: make(map[[20]byte]*builderRecord),
	}
}

// RecordSlot records the required and included items for a given slot.
// If the slot already exists, it is overwritten.
func (m *InclusionMonitor) RecordSlot(slot uint64, required []InclusionItem, included []InclusionItem) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Copy slices to avoid external mutation.
	reqCopy := make([]InclusionItem, len(required))
	copy(reqCopy, required)
	incCopy := make([]InclusionItem, len(included))
	copy(incCopy, included)

	m.slots[slot] = &slotRecord{
		slot:     slot,
		required: reqCopy,
		included: incCopy,
	}

	// Prune if over capacity.
	m.pruneExcess()
}

// SlotCompliance computes the compliance report for a single slot.
func (m *InclusionMonitor) SlotCompliance(slot uint64) (*SlotComplianceReport, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, ok := m.slots[slot]
	if !ok {
		return nil, ErrMonitorSlotNotFound
	}

	return m.computeReport(rec), nil
}

// RegisterBuilder associates a builder address with a slot, indicating that
// the builder was the block producer for that slot.
func (m *InclusionMonitor) RegisterBuilder(address [20]byte, slot uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	br, ok := m.builders[address]
	if !ok {
		br = &builderRecord{}
		m.builders[address] = br
	}
	// Avoid duplicate slot entries.
	for _, s := range br.slots {
		if s == slot {
			return
		}
	}
	br.slots = append(br.slots, slot)
}

// BuilderCompliance returns the average compliance rate for a builder across
// all slots it has been assigned to. Returns 0.0 if the builder has no
// recorded slots or is unknown.
func (m *InclusionMonitor) BuilderCompliance(builder [20]byte) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	br, ok := m.builders[builder]
	if !ok || len(br.slots) == 0 {
		return 0.0
	}

	var totalRate float64
	var count int
	for _, slot := range br.slots {
		rec, ok := m.slots[slot]
		if !ok {
			continue
		}
		report := m.computeReport(rec)
		totalRate += report.ComplianceRate
		count++
	}

	if count == 0 {
		return 0.0
	}
	return totalRate / float64(count)
}

// MostCompliant returns the top n most compliant builders, ordered by
// descending compliance rate. If fewer than n builders are registered,
// all are returned.
func (m *InclusionMonitor) MostCompliant(n int) [][20]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.builders) == 0 || n <= 0 {
		return nil
	}

	type builderScore struct {
		addr [20]byte
		rate float64
	}

	scores := make([]builderScore, 0, len(m.builders))
	for addr, br := range m.builders {
		if len(br.slots) == 0 {
			scores = append(scores, builderScore{addr: addr, rate: 0.0})
			continue
		}
		var totalRate float64
		var count int
		for _, slot := range br.slots {
			rec, ok := m.slots[slot]
			if !ok {
				continue
			}
			report := m.computeReport(rec)
			totalRate += report.ComplianceRate
			count++
		}
		rate := 0.0
		if count > 0 {
			rate = totalRate / float64(count)
		}
		scores = append(scores, builderScore{addr: addr, rate: rate})
	}

	sort.Slice(scores, func(i, j int) bool {
		if scores[i].rate != scores[j].rate {
			return scores[i].rate > scores[j].rate
		}
		// Deterministic tie-breaking by address bytes.
		for k := 0; k < 20; k++ {
			if scores[i].addr[k] != scores[j].addr[k] {
				return scores[i].addr[k] < scores[j].addr[k]
			}
		}
		return false
	})

	if n > len(scores) {
		n = len(scores)
	}
	result := make([][20]byte, n)
	for i := 0; i < n; i++ {
		result[i] = scores[i].addr
	}
	return result
}

// PenaltyAccrued returns the total penalty for a builder based on the number
// of missed inclusion items across all assigned slots.
func (m *InclusionMonitor) PenaltyAccrued(builder [20]byte) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	br, ok := m.builders[builder]
	if !ok {
		return 0
	}

	var totalMisses uint64
	for _, slot := range br.slots {
		rec, ok := m.slots[slot]
		if !ok {
			continue
		}
		report := m.computeReport(rec)
		totalMisses += uint64(report.MissedCount)
	}
	return totalMisses * m.config.PenaltyPerMiss
}

// PruneOldSlots removes all slot records with slot numbers strictly less than
// beforeSlot. Returns the number of slots pruned. Builder slot references
// to pruned slots are also cleaned up.
func (m *InclusionMonitor) PruneOldSlots(beforeSlot uint64) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	pruned := 0
	for s := range m.slots {
		if s < beforeSlot {
			delete(m.slots, s)
			pruned++
		}
	}

	// Clean builder slot references.
	for addr, br := range m.builders {
		filtered := br.slots[:0]
		for _, s := range br.slots {
			if s >= beforeSlot {
				filtered = append(filtered, s)
			}
		}
		br.slots = filtered
		if len(br.slots) == 0 {
			delete(m.builders, addr)
		}
	}

	return pruned
}

// SlotCount returns the number of currently tracked slots.
func (m *InclusionMonitor) SlotCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.slots)
}

// BuilderCount returns the number of registered builders.
func (m *InclusionMonitor) BuilderCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.builders)
}

// --- Internal helpers ---

// computeReport builds a SlotComplianceReport from a slot record.
// Must be called with at least a read lock held.
func (m *InclusionMonitor) computeReport(rec *slotRecord) *SlotComplianceReport {
	if len(rec.required) == 0 {
		return &SlotComplianceReport{
			Slot:           rec.slot,
			ComplianceRate: 1.0,
		}
	}

	// Build a set of included tx hashes for fast lookup.
	includedSet := make(map[[32]byte]struct{}, len(rec.included))
	for _, item := range rec.included {
		includedSet[item.TxHash] = struct{}{}
	}

	var missed []InclusionItem
	included := 0
	for _, req := range rec.required {
		if _, ok := includedSet[req.TxHash]; ok {
			included++
		} else {
			missed = append(missed, req)
		}
	}

	rate := float64(included) / float64(len(rec.required))

	return &SlotComplianceReport{
		Slot:           rec.slot,
		RequiredCount:  len(rec.required),
		IncludedCount:  included,
		MissedCount:    len(missed),
		ComplianceRate: rate,
		MissedItems:    missed,
	}
}

// pruneExcess removes the oldest slots if we exceed MaxTrackedSlots.
// Must be called with the write lock held.
func (m *InclusionMonitor) pruneExcess() {
	if uint64(len(m.slots)) <= m.config.MaxTrackedSlots {
		return
	}

	// Collect all slots and sort.
	allSlots := make([]uint64, 0, len(m.slots))
	for s := range m.slots {
		allSlots = append(allSlots, s)
	}
	sort.Slice(allSlots, func(i, j int) bool { return allSlots[i] < allSlots[j] })

	// Remove oldest until within capacity.
	toRemove := uint64(len(allSlots)) - m.config.MaxTrackedSlots
	for i := uint64(0); i < toRemove; i++ {
		delete(m.slots, allSlots[i])
	}
}
