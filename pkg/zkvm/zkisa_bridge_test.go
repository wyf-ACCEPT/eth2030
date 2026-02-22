package zkvm

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// buildZKISATestProgram creates a minimal RISC-V program for zkISA bridge tests.
func buildZKISATestProgram() []byte {
	instrs := []uint32{
		EncodeIType(0x13, 17, 0, 0, 0), // a7 = 0 (halt)
		EncodeIType(0x13, 10, 0, 0, 0), // a0 = 0 (exit code)
		0x00000073,                     // ECALL
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

func TestZKISABridge_NewZKISABridge(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}
	if bridge == nil {
		t.Fatal("bridge is nil")
	}
	if bridge.OpTable() == nil {
		t.Fatal("op table is nil")
	}
}

func TestZKISABridge_NilRegistry(t *testing.T) {
	_, err := NewZKISABridge(nil)
	if !errors.Is(err, ErrZKISANilRegistry) {
		t.Fatalf("expected ErrZKISANilRegistry, got %v", err)
	}
}

func TestZKISABridge_OpTable(t *testing.T) {
	table := NewZKISAOpTable()
	if table.Count() == 0 {
		t.Fatal("default op table should have entries")
	}

	// Verify standard operations exist.
	ops := []uint32{
		ZKISAOpHash, ZKISAOpSHA256, ZKISAOpECRecover,
		ZKISAOpModExp, ZKISAOpBN256Add, ZKISAOpBN256ScalarMul,
		ZKISAOpBN256Pairing, ZKISAOpBLSVerify, ZKISAOpCustom,
	}
	for _, op := range ops {
		entry, err := table.Lookup(op)
		if err != nil {
			t.Errorf("Lookup(0x%02x): %v", op, err)
			continue
		}
		if entry.BaseGas == 0 {
			t.Errorf("op 0x%02x has zero base gas", op)
		}
	}
}

func TestZKISABridge_OpTableLookupNotFound(t *testing.T) {
	table := NewZKISAOpTable()
	_, err := table.Lookup(0xDEAD)
	if !errors.Is(err, ErrZKISAOpNotFound) {
		t.Fatalf("expected ErrZKISAOpNotFound, got %v", err)
	}
}

func TestZKISABridge_OpTableRegister(t *testing.T) {
	table := NewZKISAOpTable()
	before := table.Count()

	entry := &ZKISAOpEntry{
		Selector: 0xAB,
		Name:     "test_op",
		BaseGas:  999,
	}
	table.Register(entry)

	if table.Count() != before+1 {
		t.Errorf("count: got %d, want %d", table.Count(), before+1)
	}

	fetched, err := table.Lookup(0xAB)
	if err != nil {
		t.Fatalf("Lookup(0xAB): %v", err)
	}
	if fetched.Name != "test_op" {
		t.Errorf("name: got %q, want %q", fetched.Name, "test_op")
	}
}

func TestZKISABridge_ExecuteZKISA(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	program := buildZKISATestProgram()
	input := []byte("zkisa test input")

	output, proof, gasUsed, err := bridge.ExecuteZKISA(program, input, 1<<24)
	if err != nil {
		t.Fatalf("ExecuteZKISA: %v", err)
	}
	if len(output) == 0 {
		// Output may be empty for a halt-only program; that is acceptable.
	}
	if len(proof) == 0 {
		t.Error("proof should not be empty")
	}
	if gasUsed == 0 {
		t.Error("gas used should be > 0")
	}
}

func TestZKISABridge_ExecuteZKISAEmptyProgram(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	_, _, _, err = bridge.ExecuteZKISA(nil, nil, 1<<24)
	if !errors.Is(err, ErrZKISAEmptyProgram) {
		t.Fatalf("expected ErrZKISAEmptyProgram, got %v", err)
	}
}

func TestZKISABridge_ExecuteHostCallHash(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	call := &ZKISAHostCall{
		Selector: ZKISAOpHash,
		Input:    []byte("hello"),
		GasLimit: 100000,
	}

	result, err := bridge.ExecuteHostCall(call)
	if err != nil {
		t.Fatalf("ExecuteHostCall: %v", err)
	}
	if !result.Success {
		t.Fatal("call should succeed")
	}

	// Verify output matches keccak256("hello").
	expected := crypto.Keccak256([]byte("hello"))
	if len(result.Output) != len(expected) {
		t.Fatalf("output length: got %d, want %d", len(result.Output), len(expected))
	}
	for i := range expected {
		if result.Output[i] != expected[i] {
			t.Fatalf("output byte %d: got 0x%02x, want 0x%02x", i, result.Output[i], expected[i])
		}
	}
}

func TestZKISABridge_ExecuteHostCallSHA256(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	call := &ZKISAHostCall{
		Selector: ZKISAOpSHA256,
		Input:    []byte("test data"),
		GasLimit: 100000,
	}

	result, err := bridge.ExecuteHostCall(call)
	if err != nil {
		t.Fatalf("ExecuteHostCall: %v", err)
	}
	if !result.Success {
		t.Fatal("call should succeed")
	}
	if len(result.Output) != 32 {
		t.Errorf("SHA-256 output length: got %d, want 32", len(result.Output))
	}
}

func TestZKISABridge_ExecuteHostCallGasExhausted(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	call := &ZKISAHostCall{
		Selector: ZKISAOpHash,
		Input:    []byte("hello"),
		GasLimit: 1, // Way too little gas
	}

	_, err = bridge.ExecuteHostCall(call)
	if !errors.Is(err, ErrZKISAGasExhausted) {
		t.Fatalf("expected ErrZKISAGasExhausted, got %v", err)
	}
}

func TestZKISABridge_ExecuteHostCallNotFound(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	call := &ZKISAHostCall{
		Selector: 0xDEAD,
		Input:    []byte("data"),
		GasLimit: 100000,
	}

	_, err = bridge.ExecuteHostCall(call)
	if !errors.Is(err, ErrZKISAOpNotFound) {
		t.Fatalf("expected ErrZKISAOpNotFound, got %v", err)
	}
}

func TestZKISABridge_PrecompileRequiredGas(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	precompile := &ZKISAPrecompile{Bridge: bridge}

	// Test with valid selector.
	input := make([]byte, 36) // 4-byte selector + 32 bytes data
	binary.BigEndian.PutUint32(input[:4], ZKISAOpHash)
	gas := precompile.RequiredGas(input)
	expectedGas := zkisaGasHash + 32*zkisaGasPerInputByte
	if gas != expectedGas {
		t.Errorf("RequiredGas: got %d, want %d", gas, expectedGas)
	}

	// Test with short input.
	shortGas := precompile.RequiredGas([]byte{1, 2})
	if shortGas != zkisaGasCustomBase {
		t.Errorf("RequiredGas(short): got %d, want %d", shortGas, zkisaGasCustomBase)
	}
}

func TestZKISABridge_PrecompileRun(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	precompile := &ZKISAPrecompile{Bridge: bridge}

	// Execute SHA-256 via precompile.
	input := make([]byte, 0, 4+5)
	var selBuf [4]byte
	binary.BigEndian.PutUint32(selBuf[:], ZKISAOpSHA256)
	input = append(input, selBuf[:]...)
	input = append(input, []byte("hello")...)

	output, err := precompile.Run(input)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(output) != 32 {
		t.Errorf("output length: got %d, want 32", len(output))
	}
}

func TestZKISABridge_PrecompileRunShortInput(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	precompile := &ZKISAPrecompile{Bridge: bridge}
	_, err = precompile.Run([]byte{1, 2})
	if !errors.Is(err, ErrZKISAInputTooShort) {
		t.Fatalf("expected ErrZKISAInputTooShort, got %v", err)
	}
}

func TestZKISABridge_MapPrecompileToZKISA(t *testing.T) {
	tests := []struct {
		addr     byte
		expected uint32
	}{
		{0x01, ZKISAOpECRecover},
		{0x02, ZKISAOpSHA256},
		{0x05, ZKISAOpModExp},
		{0x06, ZKISAOpBN256Add},
		{0x07, ZKISAOpBN256ScalarMul},
		{0x08, ZKISAOpBN256Pairing},
		{0x0A, ZKISAOpBLSVerify},
		{0x00, 0}, // No mapping
		{0xFF, 0}, // No mapping
	}

	for _, tc := range tests {
		got := MapPrecompileToZKISA(tc.addr)
		if got != tc.expected {
			t.Errorf("MapPrecompileToZKISA(0x%02x): got 0x%02x, want 0x%02x",
				tc.addr, got, tc.expected)
		}
	}
}

func TestZKISABridge_GasCost(t *testing.T) {
	cost := ZKISAGasCost(ZKISAOpHash, 100)
	expected := zkisaGasHash + 100*zkisaGasPerInputByte
	if cost != expected {
		t.Errorf("ZKISAGasCost: got %d, want %d", cost, expected)
	}

	// Unknown selector should use custom base.
	unknownCost := ZKISAGasCost(0xBAD, 50)
	expectedUnknown := zkisaGasCustomBase + 50*zkisaGasPerInputByte
	if unknownCost != expectedUnknown {
		t.Errorf("ZKISAGasCost(unknown): got %d, want %d", unknownCost, expectedUnknown)
	}
}

func TestZKISABridge_AllOperations(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	selectors := []uint32{
		ZKISAOpHash, ZKISAOpSHA256, ZKISAOpECRecover,
		ZKISAOpModExp, ZKISAOpBN256Add, ZKISAOpBN256ScalarMul,
		ZKISAOpBN256Pairing, ZKISAOpBLSVerify, ZKISAOpCustom,
	}

	for _, sel := range selectors {
		call := &ZKISAHostCall{
			Selector: sel,
			Input:    []byte("operation test data"),
			GasLimit: 1 << 20,
		}
		result, err := bridge.ExecuteHostCall(call)
		if err != nil {
			t.Errorf("selector 0x%02x: %v", sel, err)
			continue
		}
		if !result.Success {
			t.Errorf("selector 0x%02x: call failed", sel)
		}
		if len(result.Output) == 0 {
			t.Errorf("selector 0x%02x: empty output", sel)
		}
		if len(result.Proof) == 0 {
			t.Errorf("selector 0x%02x: empty proof", sel)
		}
		if result.GasUsed == 0 {
			t.Errorf("selector 0x%02x: zero gas", sel)
		}
	}
}

func TestZKISABridge_PrecompileAddr(t *testing.T) {
	if ZKISAPrecompileAddr != (types.BytesToAddress([]byte{0x20})) {
		t.Error("ZKISAPrecompileAddr should be 0x20")
	}
}

func TestZKISABridge_HostCallNilInput(t *testing.T) {
	reg := NewGuestRegistry()
	bridge, err := NewZKISABridge(reg)
	if err != nil {
		t.Fatalf("NewZKISABridge: %v", err)
	}

	_, err = bridge.ExecuteHostCall(nil)
	if !errors.Is(err, ErrZKISAInputTooShort) {
		t.Fatalf("expected ErrZKISAInputTooShort, got %v", err)
	}
}

func TestValidateBridgeCall(t *testing.T) {
	// Nil call.
	if err := ValidateBridgeCall(nil); err == nil {
		t.Fatal("expected error for nil call")
	}

	// Zero gas limit.
	call := &ZKISAHostCall{
		Selector: ZKISAOpHash,
		GasLimit: 0,
	}
	if err := ValidateBridgeCall(call); err == nil {
		t.Fatal("expected error for zero gas limit")
	}

	// Invalid selector.
	call2 := &ZKISAHostCall{
		Selector: 0x77, // not a valid op
		GasLimit: 100,
	}
	if err := ValidateBridgeCall(call2); err == nil {
		t.Fatal("expected error for invalid selector")
	}

	// Valid call.
	validCall := &ZKISAHostCall{
		Selector: ZKISAOpHash,
		GasLimit: 1000,
	}
	if err := ValidateBridgeCall(validCall); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
