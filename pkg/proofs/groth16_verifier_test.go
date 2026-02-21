package proofs

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/crypto"
)

func TestGroth16ValidateProofNil(t *testing.T) {
	if err := ValidateGroth16Proof(nil); err != ErrGroth16NilProof {
		t.Fatalf("expected ErrGroth16NilProof, got %v", err)
	}
}

func TestGroth16ValidateProofNilA(t *testing.T) {
	p := &BLSGroth16Proof{A: nil, B: crypto.BlsG2Generator(), C: crypto.BlsG1Generator()}
	if err := ValidateGroth16Proof(p); err != ErrGroth16InvalidA {
		t.Fatalf("expected ErrGroth16InvalidA, got %v", err)
	}
}

func TestGroth16ValidateProofNilB(t *testing.T) {
	p := &BLSGroth16Proof{A: crypto.BlsG1Generator(), B: nil, C: crypto.BlsG1Generator()}
	if err := ValidateGroth16Proof(p); err != ErrGroth16InvalidB {
		t.Fatalf("expected ErrGroth16InvalidB, got %v", err)
	}
}

func TestGroth16ValidateProofNilC(t *testing.T) {
	p := &BLSGroth16Proof{A: crypto.BlsG1Generator(), B: crypto.BlsG2Generator(), C: nil}
	if err := ValidateGroth16Proof(p); err != ErrGroth16InvalidC {
		t.Fatalf("expected ErrGroth16InvalidC, got %v", err)
	}
}

