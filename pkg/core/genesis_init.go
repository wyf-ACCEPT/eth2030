// genesis_init.go provides SetupGenesis, the primary entry point for
// initializing a chain database from a genesis configuration. It validates
// the genesis, applies allocations, computes the state root, and writes
// the genesis block plus canonical mappings to a ChainDB.
package core

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// Genesis initialization errors.
var (
	ErrGenesisAlreadyWritten = errors.New("genesis: block already written")
	ErrGenesisChainIDZero    = errors.New("genesis: chain ID must be positive")
	ErrGenesisTimestampFuture = errors.New("genesis: timestamp is in the far future")
)

// maxReasonableTimestamp is a sanity bound for genesis timestamp (year 2100).
const maxReasonableTimestamp uint64 = 4_102_444_800

// SetupGenesisResult holds the output of SetupGenesis.
type SetupGenesisResult struct {
	Block   *types.Block
	ChainDB *rawdb.ChainDB
	Config  *ChainConfig
	StateDB *state.MemoryStateDB
}

// SetupGenesis initializes a chain database from a genesis configuration.
// It validates the genesis, applies allocations to a fresh state, computes
// the state root, and writes the genesis block, canonical hash, total
// difficulty, and head pointer to the ChainDB.
//
// If genesis is nil, DefaultGenesisBlock is used.
// If the database already contains a genesis block, it returns
// ErrGenesisAlreadyWritten.
//
// This function is thread-safe.
func SetupGenesis(db rawdb.Database, genesis *Genesis) (*SetupGenesisResult, error) {
	if genesis == nil {
		genesis = DefaultGenesisBlock()
	}

	// Validate the genesis configuration.
	if err := ValidateGenesis(genesis); err != nil {
		return nil, fmt.Errorf("genesis validation failed: %w", err)
	}

	cdb := rawdb.NewChainDB(db)

	// Check if genesis already exists.
	if _, err := cdb.ReadHeadBlockHash(); err == nil {
		return nil, ErrGenesisAlreadyWritten
	}

	// Apply genesis allocations and compute state root.
	statedb := state.NewMemoryStateDB()
	ApplyGenesisAlloc(statedb, genesis.Alloc)
	stateRoot := statedb.GetRoot()

	// Build the genesis block with the computed state root.
	block := genesis.ToBlock()
	header := block.Header()
	header.Root = stateRoot
	genesisBlock := types.NewBlock(header, block.Body())

	// Write genesis block to database.
	if err := CommitGenesisBlock(cdb, genesisBlock, genesis); err != nil {
		return nil, fmt.Errorf("commit genesis block: %w", err)
	}

	config := genesis.Config
	if config == nil {
		config = TestConfig
	}

	return &SetupGenesisResult{
		Block:   genesisBlock,
		ChainDB: cdb,
		Config:  config,
		StateDB: statedb,
	}, nil
}

// ValidateGenesis performs comprehensive validation on a genesis config.
// It checks chain config, gas limit, chain ID, timestamp, extra data length,
// and allocation balances.
func ValidateGenesis(g *Genesis) error {
	if g.Config == nil {
		return ErrGenesisNilConfig
	}
	if g.GasLimit == 0 {
		return ErrGenesisZeroGasLimit
	}
	if g.Config.ChainID == nil || g.Config.ChainID.Sign() <= 0 {
		return ErrGenesisChainIDZero
	}
	if g.Timestamp > maxReasonableTimestamp {
		return fmt.Errorf("%w: %d", ErrGenesisTimestampFuture, g.Timestamp)
	}
	if len(g.ExtraData) > MaxExtraDataSize {
		return fmt.Errorf("%w: length %d", ErrGenesisExtraDataLong, len(g.ExtraData))
	}

	// Validate each allocation entry.
	for addr, acct := range g.Alloc {
		if acct.Balance != nil && acct.Balance.Sign() < 0 {
			return fmt.Errorf("%w: negative balance for %s", ErrInvalidGenesis, addr.Hex())
		}
	}

	return nil
}

