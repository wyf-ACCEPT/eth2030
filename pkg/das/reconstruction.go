package das

import (
	"errors"
	"fmt"
	"math/big"
)

var (
	ErrInsufficientCells  = errors.New("das: insufficient cells for reconstruction")
	ErrInvalidCellIndex   = errors.New("das: cell index out of range")
	ErrDuplicateCellIndex = errors.New("das: duplicate cell index")
)

// CanReconstruct returns true if the number of received cells/columns is
// sufficient to reconstruct the full extended blob via Reed-Solomon decoding.
// At least 50% of columns (NUMBER_OF_COLUMNS / 2 = 64) are required.
func CanReconstruct(receivedCount int) bool {
	return receivedCount >= ReconstructionThreshold
}

// validateCellInputs checks common preconditions for reconstruction.
func validateCellInputs(cells []Cell, cellIndices []uint64) error {
	if len(cells) != len(cellIndices) {
		return fmt.Errorf("%w: %d cells but %d indices",
			ErrInsufficientCells, len(cells), len(cellIndices))
	}
	if !CanReconstruct(len(cells)) {
		return fmt.Errorf("%w: have %d, need %d",
			ErrInsufficientCells, len(cells), ReconstructionThreshold)
	}

	seen := make(map[uint64]bool, len(cellIndices))
	for _, idx := range cellIndices {
		if idx >= CellsPerExtBlob {
			return fmt.Errorf("%w: index %d >= %d",
				ErrInvalidCellIndex, idx, CellsPerExtBlob)
		}
		if seen[idx] {
			return fmt.Errorf("%w: index %d", ErrDuplicateCellIndex, idx)
		}
		seen[idx] = true
	}
	return nil
}

// ReconstructPolynomial recovers a polynomial of degree < k from k evaluation
// points using Lagrange interpolation over the BLS12-381 scalar field.
//
// xs and ys must have the same length >= k. Only the first k points are used.
// The returned slice has length k, holding the polynomial coefficients
// [c_0, c_1, ..., c_{k-1}].
func ReconstructPolynomial(xs, ys []FieldElement, k int) ([]FieldElement, error) {
	if len(xs) != len(ys) {
		return nil, fmt.Errorf("das: xs and ys length mismatch: %d vs %d", len(xs), len(ys))
	}
	if len(xs) < k {
		return nil, fmt.Errorf("das: need at least %d points, have %d", k, len(xs))
	}

	// Use exactly k points.
	xs = xs[:k]
	ys = ys[:k]

	// Lagrange interpolation to recover coefficients.
	// First compute the Lagrange basis polynomials and accumulate.
	coeffs := make([]FieldElement, k)
	for i := range coeffs {
		coeffs[i] = FieldZero()
	}

	for i := 0; i < k; i++ {
		// Compute the i-th Lagrange basis polynomial's coefficient contribution.
		// L_i(x) = y_i * prod_{j!=i} (x - x_j) / (x_i - x_j)

		// First compute the denominator: prod_{j!=i} (x_i - x_j).
		denom := FieldOne()
		for j := 0; j < k; j++ {
			if j == i {
				continue
			}
			denom = denom.Mul(xs[i].Sub(xs[j]))
		}
		factor := ys[i].Div(denom)

		// Build the numerator polynomial prod_{j!=i} (x - x_j).
		// Start with [1] and multiply by (x - x_j) for each j != i.
		poly := make([]FieldElement, 1, k)
		poly[0] = FieldOne()
		for j := 0; j < k; j++ {
			if j == i {
				continue
			}
			// Multiply poly by (x - x_j).
			newPoly := make([]FieldElement, len(poly)+1)
			for m := range newPoly {
				newPoly[m] = FieldZero()
			}
			for m := range poly {
				// poly[m] * x -> newPoly[m+1]
				newPoly[m+1] = newPoly[m+1].Add(poly[m])
				// poly[m] * (-x_j) -> newPoly[m]
				newPoly[m] = newPoly[m].Sub(poly[m].Mul(xs[j]))
			}
			poly = newPoly
		}

		// Add factor * poly to coeffs.
		for m := range poly {
			if m < k {
				coeffs[m] = coeffs[m].Add(factor.Mul(poly[m]))
			}
		}
	}

	return coeffs, nil
}

