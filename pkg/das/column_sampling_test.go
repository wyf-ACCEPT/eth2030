package das

import (
	"testing"
)

// testNodeID returns a deterministic 32-byte node ID from a seed byte.
func testNodeID(seed byte) [32]byte {
	var id [32]byte
	id[0] = seed
	return id
}

func TestNewColumnSampler(t *testing.T) {
	nodeID := testNodeID(1)
	cfg := DefaultColumnSamplerConfig()
	cs := NewColumnSampler(cfg, nodeID)
	if cs == nil {
		t.Fatal("NewColumnSampler returned nil")
	}
	// Custody columns should be pre-computed.
	cols := cs.CustodyColumns()
	if len(cols) == 0 {
		t.Fatal("custody columns should not be empty")
	}
}

func TestNewColumnSampler_DefaultsApplied(t *testing.T) {
	nodeID := testNodeID(2)
	// Pass zero-valued config; defaults should be applied.
	cs := NewColumnSampler(ColumnSamplerConfig{}, nodeID)
	if cs.config.SamplesPerSlot != SamplesPerSlot {
		t.Errorf("SamplesPerSlot = %d, want %d", cs.config.SamplesPerSlot, SamplesPerSlot)
	}
	if cs.config.NumberOfColumns != NumberOfColumns {
		t.Errorf("NumberOfColumns = %d, want %d", cs.config.NumberOfColumns, NumberOfColumns)
	}
	if cs.config.CustodyGroupCount != CustodyRequirement {
		t.Errorf("CustodyGroupCount = %d, want %d", cs.config.CustodyGroupCount, CustodyRequirement)
	}
	if cs.config.TrackSlots != 64 {
		t.Errorf("TrackSlots = %d, want 64", cs.config.TrackSlots)
	}
}

func TestSelectColumns_Deterministic(t *testing.T) {
	nodeID := testNodeID(3)
	cfg := DefaultColumnSamplerConfig()
	cs := NewColumnSampler(cfg, nodeID)

	cols1, err := cs.SelectColumns(1)
	if err != nil {
		t.Fatalf("SelectColumns: %v", err)
	}
	cols2, err := cs.SelectColumns(1)
	if err != nil {
		t.Fatalf("SelectColumns: %v", err)
	}

	if len(cols1) != len(cols2) {
		t.Fatalf("lengths differ: %d vs %d", len(cols1), len(cols2))
	}
	for i := range cols1 {
		if cols1[i] != cols2[i] {
			t.Errorf("column[%d]: %d != %d", i, cols1[i], cols2[i])
		}
	}
}

func TestSelectColumns_SlotZero(t *testing.T) {
	nodeID := testNodeID(4)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)

	_, err := cs.SelectColumns(0)
	if err != ErrColSamplingSlotZero {
		t.Errorf("expected ErrColSamplingSlotZero, got %v", err)
	}
}

func TestSelectColumns_DifferentSlots(t *testing.T) {
	nodeID := testNodeID(5)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)

	cols1, _ := cs.SelectColumns(1)
	cols2, _ := cs.SelectColumns(2)

	// Different slots should (very likely) produce different columns.
	same := true
	if len(cols1) == len(cols2) {
		for i := range cols1 {
			if cols1[i] != cols2[i] {
				same = false
				break
			}
		}
	} else {
		same = false
	}
	if same {
		t.Error("different slots produced identical column sets")
	}
}

func TestSelectColumns_Sorted(t *testing.T) {
	nodeID := testNodeID(6)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cols, err := cs.SelectColumns(42)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(cols); i++ {
		if cols[i] <= cols[i-1] {
			t.Fatalf("columns not sorted: index %d (%d) <= index %d (%d)", i, cols[i], i-1, cols[i-1])
		}
	}
}

func TestSelectColumns_Count(t *testing.T) {
	nodeID := testNodeID(7)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cols, err := cs.SelectColumns(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != SamplesPerSlot {
		t.Errorf("got %d columns, want %d", len(cols), SamplesPerSlot)
	}
}

func TestCustodyColumns_Sorted(t *testing.T) {
	nodeID := testNodeID(8)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cols := cs.CustodyColumns()
	for i := 1; i < len(cols); i++ {
		if cols[i] <= cols[i-1] {
			t.Fatalf("custody columns not sorted at index %d", i)
		}
	}
}

func TestIsCustodyColumn(t *testing.T) {
	nodeID := testNodeID(9)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cols := cs.CustodyColumns()
	if len(cols) == 0 {
		t.Fatal("no custody columns")
	}
	// A custody column should be recognized.
	if !cs.IsCustodyColumn(cols[0]) {
		t.Errorf("IsCustodyColumn(%d) = false, want true", cols[0])
	}
}

func TestCustodySubnet(t *testing.T) {
	nodeID := testNodeID(10)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	sub := cs.CustodySubnet(ColumnIndex(65))
	expected := SubnetID(65 % DataColumnSidecarSubnetCount)
	if sub != expected {
		t.Errorf("CustodySubnet(65) = %d, want %d", sub, expected)
	}
}

func TestInitSlot_SlotZero(t *testing.T) {
	nodeID := testNodeID(11)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	err := cs.InitSlot(0)
	if err != ErrColSamplingSlotZero {
		t.Errorf("expected ErrColSamplingSlotZero, got %v", err)
	}
}

func TestInitSlot_Idempotent(t *testing.T) {
	nodeID := testNodeID(12)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	if err := cs.InitSlot(5); err != nil {
		t.Fatal(err)
	}
	// Second call should be a no-op.
	if err := cs.InitSlot(5); err != nil {
		t.Fatal(err)
	}
}

func TestRecordDownload_ColumnOOB(t *testing.T) {
	nodeID := testNodeID(13)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	err := cs.RecordDownload(1, ColumnIndex(NumberOfColumns), 100)
	if err == nil {
		t.Fatal("expected error for out-of-bound column")
	}
}

func TestRecordDownload_Success(t *testing.T) {
	nodeID := testNodeID(14)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cs.InitSlot(1)

	cols, _ := cs.SelectColumns(1)
	if len(cols) == 0 {
		t.Fatal("no columns selected")
	}
	err := cs.RecordDownload(1, cols[0], 2048)
	if err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}
}

