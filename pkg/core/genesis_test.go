package core

import (
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

func TestGenesisToBlock(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		Nonce:      42,
		Timestamp:  1000,
		GasLimit:   5_000_000,
		Difficulty: big.NewInt(100),
		ExtraData:  []byte("test genesis"),
	}
	block := g.ToBlock()

	if block.NumberU64() != 0 {
		t.Errorf("genesis block number = %d, want 0", block.NumberU64())
	}
	if block.GasLimit() != 5_000_000 {
		t.Errorf("gas limit = %d, want 5000000", block.GasLimit())
	}
	if block.Time() != 1000 {
		t.Errorf("timestamp = %d, want 1000", block.Time())
	}
	if block.Difficulty().Cmp(big.NewInt(100)) != 0 {
		t.Errorf("difficulty = %v, want 100", block.Difficulty())
	}
	if string(block.Extra()) != "test genesis" {
		t.Errorf("extra = %q, want %q", string(block.Extra()), "test genesis")
	}
	// TestConfig has London active, so base fee should be set.
	if block.BaseFee() == nil {
		t.Fatal("expected base fee to be set for London-active config")
	}
	if block.BaseFee().Cmp(new(big.Int).SetUint64(1_000_000_000)) != 0 {
		t.Errorf("base fee = %v, want 1000000000", block.BaseFee())
	}
}

func TestGenesisToBlockWithExplicitBaseFee(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   5_000_000,
		Difficulty: big.NewInt(1),
		BaseFee:    big.NewInt(42),
	}
	block := g.ToBlock()
	if block.BaseFee().Cmp(big.NewInt(42)) != 0 {
		t.Errorf("base fee = %v, want 42", block.BaseFee())
	}
}

func TestGenesisToBlockShanghaiFields(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	block := g.ToBlock()
	header := block.Header()
	if header.WithdrawalsHash == nil {
		t.Fatal("expected withdrawals hash to be set for Shanghai-active config")
	}
}

func TestGenesisToBlockCancunFields(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	block := g.ToBlock()
	header := block.Header()
	if header.ExcessBlobGas == nil {
		t.Fatal("expected excess blob gas to be set for Cancun-active config")
	}
	if *header.ExcessBlobGas != 0 {
		t.Errorf("excess blob gas = %d, want 0", *header.ExcessBlobGas)
	}
	if header.BlobGasUsed == nil {
		t.Fatal("expected blob gas used to be set for Cancun-active config")
	}
	if header.ParentBeaconRoot == nil {
		t.Fatal("expected parent beacon root to be set for Cancun-active config")
	}
}

func TestGenesisToBlockPragueFields(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	block := g.ToBlock()
	header := block.Header()
	if header.RequestsHash == nil {
		t.Fatal("expected requests hash to be set for Prague-active config")
	}
}

func TestGenesisWithAlloc(t *testing.T) {
	addr1 := types.HexToAddress("0x1000000000000000000000000000000000000001")
	addr2 := types.HexToAddress("0x2000000000000000000000000000000000000002")

	alloc := GenesisAlloc{
		addr1: GenesisAccount{
			Balance: big.NewInt(1_000_000_000),
			Nonce:   5,
		},
		addr2: GenesisAccount{
			Balance: big.NewInt(2_000_000_000),
			Code:    []byte{0x60, 0x00, 0x60, 0x00, 0xFD}, // PUSH1 0 PUSH1 0 REVERT
			Storage: map[types.Hash]types.Hash{
				types.HexToHash("0x01"): types.HexToHash("0xff"),
			},
		},
	}

	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc:      alloc,
	}

	// Verify alloc is stored correctly.
	if len(g.Alloc) != 2 {
		t.Fatalf("alloc length = %d, want 2", len(g.Alloc))
	}

	acct1 := g.Alloc[addr1]
	if acct1.Balance.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Errorf("addr1 balance = %v, want 1000000000", acct1.Balance)
	}
	if acct1.Nonce != 5 {
		t.Errorf("addr1 nonce = %d, want 5", acct1.Nonce)
	}

	acct2 := g.Alloc[addr2]
	if acct2.Balance.Cmp(big.NewInt(2_000_000_000)) != 0 {
		t.Errorf("addr2 balance = %v, want 2000000000", acct2.Balance)
	}
	if len(acct2.Code) != 5 {
		t.Errorf("addr2 code length = %d, want 5", len(acct2.Code))
	}
	if len(acct2.Storage) != 1 {
		t.Errorf("addr2 storage length = %d, want 1", len(acct2.Storage))
	}

	// ToBlock should still work with alloc set.
	block := g.ToBlock()
	if block.NumberU64() != 0 {
		t.Errorf("genesis block number = %d, want 0", block.NumberU64())
	}
}

