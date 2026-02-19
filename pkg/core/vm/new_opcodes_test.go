package vm

import (
	"math/big"
	"testing"
)

// TestSLOTNUM verifies the SLOTNUM opcode (EIP-7843) pushes the slot number.
func TestSLOTNUM(t *testing.T) {
	tests := []struct {
		name       string
		slotNumber uint64
	}{
		{"slot 0", 0},
		{"slot 1", 1},
		{"slot 100", 100},
		{"slot max uint32", 0xFFFFFFFF},
		{"slot large", 1_000_000_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockCtx := BlockContext{
				BlockNumber: big.NewInt(1),
				BaseFee:     big.NewInt(1),
				SlotNumber:  tt.slotNumber,
			}
			evm := NewEVM(blockCtx, TxContext{}, Config{})
			evm.SetJumpTable(NewGlamsterdanJumpTable())

			// Bytecode: SLOTNUM STOP
			code := []byte{byte(SLOTNUM), byte(STOP)}
			contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
			contract.Code = code

			ret, err := evm.Run(contract, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ret != nil {
				t.Fatalf("expected nil return, got %x", ret)
			}
		})
	}
}

// TestSLOTNUM_FullExecution runs SLOTNUM and verifies the stack value via
// MSTORE + RETURN.
func TestSLOTNUM_FullExecution(t *testing.T) {
	slotNum := uint64(42)
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(1),
		BaseFee:     big.NewInt(1),
		SlotNumber:  slotNum,
	}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	evm.SetJumpTable(NewGlamsterdanJumpTable())

	// SLOTNUM -> PUSH1 0x00 -> MSTORE -> PUSH1 0x20 -> PUSH1 0x00 -> RETURN
	code := []byte{
		byte(SLOTNUM),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != slotNum {
		t.Errorf("SLOTNUM returned %d, want %d", result.Uint64(), slotNum)
	}
}

// TestSLOTNUM_GasCost verifies SLOTNUM costs GasBase (2).
func TestSLOTNUM_GasCost(t *testing.T) {
	jt := NewGlamsterdanJumpTable()
	op := jt[SLOTNUM]
	if op == nil {
		t.Fatal("SLOTNUM not defined in Glamsterdan jump table")
	}
	if op.constantGas != GasBase {
		t.Errorf("SLOTNUM gas = %d, want %d (GasBase)", op.constantGas, GasBase)
	}
}

// TestSLOTNUM_NotInCancun verifies SLOTNUM is not valid in the Cancun jump table.
func TestSLOTNUM_NotInCancun(t *testing.T) {
	jt := NewCancunJumpTable()
	if jt[SLOTNUM] != nil {
		t.Error("SLOTNUM should not be defined in Cancun jump table")
	}
}

// TestCLZ verifies the CLZ opcode (EIP-7939).
func TestCLZ(t *testing.T) {
	tests := []struct {
		name   string
		input  *big.Int
		expect uint64
	}{
		{"zero", big.NewInt(0), 256},
		{"one", big.NewInt(1), 255},
		{"two", big.NewInt(2), 254},
		{"255", big.NewInt(255), 248},
		{"256", big.NewInt(256), 247},
		{"max uint8", big.NewInt(0xFF), 248},
		{"max uint16", big.NewInt(0xFFFF), 240},
		{"max uint32", new(big.Int).SetUint64(0xFFFFFFFF), 224},
		{"max uint64", new(big.Int).SetUint64(0xFFFFFFFFFFFFFFFF), 192},
		{"2^128", new(big.Int).Lsh(big.NewInt(1), 128), 127},
		{"2^255", new(big.Int).Lsh(big.NewInt(1), 255), 0},
		{"max 256-bit", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1)), 0},
		{"power of 2: 2^100", new(big.Int).Lsh(big.NewInt(1), 100), 155},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := NewStack()
			stack.Push(new(big.Int).Set(tt.input))

			var pc uint64
			evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})
			contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)

			_, err := opCLZ(&pc, evm, contract, NewMemory(), stack)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			result := stack.Peek()
			if result.Uint64() != tt.expect {
				t.Errorf("CLZ(%s) = %d, want %d", tt.input.String(), result.Uint64(), tt.expect)
			}
		})
	}
}

