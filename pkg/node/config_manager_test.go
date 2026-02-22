package node

import (
	"strings"
	"testing"
)

// --- ConfigManager Tests ---

func TestNewConfigManager(t *testing.T) {
	cm := NewConfigManager()
	cfg := cm.Config()
	if cfg == nil {
		t.Fatal("Config() is nil")
	}
	if cfg.Network.ChainID != 1 {
		t.Errorf("ChainID = %d, want 1", cfg.Network.ChainID)
	}
	if cfg.Sync.Mode != "snap" {
		t.Errorf("Sync.Mode = %q, want snap", cfg.Sync.Mode)
	}
}

func TestConfigManagerSetDataDir(t *testing.T) {
	cm := NewConfigManager()
	cm.SetDataDir("/data/eth2030", SourceCLI)

	if cm.Config().DataDir != "/data/eth2030" {
		t.Errorf("DataDir = %q, want /data/eth2030", cm.Config().DataDir)
	}
	if cm.Source("datadir") != SourceCLI {
		t.Errorf("source = %v, want CLI", cm.Source("datadir"))
	}
}

func TestConfigManagerSetLogLevel(t *testing.T) {
	cm := NewConfigManager()
	cm.SetLogLevel("debug", SourceEnv)

	if cm.Config().LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cm.Config().LogLevel)
	}
	if cm.Source("loglevel") != SourceEnv {
		t.Errorf("source = %v, want Env", cm.Source("loglevel"))
	}
}

func TestConfigManagerSetNetworkConfig(t *testing.T) {
	cm := NewConfigManager()
	cm.SetNetworkConfig(NetworkConfig{
		ChainID:   11155111,
		NetworkID: 11155111,
		GenesisHash: "0x25a5cc106eea7138acab33231d7160d69cb777ee0c2c553fcddf5138993e6dd9",
	}, SourceFile)

	cfg := cm.Config()
	if cfg.Network.ChainID != 11155111 {
		t.Errorf("ChainID = %d, want 11155111", cfg.Network.ChainID)
	}
}

func TestConfigManagerSetSyncConfig(t *testing.T) {
	cm := NewConfigManager()
	cm.SetSyncConfig(SyncConfig{
		Mode:            "full",
		MaxPeers:        100,
		ConnectTimeout:  60,
		EnableDiscovery: true,
	}, SourceCLI)

	cfg := cm.Config()
	if cfg.Sync.Mode != "full" {
		t.Errorf("Sync.Mode = %q, want full", cfg.Sync.Mode)
	}
	if cfg.Sync.MaxPeers != 100 {
		t.Errorf("Sync.MaxPeers = %d, want 100", cfg.Sync.MaxPeers)
	}
}

func TestConfigManagerSetRPCConfig(t *testing.T) {
	cm := NewConfigManager()
	cm.SetRPCConfig(ManagedRPCConfig{
		Enabled:        true,
		Host:           "0.0.0.0",
		Port:           9545,
		AllowedModules: []string{"eth", "debug"},
		RateLimit:      100,
	}, SourceFile)

	cfg := cm.Config()
	if cfg.RPC.Port != 9545 {
		t.Errorf("RPC.Port = %d, want 9545", cfg.RPC.Port)
	}
	if cfg.RPC.RateLimit != 100 {
		t.Errorf("RPC.RateLimit = %d, want 100", cfg.RPC.RateLimit)
	}
}

func TestConfigManagerSetEngineConfig(t *testing.T) {
	cm := NewConfigManager()
	cm.SetEngineConfig(EngineConfig{
		Enabled:               true,
		Host:                  "127.0.0.1",
		Port:                  8551,
		JWTSecret:             "0xdeadbeef",
		PayloadBuilderEnabled: true,
	}, SourceCLI)

	cfg := cm.Config()
	if cfg.Engine.JWTSecret != "0xdeadbeef" {
		t.Errorf("JWTSecret = %q", cfg.Engine.JWTSecret)
	}
	if !cfg.Engine.PayloadBuilderEnabled {
		t.Error("PayloadBuilderEnabled should be true")
	}
}

func TestConfigManagerSourceDefault(t *testing.T) {
	cm := NewConfigManager()
	if cm.Source("unset_field") != SourceDefault {
		t.Errorf("unset field should have source Default")
	}
}

