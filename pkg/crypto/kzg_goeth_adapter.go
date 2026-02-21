//go:build goethkzg

// Real go-eth-kzg adapter for production KZG operations.
//
// This file provides GoEthKZGRealBackend, which wraps the crate-crypto/go-eth-kzg
// library to implement the KZGCeremonyBackend interface using the real Ethereum
// KZG trusted setup from the ceremony.
//
// Build with: go build -tags goethkzg ./...
// Test with:  go test -tags goethkzg -v ./crypto/ -run GoEthKZG
package crypto

import (
	"errors"
	"fmt"

	goethkzg "github.com/crate-crypto/go-eth-kzg"
)

// GoEthKZGRealBackend wraps a go-eth-kzg Context to provide production-grade
// KZG polynomial commitment operations using the real Ethereum ceremony SRS.
// It implements the KZGCeremonyBackend interface.
type GoEthKZGRealBackend struct {
	ctx *goethkzg.Context
}

// Compile-time interface check.
var _ KZGCeremonyBackend = (*GoEthKZGRealBackend)(nil)

// NewGoEthKZGRealBackend creates a new GoEthKZGRealBackend by initializing
// a go-eth-kzg Context with the embedded Ethereum KZG ceremony trusted setup.
// This operation takes ~2-5 seconds as it processes the SRS points.
func NewGoEthKZGRealBackend() (*GoEthKZGRealBackend, error) {
	ctx, err := goethkzg.NewContext4096Secure()
	if err != nil {
		return nil, fmt.Errorf("kzg: failed to initialize go-eth-kzg context: %w", err)
	}
	return &GoEthKZGRealBackend{ctx: ctx}, nil
}

// Name returns a human-readable identifier for this backend.
func (b *GoEthKZGRealBackend) Name() string {
	return "go-eth-kzg-real"
}

// BlobToCommitment computes a KZG commitment for a blob using the real
// Ethereum ceremony SRS. The blob must be exactly KZGBytesPerBlob (131072) bytes.
// Each 32-byte field element within the blob must be a canonical BLS scalar
// (less than BLS_MODULUS).
func (b *GoEthKZGRealBackend) BlobToCommitment(blob []byte) ([KZGBytesPerCommitment]byte, error) {
	var out [KZGBytesPerCommitment]byte
	if len(blob) != KZGBytesPerBlob {
		return out, ErrKZGInvalidBlobSize
	}

	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)

	comm, err := b.ctx.BlobToKZGCommitment(&blobArr, 0)
	if err != nil {
		return out, fmt.Errorf("kzg: BlobToKZGCommitment failed: %w", err)
	}

	// goethkzg.KZGCommitment is [48]byte (via G1Point alias).
	out = [KZGBytesPerCommitment]byte(comm)
	return out, nil
}

// VerifyBlobProof verifies a KZG blob proof against a commitment using the
// real Ethereum ceremony SRS. Returns (true, nil) if valid, (false, err)
// if the proof is invalid or inputs are malformed.
func (b *GoEthKZGRealBackend) VerifyBlobProof(blob, commitment, proof []byte) (bool, error) {
	if len(blob) != KZGBytesPerBlob {
		return false, ErrKZGInvalidBlobSize
	}
	if len(commitment) != KZGBytesPerCommitment {
		return false, ErrKZGInvalidCommitmentSize
	}
	if len(proof) != KZGBytesPerProof {
		return false, ErrKZGInvalidProofSize
	}

	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)

	var comm goethkzg.KZGCommitment
	copy(comm[:], commitment)

	var p goethkzg.KZGProof
	copy(p[:], proof)

	err := b.ctx.VerifyBlobKZGProof(&blobArr, comm, p)
	if err != nil {
		return false, err
	}
	return true, nil
}

// ComputeCells computes the extended blob cells for PeerDAS (EIP-7594).
// The blob is expanded via Reed-Solomon encoding into CellsPerExtBlob (128)
// cells, each KZGBytesPerCell (2048) bytes.
func (b *GoEthKZGRealBackend) ComputeCells(blob []byte) ([][KZGBytesPerCell]byte, error) {
	if len(blob) != KZGBytesPerBlob {
		return nil, ErrKZGInvalidBlobSize
	}

	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)

	cellPtrs, err := b.ctx.ComputeCells(&blobArr, 0)
	if err != nil {
		return nil, fmt.Errorf("kzg: ComputeCells failed: %w", err)
	}

	// Convert [CellsPerExtBlob]*Cell to [][KZGBytesPerCell]byte.
	cells := make([][KZGBytesPerCell]byte, goethkzg.CellsPerExtBlob)
	for i, c := range cellPtrs {
		if c == nil {
			return nil, fmt.Errorf("kzg: ComputeCells returned nil cell at index %d", i)
		}
		cells[i] = [KZGBytesPerCell]byte(*c)
	}
	return cells, nil
}

// VerifyCellProof verifies a KZG proof for a single cell against a commitment
// using the batch cell verification API. This is the EIP-7594 cell-level proof
// verification used in PeerDAS.
func (b *GoEthKZGRealBackend) VerifyCellProof(commitment, cell, proof []byte, cellIndex uint64) (bool, error) {
	if cellIndex >= KZGCellsPerExtBlob {
		return false, ErrKZGInvalidCellIndex
	}
	if len(commitment) != KZGBytesPerCommitment {
		return false, ErrKZGInvalidCommitmentSize
	}
	if len(cell) != KZGBytesPerCell {
		return false, errors.New("kzg: invalid cell size")
	}
	if len(proof) != KZGBytesPerProof {
		return false, ErrKZGInvalidProofSize
	}

	var comm goethkzg.KZGCommitment
	copy(comm[:], commitment)

	var c goethkzg.Cell
	copy(c[:], cell)

	var p goethkzg.KZGProof
	copy(p[:], proof)

	err := b.ctx.VerifyCellKZGProofBatch(
		[]goethkzg.KZGCommitment{comm},
		[]uint64{cellIndex},
		[]*goethkzg.Cell{&c},
		[]goethkzg.KZGProof{p},
	)
	if err != nil {
		return false, err
	}
	return true, nil
}

