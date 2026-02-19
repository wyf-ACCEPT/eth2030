package core

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// Genesis validation errors.
var (
	ErrInvalidGenesis        = errors.New("genesis: invalid genesis configuration")
	ErrGenesisExtraDataLong  = errors.New("genesis: extra data exceeds 32 bytes")
	ErrGenesisZeroGasLimit   = errors.New("genesis: gas limit must be non-zero")
	ErrGenesisNilConfig      = errors.New("genesis: chain config is nil")
)

// Note: MaxExtraDataSize is defined in block_validator.go (= 32 bytes).

// DefaultGenesis returns a default mainnet genesis configuration.
// This is an alias for DefaultGenesisBlock for interface consistency.
func DefaultGenesis() *Genesis {
	return DefaultGenesisBlock()
}

// DevGenesis returns a development/test genesis configuration with prefunded
// accounts for local testing. It uses the test chain config with all forks
// active and allocates ether to well-known dev addresses.
func DevGenesis() *Genesis {
	// Prefund common dev addresses with 1000 ETH each.
	oneThousandETH := new(big.Int).Mul(big.NewInt(1000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))

	alloc := GenesisAlloc{
		types.HexToAddress("0x0000000000000000000000000000000000000001"): GenesisAccount{
			Balance: new(big.Int).Set(oneThousandETH),
		},
		types.HexToAddress("0x0000000000000000000000000000000000000002"): GenesisAccount{
			Balance: new(big.Int).Set(oneThousandETH),
		},
		types.HexToAddress("0x0000000000000000000000000000000000000003"): GenesisAccount{
			Balance: new(big.Int).Set(oneThousandETH),
		},
		types.HexToAddress("0x71562b71999567a775ef2404c3434aedf7e1b7f1"): GenesisAccount{
			Balance: new(big.Int).Set(oneThousandETH),
		},
		types.HexToAddress("0xdead000000000000000000000000000000000000"): GenesisAccount{
			Balance: new(big.Int).Set(oneThousandETH),
			Nonce:   1,
		},
	}

	return &Genesis{
		Config:     TestConfig,
		Nonce:      0,
		Timestamp:  0,
		ExtraData:  []byte("eth2028 dev genesis"),
		GasLimit:   30_000_000,
		Difficulty: big.NewInt(1),
		Alloc:      alloc,
	}
}

// GenesisHash computes the hash of the genesis block derived from this
// genesis configuration. It converts the genesis to a block and returns
// the block's header hash.
func (g *Genesis) GenesisHash() types.Hash {
	block := g.ToBlock()
	return block.Hash()
}

// Validate checks the genesis configuration for common errors. It returns
// nil if the genesis is valid, or the first error encountered.
func (g *Genesis) Validate() error {
	if g.Config == nil {
		return ErrGenesisNilConfig
	}
	if g.GasLimit == 0 {
		return ErrGenesisZeroGasLimit
	}
	if len(g.ExtraData) > MaxExtraDataSize {
		return fmt.Errorf("%w: length %d", ErrGenesisExtraDataLong, len(g.ExtraData))
	}

	// Validate alloc entries: no nil balances.
	for addr, acct := range g.Alloc {
		if acct.Balance != nil && acct.Balance.Sign() < 0 {
			return fmt.Errorf("%w: negative balance for %s", ErrInvalidGenesis, addr.Hex())
		}
	}

	return nil
}

// AllocTotal returns the sum of all balances in the genesis allocation.
// Accounts with nil balance contribute zero.
func (g *Genesis) AllocTotal() *big.Int {
	total := new(big.Int)
	for _, acct := range g.Alloc {
		if acct.Balance != nil {
			total.Add(total, acct.Balance)
		}
	}
	return total
}

// MustCommit applies the genesis allocation to a new in-memory state DB
// and returns the resulting genesis block with the state root set. It
// panics on error. The stateDB parameter is accepted as interface{} for
// flexibility but must be nil (a new MemoryStateDB will be created) or
// a *state.MemoryStateDB.
func (g *Genesis) MustCommit(stateDB interface{}) *types.Block {
	var sdb *state.MemoryStateDB
	if stateDB == nil {
		sdb = state.NewMemoryStateDB()
	} else {
		var ok bool
		sdb, ok = stateDB.(*state.MemoryStateDB)
		if !ok {
			panic("genesis: MustCommit requires nil or *state.MemoryStateDB")
		}
	}
	return g.SetupGenesisBlock(sdb)
}

// GenesisBlockHash computes the block hash for a genesis configuration by
// hashing the header of the genesis block using keccak256-based RLP encoding.
// This is a standalone function that does not require state application.
func GenesisBlockHash(g *Genesis) types.Hash {
	return g.GenesisHash()
}

// VerifyGenesisHash checks that a genesis configuration produces the expected
// block hash. Returns nil if matching, error otherwise.
func VerifyGenesisHash(g *Genesis, expected types.Hash) error {
	actual := g.GenesisHash()
	if actual != expected {
		return fmt.Errorf("%w: hash mismatch: got %s, want %s",
			ErrInvalidGenesis, actual.Hex(), expected.Hex())
	}
	return nil
}

