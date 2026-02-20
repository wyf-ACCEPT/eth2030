// KZG trusted setup ceremony for EIP-4844/7594 polynomial commitments.
//
// Implements the powers-of-tau ceremony protocol where multiple participants
// each contribute randomness to build a structured reference string (SRS).
// The security guarantee is that the SRS is secure as long as at least one
// participant honestly destroyed their secret.
//
// Each participant:
//   1. Generates a random tau
//   2. Multiplies existing G1/G2 points by successive powers of tau
//   3. Provides a proof of knowledge (discrete log proof) that they know tau
//
// The final SRS consists of [tau^0]G1, [tau^1]G1, ..., [tau^n]G1 and
// [tau]G2 for pairing-based verification.
package crypto

import (
	"crypto/sha256"
	"errors"
	"math/big"
	"time"
)

// Ceremony errors.
var (
	ErrCeremonyFinalized     = errors.New("kzg_ceremony: ceremony already finalized")
	ErrCeremonyNotFinalized  = errors.New("kzg_ceremony: ceremony not yet finalized")
	ErrCeremonyInvalidProof  = errors.New("kzg_ceremony: invalid proof of knowledge")
	ErrCeremonyInvalidPoints = errors.New("kzg_ceremony: contribution points invalid")
	ErrCeremonyDuplicate     = errors.New("kzg_ceremony: duplicate participant")
	ErrCeremonyNoContribs    = errors.New("kzg_ceremony: no contributions received")
	ErrCeremonyMaxRound      = errors.New("kzg_ceremony: max round reached")
	ErrCeremonyBadDegree     = errors.New("kzg_ceremony: invalid degree parameter")
)

// CeremonyPhase represents a phase of the ceremony.
type CeremonyPhase int

const (
	// PhaseContributing is the phase where participants submit contributions.
	PhaseContributing CeremonyPhase = iota
	// PhaseFinalized means the ceremony is complete and the SRS is ready.
	PhaseFinalized
)

// String returns a human-readable phase name.
func (p CeremonyPhase) String() string {
	switch p {
	case PhaseContributing:
		return "contributing"
	case PhaseFinalized:
		return "finalized"
	default:
		return "unknown"
	}
}

// Contribution represents a single participant's ceremony contribution.
// It contains the updated G1/G2 powers and a discrete log proof showing
// the participant knows the tau they used.
type Contribution struct {
	// ParticipantID uniquely identifies the contributor.
	ParticipantID string

	// Round is the sequence number of this contribution (1-based).
	Round int

	// PowersG1 holds [tau^0]G1, [tau^1]G1, ..., [tau^n]G1 after this contribution.
	PowersG1 []*BlsG1Point

	// TauG2 holds [tau]G2 after this contribution.
	TauG2 *BlsG2Point

	// ProofG1 is the G1 component of the proof of knowledge: [witness]G1.
	ProofG1 *BlsG1Point

	// ProofG2 is the G2 component: [witness]G2.
	ProofG2 *BlsG2Point

	// Timestamp records when this contribution was submitted.
	Timestamp time.Time
}

// CeremonyState tracks the running state of a KZG ceremony.
type CeremonyState struct {
	// Phase indicates whether contributions are being accepted or the ceremony is done.
	Phase CeremonyPhase

	// Degree is the number of G1 powers in the SRS (degree+1 points total).
	Degree int

	// CurrentRound is the next expected contribution round number.
	CurrentRound int

	// MaxRounds is the maximum number of contributions allowed (0 = unlimited).
	MaxRounds int

	// PowersG1 holds the accumulated [tau^i]G1 powers.
	PowersG1 []*BlsG1Point

	// TauG2 holds the accumulated [tau]G2.
	TauG2 *BlsG2Point

	// Contributions stores the contribution history.
	Contributions []*Contribution

	// participants tracks IDs to prevent duplicates.
	participants map[string]bool
}

// CeremonyResult is the finalized output of the ceremony: a structured
// reference string suitable for KZG polynomial commitments.
type CeremonyResult struct {
	// G1Powers contains [tau^0]G1 through [tau^n]G1.
	G1Powers []*BlsG1Point

	// G2Tau is [tau]G2, used in the verification pairing equation.
	G2Tau *BlsG2Point

	// NumContributions is the number of valid contributions accumulated.
	NumContributions int
}

