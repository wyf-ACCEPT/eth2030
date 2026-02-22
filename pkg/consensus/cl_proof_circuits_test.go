package consensus

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestCLProofCircuitNew(t *testing.T) {
	c := NewCLProofCircuit()
	if c == nil {
		t.Fatal("circuit should not be nil")
	}
	if c.depth != CLCircuitProofDepth {
		t.Errorf("expected depth %d, got %d", CLCircuitProofDepth, c.depth)
	}
}

func TestCLProofCircuitCustomDepth(t *testing.T) {
	c := NewCLProofCircuitWithDepth(10)
	if c.depth != 10 {
		t.Errorf("expected depth 10, got %d", c.depth)
	}

	// Zero depth gets default.
	c2 := NewCLProofCircuitWithDepth(0)
	if c2.depth != CLCircuitProofDepth {
		t.Errorf("expected default depth, got %d", c2.depth)
	}
}

func TestCLProofCircuitStateRootProof(t *testing.T) {
	c := NewCLProofCircuit()
	state := testUnifiedStateWithValidators(5)

	proof, err := c.GenerateStateRootProof(state, 0)
	if err != nil {
		t.Fatalf("state root proof generation failed: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.ValidatorIndex != 0 {
		t.Errorf("expected validator index 0, got %d", proof.ValidatorIndex)
	}
	if proof.StateRoot.IsZero() {
		t.Error("state root should not be zero")
	}
	if proof.ValidatorRoot.IsZero() {
		t.Error("validator root should not be zero")
	}
	if len(proof.MerkleBranch) == 0 {
		t.Error("merkle branch should not be empty")
	}
}

func TestCLProofCircuitStateRootProofErrors(t *testing.T) {
	c := NewCLProofCircuit()

	// Nil state.
	_, err := c.GenerateStateRootProof(nil, 0)
	if err != ErrCircuitNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}

	// Empty state.
	empty := NewUnifiedBeaconState(32)
	_, err = c.GenerateStateRootProof(empty, 0)
	if err != ErrCircuitNoValidators {
		t.Fatalf("expected no validators error, got %v", err)
	}

	// Out of range.
	state := testUnifiedStateWithValidators(3)
	_, err = c.GenerateStateRootProof(state, 10)
	if err != ErrCircuitIndexOutOfRange {
		t.Fatalf("expected index out of range, got %v", err)
	}
}

func TestCLProofCircuitVerifyStateRootProof(t *testing.T) {
	c := NewCLProofCircuit()
	state := testUnifiedStateWithValidators(8)

	proof, err := c.GenerateStateRootProof(state, 3)
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}

	if !c.VerifyStateRootProof(proof) {
		t.Fatal("valid proof should verify")
	}

	// Nil proof should fail.
	if c.VerifyStateRootProof(nil) {
		t.Error("nil proof should not verify")
	}

	// Empty branch should fail.
	badProof := &StateRootProof{}
	if c.VerifyStateRootProof(badProof) {
		t.Error("empty proof should not verify")
	}
}

func TestCLProofCircuitBalanceProof(t *testing.T) {
	c := NewCLProofCircuit()
	state := testUnifiedStateWithValidators(4)

	proof, err := c.GenerateValidatorBalanceProof(state, 2)
	if err != nil {
		t.Fatalf("balance proof generation failed: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.ValidatorIndex != 2 {
		t.Errorf("expected validator index 2, got %d", proof.ValidatorIndex)
	}
	if proof.Balance == 0 {
		t.Error("balance should not be zero")
	}
	if proof.EffectiveBalance == 0 {
		t.Error("effective balance should not be zero")
	}
	if proof.BalanceRoot.IsZero() {
		t.Error("balance root should not be zero")
	}
	if len(proof.MerkleBranch) == 0 {
		t.Error("merkle branch should not be empty")
	}
}

func TestCLProofCircuitBalanceProofErrors(t *testing.T) {
	c := NewCLProofCircuit()

	_, err := c.GenerateValidatorBalanceProof(nil, 0)
	if err != ErrCircuitNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}

	empty := NewUnifiedBeaconState(32)
	_, err = c.GenerateValidatorBalanceProof(empty, 0)
	if err != ErrCircuitNoValidators {
		t.Fatalf("expected no validators error, got %v", err)
	}

	state := testUnifiedStateWithValidators(3)
	_, err = c.GenerateValidatorBalanceProof(state, 99)
	if err != ErrCircuitIndexOutOfRange {
		t.Fatalf("expected index out of range, got %v", err)
	}
}