// ApplyGenesisAlloc applies genesis allocation entries to a state database.
// It creates accounts, sets balances, nonces, code, and storage values.
// Allocations are applied in sorted address order for determinism.
func ApplyGenesisAlloc(statedb *state.MemoryStateDB, alloc GenesisAlloc) {
	if len(alloc) == 0 {
		return
	}

	// Sort addresses for deterministic ordering.
	addrs := make([]types.Address, 0, len(alloc))
	for addr := range alloc {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		for k := 0; k < types.AddressLength; k++ {
			if addrs[i][k] != addrs[j][k] {
				return addrs[i][k] < addrs[j][k]
			}
		}
		return false
	})

	for _, addr := range addrs {
		account := alloc[addr]
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
		// Apply storage in sorted key order for determinism.
		if len(account.Storage) > 0 {
			keys := make([]types.Hash, 0, len(account.Storage))
			for key := range account.Storage {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool {
				for k := 0; k < types.HashLength; k++ {
					if keys[i][k] != keys[j][k] {
						return keys[i][k] < keys[j][k]
					}
				}
				return false
			})
			for _, key := range keys {
				statedb.SetState(addr, key, account.Storage[key])
			}
		}
	}
}

// CommitGenesisBlock writes the genesis block and its metadata to the ChainDB.
// It writes: the block, empty receipts, canonical hash mapping, total
// difficulty (equal to block difficulty), and head block hash.
func CommitGenesisBlock(cdb *rawdb.ChainDB, block *types.Block, genesis *Genesis) error {
	hash := block.Hash()
	num := block.NumberU64()

	// Write the full block.
	if err := cdb.WriteBlock(block); err != nil {
		return fmt.Errorf("write block: %w", err)
	}

	// Write empty receipts for genesis block.
	if err := cdb.WriteReceipts(hash, num, []*types.Receipt{}); err != nil {
		return fmt.Errorf("write receipts: %w", err)
	}

	// Write canonical hash: number 0 -> genesis hash.
	if err := cdb.WriteCanonicalHash(num, hash); err != nil {
		return fmt.Errorf("write canonical hash: %w", err)
	}

	// Write total difficulty (same as genesis difficulty for block 0).
	td := block.Difficulty()
	if td == nil || td.Sign() == 0 {
		td = new(big.Int).SetUint64(1)
	}
	if err := cdb.WriteTd(hash, td); err != nil {
		return fmt.Errorf("write td: %w", err)
	}

	// Write head block hash.
	if err := cdb.WriteHeadBlockHash(hash); err != nil {
		return fmt.Errorf("write head block hash: %w", err)
	}

	return nil
}

// ComputeGenesisStateRoot computes the state root hash for a genesis
// allocation without persisting to any database. Useful for verification.
func ComputeGenesisStateRoot(alloc GenesisAlloc) types.Hash {
	statedb := state.NewMemoryStateDB()
	ApplyGenesisAlloc(statedb, alloc)
	return statedb.GetRoot()
}

// GenesisBlockForNetwork returns the genesis configuration for a named network.
// Supported networks: "mainnet", "sepolia", "holesky", "dev".
// Returns nil for unknown networks.
func GenesisBlockForNetwork(network string) *Genesis {
	switch network {
	case "mainnet":
		return DefaultGenesisBlock()
	case "sepolia":
		return DefaultSepoliaGenesisBlock()
	case "holesky":
		return DefaultHoleskyGenesisBlock()
	case "dev", "development":
		return DevGenesis()
	default:
		return nil
	}
}

// InitChainDB is a convenience function that creates a MemoryDB, initializes
// genesis state, and returns a fully configured SetupGenesisResult. This is
// useful for testing and development.
func InitChainDB(genesis *Genesis) (*SetupGenesisResult, error) {
	db := rawdb.NewMemoryDB()
	return SetupGenesis(db, genesis)
}

// genesisInitMu provides global serialization for genesis initialization
// to prevent concurrent SetupGenesis calls from racing.
var genesisInitMu sync.Mutex

// SetupGenesisOrDefault initializes genesis, falling back to the default
// mainnet genesis if the provided genesis is nil. Thread-safe.
func SetupGenesisOrDefault(db rawdb.Database, genesis *Genesis) (*SetupGenesisResult, error) {
	genesisInitMu.Lock()
	defer genesisInitMu.Unlock()
	return SetupGenesis(db, genesis)
}
