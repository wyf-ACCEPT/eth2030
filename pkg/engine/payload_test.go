package engine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core"
	"github.com/eth2028/eth2028/core/state"
	"github.com/eth2028/eth2028/core/types"
)

// --- TestGetPayloadV4 ---

func TestGetPayloadV4_Success(t *testing.T) {
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	backend := &mockBackend{
		getPayloadV4ByIDFn: func(id PayloadID) (*GetPayloadV4Response, error) {
			if id != payloadID {
				t.Errorf("unexpected payload ID")
			}
			return &GetPayloadV4Response{
				ExecutionPayload: &ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockNumber:   100,
							GasLimit:      30_000_000,
							Timestamp:     1700000012,
							BaseFeePerGas: big.NewInt(1_000_000_000),
						},
					},
				},
				BlockValue:        big.NewInt(1_000_000),
				BlobsBundle:       &BlobsBundleV1{},
				ExecutionRequests: [][]byte{{0x00, 0x01}},
			}, nil
		},
		isPragueFn: func(timestamp uint64) bool {
			return true // Prague is active
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV4","params":[%s],"id":1}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result GetPayloadV4Response
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.ExecutionPayload == nil {
		t.Fatal("expected non-nil execution payload")
	}
	if result.ExecutionPayload.BlockNumber != 100 {
		t.Errorf("expected block number 100, got %d", result.ExecutionPayload.BlockNumber)
	}
	if len(result.ExecutionRequests) != 1 {
		t.Errorf("expected 1 execution request, got %d", len(result.ExecutionRequests))
	}
}

func TestGetPayloadV4_UnsupportedFork(t *testing.T) {
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	backend := &mockBackend{
		getPayloadV4ByIDFn: func(id PayloadID) (*GetPayloadV4Response, error) {
			return &GetPayloadV4Response{
				ExecutionPayload: &ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							Timestamp:     1700000012,
							BaseFeePerGas: big.NewInt(1_000_000_000),
						},
					},
				},
				BlockValue:        big.NewInt(0),
				ExecutionRequests: [][]byte{},
			}, nil
		},
		isPragueFn: func(timestamp uint64) bool {
			return false // Prague is NOT active
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV4","params":[%s],"id":2}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unsupported fork")
	}
	if rpcResp.Error.Code != UnsupportedForkCode {
		t.Errorf("expected error code %d, got %d", UnsupportedForkCode, rpcResp.Error.Code)
	}
}

// --- TestNewPayloadV5 ---

func TestNewPayloadV5_ValidBAL(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	backend := &mockBackend{
		processBlockV5Fn: func(payload *ExecutionPayloadV5, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
			if payload.BlockAccessList == nil {
				t.Error("expected blockAccessList to be present")
			}
			latestValid := payload.BlockHash
			return PayloadStatusV1{
				Status:          StatusValid,
				LatestValidHash: &latestValid,
			}, nil
		},
		isAmsterdamFn: func(timestamp uint64) bool {
			return true // Amsterdam is active
		},
	}

	api := NewEngineAPI(backend)

	// Build a V5 payload with BAL.
	balData, _ := json.Marshal([]byte{0xc0}) // minimal RLP (empty list)
	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(blockHash, 100),
		},
		BlockAccessList: balData,
	}

	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":3}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusValid {
		t.Errorf("expected VALID, got %s", status.Status)
	}
}

func TestNewPayloadV5_InvalidBAL(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	backend := &mockBackend{
		processBlockV5Fn: func(payload *ExecutionPayloadV5, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
			// Return INVALID because BAL mismatch.
			errMsg := "blockAccessList mismatch"
			return PayloadStatusV1{
				Status:          StatusInvalid,
				ValidationError: &errMsg,
			}, nil
		},
		isAmsterdamFn: func(timestamp uint64) bool {
			return true
		},
	}

	api := NewEngineAPI(backend)

	// Build a V5 payload with a wrong BAL.
	wrongBALData, _ := json.Marshal([]byte{0xde, 0xad, 0xbe, 0xef})
	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(blockHash, 100),
		},
		BlockAccessList: wrongBALData,
	}

	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":4}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Errorf("expected INVALID for BAL mismatch, got %s", status.Status)
	}
	if status.ValidationError == nil {
		t.Error("expected validation error to be set")
	}
}

func TestNewPayloadV5_MissingBAL(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef")
	backend := &mockBackend{
		isAmsterdamFn: func(timestamp uint64) bool {
			return true
		},
	}

	api := NewEngineAPI(backend)

	// Build a V5 payload without the blockAccessList field (nil).
	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(blockHash, 100),
		},
		BlockAccessList: nil, // missing
	}

	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":5}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for missing BAL")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d (-32602), got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

