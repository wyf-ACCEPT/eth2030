package core

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestTestnetGenesisAlloc(t *testing.T) {
	alloc := TestnetGenesisAlloc()

	// Should have exactly 10 prefunded accounts.
	if len(alloc) != 10 {
		t.Fatalf("expected 10 prefunded accounts, got %d", len(alloc))
	}

	// Each account should have 10000 ETH.
	for _, addr := range TestnetPrefundedAccounts {
		acct, ok := alloc[addr]
		if !ok {
			t.Fatalf("missing prefunded account %s", addr.Hex())
		}
		if acct.Balance.Cmp(TestnetPrefundAmount) != 0 {
			t.Fatalf("account %s has wrong balance: %s", addr.Hex(), acct.Balance.String())
		}
	}
}

func TestTestnetGenesisBlock(t *testing.T) {
	genesis := TestnetGenesisBlock()

	if genesis.Config == nil {
		t.Fatal("testnet genesis has nil config")
	}
	if genesis.Config.ChainID.Cmp(big.NewInt(11155111)) != 0 {
		t.Fatalf("expected Sepolia chain ID, got %s", genesis.Config.ChainID.String())
	}
	if genesis.GasLimit != 30_000_000 {
		t.Fatalf("expected gas limit 30_000_000, got %d", genesis.GasLimit)
	}
	if len(genesis.Alloc) != 10 {
		t.Fatalf("expected 10 alloc accounts, got %d", len(genesis.Alloc))
	}

	// Convert to block.
	block := genesis.ToBlock()
	if block.NumberU64() != 0 {
		t.Fatalf("genesis block number should be 0, got %d", block.NumberU64())
	}
}

func TestSystemContractAlloc(t *testing.T) {
	alloc := SystemContractAlloc()

	// Should have 5 system contracts.
	if len(alloc) != 5 {
		t.Fatalf("expected 5 system contracts, got %d", len(alloc))
	}

	// Verify each system contract address.
	addrs := []types.Address{
		beaconRootsAddress,
		historyStorageAddress,
		types.DepositContractAddress,
		types.WithdrawalRequestAddress,
		types.ConsolidationRequestAddress,
	}
	for _, addr := range addrs {
		acct, ok := alloc[addr]
		if !ok {
			t.Fatalf("missing system contract %s", addr.Hex())
		}
		if len(acct.Code) == 0 {
			t.Fatalf("system contract %s has no code", addr.Hex())
		}
		if acct.Nonce != 1 {
			t.Fatalf("system contract %s should have nonce 1, got %d", addr.Hex(), acct.Nonce)
		}
	}
}

func TestMergeGenesisAlloc(t *testing.T) {
	alloc := MergeGenesisAlloc()

	// Should have 10 testnet + 5 system = 15 accounts.
	if len(alloc) != 15 {
		t.Fatalf("expected 15 accounts in merge alloc, got %d", len(alloc))
	}

	// Verify testnet accounts are present.
	for _, addr := range TestnetPrefundedAccounts {
		if !AllocHasAccount(alloc, addr) {
			t.Fatalf("merge alloc missing testnet account %s", addr.Hex())
		}
	}

	// Verify system contracts are present.
	if !AllocHasAccount(alloc, beaconRootsAddress) {
		t.Fatal("merge alloc missing beacon roots contract")
	}
}

func TestMarshalGenesisAlloc(t *testing.T) {
	alloc := GenesisAlloc{
		types.HexToAddress("0x0000000000000000000000000000000000000001"): GenesisAccount{
			Balance: big.NewInt(1_000_000),
		},
		types.HexToAddress("0x0000000000000000000000000000000000000002"): GenesisAccount{
			Balance: big.NewInt(2_000_000),
			Nonce:   5,
		},
	}

	data, err := MarshalGenesisAlloc(alloc)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var entries []GenesisAllocJSON
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Entries should be in sorted address order.
	if entries[0].Address > entries[1].Address {
		t.Fatal("entries are not in sorted order")
	}
}

func TestMarshalGenesisAllocDeterministic(t *testing.T) {
	alloc := TestnetGenesisAlloc()

	data1, err := MarshalGenesisAlloc(alloc)
	if err != nil {
		t.Fatalf("first marshal error: %v", err)
	}

	data2, err := MarshalGenesisAlloc(alloc)
	if err != nil {
		t.Fatalf("second marshal error: %v", err)
	}

	if string(data1) != string(data2) {
		t.Fatal("MarshalGenesisAlloc is not deterministic")
	}
}

func TestAllocAccountCount(t *testing.T) {
	alloc := TestnetGenesisAlloc()
	if AllocAccountCount(alloc) != 10 {
		t.Fatalf("expected 10, got %d", AllocAccountCount(alloc))
	}

	if AllocAccountCount(GenesisAlloc{}) != 0 {
		t.Fatal("expected 0 for empty alloc")
	}
}

func TestAllocHasAccount(t *testing.T) {
	alloc := TestnetGenesisAlloc()

	if !AllocHasAccount(alloc, TestnetPrefundedAccounts[0]) {
		t.Fatal("expected account to be present")
	}

	missing := types.HexToAddress("0xdead000000000000000000000000000000000000")
	if AllocHasAccount(alloc, missing) {
		t.Fatal("expected account to be absent")
	}
}

func TestSnapshotGenesisState(t *testing.T) {
	alloc := GenesisAlloc{
		types.HexToAddress("0x0000000000000000000000000000000000000001"): GenesisAccount{
			Balance: big.NewInt(100),
		},
		types.HexToAddress("0x0000000000000000000000000000000000000002"): GenesisAccount{
			Balance: big.NewInt(200),
			Code:    []byte{0x60, 0x00},
		},
	}

	snap := SnapshotGenesisState(alloc)

	if snap.AccountCount != 2 {
		t.Fatalf("expected 2 accounts, got %d", snap.AccountCount)
	}
	if snap.TotalBalance.Cmp(big.NewInt(300)) != 0 {
		t.Fatalf("expected total balance 300, got %s", snap.TotalBalance.String())
	}
	if snap.CodeAccounts != 1 {
		t.Fatalf("expected 1 code account, got %d", snap.CodeAccounts)
	}
	if snap.Root == (types.Hash{}) {
		t.Fatal("expected non-zero state root")
	}
}

func TestSnapshotGenesisStateDeterministic(t *testing.T) {
	alloc := TestnetGenesisAlloc()

	snap1 := SnapshotGenesisState(alloc)
	snap2 := SnapshotGenesisState(alloc)

	if snap1.Root != snap2.Root {
		t.Fatal("genesis state snapshot is not deterministic")
	}
	if snap1.TotalBalance.Cmp(snap2.TotalBalance) != 0 {
		t.Fatal("total balance mismatch between snapshots")
	}
}

func TestSnapshotGenesisStateEmpty(t *testing.T) {
	snap := SnapshotGenesisState(GenesisAlloc{})

	if snap.AccountCount != 0 {
		t.Fatalf("expected 0 accounts, got %d", snap.AccountCount)
	}
	if snap.TotalBalance.Sign() != 0 {
		t.Fatalf("expected zero total balance, got %s", snap.TotalBalance.String())
	}
	if snap.CodeAccounts != 0 {
		t.Fatalf("expected 0 code accounts, got %d", snap.CodeAccounts)
	}
}