func TestMainnetGenesis(t *testing.T) {
	g := DefaultGenesisBlock()

	if g.Config.ChainID.Int64() != 1 {
		t.Errorf("mainnet chain id = %d, want 1", g.Config.ChainID.Int64())
	}
	if g.GasLimit != 30_000_000 {
		t.Errorf("mainnet gas limit = %d, want 30000000", g.GasLimit)
	}
	if g.Nonce != 66 {
		t.Errorf("mainnet nonce = %d, want 66", g.Nonce)
	}
	if g.Difficulty.Cmp(big.NewInt(17_179_869_184)) != 0 {
		t.Errorf("mainnet difficulty = %v, want 17179869184", g.Difficulty)
	}

	block := g.ToBlock()
	if block.NumberU64() != 0 {
		t.Errorf("mainnet genesis number = %d, want 0", block.NumberU64())
	}
}

func TestSepoliaGenesis(t *testing.T) {
	g := DefaultSepoliaGenesisBlock()

	if g.Config.ChainID.Int64() != 11155111 {
		t.Errorf("sepolia chain id = %d, want 11155111", g.Config.ChainID.Int64())
	}
	if g.GasLimit != 30_000_000 {
		t.Errorf("sepolia gas limit = %d, want 30000000", g.GasLimit)
	}
	if g.Timestamp != 1633267481 {
		t.Errorf("sepolia timestamp = %d, want 1633267481", g.Timestamp)
	}
	if string(g.ExtraData) != "Sepolia, Athens, Attica, Greece!" {
		t.Errorf("sepolia extra data = %q, want %q", string(g.ExtraData), "Sepolia, Athens, Attica, Greece!")
	}
}

func TestHoleskyGenesis(t *testing.T) {
	g := DefaultHoleskyGenesisBlock()

	if g.Config.ChainID.Int64() != 17000 {
		t.Errorf("holesky chain id = %d, want 17000", g.Config.ChainID.Int64())
	}
	if g.GasLimit != 30_000_000 {
		t.Errorf("holesky gas limit = %d, want 30000000", g.GasLimit)
	}
	if g.Timestamp != 1695902400 {
		t.Errorf("holesky timestamp = %d, want 1695902400", g.Timestamp)
	}
}

func TestChainConfigForkChecks(t *testing.T) {
	// Mainnet block-number forks
	cfg := MainnetConfig

	// Before Homestead
	if cfg.IsHomestead(big.NewInt(1_000_000)) {
		t.Error("block 1M should not be Homestead")
	}
	// At Homestead
	if !cfg.IsHomestead(big.NewInt(1_150_000)) {
		t.Error("block 1.15M should be Homestead")
	}
	// After Homestead
	if !cfg.IsHomestead(big.NewInt(2_000_000)) {
		t.Error("block 2M should be Homestead")
	}

	// Byzantium
	if cfg.IsByzantium(big.NewInt(4_000_000)) {
		t.Error("block 4M should not be Byzantium")
	}
	if !cfg.IsByzantium(big.NewInt(4_370_000)) {
		t.Error("block 4.37M should be Byzantium")
	}

	// Constantinople/Petersburg
	if !cfg.IsConstantinople(big.NewInt(7_280_000)) {
		t.Error("block 7.28M should be Constantinople")
	}
	if !cfg.IsPetersburg(big.NewInt(7_280_000)) {
		t.Error("block 7.28M should be Petersburg")
	}

	// Istanbul
	if !cfg.IsIstanbul(big.NewInt(9_069_000)) {
		t.Error("block 9.069M should be Istanbul")
	}
	if cfg.IsIstanbul(big.NewInt(9_068_999)) {
		t.Error("block 9.068999M should not be Istanbul")
	}

	// Berlin
	if !cfg.IsBerlin(big.NewInt(12_244_000)) {
		t.Error("block 12.244M should be Berlin")
	}

	// London
	if !cfg.IsLondon(big.NewInt(12_965_000)) {
		t.Error("block 12.965M should be London")
	}
	if cfg.IsLondon(big.NewInt(12_964_999)) {
		t.Error("block 12.964999M should not be London")
	}

	// EIP-specific checks
	if !cfg.IsEIP155(big.NewInt(2_675_000)) {
		t.Error("block 2.675M should be EIP-155")
	}
	if !cfg.IsEIP1559(big.NewInt(12_965_000)) {
		t.Error("block 12.965M should be EIP-1559 (London)")
	}
	if !cfg.IsEIP2929(big.NewInt(12_244_000)) {
		t.Error("block 12.244M should be EIP-2929 (Berlin)")
	}
	if !cfg.IsEIP3529(big.NewInt(12_965_000)) {
		t.Error("block 12.965M should be EIP-3529 (London)")
	}

	// Merge
	if !cfg.IsMerge() {
		t.Error("mainnet should have TTD set")
	}

	// Timestamp-based forks
	if !cfg.IsShanghai(1681338455) {
		t.Error("should be Shanghai at timestamp 1681338455")
	}
	if cfg.IsShanghai(1681338454) {
		t.Error("should not be Shanghai at timestamp 1681338454")
	}
	if !cfg.IsCancun(1710338135) {
		t.Error("should be Cancun at timestamp 1710338135")
	}
	if !cfg.IsEIP4844(1710338135) {
		t.Error("should be EIP-4844 at Cancun timestamp")
	}

	// Nil block number: should return false for all block forks
	if cfg.IsHomestead(nil) {
		t.Error("nil block should not be Homestead")
	}
	if cfg.IsLondon(nil) {
		t.Error("nil block should not be London")
	}
}

