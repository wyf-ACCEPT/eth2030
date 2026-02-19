package das

import (
	"errors"
	"fmt"
)

var (
	ErrInsufficientCells = errors.New("das: insufficient cells for reconstruction")
	ErrInvalidCellIndex  = errors.New("das: cell index out of range")
	ErrDuplicateCellIndex = errors.New("das: duplicate cell index")
)

// CanReconstruct returns true if the number of received cells/columns is
// sufficient to reconstruct the full extended blob via Reed-Solomon decoding.
// At least 50% of columns (NUMBER_OF_COLUMNS / 2 = 64) are required.
func CanReconstruct(receivedCount int) bool {
	return receivedCount >= ReconstructionThreshold
}

// ReconstructBlob attempts to reconstruct a full blob from a partial set of
// cells using Reed-Solomon erasure coding recovery.
//
// This is a placeholder implementation that validates inputs but does not
// perform actual RS decoding. A production implementation would use a proper
// Reed-Solomon decoder over the BLS12-381 scalar field.
//
// Parameters:
//   - cells: the received cells (at least ReconstructionThreshold required)
//   - cellIndices: the column indices corresponding to each cell
//
// Returns the reconstructed blob data or an error.
func ReconstructBlob(cells []Cell, cellIndices []uint64) ([]byte, error) {
	if len(cells) != len(cellIndices) {
		return nil, fmt.Errorf("%w: %d cells but %d indices",
			ErrInsufficientCells, len(cells), len(cellIndices))
	}
	if !CanReconstruct(len(cells)) {
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrInsufficientCells, len(cells), ReconstructionThreshold)
	}

	// Validate cell indices are in range and unique.
	seen := make(map[uint64]bool, len(cellIndices))
	for _, idx := range cellIndices {
		if idx >= CellsPerExtBlob {
			return nil, fmt.Errorf("%w: index %d >= %d",
				ErrInvalidCellIndex, idx, CellsPerExtBlob)
		}
		if seen[idx] {
			return nil, fmt.Errorf("%w: index %d", ErrDuplicateCellIndex, idx)
		}
		seen[idx] = true
	}

	// Placeholder: in a full implementation, we would perform RS recovery
	// over the BLS12-381 field. For now, concatenate the original (non-extension)
	// cells in order as a best-effort approximation when we have the first half.
	//
	// The actual blob is FIELD_ELEMENTS_PER_BLOB * BYTES_PER_FIELD_ELEMENT bytes,
	// which equals the first half of the extended cells (indices 0..63).
	blobSize := FieldElementsPerBlob * BytesPerFieldElement
	result := make([]byte, blobSize)

	// Build a map of cell index -> cell data for quick lookup.
	cellMap := make(map[uint64]*Cell, len(cells))
	for i, idx := range cellIndices {
		c := cells[i]
		cellMap[idx] = &c
	}

	// Fill in the original cells (first half of the extended blob).
	originalCells := CellsPerExtBlob / 2 // 64
	for i := uint64(0); i < uint64(originalCells); i++ {
		if cell, ok := cellMap[i]; ok {
			offset := int(i) * BytesPerCell
			copy(result[offset:offset+BytesPerCell], cell[:])
		}
		// Missing original cells would need RS recovery from parity cells.
	}

	return result, nil
}

// RecoverMatrix recovers the full matrix of cells from a partial set of
// matrix entries, similar to the consensus spec's recover_matrix helper.
//
// This is a placeholder that validates inputs; full RS recovery of missing
// cells is not implemented.
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

	// Placeholder: return the input entries as-is since we don't have
	// actual RS recovery. A real implementation would fill in missing cells.
	return entries, nil
}
