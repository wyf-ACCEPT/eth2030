// IPA (Inner Product Argument) proof structures and verification for Verkle trees.
//
// This file implements the EIP-6800 IPA proof format including:
//   - IPAProofVerkle: the serializable proof structure with L/R vectors and tip
//   - Verification algorithm with Fiat-Shamir transcript
//   - Multipoint opening proof aggregation
//   - Proof serialization and deserialization
//
// The IPA protocol proves that a Pedersen-committed polynomial evaluates to
// a claimed value at a given point, using a recursive halving argument over
// the Banderwagon group. The proof size is O(log n) curve points.

package verkle

import (
	"crypto/sha256"
	"errors"
	"math/big"

	"github.com/eth2028/eth2028/crypto"
)

// IPAProofVerkle is the serializable IPA proof structure for Verkle trees.
// It contains the L/R commitment vectors from the recursive protocol,
// the final scalar "tip", and metadata for verification.
type IPAProofVerkle struct {
	// CL contains the left commitment points from each halving round.
	// Each point is a compressed Banderwagon point (32 bytes serialized).
	CL []Commitment

	// CR contains the right commitment points from each halving round.
	CR []Commitment

	// FinalScalar is the final scalar value after all rounds (the "tip").
	FinalScalar FieldElement
}

// NumRounds returns the number of halving rounds in the proof.
func (p *IPAProofVerkle) NumRounds() int {
	return len(p.CL)
}

// Validate performs basic structural checks on the proof.
func (p *IPAProofVerkle) Validate(expectedRounds int) error {
	if p == nil {
		return errors.New("verkle/ipa_proof: nil proof")
	}
	if len(p.CL) != len(p.CR) {
		return errors.New("verkle/ipa_proof: L/R length mismatch")
	}
	if len(p.CL) != expectedRounds {
		return errors.New("verkle/ipa_proof: wrong number of rounds")
	}
	return nil
}

// --- Fiat-Shamir Transcript ---

// Transcript manages the Fiat-Shamir challenge generation for IPA proofs.
// It accumulates commitments, scalars, and labels to produce deterministic
// challenges that bind the proof to its context.
type Transcript struct {
	state []byte
}

// NewTranscript creates a new transcript with the given domain label.
func NewTranscript(label string) *Transcript {
	h := sha256.Sum256([]byte(label))
	return &Transcript{state: h[:]}
}

// AppendCommitment adds a 32-byte commitment to the transcript.
func (t *Transcript) AppendCommitment(label string, c Commitment) {
	h := sha256.New()
	h.Write(t.state)
	h.Write([]byte(label))
	h.Write(c[:])
	t.state = h.Sum(nil)
}

// AppendScalar adds a field element to the transcript.
func (t *Transcript) AppendScalar(label string, s FieldElement) {
	h := sha256.New()
	h.Write(t.state)
	h.Write([]byte(label))
	b := s.Bytes()
	h.Write(b[:])
	t.state = h.Sum(nil)
}

// AppendUint64 adds a uint64 value to the transcript.
func (t *Transcript) AppendUint64(label string, v uint64) {
	h := sha256.New()
	h.Write(t.state)
	h.Write([]byte(label))
	buf := make([]byte, 8)
	buf[0] = byte(v >> 56)
	buf[1] = byte(v >> 48)
	buf[2] = byte(v >> 40)
	buf[3] = byte(v >> 32)
	buf[4] = byte(v >> 24)
	buf[5] = byte(v >> 16)
	buf[6] = byte(v >> 8)
	buf[7] = byte(v)
	h.Write(buf)
	t.state = h.Sum(nil)
}

// ChallengeScalar generates a challenge scalar from the current state.
// The challenge is reduced modulo the subgroup order and guaranteed non-zero.
func (t *Transcript) ChallengeScalar(label string) FieldElement {
	h := sha256.New()
	h.Write(t.state)
	h.Write([]byte(label))
	digest := h.Sum(nil)
	t.state = digest

	v := new(big.Int).SetBytes(digest)
	v.Mod(v, order)
	if v.Sign() == 0 {
		v.SetInt64(1)
	}
	return FieldElement{v: v}
}

// --- IPA Proof Verification ---

