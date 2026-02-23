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
	httpPort := fs.Int("http.port", 8545, "HTTP-RPC server port")
	authPort := fs.Int("authrpc.port", 8551, "Engine API (authenticated RPC) port")
	jwtSecret := fs.String("authrpc.jwtsecret", "", "Path to JWT secret for Engine API auth")
	syncMode := fs.String("syncmode", "snap", "Sync mode: full, snap")
	maxPeers := fs.Int("maxpeers", 50, "Maximum number of P2P peers")
	verbosity := fs.Int("verbosity", 3, "Log level 0-5 (0=silent, 5=trace)")
	showVersion := fs.Bool("version", false, "Print version and exit")

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

	// Build go-ethereum configuration from CLI flags.
	cfg := &eth2030GethConfig{
		Node: mapNodeConfig(
			*datadir, "eth2030-geth", *network,
			*p2pPort, *httpPort, *authPort, *maxPeers,
			[]string{"eth", "net", "web3", "txpool", "engine", "admin", "debug"},
			*jwtSecret,
		),
		Eth: mapEthConfig(*network, *syncMode),
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
