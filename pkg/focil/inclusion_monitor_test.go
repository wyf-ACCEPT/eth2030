package focil

import (
	"sync"
	"testing"
)

func makeInclusionItems(n int, startHash byte) []InclusionItem {
	items := make([]InclusionItem, n)
	for i := 0; i < n; i++ {
		items[i] = InclusionItem{
			TxHash:   [32]byte{startHash + byte(i)},
			Sender:   [20]byte{0x01, byte(i)},
			GasLimit: 21000 + uint64(i)*1000,
			Priority: uint64(i + 1),
		}
	}
	return items
}

func TestInclusionMonitorNew(t *testing.T) {
	m := NewInclusionMonitor(nil)
	if m == nil {
		t.Fatal("expected non-nil monitor")
	}
	if m.config.MaxTrackedSlots != 256 {
		t.Errorf("MaxTrackedSlots = %d, want 256", m.config.MaxTrackedSlots)
	}
	if m.config.ComplianceThreshold != 0.90 {
		t.Errorf("ComplianceThreshold = %f, want 0.90", m.config.ComplianceThreshold)
	}
	if m.config.PenaltyPerMiss != 1000 {
		t.Errorf("PenaltyPerMiss = %d, want 1000", m.config.PenaltyPerMiss)
	}
}

func TestInclusionMonitorCustomConfig(t *testing.T) {
	cfg := &MonitorConfig{
		MaxTrackedSlots:     128,
		ComplianceThreshold: 0.80,
		PenaltyPerMiss:      500,
	}
	m := NewInclusionMonitor(cfg)
	if m.config.MaxTrackedSlots != 128 {
		t.Errorf("MaxTrackedSlots = %d, want 128", m.config.MaxTrackedSlots)
	}
	if m.config.ComplianceThreshold != 0.80 {
		t.Errorf("ComplianceThreshold = %f, want 0.80", m.config.ComplianceThreshold)
	}
	if m.config.PenaltyPerMiss != 500 {
		t.Errorf("PenaltyPerMiss = %d, want 500", m.config.PenaltyPerMiss)
	}
}

func TestInclusionMonitorRecordSlot(t *testing.T) {
	m := NewInclusionMonitor(nil)
	required := makeInclusionItems(5, 0x10)
	included := makeInclusionItems(3, 0x10) // first 3 match

	m.RecordSlot(100, required, included)
	if m.SlotCount() != 1 {
		t.Fatalf("slot count = %d, want 1", m.SlotCount())
	}
}