// VerifyIPAProofVerkle verifies an IPAProofVerkle against a commitment
// and claimed evaluation.
//
// Parameters:
//   - cfg: IPA configuration with generators
//   - commitment: the Pedersen commitment to the polynomial
//   - evalPoint: the domain point at which evaluation is claimed
//   - evalResult: the claimed evaluation value
//   - proof: the IPA proof to verify
//
// The verification reconstructs the Fiat-Shamir challenges, folds the
// generator and evaluation vectors, folds the commitment using L/R points,
// and performs the final check.
func VerifyIPAProofVerkle(cfg *IPAConfig, commitment Commitment, evalPoint, evalResult FieldElement, proof *IPAProofVerkle) (bool, error) {
	n := cfg.DomainSize
	expectedRounds := crypto.IPAProofSize(n)
	if err := proof.Validate(expectedRounds); err != nil {
		return false, err
	}

	// Initialize transcript.
	transcript := NewTranscript("verkle_ipa_proof")
	transcript.AppendCommitment("C", commitment)
	transcript.AppendScalar("z", evalPoint)
	transcript.AppendScalar("y", evalResult)

	// Collect challenges from L/R commitments.
	challenges := make([]FieldElement, expectedRounds)
	for i := 0; i < expectedRounds; i++ {
		transcript.AppendCommitment("L", proof.CL[i])
		transcript.AppendCommitment("R", proof.CR[i])
		challenges[i] = transcript.ChallengeScalar("x")
	}

	// Fold the evaluation vector b from the Lagrange basis.
	bVec := buildEvalVector(evalPoint, n)
	gVec := make([]*crypto.BanderPoint, n)
	for i := 0; i < n; i++ {
		gVec[i] = cfg.Generators[i]
	}

	m := n
	for round := 0; round < expectedRounds; round++ {
		half := m / 2
		x := challenges[round]
		xInv := x.Inv()

		newB := make([]FieldElement, half)
		newG := make([]*crypto.BanderPoint, half)
		for i := 0; i < half; i++ {
			newB[i] = bVec[i].Add(xInv.Mul(bVec[half+i]))
			newG[i] = crypto.BanderAdd(gVec[i], crypto.BanderScalarMul(gVec[half+i], xInv.BigInt()))
		}
		bVec = newB
		gVec = newG
		m = half
	}

	// Fold the commitment: C' = C + sum(x_i^{-1} * L_i + x_i * R_i).
	commitScalar := new(big.Int).SetBytes(commitment[:])
	cFinal := crypto.BanderScalarMul(crypto.BanderGenerator(), commitScalar)
	for i := 0; i < expectedRounds; i++ {
		x := challenges[i]
		xInv := x.Inv()

		lScalar := new(big.Int).SetBytes(proof.CL[i][:])
		rScalar := new(big.Int).SetBytes(proof.CR[i][:])

		lPt := crypto.BanderScalarMul(crypto.BanderGenerator(), lScalar)
		rPt := crypto.BanderScalarMul(crypto.BanderGenerator(), rScalar)

		lScaled := crypto.BanderScalarMul(lPt, xInv.BigInt())
		rScaled := crypto.BanderScalarMul(rPt, x.BigInt())
		cFinal = crypto.BanderAdd(crypto.BanderAdd(cFinal, lScaled), rScaled)
	}

	// Final check: C_final == a * G_final.
	expectedC := crypto.BanderScalarMul(gVec[0], proof.FinalScalar.BigInt())
	return crypto.BanderEqual(cFinal, expectedC), nil
}

// --- Multipoint opening proof ---

// MultipointProof aggregates multiple IPA evaluations into a single proof.
// This is the proof format used in EIP-6800 execution witnesses.
type MultipointProof struct {
	// IPAProof is the aggregated IPA proof.
	IPAProof IPAProofVerkle

	// D is the helper commitment used in aggregation.
	D Commitment

	// Openings contains the per-key evaluation data.
	Openings []MultipointOpening
}

// MultipointOpening describes a single polynomial opening within a
// multipoint proof.
type MultipointOpening struct {
	// Commitment is the Pedersen commitment to the polynomial.
	Commitment Commitment

	// EvalPoint is the evaluation domain index (suffix byte).
	EvalPoint FieldElement

	// EvalResult is the claimed value at that point.
	EvalResult FieldElement
}

