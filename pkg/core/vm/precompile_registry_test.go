package vm

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewPrecompileRegistryDefaults(t *testing.T) {
	r := NewPrecompileRegistry()
	if r.Count() != 10 {
		t.Fatalf("expected 10 default precompiles, got %d", r.Count())
	}

	// Verify all 10 default addresses are present.
	for i := byte(1); i <= 0x0a; i++ {
		addr := types.BytesToAddress([]byte{i})
		if !r.IsPrecompile(addr) {
			t.Errorf("expected address 0x%02x to be registered", i)
		}
	}
}

func TestRegisterAndLookup(t *testing.T) {
	r := NewPrecompileRegistry()
	custom := PrecompileInfo{
		Address:        types.BytesToAddress([]byte{0x20}),
		Name:           "custom",
		GasCost:        func([]byte) uint64 { return 42 },
		MinInput:       0,
		MaxInput:       256,
		ActivationFork: "Glamsterdan",
	}

	if err := r.Register(custom); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if r.Count() != 11 {
		t.Fatalf("expected 11 precompiles after register, got %d", r.Count())
	}

	info, ok := r.Lookup(custom.Address)
	if !ok {
		t.Fatal("Lookup returned false for registered address")
	}
	if info.Name != "custom" {
		t.Errorf("expected name 'custom', got %q", info.Name)
	}
	if info.MaxInput != 256 {
		t.Errorf("expected MaxInput 256, got %d", info.MaxInput)
	}
}

func TestRegisterConflict(t *testing.T) {
	r := NewPrecompileRegistry()

	// Address 0x01 is already taken by ecRecover.
	dup := PrecompileInfo{
		Address:        types.BytesToAddress([]byte{0x01}),
		Name:           "duplicate",
		GasCost:        func([]byte) uint64 { return 1 },
		ActivationFork: "Test",
	}
	if err := r.Register(dup); err == nil {
		t.Fatal("expected error for duplicate address, got nil")
	}
}

func TestLookupNotFound(t *testing.T) {
	r := NewPrecompileRegistry()
	_, ok := r.Lookup(types.BytesToAddress([]byte{0xff}))
	if ok {
		t.Error("expected Lookup to return false for unregistered address")
	}
}

func TestIsPrecompile(t *testing.T) {
	r := NewPrecompileRegistry()
	if !r.IsPrecompile(types.BytesToAddress([]byte{0x01})) {
		t.Error("ecRecover should be a precompile")
	}
	if r.IsPrecompile(types.BytesToAddress([]byte{0xff})) {
		t.Error("0xff should not be a precompile")
	}
}

func TestActivePrecompiles(t *testing.T) {
	r := NewPrecompileRegistry()

	// Homestead should have ecRecover, sha256, ripemd160, identity.
	homestead := r.ActivePrecompiles("Homestead")
	if len(homestead) != 4 {
		t.Fatalf("expected 4 Homestead precompiles, got %d", len(homestead))
	}
	expectedNames := []string{"ecRecover", "sha256", "ripemd160", "identity"}
	for i, info := range homestead {
		if info.Name != expectedNames[i] {
			t.Errorf("Homestead[%d]: expected %q, got %q", i, expectedNames[i], info.Name)
		}
	}

	// Byzantium should have modexp, ecAdd, ecMul, ecPairing.
	byzantium := r.ActivePrecompiles("Byzantium")
	if len(byzantium) != 4 {
		t.Fatalf("expected 4 Byzantium precompiles, got %d", len(byzantium))
	}

	// Cancun should have pointEval only.
	cancun := r.ActivePrecompiles("Cancun")
	if len(cancun) != 1 || cancun[0].Name != "pointEval" {
		t.Errorf("expected 1 Cancun precompile (pointEval), got %v", cancun)
	}

	// Unknown fork should return empty.
	unknown := r.ActivePrecompiles("Unknown")
	if len(unknown) != 0 {
		t.Errorf("expected 0 precompiles for unknown fork, got %d", len(unknown))
	}
}

