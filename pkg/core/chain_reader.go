package core

import (
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// ChainReader provides read-only access to the blockchain.
type ChainReader interface {
	GetHeader(hash types.Hash, number uint64) *types.Header
	GetHeaderByNumber(number uint64) *types.Header
	GetBlock(hash types.Hash, number uint64) *types.Block
	GetBlockByNumber(number uint64) *types.Block
	CurrentBlock() *types.Block
	CurrentHeader() *types.Header
	HasBlock(hash types.Hash, number uint64) bool
}

// MemoryChain is an in-memory ChainReader implementation for testing.
type MemoryChain struct {
	mu           sync.RWMutex
	blocksByNum  map[uint64]*types.Block
	blocksByHash map[types.Hash]*types.Block
	current      *types.Block
}

// NewMemoryChain creates a new empty in-memory chain.
func NewMemoryChain() *MemoryChain {
	return &MemoryChain{
		blocksByNum:  make(map[uint64]*types.Block),
		blocksByHash: make(map[types.Hash]*types.Block),
	}
}

// AddBlock adds a block to the in-memory chain, indexed by both number and
// hash. If no current block is set, the added block becomes the current head.
func (mc *MemoryChain) AddBlock(block *types.Block) {
	if block == nil {
		return
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()

	num := block.NumberU64()
	hash := block.Hash()
	mc.blocksByNum[num] = block
	mc.blocksByHash[hash] = block

	// Auto-advance head if this is the first block or extends the chain.
	if mc.current == nil || num > mc.current.NumberU64() {
		mc.current = block
	}
}

// SetCurrentBlock sets the head of the chain explicitly.
func (mc *MemoryChain) SetCurrentBlock(block *types.Block) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.current = block
}

// GetHeader returns the header for the block with matching hash and number.
func (mc *MemoryChain) GetHeader(hash types.Hash, number uint64) *types.Header {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	block := mc.blocksByNum[number]
	if block == nil {
		return nil
	}
	if block.Hash() != hash {
		return nil
	}
	return block.Header()
}

// GetHeaderByNumber returns the header for the block at the given number.
func (mc *MemoryChain) GetHeaderByNumber(number uint64) *types.Header {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	block := mc.blocksByNum[number]
	if block == nil {
		return nil
	}
	return block.Header()
}

// GetBlock returns the block with matching hash and number.
func (mc *MemoryChain) GetBlock(hash types.Hash, number uint64) *types.Block {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	block := mc.blocksByNum[number]
	if block == nil {
		return nil
	}
	if block.Hash() != hash {
		return nil
	}
	return block
}

// GetBlockByNumber returns the block at the given number.
func (mc *MemoryChain) GetBlockByNumber(number uint64) *types.Block {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.blocksByNum[number]
}

// CurrentBlock returns the current head block.
func (mc *MemoryChain) CurrentBlock() *types.Block {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.current
}

// CurrentHeader returns the header of the current head block.
func (mc *MemoryChain) CurrentHeader() *types.Header {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	if mc.current == nil {
		return nil
	}
	return mc.current.Header()
}

// HasBlock reports whether the chain contains a block with the given hash
// and number.
func (mc *MemoryChain) HasBlock(hash types.Hash, number uint64) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	block := mc.blocksByNum[number]
	if block == nil {
		return false
	}
	return block.Hash() == hash
}

// ChainIterator iterates over a range of blocks in a ChainReader.
type ChainIterator struct {
	reader  ChainReader
	start   uint64
	end     uint64
	current uint64
}

// NewChainIterator creates an iterator over blocks [start, end] inclusive.
func NewChainIterator(reader ChainReader, start, end uint64) *ChainIterator {
	return &ChainIterator{
		reader:  reader,
		start:   start,
		end:     end,
		current: start,
	}
}

// Next returns the next block in the range, or (nil, false) if exhausted.
func (it *ChainIterator) Next() (*types.Block, bool) {
	if it.current > it.end {
		return nil, false
	}
	block := it.reader.GetBlockByNumber(it.current)
	it.current++
	if block == nil {
		return nil, false
	}
	return block, true
}

// Reset restarts the iterator from the beginning.
func (it *ChainIterator) Reset() {
	it.current = it.start
}

// BlockCount returns the total number of blocks in the iteration range.
func (it *ChainIterator) BlockCount() uint64 {
	if it.end < it.start {
		return 0
	}
	return it.end - it.start + 1
}

// GetAncestor walks back from a given block to find an ancestor at a specific
// distance. Returns the ancestor's hash and number, or a zero hash and 0 if
// the ancestor cannot be found.
func GetAncestor(reader ChainReader, hash types.Hash, number, ancestor uint64) (types.Hash, uint64) {
	if ancestor == 0 {
		return hash, number
	}
	if ancestor > number {
		return types.Hash{}, 0
	}

	target := number - ancestor

	// Walk back through parent hashes.
	currentHash := hash
	currentNum := number
	for currentNum > target {
		header := reader.GetHeader(currentHash, currentNum)
		if header == nil {
			return types.Hash{}, 0
		}
		currentHash = header.ParentHash
		currentNum--
	}

	// Verify the target block exists.
	header := reader.GetHeader(currentHash, currentNum)
	if header == nil {
		return types.Hash{}, 0
	}
	return currentHash, currentNum
}

// GetTD returns a simplified total difficulty for the given block. It sums
// the difficulties of all blocks from genesis to the specified block number.
// Returns nil if the block is not found.
func GetTD(reader ChainReader, hash types.Hash, number uint64) *big.Int {
	block := reader.GetBlock(hash, number)
	if block == nil {
		return nil
	}

	td := new(big.Int)
	for i := uint64(0); i <= number; i++ {
		b := reader.GetBlockByNumber(i)
		if b == nil {
			// Gap in the chain; return what we have so far.
			return td
		}
		td.Add(td, b.Difficulty())
	}
	return td
}
