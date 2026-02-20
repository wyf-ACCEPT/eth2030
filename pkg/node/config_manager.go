// ConfigManager: node configuration with defaults, overrides, validation,
// multi-source merging, and fork schedule management.
package node

import (
	"errors"
	"fmt"
	"strings"
)

// ConfigManager errors.
var (
	ErrCfgMgrEmpty          = errors.New("config_manager: empty value")
	ErrCfgMgrInvalidPort    = errors.New("config_manager: invalid port number")
	ErrCfgMgrInvalidChainID = errors.New("config_manager: invalid chain ID")
	ErrCfgMgrInvalidSync    = errors.New("config_manager: invalid sync mode")
	ErrCfgMgrInvalidFork    = errors.New("config_manager: invalid fork schedule")
	ErrCfgMgrConflict       = errors.New("config_manager: conflicting settings")
	ErrCfgMgrNoJWT          = errors.New("config_manager: engine API requires JWT secret")
)

// ConfigSource identifies the origin of a configuration value.
type ConfigSource int

const (
	// SourceDefault indicates a built-in default value.
	SourceDefault ConfigSource = iota
	// SourceFile indicates a value loaded from a config file.
	SourceFile
	// SourceEnv indicates a value from an environment variable.
	SourceEnv
	// SourceCLI indicates a value from a command-line flag.
	SourceCLI
)

// String returns a human-readable name for the config source.
func (s ConfigSource) String() string {
	switch s {
	case SourceDefault:
		return "default"
	case SourceFile:
		return "file"
	case SourceEnv:
		return "env"
	case SourceCLI:
		return "cli"
	default:
		return "unknown"
	}
}

// NetworkConfig holds chain-level network configuration.
type NetworkConfig struct {
	// ChainID is the EIP-155 chain identifier.
	ChainID uint64

	// NetworkID is the devp2p network identifier.
	NetworkID uint64

	// GenesisHash is the hex-encoded genesis block hash.
	GenesisHash string

	// ForkSchedule maps fork names to activation block numbers.
	// Example: {"london": 12965000, "merge": 15537393}
	ForkSchedule map[string]uint64
}

// SyncConfig holds synchronization configuration.
type SyncConfig struct {
	// Mode is the sync strategy: "full", "snap", or "beam".
	Mode string

	// MaxPeers is the maximum number of sync peers.
	MaxPeers int

	// ConnectTimeout is the peer connection timeout in seconds.
	ConnectTimeout int

	// EnableDiscovery enables peer discovery via DHT.
	EnableDiscovery bool
}

// ManagedRPCConfig holds JSON-RPC server configuration for the config manager.
type ManagedRPCConfig struct {
	// Enabled controls whether the RPC server is started.
	Enabled bool

	// Host is the bind address for the RPC server.
	Host string

	// Port is the TCP port for the RPC server.
	Port int

	// AllowedModules lists enabled RPC namespaces.
	AllowedModules []string

	// CORSOrigins lists allowed CORS origins.
	CORSOrigins []string

	// RateLimit is the max requests per second per client (0 = unlimited).
	RateLimit int
}

// EngineConfig holds Engine API configuration.
type EngineConfig struct {
	// Enabled controls whether the Engine API server is started.
	Enabled bool

	// Host is the bind address.
	Host string

	// Port is the TCP port.
	Port int

	// JWTSecret is the hex-encoded JWT authentication secret.
	JWTSecret string

	// PayloadBuilderEnabled controls local payload building.
	PayloadBuilderEnabled bool
}

// ManagedConfig is the full configuration managed by ConfigManager.
type ManagedConfig struct {
	Network  NetworkConfig
	Sync     SyncConfig
	RPC      ManagedRPCConfig
	Engine   EngineConfig
	DataDir  string
	LogLevel string
}

