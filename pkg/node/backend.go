package node

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/core/vm"
	"github.com/eth2028/eth2028/engine"
	"github.com/eth2028/eth2028/rpc"
	"github.com/eth2028/eth2028/trie"
)

// nodeBackend adapts the Node to the rpc.Backend interface.
type nodeBackend struct {
	node *Node
}

func newNodeBackend(n *Node) rpc.Backend {
	return &nodeBackend{node: n}
}

func (b *nodeBackend) HeaderByNumber(number rpc.BlockNumber) *types.Header {
	bc := b.node.blockchain
	switch number {
	case rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		blk := bc.CurrentBlock()
		if blk != nil {
			return blk.Header()
		}
		return nil
	case rpc.EarliestBlockNumber:
		blk := bc.GetBlockByNumber(0)
		if blk != nil {
			return blk.Header()
		}
		return nil
	default:
		blk := bc.GetBlockByNumber(uint64(number))
		if blk != nil {
			return blk.Header()
		}
		return nil
	}
}

func (b *nodeBackend) HeaderByHash(hash types.Hash) *types.Header {
	blk := b.node.blockchain.GetBlock(hash)
	if blk != nil {
		return blk.Header()
	}
	return nil
}

func (b *nodeBackend) BlockByNumber(number rpc.BlockNumber) *types.Block {
	bc := b.node.blockchain
	switch number {
	case rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		return bc.CurrentBlock()
	case rpc.EarliestBlockNumber:
		return bc.GetBlockByNumber(0)
	default:
		return bc.GetBlockByNumber(uint64(number))
	}
}

func (b *nodeBackend) BlockByHash(hash types.Hash) *types.Block {
	return b.node.blockchain.GetBlock(hash)
}

func (b *nodeBackend) CurrentHeader() *types.Header {
	blk := b.node.blockchain.CurrentBlock()
	if blk != nil {
		return blk.Header()
	}
	return nil
}

func (b *nodeBackend) ChainID() *big.Int {
	return b.node.blockchain.Config().ChainID
}

func (b *nodeBackend) StateAt(root types.Hash) (state.StateDB, error) {
	return b.node.blockchain.StateAtRoot(root)
}

func (b *nodeBackend) GetProof(addr types.Address, storageKeys []types.Hash, blockNumber rpc.BlockNumber) (*trie.AccountProof, error) {
	header := b.HeaderByNumber(blockNumber)
	if header == nil {
		return nil, fmt.Errorf("block not found")
	}

	statedb, err := b.StateAt(header.Root)
	if err != nil {
		return nil, err
	}

	// Type-assert to MemoryStateDB to access trie-building methods.
	memState, ok := statedb.(*state.MemoryStateDB)
	if !ok {
		return nil, fmt.Errorf("state does not support proof generation")
	}

	// Build the full state trie from all accounts.
	stateTrie := memState.BuildStateTrie()

	// Build the storage trie for the requested account.
	storageTrie := memState.BuildStorageTrie(addr)

	// Generate account proof with storage proofs.
	return trie.ProveAccountWithStorage(stateTrie, addr, storageTrie, storageKeys)
}

func (b *nodeBackend) SendTransaction(tx *types.Transaction) error {
	return b.node.txPool.AddLocal(tx)
}

func (b *nodeBackend) GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) {
	// Check the blockchain's tx lookup index first.
	blockHash, blockNum, txIndex, found := b.node.blockchain.GetTransactionLookup(hash)
	if found {
		block := b.node.blockchain.GetBlock(blockHash)
		if block != nil {
			txs := block.Transactions()
			if int(txIndex) < len(txs) {
				return txs[txIndex], blockNum, txIndex
			}
		}
	}
	// Fall back to txpool for pending transactions.
	tx := b.node.txPool.Get(hash)
	if tx != nil {
		return tx, 0, 0
	}
	return nil, 0, 0
}

func (b *nodeBackend) SuggestGasPrice() *big.Int {
	// Return current base fee as a simple gas price suggestion.
	blk := b.node.blockchain.CurrentBlock()
	if blk != nil && blk.Header().BaseFee != nil {
		return new(big.Int).Set(blk.Header().BaseFee)
	}
	return big.NewInt(1_000_000_000) // 1 gwei default
}