func TestNewPayloadV5_UnsupportedFork(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef")
	backend := &mockBackend{
		isAmsterdamFn: func(timestamp uint64) bool {
			return false // Amsterdam is NOT active
		},
	}

	api := NewEngineAPI(backend)

	balData, _ := json.Marshal([]byte{0xc0})
	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(blockHash, 100),
		},
		BlockAccessList: balData,
	}

	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":6}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unsupported fork")
	}
	if rpcResp.Error.Code != UnsupportedForkCode {
		t.Errorf("expected error code %d (-38005), got %d", UnsupportedForkCode, rpcResp.Error.Code)
	}
}

// --- TestForkchoiceUpdatedV4 ---

func TestForkchoiceUpdatedV4_StartBuild(t *testing.T) {
	expectedID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockBackend{
		forkchoiceUpdV4Fn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV4) (ForkchoiceUpdatedResult, error) {
			if attrs == nil {
				t.Fatal("expected non-nil payload attributes")
			}
			if attrs.Timestamp != 1700000012 {
				t.Errorf("expected timestamp 1700000012, got %d", attrs.Timestamp)
			}
			if attrs.SlotNumber != 42 {
				t.Errorf("expected slot number 42, got %d", attrs.SlotNumber)
			}
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{Status: StatusValid},
				PayloadID:     &expectedID,
			}, nil
		},
		isAmsterdamFn: func(timestamp uint64) bool {
			return true
		},
	}

	api := NewEngineAPI(backend)

	fcState := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	attrs := PayloadAttributesV4{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             1700000012,
					PrevRandao:            types.HexToHash("0xaaaa"),
					SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
				},
				Withdrawals: []*Withdrawal{},
			},
			ParentBeaconBlockRoot: types.HexToHash("0xcccc"),
		},
		SlotNumber: 42,
	}

	stateJSON, _ := json.Marshal(fcState)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV4","params":[%s,%s],"id":7}`,
		stateJSON, attrsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result ForkchoiceUpdatedResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil payload ID")
	}
	if *result.PayloadID != expectedID {
		t.Errorf("payload ID mismatch: got %s, want %s", result.PayloadID.String(), expectedID.String())
	}
}

func TestForkchoiceUpdatedV4_UnsupportedFork(t *testing.T) {
	backend := &mockBackend{
		isAmsterdamFn: func(timestamp uint64) bool {
			return false // Amsterdam NOT active
		},
	}

	api := NewEngineAPI(backend)

	fcState := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	attrs := PayloadAttributesV4{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             1700000012,
					PrevRandao:            types.HexToHash("0xaaaa"),
					SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
				},
				Withdrawals: []*Withdrawal{},
			},
			ParentBeaconBlockRoot: types.HexToHash("0xcccc"),
		},
		SlotNumber: 42,
	}

	stateJSON, _ := json.Marshal(fcState)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV4","params":[%s,%s],"id":8}`,
		stateJSON, attrsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unsupported fork")
	}
	if rpcResp.Error.Code != UnsupportedForkCode {
		t.Errorf("expected error code %d (-38005), got %d", UnsupportedForkCode, rpcResp.Error.Code)
	}
}

func TestForkchoiceUpdatedV4_NullAttributes(t *testing.T) {
	headHash := types.HexToHash("0x1111")
	backend := &mockBackend{
		forkchoiceUpdV4Fn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV4) (ForkchoiceUpdatedResult, error) {
			if attrs != nil {
				t.Error("expected nil payload attributes")
			}
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{
					Status:          StatusValid,
					LatestValidHash: &headHash,
				},
			}, nil
		},
		isAmsterdamFn: func(timestamp uint64) bool {
			return true
		},
	}

	api := NewEngineAPI(backend)

	fcState := ForkchoiceStateV1{
		HeadBlockHash:      headHash,
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	stateJSON, _ := json.Marshal(fcState)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV4","params":[%s,null],"id":9}`, stateJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result ForkchoiceUpdatedResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID != nil {
		t.Error("expected nil payload ID without attributes")
	}
}

// --- TestGetPayloadV6 ---

