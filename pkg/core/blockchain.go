package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/rawdb"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

var (
	ErrNoGenesis      = errors.New("genesis block not provided")
	ErrGenesisExists  = errors.New("genesis already initialized")
	ErrBlockNotFound  = errors.New("block not found")
	ErrInvalidChain   = errors.New("invalid chain: blocks not contiguous")
	ErrFutureBlock2   = errors.New("block number too high")
	ErrStateNotFound  = errors.New("state not found for block")
)

// Blockchain manages the canonical chain of blocks, applying state
// transitions and persisting data to the underlying database.
type Blockchain struct {
	mu        sync.RWMutex
	config    *ChainConfig
	db        rawdb.Database
	hc        *HeaderChain
	processor *StateProcessor
	validator *BlockValidator

	// Block cache: hash -> block.
	blockCache map[types.Hash]*types.Block

	// Canonical number -> hash for quick lookups.
	canonCache map[uint64]types.Hash

	// Genesis state (used as base for re-execution).
	genesisState *state.MemoryStateDB

	// Current state after processing the head block.
	currentState *state.MemoryStateDB

	// The genesis block.
	genesis *types.Block

	// Current head block.
	currentBlock *types.Block
}

// NewBlockchain creates a new blockchain initialized with the given genesis block.
// The statedb should contain the genesis state (pre-funded accounts, etc.).
func NewBlockchain(config *ChainConfig, genesis *types.Block, statedb *state.MemoryStateDB, db rawdb.Database) (*Blockchain, error) {
	if genesis == nil {
		return nil, ErrNoGenesis
	}

	bc := &Blockchain{
		config:       config,
		db:           db,
		processor:    NewStateProcessor(config),
		validator:    NewBlockValidator(config),
		blockCache:   make(map[types.Hash]*types.Block),
		canonCache:   make(map[uint64]types.Hash),
		genesisState: statedb,
		currentState: statedb.Copy(),
		genesis:      genesis,
		currentBlock: genesis,
	}

	// Create HeaderChain from genesis header.
	bc.hc = NewHeaderChain(config, genesis.Header())

	// Store genesis in caches.
	hash := genesis.Hash()
	bc.blockCache[hash] = genesis
	bc.canonCache[genesis.NumberU64()] = hash

	// Persist genesis to rawdb.
	bc.writeBlock(genesis)
	rawdb.WriteCanonicalHash(db, genesis.NumberU64(), hash)
	rawdb.WriteHeadBlockHash(db, hash)
	rawdb.WriteHeadHeaderHash(db, hash)

	return bc, nil
}

// InsertBlock validates, executes, and inserts a single block.
func (bc *Blockchain) InsertBlock(block *types.Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.insertBlock(block)
}

// insertBlock is the internal insert without locking.
func (bc *Blockchain) insertBlock(block *types.Block) error {
	hash := block.Hash()

	// Skip if already known.
	if _, ok := bc.blockCache[hash]; ok {
		return nil
	}

	header := block.Header()

	// Find parent.
	parent := bc.blockCache[header.ParentHash]
	if parent == nil {
		return fmt.Errorf("%w: parent %v", ErrUnknownParent, header.ParentHash)
	}

	// Validate header against parent.
	parentHeader := parent.Header()
	if err := bc.validator.ValidateHeader(header, parentHeader); err != nil {
		return err
	}

	// Validate body.
	if err := bc.validator.ValidateBody(block); err != nil {
		return err
	}

	// Build state for execution by re-executing from genesis.
	statedb, err := bc.stateAt(parent)
	if err != nil {
		return fmt.Errorf("state at parent %d: %w", parent.NumberU64(), err)
	}

	// Execute transactions.
	_, err = bc.processor.Process(block, statedb)
	if err != nil {
		return fmt.Errorf("process block %d: %w", block.NumberU64(), err)
	}

	// Store in block cache.
	bc.blockCache[hash] = block

	// Update canonical chain if this extends the head.
	num := block.NumberU64()
	if num > bc.currentBlock.NumberU64() {
		bc.canonCache[num] = hash
		bc.currentBlock = block
		bc.currentState = statedb.(*state.MemoryStateDB)

		// Persist to rawdb.
		bc.writeBlock(block)
		rawdb.WriteCanonicalHash(bc.db, num, hash)
		rawdb.WriteHeadBlockHash(bc.db, hash)
		rawdb.WriteHeadHeaderHash(bc.db, hash)

		// Update header chain.
		bc.hc.InsertHeaders([]*types.Header{header})
	}

	return nil
}

// InsertChain inserts a chain of blocks sequentially.
// Blocks must be in ascending order but need not be contiguous with the head
// at the time of the call (though each must connect to its parent).
func (bc *Blockchain) InsertChain(blocks []*types.Block) (int, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	for i, block := range blocks {
		if err := bc.insertBlock(block); err != nil {
			return i, err
		}
	}
	return len(blocks), nil
}

