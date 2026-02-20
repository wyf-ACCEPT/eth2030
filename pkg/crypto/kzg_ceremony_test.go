package crypto

import (
	"math/big"
	"testing"
)

func TestNewKZGCeremony(t *testing.T) {
	c, err := NewKZGCeremony(4, 0)
	if err != nil {
		t.Fatalf("NewKZGCeremony: %v", err)
	}
	if c.Phase() != PhaseContributing {
		t.Errorf("Phase = %v, want contributing", c.Phase())
	}
	if c.Round() != 1 {
		t.Errorf("Round = %d, want 1", c.Round())
	}
	if c.NumContributions() != 0 {
		t.Errorf("NumContributions = %d, want 0", c.NumContributions())
	}
	if c.Degree() != 4 {
		t.Errorf("Degree = %d, want 4", c.Degree())
	}
}

func TestNewKZGCeremonyBadDegree(t *testing.T) {
	_, err := NewKZGCeremony(0, 0)
	if err != ErrCeremonyBadDegree {
		t.Errorf("expected ErrCeremonyBadDegree, got %v", err)
	}
	_, err = NewKZGCeremony(-1, 0)
	if err != ErrCeremonyBadDegree {
		t.Errorf("expected ErrCeremonyBadDegree for -1, got %v", err)
	}
}

func TestCeremonyPhaseString(t *testing.T) {
	if PhaseContributing.String() != "contributing" {
		t.Errorf("PhaseContributing.String() = %q", PhaseContributing.String())
	}
	if PhaseFinalized.String() != "finalized" {
		t.Errorf("PhaseFinalized.String() = %q", PhaseFinalized.String())
	}
	unknown := CeremonyPhase(99)
	if unknown.String() != "unknown" {
		t.Errorf("unknown phase String() = %q", unknown.String())
	}
}

func TestGenerateContributionBasic(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution(
		"alice",
		tau,
		c.state.PowersG1,
		c.state.TauG2,
		c.Round(),
	)

	if contrib.ParticipantID != "alice" {
		t.Errorf("ParticipantID = %q", contrib.ParticipantID)
	}
	if contrib.Round != 1 {
		t.Errorf("Round = %d, want 1", contrib.Round)
	}
	if len(contrib.PowersG1) != 5 {
		t.Errorf("PowersG1 len = %d, want 5", len(contrib.PowersG1))
	}
	if contrib.TauG2 == nil {
		t.Fatal("TauG2 is nil")
	}
	if contrib.ProofG1 == nil || contrib.ProofG2 == nil {
		t.Fatal("proof points are nil")
	}
}

func TestVerifyContributionValid(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(13)
	contrib := GenerateContribution(
		"bob",
		tau,
		c.state.PowersG1,
		c.state.TauG2,
		c.Round(),
	)

	if !VerifyContribution(contrib, 4) {
		t.Fatal("valid contribution should verify")
	}
}

func TestVerifyContributionWrongDegree(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution(
		"charlie",
		tau,
		c.state.PowersG1,
		c.state.TauG2,
		c.Round(),
	)

	// Wrong degree: contribution has 5 points but we check for degree 3 (needs 4).
	if VerifyContribution(contrib, 3) {
		t.Fatal("should fail with wrong degree")
	}
}

func TestVerifyContributionNilPoints(t *testing.T) {
	contrib := &Contribution{
		ParticipantID: "test",
		PowersG1:      make([]*BlsG1Point, 5),
		TauG2:         nil,
		ProofG1:       nil,
		ProofG2:       nil,
	}
	if VerifyContribution(contrib, 4) {
		t.Fatal("should fail with nil points")
	}
}

func TestAccumulateContributionSingle(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution(
		"alice",
		tau,
		c.state.PowersG1,
		c.state.TauG2,
		c.Round(),
	)

	err := c.AccumulateContribution(contrib)
	if err != nil {
		t.Fatalf("AccumulateContribution: %v", err)
	}

	if c.NumContributions() != 1 {
		t.Errorf("NumContributions = %d, want 1", c.NumContributions())
	}
	if c.Round() != 2 {
		t.Errorf("Round = %d, want 2", c.Round())
	}
}

