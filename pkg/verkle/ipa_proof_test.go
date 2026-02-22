package verkle

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

func TestIPAProofVerkleValidate(t *testing.T) {
	// Valid proof with 8 rounds (for 256-element domain).
	proof := &IPAProofVerkle{
		CL:          make([]Commitment, 8),
		CR:          make([]Commitment, 8),
		FinalScalar: One(),
	}
	if err := proof.Validate(8); err != nil {
		t.Fatal("valid proof rejected:", err)
	}

	// Wrong number of rounds.
	if err := proof.Validate(4); err == nil {
		t.Fatal("expected error for wrong round count")
	}

	// Nil proof.
	var nilProof *IPAProofVerkle
	if err := nilProof.Validate(8); err == nil {
		t.Fatal("expected error for nil proof")
	}
}

func TestIPAProofVerkleLRMismatch(t *testing.T) {
	proof := &IPAProofVerkle{
		CL:          make([]Commitment, 3),
		CR:          make([]Commitment, 4),
		FinalScalar: One(),
	}
	if err := proof.Validate(3); err == nil {
		t.Fatal("expected error for L/R length mismatch")
	}
}

func TestTranscript(t *testing.T) {
	// Same inputs must produce same challenges.
	t1 := NewTranscript("test")
	t1.AppendCommitment("C", Commitment{1, 2, 3})
	t1.AppendScalar("z", FieldElementFromUint64(42))
	c1 := t1.ChallengeScalar("x")

	t2 := NewTranscript("test")
	t2.AppendCommitment("C", Commitment{1, 2, 3})
	t2.AppendScalar("z", FieldElementFromUint64(42))
	c2 := t2.ChallengeScalar("x")

	if !c1.Equal(c2) {
		t.Fatal("same inputs produced different challenges")
	}
}

func TestTranscriptDifferentLabels(t *testing.T) {
	t1 := NewTranscript("label-A")
	c1 := t1.ChallengeScalar("x")

	t2 := NewTranscript("label-B")
	c2 := t2.ChallengeScalar("x")

	if c1.Equal(c2) {
		t.Fatal("different labels produced same challenge")
	}
}

func TestTranscriptDifferentInputs(t *testing.T) {
	t1 := NewTranscript("test")
	t1.AppendScalar("v", FieldElementFromUint64(1))
	c1 := t1.ChallengeScalar("x")

	t2 := NewTranscript("test")
	t2.AppendScalar("v", FieldElementFromUint64(2))
	c2 := t2.ChallengeScalar("x")

	if c1.Equal(c2) {
		t.Fatal("different inputs produced same challenge")
	}
}

func TestTranscriptAppendUint64(t *testing.T) {
	t1 := NewTranscript("test")
	t1.AppendUint64("n", 256)
	c1 := t1.ChallengeScalar("x")

	t2 := NewTranscript("test")
	t2.AppendUint64("n", 256)
	c2 := t2.ChallengeScalar("x")

	if !c1.Equal(c2) {
		t.Fatal("same uint64 inputs produced different challenges")
	}

	t3 := NewTranscript("test")
	t3.AppendUint64("n", 512)
	c3 := t3.ChallengeScalar("x")

	if c1.Equal(c3) {
		t.Fatal("different uint64 values produced same challenge")
	}
}

func TestTranscriptChallengeNonZero(t *testing.T) {
	// Challenges should always be non-zero.
	for i := 0; i < 20; i++ {
		tr := NewTranscript("nonzero-test")
		tr.AppendUint64("i", uint64(i))
		c := tr.ChallengeScalar("x")
		if c.IsZero() {
			t.Fatalf("challenge %d is zero", i)
		}
	}
}

func TestSerializeDeserializeIPAProofVerkle(t *testing.T) {
	proof := &IPAProofVerkle{
		CL: []Commitment{
			{1, 2, 3},
			{4, 5, 6},
		},
		CR: []Commitment{
			{7, 8, 9},
			{10, 11, 12},
		},
		FinalScalar: FieldElementFromUint64(42),
	}

	data, err := SerializeIPAProofVerkle(proof)
	if err != nil {
		t.Fatal(err)
	}

	// Expected size: 1 + 2*64 + 32 = 161 bytes.
	expectedSize := 1 + 2*64 + 32
	if len(data) != expectedSize {
		t.Fatalf("expected %d bytes, got %d", expectedSize, len(data))
	}

	// Deserialize.
	decoded, err := DeserializeIPAProofVerkle(data)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.NumRounds() != 2 {
		t.Fatalf("expected 2 rounds, got %d", decoded.NumRounds())
	}

	// Check CL values.
	for i := 0; i < 2; i++ {
		if proof.CL[i] != decoded.CL[i] {
			t.Fatalf("CL[%d] mismatch", i)
		}
		if proof.CR[i] != decoded.CR[i] {
			t.Fatalf("CR[%d] mismatch", i)
		}
	}

	if !proof.FinalScalar.Equal(decoded.FinalScalar) {
		t.Fatal("FinalScalar mismatch")
	}
}