func TestInclusionMonitorSlotCompliance(t *testing.T) {
	m := NewInclusionMonitor(nil)
	required := makeInclusionItems(5, 0x10)
	included := makeInclusionItems(3, 0x10)

	m.RecordSlot(100, required, included)
	report, err := m.SlotCompliance(100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.RequiredCount != 5 {
		t.Errorf("RequiredCount = %d, want 5", report.RequiredCount)
	}
	if report.IncludedCount != 3 {
		t.Errorf("IncludedCount = %d, want 3", report.IncludedCount)
	}
	if report.MissedCount != 2 {
		t.Errorf("MissedCount = %d, want 2", report.MissedCount)
	}
	expectedRate := 3.0 / 5.0
	if report.ComplianceRate != expectedRate {
		t.Errorf("ComplianceRate = %f, want %f", report.ComplianceRate, expectedRate)
	}
}

func TestInclusionMonitorFullCompliance(t *testing.T) {
	m := NewInclusionMonitor(nil)
	required := makeInclusionItems(4, 0x20)
	included := makeInclusionItems(4, 0x20)

	m.RecordSlot(200, required, included)
	report, err := m.SlotCompliance(200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.ComplianceRate != 1.0 {
		t.Errorf("ComplianceRate = %f, want 1.0", report.ComplianceRate)
	}
	if report.MissedCount != 0 {
		t.Errorf("MissedCount = %d, want 0", report.MissedCount)
	}
}

func TestInclusionMonitorPartialCompliance(t *testing.T) {
	m := NewInclusionMonitor(nil)
	required := makeInclusionItems(10, 0x30)
	included := makeInclusionItems(7, 0x30) // first 7 of 10

	m.RecordSlot(300, required, included)
	report, err := m.SlotCompliance(300)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.MissedCount != 3 {
		t.Errorf("MissedCount = %d, want 3", report.MissedCount)
	}
	if len(report.MissedItems) != 3 {
		t.Errorf("len(MissedItems) = %d, want 3", len(report.MissedItems))
	}
}

func TestInclusionMonitorZeroCompliance(t *testing.T) {
	m := NewInclusionMonitor(nil)
	required := makeInclusionItems(5, 0x40)
	// Include items with completely different hashes.
	included := makeInclusionItems(5, 0xA0)

	m.RecordSlot(400, required, included)
	report, err := m.SlotCompliance(400)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.ComplianceRate != 0.0 {
		t.Errorf("ComplianceRate = %f, want 0.0", report.ComplianceRate)
	}
	if report.MissedCount != 5 {
		t.Errorf("MissedCount = %d, want 5", report.MissedCount)
	}
}

func TestInclusionMonitorEmptySlot(t *testing.T) {
	m := NewInclusionMonitor(nil)
	// No required items means vacuous compliance.
	m.RecordSlot(500, nil, nil)
	report, err := m.SlotCompliance(500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.ComplianceRate != 1.0 {
		t.Errorf("ComplianceRate = %f, want 1.0 for empty slot", report.ComplianceRate)
	}
}

func TestInclusionMonitorSlotNotFound(t *testing.T) {
	m := NewInclusionMonitor(nil)
	_, err := m.SlotCompliance(999)
	if err != ErrMonitorSlotNotFound {
		t.Errorf("expected ErrMonitorSlotNotFound, got %v", err)
	}
}

func TestInclusionMonitorRegisterBuilder(t *testing.T) {
	m := NewInclusionMonitor(nil)
	addr := [20]byte{0xAA, 0xBB}
	m.RegisterBuilder(addr, 100)
	m.RegisterBuilder(addr, 200)
	// Register same slot again (no duplicate).
	m.RegisterBuilder(addr, 100)

	if m.BuilderCount() != 1 {
		t.Errorf("builder count = %d, want 1", m.BuilderCount())
	}
}

func TestInclusionMonitorBuilderCompliance(t *testing.T) {
	m := NewInclusionMonitor(nil)
	builder := [20]byte{0xCC}

	// Slot 100: 100% compliance.
	required1 := makeInclusionItems(4, 0x10)
	included1 := makeInclusionItems(4, 0x10)
	m.RecordSlot(100, required1, included1)
	m.RegisterBuilder(builder, 100)

	// Slot 200: 50% compliance (2 of 4).
	required2 := makeInclusionItems(4, 0x20)
	included2 := makeInclusionItems(2, 0x20)
	m.RecordSlot(200, required2, included2)
	m.RegisterBuilder(builder, 200)

	rate := m.BuilderCompliance(builder)
	expected := (1.0 + 0.5) / 2.0 // 0.75
	if rate != expected {
		t.Errorf("BuilderCompliance = %f, want %f", rate, expected)
	}
}

func TestInclusionMonitorBuilderComplianceUnknown(t *testing.T) {
	m := NewInclusionMonitor(nil)
	unknown := [20]byte{0xFF}
	rate := m.BuilderCompliance(unknown)
	if rate != 0.0 {
		t.Errorf("expected 0.0 for unknown builder, got %f", rate)
	}
}

func TestInclusionMonitorMostCompliant(t *testing.T) {
	m := NewInclusionMonitor(nil)

	builderA := [20]byte{0x01} // 100% compliant
	builderB := [20]byte{0x02} // 50% compliant
	builderC := [20]byte{0x03} // 75% compliant

	// Builder A: 100%.
	reqA := makeInclusionItems(4, 0x10)
	incA := makeInclusionItems(4, 0x10)
	m.RecordSlot(100, reqA, incA)
	m.RegisterBuilder(builderA, 100)

	// Builder B: 50%.
	reqB := makeInclusionItems(4, 0x20)
	incB := makeInclusionItems(2, 0x20)
	m.RecordSlot(200, reqB, incB)
	m.RegisterBuilder(builderB, 200)

	// Builder C: 75%.
	reqC := makeInclusionItems(4, 0x30)
	incC := makeInclusionItems(3, 0x30)
	m.RecordSlot(300, reqC, incC)
	m.RegisterBuilder(builderC, 300)

	top := m.MostCompliant(2)
	if len(top) != 2 {
		t.Fatalf("len(MostCompliant) = %d, want 2", len(top))
	}
	if top[0] != builderA {
		t.Errorf("top[0] = %x, want %x", top[0], builderA)
	}
	if top[1] != builderC {
		t.Errorf("top[1] = %x, want %x", top[1], builderC)
	}
}

func TestInclusionMonitorMostCompliantLargeN(t *testing.T) {
	m := NewInclusionMonitor(nil)
	builderA := [20]byte{0x01}
	m.RecordSlot(100, makeInclusionItems(2, 0x10), makeInclusionItems(2, 0x10))
	m.RegisterBuilder(builderA, 100)

	// Request more builders than exist.
	top := m.MostCompliant(10)
	if len(top) != 1 {
		t.Errorf("len(MostCompliant) = %d, want 1", len(top))
	}
}

func TestInclusionMonitorPenaltyAccrued(t *testing.T) {
	cfg := &MonitorConfig{PenaltyPerMiss: 500}
	m := NewInclusionMonitor(cfg)
	builder := [20]byte{0xDD}

	// 3 required, 1 included => 2 misses.
	required := makeInclusionItems(3, 0x50)
	included := makeInclusionItems(1, 0x50)
	m.RecordSlot(100, required, included)
	m.RegisterBuilder(builder, 100)

	penalty := m.PenaltyAccrued(builder)
	expected := uint64(2) * 500
	if penalty != expected {
		t.Errorf("PenaltyAccrued = %d, want %d", penalty, expected)
	}
}

func TestInclusionMonitorPenaltyAccruedMultipleSlots(t *testing.T) {
	m := NewInclusionMonitor(nil)
	builder := [20]byte{0xEE}

	// Slot 100: 2 misses.
	m.RecordSlot(100, makeInclusionItems(5, 0x10), makeInclusionItems(3, 0x10))
	m.RegisterBuilder(builder, 100)

	// Slot 200: 1 miss.
	m.RecordSlot(200, makeInclusionItems(3, 0x20), makeInclusionItems(2, 0x20))
	m.RegisterBuilder(builder, 200)

	penalty := m.PenaltyAccrued(builder)
	expected := uint64(3) * 1000 // 3 total misses * 1000
	if penalty != expected {
		t.Errorf("PenaltyAccrued = %d, want %d", penalty, expected)
	}
}

func TestInclusionMonitorPruneOldSlots(t *testing.T) {
	m := NewInclusionMonitor(nil)
	for i := uint64(1); i <= 10; i++ {
		m.RecordSlot(i, makeInclusionItems(1, byte(i)), makeInclusionItems(1, byte(i)))
	}
	if m.SlotCount() != 10 {
		t.Fatalf("slot count = %d, want 10", m.SlotCount())
	}

	pruned := m.PruneOldSlots(6)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}
	if m.SlotCount() != 5 {
		t.Errorf("slot count after prune = %d, want 5", m.SlotCount())
	}

	// Slots 1-5 should be gone.
	for i := uint64(1); i <= 5; i++ {
		if _, err := m.SlotCompliance(i); err != ErrMonitorSlotNotFound {
			t.Errorf("slot %d should be pruned", i)
		}
	}
	// Slots 6-10 should remain.
	for i := uint64(6); i <= 10; i++ {
		if _, err := m.SlotCompliance(i); err != nil {
			t.Errorf("slot %d should exist: %v", i, err)
		}
	}
}

func TestInclusionMonitorPruneOldSlotsBuilderCleanup(t *testing.T) {
	m := NewInclusionMonitor(nil)
	builder := [20]byte{0xAA}

	m.RecordSlot(10, makeInclusionItems(1, 0x10), nil)
	m.RecordSlot(20, makeInclusionItems(1, 0x20), nil)
	m.RegisterBuilder(builder, 10)
	m.RegisterBuilder(builder, 20)

	m.PruneOldSlots(15) // removes slot 10

	// Builder should still exist but only with slot 20.
	if m.BuilderCount() != 1 {
		t.Errorf("builder count = %d, want 1", m.BuilderCount())
	}

	// Prune remaining.
	m.PruneOldSlots(25)
	if m.BuilderCount() != 0 {
		t.Errorf("builder count after full prune = %d, want 0", m.BuilderCount())
	}
}

func TestInclusionMonitorMaxTrackedSlots(t *testing.T) {
	cfg := &MonitorConfig{MaxTrackedSlots: 5}
	m := NewInclusionMonitor(cfg)

	for i := uint64(1); i <= 10; i++ {
		m.RecordSlot(i, makeInclusionItems(1, byte(i)), nil)
	}

	if m.SlotCount() > 5 {
		t.Errorf("slot count = %d, should be <= 5", m.SlotCount())
	}
}

func TestInclusionMonitorConcurrentAccess(t *testing.T) {
	m := NewInclusionMonitor(nil)
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			slot := uint64(idx + 1)
			required := makeInclusionItems(3, byte(idx))
			included := makeInclusionItems(2, byte(idx))
			m.RecordSlot(slot, required, included)

			builder := [20]byte{byte(idx)}
			m.RegisterBuilder(builder, slot)
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			m.SlotCompliance(uint64(idx + 1))
			builder := [20]byte{byte(idx)}
			m.BuilderCompliance(builder)
			m.PenaltyAccrued(builder)
			m.MostCompliant(5)
			m.SlotCount()
			m.BuilderCount()
		}(i)
	}

	wg.Wait()
}

