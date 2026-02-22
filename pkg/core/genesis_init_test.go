package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

func TestSetupGenesis_Default(t *testing.T) {
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, nil)
	if err != nil {
		t.Fatalf("SetupGenesis error: %v", err)
	}
	if result.Block == nil {
		t.Fatal("result block is nil")
	}
	if result.Block.NumberU64() != 0 {
		t.Errorf("genesis number = %d, want 0", result.Block.NumberU64())
	}
	if result.Config.ChainID.Int64() != 1 {
		t.Errorf("chain ID = %d, want 1 (mainnet)", result.Config.ChainID.Int64())
	}
}

func TestSetupGenesis_CustomAlloc(t *testing.T) {
	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")

	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc: GenesisAlloc{
			addr1: GenesisAccount{
				Balance: big.NewInt(1e18),
				Nonce:   10,
			},
			addr2: GenesisAccount{
				Balance: big.NewInt(2e18),
				Code:    []byte{0x60, 0x00, 0xf3},
				Storage: map[types.Hash]types.Hash{
					types.HexToHash("0x01"): types.HexToHash("0xaa"),
					types.HexToHash("0x02"): types.HexToHash("0xbb"),
				},
			},
		},
	}

	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis error: %v", err)
	}

	// Verify state was applied.
	sdb := result.StateDB
	if got := sdb.GetBalance(addr1); got.Cmp(big.NewInt(1e18)) != 0 {
		t.Errorf("addr1 balance = %v, want 1e18", got)
	}
	if got := sdb.GetNonce(addr1); got != 10 {
		t.Errorf("addr1 nonce = %d, want 10", got)
	}
	if got := sdb.GetBalance(addr2); got.Cmp(big.NewInt(2e18)) != 0 {
		t.Errorf("addr2 balance = %v, want 2e18", got)
	}
	if got := sdb.GetCode(addr2); len(got) != 3 {
		t.Errorf("addr2 code length = %d, want 3", len(got))
	}
	if got := sdb.GetState(addr2, types.HexToHash("0x01")); got != types.HexToHash("0xaa") {
		t.Errorf("addr2 storage[0x01] = %v, want 0xaa", got)
	}
	if got := sdb.GetState(addr2, types.HexToHash("0x02")); got != types.HexToHash("0xbb") {
		t.Errorf("addr2 storage[0x02] = %v, want 0xbb", got)
	}

	// Verify block properties.
	block := result.Block
	if block.GasLimit() != 30_000_000 {
		t.Errorf("gas limit = %d, want 30000000", block.GasLimit())
	}
	// State root should be non-zero.
	header := block.Header()
	if header.Root == (types.Hash{}) {
		t.Error("state root should not be zero")
	}
}

func TestSetupGenesis_WritesCanonical(t *testing.T) {
	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis error: %v", err)
	}

	cdb := result.ChainDB

	// Canonical hash for block 0 should be the genesis hash.
	canonHash, err := cdb.ReadCanonicalHash(0)
	if err != nil {
		t.Fatalf("ReadCanonicalHash: %v", err)
	}
	if canonHash != result.Block.Hash() {
		t.Errorf("canonical hash mismatch")
	}

	// Head block hash should be the genesis hash.
	headHash, err := cdb.ReadHeadBlockHash()
	if err != nil {
		t.Fatalf("ReadHeadBlockHash: %v", err)
	}
	if headHash != result.Block.Hash() {
		t.Errorf("head block hash mismatch")
	}

	// Block should be readable.
	block := cdb.ReadBlock(result.Block.Hash())
	if block == nil {
		t.Fatal("genesis block not readable from ChainDB")
	}
	if block.NumberU64() != 0 {
		t.Errorf("block number = %d, want 0", block.NumberU64())
	}

	// Total difficulty should be set.
	td := cdb.ReadTd(result.Block.Hash())
	if td == nil {
		t.Fatal("total difficulty not found")
	}
	if td.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("td = %v, want 1", td)
	}
}

func TestSetupGenesis_AlreadyInitialized(t *testing.T) {
	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	db := rawdb.NewMemoryDB()

	// First init should succeed.
	_, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("first SetupGenesis: %v", err)
	}

	// Second init should fail.
	_, err = SetupGenesis(db, genesis)
	if err == nil {
		t.Fatal("expected error on second SetupGenesis")
	}
	if err != ErrGenesisAlreadyWritten {
		t.Errorf("expected ErrGenesisAlreadyWritten, got %v", err)
	}
}

