package crypto

import (
	"math/big"
	"testing"
)

func TestInnerProduct(t *testing.T) {
	a := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)}
	b := []*big.Int{big.NewInt(5), big.NewInt(6), big.NewInt(7), big.NewInt(8)}

	// <a, b> = 1*5 + 2*6 + 3*7 + 4*8 = 5 + 12 + 21 + 32 = 70
	result := innerProduct(a, b)
	if result.Cmp(big.NewInt(70)) != 0 {
		t.Errorf("inner product = %s, want 70", result.Text(10))
	}
}

func TestInnerProductEmpty(t *testing.T) {
	result := innerProduct(nil, nil)
	if result.Sign() != 0 {
		t.Error("inner product of empty vectors should be 0")
	}
}

func TestIPAProofSize(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{1, 0},
		{2, 1},
		{4, 2},
		{8, 3},
		{16, 4},
		{256, 8},
	}
	for _, tt := range tests {
		got := IPAProofSize(tt.n)
		if got != tt.want {
			t.Errorf("IPAProofSize(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestIPAProveAndVerify(t *testing.T) {
	// Use a small vector of length 4.
	n := 4
	gens := GeneratePedersenGenerators()
	generators := make([]*BanderPoint, n)
	for i := 0; i < n; i++ {
		generators[i] = gens[i]
	}

	// Witness vector a = [3, 7, 2, 5]
	a := []*big.Int{big.NewInt(3), big.NewInt(7), big.NewInt(2), big.NewInt(5)}

	// Public vector b = [1, 1, 1, 1] (simplest evaluation)
	b := []*big.Int{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1)}

	// Commitment C = <a, G>
	commitment := BanderMSM(generators, a)

	// Generate proof.
	proof, v, err := IPAProve(generators, a, b, commitment)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	// Check claimed inner product.
	expected := innerProduct(a, b)
	if v.Cmp(expected) != 0 {
		t.Fatalf("inner product = %s, want %s", v.Text(10), expected.Text(10))
	}

	// Verify proof.
	ok, err := IPAVerify(generators, commitment, b, v, proof)
	if err != nil {
		t.Fatalf("IPAVerify error: %v", err)
	}
	if !ok {
		t.Error("valid proof should verify")
	}
}

func TestIPAProveAndVerifyLarger(t *testing.T) {
	n := 8
	gens := GeneratePedersenGenerators()
	generators := make([]*BanderPoint, n)
	for i := 0; i < n; i++ {
		generators[i] = gens[i]
	}

	a := make([]*big.Int, n)
	b := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		a[i] = big.NewInt(int64(i + 1))
		b[i] = big.NewInt(int64(n - i))
	}

	commitment := BanderMSM(generators, a)

	proof, v, err := IPAProve(generators, a, b, commitment)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	expected := innerProduct(a, b)
	if v.Cmp(expected) != 0 {
		t.Fatalf("inner product mismatch")
	}

	ok, err := IPAVerify(generators, commitment, b, v, proof)
	if err != nil {
		t.Fatalf("IPAVerify error: %v", err)
	}
	if !ok {
		t.Error("valid proof should verify")
	}
}

func TestIPAProofRoundCount(t *testing.T) {
	n := 4
	gens := GeneratePedersenGenerators()
	generators := make([]*BanderPoint, n)
	for i := 0; i < n; i++ {
		generators[i] = gens[i]
	}

	a := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)}
	b := []*big.Int{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1)}

	commitment := BanderMSM(generators, a)

	proof, _, err := IPAProve(generators, a, b, commitment)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	if len(proof.L) != 2 {
		t.Errorf("expected 2 L points for n=4, got %d", len(proof.L))
	}
	if len(proof.R) != 2 {
		t.Errorf("expected 2 R points for n=4, got %d", len(proof.R))
	}
}