// DefaultManagedConfig returns a ManagedConfig with sensible defaults.
func DefaultManagedConfig() *ManagedConfig {
	return &ManagedConfig{
		Network: NetworkConfig{
			ChainID:      1,
			NetworkID:    1,
			GenesisHash:  "0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3",
			ForkSchedule: map[string]uint64{},
		},
		Sync: SyncConfig{
			Mode:            "snap",
			MaxPeers:        50,
			ConnectTimeout:  30,
			EnableDiscovery: true,
		},
		RPC: ManagedRPCConfig{
			Enabled:        true,
			Host:           "127.0.0.1",
			Port:           8545,
			AllowedModules: []string{"eth", "net", "web3"},
			CORSOrigins:    nil,
			RateLimit:      0,
		},
		Engine: EngineConfig{
			Enabled:               true,
			Host:                  "127.0.0.1",
			Port:                  8551,
			JWTSecret:             "",
			PayloadBuilderEnabled: false,
		},
		DataDir:  "",
		LogLevel: "info",
	}
}

// ConfigManager provides validated, multi-source configuration management.
type ConfigManager struct {
	base     *ManagedConfig
	sources  map[string]ConfigSource // tracks where each field came from
}

// NewConfigManager creates a ConfigManager with default configuration.
func NewConfigManager() *ConfigManager {
	return &ConfigManager{
		base:    DefaultManagedConfig(),
		sources: make(map[string]ConfigSource),
	}
}

// Config returns the current configuration.
func (cm *ConfigManager) Config() *ManagedConfig {
	return cm.base
}

// SetDataDir sets the data directory.
func (cm *ConfigManager) SetDataDir(dir string, source ConfigSource) {
	cm.base.DataDir = dir
	cm.sources["datadir"] = source
}

// SetLogLevel sets the log level.
func (cm *ConfigManager) SetLogLevel(level string, source ConfigSource) {
	cm.base.LogLevel = level
	cm.sources["loglevel"] = source
}

// SetNetworkConfig replaces the network configuration.
func (cm *ConfigManager) SetNetworkConfig(nc NetworkConfig, source ConfigSource) {
	cm.base.Network = nc
	cm.sources["network"] = source
}

// SetSyncConfig replaces the sync configuration.
func (cm *ConfigManager) SetSyncConfig(sc SyncConfig, source ConfigSource) {
	cm.base.Sync = sc
	cm.sources["sync"] = source
}

// SetRPCConfig replaces the RPC configuration.
func (cm *ConfigManager) SetRPCConfig(rc ManagedRPCConfig, source ConfigSource) {
	cm.base.RPC = rc
	cm.sources["rpc"] = source
}

// SetEngineConfig replaces the Engine API configuration.
func (cm *ConfigManager) SetEngineConfig(ec EngineConfig, source ConfigSource) {
	cm.base.Engine = ec
	cm.sources["engine"] = source
}

// Source returns the ConfigSource for a given field key.
func (cm *ConfigManager) Source(field string) ConfigSource {
	src, ok := cm.sources[field]
	if !ok {
		return SourceDefault
	}
	return src
}

// --- Validation ---

// ConfigValidator validates a ManagedConfig for correctness and consistency.
type ConfigValidator struct{}

// NewConfigValidator creates a new config validator.
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate checks the full configuration. Returns all errors found.
func (cv *ConfigValidator) Validate(cfg *ManagedConfig) []error {
	var errs []error

	errs = append(errs, cv.validateNetwork(cfg.Network)...)
	errs = append(errs, cv.validateSync(cfg.Sync)...)
	errs = append(errs, cv.validateRPC(cfg.RPC)...)
	errs = append(errs, cv.validateEngine(cfg.Engine)...)

	if cfg.LogLevel != "" {
		switch cfg.LogLevel {
		case "debug", "info", "warn", "error", "trace":
		default:
			errs = append(errs, fmt.Errorf("unknown log level %q", cfg.LogLevel))
		}
	}

	// Cross-field validation: snap sync needs discovery.
	if cfg.Sync.Mode == "snap" && !cfg.Sync.EnableDiscovery {
		errs = append(errs, fmt.Errorf("%w: snap sync requires discovery", ErrCfgMgrConflict))
	}

	// Engine API needs JWT secret.
	if cfg.Engine.Enabled && cfg.Engine.JWTSecret == "" {
		errs = append(errs, ErrCfgMgrNoJWT)
	}

	return errs
}

