package node

import (
	"strings"
	"testing"
)

func TestDefaultNodeConfig(t *testing.T) {
	cfg := DefaultNodeConfig()

	if cfg.NetworkID != 1 {
		t.Errorf("NetworkID = %d, want 1", cfg.NetworkID)
	}
	if cfg.SyncMode != "snap" {
		t.Errorf("SyncMode = %q, want snap", cfg.SyncMode)
	}
	if cfg.P2P.Port != 30303 {
		t.Errorf("P2P.Port = %d, want 30303", cfg.P2P.Port)
	}
	if cfg.P2P.MaxPeers != 50 {
		t.Errorf("P2P.MaxPeers = %d, want 50", cfg.P2P.MaxPeers)
	}
	if !cfg.RPC.Enabled {
		t.Error("RPC.Enabled should be true by default")
	}
	if cfg.RPC.Host != "127.0.0.1" {
		t.Errorf("RPC.Host = %q, want 127.0.0.1", cfg.RPC.Host)
	}
	if cfg.RPC.Port != 8545 {
		t.Errorf("RPC.Port = %d, want 8545", cfg.RPC.Port)
	}
	if len(cfg.RPC.APIs) != 3 {
		t.Errorf("RPC.APIs len = %d, want 3", len(cfg.RPC.APIs))
	}
	if cfg.Mining.Enabled {
		t.Error("Mining.Enabled should be false by default")
	}
	if cfg.Mining.GasLimit != 30_000_000 {
		t.Errorf("Mining.GasLimit = %d, want 30000000", cfg.Mining.GasLimit)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want info", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format = %q, want text", cfg.Log.Format)
	}
}

func TestDefaultNodeConfigValidates(t *testing.T) {
	cfg := DefaultNodeConfig()
	if err := cfg.ValidateNodeConfig(); err != nil {
		t.Errorf("default config should validate: %v", err)
	}
}

func TestLoadConfigFull(t *testing.T) {
	input := `
# Top-level settings
datadir = "/data/eth2028"
network_id = 11155111
sync_mode = "full"

[p2p]
port = 30304
max_peers = 100
bootstrap_nodes = ["enode://abc@1.2.3.4:30303", "enode://def@5.6.7.8:30303"]

[rpc]
enabled = true
host = "0.0.0.0"
port = 8546
apis = ["eth", "net", "web3", "debug"]

[mining]
enabled = true
coinbase = "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
gas_limit = 15000000

[log]
level = "debug"
format = "json"
`
	cfg, err := LoadConfig([]byte(input))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.DataDir != "/data/eth2028" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.NetworkID != 11155111 {
		t.Errorf("NetworkID = %d", cfg.NetworkID)
	}
	if cfg.SyncMode != "full" {
		t.Errorf("SyncMode = %q", cfg.SyncMode)
	}
	if cfg.P2P.Port != 30304 {
		t.Errorf("P2P.Port = %d", cfg.P2P.Port)
	}
	if cfg.P2P.MaxPeers != 100 {
		t.Errorf("P2P.MaxPeers = %d", cfg.P2P.MaxPeers)
	}
	if len(cfg.P2P.BootstrapNodes) != 2 {
		t.Fatalf("P2P.BootstrapNodes len = %d, want 2", len(cfg.P2P.BootstrapNodes))
	}
	if cfg.P2P.BootstrapNodes[0] != "enode://abc@1.2.3.4:30303" {
		t.Errorf("BootstrapNodes[0] = %q", cfg.P2P.BootstrapNodes[0])
	}
	if !cfg.RPC.Enabled {
		t.Error("RPC.Enabled should be true")
	}
	if cfg.RPC.Host != "0.0.0.0" {
		t.Errorf("RPC.Host = %q", cfg.RPC.Host)
	}
	if cfg.RPC.Port != 8546 {
		t.Errorf("RPC.Port = %d", cfg.RPC.Port)
	}
	if len(cfg.RPC.APIs) != 4 {
		t.Fatalf("RPC.APIs len = %d, want 4", len(cfg.RPC.APIs))
	}
	if cfg.Mining.Enabled != true {
		t.Error("Mining.Enabled should be true")
	}
	if cfg.Mining.Coinbase != "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf("Mining.Coinbase = %q", cfg.Mining.Coinbase)
	}
	if cfg.Mining.GasLimit != 15000000 {
		t.Errorf("Mining.GasLimit = %d", cfg.Mining.GasLimit)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q", cfg.Log.Format)
	}
}

