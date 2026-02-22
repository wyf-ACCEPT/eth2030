package node

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// NodeConfig holds the full configuration for an eth2030 node,
// parsed from a TOML-like configuration file. It is separate from
// Config to support richer structured configuration with nested sections.
type NodeConfig struct {
	DataDir   string
	NetworkID uint64
	SyncMode  string

	P2P     P2PConfig
	RPC     RPCConfig
	Mining  MiningConfig
	Log     LogConfig
}

// P2PConfig holds P2P networking configuration.
type P2PConfig struct {
	Port           int
	MaxPeers       int
	BootstrapNodes []string
}

// RPCConfig holds JSON-RPC server configuration.
type RPCConfig struct {
	Enabled bool
	Host    string
	Port    int
	APIs    []string
}

// MiningConfig holds block production configuration.
type MiningConfig struct {
	Enabled  bool
	Coinbase string
	GasLimit uint64
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level  string
	Format string
}

// DefaultNodeConfig returns a NodeConfig with sensible defaults.
func DefaultNodeConfig() *NodeConfig {
	return &NodeConfig{
		DataDir:   defaultDataDir(),
		NetworkID: 1,
		SyncMode:  "snap",
		P2P: P2PConfig{
			Port:           30303,
			MaxPeers:       50,
			BootstrapNodes: nil,
		},
		RPC: RPCConfig{
			Enabled: true,
			Host:    "127.0.0.1",
			Port:    8545,
			APIs:    []string{"eth", "net", "web3"},
		},
		Mining: MiningConfig{
			Enabled:  false,
			Coinbase: "",
			GasLimit: 30_000_000,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// ValidateNodeConfig checks the configuration for correctness.
func (nc *NodeConfig) ValidateNodeConfig() error {
	if nc.DataDir == "" {
		return errors.New("config: datadir must not be empty")
	}
	if nc.NetworkID == 0 {
		return errors.New("config: network_id must be greater than 0")
	}
	switch nc.SyncMode {
	case "full", "snap":
	default:
		return fmt.Errorf("config: unknown sync_mode %q", nc.SyncMode)
	}

	// P2P validation.
	if nc.P2P.Port < 0 || nc.P2P.Port > 65535 {
		return fmt.Errorf("config: invalid p2p port: %d", nc.P2P.Port)
	}
	if nc.P2P.MaxPeers < 0 {
		return fmt.Errorf("config: invalid max_peers: %d", nc.P2P.MaxPeers)
	}

	// RPC validation.
	if nc.RPC.Port < 0 || nc.RPC.Port > 65535 {
		return fmt.Errorf("config: invalid rpc port: %d", nc.RPC.Port)
	}
	if nc.RPC.Enabled && nc.RPC.Host == "" {
		return errors.New("config: rpc host must not be empty when rpc is enabled")
	}

	// Mining validation.
	if nc.Mining.Enabled && nc.Mining.Coinbase == "" {
		return errors.New("config: coinbase must be set when mining is enabled")
	}
	if nc.Mining.GasLimit == 0 {
		return errors.New("config: gas_limit must be greater than 0")
	}

	// Log validation.
	switch nc.Log.Level {
	case "debug", "info", "warn", "error", "trace":
	default:
		return fmt.Errorf("config: unknown log level %q", nc.Log.Level)
	}
	switch nc.Log.Format {
	case "text", "json":
	default:
		return fmt.Errorf("config: unknown log format %q", nc.Log.Format)
	}

	return nil
}

// LoadConfig parses a TOML-like configuration from raw bytes into a NodeConfig.
// The parser handles key = value pairs and [section] headers. It supports
// string values (quoted or unquoted), integers, booleans, and arrays.
func LoadConfig(data []byte) (*NodeConfig, error) {
	cfg := DefaultNodeConfig()
	section := ""

	lines := strings.Split(string(data), "\n")
	for lineNum, raw := range lines {
		line := strings.TrimSpace(raw)

		// Skip empty lines and comments.
		if line == "" || line[0] == '#' {
			continue
		}

		// Section header.
		if line[0] == '[' {
			end := strings.Index(line, "]")
			if end < 0 {
				return nil, fmt.Errorf("line %d: unclosed section header", lineNum+1)
			}
			section = strings.TrimSpace(line[1:end])
			continue
		}

		// Key = value pair.
		eqIdx := strings.Index(line, "=")
		if eqIdx < 0 {
			return nil, fmt.Errorf("line %d: expected key = value", lineNum+1)
		}
		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		if err := applyConfigValue(cfg, section, key, val, lineNum+1); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// applyConfigValue sets a single configuration field based on section, key, value.
func applyConfigValue(cfg *NodeConfig, section, key, val string, lineNum int) error {
	switch section {
	case "":
		return applyTopLevel(cfg, key, val, lineNum)
	case "p2p":
		return applyP2P(cfg, key, val, lineNum)
	case "rpc":
		return applyRPC(cfg, key, val, lineNum)
	case "mining":
		return applyMining(cfg, key, val, lineNum)
	case "log":
		return applyLog(cfg, key, val, lineNum)
	default:
		return fmt.Errorf("line %d: unknown section [%s]", lineNum, section)
	}
}

func applyTopLevel(cfg *NodeConfig, key, val string, lineNum int) error {
	switch key {
	case "datadir":
		cfg.DataDir = unquote(val)
	case "network_id":
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return fmt.Errorf("line %d: invalid network_id: %w", lineNum, err)
		}
		cfg.NetworkID = n
	case "sync_mode":
		cfg.SyncMode = unquote(val)
	default:
		return fmt.Errorf("line %d: unknown key %q in top-level", lineNum, key)
	}
	return nil
}

func applyP2P(cfg *NodeConfig, key, val string, lineNum int) error {
	switch key {
	case "port":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("line %d: invalid p2p port: %w", lineNum, err)
		}
		cfg.P2P.Port = n
	case "max_peers":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("line %d: invalid max_peers: %w", lineNum, err)
		}
		cfg.P2P.MaxPeers = n
	case "bootstrap_nodes":
		cfg.P2P.BootstrapNodes = parseStringArray(val)
	default:
		return fmt.Errorf("line %d: unknown key %q in [p2p]", lineNum, key)
	}
	return nil
}

func applyRPC(cfg *NodeConfig, key, val string, lineNum int) error {
	switch key {
	case "enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("line %d: invalid rpc enabled: %w", lineNum, err)
		}
		cfg.RPC.Enabled = b
	case "host":
		cfg.RPC.Host = unquote(val)
	case "port":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("line %d: invalid rpc port: %w", lineNum, err)
		}
		cfg.RPC.Port = n
	case "apis":
		cfg.RPC.APIs = parseStringArray(val)
	default:
		return fmt.Errorf("line %d: unknown key %q in [rpc]", lineNum, key)
	}
	return nil
}