func TestConfigSourceString(t *testing.T) {
	tests := []struct {
		src  ConfigSource
		want string
	}{
		{SourceDefault, "default"},
		{SourceFile, "file"},
		{SourceEnv, "env"},
		{SourceCLI, "cli"},
		{ConfigSource(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.src.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.src, got, tt.want)
		}
	}
}

// --- ConfigValidator Tests ---

func TestConfigValidatorDefaultConfig(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	// Set JWT secret so engine validation passes.
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	if len(errs) != 0 {
		t.Fatalf("default config should validate, got %v", errs)
	}
}

func TestConfigValidatorInvalidChainID(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Network.ChainID = 0
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	hasChainErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "chain ID") {
			hasChainErr = true
		}
	}
	if !hasChainErr {
		t.Error("should report invalid chain ID")
	}
}

func TestConfigValidatorInvalidSyncMode(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Sync.Mode = "turbo"
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	hasSyncErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "sync") {
			hasSyncErr = true
		}
	}
	if !hasSyncErr {
		t.Error("should report invalid sync mode")
	}
}

func TestConfigValidatorInvalidRPCPort(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.RPC.Port = -1
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	hasPortErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "port") {
			hasPortErr = true
		}
	}
	if !hasPortErr {
		t.Error("should report invalid RPC port")
	}
}

func TestConfigValidatorInvalidEnginePort(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Engine.Port = 70000
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	hasPortErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "port") {
			hasPortErr = true
		}
	}
	if !hasPortErr {
		t.Error("should report invalid engine port")
	}
}

func TestConfigValidatorSnapSyncNeedsDiscovery(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Sync.Mode = "snap"
	cfg.Sync.EnableDiscovery = false
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	hasConflict := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "snap sync requires discovery") {
			hasConflict = true
		}
	}
	if !hasConflict {
		t.Error("should detect snap sync + no discovery conflict")
	}
}

func TestConfigValidatorEngineNeedsJWT(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Engine.Enabled = true
	cfg.Engine.JWTSecret = ""

	errs := cv.Validate(cfg)
	hasJWTErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "JWT") {
			hasJWTErr = true
		}
	}
	if !hasJWTErr {
		t.Error("should detect missing JWT secret")
	}
}

func TestConfigValidatorInvalidLogLevel(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.LogLevel = "verbose"
	cfg.Engine.JWTSecret = "0xtest"

	errs := cv.Validate(cfg)
	hasLogErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "log level") {
			hasLogErr = true
		}
	}
	if !hasLogErr {
		t.Error("should detect invalid log level")
	}
}

func TestConfigValidatorForkOrder(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Engine.JWTSecret = "0xtest"
	cfg.Network.ForkSchedule = map[string]uint64{
		"london":  12965000,
		"merge":   15537393,
		"cancun":  10000000, // before merge: invalid
	}

	errs := cv.Validate(cfg)
	hasForkErr := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "fork") {
			hasForkErr = true
		}
	}
	if !hasForkErr {
		t.Error("should detect fork ordering error")
	}
}

func TestConfigValidatorValidForkOrder(t *testing.T) {
	cv := NewConfigValidator()
	cfg := DefaultManagedConfig()
	cfg.Engine.JWTSecret = "0xtest"
	cfg.Network.ForkSchedule = map[string]uint64{
		"london":  12965000,
		"merge":   15537393,
		"cancun":  19426587,
	}

	errs := cv.Validate(cfg)
	if len(errs) != 0 {
		t.Errorf("valid fork order should pass: %v", errs)
	}
}

// --- ConfigMerge Tests ---

func TestConfigMergeEmpty(t *testing.T) {
	result := ConfigMerge()
	if result.Network.ChainID != 1 {
		t.Errorf("ChainID = %d, want 1 (default)", result.Network.ChainID)
	}
}

func TestConfigMergeNil(t *testing.T) {
	result := ConfigMerge(nil, nil)
	if result.Sync.Mode != "snap" {
		t.Errorf("Mode = %q, want snap (default)", result.Sync.Mode)
	}
}

func TestConfigMergeSingle(t *testing.T) {
	override := &ManagedConfig{
		DataDir:  "/override",
		LogLevel: "debug",
	}
	result := ConfigMerge(override)
	if result.DataDir != "/override" {
		t.Errorf("DataDir = %q, want /override", result.DataDir)
	}
	if result.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", result.LogLevel)
	}
}

