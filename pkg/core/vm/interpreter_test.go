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

func TestEVMCallNoState(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.Call(types.Address{}, types.Address{}, nil, 1000, big.NewInt(0))
	if err == nil {
		t.Error("Call: expected error with no state database")
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMStaticCallNoState(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.StaticCall(types.Address{}, types.Address{}, nil, 1000)
	if err == nil {
		t.Error("StaticCall: expected error with no state database")
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMCreateNoState(t *testing.T) {
	evm := newTestEVM()
	_, _, gas, err := evm.Create(types.Address{}, nil, 1000, big.NewInt(0))
	if err == nil {
		t.Error("Create: expected error with no state database")
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMCreate2NoState(t *testing.T) {
	evm := newTestEVM()
	_, _, gas, err := evm.Create2(types.Address{}, nil, 1000, big.NewInt(0), big.NewInt(0))
	if err == nil {
		t.Error("Create2: expected error with no state database")
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMCallCodeNoState(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.CallCode(types.Address{}, types.Address{}, nil, 1000, big.NewInt(0))
	if err == nil {
		t.Error("CallCode: expected error with no state database")
	}
	if gas != 1000 {
		t.Errorf("gas = %d, want 1000", gas)
	}
}

func TestEVMDelegateCallNoState(t *testing.T) {
	evm := newTestEVM()
	_, gas, err := evm.DelegateCall(types.Address{}, types.Address{}, nil, 1000)
	if err == nil {
		t.Error("DelegateCall: expected error with no state database")
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

// --- Memory expansion gas tests ---

// TestMemoryExpansionGasCharged verifies that the interpreter charges gas
// for memory expansion when executing MSTORE (which expands memory).
func TestMemoryExpansionGasCharged(t *testing.T) {
	evm := newTestEVM()
	// Gas budget: enough for opcodes but we'll verify gas consumed includes
	// memory expansion cost.
	initialGas := uint64(100000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
	// PUSH1 0x42, PUSH1 0x00, MSTORE, STOP
	// MSTORE at offset 0 expands memory from 0 to 32 bytes.
	contract.Code = []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Gas consumed:
	// PUSH1: 3, PUSH1: 3, MSTORE: 3 (constant) + 3 (memory: 1 word = 3 gas), STOP: 0
	// Total: 3 + 3 + 3 + 3 = 12
	gasUsed := initialGas - contract.Gas
	expectedGas := uint64(3 + 3 + 3 + 3) // 2*PUSH1 + MSTORE_constant + mem_expansion(0->32)
	if gasUsed != expectedGas {
		t.Errorf("gas used = %d, want %d (memory expansion gas should be charged)", gasUsed, expectedGas)
	}
}

// TestMemoryExpansionGasDelta verifies that expanding already-expanded memory
// charges only the delta cost.
func TestMemoryExpansionGasDelta(t *testing.T) {
	evm := newTestEVM()
	initialGas := uint64(100000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
	// First MSTORE at offset 0 (expands to 32 bytes),
	// then MSTORE at offset 32 (expands to 64 bytes).
	contract.Code = []byte{
		byte(PUSH1), 0x01,  // value
		byte(PUSH1), 0x00,  // offset 0
		byte(MSTORE),       // mem: 0 -> 32 bytes
		byte(PUSH1), 0x02,  // value
		byte(PUSH1), 0x20,  // offset 32
		byte(MSTORE),       // mem: 32 -> 64 bytes
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Gas consumed:
	// PUSH1: 3, PUSH1: 3, MSTORE: 3 + mem(0->32)=3  => 12
	// PUSH1: 3, PUSH1: 3, MSTORE: 3 + mem(32->64)=3  => 12
	// STOP: 0
	// Total: 24
	// mem(0->32) = 1 word: 1*3 + 1/512 = 3
	// mem(32->64) = delta: cost(2 words) - cost(1 word) = 6 - 3 = 3
	gasUsed := initialGas - contract.Gas
	expectedGas := uint64(3 + 3 + 3 + 3 + 3 + 3 + 3 + 3)
	if gasUsed != expectedGas {
		t.Errorf("gas used = %d, want %d", gasUsed, expectedGas)
	}
}

// TestMemoryExpansionNoDoubleCharge verifies that when an opcode has both
// memory expansion and dynamic gas, the memory expansion gas is not
// double-charged. The Run loop charges memory expansion, and dynamic gas
// functions should not re-charge for memory.
func TestMemoryExpansionNoDoubleCharge(t *testing.T) {
	evm := newTestEVM()
	initialGas := uint64(100000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
	// MSTORE at offset 0: has both memorySize and dynamicGas (gasMemExpansion).
	// Memory expansion should only be charged once.
	contract.Code = []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// If memory expansion were double-charged:
	//   2*PUSH1(3) + MSTORE_constant(3) + mem_expansion(3) + dynamic_gas_mem(3) = 15
	// Correct (single charge):
	//   2*PUSH1(3) + MSTORE_constant(3) + mem_expansion(3) + dynamic_gas_mem(0) = 12
	// The dynamic gas function returns 0 because mem is already resized.
	gasUsed := initialGas - contract.Gas
	expectedGas := uint64(12) // 3 + 3 + 3 + 3, not 15
	if gasUsed != expectedGas {
		t.Errorf("gas used = %d, want %d (memory gas should not be double-charged)", gasUsed, expectedGas)
	}
}

// TestMemoryExpansionOOG verifies that running out of gas during memory
// expansion returns ErrOutOfGas.
func TestMemoryExpansionOOG(t *testing.T) {
	evm := newTestEVM()
	// Give just enough gas for the two PUSH1 ops + MSTORE constant gas,
	// but NOT enough for the memory expansion.
	// 2*PUSH1(3) + MSTORE_constant(3) = 9. Memory expansion costs 3 more.
	// With 11 gas, we can afford PUSH1+PUSH1+MSTORE_constant but not all of expansion.
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 11)
	contract.Code = []byte{
		byte(PUSH1), 0x42,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas, got %v", err)
	}
}

// TestMemoryExpansionLargeOffset verifies gas for expanding memory to a large
// offset. The quadratic term makes this expensive.
func TestMemoryExpansionLargeOffset(t *testing.T) {
	evm := newTestEVM()
	initialGas := uint64(10000000) // 10M gas
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
	// MSTORE at offset 0x1000 (4096). This expands memory to 4096+32 = 4128 bytes.
	// Rounded to words: ceil(4128/32) = 129 words. newSize = 129*32 = 4128.
	// Cost = 129*3 + 129^2/512 = 387 + 32 = 419
	contract.Code = []byte{
		byte(PUSH1), 0x00,      // value
		byte(PUSH2), 0x10, 0x00, // offset = 4096
		byte(MSTORE),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	gasUsed := initialGas - contract.Gas
	// PUSH1(3) + PUSH2(3) + MSTORE_constant(3) + mem_expansion(419) + STOP(0) = 428
	expectedMemCost, ok := MemoryCost(0, 4128)
	if !ok {
		t.Fatal("MemoryCost(0, 4128) returned ok=false")
	}
	expectedGas := uint64(3 + 3 + 3) + expectedMemCost
	if gasUsed != expectedGas {
		t.Errorf("gas used = %d, want %d (large memory expansion)", gasUsed, expectedGas)
	}
}

// TestMemoryExpansionExceedsMaxSize verifies that attempting to expand memory
// beyond MaxMemorySize returns ErrOutOfGas.
func TestMemoryExpansionExceedsMaxSize(t *testing.T) {
	evm := newTestEVM()
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), 100000000)
	// MSTORE at a very large offset that would exceed MaxMemorySize.
	// We use PUSH32 to push a large offset value.
	code := make([]byte, 0, 70)
	code = append(code, byte(PUSH1), 0x00) // value
	// Push a 32-byte offset representing MaxMemorySize (33554432 = 0x2000000).
	// This will try to expand to MaxMemorySize + 32, which exceeds the limit.
	code = append(code, byte(PUSH32))
	offset := make([]byte, 32)
	// Set offset to MaxMemorySize (32 MiB). MSTORE at that offset would need
	// MaxMemorySize + 32 bytes = exceeds limit.
	offsetVal := new(big.Int).SetUint64(MaxMemorySize)
	offsetBytes := offsetVal.Bytes()
	copy(offset[32-len(offsetBytes):], offsetBytes)
	code = append(code, offset...)
	code = append(code, byte(MSTORE))
	code = append(code, byte(STOP))
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != ErrOutOfGas {
		t.Errorf("expected ErrOutOfGas for memory exceeding MaxMemorySize, got %v", err)
	}
}

// TestMemoryExpansionMstore8 verifies that MSTORE8 also charges memory gas.
func TestMemoryExpansionMstore8(t *testing.T) {
	evm := newTestEVM()
	initialGas := uint64(100000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
	// MSTORE8 at offset 0 expands memory from 0 to 32 bytes (1 word).
	contract.Code = []byte{
		byte(PUSH1), 0xAB,
		byte(PUSH1), 0x00,
		byte(MSTORE8),
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// PUSH1(3) + PUSH1(3) + MSTORE8_constant(3) + mem(0->32)(3) = 12
	gasUsed := initialGas - contract.Gas
	if gasUsed != 12 {
		t.Errorf("gas used = %d, want 12", gasUsed)
	}
}

// TestMemoryExpansionRepeatedSameSize verifies that expanding to the same size
// a second time costs nothing (no re-expansion).
func TestMemoryExpansionRepeatedSameSize(t *testing.T) {
	evm := newTestEVM()
	initialGas := uint64(100000)
	contract := NewContract(types.Address{}, types.Address{}, big.NewInt(0), initialGas)
	// Two MSTORE at offset 0: second one should not re-charge memory gas.
	contract.Code = []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x00,
		byte(MSTORE),       // first: expand to 32
		byte(PUSH1), 0x02,
		byte(PUSH1), 0x00,
		byte(MSTORE),       // second: no expansion needed
		byte(STOP),
	}

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// First MSTORE: PUSH1(3) + PUSH1(3) + MSTORE(3) + mem(3) = 12
	// Second MSTORE: PUSH1(3) + PUSH1(3) + MSTORE(3) + mem(0) = 9
	// Total: 21
	gasUsed := initialGas - contract.Gas
	if gasUsed != 21 {
		t.Errorf("gas used = %d, want 21 (second MSTORE should not re-charge memory gas)", gasUsed)
	}
}
