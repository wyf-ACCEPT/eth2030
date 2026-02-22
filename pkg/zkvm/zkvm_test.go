package zkvm

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Verifier tests ---

func TestVerifyProofValid(t *testing.T) {
	vk := &VerificationKey{
		Data:        []byte("test-vk"),
		ProgramHash: types.Hash{0x01},
	}
	proof := &Proof{
		Data:         []byte("test-proof"),
		PublicInputs: []byte("test-inputs"),
	}

	valid, err := VerifyProof(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected valid proof")
	}
}

func TestVerifyProofNilVK(t *testing.T) {
	proof := &Proof{Data: []byte("data"), PublicInputs: []byte("in")}
	_, err := VerifyProof(nil, proof)
	if err != ErrNilVerificationKey {
		t.Errorf("expected ErrNilVerificationKey, got %v", err)
	}
}

func TestVerifyProofNilProof(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	_, err := VerifyProof(vk, nil)
	if err != ErrNilProof {
		t.Errorf("expected ErrNilProof, got %v", err)
	}
}

func TestVerifyProofEmptyVK(t *testing.T) {
	vk := &VerificationKey{Data: []byte{}}
	proof := &Proof{Data: []byte("proof"), PublicInputs: []byte("in")}
	_, err := VerifyProof(vk, proof)
	if err != ErrEmptyVKData {
		t.Errorf("expected ErrEmptyVKData, got %v", err)
	}
}

func TestVerifyProofEmptyProofData(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte{}, PublicInputs: []byte("in")}
	_, err := VerifyProof(vk, proof)
	if err != ErrEmptyProofData {
		t.Errorf("expected ErrEmptyProofData, got %v", err)
	}
}

func TestVerifyProofNoPublicInputs(t *testing.T) {
	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("proof"), PublicInputs: nil}
	valid, err := VerifyProof(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected invalid proof with no public inputs")
	}
}

func TestMockVerifierProve(t *testing.T) {
	v := &MockVerifier{}
	program := &GuestProgram{
		Code:       []byte("test-code"),
		EntryPoint: "main",
		Version:    1,
	}

	proof, err := v.Prove(program, []byte("input"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
	if !bytes.HasPrefix(proof.Data, []byte("mock-proof:")) {
		t.Error("expected mock proof prefix")
	}
}

func TestMockVerifierProveFails(t *testing.T) {
	v := &MockVerifier{}
	_, err := v.Prove(nil, []byte("input"))
	if err == nil {
		t.Error("expected error for nil program")
	}

	_, err = v.Prove(&GuestProgram{}, []byte("input"))
	if err == nil {
		t.Error("expected error for empty program")
	}
}

func TestMockVerifierName(t *testing.T) {
	v := &MockVerifier{}
	if v.Name() != "mock" {
		t.Errorf("expected name 'mock', got '%s'", v.Name())
	}
}

func TestRejectingVerifier(t *testing.T) {
	v := &RejectingVerifier{}

	if v.Name() != "rejecting" {
		t.Errorf("expected name 'rejecting', got '%s'", v.Name())
	}

	_, err := v.Prove(&GuestProgram{Code: []byte("x")}, nil)
	if err == nil {
		t.Error("expected error from rejecting prover")
	}

	vk := &VerificationKey{Data: []byte("vk")}
	proof := &Proof{Data: []byte("proof"), PublicInputs: []byte("in")}
	valid, err := v.Verify(vk, proof)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("rejecting verifier should never return valid")
	}
}

// --- Guest context tests ---

func TestGuestContextCreation(t *testing.T) {
	stateRoot := types.Hash{0x11, 0x22}
	witness := []byte("test-witness")

	ctx := NewGuestContext(stateRoot, witness)
	if ctx.StateRoot() != stateRoot {
		t.Error("state root mismatch")
	}
	if !bytes.Equal(ctx.Witness(), witness) {
		t.Error("witness mismatch")
	}
	if ctx.ChainID() != 0 {
		t.Errorf("expected default chainID 0, got %d", ctx.ChainID())
	}
	if ctx.IsExecuted() {
		t.Error("expected not executed")
	}
}

func TestGuestContextWithChain(t *testing.T) {
	ctx := NewGuestContextWithChain(types.Hash{}, nil, 42)
	if ctx.ChainID() != 42 {
		t.Errorf("expected chainID 42, got %d", ctx.ChainID())
	}
}

func TestExecuteBlockBasic(t *testing.T) {
	stateRoot := types.Hash{0xaa}
	witness := []byte("witness")
	blockData := []byte("block-data")

	ctx := NewGuestContext(stateRoot, witness)
	postState, err := ExecuteBlock(ctx, blockData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Post-state should be non-zero and different from pre-state.
	if postState == (types.Hash{}) {
		t.Error("expected non-zero post-state root")
	}
	if postState == stateRoot {
		t.Error("expected post-state to differ from pre-state")
	}

	// Context should be marked as executed.
	if !ctx.IsExecuted() {
		t.Error("expected context to be executed")
	}
}

func TestExecuteBlockNilContext(t *testing.T) {
	_, err := ExecuteBlock(nil, []byte("block"))
	if err != ErrNilGuestContext {
		t.Errorf("expected ErrNilGuestContext, got %v", err)
	}
}

func TestExecuteBlockEmptyData(t *testing.T) {
	ctx := NewGuestContext(types.Hash{}, nil)
	_, err := ExecuteBlock(ctx, nil)
	if err != ErrEmptyBlockData {
		t.Errorf("expected ErrEmptyBlockData, got %v", err)
	}

	_, err = ExecuteBlock(ctx, []byte{})
	if err != ErrEmptyBlockData {
		t.Errorf("expected ErrEmptyBlockData for empty slice, got %v", err)
	}
}

func TestExecuteBlockDoubleExecution(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0xaa}, nil)

	_, err := ExecuteBlock(ctx, []byte("block1"))
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}

	_, err = ExecuteBlock(ctx, []byte("block2"))
	if err == nil {
		t.Error("expected error on double execution")
	}
}

