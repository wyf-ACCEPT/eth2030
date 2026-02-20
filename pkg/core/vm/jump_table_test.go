package vm

import "testing"

func TestFrontierJumpTableArithmetic(t *testing.T) {
	tbl := NewFrontierJumpTable()

	tests := []struct {
		opcode   OpCode
		name     string
		gas      uint64
		minStack int
	}{
		{STOP, "STOP", GasStop, 0},
		{ADD, "ADD", GasVerylow, 2},
		{MUL, "MUL", GasVerylow, 2},
		{SUB, "SUB", GasVerylow, 2},
		{DIV, "DIV", GasLow, 2},
		{SDIV, "SDIV", GasLow, 2},
		{MOD, "MOD", GasLow, 2},
		{SMOD, "SMOD", GasLow, 2},
		{ADDMOD, "ADDMOD", GasMid, 3},
		{MULMOD, "MULMOD", GasMid, 3},
		{EXP, "EXP", GasHigh, 2},
		{SIGNEXTEND, "SIGNEXTEND", GasLow, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := tbl[tt.opcode]
			if op == nil {
				t.Fatalf("%s not defined in Frontier jump table", tt.name)
			}
			if op.constantGas != tt.gas {
				t.Errorf("%s constantGas = %d, want %d", tt.name, op.constantGas, tt.gas)
			}
			if op.minStack != tt.minStack {
				t.Errorf("%s minStack = %d, want %d", tt.name, op.minStack, tt.minStack)
			}
		})
	}
}

func TestFrontierJumpTableComparison(t *testing.T) {
	tbl := NewFrontierJumpTable()

	opcodes := []OpCode{LT, GT, SLT, SGT, EQ}
	for _, op := range opcodes {
		entry := tbl[op]
		if entry == nil {
			t.Fatalf("opcode 0x%02x not defined", op)
		}
		if entry.constantGas != GasVerylow {
			t.Errorf("opcode 0x%02x gas = %d, want %d", op, entry.constantGas, GasVerylow)
		}
		if entry.minStack != 2 {
			t.Errorf("opcode 0x%02x minStack = %d, want 2", op, entry.minStack)
		}
	}
}

func TestFrontierJumpTableHaltOpcodes(t *testing.T) {
	tbl := NewFrontierJumpTable()

	if !tbl[STOP].halts {
		t.Error("STOP should halt")
	}
	if !tbl[RETURN].halts {
		t.Error("RETURN should halt")
	}
	if !tbl[SELFDESTRUCT].halts {
		t.Error("SELFDESTRUCT should halt")
	}
}

func TestFrontierJumpTableJumpOpcodes(t *testing.T) {
	tbl := NewFrontierJumpTable()

	if !tbl[JUMP].jumps {
		t.Error("JUMP should have jumps=true")
	}
	if !tbl[JUMPI].jumps {
		t.Error("JUMPI should have jumps=true")
	}
	if tbl[ADD].jumps {
		t.Error("ADD should have jumps=false")
	}
}

func TestFrontierJumpTableWriteOpcodes(t *testing.T) {
	tbl := NewFrontierJumpTable()

	if !tbl[SSTORE].writes {
		t.Error("SSTORE should have writes=true")
	}
	if !tbl[SELFDESTRUCT].writes {
		t.Error("SELFDESTRUCT should have writes=true")
	}
	if !tbl[CREATE].writes {
		t.Error("CREATE should have writes=true")
	}
	if tbl[SLOAD].writes {
		t.Error("SLOAD should have writes=false")
	}
}

func TestFrontierJumpTablePushOps(t *testing.T) {
	tbl := NewFrontierJumpTable()

	// PUSH1-PUSH32 should all be defined.
	for i := 1; i <= 32; i++ {
		op := PUSH1 + OpCode(i-1)
		entry := tbl[op]
		if entry == nil {
			t.Fatalf("PUSH%d not defined", i)
		}
		if entry.constantGas != GasPush {
			t.Errorf("PUSH%d gas = %d, want %d", i, entry.constantGas, GasPush)
		}
	}
}

