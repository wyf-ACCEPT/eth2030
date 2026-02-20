package vm

import (
	"sync"
	"testing"
)

// buildEWASMTestModule returns a minimal valid WASM binary with a "run" export.
func buildEWASMTestModule() []byte {
	return BuildMinimalWasm("run")
}

// buildEWASMTestModuleNoRun returns a minimal valid WASM binary without a "run" export.
func buildEWASMTestModuleNoRun() []byte {
	return BuildMinimalWasm("other")
}

func TestNewEWASMPrecompileRegistry(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	if reg == nil {
		t.Fatal("NewEWASMPrecompileRegistry returned nil")
	}
	if reg.Count() != 0 {
		t.Errorf("expected empty registry, got count=%d", reg.Count())
	}
}

func TestRegisterAndIsRegistered(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:      "testPrecompile",
		WasmCode:  wasm,
		GasCostFn: func(input []byte) uint64 { return 100 },
	}

	if err := reg.Register(0x01, p); err != nil {
		t.Fatalf("Register(0x01) failed: %v", err)
	}

	if !reg.IsRegistered(0x01) {
		t.Error("expected 0x01 to be registered")
	}
	if reg.IsRegistered(0x02) {
		t.Error("expected 0x02 to not be registered")
	}
}

func TestRegisterDuplicateAddress(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{Name: "first", WasmCode: wasm}
	if err := reg.Register(0x01, p); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	p2 := EWASMPrecompile{Name: "second", WasmCode: wasm}
	err := reg.Register(0x01, p2)
	if err != ErrEWASMAlreadyRegistered {
		t.Errorf("expected ErrEWASMAlreadyRegistered, got %v", err)
	}
}

func TestRegisterInvalidAddress(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	for _, addr := range []byte{0x00, 0x0b, 0x0f, 0xff} {
		err := reg.Register(addr, EWASMPrecompile{Name: "bad", WasmCode: wasm})
		if err != errEWASMInvalidAddress {
			t.Errorf("Register(0x%02x): expected errEWASMInvalidAddress, got %v", addr, err)
		}
	}
}

