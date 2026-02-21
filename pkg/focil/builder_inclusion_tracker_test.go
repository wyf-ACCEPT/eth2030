package focil

import (
	"fmt"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// makeTestHashes creates a slice of distinct test hashes.
func makeTestHashes(n int) []types.Hash {
	hashes := make([]types.Hash, n)
	for i := 0; i < n; i++ {
		hashes[i] = types.HexToHash(fmt.Sprintf("0x%02x", i+1))
	}
	return hashes
}

func TestBuilderInclusionTrackerRecordSlot(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(5)
	included := required[:3] // include 3 of 5

	tracker.RecordSlot(100, "builder1", included, required)

	if tracker.SlotCount() != 1 {
		t.Errorf("SlotCount = %d, want 1", tracker.SlotCount())
	}
	if tracker.TrackedBuilderCount() != 1 {
		t.Errorf("TrackedBuilderCount = %d, want 1", tracker.TrackedBuilderCount())
	}
}

func TestBuilderInclusionTrackerGetComplianceRate(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(4)
	included := required[:2] // 50% compliance

	tracker.RecordSlot(100, "builder1", included, required)

	rate := tracker.GetComplianceRate("builder1")
	if rate != 0.5 {
		t.Errorf("GetComplianceRate = %f, want 0.5", rate)
	}
}

func TestBuilderInclusionTrackerGetComplianceRateMultipleSlots(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(10)

	// Slot 100: 3/4 = 75%
	tracker.RecordSlot(100, "builder1", hashes[:3], hashes[:4])

	// Slot 200: 4/4 = 100%
	tracker.RecordSlot(200, "builder1", hashes[:4], hashes[:4])

	// Average: (0.75 + 1.0) / 2 = 0.875
	rate := tracker.GetComplianceRate("builder1")
	if rate != 0.875 {
		t.Errorf("GetComplianceRate = %f, want 0.875", rate)
	}
}

func TestBuilderInclusionTrackerGetComplianceRateUnknown(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	rate := tracker.GetComplianceRate("unknown")
	if rate != 0.0 {
		t.Errorf("GetComplianceRate(unknown) = %f, want 0.0", rate)
	}
}

func TestBuilderInclusionTrackerGetComplianceRateNoRequired(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	// Slot with no required transactions -> vacuously compliant.
	tracker.RecordSlot(100, "builder1", nil, nil)

	rate := tracker.GetComplianceRate("builder1")
	if rate != 1.0 {
		t.Errorf("GetComplianceRate (no required) = %f, want 1.0", rate)
	}
}

func TestBuilderInclusionTrackerGetSlotCompliance(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(5)
	included := required[:2] // 2 of 5 included

	tracker.RecordSlot(100, "builder1", included, required)

	result := tracker.GetSlotCompliance(100)
	if result == nil {
		t.Fatal("GetSlotCompliance returned nil")
	}
	if result.Slot != 100 {
		t.Errorf("Slot = %d, want 100", result.Slot)
	}
	if result.BuilderID != "builder1" {
		t.Errorf("BuilderID = %s, want builder1", result.BuilderID)
	}
	if result.IncludedCount != 2 {
		t.Errorf("IncludedCount = %d, want 2", result.IncludedCount)
	}
	if result.RequiredCount != 5 {
		t.Errorf("RequiredCount = %d, want 5", result.RequiredCount)
	}
	if len(result.MissingTxs) != 3 {
		t.Errorf("MissingTxs len = %d, want 3", len(result.MissingTxs))
	}
	if result.CompliancePercent != 40.0 {
		t.Errorf("CompliancePercent = %f, want 40.0", result.CompliancePercent)
	}
}

func TestBuilderInclusionTrackerGetSlotComplianceNotFound(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	result := tracker.GetSlotCompliance(999)
	if result != nil {
		t.Error("expected nil for non-existent slot")
	}
}

func TestBuilderInclusionTrackerIsCompliant(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(4)
	included := required[:3] // 75% compliance

	tracker.RecordSlot(100, "builder1", included, required)

	if !tracker.IsCompliant("builder1", 0.75) {
		t.Error("expected compliant at 0.75 threshold")
	}
	if tracker.IsCompliant("builder1", 0.80) {
		t.Error("expected non-compliant at 0.80 threshold")
	}
}

func TestBuilderInclusionTrackerIsCompliantUnknown(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	if tracker.IsCompliant("unknown", 0.5) {
		t.Error("unknown builder should not be compliant")
	}
}

func TestBuilderInclusionTrackerGetNonCompliantBuilders(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(10)

	// builder1: 100% compliance.
	tracker.RecordSlot(100, "builder1", hashes[:4], hashes[:4])

	// builder2: 50% compliance.
	tracker.RecordSlot(200, "builder2", hashes[:2], hashes[:4])

	// builder3: 0% compliance.
	tracker.RecordSlot(300, "builder3", nil, hashes[:4])

	nonCompliant := tracker.GetNonCompliantBuilders(0.75)
	if len(nonCompliant) != 2 {
		t.Fatalf("GetNonCompliantBuilders len = %d, want 2", len(nonCompliant))
	}
	// Should be sorted alphabetically.
	if nonCompliant[0] != "builder2" {
		t.Errorf("nonCompliant[0] = %s, want builder2", nonCompliant[0])
	}
	if nonCompliant[1] != "builder3" {
		t.Errorf("nonCompliant[1] = %s, want builder3", nonCompliant[1])
	}
}

func TestBuilderInclusionTrackerGetNonCompliantBuildersAll(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)
	tracker.RecordSlot(100, "builder1", hashes[:4], hashes[:4]) // 100%
	tracker.RecordSlot(200, "builder2", hashes[:4], hashes[:4]) // 100%

	// With threshold 1.0, all should be compliant.
	nonCompliant := tracker.GetNonCompliantBuilders(1.0)
	if len(nonCompliant) != 0 {
		t.Errorf("expected no non-compliant builders, got %d", len(nonCompliant))
	}
}

