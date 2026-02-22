package engine

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// makeGenesisAt creates a genesis block with a specified timestamp.
func makeGenesisAt(timestamp uint64) *types.Block {
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
		Time:          timestamp,
		BlobGasUsed:   &blobGas,
		ExcessBlobGas: &excessBlobGas,
	}
	return types.NewBlock(header, nil)
}

// newBackendWithGenesis creates a backend with a genesis block at timestamp ts.
func newBackendWithGenesis(ts uint64) (*EngineBackend, *types.Block) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesisAt(ts)
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
	return b, genesis
}

// --- ForkchoiceUpdated: head selection and state update ---

func TestForkchoiceUpdated_HeadSelection(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	// Build and process a valid child block.
	payload1 := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xfee"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				GasUsed:       0,
				Timestamp:     1700000012,
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}
	status1, err := b.ProcessBlock(payload1, nil, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if status1.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", status1.Status)
	}
	block1Hash := *status1.LatestValidHash

	// Move head to block1.
	result, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      block1Hash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}

	// Verify head is updated.
	b.mu.RLock()
	if b.headHash != block1Hash {
		t.Errorf("headHash = %s, want %s", b.headHash.Hex(), block1Hash.Hex())
	}
	b.mu.RUnlock()
}

// --- Safe and finalized block tracking ---

func TestForkchoiceUpdated_SafeFinalizedTracking(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	safeHash := types.HexToHash("0xaaaa000000000000000000000000000000000000000000000000000000000001")
	finalizedHash := types.HexToHash("0xbbbb000000000000000000000000000000000000000000000000000000000001")

	// Set forkchoice with zero head (allowed per spec).
	result, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      types.Hash{},
			SafeBlockHash:      safeHash,
			FinalizedBlockHash: finalizedHash,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID for zero head, got %s", result.PayloadStatus.Status)
	}

	b.mu.RLock()
	if b.safeHash != safeHash {
		t.Errorf("safeHash = %s, want %s", b.safeHash.Hex(), safeHash.Hex())
	}
	if b.finalHash != finalizedHash {
		t.Errorf("finalHash = %s, want %s", b.finalHash.Hex(), finalizedHash.Hex())
	}
	b.mu.RUnlock()

	// Now update all three to genesis hash.
	_, err = b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated: %v", err)
	}

	b.mu.RLock()
	if b.headHash != genesisHash {
		t.Errorf("headHash not updated to genesis")
	}
	if b.safeHash != genesisHash {
		t.Errorf("safeHash not updated to genesis")
	}
	if b.finalHash != genesisHash {
		t.Errorf("finalHash not updated to genesis")
	}
	b.mu.RUnlock()
}

// --- Reorg handling: switch head to a different branch ---

func TestForkchoiceUpdated_ReorgToNewHead(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	// Build block A at height 1.
	payloadA := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xaaa"),
				PrevRandao:    types.HexToHash("0xaaa1"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000012,
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}
	statusA, _ := b.ProcessBlock(payloadA, nil, types.Hash{})
	blockAHash := *statusA.LatestValidHash

	// Build block B at height 1 (different branch from genesis).
	payloadB := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xbbb"),
				PrevRandao:    types.HexToHash("0xbbb2"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000015,
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}
	statusB, _ := b.ProcessBlock(payloadB, nil, types.Hash{})
	blockBHash := *statusB.LatestValidHash

	// Point head to block A.
	b.ForkchoiceUpdated(ForkchoiceStateV1{
		HeadBlockHash:      blockAHash,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	}, nil)

	b.mu.RLock()
	if b.headHash != blockAHash {
		t.Errorf("head should be block A")
	}
	b.mu.RUnlock()

	// Reorg: switch head to block B.
	result, err := b.ForkchoiceUpdated(ForkchoiceStateV1{
		HeadBlockHash:      blockBHash,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	}, nil)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated (reorg): %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}

	b.mu.RLock()
	if b.headHash != blockBHash {
		t.Errorf("head should have reorged to block B")
	}
	b.mu.RUnlock()
}

// --- Invalid payload detection: bad timestamp ---

func TestProcessBlock_InvalidTimestamp(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	// Payload with timestamp <= parent timestamp should be INVALID.
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xfee"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000000, // same as parent
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	status, err := b.ProcessBlock(payload, nil, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Errorf("expected INVALID for non-progressing timestamp, got %s", status.Status)
	}
	if status.ValidationError == nil {
		t.Error("expected ValidationError to be set")
	}
}

