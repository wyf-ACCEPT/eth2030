// batch_blob_verify.go implements batch verification for post-quantum lattice
// blob commitments. Batch verification uses a random linear combination
// technique to verify multiple commitment-opening pairs simultaneously,
// amortizing the verification cost.
//
// When a batch fails, the verifier performs failure isolation to identify
// which specific commitment(s) are invalid.
package pqc

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

// Batch verification errors.
var (
	ErrBatchVerifyEmpty    = errors.New("batch_verify: empty batch")
	ErrBatchVerifyMismatch = errors.New("batch_verify: length mismatch")
	ErrBatchVerifyNilEntry = errors.New("batch_verify: nil entry in batch")
)

// BatchVerifyResult contains the result of a batch verification.
type BatchVerifyResult struct {
	// Valid is true if the entire batch verified successfully.
	Valid bool

	// FailedIndices lists the indices of failed commitments (only populated
	// when Valid is false and failure isolation was performed).
	FailedIndices []int
}

// LatticeBatchVerifier performs batch verification of lattice commitments.
type LatticeBatchVerifier struct {
	scheme *LatticeCommitScheme
}

// NewLatticeBatchVerifier creates a batch verifier for the given scheme.
func NewLatticeBatchVerifier(scheme *LatticeCommitScheme) *LatticeBatchVerifier {
	return &LatticeBatchVerifier{scheme: scheme}
}

// BatchVerify verifies multiple commitments against their openings and data
// using a random linear combination. Returns true if all are valid.
func (bv *LatticeBatchVerifier) BatchVerify(
	commitments []*LatticeCommitment,
	openings []*LatticeOpening,
	blobs [][]byte,
) (bool, error) {
	if len(commitments) == 0 {
		return false, ErrBatchVerifyEmpty
	}
	if len(commitments) != len(openings) || len(commitments) != len(blobs) {
		return false, ErrBatchVerifyMismatch
	}

	// Derive random scalars for the linear combination.
	// The scalars are derived from a hash of all commitments to prevent
	// an adversary from choosing commitments that cancel in the combination.
	scalars := deriveRandomScalars(commitments)

	// Compute the random linear combination of commitments and re-derived values.
	// If sum(r_i * c_i) == sum(r_i * (A*s_i + e_i + m_i)), all are valid.
	for k := 0; k < LatticeK; k++ {
		lhsAcc := NewPoly()
		rhsAcc := NewPoly()

		for i := range commitments {
			if commitments[i] == nil || openings[i] == nil || blobs[i] == nil {
				return false, ErrBatchVerifyNilEntry
			}

			scalar := scalars[i]

			// LHS: r_i * c_i[k]
			lhsTerm := commitments[i].CommitVec[k].ScalarMul(scalar)
			lhsAcc = lhsAcc.Add(lhsTerm)

			// RHS: r_i * (A*s + e + m)[k]
			recomputed := NewPoly()
			for j := 0; j < LatticeK; j++ {
				product := bv.scheme.matrixA[k][j].MulNTT(openings[i].Secret[j])
				recomputed = recomputed.Add(product)
			}
			recomputed = recomputed.Add(openings[i].Error[k])
			recomputed = recomputed.Add(openings[i].MessagePoly[k])
			rhsTerm := recomputed.ScalarMul(scalar)
			rhsAcc = rhsAcc.Add(rhsTerm)
		}

		if !lhsAcc.Equal(rhsAcc) {
			return false, nil
		}
	}
	return true, nil
}

// BatchVerifyWithIsolation verifies a batch and, on failure, identifies
// which specific commitments are invalid.
func (bv *LatticeBatchVerifier) BatchVerifyWithIsolation(
	commitments []*LatticeCommitment,
	openings []*LatticeOpening,
	blobs [][]byte,
) (*BatchVerifyResult, error) {
	valid, err := bv.BatchVerify(commitments, openings, blobs)
	if err != nil {
		return nil, err
	}

	result := &BatchVerifyResult{Valid: valid}
	if valid {
		return result, nil
	}

	// Batch failed: isolate individual failures.
	result.FailedIndices = bv.isolateFailures(commitments, openings, blobs)
	return result, nil
}

// isolateFailures individually verifies each commitment to find failures.
func (bv *LatticeBatchVerifier) isolateFailures(
	commitments []*LatticeCommitment,
	openings []*LatticeOpening,
	blobs [][]byte,
) []int {
	var failed []int
	for i := range commitments {
		if commitments[i] == nil || openings[i] == nil || blobs[i] == nil {
			failed = append(failed, i)
			continue
		}
		if !bv.scheme.Verify(commitments[i], openings[i], blobs[i]) {
			failed = append(failed, i)
		}
	}
	return failed
}

// BatchVerifyBlobs is a convenience function that verifies PQBlobCommitments
// against blob data by recomputing commitments and comparing hashes.
func (bv *LatticeBatchVerifier) BatchVerifyBlobs(
	pqCommits []*PQBlobCommitment,
	blobs [][]byte,
) (bool, error) {
	if len(pqCommits) == 0 {
		return false, ErrBatchVerifyEmpty
	}
	if len(pqCommits) != len(blobs) {
		return false, ErrBatchVerifyMismatch
	}

	for i := range pqCommits {
		if pqCommits[i] == nil || blobs[i] == nil {
			return false, ErrBatchVerifyNilEntry
		}
		if !bv.scheme.VerifyBlob(pqCommits[i], blobs[i]) {
			return false, nil
		}
	}
	return true, nil
}

// BatchVerifyBlobsWithIsolation verifies PQBlobCommitments and identifies
// failing entries.
func (bv *LatticeBatchVerifier) BatchVerifyBlobsWithIsolation(
	pqCommits []*PQBlobCommitment,
	blobs [][]byte,
) (*BatchVerifyResult, error) {
	valid, err := bv.BatchVerifyBlobs(pqCommits, blobs)
	if err != nil {
		return nil, err
	}

	result := &BatchVerifyResult{Valid: valid}
	if valid {
		return result, nil
	}

	// Isolate failures.
	var failed []int
	for i := range pqCommits {
		if pqCommits[i] == nil || blobs[i] == nil || len(blobs[i]) == 0 {
			failed = append(failed, i)
			continue
		}
		if !bv.scheme.VerifyBlob(pqCommits[i], blobs[i]) {
			failed = append(failed, i)
		}
	}
	result.FailedIndices = failed
	return result, nil
}

// deriveRandomScalars generates pseudo-random scalars from a hash of all
// commitments. Each scalar is a small value in [1, q) to ensure the random
// linear combination is non-trivial.
func deriveRandomScalars(commitments []*LatticeCommitment) []int16 {
	// Build a transcript of all commitment hashes.
	h := sha256.New()
	h.Write([]byte("batch-verify-v1"))
	for _, c := range commitments {
		if c != nil {
			h.Write(c.Hash[:])
		}
	}
	seed := h.Sum(nil)

	scalars := make([]int16, len(commitments))
	for i := range commitments {
		// Derive scalar: SHA256(seed || index), take 2 bytes mod (q-1) + 1.
		var idxBuf [4]byte
		binary.BigEndian.PutUint32(idxBuf[:], uint32(i))
		sh := sha256.Sum256(append(seed, idxBuf[:]...))
		val := uint16(sh[0]) | (uint16(sh[1]) << 8)
		scalar := int16(val%uint16(PolyQ-1)) + 1 // in [1, q)
		scalars[i] = scalar
	}
	return scalars
}
