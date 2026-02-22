package das

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestDefaultDAValidatorConfig(t *testing.T) {
	cfg := DefaultDAValidatorConfig()
	if cfg.MinCustodyGroups != CustodyRequirement {
		t.Errorf("MinCustodyGroups = %d, want %d", cfg.MinCustodyGroups, CustodyRequirement)
	}
	if cfg.ColumnCount != NumberOfColumns {
		t.Errorf("ColumnCount = %d, want %d", cfg.ColumnCount, NumberOfColumns)
	}
	if cfg.SamplesPerSlot != SamplesPerSlot {
		t.Errorf("SamplesPerSlot = %d, want %d", cfg.SamplesPerSlot, SamplesPerSlot)
	}
	if cfg.MaxBlobsPerBlock != int(MaxBlobCommitmentsPerBlock) {
		t.Errorf("MaxBlobsPerBlock = %d, want %d", cfg.MaxBlobsPerBlock, MaxBlobCommitmentsPerBlock)
	}
}

func TestNewDAValidator(t *testing.T) {
	cfg := DefaultDAValidatorConfig()
	v := NewDAValidator(cfg)
	if v == nil {
		t.Fatal("NewDAValidator returned nil")
	}
}

func TestComputeCustodyColumns(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01, 0x02, 0x03}

	columns := v.ComputeCustodyColumns(nodeID, CustodyRequirement)
	if len(columns) != CustodyRequirement {
		t.Fatalf("expected %d columns, got %d", CustodyRequirement, len(columns))
	}

	// Verify columns are sorted.
	for i := 1; i < len(columns); i++ {
		if columns[i] <= columns[i-1] {
			t.Errorf("columns not sorted: columns[%d]=%d <= columns[%d]=%d",
				i, columns[i], i-1, columns[i-1])
		}
	}

	// Verify all columns are in valid range.
	for _, c := range columns {
		if c >= uint64(NumberOfColumns) {
			t.Errorf("column %d out of range [0, %d)", c, NumberOfColumns)
		}
	}

	// Verify uniqueness.
	seen := make(map[uint64]bool)
	for _, c := range columns {
		if seen[c] {
			t.Errorf("duplicate column %d", c)
		}
		seen[c] = true
	}
}

func TestComputeCustodyColumnsDeterministic(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0xaa, 0xbb}

	cols1 := v.ComputeCustodyColumns(nodeID, CustodyRequirement)
	cols2 := v.ComputeCustodyColumns(nodeID, CustodyRequirement)

	if len(cols1) != len(cols2) {
		t.Fatal("non-deterministic result lengths")
	}
	for i := range cols1 {
		if cols1[i] != cols2[i] {
			t.Fatalf("non-deterministic: cols1[%d]=%d != cols2[%d]=%d",
				i, cols1[i], i, cols2[i])
		}
	}
}

func TestComputeCustodyColumnsZeroGroups(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01}

	columns := v.ComputeCustodyColumns(nodeID, 0)
	if len(columns) != 0 {
		t.Errorf("expected 0 columns for 0 groups, got %d", len(columns))
	}
}

func TestComputeCustodyColumnsExcessGroups(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01}

	// Requesting more groups than exist should be capped.
	columns := v.ComputeCustodyColumns(nodeID, NumberOfCustodyGroups+10)
	if len(columns) != NumberOfColumns {
		t.Errorf("expected %d columns for max groups, got %d", NumberOfColumns, len(columns))
	}
}

func TestValidateColumnSidecarValid(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	slot := uint64(100)
	colIdx := uint64(42)
	data := []byte("test column data for sidecar")

	proof := ComputeColumnProof(slot, colIdx, data)

	err := v.ValidateColumnSidecar(slot, colIdx, data, proof)
	if err != nil {
		t.Fatalf("valid sidecar failed: %v", err)
	}
}

func TestValidateColumnSidecarInvalidIndex(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	data := []byte("test data")
	proof := []byte("test proof")

	err := v.ValidateColumnSidecar(0, 128, data, proof)
	if err == nil {
		t.Fatal("expected error for column index 128")
	}
}

func TestValidateColumnSidecarEmptyData(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())

	err := v.ValidateColumnSidecar(0, 0, nil, []byte("proof"))
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestValidateColumnSidecarEmptyProof(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())

	err := v.ValidateColumnSidecar(0, 0, []byte("data"), nil)
	if err == nil {
		t.Fatal("expected error for empty proof")
	}
}

func TestValidateColumnSidecarBadProof(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	data := []byte("test data")
	badProof := make([]byte, 32) // wrong proof

	err := v.ValidateColumnSidecar(0, 0, data, badProof)
	if err == nil {
		t.Fatal("expected error for bad proof")
	}
}

func TestValidateCustodyAssignmentValid(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01, 0x02, 0x03}

	// Get the correct custody columns.
	columns := v.ComputeCustodyColumns(nodeID, CustodyRequirement)

	err := v.ValidateCustodyAssignment(nodeID, columns)
	if err != nil {
		t.Fatalf("valid custody assignment failed: %v", err)
	}
}