// TestCLZ_GasCost verifies CLZ costs GasFastStep (5).
func TestCLZ_GasCost(t *testing.T) {
	jt := NewGlamsterdanJumpTable()
	op := jt[CLZ]
	if op == nil {
		t.Fatal("CLZ not defined in Glamsterdan jump table")
	}
	if op.constantGas != GasFastStep {
		t.Errorf("CLZ gas = %d, want %d (GasFastStep)", op.constantGas, GasFastStep)
	}
}

// TestCLZ_NotInCancun verifies CLZ is not valid in the Cancun jump table.
func TestCLZ_NotInCancun(t *testing.T) {
	jt := NewCancunJumpTable()
	if jt[CLZ] != nil {
		t.Error("CLZ should not be defined in Cancun jump table")
	}
}

// TestDecodeSingle verifies the EIP-8024 single-byte decoder.
func TestDecodeSingle(t *testing.T) {
	// x=0 -> 17, x=1 -> 18, ..., x=90 -> 107
	if got := decodeSingle(0); got != 17 {
		t.Errorf("decodeSingle(0) = %d, want 17", got)
	}
	if got := decodeSingle(90); got != 107 {
		t.Errorf("decodeSingle(90) = %d, want 107", got)
	}
	// x=128 -> 108, x=255 -> 235
	if got := decodeSingle(128); got != 108 {
		t.Errorf("decodeSingle(128) = %d, want 108", got)
	}
	if got := decodeSingle(255); got != 235 {
		t.Errorf("decodeSingle(255) = %d, want 235", got)
	}
}

// TestDecodePair verifies the EIP-8024 pair-byte decoder.
func TestDecodePair(t *testing.T) {
	// x=0: k=0, q=0, r=0, q >= r, return (0+1, 29-0) = (1, 29)
	n, m := decodePair(0)
	if n != 1 || m != 29 {
		t.Errorf("decodePair(0) = (%d, %d), want (1, 29)", n, m)
	}

	// x=1: k=1, q=0, r=1, q < r, return (0+1, 1+1) = (1, 2)
	n, m = decodePair(1)
	if n != 1 || m != 2 {
		t.Errorf("decodePair(1) = (%d, %d), want (1, 2)", n, m)
	}
}

