package core

import (
	"math/big"
	"testing"
)

func TestForkOrder(t *testing.T) {
	if len(ForkOrder) == 0 {
		t.Fatal("ForkOrder is empty")
	}
	// Verify known forks are present.
	expected := []string{
		"Homestead", "Byzantium", "London", "Paris",
		"Shanghai", "Cancun", "Prague", "Amsterdam",
		"Glamsterdan", "Hogota",
	}
	forkSet := make(map[string]bool)
	for _, f := range ForkOrder {
		forkSet[f] = true
	}
	for _, name := range expected {
		if !forkSet[name] {
			t.Errorf("ForkOrder missing %s", name)
		}
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := DevConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DevConfig should be valid: %v", err)
	}
}

func TestValidate_NilChainID(t *testing.T) {
	cfg := DevConfig()
	cfg.ChainID = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for nil ChainID")
	}
}

func TestValidate_ZeroChainID(t *testing.T) {
	cfg := DevConfig()
	cfg.ChainID = big.NewInt(0)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero ChainID")
	}
}

func TestValidate_NegativeChainID(t *testing.T) {
	cfg := DevConfig()
	cfg.ChainID = big.NewInt(-1)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative ChainID")
	}
}

func TestValidate_BlockForkOrdering(t *testing.T) {
	cfg := DevConfig()
	// Berlin must not come before Istanbul.
	cfg.IstanbulBlock = big.NewInt(100)
	cfg.BerlinBlock = big.NewInt(50) // Before Istanbul
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for out-of-order block forks")
	}
}

func TestValidate_TimestampForkOrdering(t *testing.T) {
	cfg := TestnetConfig()
	// Hogota before Glamsterdan is invalid.
	glamsterdan := uint64(5000)
	hogota := uint64(3000)
	cfg.GlamsterdanTime = &glamsterdan
	cfg.HogotaTime = &hogota
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for out-of-order timestamp forks")
	}
}

func TestValidate_ShanghaiWithoutTTD(t *testing.T) {
	shanghaiTime := uint64(100)
	cfg := &ChainConfig{
		ChainID:                 big.NewInt(1),
		TerminalTotalDifficulty: nil,
		ShanghaiTime:            &shanghaiTime,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error: Shanghai requires TTD")
	}
}

func TestValidate_MainnetConfig(t *testing.T) {
	cfg := MainnetConfig
	if err := cfg.Validate(); err != nil {
		t.Fatalf("MainnetConfig should be valid: %v", err)
	}
}

func TestValidate_SepoliaConfig(t *testing.T) {
	cfg := SepoliaConfig
	if err := cfg.Validate(); err != nil {
		t.Fatalf("SepoliaConfig should be valid: %v", err)
	}
}

func TestValidate_HoleskyConfig(t *testing.T) {
	cfg := HoleskyConfig
	if err := cfg.Validate(); err != nil {
		t.Fatalf("HoleskyConfig should be valid: %v", err)
	}
}

func TestActiveFork_DevConfig(t *testing.T) {
	cfg := DevConfig()
	fork := cfg.ActiveFork(0)
	if fork != "Hogota" {
		t.Fatalf("expected Hogota at time 0, got %s", fork)
	}
}

func TestActiveFork_Progression(t *testing.T) {
	cfg := TestnetConfig()
	tests := []struct {
		time     uint64
		expected string
	}{
		{0, "Cancun"},       // Shanghai=0, Cancun=0, Prague=1000
		{500, "Cancun"},     // Before Prague
		{1000, "Prague"},    // Prague activated
		{1500, "Prague"},    // Still Prague
		{2000, "Amsterdam"}, // Amsterdam activated
		{3000, "Glamsterdan"},
		{4000, "Hogota"},
		{999999, "Hogota"},
	}

	for _, tt := range tests {
		got := cfg.ActiveFork(tt.time)
		if got != tt.expected {
			t.Errorf("ActiveFork(%d) = %s, want %s", tt.time, got, tt.expected)
		}
	}
}

