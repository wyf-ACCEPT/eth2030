package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/eth2028/eth2028/node"
)

// Configuration errors.
var (
	ErrConfigFileNotFound = errors.New("config file not found")
	ErrInvalidConfig      = errors.New("invalid configuration")
	ErrUnknownNetwork     = errors.New("unknown network name")
)

// NetworkConfig holds chain-specific network parameters used to
// configure the node for a particular Ethereum network.
type NetworkConfig struct {
	Name      string // e.g., "mainnet", "sepolia", "holesky"
	NetworkID uint64
	ChainID   uint64
	Bootnodes []string
	Genesis   string // optional path to a custom genesis file
}

// Config aggregates all configuration sources (TOML file, CLI flags,
// environment variables) into a single structure that the node consumes.
type Config struct {
	// Node holds the core node configuration.
	Node node.Config

	// Network holds chain-level configuration.
	Network NetworkConfig

	// ConfigFile is the path to the TOML config file that was loaded.
	ConfigFile string

	// ExtraFlags captures CLI flags not covered by the node config.
	ExtraFlags map[string]string
}

// PredefinedNetworks maps network names to their configurations.
var PredefinedNetworks = map[string]NetworkConfig{
	"mainnet": {
		Name:      "mainnet",
		NetworkID: 1,
		ChainID:   1,
		Bootnodes: []string{
			"enode://d860a01f9722d78051619d1e2351aba3f43f943f6f00718d1b9baa4101932a1f5011f16bb2b1bb35db20d6fe28fa0bf09636d26a87d31de9ec6203eeedb1f666d@18.138.108.67:30303",
			"enode://22a8232c3abc76a16ae9d6c3b164f98775fe226f0917b0ca871128a74a8e9630b458460865bab457221f1d448dd9791d24c4e5d88786180ac185df813a68d4de2@3.209.45.79:30303",
		},
	},
	"sepolia": {
		Name:      "sepolia",
		NetworkID: 11155111,
		ChainID:   11155111,
		Bootnodes: []string{
			"enode://4e5e92199ee224a01932a377160aa432f31d0b351f84ab413a8e0a42f4f36476f8fb1cbe914af0d9aef0d51571571c39c39904a35e4e3c2d3a2f8334d2341e828@138.197.51.181:30303",
		},
	},
	"holesky": {
		Name:      "holesky",
		NetworkID: 17000,
		ChainID:   17000,
		Bootnodes: []string{
			"enode://ac906289e4b7f12df423d654c5a962b6ebe5b3a74cc9e06571e8f02aacc4e3e4eb9a42f517944e55b081fa7e7aae770461b08782119d0b316ae90af56056e90c5@18.209.78.149:30303",
		},
	},
}

