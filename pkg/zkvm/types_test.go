package zkvm

import (
	"bytes"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestGuestProgramStruct(t *testing.T) {
	code := []byte{0x01, 0x02, 0x03}
	p := GuestProgram{
		Code:       code,
		EntryPoint: "main",
		Version:    5,
	}

	if !bytes.Equal(p.Code, code) {
		t.Error("Code mismatch")
	}
	if p.EntryPoint != "main" {
		t.Errorf("EntryPoint = %q, want %q", p.EntryPoint, "main")
	}
	if p.Version != 5 {
		t.Errorf("Version = %d, want 5", p.Version)
	}
}

func TestGuestProgramEmptyFields(t *testing.T) {
	p := GuestProgram{}

	if p.Code != nil {
		t.Error("expected nil Code")
	}
	if p.EntryPoint != "" {
		t.Errorf("expected empty EntryPoint, got %q", p.EntryPoint)
	}
	if p.Version != 0 {
		t.Errorf("expected Version 0, got %d", p.Version)
	}
}

func TestVerificationKeyStruct(t *testing.T) {
	data := []byte{0xAA, 0xBB}
	hash := types.Hash{0x11, 0x22}

	vk := VerificationKey{
		Data:        data,
		ProgramHash: hash,
	}

	if !bytes.Equal(vk.Data, data) {
		t.Error("Data mismatch")
	}
	if vk.ProgramHash != hash {
		t.Error("ProgramHash mismatch")
	}
}

func TestVerificationKeyZeroValue(t *testing.T) {
	vk := VerificationKey{}

	if vk.Data != nil {
		t.Error("expected nil Data")
	}
	if vk.ProgramHash != (types.Hash{}) {
		t.Error("expected zero ProgramHash")
	}
}

func TestProofStruct(t *testing.T) {
	proofData := []byte("proof-bytes")
	inputs := []byte("public-inputs")

	p := Proof{
		Data:         proofData,
		PublicInputs: inputs,
	}

	if !bytes.Equal(p.Data, proofData) {
		t.Error("Data mismatch")
	}
	if !bytes.Equal(p.PublicInputs, inputs) {
		t.Error("PublicInputs mismatch")
	}
}

func TestProofZeroValue(t *testing.T) {
	p := Proof{}

	if p.Data != nil {
		t.Error("expected nil Data")
	}
	if p.PublicInputs != nil {
		t.Error("expected nil PublicInputs")
	}
}

func TestExecutionResultStruct(t *testing.T) {
	pre := types.Hash{0x01}
	post := types.Hash{0x02}
	receipts := types.Hash{0x03}

	r := ExecutionResult{
		PreStateRoot:  pre,
		PostStateRoot: post,
		ReceiptsRoot:  receipts,
		GasUsed:       21000,
		Success:       true,
	}

	if r.PreStateRoot != pre {
		t.Error("PreStateRoot mismatch")
	}
	if r.PostStateRoot != post {
		t.Error("PostStateRoot mismatch")
	}
	if r.ReceiptsRoot != receipts {
		t.Error("ReceiptsRoot mismatch")
	}
	if r.GasUsed != 21000 {
		t.Errorf("GasUsed = %d, want 21000", r.GasUsed)
	}
	if !r.Success {
		t.Error("expected Success = true")
	}
}

func TestExecutionResultZeroValue(t *testing.T) {
	r := ExecutionResult{}

	if r.PreStateRoot != (types.Hash{}) {
		t.Error("expected zero PreStateRoot")
	}
	if r.PostStateRoot != (types.Hash{}) {
		t.Error("expected zero PostStateRoot")
	}
	if r.ReceiptsRoot != (types.Hash{}) {
		t.Error("expected zero ReceiptsRoot")
	}
	if r.GasUsed != 0 {
		t.Errorf("expected GasUsed 0, got %d", r.GasUsed)
	}
	if r.Success {
		t.Error("expected Success = false")
	}
}

func TestGuestInputStruct(t *testing.T) {
	gi := GuestInput{
		ChainID:     1,
		BlockData:   []byte("rlp-block"),
		WitnessData: []byte("rlp-witness"),
	}

	if gi.ChainID != 1 {
		t.Errorf("ChainID = %d, want 1", gi.ChainID)
	}
	if !bytes.Equal(gi.BlockData, []byte("rlp-block")) {
		t.Error("BlockData mismatch")
	}
	if !bytes.Equal(gi.WitnessData, []byte("rlp-witness")) {
		t.Error("WitnessData mismatch")
	}
}

func TestGuestInputZeroValue(t *testing.T) {
	gi := GuestInput{}

	if gi.ChainID != 0 {
		t.Errorf("expected ChainID 0, got %d", gi.ChainID)
	}
	if gi.BlockData != nil {
		t.Error("expected nil BlockData")
	}
	if gi.WitnessData != nil {
		t.Error("expected nil WitnessData")
	}
}

func TestGuestInputLargeChainID(t *testing.T) {
	gi := GuestInput{
		ChainID: ^uint64(0), // max uint64
	}
	if gi.ChainID != ^uint64(0) {
		t.Error("max uint64 chain ID not preserved")
	}
}

func TestProverBackendInterfaceImplementations(t *testing.T) {
	// Verify known types implement ProverBackend.
	var _ ProverBackend = (*MockVerifier)(nil)
	var _ ProverBackend = (*RejectingVerifier)(nil)
}