func TestAccumulateContributionMultiple(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	// First contribution.
	tau1 := big.NewInt(7)
	contrib1 := GenerateContribution("alice", tau1, c.state.PowersG1, c.state.TauG2, c.Round())
	if err := c.AccumulateContribution(contrib1); err != nil {
		t.Fatalf("first contribution: %v", err)
	}

	// Second contribution builds on the first.
	tau2 := big.NewInt(13)
	contrib2 := GenerateContribution("bob", tau2, c.state.PowersG1, c.state.TauG2, c.Round())
	if err := c.AccumulateContribution(contrib2); err != nil {
		t.Fatalf("second contribution: %v", err)
	}

	if c.NumContributions() != 2 {
		t.Errorf("NumContributions = %d, want 2", c.NumContributions())
	}
}

func TestAccumulateContributionDuplicate(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution("alice", tau, c.state.PowersG1, c.state.TauG2, c.Round())
	c.AccumulateContribution(contrib)

	// Same participant tries again with new tau.
	tau2 := big.NewInt(11)
	contrib2 := GenerateContribution("alice", tau2, c.state.PowersG1, c.state.TauG2, c.Round())
	err := c.AccumulateContribution(contrib2)
	if err != ErrCeremonyDuplicate {
		t.Errorf("expected ErrCeremonyDuplicate, got %v", err)
	}
}

func TestAccumulateContributionMaxRounds(t *testing.T) {
	c, _ := NewKZGCeremony(2, 1) // max 1 round

	tau := big.NewInt(5)
	contrib := GenerateContribution("alice", tau, c.state.PowersG1, c.state.TauG2, c.Round())
	c.AccumulateContribution(contrib)

	// Second contribution should be rejected (round 2 > maxRounds 1).
	tau2 := big.NewInt(9)
	contrib2 := GenerateContribution("bob", tau2, c.state.PowersG1, c.state.TauG2, c.Round())
	err := c.AccumulateContribution(contrib2)
	if err != ErrCeremonyMaxRound {
		t.Errorf("expected ErrCeremonyMaxRound, got %v", err)
	}
}

