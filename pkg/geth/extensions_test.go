package geth

import (
	"math/big"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/vm"
)

// pragueRules returns a params.Rules with all forks through Prague enabled.
func pragueRules() params.Rules {
	zero := big.NewInt(0)
	ts := uint64(0)
	cfg := &params.ChainConfig{
		ChainID:             big.NewInt(1),
		HomesteadBlock:      zero,
		EIP150Block:         zero,
		EIP155Block:         zero,
		EIP158Block:         zero,
		ByzantiumBlock:      zero,
		ConstantinopleBlock: zero,
		PetersburgBlock:     zero,
		IstanbulBlock:       zero,
		BerlinBlock:         zero,
		LondonBlock:         zero,
		ShanghaiTime:        &ts,
		CancunTime:          &ts,
		PragueTime:          &ts,
	}
	return cfg.Rules(zero, true, 0)
}

func TestPrecompileAdapterInterface(t *testing.T) {
	// Verify PrecompileAdapter satisfies go-ethereum's PrecompiledContract.
	inner := &vm.NTTPrecompileAdapter{}
	adapter := NewPrecompileAdapter(inner, "ntt")

	// Check interface compliance at compile time.
	var _ gethvm.PrecompiledContract = adapter

	if adapter.Name() != "ntt" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "ntt")
	}

	// RequiredGas with empty input should return base cost.
	gas := adapter.RequiredGas(nil)
	if gas != 0 {
		t.Errorf("RequiredGas(nil) = %d, want 0", gas)
	}
}

func TestInjectCustomPrecompilesPrague(t *testing.T) {
	rules := pragueRules()
	precompiles := InjectCustomPrecompiles(rules, ForkLevelPrague)

	// Should have go-ethereum's standard precompiles but no custom ones.
	standardCount := len(gethvm.ActivePrecompiledContracts(rules))
	if len(precompiles) != standardCount {
		t.Errorf("Prague: got %d precompiles, want %d (standard only)", len(precompiles), standardCount)
	}

	// NTT should NOT be present.
	nttAddr := gethcommon.BytesToAddress([]byte{0x15})
	if _, ok := precompiles[nttAddr]; ok {
		t.Error("NTT precompile should not be active at Prague level")
	}
}

func TestInjectCustomPrecompilesGlamsterdam(t *testing.T) {
	rules := pragueRules()
	precompiles := InjectCustomPrecompiles(rules, ForkLevelGlamsterdam)

	// Glamsterdam should replace precompiles at 0x06, 0x08, 0x09, 0x0a.
	ecAddAddr := gethcommon.BytesToAddress([]byte{0x06})
	p, ok := precompiles[ecAddAddr]
	if !ok {
		t.Fatal("ecAdd precompile missing")
	}
	if p.Name() != "ecAddGlamsterdam" {
		t.Errorf("ecAdd name = %q, want %q", p.Name(), "ecAddGlamsterdam")
	}

	// Check repriced gas: Glamsterdam ecAdd is 314 gas.
	gas := p.RequiredGas(make([]byte, 128))
	if gas != vm.GasECADDGlamsterdan {
		t.Errorf("ecAdd gas = %d, want %d", gas, vm.GasECADDGlamsterdan)
	}
}

func TestInjectCustomPrecompilesIPlus(t *testing.T) {
	rules := pragueRules()
	precompiles := InjectCustomPrecompiles(rules, ForkLevelIPlus)

	// NTT at 0x15 should be present.
	nttAddr := gethcommon.BytesToAddress([]byte{0x15})
	p, ok := precompiles[nttAddr]
	if !ok {
		t.Fatal("NTT precompile missing at I+ fork")
	}
	if p.Name() != "ntt" {
		t.Errorf("NTT name = %q, want %q", p.Name(), "ntt")
	}

	// NII precompiles should be present.
	niiAddrs := []struct {
		addr gethcommon.Address
		name string
	}{
		{gethcommon.BytesToAddress([]byte{0x02, 0x01}), "niiModExp"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x02}), "niiFieldMul"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x03}), "niiFieldInv"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x04}), "niiBatchVerify"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x05}), "fieldMulExt"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x06}), "fieldInvExt"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x07}), "fieldExp"},
		{gethcommon.BytesToAddress([]byte{0x02, 0x08}), "batchFieldVerify"},
	}
	for _, tt := range niiAddrs {
		p, ok := precompiles[tt.addr]
		if !ok {
			t.Errorf("precompile %s missing at I+ fork", tt.name)
			continue
		}
		if p.Name() != tt.name {
			t.Errorf("precompile at %v: name = %q, want %q", tt.addr, p.Name(), tt.name)
		}
	}

	// Glamsterdam repriced precompiles should also be present (cumulative).
	ecAddAddr := gethcommon.BytesToAddress([]byte{0x06})
	if _, ok := precompiles[ecAddAddr]; !ok {
		t.Error("Glamsterdam repriced ecAdd missing at I+ fork")
	}
}