func TestLoadConfigEmpty(t *testing.T) {
	cfg, err := LoadConfig([]byte(""))
	if err != nil {
		t.Fatalf("LoadConfig on empty input should not error: %v", err)
	}
	// Should return defaults.
	if cfg.NetworkID != 1 {
		t.Errorf("NetworkID = %d, want 1 (default)", cfg.NetworkID)
	}
}

func TestLoadConfigComments(t *testing.T) {
	input := `# This is a comment
# Another comment
datadir = "/tmp/test"
# network_id = 999
`
	cfg, err := LoadConfig([]byte(input))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.DataDir != "/tmp/test" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	// Commented-out network_id should not be applied.
	if cfg.NetworkID != 1 {
		t.Errorf("NetworkID = %d, want 1 (default, commented line ignored)", cfg.NetworkID)
	}
}

func TestLoadConfigInvalidSection(t *testing.T) {
	input := `[unknown_section]
foo = "bar"
`
	_, err := LoadConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error for unknown section")
	}
	if !strings.Contains(err.Error(), "unknown section") {
		t.Errorf("error should mention unknown section, got: %v", err)
	}
}

func TestLoadConfigUnclosedSection(t *testing.T) {
	input := `[p2p
port = 30303
`
	_, err := LoadConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error for unclosed section header")
	}
	if !strings.Contains(err.Error(), "unclosed") {
		t.Errorf("error should mention unclosed, got: %v", err)
	}
}

func TestLoadConfigInvalidValue(t *testing.T) {
	input := `network_id = notanumber`
	_, err := LoadConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error for non-numeric network_id")
	}
}

func TestLoadConfigMissingEquals(t *testing.T) {
	input := `datadir`
	_, err := LoadConfig([]byte(input))
	if err == nil {
		t.Fatal("expected error for missing equals sign")
	}
	if !strings.Contains(err.Error(), "key = value") {
		t.Errorf("error should mention key = value, got: %v", err)
	}
}

func TestValidateNodeConfigErrors(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*NodeConfig)
	}{
		{"empty datadir", func(c *NodeConfig) { c.DataDir = "" }},
		{"zero network_id", func(c *NodeConfig) { c.NetworkID = 0 }},
		{"bad sync_mode", func(c *NodeConfig) { c.SyncMode = "turbo" }},
		{"bad p2p port", func(c *NodeConfig) { c.P2P.Port = -1 }},
		{"bad max_peers", func(c *NodeConfig) { c.P2P.MaxPeers = -5 }},
		{"bad rpc port", func(c *NodeConfig) { c.RPC.Port = 99999 }},
		{"empty rpc host when enabled", func(c *NodeConfig) { c.RPC.Enabled = true; c.RPC.Host = "" }},
		{"mining enabled no coinbase", func(c *NodeConfig) { c.Mining.Enabled = true; c.Mining.Coinbase = "" }},
		{"zero gas_limit", func(c *NodeConfig) { c.Mining.GasLimit = 0 }},
		{"bad log level", func(c *NodeConfig) { c.Log.Level = "verbose" }},
		{"bad log format", func(c *NodeConfig) { c.Log.Format = "xml" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultNodeConfig()
			tt.modify(cfg)
			if err := cfg.ValidateNodeConfig(); err == nil {
				t.Errorf("expected validation error for %s", tt.name)
			}
		})
	}
}

