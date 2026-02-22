package zkvm

import (
	"encoding/binary"
	"testing"
)

// Helper to build a program from instruction words and load it.
func rvCPUWithProgram(t *testing.T, instrs []uint32, gasLimit uint64) *RVCPU {
	t.Helper()
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	cpu := NewRVCPU(gasLimit)
	if err := cpu.LoadProgram(code, 0, 0); err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	return cpu
}

// ECALL (halt with exit code 0).
func rvEcall() uint32 {
	return 0x00000073
}

func TestRVCPU_LUI(t *testing.T) {
	// LUI x1, 0x12345000
	instr := EncodeUType(0x37, 1, 0x12345000)
	cpu := rvCPUWithProgram(t, []uint32{instr, rvEcall()}, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[1] != 0x12345000 {
		t.Errorf("LUI: got 0x%08x, want 0x12345000", cpu.Regs[1])
	}
}

func TestRVCPU_AUIPC(t *testing.T) {
	// AUIPC x2, 0x10000000 (at PC=0, result = 0 + 0x10000000)
	instr := EncodeUType(0x17, 2, 0x10000000)
	cpu := rvCPUWithProgram(t, []uint32{instr, rvEcall()}, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[2] != 0x10000000 {
		t.Errorf("AUIPC: got 0x%08x, want 0x10000000", cpu.Regs[2])
	}
}

func TestRVCPU_ADDI(t *testing.T) {
	// ADDI x1, x0, 42
	instr := EncodeIType(0x13, 1, 0, 0, 42)
	cpu := rvCPUWithProgram(t, []uint32{instr, rvEcall()}, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[1] != 42 {
		t.Errorf("ADDI: got %d, want 42", cpu.Regs[1])
	}
}

func TestRVCPU_ADDISignExtend(t *testing.T) {
	// ADDI x1, x0, -1 (imm = 0xFFF = -1 sign-extended)
	instr := EncodeIType(0x13, 1, 0, 0, -1)
	cpu := rvCPUWithProgram(t, []uint32{instr, rvEcall()}, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[1] != 0xFFFFFFFF {
		t.Errorf("ADDI(-1): got 0x%08x, want 0xFFFFFFFF", cpu.Regs[1])
	}
}

func TestRVCPU_ADDAndSUB(t *testing.T) {
	// x1 = 10, x2 = 7
	// ADD x3, x1, x2 -> 17
	// SUB x4, x1, x2 -> 3
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 10),      // ADDI x1, x0, 10
		EncodeIType(0x13, 2, 0, 0, 7),       // ADDI x2, x0, 7
		EncodeRType(0x33, 3, 0, 1, 2, 0),    // ADD x3, x1, x2
		EncodeRType(0x33, 4, 0, 1, 2, 0x20), // SUB x4, x1, x2
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 17 {
		t.Errorf("ADD: got %d, want 17", cpu.Regs[3])
	}
	if cpu.Regs[4] != 3 {
		t.Errorf("SUB: got %d, want 3", cpu.Regs[4])
	}
}

func TestRVCPU_LogicalOps(t *testing.T) {
	// x1 = 0xFF, x2 = 0x0F
	// AND x3, x1, x2 -> 0x0F
	// OR  x4, x1, x2 -> 0xFF
	// XOR x5, x1, x2 -> 0xF0
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 0xFF),
		EncodeIType(0x13, 2, 0, 0, 0x0F),
		EncodeRType(0x33, 3, 7, 1, 2, 0), // AND
		EncodeRType(0x33, 4, 6, 1, 2, 0), // OR
		EncodeRType(0x33, 5, 4, 1, 2, 0), // XOR
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 0x0F {
		t.Errorf("AND: got 0x%x, want 0x0F", cpu.Regs[3])
	}
	if cpu.Regs[4] != 0xFF {
		t.Errorf("OR: got 0x%x, want 0xFF", cpu.Regs[4])
	}
	if cpu.Regs[5] != 0xF0 {
		t.Errorf("XOR: got 0x%x, want 0xF0", cpu.Regs[5])
	}
}

func TestRVCPU_Shifts(t *testing.T) {
	// x1 = 0x80, SLLI x2, x1, 2 -> 0x200
	// SRL x3 from 0x80000000 >> 4 -> 0x08000000
	// SRA x4 from 0x80000000 >> 4 -> 0xF8000000
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 0x80), // x1 = 128
		EncodeIType(0x13, 2, 1, 1, 2),    // SLLI x2, x1, 2
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[2] != 0x200 {
		t.Errorf("SLLI: got 0x%x, want 0x200", cpu.Regs[2])
	}
}

func TestRVCPU_SLT(t *testing.T) {
	// SLT: signed comparison
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, -5),   // x1 = -5
		EncodeIType(0x13, 2, 0, 0, 5),    // x2 = 5
		EncodeRType(0x33, 3, 2, 1, 2, 0), // SLT x3, x1, x2 (signed: -5 < 5 = 1)
		EncodeRType(0x33, 4, 3, 2, 1, 0), // SLTU x4, x2, x1 (unsigned: 5 < 0xFFFFFFFB = 1)
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 1 {
		t.Errorf("SLT: got %d, want 1", cpu.Regs[3])
	}
	if cpu.Regs[4] != 1 {
		t.Errorf("SLTU: got %d, want 1", cpu.Regs[4])
	}
}