// evaluatePolynomial evaluates a polynomial at point x.
// coeffs[i] is the coefficient of x^i.
func evaluatePolynomial(coeffs []FieldElement, x FieldElement) FieldElement {
	if len(coeffs) == 0 {
		return FieldZero()
	}
	// Horner's method.
	result := coeffs[len(coeffs)-1]
	for i := len(coeffs) - 2; i >= 0; i-- {
		result = result.Mul(x).Add(coeffs[i])
	}
	return result
}

// cellToFieldElements converts a cell's raw bytes to field elements.
// Each field element is 32 bytes (big-endian within BLS12-381 scalar field).
func cellToFieldElements(cell *Cell) []FieldElement {
	elems := make([]FieldElement, FieldElementsPerCell)
	for i := 0; i < FieldElementsPerCell; i++ {
		b := new(big.Int).SetBytes(cell[i*BytesPerFieldElement : (i+1)*BytesPerFieldElement])
		elems[i] = NewFieldElement(b)
	}
	return elems
}

// fieldElementsToBytes converts field elements back to raw bytes.
func fieldElementsToBytes(elems []FieldElement, size int) []byte {
	result := make([]byte, size)
	for i, elem := range elems {
		offset := i * BytesPerFieldElement
		if offset+BytesPerFieldElement > size {
			break
		}
		b := elem.BigInt().Bytes()
		// Right-align in 32-byte slot.
		start := offset + BytesPerFieldElement - len(b)
		copy(result[start:offset+BytesPerFieldElement], b)
	}
	return result
}

// ReconstructBlob reconstructs a full blob from a partial set of cells using
// Reed-Solomon erasure coding recovery via Lagrange interpolation over the
// BLS12-381 scalar field.
//
// The blob is encoded as a polynomial of degree < FieldElementsPerBlob (4096),
// evaluated at CellsPerExtBlob (128) positions. Each cell contains
// FieldElementsPerCell (64) consecutive field elements from the evaluation
// domain. Given >= 50% of cells, we recover each field element column
// independently using Lagrange interpolation.
//
// Parameters:
//   - cells: the received cells (at least ReconstructionThreshold required)
//   - cellIndices: the column indices corresponding to each cell
//
// Returns the reconstructed blob data or an error.
func ReconstructBlob(cells []Cell, cellIndices []uint64) ([]byte, error) {
	if err := validateCellInputs(cells, cellIndices); err != nil {
		return nil, err
	}

	numCells := len(cells)
	blobSize := FieldElementsPerBlob * BytesPerFieldElement

	// Compute the evaluation domain roots of unity.
	// The extended blob uses CellsPerExtBlob evaluation positions.
	// Each cell i holds field elements evaluated at positions related to cell index i.
	// We use the cell indices as the x-coordinates for interpolation.

	// For RS reconstruction, we treat each "field element position" within
	// a cell independently. For field element position j within cells,
	// we have evaluations at x = cellIndex for each received cell.
	// We need to interpolate and recover the original data blob (first half).

	// Precompute x-coordinates: use cell indices as evaluation points.
	xs := make([]FieldElement, numCells)
	for i, idx := range cellIndices {
		xs[i] = NewFieldElementFromUint64(idx)
	}

	// Parse all cells into field elements.
	cellElems := make([][]FieldElement, numCells)
	for i := range cells {
		cellElems[i] = cellToFieldElements(&cells[i])
	}

	// For each field element position within a cell, reconstruct the
	// polynomial and evaluate at the original positions (0..63).
	originalCells := CellsPerExtBlob / 2
	resultElems := make([]FieldElement, FieldElementsPerBlob)

	for fePos := 0; fePos < FieldElementsPerCell; fePos++ {
		// Gather y-values for this field element position.
		ys := make([]FieldElement, numCells)
		for i := range cells {
			ys[i] = cellElems[i][fePos]
		}

		// Recover the polynomial (degree < CellsPerExtBlob).
		// We only need evaluations at the first half of indices (0..63).
		coeffs, err := ReconstructPolynomial(xs, ys, numCells)
		if err != nil {
			return nil, fmt.Errorf("das: polynomial reconstruction failed at position %d: %w", fePos, err)
		}

		// Evaluate at original cell indices 0..63 to get blob field elements.
		for cellIdx := 0; cellIdx < originalCells; cellIdx++ {
			x := NewFieldElementFromUint64(uint64(cellIdx))
			val := evaluatePolynomial(coeffs, x)
			resultElems[cellIdx*FieldElementsPerCell+fePos] = val
		}
	}

	return fieldElementsToBytes(resultElems, blobSize), nil
}