func (b *nodeBackend) GetReceipts(blockHash types.Hash) []*types.Receipt {
	return b.node.blockchain.GetReceipts(blockHash)
}

func (b *nodeBackend) GetLogs(blockHash types.Hash) []*types.Log {
	return b.node.blockchain.GetLogs(blockHash)
}

func (b *nodeBackend) GetBlockReceipts(number uint64) []*types.Receipt {
	return b.node.blockchain.GetBlockReceipts(number)
}

func (b *nodeBackend) EVMCall(from types.Address, to *types.Address, data []byte, gas uint64, value *big.Int, blockNumber rpc.BlockNumber) ([]byte, uint64, error) {
	bc := b.node.blockchain

	// Resolve block header.
	var header *types.Header
	switch blockNumber {
	case rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		blk := bc.CurrentBlock()
		if blk != nil {
			header = blk.Header()
		}
	default:
		blk := bc.GetBlockByNumber(uint64(blockNumber))
		if blk != nil {
			header = blk.Header()
		}
	}
	if header == nil {
		return nil, 0, fmt.Errorf("block not found")
	}

	// Get state at this block.
	statedb, err := b.StateAt(header.Root)
	if err != nil {
		return nil, 0, fmt.Errorf("state not found: %w", err)
	}

	// Default gas to 50M if zero.
	if gas == 0 {
		gas = 50_000_000
	}
	if value == nil {
		value = new(big.Int)
	}

	// Build block and tx contexts.
	blockCtx := vm.BlockContext{
		GetHash:     bc.GetHashFn(),
		BlockNumber: header.Number,
		Time:        header.Time,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
	}
	txCtx := vm.TxContext{
		Origin:   from,
		GasPrice: header.BaseFee,
	}

	evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)

	if to == nil {
		// Contract creation call - just return empty.
		return nil, gas, nil
	}

	ret, gasLeft, err := evm.Call(from, *to, data, gas, value)
	return ret, gasLeft, err
}

// TraceTransaction re-executes a transaction with a StructLogTracer attached.
// It looks up the block containing the transaction, re-processes all prior
// transactions to build up state, then executes the target tx with tracing.
func (b *nodeBackend) TraceTransaction(txHash types.Hash) (*vm.StructLogTracer, error) {
	bc := b.node.blockchain

	// Look up the transaction in the chain index.
	blockHash, _, txIndex, found := bc.GetTransactionLookup(txHash)
	if !found {
		return nil, fmt.Errorf("transaction %v not found", txHash)
	}

	block := bc.GetBlock(blockHash)
	if block == nil {
		return nil, fmt.Errorf("block %v not found", blockHash)
	}

	txs := block.Transactions()
	if int(txIndex) >= len(txs) {
		return nil, fmt.Errorf("transaction index %d out of range", txIndex)
	}

	// Get state at the parent block.
	header := block.Header()
	parentBlock := bc.GetBlock(header.ParentHash)
	if parentBlock == nil {
		return nil, fmt.Errorf("parent block %v not found", header.ParentHash)
	}
	statedb, err := b.StateAt(parentBlock.Header().Root)
	if err != nil {
		return nil, fmt.Errorf("state not found for parent block: %w", err)
	}

	blockCtx := vm.BlockContext{
		GetHash:     bc.GetHashFn(),
		BlockNumber: header.Number,
		Time:        header.Time,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
	}

	// Re-execute all transactions before the target to build up state.
	for i := uint64(0); i < txIndex; i++ {
		tx := txs[i]
		from := types.Address{}
		if sender := tx.Sender(); sender != nil {
			from = *sender
		}
		txCtx := vm.TxContext{
			Origin:   from,
			GasPrice: tx.GasPrice(),
		}
		evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)
		to := tx.To()
		if to != nil {
			evm.Call(from, *to, tx.Data(), tx.Gas(), tx.Value())
		}
		// Update nonce after replaying the transaction.
		statedb.SetNonce(from, statedb.GetNonce(from)+1)
	}

	// Now execute the target transaction with tracing enabled.
	targetTx := txs[txIndex]
	from := types.Address{}
	if sender := targetTx.Sender(); sender != nil {
		from = *sender
	}
	txCtx := vm.TxContext{
		Origin:   from,
		GasPrice: targetTx.GasPrice(),
	}

	tracer := vm.NewStructLogTracer()
	tracingCfg := vm.Config{
		Debug:  true,
		Tracer: tracer,
	}
	evm := vm.NewEVMWithState(blockCtx, txCtx, tracingCfg, statedb)

	to := targetTx.To()
	if to != nil {
		ret, gasLeft, err := evm.Call(from, *to, targetTx.Data(), targetTx.Gas(), targetTx.Value())
		gasUsed := targetTx.Gas() - gasLeft
		tracer.CaptureEnd(ret, gasUsed, err)
	}

	return tracer, nil
}

