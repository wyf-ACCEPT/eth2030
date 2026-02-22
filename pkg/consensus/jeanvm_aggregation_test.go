package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestJeanVMAggregatorCreation(t *testing.T) {
	agg := NewJeanVMAggregator()
	if agg == nil {
		t.Fatal("aggregator should not be nil")
	}
}

func TestJeanVMAggregationCircuitCreation(t *testing.T) {
	msg := types.HexToHash("0xaabb")
	circuit := NewAggregationCircuit(64, msg)
	if circuit.CommitteeSize != 64 {
		t.Errorf("committee size = %d, want 64", circuit.CommitteeSize)
	}
	if circuit.ConstraintCount == 0 {
		t.Error("constraint count should not be zero")
	}
	// 64 * 1200 + 500 = 77300.
	expected := uint64(64)*1200 + 500
	if circuit.ConstraintCount != expected {
		t.Errorf("constraint count = %d, want %d", circuit.ConstraintCount, expected)
	}
}

func TestJeanVMAggregationCircuitEvaluate(t *testing.T) {
	msg := types.HexToHash("0xaabb")
	circuit := NewAggregationCircuit(3, msg)

	// Should fail without inputs.
	if circuit.Evaluate() {
		t.Error("evaluate should fail without inputs")
	}

	// Set valid inputs.
	circuit.SetPublicInputs([][]byte{
		{0x01, 0x02},
		{0x03, 0x04},
		{0x05, 0x06},
	})
	circuit.SetAggregateSignature([]byte{0xaa, 0xbb})

	if !circuit.Evaluate() {
		t.Error("evaluate should succeed with valid inputs")
	}
	if !circuit.Satisfied {
		t.Error("circuit should be satisfied")
	}
}

func TestJeanVMAggregationCircuitEvalMismatch(t *testing.T) {
	msg := types.HexToHash("0xaabb")
	circuit := NewAggregationCircuit(3, msg)

	// Wrong number of pubkeys.
	circuit.SetPublicInputs([][]byte{{0x01}, {0x02}})
	circuit.SetAggregateSignature([]byte{0xaa})

	if circuit.Evaluate() {
		t.Error("evaluate should fail on committee size mismatch")
	}
}

func TestJeanVMAggregateWithProof(t *testing.T) {
	agg := NewJeanVMAggregator()
	msg := types.HexToHash("0xcc")

	attestations := makeTestJeanVMAttestations(10)

	proof, err := agg.AggregateWithProof(attestations, msg)
	if err != nil {
		t.Fatalf("AggregateWithProof error: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if len(proof.ProofBytes) != jeanVMProofSize {
		t.Errorf("proof size = %d, want %d", len(proof.ProofBytes), jeanVMProofSize)
	}
	if proof.NumSignatures != 10 {
		t.Errorf("num signatures = %d, want 10", proof.NumSignatures)
	}
	if proof.CommitteeRoot.IsZero() {
		t.Error("committee root should not be zero")
	}
	if len(proof.AggregateSignature) == 0 {
		t.Error("aggregate signature should not be empty")
	}
}

func TestJeanVMAggregateWithProofEmpty(t *testing.T) {
	agg := NewJeanVMAggregator()
	_, err := agg.AggregateWithProof(nil, types.Hash{})
	if err != ErrJeanVMNoAttestations {
		t.Errorf("empty attestations error = %v, want ErrJeanVMNoAttestations", err)
	}
}

func TestJeanVMVerifyAggregationProof(t *testing.T) {
	agg := NewJeanVMAggregator()
	msg := types.HexToHash("0xdd")

	attestations := makeTestJeanVMAttestations(5)
	proof, err := agg.AggregateWithProof(attestations, msg)
	if err != nil {
		t.Fatalf("AggregateWithProof error: %v", err)
	}

	// Extract pubkeys for verification.
	pubkeys := make([][]byte, len(attestations))
	for i, att := range attestations {
		pubkeys[i] = att.PublicKey
	}

	valid, err := agg.VerifyAggregationProof(proof, pubkeys)
	if err != nil {
		t.Fatalf("VerifyAggregationProof error: %v", err)
	}
	if !valid {
		t.Error("proof should be valid")
	}
}

func TestJeanVMVerifyAggregationProofNil(t *testing.T) {
	agg := NewJeanVMAggregator()
	_, err := agg.VerifyAggregationProof(nil, nil)
	if err != ErrJeanVMInvalidProof {
		t.Errorf("nil proof error = %v, want ErrJeanVMInvalidProof", err)
	}
}

func TestJeanVMVerifyAggregationProofCommitteeMismatch(t *testing.T) {
	agg := NewJeanVMAggregator()
	msg := types.HexToHash("0xee")

	attestations := makeTestJeanVMAttestations(5)
	proof, _ := agg.AggregateWithProof(attestations, msg)

	// Wrong number of pubkeys.
	wrongPubkeys := make([][]byte, 3)
	for i := range wrongPubkeys {
		wrongPubkeys[i] = []byte{byte(i)}
	}

	_, err := agg.VerifyAggregationProof(proof, wrongPubkeys)
	if err != ErrJeanVMCommitteeMismatch {
		t.Errorf("committee mismatch error = %v, want ErrJeanVMCommitteeMismatch", err)
	}
}

func TestJeanVMBatchAggregateWithProof(t *testing.T) {
	agg := NewJeanVMAggregator()

	committees := [][]JeanVMAttestationInput{
		makeTestJeanVMAttestations(5),
		makeTestJeanVMAttestations(8),
		makeTestJeanVMAttestations(3),
	}
	messages := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
		types.HexToHash("0x03"),
	}

	proof, err := agg.BatchAggregateWithProof(committees, messages)
	if err != nil {
		t.Fatalf("BatchAggregateWithProof error: %v", err)
	}
	if proof == nil {
		t.Fatal("batch proof should not be nil")
	}
	if proof.NumCommittees != 3 {
		t.Errorf("num committees = %d, want 3", proof.NumCommittees)
	}
	if proof.TotalSignatures != 16 {
		t.Errorf("total signatures = %d, want 16", proof.TotalSignatures)
	}
	if proof.BatchRoot.IsZero() {
		t.Error("batch root should not be zero")
	}
}

