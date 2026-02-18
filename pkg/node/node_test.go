package node

import (
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	if cfg.P2PPort != 30303 {
		t.Errorf("expected P2P port 30303, got %d", cfg.P2PPort)
	}
	if cfg.RPCPort != 8545 {
		t.Errorf("expected RPC port 8545, got %d", cfg.RPCPort)
	}
	if cfg.EnginePort != 8551 {
		t.Errorf("expected Engine port 8551, got %d", cfg.EnginePort)
	}
	if cfg.Network != "mainnet" {
		t.Errorf("expected network mainnet, got %s", cfg.Network)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "empty datadir",
			modify:  func(c *Config) { c.DataDir = "" },
			wantErr: true,
		},
		{
			name:    "invalid network",
			modify:  func(c *Config) { c.Network = "foonet" },
			wantErr: true,
		},
		{
			name:    "invalid sync mode",
			modify:  func(c *Config) { c.SyncMode = "turbo" },
			wantErr: true,
		},
		{
			name:    "invalid port",
			modify:  func(c *Config) { c.P2PPort = -1 },
			wantErr: true,
		},
		{
			name:    "invalid log level",
			modify:  func(c *Config) { c.LogLevel = "verbose" },
			wantErr: true,
		},
		{
			name:    "sepolia network",
			modify:  func(c *Config) { c.Network = "sepolia" },
			wantErr: false,
		},
		{
			name:    "holesky network",
			modify:  func(c *Config) { c.Network = "holesky" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigAddrs(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.P2PAddr() != ":30303" {
		t.Errorf("P2PAddr() = %s, want :30303", cfg.P2PAddr())
	}
	if cfg.RPCAddr() != "127.0.0.1:8545" {
		t.Errorf("RPCAddr() = %s, want 127.0.0.1:8545", cfg.RPCAddr())
	}
	if cfg.EngineAddr() != "127.0.0.1:8551" {
		t.Errorf("EngineAddr() = %s, want 127.0.0.1:8551", cfg.EngineAddr())
	}
}

func TestNewNode(t *testing.T) {
	// Use ephemeral ports to avoid conflicts.
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if n.Blockchain() == nil {
		t.Error("blockchain should not be nil")
	}
	if n.TxPool() == nil {
		t.Error("txpool should not be nil")
	}
	if n.Config().Network != "mainnet" {
		t.Errorf("expected mainnet, got %s", n.Config().Network)
	}

	// Genesis block should exist.
	genesis := n.Blockchain().Genesis()
	if genesis == nil {
		t.Fatal("genesis block should not be nil")
	}
	if genesis.NumberU64() != 0 {
		t.Errorf("genesis number = %d, want 0", genesis.NumberU64())
	}
}

func TestNewNode_NilConfig(t *testing.T) {
	// Passing nil should use default config.
	n, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) error: %v", err)
	}
	if n.Config().Network != "mainnet" {
		t.Errorf("expected mainnet, got %s", n.Config().Network)
	}
}

func TestNewNode_InvalidConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Network = "badnet"
	_, err := New(&cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestNode_StartStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Double start should error.
	if err := n.Start(); err == nil {
		t.Error("expected error on double start")
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

func TestNode_StopWithoutStart(t *testing.T) {
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Stop on a node that was never started should be a no-op.
	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() on non-started node should not error: %v", err)
	}
}

func TestNode_DoubleStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("first Stop() error: %v", err)
	}

	// Second stop should be a no-op (not panic on closed channel).
	if err := n.Stop(); err != nil {
		t.Fatalf("second Stop() should not error: %v", err)
	}
}

func TestNode_Running(t *testing.T) {
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	if n.Running() {
		t.Error("node should not be running before Start()")
	}

	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !n.Running() {
		t.Error("node should be running after Start()")
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if n.Running() {
		t.Error("node should not be running after Stop()")
	}
}

func TestNode_Lifecycle(t *testing.T) {
	// Full lifecycle test: create, start, verify subsystems, stop.
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Verify subsystems are initialized before start.
	if n.Blockchain() == nil {
		t.Fatal("blockchain should be initialized after New()")
	}
	if n.TxPool() == nil {
		t.Fatal("txpool should be initialized after New()")
	}
	if n.Config() == nil {
		t.Fatal("config should be initialized after New()")
	}

	// Genesis should be accessible.
	genesis := n.Blockchain().Genesis()
	if genesis == nil {
		t.Fatal("genesis block should exist")
	}
	if genesis.NumberU64() != 0 {
		t.Errorf("genesis block number = %d, want 0", genesis.NumberU64())
	}

	// Start the node.
	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Verify it is running.
	if !n.Running() {
		t.Error("node should be running after Start()")
	}

	// Stop the node.
	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if n.Running() {
		t.Error("node should not be running after Stop()")
	}
}

func TestNode_Backend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.P2PPort = 0
	cfg.RPCPort = 0
	cfg.EnginePort = 0

	n, err := New(&cfg)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Test the RPC backend adapter.
	backend := newNodeBackend(n)

	// ChainID should match mainnet.
	chainID := backend.ChainID()
	if chainID == nil || chainID.Int64() != 1 {
		t.Errorf("ChainID() = %v, want 1", chainID)
	}

	// CurrentHeader should return genesis.
	header := backend.CurrentHeader()
	if header == nil {
		t.Fatal("CurrentHeader() should not be nil")
	}
	if header.Number.Uint64() != 0 {
		t.Errorf("CurrentHeader().Number = %d, want 0", header.Number.Uint64())
	}

	// SuggestGasPrice should return non-nil.
	price := backend.SuggestGasPrice()
	if price == nil || price.Sign() <= 0 {
		t.Error("SuggestGasPrice() should return positive value")
	}
}