func TestActiveFork_PreMerge(t *testing.T) {
	cfg := &ChainConfig{
		ChainID:     big.NewInt(1),
		LondonBlock: big.NewInt(0),
		// No TTD set = pre-merge.
	}
	fork := cfg.ActiveFork(0)
	if fork != "London" {
		t.Fatalf("expected London for pre-merge, got %s", fork)
	}
}

func TestActiveFork_MergeOnly(t *testing.T) {
	cfg := &ChainConfig{
		ChainID:                 big.NewInt(1),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		// No Shanghai time.
	}
	fork := cfg.ActiveFork(0)
	if fork != "Paris" {
		t.Fatalf("expected Paris for merge-only, got %s", fork)
	}
}

func TestGetRules(t *testing.T) {
	cfg := DevConfig()
	rules := cfg.GetRules(0, 0)
	if rules == nil {
		t.Fatal("GetRules returned nil")
	}
	if !rules.IsMerge {
		t.Fatal("expected IsMerge to be true")
	}
	if !rules.IsShanghai {
		t.Fatal("expected IsShanghai to be true")
	}
	if !rules.IsCancun {
		t.Fatal("expected IsCancun to be true")
	}
	if !rules.IsPrague {
		t.Fatal("expected IsPrague to be true")
	}
	if !rules.IsGlamsterdan {
		t.Fatal("expected IsGlamsterdan to be true")
	}
	if !rules.IsHogota {
		t.Fatal("expected IsHogota to be true")
	}
	if rules.ChainID.Cmp(big.NewInt(1337)) != 0 {
		t.Fatalf("expected chainID 1337, got %s", rules.ChainID)
	}
}

func TestGetRules_BeforeFork(t *testing.T) {
	cfg := TestnetConfig()
	// Time 500: Prague is at 1000, so it should be inactive.
	rules := cfg.GetRules(0, 500)
	if rules.IsPrague {
		t.Fatal("IsPrague should be false before Prague time")
	}
	if rules.IsAmsterdam {
		t.Fatal("IsAmsterdam should be false before Amsterdam time")
	}
	if !rules.IsCancun {
		t.Fatal("IsCancun should be true (time 0)")
	}
}

func TestMainnetConfigFunc(t *testing.T) {
	cfg := MainnetConfigFunc()
	if cfg == nil {
		t.Fatal("MainnetConfigFunc returned nil")
	}
	if cfg.ChainID.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected chainID 1, got %s", cfg.ChainID)
	}
	// Verify it's a copy.
	cfg.ChainID = big.NewInt(999)
	if MainnetConfig.ChainID.Cmp(big.NewInt(1)) != 0 {
		t.Fatal("MainnetConfig was mutated through copy")
	}
}

func TestTestnetConfig(t *testing.T) {
	cfg := TestnetConfig()
	if cfg == nil {
		t.Fatal("TestnetConfig returned nil")
	}
	if cfg.ChainID.Cmp(big.NewInt(11155111)) != 0 {
		t.Fatalf("expected chainID 11155111, got %s", cfg.ChainID)
	}
	if cfg.PragueTime == nil {
		t.Fatal("PragueTime should be set")
	}
	if cfg.GlamsterdanTime == nil {
		t.Fatal("GlamsterdanTime should be set")
	}
	if cfg.HogotaTime == nil {
		t.Fatal("HogotaTime should be set")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("TestnetConfig should be valid: %v", err)
	}
}

func TestDevConfig(t *testing.T) {
	cfg := DevConfig()
	if cfg == nil {
		t.Fatal("DevConfig returned nil")
	}
	if cfg.ChainID.Cmp(big.NewInt(1337)) != 0 {
		t.Fatalf("expected chainID 1337, got %s", cfg.ChainID)
	}

	// All forks should be active at time 0.
	if !cfg.IsShanghai(0) {
		t.Fatal("Shanghai should be active at time 0")
	}
	if !cfg.IsCancun(0) {
		t.Fatal("Cancun should be active at time 0")
	}
	if !cfg.IsPrague(0) {
		t.Fatal("Prague should be active at time 0")
	}
	if !cfg.IsAmsterdam(0) {
		t.Fatal("Amsterdam should be active at time 0")
	}
	if !cfg.IsGlamsterdan(0) {
		t.Fatal("Glamsterdan should be active at time 0")
	}
	if !cfg.IsHogota(0) {
		t.Fatal("Hogota should be active at time 0")
	}
	if !cfg.IsBPO1(0) {
		t.Fatal("BPO1 should be active at time 0")
	}
	if !cfg.IsBPO2(0) {
		t.Fatal("BPO2 should be active at time 0")
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("DevConfig should be valid: %v", err)
	}
}

