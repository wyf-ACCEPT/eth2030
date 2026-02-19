package zkvm

import (
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

func TestNewRiscVGuest(t *testing.T) {
	program := []byte("test-program")
	input := []byte("test-input")
	config := DefaultGuestConfig()

	guest := NewRiscVGuest(program, input, config)
	if guest == nil {
		t.Fatal("expected non-nil guest")
	}
}

func TestRiscVGuest_Execute(t *testing.T) {
	program := []byte("test-program")
	input := []byte("test-input")
	config := DefaultGuestConfig()

	guest := NewRiscVGuest(program, input, config)
	exec, err := guest.Execute()
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !exec.Success {
		t.Error("expected successful execution")
	}
	if len(exec.Output) == 0 {
		t.Error("expected non-empty output")
	}
	if len(exec.ProofData) == 0 {
		t.Error("expected non-empty proof data")
	}
	if exec.Cycles == 0 {
		t.Error("expected non-zero cycle count")
	}
}

func TestRiscVGuest_Execute_EmptyProgram(t *testing.T) {
	guest := NewRiscVGuest(nil, []byte("input"), DefaultGuestConfig())
	_, err := guest.Execute()
	if err != ErrGuestEmptyProgram {
		t.Fatalf("expected ErrGuestEmptyProgram, got %v", err)
	}
}

func TestRiscVGuest_Execute_CycleLimit(t *testing.T) {
	config := CanonicalGuestConfig{
		MaxCycles:   10,
		MemoryLimit: DefaultMemoryLimit,
		ProofSystem: DefaultProofSystem,
	}

	// Program + input size exceeds 10 cycles.
	program := make([]byte, 8)
	input := make([]byte, 8)
	guest := NewRiscVGuest(program, input, config)
	_, err := guest.Execute()
	if err != ErrGuestCycleLimit {
		t.Fatalf("expected ErrGuestCycleLimit, got %v", err)
	}
}

func TestRiscVGuest_Execute_MemoryLimit(t *testing.T) {
	config := CanonicalGuestConfig{
		MaxCycles:   1 << 30, // large cycle limit
		MemoryLimit: 10,      // tiny memory limit
		ProofSystem: DefaultProofSystem,
	}

	program := make([]byte, 4) // 4 * 4 = 16 > 10
	guest := NewRiscVGuest(program, []byte("x"), config)
	_, err := guest.Execute()
	if err != ErrGuestMemoryLimit {
		t.Fatalf("expected ErrGuestMemoryLimit, got %v", err)
	}
}

func TestVerifyExecution(t *testing.T) {
	program := []byte("test-program")
	input := []byte("test-input")

	guest := NewRiscVGuest(program, input, DefaultGuestConfig())
	exec, err := guest.Execute()
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if !VerifyExecution(exec, program, input) {
		t.Error("verification should succeed for valid execution")
	}
}

func TestVerifyExecution_WrongProgram(t *testing.T) {
	program := []byte("test-program")
	input := []byte("test-input")

	guest := NewRiscVGuest(program, input, DefaultGuestConfig())
	exec, err := guest.Execute()
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if VerifyExecution(exec, []byte("wrong-program"), input) {
		t.Error("verification should fail for wrong program")
	}
}

func TestVerifyExecution_NilExecution(t *testing.T) {
	if VerifyExecution(nil, []byte("p"), []byte("i")) {
		t.Error("nil execution should not verify")
	}
}

func TestVerifyExecution_FailedExecution(t *testing.T) {
	exec := &GuestExecution{Success: false}
	if VerifyExecution(exec, []byte("p"), []byte("i")) {
		t.Error("failed execution should not verify")
	}
}

func TestGuestRegistry(t *testing.T) {
	reg := NewGuestRegistry()

	program := []byte("my-guest-program")
	hash, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	expectedHash := crypto.Keccak256Hash(program)
	if hash != expectedHash {
		t.Errorf("hash mismatch: got %s, want %s", hash.Hex(), expectedHash.Hex())
	}

	// Retrieve.
	got, err := reg.GetGuest(hash)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(got) != string(program) {
		t.Error("retrieved program mismatch")
	}

	// Count.
	if reg.Count() != 1 {
		t.Errorf("expected count 1, got %d", reg.Count())
	}
}

func TestGuestRegistry_DuplicateRegistration(t *testing.T) {
	reg := NewGuestRegistry()
	program := []byte("duplicate-program")

	_, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	_, err = reg.RegisterGuest(program)
	if err != ErrGuestAlreadyRegistered {
		t.Fatalf("expected ErrGuestAlreadyRegistered, got %v", err)
	}
}

func TestGuestRegistry_EmptyProgram(t *testing.T) {
	reg := NewGuestRegistry()
	_, err := reg.RegisterGuest(nil)
	if err != ErrGuestEmptyProgram {
		t.Fatalf("expected ErrGuestEmptyProgram, got %v", err)
	}
}

func TestGuestRegistry_NotFound(t *testing.T) {
	reg := NewGuestRegistry()
	_, err := reg.GetGuest(types.HexToHash("0xdeadbeef"))
	if err != ErrGuestNotRegistered {
		t.Fatalf("expected ErrGuestNotRegistered, got %v", err)
	}
}

func TestCanonicalGuestPrecompile_RequiredGas(t *testing.T) {
	p := &CanonicalGuestPrecompile{
		Registry: NewGuestRegistry(),
		Config:   DefaultGuestConfig(),
	}

	// Short input.
	gas := p.RequiredGas(make([]byte, 10))
	if gas != GuestPrecompileBaseGas {
		t.Errorf("expected base gas %d, got %d", GuestPrecompileBaseGas, gas)
	}

	// Normal input: 32 bytes hash + 100 bytes input.
	input := make([]byte, 132)
	gas = p.RequiredGas(input)
	expected := uint64(GuestPrecompileBaseGas + 100*GuestPrecompilePerCycleGas)
	if gas != expected {
		t.Errorf("expected gas %d, got %d", expected, gas)
	}
}

func TestCanonicalGuestPrecompile_Run(t *testing.T) {
	reg := NewGuestRegistry()
	program := []byte("precompile-test-program")
	programHash, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}

	p := &CanonicalGuestPrecompile{
		Registry: reg,
		Config:   DefaultGuestConfig(),
	}

	// Build input: programHash(32) || guestInput.
	guestInput := []byte("hello-world")
	input := make([]byte, 32+len(guestInput))
	copy(input[:32], programHash[:])
	copy(input[32:], guestInput)

	output, err := p.Run(input)
	if err != nil {
		t.Fatalf("precompile run failed: %v", err)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
}

func TestCanonicalGuestPrecompile_Run_ShortInput(t *testing.T) {
	p := &CanonicalGuestPrecompile{
		Registry: NewGuestRegistry(),
		Config:   DefaultGuestConfig(),
	}

	_, err := p.Run(make([]byte, 10))
	if err != ErrGuestInputTooShort {
		t.Fatalf("expected ErrGuestInputTooShort, got %v", err)
	}
}

func TestCanonicalGuestPrecompile_Run_UnregisteredProgram(t *testing.T) {
	p := &CanonicalGuestPrecompile{
		Registry: NewGuestRegistry(),
		Config:   DefaultGuestConfig(),
	}

	input := make([]byte, 64)
	_, err := p.Run(input)
	if err == nil {
		t.Fatal("expected error for unregistered program")
	}
}

func TestCanonicalGuestPrecompileAddr(t *testing.T) {
	expected := types.BytesToAddress([]byte{0x02, 0x00})
	if CanonicalGuestPrecompileAddr != expected {
		t.Errorf("wrong precompile address: got %s, want %s",
			CanonicalGuestPrecompileAddr.Hex(), expected.Hex())
	}
}
