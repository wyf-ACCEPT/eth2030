package proofs

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func makeCircuitUserOp(nonce uint64) *UserOperation {
	return &UserOperation{
		Sender:               types.HexToAddress("0x1111111111111111111111111111111111111111"),
		Nonce:                nonce,
		CallData:             []byte{0x01, 0x02, 0x03},
		CallGasLimit:         100_000,
		VerificationGasLimit: 200_000,
		PreVerificationGas:   21_000,
		MaxFeePerGas:         50,
		MaxPriorityFeePerGas: 2,
		Signature:            []byte("test-sig-data-12345"),
	}
}

func makeCircuitStateProof(nonce uint64, balance *big.Int) *AAStateProof {
	return &AAStateProof{
		AccountNonce:   nonce,
		AccountBalance: balance,
		StateRoot:      types.HexToHash("0xaabbccdd"),
		StorageProof:   []byte{0xff},
	}
}

func TestAACircuitProveValidation(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18)) // nonce=0, balance=1 ETH

	proof, pub, err := ProveAAValidation(op, sp)
	if err != nil {
		t.Fatalf("ProveAAValidation: %v", err)
	}
	if proof == nil || pub == nil {
		t.Fatal("expected non-nil proof and public inputs")
	}
	if proof.Version != AACircuitVersion {
		t.Errorf("version: got %d, want %d", proof.Version, AACircuitVersion)
	}
	if pub.Nonce != 1 {
		t.Errorf("nonce: got %d, want 1", pub.Nonce)
	}
	if proof.GasCost == 0 {
		t.Error("gas cost should be non-zero")
	}
}

func TestAACircuitProveNilWitness(t *testing.T) {
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	_, _, err := ProveAAValidation(nil, sp)
	if err != ErrAACircuitNilWitness {
		t.Errorf("expected ErrAACircuitNilWitness, got %v", err)
	}
}

func TestAACircuitProveNilStateProof(t *testing.T) {
	op := makeCircuitUserOp(1)
	_, _, err := ProveAAValidation(op, nil)
	if err != ErrAACircuitNilStateProof {
		t.Errorf("expected ErrAACircuitNilStateProof, got %v", err)
	}
}

func TestAACircuitProveInvalidNonce(t *testing.T) {
	op := makeCircuitUserOp(5)
	sp := makeCircuitStateProof(0, big.NewInt(1e18)) // expects nonce 1, not 5
	_, _, err := ProveAAValidation(op, sp)
	if err != ErrAACircuitInvalidNonce {
		t.Errorf("expected ErrAACircuitInvalidNonce, got %v", err)
	}
}

func TestAACircuitProveEmptySignature(t *testing.T) {
	op := makeCircuitUserOp(1)
	op.Signature = nil
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	_, _, err := ProveAAValidation(op, sp)
	if err != ErrAACircuitInvalidSig {
		t.Errorf("expected ErrAACircuitInvalidSig, got %v", err)
	}
}

func TestAACircuitProveInsufficientGas(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1)) // 1 wei, not enough
	_, _, err := ProveAAValidation(op, sp)
	if err != ErrAACircuitInsufficientGas {
		t.Errorf("expected ErrAACircuitInsufficientGas, got %v", err)
	}
}

func TestAACircuitVerifyValid(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	proof, pub, err := ProveAAValidation(op, sp)
	if err != nil {
		t.Fatalf("ProveAAValidation: %v", err)
	}
	if !VerifyAAProof(proof, pub) {
		t.Error("valid proof should verify")
	}
}

func TestAACircuitVerifyTamperedProof(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	proof, pub, err := ProveAAValidation(op, sp)
	if err != nil {
		t.Fatalf("ProveAAValidation: %v", err)
	}
	// Tamper with pi_C.
	proof.Groth16.C.X[0] ^= 0xff
	if VerifyAAProof(proof, pub) {
		t.Error("tampered proof should not verify")
	}
}

func TestAACircuitVerifyNilProof(t *testing.T) {
	pub := &AAPublicInputs{}
	if VerifyAAProof(nil, pub) {
		t.Error("nil proof should not verify")
	}
}

func TestAACircuitVerifyNilPublicInputs(t *testing.T) {
	proof := &AACircuitProof{Version: AACircuitVersion}
	if VerifyAAProof(proof, nil) {
		t.Error("nil public inputs should not verify")
	}
}

func TestAACircuitVerifyWrongVersion(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	proof, pub, _ := ProveAAValidation(op, sp)
	proof.Version = 0xff
	if VerifyAAProof(proof, pub) {
		t.Error("wrong version should not verify")
	}
}

func TestAACircuitVerifyMismatchedOpHash(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	proof, pub, _ := ProveAAValidation(op, sp)
	pub.OpHash = types.HexToHash("0xdead")
	if VerifyAAProof(proof, pub) {
		t.Error("mismatched OpHash should not verify")
	}
}

func TestAACircuitBatchAdd(t *testing.T) {
	batch := NewAAProofBatch()
	if batch.Size() != 0 {
		t.Errorf("empty batch size: got %d, want 0", batch.Size())
	}

	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	proof, _, _ := ProveAAValidation(op, sp)

	if err := batch.Add(proof); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if batch.Size() != 1 {
		t.Errorf("batch size: got %d, want 1", batch.Size())
	}
}

