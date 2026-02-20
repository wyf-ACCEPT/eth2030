package zkvm

import (
	"testing"
)

func TestLeanOpcodeString(t *testing.T) {
	tests := []struct {
		op   LeanOpcode
		want string
	}{
		{OpADD, "ADD"},
		{OpMUL, "MUL"},
		{OpHASH, "HASH"},
		{OpVERIFY, "VERIFY"},
		{OpPUSH, "PUSH"},
		{OpDUP, "DUP"},
		{LeanOpcode(0xFF), "INVALID"},
	}
	for _, tt := range tests {
		if got := tt.op.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.op, got, tt.want)
		}
	}
}

func TestLeanOpcodeIsValid(t *testing.T) {
	if !OpADD.IsValid() {
		t.Error("OpADD should be valid")
	}
	if !OpDUP.IsValid() {
		t.Error("OpDUP should be valid")
	}
	if LeanOpcode(0x00).IsValid() {
		t.Error("opcode 0x00 should be invalid")
	}
	if LeanOpcode(0xFF).IsValid() {
		t.Error("opcode 0xFF should be invalid")
	}
}

func TestLeanVMExecutePushAndHash(t *testing.T) {
	vm := NewLeanVM(0) // use default cycle limit
	inputs := [][]byte{{0xaa, 0xbb, 0xcc}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpHASH},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.Steps != 2 {
		t.Errorf("Steps: got %d, want 2", result.Steps)
	}
	if result.GasUsed != GasPUSH+GasHASH {
		t.Errorf("GasUsed: got %d, want %d", result.GasUsed, GasPUSH+GasHASH)
	}
	if len(result.Output) != 32 {
		t.Errorf("output length: got %d, want 32 (keccak256 hash)", len(result.Output))
	}
}

func TestLeanVMExecuteAdd(t *testing.T) {
	vm := NewLeanVM(0)
	inputs := [][]byte{{0x0F}, {0xF0}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpPUSH, Operands: []uint32{1}},
		{Opcode: OpADD},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	// XOR of 0x0F and 0xF0 = 0xFF
	if len(result.Output) != 1 || result.Output[0] != 0xFF {
		t.Errorf("output: got %x, want ff", result.Output)
	}
}

func TestLeanVMExecuteMul(t *testing.T) {
	vm := NewLeanVM(0)
	inputs := [][]byte{{0x01, 0x02}, {0x03, 0x04}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpPUSH, Operands: []uint32{1}},
		{Opcode: OpMUL},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	// MUL produces a keccak256 hash, so 32 bytes.
	if len(result.Output) != 32 {
		t.Errorf("output length: got %d, want 32", len(result.Output))
	}
}

func TestLeanVMExecuteVerifyEqual(t *testing.T) {
	vm := NewLeanVM(0)
	inputs := [][]byte{{0xde, 0xad}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpDUP},
		{Opcode: OpVERIFY},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Output) != 1 || result.Output[0] != 0x01 {
		t.Errorf("expected VERIFY(x, x) = 0x01, got %x", result.Output)
	}
}

func TestLeanVMExecuteVerifyNotEqual(t *testing.T) {
	vm := NewLeanVM(0)
	inputs := [][]byte{{0x01}, {0x02}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpPUSH, Operands: []uint32{1}},
		{Opcode: OpVERIFY},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Output) != 1 || result.Output[0] != 0x00 {
		t.Errorf("expected VERIFY(1, 2) = 0x00, got %x", result.Output)
	}
}

func TestLeanVMExecuteDup(t *testing.T) {
	vm := NewLeanVM(0)
	inputs := [][]byte{{0xab, 0xcd}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpDUP},
		{Opcode: OpVERIFY},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// DUP + VERIFY should yield 0x01 (equal).
	if len(result.Output) != 1 || result.Output[0] != 0x01 {
		t.Errorf("expected 0x01, got %x", result.Output)
	}
}

func TestLeanVMEmptyProgram(t *testing.T) {
	vm := NewLeanVM(0)
	_, err := vm.Execute(nil, nil)
	if err != ErrLeanEmptyProgram {
		t.Errorf("expected ErrLeanEmptyProgram, got %v", err)
	}
}

func TestLeanVMStackUnderflow(t *testing.T) {
	vm := NewLeanVM(0)
	// ADD with empty stack.
	program := []LeanOp{{Opcode: OpADD}}
	_, err := vm.Execute(program, nil)
	if err != ErrLeanStackUnderflow {
		t.Errorf("expected ErrLeanStackUnderflow, got %v", err)
	}
}