func TestValidate_NegativeBlockFork(t *testing.T) {
	cfg := DevConfig()
	cfg.HomesteadBlock = big.NewInt(-1)
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for negative fork block")
	}
}

func TestValidate_SkippedTimestampForks(t *testing.T) {
	// It's valid to skip forks (leave them nil).
	shanghaiTime := uint64(100)
	glamsterdanTime := uint64(200)
	cfg := &ChainConfig{
		ChainID:                 big.NewInt(1),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            &shanghaiTime,
		// CancunTime, PragueTime, AmsterdamTime all nil (skipped)
		GlamsterdanTime: &glamsterdanTime,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("skipped forks should be valid: %v", err)
	}
}

func TestActiveFork_ShanghaiOnly(t *testing.T) {
	shanghaiTime := uint64(100)
	cfg := &ChainConfig{
		ChainID:                 big.NewInt(1),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            &shanghaiTime,
	}
	if cfg.ActiveFork(99) != "Paris" {
		t.Fatalf("expected Paris before Shanghai, got %s", cfg.ActiveFork(99))
	}
	if cfg.ActiveFork(100) != "Shanghai" {
		t.Fatalf("expected Shanghai at activation, got %s", cfg.ActiveFork(100))
	}
}

func TestGetRules_AllEIPs(t *testing.T) {
	cfg := DevConfig()
	rules := cfg.GetRules(0, 0)

	// Verify all EIP-specific flags.
	checks := []struct {
		name   string
		active bool
	}{
		{"IsHomestead", rules.IsHomestead},
		{"IsEIP150", rules.IsEIP150},
		{"IsEIP155", rules.IsEIP155},
		{"IsEIP158", rules.IsEIP158},
		{"IsByzantium", rules.IsByzantium},
		{"IsConstantinople", rules.IsConstantinople},
		{"IsPetersburg", rules.IsPetersburg},
		{"IsIstanbul", rules.IsIstanbul},
		{"IsBerlin", rules.IsBerlin},
		{"IsEIP2929", rules.IsEIP2929},
		{"IsLondon", rules.IsLondon},
		{"IsEIP1559", rules.IsEIP1559},
		{"IsEIP3529", rules.IsEIP3529},
		{"IsShanghai", rules.IsShanghai},
		{"IsCancun", rules.IsCancun},
		{"IsEIP4844", rules.IsEIP4844},
		{"IsPrague", rules.IsPrague},
		{"IsEIP7702", rules.IsEIP7702},
		{"IsAmsterdam", rules.IsAmsterdam},
		{"IsGlamsterdan", rules.IsGlamsterdan},
		{"IsHogota", rules.IsHogota},
		{"IsEIP7999", rules.IsEIP7999},
	}

	for _, check := range checks {
		if !check.active {
			t.Errorf("expected %s to be true in DevConfig at time 0", check.name)
		}
	}
}

func TestValidate_TestConfigVariants(t *testing.T) {
	configs := []*ChainConfig{
		TestConfig,
		TestConfigGlamsterdan,
		TestConfigHogota,
		TestConfigBPO2,
	}
	for i, cfg := range configs {
		if err := cfg.Validate(); err != nil {
			t.Errorf("TestConfig variant %d should be valid: %v", i, err)
		}
	}
}

func TestMainnetConfigFunc_TTDCopy(t *testing.T) {
	cfg := MainnetConfigFunc()
	if cfg.TerminalTotalDifficulty == nil {
		t.Fatal("TTD should be set")
	}
	// Mutate the copy.
	cfg.TerminalTotalDifficulty = big.NewInt(0)
	// Original must be unchanged.
	if MainnetConfig.TerminalTotalDifficulty.Cmp(MainnetTerminalTotalDifficulty) != 0 {
		t.Fatal("MainnetConfig TTD was mutated through copy")
	}
}