func TestEth2028ForkLevelFromConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *core.ChainConfig
		time   uint64
		want   Eth2028ForkLevel
	}{
		{
			name:   "nil config",
			config: nil,
			time:   1000,
			want:   ForkLevelPrague,
		},
		{
			name: "pre-glamsterdam",
			config: &core.ChainConfig{
				ChainID: big.NewInt(1),
			},
			time: 1000,
			want: ForkLevelPrague,
		},
		{
			name: "glamsterdam active",
			config: func() *core.ChainConfig {
				ts := uint64(500)
				return &core.ChainConfig{
					ChainID:         big.NewInt(1),
					GlamsterdanTime: &ts,
				}
			}(),
			time: 1000,
			want: ForkLevelGlamsterdam,
		},
		{
			name: "hogota active",
			config: func() *core.ChainConfig {
				ts := uint64(500)
				ts2 := uint64(800)
				return &core.ChainConfig{
					ChainID:         big.NewInt(1),
					GlamsterdanTime: &ts,
					HogotaTime:      &ts2,
				}
			}(),
			time: 1000,
			want: ForkLevelHogota,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Eth2028ForkLevelFromConfig(tt.config, tt.time)
			if got != tt.want {
				t.Errorf("Eth2028ForkLevelFromConfig() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCustomPrecompileAddresses(t *testing.T) {
	// Prague: no custom precompile addresses.
	addrs := CustomPrecompileAddresses(ForkLevelPrague)
	if len(addrs) != 0 {
		t.Errorf("Prague: got %d custom addresses, want 0", len(addrs))
	}

	// Glamsterdam: 4 repriced precompiles.
	addrs = CustomPrecompileAddresses(ForkLevelGlamsterdam)
	if len(addrs) != 4 {
		t.Errorf("Glamsterdam: got %d custom addresses, want 4", len(addrs))
	}

	// I+: 4 repriced + 1 NTT + 8 NII/field = 13.
	addrs = CustomPrecompileAddresses(ForkLevelIPlus)
	if len(addrs) != 13 {
		t.Errorf("I+: got %d custom addresses, want 13", len(addrs))
	}
}

func TestCustomPrecompileCount(t *testing.T) {
	if got := CustomPrecompileCount(ForkLevelPrague); got != 0 {
		t.Errorf("Prague: count = %d, want 0", got)
	}
	if got := CustomPrecompileCount(ForkLevelGlamsterdam); got != 4 {
		t.Errorf("Glamsterdam: count = %d, want 4", got)
	}
	if got := CustomPrecompileCount(ForkLevelIPlus); got != 13 {
		t.Errorf("I+: count = %d, want 13", got)
	}
}

func TestPrecompileNamesFunc(t *testing.T) {
	names := PrecompileNames(ForkLevelGlamsterdam)
	if len(names) != 4 {
		t.Errorf("Glamsterdam: got %d names, want 4", len(names))
	}

	ecAddAddr := gethcommon.BytesToAddress([]byte{0x06})
	if name, ok := names[ecAddAddr]; !ok || name != "ecAddGlamsterdam" {
		t.Errorf("ecAdd name = %q, want %q", name, "ecAddGlamsterdam")
	}
}

func TestListCustomPrecompiles(t *testing.T) {
	all := ListCustomPrecompiles()
	if len(all) != 13 {
		t.Errorf("total custom precompiles = %d, want 13", len(all))
	}

	// Verify categories.
	categories := make(map[string]int)
	for _, p := range all {
		categories[p.Category]++
	}
	if categories["repricing"] != 4 {
		t.Errorf("repricing count = %d, want 4", categories["repricing"])
	}
	if categories["ntt"] != 1 {
		t.Errorf("ntt count = %d, want 1", categories["ntt"])
	}
	if categories["nii"] != 4 {
		t.Errorf("nii count = %d, want 4", categories["nii"])
	}
	if categories["field"] != 4 {
		t.Errorf("field count = %d, want 4", categories["field"])
	}
}

func TestToGethChainConfigWithEth2028Forks(t *testing.T) {
	ts := uint64(1000)
	c := &core.ChainConfig{
		ChainID:         big.NewInt(1),
		GlamsterdanTime: &ts,
	}

	gc := ToGethChainConfigWithEth2028Forks(c)
	if gc == nil {
		t.Fatal("got nil config")
	}

	// Prague should be set since Glamsterdam extends it.
	if gc.PragueTime == nil {
		t.Error("PragueTime should be set when Glamsterdam is active")
	}
	if *gc.PragueTime != ts {
		t.Errorf("PragueTime = %d, want %d", *gc.PragueTime, ts)
	}
}

func TestToGethChainConfigWithEth2028ForksNil(t *testing.T) {
	gc := ToGethChainConfigWithEth2028Forks(nil)
	if gc != nil {
		t.Error("expected nil for nil input")
	}
}

func TestOpcodeExtensionNote(t *testing.T) {
	if OpcodeExtensionNote == "" {
		t.Error("OpcodeExtensionNote should not be empty")
	}
}