func TestGetPayloadV6_Success(t *testing.T) {
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	backend := &mockBackend{
		getPayloadV6ByIDFn: func(id PayloadID) (*GetPayloadV6Response, error) {
			if id != payloadID {
				t.Errorf("unexpected payload ID")
			}
			balData, _ := json.Marshal([]byte{0xc0})
			return &GetPayloadV6Response{
				ExecutionPayload: &ExecutionPayloadV5{
					ExecutionPayloadV4: ExecutionPayloadV4{
						ExecutionPayloadV3: ExecutionPayloadV3{
							ExecutionPayloadV2: ExecutionPayloadV2{
								ExecutionPayloadV1: ExecutionPayloadV1{
									BlockNumber:   200,
									GasLimit:      30_000_000,
									Timestamp:     1700000024,
									BaseFeePerGas: big.NewInt(1_000_000_000),
								},
							},
						},
					},
					BlockAccessList: balData,
				},
				BlockValue:        big.NewInt(2_000_000),
				BlobsBundle:       &BlobsBundleV1{},
				ExecutionRequests: [][]byte{},
			}, nil
		},
		isAmsterdamFn: func(timestamp uint64) bool {
			return true
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV6","params":[%s],"id":10}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result GetPayloadV6Response
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.ExecutionPayload == nil {
		t.Fatal("expected non-nil execution payload")
	}
	if result.ExecutionPayload.BlockNumber != 200 {
		t.Errorf("expected block number 200, got %d", result.ExecutionPayload.BlockNumber)
	}
	if result.ExecutionPayload.BlockAccessList == nil {
		t.Error("expected blockAccessList to be set")
	}
}

func TestGetPayloadV6_UnsupportedFork(t *testing.T) {
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}

	backend := &mockBackend{
		getPayloadV6ByIDFn: func(id PayloadID) (*GetPayloadV6Response, error) {
			return &GetPayloadV6Response{
				ExecutionPayload: &ExecutionPayloadV5{
					ExecutionPayloadV4: ExecutionPayloadV4{
						ExecutionPayloadV3: ExecutionPayloadV3{
							ExecutionPayloadV2: ExecutionPayloadV2{
								ExecutionPayloadV1: ExecutionPayloadV1{
									Timestamp:     1700000024,
									BaseFeePerGas: big.NewInt(1_000_000_000),
								},
							},
						},
					},
				},
				BlockValue:        big.NewInt(0),
				ExecutionRequests: [][]byte{},
			}, nil
		},
		isAmsterdamFn: func(timestamp uint64) bool {
			return false // Amsterdam NOT active
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV6","params":[%s],"id":11}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unsupported fork")
	}
	if rpcResp.Error.Code != UnsupportedForkCode {
		t.Errorf("expected error code %d (-38005), got %d", UnsupportedForkCode, rpcResp.Error.Code)
	}
}

// --- TestPayloadBuilder ---

func TestPayloadBuilder_BlockValue(t *testing.T) {
	// Test block value calculation with the real EngineBackend.
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Build a payload via forkchoice (empty block).
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

	// Get the V4 payload to check block value.
	resp, err := b.GetPayloadV4ByID(*result.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadV4ByID error: %v", err)
	}
	if resp.ExecutionPayload == nil {
		t.Fatal("expected non-nil ExecutionPayload")
	}
	if resp.BlockValue == nil {
		t.Fatal("expected non-nil BlockValue")
	}
	// Empty block should have zero block value.
	if resp.BlockValue.Sign() != 0 {
		t.Errorf("expected zero block value for empty block, got %s", resp.BlockValue)
	}
	if resp.ExecutionRequests == nil {
		t.Error("expected non-nil ExecutionRequests")
	}
}

func TestPayloadBuilder_GetPayloadV6(t *testing.T) {
	// Test GetPayloadV6ByID with the real EngineBackend (Amsterdam config).
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
		t.Fatalf("ForkchoiceUpdated error: %v", err)
	}
	if result.PayloadID == nil {
		t.Fatal("expected PayloadID")
	}

	resp, err := b.GetPayloadV6ByID(*result.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadV6ByID error: %v", err)
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
	if resp.ExecutionRequests == nil {
		t.Error("expected non-nil ExecutionRequests")
	}
}

// --- TestForkchoiceUpdatedV4 with real backend ---

func TestForkchoiceUpdatedV4_RealBackend(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)
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
		SlotNumber: 1,
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
		t.Fatalf("ForkchoiceUpdatedV4 error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil PayloadID")
	}

	// Retrieve using V6.
	resp, err := b.GetPayloadV6ByID(*result.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadV6ByID error: %v", err)
	}
	if resp.ExecutionPayload == nil {
		t.Fatal("expected non-nil ExecutionPayload")
	}
}

// --- TestIsPrague / TestIsAmsterdam ---

func TestBackend_IsPrague(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	// TestConfig has Prague at timestamp 0, so any timestamp should be Prague.
	if !b.IsPrague(1700000000) {
		t.Error("expected IsPrague to return true with TestConfig")
	}
}

func TestBackend_IsAmsterdam(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(core.TestConfig, statedb, genesis)

	// TestConfig has Amsterdam at timestamp 0, so any timestamp should be Amsterdam.
	if !b.IsAmsterdam(1700000000) {
		t.Error("expected IsAmsterdam to return true with TestConfig")
	}
}

func TestBackend_IsNotAmsterdam(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	// Use a config where Amsterdam is not active.
	b := NewEngineBackend(SepoliaConfig, statedb, genesis)

	if b.IsAmsterdam(1700000000) {
		t.Error("expected IsAmsterdam to return false for Sepolia (Amsterdam not scheduled)")
	}
}

// SepoliaConfig mirrors the Sepolia test net config (no Amsterdam).
var SepoliaConfig = core.SepoliaConfig
