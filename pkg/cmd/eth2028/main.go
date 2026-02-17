// Command eth2028 is the main entry point for the eth2028 Ethereum client.
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

func main() {
	cfg := node.DefaultConfig()

	flag.StringVar(&cfg.DataDir, "datadir", cfg.DataDir, "data directory for databases and keystore")
	flag.IntVar(&cfg.P2PPort, "port", cfg.P2PPort, "P2P network listening port")
	flag.IntVar(&cfg.RPCPort, "rpc-port", cfg.RPCPort, "HTTP-RPC server listening port")
	flag.IntVar(&cfg.EnginePort, "engine-port", cfg.EnginePort, "Engine API server listening port")
	flag.IntVar(&cfg.MaxPeers, "maxpeers", cfg.MaxPeers, "maximum number of network peers")
	flag.StringVar(&cfg.Network, "network", cfg.Network, "network to join (mainnet, sepolia, holesky)")
	flag.StringVar(&cfg.SyncMode, "syncmode", cfg.SyncMode, "sync mode (full, snap)")
	flag.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log verbosity (debug, info, warn, error)")
	flag.Parse()

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)
		os.Exit(1)
	}

	n, err := node.New(&cfg)
	if err != nil {
		log.Fatalf("Failed to create node: %v", err)
	}

	if err := n.Start(); err != nil {
		log.Fatalf("Failed to start node: %v", err)
	}

	// Wait for interrupt signal.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Received shutdown signal")
	if err := n.Stop(); err != nil {
		log.Fatalf("Failed to stop node: %v", err)
	}
}
