package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
)

var (
	ErrNoGenesis      = errors.New("genesis block not provided")
	ErrGenesisExists  = errors.New("genesis already initialized")
	ErrBlockNotFound  = errors.New("block not found")
	ErrInvalidChain   = errors.New("invalid chain: blocks not contiguous")
	ErrFutureBlock2   = errors.New("block number too high")
	ErrStateNotFound  = errors.New("state not found for block")
)

// TxLookupEntry stores the location of a transaction within the chain.
type TxLookupEntry struct {
	BlockHash   types.Hash
	BlockNumber uint64
	TxIndex     uint64
}

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

	// Receipt cache: blockHash -> receipts.
	receiptCache map[types.Hash][]*types.Receipt

	// Transaction lookup: txHash -> location in chain.
	txLookup map[types.Hash]TxLookupEntry

	// State snapshot cache to avoid re-execution from genesis.
	sc *stateCache

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

	proc := NewStateProcessor(config)
	bc := &Blockchain{
		config:       config,
		db:           db,
		processor:    proc,
		validator:    NewBlockValidator(config),
		blockCache:   make(map[types.Hash]*types.Block),
		canonCache:   make(map[uint64]types.Hash),
		receiptCache: make(map[types.Hash][]*types.Receipt),
		txLookup:     make(map[types.Hash]TxLookupEntry),
		sc:           newStateCache(),
		genesisState: statedb,
		currentState: statedb.Copy(),
		genesis:      genesis,
		currentBlock: genesis,
	}

	// Wire up GetHash for BLOCKHASH opcode support.
	proc.SetGetHash(bc.GetHashFn())

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

	// Execute transactions (with BAL tracking when Amsterdam is active).
	result, err := bc.processor.ProcessWithBAL(block, statedb)
	if err != nil {
		return fmt.Errorf("process block %d: %w", block.NumberU64(), err)
	}
	receipts := result.Receipts

	// Validate gas used: the total gas consumed must match header.GasUsed.
	var totalGasUsed uint64
	for _, r := range receipts {
		totalGasUsed += r.GasUsed
	}
	if header.GasUsed != totalGasUsed {
		return fmt.Errorf("%w: header=%d computed=%d", ErrInvalidGasUsedTotal, header.GasUsed, totalGasUsed)
	}

	// Validate receipt root: the Merkle trie hash of receipts must match
	// header.ReceiptHash.
	computedReceiptHash := computeReceiptsRoot(receipts)
	if header.ReceiptHash != computedReceiptHash {
		return fmt.Errorf("%w: header=%s computed=%s", ErrInvalidReceiptRoot,
			header.ReceiptHash.Hex(), computedReceiptHash.Hex())
	}

	// Validate block bloom: the bloom in the header must match the computed
	// bloom from all receipt logs.
	blockBloom := types.CreateBloom(receipts)
	if header.Bloom != blockBloom {
		return fmt.Errorf("invalid bloom (remote: %x local: %x)", header.Bloom, blockBloom)
	}

	// EIP-7685: process execution layer requests (Prague+).
	// ProcessRequests may modify state (e.g. clearing request count slots),
	// so it must run before computing the state root.
	if bc.config != nil && bc.config.IsPrague(header.Time) {
		if _, err := ProcessRequests(bc.config, statedb, header); err != nil {
			return fmt.Errorf("process requests block %d: %w", block.NumberU64(), err)
		}
	}

	// Validate state root: the post-execution state root must match header.Root.
	computedRoot := statedb.GetRoot()
	if header.Root != computedRoot {
		return fmt.Errorf("%w: header=%s computed=%s", ErrInvalidStateRoot,
			header.Root.Hex(), computedRoot.Hex())
	}

	// Validate Block Access List hash (EIP-7928).
	var computedBALHash *types.Hash
	if result.BlockAccessList != nil {
		h := result.BlockAccessList.Hash()
		computedBALHash = &h
	}
	if err := bc.validator.ValidateBlockAccessList(header, computedBALHash); err != nil {
		return err
	}

	// Store in block cache.
	bc.blockCache[hash] = block

	num := block.NumberU64()
	txs := block.Transactions()

	// Populate derived fields on receipts and store tx lookup entries.
	for i, receipt := range receipts {
		receipt.BlockHash = hash
		receipt.BlockNumber = new(big.Int).SetUint64(num)
		receipt.TransactionIndex = uint(i)
		if i < len(txs) {
			receipt.TxHash = txs[i].Hash()
		}
		// Set log context fields.
		for j, log := range receipt.Logs {
			log.BlockHash = hash
			log.BlockNumber = num
			log.TxHash = receipt.TxHash
			log.TxIndex = uint(i)
			log.Index = uint(j)
		}
	}

	// Cache receipts by block hash.
	bc.receiptCache[hash] = receipts

	// Build tx lookup index.
	for i, tx := range txs {
		bc.txLookup[tx.Hash()] = TxLookupEntry{
			BlockHash:   hash,
			BlockNumber: num,
			TxIndex:     uint64(i),
		}
	}

	// Update canonical chain if this extends the head.
	if num > bc.currentBlock.NumberU64() {
		bc.canonCache[num] = hash
		bc.currentBlock = block
		bc.currentState = statedb.(*state.MemoryStateDB)

		// Persist to rawdb.
		bc.writeBlock(block)
		bc.writeReceipts(num, hash, receipts)
		bc.writeTxLookups(txs, num)
		rawdb.WriteCanonicalHash(bc.db, num, hash)
		rawdb.WriteHeadBlockHash(bc.db, hash)
		rawdb.WriteHeadHeaderHash(bc.db, hash)

		// Update header chain.
		bc.hc.InsertHeaders([]*types.Header{header})

		// Cache state snapshot at regular intervals.
		if shouldSnapshot(num) {
			bc.sc.put(hash, num, bc.currentState)
		}
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
// It checks the in-memory cache first, then falls back to rawdb.
func (bc *Blockchain) GetBlock(hash types.Hash) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if b, ok := bc.blockCache[hash]; ok {
		return b
	}
	// Fallback: try reading from rawdb.
	return bc.readBlock(hash)
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

// StateAtRoot returns the state for the given state root hash.
// It searches canonical blocks for one whose header root matches,
// then re-executes from the nearest cached snapshot.
func (bc *Blockchain) StateAtRoot(root types.Hash) (state.StateDB, error) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()

	// Fast path: check if root matches the current head state.
	if bc.currentState.GetRoot() == root {
		return bc.currentState.Copy(), nil
	}

	// Check if root matches the genesis state.
	if bc.genesisState.GetRoot() == root {
		return bc.genesisState.Copy(), nil
	}

	// Search canonical blocks for a block with this state root.
	for _, block := range bc.blockCache {
		if block.Header().Root == root {
			return bc.stateAt(block)
		}
	}

	return nil, fmt.Errorf("%w: no block found with state root %v", ErrStateNotFound, root)
}