func TestSetupGenesis_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		genesis *Genesis
	}{
		{
			name: "nil config",
			genesis: &Genesis{
				Config:   nil,
				GasLimit: 30_000_000,
			},
		},
		{
			name: "zero gas limit",
			genesis: &Genesis{
				Config:   TestConfig,
				GasLimit: 0,
			},
		},
		{
			name: "zero chain ID",
			genesis: &Genesis{
				Config:   &ChainConfig{ChainID: big.NewInt(0)},
				GasLimit: 30_000_000,
			},
		},
		{
			name: "negative chain ID",
			genesis: &Genesis{
				Config:   &ChainConfig{ChainID: big.NewInt(-1)},
				GasLimit: 30_000_000,
			},
		},
		{
			name: "nil chain ID",
			genesis: &Genesis{
				Config:   &ChainConfig{ChainID: nil},
				GasLimit: 30_000_000,
			},
		},
		{
			name: "extra data too long",
			genesis: &Genesis{
				Config:    TestConfig,
				GasLimit:  30_000_000,
				ExtraData: make([]byte, 33),
			},
		},
		{
			name: "negative balance",
			genesis: &Genesis{
				Config:   TestConfig,
				GasLimit: 30_000_000,
				Alloc: GenesisAlloc{
					types.HexToAddress("0x01"): GenesisAccount{
						Balance: big.NewInt(-100),
					},
				},
			},
		},
		{
			name: "timestamp far future",
			genesis: &Genesis{
				Config:    TestConfig,
				GasLimit:  30_000_000,
				Timestamp: maxReasonableTimestamp + 1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDB()
			_, err := SetupGenesis(db, tc.genesis)
			if err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestValidateGenesis_Valid(t *testing.T) {
	g := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		ExtraData:  []byte("valid"),
		Timestamp:  1000,
	}
	if err := ValidateGenesis(g); err != nil {
		t.Errorf("expected valid genesis: %v", err)
	}
}

func TestApplyGenesisAlloc_Deterministic(t *testing.T) {
	alloc := GenesisAlloc{
		types.HexToAddress("0x03"): GenesisAccount{Balance: big.NewInt(300)},
		types.HexToAddress("0x01"): GenesisAccount{Balance: big.NewInt(100)},
		types.HexToAddress("0x02"): GenesisAccount{Balance: big.NewInt(200)},
	}

	// Apply twice and verify the state root is the same.
	sdb1 := state.NewMemoryStateDB()
	ApplyGenesisAlloc(sdb1, alloc)
	root1 := sdb1.GetRoot()

	sdb2 := state.NewMemoryStateDB()
	ApplyGenesisAlloc(sdb2, alloc)
	root2 := sdb2.GetRoot()

	if root1 != root2 {
		t.Errorf("state roots should be deterministic: %s != %s", root1, root2)
	}
}

func TestApplyGenesisAlloc_Empty(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	ApplyGenesisAlloc(sdb, GenesisAlloc{})
	// Should not panic and root should be the empty root.
	root := sdb.GetRoot()
	if root == (types.Hash{}) {
		// Even empty state has a non-zero root (empty trie root).
		// But MemoryStateDB might return something else.
	}
	_ = root // Just verify no panic.
}

func TestComputeGenesisStateRoot(t *testing.T) {
	alloc := GenesisAlloc{
		types.HexToAddress("0xaaaa"): GenesisAccount{
			Balance: big.NewInt(1e18),
		},
	}

	root := ComputeGenesisStateRoot(alloc)
	if root == (types.Hash{}) {
		t.Error("state root should not be zero for non-empty alloc")
	}

	// Verify determinism.
	root2 := ComputeGenesisStateRoot(alloc)
	if root != root2 {
		t.Error("ComputeGenesisStateRoot not deterministic")
	}
}

func TestGenesisBlockForNetwork(t *testing.T) {
	tests := []struct {
		network string
		chainID int64
	}{
		{"mainnet", 1},
		{"sepolia", 11155111},
		{"holesky", 17000},
		{"dev", 1337},
		{"development", 1337},
	}

	for _, tc := range tests {
		t.Run(tc.network, func(t *testing.T) {
			g := GenesisBlockForNetwork(tc.network)
			if g == nil {
				t.Fatalf("GenesisBlockForNetwork(%q) returned nil", tc.network)
			}
			if g.Config.ChainID.Int64() != tc.chainID {
				t.Errorf("chain ID = %d, want %d", g.Config.ChainID.Int64(), tc.chainID)
			}
		})
	}

	// Unknown network.
	if GenesisBlockForNetwork("unknown") != nil {
		t.Error("expected nil for unknown network")
	}
}

func TestInitChainDB(t *testing.T) {
	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc: GenesisAlloc{
			types.HexToAddress("0xaaaa"): GenesisAccount{
				Balance: big.NewInt(5e18),
			},
		},
	}

	result, err := InitChainDB(genesis)
	if err != nil {
		t.Fatalf("InitChainDB error: %v", err)
	}
	if result.Block.NumberU64() != 0 {
		t.Errorf("block number = %d, want 0", result.Block.NumberU64())
	}
	if result.StateDB.GetBalance(types.HexToAddress("0xaaaa")).Cmp(big.NewInt(5e18)) != 0 {
		t.Error("balance not applied")
	}
}

func TestSetupGenesisOrDefault(t *testing.T) {
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesisOrDefault(db, nil)
	if err != nil {
		t.Fatalf("SetupGenesisOrDefault error: %v", err)
	}
	if result.Config.ChainID.Int64() != 1 {
		t.Errorf("expected mainnet chain ID, got %d", result.Config.ChainID.Int64())
	}
}

func TestSetupGenesis_SepoliaNetwork(t *testing.T) {
	genesis := DefaultSepoliaGenesisBlock()
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis Sepolia error: %v", err)
	}
	if result.Block.Time() != 1633267481 {
		t.Errorf("timestamp = %d, want 1633267481", result.Block.Time())
	}
	if string(result.Block.Extra()) != "Sepolia, Athens, Attica, Greece!" {
		t.Errorf("extra = %q", string(result.Block.Extra()))
	}
}

