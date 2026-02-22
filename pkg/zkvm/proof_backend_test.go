package zkvm

import (
	"encoding/binary"
	"testing"
)

// buildTestTrace creates a witness trace from a small program run.
func buildTestTrace(t *testing.T) *RVWitnessCollector {
	t.Helper()
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 7),    // ADDI x1, x0, 7
		EncodeIType(0x13, 2, 0, 0, 6),    // ADDI x2, x0, 6
		EncodeRType(0x33, 3, 0, 1, 2, 1), // MUL x3, x1, x2 (funct7=1)
		0x00000073,                       // ECALL (halt)
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}

	cpu := NewRVCPU(100)
	if err := cpu.LoadProgram(code, 0, 0); err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	cpu.Regs[17] = RVEcallHalt
	cpu.Witness = NewRVWitnessCollector()

	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return cpu.Witness
}

func TestProofBackend_ProveExecution(t *testing.T) {
	trace := buildTestTrace(t)

	programHash := HashProgram([]byte("test-program"))
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs-data"),
		ProgramHash:  programHash,
	}

	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	if len(result.ProofBytes) != groth16ProofSize {
		t.Errorf("ProofBytes length: got %d, want %d", len(result.ProofBytes), groth16ProofSize)
	}

	if len(result.VerificationKey) != 32 {
		t.Errorf("VerificationKey length: got %d, want 32", len(result.VerificationKey))
	}

	// Trace commitment should be non-zero.
	allZero := true
	for _, b := range result.TraceCommitment {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("TraceCommitment is all zeros")
	}
}

func TestProofBackend_VerifyExecProof(t *testing.T) {
	trace := buildTestTrace(t)

	programHash := HashProgram([]byte("test-program"))
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs-data"),
		ProgramHash:  programHash,
	}

	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	// Verify should pass with correct program hash.
	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		t.Fatalf("VerifyExecProof: %v", err)
	}
	if !valid {
		t.Error("VerifyExecProof returned false for valid proof")
	}
}

func TestProofBackend_VerifyWithWrongProgramHash(t *testing.T) {
	trace := buildTestTrace(t)

	programHash := HashProgram([]byte("test-program"))
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs-data"),
		ProgramHash:  programHash,
	}

	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	// Verify with wrong program hash should fail.
	wrongHash := HashProgram([]byte("wrong-program"))
	valid, err := VerifyExecProof(result, wrongHash)
	if err != nil {
		t.Fatalf("VerifyExecProof error: %v", err)
	}
	if valid {
		t.Error("VerifyExecProof should fail with wrong program hash")
	}
}

func TestProofBackend_NilRequest(t *testing.T) {
	_, err := ProveExecution(nil)
	if err != ErrProofBackendNilRequest {
		t.Errorf("expected ErrProofBackendNilRequest, got %v", err)
	}
}

func TestProofBackend_NilTrace(t *testing.T) {
	req := &ProofRequest{
		Trace:        nil,
		PublicInputs: []byte("data"),
	}
	_, err := ProveExecution(req)
	if err != ErrProofBackendNilTrace {
		t.Errorf("expected ErrProofBackendNilTrace, got %v", err)
	}
}

func TestProofBackend_EmptyTrace(t *testing.T) {
	req := &ProofRequest{
		Trace:        NewRVWitnessCollector(),
		PublicInputs: []byte("data"),
	}
	_, err := ProveExecution(req)
	if err != ErrProofBackendEmptyTrace {
		t.Errorf("expected ErrProofBackendEmptyTrace, got %v", err)
	}
}

func TestProofBackend_VerifyNilResult(t *testing.T) {
	_, err := VerifyExecProof(nil, [32]byte{})
	if err != ErrProofBackendNilResult {
		t.Errorf("expected ErrProofBackendNilResult, got %v", err)
	}
}

func TestProofBackend_VerifyBadProofLength(t *testing.T) {
	result := &ProofResult{
		ProofBytes: []byte("too-short"),
	}
	_, err := VerifyExecProof(result, [32]byte{})
	if err != ErrProofBackendBadLength {
		t.Errorf("expected ErrProofBackendBadLength, got %v", err)
	}
}