// stateAt returns the state after executing up to (and including) the given block.
// For the genesis block, this is the genesis state directly.
// It checks the state cache for a snapshot closer to the target block to avoid
// re-executing the entire chain from genesis.
func (bc *Blockchain) stateAt(block *types.Block) (state.StateDB, error) {
	if block.Hash() == bc.genesis.Hash() {
		return bc.genesisState.Copy(), nil
	}

	// Check if we have an exact cached state for this block.
	if cached, ok := bc.sc.get(block.Hash()); ok {
		return cached, nil
	}

	// Collect the chain of blocks from genesis (or a cached snapshot) to this block.
	var chain []*types.Block
	current := block
	var baseState *state.MemoryStateDB

	for current.Hash() != bc.genesis.Hash() {
		// Check if we have a cached state for this ancestor.
		if cached, ok := bc.sc.get(current.ParentHash()); ok {
			// We have a cached state at the parent; re-execute from there.
			baseState = cached
			chain = append(chain, current)
			break
		}
		chain = append(chain, current)
		parent, ok := bc.blockCache[current.ParentHash()]
		if !ok {
			return nil, fmt.Errorf("%w: missing ancestor at %v", ErrStateNotFound, current.ParentHash())
		}
		current = parent
	}

	// Use genesis state as base if no cached snapshot was found.
	if baseState == nil {
		baseState = bc.genesisState.Copy()
	}

	// Re-execute from the base state.
	for i := len(chain) - 1; i >= 0; i-- {
		b := chain[i]
		if _, err := bc.processor.Process(b, baseState); err != nil {
			return nil, fmt.Errorf("re-execute block %d: %w", b.NumberU64(), err)
		}
	}
	return baseState, nil
}

