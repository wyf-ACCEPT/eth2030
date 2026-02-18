package engine

import (
	"encoding/json"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// BuiltPayload holds the result of a payload build process.
type BuiltPayload struct {
	Block             *types.Block
	Receipts          []*types.Receipt
	BlockValue        *big.Int
	BlobsBundle       *BlobsBundleV1
	Override          bool
	ExecutionRequests [][]byte
	BAL               *bal.BlockAccessList
}

// PayloadBuilder manages async payload construction.
type PayloadBuilder struct {
	mu       sync.RWMutex
	config   *core.ChainConfig
	statedb  *state.MemoryStateDB
	txPool   core.TxPoolReader
	payloads map[PayloadID]*BuiltPayload
}

// NewPayloadBuilder creates a new PayloadBuilder.
func NewPayloadBuilder(config *core.ChainConfig, statedb *state.MemoryStateDB, txPool core.TxPoolReader) *PayloadBuilder {
	return &PayloadBuilder{
		config:   config,
		statedb:  statedb,
		txPool:   txPool,
		payloads: make(map[PayloadID]*BuiltPayload),
	}
}

// StartBuild begins building a payload with the given attributes.
func (pb *PayloadBuilder) StartBuild(
	id PayloadID,
	parentBlock *types.Block,
	attrs *PayloadAttributesV4,
) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	builder := core.NewBlockBuilder(pb.config, nil, pb.txPool)
	builder.SetState(pb.statedb.Copy())
	parentHeader := parentBlock.Header()

	var beaconRoot *types.Hash
	if (attrs.ParentBeaconBlockRoot != types.Hash{}) {
		br := attrs.ParentBeaconBlockRoot
		beaconRoot = &br
	}

	block, receipts, err := builder.BuildBlock(parentHeader, &core.BuildBlockAttributes{
		Timestamp:    attrs.Timestamp,
		FeeRecipient: attrs.SuggestedFeeRecipient,
		Random:       attrs.PrevRandao,
		GasLimit:     parentHeader.GasLimit,
		Withdrawals:  WithdrawalsToCore(attrs.Withdrawals),
		BeaconRoot:   beaconRoot,
	})
	if err != nil {
		return err
	}

	// Calculate block value as the sum of effective tips paid by transactions.
	blockValue := calcBlockValue(block, receipts, parentHeader.BaseFee)

	pb.payloads[id] = &BuiltPayload{
		Block:             block,
		Receipts:          receipts,
		BlockValue:        blockValue,
		BlobsBundle:       &BlobsBundleV1{},
		ExecutionRequests: [][]byte{},
	}

	return nil
}

// GetPayload retrieves a completed payload by its ID.
func (pb *PayloadBuilder) GetPayload(id PayloadID) (*BuiltPayload, error) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	built, ok := pb.payloads[id]
	if !ok {
		return nil, ErrUnknownPayload
	}
	return built, nil
}

// calcBlockValue computes the total tips (block value) from receipts.
// Block value = sum over txs of (effectiveGasPrice - baseFee) * gasUsed.
func calcBlockValue(block *types.Block, receipts []*types.Receipt, baseFee *big.Int) *big.Int {
	total := new(big.Int)
	if baseFee == nil {
		return total
	}

	txs := block.Transactions()
	for i, receipt := range receipts {
		if i >= len(txs) {
			break
		}
		tx := txs[i]

		// Compute effective gas price.
		effectivePrice := effectiveTipPerGas(tx, baseFee)
		if effectivePrice.Sign() <= 0 {
			continue
		}

		// tip = effectiveTip * gasUsed
		tip := new(big.Int).Mul(effectivePrice, new(big.Int).SetUint64(receipt.GasUsed))
		total.Add(total, tip)
	}
	return total
}

// effectiveTipPerGas computes (effectiveGasPrice - baseFee) for a transaction.
func effectiveTipPerGas(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if tx.GasFeeCap() == nil || tx.GasTipCap() == nil {
		// Legacy transaction: tip = gasPrice - baseFee.
		gp := tx.GasPrice()
		if gp == nil {
			return new(big.Int)
		}
		return new(big.Int).Sub(gp, baseFee)
	}

	// EIP-1559: effectiveTip = min(gasTipCap, gasFeeCap - baseFee)
	maxTip := new(big.Int).Sub(tx.GasFeeCap(), baseFee)
	if maxTip.Cmp(tx.GasTipCap()) > 0 {
		return new(big.Int).Set(tx.GasTipCap())
	}
	return maxTip
}

// blockToPayloadV5 converts a built block to an ExecutionPayloadV5 with BAL.
func blockToPayloadV5(block *types.Block, prevRandao types.Hash, withdrawals []*Withdrawal, blockBAL *bal.BlockAccessList) *ExecutionPayloadV5 {
	ep4 := blockToPayload(block, prevRandao, withdrawals)

	var balData json.RawMessage
	if blockBAL != nil {
		encoded, err := blockBAL.EncodeRLP()
		if err == nil {
			balData, _ = json.Marshal(encoded)
		}
	}
	if balData == nil {
		balData = json.RawMessage("null")
	}

	return &ExecutionPayloadV5{
		ExecutionPayloadV4: *ep4,
		BlockAccessList:    balData,
	}
}