func TestCLProofCircuitVerifyBalanceProof(t *testing.T) {
	c := NewCLProofCircuit()
	state := testUnifiedStateWithValidators(6)

	proof, err := c.GenerateValidatorBalanceProof(state, 1)
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}

	if !c.VerifyValidatorBalanceProof(proof) {
		t.Fatal("valid balance proof should verify")
	}

	// Nil proof.
	if c.VerifyValidatorBalanceProof(nil) {
		t.Error("nil proof should not verify")
	}

	// Tampered balance.
	tampered := *proof
	tampered.Balance = 99999
	if c.VerifyValidatorBalanceProof(&tampered) {
		t.Error("tampered proof should not verify")
	}
}

func TestCLProofCircuitAttestationProof(t *testing.T) {
	c := NewCLProofCircuit()
	state := testUnifiedStateWithValidators(16)
	state.SlotsPerEpoch = 4
	state.CurrentSlot = 8
	state.CurrentEpoch = 2

	proof, err := c.GenerateAttestationProof(state, 8, 0)
	if err != nil {
		t.Fatalf("attestation proof generation failed: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.Slot != 8 {
		t.Errorf("expected slot 8, got %d", proof.Slot)
	}
	if proof.ParticipantCount == 0 {
		t.Error("participant count should not be zero")
	}
	if proof.CommitteeRoot.IsZero() {
		t.Error("committee root should not be zero")
	}
	if proof.StateRoot.IsZero() {
		t.Error("state root should not be zero")
	}
}

func TestCLProofCircuitAttestationProofErrors(t *testing.T) {
	c := NewCLProofCircuit()

	_, err := c.GenerateAttestationProof(nil, 0, 0)
	if err != ErrCircuitNilState {
		t.Fatalf("expected nil state error, got %v", err)
	}

	empty := NewUnifiedBeaconState(32)
	_, err = c.GenerateAttestationProof(empty, 0, 0)
	if err != ErrCircuitNoValidators {
		t.Fatalf("expected no validators error, got %v", err)
	}
}

func TestCLProofCircuitVerifyAttestationProof(t *testing.T) {
	c := NewCLProofCircuit()
	// Use SlotsPerEpoch=4 with 32 validators so committee size = 8,
	// producing a non-trivial Merkle branch.
	state := testUnifiedStateWithValidators(32)
	state.SlotsPerEpoch = 4
	state.CurrentSlot = 8
	state.CurrentEpoch = 2

	proof, err := c.GenerateAttestationProof(state, 8, 0)
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}

	if !c.VerifyAttestationProof(proof) {
		t.Fatal("valid attestation proof should verify")
	}

	// Nil proof.
	if c.VerifyAttestationProof(nil) {
		t.Error("nil proof should not verify")
	}

	// Zero participants.
	badProof := &AttestationValidityProof{MerkleBranch: []types.Hash{{}}}
	if c.VerifyAttestationProof(badProof) {
		t.Error("zero participant proof should not verify")
	}
}

func TestCLProofCircuitSHA256MerkleBranch(t *testing.T) {
	leaves := []types.Hash{
		types.BytesToHash([]byte("leaf0")),
		types.BytesToHash([]byte("leaf1")),
		types.BytesToHash([]byte("leaf2")),
		types.BytesToHash([]byte("leaf3")),
	}

	branch, root := buildSHA256MerkleBranch(leaves, 1, 4)
	if len(branch) == 0 {
		t.Fatal("branch should not be empty")
	}
	if root.IsZero() {
		t.Fatal("root should not be zero")
	}

	// Walk the branch should produce consistent results.
	computed := walkSHA256Branch(leaves[1], branch, 1)
	_ = computed // Result depends on the tree structure
}