// writeBlock persists a block's header and body to rawdb using RLP encoding.
func (bc *Blockchain) writeBlock(block *types.Block) {
	num := block.NumberU64()
	hash := block.Hash()

	// RLP-encode the header.
	headerData, err := block.Header().EncodeRLP()
	if err != nil {
		return
	}
	rawdb.WriteHeader(bc.db, num, hash, headerData)

	// RLP-encode the body (transactions list + uncles list).
	bodyData, err := encodeBlockBody(block.Body())
	if err != nil {
		return
	}
	rawdb.WriteBody(bc.db, num, hash, bodyData)
}

// readBlock retrieves a block from rawdb by looking up the block number
// from the header hash, then reading and decoding header and body.
func (bc *Blockchain) readBlock(hash types.Hash) *types.Block {
	// Look up block number from hash.
	num, err := rawdb.ReadHeaderNumber(bc.db, hash)
	if err != nil {
		return nil
	}

	// Read header.
	headerData, err := rawdb.ReadHeader(bc.db, num, hash)
	if err != nil || len(headerData) == 0 {
		return nil
	}
	header, err := types.DecodeHeaderRLP(headerData)
	if err != nil {
		return nil
	}

	// Read body.
	bodyData, err := rawdb.ReadBody(bc.db, num, hash)
	if err != nil || len(bodyData) == 0 {
		// Body may be empty for blocks with no transactions; create block with header only.
		return types.NewBlock(header, nil)
	}
	body, err := decodeBlockBody(bodyData)
	if err != nil {
		// If body decode fails, return header-only block.
		return types.NewBlock(header, nil)
	}
	return types.NewBlock(header, body)
}

// encodeBlockBody RLP-encodes a block body as [transactions_list, uncles_list, withdrawals_list].
// The withdrawals list is included only when body.Withdrawals is non-nil (post-Shanghai).
func encodeBlockBody(body *types.Body) ([]byte, error) {
	// Encode transactions.
	var txsPayload []byte
	if body != nil {
		for _, tx := range body.Transactions {
			txEnc, err := tx.EncodeRLP()
			if err != nil {
				return nil, err
			}
			// Wrap raw tx bytes as an RLP byte string.
			wrapped, err := rlp.EncodeToBytes(txEnc)
			if err != nil {
				return nil, err
			}
			txsPayload = append(txsPayload, wrapped...)
		}
	}

	// Encode uncles.
	var unclesPayload []byte
	if body != nil {
		for _, uncle := range body.Uncles {
			uncleEnc, err := uncle.EncodeRLP()
			if err != nil {
				return nil, err
			}
			unclesPayload = append(unclesPayload, uncleEnc...)
		}
	}

	var payload []byte
	payload = append(payload, rlp.WrapList(txsPayload)...)
	payload = append(payload, rlp.WrapList(unclesPayload)...)

	// Encode withdrawals (post-Shanghai).
	if body != nil && body.Withdrawals != nil {
		var wsPayload []byte
		for _, w := range body.Withdrawals {
			wEnc, err := rlp.EncodeToBytes([]interface{}{w.Index, w.ValidatorIndex, w.Address, w.Amount})
			if err != nil {
				return nil, err
			}
			wsPayload = append(wsPayload, wEnc...)
		}
		payload = append(payload, rlp.WrapList(wsPayload)...)
	}

	return rlp.WrapList(payload), nil
}