// TestDUPN verifies the DUPN opcode (EIP-8024).
func TestDUPN(t *testing.T) {
	// Set up a stack with 20 items (values 0..19, top is 19).
	stack := NewStack()
	for i := 0; i < 20; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	// DUPN with immediate byte 0 -> decodeSingle(0) = 17
	// So it should duplicate item at depth 17 (1-indexed from top).
	// Dup(17) reads data[len-17] = data[20-17] = data[3] = value 3.
	code := []byte{byte(DUPN), 0x00, byte(STOP)}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opDupN(&pc, evm, contract, NewMemory(), stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Stack should now have 21 items, top should be 3.
	if stack.Len() != 21 {
		t.Fatalf("stack length = %d, want 21", stack.Len())
	}
	top := stack.Peek()
	if top.Int64() != 3 {
		t.Errorf("DUPN top = %d, want 3", top.Int64())
	}
	// PC should have been incremented by 1 (for the immediate byte).
	if pc != 1 {
		t.Errorf("PC = %d, want 1", pc)
	}
}

// TestDUPN_InvalidRange verifies DUPN rejects bytes in the excluded range 91-127.
func TestDUPN_InvalidRange(t *testing.T) {
	stack := NewStack()
	for i := 0; i < 200; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	code := []byte{byte(DUPN), 91, byte(STOP)} // 91 is in excluded range
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opDupN(&pc, evm, contract, NewMemory(), stack)
	if err != ErrInvalidOpCode {
		t.Errorf("DUPN with byte 91 should fail with ErrInvalidOpCode, got %v", err)
	}
}

// TestDUPN_StackUnderflow verifies DUPN fails if the stack is too shallow.
func TestDUPN_StackUnderflow(t *testing.T) {
	stack := NewStack()
	// Only push 5 items. decodeSingle(0) = 17, requires 17 items.
	for i := 0; i < 5; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	code := []byte{byte(DUPN), 0x00, byte(STOP)}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opDupN(&pc, evm, contract, NewMemory(), stack)
	if err != ErrStackUnderflow {
		t.Errorf("DUPN with insufficient stack should fail with ErrStackUnderflow, got %v", err)
	}
}

// TestSWAPN verifies the SWAPN opcode (EIP-8024).
func TestSWAPN(t *testing.T) {
	// Set up a stack with 20 items (values 0..19, top is 19).
	stack := NewStack()
	for i := 0; i < 20; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	// SWAPN with immediate byte 0 -> decodeSingle(0) = 17
	// Swaps top (19) with item at depth 17 from top.
	// depth 17 from top = data[19-17] = data[2] = value 2
	code := []byte{byte(SWAPN), 0x00, byte(STOP)}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opSwapN(&pc, evm, contract, NewMemory(), stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Stack length unchanged.
	if stack.Len() != 20 {
		t.Fatalf("stack length = %d, want 20", stack.Len())
	}
	// Top should now be 2 (previously at depth 17).
	top := stack.Peek()
	if top.Int64() != 2 {
		t.Errorf("SWAPN top = %d, want 2", top.Int64())
	}
	// Item at depth 17 should now be 19 (previously at top).
	data := stack.Data()
	if data[2].Int64() != 19 {
		t.Errorf("SWAPN data[2] = %d, want 19", data[2].Int64())
	}
}

// TestSWAPN_InvalidRange verifies SWAPN rejects bytes in the excluded range 91-127.
func TestSWAPN_InvalidRange(t *testing.T) {
	stack := NewStack()
	for i := 0; i < 200; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	code := []byte{byte(SWAPN), 100, byte(STOP)} // 100 is in excluded range
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opSwapN(&pc, evm, contract, NewMemory(), stack)
	if err != ErrInvalidOpCode {
		t.Errorf("SWAPN with byte 100 should fail with ErrInvalidOpCode, got %v", err)
	}
}

// TestEXCHANGE verifies the EXCHANGE opcode (EIP-8024).
func TestEXCHANGE(t *testing.T) {
	// Set up a stack with 30 items (values 0..29, top is 29).
	stack := NewStack()
	for i := 0; i < 30; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	// EXCHANGE with immediate byte 1: decodePair(1) -> (1, 2)
	// Swaps item at depth 1 with item at depth 2 from top.
	// depth 1 from top = data[29-1] = data[28] = value 28
	// depth 2 from top = data[29-2] = data[27] = value 27
	code := []byte{byte(EXCHANGE), 0x01, byte(STOP)}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opExchange(&pc, evm, contract, NewMemory(), stack)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Stack length unchanged.
	if stack.Len() != 30 {
		t.Fatalf("stack length = %d, want 30", stack.Len())
	}
	// data[27] should now be 28, data[28] should now be 27.
	data := stack.Data()
	if data[27].Int64() != 28 {
		t.Errorf("EXCHANGE data[27] = %d, want 28", data[27].Int64())
	}
	if data[28].Int64() != 27 {
		t.Errorf("EXCHANGE data[28] = %d, want 27", data[28].Int64())
	}
}

// TestEXCHANGE_InvalidRange verifies EXCHANGE rejects bytes in the excluded range 80-127.
func TestEXCHANGE_InvalidRange(t *testing.T) {
	stack := NewStack()
	for i := 0; i < 200; i++ {
		stack.Push(big.NewInt(int64(i)))
	}

	code := []byte{byte(EXCHANGE), 80, byte(STOP)} // 80 is in excluded range
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opExchange(&pc, evm, contract, NewMemory(), stack)
	if err != ErrInvalidOpCode {
		t.Errorf("EXCHANGE with byte 80 should fail with ErrInvalidOpCode, got %v", err)
	}
}

// TestEXCHANGE_StackUnderflow verifies EXCHANGE fails if the stack is too shallow.
func TestEXCHANGE_StackUnderflow(t *testing.T) {
	stack := NewStack()
	stack.Push(big.NewInt(1))
	stack.Push(big.NewInt(2))

	// decodePair(1) = (1, 2), need max(1,2)+1 = 3 items, but we only have 2.
	code := []byte{byte(EXCHANGE), 0x01, byte(STOP)}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}, TxContext{}, Config{})

	var pc uint64
	_, err := opExchange(&pc, evm, contract, NewMemory(), stack)
	if err != ErrStackUnderflow {
		t.Errorf("EXCHANGE with insufficient stack should fail with ErrStackUnderflow, got %v", err)
	}
}

