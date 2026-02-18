// Command eth2028 is the main entry point for the eth2028 Ethereum client.
//
// Usage:
//
//	eth2028 [flags]
//
// Flags:
//
//	-datadir      Data directory for blockchain storage (default: "eth2028-data")
//	-rpc.port     JSON-RPC HTTP port (default: 8545)
//	-engine.port  Engine API HTTP port (default: 8551)
//	-p2p.port     P2P TCP listen port (default: 30303)
//	-networkid    Network identifier: mainnet, sepolia, holesky (default: "mainnet")
//	-loglevel     Log verbosity: debug, info, warn, error (default: "info")
//	-maxpeers     Maximum number of P2P peers (default: 50)
//	-syncmode     Sync mode: full, snap (default: "full")
//	-version      Print version and exit
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/eth2028/eth2028/node"
)

// Build-time version info, overridable with ldflags:
//
//	go build -ldflags "-X main.version=v0.2.0 -X main.commit=abc1234"
var (
	version = "v0.1.0-dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run())
}

// run is the actual entry point, returning an exit code. This pattern
// makes it easy to test the binary without calling os.Exit directly.
func run() int {
	cfg := node.DefaultConfig()

	// CLI flags.
	flag.StringVar(&cfg.DataDir, "datadir", cfg.DataDir, "data directory for blockchain storage")
	flag.IntVar(&cfg.RPCPort, "rpc.port", cfg.RPCPort, "JSON-RPC HTTP server port")
	flag.IntVar(&cfg.EnginePort, "engine.port", cfg.EnginePort, "Engine API HTTP server port")
	flag.IntVar(&cfg.P2PPort, "p2p.port", cfg.P2PPort, "P2P TCP listen port")
	flag.StringVar(&cfg.Network, "networkid", cfg.Network, "network to join (mainnet, sepolia, holesky)")
	flag.StringVar(&cfg.LogLevel, "loglevel", cfg.LogLevel, "log verbosity (debug, info, warn, error)")
	flag.IntVar(&cfg.MaxPeers, "maxpeers", cfg.MaxPeers, "maximum number of P2P peers")
	flag.StringVar(&cfg.SyncMode, "syncmode", cfg.SyncMode, "sync mode (full, snap)")
	showVersion := flag.Bool("version", false, "print version and exit")

	flag.Parse()

	if *showVersion {
		fmt.Printf("eth2028 %s (commit %s)\n", version, commit)
		return 0
	}

	// Configure log format.
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Startup banner showing resolved configuration.
	log.Printf("eth2028 %s starting", version)
	log.Printf("  network:     %s", cfg.Network)
	log.Printf("  datadir:     %s", cfg.DataDir)
	log.Printf("  rpc port:    %d", cfg.RPCPort)
	log.Printf("  engine port: %d", cfg.EnginePort)
	log.Printf("  p2p port:    %d", cfg.P2PPort)
	log.Printf("  max peers:   %d", cfg.MaxPeers)
	log.Printf("  sync mode:   %s", cfg.SyncMode)
	log.Printf("  log level:   %s", cfg.LogLevel)

	// Validate configuration before creating the node.
	if err := cfg.Validate(); err != nil {
		log.Printf("Invalid configuration: %v", err)
		return 1
	}

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