// decodeBlockBody decodes an RLP-encoded block body [transactions_list, uncles_list, withdrawals_list?].
// The withdrawals list is optional (post-Shanghai).
func decodeBlockBody(data []byte) (*types.Body, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, fmt.Errorf("opening body list: %w", err)
	}

	// Decode transactions.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening txs list: %w", err)
	}
	var txs []*types.Transaction
	for !s.AtListEnd() {
		txBytes, err := s.Bytes()
		if err != nil {
			return nil, fmt.Errorf("reading tx bytes: %w", err)
		}
		tx, err := types.DecodeTxRLP(txBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding tx: %w", err)
		}
		txs = append(txs, tx)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing txs list: %w", err)
	}

	// Decode uncles.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening uncles list: %w", err)
	}
	var uncles []*types.Header
	for !s.AtListEnd() {
		uncleBytes, err := s.RawItem()
		if err != nil {
			return nil, fmt.Errorf("reading uncle: %w", err)
		}
		uncle, err := types.DecodeHeaderRLP(uncleBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding uncle: %w", err)
		}
		uncles = append(uncles, uncle)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing uncles list: %w", err)
	}

	body := &types.Body{
		Transactions: txs,
		Uncles:       uncles,
	}

	// Decode optional withdrawals (post-Shanghai).
	if !s.AtListEnd() {
		_, err = s.List()
		if err != nil {
			return nil, fmt.Errorf("opening withdrawals list: %w", err)
		}
		var withdrawals []*types.Withdrawal
		for !s.AtListEnd() {
			wBytes, err := s.RawItem()
			if err != nil {
				return nil, fmt.Errorf("reading withdrawal: %w", err)
			}
			w, err := decodeWithdrawal(wBytes)
			if err != nil {
				return nil, fmt.Errorf("decoding withdrawal: %w", err)
			}
			withdrawals = append(withdrawals, w)
		}
		if err := s.ListEnd(); err != nil {
			return nil, fmt.Errorf("closing withdrawals list: %w", err)
		}
		if withdrawals == nil {
			withdrawals = []*types.Withdrawal{} // empty but non-nil
		}
		body.Withdrawals = withdrawals
	}

	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing body list: %w", err)
	}

	return body, nil
}

// decodeWithdrawal decodes an RLP-encoded withdrawal [index, validatorIndex, address, amount].
func decodeWithdrawal(data []byte) (*types.Withdrawal, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	w := &types.Withdrawal{}
	w.Index, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	w.ValidatorIndex, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	addrBytes, err := s.Bytes()
	if err != nil {
		return nil, err
	}
	copy(w.Address[types.AddressLength-len(addrBytes):], addrBytes)
	w.Amount, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return w, nil
}

// Reorg replaces the canonical chain from the fork point with the new chain
// ending at newHead. It finds the common ancestor between the current
// canonical chain and the new chain, un-indexes old canonical blocks,
// re-indexes the new canonical blocks, and updates the current block pointer.
func (bc *Blockchain) Reorg(newHead *types.Block) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	return bc.reorg(newHead)
}

// reorg is the internal reorg implementation without locking.
func (bc *Blockchain) reorg(newHead *types.Block) error {
	// Build the new chain's ancestry: walk from newHead to genesis.
	// Collect blocks in reverse order (head first).
	var newChain []*types.Block
	current := newHead
	for {
		if _, ok := bc.blockCache[current.Hash()]; !ok {
			bc.blockCache[current.Hash()] = current
		}
		if current.NumberU64() == 0 {
			break
		}
		newChain = append(newChain, current)
		parent, ok := bc.blockCache[current.ParentHash()]
		if !ok {
			return fmt.Errorf("%w: missing ancestor %v during reorg", ErrBlockNotFound, current.ParentHash())
		}
		current = parent
	}

	// Determine the maximum height to clean up.
	oldHead := bc.currentBlock.NumberU64()
	newHeight := newHead.NumberU64()
	maxHeight := oldHead
	if newHeight > maxHeight {
		maxHeight = newHeight
	}

	// Un-index all canonical blocks above genesis.
	for n := maxHeight; n >= 1; n-- {
		if hash, ok := bc.canonCache[n]; ok {
			delete(bc.canonCache, n)
			rawdb.DeleteCanonicalHash(bc.db, n)

			bc.hc.mu.Lock()
			if h, ok := bc.hc.headers[n]; ok {
				delete(bc.hc.headersByHash, h.Hash())
				delete(bc.hc.headers, n)
			}
			bc.hc.mu.Unlock()
			_ = hash
		}
	}

	// Re-index the new chain from lowest block to highest.
	for i := len(newChain) - 1; i >= 0; i-- {
		blk := newChain[i]
		hash := blk.Hash()
		num := blk.NumberU64()

		bc.blockCache[hash] = blk
		bc.canonCache[num] = hash
		bc.writeBlock(blk)
		rawdb.WriteCanonicalHash(bc.db, num, hash)

		h := blk.Header()
		bc.hc.mu.Lock()
		bc.hc.headersByHash[h.Hash()] = h
		bc.hc.headers[num] = h
		bc.hc.mu.Unlock()
	}

	// Update current block.
	bc.currentBlock = newHead
	rawdb.WriteHeadBlockHash(bc.db, newHead.Hash())
	rawdb.WriteHeadHeaderHash(bc.db, newHead.Hash())

	bc.hc.mu.Lock()
	bc.hc.currentHeader = newHead.Header()
	bc.hc.mu.Unlock()

	// Re-derive state for the new head.
	statedb, err := bc.stateAt(newHead)
	if err != nil {
		return fmt.Errorf("re-derive state after reorg at %d: %w", newHead.NumberU64(), err)
	}
	bc.currentState = statedb.(*state.MemoryStateDB)

	return nil
}