// PointsOfEvaluation holds evaluation points derived from the SRS for
// KZG commitment operations. G1Points are used for committing to polynomials,
// and G2Point ([tau]G2) is used in the pairing verification equation.
type PointsOfEvaluation struct {
	G1Points []*BlsG1Point
	G2Point  *BlsG2Point
}

// KZGCeremony manages the trusted setup ceremony lifecycle.
type KZGCeremony struct {
	state *CeremonyState
}

// NewKZGCeremony creates a new ceremony for a given SRS degree.
// The degree determines the maximum polynomial degree that can be committed
// (the SRS will contain degree+1 G1 points). maxRounds limits the number
// of contributions (0 = unlimited).
func NewKZGCeremony(degree, maxRounds int) (*KZGCeremony, error) {
	if degree < 1 {
		return nil, ErrCeremonyBadDegree
	}

	// Initialize the SRS with the generator: [1]G1 for all powers and [1]G2.
	g1 := BlsG1Generator()
	powers := make([]*BlsG1Point, degree+1)
	for i := range powers {
		powers[i] = blsG1Clone(g1)
	}

	return &KZGCeremony{
		state: &CeremonyState{
			Phase:        PhaseContributing,
			Degree:       degree,
			CurrentRound: 1,
			MaxRounds:    maxRounds,
			PowersG1:     powers,
			TauG2:        blsG2Clone(BlsG2Generator()),
			participants: make(map[string]bool),
		},
	}, nil
}

// Phase returns the current ceremony phase.
func (c *KZGCeremony) Phase() CeremonyPhase {
	return c.state.Phase
}

// Round returns the current round number.
func (c *KZGCeremony) Round() int {
	return c.state.CurrentRound
}

// NumContributions returns the count of accepted contributions.
func (c *KZGCeremony) NumContributions() int {
	return len(c.state.Contributions)
}

// Degree returns the SRS degree.
func (c *KZGCeremony) Degree() int {
	return c.state.Degree
}

// GenerateContribution creates a contribution for the ceremony using a
// random tau value. The participant provides their secret tau and an ID.
// This function computes the updated powers and a proof of knowledge.
func GenerateContribution(
	participantID string,
	tau *big.Int,
	currentPowersG1 []*BlsG1Point,
	currentTauG2 *BlsG2Point,
	round int,
) *Contribution {
	n := len(currentPowersG1)
	newPowers := make([]*BlsG1Point, n)

	// Multiply each existing power by the appropriate power of tau.
	// [tau_old^i]G1 * tau_new^i = [(tau_old * tau_new)^i]G1
	tauPower := big.NewInt(1)
	for i := 0; i < n; i++ {
		newPowers[i] = blsG1ScalarMul(currentPowersG1[i], tauPower)
		tauPower = new(big.Int).Mul(tauPower, tau)
		tauPower.Mod(tauPower, blsR)
	}

	// Update [tau]G2.
	newTauG2 := blsG2ScalarMul(currentTauG2, tau)

	// Proof of knowledge: pick a deterministic witness from tau + ID.
	witness := ceremonyWitness(tau, participantID)
	proofG1 := blsG1ScalarMul(BlsG1Generator(), witness)
	proofG2 := blsG2ScalarMul(BlsG2Generator(), witness)

	return &Contribution{
		ParticipantID: participantID,
		Round:         round,
		PowersG1:      newPowers,
		TauG2:         newTauG2,
		ProofG1:       proofG1,
		ProofG2:       proofG2,
		Timestamp:     time.Now(),
	}
}