// txPoolAdapter adapts *txpool.TxPool to core.TxPoolReader.
type txPoolAdapter struct {
	node *Node
}

func (a *txPoolAdapter) Pending() []*types.Transaction {
	return a.node.txPool.PendingFlat()
}

// pendingPayload stores a built payload for later retrieval by getPayload.
type pendingPayload struct {
	block    *types.Block
	receipts []*types.Receipt
}

// engineBackend adapts the Node to the engine.Backend interface.
type engineBackend struct {
	node *Node

	mu       sync.Mutex
	payloads map[engine.PayloadID]*pendingPayload
	builder  *core.BlockBuilder
}

func newEngineBackend(n *Node) engine.Backend {
	pool := &txPoolAdapter{node: n}
	builder := core.NewBlockBuilder(n.blockchain.Config(), n.blockchain, pool)
	return &engineBackend{
		node:     n,
		payloads: make(map[engine.PayloadID]*pendingPayload),
		builder:  builder,
	}
}

func (b *engineBackend) ProcessBlock(
	payload *engine.ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (engine.PayloadStatusV1, error) {
	bc := b.node.blockchain

	// Convert payload to a block.
	header := &types.Header{
		ParentHash:  payload.ParentHash,
		Coinbase:    payload.FeeRecipient,
		Root:        payload.StateRoot,
		ReceiptHash: payload.ReceiptsRoot,
		Bloom:       payload.LogsBloom,
		Number:      new(big.Int).SetUint64(payload.BlockNumber),
		GasLimit:    payload.GasLimit,
		GasUsed:     payload.GasUsed,
		Time:        payload.Timestamp,
		Extra:       payload.ExtraData,
		BaseFee:     payload.BaseFeePerGas,
		MixDigest:   payload.PrevRandao,
	}

	// Decode transactions from raw bytes.
	var txs []*types.Transaction
	for _, raw := range payload.Transactions {
		tx, err := types.DecodeTxRLP(raw)
		if err != nil {
			latestValid := payload.ParentHash
			return engine.PayloadStatusV1{
				Status:          engine.StatusInvalid,
				LatestValidHash: &latestValid,
			}, nil
		}
		txs = append(txs, tx)
	}

	block := types.NewBlock(header, &types.Body{Transactions: txs})

	// Verify block hash matches.
	if block.Hash() != payload.BlockHash {
		latestValid := payload.ParentHash
		return engine.PayloadStatusV1{
			Status:          engine.StatusInvalid,
			LatestValidHash: &latestValid,
		}, nil
	}

	// Check if parent is known.
	if !bc.HasBlock(payload.ParentHash) {
		return engine.PayloadStatusV1{
			Status: engine.StatusSyncing,
		}, nil
	}

	// Insert the block.
	if err := bc.InsertBlock(block); err != nil {
		latestValid := payload.ParentHash
		return engine.PayloadStatusV1{
			Status:          engine.StatusInvalid,
			LatestValidHash: &latestValid,
		}, nil
	}

	blockHash := block.Hash()
	return engine.PayloadStatusV1{
		Status:          engine.StatusValid,
		LatestValidHash: &blockHash,
	}, nil
}

func (b *engineBackend) ForkchoiceUpdated(
	fcState engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributesV3,
) (engine.ForkchoiceUpdatedResult, error) {
	bc := b.node.blockchain

	// Check if we know the head block.
	headBlock := bc.GetBlock(fcState.HeadBlockHash)
	var payloadStatus engine.PayloadStatusV1
	if headBlock == nil {
		// We don't know this block yet; report syncing.
		payloadStatus = engine.PayloadStatusV1{
			Status: engine.StatusSyncing,
		}
		return engine.ForkchoiceUpdatedResult{
			PayloadStatus: payloadStatus,
		}, nil
	}

	// Head is known. Report valid.
	headHash := headBlock.Hash()
	payloadStatus = engine.PayloadStatusV1{
		Status:          engine.StatusValid,
		LatestValidHash: &headHash,
	}

	// If no payload attributes, just return the forkchoice acknowledgment.
	if payloadAttributes == nil {
		return engine.ForkchoiceUpdatedResult{
			PayloadStatus: payloadStatus,
		}, nil
	}

	// Payload attributes provided: build a new block.
	parentHeader := headBlock.Header()

	// Convert engine withdrawals to core types.
	var withdrawals []*types.Withdrawal
	for _, w := range payloadAttributes.Withdrawals {
		withdrawals = append(withdrawals, &types.Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		})
	}

	beaconRoot := payloadAttributes.ParentBeaconBlockRoot
	attrs := &core.BuildBlockAttributes{
		Timestamp:    payloadAttributes.Timestamp,
		FeeRecipient: payloadAttributes.SuggestedFeeRecipient,
		Random:       payloadAttributes.PrevRandao,
		Withdrawals:  withdrawals,
		BeaconRoot:   &beaconRoot,
		GasLimit:     parentHeader.GasLimit, // keep parent gas limit
	}

	block, receipts, err := b.builder.BuildBlock(parentHeader, attrs)
	if err != nil {
		return engine.ForkchoiceUpdatedResult{
			PayloadStatus: payloadStatus,
		}, fmt.Errorf("build block: %w", err)
	}

	// Generate a payload ID from the block parameters.
	payloadID := generatePayloadID(parentHeader.Hash(), attrs)

	// Store the built payload.
	b.mu.Lock()
	b.payloads[payloadID] = &pendingPayload{
		block:    block,
		receipts: receipts,
	}
	b.mu.Unlock()

	return engine.ForkchoiceUpdatedResult{
		PayloadStatus: payloadStatus,
		PayloadID:     &payloadID,
	}, nil
}

