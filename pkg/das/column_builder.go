// column_builder.go implements a DataColumn builder for PeerDAS (EIP-7594).
// It constructs data columns from blob data, assigns custody responsibilities
// based on node ID, computes column commitments using KZG, and supports
// column-level gossip messages with validation and deduplication.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package das

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"golang.org/x/crypto/sha3"
)

// Column builder errors.
var (
	ErrBuilderNilBlobs       = errors.New("das/colbuilder: nil blob data")
	ErrBuilderEmptyBlobs     = errors.New("das/colbuilder: empty blob data")
	ErrBuilderBlobTooLarge   = errors.New("das/colbuilder: blob exceeds maximum size")
	ErrBuilderColumnOOB      = errors.New("das/colbuilder: column index out of range")
	ErrBuilderDuplicateCol   = errors.New("das/colbuilder: duplicate column in batch")
	ErrBuilderInvalidCell    = errors.New("das/colbuilder: cell data size mismatch")
	ErrBuilderMsgNil         = errors.New("das/colbuilder: nil gossip message")
	ErrBuilderMsgColOOB      = errors.New("das/colbuilder: gossip message column out of range")
	ErrBuilderMsgBlobOOB     = errors.New("das/colbuilder: gossip message blob index out of range")
	ErrBuilderMsgDataInvalid = errors.New("das/colbuilder: gossip message data invalid")
	ErrBuilderAlreadySeen    = errors.New("das/colbuilder: message already seen (duplicate)")
)

// ColumnBuilderConfig configures the column builder.
type ColumnBuilderConfig struct {
	// NumColumns is the total columns in the extended data matrix.
	NumColumns int

	// MaxBlobs is the maximum number of blobs per block.
	MaxBlobs int

	// CellSize is the byte size of each cell.
	CellSize int

	// MaxDedup is the capacity of the deduplication set.
	MaxDedup int
}

// DefaultColumnBuilderConfig returns production defaults from the Fulu spec.
func DefaultColumnBuilderConfig() ColumnBuilderConfig {
	return ColumnBuilderConfig{
		NumColumns: NumberOfColumns,
		MaxBlobs:   MaxBlobCommitmentsPerBlock,
		CellSize:   BytesPerCell,
		MaxDedup:   8192,
	}
}

// BuiltColumn represents a fully constructed data column with its computed
// commitment and associated metadata.
type BuiltColumn struct {
	// Index is the column index in [0, NumColumns).
	Index ColumnIndex

	// Cells contains one cell per blob in the block.
	Cells []Cell

	// Commitment is the keccak256 commitment over all cells in the column.
	Commitment [32]byte

	// Proofs contains one proof per cell (blob) in the column.
	Proofs []KZGProof

	// BlobCount is the number of blobs this column spans.
	BlobCount int
}

// ColumnGossipMessage is a network gossip message carrying a single column
// with validation metadata.
type ColumnGossipMessage struct {
	// ColumnIndex identifies which column this message carries.
	ColumnIndex ColumnIndex

	// Slot is the beacon slot this column belongs to.
	Slot uint64

	// BlobIndex identifies which blob this cell comes from.
	BlobIndex uint64

	// CellData is the raw cell data.
	CellData []byte

	// Proof is the KZG proof for this cell.
	Proof KZGProof

	// MessageHash is the deduplication hash for this message.
	MessageHash [32]byte
}

// CustodyAssignmentResult describes which columns a node should build and
// serve for custody purposes.
type CustodyAssignmentResult struct {
	NodeID        [32]byte
	ColumnIndices []ColumnIndex
	GroupIndices  []CustodyGroup
}

// ColumnBuilder constructs data columns from blob data, computes commitments,
// manages custody assignments, and handles column gossip deduplication.
// All public methods are safe for concurrent use.
type ColumnBuilder struct {
	mu     sync.RWMutex
	config ColumnBuilderConfig

	// builtColumns caches columns built in the current round.
	builtColumns map[ColumnIndex]*BuiltColumn

	// dedupSet tracks seen gossip message hashes for deduplication.
	dedupSet map[[32]byte]bool

	// dedupOrder maintains insertion order for LRU eviction.
	dedupOrder [][32]byte
}

