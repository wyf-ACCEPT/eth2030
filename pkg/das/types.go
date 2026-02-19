// Package das implements PeerDAS (Peer Data Availability Sampling) data
// structures and verification logic per EIP-7594 and the Fulu DAS core spec.
package das

// PeerDAS constants from the Fulu consensus spec.
const (
	// NumberOfColumns is the number of columns in the extended data matrix.
	// Equal to CELLS_PER_EXT_BLOB.
	NumberOfColumns = 128

	// NumberOfCustodyGroups is the number of custody groups available for
	// nodes to custody.
	NumberOfCustodyGroups = 128

	// CustodyRequirement is the minimum number of custody groups an honest
	// node custodies and serves samples from.
	CustodyRequirement = 4

	// SamplesPerSlot is the minimum number of samples for an honest node.
	SamplesPerSlot = 8

	// DataColumnSidecarSubnetCount is the number of subnets used to gossip
	// data column sidecars.
	DataColumnSidecarSubnetCount = 64

	// FieldElementsPerBlob is the number of field elements in a blob (4096).
	FieldElementsPerBlob = 4096

	// FieldElementsPerExtBlob is the number of field elements in an extended blob.
	FieldElementsPerExtBlob = 2 * FieldElementsPerBlob

	// FieldElementsPerCell is the number of field elements in a single cell.
	FieldElementsPerCell = 64

	// BytesPerFieldElement is the byte size of a BLS scalar field element.
	BytesPerFieldElement = 32

	// BytesPerCell is the byte size of a single cell.
	BytesPerCell = FieldElementsPerCell * BytesPerFieldElement // 2048

	// CellsPerExtBlob is the number of cells in an extended blob.
	CellsPerExtBlob = FieldElementsPerExtBlob / FieldElementsPerCell // 128

	// MaxBlobCommitmentsPerBlock is the maximum number of blob commitments
	// per block (EIP-7691 increase).
	MaxBlobCommitmentsPerBlock = 9

	// ReconstructionThreshold is the minimum fraction of columns needed
	// for reconstruction (50%).
	ReconstructionThreshold = NumberOfColumns / 2
)

// SubnetID identifies a data column sidecar gossip subnet.
type SubnetID uint64

// CustodyGroup identifies a custody group.
type CustodyGroup uint64

// ColumnIndex identifies a column in the extended data matrix.
type ColumnIndex uint64

// RowIndex identifies a row (blob) in the extended data matrix.
type RowIndex uint64

// Cell is the smallest unit of blob data that can come with its own KZG proof.
// It contains FIELD_ELEMENTS_PER_CELL field elements.
type Cell [BytesPerCell]byte

// KZGCommitment is a 48-byte compressed BLS12-381 G1 point representing a
// polynomial commitment.
type KZGCommitment [48]byte

// KZGProof is a 48-byte compressed BLS12-381 G1 point representing a KZG proof.
type KZGProof [48]byte

// DataColumn holds the cells for a single column index across all blobs in a block.
type DataColumn struct {
	Index     ColumnIndex
	Cells     []Cell
	KZGProofs []KZGProof
}

// DataColumnSidecar is the network-level container for a data column, including
// the KZG commitments and an inclusion proof as defined in the Fulu DAS spec.
type DataColumnSidecar struct {
	// Index is the column index in [0, NUMBER_OF_COLUMNS).
	Index ColumnIndex

	// Column contains one cell per blob in the block.
	Column []Cell

	// KZGCommitments contains one commitment per blob in the block.
	KZGCommitments []KZGCommitment

	// KZGProofs contains one proof per blob in the block.
	KZGProofs []KZGProof

	// SignedBlockHeader is omitted here as it depends on CL types.
	// In a full implementation this would reference the signed beacon block header.

	// InclusionProof is the Merkle proof for KZG commitments inclusion.
	InclusionProof [][32]byte
}

// MatrixEntry represents a single cell in the extended data matrix along with
// its proof and position.
type MatrixEntry struct {
	Cell        Cell
	KZGProof    KZGProof
	ColumnIndex ColumnIndex
	RowIndex    RowIndex
}
