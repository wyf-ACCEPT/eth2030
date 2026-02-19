package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/eth2028/eth2028/core/types"
	"golang.org/x/crypto/sha3"
)

// Validator errors.
var (
	ErrInvalidColumnIdx    = errors.New("das/validator: column index out of range")
	ErrInvalidCustodyGroup = errors.New("das/validator: invalid custody group assignment")
	ErrDataUnavailable     = errors.New("das/validator: data unavailable")
	ErrInvalidColumnProof  = errors.New("das/validator: invalid column proof")
)

// DAValidatorConfig holds configuration for the data availability validator.
type DAValidatorConfig struct {
	// MinCustodyGroups is the minimum number of custody groups a node must serve.
	MinCustodyGroups int

	// ColumnCount is the total number of columns in the extended data matrix.
	ColumnCount int

	// SamplesPerSlot is the number of random column samples per slot.
	SamplesPerSlot int

	// MaxBlobsPerBlock is the maximum number of blobs per block.
	MaxBlobsPerBlock int
}

// DefaultDAValidatorConfig returns the default PeerDAS validator configuration
// based on the Fulu consensus spec parameters.
func DefaultDAValidatorConfig() DAValidatorConfig {
	return DAValidatorConfig{
		MinCustodyGroups: CustodyRequirement,           // 4
		ColumnCount:      NumberOfColumns,               // 128
		SamplesPerSlot:   SamplesPerSlot,                // 8
		MaxBlobsPerBlock: int(MaxBlobCommitmentsPerBlock), // 9
	}
}

// DAValidator validates blob data availability using PeerDAS column custody
// and sampling. It wraps the core DAS primitives into a cohesive validator
// that can verify column sidecars, check custody assignments, and determine
// data availability for a given slot.
type DAValidator struct {
	config DAValidatorConfig
}

// NewDAValidator creates a new DA validator with the given configuration.
func NewDAValidator(config DAValidatorConfig) *DAValidator {
	return &DAValidator{config: config}
}

// ValidateColumnSidecar validates a column sidecar for a given slot. It checks
// that the column index is within range, that data and proof are non-empty,
// and that the proof is structurally valid (hash-based check).
func (v *DAValidator) ValidateColumnSidecar(slot uint64, columnIndex uint64, data []byte, proof []byte) error {
	if columnIndex >= uint64(v.config.ColumnCount) {
		return fmt.Errorf("%w: %d >= %d", ErrInvalidColumnIdx, columnIndex, v.config.ColumnCount)
	}
	if len(data) == 0 {
		return fmt.Errorf("%w: empty column data", ErrInvalidColumnProof)
	}
	if len(proof) == 0 {
		return fmt.Errorf("%w: empty proof", ErrInvalidColumnProof)
	}

	// Verify proof: keccak256(slot || columnIndex || data) == proof.
	expected := computeColumnProof(slot, columnIndex, data)
	if len(expected) != len(proof) {
		return fmt.Errorf("%w: proof length mismatch", ErrInvalidColumnProof)
	}
	for i := range expected {
		if expected[i] != proof[i] {
			return fmt.Errorf("%w: proof mismatch at byte %d", ErrInvalidColumnProof, i)
		}
	}
	return nil
}

// computeColumnProof computes keccak256(slot || columnIndex || data) for
// sidecar verification. Exported via ComputeColumnProof for test helpers.
func computeColumnProof(slot uint64, columnIndex uint64, data []byte) []byte {
	h := sha3.NewLegacyKeccak256()
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], slot)
	binary.LittleEndian.PutUint64(buf[8:], columnIndex)
	h.Write(buf[:])
	h.Write(data)
	return h.Sum(nil)
}

// ComputeColumnProof computes the expected proof for a column sidecar.
// This is exported so that tests can construct valid sidecars.
func ComputeColumnProof(slot uint64, columnIndex uint64, data []byte) []byte {
	return computeColumnProof(slot, columnIndex, data)
}