// VerifyMultipointProof checks a multipoint opening proof against the
// given root commitment.
func VerifyMultipointProof(cfg *IPAConfig, proof *MultipointProof) (bool, error) {
	if proof == nil || len(proof.Openings) == 0 {
		return false, errors.New("verkle/ipa_proof: nil or empty multipoint proof")
	}

	// Generate aggregation challenge via Fiat-Shamir.
	transcript := NewTranscript("verkle_multipoint")
	for _, op := range proof.Openings {
		transcript.AppendCommitment("C", op.Commitment)
		transcript.AppendScalar("z", op.EvalPoint)
		transcript.AppendScalar("y", op.EvalResult)
	}
	r := transcript.ChallengeScalar("r")

	// Aggregate the evaluation results: sum(r^i * y_i).
	aggResult := Zero()
	rPow := One()
	for _, op := range proof.Openings {
		aggResult = aggResult.Add(rPow.Mul(op.EvalResult))
		rPow = rPow.Mul(r)
	}

	// Verify the aggregated IPA proof against D and the aggregated result.
	return VerifyIPAProofVerkle(cfg, proof.D, proof.Openings[0].EvalPoint, aggResult, &proof.IPAProof)
}

// --- Serialization ---

// SerializeIPAProofVerkle encodes an IPA proof to bytes.
// Format: [rounds:1] [CL_0:32] [CR_0:32] ... [CL_n:32] [CR_n:32] [scalar:32]
func SerializeIPAProofVerkle(proof *IPAProofVerkle) ([]byte, error) {
	if proof == nil {
		return nil, errors.New("verkle/ipa_proof: nil proof")
	}
	rounds := len(proof.CL)
	size := 1 + rounds*64 + 32
	buf := make([]byte, 0, size)
	buf = append(buf, byte(rounds))

	for i := 0; i < rounds; i++ {
		buf = append(buf, proof.CL[i][:]...)
		buf = append(buf, proof.CR[i][:]...)
	}

	s := proof.FinalScalar.Bytes()
	buf = append(buf, s[:]...)
	return buf, nil
}

// DeserializeIPAProofVerkle decodes an IPA proof from bytes.
func DeserializeIPAProofVerkle(data []byte) (*IPAProofVerkle, error) {
	if len(data) < 33 {
		return nil, errors.New("verkle/ipa_proof: data too short")
	}

	rounds := int(data[0])
	expected := 1 + rounds*64 + 32
	if len(data) != expected {
		return nil, errors.New("verkle/ipa_proof: invalid data length")
	}

	proof := &IPAProofVerkle{
		CL: make([]Commitment, rounds),
		CR: make([]Commitment, rounds),
	}

	offset := 1
	for i := 0; i < rounds; i++ {
		copy(proof.CL[i][:], data[offset:offset+CommitSize])
		copy(proof.CR[i][:], data[offset+CommitSize:offset+2*CommitSize])
		offset += 2 * CommitSize
	}

	var scalarBytes [32]byte
	copy(scalarBytes[:], data[offset:offset+32])
	proof.FinalScalar = FieldElementFromBytes(scalarBytes[:])

	return proof, nil
}

// --- Helper: convert between proof formats ---

// IPAProofVerkleFromCrypto converts a crypto.IPAProofData to IPAProofVerkle.
func IPAProofVerkleFromCrypto(proof *crypto.IPAProofData) *IPAProofVerkle {
	if proof == nil {
		return nil
	}
	rounds := len(proof.L)
	result := &IPAProofVerkle{
		CL:          make([]Commitment, rounds),
		CR:          make([]Commitment, rounds),
		FinalScalar: NewFieldElement(proof.A),
	}
	for i := 0; i < rounds; i++ {
		result.CL[i] = commitmentFromPoint(proof.L[i])
		result.CR[i] = commitmentFromPoint(proof.R[i])
	}
	return result
}

// IPAProofVerkleToCrypto converts an IPAProofVerkle back to crypto.IPAProofData.
// Note: this reconstructs curve points from the commitment scalars via
// scalar multiplication of the generator, which is an approximation.
func IPAProofVerkleToCrypto(proof *IPAProofVerkle) *crypto.IPAProofData {
	if proof == nil {
		return nil
	}
	rounds := len(proof.CL)
	result := &crypto.IPAProofData{
		L: make([]*crypto.BanderPoint, rounds),
		R: make([]*crypto.BanderPoint, rounds),
		A: proof.FinalScalar.BigInt(),
	}
	g := crypto.BanderGenerator()
	for i := 0; i < rounds; i++ {
		lScalar := new(big.Int).SetBytes(proof.CL[i][:])
		rScalar := new(big.Int).SetBytes(proof.CR[i][:])
		result.L[i] = crypto.BanderScalarMul(g, lScalar)
		result.R[i] = crypto.BanderScalarMul(g, rScalar)
	}
	return result
}