func TestJeanVMBatchAggregateEmpty(t *testing.T) {
	agg := NewJeanVMAggregator()
	_, err := agg.BatchAggregateWithProof(nil, nil)
	if err != ErrJeanVMBatchEmpty {
		t.Errorf("empty batch error = %v, want ErrJeanVMBatchEmpty", err)
	}
}

func TestJeanVMVerifyBatchProof(t *testing.T) {
	agg := NewJeanVMAggregator()

	committees := [][]JeanVMAttestationInput{
		makeTestJeanVMAttestations(4),
		makeTestJeanVMAttestations(6),
	}
	messages := []types.Hash{
		types.HexToHash("0xaa"),
		types.HexToHash("0xbb"),
	}

	proof, err := agg.BatchAggregateWithProof(committees, messages)
	if err != nil {
		t.Fatalf("BatchAggregateWithProof error: %v", err)
	}

	valid, err := agg.VerifyBatchProof(proof)
	if err != nil {
		t.Fatalf("VerifyBatchProof error: %v", err)
	}
	if !valid {
		t.Error("batch proof should be valid")
	}
}

func TestJeanVMVerifyBatchProofInvalid(t *testing.T) {
	agg := NewJeanVMAggregator()
	_, err := agg.VerifyBatchProof(nil)
	if err != ErrJeanVMInvalidProof {
		t.Errorf("nil batch proof error = %v, want ErrJeanVMInvalidProof", err)
	}
}

func TestJeanVMEstimateGas(t *testing.T) {
	agg := NewJeanVMAggregator()

	gas := agg.EstimateGas(100)
	expected := uint64(jeanVMBaseGasCost + 100*jeanVMPerSigGasCost)
	if gas != expected {
		t.Errorf("gas = %d, want %d", gas, expected)
	}
}

func TestJeanVMEstimateBatchGas(t *testing.T) {
	agg := NewJeanVMAggregator()

	gas := agg.EstimateBatchGas(4, 200)
	base := uint64(jeanVMBaseGasCost*4 + 200*jeanVMPerSigGasCost)
	discount := base * jeanVMBatchDiscountPercent / 100
	expected := base - discount
	if gas != expected {
		t.Errorf("batch gas = %d, want %d", gas, expected)
	}
}