func TestAACircuitBatchAddNil(t *testing.T) {
	batch := NewAAProofBatch()
	if err := batch.Add(nil); err != ErrAACircuitNilWitness {
		t.Errorf("expected ErrAACircuitNilWitness, got %v", err)
	}
}

func TestAACircuitBatchAggregate(t *testing.T) {
	batch := NewAAProofBatch()
	for i := uint64(1); i <= 4; i++ {
		op := makeCircuitUserOp(i)
		sp := makeCircuitStateProof(i-1, big.NewInt(1e18))
		proof, _, _ := ProveAAValidation(op, sp)
		batch.Add(proof)
	}

	agg, err := batch.Aggregate()
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if agg.BatchSize != 4 {
		t.Errorf("batch size: got %d, want 4", agg.BatchSize)
	}
	if agg.TotalGas == 0 {
		t.Error("total gas should be non-zero")
	}
	if agg.BatchRoot == (types.Hash{}) {
		t.Error("batch root should not be zero")
	}
	if len(agg.ProofData) == 0 {
		t.Error("proof data should not be empty")
	}
}

func TestAACircuitBatchAggregateEmpty(t *testing.T) {
	batch := NewAAProofBatch()
	_, err := batch.Aggregate()
	if err != ErrAACircuitBatchEmpty {
		t.Errorf("expected ErrAACircuitBatchEmpty, got %v", err)
	}
}

func TestAACircuitCompressProof(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))
	proof, _, _ := ProveAAValidation(op, sp)

	compressed, err := CompressAAProof(proof)
	if err != nil {
		t.Fatalf("CompressAAProof: %v", err)
	}
	// version(1) + 4*32 + 32 + 8 + 8 + 32 = 209
	expectedSize := 1 + 4*32 + 32 + 8 + 8 + 32
	if len(compressed) != expectedSize {
		t.Errorf("compressed size: got %d, want %d", len(compressed), expectedSize)
	}
	if compressed[0] != AACircuitVersion {
		t.Errorf("compressed version: got %d, want %d", compressed[0], AACircuitVersion)
	}
}

func TestAACircuitCompressNil(t *testing.T) {
	_, err := CompressAAProof(nil)
	if err != ErrAACircuitCompressFailed {
		t.Errorf("expected ErrAACircuitCompressFailed, got %v", err)
	}
}

func TestAACircuitG1PointBytes(t *testing.T) {
	p := G1Point{X: [32]byte{0x01}, Y: [32]byte{0x02}}
	b := p.Bytes()
	if len(b) != g1PointSize {
		t.Errorf("G1 size: got %d, want %d", len(b), g1PointSize)
	}
	if b[0] != 0x01 || b[32] != 0x02 {
		t.Error("G1 point bytes incorrect")
	}
}

func TestAACircuitG2PointBytes(t *testing.T) {
	p := G2Point{
		X1: [32]byte{0x01}, X2: [32]byte{0x02},
		Y1: [32]byte{0x03}, Y2: [32]byte{0x04},
	}
	b := p.Bytes()
	if len(b) != g2PointSize {
		t.Errorf("G2 size: got %d, want %d", len(b), g2PointSize)
	}
}

func TestAACircuitGroth16ProofBytes(t *testing.T) {
	proof := &Groth16Proof{}
	b := proof.Bytes()
	if len(b) != groth16Size {
		t.Errorf("Groth16 size: got %d, want %d", len(b), groth16Size)
	}
}

func TestAACircuitDeterministic(t *testing.T) {
	op := makeCircuitUserOp(1)
	sp := makeCircuitStateProof(0, big.NewInt(1e18))

	proof1, pub1, _ := ProveAAValidation(op, sp)
	proof2, pub2, _ := ProveAAValidation(op, sp)

	if proof1.Groth16.A.X != proof2.Groth16.A.X {
		t.Error("proof should be deterministic")
	}
	if pub1.OpHash != pub2.OpHash {
		t.Error("public inputs should be deterministic")
	}
}

func TestAACircuitGasCost(t *testing.T) {
	c := NewAAValidationCircuit()
	cost := c.CircuitGasCost()
	expected := aaCircuitBaseGas + uint64(aaCircuitTotalConstraints)*aaCircuitPerConstraint
	if cost != expected {
		t.Errorf("gas cost: got %d, want %d", cost, expected)
	}
}

func TestAACircuitConstraintCount(t *testing.T) {
	c := NewAAValidationCircuit()
	count := c.ConstraintCount()
	if count != aaCircuitTotalConstraints {
		t.Errorf("constraint count: got %d, want %d", count, aaCircuitTotalConstraints)
	}
}

func TestAACircuitMerkleRootSHA256(t *testing.T) {
	// Empty leaves.
	root := merkleRootSHA256(nil)
	if len(root) != 32 {
		t.Errorf("empty root len: got %d, want 32", len(root))
	}

	// Single leaf.
	leaf := []byte{0x01, 0x02, 0x03}
	root = merkleRootSHA256([][]byte{leaf})
	if len(root) == 0 {
		t.Error("single leaf root should not be empty")
	}

	// Two leaves.
	root2 := merkleRootSHA256([][]byte{leaf, leaf})
	if len(root2) != 32 {
		t.Errorf("two-leaf root len: got %d, want 32", len(root2))
	}

	// Three leaves (odd count).
	root3 := merkleRootSHA256([][]byte{leaf, leaf, leaf})
	if len(root3) != 32 {
		t.Errorf("three-leaf root len: got %d, want 32", len(root3))
	}
}