func (cv *ConfigValidator) validateNetwork(nc NetworkConfig) []error {
	var errs []error
	if nc.ChainID == 0 {
		errs = append(errs, ErrCfgMgrInvalidChainID)
	}
	if nc.NetworkID == 0 {
		errs = append(errs, fmt.Errorf("network_id must be > 0"))
	}

	// Validate fork schedule ordering if multiple forks are present.
	if len(nc.ForkSchedule) > 1 {
		if err := validateForkOrder(nc.ForkSchedule); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func (cv *ConfigValidator) validateSync(sc SyncConfig) []error {
	var errs []error
	switch sc.Mode {
	case "full", "snap", "beam":
	default:
		errs = append(errs, fmt.Errorf("%w: %q", ErrCfgMgrInvalidSync, sc.Mode))
	}
	if sc.MaxPeers < 0 {
		errs = append(errs, fmt.Errorf("max_peers must be >= 0"))
	}
	if sc.ConnectTimeout < 0 {
		errs = append(errs, fmt.Errorf("connect_timeout must be >= 0"))
	}
	return errs
}

func (cv *ConfigValidator) validateRPC(rc ManagedRPCConfig) []error {
	var errs []error
	if rc.Port < 0 || rc.Port > 65535 {
		errs = append(errs, fmt.Errorf("%w: rpc port %d", ErrCfgMgrInvalidPort, rc.Port))
	}
	if rc.Enabled && rc.Host == "" {
		errs = append(errs, fmt.Errorf("rpc host must not be empty when enabled"))
	}
	if rc.RateLimit < 0 {
		errs = append(errs, fmt.Errorf("rpc rate_limit must be >= 0"))
	}
	return errs
}

func (cv *ConfigValidator) validateEngine(ec EngineConfig) []error {
	var errs []error
	if ec.Port < 0 || ec.Port > 65535 {
		errs = append(errs, fmt.Errorf("%w: engine port %d", ErrCfgMgrInvalidPort, ec.Port))
	}
	if ec.Enabled && ec.Host == "" {
		errs = append(errs, fmt.Errorf("engine host must not be empty when enabled"))
	}
	return errs
}

// validateForkOrder checks that known forks are in ascending block order.
// Returns an error if any fork has a lower block number than a predecessor.
func validateForkOrder(forks map[string]uint64) error {
	// Known fork ordering (subset).
	knownOrder := []string{
		"homestead", "tangerine", "spurious", "byzantium", "constantinople",
		"istanbul", "berlin", "london", "merge", "shanghai", "cancun",
		"glamsterdam", "hogota",
	}

	lastBlock := uint64(0)
	lastFork := ""
	for _, name := range knownOrder {
		block, ok := forks[name]
		if !ok {
			continue
		}
		if block < lastBlock {
			return fmt.Errorf("%w: %s (block %d) before %s (block %d)",
				ErrCfgMgrInvalidFork, name, block, lastFork, lastBlock)
		}
		lastBlock = block
		lastFork = name
	}
	return nil
}

// --- Config Merging ---

// ConfigMerge merges multiple configuration sources with precedence.
// Later sources override earlier ones. Sources are applied in order:
// default < file < env < CLI.
func ConfigMerge(configs ...*ManagedConfig) *ManagedConfig {
	if len(configs) == 0 {
		return DefaultManagedConfig()
	}

	result := DefaultManagedConfig()
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		mergeManagedConfig(result, cfg)
	}
	return result
}

// mergeManagedConfig applies non-zero values from src onto dst.
func mergeManagedConfig(dst, src *ManagedConfig) {
	// Network
	if src.Network.ChainID != 0 {
		dst.Network.ChainID = src.Network.ChainID
	}
	if src.Network.NetworkID != 0 {
		dst.Network.NetworkID = src.Network.NetworkID
	}
	if src.Network.GenesisHash != "" {
		dst.Network.GenesisHash = src.Network.GenesisHash
	}
	if len(src.Network.ForkSchedule) > 0 {
		dst.Network.ForkSchedule = src.Network.ForkSchedule
	}

	// Sync
	if src.Sync.Mode != "" {
		dst.Sync.Mode = src.Sync.Mode
	}
	if src.Sync.MaxPeers != 0 {
		dst.Sync.MaxPeers = src.Sync.MaxPeers
	}
	if src.Sync.ConnectTimeout != 0 {
		dst.Sync.ConnectTimeout = src.Sync.ConnectTimeout
	}

	// RPC
	if src.RPC.Host != "" {
		dst.RPC.Host = src.RPC.Host
	}
	if src.RPC.Port != 0 {
		dst.RPC.Port = src.RPC.Port
	}
	if len(src.RPC.AllowedModules) > 0 {
		dst.RPC.AllowedModules = src.RPC.AllowedModules
	}
	if len(src.RPC.CORSOrigins) > 0 {
		dst.RPC.CORSOrigins = src.RPC.CORSOrigins
	}
	if src.RPC.RateLimit != 0 {
		dst.RPC.RateLimit = src.RPC.RateLimit
	}

	// Engine
	if src.Engine.Host != "" {
		dst.Engine.Host = src.Engine.Host
	}
	if src.Engine.Port != 0 {
		dst.Engine.Port = src.Engine.Port
	}
	if src.Engine.JWTSecret != "" {
		dst.Engine.JWTSecret = src.Engine.JWTSecret
	}

	// Top-level
	if src.DataDir != "" {
		dst.DataDir = src.DataDir
	}
	if src.LogLevel != "" {
		dst.LogLevel = src.LogLevel
	}
}

// --- Fork Schedule Helpers ---

// ForkSchedule provides helper methods for working with fork activation blocks.
type ForkSchedule struct {
	forks map[string]uint64
}

// NewForkSchedule creates a fork schedule from a map of fork name to block.
func NewForkSchedule(forks map[string]uint64) *ForkSchedule {
	m := make(map[string]uint64, len(forks))
	for k, v := range forks {
		m[k] = v
	}
	return &ForkSchedule{forks: m}
}

// IsActive returns whether a fork is active at the given block number.
func (fs *ForkSchedule) IsActive(fork string, block uint64) bool {
	activation, ok := fs.forks[fork]
	if !ok {
		return false
	}
	return block >= activation
}

// ActivationBlock returns the activation block for a fork, or 0 and false
// if the fork is not in the schedule.
func (fs *ForkSchedule) ActivationBlock(fork string) (uint64, bool) {
	b, ok := fs.forks[fork]
	return b, ok
}

// ActiveForks returns all forks active at the given block number.
func (fs *ForkSchedule) ActiveForks(block uint64) []string {
	var active []string
	for name, activation := range fs.forks {
		if block >= activation {
			active = append(active, name)
		}
	}
	return active
}

// ForkCount returns the total number of forks in the schedule.
func (fs *ForkSchedule) ForkCount() int {
	return len(fs.forks)
}

// FormatForkSchedule returns a human-readable string of the fork schedule.
func FormatForkSchedule(forks map[string]uint64) string {
	if len(forks) == 0 {
		return "(empty)"
	}
	parts := make([]string, 0, len(forks))
	for name, block := range forks {
		parts = append(parts, fmt.Sprintf("%s@%d", name, block))
	}
	return strings.Join(parts, ", ")
}