func TestExecuteSuccess(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:      "identity",
		WasmCode:  wasm,
		GasCostFn: func(input []byte) uint64 { return 50 },
	}
	if err := reg.Register(0x04, p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	output, remainingGas, err := reg.Execute(0x04, []byte("hello"), 1000)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if remainingGas != 950 {
		t.Errorf("expected 950 remaining gas, got %d", remainingGas)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
}

func TestExecuteNotFound(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	_, _, err := reg.Execute(0x01, []byte("test"), 1000)
	if err != ErrEWASMPrecompileNotFound {
		t.Errorf("expected ErrEWASMPrecompileNotFound, got %v", err)
	}
}

func TestExecuteOutOfGas(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:      "expensive",
		WasmCode:  wasm,
		GasCostFn: func(input []byte) uint64 { return 500 },
	}
	if err := reg.Register(0x02, p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	_, remainingGas, err := reg.Execute(0x02, []byte("data"), 100)
	if err != ErrEWASMOutOfGas {
		t.Errorf("expected ErrEWASMOutOfGas, got %v", err)
	}
	if remainingGas != 0 {
		t.Errorf("expected 0 remaining gas, got %d", remainingGas)
	}
}

func TestExecuteNoRunExport(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModuleNoRun()

	p := EWASMPrecompile{
		Name:      "norun",
		WasmCode:  wasm,
		GasCostFn: func(input []byte) uint64 { return 10 },
	}
	if err := reg.Register(0x03, p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	_, _, err := reg.Execute(0x03, []byte("test"), 1000)
	if err != ErrEWASMExecutionFailed {
		t.Errorf("expected ErrEWASMExecutionFailed, got %v", err)
	}
}

func TestEWASMGasCost(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:     "sha256",
		WasmCode: wasm,
		GasCostFn: func(input []byte) uint64 {
			return 60 + 12*uint64((len(input)+31)/32)
		},
	}
	if err := reg.Register(0x02, p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	cost := reg.GasCost(0x02, make([]byte, 64))
	expected := uint64(60 + 12*2) // 2 words
	if cost != expected {
		t.Errorf("expected gas cost %d, got %d", expected, cost)
	}

	// Unregistered address returns 0.
	cost = reg.GasCost(0x05, make([]byte, 32))
	if cost != 0 {
		t.Errorf("expected 0 for unregistered, got %d", cost)
	}
}

func TestGasCostNilFunction(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:     "nogas",
		WasmCode: wasm,
		// GasCostFn is nil
	}
	if err := reg.Register(0x04, p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	cost := reg.GasCost(0x04, []byte("test"))
	if cost != 0 {
		t.Errorf("expected 0 gas cost with nil function, got %d", cost)
	}
}

func TestMigrateFromNative(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	if err := reg.MigrateFromNative(0x01, "ecRecover", wasm); err != nil {
		t.Fatalf("MigrateFromNative failed: %v", err)
	}

	if !reg.IsRegistered(0x01) {
		t.Error("expected 0x01 to be registered after migration")
	}
	if !reg.IsMigrated(0x01) {
		t.Error("expected 0x01 to be marked as migrated")
	}

	// Verify default gas cost works.
	cost := reg.GasCost(0x01, make([]byte, 100))
	expected := uint64(100 + 3*100) // 100 + 3*len(input)
	if cost != expected {
		t.Errorf("expected gas cost %d, got %d", expected, cost)
	}
}

func TestMigrateFromNativeInvalidWasm(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()

	err := reg.MigrateFromNative(0x01, "ecRecover", nil)
	if err != errEWASMNilWasmCode {
		t.Errorf("expected errEWASMNilWasmCode, got %v", err)
	}

	err = reg.MigrateFromNative(0x01, "ecRecover", []byte{})
	if err != errEWASMEmptyWasmCode {
		t.Errorf("expected errEWASMEmptyWasmCode, got %v", err)
	}

	// Invalid WASM bytecode.
	err = reg.MigrateFromNative(0x01, "ecRecover", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for invalid WASM magic")
	}
}

func TestMigrateFromNativeInvalidAddress(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	err := reg.MigrateFromNative(0x00, "bad", wasm)
	if err != errEWASMInvalidAddress {
		t.Errorf("expected errEWASMInvalidAddress, got %v", err)
	}
}

func TestListMigrated(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	// No migrations yet.
	list := reg.ListMigrated()
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d entries", len(list))
	}

	// Migrate a few.
	_ = reg.MigrateFromNative(0x04, "identity", wasm)
	_ = reg.MigrateFromNative(0x02, "sha256", wasm)
	_ = reg.MigrateFromNative(0x01, "ecRecover", wasm)

	list = reg.ListMigrated()
	if len(list) != 3 {
		t.Fatalf("expected 3 migrated, got %d", len(list))
	}
	// Should be sorted.
	if list[0] != 0x01 || list[1] != 0x02 || list[2] != 0x04 {
		t.Errorf("expected sorted [0x01, 0x02, 0x04], got %v", list)
	}
}

func TestGetPrecompile(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{Name: "ecRecover", WasmCode: wasm}
	_ = reg.Register(0x01, p)

	got := reg.Get(0x01)
	if got == nil {
		t.Fatal("Get returned nil for registered precompile")
	}
	if got.Name != "ecRecover" {
		t.Errorf("expected name 'ecRecover', got '%s'", got.Name)
	}

	// Modifying the returned copy should not affect the registry.
	got.Name = "modified"
	got2 := reg.Get(0x01)
	if got2.Name != "ecRecover" {
		t.Error("registry was mutated through returned copy")
	}

	// Non-existent address.
	if reg.Get(0x09) != nil {
		t.Error("expected nil for unregistered address")
	}
}

func TestUnregister(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	_ = reg.MigrateFromNative(0x01, "ecRecover", wasm)
	if !reg.IsRegistered(0x01) {
		t.Fatal("expected registered before unregister")
	}

	if err := reg.Unregister(0x01); err != nil {
		t.Fatalf("Unregister failed: %v", err)
	}

	if reg.IsRegistered(0x01) {
		t.Error("expected unregistered after Unregister")
	}
	if reg.IsMigrated(0x01) {
		t.Error("expected not migrated after Unregister")
	}

	// Double unregister should fail.
	err := reg.Unregister(0x01)
	if err != ErrEWASMPrecompileNotFound {
		t.Errorf("expected ErrEWASMPrecompileNotFound, got %v", err)
	}
}

func TestAllStandardAddresses(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	// Register all 10 standard addresses (0x01-0x0a).
	for addr := byte(0x01); addr <= 0x0a; addr++ {
		p := EWASMPrecompile{
			Name:      "precompile",
			WasmCode:  wasm,
			GasCostFn: func(input []byte) uint64 { return 100 },
		}
		if err := reg.Register(addr, p); err != nil {
			t.Fatalf("Register(0x%02x) failed: %v", addr, err)
		}
	}

	if reg.Count() != 10 {
		t.Errorf("expected 10 registered, got %d", reg.Count())
	}

	for addr := byte(0x01); addr <= 0x0a; addr++ {
		if !reg.IsRegistered(addr) {
			t.Errorf("expected 0x%02x to be registered", addr)
		}
	}
}

func TestEWASMConcurrentAccess(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:      "concurrent",
		WasmCode:  wasm,
		GasCostFn: func(input []byte) uint64 { return 10 },
	}
	_ = reg.Register(0x01, p)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Mix reads and executions.
			reg.IsRegistered(0x01)
			reg.GasCost(0x01, []byte("test"))
			reg.Execute(0x01, []byte("concurrent"), 1000)
			reg.Count()
			reg.ListMigrated()
		}(i)
	}
	wg.Wait()
}