// NewColumnBuilder creates a new column builder with the given configuration.
func NewColumnBuilder(config ColumnBuilderConfig) *ColumnBuilder {
	if config.NumColumns <= 0 {
		config.NumColumns = NumberOfColumns
	}
	if config.MaxBlobs <= 0 {
		config.MaxBlobs = MaxBlobCommitmentsPerBlock
	}
	if config.CellSize <= 0 {
		config.CellSize = BytesPerCell
	}
	if config.MaxDedup <= 0 {
		config.MaxDedup = 8192
	}
	return &ColumnBuilder{
		config:       config,
		builtColumns: make(map[ColumnIndex]*BuiltColumn),
		dedupSet:     make(map[[32]byte]bool),
	}
}

// BuildColumns constructs data columns from the given blob data. Each blob
// is split into cells, and each column collects one cell per blob at the
// same column index. Returns columns sorted by index.
//
// blobData is a slice of raw blobs. Each blob is split into cells of CellSize.
func (cb *ColumnBuilder) BuildColumns(blobData [][]byte) ([]*BuiltColumn, error) {
	if blobData == nil {
		return nil, ErrBuilderNilBlobs
	}
	if len(blobData) == 0 {
		return nil, ErrBuilderEmptyBlobs
	}
	if len(blobData) > cb.config.MaxBlobs {
		return nil, fmt.Errorf("%w: %d blobs > max %d",
			ErrBuilderBlobTooLarge, len(blobData), cb.config.MaxBlobs)
	}

	numBlobs := len(blobData)

	// Determine cells per blob: min of all blobs divided by CellSize.
	// Use NumColumns as the target cell count.
	cellsPerBlob := cb.config.NumColumns

	// Build columns: column[i] collects cell[i] from each blob.
	columns := make([]*BuiltColumn, cb.config.NumColumns)
	for colIdx := 0; colIdx < cb.config.NumColumns; colIdx++ {
		col := &BuiltColumn{
			Index:     ColumnIndex(colIdx),
			Cells:     make([]Cell, numBlobs),
			Proofs:    make([]KZGProof, numBlobs),
			BlobCount: numBlobs,
		}

		for blobIdx := 0; blobIdx < numBlobs; blobIdx++ {
			cell := extractCell(blobData[blobIdx], colIdx, cellsPerBlob, cb.config.CellSize)
			col.Cells[blobIdx] = cell
			col.Proofs[blobIdx] = computeCellProof(blobData[blobIdx], blobIdx, colIdx)
		}

		col.Commitment = computeColumnCommitment(col)
		columns[colIdx] = col
	}

	// Cache the built columns.
	cb.mu.Lock()
	for _, col := range columns {
		cb.builtColumns[col.Index] = col
	}
	cb.mu.Unlock()

	return columns, nil
}

// BuildColumnSubset constructs only the specified column indices from blob data.
// This is useful for building just the custody columns a node is responsible for.
func (cb *ColumnBuilder) BuildColumnSubset(blobData [][]byte, indices []ColumnIndex) ([]*BuiltColumn, error) {
	if blobData == nil {
		return nil, ErrBuilderNilBlobs
	}
	if len(blobData) == 0 {
		return nil, ErrBuilderEmptyBlobs
	}

	numBlobs := len(blobData)
	cellsPerBlob := cb.config.NumColumns

	// Check for duplicates and OOB.
	seen := make(map[ColumnIndex]bool, len(indices))
	for _, idx := range indices {
		if uint64(idx) >= uint64(cb.config.NumColumns) {
			return nil, fmt.Errorf("%w: %d >= %d", ErrBuilderColumnOOB, idx, cb.config.NumColumns)
		}
		if seen[idx] {
			return nil, fmt.Errorf("%w: column %d", ErrBuilderDuplicateCol, idx)
		}
		seen[idx] = true
	}

	columns := make([]*BuiltColumn, len(indices))
	for i, colIdx := range indices {
		col := &BuiltColumn{
			Index:     colIdx,
			Cells:     make([]Cell, numBlobs),
			Proofs:    make([]KZGProof, numBlobs),
			BlobCount: numBlobs,
		}

		for blobIdx := 0; blobIdx < numBlobs; blobIdx++ {
			cell := extractCell(blobData[blobIdx], int(colIdx), cellsPerBlob, cb.config.CellSize)
			col.Cells[blobIdx] = cell
			col.Proofs[blobIdx] = computeCellProof(blobData[blobIdx], blobIdx, int(colIdx))
		}

		col.Commitment = computeColumnCommitment(col)
		columns[i] = col
	}

	// Sort by index.
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Index < columns[j].Index
	})

	cb.mu.Lock()
	for _, col := range columns {
		cb.builtColumns[col.Index] = col
	}
	cb.mu.Unlock()

	return columns, nil
}

