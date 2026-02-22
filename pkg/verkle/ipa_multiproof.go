// Multi-point IPA proof for Verkle trees.
//
// This file implements the Verkle multi-point opening protocol, which
// aggregates multiple single-point IPA proofs into a single proof via
// random linear combination (Fiat-Shamir). This is the proof format
// used in EIP-6800 execution witnesses.

package verkle

import (
	"crypto/sha256"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/crypto"
)

// VerkleMultiProof contains a multi-point opening proof for multiple
// keys in the Verkle tree. It aggregates individual IPA proofs into a
// single verification using random linear combination.
type VerkleMultiProof struct {
	// D is the helper commitment used in the aggregation.
	D Commitment

	// IPAData is the aggregated IPA proof.
	IPAData *crypto.IPAProofData

	// Commitments are the node commitments along the paths.
	Commitments []Commitment

	// EvalPoints are the evaluation domain indices for each opening.
	EvalPoints []FieldElement

	// EvalResults are the claimed evaluation values.
	EvalResults []FieldElement
}

// VerkleProofMulti generates a multi-point proof for multiple keys in
// the tree. It collects the relevant commitments and evaluations, then
// aggregates them into a single IPA proof.
//
// Parameters:
//   - cfg: the IPA configuration
//   - tree: the Verkle tree to prove against
//   - keys: the keys to prove (each 32 bytes)
//
// Returns the aggregated proof and the values at each key.
func VerkleProofMulti(cfg *IPAConfig, tree *Tree, keys [][KeySize]byte) (*VerkleMultiProof, [][ValueSize]byte, error) {
	if len(keys) == 0 {
		return nil, nil, errors.New("verkle/multiproof: no keys")
	}

	openings, resultValues := collectOpenings(tree, keys)
	if len(openings) == 0 {
		return nil, nil, errors.New("verkle/multiproof: no openings collected")
	}

	// Generate a random challenge for aggregation via Fiat-Shamir.
	r := computeAggChallenge(openings)

	// Aggregate polynomials: h(X) = sum(r^i * f_i(X))
	aggPoly := make([]FieldElement, cfg.DomainSize)
	for i := range aggPoly {
		aggPoly[i] = Zero()
	}

	rPower := One()
	aggEvalResult := Zero()
	for _, op := range openings {
		for j := 0; j < cfg.DomainSize; j++ {
			if j < len(op.polynomial) {
				aggPoly[j] = aggPoly[j].Add(rPower.Mul(op.polynomial[j]))
			}
		}
		aggEvalResult = aggEvalResult.Add(rPower.Mul(op.evalResult))
		rPower = rPower.Mul(r)
	}

	// Use the first opening's eval point for the aggregated proof.
	aggEvalPoint := openings[0].evalPoint

	// Compute the aggregated commitment and IPA proof.
	aInts := fieldSliceToBig(aggPoly)
	bVec := buildEvalVector(aggEvalPoint, cfg.DomainSize)
	bInts := fieldSliceToBig(bVec)
	commitPt := crypto.BanderMSM(cfg.Generators, aInts)

	ipaProof, _, err := crypto.IPAProve(cfg.Generators, aInts, bInts, commitPt)
	if err != nil {
		return nil, nil, err
	}

	// Collect all node commitments.
	commitments := make([]Commitment, len(openings))
	evalPts := make([]FieldElement, len(openings))
	evalRes := make([]FieldElement, len(openings))
	for i, op := range openings {
		commitments[i] = op.nodeCommit
		evalPts[i] = op.evalPoint
		evalRes[i] = op.evalResult
	}

	proof := &VerkleMultiProof{
		D:           commitmentFromPoint(commitPt),
		IPAData:     ipaProof,
		Commitments: commitments,
		EvalPoints:  evalPts,
		EvalResults: evalRes,
	}
	return proof, resultValues, nil
}

