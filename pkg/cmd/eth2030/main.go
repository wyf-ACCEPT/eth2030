// Command eth2030 is the main entry point for the eth2030 Ethereum client.
//
// Usage:
//
//	eth2030 [flags]
//
// Flags:
//
//	--datadir      Data directory path (default: ~/.eth2030)
//	--port         P2P listening port (default: 30303)
//	--http.port    HTTP-RPC port (default: 8545)
//	--engine.port  Engine API port (default: 8551)
//	--syncmode     Sync mode: full, snap (default: snap)
//	--networkid    Network ID (default: 1)
//	--maxpeers     Max P2P peers (default: 50)
//	--verbosity    Log level 0-5 (default: 3)
//	--metrics      Enable metrics collection (default: false)
//	--version      Print version and exit
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/eth2030/eth2030/node"
)

// Build-time version info, overridable with ldflags:
//
//	go build -ldflags "-X main.version=v0.2.0 -X main.commit=abc1234"
var (
	version = "v0.1.0-dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the actual entry point, returning an exit code. Accepts CLI
// arguments (without the program name) so it can be tested in isolation.
func run(args []string) int {
	cfg, exit, code := parseFlags(args)
	if exit {
		return code
	}

	// Apply verbosity to log level.
	cfg.LogLevel = node.VerbosityToLogLevel(cfg.Verbosity)

	// Configure log format.
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Startup banner showing resolved configuration.
	log.Printf("eth2030 %s starting", version)
	log.Printf("  datadir:     %s", cfg.DataDir)
	log.Printf("  network id:  %d", cfg.NetworkID)
	log.Printf("  p2p port:    %d", cfg.P2PPort)
	log.Printf("  http port:   %d", cfg.RPCPort)
	log.Printf("  engine port: %d", cfg.EnginePort)
	log.Printf("  max peers:   %d", cfg.MaxPeers)
	log.Printf("  sync mode:   %s", cfg.SyncMode)
	log.Printf("  verbosity:   %d (%s)", cfg.Verbosity, cfg.LogLevel)
	log.Printf("  metrics:     %v", cfg.Metrics)

	// Validate configuration before doing any work.
	if err := cfg.Validate(); err != nil {
		log.Printf("Invalid configuration: %v", err)
		return 1
	}

	// Initialize data directory structure.
	if err := cfg.InitDataDir(); err != nil {
		log.Printf("Failed to initialize datadir: %v", err)
		return 1
	}
	log.Printf("Data directory initialized: %s", cfg.DataDir)

	// Create the node (initializes StateDB, blockchain, txpool, RPC, Engine API).
	n, err := node.New(&cfg)
	if err != nil {
		log.Printf("Failed to create node: %v", err)
		return 1
	}

	// Start all subsystems (P2P, RPC server, Engine API server).
	if err := n.Start(); err != nil {
		log.Printf("Failed to start node: %v", err)
		return 1
	}

	// Wait for SIGINT or SIGTERM to initiate graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Printf("Received signal %v, shutting down...", sig)

	if err := n.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
		return 1
	}

	log.Println("Shutdown complete")
	return 0
}

// parseFlags parses CLI arguments into a Config. Returns the config, whether
// the caller should exit immediately, and the exit code.
func parseFlags(args []string) (node.Config, bool, int) {
	cfg := node.DefaultConfig()
	fs := newFlagSet(&cfg)

	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cfg, true, 2
	}

	if *showVersion {
		fmt.Printf("eth2030 %s (commit %s)\n", version, commit)
		return cfg, true, 0
	}

	return cfg, false, 0
}

// newFlagSet creates a flag.FlagSet that binds all CLI flags to the given
// Config. The FlagSet uses ContinueOnError so callers control the error
// handling behavior.
func newFlagSet(cfg *node.Config) *flagSet {
	fs := newCustomFlagSet("eth2030")
	fs.StringVar(&cfg.DataDir, "datadir", cfg.DataDir, "data directory path")
	fs.IntVar(&cfg.P2PPort, "port", cfg.P2PPort, "P2P listening port")
	fs.IntVar(&cfg.RPCPort, "http.port", cfg.RPCPort, "HTTP-RPC server port")
	fs.IntVar(&cfg.EnginePort, "engine.port", cfg.EnginePort, "Engine API server port")
	fs.StringVar(&cfg.SyncMode, "syncmode", cfg.SyncMode, "sync mode (full, snap)")
	fs.Uint64Var(&cfg.NetworkID, "networkid", cfg.NetworkID, "network identifier")
	fs.IntVar(&cfg.MaxPeers, "maxpeers", cfg.MaxPeers, "maximum number of P2P peers")
	fs.IntVar(&cfg.Verbosity, "verbosity", cfg.Verbosity, "log level 0-5 (0=silent, 5=trace)")
	fs.BoolVar(&cfg.Metrics, "metrics", cfg.Metrics, "enable metrics collection")
	return fs
}
