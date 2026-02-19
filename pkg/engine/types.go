// Package engine defines types for the Engine API (CL-EL communication).
package engine

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2028/eth2028/core/types"
)

// PayloadID is the identifier for an execution payload being assembled.
type PayloadID [8]byte

// String returns the hex representation of the PayloadID.
func (id PayloadID) String() string {
	return fmt.Sprintf("0x%x", id[:])
}

// Withdrawal represents a validator withdrawal.
type Withdrawal struct {
	Index          uint64        `json:"index"`
	ValidatorIndex uint64        `json:"validatorIndex"`
	Address        types.Address `json:"address"`
	Amount         uint64        `json:"amount"` // in Gwei
}

// ExecutionPayloadV1 is the Prague V1 execution payload.
type ExecutionPayloadV1 struct {
	ParentHash    types.Hash    `json:"parentHash"`
	FeeRecipient  types.Address `json:"feeRecipient"`
	StateRoot     types.Hash    `json:"stateRoot"`
	ReceiptsRoot  types.Hash    `json:"receiptsRoot"`
	LogsBloom     types.Bloom   `json:"logsBloom"`
	PrevRandao    types.Hash    `json:"prevRandao"`
	BlockNumber   uint64        `json:"blockNumber"`
	GasLimit      uint64        `json:"gasLimit"`
	GasUsed       uint64        `json:"gasUsed"`
	Timestamp     uint64        `json:"timestamp"`
	ExtraData     []byte        `json:"extraData"`
	BaseFeePerGas *big.Int      `json:"baseFeePerGas"`
	BlockHash     types.Hash    `json:"blockHash"`
	Transactions  [][]byte      `json:"transactions"`
}

// ExecutionPayloadV2 extends V1 with withdrawals (Shanghai).
type ExecutionPayloadV2 struct {
	ExecutionPayloadV1
	Withdrawals []*Withdrawal `json:"withdrawals"`
}

// ExecutionPayloadV3 extends V2 with blob gas (Cancun/EIP-4844).
type ExecutionPayloadV3 struct {
	ExecutionPayloadV2
	BlobGasUsed   uint64 `json:"blobGasUsed"`
	ExcessBlobGas uint64 `json:"excessBlobGas"`
}

// ExecutionPayloadV4 extends V3 with execution requests (Prague/EIP-7685).
type ExecutionPayloadV4 struct {
	ExecutionPayloadV3
	ExecutionRequests [][]byte `json:"executionRequests"`
}

// ExecutionPayloadV5 extends V4 with Block Access Lists (Amsterdam/EIP-7928).
type ExecutionPayloadV5 struct {
	ExecutionPayloadV4
	BlockAccessList json.RawMessage `json:"blockAccessList,omitempty"`
}

// ForkchoiceStateV1 represents the fork choice state from the consensus layer.
type ForkchoiceStateV1 struct {
	HeadBlockHash      types.Hash `json:"headBlockHash"`
	SafeBlockHash      types.Hash `json:"safeBlockHash"`
	FinalizedBlockHash types.Hash `json:"finalizedBlockHash"`
}

// PayloadAttributesV1 contains attributes for building a new payload.
type PayloadAttributesV1 struct {
	Timestamp             uint64        `json:"timestamp"`
	PrevRandao            types.Hash    `json:"prevRandao"`
	SuggestedFeeRecipient types.Address `json:"suggestedFeeRecipient"`
}

// PayloadAttributesV2 extends V1 with withdrawals.
type PayloadAttributesV2 struct {
	PayloadAttributesV1
	Withdrawals []*Withdrawal `json:"withdrawals"`
}

// PayloadAttributesV3 extends V2 with parent beacon block root.
type PayloadAttributesV3 struct {
	PayloadAttributesV2
	ParentBeaconBlockRoot types.Hash `json:"parentBeaconBlockRoot"`
}

// PayloadAttributesV4 extends V3 with slot number and inclusion list (Amsterdam/FOCIL).
type PayloadAttributesV4 struct {
	PayloadAttributesV3
	SlotNumber               uint64   `json:"slotNumber"`
	InclusionListTransactions [][]byte `json:"inclusionListTransactions,omitempty"` // EIP-7805 FOCIL
}

// GetPayloadV3Response is the response for engine_getPayloadV3 (Cancun).
type GetPayloadV3Response struct {
	ExecutionPayload *ExecutionPayloadV3 `json:"executionPayload"`
	BlockValue       *big.Int            `json:"blockValue"`
	BlobsBundle      *BlobsBundleV1      `json:"blobsBundle"`
	Override         bool                `json:"shouldOverrideBuilder"`
}

// GetPayloadV4Response is the response for engine_getPayloadV4 (Prague).
type GetPayloadV4Response struct {
	ExecutionPayload  *ExecutionPayloadV3 `json:"executionPayload"`
	BlockValue        *big.Int            `json:"blockValue"`
	BlobsBundle       *BlobsBundleV1      `json:"blobsBundle"`
	Override          bool                `json:"shouldOverrideBuilder"`
	ExecutionRequests [][]byte            `json:"executionRequests"`
}

// GetPayloadV6Response is the response for engine_getPayloadV6 (Amsterdam).
type GetPayloadV6Response struct {
	ExecutionPayload  *ExecutionPayloadV5 `json:"executionPayload"`
	BlockValue        *big.Int            `json:"blockValue"`
	BlobsBundle       *BlobsBundleV1      `json:"blobsBundle"`
	Override          bool                `json:"shouldOverrideBuilder"`
	ExecutionRequests [][]byte            `json:"executionRequests"`
}

// PayloadStatus values.
const (
	StatusValid            = "VALID"
	StatusInvalid          = "INVALID"
	StatusSyncing          = "SYNCING"
	StatusAccepted         = "ACCEPTED"
	StatusInvalidBlockHash = "INVALID_BLOCK_HASH"
)

// PayloadStatusV1 is the response to engine_newPayload.
type PayloadStatusV1 struct {
	Status          string     `json:"status"`
	LatestValidHash *types.Hash `json:"latestValidHash,omitempty"`
	ValidationError *string    `json:"validationError,omitempty"`
}

// ForkchoiceUpdatedResult is the response to engine_forkchoiceUpdated.
type ForkchoiceUpdatedResult struct {
	PayloadStatus PayloadStatusV1 `json:"payloadStatus"`
	PayloadID     *PayloadID      `json:"payloadId,omitempty"`
}

// TransitionConfigurationV1 for Engine API transition configuration exchange.
type TransitionConfigurationV1 struct {
	TerminalTotalDifficulty *big.Int   `json:"terminalTotalDifficulty"`
	TerminalBlockHash       types.Hash `json:"terminalBlockHash"`
	TerminalBlockNumber     uint64     `json:"terminalBlockNumber"`
}

// BlobsBundleV1 is the blobs bundle returned by engine_getPayload.
type BlobsBundleV1 struct {
	Commitments [][]byte `json:"commitments"`
	Proofs      [][]byte `json:"proofs"`
	Blobs       [][]byte `json:"blobs"`
}

// GetPayloadResponse is the combined response for engine_getPayload.
type GetPayloadResponse struct {
	ExecutionPayload *ExecutionPayloadV4 `json:"executionPayload"`
	BlockValue       *big.Int            `json:"blockValue"`
	BlobsBundle      *BlobsBundleV1      `json:"blobsBundle,omitempty"`
	Override         bool                `json:"shouldOverrideBuilder"`
}
