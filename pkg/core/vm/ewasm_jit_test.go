package vm

import (
	"bytes"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// validMinimalWasm returns a minimal valid WASM binary (magic + version only,
// no sections).
func validMinimalWasm() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version 1
	}
}

func TestValidateWasmBytecodeValid(t *testing.T) {
	code := validMinimalWasm()
	if err := ValidateWasmBytecode(code); err != nil {
		t.Fatalf("ValidateWasmBytecode: %v", err)
	}
}

func TestValidateWasmBytecodeTooShort(t *testing.T) {
	if err := ValidateWasmBytecode([]byte{0x00, 0x61}); err != ErrWasmTooShort {
		t.Fatalf("expected ErrWasmTooShort, got %v", err)
	}
	if err := ValidateWasmBytecode(nil); err != ErrWasmTooShort {
		t.Fatalf("expected ErrWasmTooShort for nil, got %v", err)
	}
}

func TestValidateWasmBytecodeInvalidMagic(t *testing.T) {
	code := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0x00, 0x00}
	if err := ValidateWasmBytecode(code); err != ErrWasmInvalidMagic {
		t.Fatalf("expected ErrWasmInvalidMagic, got %v", err)
	}
}

func TestValidateWasmBytecodeInvalidVersion(t *testing.T) {
	code := []byte{0x00, 0x61, 0x73, 0x6d, 0x02, 0x00, 0x00, 0x00}
	if err := ValidateWasmBytecode(code); err != ErrWasmInvalidVersion {
		t.Fatalf("expected ErrWasmInvalidVersion, got %v", err)
	}
}

func TestValidateWasmBytecodeTooLarge(t *testing.T) {
	code := make([]byte, WasmMaxSize+1)
	copy(code, validMinimalWasm())
	if err := ValidateWasmBytecode(code); err != ErrWasmTooLarge {
		t.Fatalf("expected ErrWasmTooLarge, got %v", err)
	}
}

func TestValidateWasmBytecodeSectionTooLong(t *testing.T) {
	// Valid header + section ID + LEB128 size that exceeds remaining bytes.
	code := validMinimalWasm()
	code = append(code, WasmSectionCustom) // section id
	code = append(code, 0xFF, 0x01)        // LEB128 size = 255, but no data
	if err := ValidateWasmBytecode(code); err != ErrWasmSectionTooLong {
		t.Fatalf("expected ErrWasmSectionTooLong, got %v", err)
	}
}

func TestValidateWasmBytecodeDuplicateSection(t *testing.T) {
	code := validMinimalWasm()
	// Add two type sections (non-custom, so duplicate).
	code = append(code, WasmSectionType, 0x01, 0x00) // type section, size 1, dummy byte
	code = append(code, WasmSectionType, 0x01, 0x00) // duplicate
	if err := ValidateWasmBytecode(code); err != ErrWasmDupSection {
		t.Fatalf("expected ErrWasmDupSection, got %v", err)
	}
}

func TestValidateWasmBytecodeCustomSectionsAllowed(t *testing.T) {
	code := validMinimalWasm()
	// Multiple custom sections are allowed.
	code = append(code, WasmSectionCustom, 0x01, 0x00)
	code = append(code, WasmSectionCustom, 0x02, 0x00, 0x00)
	if err := ValidateWasmBytecode(code); err != nil {
		t.Fatalf("ValidateWasmBytecode with multiple custom sections: %v", err)
	}
}

func TestCompileModuleMinimal(t *testing.T) {
	code := validMinimalWasm()
	mod, err := CompileModule(code)
	if err != nil {
		t.Fatalf("CompileModule: %v", err)
	}
	if mod.Hash == (types.Hash{}) {
		t.Error("Hash should not be zero")
	}
	if len(mod.Code) != len(code) {
		t.Errorf("Code length = %d, want %d", len(mod.Code), len(code))
	}
	if mod.CompileTime <= 0 {
		t.Error("CompileTime should be positive")
	}
}

func TestCompileModuleWithExports(t *testing.T) {
	code := BuildMinimalWasm("main", "init")
	mod, err := CompileModule(code)
	if err != nil {
		t.Fatalf("CompileModule: %v", err)
	}
	if len(mod.ExportedFunctions) != 2 {
		t.Fatalf("ExportedFunctions = %v, want [main init]", mod.ExportedFunctions)
	}
	if mod.ExportedFunctions[0] != "main" {
		t.Errorf("ExportedFunctions[0] = %q, want %q", mod.ExportedFunctions[0], "main")
	}
	if mod.ExportedFunctions[1] != "init" {
		t.Errorf("ExportedFunctions[1] = %q, want %q", mod.ExportedFunctions[1], "init")
	}
	if mod.CodeSectionSize == 0 {
		t.Error("CodeSectionSize should be > 0")
	}
}