// --- Additional methods beyond the KZGCeremonyBackend interface ---

// ComputeBlobProof computes a KZG proof for a blob given its commitment.
// This is the EIP-4844 blob proof computation used for on-chain verification.
func (b *GoEthKZGRealBackend) ComputeBlobProof(blob []byte, commitment [KZGBytesPerCommitment]byte) ([KZGBytesPerProof]byte, error) {
	var out [KZGBytesPerProof]byte
	if len(blob) != KZGBytesPerBlob {
		return out, ErrKZGInvalidBlobSize
	}

	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)

	comm := goethkzg.KZGCommitment(commitment)

	proof, err := b.ctx.ComputeBlobKZGProof(&blobArr, comm, 0)
	if err != nil {
		return out, fmt.Errorf("kzg: ComputeBlobKZGProof failed: %w", err)
	}

	out = [KZGBytesPerProof]byte(proof)
	return out, nil
}

// VerifyBlobProofBatch verifies a batch of blob proofs against their
// commitments. This is more efficient than verifying each proof individually.
func (b *GoEthKZGRealBackend) VerifyBlobProofBatch(blobs [][]byte, commitments, proofs [][KZGBytesPerCommitment]byte) (bool, error) {
	n := len(blobs)
	if n != len(commitments) || n != len(proofs) {
		return false, errors.New("kzg: batch length mismatch")
	}

	blobPtrs := make([]*goethkzg.Blob, n)
	comms := make([]goethkzg.KZGCommitment, n)
	kzgProofs := make([]goethkzg.KZGProof, n)

	for i := 0; i < n; i++ {
		if len(blobs[i]) != KZGBytesPerBlob {
			return false, fmt.Errorf("kzg: blob %d has invalid size %d", i, len(blobs[i]))
		}
		blobPtrs[i] = new(goethkzg.Blob)
		copy(blobPtrs[i][:], blobs[i])
		comms[i] = goethkzg.KZGCommitment(commitments[i])
		kzgProofs[i] = goethkzg.KZGProof(proofs[i])
	}

	err := b.ctx.VerifyBlobKZGProofBatch(blobPtrs, comms, kzgProofs)
	if err != nil {
		return false, err
	}
	return true, nil
}

// ComputeCellsAndProofs computes both the extended blob cells and their
// corresponding KZG proofs for PeerDAS (EIP-7594). This is more efficient
// than calling ComputeCells and computing proofs separately.
func (b *GoEthKZGRealBackend) ComputeCellsAndProofs(blob []byte) ([][KZGBytesPerCell]byte, [][KZGBytesPerProof]byte, error) {
	if len(blob) != KZGBytesPerBlob {
		return nil, nil, ErrKZGInvalidBlobSize
	}

	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)

	cellPtrs, kzgProofs, err := b.ctx.ComputeCellsAndKZGProofs(&blobArr, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("kzg: ComputeCellsAndKZGProofs failed: %w", err)
	}

	cells := make([][KZGBytesPerCell]byte, goethkzg.CellsPerExtBlob)
	for i, c := range cellPtrs {
		if c == nil {
			return nil, nil, fmt.Errorf("kzg: ComputeCellsAndKZGProofs returned nil cell at index %d", i)
		}
		cells[i] = [KZGBytesPerCell]byte(*c)
	}

	proofs := make([][KZGBytesPerProof]byte, goethkzg.CellsPerExtBlob)
	for i, p := range kzgProofs {
		proofs[i] = [KZGBytesPerProof]byte(p)
	}

	return cells, proofs, nil
}

// RecoverCells recovers the full set of extended blob cells from a subset
// of at least 64 cells (50% of CellsPerExtBlob=128). The cellIDs must be
// in ascending order and each must be < CellsPerExtBlob.
func (b *GoEthKZGRealBackend) RecoverCells(cellIDs []uint64, cells [][KZGBytesPerCell]byte) ([][KZGBytesPerCell]byte, error) {
	if len(cellIDs) != len(cells) {
		return nil, errors.New("kzg: cellIDs and cells length mismatch")
	}

	// Convert [][KZGBytesPerCell]byte to []*goethkzg.Cell.
	cellPtrs := make([]*goethkzg.Cell, len(cells))
	for i := range cells {
		c := goethkzg.Cell(cells[i])
		cellPtrs[i] = &c
	}

	recoveredPtrs, err := b.ctx.RecoverCells(cellIDs, cellPtrs, 0)
	if err != nil {
		return nil, fmt.Errorf("kzg: RecoverCells failed: %w", err)
	}

	result := make([][KZGBytesPerCell]byte, goethkzg.CellsPerExtBlob)
	for i, c := range recoveredPtrs {
		if c == nil {
			return nil, fmt.Errorf("kzg: RecoverCells returned nil cell at index %d", i)
		}
		result[i] = [KZGBytesPerCell]byte(*c)
	}
	return result, nil
}
