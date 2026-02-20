// chain_reader_ext.go provides extended chain reader functionality including
// transaction and receipt lookups, chain traversal utilities, and a
// FullChainReader interface that wraps the base ChainReader with additional
// capabilities such as GetTransaction, GetReceipt, and GetTD operations.
package core

import (
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// FullChainReader extends ChainReader with transaction and receipt lookups,
// total difficulty queries, and chain metadata operations.
type FullChainReader interface {
	ChainReader
	GetTransaction(hash types.Hash) (*types.Transaction, types.Hash, uint64, uint64)
	GetReceipt(txHash types.Hash) (*types.Receipt, types.Hash, uint64, uint64)
	GetTotalDifficulty(hash types.Hash, number uint64) *big.Int
	GetCanonicalHash(number uint64) types.Hash
	ChainHeight() uint64
}

// MemoryFullChain is an in-memory FullChainReader implementation that stores
// blocks, receipts, and transaction indices. It wraps MemoryChain and adds
// receipt storage and transaction lookup capabilities.
type MemoryFullChain struct {
	mu           sync.RWMutex
	chain        *MemoryChain
	receipts     map[types.Hash][]*types.Receipt  // blockHash -> receipts
	txIndex      map[types.Hash]txLookup          // txHash -> location
	canonHashes  map[uint64]types.Hash            // number -> canonical hash
	tdCache      map[types.Hash]*big.Int          // blockHash -> total difficulty
}

// txLookup stores the location of a transaction in the chain for the
// MemoryFullChain. This is distinct from the TxLookupEntry in blockchain.go
// to avoid symbol conflicts.
type txLookup struct {
	blockHash   types.Hash
	blockNumber uint64
	txIndex     uint64
}

// NewMemoryFullChain creates a new empty in-memory FullChainReader.
func NewMemoryFullChain() *MemoryFullChain {
	return &MemoryFullChain{
		chain:       NewMemoryChain(),
		receipts:    make(map[types.Hash][]*types.Receipt),
		txIndex:     make(map[types.Hash]txLookup),
		canonHashes: make(map[uint64]types.Hash),
		tdCache:     make(map[types.Hash]*big.Int),
	}
}

// AddBlock adds a block and indexes its transactions.
func (mfc *MemoryFullChain) AddBlock(block *types.Block) {
	if block == nil {
		return
	}
	mfc.mu.Lock()
	defer mfc.mu.Unlock()

	mfc.chain.AddBlock(block)
	hash := block.Hash()
	num := block.NumberU64()
	mfc.canonHashes[num] = hash

	// Index transactions.
	for i, tx := range block.Transactions() {
		mfc.txIndex[tx.Hash()] = txLookup{
			blockHash:   hash,
			blockNumber: num,
			txIndex:     uint64(i),
		}
	}

	// Compute and cache total difficulty.
	var parentTD *big.Int
	if num > 0 {
		parentBlock := mfc.chain.GetBlockByNumber(num - 1)
		if parentBlock != nil {
			if cached, ok := mfc.tdCache[parentBlock.Hash()]; ok {
				parentTD = cached
			}
		}
	}
	if parentTD == nil {
		parentTD = new(big.Int)
	}
	blockDiff := block.Difficulty()
	if blockDiff == nil {
		blockDiff = new(big.Int)
	}
	td := new(big.Int).Add(parentTD, blockDiff)
	mfc.tdCache[hash] = td
}

// AddReceipts stores receipts for a block identified by its hash.
func (mfc *MemoryFullChain) AddReceipts(blockHash types.Hash, receipts []*types.Receipt) {
	mfc.mu.Lock()
	defer mfc.mu.Unlock()
	mfc.receipts[blockHash] = receipts
}

// GetHeader returns the header for the block with matching hash and number.
func (mfc *MemoryFullChain) GetHeader(hash types.Hash, number uint64) *types.Header {
	return mfc.chain.GetHeader(hash, number)
}

// GetHeaderByNumber returns the header at the given canonical number.
func (mfc *MemoryFullChain) GetHeaderByNumber(number uint64) *types.Header {
	return mfc.chain.GetHeaderByNumber(number)
}

// GetBlock returns the block with matching hash and number.
func (mfc *MemoryFullChain) GetBlock(hash types.Hash, number uint64) *types.Block {
	return mfc.chain.GetBlock(hash, number)
}

// GetBlockByNumber returns the canonical block at the given number.
func (mfc *MemoryFullChain) GetBlockByNumber(number uint64) *types.Block {
	return mfc.chain.GetBlockByNumber(number)
}

// CurrentBlock returns the current head block.
func (mfc *MemoryFullChain) CurrentBlock() *types.Block {
	return mfc.chain.CurrentBlock()
}

// CurrentHeader returns the header of the current head block.
func (mfc *MemoryFullChain) CurrentHeader() *types.Header {
	return mfc.chain.CurrentHeader()
}

// HasBlock reports whether the chain has a block with the given hash and number.
func (mfc *MemoryFullChain) HasBlock(hash types.Hash, number uint64) bool {
	return mfc.chain.HasBlock(hash, number)
}

// GetTransaction returns the transaction, its block hash, block number, and
// index within the block. Returns nil values if the transaction is not found.
func (mfc *MemoryFullChain) GetTransaction(hash types.Hash) (*types.Transaction, types.Hash, uint64, uint64) {
	mfc.mu.RLock()
	defer mfc.mu.RUnlock()

	lookup, ok := mfc.txIndex[hash]
	if !ok {
		return nil, types.Hash{}, 0, 0
	}

	block := mfc.chain.GetBlockByNumber(lookup.blockNumber)
	if block == nil {
		return nil, types.Hash{}, 0, 0
	}

	txs := block.Transactions()
	if int(lookup.txIndex) >= len(txs) {
		return nil, types.Hash{}, 0, 0
	}

	return txs[lookup.txIndex], lookup.blockHash, lookup.blockNumber, lookup.txIndex
}

// GetReceipt returns the receipt for a transaction, along with the block hash,
// block number, and receipt index. Returns nil if not found.
func (mfc *MemoryFullChain) GetReceipt(txHash types.Hash) (*types.Receipt, types.Hash, uint64, uint64) {
	mfc.mu.RLock()
	defer mfc.mu.RUnlock()

	lookup, ok := mfc.txIndex[txHash]
	if !ok {
		return nil, types.Hash{}, 0, 0
	}

	blockReceipts, ok := mfc.receipts[lookup.blockHash]
	if !ok {
		return nil, types.Hash{}, 0, 0
	}

	if int(lookup.txIndex) >= len(blockReceipts) {
		return nil, types.Hash{}, 0, 0
	}

	return blockReceipts[lookup.txIndex], lookup.blockHash, lookup.blockNumber, lookup.txIndex
}

// GetTotalDifficulty returns the total difficulty at the given block.
// Returns nil if the block is not found.
func (mfc *MemoryFullChain) GetTotalDifficulty(hash types.Hash, number uint64) *big.Int {
	mfc.mu.RLock()
	defer mfc.mu.RUnlock()

	// Verify block existence (hash must match the block at that number).
	block := mfc.chain.GetBlock(hash, number)
	if block == nil {
		return nil
	}

	if td, ok := mfc.tdCache[hash]; ok {
		return new(big.Int).Set(td)
	}
	return nil
}

// GetCanonicalHash returns the canonical block hash for the given number.
// Returns the zero hash if the number is not in the canonical chain.
func (mfc *MemoryFullChain) GetCanonicalHash(number uint64) types.Hash {
	mfc.mu.RLock()
	defer mfc.mu.RUnlock()
	return mfc.canonHashes[number]
}

// ChainHeight returns the block number of the current head.
// Returns 0 if the chain is empty.
func (mfc *MemoryFullChain) ChainHeight() uint64 {
	cur := mfc.chain.CurrentBlock()
	if cur == nil {
		return 0
	}
	return cur.NumberU64()
}

// BlockRange returns blocks in the range [start, end] inclusive from the
// canonical chain. Missing blocks in the range are skipped.
func (mfc *MemoryFullChain) BlockRange(start, end uint64) []*types.Block {
	if end < start {
		return nil
	}
	var blocks []*types.Block
	for n := start; n <= end; n++ {
		block := mfc.chain.GetBlockByNumber(n)
		if block != nil {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// Interface compliance assertions.
var _ FullChainReader = (*MemoryFullChain)(nil)
var _ ChainReader = (*MemoryFullChain)(nil)