// VerkleVerifyMulti verifies a multi-point proof against the tree root.
func VerkleVerifyMulti(cfg *IPAConfig, root Commitment, proof *VerkleMultiProof, keys [][KeySize]byte, values [][ValueSize]byte) bool {
	if proof == nil || proof.IPAData == nil {
		return false
	}
	if len(keys) != len(values) {
		return false
	}
	if len(keys) != len(proof.EvalPoints) || len(keys) != len(proof.EvalResults) {
		return false
	}
	if len(proof.Commitments) != len(keys) {
		return false
	}

	// Recompute the aggregation challenge.
	r := recomputeAggChallenge(proof)

	// Recompute aggregated evaluation result.
	aggEvalResult := Zero()
	rPower := One()
	for _, ep := range proof.EvalResults {
		aggEvalResult = aggEvalResult.Add(rPower.Mul(ep))
		rPower = rPower.Mul(r)
	}

	// Verify the aggregated IPA proof.
	aggEvalPoint := proof.EvalPoints[0]
	bVec := buildEvalVector(aggEvalPoint, cfg.DomainSize)
	bInts := fieldSliceToBig(bVec)

	// Reconstruct the commitment point from the D field.
	dScalar := new(big.Int).SetBytes(proof.D[:])
	commitPt := crypto.BanderScalarMul(crypto.BanderGenerator(), dScalar)

	ok, err := crypto.IPAVerify(cfg.Generators, commitPt, bInts, aggEvalResult.BigInt(), proof.IPAData)
	if err != nil {
		return false
	}
	return ok
}

// --- Internal helpers for multi-proof ---

// multiOpening holds per-key data for the multi-proof aggregation.
type multiOpening struct {
	nodeCommit Commitment
	evalPoint  FieldElement
	evalResult FieldElement
	polynomial []FieldElement
}

// collectOpenings traverses the tree for each key and extracts the
// leaf polynomial, commitment, eval point (suffix), and value.
func collectOpenings(tree *Tree, keys [][KeySize]byte) ([]multiOpening, [][ValueSize]byte) {
	var openings []multiOpening
	var resultValues [][ValueSize]byte

	for _, key := range keys {
		stem, suffix := splitKey(key)
		leaf := tree.getLeaf(stem)

		var val [ValueSize]byte
		if leaf != nil {
			v := leaf.Get(suffix)
			if v != nil {
				val = *v
			}
		}
		resultValues = append(resultValues, val)

		// Build the leaf polynomial (the committed vector).
		poly := make([]FieldElement, NodeWidth)
		for idx := range poly {
			poly[idx] = Zero()
		}
		if leaf != nil {
			poly[0] = FieldElementFromBytes(leaf.stem[:])
			for i := 0; i < NodeWidth-1; i++ {
				lv := leaf.Get(byte(i))
				if lv != nil {
					poly[i+1] = FieldElementFromBytes(lv[:])
				}
			}
		}

		c := Commitment{}
		if leaf != nil {
			c = leaf.Commit()
		}

		openings = append(openings, multiOpening{
			nodeCommit: c,
			evalPoint:  FieldElementFromUint64(uint64(suffix)),
			evalResult: FieldElementFromBytes(val[:]),
			polynomial: poly,
		})
	}
	return openings, resultValues
}

// computeAggChallenge derives the Fiat-Shamir aggregation challenge
// from the openings data.
func computeAggChallenge(openings []multiOpening) FieldElement {
	transcript := sha256.New()
	transcript.Write([]byte("verkle_multiproof"))
	for _, op := range openings {
		transcript.Write(op.nodeCommit[:])
		b := op.evalPoint.Bytes()
		transcript.Write(b[:])
		b = op.evalResult.Bytes()
		transcript.Write(b[:])
	}
	rBytes := transcript.Sum(nil)
	r := FieldElementFromBytes(rBytes)
	if r.IsZero() {
		r = One()
	}
	return r
}

// recomputeAggChallenge re-derives the aggregation challenge from a proof
// during verification.
func recomputeAggChallenge(proof *VerkleMultiProof) FieldElement {
	transcript := sha256.New()
	transcript.Write([]byte("verkle_multiproof"))
	for i := range proof.Commitments {
		transcript.Write(proof.Commitments[i][:])
		b := proof.EvalPoints[i].Bytes()
		transcript.Write(b[:])
		b = proof.EvalResults[i].Bytes()
		transcript.Write(b[:])
	}
	rBytes := transcript.Sum(nil)
	r := FieldElementFromBytes(rBytes)
	if r.IsZero() {
		r = One()
	}
	return r
}