func TestChainConfigForkChecksTestnet(t *testing.T) {
	// Testnet configs have all block forks at 0.
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
	}{
		{"Sepolia", SepoliaConfig},
		{"Holesky", HoleskyConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.cfg.IsHomestead(big.NewInt(0)) {
				t.Error("testnet should be Homestead at block 0")
			}
			if !tc.cfg.IsLondon(big.NewInt(0)) {
				t.Error("testnet should be London at block 0")
			}
			if !tc.cfg.IsMerge() {
				t.Error("testnet should have TTD set")
			}
		})
	}
}

func TestRules(t *testing.T) {
	// All forks active.
	rules := TestConfig.Rules(big.NewInt(0), true, 0)

	if rules.ChainID.Int64() != 1337 {
		t.Errorf("rules chain id = %d, want 1337", rules.ChainID.Int64())
	}
	if !rules.IsHomestead {
		t.Error("expected IsHomestead in rules")
	}
	if !rules.IsEIP155 {
		t.Error("expected IsEIP155 in rules")
	}
	if !rules.IsByzantium {
		t.Error("expected IsByzantium in rules")
	}
	if !rules.IsConstantinople {
		t.Error("expected IsConstantinople in rules")
	}
	if !rules.IsPetersburg {
		t.Error("expected IsPetersburg in rules")
	}
	if !rules.IsIstanbul {
		t.Error("expected IsIstanbul in rules")
	}
	if !rules.IsBerlin {
		t.Error("expected IsBerlin in rules")
	}
	if !rules.IsEIP2929 {
		t.Error("expected IsEIP2929 in rules")
	}
	if !rules.IsLondon {
		t.Error("expected IsLondon in rules")
	}
	if !rules.IsEIP1559 {
		t.Error("expected IsEIP1559 in rules")
	}
	if !rules.IsEIP3529 {
		t.Error("expected IsEIP3529 in rules")
	}
	if !rules.IsMerge {
		t.Error("expected IsMerge in rules")
	}
	if !rules.IsShanghai {
		t.Error("expected IsShanghai in rules")
	}
	if !rules.IsCancun {
		t.Error("expected IsCancun in rules")
	}
	if !rules.IsEIP4844 {
		t.Error("expected IsEIP4844 in rules")
	}
	if !rules.IsPrague {
		t.Error("expected IsPrague in rules")
	}
	if !rules.IsEIP7702 {
		t.Error("expected IsEIP7702 in rules")
	}
	if !rules.IsAmsterdam {
		t.Error("expected IsAmsterdam in rules")
	}
}

func TestRulesPreMerge(t *testing.T) {
	// Passing isMerge=false: timestamp forks should not be active.
	rules := TestConfig.Rules(big.NewInt(0), false, 0)

	if !rules.IsLondon {
		t.Error("expected IsLondon even without merge")
	}
	if rules.IsMerge {
		t.Error("isMerge should be false")
	}
	if rules.IsShanghai {
		t.Error("IsShanghai should be false pre-merge")
	}
	if rules.IsCancun {
		t.Error("IsCancun should be false pre-merge")
	}
	if rules.IsPrague {
		t.Error("IsPrague should be false pre-merge")
	}
}