func TestFrontierJumpTableDupSwapOps(t *testing.T) {
	tbl := NewFrontierJumpTable()

	for i := 1; i <= 16; i++ {
		dupOp := DUP1 + OpCode(i-1)
		entry := tbl[dupOp]
		if entry == nil {
			t.Fatalf("DUP%d not defined", i)
		}
		if entry.constantGas != GasDup {
			t.Errorf("DUP%d gas = %d, want %d", i, entry.constantGas, GasDup)
		}
		if entry.minStack != i {
			t.Errorf("DUP%d minStack = %d, want %d", i, entry.minStack, i)
		}
	}
	for i := 1; i <= 16; i++ {
		swapOp := SWAP1 + OpCode(i-1)
		entry := tbl[swapOp]
		if entry == nil {
			t.Fatalf("SWAP%d not defined", i)
		}
		if entry.constantGas != GasSwap {
			t.Errorf("SWAP%d gas = %d, want %d", i, entry.constantGas, GasSwap)
		}
		if entry.minStack != i+1 {
			t.Errorf("SWAP%d minStack = %d, want %d", i, entry.minStack, i+1)
		}
	}
}

func TestFrontierJumpTableLogOps(t *testing.T) {
	tbl := NewFrontierJumpTable()

	for i := 0; i <= 4; i++ {
		logOp := LOG0 + OpCode(i)
		entry := tbl[logOp]
		if entry == nil {
			t.Fatalf("LOG%d not defined", i)
		}
		if entry.constantGas != GasLog {
			t.Errorf("LOG%d gas = %d, want %d", i, entry.constantGas, GasLog)
		}
		if entry.minStack != 2+i {
			t.Errorf("LOG%d minStack = %d, want %d", i, entry.minStack, 2+i)
		}
		if !entry.writes {
			t.Errorf("LOG%d should have writes=true", i)
		}
	}
}

func TestHomesteadAddsDelegate(t *testing.T) {
	tbl := NewHomesteadJumpTable()
	if tbl[DELEGATECALL] == nil {
		t.Fatal("Homestead should define DELEGATECALL")
	}
	if tbl[DELEGATECALL].minStack != 6 {
		t.Errorf("DELEGATECALL minStack = %d, want 6", tbl[DELEGATECALL].minStack)
	}
}

func TestByzantiumAddsRevert(t *testing.T) {
	tbl := NewByzantiumJumpTable()
	if tbl[REVERT] == nil {
		t.Fatal("Byzantium should define REVERT")
	}
	if !tbl[REVERT].halts {
		t.Error("REVERT should halt")
	}
	if tbl[RETURNDATASIZE] == nil {
		t.Fatal("Byzantium should define RETURNDATASIZE")
	}
	if tbl[RETURNDATACOPY] == nil {
		t.Fatal("Byzantium should define RETURNDATACOPY")
	}
	if tbl[STATICCALL] == nil {
		t.Fatal("Byzantium should define STATICCALL")
	}
}

func TestConstantinopleAddsShifts(t *testing.T) {
	tbl := NewConstantinopleJumpTable()
	for _, op := range []OpCode{SHL, SHR, SAR} {
		if tbl[op] == nil {
			t.Fatalf("Constantinople should define 0x%02x", op)
		}
		if tbl[op].constantGas != GasVerylow {
			t.Errorf("opcode 0x%02x gas = %d, want %d", op, tbl[op].constantGas, GasVerylow)
		}
	}
	if tbl[EXTCODEHASH] == nil {
		t.Fatal("Constantinople should define EXTCODEHASH")
	}
	if tbl[CREATE2] == nil {
		t.Fatal("Constantinople should define CREATE2")
	}
}