func TestBuilderInclusionTrackerGetMissingTransactions(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(5)
	included := required[:2] // include first 2

	tracker.RecordSlot(100, "builder1", included, required)

	missing := tracker.GetMissingTransactions(100)
	if len(missing) != 3 {
		t.Fatalf("GetMissingTransactions len = %d, want 3", len(missing))
	}

	// Verify the missing hashes are the ones not included.
	missingSet := make(map[types.Hash]bool)
	for _, h := range missing {
		missingSet[h] = true
	}
	for _, h := range required[2:] {
		if !missingSet[h] {
			t.Errorf("expected hash %s to be in missing set", h.Hex())
		}
	}
}

func TestBuilderInclusionTrackerGetMissingTransactionsNotFound(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	missing := tracker.GetMissingTransactions(999)
	if missing != nil {
		t.Error("expected nil for non-existent slot")
	}
}

func TestBuilderInclusionTrackerGetMissingTransactionsNoneMissing(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(3)
	tracker.RecordSlot(100, "builder1", required, required) // all included

	missing := tracker.GetMissingTransactions(100)
	if len(missing) != 0 {
		t.Errorf("expected 0 missing, got %d", len(missing))
	}
}

func TestBuilderInclusionTrackerPruneHistory(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)

	tracker.RecordSlot(10, "builder1", hashes[:2], hashes)
	tracker.RecordSlot(20, "builder1", hashes[:3], hashes)
	tracker.RecordSlot(30, "builder2", hashes, hashes)

	pruned := tracker.PruneHistory(25)
	if pruned != 2 {
		t.Errorf("PruneHistory = %d, want 2", pruned)
	}

	if tracker.SlotCount() != 1 {
		t.Errorf("SlotCount after prune = %d, want 1", tracker.SlotCount())
	}

	// builder1 should be removed (all slots pruned).
	if slots := tracker.GetBuilderSlots("builder1"); slots != nil {
		t.Errorf("builder1 should have no slots after prune, got %v", slots)
	}

	// builder2 should still have slot 30.
	if slots := tracker.GetBuilderSlots("builder2"); len(slots) != 1 {
		t.Errorf("builder2 slots = %d, want 1", len(slots))
	}
}

func TestBuilderInclusionTrackerGetBuilderSlots(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)
	tracker.RecordSlot(100, "builder1", hashes, hashes)
	tracker.RecordSlot(200, "builder1", hashes, hashes)

	slots := tracker.GetBuilderSlots("builder1")
	if len(slots) != 2 {
		t.Fatalf("GetBuilderSlots len = %d, want 2", len(slots))
	}

	// Verify returned is a copy.
	slots[0] = 999
	original := tracker.GetBuilderSlots("builder1")
	if original[0] == 999 {
		t.Error("GetBuilderSlots should return a copy")
	}
}

func TestBuilderInclusionTrackerGetBuilderSlotsUnknown(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	if tracker.GetBuilderSlots("unknown") != nil {
		t.Error("expected nil for unknown builder")
	}
}

func TestBuilderInclusionTrackerGetAllSlotCompliance(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)
	tracker.RecordSlot(300, "builder1", hashes, hashes)
	tracker.RecordSlot(100, "builder2", hashes[:2], hashes)
	tracker.RecordSlot(200, "builder1", hashes[:3], hashes)

	results := tracker.GetAllSlotCompliance()
	if len(results) != 3 {
		t.Fatalf("GetAllSlotCompliance len = %d, want 3", len(results))
	}

	// Should be sorted by slot ascending.
	if results[0].Slot != 100 {
		t.Errorf("results[0].Slot = %d, want 100", results[0].Slot)
	}
	if results[1].Slot != 200 {
		t.Errorf("results[1].Slot = %d, want 200", results[1].Slot)
	}
	if results[2].Slot != 300 {
		t.Errorf("results[2].Slot = %d, want 300", results[2].Slot)
	}
}

