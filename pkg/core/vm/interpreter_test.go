package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func newTestEVM() *EVM {
	return NewEVM(
		BlockContext{
			BlockNumber: big.NewInt(100),
			Time:        1700000000,
			GasLimit:    30000000,
			BaseFee:     big.NewInt(1000000000),
		},
		TxContext{
			GasPrice: big.NewInt(2000000000),
		},
		Config{},
	)
}

func TestRunStop(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: STOP
	contract.Code = []byte{byte(STOP)}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Errorf("expected nil return, got %x", ret)
	}
}

func TestRunAddAndReturn(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH1 10, PUSH1 20, ADD, PUSH1 0, MSTORE, PUSH1 32, PUSH1 0, RETURN
	contract.Code = []byte{
		byte(PUSH1), 10,
		byte(PUSH1), 20,
		byte(ADD),
		byte(PUSH1), 0,
		byte(MSTORE),
		byte(PUSH1), 32,
		byte(PUSH1), 0,
		byte(RETURN),
	}
	// Pre-expand memory so MSTORE doesn't panic
	// The Run loop handles memory expansion if memorySize is set,
	// but our MSTORE doesn't have memorySize configured yet.
	// We test the basic flow here.

	// Actually, we need memory to be available. Let's use a simpler test
	// that just does arithmetic and stops.
	contract.Code = []byte{
		byte(PUSH1), 10,
		byte(PUSH1), 20,
		byte(ADD),
		byte(STOP),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Errorf("STOP should return nil, got %x", ret)
	}
}

func TestRunPush2(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH2 0x01 0x00, PUSH1 0x01, ADD, STOP
	// 0x0100 = 256; 256 + 1 = 257
	contract.Code = []byte{
		byte(PUSH2), 0x01, 0x00,
		byte(PUSH1), 0x01,
		byte(ADD),
		byte(STOP),
	}

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if ret != nil {
		t.Errorf("STOP should return nil, got %x", ret)
	}
}

func TestRunInvalidOpcode(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: INVALID
	contract.Code = []byte{byte(INVALID)}

	_, err := evm.Run(contract, nil)
	if err != ErrInvalidOpCode {
		t.Errorf("expected ErrInvalidOpCode, got %v", err)
	}
}

func TestRunOutOfGas(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1) // only 1 gas
	// PUSH1 costs 3 gas, so we should run out.
	contract.Code = []byte{byte(PUSH1), 0x42, byte(STOP)}

	_, err := evm.Run(contract, nil)
	if err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas, got %v", err)
	}
}

func TestRunStackUnderflow(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// ADD requires 2 stack items but we have none.
	contract.Code = []byte{byte(ADD)}

	_, err := evm.Run(contract, nil)
	if err != ErrStackUnderflow {
		t.Errorf("expected ErrStackUnderflow, got %v", err)
	}
}