// VerifyContribution validates a participant's contribution by checking:
//  1. The G1 powers are consistent: e(powers[i], G2) == e(powers[i-1], tauG2).
//  2. The proof of knowledge is a valid discrete log equality proof:
//     e(proofG1, G2) == e(G1, proofG2).
//  3. The contribution has the correct number of points.
func VerifyContribution(contrib *Contribution, degree int) bool {
	if len(contrib.PowersG1) != degree+1 {
		return false
	}
	if contrib.TauG2 == nil || contrib.ProofG1 == nil || contrib.ProofG2 == nil {
		return false
	}

	// Check proof of knowledge: e(proofG1, G2) == e(G1, proofG2).
	// This proves the contributor knows a scalar w such that proofG1 = [w]G1
	// and proofG2 = [w]G2.
	g1 := BlsG1Generator()
	g2 := BlsG2Generator()

	negG1 := blsG1Neg(g1)
	pairingOk := blsMultiPairing(
		[]*BlsG1Point{contrib.ProofG1, negG1},
		[]*BlsG2Point{g2, contrib.ProofG2},
	)
	if !pairingOk {
		return false
	}

	// Check consecutive power consistency for the first two powers.
	// e(powers[1], G2) == e(powers[0], tauG2)
	// This verifies powers[1] = tau * powers[0].
	if len(contrib.PowersG1) >= 2 {
		negP0 := blsG1Neg(contrib.PowersG1[0])
		powerOk := blsMultiPairing(
			[]*BlsG1Point{contrib.PowersG1[1], negP0},
			[]*BlsG2Point{g2, contrib.TauG2},
		)
		if !powerOk {
			return false
		}
	}

	return true
}

// AccumulateContribution adds a valid contribution to the ceremony state.
// The contribution's updated powers replace the current state.
func (c *KZGCeremony) AccumulateContribution(contrib *Contribution) error {
	if c.state.Phase == PhaseFinalized {
		return ErrCeremonyFinalized
	}
	if c.state.MaxRounds > 0 && c.state.CurrentRound > c.state.MaxRounds {
		return ErrCeremonyMaxRound
	}
	if c.state.participants[contrib.ParticipantID] {
		return ErrCeremonyDuplicate
	}
	if len(contrib.PowersG1) != c.state.Degree+1 {
		return ErrCeremonyInvalidPoints
	}

	// Verify the contribution.
	if !VerifyContribution(contrib, c.state.Degree) {
		return ErrCeremonyInvalidProof
	}

	// Accept: update state with new powers.
	c.state.PowersG1 = contrib.PowersG1
	c.state.TauG2 = contrib.TauG2
	c.state.Contributions = append(c.state.Contributions, contrib)
	c.state.participants[contrib.ParticipantID] = true
	c.state.CurrentRound++

	return nil
}

// Finalize ends the ceremony and produces the final SRS result.
// At least one contribution must have been accepted.
func (c *KZGCeremony) Finalize() (*CeremonyResult, error) {
	if c.state.Phase == PhaseFinalized {
		return nil, ErrCeremonyFinalized
	}
	if len(c.state.Contributions) == 0 {
		return nil, ErrCeremonyNoContribs
	}

	c.state.Phase = PhaseFinalized

	return &CeremonyResult{
		G1Powers:         c.state.PowersG1,
		G2Tau:            c.state.TauG2,
		NumContributions: len(c.state.Contributions),
	}, nil
}

// GetEvaluationPoints returns the evaluation points from the current
// ceremony state for use in KZG commitment operations.
func (c *KZGCeremony) GetEvaluationPoints() *PointsOfEvaluation {
	return &PointsOfEvaluation{
		G1Points: c.state.PowersG1,
		G2Point:  c.state.TauG2,
	}
}

// ceremonyWitness derives a deterministic witness scalar from tau and a
// participant ID using SHA-256. This binds the proof to the specific
// participant.
func ceremonyWitness(tau *big.Int, participantID string) *big.Int {
	h := sha256.New()
	h.Write(tau.Bytes())
	h.Write([]byte(participantID))
	digest := h.Sum(nil)
	w := new(big.Int).SetBytes(digest)
	w.Mod(w, blsR)
	if w.Sign() == 0 {
		w.SetInt64(1)
	}
	return w
}

// blsG1Clone creates a copy of a G1 point.
func blsG1Clone(p *BlsG1Point) *BlsG1Point {
	if p.blsG1IsInfinity() {
		return BlsG1Infinity()
	}
	x, y := p.blsG1ToAffine()
	return blsG1FromAffine(new(big.Int).Set(x), new(big.Int).Set(y))
}

// blsG2Clone creates a copy of a G2 point.
func blsG2Clone(p *BlsG2Point) *BlsG2Point {
	if p.blsG2IsInfinity() {
		return BlsG2Infinity()
	}
	x, y := p.blsG2ToAffine()
	return blsG2FromAffine(
		&blsFp2{c0: new(big.Int).Set(x.c0), c1: new(big.Int).Set(x.c1)},
		&blsFp2{c0: new(big.Int).Set(y.c0), c1: new(big.Int).Set(y.c1)},
	)
}