func TestBuilderInclusionTrackerGetBuilderComplianceHistory(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)
	tracker.RecordSlot(200, "builder1", hashes[:2], hashes) // 50%
	tracker.RecordSlot(100, "builder1", hashes[:3], hashes) // 75%

	history := tracker.GetBuilderComplianceHistory("builder1")
	if len(history) != 2 {
		t.Fatalf("history len = %d, want 2", len(history))
	}

	// Should be sorted by slot ascending.
	if history[0].Slot != 100 {
		t.Errorf("history[0].Slot = %d, want 100", history[0].Slot)
	}
	if history[0].CompliancePercent != 75.0 {
		t.Errorf("history[0].CompliancePercent = %f, want 75.0", history[0].CompliancePercent)
	}
	if history[1].Slot != 200 {
		t.Errorf("history[1].Slot = %d, want 200", history[1].Slot)
	}
	if history[1].CompliancePercent != 50.0 {
		t.Errorf("history[1].CompliancePercent = %f, want 50.0", history[1].CompliancePercent)
	}
}

func TestBuilderInclusionTrackerGetBuilderComplianceHistoryUnknown(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	if tracker.GetBuilderComplianceHistory("unknown") != nil {
		t.Error("expected nil for unknown builder")
	}
}

func TestBuilderInclusionTrackerRecordSlotOverwrite(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)

	// Record first time.
	tracker.RecordSlot(100, "builder1", hashes[:1], hashes) // 25%

	// Overwrite with better compliance.
	tracker.RecordSlot(100, "builder1", hashes, hashes) // 100%

	result := tracker.GetSlotCompliance(100)
	if result.CompliancePercent != 100.0 {
		t.Errorf("CompliancePercent after overwrite = %f, want 100.0", result.CompliancePercent)
	}

	// Should still have 1 slot count.
	if tracker.SlotCount() != 1 {
		t.Errorf("SlotCount = %d, want 1", tracker.SlotCount())
	}
}

func TestBuilderInclusionTrackerRecordSlotDifferentBuilder(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(4)

	// Record slot 100 for builder1.
	tracker.RecordSlot(100, "builder1", hashes[:2], hashes)

	// Overwrite slot 100 for builder2.
	tracker.RecordSlot(100, "builder2", hashes, hashes)

	// builder2 should own slot 100.
	result := tracker.GetSlotCompliance(100)
	if result.BuilderID != "builder2" {
		t.Errorf("BuilderID = %s, want builder2", result.BuilderID)
	}

	// builder1 should have no slots.
	if slots := tracker.GetBuilderSlots("builder1"); len(slots) != 0 {
		t.Errorf("builder1 should have 0 slots after reassignment, got %d", len(slots))
	}
}

func TestBuilderInclusionTrackerConcurrentAccess(t *testing.T) {
	tracker := NewBuilderInclusionTracker()
	hashes := makeTestHashes(4)
	done := make(chan struct{})

	// Concurrent record + read.
	for i := 0; i < 10; i++ {
		go func(slot uint64) {
			tracker.RecordSlot(slot, "builder1", hashes, hashes)
			_ = tracker.GetComplianceRate("builder1")
			_ = tracker.GetSlotCompliance(slot)
			_ = tracker.GetMissingTransactions(slot)
			done <- struct{}{}
		}(uint64(i + 1))
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	if tracker.SlotCount() != 10 {
		t.Errorf("SlotCount = %d, want 10", tracker.SlotCount())
	}
}

func TestBuilderInclusionTrackerPerfectCompliance(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	hashes := makeTestHashes(10)

	// All transactions included across multiple slots.
	for i := uint64(0); i < 5; i++ {
		tracker.RecordSlot(i+1, "builder1", hashes, hashes)
	}

	rate := tracker.GetComplianceRate("builder1")
	if rate != 1.0 {
		t.Errorf("GetComplianceRate = %f, want 1.0", rate)
	}

	if !tracker.IsCompliant("builder1", 1.0) {
		t.Error("builder with 100% compliance should be compliant at threshold 1.0")
	}
}

func TestBuilderInclusionTrackerZeroCompliance(t *testing.T) {
	tracker := NewBuilderInclusionTracker()

	required := makeTestHashes(5)
	// Include nothing.
	tracker.RecordSlot(100, "builder1", nil, required)

	rate := tracker.GetComplianceRate("builder1")
	if rate != 0.0 {
		t.Errorf("GetComplianceRate = %f, want 0.0", rate)
	}

	result := tracker.GetSlotCompliance(100)
	if result.IncludedCount != 0 {
		t.Errorf("IncludedCount = %d, want 0", result.IncludedCount)
	}
	if len(result.MissingTxs) != 5 {
		t.Errorf("MissingTxs len = %d, want 5", len(result.MissingTxs))
	}
}