// ValidateCustodyAssignment verifies that a node identified by nodeID is
// correctly assigned to the given set of columns. The columns must match
// the deterministic custody computation for at least MinCustodyGroups groups.
func (v *DAValidator) ValidateCustodyAssignment(nodeID types.Hash, columns []uint64) error {
	expected := v.ComputeCustodyColumns(nodeID, v.config.MinCustodyGroups)

	// Build a set of expected columns.
	expectedSet := make(map[uint64]bool, len(expected))
	for _, c := range expected {
		expectedSet[c] = true
	}

	// Every claimed column must be in the expected set.
	for _, c := range columns {
		if c >= uint64(v.config.ColumnCount) {
			return fmt.Errorf("%w: column %d out of range", ErrInvalidCustodyGroup, c)
		}
		if !expectedSet[c] {
			return fmt.Errorf("%w: column %d not assigned to node", ErrInvalidCustodyGroup, c)
		}
	}

	// The node must custody at least the minimum number of columns.
	if len(columns) < len(expected) {
		return fmt.Errorf("%w: node custodies %d columns, need %d",
			ErrInvalidCustodyGroup, len(columns), len(expected))
	}

	return nil
}

// ComputeCustodyColumns computes which columns a node must custody based on
// its node ID and the number of custody groups. It uses a hash-chain approach:
// for each group index, hash(nodeID || groupIndex) mod (columnCount / custodyGroups)
// determines the column offset within that group.
//
// With the default config (columnCount=128, custodyGroups matching
// NumberOfCustodyGroups=128), each group maps to exactly one column. This
// delegates to the spec-compliant GetCustodyColumns when possible.
func (v *DAValidator) ComputeCustodyColumns(nodeID types.Hash, custodyGroups int) []uint64 {
	if custodyGroups <= 0 {
		return nil
	}
	if custodyGroups > NumberOfCustodyGroups {
		custodyGroups = NumberOfCustodyGroups
	}

	// Use the spec-compliant implementation from sampling.go.
	var nodeID32 [32]byte
	copy(nodeID32[:], nodeID[:])

	specColumns, err := GetCustodyColumns(nodeID32, uint64(custodyGroups))
	if err != nil {
		return nil
	}

	columns := make([]uint64, len(specColumns))
	for i, c := range specColumns {
		columns[i] = uint64(c)
	}
	return columns
}

// IsDataAvailable checks whether enough columns are available for the given
// slot to consider the data available. All required columns must be present
// in the availableColumns map.
func (v *DAValidator) IsDataAvailable(slot uint64, availableColumns map[uint64]bool, requiredColumns []uint64) bool {
	for _, col := range requiredColumns {
		if !availableColumns[col] {
			return false
		}
	}
	return true
}

// SampleColumns selects random columns to sample for a given slot and node ID.
// The selection is deterministic: the same (slot, nodeID) always produces the
// same sample set. The number of samples is config.SamplesPerSlot.
func (v *DAValidator) SampleColumns(slot uint64, nodeID types.Hash) []uint64 {
	if v.config.SamplesPerSlot <= 0 || v.config.ColumnCount <= 0 {
		return nil
	}

	// Hash slot + nodeID to get a seed.
	h := sha3.NewLegacyKeccak256()
	var slotBuf [8]byte
	binary.LittleEndian.PutUint64(slotBuf[:], slot)
	h.Write(slotBuf[:])
	h.Write(nodeID[:])
	seed := h.Sum(nil)

	seen := make(map[uint64]bool)
	samples := make([]uint64, 0, v.config.SamplesPerSlot)
	counter := uint64(0)

	for len(samples) < v.config.SamplesPerSlot {
		// Hash seed + counter for each sample.
		sh := sha3.NewLegacyKeccak256()
		sh.Write(seed)
		var cBuf [8]byte
		binary.LittleEndian.PutUint64(cBuf[:], counter)
		sh.Write(cBuf[:])
		digest := sh.Sum(nil)

		val := binary.LittleEndian.Uint64(digest[:8])
		col := val % uint64(v.config.ColumnCount)

		if !seen[col] {
			seen[col] = true
			samples = append(samples, col)
		}
		counter++
	}

	sort.Slice(samples, func(i, j int) bool {
		return samples[i] < samples[j]
	})
	return samples
}
