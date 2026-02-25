package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/log"
)

var (
	version = "v0.2.0"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("eth2030-geth", flag.ContinueOnError)

	// Node flags.
	datadir := fs.String("datadir", defaultDataDir(), "Data directory for the node")
	network := fs.String("network", "mainnet", "Network to join (mainnet, sepolia, holesky)")
	p2pPort := fs.Int("port", 30303, "P2P listening port")
	httpAddr := fs.String("http.addr", "127.0.0.1", "HTTP-RPC server listening address")
	httpPort := fs.Int("http.port", 8545, "HTTP-RPC server port")
	authAddr := fs.String("authrpc.addr", "127.0.0.1", "Engine API listening address")
	authPort := fs.Int("authrpc.port", 8551, "Engine API (authenticated RPC) port")
	authVhosts := fs.String("authrpc.vhosts", "localhost", "Comma-separated list of virtual hostnames for Engine API (use * for all)")
	jwtSecret := fs.String("authrpc.jwtsecret", "", "Path to JWT secret for Engine API auth")
	syncMode := fs.String("syncmode", "snap", "Sync mode: full, snap")
	maxPeers := fs.Int("maxpeers", 50, "Maximum number of P2P peers")
	verbosity := fs.Int("verbosity", 3, "Log level 0-5 (0=silent, 5=trace)")
	showVersion := fs.Bool("version", false, "Print version and exit")

	// Kurtosis-compatible flags.
	genesisPath := fs.String("override.genesis", "", "Path to custom genesis.json (for Kurtosis devnets)")
	httpEnabled := fs.Bool("http", false, "Enable HTTP-RPC server (always enabled, kept for compatibility)")
	httpVhosts := fs.String("http.vhosts", "*", "Comma-separated list of virtual hostnames for HTTP-RPC (use * for all)")
	httpCORSDomain := fs.String("http.corsdomain", "*", "Comma-separated list of CORS domains for HTTP-RPC")
	httpAPI := fs.String("http.api", "admin,engine,net,eth,web3,debug,txpool", "Comma-separated list of HTTP-RPC API modules")
	wsEnabled := fs.Bool("ws", false, "Enable WebSocket RPC server")
	wsAddr := fs.String("ws.addr", "127.0.0.1", "WebSocket RPC server listening address")
	wsPort := fs.Int("ws.port", 8546, "WebSocket RPC server port")
	wsAPI := fs.String("ws.api", "", "Comma-separated list of WebSocket RPC API modules")
	wsOrigins := fs.String("ws.origins", "", "Comma-separated list of allowed WebSocket origins")
	natAddr := fs.String("nat", "", "NAT traversal method (e.g., extip:1.2.3.4)")
	networkID := fs.Uint64("networkid", 0, "Network ID override (0 = use genesis chain ID)")
	discoveryPort := fs.Int("discovery.port", 30303, "UDP port for node discovery")
	bootnodes := fs.String("bootnodes", "", "Comma-separated enode URLs for bootstrap")
	metricsEnabled := fs.Bool("metrics", false, "Enable metrics collection")
	metricsAddr := fs.String("metrics.addr", "127.0.0.1", "Metrics HTTP server listening address")
	metricsPort := fs.Int("metrics.port", 9001, "Metrics HTTP server port")
	allowUnprotectedTxs := fs.Bool("rpc.allow-unprotected-txs", false, "Allow unprotected (non-EIP155) transactions over RPC")
	minerGasPrice := fs.Uint64("miner.gasprice", 0, "Minimum gas price for mining (wei)")
	minerGasLimit := fs.Uint64("miner.gaslimit", 0, "Target gas ceiling for mined blocks")
	gcMode := fs.String("gcmode", "", "GC mode: archive (no pruning) or full (default)")

	// ETH2030 fork override flags (for testing future forks).
	glamsterdamOverride := fs.Uint64("override.glamsterdam", 0, "Override Glamsterdam fork timestamp (testing only)")
	hogotaOverride := fs.Uint64("override.hogota", 0, "Override Hogota fork timestamp (testing only)")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 2
	}

	if *showVersion {
		fmt.Printf("eth2030-geth %s (commit %s)\n", version, commit)
		return 0
	}

	// Set up go-ethereum structured logging.
	setupLogging(*verbosity)

	log.Info("Starting eth2030-geth",
		"version", version,
		"network", *network,
		"datadir", *datadir,
		"syncmode", *syncMode,
	)

	_ = *httpEnabled // HTTP always enabled; flag accepted for compatibility.

	// Build go-ethereum configuration from CLI flags.
	cfg := &eth2030GethConfig{
		Node: mapNodeConfig(nodeConfigParams{
			datadir: *datadir, name: "eth2030-geth", network: *network,
			p2pPort: *p2pPort, httpAddr: *httpAddr, httpPort: *httpPort,
			authAddr: *authAddr, authPort: *authPort, authVhosts: *authVhosts,
			maxPeers: *maxPeers, jwtSecret: *jwtSecret,
			httpVhosts: *httpVhosts, httpCORSDomain: *httpCORSDomain, httpAPI: *httpAPI,
			wsEnabled: *wsEnabled, wsAddr: *wsAddr, wsPort: *wsPort,
			wsAPI: *wsAPI, wsOrigins: *wsOrigins,
			discoveryPort: *discoveryPort, bootnodes: *bootnodes, natAddr: *natAddr,
			metricsEnabled: *metricsEnabled, metricsAddr: *metricsAddr, metricsPort: *metricsPort,
			allowUnprotectedTxs: *allowUnprotectedTxs,
		}),
		Eth: mapEthConfig(ethConfigParams{
			network: *network, syncMode: *syncMode, genesisPath: *genesisPath,
			networkID: *networkID, gcMode: *gcMode,
			minerGasPrice: *minerGasPrice, minerGasLimit: *minerGasLimit,
		}),
	}

	// Auto-generate JWT secret path if not provided.
	if cfg.Node.JWTSecret == "" {
		cfg.Node.JWTSecret = filepath.Join(*datadir, "jwtsecret")
	}

	// Set up precompile injector for future forks.
	var glamTime, hogTime *uint64
	if *glamsterdamOverride > 0 {
		glamTime = glamsterdamOverride
		log.Info("Glamsterdam fork override set", "timestamp", *glamTime)
	}
	if *hogotaOverride > 0 {
		hogTime = hogotaOverride
		log.Info("Hogota fork override set", "timestamp", *hogTime)
	}
	injector := newPrecompileInjector(glamTime, hogTime, nil)
	_ = injector // Available for future RPC-level precompile injection.

	// Create and start the full node.
	stack, _ := makeFullNode(cfg)

	log.Info("Node created, starting sync",
		"p2p.port", *p2pPort,
		"http.port", *httpPort,
		"engine.port", *authPort,
		"maxpeers", *maxPeers,
	)

	startAndWait(stack)

	log.Info("Shutdown complete")
	return 0
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".eth2030-geth"
	}
	return filepath.Join(home, ".eth2030-geth")
}

func setupLogging(verbosity int) {
	var lvl slog.Level
	switch {
	case verbosity <= 1:
		lvl = slog.LevelError
	case verbosity == 2:
		lvl = slog.LevelWarn
	case verbosity == 3:
		lvl = slog.LevelInfo
	case verbosity == 4:
		lvl = slog.LevelDebug
	default:
		lvl = log.LevelTrace
	}
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, lvl, true)))
}
