// Package node implements the eth2028 full node lifecycle,
// wiring together blockchain, RPC, Engine API, P2P, and TxPool.
package node

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Config holds all configuration for an eth2028 node.
type Config struct {
	// DataDir is the root directory for all data storage.
	DataDir string

	// Name is a human-readable node identifier (used in logs).
	Name string

	// Network selects the Ethereum network (mainnet, sepolia, holesky).
	Network string

	// SyncMode selects the sync strategy (full, snap).
	SyncMode string

	// P2PPort is the TCP port for devp2p connections.
	P2PPort int

	// RPCPort is the HTTP port for the JSON-RPC server.
	RPCPort int

	// EnginePort is the HTTP port for the Engine API server.
	EnginePort int

	// MaxPeers is the maximum number of P2P peers.
	MaxPeers int

	// LogLevel controls log verbosity (debug, info, warn, error).
	LogLevel string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		DataDir:    "eth2028-data",
		Name:       "eth2028",
		Network:    "mainnet",
		SyncMode:   "full",
		P2PPort:    30303,
		RPCPort:    8545,
		EnginePort: 8551,
		MaxPeers:   50,
		LogLevel:   "info",
	}
}

// Validate checks configuration values for correctness.
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return errors.New("config: datadir must not be empty")
	}
	if c.P2PPort < 0 || c.P2PPort > 65535 {
		return fmt.Errorf("config: invalid p2p port: %d", c.P2PPort)
	}
	if c.RPCPort < 0 || c.RPCPort > 65535 {
		return fmt.Errorf("config: invalid rpc port: %d", c.RPCPort)
	}
	if c.EnginePort < 0 || c.EnginePort > 65535 {
		return fmt.Errorf("config: invalid engine port: %d", c.EnginePort)
	}
	if c.MaxPeers < 0 {
		return fmt.Errorf("config: invalid max peers: %d", c.MaxPeers)
	}
	switch c.Network {
	case "mainnet", "sepolia", "holesky":
	default:
		return fmt.Errorf("config: unknown network %q", c.Network)
	}
	switch c.SyncMode {
	case "full", "snap":
	default:
		return fmt.Errorf("config: unknown sync mode %q", c.SyncMode)
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("config: unknown log level %q", c.LogLevel)
	}
	return nil
}

// ResolvePath resolves a path relative to the data directory.
func (c *Config) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.DataDir, path)
}

// P2PAddr returns the P2P listen address string.
func (c *Config) P2PAddr() string {
	return fmt.Sprintf(":%d", c.P2PPort)
}

// RPCAddr returns the RPC listen address string.
func (c *Config) RPCAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.RPCPort)
}

// EngineAddr returns the Engine API listen address string.
func (c *Config) EngineAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.EnginePort)
}