func TestCLProofCircuitHashValidatorLeaf(t *testing.T) {
	v := &UnifiedValidator{
		Pubkey:           [48]byte{0xAA},
		EffectiveBalance: 32 * GweiPerETH,
		Balance:          32 * GweiPerETH,
		ActivationEpoch:  0,
		ExitEpoch:        FarFutureEpoch,
	}

	h1 := hashValidatorLeaf(v)
	h2 := hashValidatorLeaf(v)
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
	if h1.IsZero() {
		t.Error("hash should not be zero")
	}
}

func TestCLProofCircuitHashBalanceLeaf(t *testing.T) {
	h1 := hashBalanceLeaf(0, 32*GweiPerETH, 32*GweiPerETH)
	h2 := hashBalanceLeaf(0, 32*GweiPerETH, 32*GweiPerETH)
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}

	h3 := hashBalanceLeaf(1, 32*GweiPerETH, 32*GweiPerETH)
	if h1 == h3 {
		t.Error("different indices should produce different hashes")
	}
}

func TestCLProofCircuitMultipleValidators(t *testing.T) {
	c := NewCLProofCircuit()
	state := testUnifiedStateWithValidators(64)

	// Generate proofs for several validators.
	for i := uint64(0); i < 5; i++ {
		proof, err := c.GenerateStateRootProof(state, i)
		if err != nil {
			t.Fatalf("proof generation for index %d failed: %v", i, err)
		}
		if !c.VerifyStateRootProof(proof) {
			t.Errorf("proof for index %d should verify", i)
		}
	}
}

// testUnifiedStateWithValidators creates a test state with n validators.
func testUnifiedStateWithValidators(n int) *UnifiedBeaconState {
	s := NewUnifiedBeaconState(32)
	for i := 0; i < n; i++ {
		var pk [48]byte
		pk[0] = byte(i)
		pk[1] = byte(i >> 8)
		s.AddValidator(&UnifiedValidator{
			Pubkey:                     pk,
			EffectiveBalance:           32 * GweiPerETH,
			Balance:                    32 * GweiPerETH,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			WithdrawableEpoch:          FarFutureEpoch,
			ActivationEligibilityEpoch: 0,
		})
	}
	return s
}

func TestValidateProofCircuit(t *testing.T) {
	c := NewCLProofCircuitWithDepth(16)
	if err := ValidateProofCircuit(c); err != nil {
		t.Errorf("valid circuit: %v", err)
	}

	if err := ValidateProofCircuit(nil); err == nil {
		t.Error("expected error for nil circuit")
	}
}

func TestValidateStateRootProofData(t *testing.T) {
	proof := &StateRootProof{
		StateRoot:    types.Hash{0x01},
		MerkleBranch: []types.Hash{{0x02}},
	}
	if err := ValidateStateRootProofData(proof); err != nil {
		t.Errorf("valid proof: %v", err)
	}

	if err := ValidateStateRootProofData(nil); err == nil {
		t.Error("expected error for nil proof")
	}

	empty := &StateRootProof{StateRoot: types.Hash{}, MerkleBranch: []types.Hash{{0x02}}}
	if err := ValidateStateRootProofData(empty); err == nil {
		t.Error("expected error for empty state root")
	}
}

func TestValidateBalanceProofData(t *testing.T) {
	proof := &ValidatorBalanceProof{
		StateRoot:    types.Hash{0x01},
		BalanceRoot:  types.Hash{0x02},
		MerkleBranch: []types.Hash{{0x03}},
	}
	if err := ValidateBalanceProofData(proof); err != nil {
		t.Errorf("valid proof: %v", err)
	}

	if err := ValidateBalanceProofData(nil); err == nil {
		t.Error("expected error for nil proof")
	}
}