func TestIstanbulAddsSelfbalance(t *testing.T) {
	tbl := NewIstanbulJumpTable()
	if tbl[CHAINID] == nil {
		t.Fatal("Istanbul should define CHAINID")
	}
	if tbl[SELFBALANCE] == nil {
		t.Fatal("Istanbul should define SELFBALANCE")
	}
}

func TestBerlinWarmColdAccounting(t *testing.T) {
	tbl := NewBerlinJumpTable()
	// Berlin switches SLOAD to warm cost as constant.
	if tbl[SLOAD].constantGas != WarmStorageReadCost {
		t.Errorf("Berlin SLOAD constantGas = %d, want %d", tbl[SLOAD].constantGas, WarmStorageReadCost)
	}
	if tbl[SLOAD].dynamicGas == nil {
		t.Error("Berlin SLOAD should have dynamic gas")
	}
	// BALANCE should also use warm storage read cost.
	if tbl[BALANCE].constantGas != WarmStorageReadCost {
		t.Errorf("Berlin BALANCE constantGas = %d, want %d", tbl[BALANCE].constantGas, WarmStorageReadCost)
	}
}

func TestLondonAddsBaseFee(t *testing.T) {
	tbl := NewLondonJumpTable()
	if tbl[BASEFEE] == nil {
		t.Fatal("London should define BASEFEE")
	}
	if tbl[BASEFEE].constantGas != GasBase {
		t.Errorf("BASEFEE gas = %d, want %d", tbl[BASEFEE].constantGas, GasBase)
	}
}

func TestShanghaiAddsPush0(t *testing.T) {
	tbl := NewShanghaiJumpTable()
	if tbl[PUSH0] == nil {
		t.Fatal("Shanghai should define PUSH0")
	}
	if tbl[PUSH0].constantGas != GasPush0 {
		t.Errorf("PUSH0 gas = %d, want %d", tbl[PUSH0].constantGas, GasPush0)
	}
}

func TestCancunAddsTransientStorage(t *testing.T) {
	tbl := NewCancunJumpTable()
	if tbl[TLOAD] == nil {
		t.Fatal("Cancun should define TLOAD")
	}
	if tbl[TLOAD].constantGas != GasTload {
		t.Errorf("TLOAD gas = %d, want %d", tbl[TLOAD].constantGas, GasTload)
	}
	if tbl[TSTORE] == nil {
		t.Fatal("Cancun should define TSTORE")
	}
	if tbl[TSTORE].constantGas != GasTstore {
		t.Errorf("TSTORE gas = %d, want %d", tbl[TSTORE].constantGas, GasTstore)
	}
	if !tbl[TSTORE].writes {
		t.Error("TSTORE should have writes=true")
	}
	if tbl[MCOPY] == nil {
		t.Fatal("Cancun should define MCOPY")
	}
	if tbl[BLOBHASH] == nil {
		t.Fatal("Cancun should define BLOBHASH")
	}
	if tbl[BLOBBASEFEE] == nil {
		t.Fatal("Cancun should define BLOBBASEFEE")
	}
}

func TestGlamsterdanRepricedOpcodes(t *testing.T) {
	tbl := NewGlamsterdanJumpTable()

	// EIP-7904 repriced opcodes.
	tests := []struct {
		opcode OpCode
		name   string
		want   uint64
	}{
		{DIV, "DIV", GasDivGlamsterdan},
		{SDIV, "SDIV", GasSdivGlamsterdan},
		{MOD, "MOD", GasModGlamsterdan},
		{MULMOD, "MULMOD", GasMulmodGlamsterdan},
		{KECCAK256, "KECCAK256", GasKeccak256Glamsterdan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := tbl[tt.opcode]
			if op == nil {
				t.Fatalf("%s not defined in Glamsterdan", tt.name)
			}
			if op.constantGas != tt.want {
				t.Errorf("%s gas = %d, want %d", tt.name, op.constantGas, tt.want)
			}
		})
	}
}