func (b *engineBackend) ProcessBlockV4(
	payload *engine.ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (engine.PayloadStatusV1, error) {
	return b.ProcessBlock(payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

func (b *engineBackend) ProcessBlockV5(
	payload *engine.ExecutionPayloadV5,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (engine.PayloadStatusV1, error) {
	// Delegate to V3 processing for the base payload.
	return b.ProcessBlock(&payload.ExecutionPayloadV3, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

func (b *engineBackend) ForkchoiceUpdatedV4(
	state engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributesV4,
) (engine.ForkchoiceUpdatedResult, error) {
	// Promote V4 attributes to V3 and delegate.
	var v3Attrs *engine.PayloadAttributesV3
	if payloadAttributes != nil {
		v3Attrs = &payloadAttributes.PayloadAttributesV3
	}
	return b.ForkchoiceUpdated(state, v3Attrs)
}

func (b *engineBackend) GetPayloadV4ByID(id engine.PayloadID) (*engine.GetPayloadV4Response, error) {
	resp, err := b.GetPayloadByID(id)
	if err != nil {
		return nil, err
	}
	return &engine.GetPayloadV4Response{
		ExecutionPayload:  &resp.ExecutionPayload.ExecutionPayloadV3,
		BlockValue:        resp.BlockValue,
		BlobsBundle:       resp.BlobsBundle,
		ExecutionRequests: [][]byte{},
	}, nil
}

func (b *engineBackend) GetPayloadV6ByID(id engine.PayloadID) (*engine.GetPayloadV6Response, error) {
	resp, err := b.GetPayloadByID(id)
	if err != nil {
		return nil, err
	}
	return &engine.GetPayloadV6Response{
		ExecutionPayload: &engine.ExecutionPayloadV5{
			ExecutionPayloadV4: *resp.ExecutionPayload,
		},
		BlockValue:        resp.BlockValue,
		BlobsBundle:       resp.BlobsBundle,
		ExecutionRequests: [][]byte{},
	}, nil
}

func (b *engineBackend) GetHeadTimestamp() uint64 {
	head := b.node.blockchain.CurrentBlock()
	if head != nil {
		return head.Time()
	}
	return 0
}

func (b *engineBackend) IsCancun(timestamp uint64) bool {
	return b.node.blockchain.Config().IsCancun(timestamp)
}

func (b *engineBackend) IsPrague(timestamp uint64) bool {
	return b.node.blockchain.Config().IsPrague(timestamp)
}

func (b *engineBackend) IsAmsterdam(timestamp uint64) bool {
	return b.node.blockchain.Config().IsAmsterdam(timestamp)
}

func (b *engineBackend) GetPayloadByID(id engine.PayloadID) (*engine.GetPayloadResponse, error) {
	b.mu.Lock()
	payload, ok := b.payloads[id]
	if ok {
		delete(b.payloads, id)
	}
	b.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("payload %v not found", id)
	}

	block := payload.block
	header := block.Header()

	// Convert block to execution payload.
	execPayload := &engine.ExecutionPayloadV4{
		ExecutionPayloadV3: engine.ExecutionPayloadV3{
			ExecutionPayloadV2: engine.ExecutionPayloadV2{
				ExecutionPayloadV1: engine.ExecutionPayloadV1{
					ParentHash:    header.ParentHash,
					FeeRecipient:  header.Coinbase,
					StateRoot:     header.Root,
					ReceiptsRoot:  header.ReceiptHash,
					LogsBloom:     header.Bloom,
					PrevRandao:    header.MixDigest,
					BlockNumber:   block.NumberU64(),
					GasLimit:      header.GasLimit,
					GasUsed:       header.GasUsed,
					Timestamp:     header.Time,
					ExtraData:     header.Extra,
					BaseFeePerGas: header.BaseFee,
					BlockHash:     block.Hash(),
					Transactions:  encodeTxsRLP(block.Transactions()),
				},
			},
		},
	}

	// Add withdrawals if present.
	if ws := block.Withdrawals(); ws != nil {
		for _, w := range ws {
			execPayload.Withdrawals = append(execPayload.Withdrawals, &engine.Withdrawal{
				Index:          w.Index,
				ValidatorIndex: w.ValidatorIndex,
				Address:        w.Address,
				Amount:         w.Amount,
			})
		}
	}

	// Calculate block value (sum of priority fees paid).
	blockValue := new(big.Int)
	for _, receipt := range payload.receipts {
		if receipt.EffectiveGasPrice != nil && header.BaseFee != nil {
			tip := new(big.Int).Sub(receipt.EffectiveGasPrice, header.BaseFee)
			if tip.Sign() > 0 {
				tipTotal := new(big.Int).Mul(tip, new(big.Int).SetUint64(receipt.GasUsed))
				blockValue.Add(blockValue, tipTotal)
			}
		}
	}

	return &engine.GetPayloadResponse{
		ExecutionPayload: execPayload,
		BlockValue:       blockValue,
		BlobsBundle:      &engine.BlobsBundleV1{},
		Override:         false,
	}, nil
}

// generatePayloadID creates a deterministic PayloadID from the parent hash
// and build attributes.
func generatePayloadID(parentHash types.Hash, attrs *core.BuildBlockAttributes) engine.PayloadID {
	var id engine.PayloadID

	// Mix parent hash, timestamp, and fee recipient into the ID.
	// Use a simple approach: take bytes from parent hash + timestamp.
	copy(id[:], parentHash[:4])
	binary.BigEndian.PutUint32(id[4:], uint32(attrs.Timestamp))

	// If the ID collides (unlikely), add some randomness.
	if id == (engine.PayloadID{}) {
		rand.Read(id[:])
	}

	return id
}

// encodeTxsRLP encodes a list of transactions to RLP byte slices
// for inclusion in an Engine API ExecutionPayload.
func encodeTxsRLP(txs []*types.Transaction) [][]byte {
	if len(txs) == 0 {
		return nil
	}
	encoded := make([][]byte, len(txs))
	for i, tx := range txs {
		raw, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		encoded[i] = raw
	}
	return encoded
}