func TestExecuteWithCaching(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:      "cached",
		WasmCode:  wasm,
		GasCostFn: func(input []byte) uint64 { return 10 },
	}
	_ = reg.Register(0x05, p)

	// Execute twice with same input - second should hit cache.
	out1, gas1, err1 := reg.Execute(0x05, []byte("input1"), 1000)
	if err1 != nil {
		t.Fatalf("first Execute failed: %v", err1)
	}

	out2, gas2, err2 := reg.Execute(0x05, []byte("input1"), 1000)
	if err2 != nil {
		t.Fatalf("second Execute failed: %v", err2)
	}

	if gas1 != gas2 {
		t.Errorf("gas mismatch: %d vs %d", gas1, gas2)
	}

	// Same input should produce same output (deterministic).
	if len(out1) != len(out2) {
		t.Error("output length mismatch")
	}
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Error("outputs differ for same input")
			break
		}
	}
}

func TestEwasmPrecompileHash(t *testing.T) {
	h1 := ewasmPrecompileHash(0x01, []byte("input"), []byte("hash"))
	h2 := ewasmPrecompileHash(0x01, []byte("input"), []byte("hash"))
	h3 := ewasmPrecompileHash(0x02, []byte("input"), []byte("hash"))

	if len(h1) != 32 {
		t.Errorf("expected 32-byte hash, got %d bytes", len(h1))
	}

	// Same inputs produce same hash.
	for i := range h1 {
		if h1[i] != h2[i] {
			t.Error("hash not deterministic")
			break
		}
	}

	// Different address produces different hash.
	same := true
	for i := range h1 {
		if h1[i] != h3[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("different addresses should produce different hashes")
	}
}

func TestMigrateAlreadyRegistered(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	_ = reg.MigrateFromNative(0x01, "ecRecover", wasm)
	err := reg.MigrateFromNative(0x01, "ecRecover2", wasm)
	if err != ErrEWASMAlreadyRegistered {
		t.Errorf("expected ErrEWASMAlreadyRegistered, got %v", err)
	}
}

func TestExecuteNilGasCostFn(t *testing.T) {
	reg := NewEWASMPrecompileRegistry()
	wasm := buildEWASMTestModule()

	p := EWASMPrecompile{
		Name:     "nogas",
		WasmCode: wasm,
		// GasCostFn is nil - means zero gas cost.
	}
	_ = reg.Register(0x06, p)

	output, remainingGas, err := reg.Execute(0x06, []byte("test"), 500)
	if err != nil {
		t.Fatalf("Execute with nil gas fn failed: %v", err)
	}
	if remainingGas != 500 {
		t.Errorf("expected 500 remaining gas (0 cost), got %d", remainingGas)
	}
	if len(output) == 0 {
		t.Error("expected non-empty output")
	}
}