// GetBlock retrieves a block by hash, or nil if not found.
func (bc *Blockchain) GetBlock(hash types.Hash) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.blockCache[hash]
}

// GetBlockByNumber retrieves the canonical block for a given number.
func (bc *Blockchain) GetBlockByNumber(number uint64) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	hash, ok := bc.canonCache[number]
	if !ok {
		return nil
	}
	return bc.blockCache[hash]
}

// CurrentBlock returns the head of the canonical chain.
func (bc *Blockchain) CurrentBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock
}

// HasBlock checks if a block with the given hash exists.
func (bc *Blockchain) HasBlock(hash types.Hash) bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	_, ok := bc.blockCache[hash]
	return ok
}

// SetHead rewinds the canonical chain to the given block number.
// Blocks above the target number are removed from the canonical index.
func (bc *Blockchain) SetHead(number uint64) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	target, ok := bc.canonCache[number]
	if !ok {
		return fmt.Errorf("%w: no canonical block at %d", ErrBlockNotFound, number)
	}

	// Remove canonical entries above target.
	current := bc.currentBlock.NumberU64()
	for n := current; n > number; n-- {
		if hash, ok := bc.canonCache[n]; ok {
			rawdb.DeleteCanonicalHash(bc.db, n)
			delete(bc.canonCache, n)
			// Remove from block cache too.
			delete(bc.blockCache, hash)
		}
	}

	// Set new head.
	bc.currentBlock = bc.blockCache[target]

	// Re-derive state by re-executing from genesis.
	statedb, err := bc.stateAt(bc.currentBlock)
	if err != nil {
		return fmt.Errorf("re-derive state at %d: %w", number, err)
	}
	bc.currentState = statedb.(*state.MemoryStateDB)

	// Update rawdb pointers.
	hash := bc.currentBlock.Hash()
	rawdb.WriteHeadBlockHash(bc.db, hash)
	rawdb.WriteHeadHeaderHash(bc.db, hash)

	// Rewind header chain.
	bc.hc.SetHead(number)

	return nil
}

// GetHashFn returns a GetHashFunc that resolves block number -> hash
// for the BLOCKHASH opcode (EIP-210 compatible, up to 256 blocks back).
func (bc *Blockchain) GetHashFn() func(uint64) types.Hash {
	return func(number uint64) types.Hash {
		bc.mu.RLock()
		defer bc.mu.RUnlock()
		if hash, ok := bc.canonCache[number]; ok {
			return hash
		}
		return types.Hash{}
	}
}

// Genesis returns the genesis block.
func (bc *Blockchain) Genesis() *types.Block {
	return bc.genesis
}

// Config returns the chain configuration.
func (bc *Blockchain) Config() *ChainConfig {
	return bc.config
}

// State returns a copy of the current state.
func (bc *Blockchain) State() *state.MemoryStateDB {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentState.Copy()
}

// stateAt returns the state after executing up to (and including) the given block.
// For the genesis block, this is the genesis state directly.
func (bc *Blockchain) stateAt(block *types.Block) (state.StateDB, error) {
	if block.Hash() == bc.genesis.Hash() {
		return bc.genesisState.Copy(), nil
	}

	// Collect the chain of blocks from genesis to this block.
	var chain []*types.Block
	current := block
	for current.Hash() != bc.genesis.Hash() {
		chain = append(chain, current)
		parent, ok := bc.blockCache[current.ParentHash()]
		if !ok {
			return nil, fmt.Errorf("%w: missing ancestor at %v", ErrStateNotFound, current.ParentHash())
		}
		current = parent
	}

	// Re-execute from genesis.
	statedb := bc.genesisState.Copy()
	for i := len(chain) - 1; i >= 0; i-- {
		b := chain[i]
		if _, err := bc.processor.Process(b, statedb); err != nil {
			return nil, fmt.Errorf("re-execute block %d: %w", b.NumberU64(), err)
		}
	}
	return statedb, nil
}

// writeBlock persists a block's header data to rawdb.
func (bc *Blockchain) writeBlock(block *types.Block) {
	num := block.NumberU64()
	hash := block.Hash()
	// Store a placeholder — full RLP serialization is left for later.
	rawdb.WriteHeader(bc.db, num, hash, []byte("header"))
	rawdb.WriteBody(bc.db, num, hash, []byte("body"))
}

// ChainLength returns the length of the canonical chain (genesis = 1).
func (bc *Blockchain) ChainLength() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock.NumberU64() + 1
}

// makeGenesis is a helper for creating a genesis block with the given gas limit and base fee.
func makeGenesis(gasLimit uint64, baseFee *big.Int) *types.Block {
	header := &types.Header{
		Number:     big.NewInt(0),
		GasLimit:   gasLimit,
		GasUsed:    0,
		Time:       0,
		Difficulty: new(big.Int),
		BaseFee:    baseFee,
		UncleHash:  EmptyUncleHash,
	}
	return types.NewBlock(header, nil)
}