func TestAccumulateAfterFinalize(t *testing.T) {
	c, _ := NewKZGCeremony(2, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution("alice", tau, c.state.PowersG1, c.state.TauG2, c.Round())
	c.AccumulateContribution(contrib)
	c.Finalize()

	tau2 := big.NewInt(11)
	contrib2 := GenerateContribution("bob", tau2, c.state.PowersG1, c.state.TauG2, 99)
	err := c.AccumulateContribution(contrib2)
	if err != ErrCeremonyFinalized {
		t.Errorf("expected ErrCeremonyFinalized, got %v", err)
	}
}

func TestAccumulateInvalidPoints(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	// Contribution with wrong number of points.
	contrib := &Contribution{
		ParticipantID: "eve",
		PowersG1:      make([]*BlsG1Point, 3), // wrong: need 5
		TauG2:         BlsG2Generator(),
		ProofG1:       BlsG1Generator(),
		ProofG2:       BlsG2Generator(),
	}
	err := c.AccumulateContribution(contrib)
	if err != ErrCeremonyInvalidPoints {
		t.Errorf("expected ErrCeremonyInvalidPoints, got %v", err)
	}
}

func TestFinalizeSuccess(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution("alice", tau, c.state.PowersG1, c.state.TauG2, c.Round())
	c.AccumulateContribution(contrib)

	result, err := c.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if result.NumContributions != 1 {
		t.Errorf("NumContributions = %d, want 1", result.NumContributions)
	}
	if len(result.G1Powers) != 5 {
		t.Errorf("G1Powers len = %d, want 5", len(result.G1Powers))
	}
	if result.G2Tau == nil {
		t.Fatal("G2Tau is nil")
	}
	if c.Phase() != PhaseFinalized {
		t.Errorf("Phase = %v, want finalized", c.Phase())
	}
}

func TestFinalizeNoContributions(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	_, err := c.Finalize()
	if err != ErrCeremonyNoContribs {
		t.Errorf("expected ErrCeremonyNoContribs, got %v", err)
	}
}

func TestFinalizeDouble(t *testing.T) {
	c, _ := NewKZGCeremony(2, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution("alice", tau, c.state.PowersG1, c.state.TauG2, c.Round())
	c.AccumulateContribution(contrib)
	c.Finalize()

	_, err := c.Finalize()
	if err != ErrCeremonyFinalized {
		t.Errorf("expected ErrCeremonyFinalized on double finalize, got %v", err)
	}
}

func TestGetEvaluationPoints(t *testing.T) {
	c, _ := NewKZGCeremony(4, 0)

	tau := big.NewInt(7)
	contrib := GenerateContribution("alice", tau, c.state.PowersG1, c.state.TauG2, c.Round())
	c.AccumulateContribution(contrib)

	pts := c.GetEvaluationPoints()
	if len(pts.G1Points) != 5 {
		t.Errorf("G1Points len = %d, want 5", len(pts.G1Points))
	}
	if pts.G2Point == nil {
		t.Fatal("G2Point is nil")
	}
}

func TestCeremonyWitnessDeterministic(t *testing.T) {
	tau := big.NewInt(42)
	w1 := ceremonyWitness(tau, "alice")
	w2 := ceremonyWitness(tau, "alice")
	if w1.Cmp(w2) != 0 {
		t.Fatal("witness should be deterministic")
	}
}

func TestCeremonyWitnessDifferentIDs(t *testing.T) {
	tau := big.NewInt(42)
	w1 := ceremonyWitness(tau, "alice")
	w2 := ceremonyWitness(tau, "bob")
	if w1.Cmp(w2) == 0 {
		t.Fatal("different IDs should produce different witnesses")
	}
}

func TestCeremonyEndToEnd(t *testing.T) {
	// Full ceremony with 3 participants.
	c, _ := NewKZGCeremony(4, 10)

	taus := []*big.Int{big.NewInt(7), big.NewInt(13), big.NewInt(23)}
	ids := []string{"alice", "bob", "charlie"}

	for i, tau := range taus {
		contrib := GenerateContribution(ids[i], tau, c.state.PowersG1, c.state.TauG2, c.Round())
		err := c.AccumulateContribution(contrib)
		if err != nil {
			t.Fatalf("contribution %d: %v", i, err)
		}
	}

	result, err := c.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if result.NumContributions != 3 {
		t.Errorf("NumContributions = %d, want 3", result.NumContributions)
	}

	// Verify the SRS: powers[0] should be the generator.
	genG1 := BlsG1Generator()
	genX, genY := genG1.blsG1ToAffine()
	p0X, p0Y := result.G1Powers[0].blsG1ToAffine()
	if genX.Cmp(p0X) != 0 || genY.Cmp(p0Y) != 0 {
		t.Fatal("powers[0] should be the generator G1")
	}

	// Verify powers[1] = [combined_tau]G1.
	// Combined tau = 7 * 13 * 23 = 2093
	combinedTau := big.NewInt(7 * 13 * 23)
	expectedP1 := blsG1ScalarMul(genG1, combinedTau)
	ep1X, ep1Y := expectedP1.blsG1ToAffine()
	p1X, p1Y := result.G1Powers[1].blsG1ToAffine()
	if ep1X.Cmp(p1X) != 0 || ep1Y.Cmp(p1Y) != 0 {
		t.Fatal("powers[1] should be [combined_tau]G1")
	}

	// Verify powers[2] = [combined_tau^2]G1.
	tauSq := new(big.Int).Mul(combinedTau, combinedTau)
	tauSq.Mod(tauSq, blsR)
	expectedP2 := blsG1ScalarMul(genG1, tauSq)
	ep2X, ep2Y := expectedP2.blsG1ToAffine()
	p2X, p2Y := result.G1Powers[2].blsG1ToAffine()
	if ep2X.Cmp(p2X) != 0 || ep2Y.Cmp(p2Y) != 0 {
		t.Fatal("powers[2] should be [combined_tau^2]G1")
	}
}

func TestBlsG1Clone(t *testing.T) {
	gen := BlsG1Generator()
	cloned := blsG1Clone(gen)

	gx, gy := gen.blsG1ToAffine()
	cx, cy := cloned.blsG1ToAffine()
	if gx.Cmp(cx) != 0 || gy.Cmp(cy) != 0 {
		t.Fatal("cloned G1 should match original")
	}
}

func TestBlsG1CloneInfinity(t *testing.T) {
	inf := BlsG1Infinity()
	cloned := blsG1Clone(inf)
	if !cloned.blsG1IsInfinity() {
		t.Fatal("cloned infinity should be infinity")
	}
}

func TestBlsG2Clone(t *testing.T) {
	gen := BlsG2Generator()
	cloned := blsG2Clone(gen)

	gx, gy := gen.blsG2ToAffine()
	cx, cy := cloned.blsG2ToAffine()
	if !gx.equal(cx) || !gy.equal(cy) {
		t.Fatal("cloned G2 should match original")
	}
}

func TestBlsG2CloneInfinity(t *testing.T) {
	inf := BlsG2Infinity()
	cloned := blsG2Clone(inf)
	if !cloned.blsG2IsInfinity() {
		t.Fatal("cloned infinity should be infinity")
	}
}