// GetBuiltColumn retrieves a previously built column by index.
func (cb *ColumnBuilder) GetBuiltColumn(idx ColumnIndex) *BuiltColumn {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.builtColumns[idx]
}

// BuiltColumnCount returns how many columns are cached.
func (cb *ColumnBuilder) BuiltColumnCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.builtColumns)
}

// AssignCustodyColumns computes the custody column indices for a given node.
func (cb *ColumnBuilder) AssignCustodyColumns(nodeID [32]byte, custodyGroupCount uint64) (*CustodyAssignmentResult, error) {
	groups, err := GetCustodyGroups(nodeID, custodyGroupCount)
	if err != nil {
		return nil, fmt.Errorf("das/colbuilder: custody groups: %w", err)
	}

	var columnIndices []ColumnIndex
	for _, g := range groups {
		cols, err := ComputeColumnsForCustodyGroup(g)
		if err != nil {
			continue
		}
		columnIndices = append(columnIndices, cols...)
	}
	sort.Slice(columnIndices, func(i, j int) bool {
		return columnIndices[i] < columnIndices[j]
	})

	return &CustodyAssignmentResult{
		NodeID:        nodeID,
		ColumnIndices: columnIndices,
		GroupIndices:  groups,
	}, nil
}

// CreateGossipMessage creates a column gossip message for broadcasting a cell.
func (cb *ColumnBuilder) CreateGossipMessage(slot uint64, blobIdx uint64, colIdx ColumnIndex, cellData []byte, proof KZGProof) (*ColumnGossipMessage, error) {
	if uint64(colIdx) >= uint64(cb.config.NumColumns) {
		return nil, fmt.Errorf("%w: %d", ErrBuilderMsgColOOB, colIdx)
	}
	if blobIdx >= uint64(cb.config.MaxBlobs) {
		return nil, fmt.Errorf("%w: %d", ErrBuilderMsgBlobOOB, blobIdx)
	}
	if len(cellData) == 0 || len(cellData) > cb.config.CellSize {
		return nil, fmt.Errorf("%w: size %d", ErrBuilderMsgDataInvalid, len(cellData))
	}

	msg := &ColumnGossipMessage{
		ColumnIndex: colIdx,
		Slot:        slot,
		BlobIndex:   blobIdx,
		CellData:    append([]byte(nil), cellData...),
		Proof:       proof,
	}
	msg.MessageHash = computeGossipMessageHash(msg)
	return msg, nil
}

// ValidateGossipMessage performs structural validation on a gossip message.
func (cb *ColumnBuilder) ValidateGossipMessage(msg *ColumnGossipMessage) error {
	if msg == nil {
		return ErrBuilderMsgNil
	}
	if uint64(msg.ColumnIndex) >= uint64(cb.config.NumColumns) {
		return fmt.Errorf("%w: column %d >= %d", ErrBuilderMsgColOOB, msg.ColumnIndex, cb.config.NumColumns)
	}
	if msg.BlobIndex >= uint64(cb.config.MaxBlobs) {
		return fmt.Errorf("%w: blob %d >= %d", ErrBuilderMsgBlobOOB, msg.BlobIndex, cb.config.MaxBlobs)
	}
	if len(msg.CellData) == 0 || len(msg.CellData) > cb.config.CellSize {
		return fmt.Errorf("%w: size %d", ErrBuilderMsgDataInvalid, len(msg.CellData))
	}
	return nil
}