func TestVerifySample_ColumnOOB(t *testing.T) {
	nodeID := testNodeID(15)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	err := cs.VerifySample(1, ColumnIndex(NumberOfColumns), nil, [32]byte{})
	if err == nil {
		t.Fatal("expected error for out-of-bound column")
	}
}

func TestVerifySample_ProofMismatch(t *testing.T) {
	nodeID := testNodeID(16)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cs.InitSlot(1)

	data := []byte("test-data")
	var badRoot [32]byte
	err := cs.VerifySample(1, 0, data, badRoot)
	if err == nil {
		t.Fatal("expected proof mismatch error")
	}
}

func TestVerifySample_Success(t *testing.T) {
	nodeID := testNodeID(17)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cs.InitSlot(1)

	data := []byte("column-data-payload")
	root := computeColumnRoot(1, 0, data)

	err := cs.VerifySample(1, 0, data, root)
	if err != nil {
		t.Fatalf("VerifySample: %v", err)
	}
}

func TestGetAvailability_SlotZero(t *testing.T) {
	nodeID := testNodeID(18)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	_, err := cs.GetAvailability(0)
	if err != ErrColSamplingSlotZero {
		t.Errorf("expected ErrColSamplingSlotZero, got %v", err)
	}
}

func TestGetAvailability_Uninitialized(t *testing.T) {
	nodeID := testNodeID(19)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)

	avail, err := cs.GetAvailability(1)
	if err != nil {
		t.Fatal(err)
	}
	if avail.Available {
		t.Error("should not be available for uninitialized slot")
	}
	if avail.Score != 0.0 {
		t.Errorf("score = %f, want 0.0", avail.Score)
	}
}

func TestGetAvailability_FullAvailability(t *testing.T) {
	nodeID := testNodeID(20)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cs.InitSlot(1)

	cols, _ := cs.SelectColumns(1)
	for _, col := range cols {
		data := []byte("payload")
		root := computeColumnRoot(1, uint64(col), data)
		if err := cs.VerifySample(1, col, data, root); err != nil {
			t.Fatalf("VerifySample(%d): %v", col, err)
		}
	}

	avail, err := cs.GetAvailability(1)
	if err != nil {
		t.Fatal(err)
	}
	if !avail.Available {
		t.Error("expected Available = true")
	}
	if avail.Score != 1.0 {
		t.Errorf("score = %f, want 1.0", avail.Score)
	}
	if len(avail.VerifiedColumns) != len(cols) {
		t.Errorf("verified = %d, want %d", len(avail.VerifiedColumns), len(cols))
	}
}

func TestIsAvailable(t *testing.T) {
	nodeID := testNodeID(21)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)
	cs.InitSlot(1)

	// Before verification, not available.
	if cs.IsAvailable(1) {
		t.Error("should not be available before verification")
	}

	// Verify all required columns.
	cols, _ := cs.SelectColumns(1)
	for _, col := range cols {
		data := []byte("data")
		root := computeColumnRoot(1, uint64(col), data)
		cs.VerifySample(1, col, data, root)
	}

	if !cs.IsAvailable(1) {
		t.Error("should be available after all verifications")
	}
}

func TestPruneBefore(t *testing.T) {
	nodeID := testNodeID(22)
	cs := NewColumnSampler(DefaultColumnSamplerConfig(), nodeID)

	for slot := uint64(1); slot <= 10; slot++ {
		cs.InitSlot(slot)
	}

	pruned := cs.PruneBefore(6)
	if pruned != 5 {
		t.Errorf("pruned = %d, want 5", pruned)
	}

	// Slots 1-5 should be gone; slot 6 should remain.
	avail, _ := cs.GetAvailability(6)
	if avail == nil {
		t.Fatal("slot 6 should still exist")
	}
}

func TestEvictOldSlots(t *testing.T) {
	nodeID := testNodeID(23)
	cfg := DefaultColumnSamplerConfig()
	cfg.TrackSlots = 4
	cs := NewColumnSampler(cfg, nodeID)

	// Init slots 1-10; eviction runs when len(slots) > TrackSlots,
	// removing slots older than (currentSlot - TrackSlots). After
	// adding slot 10, anything before slot 6 is evicted, leaving
	// at most TrackSlots + 1 entries (the trigger condition is >).
	for slot := uint64(1); slot <= 10; slot++ {
		cs.InitSlot(slot)
	}

	cs.mu.RLock()
	count := len(cs.slots)
	cs.mu.RUnlock()

	// The eviction keeps slots >= (currentSlot - TrackSlots) = 6,
	// so slots 6,7,8,9,10 = 5 slots. The count should not exceed
	// TrackSlots + 1 since eviction fires when > TrackSlots.
	if count > cfg.TrackSlots+1 {
		t.Errorf("expected at most %d tracked slots, got %d", cfg.TrackSlots+1, count)
	}
}