func TestLeanVMStackUnderflowHash(t *testing.T) {
	vm := NewLeanVM(0)
	program := []LeanOp{{Opcode: OpHASH}}
	_, err := vm.Execute(program, nil)
	if err != ErrLeanStackUnderflow {
		t.Errorf("expected ErrLeanStackUnderflow for HASH, got %v", err)
	}
}

func TestLeanVMStackUnderflowDup(t *testing.T) {
	vm := NewLeanVM(0)
	program := []LeanOp{{Opcode: OpDUP}}
	_, err := vm.Execute(program, nil)
	if err != ErrLeanStackUnderflow {
		t.Errorf("expected ErrLeanStackUnderflow for DUP, got %v", err)
	}
}

func TestLeanVMInvalidOpcode(t *testing.T) {
	vm := NewLeanVM(0)
	program := []LeanOp{{Opcode: LeanOpcode(0xFF)}}
	_, err := vm.Execute(program, nil)
	if err != ErrLeanInvalidOpcode {
		t.Errorf("expected ErrLeanInvalidOpcode, got %v", err)
	}
}

func TestLeanVMCycleLimit(t *testing.T) {
	vm := NewLeanVM(2) // only 2 cycles allowed
	inputs := [][]byte{{0x01}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpADD}, // should hit the cycle limit
	}
	_, err := vm.Execute(program, inputs)
	if err != ErrLeanCycleLimit {
		t.Errorf("expected ErrLeanCycleLimit, got %v", err)
	}
}

func TestLeanVMInvalidOperandIndex(t *testing.T) {
	vm := NewLeanVM(0)
	inputs := [][]byte{{0x01}}
	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{99}}, // out of range
	}
	_, err := vm.Execute(program, inputs)
	if err != ErrLeanInvalidOperand {
		t.Errorf("expected ErrLeanInvalidOperand, got %v", err)
	}
}

func TestLeanVMPushNoOperands(t *testing.T) {
	vm := NewLeanVM(0)
	program := []LeanOp{
		{Opcode: OpPUSH}, // no operands
	}
	_, err := vm.Execute(program, nil)
	if err != ErrLeanInvalidOperand {
		t.Errorf("expected ErrLeanInvalidOperand, got %v", err)
	}
}

func TestAggregateProofs(t *testing.T) {
	proofs := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05, 0x06},
		{0x07, 0x08, 0x09},
	}

	agg, err := AggregateProofs(proofs)
	if err != nil {
		t.Fatalf("AggregateProofs: %v", err)
	}
	if len(agg) != 32 {
		t.Errorf("aggregated length: got %d, want 32", len(agg))
	}

	// Deterministic check.
	agg2, _ := AggregateProofs(proofs)
	if !bytesEqual(agg, agg2) {
		t.Error("AggregateProofs should be deterministic")
	}
}

func TestAggregateProofsEmpty(t *testing.T) {
	_, err := AggregateProofs(nil)
	if err != ErrLeanAggregateEmpty {
		t.Errorf("expected ErrLeanAggregateEmpty, got %v", err)
	}
}

func TestVerifyAggregated(t *testing.T) {
	proofs := [][]byte{
		{0xaa, 0xbb},
		{0xcc, 0xdd},
	}

	agg, _ := AggregateProofs(proofs)
	if !VerifyAggregated(agg, proofs) {
		t.Fatal("valid aggregated proof should verify")
	}
}

func TestVerifyAggregatedTampered(t *testing.T) {
	proofs := [][]byte{{0x01}, {0x02}}
	agg, _ := AggregateProofs(proofs)
	agg[0] ^= 0xff
	if VerifyAggregated(agg, proofs) {
		t.Fatal("tampered aggregated proof should not verify")
	}
}

func TestVerifyAggregatedEmptyInputs(t *testing.T) {
	if VerifyAggregated(nil, nil) {
		t.Fatal("nil inputs should not verify")
	}
	if VerifyAggregated([]byte{0x01}, nil) {
		t.Fatal("nil commitments should not verify")
	}
}

func TestCompileToLean(t *testing.T) {
	// PUSH1 0xAA, PUSH1 0xBB, ADD, SHA3
	evm := []byte{0x60, 0xAA, 0x60, 0xBB, 0x01, 0x20}
	ops, err := CompileToLean(evm)
	if err != nil {
		t.Fatalf("CompileToLean: %v", err)
	}
	if len(ops) != 4 {
		t.Fatalf("expected 4 ops, got %d", len(ops))
	}
	if ops[0].Opcode != OpPUSH {
		t.Errorf("op[0]: got %s, want PUSH", ops[0].Opcode)
	}
	if ops[1].Opcode != OpPUSH {
		t.Errorf("op[1]: got %s, want PUSH", ops[1].Opcode)
	}
	if ops[2].Opcode != OpADD {
		t.Errorf("op[2]: got %s, want ADD", ops[2].Opcode)
	}
	if ops[3].Opcode != OpHASH {
		t.Errorf("op[3]: got %s, want HASH", ops[3].Opcode)
	}
}

