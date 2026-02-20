package zkvm

import (
	"encoding/binary"
	"testing"
)

func TestRVWitness_RecordAndCount(t *testing.T) {
	w := NewRVWitnessCollector()

	if w.StepCount() != 0 {
		t.Errorf("initial StepCount: got %d, want 0", w.StepCount())
	}

	var regsBefore, regsAfter [RVRegCount]uint32
	regsBefore[1] = 10
	regsAfter[1] = 10
	regsAfter[2] = 20

	w.RecordStep(0, 0x00A00093, regsBefore, regsAfter, nil)

	if w.StepCount() != 1 {
		t.Errorf("StepCount after 1 record: got %d, want 1", w.StepCount())
	}

	step := w.Steps[0]
	if step.PC != 0 {
		t.Errorf("Step PC: got %d, want 0", step.PC)
	}
	if step.Instruction != 0x00A00093 {
		t.Errorf("Step Instruction: got 0x%08x, want 0x00A00093", step.Instruction)
	}
	if step.RegsBefore[1] != 10 {
		t.Errorf("RegsBefore[1]: got %d, want 10", step.RegsBefore[1])
	}
	if step.RegsAfter[2] != 20 {
		t.Errorf("RegsAfter[2]: got %d, want 20", step.RegsAfter[2])
	}
}

func TestRVWitness_MemoryOps(t *testing.T) {
	w := NewRVWitnessCollector()

	var regs [RVRegCount]uint32
	memOps := []MemOp{
		{Addr: 0x1000, Value: 0xDEAD, IsWrite: true},
		{Addr: 0x1000, Value: 0xDEAD, IsWrite: false},
	}

	w.RecordStep(4, 0x00112023, regs, regs, memOps)

	step := w.Steps[0]
	if len(step.MemoryOps) != 2 {
		t.Fatalf("MemoryOps count: got %d, want 2", len(step.MemoryOps))
	}
	if !step.MemoryOps[0].IsWrite {
		t.Error("first MemOp should be write")
	}
	if step.MemoryOps[1].IsWrite {
		t.Error("second MemOp should be read")
	}
}

func TestRVWitness_SerializeDeserialize(t *testing.T) {
	w := NewRVWitnessCollector()

	var regs1, regs2 [RVRegCount]uint32
	regs1[1] = 42
	regs2[1] = 42
	regs2[2] = 84

	w.RecordStep(0, 0x12345678, regs1, regs2, nil)
	w.RecordStep(4, 0xAABBCCDD, regs2, regs1, []MemOp{
		{Addr: 0x100, Value: 0xFF, IsWrite: true},
	})

	data := w.Serialize()
	if len(data) == 0 {
		t.Fatal("Serialize returned empty data")
	}

	w2, err := DeserializeWitness(data)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}

	if w2.StepCount() != 2 {
		t.Fatalf("deserialized StepCount: got %d, want 2", w2.StepCount())
	}

	// Verify first step.
	s0 := w2.Steps[0]
	if s0.PC != 0 || s0.Instruction != 0x12345678 {
		t.Errorf("step 0: PC=%d Instr=0x%x", s0.PC, s0.Instruction)
	}
	if s0.RegsBefore[1] != 42 {
		t.Errorf("step 0 RegsBefore[1]: got %d, want 42", s0.RegsBefore[1])
	}
	if s0.RegsAfter[2] != 84 {
		t.Errorf("step 0 RegsAfter[2]: got %d, want 84", s0.RegsAfter[2])
	}

	// Verify second step memops.
	s1 := w2.Steps[1]
	if len(s1.MemoryOps) != 1 {
		t.Fatalf("step 1 MemoryOps: got %d, want 1", len(s1.MemoryOps))
	}
	if s1.MemoryOps[0].Addr != 0x100 || s1.MemoryOps[0].Value != 0xFF {
		t.Errorf("step 1 MemOp: addr=0x%x val=0x%x", s1.MemoryOps[0].Addr, s1.MemoryOps[0].Value)
	}
	if !s1.MemoryOps[0].IsWrite {
		t.Error("step 1 MemOp should be write")
	}
}

