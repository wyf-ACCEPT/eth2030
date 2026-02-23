// Package main implements the eth2030-geth binary, which embeds go-ethereum
// as a library for full Ethereum node operation with ETH2030 custom precompiles.
package main

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	gethnode "github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
)

// eth2030GethConfig holds the go-ethereum node and eth service configuration.
type eth2030GethConfig struct {
	Node gethnode.Config
	Eth  ethconfig.Config
}

// mapNodeConfig creates a go-ethereum node.Config from CLI parameters.
func mapNodeConfig(datadir, name string, p2pPort, httpPort, authPort, maxPeers int,
	httpModules []string, jwtSecret string) gethnode.Config {

	return gethnode.Config{
		Name:             name,
		Version:          version,
		DataDir:          datadir,
		HTTPHost:         "127.0.0.1",
		HTTPPort:         httpPort,
		HTTPModules:      httpModules,
		HTTPVirtualHosts: []string{"localhost"},
		AuthAddr:         "127.0.0.1",
		AuthPort:         authPort,
		JWTSecret:        jwtSecret,
		P2P: p2p.Config{
			ListenAddr: fmt.Sprintf(":%d", p2pPort),
			MaxPeers:   maxPeers,
		},
	}
}

// mapEthConfig creates a go-ethereum ethconfig.Config for the selected network.
func mapEthConfig(network, syncMode string) ethconfig.Config {
	cfg := ethconfig.Defaults

	switch syncMode {
	case "full":
		cfg.SyncMode = ethconfig.FullSync
	default:
		cfg.SyncMode = ethconfig.SnapSync
	}

	switch network {
	case "sepolia":
		cfg.Genesis = core.DefaultSepoliaGenesisBlock()
		cfg.NetworkId = 11155111
	case "holesky":
		cfg.Genesis = core.DefaultHoleskyGenesisBlock()
		cfg.NetworkId = 17000
	default:
		cfg.Genesis = core.DefaultGenesisBlock()
		cfg.NetworkId = 1
	}

	return cfg
}
