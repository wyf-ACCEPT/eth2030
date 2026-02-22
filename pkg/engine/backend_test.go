package engine

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// makeGenesis creates a minimal genesis block for testing.
func makeGenesis() *types.Block {
	blobGas := uint64(0)
	excessBlobGas := uint64(0)
	header := &types.Header{
		Number:        big.NewInt(0),
		GasLimit:      30_000_000,
		BaseFee:       big.NewInt(1_000_000_000),
		Difficulty:    new(big.Int),
		UncleHash:     types.EmptyUncleHash,
		Root:          types.EmptyRootHash,
		TxHash:        types.EmptyRootHash,
		ReceiptHash:   types.EmptyRootHash,
		Time:          1700000000,
		BlobGasUsed:   &blobGas,
		ExcessBlobGas: &excessBlobGas,
	}
	return types.NewBlock(header, nil)
}

func TestNewEngineBackend(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	if b == nil {
		t.Fatal("NewEngineBackend returned nil")
	}
	if b.config != core.TestConfig {
		t.Error("config not set correctly")
	}
	if len(b.blocks) != 1 {
		t.Errorf("expected 1 block (genesis), got %d", len(b.blocks))
	}
	genesisHash := genesis.Hash()
	if b.headHash != genesisHash {
		t.Error("headHash should be genesis hash")
	}
	if b.safeHash != genesisHash {
		t.Error("safeHash should be genesis hash")
	}
	if b.finalHash != genesisHash {
		t.Error("finalHash should be genesis hash")
	}
}

func TestProcessValidBlock(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	genesisHash := genesis.Hash()

	// Create a payload for block 1 with no transactions.
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xfee"),
				StateRoot:     types.Hash{},
				ReceiptsRoot:  types.Hash{},
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				GasUsed:       0,
				Timestamp:     1700000012,
				ExtraData:     []byte("test"),
				BaseFeePerGas: big.NewInt(875_000_000),
				BlockHash:     types.Hash{}, // computed later
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
		BlobGasUsed:   0,
		ExcessBlobGas: 0,
	}

	status, err := b.ProcessBlock(payload, nil, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBlock returned error: %v", err)
	}
	if status.Status != StatusValid {
		errMsg := ""
		if status.ValidationError != nil {
			errMsg = *status.ValidationError
		}
		t.Fatalf("expected VALID status, got %s: %s", status.Status, errMsg)
	}
	if status.LatestValidHash == nil {
		t.Error("expected LatestValidHash to be set")
	}

	// Check block was stored.
	b.mu.RLock()
	if len(b.blocks) != 2 {
		t.Errorf("expected 2 blocks, got %d", len(b.blocks))
	}
	b.mu.RUnlock()
}

func TestForkchoiceUpdated(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	result, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated returned error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID status, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID != nil {
		t.Error("expected nil PayloadID without attributes")
	}
	if b.headHash != genesisHash {
		t.Error("headHash not updated")
	}
	if b.safeHash != genesisHash {
		t.Error("safeHash not updated")
	}
	if b.finalHash != genesisHash {
		t.Error("finalHash not updated")
	}
}

func TestForkchoiceUpdated_UnknownHead(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	unknownHash := types.HexToHash("0xdeadbeef")
	result, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      unknownHash,
			SafeBlockHash:      unknownHash,
			FinalizedBlockHash: unknownHash,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated returned error: %v", err)
	}
	if result.PayloadStatus.Status != StatusSyncing {
		t.Errorf("expected SYNCING for unknown head, got %s", result.PayloadStatus.Status)
	}
}

func TestForkchoiceWithPayloadAttributes(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	attrs := &PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000012,
				PrevRandao:            types.HexToHash("0xrandao"),
				SuggestedFeeRecipient: types.HexToAddress("0xfee"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xbeacon"),
	}

	result, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated returned error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil PayloadID")
	}

	// Verify payload was stored.
	b.mu.RLock()
	_, ok := b.payloads[*result.PayloadID]
	b.mu.RUnlock()
	if !ok {
		t.Error("payload not stored in backend")
	}
}

func TestGetPayload(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Build a payload via forkchoice.
	attrs := &PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000012,
				PrevRandao:            types.HexToHash("0xrandao"),
				SuggestedFeeRecipient: types.HexToAddress("0xfee"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xbeacon"),
	}

	result, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated error: %v", err)
	}
	if result.PayloadID == nil {
		t.Fatal("expected PayloadID")
	}

	// Retrieve the payload.
	resp, err := b.GetPayloadByID(*result.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadByID error: %v", err)
	}
	if resp.ExecutionPayload == nil {
		t.Fatal("expected non-nil ExecutionPayload")
	}
	if resp.ExecutionPayload.BlockNumber != 1 {
		t.Errorf("expected block number 1, got %d", resp.ExecutionPayload.BlockNumber)
	}
	if resp.ExecutionPayload.Timestamp != 1700000012 {
		t.Errorf("expected timestamp 1700000012, got %d", resp.ExecutionPayload.Timestamp)
	}
	if resp.ExecutionPayload.FeeRecipient != types.HexToAddress("0xfee") {
		t.Error("fee recipient mismatch")
	}
	if resp.BlockValue == nil {
		t.Error("expected non-nil BlockValue")
	}
}

func TestGetPayload_Unknown(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	unknownID := PayloadID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	_, err := b.GetPayloadByID(unknownID)
	if err != ErrUnknownPayload {
		t.Errorf("expected ErrUnknownPayload, got %v", err)
	}
}

func TestProcessInvalidBlock(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	// Create a payload with unknown parent.
	unknownParent := types.HexToHash("0xdeadbeef")
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    unknownParent,
				FeeRecipient:  types.HexToAddress("0xfee"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000012,
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	status, err := b.ProcessBlock(payload, nil, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBlock returned unexpected error: %v", err)
	}
	// With an unknown parent, the node should return SYNCING.
	if status.Status != StatusSyncing {
		t.Errorf("expected SYNCING for unknown parent, got %s", status.Status)
	}
}

func TestProcessBlock_InvalidTransaction(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Create a payload with garbage transaction data.
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xfee"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000012,
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{{0xde, 0xad}}, // invalid RLP
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	status, err := b.ProcessBlock(payload, nil, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBlock returned unexpected error: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Errorf("expected INVALID for bad tx, got %s", status.Status)
	}
	if status.ValidationError == nil {
		t.Error("expected ValidationError to be set")
	}
}

func TestBackendImplementsInterface(t *testing.T) {
	var _ Backend = (*EngineBackend)(nil)
}