func TestJeanVMStats(t *testing.T) {
	agg := NewJeanVMAggregator()
	msg := types.HexToHash("0xff")

	attestations := makeTestJeanVMAttestations(5)
	proof, _ := agg.AggregateWithProof(attestations, msg)

	pubkeys := make([][]byte, len(attestations))
	for i, att := range attestations {
		pubkeys[i] = att.PublicKey
	}
	agg.VerifyAggregationProof(proof, pubkeys)

	pg, pv, sa, bp := agg.Stats()
	if pg != 1 {
		t.Errorf("proofs generated = %d, want 1", pg)
	}
	if pv != 1 {
		t.Errorf("proofs verified = %d, want 1", pv)
	}
	if sa != 5 {
		t.Errorf("sigs aggregated = %d, want 5", sa)
	}
	if bp != 0 {
		t.Errorf("batches processed = %d, want 0", bp)
	}
}

func TestJeanVMBatchCircuitCreation(t *testing.T) {
	committees := [][]JeanVMAttestationInput{
		makeTestJeanVMAttestations(10),
		makeTestJeanVMAttestations(20),
	}
	messages := []types.Hash{
		types.HexToHash("0x01"),
		types.HexToHash("0x02"),
	}

	bc := NewBatchAggregationCircuit(committees, messages)
	if bc.TotalSigs != 30 {
		t.Errorf("total sigs = %d, want 30", bc.TotalSigs)
	}
	if bc.ConstraintCnt == 0 {
		t.Error("constraint count should not be zero")
	}
}

func TestJeanVMCommitteeRoot(t *testing.T) {
	pubkeys := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05, 0x06},
		{0x07, 0x08, 0x09},
	}
	root := jeanVMCommitteeRoot(pubkeys)
	if root.IsZero() {
		t.Error("committee root should not be zero")
	}

	// Same pubkeys should produce same root.
	root2 := jeanVMCommitteeRoot(pubkeys)
	if root != root2 {
		t.Error("committee root should be deterministic")
	}

	// Different pubkeys should produce different root.
	pubkeys2 := [][]byte{{0xaa}, {0xbb}}
	root3 := jeanVMCommitteeRoot(pubkeys2)
	if root == root3 {
		t.Error("different pubkeys should produce different root")
	}
}

func TestJeanVMCommitteeRootEmpty(t *testing.T) {
	root := jeanVMCommitteeRoot(nil)
	if !root.IsZero() {
		t.Error("empty committee root should be zero")
	}
}

func TestJeanVMProofSizeConstant(t *testing.T) {
	// Groth16: A(48) + B(96) + C(48) = 192 bytes.
	if jeanVMProofSize != 192 {
		t.Errorf("proof size = %d, want 192", jeanVMProofSize)
	}
}

// makeTestJeanVMAttestations creates n test attestation inputs.
func makeTestJeanVMAttestations(n int) []JeanVMAttestationInput {
	atts := make([]JeanVMAttestationInput, n)
	for i := 0; i < n; i++ {
		pk := make([]byte, 48)
		pk[0] = byte(i + 1)
		pk[1] = byte(i + 100)
		sig := make([]byte, 96)
		sig[0] = byte(i + 50)
		sig[1] = byte(i + 200)
		atts[i] = JeanVMAttestationInput{
			ValidatorIndex: uint64(i),
			PublicKey:      pk,
			Signature:      sig,
			Slot:           uint64(100 + i),
			CommitteeIndex: 0,
		}
	}
	return atts
}

func TestValidateAggregationProof(t *testing.T) {
	// Valid proof.
	proof := &JeanVMAggregationProof{
		ProofBytes:         make([]byte, jeanVMProofSize),
		NumSignatures:      10,
		CommitteeRoot:      types.Hash{0x01},
		AggregateSignature: []byte("sig"),
	}
	if err := ValidateAggregationProof(proof); err != nil {
		t.Errorf("valid proof: %v", err)
	}

	// Nil.
	if err := ValidateAggregationProof(nil); err == nil {
		t.Error("expected error for nil proof")
	}

	// Wrong proof size.
	badSize := &JeanVMAggregationProof{
		ProofBytes: make([]byte, 10), NumSignatures: 1,
		CommitteeRoot: types.Hash{0x01}, AggregateSignature: []byte("sig"),
	}
	if err := ValidateAggregationProof(badSize); err == nil {
		t.Error("expected error for wrong proof size")
	}

	// Zero signatures.
	noSig := &JeanVMAggregationProof{
		ProofBytes: make([]byte, jeanVMProofSize), NumSignatures: 0,
		CommitteeRoot: types.Hash{0x01}, AggregateSignature: []byte("sig"),
	}
	if err := ValidateAggregationProof(noSig); err == nil {
		t.Error("expected error for zero signatures")
	}
}