func TestInclusionMonitorMultipleBuilders(t *testing.T) {
	m := NewInclusionMonitor(nil)

	builders := [5][20]byte{
		{0x01}, {0x02}, {0x03}, {0x04}, {0x05},
	}

	for i, b := range builders {
		slot := uint64(100 + i)
		count := i + 1 // 1, 2, 3, 4, 5 included of 5
		required := makeInclusionItems(5, byte(0x10*i))
		included := makeInclusionItems(count, byte(0x10*i))
		m.RecordSlot(slot, required, included)
		m.RegisterBuilder(b, slot)
	}

	if m.BuilderCount() != 5 {
		t.Errorf("builder count = %d, want 5", m.BuilderCount())
	}

	// Builder 5 (index 4) should have 100% compliance (5/5).
	rate := m.BuilderCompliance(builders[4])
	if rate != 1.0 {
		t.Errorf("builder[4] compliance = %f, want 1.0", rate)
	}

	// Builder 1 (index 0) should have 20% compliance (1/5).
	rate = m.BuilderCompliance(builders[0])
	expected := 1.0 / 5.0
	if rate != expected {
		t.Errorf("builder[0] compliance = %f, want %f", rate, expected)
	}
}

func TestInclusionMonitorPenaltyAccruedUnknownBuilder(t *testing.T) {
	m := NewInclusionMonitor(nil)
	unknown := [20]byte{0xFF}
	penalty := m.PenaltyAccrued(unknown)
	if penalty != 0 {
		t.Errorf("expected 0 penalty for unknown builder, got %d", penalty)
	}
}