func TestCompileModuleInvalid(t *testing.T) {
	_, err := CompileModule([]byte{0xFF})
	if err == nil {
		t.Fatal("expected error for invalid bytecode")
	}
}

func TestExecuteExportSuccess(t *testing.T) {
	code := BuildMinimalWasm("run")
	mod, _ := CompileModule(code)

	result, err := ExecuteExport(mod, "run", []byte{0x01, 0x02})
	if err != nil {
		t.Fatalf("ExecuteExport: %v", err)
	}
	if len(result) != 32 {
		t.Errorf("result length = %d, want 32", len(result))
	}
}

func TestExecuteExportDeterministic(t *testing.T) {
	code := BuildMinimalWasm("run")
	mod, _ := CompileModule(code)

	r1, _ := ExecuteExport(mod, "run", []byte{0x01})
	r2, _ := ExecuteExport(mod, "run", []byte{0x01})
	if !bytes.Equal(r1, r2) {
		t.Error("execution should be deterministic")
	}

	// Different args should produce different result.
	r3, _ := ExecuteExport(mod, "run", []byte{0x02})
	if bytes.Equal(r1, r3) {
		t.Error("different args should produce different result")
	}
}

func TestExecuteExportNotFound(t *testing.T) {
	code := BuildMinimalWasm("run")
	mod, _ := CompileModule(code)

	_, err := ExecuteExport(mod, "nonexistent", nil)
	if err != ErrWasmExportNotFound {
		t.Fatalf("expected ErrWasmExportNotFound, got %v", err)
	}
}

func TestExecuteExportNilModule(t *testing.T) {
	_, err := ExecuteExport(nil, "run", nil)
	if err != ErrWasmExecFailed {
		t.Fatalf("expected ErrWasmExecFailed, got %v", err)
	}
}

func TestJITCachePutAndGet(t *testing.T) {
	cache := NewJITCache(4)
	code := BuildMinimalWasm("test")
	mod, _ := CompileModule(code)

	cache.CacheModule(mod.Hash, mod)
	got, ok := cache.GetCachedModule(mod.Hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Hash != mod.Hash {
		t.Error("cached module hash mismatch")
	}
}

func TestJITCacheMiss(t *testing.T) {
	cache := NewJITCache(4)
	_, ok := cache.GetCachedModule(types.Hash{0xFF})
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestJITCacheEviction(t *testing.T) {
	cache := NewJITCache(2)

	// Insert 3 modules into a cache of capacity 2.
	var hashes [3]types.Hash
	for i := 0; i < 3; i++ {
		code := BuildMinimalWasm("fn" + string(rune('a'+i)))
		mod, _ := CompileModule(code)
		hashes[i] = mod.Hash
		cache.CacheModule(mod.Hash, mod)
	}

	if cache.Size() != 2 {
		t.Errorf("Size = %d, want 2", cache.Size())
	}

	// First module should have been evicted (LRU).
	_, ok := cache.GetCachedModule(hashes[0])
	if ok {
		t.Error("expected first module to be evicted")
	}

	// Last two should still be present.
	_, ok = cache.GetCachedModule(hashes[1])
	if !ok {
		t.Error("expected second module to be cached")
	}
	_, ok = cache.GetCachedModule(hashes[2])
	if !ok {
		t.Error("expected third module to be cached")
	}
}

func TestJITCacheLRUOrdering(t *testing.T) {
	cache := NewJITCache(2)

	code1 := BuildMinimalWasm("a")
	mod1, _ := CompileModule(code1)
	code2 := BuildMinimalWasm("b")
	mod2, _ := CompileModule(code2)

	cache.CacheModule(mod1.Hash, mod1)
	cache.CacheModule(mod2.Hash, mod2)

	// Access mod1 to make it most recently used.
	cache.GetCachedModule(mod1.Hash)

	// Insert a third module; mod2 should be evicted (it's now LRU).
	code3 := BuildMinimalWasm("c")
	mod3, _ := CompileModule(code3)
	cache.CacheModule(mod3.Hash, mod3)

	_, ok := cache.GetCachedModule(mod2.Hash)
	if ok {
		t.Error("expected mod2 to be evicted (LRU)")
	}
	_, ok = cache.GetCachedModule(mod1.Hash)
	if !ok {
		t.Error("expected mod1 to still be cached (recently accessed)")
	}
}

func TestJITCacheClear(t *testing.T) {
	cache := NewJITCache(4)
	code := BuildMinimalWasm("test")
	mod, _ := CompileModule(code)
	cache.CacheModule(mod.Hash, mod)

	cache.Clear()
	if cache.Size() != 0 {
		t.Errorf("Size after Clear = %d, want 0", cache.Size())
	}
}

func TestJITCacheUpdate(t *testing.T) {
	cache := NewJITCache(4)
	code := BuildMinimalWasm("test")
	mod1, _ := CompileModule(code)
	cache.CacheModule(mod1.Hash, mod1)

	// Update with same hash.
	mod2, _ := CompileModule(code)
	cache.CacheModule(mod2.Hash, mod2)

	if cache.Size() != 1 {
		t.Errorf("Size after update = %d, want 1", cache.Size())
	}
}

func TestJITCacheConcurrency(t *testing.T) {
	cache := NewJITCache(32)
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			code := BuildMinimalWasm("fn" + string(rune(idx)))
			mod, err := CompileModule(code)
			if err != nil {
				return
			}
			cache.CacheModule(mod.Hash, mod)
			cache.GetCachedModule(mod.Hash)
			cache.Size()
		}(i)
	}
	wg.Wait()

	if cache.Size() > 32 {
		t.Errorf("Size = %d, exceeds capacity 32", cache.Size())
	}
}