func TestCompileToLeanEmpty(t *testing.T) {
	_, err := CompileToLean(nil)
	if err != ErrLeanCompileEmpty {
		t.Errorf("expected ErrLeanCompileEmpty, got %v", err)
	}
}

func TestCompileToLeanUnsupported(t *testing.T) {
	// STOP (0x00) is unsupported.
	_, err := CompileToLean([]byte{0x00})
	if err != ErrLeanCompileUnsupported {
		t.Errorf("expected ErrLeanCompileUnsupported, got %v", err)
	}
}

func TestCompileToLeanMul(t *testing.T) {
	evm := []byte{0x60, 0x01, 0x60, 0x02, 0x02} // PUSH1 1, PUSH1 2, MUL
	ops, err := CompileToLean(evm)
	if err != nil {
		t.Fatalf("CompileToLean: %v", err)
	}
	if len(ops) != 3 {
		t.Fatalf("expected 3 ops, got %d", len(ops))
	}
	if ops[2].Opcode != OpMUL {
		t.Errorf("op[2]: got %s, want MUL", ops[2].Opcode)
	}
}

func TestCompileToLeanEQ(t *testing.T) {
	evm := []byte{0x60, 0x01, 0x80, 0x14} // PUSH1 1, DUP1, EQ
	ops, err := CompileToLean(evm)
	if err != nil {
		t.Fatalf("CompileToLean: %v", err)
	}
	if len(ops) != 3 {
		t.Fatalf("expected 3 ops, got %d", len(ops))
	}
	if ops[1].Opcode != OpDUP {
		t.Errorf("op[1]: got %s, want DUP", ops[1].Opcode)
	}
	if ops[2].Opcode != OpVERIFY {
		t.Errorf("op[2]: got %s, want VERIFY", ops[2].Opcode)
	}
}

func TestCompileToLeanTruncatedPush(t *testing.T) {
	evm := []byte{0x60} // PUSH1 with no data byte
	_, err := CompileToLean(evm)
	if err != ErrLeanCompileUnsupported {
		t.Errorf("expected ErrLeanCompileUnsupported, got %v", err)
	}
}

func TestAggregateAndCommit(t *testing.T) {
	proofs := [][]byte{{0x10, 0x20}, {0x30, 0x40}}

	agg, commitment, err := AggregateAndCommit(proofs)
	if err != nil {
		t.Fatalf("AggregateAndCommit: %v", err)
	}
	if len(agg) != 32 {
		t.Errorf("aggregated length: got %d, want 32", len(agg))
	}
	if commitment.IsZero() {
		t.Error("commitment should not be zero")
	}
}

func TestLeanVMComplexProgram(t *testing.T) {
	// Test a multi-step program: PUSH(0), HASH, PUSH(1), HASH, VERIFY
	vm := NewLeanVM(0)
	data := []byte{0xde, 0xad, 0xbe, 0xef}
	inputs := [][]byte{data, data} // same data twice

	program := []LeanOp{
		{Opcode: OpPUSH, Operands: []uint32{0}},
		{Opcode: OpHASH},
		{Opcode: OpPUSH, Operands: []uint32{1}},
		{Opcode: OpHASH},
		{Opcode: OpVERIFY},
	}

	result, err := vm.Execute(program, inputs)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	// HASH(data) == HASH(data) -> 0x01
	if len(result.Output) != 1 || result.Output[0] != 0x01 {
		t.Errorf("expected 0x01 (equal hashes), got %x", result.Output)
	}
	if result.Steps != 5 {
		t.Errorf("Steps: got %d, want 5", result.Steps)
	}
	expectedGas := 2*GasPUSH + 2*GasHASH + GasVERIFY
	if result.GasUsed != expectedGas {
		t.Errorf("GasUsed: got %d, want %d", result.GasUsed, expectedGas)
	}
}

func TestAggregateProofsDifferentOrder(t *testing.T) {
	p1 := [][]byte{{0x01}, {0x02}}
	p2 := [][]byte{{0x02}, {0x01}}

	a1, _ := AggregateProofs(p1)
	a2, _ := AggregateProofs(p2)

	// Order matters: different order should produce different aggregation.
	if bytesEqual(a1, a2) {
		t.Error("different proof order should produce different aggregated proof")
	}
}