// TestEIP8024_GasCost verifies DUPN, SWAPN, EXCHANGE each cost GasVerylow (3).
func TestEIP8024_GasCost(t *testing.T) {
	jt := NewGlamsterdanJumpTable()

	for _, tt := range []struct {
		name string
		op   OpCode
	}{
		{"DUPN", DUPN},
		{"SWAPN", SWAPN},
		{"EXCHANGE", EXCHANGE},
	} {
		t.Run(tt.name, func(t *testing.T) {
			op := jt[tt.op]
			if op == nil {
				t.Fatalf("%s not defined in Glamsterdan jump table", tt.name)
			}
			if op.constantGas != GasVerylow {
				t.Errorf("%s gas = %d, want %d (GasVerylow)", tt.name, op.constantGas, GasVerylow)
			}
		})
	}
}

// TestEIP8024_NotInCancun verifies DUPN/SWAPN/EXCHANGE are not valid in Cancun.
func TestEIP8024_NotInCancun(t *testing.T) {
	jt := NewCancunJumpTable()
	for _, op := range []OpCode{DUPN, SWAPN, EXCHANGE} {
		if jt[op] != nil {
			t.Errorf("%s should not be defined in Cancun jump table", op)
		}
	}
}

// TestOpcodeNames checks that all new opcodes have string names.
func TestOpcodeNames(t *testing.T) {
	for _, tt := range []struct {
		op   OpCode
		name string
	}{
		{CLZ, "CLZ"},
		{SLOTNUM, "SLOTNUM"},
		{DUPN, "DUPN"},
		{SWAPN, "SWAPN"},
		{EXCHANGE, "EXCHANGE"},
	} {
		if got := tt.op.String(); got != tt.name {
			t.Errorf("%#x.String() = %q, want %q", byte(tt.op), got, tt.name)
		}
	}
}

// TestOpcodeValues checks that opcode byte values match the spec.
func TestOpcodeValues(t *testing.T) {
	if CLZ != 0x1e {
		t.Errorf("CLZ = 0x%x, want 0x1e", byte(CLZ))
	}
	if SLOTNUM != 0x4b {
		t.Errorf("SLOTNUM = 0x%x, want 0x4b", byte(SLOTNUM))
	}
	if DUPN != 0xe6 {
		t.Errorf("DUPN = 0x%x, want 0xe6", byte(DUPN))
	}
	if SWAPN != 0xe7 {
		t.Errorf("SWAPN = 0x%x, want 0xe7", byte(SWAPN))
	}
	if EXCHANGE != 0xe8 {
		t.Errorf("EXCHANGE = 0x%x, want 0xe8", byte(EXCHANGE))
	}
}

// TestCLZ_FullExecution runs CLZ through the interpreter loop with MSTORE+RETURN.
func TestCLZ_FullExecution(t *testing.T) {
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(1),
		BaseFee:     big.NewInt(1),
	}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	evm.SetJumpTable(NewGlamsterdanJumpTable())

	// PUSH1 0x01 -> CLZ -> PUSH1 0x00 -> MSTORE -> PUSH1 0x20 -> PUSH1 0x00 -> RETURN
	// CLZ(1) = 255
	code := []byte{
		byte(PUSH1), 0x01,
		byte(CLZ),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract([20]byte{}, [20]byte{}, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(ret))
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 255 {
		t.Errorf("CLZ(1) returned %d, want 255", result.Uint64())
	}
}