func TestInclusionMonitorMostCompliantEmpty(t *testing.T) {
	m := NewInclusionMonitor(nil)
	top := m.MostCompliant(5)
	if len(top) != 0 {
		t.Errorf("expected empty result, got %d", len(top))
	}
}

func TestInclusionMonitorMostCompliantZeroN(t *testing.T) {
	m := NewInclusionMonitor(nil)
	m.RecordSlot(1, makeInclusionItems(1, 0x01), makeInclusionItems(1, 0x01))
	m.RegisterBuilder([20]byte{0x01}, 1)
	top := m.MostCompliant(0)
	if len(top) != 0 {
		t.Errorf("expected empty result for n=0, got %d", len(top))
	}
}

func TestInclusionMonitorRecordSlotOverwrite(t *testing.T) {
	m := NewInclusionMonitor(nil)

	// Record slot with 0 included.
	m.RecordSlot(100, makeInclusionItems(5, 0x10), nil)
	report, _ := m.SlotCompliance(100)
	if report.ComplianceRate != 0.0 {
		t.Errorf("initial rate = %f, want 0.0", report.ComplianceRate)
	}

	// Overwrite with full compliance.
	m.RecordSlot(100, makeInclusionItems(5, 0x10), makeInclusionItems(5, 0x10))
	report, _ = m.SlotCompliance(100)
	if report.ComplianceRate != 1.0 {
		t.Errorf("overwritten rate = %f, want 1.0", report.ComplianceRate)
	}
}