func TestIPASerializeDeserialize(t *testing.T) {
	n := 4
	gens := GeneratePedersenGenerators()
	generators := make([]*BanderPoint, n)
	for i := 0; i < n; i++ {
		generators[i] = gens[i]
	}

	a := []*big.Int{big.NewInt(3), big.NewInt(7), big.NewInt(2), big.NewInt(5)}
	b := []*big.Int{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1)}

	commitment := BanderMSM(generators, a)
	proof, v, err := IPAProve(generators, a, b, commitment)
	if err != nil {
		t.Fatalf("IPAProve error: %v", err)
	}

	// Serialize.
	data := IPASerialize(proof)
	if len(data) == 0 {
		t.Fatal("serialized proof is empty")
	}

	// Expected size: 1 + 2*64 + 32 = 161 bytes for n=4 (2 rounds).
	expectedSize := 1 + 2*64 + 32
	if len(data) != expectedSize {
		t.Errorf("serialized size = %d, want %d", len(data), expectedSize)
	}

	// Deserialize.
	proof2, err := IPADeserialize(data)
	if err != nil {
		t.Fatalf("IPADeserialize error: %v", err)
	}

	// Verify the deserialized proof.
	ok, err := IPAVerify(generators, commitment, b, v, proof2)
	if err != nil {
		t.Fatalf("IPAVerify after deserialize error: %v", err)
	}
	if !ok {
		t.Error("deserialized proof should verify")
	}
}

func TestIPAInvalidInputs(t *testing.T) {
	gens := GeneratePedersenGenerators()

	// Empty vectors.
	_, _, err := IPAProve(nil, nil, nil, BanderIdentity())
	if err == nil {
		t.Error("empty vectors should error")
	}

	// Mismatched lengths.
	_, _, err = IPAProve(
		[]*BanderPoint{gens[0]},
		[]*big.Int{big.NewInt(1), big.NewInt(2)},
		[]*big.Int{big.NewInt(1)},
		BanderIdentity(),
	)
	if err == nil {
		t.Error("mismatched lengths should error")
	}

	// Non-power-of-2.
	_, _, err = IPAProve(
		[]*BanderPoint{gens[0], gens[1], gens[2]},
		[]*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3)},
		[]*big.Int{big.NewInt(1), big.NewInt(1), big.NewInt(1)},
		BanderIdentity(),
	)
	if err == nil {
		t.Error("non-power-of-2 should error")
	}
}

func TestIPAVerifyInvalidProof(t *testing.T) {
	n := 4
	gens := GeneratePedersenGenerators()
	generators := make([]*BanderPoint, n)
	for i := 0; i < n; i++ {
		generators[i] = gens[i]
	}

	// Wrong number of L/R points.
	proof := &IPAProofData{
		L: []*BanderPoint{BanderIdentity()},
		R: []*BanderPoint{BanderIdentity(), BanderIdentity()},
		A: big.NewInt(1),
	}
	b := []*big.Int{big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(1)}
	_, err := IPAVerify(generators, BanderIdentity(), b, big.NewInt(0), proof)
	if err == nil {
		t.Error("mismatched L/R count should error")
	}
}

func TestIPATranscriptDeterminism(t *testing.T) {
	t1 := newIPATranscript("test")
	t2 := newIPATranscript("test")

	g := BanderGenerator()
	t1.appendPoint(g)
	t2.appendPoint(g)

	c1 := t1.challenge()
	c2 := t2.challenge()

	if c1.Cmp(c2) != 0 {
		t.Error("same transcript should produce same challenge")
	}
}

func TestIPATranscriptDifferentInputs(t *testing.T) {
	t1 := newIPATranscript("test")
	t2 := newIPATranscript("test")

	g := BanderGenerator()
	g2 := BanderScalarMul(g, big.NewInt(2))

	t1.appendPoint(g)
	t2.appendPoint(g2)

	c1 := t1.challenge()
	c2 := t2.challenge()

	if c1.Cmp(c2) == 0 {
		t.Error("different transcript inputs should produce different challenges")
	}
}

func TestIPASerializeNil(t *testing.T) {
	data := IPASerialize(nil)
	if data != nil {
		t.Error("serializing nil proof should return nil")
	}
}

func TestIPADeserializeTooShort(t *testing.T) {
	_, err := IPADeserialize([]byte{0x01})
	if err == nil {
		t.Error("deserializing too-short data should error")
	}
}