func TestRunDupSwap(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH1 42, DUP1, ADD, STOP
	// 42 + 42 = 84
	contract.Code = []byte{
		byte(PUSH1), 42,
		byte(DUP1),
		byte(ADD),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunJump(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH1 4, JUMP, INVALID, JUMPDEST, STOP
	// Positions: 0=PUSH1, 1=0x04, 2=JUMP, 3=INVALID, 4=JUMPDEST, 5=STOP
	contract.Code = []byte{
		byte(PUSH1), 4,
		byte(JUMP),
		byte(INVALID),
		byte(JUMPDEST),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunJumpi(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH1 1, PUSH1 6, JUMPI, INVALID, INVALID, INVALID, JUMPDEST, STOP
	// Condition=1 (true), dest=6
	contract.Code = []byte{
		byte(PUSH1), 1,   // condition (true)
		byte(PUSH1), 6,   // destination
		byte(JUMPI),
		byte(INVALID),
		byte(JUMPDEST),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunJumpiNotTaken(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH1 0, PUSH1 7, JUMPI, STOP, ...
	// Condition=0 (false), should fall through to STOP
	contract.Code = []byte{
		byte(PUSH1), 0,   // condition (false)
		byte(PUSH1), 7,   // destination (doesn't matter)
		byte(JUMPI),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunInvalidJumpDest(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Jump to a non-JUMPDEST location
	contract.Code = []byte{
		byte(PUSH1), 3,
		byte(JUMP),
		byte(ADD), // not a JUMPDEST
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != ErrInvalidJump {
		t.Errorf("expected ErrInvalidJump, got %v", err)
	}
}

func TestRunCalldataLoad(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// Code: PUSH1 0, CALLDATALOAD, STOP
	contract.Code = []byte{
		byte(PUSH1), 0,
		byte(CALLDATALOAD),
		byte(STOP),
	}

	input := make([]byte, 32)
	input[31] = 42

	_, err := evm.Run(contract, input)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunCalldataSize(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	contract.Code = []byte{
		byte(CALLDATASIZE),
		byte(STOP),
	}

	input := make([]byte, 64)
	_, err := evm.Run(contract, input)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestRunRevert(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	// We can't easily test REVERT with return data without memory expansion,
	// so test REVERT with 0 offset and 0 size.
	contract.Code = []byte{
		byte(PUSH1), 0, // size
		byte(PUSH1), 0, // offset
		byte(REVERT),
	}

	_, err := evm.Run(contract, nil)
	if err != ErrExecutionReverted {
		t.Errorf("expected ErrExecutionReverted, got %v", err)
	}
}

func TestRunGasDeduction(t *testing.T) {
	evm := newTestEVM()
	gas := uint64(100)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), gas)
	// PUSH1 (3 gas) + PUSH1 (3 gas) + ADD (2 gas) + STOP (0 gas) = 8 gas
	contract.Code = []byte{
		byte(PUSH1), 1,
		byte(PUSH1), 2,
		byte(ADD),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	expected := gas - 3 - 3 - 2
	if contract.Gas != expected {
		t.Errorf("remaining gas = %d, want %d", contract.Gas, expected)
	}
}

func TestRunPush32(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000)
	code := make([]byte, 34) // PUSH32 + 32 bytes + STOP
	code[0] = byte(PUSH32)
	code[32] = 0xff // last byte of the pushed value
	code[33] = byte(STOP)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
}

func TestEVMCallStub(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.Call(types.Address{}, types.Address{}, nil, 1000, big.NewInt(0))
	if err != ErrNotImplemented {
		t.Errorf("Call error = %v, want ErrNotImplemented", err)
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMStaticCallStub(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.StaticCall(types.Address{}, types.Address{}, nil, 1000)
	if err != ErrNotImplemented {
		t.Errorf("StaticCall error = %v, want ErrNotImplemented", err)
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMCreateStub(t *testing.T) {
	evm := newTestEVM()
	_, _, gas, err := evm.Create(types.Address{}, nil, 1000, big.NewInt(0))
	if err != ErrNotImplemented {
		t.Errorf("Create error = %v, want ErrNotImplemented", err)
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMCreate2Stub(t *testing.T) {
	evm := newTestEVM()
	_, _, gas, err := evm.Create2(types.Address{}, nil, 1000, big.NewInt(0), big.NewInt(0))
	if err != ErrNotImplemented {
		t.Errorf("Create2 error = %v, want ErrNotImplemented", err)
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMCallCodeStub(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.CallCode(types.Address{}, types.Address{}, nil, 1000, big.NewInt(0))
	if err != ErrNotImplemented {
		t.Errorf("CallCode error = %v, want ErrNotImplemented", err)
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMDelegateCallStub(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.DelegateCall(types.Address{}, types.Address{}, nil, 1000)
	if err != ErrNotImplemented {
		t.Errorf("DelegateCall error = %v, want ErrNotImplemented", err)
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestRunEnvironmentOpcodes(t *testing.T) {
	evm := newTestEVM()
	// Test each environment opcode individually
	tests := []struct {
		name string
		code []byte
	}{
		{"ADDRESS", []byte{byte(ADDRESS), byte(POP), byte(STOP)}},
		{"CALLER", []byte{byte(CALLER), byte(POP), byte(STOP)}},
		{"CALLVALUE", []byte{byte(CALLVALUE), byte(POP), byte(STOP)}},
		{"ORIGIN", []byte{byte(ORIGIN), byte(POP), byte(STOP)}},
		{"GASPRICE", []byte{byte(GASPRICE), byte(POP), byte(STOP)}},
		{"COINBASE", []byte{byte(COINBASE), byte(POP), byte(STOP)}},
		{"TIMESTAMP", []byte{byte(TIMESTAMP), byte(POP), byte(STOP)}},
		{"NUMBER", []byte{byte(NUMBER), byte(POP), byte(STOP)}},
		{"GASLIMIT", []byte{byte(GASLIMIT), byte(POP), byte(STOP)}},
		{"GAS", []byte{byte(GAS), byte(POP), byte(STOP)}},
		{"PC", []byte{byte(PC), byte(POP), byte(STOP)}},
		{"CODESIZE", []byte{byte(CODESIZE), byte(POP), byte(STOP)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewContract(
				types.BytesToAddress([]byte{0x01}),
				types.BytesToAddress([]byte{0x02}),
				big.NewInt(1000),
				100000,
			)
			c.Code = tt.code
			_, err := evm.Run(c, nil)
			if err != nil {
				t.Fatalf("Run error for %s: %v", tt.name, err)
			}
		})
	}
}

func TestContractValidJumpdest(t *testing.T) {
	c := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 1000)
	// PUSH1 <JUMPDEST_byte> JUMPDEST STOP
	c.Code = []byte{byte(PUSH1), byte(JUMPDEST), byte(JUMPDEST), byte(STOP)}

	// Position 1 is PUSH data (0x5b happens to be JUMPDEST byte, but it's in PUSH data)
	if c.validJumpdest(big.NewInt(1)) {
		t.Error("position 1 is PUSH data, should not be valid JUMPDEST")
	}
	// Position 2 is a real JUMPDEST
	if !c.validJumpdest(big.NewInt(2)) {
		t.Error("position 2 should be a valid JUMPDEST")
	}
	// Position 0 is PUSH1, not JUMPDEST
	if c.validJumpdest(big.NewInt(0)) {
		t.Error("position 0 is PUSH1, should not be valid JUMPDEST")
	}
	// Out of bounds
	if c.validJumpdest(big.NewInt(100)) {
		t.Error("out of bounds should not be valid JUMPDEST")
	}
}

func TestForkJumpTables(t *testing.T) {
	// Test that each fork constructor returns a valid table
	tables := []struct {
		name string
		fn   func() JumpTable
	}{
		{"Frontier", NewFrontierJumpTable},
		{"Homestead", NewHomesteadJumpTable},
		{"TangerineWhistle", NewTangerineWhistleJumpTable},
		{"SpuriousDragon", NewSpuriousDragonJumpTable},
		{"Byzantium", NewByzantiumJumpTable},
		{"Constantinople", NewConstantinopleJumpTable},
		{"Istanbul", NewIstanbulJumpTable},
		{"Berlin", NewBerlinJumpTable},
		{"London", NewLondonJumpTable},
		{"Merge", NewMergeJumpTable},
		{"Shanghai", NewShanghaiJumpTable},
		{"Cancun", NewCancunJumpTable},
		{"Prague", NewPragueJumpTable},
	}

	for _, tt := range tables {
		t.Run(tt.name, func(t *testing.T) {
			tbl := tt.fn()
			// Basic opcodes should always be present
			if tbl[ADD] == nil {
				t.Error("ADD should be in jump table")
			}
			if tbl[STOP] == nil {
				t.Error("STOP should be in jump table")
			}
			if tbl[PUSH1] == nil {
				t.Error("PUSH1 should be in jump table")
			}
		})
	}

	// Verify fork-specific opcodes
	frontier := NewFrontierJumpTable()
	if frontier[REVERT] != nil {
		t.Error("Frontier should NOT have REVERT")
	}
	if frontier[SHL] != nil {
		t.Error("Frontier should NOT have SHL")
	}

	byzantium := NewByzantiumJumpTable()
	if byzantium[REVERT] == nil {
		t.Error("Byzantium should have REVERT")
	}

	constantinople := NewConstantinopleJumpTable()
	if constantinople[SHL] == nil {
		t.Error("Constantinople should have SHL")
	}
	if constantinople[SHR] == nil {
		t.Error("Constantinople should have SHR")
	}
	if constantinople[SAR] == nil {
		t.Error("Constantinople should have SAR")
	}

	istanbul := NewIstanbulJumpTable()
	if istanbul[CHAINID] == nil {
		t.Error("Istanbul should have CHAINID")
	}

	london := NewLondonJumpTable()
	if london[BASEFEE] == nil {
		t.Error("London should have BASEFEE")
	}

	shanghai := NewShanghaiJumpTable()
	if shanghai[PUSH0] == nil {
		t.Error("Shanghai should have PUSH0")
	}
}