func TestWasmGasCalculatorCompile(t *testing.T) {
	gc := DefaultWasmGasCalculator()

	gas := gc.CompileGas(100)
	expected := gc.BaseCompileGas + gc.PerByteCompileGas*100
	if gas != expected {
		t.Errorf("CompileGas(100) = %d, want %d", gas, expected)
	}
}

func TestWasmGasCalculatorExecute(t *testing.T) {
	gc := DefaultWasmGasCalculator()

	gas := gc.ExecuteGas(64, 200)
	expected := gc.BaseExecGas + gc.PerByteInputGas*64 + gc.PerCodeByteGas*200
	if gas != expected {
		t.Errorf("ExecuteGas(64, 200) = %d, want %d", gas, expected)
	}
}

func TestWasmGasCalculatorTotal(t *testing.T) {
	gc := DefaultWasmGasCalculator()

	total := gc.TotalGas(100, 64, 200)
	expected := gc.CompileGas(100) + gc.ExecuteGas(64, 200)
	if total != expected {
		t.Errorf("TotalGas = %d, want %d", total, expected)
	}
}

func TestWasmGasCalculatorZeroInput(t *testing.T) {
	gc := DefaultWasmGasCalculator()

	gas := gc.CompileGas(0)
	if gas != gc.BaseCompileGas {
		t.Errorf("CompileGas(0) = %d, want base %d", gas, gc.BaseCompileGas)
	}
	gas = gc.ExecuteGas(0, 0)
	if gas != gc.BaseExecGas {
		t.Errorf("ExecuteGas(0,0) = %d, want base %d", gas, gc.BaseExecGas)
	}
}

func TestBuildMinimalWasm(t *testing.T) {
	code := BuildMinimalWasm("main")
	if err := ValidateWasmBytecode(code); err != nil {
		t.Fatalf("BuildMinimalWasm produced invalid WASM: %v", err)
	}
	mod, err := CompileModule(code)
	if err != nil {
		t.Fatalf("CompileModule on BuildMinimalWasm: %v", err)
	}
	if len(mod.ExportedFunctions) != 1 || mod.ExportedFunctions[0] != "main" {
		t.Errorf("exports = %v, want [main]", mod.ExportedFunctions)
	}
}

func TestBuildMinimalWasmNoExports(t *testing.T) {
	code := BuildMinimalWasm()
	if err := ValidateWasmBytecode(code); err != nil {
		t.Fatalf("BuildMinimalWasm() produced invalid WASM: %v", err)
	}
}

func TestDecodeLEB128(t *testing.T) {
	tests := []struct {
		input []byte
		want  uint32
		wantN int
	}{
		{[]byte{0x00}, 0, 1},
		{[]byte{0x01}, 1, 1},
		{[]byte{0x7F}, 127, 1},
		{[]byte{0x80, 0x01}, 128, 2},
		{[]byte{0xE5, 0x8E, 0x26}, 624485, 3},
	}
	for _, tt := range tests {
		got, n, err := decodeLEB128(tt.input)
		if err != nil {
			t.Errorf("decodeLEB128(%v): %v", tt.input, err)
			continue
		}
		if got != tt.want || n != tt.wantN {
			t.Errorf("decodeLEB128(%v) = (%d, %d), want (%d, %d)", tt.input, got, n, tt.want, tt.wantN)
		}
	}
}

func TestDecodeLEB128Invalid(t *testing.T) {
	// All bytes have high bit set but never terminate within 5 bytes.
	_, _, err := decodeLEB128([]byte{0x80, 0x80, 0x80, 0x80, 0x80})
	if err == nil {
		t.Fatal("expected error for unterminated LEB128")
	}
}

func TestWasmModuleCodeIsCopy(t *testing.T) {
	code := BuildMinimalWasm("test")
	mod, _ := CompileModule(code)

	// Mutate original code.
	code[0] = 0xFF
	if mod.Code[0] == 0xFF {
		t.Error("module code should be a copy, not a reference")
	}
}