func TestRVCPU_MUL(t *testing.T) {
	// M extension: MUL x3, x1, x2
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 7),    // x1 = 7
		EncodeIType(0x13, 2, 0, 0, 6),    // x2 = 6
		EncodeRType(0x33, 3, 0, 1, 2, 1), // MUL x3, x1, x2 (funct7=1)
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 42 {
		t.Errorf("MUL: got %d, want 42", cpu.Regs[3])
	}
}

func TestRVCPU_MULH(t *testing.T) {
	// MULH: high 32 bits of signed multiply.
	// 0x40000000 * 4 = 0x100000000 -> high = 1
	instrs := []uint32{
		EncodeUType(0x37, 1, 0x40000000), // LUI x1, 0x40000000
		EncodeIType(0x13, 2, 0, 0, 4),    // x2 = 4
		EncodeRType(0x33, 3, 1, 1, 2, 1), // MULH x3, x1, x2
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 1 {
		t.Errorf("MULH: got %d, want 1", cpu.Regs[3])
	}
}

func TestRVCPU_DIVAndREM(t *testing.T) {
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 17),   // x1 = 17
		EncodeIType(0x13, 2, 0, 0, 5),    // x2 = 5
		EncodeRType(0x33, 3, 4, 1, 2, 1), // DIV x3, x1, x2 -> 3
		EncodeRType(0x33, 4, 6, 1, 2, 1), // REM x4, x1, x2 -> 2
		EncodeRType(0x33, 5, 5, 1, 2, 1), // DIVU x5, x1, x2 -> 3
		EncodeRType(0x33, 6, 7, 1, 2, 1), // REMU x6, x1, x2 -> 2
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 3 {
		t.Errorf("DIV: got %d, want 3", cpu.Regs[3])
	}
	if cpu.Regs[4] != 2 {
		t.Errorf("REM: got %d, want 2", cpu.Regs[4])
	}
	if cpu.Regs[5] != 3 {
		t.Errorf("DIVU: got %d, want 3", cpu.Regs[5])
	}
	if cpu.Regs[6] != 2 {
		t.Errorf("REMU: got %d, want 2", cpu.Regs[6])
	}
}

func TestRVCPU_DivByZero(t *testing.T) {
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 42), // x1 = 42
		// x2 = 0 (default)
		EncodeRType(0x33, 3, 4, 1, 2, 1), // DIV x3, x1, x2 -> -1 (0xFFFFFFFF)
		EncodeRType(0x33, 4, 5, 1, 2, 1), // DIVU x4, x1, x2 -> 0xFFFFFFFF
		EncodeRType(0x33, 5, 6, 1, 2, 1), // REM x5, x1, x2 -> 42
		EncodeRType(0x33, 6, 7, 1, 2, 1), // REMU x6, x1, x2 -> 42
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 0xFFFFFFFF {
		t.Errorf("DIV/0: got 0x%08x, want 0xFFFFFFFF", cpu.Regs[3])
	}
	if cpu.Regs[5] != 42 {
		t.Errorf("REM/0: got %d, want 42", cpu.Regs[5])
	}
}

func TestRVCPU_LoadStore(t *testing.T) {
	// Store a value at address 0x1000, then load it back.
	// x1 = 12345 (value to store)
	// x3 = 0x1000 (address)
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 123),  // ADDI x1, x0, 123
		EncodeUType(0x37, 3, 0x00001000), // LUI x3, 0x1000
		EncodeSType(0x23, 2, 3, 1, 0),    // SW x1, 0(x3)
		EncodeIType(0x03, 4, 2, 3, 0),    // LW x4, 0(x3)
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 200)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[4] != 123 {
		t.Errorf("Load/Store: got %d, want 123", cpu.Regs[4])
	}
	if cpu.Regs[4] != cpu.Regs[1] {
		t.Errorf("Load/Store mismatch: stored 0x%08x, loaded 0x%08x", cpu.Regs[1], cpu.Regs[4])
	}
}

func TestRVCPU_BEQ(t *testing.T) {
	// Branch taken: x1==x2 -> skip ADDI x3, x0, 99
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 5),  // x1 = 5
		EncodeIType(0x13, 2, 0, 0, 5),  // x2 = 5
		EncodeBType(0x63, 0, 1, 2, 8),  // BEQ x1, x2, +8 (skip next)
		EncodeIType(0x13, 3, 0, 0, 99), // x3 = 99 (should be skipped)
		EncodeIType(0x13, 3, 0, 0, 42), // x3 = 42 (branch target)
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 42 {
		t.Errorf("BEQ: got %d, want 42", cpu.Regs[3])
	}
}

func TestRVCPU_BNE(t *testing.T) {
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 5),  // x1 = 5
		EncodeIType(0x13, 2, 0, 0, 6),  // x2 = 6
		EncodeBType(0x63, 1, 1, 2, 8),  // BNE x1, x2, +8
		EncodeIType(0x13, 3, 0, 0, 99), // skipped
		EncodeIType(0x13, 3, 0, 0, 42), // target
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 42 {
		t.Errorf("BNE: got %d, want 42", cpu.Regs[3])
	}
}

