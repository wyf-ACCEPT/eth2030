package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestDefaultGenesis(t *testing.T) {
	g := DefaultGenesis()
	if g == nil {
		t.Fatal("DefaultGenesis returned nil")
	}
	if g.Config == nil {
		t.Fatal("DefaultGenesis config is nil")
	}
	if g.Config.ChainID.Int64() != 1 {
		t.Errorf("DefaultGenesis chain ID = %d, want 1", g.Config.ChainID.Int64())
	}
	if g.GasLimit != 30_000_000 {
		t.Errorf("DefaultGenesis gas limit = %d, want 30000000", g.GasLimit)
	}
	if g.Difficulty.Cmp(big.NewInt(17_179_869_184)) != 0 {
		t.Errorf("DefaultGenesis difficulty = %v, want 17179869184", g.Difficulty)
	}
}

func TestDevGenesis(t *testing.T) {
	g := DevGenesis()
	if g == nil {
		t.Fatal("DevGenesis returned nil")
	}
	if g.Config == nil {
		t.Fatal("DevGenesis config is nil")
	}
	if g.Config.ChainID.Int64() != 1337 {
		t.Errorf("DevGenesis chain ID = %d, want 1337", g.Config.ChainID.Int64())
	}
	if g.GasLimit != 30_000_000 {
		t.Errorf("DevGenesis gas limit = %d, want 30000000", g.GasLimit)
	}
	if len(g.Alloc) == 0 {
		t.Fatal("DevGenesis should have prefunded accounts")
	}
	if len(g.Alloc) != 5 {
		t.Errorf("DevGenesis alloc count = %d, want 5", len(g.Alloc))
	}

	// Verify a known dev address is prefunded.
	addr := types.HexToAddress("0x0000000000000000000000000000000000000001")
	acct, ok := g.Alloc[addr]
	if !ok {
		t.Fatal("DevGenesis missing account 0x01")
	}
	oneThousandETH := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	if acct.Balance.Cmp(oneThousandETH) != 0 {
		t.Errorf("DevGenesis 0x01 balance = %v, want %v", acct.Balance, oneThousandETH)
	}
}

func TestDevGenesisToBlock(t *testing.T) {
	g := DevGenesis()
	block := g.ToBlock()
	if block.NumberU64() != 0 {
		t.Errorf("dev genesis block number = %d, want 0", block.NumberU64())
	}
	if block.GasLimit() != 30_000_000 {
		t.Errorf("dev genesis gas limit = %d, want 30000000", block.GasLimit())
	}
	if string(block.Extra()) != "eth2028 dev genesis" {
		t.Errorf("dev genesis extra = %q, want %q", string(block.Extra()), "eth2028 dev genesis")
	}
}

func TestGenesisHashDeterministic(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}

	h1 := g.GenesisHash()
	h2 := g.GenesisHash()

	if h1 != h2 {
		t.Errorf("GenesisHash not deterministic: %s != %s", h1.Hex(), h2.Hex())
	}

	// Hash should be non-zero.
	if h1 == (types.Hash{}) {
		t.Error("GenesisHash returned zero hash")
	}
}

func TestGenesisHashDifferent(t *testing.T) {
	g1 := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	g2 := &Genesis{
		Config:     TestConfig,
		GasLimit:   31_000_000,
		Difficulty: big.NewInt(1),
	}

	h1 := g1.GenesisHash()
	h2 := g2.GenesisHash()

	if h1 == h2 {
		t.Error("different genesis configs should produce different hashes")
	}
}

func TestGenesisValidateValid(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		ExtraData:  []byte("short extra"),
	}
	if err := g.Validate(); err != nil {
		t.Errorf("valid genesis failed validation: %v", err)
	}
}

func TestGenesisValidateNilConfig(t *testing.T) {
	g := &Genesis{
		Config:   nil,
		GasLimit: 30_000_000,
	}
	err := g.Validate()
	if err != ErrGenesisNilConfig {
		t.Errorf("expected ErrGenesisNilConfig, got %v", err)
	}
}

func TestGenesisValidateZeroGasLimit(t *testing.T) {
	g := &Genesis{
		Config:   TestConfig,
		GasLimit: 0,
	}
	err := g.Validate()
	if err != ErrGenesisZeroGasLimit {
		t.Errorf("expected ErrGenesisZeroGasLimit, got %v", err)
	}
}