// ChainLength returns the length of the canonical chain (genesis = 1).
func (bc *Blockchain) ChainLength() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock.NumberU64() + 1
}

// GetReceipts returns the receipts for a block identified by hash.
func (bc *Blockchain) GetReceipts(blockHash types.Hash) []*types.Receipt {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.receiptCache[blockHash]
}

// GetBlockReceipts returns the receipts for the canonical block at the given number.
func (bc *Blockchain) GetBlockReceipts(number uint64) []*types.Receipt {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	hash, ok := bc.canonCache[number]
	if !ok {
		return nil
	}
	return bc.receiptCache[hash]
}

// GetLogs returns all logs from receipts for the block identified by hash.
func (bc *Blockchain) GetLogs(blockHash types.Hash) []*types.Log {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	receipts := bc.receiptCache[blockHash]
	var logs []*types.Log
	for _, receipt := range receipts {
		logs = append(logs, receipt.Logs...)
	}
	return logs
}

// GetTransactionLookup returns the block location for a transaction hash.
func (bc *Blockchain) GetTransactionLookup(txHash types.Hash) (blockHash types.Hash, blockNumber uint64, txIndex uint64, found bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	entry, ok := bc.txLookup[txHash]
	if !ok {
		return types.Hash{}, 0, 0, false
	}
	return entry.BlockHash, entry.BlockNumber, entry.TxIndex, true
}

// HistoryOldestBlock returns the oldest block number for which block bodies
// and receipts are available. Returns 0 if no history pruning has occurred.
// Used by the RPC layer to detect EIP-4444 pruned data.
func (bc *Blockchain) HistoryOldestBlock() uint64 {
	oldest, _ := rawdb.ReadHistoryOldest(bc.db)
	return oldest
}

// PruneHistory prunes block bodies and receipts older than
// (head - retention) blocks. Headers are preserved. Returns the number
// of blocks pruned.
func (bc *Blockchain) PruneHistory(retention uint64) (uint64, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.currentBlock == nil {
		return 0, nil
	}
	head := bc.currentBlock.NumberU64()
	pruned, _, err := rawdb.PruneHistory(bc.db, head, retention)
	return pruned, err
}

// writeReceipts encodes and persists receipts for a block.
func (bc *Blockchain) writeReceipts(number uint64, hash types.Hash, receipts []*types.Receipt) {
	if len(receipts) == 0 {
		return
	}
	var encoded []byte
	for _, r := range receipts {
		data, err := r.EncodeRLP()
		if err != nil {
			continue
		}
		encoded = append(encoded, data...)
	}
	rawdb.WriteReceipts(bc.db, number, hash, encoded)
}

// writeTxLookups persists transaction hash -> block number mappings.
func (bc *Blockchain) writeTxLookups(txs []*types.Transaction, blockNumber uint64) {
	for _, tx := range txs {
		rawdb.WriteTxLookup(bc.db, tx.Hash(), blockNumber)
	}
}

// makeGenesis is a helper for creating a genesis block with the given gas limit and base fee.
func makeGenesis(gasLimit uint64, baseFee *big.Int) *types.Block {
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	calldataGasUsed := uint64(0)
	calldataExcessGas := uint64(0)
	emptyWithdrawalsHash := types.EmptyRootHash
	emptyRoot := types.EmptyRootHash
	header := &types.Header{
		Number:            big.NewInt(0),
		GasLimit:          gasLimit,
		GasUsed:           0,
		Time:              0,
		Difficulty:        new(big.Int),
		BaseFee:           baseFee,
		UncleHash:         EmptyUncleHash,
		WithdrawalsHash:   &emptyWithdrawalsHash,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyRoot,
		RequestsHash:      &emptyRoot,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}
	return types.NewBlock(header, &types.Body{Withdrawals: []*types.Withdrawal{}})
}