func TestRulesMergeRequiresLondon(t *testing.T) {
	// A config where London is not active: merge should be disallowed.
	cfg := &ChainConfig{
		ChainID:                 big.NewInt(1),
		LondonBlock:             big.NewInt(100),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
	}
	rules := cfg.Rules(big.NewInt(50), true, 0)
	if rules.IsMerge {
		t.Error("merge should not be active before London")
	}
	if rules.IsShanghai {
		t.Error("Shanghai should not be active if merge is not active")
	}
}

func TestPetersburgNilFallback(t *testing.T) {
	// Petersburg nil -> falls back to Constantinople block.
	cfg := &ChainConfig{
		ChainID:             big.NewInt(1),
		ConstantinopleBlock: big.NewInt(100),
		PetersburgBlock:     nil,
	}
	if !cfg.IsPetersburg(big.NewInt(100)) {
		t.Error("Petersburg should activate at Constantinople block when nil")
	}
	if cfg.IsPetersburg(big.NewInt(99)) {
		t.Error("Petersburg should not be active before Constantinople")
	}
}

func TestSetupGenesisBlock(t *testing.T) {
	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")

	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc: GenesisAlloc{
			addr1: GenesisAccount{
				Balance: big.NewInt(1e18),
				Nonce:   5,
			},
			addr2: GenesisAccount{
				Balance: big.NewInt(2e18),
				Code:    []byte{0x60, 0x00, 0xf3}, // PUSH1 0 RETURN
				Storage: map[types.Hash]types.Hash{
					types.HexToHash("0x01"): types.HexToHash("0xff"),
				},
			},
		},
	}

	statedb := state.NewMemoryStateDB()
	block := g.SetupGenesisBlock(statedb)

	// Verify genesis block properties.
	if block.NumberU64() != 0 {
		t.Fatalf("genesis number = %d, want 0", block.NumberU64())
	}

	// Verify state was applied.
	if got := statedb.GetBalance(addr1); got.Cmp(big.NewInt(1e18)) != 0 {
		t.Errorf("addr1 balance = %v, want 1e18", got)
	}
	if got := statedb.GetNonce(addr1); got != 5 {
		t.Errorf("addr1 nonce = %d, want 5", got)
	}
	if got := statedb.GetBalance(addr2); got.Cmp(big.NewInt(2e18)) != 0 {
		t.Errorf("addr2 balance = %v, want 2e18", got)
	}
	if got := statedb.GetCode(addr2); len(got) != 3 {
		t.Errorf("addr2 code length = %d, want 3", len(got))
	}
	if got := statedb.GetState(addr2, types.HexToHash("0x01")); got != types.HexToHash("0xff") {
		t.Errorf("addr2 storage[0x01] = %v, want 0xff", got)
	}

	// State root should be non-empty.
	header := block.Header()
	if header.Root == (types.Hash{}) {
		t.Error("genesis state root should not be zero")
	}
}

func TestCommitGenesis(t *testing.T) {
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

	db := rawdb.NewMemoryDB()
	bc, err := g.CommitGenesis(db)
	if err != nil {
		t.Fatalf("CommitGenesis error: %v", err)
	}

	// Verify blockchain is initialized.
	if bc.Genesis().NumberU64() != 0 {
		t.Errorf("genesis number = %d, want 0", bc.Genesis().NumberU64())
	}
	if bc.CurrentBlock().NumberU64() != 0 {
		t.Errorf("current block = %d, want 0", bc.CurrentBlock().NumberU64())
	}

	// Verify state.
	st := bc.State()
	addr := types.HexToAddress("0xaaaa")
	if got := st.GetBalance(addr); got.Cmp(big.NewInt(1e18)) != 0 {
		t.Errorf("balance after CommitGenesis = %v, want 1e18", got)
	}
}

func TestNoForkConfig(t *testing.T) {
	// Config with no forks set: everything should be false.
	cfg := &ChainConfig{
		ChainID: big.NewInt(1),
	}
	if cfg.IsHomestead(big.NewInt(1_000_000)) {
		t.Error("should not be Homestead with nil HomesteadBlock")
	}
	if cfg.IsLondon(big.NewInt(1_000_000)) {
		t.Error("should not be London with nil LondonBlock")
	}
	if cfg.IsMerge() {
		t.Error("should not be merge with nil TTD")
	}
	if cfg.IsShanghai(1_000_000) {
		t.Error("should not be Shanghai with nil ShanghaiTime")
	}
}