func applyMining(cfg *NodeConfig, key, val string, lineNum int) error {
	switch key {
	case "enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("line %d: invalid mining enabled: %w", lineNum, err)
		}
		cfg.Mining.Enabled = b
	case "coinbase":
		cfg.Mining.Coinbase = unquote(val)
	case "gas_limit":
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return fmt.Errorf("line %d: invalid gas_limit: %w", lineNum, err)
		}
		cfg.Mining.GasLimit = n
	default:
		return fmt.Errorf("line %d: unknown key %q in [mining]", lineNum, key)
	}
	return nil
}

func applyLog(cfg *NodeConfig, key, val string, lineNum int) error {
	switch key {
	case "level":
		cfg.Log.Level = unquote(val)
	case "format":
		cfg.Log.Format = unquote(val)
	default:
		return fmt.Errorf("line %d: unknown key %q in [log]", lineNum, key)
	}
	return nil
}

// unquote strips surrounding double quotes from a string value.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseStringArray parses a TOML-like array: ["a", "b", "c"].
func parseStringArray(s string) []string {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		// Single value without brackets.
		v := unquote(strings.TrimSpace(s))
		if v == "" {
			return nil
		}
		return []string{v}
	}

	inner := s[1 : len(s)-1]
	if strings.TrimSpace(inner) == "" {
		return nil
	}

	parts := strings.Split(inner, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		v := unquote(strings.TrimSpace(p))
		if v != "" {
			result = append(result, v)
		}
	}
	return result
}

// MergeNodeConfig merges an override config onto a base config.
// Non-zero/non-empty values from override take priority over base.
func MergeNodeConfig(base, override *NodeConfig) *NodeConfig {
	result := *base

	if override.DataDir != "" {
		result.DataDir = override.DataDir
	}
	if override.NetworkID != 0 {
		result.NetworkID = override.NetworkID
	}
	if override.SyncMode != "" {
		result.SyncMode = override.SyncMode
	}

	// P2P
	if override.P2P.Port != 0 {
		result.P2P.Port = override.P2P.Port
	}
	if override.P2P.MaxPeers != 0 {
		result.P2P.MaxPeers = override.P2P.MaxPeers
	}
	if len(override.P2P.BootstrapNodes) > 0 {
		result.P2P.BootstrapNodes = override.P2P.BootstrapNodes
	}

	// RPC: Enabled is always merged since it's meaningful as true or false.
	// We merge it only if the override has any RPC field set.
	if override.RPC.Host != "" {
		result.RPC.Host = override.RPC.Host
	}
	if override.RPC.Port != 0 {
		result.RPC.Port = override.RPC.Port
	}
	if len(override.RPC.APIs) > 0 {
		result.RPC.APIs = override.RPC.APIs
	}

	// Mining
	if override.Mining.Coinbase != "" {
		result.Mining.Coinbase = override.Mining.Coinbase
	}
	if override.Mining.GasLimit != 0 {
		result.Mining.GasLimit = override.Mining.GasLimit
	}

	// Log
	if override.Log.Level != "" {
		result.Log.Level = override.Log.Level
	}
	if override.Log.Format != "" {
		result.Log.Format = override.Log.Format
	}

	return &result
}
