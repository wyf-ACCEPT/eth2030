package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"golang.org/x/crypto/sha3"
)

var (
	ErrInvalidCustodyCount = errors.New("das: custody count exceeds number of custody groups")
	ErrInvalidColumnIndex  = errors.New("das: column index out of range")
	ErrInvalidSidecar      = errors.New("das: invalid data column sidecar")
	ErrMismatchedLengths   = errors.New("das: mismatched commitment/proof/cell lengths")
)

// uint256Max is 2^256 - 1, used for overflow prevention in GetCustodyGroups.
var uint256Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// GetCustodyGroups computes the custody groups for a given node ID and custody
// count, following the consensus spec's get_custody_groups algorithm.
//
// The node ID is hashed iteratively to produce pseudo-random custody group
// assignments. The result is sorted in ascending order.
func GetCustodyGroups(nodeID [32]byte, custodyGroupCount uint64) ([]CustodyGroup, error) {
	if custodyGroupCount > NumberOfCustodyGroups {
		return nil, ErrInvalidCustodyCount
	}
	// Fast path: if all groups are custodied, return them all.
	if custodyGroupCount == NumberOfCustodyGroups {
		groups := make([]CustodyGroup, NumberOfCustodyGroups)
		for i := uint64(0); i < NumberOfCustodyGroups; i++ {
			groups[i] = CustodyGroup(i)
		}
		return groups, nil
	}

	currentID := new(big.Int).SetBytes(nodeID[:])
	seen := make(map[CustodyGroup]bool)
	groups := make([]CustodyGroup, 0, custodyGroupCount)

	for uint64(len(groups)) < custodyGroupCount {
		// Hash the current ID to get a pseudo-random value.
		idBytes := make([]byte, 32)
		currentID.FillBytes(idBytes)

		h := sha3.NewLegacyKeccak256()
		h.Write(idBytes)
		digest := h.Sum(nil)

		// Take the first 8 bytes as a uint64 and mod by NumberOfCustodyGroups.
		val := binary.LittleEndian.Uint64(digest[:8])
		group := CustodyGroup(val % NumberOfCustodyGroups)

		if !seen[group] {
			seen[group] = true
			groups = append(groups, group)
		}

		// Increment current_id with overflow protection.
		if currentID.Cmp(uint256Max) == 0 {
			currentID.SetUint64(0)
		} else {
			currentID.Add(currentID, big.NewInt(1))
		}
	}

	// Return sorted.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i] < groups[j]
	})
	return groups, nil
}

// ComputeColumnsForCustodyGroup returns the column indices assigned to a
// given custody group, following the consensus spec's
// compute_columns_for_custody_group algorithm.
func ComputeColumnsForCustodyGroup(group CustodyGroup) ([]ColumnIndex, error) {
	if uint64(group) >= NumberOfCustodyGroups {
		return nil, fmt.Errorf("%w: group %d", ErrInvalidColumnIndex, group)
	}
	columnsPerGroup := NumberOfColumns / NumberOfCustodyGroups
	columns := make([]ColumnIndex, columnsPerGroup)
	for i := 0; i < columnsPerGroup; i++ {
		columns[i] = ColumnIndex(NumberOfCustodyGroups*uint64(i) + uint64(group))
	}
	return columns, nil
}

// GetCustodyColumns returns all column indices that a node should custody,
// given its node ID and custody group count. This is a convenience wrapper
// that combines GetCustodyGroups and ComputeColumnsForCustodyGroup.
func GetCustodyColumns(nodeID [32]byte, custodyGroupCount uint64) ([]ColumnIndex, error) {
	groups, err := GetCustodyGroups(nodeID, custodyGroupCount)
	if err != nil {
		return nil, err
	}

	var columns []ColumnIndex
	for _, g := range groups {
		cols, err := ComputeColumnsForCustodyGroup(g)
		if err != nil {
			return nil, err
		}
		columns = append(columns, cols...)
	}

	sort.Slice(columns, func(i, j int) bool {
		return columns[i] < columns[j]
	})
	return columns, nil
}

// ShouldCustodyColumn returns true if columnIndex is in the node's custody set.
func ShouldCustodyColumn(columnIndex ColumnIndex, custodyColumns []ColumnIndex) bool {
	for _, c := range custodyColumns {
		if c == columnIndex {
			return true
		}
	}
	return false
}

// VerifyDataColumnSidecar performs basic structural validation of a
// DataColumnSidecar. It checks that the column index is in range and that
// the cells, commitments, and proofs slices have consistent lengths.
//
// Cryptographic verification (KZG proof checking) is left to higher-level
// callers that have access to the full KZG library.
func VerifyDataColumnSidecar(sidecar *DataColumnSidecar) error {
	if sidecar == nil {
		return ErrInvalidSidecar
	}
	if uint64(sidecar.Index) >= NumberOfColumns {
		return fmt.Errorf("%w: index %d >= %d", ErrInvalidColumnIndex, sidecar.Index, NumberOfColumns)
	}
	blobCount := len(sidecar.Column)
	if blobCount == 0 {
		return fmt.Errorf("%w: empty column", ErrInvalidSidecar)
	}
	if blobCount > MaxBlobCommitmentsPerBlock {
		return fmt.Errorf("%w: too many cells %d > %d", ErrInvalidSidecar, blobCount, MaxBlobCommitmentsPerBlock)
	}
	if len(sidecar.KZGCommitments) != blobCount {
		return fmt.Errorf("%w: %d commitments for %d cells", ErrMismatchedLengths, len(sidecar.KZGCommitments), blobCount)
	}
	if len(sidecar.KZGProofs) != blobCount {
		return fmt.Errorf("%w: %d proofs for %d cells", ErrMismatchedLengths, len(sidecar.KZGProofs), blobCount)
	}
	return nil
}

// ColumnSubnet returns the subnet ID for a given column index.
// subnet = column_index % DATA_COLUMN_SIDECAR_SUBNET_COUNT
func ColumnSubnet(columnIndex ColumnIndex) SubnetID {
	return SubnetID(uint64(columnIndex) % DataColumnSidecarSubnetCount)
}