func TestConfigMergeMultiple(t *testing.T) {
	file := &ManagedConfig{
		Network: NetworkConfig{ChainID: 5},
		Sync:    SyncConfig{Mode: "full"},
	}
	cli := &ManagedConfig{
		DataDir:  "/cli/path",
		LogLevel: "error",
	}

	result := ConfigMerge(file, cli)
	if result.Network.ChainID != 5 {
		t.Errorf("ChainID = %d, want 5 (from file)", result.Network.ChainID)
	}
	if result.Sync.Mode != "full" {
		t.Errorf("Mode = %q, want full (from file)", result.Sync.Mode)
	}
	if result.DataDir != "/cli/path" {
		t.Errorf("DataDir = %q, want /cli/path (from cli)", result.DataDir)
	}
	if result.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want error (from cli)", result.LogLevel)
	}
}

func TestConfigMergePreservesDefaults(t *testing.T) {
	override := &ManagedConfig{
		DataDir: "/data",
	}
	result := ConfigMerge(override)

	// Fields not in override should be defaults.
	if result.RPC.Port != 8545 {
		t.Errorf("RPC.Port = %d, want 8545 (default)", result.RPC.Port)
	}
	if result.Engine.Port != 8551 {
		t.Errorf("Engine.Port = %d, want 8551 (default)", result.Engine.Port)
	}
}

func TestConfigMergeLaterOverridesEarlier(t *testing.T) {
	first := &ManagedConfig{DataDir: "/first"}
	second := &ManagedConfig{DataDir: "/second"}

	result := ConfigMerge(first, second)
	if result.DataDir != "/second" {
		t.Errorf("DataDir = %q, want /second", result.DataDir)
	}
}

// --- Fork Schedule Tests ---

func TestForkScheduleIsActive(t *testing.T) {
	fs := NewForkSchedule(map[string]uint64{
		"london": 12965000,
		"merge":  15537393,
	})

	if fs.IsActive("london", 12964999) {
		t.Error("london should not be active before block 12965000")
	}
	if !fs.IsActive("london", 12965000) {
		t.Error("london should be active at block 12965000")
	}
	if !fs.IsActive("london", 13000000) {
		t.Error("london should be active after block 12965000")
	}
	if fs.IsActive("unknown", 99999999) {
		t.Error("unknown fork should not be active")
	}
}

func TestForkScheduleActivationBlock(t *testing.T) {
	fs := NewForkSchedule(map[string]uint64{
		"london": 12965000,
	})

	block, ok := fs.ActivationBlock("london")
	if !ok || block != 12965000 {
		t.Errorf("london activation = %d, ok=%v", block, ok)
	}

	_, ok = fs.ActivationBlock("unknown")
	if ok {
		t.Error("unknown fork should not have activation block")
	}
}

func TestForkScheduleActiveForks(t *testing.T) {
	fs := NewForkSchedule(map[string]uint64{
		"london":  12965000,
		"merge":   15537393,
		"cancun":  19426587,
	})

	active := fs.ActiveForks(15600000)
	if len(active) != 2 {
		t.Errorf("active forks = %d, want 2", len(active))
	}

	// Check london and merge are active.
	hasLondon, hasMerge := false, false
	for _, f := range active {
		if f == "london" {
			hasLondon = true
		}
		if f == "merge" {
			hasMerge = true
		}
	}
	if !hasLondon || !hasMerge {
		t.Errorf("expected london and merge, got %v", active)
	}
}

func TestForkScheduleCount(t *testing.T) {
	fs := NewForkSchedule(map[string]uint64{
		"london":  12965000,
		"merge":   15537393,
	})
	if fs.ForkCount() != 2 {
		t.Errorf("ForkCount() = %d, want 2", fs.ForkCount())
	}
}

func TestFormatForkScheduleEmpty(t *testing.T) {
	result := FormatForkSchedule(map[string]uint64{})
	if result != "(empty)" {
		t.Errorf("FormatForkSchedule({}) = %q, want (empty)", result)
	}
}

func TestFormatForkScheduleNonEmpty(t *testing.T) {
	result := FormatForkSchedule(map[string]uint64{"london": 12965000})
	if !strings.Contains(result, "london@12965000") {
		t.Errorf("FormatForkSchedule should contain london@12965000, got %q", result)
	}
}
