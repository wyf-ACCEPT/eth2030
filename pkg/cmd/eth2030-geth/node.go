package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/catalyst"
	"github.com/ethereum/go-ethereum/eth/tracers"
	"github.com/ethereum/go-ethereum/log"
	gethnode "github.com/ethereum/go-ethereum/node"
)

// makeFullNode creates and configures the go-ethereum node with all services.
func makeFullNode(cfg *eth2030GethConfig) (*gethnode.Node, *eth.Ethereum) {
	stack, err := gethnode.New(&cfg.Node)
	if err != nil {
		log.Crit("Failed to create P2P node", "err", err)
	}

	backend, err := eth.New(stack, &cfg.Eth)
	if err != nil {
		log.Crit("Failed to create Ethereum service", "err", err)
	}

	// Register tracer APIs (debug_traceTransaction, etc.).
	stack.RegisterAPIs(tracers.APIs(backend.APIBackend))

	// Register Engine API for consensus client communication.
	if err := catalyst.Register(stack, backend); err != nil {
		log.Crit("Failed to register Engine API", "err", err)
	}

	return stack, backend
}

// startAndWait starts the node and blocks until SIGINT/SIGTERM.
func startAndWait(stack *gethnode.Node) {
	if err := stack.Start(); err != nil {
		log.Crit("Failed to start node", "err", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Info("Received shutdown signal", "signal", sig)

	stack.Close()
}
