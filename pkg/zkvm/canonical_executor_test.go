package zkvm

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// buildHaltProgram constructs a minimal RISC-V program that halts with exit code 0.
// It sets a7=0 (ecall halt), a0=0 (exit code 0), then ecalls.
func buildCanonExecHaltProgram() []byte {
	instrs := []uint32{
		// ADDI x17, x0, 0 -> a7 = RVEcallHalt (0)
		EncodeIType(0x13, 17, 0, 0, 0),
		// ADDI x10, x0, 0 -> a0 = 0 (exit code)
		EncodeIType(0x13, 10, 0, 0, 0),
		// ECALL
		0x00000073,
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

// buildOutputProgram constructs a program that reads an input byte and writes
// it to the output buffer, then halts.
func buildCanonExecOutputProgram() []byte {
	instrs := []uint32{
		// ADDI x17, x0, 2 -> a7 = RVEcallInput
		EncodeIType(0x13, 17, 0, 0, 2),
		// ADDI x10, x0, 0
		EncodeIType(0x13, 10, 0, 0, 0),
		// ECALL (input: read byte into a0)
		0x00000073,
		// ADDI x17, x0, 1 -> a7 = RVEcallOutput
		EncodeIType(0x13, 17, 0, 0, 1),
		// ECALL (output: write a0 to output)
		0x00000073,
		// ADDI x17, x0, 0 -> a7 = RVEcallHalt
		EncodeIType(0x13, 17, 0, 0, 0),
		// ADDI x10, x0, 0 -> exit code 0
		EncodeIType(0x13, 10, 0, 0, 0),
		// ECALL (halt)
		0x00000073,
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

func TestCanonExec_NewCanonicalExecutor(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()

	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}
	if exec == nil {
		t.Fatal("executor is nil")
	}
}

func TestCanonExec_NilRegistry(t *testing.T) {
	config := DefaultCanonicalExecutorConfig()
	_, err := NewCanonicalExecutor(nil, config)
	if !errors.Is(err, ErrCanonExecNilRegistry) {
		t.Fatalf("expected ErrCanonExecNilRegistry, got %v", err)
	}
}

func TestCanonExec_ExecuteGuestHalt(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	output, witness, err := exec.ExecuteGuest(programID, nil)
	if err != nil {
		t.Fatalf("ExecuteGuest: %v", err)
	}
	if output.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", output.ExitCode)
	}
	if output.GasUsed == 0 {
		t.Error("gas used should be > 0")
	}
	if witness == nil {
		t.Error("witness should not be nil when CollectWitness is enabled")
	}
	if witness.StepCount() == 0 {
		t.Error("witness step count should be > 0")
	}
}

func TestCanonExec_ExecuteGuestOutput(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecOutputProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	input := []byte{0x42}
	output, _, err := exec.ExecuteGuest(programID, input)
	if err != nil {
		t.Fatalf("ExecuteGuest: %v", err)
	}
	if len(output.Output) != 1 {
		t.Fatalf("output length: got %d, want 1", len(output.Output))
	}
	if output.Output[0] != 0x42 {
		t.Errorf("output byte: got 0x%02x, want 0x42", output.Output[0])
	}
}

func TestCanonExec_ProgramNotFound(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	fakeID := crypto.Keccak256Hash([]byte("nonexistent"))
	_, _, err = exec.ExecuteGuest(fakeID, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent program")
	}
	if !errors.Is(err, ErrCanonExecProgramNotFound) {
		t.Fatalf("expected ErrCanonExecProgramNotFound, got %v", err)
	}
}

func TestCanonExec_GasExhausted(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	config.GasLimit = 1 // Only 1 instruction allowed
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	_, _, err = exec.ExecuteGuest(programID, nil)
	if !errors.Is(err, ErrCanonExecGasExhausted) {
		t.Fatalf("expected ErrCanonExecGasExhausted, got %v", err)
	}
}

func TestCanonExec_ExecuteAndProve(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	output, proof, err := exec.ExecuteAndProve(programID, []byte("test input"))
	if err != nil {
		t.Fatalf("ExecuteAndProve: %v", err)
	}
	if output == nil {
		t.Fatal("output is nil")
	}
	if proof == nil {
		t.Fatal("proof is nil")
	}
	if proof.ProofResult == nil {
		t.Fatal("proof.ProofResult is nil")
	}
	if len(proof.ProofResult.ProofBytes) != groth16ProofSize {
		t.Errorf("proof size: got %d, want %d", len(proof.ProofResult.ProofBytes), groth16ProofSize)
	}
}

func TestCanonExec_VerifyGuestProof(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	_, proof, err := exec.ExecuteAndProve(programID, []byte("verify me"))
	if err != nil {
		t.Fatalf("ExecuteAndProve: %v", err)
	}

	if err := exec.VerifyGuestProof(proof); err != nil {
		t.Fatalf("VerifyGuestProof: %v", err)
	}
}

func TestCanonExec_VerifyGuestProofTampered(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	_, proof, err := exec.ExecuteAndProve(programID, []byte("tamper test"))
	if err != nil {
		t.Fatalf("ExecuteAndProve: %v", err)
	}

	// Tamper with the proof bytes.
	proof.ProofResult.ProofBytes[0] ^= 0xFF

	if err := exec.VerifyGuestProof(proof); err == nil {
		t.Fatal("expected verification failure for tampered proof")
	}
}

func TestCanonExec_VerifyNilProof(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	err = exec.VerifyGuestProof(nil)
	if !errors.Is(err, ErrCanonExecNilProof) {
		t.Fatalf("expected ErrCanonExecNilProof, got %v", err)
	}
}

func TestCanonExec_ComputeProgramID(t *testing.T) {
	program := []byte{1, 2, 3, 4}
	id := ComputeProgramID(program)
	expected := crypto.Keccak256Hash(program)
	if id != expected {
		t.Errorf("ComputeProgramID mismatch: got %x, want %x", id, expected)
	}
}

func TestCanonExec_DefaultConfig(t *testing.T) {
	config := DefaultCanonicalExecutorConfig()
	if config.GasLimit == 0 {
		t.Error("default GasLimit should be > 0")
	}
	if config.MemoryPages == 0 {
		t.Error("default MemoryPages should be > 0")
	}
	if config.ProgramBase == 0 {
		t.Error("default ProgramBase should be > 0")
	}
	if !config.CollectWitness {
		t.Error("default CollectWitness should be true")
	}
}

func TestCanonExec_WitnessTrace(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	_, witness, err := exec.ExecuteGuest(programID, nil)
	if err != nil {
		t.Fatalf("ExecuteGuest: %v", err)
	}

	// Verify witness can be serialized and deserialized.
	serialized := witness.Serialize()
	if len(serialized) == 0 {
		t.Fatal("serialized witness is empty")
	}

	deserialized, err := DeserializeWitness(serialized)
	if err != nil {
		t.Fatalf("DeserializeWitness: %v", err)
	}
	if deserialized.StepCount() != witness.StepCount() {
		t.Errorf("step count mismatch: got %d, want %d", deserialized.StepCount(), witness.StepCount())
	}
}

func TestCanonExec_ProofDeterminism(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	input := []byte("determinism test")

	_, proof1, err := exec.ExecuteAndProve(programID, input)
	if err != nil {
		t.Fatalf("first ExecuteAndProve: %v", err)
	}

	_, proof2, err := exec.ExecuteAndProve(programID, input)
	if err != nil {
		t.Fatalf("second ExecuteAndProve: %v", err)
	}

	// Same program + same input = same proof.
	if len(proof1.ProofResult.ProofBytes) != len(proof2.ProofResult.ProofBytes) {
		t.Fatal("proof sizes differ")
	}
	for i := range proof1.ProofResult.ProofBytes {
		if proof1.ProofResult.ProofBytes[i] != proof2.ProofResult.ProofBytes[i] {
			t.Fatalf("proof byte %d differs", i)
		}
	}
}

func TestCanonExec_BuildPublicInputs(t *testing.T) {
	output := &GuestOutput{
		Output:      []byte("hello"),
		ProgramHash: [32]byte{1, 2, 3},
	}
	input := []byte("world")
	pi := buildPublicInputsFromOutput(output, input)
	if len(pi) != 32+32+32+8 {
		t.Errorf("public inputs length: got %d, want %d", len(pi), 104)
	}
}