func TestSerializeDeserializeEmpty(t *testing.T) {
	proof := &IPAProofVerkle{
		FinalScalar: FieldElementFromUint64(1),
	}

	data, err := SerializeIPAProofVerkle(proof)
	if err != nil {
		t.Fatal(err)
	}

	if len(data) != 33 { // 1 + 0 + 32
		t.Fatalf("expected 33 bytes, got %d", len(data))
	}

	decoded, err := DeserializeIPAProofVerkle(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.NumRounds() != 0 {
		t.Fatal("expected 0 rounds")
	}
}

func TestDeserializeErrors(t *testing.T) {
	// Too short.
	_, err := DeserializeIPAProofVerkle([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for too-short data")
	}

	// Invalid length (1 round claimed but wrong total size).
	_, err = DeserializeIPAProofVerkle(make([]byte, 50))
	if err == nil {
		t.Fatal("expected error for invalid length")
	}
}

func TestSerializeNilProof(t *testing.T) {
	_, err := SerializeIPAProofVerkle(nil)
	if err == nil {
		t.Fatal("expected error for nil proof")
	}
}

func TestIPAProofVerkleFromCrypto(t *testing.T) {
	// Create a simple IPA proof using the crypto layer.
	n := 4
	gens := crypto.GeneratePedersenGenerators()
	generators := make([]*crypto.BanderPoint, n)
	for i := 0; i < n; i++ {
		generators[i] = gens[i]
	}

	a := make([]*big.Int, n)
	b := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		a[i] = big.NewInt(int64(i + 1))
		b[i] = big.NewInt(int64(n - i))
	}

	commitment := crypto.BanderMSM(generators, a)
	proof, v, err := crypto.IPAProve(generators, a, b, commitment)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("inner product is nil")
	}

	// Convert to verkle proof format.
	vProof := IPAProofVerkleFromCrypto(proof)
	if vProof == nil {
		t.Fatal("conversion produced nil")
	}

	expectedRounds := crypto.IPAProofSize(n)
	if vProof.NumRounds() != expectedRounds {
		t.Fatalf("expected %d rounds, got %d", expectedRounds, vProof.NumRounds())
	}

	// Roundtrip through serialization.
	data, err := SerializeIPAProofVerkle(vProof)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DeserializeIPAProofVerkle(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.NumRounds() != expectedRounds {
		t.Fatal("roundtrip lost rounds")
	}
	if !decoded.FinalScalar.Equal(vProof.FinalScalar) {
		t.Fatal("roundtrip lost final scalar")
	}
}

func TestIPAProofVerkleFromCryptoNil(t *testing.T) {
	result := IPAProofVerkleFromCrypto(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestIPAProofVerkleToCrypto(t *testing.T) {
	proof := &IPAProofVerkle{
		CL:          []Commitment{{1}, {2}},
		CR:          []Commitment{{3}, {4}},
		FinalScalar: FieldElementFromUint64(99),
	}

	cryptoProof := IPAProofVerkleToCrypto(proof)
	if cryptoProof == nil {
		t.Fatal("conversion returned nil")
	}
	if len(cryptoProof.L) != 2 || len(cryptoProof.R) != 2 {
		t.Fatal("wrong number of L/R points")
	}
	if cryptoProof.A.Cmp(big.NewInt(99)) != 0 {
		t.Fatal("final scalar mismatch")
	}
}

func TestIPAProofVerkleToCryptoNil(t *testing.T) {
	result := IPAProofVerkleToCrypto(nil)
	if result != nil {
		t.Fatal("expected nil for nil input")
	}
}

func TestVerifyMultipointProofNil(t *testing.T) {
	cfg := DefaultIPAConfig()
	ok, err := VerifyMultipointProof(cfg, nil)
	if err == nil {
		t.Fatal("expected error for nil proof")
	}
	if ok {
		t.Fatal("nil proof should not verify")
	}
}

func TestVerifyMultipointProofEmpty(t *testing.T) {
	cfg := DefaultIPAConfig()
	proof := &MultipointProof{}
	ok, err := VerifyMultipointProof(cfg, proof)
	if err == nil {
		t.Fatal("expected error for empty proof")
	}
	if ok {
		t.Fatal("empty proof should not verify")
	}
}

func TestIPAProofNumRounds(t *testing.T) {
	proof := &IPAProofVerkle{
		CL: make([]Commitment, 5),
		CR: make([]Commitment, 5),
	}
	if proof.NumRounds() != 5 {
		t.Fatalf("expected 5 rounds, got %d", proof.NumRounds())
	}
}

func TestTranscriptMultipleChallenge(t *testing.T) {
	// Successive challenges from the same transcript should differ.
	tr := NewTranscript("multi-challenge")
	tr.AppendScalar("v", FieldElementFromUint64(1))

	c1 := tr.ChallengeScalar("x1")
	c2 := tr.ChallengeScalar("x2")

	if c1.Equal(c2) {
		t.Fatal("successive challenges should differ")
	}
}