func TestGroth16ValidateProofValid(t *testing.T) {
	p := &BLSGroth16Proof{A: crypto.BlsG1Generator(), B: crypto.BlsG2Generator(), C: crypto.BlsG1Generator()}
	if err := ValidateGroth16Proof(p); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestGroth16ValidateVKNil(t *testing.T) {
	if err := ValidateVerifyingKey(nil); err != ErrGroth16NilVK {
		t.Fatalf("expected ErrGroth16NilVK, got %v", err)
	}
}

func TestGroth16ValidateVKNilAlpha(t *testing.T) {
	vk := makeVK()
	vk.Alpha = nil
	if err := ValidateVerifyingKey(vk); err != ErrGroth16InvalidAlpha {
		t.Fatalf("expected ErrGroth16InvalidAlpha, got %v", err)
	}
}

func TestGroth16ValidateVKNilBeta(t *testing.T) {
	vk := makeVK()
	vk.Beta = nil
	if err := ValidateVerifyingKey(vk); err != ErrGroth16InvalidBeta {
		t.Fatalf("expected ErrGroth16InvalidBeta, got %v", err)
	}
}

func TestGroth16ValidateVKNilGamma(t *testing.T) {
	vk := makeVK()
	vk.Gamma = nil
	if err := ValidateVerifyingKey(vk); err != ErrGroth16InvalidGamma {
		t.Fatalf("expected ErrGroth16InvalidGamma, got %v", err)
	}
}

func TestGroth16ValidateVKNilDelta(t *testing.T) {
	vk := makeVK()
	vk.Delta = nil
	if err := ValidateVerifyingKey(vk); err != ErrGroth16InvalidDelta {
		t.Fatalf("expected ErrGroth16InvalidDelta, got %v", err)
	}
}

func TestGroth16ValidateVKNoIC(t *testing.T) {
	vk := makeVK()
	vk.IC = nil
	if err := ValidateVerifyingKey(vk); err != ErrGroth16NoIC {
		t.Fatalf("expected ErrGroth16NoIC, got %v", err)
	}
}

func TestGroth16ValidateVKValid(t *testing.T) {
	if err := ValidateVerifyingKey(makeVK()); err != nil {
		t.Fatalf("expected valid: %v", err)
	}
}

func TestGroth16BackendNames(t *testing.T) {
	if (&PureGoGroth16Backend{}).Name() != "pure-go-groth16" {
		t.Fatal("wrong name")
	}
	if (&GnarkGroth16Backend{}).Name() != "gnark-groth16" {
		t.Fatal("wrong name")
	}
}

func TestGroth16DefaultAndSetBackend(t *testing.T) {
	SetGroth16Backend(nil)
	if DefaultGroth16Backend().Name() != "pure-go-groth16" {
		t.Fatal("default wrong")
	}
	defer SetGroth16Backend(nil)
	SetGroth16Backend(&GnarkGroth16Backend{})
	if DefaultGroth16Backend().Name() != "gnark-groth16" {
		t.Fatal("set wrong")
	}
	SetGroth16Backend(nil)
	if DefaultGroth16Backend().Name() != "pure-go-groth16" {
		t.Fatal("reset wrong")
	}
}

func TestGroth16IntegrationStatus(t *testing.T) {
	SetGroth16Backend(nil)
	if Groth16IntegrationStatus() != "pure-go-groth16" {
		t.Fatalf("got %s", Groth16IntegrationStatus())
	}
}

func TestGroth16GasEstimation(t *testing.T) {
	gas0 := EstimateGroth16VerifyGas(0)
	gas1 := EstimateGroth16VerifyGas(1)
	gas10 := EstimateGroth16VerifyGas(10)
	if gas0 == 0 || gas1 <= gas0 || gas10 <= gas1 {
		t.Fatalf("gas should increase: %d, %d, %d", gas0, gas1, gas10)
	}
}

func TestGroth16GasEstimationNegative(t *testing.T) {
	if EstimateGroth16VerifyGas(-5) != EstimateGroth16VerifyGas(0) {
		t.Fatal("negative should be treated as 0")
	}
}

func TestGroth16Setup(t *testing.T) {
	circuit := &CircuitDefinition{
		Name: "test", PublicInputCount: 2,
		Constraints: []Constraint{{Type: ConstraintLinear, A: []Term{{VarID: 0, Coeff: 1}}}},
	}
	pk, vk, err := (&PureGoGroth16Backend{}).Setup(circuit)
	if err != nil {
		t.Fatal(err)
	}
	if pk.CircuitName != "test" || len(vk.IC) != 3 {
		t.Fatalf("pk=%s, IC=%d", pk.CircuitName, len(vk.IC))
	}
}

func TestGroth16SetupNilCircuit(t *testing.T) {
	if _, _, err := (&PureGoGroth16Backend{}).Setup(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestGroth16VerifyICMismatch(t *testing.T) {
	g1, g2 := crypto.BlsG1Generator(), crypto.BlsG2Generator()
	vk := &BLSGroth16VerifyingKey{Alpha: g1, Beta: g2, Gamma: g2, Delta: g2, IC: []*crypto.BlsG1Point{g1, g1}}
	proof := &BLSGroth16Proof{A: g1, B: g2, C: g1}
	if _, err := (&PureGoGroth16Backend{}).Verify(vk, proof, [][]byte{{1}, {2}}); err == nil {
		t.Fatal("expected IC mismatch")
	}
}

func TestGroth16SerializeDeserialize(t *testing.T) {
	proof := &BLSGroth16Proof{A: crypto.BlsG1Generator(), B: crypto.BlsG2Generator(), C: crypto.BlsG1Generator()}
	data, err := SerializeBLSGroth16Proof(proof)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 192 {
		t.Fatalf("len=%d", len(data))
	}
	proof2, err := DeserializeBLSGroth16Proof(data)
	if err != nil {
		t.Fatal(err)
	}
	data2, _ := SerializeBLSGroth16Proof(proof2)
	for i := range data {
		if data[i] != data2[i] {
			t.Fatalf("byte %d: %02x vs %02x", i, data[i], data2[i])
		}
	}
}

func TestGroth16DeserializeInvalidLength(t *testing.T) {
	if _, err := DeserializeBLSGroth16Proof(make([]byte, 100)); err == nil {
		t.Fatal("expected error")
	}
}

func TestGroth16ProofFingerprint(t *testing.T) {
	proof := &BLSGroth16Proof{A: crypto.BlsG1Generator(), B: crypto.BlsG2Generator(), C: crypto.BlsG1Generator()}
	fp1, _ := BLSGroth16ProofFingerprint(proof)
	fp2, _ := BLSGroth16ProofFingerprint(proof)
	if fp1 != fp2 {
		t.Fatal("fingerprints should match")
	}
}

func TestGroth16ProofFingerprintNil(t *testing.T) {
	if _, err := BLSGroth16ProofFingerprint(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestGroth16EncodePublicInput(t *testing.T) {
	inp := EncodePublicInput(42)
	if len(inp) != 32 || inp[31] != 42 {
		t.Fatalf("wrong encoding: len=%d, last=%d", len(inp), inp[31])
	}
}

func TestGroth16VerifyWithInfinityProof(t *testing.T) {
	g1, g2 := crypto.BlsG1Generator(), crypto.BlsG2Generator()
	vk := &BLSGroth16VerifyingKey{Alpha: g1, Beta: g2, Gamma: g2, Delta: g2, IC: []*crypto.BlsG1Point{g1}}
	proof := &BLSGroth16Proof{A: crypto.BlsG1Infinity(), B: g2, C: crypto.BlsG1Infinity()}
	// Should not panic.
	_, err := (&PureGoGroth16Backend{}).Verify(vk, proof, nil)
	if err != nil {
		t.Fatalf("should not error: %v", err)
	}
}

func TestGroth16ConcurrentBackendAccess(t *testing.T) {
	defer SetGroth16Backend(nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); SetGroth16Backend(&GnarkGroth16Backend{}) }()
		go func() { defer wg.Done(); _ = DefaultGroth16Backend().Name() }()
	}
	wg.Wait()
}

func TestGroth16VKDifferentICLengths(t *testing.T) {
	g1, g2 := crypto.BlsG1Generator(), crypto.BlsG2Generator()
	for _, n := range []int{1, 3, 5} {
		ic := make([]*crypto.BlsG1Point, n)
		for i := range ic {
			ic[i] = g1
		}
		vk := &BLSGroth16VerifyingKey{Alpha: g1, Beta: g2, Gamma: g2, Delta: g2, IC: ic}
		if err := ValidateVerifyingKey(vk); err != nil {
			t.Fatalf("IC len %d should be valid: %v", n, err)
		}
	}
}

func TestGroth16NegateG1(t *testing.T) {
	g1 := crypto.BlsG1Generator()
	neg := g16NegateG1(g1)
	negNeg := g16NegateG1(neg)
	s1 := crypto.SerializeG1(g1)
	s2 := crypto.SerializeG1(negNeg)
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("double negate failed, byte %d", i)
		}
	}
}

func TestGroth16NegateInfinity(t *testing.T) {
	inf := crypto.BlsG1Infinity()
	neg := g16NegateG1(inf)
	if crypto.SerializeG1(neg)[0]&0x40 == 0 {
		t.Fatal("negated infinity should be infinity")
	}
}

func TestGroth16ScalarMulG1(t *testing.T) {
	g1 := crypto.BlsG1Generator()
	p := g16ScalarMulG1(g1, 1)
	s1 := crypto.SerializeG1(g1)
	s2 := crypto.SerializeG1(p)
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("1*G1 != G1, byte %d", i)
		}
	}
	p0 := g16ScalarMulG1(g1, 0)
	if crypto.SerializeG1(p0)[0]&0x40 == 0 {
		t.Fatal("0*G1 should be infinity")
	}
}

func TestGroth16EncodeDecodeG1Roundtrip(t *testing.T) {
	g1 := crypto.BlsG1Generator()
	buf := make([]byte, blsG16G1Enc)
	g16PutG1(buf, g1)
	allZ := true
	for _, b := range buf {
		if b != 0 {
			allZ = false
		}
	}
	if allZ {
		t.Fatal("encoded G1 should not be all zeros")
	}
	decoded := g16BytesToG1(buf)
	s1 := crypto.SerializeG1(g1)
	s2 := crypto.SerializeG1(decoded)
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("round-trip failed, byte %d", i)
		}
	}
}

func makeVK() *BLSGroth16VerifyingKey {
	g1, g2 := crypto.BlsG1Generator(), crypto.BlsG2Generator()
	return &BLSGroth16VerifyingKey{Alpha: g1, Beta: g2, Gamma: g2, Delta: g2, IC: []*crypto.BlsG1Point{g1}}
}
