package vm

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDefaultPurgeConfig(t *testing.T) {
	cfg := DefaultPurgeConfig()
	if !cfg.EnableSELFDESTRUCTRestriction {
		t.Fatal("expected EnableSELFDESTRUCTRestriction to be true by default")
	}
	if !cfg.EnableEmptyAccountPurge {
		t.Fatal("expected EnableEmptyAccountPurge to be true by default")
	}
	if cfg.PurgeGasCost != 5000 {
		t.Fatalf("expected PurgeGasCost=5000, got %d", cfg.PurgeGasCost)
	}
}

func TestShouldPurgeEmptyAccount(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	// Empty account: zero balance, zero nonce, no code.
	if !pm.ShouldPurgeAccount(addr, big.NewInt(0), 0, 0) {
		t.Fatal("expected empty account to be purgeable")
	}

	// Nil balance treated as zero.
	if !pm.ShouldPurgeAccount(addr, nil, 0, 0) {
		t.Fatal("expected nil-balance account to be purgeable")
	}
}

func TestShouldPurgeNonEmpty(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Account with balance.
	if pm.ShouldPurgeAccount(addr, big.NewInt(1), 0, 0) {
		t.Fatal("account with balance should not be purgeable")
	}

	// Account with nonce.
	if pm.ShouldPurgeAccount(addr, big.NewInt(0), 1, 0) {
		t.Fatal("account with nonce should not be purgeable")
	}
}

func TestShouldPurgeWithCode(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")

	if pm.ShouldPurgeAccount(addr, big.NewInt(0), 0, 100) {
		t.Fatal("account with code should not be purgeable")
	}
}

func TestPurgeEmptyAccounts(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())

	empty1 := types.HexToAddress("0x0000000000000000000000000000000000000001")
	empty2 := types.HexToAddress("0x0000000000000000000000000000000000000002")
	funded := types.HexToAddress("0x0000000000000000000000000000000000000003")

	accounts := map[types.Address]bool{
		empty1: true,
		empty2: true,
		funded: true,
	}

	balances := map[types.Address]*big.Int{
		empty1: big.NewInt(0),
		empty2: big.NewInt(0),
		funded: big.NewInt(1000),
	}

	result := pm.PurgeEmptyAccounts(
		accounts,
		func(addr types.Address) *big.Int { return balances[addr] },
		func(addr types.Address) uint64 { return 0 },
		func(addr types.Address) int { return 0 },
	)

	if result.AccountsPurged != 2 {
		t.Fatalf("expected 2 accounts purged, got %d", result.AccountsPurged)
	}
	if result.GasUsed != 10000 { // 2 * 5000
		t.Fatalf("expected GasUsed=10000, got %d", result.GasUsed)
	}
}

func TestCanSelfDestructSameTx(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())
	addr := types.HexToAddress("0x4444444444444444444444444444444444444444")

	// Created in current transaction: allowed to self-destruct.
	if !pm.CanSelfDestruct(addr, true) {
		t.Fatal("expected CanSelfDestruct to return true for same-tx creation")
	}
}

func TestCanSelfDestructDifferentTx(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())
	addr := types.HexToAddress("0x5555555555555555555555555555555555555555")

	// Not created in current transaction: EIP-6780 prevents self-destruct.
	if pm.CanSelfDestruct(addr, false) {
		t.Fatal("expected CanSelfDestruct to return false for different-tx creation")
	}

	// Without EIP-6780 restriction, always allowed.
	cfg := DefaultPurgeConfig()
	cfg.EnableSELFDESTRUCTRestriction = false
	pm2 := NewPurgeManager(cfg)
	if !pm2.CanSelfDestruct(addr, false) {
		t.Fatal("expected CanSelfDestruct to return true when restriction disabled")
	}
}

func TestPurgeStats(t *testing.T) {
	pm := NewPurgeManager(DefaultPurgeConfig())
	addr := types.HexToAddress("0x6666666666666666666666666666666666666666")

	// Check some accounts.
	pm.ShouldPurgeAccount(addr, big.NewInt(0), 0, 0)   // empty -> purge candidate
	pm.ShouldPurgeAccount(addr, big.NewInt(100), 0, 0)  // non-empty
	pm.ShouldPurgeAccount(addr, big.NewInt(0), 0, 0)    // empty -> purge candidate

	// Run a batch purge to actually count purged.
	accounts := map[types.Address]bool{
		types.HexToAddress("0x01"): true,
		types.HexToAddress("0x02"): true,
	}
	pm.PurgeEmptyAccounts(
		accounts,
		func(types.Address) *big.Int { return big.NewInt(0) },
		func(types.Address) uint64 { return 0 },
		func(types.Address) int { return 0 },
	)

	purged, checked := pm.Stats()
	// 3 from ShouldPurgeAccount calls + 2 from PurgeEmptyAccounts (which calls ShouldPurgeAccount)
	if checked != 5 {
		t.Fatalf("expected checked=5, got %d", checked)
	}
	// 2 from PurgeEmptyAccounts
	if purged != 2 {
		t.Fatalf("expected purged=2, got %d", purged)
	}
}
