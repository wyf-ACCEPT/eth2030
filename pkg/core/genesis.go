package core

import (
	"encoding/json"
	"math/big"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// GenesisAccount represents an account in the genesis allocation.
type GenesisAccount struct {
	Balance *big.Int
	Code    []byte
	Nonce   uint64
	Storage map[types.Hash]types.Hash
}

// GenesisAlloc is the genesis allocation map: address -> account.
type GenesisAlloc map[types.Address]GenesisAccount

// Genesis specifies the header fields and pre-funded accounts of a genesis block.
type Genesis struct {
	Config     *ChainConfig
	Nonce      uint64
	Timestamp  uint64
	ExtraData  []byte
	GasLimit   uint64
	Difficulty *big.Int
	MixHash    types.Hash
	Coinbase   types.Address
	Alloc      GenesisAlloc

	// Optional overrides for consensus tests.
	Number        uint64
	GasUsed       uint64
	ParentHash    types.Hash
	BaseFee       *big.Int
	ExcessBlobGas *uint64
	BlobGasUsed   *uint64
}

// ToBlock creates a genesis block from the spec.
func (g *Genesis) ToBlock() *types.Block {
	head := &types.Header{
		ParentHash:  g.ParentHash,
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    g.Coinbase,
		Root:        types.EmptyRootHash,
		TxHash:      types.EmptyRootHash,
		ReceiptHash: types.EmptyRootHash,
		Difficulty:  g.Difficulty,
		Number:      new(big.Int).SetUint64(g.Number),
		GasLimit:    g.GasLimit,
		GasUsed:     g.GasUsed,
		Time:        g.Timestamp,
		MixDigest:   g.MixHash,
		Nonce:       types.BlockNonce{},
	}

	// Encode nonce into BlockNonce (big-endian).
	if g.Nonce != 0 {
		for i := 7; i >= 0; i-- {
			head.Nonce[i] = byte(g.Nonce)
			g.Nonce >>= 8
		}
	}

	if len(g.ExtraData) > 0 {
		head.Extra = make([]byte, len(g.ExtraData))
		copy(head.Extra, g.ExtraData)
	}

	if g.Difficulty == nil {
		head.Difficulty = new(big.Int)
	}

	// EIP-1559: set base fee for London+ chains.
	if g.BaseFee != nil {
		head.BaseFee = new(big.Int).Set(g.BaseFee)
	} else if g.Config != nil && g.Config.IsLondon(head.Number) {
		head.BaseFee = new(big.Int).SetUint64(1_000_000_000) // 1 gwei initial base fee
	}

	// Shanghai: empty withdrawals hash.
	if g.Config != nil && g.Config.IsShanghai(g.Timestamp) {
		emptyWithdrawalsHash := types.EmptyRootHash
		head.WithdrawalsHash = &emptyWithdrawalsHash
	}

	// Cancun: blob gas fields.
	if g.Config != nil && g.Config.IsCancun(g.Timestamp) {
		if g.ExcessBlobGas != nil {
			ebg := *g.ExcessBlobGas
			head.ExcessBlobGas = &ebg
		} else {
			zero := uint64(0)
			head.ExcessBlobGas = &zero
		}
		if g.BlobGasUsed != nil {
			bgu := *g.BlobGasUsed
			head.BlobGasUsed = &bgu
		} else {
			zero := uint64(0)
			head.BlobGasUsed = &zero
		}
		emptyRoot := types.EmptyRootHash
		head.ParentBeaconRoot = &emptyRoot
	}

	// Prague: requests hash.
	if g.Config != nil && g.Config.IsPrague(g.Timestamp) {
		emptyHash := types.EmptyRootHash
		head.RequestsHash = &emptyHash
	}

	// Glamsterdan / EIP-7706: calldata gas fields.
	if g.Config != nil && g.Config.IsGlamsterdan(g.Timestamp) {
		zero := uint64(0)
		head.CalldataGasUsed = &zero
		zero2 := uint64(0)
		head.CalldataExcessGas = &zero2
	}

	return types.NewBlock(head, nil)
}

// SetupGenesisBlock initializes a genesis block's state. It applies the genesis
// allocation (balances, code, nonces, storage) to the given state and returns
// the genesis block with its state root set.
func (g *Genesis) SetupGenesisBlock(statedb *state.MemoryStateDB) *types.Block {
	// Apply genesis alloc to state.
	for addr, account := range g.Alloc {
		statedb.CreateAccount(addr)
		if account.Balance != nil {
			statedb.AddBalance(addr, account.Balance)
		}
		if account.Nonce > 0 {
			statedb.SetNonce(addr, account.Nonce)
		}
		if len(account.Code) > 0 {
			statedb.SetCode(addr, account.Code)
		}
		for key, val := range account.Storage {
			statedb.SetState(addr, key, val)
		}
	}

	// Compute the state root from the genesis state.
	stateRoot := statedb.GetRoot()

	// Build the genesis block with the computed state root.
	block := g.ToBlock()
	header := block.Header()
	header.Root = stateRoot
	return types.NewBlock(header, block.Body())
}

// CommitGenesis initializes the database with the genesis block and state.
// Returns the initialized blockchain.
func (g *Genesis) CommitGenesis(db rawdb.Database) (*Blockchain, error) {
	statedb := state.NewMemoryStateDB()
	block := g.SetupGenesisBlock(statedb)

	config := g.Config
	if config == nil {
		config = TestConfig
	}

	bc, err := NewBlockchain(config, block, statedb, db)
	if err != nil {
		return nil, err
	}

	// Store genesis config as JSON in rawdb.
	configData, err := json.Marshal(config)
	if err == nil {
		db.Put([]byte("eth2028-chain-config"), configData)
	}

	return bc, nil
}

// DefaultGenesisBlock returns the mainnet genesis specification.
func DefaultGenesisBlock() *Genesis {
	return &Genesis{
		Config:     MainnetConfig,
		Nonce:      66,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(17_179_869_184),
		Alloc:      GenesisAlloc{},
	}
}

// DefaultSepoliaGenesisBlock returns the Sepolia testnet genesis specification.
func DefaultSepoliaGenesisBlock() *Genesis {
	return &Genesis{
		Config:     SepoliaConfig,
		Nonce:      0,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Timestamp:  1633267481,
		ExtraData:  []byte("Sepolia, Athens, Attica, Greece!"),
		Alloc:      GenesisAlloc{},
	}
}

// DefaultHoleskyGenesisBlock returns the Holesky testnet genesis specification.
func DefaultHoleskyGenesisBlock() *Genesis {
	return &Genesis{
		Config:     HoleskyConfig,
		Nonce:      0,
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Timestamp:  1695902400,
		Alloc:      GenesisAlloc{},
	}
}