func TestExecuteBlockDeterministic(t *testing.T) {
	stateRoot := types.Hash{0xbb}
	witness := []byte("w")
	block := []byte("block")

	ctx1 := NewGuestContext(stateRoot, witness)
	ctx2 := NewGuestContext(stateRoot, witness)

	r1, _ := ExecuteBlock(ctx1, block)
	r2, _ := ExecuteBlock(ctx2, block)

	if r1 != r2 {
		t.Error("expected deterministic results for same inputs")
	}
}

func TestExecuteBlockFull(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0xcc}, []byte("witness"))
	result, err := ExecuteBlockFull(ctx, []byte("block"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("expected success=true")
	}
	if result.PreStateRoot != (types.Hash{0xcc}) {
		t.Error("pre-state root mismatch")
	}
	if result.PostStateRoot == (types.Hash{}) {
		t.Error("expected non-zero post-state root")
	}
	if result.ReceiptsRoot == (types.Hash{}) {
		t.Error("expected non-zero receipts root")
	}
	if result.GasUsed == 0 {
		t.Error("expected non-zero gas used")
	}
}

func TestExecuteBlockFullFailure(t *testing.T) {
	ctx := NewGuestContext(types.Hash{0xdd}, nil)
	result, err := ExecuteBlockFull(ctx, nil)
	if err == nil {
		t.Error("expected error for nil block data")
	}
	if result.Success {
		t.Error("expected success=false on failure")
	}
}

// --- ProverBackend interface tests ---

func TestProverBackendInterface(t *testing.T) {
	// Verify both MockVerifier and RejectingVerifier implement ProverBackend.
	var _ ProverBackend = (*MockVerifier)(nil)
	var _ ProverBackend = (*RejectingVerifier)(nil)
}

// --- Type tests ---

func TestGuestProgramFields(t *testing.T) {
	p := GuestProgram{
		Code:       []byte{0x01, 0x02},
		EntryPoint: "execute",
		Version:    3,
	}
	if p.EntryPoint != "execute" {
		t.Errorf("expected entry point 'execute', got '%s'", p.EntryPoint)
	}
	if p.Version != 3 {
		t.Errorf("expected version 3, got %d", p.Version)
	}
}

func TestGuestInputFields(t *testing.T) {
	gi := GuestInput{
		ChainID:     1,
		BlockData:   []byte("block"),
		WitnessData: []byte("witness"),
	}
	if gi.ChainID != 1 {
		t.Errorf("expected chainID 1, got %d", gi.ChainID)
	}
}