func TestValidateCustodyAssignmentWrongColumn(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01, 0x02, 0x03}

	// Use columns not assigned to this node.
	columns := v.ComputeCustodyColumns(nodeID, CustodyRequirement)

	// Modify one column to an invalid one.
	// Find a column NOT in the set.
	assigned := make(map[uint64]bool)
	for _, c := range columns {
		assigned[c] = true
	}
	wrongCol := uint64(0)
	for assigned[wrongCol] {
		wrongCol++
	}
	wrongColumns := make([]uint64, len(columns))
	copy(wrongColumns, columns)
	wrongColumns[0] = wrongCol

	err := v.ValidateCustodyAssignment(nodeID, wrongColumns)
	if err == nil {
		t.Fatal("expected error for wrong column assignment")
	}
}

func TestValidateCustodyAssignmentTooFew(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01, 0x02, 0x03}

	// Get correct columns but only provide a subset.
	columns := v.ComputeCustodyColumns(nodeID, CustodyRequirement)
	if len(columns) < 2 {
		t.Skip("need at least 2 custody columns")
	}

	err := v.ValidateCustodyAssignment(nodeID, columns[:1])
	if err == nil {
		t.Fatal("expected error for too few custody columns")
	}
}

func TestValidateCustodyAssignmentOutOfRange(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01}

	err := v.ValidateCustodyAssignment(nodeID, []uint64{200})
	if err == nil {
		t.Fatal("expected error for out-of-range column")
	}
}

func TestIsDataAvailable(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())

	available := map[uint64]bool{0: true, 5: true, 10: true, 42: true}
	required := []uint64{0, 5, 10}

	if !v.IsDataAvailable(100, available, required) {
		t.Error("data should be available when all required columns present")
	}
}

func TestIsDataAvailableMissing(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())

	available := map[uint64]bool{0: true, 5: true}
	required := []uint64{0, 5, 10}

	if v.IsDataAvailable(100, available, required) {
		t.Error("data should not be available when required column 10 is missing")
	}
}

func TestIsDataAvailableEmpty(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())

	// No required columns means data is trivially available.
	available := map[uint64]bool{0: true}
	if !v.IsDataAvailable(100, available, nil) {
		t.Error("data should be available when no columns required")
	}
}

func TestSampleColumns(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01, 0x02}

	samples := v.SampleColumns(100, nodeID)
	if len(samples) != SamplesPerSlot {
		t.Fatalf("expected %d samples, got %d", SamplesPerSlot, len(samples))
	}

	// Verify sorted.
	for i := 1; i < len(samples); i++ {
		if samples[i] <= samples[i-1] {
			t.Errorf("samples not sorted at index %d", i)
		}
	}

	// Verify all in range.
	for _, s := range samples {
		if s >= uint64(NumberOfColumns) {
			t.Errorf("sample %d out of range [0, %d)", s, NumberOfColumns)
		}
	}

	// Verify uniqueness.
	seen := make(map[uint64]bool)
	for _, s := range samples {
		if seen[s] {
			t.Errorf("duplicate sample %d", s)
		}
		seen[s] = true
	}
}

func TestSampleColumnsDeterministic(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0xaa}

	s1 := v.SampleColumns(50, nodeID)
	s2 := v.SampleColumns(50, nodeID)

	if len(s1) != len(s2) {
		t.Fatal("non-deterministic sample counts")
	}
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("non-deterministic: s1[%d]=%d != s2[%d]=%d", i, s1[i], i, s2[i])
		}
	}
}

func TestSampleColumnsDifferentSlots(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	nodeID := types.Hash{0x01}

	s1 := v.SampleColumns(100, nodeID)
	s2 := v.SampleColumns(200, nodeID)

	// Different slots should (very likely) produce different samples.
	allSame := true
	for i := range s1 {
		if s1[i] != s2[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Log("warning: different slots produced same samples (unlikely but possible)")
	}
}

func TestSampleColumnsDifferentNodes(t *testing.T) {
	v := NewDAValidator(DefaultDAValidatorConfig())
	n1 := types.Hash{0x01}
	n2 := types.Hash{0x02}

	s1 := v.SampleColumns(100, n1)
	s2 := v.SampleColumns(100, n2)

	allSame := true
	for i := range s1 {
		if s1[i] != s2[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Log("warning: different nodes produced same samples (unlikely but possible)")
	}
}

func TestSampleColumnsZeroConfig(t *testing.T) {
	v := NewDAValidator(DAValidatorConfig{
		SamplesPerSlot: 0,
		ColumnCount:    128,
	})
	samples := v.SampleColumns(0, types.Hash{})
	if len(samples) != 0 {
		t.Errorf("expected 0 samples for zero config, got %d", len(samples))
	}
}

func TestComputeColumnProofConsistency(t *testing.T) {
	slot := uint64(42)
	col := uint64(7)
	data := []byte("hello world")

	p1 := ComputeColumnProof(slot, col, data)
	p2 := ComputeColumnProof(slot, col, data)

	if len(p1) != 32 {
		t.Fatalf("expected 32-byte proof, got %d", len(p1))
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			t.Fatal("proof computation is non-deterministic")
		}
	}

	// Different data should produce different proof.
	p3 := ComputeColumnProof(slot, col, []byte("different data"))
	same := true
	for i := range p1 {
		if p1[i] != p3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different data produced same proof")
	}
}