func TestRVWitness_Reset(t *testing.T) {
	w := NewRVWitnessCollector()
	var regs [RVRegCount]uint32
	w.RecordStep(0, 0, regs, regs, nil)
	w.RecordStep(4, 0, regs, regs, nil)

	if w.StepCount() != 2 {
		t.Fatalf("before reset: %d steps", w.StepCount())
	}

	w.Reset()
	if w.StepCount() != 0 {
		t.Errorf("after reset: got %d steps, want 0", w.StepCount())
	}
}

func TestRVWitness_TraceCommitment(t *testing.T) {
	w := NewRVWitnessCollector()
	var regs [RVRegCount]uint32
	w.RecordStep(0, 0x00100093, regs, regs, nil)
	w.RecordStep(4, 0x00200113, regs, regs, nil)
	w.RecordStep(8, 0x002081B3, regs, regs, nil)

	commitment := w.ComputeTraceCommitment()

	// The commitment should be deterministic.
	commitment2 := w.ComputeTraceCommitment()
	if commitment != commitment2 {
		t.Error("trace commitment is not deterministic")
	}

	// The commitment should be non-zero.
	allZero := true
	for _, b := range commitment {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("trace commitment is all zeros")
	}
}

func TestRVWitness_EmptyCommitment(t *testing.T) {
	w := NewRVWitnessCollector()
	commitment := w.ComputeTraceCommitment()

	// Empty trace should still produce a valid (non-zero) commitment.
	// (SHA-256 of empty input is a known constant.)
	allZero := true
	for _, b := range commitment {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("empty commitment should not be all zeros")
	}
}

func TestRVWitness_DifferentTracesProduceDifferentCommitments(t *testing.T) {
	w1 := NewRVWitnessCollector()
	w2 := NewRVWitnessCollector()

	var regs1, regs2 [RVRegCount]uint32
	regs1[1] = 1
	regs2[1] = 2

	w1.RecordStep(0, 0x00100093, regs1, regs1, nil)
	w2.RecordStep(0, 0x00200093, regs2, regs2, nil)

	c1 := w1.ComputeTraceCommitment()
	c2 := w2.ComputeTraceCommitment()

	if c1 == c2 {
		t.Error("different traces should produce different commitments")
	}
}

func TestRVWitness_MerkleRootSingleStep(t *testing.T) {
	// A single-step trace: the Merkle root equals the leaf hash.
	w := NewRVWitnessCollector()
	var regs [RVRegCount]uint32
	regs[1] = 42
	w.RecordStep(0, 0xABCD, regs, regs, nil)

	commitment := w.ComputeTraceCommitment()
	leafHash := hashWitnessStep(w.Steps[0])

	if commitment != leafHash {
		t.Error("single-step commitment should equal leaf hash")
	}
}

func TestRVWitness_IntegrationWithCPU(t *testing.T) {
	// Run a small program and check that the witness trace is correct.
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 5),    // ADDI x1, x0, 5
		EncodeIType(0x13, 2, 0, 0, 10),   // ADDI x2, x0, 10
		EncodeRType(0x33, 3, 0, 1, 2, 0), // ADD x3, x1, x2
		0x00000073,                         // ECALL (halt)
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

	if cpu.Witness.StepCount() != 4 {
		t.Errorf("expected 4 witness steps, got %d", cpu.Witness.StepCount())
	}

	// Verify the last step captures the ADD result.
	addStep := cpu.Witness.Steps[2]
	if addStep.RegsAfter[3] != 15 {
		t.Errorf("ADD result in witness: got %d, want 15", addStep.RegsAfter[3])
	}

	// Serialize and check round-trip.
	data := cpu.Witness.Serialize()
	restored, err := DeserializeWitness(data)
	if err != nil {
		t.Fatalf("Deserialize: %v", err)
	}
	if restored.StepCount() != cpu.Witness.StepCount() {
		t.Errorf("restored steps: got %d, want %d", restored.StepCount(), cpu.Witness.StepCount())
	}
}
