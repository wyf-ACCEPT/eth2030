package main

import (
	"testing"

	"github.com/ethereum/go-ethereum/eth/ethconfig"

	"github.com/eth2030/eth2030/geth"
)

func TestMapEthConfigMainnet(t *testing.T) {
	cfg := mapEthConfig("mainnet", "snap")
	if cfg.Genesis == nil {
		t.Fatal("expected mainnet genesis to be non-nil")
	}
	if cfg.NetworkId != 1 {
		t.Fatalf("expected network id 1, got %d", cfg.NetworkId)
	}
	if cfg.SyncMode != ethconfig.SnapSync {
		t.Fatalf("expected snap sync, got %v", cfg.SyncMode)
	}
}

func TestMapEthConfigSepolia(t *testing.T) {
	cfg := mapEthConfig("sepolia", "full")
	if cfg.Genesis == nil {
		t.Fatal("expected sepolia genesis to be non-nil")
	}
	if cfg.NetworkId != 11155111 {
		t.Fatalf("expected network id 11155111, got %d", cfg.NetworkId)
	}
	if cfg.SyncMode != ethconfig.FullSync {
		t.Fatalf("expected full sync, got %v", cfg.SyncMode)
	}
}

func TestMapEthConfigHolesky(t *testing.T) {
	cfg := mapEthConfig("holesky", "snap")
	if cfg.Genesis == nil {
		t.Fatal("expected holesky genesis to be non-nil")
	}
	if cfg.NetworkId != 17000 {
		t.Fatalf("expected network id 17000, got %d", cfg.NetworkId)
	}
}

func TestMapNodeConfig(t *testing.T) {
	cfg := mapNodeConfig("/tmp/test", "test-node", 30303, 8545, 8551, 25,
		[]string{"eth", "web3"}, "/tmp/jwt")
	if cfg.DataDir != "/tmp/test" {
		t.Fatalf("expected datadir /tmp/test, got %s", cfg.DataDir)
	}
	if cfg.HTTPPort != 8545 {
		t.Fatalf("expected http port 8545, got %d", cfg.HTTPPort)
	}
	if cfg.AuthPort != 8551 {
		t.Fatalf("expected auth port 8551, got %d", cfg.AuthPort)
	}
	if cfg.P2P.MaxPeers != 25 {
		t.Fatalf("expected max peers 25, got %d", cfg.P2P.MaxPeers)
	}
	if cfg.P2P.ListenAddr != ":30303" {
		t.Fatalf("expected listen addr :30303, got %s", cfg.P2P.ListenAddr)
	}
}

func TestPrecompileInjectorForkLevels(t *testing.T) {
	glamTime := uint64(1700000000)
	hogTime := uint64(1800000000)
	iPlusTime := uint64(1900000000)
	pi := newPrecompileInjector(&glamTime, &hogTime, &iPlusTime)

	if level := pi.forkLevelAtTime(1600000000); level != geth.ForkLevelPrague {
		t.Fatalf("expected Prague, got %v", level)
	}
	if level := pi.forkLevelAtTime(1700000000); level != geth.ForkLevelGlamsterdam {
		t.Fatalf("expected Glamsterdam, got %v", level)
	}
	if level := pi.forkLevelAtTime(1800000000); level != geth.ForkLevelHogota {
		t.Fatalf("expected Hogota, got %v", level)
	}
	if level := pi.forkLevelAtTime(1900000000); level != geth.ForkLevelIPlus {
		t.Fatalf("expected IPlus, got %v", level)
	}
}

func TestPrecompileInjectorNoForks(t *testing.T) {
	pi := newPrecompileInjector(nil, nil, nil)
	addrs := pi.CustomAddresses(9999999999)
	if len(addrs) != 0 {
		t.Fatalf("expected no custom addresses, got %d", len(addrs))
	}
}

func TestPrecompileInjectorGlamsterdamAddresses(t *testing.T) {
	glamTime := uint64(1000)
	pi := newPrecompileInjector(&glamTime, nil, nil)

	// Before fork.
	addrs := pi.CustomAddresses(999)
	if len(addrs) != 0 {
		t.Fatalf("expected no addresses before fork, got %d", len(addrs))
	}

	// At fork activation.
	addrs = pi.CustomAddresses(1000)
	if len(addrs) != 4 {
		t.Fatalf("expected 4 Glamsterdam addresses, got %d", len(addrs))
	}
}

func TestVersionFlag(t *testing.T) {
	code := run([]string{"--version"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}
