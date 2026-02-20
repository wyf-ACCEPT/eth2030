package das

import (
	"testing"
)

// makeTestDataCell creates a test DataCell with a valid proof.
func makeTestDataCell(colIdx, rowIdx uint64) DataCell {
	var data [BytesPerCell]byte
	// Fill with non-zero deterministic content.
	for i := range data {
		data[i] = byte((int(colIdx) + int(rowIdx) + i) % 251)
	}
	// Ensure not all zeros.
	data[0] = 0x42

	proof := ComputeCellProof(colIdx, rowIdx, data[:])
	return DataCell{
		Data:        data,
		Proof:       proof,
		ColumnIndex: colIdx,
		RowIndex:    rowIdx,
	}
}

func TestCellValidatorValidateCell(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := makeTestDataCell(5, 2)
	if err := cv.ValidateCell(cell, 5); err != nil {
		t.Fatalf("ValidateCell: %v", err)
	}
}

func TestCellValidatorColumnOutOfRange(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := makeTestDataCell(5, 2)
	cell.ColumnIndex = NumberOfColumns + 1
	if err := cv.ValidateCell(cell, cell.ColumnIndex); err == nil {
		t.Fatal("expected error for out-of-range column index")
	}
}

func TestCellValidatorRowOutOfRange(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := makeTestDataCell(5, 2)
	cell.RowIndex = MaxBlobCommitmentsPerBlock + 1
	if err := cv.ValidateCell(cell, cell.ColumnIndex); err == nil {
		t.Fatal("expected error for out-of-range row index")
	}
}

func TestCellValidatorColumnMismatch(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := makeTestDataCell(5, 2)
	// Pass a different column index.
	if err := cv.ValidateCell(cell, 10); err == nil {
		t.Fatal("expected error for column index mismatch")
	}
}

func TestCellValidatorEmptyData(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := DataCell{
		Data:        [BytesPerCell]byte{}, // all zeros
		Proof:       [32]byte{},
		ColumnIndex: 1,
		RowIndex:    0,
	}
	if err := cv.ValidateCell(cell, 1); err == nil {
		t.Fatal("expected error for all-zero cell data")
	}
}

func TestCellValidatorBadProof(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := makeTestDataCell(3, 1)
	cell.Proof = [32]byte{0xBA, 0xD0} // Invalid proof.
	if err := cv.ValidateCell(cell, 3); err == nil {
		t.Fatal("expected error for invalid proof")
	}
}

func TestCellValidateBatchSequential(t *testing.T) {
	cfg := DefaultCellValidatorConfig()
	cfg.ParallelValidation = false
	cv := NewDataCellValidator(cfg)

	cells := []DataCell{
		makeTestDataCell(0, 0),
		makeTestDataCell(1, 0),
		makeTestDataCell(2, 0),
	}

	result := cv.ValidateCellBatch(cells)
	if result.Valid != 3 {
		t.Fatalf("expected 3 valid, got %d", result.Valid)
	}
	if result.Invalid != 0 {
		t.Fatalf("expected 0 invalid, got %d", result.Invalid)
	}
}

func TestCellValidateBatchParallel(t *testing.T) {
	cfg := DefaultCellValidatorConfig()
	cfg.ParallelValidation = true
	cv := NewDataCellValidator(cfg)

	cells := make([]DataCell, 10)
	for i := range cells {
		cells[i] = makeTestDataCell(uint64(i), 0)
	}

	result := cv.ValidateCellBatch(cells)
	if result.Valid != 10 {
		t.Fatalf("expected 10 valid, got %d (invalid=%d, errors=%v)",
			result.Valid, result.Invalid, result.Errors)
	}
}

func TestCellValidateBatchWithInvalid(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cells := []DataCell{
		makeTestDataCell(0, 0),
		makeTestDataCell(1, 0),
	}
	// Corrupt the second cell's proof.
	cells[1].Proof = [32]byte{0xFF}

	result := cv.ValidateCellBatch(cells)
	if result.Valid != 1 {
		t.Fatalf("expected 1 valid, got %d", result.Valid)
	}
	if result.Invalid != 1 {
		t.Fatalf("expected 1 invalid, got %d", result.Invalid)
	}
	if _, ok := result.Errors[1]; !ok {
		t.Fatal("expected error at index 1")
	}
}