// LoadConfig reads configuration from a TOML file path. Returns a Config
// populated from the file with defaults applied to any unspecified fields.
// If path is empty, returns a Config with all defaults.
func LoadConfig(path string) (*Config, error) {
	cfg := &Config{
		Node:       node.DefaultConfig(),
		ExtraFlags: make(map[string]string),
	}

	if path == "" {
		MergeDefaults(cfg)
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrConfigFileNotFound
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg.ConfigFile = path

	// Parse the TOML-like file into the node config using the existing
	// node package parser.
	nodeCfg, err := node.LoadConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Map NodeConfig fields to the flat node.Config that the CLI uses.
	cfg.Node.DataDir = nodeCfg.DataDir
	cfg.Node.NetworkID = nodeCfg.NetworkID
	cfg.Node.SyncMode = nodeCfg.SyncMode
	cfg.Node.P2PPort = nodeCfg.P2P.Port
	cfg.Node.MaxPeers = nodeCfg.P2P.MaxPeers
	cfg.Node.RPCPort = nodeCfg.RPC.Port
	cfg.Node.LogLevel = nodeCfg.Log.Level

	MergeDefaults(cfg)
	return cfg, nil
}

// MergeDefaults fills in any zero-valued fields in cfg with the predefined
// network defaults. It also resolves the Network config based on the node's
// network name.
func MergeDefaults(cfg *Config) {
	defaults := node.DefaultConfig()

	if cfg.Node.DataDir == "" {
		cfg.Node.DataDir = defaults.DataDir
	}
	if cfg.Node.Name == "" {
		cfg.Node.Name = defaults.Name
	}
	if cfg.Node.Network == "" {
		cfg.Node.Network = defaults.Network
	}
	if cfg.Node.NetworkID == 0 {
		cfg.Node.NetworkID = defaults.NetworkID
	}
	if cfg.Node.SyncMode == "" {
		cfg.Node.SyncMode = defaults.SyncMode
	}
	if cfg.Node.P2PPort == 0 {
		cfg.Node.P2PPort = defaults.P2PPort
	}
	if cfg.Node.RPCPort == 0 {
		cfg.Node.RPCPort = defaults.RPCPort
	}
	if cfg.Node.EnginePort == 0 {
		cfg.Node.EnginePort = defaults.EnginePort
	}
	if cfg.Node.MaxPeers == 0 {
		cfg.Node.MaxPeers = defaults.MaxPeers
	}
	if cfg.Node.LogLevel == "" {
		cfg.Node.LogLevel = defaults.LogLevel
	}
	if cfg.Node.Verbosity == 0 {
		cfg.Node.Verbosity = defaults.Verbosity
	}

	// Resolve network config from node network name.
	if net, ok := PredefinedNetworks[cfg.Node.Network]; ok {
		cfg.Network = net
	}
}

// ValidateConfig checks the Config for internal consistency and returns
// an error describing the first problem found.
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("%w: nil config", ErrInvalidConfig)
	}

	// Validate the underlying node config.
	if err := cfg.Node.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	// Network consistency: if a known network is selected, the network ID
	// must match the predefined value.
	if net, ok := PredefinedNetworks[cfg.Node.Network]; ok {
		if cfg.Node.NetworkID != net.NetworkID {
			return fmt.Errorf("%w: network %q expects network_id %d, got %d",
				ErrInvalidConfig, cfg.Node.Network, net.NetworkID, cfg.Node.NetworkID)
		}
	}

	// Port conflicts.
	ports := map[string]int{
		"p2p":    cfg.Node.P2PPort,
		"rpc":    cfg.Node.RPCPort,
		"engine": cfg.Node.EnginePort,
	}
	seen := make(map[int]string, 3)
	for name, port := range ports {
		if prev, exists := seen[port]; exists {
			return fmt.Errorf("%w: port %d used by both %s and %s",
				ErrInvalidConfig, port, prev, name)
		}
		seen[port] = name
	}

	return nil
}

// NewChainConfig creates a NetworkConfig for the given network name by
// looking up the predefined networks. Returns an error for unknown networks.
func NewChainConfig(network string) (*NetworkConfig, error) {
	net, ok := PredefinedNetworks[strings.ToLower(network)]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownNetwork, network)
	}
	result := net
	return &result, nil
}

// ApplyEnvironment reads environment variables and overrides Config fields.
// Environment variables use the prefix ETH2028_ followed by uppercase
// field names (e.g., ETH2028_DATADIR, ETH2028_P2P_PORT).
func ApplyEnvironment(cfg *Config) {
	if v := os.Getenv("ETH2028_DATADIR"); v != "" {
		cfg.Node.DataDir = v
	}
	if v := os.Getenv("ETH2028_NETWORK"); v != "" {
		cfg.Node.Network = v
	}
	if v := os.Getenv("ETH2028_NETWORK_ID"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			cfg.Node.NetworkID = n
		}
	}
	if v := os.Getenv("ETH2028_SYNCMODE"); v != "" {
		cfg.Node.SyncMode = v
	}
	if v := os.Getenv("ETH2028_P2P_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Node.P2PPort = n
		}
	}
	if v := os.Getenv("ETH2028_RPC_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Node.RPCPort = n
		}
	}
	if v := os.Getenv("ETH2028_ENGINE_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Node.EnginePort = n
		}
	}
	if v := os.Getenv("ETH2028_MAX_PEERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Node.MaxPeers = n
		}
	}
	if v := os.Getenv("ETH2028_LOG_LEVEL"); v != "" {
		cfg.Node.LogLevel = v
	}
	if v := os.Getenv("ETH2028_VERBOSITY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Node.Verbosity = n
		}
	}
	if v := os.Getenv("ETH2028_METRICS"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Node.Metrics = b
		}
	}
}

// MergeCLIFlags applies CLI flag values into the Config, overriding any
// values set by the config file or environment.
func MergeCLIFlags(cfg *Config, nodeCfg node.Config) {
	cfg.Node = nodeCfg
	MergeDefaults(cfg)
}