// RecoverCellsAndProofs recovers all missing cells and their indices from
// a partial set of cells. Returns the full set of CellsPerExtBlob cells.
//
// Note: KZG proof recovery requires the commitment and is not implemented
// here. The returned proofs slice will contain zero proofs for recovered cells.
func RecoverCellsAndProofs(cells []Cell, cellIndices []uint64) ([]Cell, []KZGProof, error) {
	if err := validateCellInputs(cells, cellIndices); err != nil {
		return nil, nil, err
	}

	numCells := len(cells)

	// Build index set for quick lookup.
	indexSet := make(map[uint64]int, numCells)
	for i, idx := range cellIndices {
		indexSet[idx] = i
	}

	// Precompute evaluation points.
	xs := make([]FieldElement, numCells)
	for i, idx := range cellIndices {
		xs[i] = NewFieldElementFromUint64(idx)
	}

	// Parse all cells into field elements.
	cellElems := make([][]FieldElement, numCells)
	for i := range cells {
		cellElems[i] = cellToFieldElements(&cells[i])
	}

	// Output: full set of cells and proofs.
	allCells := make([]Cell, CellsPerExtBlob)
	allProofs := make([]KZGProof, CellsPerExtBlob)

	// Copy existing cells.
	for i, idx := range cellIndices {
		allCells[idx] = cells[i]
	}

	// For each field element position, reconstruct and evaluate missing cells.
	for fePos := 0; fePos < FieldElementsPerCell; fePos++ {
		ys := make([]FieldElement, numCells)
		for i := range cells {
			ys[i] = cellElems[i][fePos]
		}

		coeffs, err := ReconstructPolynomial(xs, ys, numCells)
		if err != nil {
			return nil, nil, fmt.Errorf("das: recovery failed at position %d: %w", fePos, err)
		}

		// Fill in missing cells.
		for cellIdx := uint64(0); cellIdx < CellsPerExtBlob; cellIdx++ {
			if _, exists := indexSet[cellIdx]; exists {
				continue // Already have this cell.
			}
			x := NewFieldElementFromUint64(cellIdx)
			val := evaluatePolynomial(coeffs, x)
			b := val.BigInt().Bytes()
			start := fePos*BytesPerFieldElement + BytesPerFieldElement - len(b)
			copy(allCells[cellIdx][start:fePos*BytesPerFieldElement+BytesPerFieldElement], b)
		}
	}

	return allCells, allProofs, nil
}

// RecoverMatrix recovers the full matrix of cells from a partial set of
// matrix entries, following the consensus spec's recover_matrix helper.
// Each row (blob) is reconstructed independently.
func RecoverMatrix(entries []MatrixEntry, blobCount int) ([]MatrixEntry, error) {
	if blobCount <= 0 {
		return nil, fmt.Errorf("das: invalid blob count %d", blobCount)
	}

	// Group entries by row (blob).
	byRow := make(map[RowIndex][]MatrixEntry)
	for _, e := range entries {
		byRow[e.RowIndex] = append(byRow[e.RowIndex], e)
	}

	// Check each row has enough entries for reconstruction.
	for row := 0; row < blobCount; row++ {
		rowEntries := byRow[RowIndex(row)]
		if !CanReconstruct(len(rowEntries)) {
			return nil, fmt.Errorf("%w: row %d has %d cells, need %d",
				ErrInsufficientCells, row, len(rowEntries), ReconstructionThreshold)
		}
	}

	// Recover each row independently.
	var result []MatrixEntry
	for row := 0; row < blobCount; row++ {
		rowEntries := byRow[RowIndex(row)]

		cells := make([]Cell, len(rowEntries))
		indices := make([]uint64, len(rowEntries))
		for i, e := range rowEntries {
			cells[i] = e.Cell
			indices[i] = uint64(e.ColumnIndex)
		}

		allCells, allProofs, err := RecoverCellsAndProofs(cells, indices)
		if err != nil {
			return nil, fmt.Errorf("das: row %d recovery failed: %w", row, err)
		}

		for colIdx := uint64(0); colIdx < CellsPerExtBlob; colIdx++ {
			result = append(result, MatrixEntry{
				Cell:        allCells[colIdx],
				KZGProof:    allProofs[colIdx],
				ColumnIndex: ColumnIndex(colIdx),
				RowIndex:    RowIndex(row),
			})
		}
	}

	return result, nil
}