func TestActivePrecompilesSorted(t *testing.T) {
	r := NewPrecompileRegistry()
	all := r.AllPrecompiles()
	for i := 1; i < len(all); i++ {
		if !addressLess(all[i-1].Address, all[i].Address) {
			t.Errorf("AllPrecompiles not sorted: %s >= %s",
				all[i-1].Address.Hex(), all[i].Address.Hex())
		}
	}
}

func TestGasCost(t *testing.T) {
	r := NewPrecompileRegistry()

	// ecRecover: fixed 3000 gas.
	gas, err := r.GasCost(types.BytesToAddress([]byte{0x01}), nil)
	if err != nil {
		t.Fatalf("GasCost failed: %v", err)
	}
	if gas != 3000 {
		t.Errorf("ecRecover gas: expected 3000, got %d", gas)
	}

	// sha256: 60 + 12 * ceil(len/32).
	input := make([]byte, 64)
	gas, err = r.GasCost(types.BytesToAddress([]byte{0x02}), input)
	if err != nil {
		t.Fatalf("GasCost failed: %v", err)
	}
	expected := uint64(60 + 12*2)
	if gas != expected {
		t.Errorf("sha256 gas for 64-byte input: expected %d, got %d", expected, gas)
	}

	// identity: 15 + 3 * ceil(len/32).
	gas, err = r.GasCost(types.BytesToAddress([]byte{0x04}), make([]byte, 100))
	if err != nil {
		t.Fatalf("GasCost failed: %v", err)
	}
	// ceil(100/32) = 4
	expected = 15 + 3*4
	if gas != expected {
		t.Errorf("identity gas for 100-byte input: expected %d, got %d", expected, gas)
	}
}

func TestGasCostNotFound(t *testing.T) {
	r := NewPrecompileRegistry()
	_, err := r.GasCost(types.BytesToAddress([]byte{0xff}), nil)
	if err == nil {
		t.Error("expected error for unregistered address")
	}
}

func TestAllPrecompiles(t *testing.T) {
	r := NewPrecompileRegistry()
	all := r.AllPrecompiles()
	if len(all) != 10 {
		t.Fatalf("expected 10 precompiles, got %d", len(all))
	}

	// Should be sorted by address.
	for i := 1; i < len(all); i++ {
		if !addressLess(all[i-1].Address, all[i].Address) {
			t.Errorf("not sorted at index %d", i)
		}
	}

	// First should be ecRecover (0x01), last should be pointEval (0x0a).
	if all[0].Name != "ecRecover" {
		t.Errorf("first precompile: expected ecRecover, got %q", all[0].Name)
	}
	if all[9].Name != "pointEval" {
		t.Errorf("last precompile: expected pointEval, got %q", all[9].Name)
	}
}

func TestForkPrecompiles(t *testing.T) {
	r := NewPrecompileRegistry()
	forks := r.ForkPrecompiles()

	if len(forks) != 4 {
		t.Fatalf("expected 4 forks, got %d", len(forks))
	}
	if len(forks["Homestead"]) != 4 {
		t.Errorf("Homestead: expected 4 addresses, got %d", len(forks["Homestead"]))
	}
	if len(forks["Byzantium"]) != 4 {
		t.Errorf("Byzantium: expected 4 addresses, got %d", len(forks["Byzantium"]))
	}
	if len(forks["Istanbul"]) != 1 {
		t.Errorf("Istanbul: expected 1 address, got %d", len(forks["Istanbul"]))
	}
	if len(forks["Cancun"]) != 1 {
		t.Errorf("Cancun: expected 1 address, got %d", len(forks["Cancun"]))
	}

	// Verify Homestead addresses are sorted.
	addrs := forks["Homestead"]
	for i := 1; i < len(addrs); i++ {
		if !addressLess(addrs[i-1], addrs[i]) {
			t.Error("Homestead addresses not sorted")
		}
	}
}