func TestProofBackend_TamperedProof(t *testing.T) {
	trace := buildTestTrace(t)

	programHash := HashProgram([]byte("test-program"))
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs-data"),
		ProgramHash:  programHash,
	}

	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	// Tamper with proof bytes.
	result.ProofBytes[0] ^= 0xFF

	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		t.Fatalf("VerifyExecProof error: %v", err)
	}
	if valid {
		t.Error("VerifyExecProof should fail for tampered proof")
	}
}

func TestProofBackend_TamperedVK(t *testing.T) {
	trace := buildTestTrace(t)

	programHash := HashProgram([]byte("test-program"))
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs-data"),
		ProgramHash:  programHash,
	}

	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	// Tamper with VK.
	result.VerificationKey[0] ^= 0xFF

	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		t.Fatalf("VerifyExecProof error: %v", err)
	}
	if valid {
		t.Error("VerifyExecProof should fail for tampered VK")
	}
}

func TestProofBackend_DeterministicProof(t *testing.T) {
	trace := buildTestTrace(t)

	programHash := HashProgram([]byte("test-program"))
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs-data"),
		ProgramHash:  programHash,
	}

	result1, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution 1: %v", err)
	}
	result2, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution 2: %v", err)
	}

	// Same trace + inputs should produce the same proof.
	if len(result1.ProofBytes) != len(result2.ProofBytes) {
		t.Fatal("proof lengths differ")
	}
	for i := range result1.ProofBytes {
		if result1.ProofBytes[i] != result2.ProofBytes[i] {
			t.Errorf("proof byte %d differs", i)
			break
		}
	}
}

func TestProofBackend_DifferentInputsProduceDifferentProofs(t *testing.T) {
	trace := buildTestTrace(t)
	programHash := HashProgram([]byte("test-program"))

	req1 := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("input-A"),
		ProgramHash:  programHash,
	}
	req2 := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("input-B"),
		ProgramHash:  programHash,
	}

	result1, _ := ProveExecution(req1)
	result2, _ := ProveExecution(req2)

	same := true
	for i := range result1.ProofBytes {
		if result1.ProofBytes[i] != result2.ProofBytes[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different inputs should produce different proofs")
	}
}

func TestProofBackend_HashProgram(t *testing.T) {
	h1 := HashProgram([]byte("program-A"))
	h2 := HashProgram([]byte("program-B"))
	h3 := HashProgram([]byte("program-A"))

	if h1 == h2 {
		t.Error("different programs should have different hashes")
	}
	if h1 != h3 {
		t.Error("same program should have same hash")
	}
}

func TestProofBackend_EndToEnd(t *testing.T) {
	// Full end-to-end: build program -> execute with witness -> prove -> verify.
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 100),  // x1 = 100
		EncodeIType(0x13, 2, 0, 0, 200),  // x2 = 200
		EncodeRType(0x33, 3, 0, 1, 2, 0), // ADD x3 = x1+x2 = 300
		EncodeUType(0x37, 4, 0x00001000), // LUI x4, 0x1000
		EncodeSType(0x23, 2, 4, 3, 0),    // SW x3, 0(x4)
		EncodeIType(0x03, 5, 2, 4, 0),    // LW x5, 0(x4)
		0x00000073,                       // ECALL halt
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}

	cpu := NewRVCPU(200)
	if err := cpu.LoadProgram(code, 0, 0); err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	cpu.Regs[17] = RVEcallHalt
	cpu.Witness = NewRVWitnessCollector()

	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if cpu.Regs[5] != 300 {
		t.Fatalf("x5: got %d, want 300", cpu.Regs[5])
	}

	// Generate proof.
	programHash := HashProgram(code)
	req := &ProofRequest{
		Trace:        cpu.Witness,
		PublicInputs: code[:8], // Use first 2 instructions as public input.
		ProgramHash:  programHash,
	}

	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution: %v", err)
	}

	// Verify proof.
	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		t.Fatalf("VerifyExecProof: %v", err)
	}
	if !valid {
		t.Error("end-to-end proof verification failed")
	}
}