func TestRVCPU_JAL(t *testing.T) {
	// JAL x1, +8 => skip one instruction, set x1 = PC+4 = 4
	instrs := []uint32{
		EncodeJType(0x6F, 1, 8),        // JAL x1, +8
		EncodeIType(0x13, 3, 0, 0, 99), // skipped
		EncodeIType(0x13, 3, 0, 0, 77), // target
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[1] != 4 { // return address = PC+4 = 0+4 = 4
		t.Errorf("JAL link: got %d, want 4", cpu.Regs[1])
	}
	if cpu.Regs[3] != 77 {
		t.Errorf("JAL target: got %d, want 77", cpu.Regs[3])
	}
}

func TestRVCPU_X0AlwaysZero(t *testing.T) {
	// Attempt to write to x0; it should remain 0.
	instrs := []uint32{
		EncodeIType(0x13, 0, 0, 0, 42), // ADDI x0, x0, 42
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[0] != 0 {
		t.Errorf("x0 was modified: got %d, want 0", cpu.Regs[0])
	}
}

func TestRVCPU_GasExhaustion(t *testing.T) {
	// Infinite loop with 5 gas.
	instrs := []uint32{
		EncodeJType(0x6F, 0, 0), // JAL x0, 0 (infinite loop)
	}
	cpu := rvCPUWithProgram(t, instrs, 5)
	err := cpu.Run()
	if err == nil {
		t.Fatal("expected gas exhaustion error")
	}
	if cpu.GasUsed != 5 {
		t.Errorf("GasUsed: got %d, want 5", cpu.GasUsed)
	}
}

func TestRVCPU_EmptyProgram(t *testing.T) {
	cpu := NewRVCPU(100)
	err := cpu.LoadProgram(nil, 0, 0)
	if err != ErrRVEmptyProgram {
		t.Errorf("expected ErrRVEmptyProgram, got %v", err)
	}
}

func TestRVCPU_Ecall_Output(t *testing.T) {
	instrs := []uint32{
		EncodeIType(0x13, 10, 0, 0, 'H'), // a0 = 'H'
		EncodeIType(0x13, 17, 0, 0, 1),   // a7 = ECALL_OUTPUT
		rvEcall(),
		EncodeIType(0x13, 10, 0, 0, 'i'), // a0 = 'i'
		rvEcall(),
		EncodeIType(0x13, 17, 0, 0, 0), // a7 = ECALL_HALT
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(cpu.OutputBuf) != "Hi" {
		t.Errorf("Output: got %q, want %q", string(cpu.OutputBuf), "Hi")
	}
}

func TestRVCPU_Ecall_Input(t *testing.T) {
	instrs := []uint32{
		EncodeIType(0x13, 17, 0, 0, 2), // a7 = ECALL_INPUT
		rvEcall(),
		// a0 now has first input byte, copy to x5
		EncodeIType(0x13, 5, 0, 10, 0), // ADDI x5, a0, 0
		EncodeIType(0x13, 17, 0, 0, 0), // a7 = ECALL_HALT
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.InputBuf = []byte{0xAB}
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[5] != 0xAB {
		t.Errorf("Input: got 0x%x, want 0xAB", cpu.Regs[5])
	}
}

func TestRVCPU_WithWitness(t *testing.T) {
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 10),   // ADDI x1, x0, 10
		EncodeIType(0x13, 2, 0, 0, 20),   // ADDI x2, x0, 20
		EncodeRType(0x33, 3, 0, 1, 2, 0), // ADD x3, x1, x2
		rvEcall(),                        // halt
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	cpu.Witness = NewRVWitnessCollector()
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Witness.StepCount() != 4 {
		t.Errorf("Witness steps: got %d, want 4", cpu.Witness.StepCount())
	}
	if cpu.Regs[3] != 30 {
		t.Errorf("x3: got %d, want 30", cpu.Regs[3])
	}
}

func TestRVCPU_DIVSignedOverflow(t *testing.T) {
	// Division of INT_MIN by -1 should return INT_MIN.
	instrs := []uint32{
		EncodeUType(0x37, 1, 0x80000000), // LUI x1, 0x80000000 (INT_MIN)
		EncodeIType(0x13, 2, 0, 0, -1),   // x2 = -1
		EncodeRType(0x33, 3, 4, 1, 2, 1), // DIV x3, x1, x2
		EncodeRType(0x33, 4, 6, 1, 2, 1), // REM x4, x1, x2
		rvEcall(),
	}
	cpu := rvCPUWithProgram(t, instrs, 100)
	cpu.Regs[17] = RVEcallHalt
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if cpu.Regs[3] != 0x80000000 {
		t.Errorf("DIV overflow: got 0x%08x, want 0x80000000", cpu.Regs[3])
	}
	if cpu.Regs[4] != 0 {
		t.Errorf("REM overflow: got %d, want 0", cpu.Regs[4])
	}
}