func TestGlamsterdanNewOpcodes(t *testing.T) {
	tbl := NewGlamsterdanJumpTable()

	// SLOTNUM (EIP-7843).
	if tbl[SLOTNUM] == nil {
		t.Fatal("Glamsterdan should define SLOTNUM")
	}
	if tbl[SLOTNUM].constantGas != GasBase {
		t.Errorf("SLOTNUM gas = %d, want %d", tbl[SLOTNUM].constantGas, GasBase)
	}

	// CLZ (EIP-7939).
	if tbl[CLZ] == nil {
		t.Fatal("Glamsterdan should define CLZ")
	}
	if tbl[CLZ].constantGas != GasFastStep {
		t.Errorf("CLZ gas = %d, want %d", tbl[CLZ].constantGas, GasFastStep)
	}

	// DUPN, SWAPN, EXCHANGE (EIP-8024).
	if tbl[DUPN] == nil {
		t.Fatal("Glamsterdan should define DUPN")
	}
	if tbl[SWAPN] == nil {
		t.Fatal("Glamsterdan should define SWAPN")
	}
	if tbl[EXCHANGE] == nil {
		t.Fatal("Glamsterdan should define EXCHANGE")
	}
}

func TestGlamsterdanFrameTxOpcodes(t *testing.T) {
	tbl := NewGlamsterdanJumpTable()

	// EIP-8141 frame transaction opcodes.
	if tbl[APPROVE] == nil {
		t.Fatal("Glamsterdan should define APPROVE")
	}
	if !tbl[APPROVE].halts {
		t.Error("APPROVE should halt")
	}
	if tbl[TXPARAMLOAD] == nil {
		t.Fatal("Glamsterdan should define TXPARAMLOAD")
	}
	if tbl[TXPARAMSIZE] == nil {
		t.Fatal("Glamsterdan should define TXPARAMSIZE")
	}
	if tbl[TXPARAMCOPY] == nil {
		t.Fatal("Glamsterdan should define TXPARAMCOPY")
	}
}

func TestVerkleJumpTableOverrides(t *testing.T) {
	tbl := NewVerkleJumpTable()

	// Verkle replaces EIP-2929 gas with witness-based gas.
	if tbl[SLOAD].constantGas != 0 {
		t.Errorf("Verkle SLOAD constantGas = %d, want 0", tbl[SLOAD].constantGas)
	}
	if tbl[SLOAD].dynamicGas == nil {
		t.Error("Verkle SLOAD should have dynamic gas")
	}
	if tbl[SSTORE].constantGas != 0 {
		t.Errorf("Verkle SSTORE constantGas = %d, want 0", tbl[SSTORE].constantGas)
	}
	if tbl[BALANCE].constantGas != 0 {
		t.Errorf("Verkle BALANCE constantGas = %d, want 0", tbl[BALANCE].constantGas)
	}
	if tbl[CALL].constantGas != 0 {
		t.Errorf("Verkle CALL constantGas = %d, want 0", tbl[CALL].constantGas)
	}
}

func TestForkInheritance(t *testing.T) {
	// Each fork should inherit all opcodes from previous forks.
	frontier := NewFrontierJumpTable()
	homestead := NewHomesteadJumpTable()

	// Homestead should still have ADD from Frontier.
	if homestead[ADD] == nil {
		t.Error("Homestead should inherit ADD from Frontier")
	}
	if frontier[ADD].constantGas != homestead[ADD].constantGas {
		t.Error("ADD gas should be same in Frontier and Homestead")
	}

	// Cancun should still have DELEGATECALL from Homestead.
	cancun := NewCancunJumpTable()
	if cancun[DELEGATECALL] == nil {
		t.Error("Cancun should inherit DELEGATECALL")
	}
}

func TestOperationGetConstantGas(t *testing.T) {
	op := &operation{constantGas: 42}
	if op.GetConstantGas() != 42 {
		t.Errorf("GetConstantGas() = %d, want 42", op.GetConstantGas())
	}
}