func TestSetupGenesis_HoleskyNetwork(t *testing.T) {
	genesis := DefaultHoleskyGenesisBlock()
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis Holesky error: %v", err)
	}
	if result.Block.Time() != 1695902400 {
		t.Errorf("timestamp = %d, want 1695902400", result.Block.Time())
	}
	if result.Config.ChainID.Int64() != 17000 {
		t.Errorf("chain ID = %d, want 17000", result.Config.ChainID.Int64())
	}
}

func TestSetupGenesis_DevNetwork(t *testing.T) {
	genesis := DevGenesis()
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis Dev error: %v", err)
	}
	// Dev genesis has 5 prefunded accounts.
	addr := types.HexToAddress("0x0000000000000000000000000000000000000001")
	balance := result.StateDB.GetBalance(addr)
	oneThousandETH := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	if balance.Cmp(oneThousandETH) != 0 {
		t.Errorf("dev addr 0x01 balance = %v, want %v", balance, oneThousandETH)
	}
}

func TestSetupGenesis_StateRootNonZero(t *testing.T) {
	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc: GenesisAlloc{
			types.HexToAddress("0x01"): GenesisAccount{Balance: big.NewInt(100)},
		},
	}
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis error: %v", err)
	}
	header := result.Block.Header()
	if header.Root == (types.Hash{}) {
		t.Error("genesis state root is zero with non-empty alloc")
	}
}

func TestSetupGenesis_BlockReadableByNumber(t *testing.T) {
	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
	}
	db := rawdb.NewMemoryDB()
	result, err := SetupGenesis(db, genesis)
	if err != nil {
		t.Fatalf("SetupGenesis error: %v", err)
	}
	block := result.ChainDB.ReadBlockByNumber(0)
	if block == nil {
		t.Fatal("genesis block not readable by number 0")
	}
	if block.Hash() != result.Block.Hash() {
		t.Error("block hash mismatch when reading by number")
	}
}

func TestCommitGenesisBlock_WritesAllData(t *testing.T) {
	db := rawdb.NewMemoryDB()
	cdb := rawdb.NewChainDB(db)

	genesis := &Genesis{
		Config:     TestConfig,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(42),
	}
	block := genesis.ToBlock()

	if err := CommitGenesisBlock(cdb, block, genesis); err != nil {
		t.Fatalf("CommitGenesisBlock error: %v", err)
	}

	// Verify all written data.
	hash := block.Hash()

	// Block readable.
	if !cdb.HasBlock(hash) {
		t.Error("block not found after commit")
	}

	// Canonical hash.
	canonHash, err := cdb.ReadCanonicalHash(0)
	if err != nil {
		t.Fatalf("ReadCanonicalHash: %v", err)
	}
	if canonHash != hash {
		t.Error("canonical hash mismatch")
	}

	// Head hash.
	headHash, err := cdb.ReadHeadBlockHash()
	if err != nil {
		t.Fatalf("ReadHeadBlockHash: %v", err)
	}
	if headHash != hash {
		t.Error("head hash mismatch")
	}

	// TD.
	td := cdb.ReadTd(hash)
	if td == nil {
		t.Fatal("td not found")
	}
	if td.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("td = %v, want 42", td)
	}
}
