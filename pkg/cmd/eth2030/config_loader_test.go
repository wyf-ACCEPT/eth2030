package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eth2030/eth2030/node"
)

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig empty path error: %v", err)
	}

	defaults := node.DefaultConfig()
	if cfg.Node.DataDir != defaults.DataDir {
		t.Errorf("DataDir = %q, want %q", cfg.Node.DataDir, defaults.DataDir)
	}
	if cfg.Node.P2PPort != defaults.P2PPort {
		t.Errorf("P2PPort = %d, want %d", cfg.Node.P2PPort, defaults.P2PPort)
	}
	if cfg.Node.RPCPort != defaults.RPCPort {
		t.Errorf("RPCPort = %d, want %d", cfg.Node.RPCPort, defaults.RPCPort)
	}
}

func TestLoadConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	content := `datadir = "/data/test"
network_id = 11155111
sync_mode = "full"

[p2p]
port = 30304
max_peers = 100

[rpc]
enabled = true
host = "0.0.0.0"
port = 9545

[log]
level = "debug"
format = "text"
`
	os.WriteFile(path, []byte(content), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error: %v", err)
	}

	if cfg.Node.DataDir != "/data/test" {
		t.Errorf("DataDir = %q, want /data/test", cfg.Node.DataDir)
	}
	if cfg.Node.NetworkID != 11155111 {
		t.Errorf("NetworkID = %d, want 11155111", cfg.Node.NetworkID)
	}
	if cfg.Node.SyncMode != "full" {
		t.Errorf("SyncMode = %q, want full", cfg.Node.SyncMode)
	}
	if cfg.Node.P2PPort != 30304 {
		t.Errorf("P2PPort = %d, want 30304", cfg.Node.P2PPort)
	}
	if cfg.Node.MaxPeers != 100 {
		t.Errorf("MaxPeers = %d, want 100", cfg.Node.MaxPeers)
	}
	if cfg.Node.RPCPort != 9545 {
		t.Errorf("RPCPort = %d, want 9545", cfg.Node.RPCPort)
	}
	if cfg.ConfigFile != path {
		t.Errorf("ConfigFile = %q, want %q", cfg.ConfigFile, path)
	}
}

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.toml")
	if !errors.Is(err, ErrConfigFileNotFound) {
		t.Errorf("expected ErrConfigFileNotFound, got %v", err)
	}
}

func TestLoadConfigInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	os.WriteFile(path, []byte("[unclosed_section\n"), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestMergeDefaults(t *testing.T) {
	cfg := &Config{
		ExtraFlags: make(map[string]string),
	}
	// All zero values.
	MergeDefaults(cfg)

	defaults := node.DefaultConfig()
	if cfg.Node.DataDir != defaults.DataDir {
		t.Errorf("DataDir = %q, want %q", cfg.Node.DataDir, defaults.DataDir)
	}
	if cfg.Node.P2PPort != defaults.P2PPort {
		t.Errorf("P2PPort = %d, want %d", cfg.Node.P2PPort, defaults.P2PPort)
	}
	if cfg.Node.Network != defaults.Network {
		t.Errorf("Network = %q, want %q", cfg.Node.Network, defaults.Network)
	}
	// Mainnet network config should be resolved.
	if cfg.Network.ChainID != 1 {
		t.Errorf("Network.ChainID = %d, want 1", cfg.Network.ChainID)
	}
}

func TestMergeDefaultsPreservesExisting(t *testing.T) {
	cfg := &Config{
		Node:       node.Config{DataDir: "/custom", P2PPort: 31000},
		ExtraFlags: make(map[string]string),
	}
	MergeDefaults(cfg)

	if cfg.Node.DataDir != "/custom" {
		t.Errorf("DataDir = %q, want /custom (should not be overwritten)", cfg.Node.DataDir)
	}
	if cfg.Node.P2PPort != 31000 {
		t.Errorf("P2PPort = %d, want 31000 (should not be overwritten)", cfg.Node.P2PPort)
	}
}

func TestValidateConfigValid(t *testing.T) {
	cfg := &Config{
		Node: node.DefaultConfig(),
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Errorf("valid config should pass: %v", err)
	}
}

func TestValidateConfigNil(t *testing.T) {
	err := ValidateConfig(nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestValidateConfigNetworkIDMismatch(t *testing.T) {
	cfg := &Config{
		Node: node.DefaultConfig(),
	}
	cfg.Node.Network = "mainnet"
	cfg.Node.NetworkID = 999 // wrong for mainnet

	err := ValidateConfig(cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestValidateConfigPortConflict(t *testing.T) {
	cfg := &Config{
		Node: node.DefaultConfig(),
	}
	cfg.Node.RPCPort = 30303 // same as P2P port

	err := ValidateConfig(cfg)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Errorf("expected ErrInvalidConfig for port conflict, got %v", err)
	}
}

func TestNewChainConfig(t *testing.T) {
	tests := []struct {
		name      string
		wantID    uint64
		wantChain uint64
	}{
		{"mainnet", 1, 1},
		{"sepolia", 11155111, 11155111},
		{"holesky", 17000, 17000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			net, err := NewChainConfig(tt.name)
			if err != nil {
				t.Fatalf("NewChainConfig(%q) error: %v", tt.name, err)
			}
			if net.NetworkID != tt.wantID {
				t.Errorf("NetworkID = %d, want %d", net.NetworkID, tt.wantID)
			}
			if net.ChainID != tt.wantChain {
				t.Errorf("ChainID = %d, want %d", net.ChainID, tt.wantChain)
			}
			if net.Name != tt.name {
				t.Errorf("Name = %q, want %q", net.Name, tt.name)
			}
		})
	}
}

func TestNewChainConfigUnknown(t *testing.T) {
	_, err := NewChainConfig("unknownnet")
	if !errors.Is(err, ErrUnknownNetwork) {
		t.Errorf("expected ErrUnknownNetwork, got %v", err)
	}
}

func TestNewChainConfigCaseInsensitive(t *testing.T) {
	net, err := NewChainConfig("Mainnet")
	if err != nil {
		t.Fatalf("NewChainConfig(Mainnet) error: %v", err)
	}
	if net.NetworkID != 1 {
		t.Errorf("NetworkID = %d, want 1", net.NetworkID)
	}
}

func TestNewChainConfigBootnodes(t *testing.T) {
	net, err := NewChainConfig("mainnet")
	if err != nil {
		t.Fatal(err)
	}
	if len(net.Bootnodes) == 0 {
		t.Error("mainnet should have bootnodes")
	}
}

func TestApplyEnvironment(t *testing.T) {
	cfg := &Config{
		Node:       node.DefaultConfig(),
		ExtraFlags: make(map[string]string),
	}

	// Set environment variables.
	t.Setenv("ETH2028_DATADIR", "/env/data")
	t.Setenv("ETH2028_P2P_PORT", "31111")
	t.Setenv("ETH2028_VERBOSITY", "5")
	t.Setenv("ETH2028_METRICS", "true")

	ApplyEnvironment(cfg)

	if cfg.Node.DataDir != "/env/data" {
		t.Errorf("DataDir = %q, want /env/data", cfg.Node.DataDir)
	}
	if cfg.Node.P2PPort != 31111 {
		t.Errorf("P2PPort = %d, want 31111", cfg.Node.P2PPort)
	}
	if cfg.Node.Verbosity != 5 {
		t.Errorf("Verbosity = %d, want 5", cfg.Node.Verbosity)
	}
	if !cfg.Node.Metrics {
		t.Error("Metrics should be true")
	}
}

func TestApplyEnvironmentInvalidValues(t *testing.T) {
	cfg := &Config{
		Node:       node.DefaultConfig(),
		ExtraFlags: make(map[string]string),
	}

	origPort := cfg.Node.P2PPort

	t.Setenv("ETH2028_P2P_PORT", "notanumber")
	ApplyEnvironment(cfg)

	// Invalid values should be ignored.
	if cfg.Node.P2PPort != origPort {
		t.Errorf("P2PPort = %d, want %d (should be unchanged)", cfg.Node.P2PPort, origPort)
	}
}

func TestMergeCLIFlags(t *testing.T) {
	cfg := &Config{
		Node:       node.DefaultConfig(),
		ExtraFlags: make(map[string]string),
	}

	cliCfg := node.DefaultConfig()
	cliCfg.P2PPort = 40000
	cliCfg.MaxPeers = 200
	cliCfg.SyncMode = "full"

	MergeCLIFlags(cfg, cliCfg)

	if cfg.Node.P2PPort != 40000 {
		t.Errorf("P2PPort = %d, want 40000", cfg.Node.P2PPort)
	}
	if cfg.Node.MaxPeers != 200 {
		t.Errorf("MaxPeers = %d, want 200", cfg.Node.MaxPeers)
	}
}

func TestPredefinedNetworksConsistency(t *testing.T) {
	for name, net := range PredefinedNetworks {
		if net.Name != name {
			t.Errorf("PredefinedNetworks[%q].Name = %q, should match key", name, net.Name)
		}
		if net.NetworkID == 0 {
			t.Errorf("PredefinedNetworks[%q].NetworkID should not be 0", name)
		}
		if net.ChainID == 0 {
			t.Errorf("PredefinedNetworks[%q].ChainID should not be 0", name)
		}
	}
}

func TestConfigExtraFlags(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ExtraFlags == nil {
		t.Fatal("ExtraFlags should not be nil")
	}
	// ExtraFlags starts empty.
	if len(cfg.ExtraFlags) != 0 {
		t.Errorf("ExtraFlags len = %d, want 0", len(cfg.ExtraFlags))
	}
}