// CheckDuplicate checks if a gossip message has already been seen.
// Returns ErrBuilderAlreadySeen if the message is a duplicate, nil otherwise.
// Non-duplicate messages are added to the dedup set.
func (cb *ColumnBuilder) CheckDuplicate(msg *ColumnGossipMessage) error {
	if msg == nil {
		return ErrBuilderMsgNil
	}

	hash := msg.MessageHash
	if hash == [32]byte{} {
		hash = computeGossipMessageHash(msg)
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.dedupSet[hash] {
		return ErrBuilderAlreadySeen
	}

	// Evict oldest if at capacity.
	if len(cb.dedupOrder) >= cb.config.MaxDedup {
		oldest := cb.dedupOrder[0]
		cb.dedupOrder = cb.dedupOrder[1:]
		delete(cb.dedupSet, oldest)
	}

	cb.dedupSet[hash] = true
	cb.dedupOrder = append(cb.dedupOrder, hash)
	return nil
}

// DedupSize returns the current size of the deduplication set.
func (cb *ColumnBuilder) DedupSize() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.dedupSet)
}

// ClearBuilt clears the built column cache.
func (cb *ColumnBuilder) ClearBuilt() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.builtColumns = make(map[ColumnIndex]*BuiltColumn)
}

// ClearDedup clears the deduplication set.
func (cb *ColumnBuilder) ClearDedup() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.dedupSet = make(map[[32]byte]bool)
	cb.dedupOrder = nil
}

// ToDataColumnSidecar converts a BuiltColumn into a DataColumnSidecar
// suitable for network transmission.
func (cb *ColumnBuilder) ToDataColumnSidecar(col *BuiltColumn, commitments []KZGCommitment) *DataColumnSidecar {
	sidecar := &DataColumnSidecar{
		Index:          col.Index,
		Column:         make([]Cell, len(col.Cells)),
		KZGCommitments: make([]KZGCommitment, len(commitments)),
		KZGProofs:      make([]KZGProof, len(col.Proofs)),
	}
	copy(sidecar.Column, col.Cells)
	copy(sidecar.KZGCommitments, commitments)
	copy(sidecar.KZGProofs, col.Proofs)
	return sidecar
}

// --- internal helpers ---

// extractCell extracts a cell from a blob at the given column index.
// If the blob is shorter than expected, the cell is zero-padded.
func extractCell(blob []byte, colIdx, cellsPerBlob, cellSize int) Cell {
	var cell Cell
	if cellsPerBlob <= 0 || cellSize <= 0 {
		return cell
	}

	offset := colIdx * cellSize
	if offset >= len(blob) {
		return cell // zero cell
	}

	end := offset + cellSize
	if end > len(blob) {
		end = len(blob)
	}
	copy(cell[:], blob[offset:end])
	return cell
}

// computeCellProof computes a simulated KZG proof for a cell.
// In production, this would use the actual KZG library.
func computeCellProof(blob []byte, blobIdx, colIdx int) KZGProof {
	h := sha3.NewLegacyKeccak256()
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[:8], uint64(blobIdx))
	binary.LittleEndian.PutUint64(buf[8:], uint64(colIdx))
	h.Write(buf[:])
	if len(blob) > 0 {
		h.Write(blob)
	}
	digest := h.Sum(nil)
	var proof KZGProof
	copy(proof[:], digest)
	return proof
}

// computeColumnCommitment computes a keccak256 commitment over all cells
// in a column. This stands in for KZG commitment in the simplified model.
func computeColumnCommitment(col *BuiltColumn) [32]byte {
	h := sha3.NewLegacyKeccak256()
	var idxBuf [8]byte
	binary.LittleEndian.PutUint64(idxBuf[:], uint64(col.Index))
	h.Write(idxBuf[:])
	for _, cell := range col.Cells {
		h.Write(cell[:])
	}
	var commitment [32]byte
	copy(commitment[:], h.Sum(nil))
	return commitment
}

// computeGossipMessageHash computes a deduplication hash for a gossip message.
func computeGossipMessageHash(msg *ColumnGossipMessage) [32]byte {
	h := sha3.NewLegacyKeccak256()
	var buf [24]byte
	binary.LittleEndian.PutUint64(buf[:8], msg.Slot)
	binary.LittleEndian.PutUint64(buf[8:16], msg.BlobIndex)
	binary.LittleEndian.PutUint64(buf[16:], uint64(msg.ColumnIndex))
	h.Write(buf[:])
	h.Write(msg.CellData)
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}