func TestMergeNodeConfig(t *testing.T) {
	base := DefaultNodeConfig()

	override := &NodeConfig{
		DataDir:   "/override/path",
		NetworkID: 17000,
		SyncMode:  "full",
		P2P: P2PConfig{
			Port:           31000,
			MaxPeers:       200,
			BootstrapNodes: []string{"enode://override@1.2.3.4:30303"},
		},
		RPC: RPCConfig{
			Host: "0.0.0.0",
			Port: 9000,
			APIs: []string{"eth", "debug"},
		},
		Mining: MiningConfig{
			Coinbase: "0xabcdef",
			GasLimit: 20_000_000,
		},
		Log: LogConfig{
			Level:  "debug",
			Format: "json",
		},
	}

	merged := MergeNodeConfig(base, override)

	if merged.DataDir != "/override/path" {
		t.Errorf("DataDir = %q, want /override/path", merged.DataDir)
	}
	if merged.NetworkID != 17000 {
		t.Errorf("NetworkID = %d, want 17000", merged.NetworkID)
	}
	if merged.SyncMode != "full" {
		t.Errorf("SyncMode = %q, want full", merged.SyncMode)
	}
	if merged.P2P.Port != 31000 {
		t.Errorf("P2P.Port = %d, want 31000", merged.P2P.Port)
	}
	if merged.P2P.MaxPeers != 200 {
		t.Errorf("P2P.MaxPeers = %d, want 200", merged.P2P.MaxPeers)
	}
	if len(merged.P2P.BootstrapNodes) != 1 {
		t.Fatalf("BootstrapNodes len = %d, want 1", len(merged.P2P.BootstrapNodes))
	}
	if merged.RPC.Host != "0.0.0.0" {
		t.Errorf("RPC.Host = %q", merged.RPC.Host)
	}
	if merged.RPC.Port != 9000 {
		t.Errorf("RPC.Port = %d", merged.RPC.Port)
	}
	if len(merged.RPC.APIs) != 2 {
		t.Fatalf("RPC.APIs len = %d, want 2", len(merged.RPC.APIs))
	}
	if merged.Mining.Coinbase != "0xabcdef" {
		t.Errorf("Mining.Coinbase = %q", merged.Mining.Coinbase)
	}
	if merged.Mining.GasLimit != 20_000_000 {
		t.Errorf("Mining.GasLimit = %d", merged.Mining.GasLimit)
	}
	if merged.Log.Level != "debug" {
		t.Errorf("Log.Level = %q", merged.Log.Level)
	}
	if merged.Log.Format != "json" {
		t.Errorf("Log.Format = %q", merged.Log.Format)
	}
}

func TestMergeNodeConfigPreservesBase(t *testing.T) {
	base := DefaultNodeConfig()
	override := &NodeConfig{} // All zero values.

	merged := MergeNodeConfig(base, override)

	// Zero-value override fields should preserve base.
	if merged.DataDir != base.DataDir {
		t.Errorf("DataDir should be preserved from base")
	}
	if merged.P2P.Port != base.P2P.Port {
		t.Errorf("P2P.Port should be preserved from base")
	}
	if merged.RPC.Host != base.RPC.Host {
		t.Errorf("RPC.Host should be preserved from base")
	}
	if merged.Log.Level != base.Log.Level {
		t.Errorf("Log.Level should be preserved from base")
	}
}

func TestMergeNodeConfigDoesNotMutateBase(t *testing.T) {
	base := DefaultNodeConfig()
	origDataDir := base.DataDir

	override := &NodeConfig{
		DataDir: "/new/path",
	}

	MergeNodeConfig(base, override)

	if base.DataDir != origDataDir {
		t.Error("MergeNodeConfig should not mutate the base config")
	}
}

func TestLoadConfigEmptyArray(t *testing.T) {
	input := `[p2p]
bootstrap_nodes = []
`
	cfg, err := LoadConfig([]byte(input))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.P2P.BootstrapNodes != nil {
		t.Errorf("empty array should result in nil, got %v", cfg.P2P.BootstrapNodes)
	}
}

func TestLoadConfigPartialOverride(t *testing.T) {
	// Only override a few fields; rest should be defaults.
	input := `network_id = 5

[log]
level = "error"
`
	cfg, err := LoadConfig([]byte(input))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.NetworkID != 5 {
		t.Errorf("NetworkID = %d, want 5", cfg.NetworkID)
	}
	if cfg.Log.Level != "error" {
		t.Errorf("Log.Level = %q, want error", cfg.Log.Level)
	}
	// Defaults should be preserved.
	if cfg.P2P.Port != 30303 {
		t.Errorf("P2P.Port = %d, want 30303 (default)", cfg.P2P.Port)
	}
	if cfg.RPC.Port != 8545 {
		t.Errorf("RPC.Port = %d, want 8545 (default)", cfg.RPC.Port)
	}
}

func TestLoadConfigUnquotedStrings(t *testing.T) {
	input := `datadir = /tmp/unquoted
sync_mode = full
`
	cfg, err := LoadConfig([]byte(input))
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}
	if cfg.DataDir != "/tmp/unquoted" {
		t.Errorf("DataDir = %q, want /tmp/unquoted", cfg.DataDir)
	}
	if cfg.SyncMode != "full" {
		t.Errorf("SyncMode = %q, want full", cfg.SyncMode)
	}
}