func TestCellValidateBatchEmpty(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	result := cv.ValidateCellBatch(nil)
	if len(result.Errors) == 0 {
		t.Fatal("expected error for empty batch")
	}
}

func TestCellValidateBatchDuplicates(t *testing.T) {
	cv := NewDataCellValidator(DefaultCellValidatorConfig())

	cell := makeTestDataCell(5, 2)
	cells := []DataCell{cell, cell} // duplicate position

	result := cv.ValidateCellBatch(cells)
	if result.Invalid != 1 {
		t.Fatalf("expected 1 invalid (duplicate), got %d", result.Invalid)
	}
}

func TestComputeCellProofDeterministic(t *testing.T) {
	data := make([]byte, BytesPerCell)
	for i := range data {
		data[i] = byte(i % 256)
	}

	proof1 := ComputeCellProof(10, 5, data)
	proof2 := ComputeCellProof(10, 5, data)
	if proof1 != proof2 {
		t.Fatal("expected deterministic proof computation")
	}

	// Different column should give different proof.
	proof3 := ComputeCellProof(11, 5, data)
	if proof1 == proof3 {
		t.Fatal("expected different proof for different column")
	}
}

func TestCellReconstructorCanReconstruct(t *testing.T) {
	cr := NewCellReconstructor()

	if !cr.CanReconstructColumn(5, 5) {
		t.Fatal("expected true when all cells available")
	}
	if cr.CanReconstructColumn(1, 5) {
		t.Fatal("expected false with only 1 of 5 cells")
	}
	if !cr.CanReconstructColumn(ReconstructionThreshold, 100) {
		t.Fatal("expected true at reconstruction threshold")
	}
}

func TestCellReconstructorReconstructCells(t *testing.T) {
	cr := NewCellReconstructor()
	cr.MinCellsRequired = 2 // Lower threshold for testing.

	// Create 3 cells for column 0.
	cells := []DataCell{
		makeTestDataCell(0, 0),
		makeTestDataCell(0, 1),
		makeTestDataCell(0, 2),
	}

	// Provide all 3 cells; reconstruction should return them as-is.
	result, err := cr.ReconstructCells(cells, 3)
	if err != nil {
		t.Fatalf("ReconstructCells: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 cells, got %d", len(result))
	}
}

func TestCellReconstructorPartial(t *testing.T) {
	cr := NewCellReconstructor()
	cr.MinCellsRequired = 2

	// Provide 2 of 3 cells.
	available := []DataCell{
		makeTestDataCell(0, 0),
		makeTestDataCell(0, 2),
	}

	result, err := cr.ReconstructCells(available, 3)
	if err != nil {
		t.Fatalf("ReconstructCells with partial data: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 cells after reconstruction, got %d", len(result))
	}

	// The reconstructed cell should be at row 1.
	if result[1].ColumnIndex != 0 {
		t.Fatalf("expected column 0, got %d", result[1].ColumnIndex)
	}
	if result[1].RowIndex != 1 {
		t.Fatalf("expected row 1, got %d", result[1].RowIndex)
	}

	// Verify the proof matches.
	expectedProof := ComputeCellProof(0, 1, result[1].Data[:])
	if result[1].Proof != expectedProof {
		t.Fatal("reconstructed cell proof does not match")
	}
}

func TestCellReconstructorInsufficientData(t *testing.T) {
	cr := NewCellReconstructor()
	cr.MinCellsRequired = 5

	// Only 2 cells available, need 5.
	available := []DataCell{
		makeTestDataCell(0, 0),
		makeTestDataCell(0, 1),
	}

	_, err := cr.ReconstructCells(available, 10)
	if err == nil {
		t.Fatal("expected error for insufficient data")
	}
}

func TestCellReconstructorEmpty(t *testing.T) {
	cr := NewCellReconstructor()
	_, err := cr.ReconstructCells(nil, 5)
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}
