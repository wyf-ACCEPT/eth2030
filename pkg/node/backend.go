package node

import (
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/engine"
	"github.com/eth2028/eth2028/rpc"
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
	// For now, return the current state regardless of root.
	// A proper implementation would look up state by trie root.
	return b.node.blockchain.State(), nil
}

func (b *nodeBackend) SendTransaction(tx *types.Transaction) error {
	return b.node.txPool.AddLocal(tx)
}

func (b *nodeBackend) GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) {
	// Check txpool first.
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
	return nil // Receipts not stored yet
}

func (b *nodeBackend) GetLogs(blockHash types.Hash) []*types.Log {
	return nil // Logs not indexed yet
}

func (b *nodeBackend) GetBlockReceipts(number uint64) []*types.Receipt {
	return nil // Receipts not stored yet
}

func (b *nodeBackend) EVMCall(from types.Address, to *types.Address, data []byte, gas uint64, value *big.Int, blockNumber rpc.BlockNumber) ([]byte, uint64, error) {
	// Stub: a real implementation would create an EVM and execute the call.
	return nil, 0, fmt.Errorf("eth_call not fully implemented")
}

// engineBackend adapts the Node to the engine.Backend interface.
type engineBackend struct {
	node *Node
}

func newEngineBackend(n *Node) engine.Backend {
	return &engineBackend{node: n}
}

func (b *engineBackend) ProcessBlock(
	payload *engine.ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (engine.PayloadStatusV1, error) {
	// Stub: a real implementation would convert payload to a block and insert.
	return engine.PayloadStatusV1{
		Status: engine.StatusSyncing,
	}, nil
}

func (b *engineBackend) ForkchoiceUpdated(
	state engine.ForkchoiceStateV1,
	payloadAttributes *engine.PayloadAttributesV3,
) (engine.ForkchoiceUpdatedResult, error) {
	// Stub: acknowledge forkchoice but don't build payloads yet.
	return engine.ForkchoiceUpdatedResult{
		PayloadStatus: engine.PayloadStatusV1{
			Status: engine.StatusSyncing,
		},
	}, nil
}

func (b *engineBackend) GetPayloadByID(id engine.PayloadID) (*engine.GetPayloadResponse, error) {
	return nil, fmt.Errorf("payload %v not found", id)
}