// --- Payload attributes: invalid timestamp rejected ---

func TestForkchoiceUpdated_InvalidPayloadAttrsTimestamp(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	// Zero timestamp in attributes should return an error.
	attrs := &PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp: 0,
			},
		},
	}

	_, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != ErrInvalidPayloadAttributes {
		t.Errorf("expected ErrInvalidPayloadAttributes, got %v", err)
	}
}

// --- Payload attributes: timestamp not progressing past parent ---

func TestForkchoiceUpdated_PayloadAttrsTimestampNotProgressing(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	// Timestamp equal to parent should fail.
	attrs := &PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000000, // same as genesis
				PrevRandao:            types.HexToHash("0xrand"),
				SuggestedFeeRecipient: types.HexToAddress("0xfee"),
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	_, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != ErrInvalidPayloadAttributes {
		t.Errorf("expected ErrInvalidPayloadAttributes, got %v", err)
	}
}

// --- ForkchoiceUpdatedV4: with payload attributes including FOCIL ILs ---

func TestForkchoiceUpdatedV4_WithILAttributes(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	attrs := &PayloadAttributesV4{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             1700000012,
					PrevRandao:            types.HexToHash("0xrandao"),
					SuggestedFeeRecipient: types.HexToAddress("0xfee"),
				},
				Withdrawals: []*Withdrawal{},
			},
			ParentBeaconBlockRoot: types.HexToHash("0xbeacon"),
		},
		SlotNumber:                42,
		InclusionListTransactions: [][]byte{},
	}

	result, err := b.ForkchoiceUpdatedV4(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdatedV4: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil PayloadID")
	}
}

// --- ForkchoiceUpdatedV4: unknown head returns SYNCING ---

func TestForkchoiceUpdatedV4_UnknownHead(t *testing.T) {
	b, _ := newBackendWithGenesis(1700000000)
	unknownHash := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeef00000000000000000000000000000001")

	result, err := b.ForkchoiceUpdatedV4(
		ForkchoiceStateV1{
			HeadBlockHash:      unknownHash,
			SafeBlockHash:      unknownHash,
			FinalizedBlockHash: unknownHash,
		},
		nil,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdatedV4: %v", err)
	}
	if result.PayloadStatus.Status != StatusSyncing {
		t.Errorf("expected SYNCING for unknown head, got %s", result.PayloadStatus.Status)
	}
}

// --- ForkchoiceUpdatedV4: zero timestamp in attributes ---

func TestForkchoiceUpdatedV4_InvalidAttrsTimestamp(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	attrs := &PayloadAttributesV4{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp: 0,
				},
			},
		},
	}

	_, err := b.ForkchoiceUpdatedV4(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != ErrInvalidPayloadAttributes {
		t.Errorf("expected ErrInvalidPayloadAttributes, got %v", err)
	}
}

// --- GetPayloadV4ByID and GetPayloadV6ByID: unknown IDs ---

func TestGetPayloadV4ByID_Unknown(t *testing.T) {
	b, _ := newBackendWithGenesis(1700000000)

	unknownID := PayloadID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x02}
	_, err := b.GetPayloadV4ByID(unknownID)
	if err != ErrUnknownPayload {
		t.Errorf("expected ErrUnknownPayload, got %v", err)
	}
}

func TestGetPayloadV6ByID_Unknown(t *testing.T) {
	b, _ := newBackendWithGenesis(1700000000)

	unknownID := PayloadID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	_, err := b.GetPayloadV6ByID(unknownID)
	if err != ErrUnknownPayload {
		t.Errorf("expected ErrUnknownPayload, got %v", err)
	}
}

// --- GetHeadTimestamp ---

func TestGetHeadTimestamp(t *testing.T) {
	b, _ := newBackendWithGenesis(1700000000)
	if ts := b.GetHeadTimestamp(); ts != 1700000000 {
		t.Errorf("GetHeadTimestamp = %d, want 1700000000", ts)
	}
}

// --- ProcessBlock: block hash mismatch detection ---

func TestProcessBlock_BlockHashMismatch(t *testing.T) {
	b, genesis := newBackendWithGenesis(1700000000)
	genesisHash := genesis.Hash()

	wrongHash := types.HexToHash("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddead0099")
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
				BlockHash:     wrongHash,
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	status, err := b.ProcessBlock(payload, nil, types.Hash{})
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if status.Status != StatusInvalidBlockHash {
		t.Errorf("expected INVALID_BLOCK_HASH, got %s", status.Status)
	}
}
