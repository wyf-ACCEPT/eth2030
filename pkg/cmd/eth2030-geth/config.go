// Package main implements the eth2030-geth binary, which embeds go-ethereum
// as a library for full Ethereum node operation with ETH2030 custom precompiles.
package main

import (
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	gethnode "github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
)

// eth2030GethConfig holds the go-ethereum node and eth service configuration.
type eth2030GethConfig struct {
	Node gethnode.Config
	Eth  ethconfig.Config
}

// mapNodeConfig creates a go-ethereum node.Config from CLI parameters.
func mapNodeConfig(datadir, name, network string, p2pPort int, httpAddr string, httpPort int, authAddr string, authPort int, authVhosts string, maxPeers int,
	httpModules []string, jwtSecret string) gethnode.Config {

	return gethnode.Config{
		Name:             name,
		Version:          version,
		DataDir:          datadir,
		HTTPHost:         httpAddr,
		HTTPPort:         httpPort,
		HTTPModules:      httpModules,
		HTTPVirtualHosts: []string{"localhost"},
		AuthAddr:         authAddr,
		AuthPort:         authPort,
		AuthVirtualHosts: splitAndTrim(authVhosts),
		JWTSecret:        jwtSecret,
		P2P: p2p.Config{
			ListenAddr:       fmt.Sprintf(":%d", p2pPort),
			MaxPeers:         maxPeers,
			BootstrapNodes:   parseBootnodes(network),
			BootstrapNodesV5: parseBootnodes(network),
		},
	}
}

// parseBootnodes returns the go-ethereum bootstrap nodes for the given network.
func parseBootnodes(network string) []*enode.Node {
	var urls []string
	switch network {
	case "sepolia":
		urls = params.SepoliaBootnodes
	case "holesky":
		urls = params.HoleskyBootnodes
	default:
		urls = params.MainnetBootnodes
	}
	nodes := make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		n, err := enode.Parse(enode.ValidSchemes, url)
		if err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			result = append(result, v)
		}
	}
	return result
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