func TestCount(t *testing.T) {
	r := NewPrecompileRegistry()
	if r.Count() != 10 {
		t.Errorf("expected 10, got %d", r.Count())
	}

	_ = r.Register(PrecompileInfo{
		Address:        types.BytesToAddress([]byte{0x30}),
		Name:           "extra",
		GasCost:        func([]byte) uint64 { return 1 },
		ActivationFork: "Future",
	})
	if r.Count() != 11 {
		t.Errorf("expected 11, got %d", r.Count())
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	r := NewPrecompileRegistry()
	var wg sync.WaitGroup

	// Concurrent reads while registering new precompiles.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r.IsPrecompile(types.BytesToAddress([]byte{0x01}))
			r.AllPrecompiles()
			r.GasCost(types.BytesToAddress([]byte{0x02}), []byte{1, 2, 3})
			r.ActivePrecompiles("Homestead")
			r.ForkPrecompiles()
			r.Count()
		}(i)
	}

	// Concurrent writes.
	for i := byte(0x40); i < 0x60; i++ {
		wg.Add(1)
		go func(b byte) {
			defer wg.Done()
			_ = r.Register(PrecompileInfo{
				Address:        types.BytesToAddress([]byte{b}),
				Name:           "concurrent",
				GasCost:        func([]byte) uint64 { return 1 },
				ActivationFork: "Test",
			})
		}(i)
	}

	wg.Wait()

	// All 32 concurrent precompiles plus 10 defaults.
	if r.Count() != 10+32 {
		t.Errorf("expected 42 precompiles after concurrent ops, got %d", r.Count())
	}
}

func TestLookupReturnsCopy(t *testing.T) {
	r := NewPrecompileRegistry()
	info, ok := r.Lookup(types.BytesToAddress([]byte{0x01}))
	if !ok {
		t.Fatal("Lookup returned false")
	}

	// Mutating the returned copy should not affect the registry.
	info.Name = "MUTATED"

	info2, _ := r.Lookup(types.BytesToAddress([]byte{0x01}))
	if info2.Name == "MUTATED" {
		t.Error("Lookup returned a reference instead of a copy")
	}
}

func TestGasCostNilFunc(t *testing.T) {
	r := NewPrecompileRegistry()
	_ = r.Register(PrecompileInfo{
		Address:        types.BytesToAddress([]byte{0x50}),
		Name:           "nogas",
		GasCost:        nil,
		ActivationFork: "Test",
	})

	gas, err := r.GasCost(types.BytesToAddress([]byte{0x50}), nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gas != 0 {
		t.Errorf("expected 0 gas for nil GasCost, got %d", gas)
	}
}

func TestAddressLess(t *testing.T) {
	a := types.BytesToAddress([]byte{0x01})
	b := types.BytesToAddress([]byte{0x02})
	if !addressLess(a, b) {
		t.Error("0x01 should be less than 0x02")
	}
	if addressLess(b, a) {
		t.Error("0x02 should not be less than 0x01")
	}
	if addressLess(a, a) {
		t.Error("equal addresses should not be less")
	}
}

func TestDefaultPrecompileNames(t *testing.T) {
	r := NewPrecompileRegistry()
	expected := map[byte]string{
		0x01: "ecRecover",
		0x02: "sha256",
		0x03: "ripemd160",
		0x04: "identity",
		0x05: "modexp",
		0x06: "ecAdd",
		0x07: "ecMul",
		0x08: "ecPairing",
		0x09: "blake2f",
		0x0a: "pointEval",
	}
	for addr, name := range expected {
		info, ok := r.Lookup(types.BytesToAddress([]byte{addr}))
		if !ok {
			t.Errorf("address 0x%02x not found", addr)
			continue
		}
		if info.Name != name {
			t.Errorf("address 0x%02x: expected name %q, got %q", addr, name, info.Name)
		}
	}
}

func TestEcPairingGasCostScales(t *testing.T) {
	r := NewPrecompileRegistry()
	addr := types.BytesToAddress([]byte{0x08})

	// 0 pairs: 45000.
	gas0, _ := r.GasCost(addr, nil)
	if gas0 != 45000 {
		t.Errorf("0 pairs: expected 45000, got %d", gas0)
	}

	// 1 pair (192 bytes): 45000 + 34000.
	gas1, _ := r.GasCost(addr, make([]byte, 192))
	if gas1 != 79000 {
		t.Errorf("1 pair: expected 79000, got %d", gas1)
	}

	// 2 pairs (384 bytes): 45000 + 2*34000.
	gas2, _ := r.GasCost(addr, make([]byte, 384))
	if gas2 != 113000 {
		t.Errorf("2 pairs: expected 113000, got %d", gas2)
	}
}