func TestGenesisValidateExtraDataTooLong(t *testing.T) {
	g := &Genesis{
		Config:    TestConfig,
		GasLimit:  30_000_000,
		ExtraData: make([]byte, 33),
	}
	err := g.Validate()
	if err == nil {
		t.Fatal("expected error for extra data too long")
	}
}

func TestGenesisValidateExtraDataExact32(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		ExtraData:  make([]byte, 32),
	}
	if err := g.Validate(); err != nil {
		t.Errorf("32-byte extra data should be valid: %v", err)
	}
}

func TestGenesisValidateNegativeBalance(t *testing.T) {
	g := &Genesis{
		Config:   TestConfig,
		GasLimit: 30_000_000,
		Alloc: GenesisAlloc{
			types.HexToAddress("0x01"): GenesisAccount{
				Balance: big.NewInt(-1),
			},
		},
	}
	err := g.Validate()
	if err == nil {
		t.Fatal("expected error for negative balance")
	}
}

func TestAllocTotal(t *testing.T) {
	g := &Genesis{
		Config:   TestConfig,
		GasLimit: 30_000_000,
		Alloc: GenesisAlloc{
			types.HexToAddress("0x01"): GenesisAccount{
				Balance: big.NewInt(1_000_000),
			},
			types.HexToAddress("0x02"): GenesisAccount{
				Balance: big.NewInt(2_000_000),
			},
			types.HexToAddress("0x03"): GenesisAccount{
				Balance: big.NewInt(3_000_000),
			},
		},
	}

	total := g.AllocTotal()
	expected := big.NewInt(6_000_000)
	if total.Cmp(expected) != 0 {
		t.Errorf("AllocTotal = %v, want %v", total, expected)
	}
}

func TestAllocTotalEmpty(t *testing.T) {
	g := &Genesis{
		Config:   TestConfig,
		GasLimit: 30_000_000,
		Alloc:    GenesisAlloc{},
	}

	total := g.AllocTotal()
	if total.Sign() != 0 {
		t.Errorf("AllocTotal of empty alloc = %v, want 0", total)
	}
}

func TestAllocTotalNilBalance(t *testing.T) {
	g := &Genesis{
		Config:   TestConfig,
		GasLimit: 30_000_000,
		Alloc: GenesisAlloc{
			types.HexToAddress("0x01"): GenesisAccount{
				Balance: big.NewInt(100),
			},
			types.HexToAddress("0x02"): GenesisAccount{
				Balance: nil,
			},
		},
	}

	total := g.AllocTotal()
	if total.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("AllocTotal = %v, want 100", total)
	}
}

func TestAllocTotalDevGenesis(t *testing.T) {
	g := DevGenesis()
	total := g.AllocTotal()

	// 5 accounts each with 1000 ETH = 5000 ETH.
	oneThousandETH := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	expected := new(big.Int).Mul(big.NewInt(5), oneThousandETH)
	if total.Cmp(expected) != 0 {
		t.Errorf("DevGenesis AllocTotal = %v, want %v", total, expected)
	}
}

func TestMustCommitNilStateDB(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc: GenesisAlloc{
			types.HexToAddress("0xaaaa"): GenesisAccount{
				Balance: big.NewInt(1e18),
			},
		},
	}

	block := g.MustCommit(nil)
	if block.NumberU64() != 0 {
		t.Errorf("MustCommit block number = %d, want 0", block.NumberU64())
	}

	// State root should be non-zero (alloc was applied).
	header := block.Header()
	if header.Root == (types.Hash{}) {
		t.Error("MustCommit state root should not be zero")
	}
}

func TestMustCommitPanic(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}

	// Passing an invalid type should panic.
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustCommit should panic with invalid stateDB type")
		}
	}()

	g.MustCommit("not a state db")
}

func TestVerifyGenesisHashMatch(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}

	expected := g.GenesisHash()
	if err := VerifyGenesisHash(g, expected); err != nil {
		t.Errorf("VerifyGenesisHash failed for matching hash: %v", err)
	}
}

func TestVerifyGenesisHashMismatch(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}

	wrongHash := types.Hash{0xff}
	err := VerifyGenesisHash(g, wrongHash)
	if err == nil {
		t.Fatal("expected error for hash mismatch")
	}
}

func TestGenesisBlockHashFunction(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}

	h1 := GenesisBlockHash(g)
	h2 := g.GenesisHash()

	if h1 != h2 {
		t.Errorf("GenesisBlockHash and GenesisHash disagree: %s != %s", h1.Hex(), h2.Hex())
	}
}
