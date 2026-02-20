package engine

import (
	"encoding/json"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

// mockGlamsterdamBackend implements GlamsterdamBackend for testing.
type mockGlamsterdamBackend struct {
	mu sync.Mutex

	newPayloadResp *PayloadStatusV1
	newPayloadErr  error
	fcuResult      *ForkchoiceUpdatedResult
	fcuErr         error
	getPayloadResp *GetPayloadV5Response
	getPayloadErr  error
	getBlobsResp   []*BlobAndProofV2
	getBlobsErr    error

	// Track calls for assertions.
	lastPayload    *ExecutionPayloadV5
	lastBlobHashes []types.Hash
	lastBeaconRoot types.Hash
	lastRequests   [][]byte
	lastFCState    *ForkchoiceStateV1
	lastAttrs      *GlamsterdamPayloadAttributes
	lastPayloadID  PayloadID
	lastBlobQuery  []types.Hash
}

func (m *mockGlamsterdamBackend) NewPayloadV5(
	payload *ExecutionPayloadV5,
	hashes []types.Hash,
	root types.Hash,
	requests [][]byte,
) (*PayloadStatusV1, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPayload = payload
	m.lastBlobHashes = hashes
	m.lastBeaconRoot = root
	m.lastRequests = requests
	if m.newPayloadErr != nil {
		return nil, m.newPayloadErr
	}
	if m.newPayloadResp != nil {
		return m.newPayloadResp, nil
	}
	hash := payload.BlockHash
	return &PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &hash,
	}, nil
}

func (m *mockGlamsterdamBackend) ForkchoiceUpdatedV4G(
	state *ForkchoiceStateV1,
	attrs *GlamsterdamPayloadAttributes,
) (*ForkchoiceUpdatedResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastFCState = state
	m.lastAttrs = attrs
	if m.fcuErr != nil {
		return nil, m.fcuErr
	}
	if m.fcuResult != nil {
		return m.fcuResult, nil
	}
	hash := state.HeadBlockHash
	return &ForkchoiceUpdatedResult{
		PayloadStatus: PayloadStatusV1{
			Status:          StatusValid,
			LatestValidHash: &hash,
		},
	}, nil
}

func (m *mockGlamsterdamBackend) GetPayloadV5(id PayloadID) (*GetPayloadV5Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPayloadID = id
	if m.getPayloadErr != nil {
		return nil, m.getPayloadErr
	}
	if m.getPayloadResp != nil {
		return m.getPayloadResp, nil
	}
	return nil, ErrUnknownPayload
}

func (m *mockGlamsterdamBackend) GetBlobsV2(hashes []types.Hash) ([]*BlobAndProofV2, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastBlobQuery = hashes
	if m.getBlobsErr != nil {
		return nil, m.getBlobsErr
	}
	return m.getBlobsResp, nil
}

func makeGlamsterdamPayload() *ExecutionPayloadV5 {
	return &ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: ExecutionPayloadV3{
				ExecutionPayloadV2: ExecutionPayloadV2{
					ExecutionPayloadV1: ExecutionPayloadV1{
						ParentHash:    types.HexToHash("0x01"),
						FeeRecipient:  types.HexToAddress("0xdead"),
						StateRoot:     types.HexToHash("0x02"),
						ReceiptsRoot:  types.HexToHash("0x03"),
						BlockNumber:   200,
						GasLimit:      30_000_000,
						GasUsed:       21_000,
						Timestamp:     1800000000,
						BaseFeePerGas: big.NewInt(1_000_000_000),
						BlockHash:     types.HexToHash("0xbb"),
						Transactions:  [][]byte{},
					},
					Withdrawals: []*Withdrawal{},
				},
				BlobGasUsed:   0,
				ExcessBlobGas: 0,
			},
			ExecutionRequests: [][]byte{},
		},
		BlockAccessList: json.RawMessage(`[]`),
	}
}

func TestNewEngineGlamsterdam(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)
	if e == nil {
		t.Fatal("NewEngineGlamsterdam returned nil")
	}
}

func TestGlamsterdam_NewPayloadV5_Valid(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	root := types.HexToHash("0xbeef")
	requests := [][]byte{{0x01, 0xaa}, {0x02, 0xbb}}

	status, err := e.HandleNewPayloadV5(payload, nil, root, requests)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", status.Status)
	}
	if status.LatestValidHash == nil || *status.LatestValidHash != payload.BlockHash {
		t.Fatal("LatestValidHash mismatch")
	}
}

func TestGlamsterdam_NewPayloadV5_NilPayload(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	_, err := e.HandleNewPayloadV5(nil, nil, types.HexToHash("0xbeef"), [][]byte{})
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_ZeroBeaconRoot(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	_, err := e.HandleNewPayloadV5(payload, nil, types.Hash{}, [][]byte{})
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_NilRequests(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), nil)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_NilBlockAccessList(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	payload.BlockAccessList = nil
	_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), [][]byte{})
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_BadRequestOrder(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	// Request types not ascending (0x02 before 0x01).
	requests := [][]byte{{0x02, 0xaa}, {0x01, 0xbb}}
	_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), requests)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_ShortRequest(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	// Request with only 1 byte (too short per EIP-7685).
	requests := [][]byte{{0x01}}
	_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), requests)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_DuplicateRequestType(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	// Duplicate request type 0x01.
	requests := [][]byte{{0x01, 0xaa}, {0x01, 0xbb}}
	_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), requests)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_NewPayloadV5_BackendError(t *testing.T) {
	backend := &mockGlamsterdamBackend{newPayloadErr: ErrUnsupportedFork}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), [][]byte{})
	if err != ErrUnsupportedFork {
		t.Fatalf("expected ErrUnsupportedFork, got %v", err)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_Valid(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		SafeBlockHash:      types.HexToHash("0xbb"),
		FinalizedBlockHash: types.HexToHash("0xcc"),
	}
	attrs := &GlamsterdamPayloadAttributes{
		Timestamp:             1800000000,
		PrevRandao:            types.HexToHash("0xdd"),
		SuggestedFeeRecipient: types.HexToAddress("0xdead"),
		ParentBeaconBlockRoot: types.HexToHash("0xee"),
		TargetBlobCount:       6,
		SlotNumber:            100,
	}

	result, err := e.HandleForkchoiceUpdatedV4(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_NilState(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	_, err := e.HandleForkchoiceUpdatedV4(nil, nil)
	if err != ErrInvalidForkchoiceState {
		t.Fatalf("expected ErrInvalidForkchoiceState, got %v", err)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_ZeroHead(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	_, err := e.HandleForkchoiceUpdatedV4(&ForkchoiceStateV1{}, nil)
	if err != ErrInvalidForkchoiceState {
		t.Fatalf("expected ErrInvalidForkchoiceState, got %v", err)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_NilAttrs(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	state := &ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
	result, err := e.HandleForkchoiceUpdatedV4(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_ZeroTimestamp(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	state := &ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
	attrs := &GlamsterdamPayloadAttributes{
		Timestamp:             0,
		ParentBeaconBlockRoot: types.HexToHash("0xee"),
	}
	_, err := e.HandleForkchoiceUpdatedV4(state, attrs)
	if err != ErrInvalidPayloadAttributes {
		t.Fatalf("expected ErrInvalidPayloadAttributes, got %v", err)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_ZeroBeaconRoot(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	state := &ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
	attrs := &GlamsterdamPayloadAttributes{
		Timestamp:             1800000000,
		ParentBeaconBlockRoot: types.Hash{}, // zero
	}
	_, err := e.HandleForkchoiceUpdatedV4(state, attrs)
	if err != ErrInvalidPayloadAttributes {
		t.Fatalf("expected ErrInvalidPayloadAttributes, got %v", err)
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_WithPayloadID(t *testing.T) {
	pid := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockGlamsterdamBackend{
		fcuResult: &ForkchoiceUpdatedResult{
			PayloadStatus: PayloadStatusV1{Status: StatusValid},
			PayloadID:     &pid,
		},
	}
	e := NewEngineGlamsterdam(backend)

	state := &ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
	attrs := &GlamsterdamPayloadAttributes{
		Timestamp:             1800000000,
		ParentBeaconBlockRoot: types.HexToHash("0xee"),
	}

	result, err := e.HandleForkchoiceUpdatedV4(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadID == nil || *result.PayloadID != pid {
		t.Fatal("PayloadID mismatch")
	}
}

func TestGlamsterdam_ForkchoiceUpdatedV4_BackendError(t *testing.T) {
	backend := &mockGlamsterdamBackend{fcuErr: ErrInvalidForkchoiceState}
	e := NewEngineGlamsterdam(backend)

	state := &ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
	_, err := e.HandleForkchoiceUpdatedV4(state, nil)
	if err != ErrInvalidForkchoiceState {
		t.Fatalf("expected ErrInvalidForkchoiceState, got %v", err)
	}
}

func TestGlamsterdam_GetPayloadV5_Valid(t *testing.T) {
	expected := &GetPayloadV5Response{
		ExecutionPayload: &ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					BlockHash:   types.HexToHash("0xcc"),
					BlockNumber: 200,
				},
			},
		},
		BlobsBundle:       &BlobsBundleV2{},
		ExecutionRequests: [][]byte{},
	}
	backend := &mockGlamsterdamBackend{getPayloadResp: expected}
	e := NewEngineGlamsterdam(backend)

	id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	result, err := e.HandleGetPayloadV5(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExecutionPayload.BlockHash != expected.ExecutionPayload.BlockHash {
		t.Fatal("payload mismatch")
	}
}

func TestGlamsterdam_GetPayloadV5_ZeroID(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	_, err := e.HandleGetPayloadV5(PayloadID{})
	if err != ErrUnknownPayload {
		t.Fatalf("expected ErrUnknownPayload, got %v", err)
	}
}

func TestGlamsterdam_GetPayloadV5_NotFound(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	_, err := e.HandleGetPayloadV5(id)
	if err != ErrUnknownPayload {
		t.Fatalf("expected ErrUnknownPayload, got %v", err)
	}
}

func TestGlamsterdam_GetBlobsV2_Valid(t *testing.T) {
	expectedBlobs := []*BlobAndProofV2{
		{Blob: make([]byte, 131072), Proofs: [][]byte{{0x01}}},
	}
	backend := &mockGlamsterdamBackend{getBlobsResp: expectedBlobs}
	e := NewEngineGlamsterdam(backend)

	hashes := []types.Hash{types.HexToHash("0xaa")}
	result, err := e.HandleGetBlobsV2(hashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 blob, got %d", len(result))
	}
}

func TestGlamsterdam_GetBlobsV2_NilHashes(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	_, err := e.HandleGetBlobsV2(nil)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestGlamsterdam_GetBlobsV2_TooMany(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	hashes := make([]types.Hash, 129) // exceeds 128
	_, err := e.HandleGetBlobsV2(hashes)
	if err != ErrTooLargeRequest {
		t.Fatalf("expected ErrTooLargeRequest, got %v", err)
	}
}

func TestGlamsterdam_GetBlobsV2_ExactLimit(t *testing.T) {
	backend := &mockGlamsterdamBackend{getBlobsResp: make([]*BlobAndProofV2, 128)}
	e := NewEngineGlamsterdam(backend)

	hashes := make([]types.Hash, 128) // exactly 128 is OK
	_, err := e.HandleGetBlobsV2(hashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGlamsterdam_GetBlobsV2_MissingBlob(t *testing.T) {
	// Backend returns nil (all-or-nothing when any blob missing).
	backend := &mockGlamsterdamBackend{getBlobsResp: nil}
	e := NewEngineGlamsterdam(backend)

	hashes := []types.Hash{types.HexToHash("0xaa")}
	result, err := e.HandleGetBlobsV2(hashes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for missing blobs")
	}
}

func TestGlamsterdam_GetClientVersionV2(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	versions := e.HandleGetClientVersionV2(nil)
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	v := versions[0]
	if v.Code != "ET" {
		t.Fatalf("expected code ET, got %s", v.Code)
	}
	if v.Name != "eth2028" {
		t.Fatalf("expected name eth2028, got %s", v.Name)
	}
	if v.OS != "linux" {
		t.Fatalf("expected OS linux, got %s", v.OS)
	}
	if v.Language != "go" {
		t.Fatalf("expected language go, got %s", v.Language)
	}
	if len(v.Capabilities) == 0 {
		t.Fatal("expected non-empty capabilities")
	}
}

func TestGlamsterdam_Concurrency(t *testing.T) {
	backend := &mockGlamsterdamBackend{
		getBlobsResp: []*BlobAndProofV2{{Blob: []byte{0x01}, Proofs: [][]byte{}}},
	}
	e := NewEngineGlamsterdam(backend)

	var wg sync.WaitGroup
	errCh := make(chan error, 40)

	// Concurrent HandleNewPayloadV5.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := makeGlamsterdamPayload()
			_, err := e.HandleNewPayloadV5(payload, nil, types.HexToHash("0xbeef"), [][]byte{})
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Concurrent HandleForkchoiceUpdatedV4.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := &ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
			_, err := e.HandleForkchoiceUpdatedV4(state, nil)
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Concurrent HandleGetBlobsV2.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hashes := []types.Hash{types.HexToHash("0xaa")}
			_, err := e.HandleGetBlobsV2(hashes)
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Concurrent HandleGetPayloadV5 (expected to fail with ErrUnknownPayload).
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
			e.HandleGetPayloadV5(id) // Will return ErrUnknownPayload, which is expected.
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent error: %v", err)
	}
}

func TestGlamsterdam_JSONRPC_NewPayloadV5(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	payload := makeGlamsterdamPayload()
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	rootJSON, _ := json.Marshal(types.HexToHash("0xbeef"))
	requestsJSON, _ := json.Marshal([][]byte{})

	params := []json.RawMessage{payloadJSON, hashesJSON, rootJSON, requestsJSON}
	result, rpcErr := e.HandleJSONRPC("engine_newPayloadV5", params)
	if rpcErr != nil {
		t.Fatalf("RPC error: %s", rpcErr.Message)
	}
	status, ok := result.(*PayloadStatusV1)
	if !ok {
		t.Fatalf("expected *PayloadStatusV1, got %T", result)
	}
	if status.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", status.Status)
	}
}

func TestGlamsterdam_JSONRPC_ForkchoiceUpdatedV4(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	state := ForkchoiceStateV1{HeadBlockHash: types.HexToHash("0xaa")}
	stateJSON, _ := json.Marshal(state)

	params := []json.RawMessage{stateJSON, json.RawMessage(`null`)}
	result, rpcErr := e.HandleJSONRPC("engine_forkchoiceUpdatedV4", params)
	if rpcErr != nil {
		t.Fatalf("RPC error: %s", rpcErr.Message)
	}
	fcu, ok := result.(*ForkchoiceUpdatedResult)
	if !ok {
		t.Fatalf("expected *ForkchoiceUpdatedResult, got %T", result)
	}
	if fcu.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", fcu.PayloadStatus.Status)
	}
}

func TestGlamsterdam_JSONRPC_GetPayloadV5(t *testing.T) {
	expected := &GetPayloadV5Response{
		ExecutionPayload: &ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{BlockHash: types.HexToHash("0xcc")},
			},
		},
	}
	backend := &mockGlamsterdamBackend{getPayloadResp: expected}
	e := NewEngineGlamsterdam(backend)

	id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	idJSON, _ := json.Marshal(id)

	_, rpcErr := e.HandleJSONRPC("engine_getPayloadV5", []json.RawMessage{idJSON})
	if rpcErr != nil {
		t.Fatalf("RPC error: %s", rpcErr.Message)
	}
}

func TestGlamsterdam_JSONRPC_GetBlobsV2(t *testing.T) {
	backend := &mockGlamsterdamBackend{
		getBlobsResp: []*BlobAndProofV2{{Blob: []byte{0x01}, Proofs: [][]byte{}}},
	}
	e := NewEngineGlamsterdam(backend)

	hashesJSON, _ := json.Marshal([]types.Hash{types.HexToHash("0xaa")})
	_, rpcErr := e.HandleJSONRPC("engine_getBlobsV2", []json.RawMessage{hashesJSON})
	if rpcErr != nil {
		t.Fatalf("RPC error: %s", rpcErr.Message)
	}
}

func TestGlamsterdam_JSONRPC_GetClientVersionV2(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	result, rpcErr := e.HandleJSONRPC("engine_getClientVersionV2", nil)
	if rpcErr != nil {
		t.Fatalf("RPC error: %s", rpcErr.Message)
	}
	versions, ok := result.([]ClientVersionV2)
	if !ok {
		t.Fatalf("expected []ClientVersionV2, got %T", result)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
}

func TestGlamsterdam_JSONRPC_MethodNotFound(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	_, rpcErr := e.HandleJSONRPC("engine_nonexistent", nil)
	if rpcErr == nil {
		t.Fatal("expected RPC error for unknown method")
	}
	if rpcErr.Code != MethodNotFoundCode {
		t.Fatalf("expected code %d, got %d", MethodNotFoundCode, rpcErr.Code)
	}
}

func TestGlamsterdam_JSONRPC_InvalidParamCount(t *testing.T) {
	backend := &mockGlamsterdamBackend{}
	e := NewEngineGlamsterdam(backend)

	// engine_newPayloadV5 expects 4 params.
	_, rpcErr := e.HandleJSONRPC("engine_newPayloadV5", []json.RawMessage{json.RawMessage(`{}`)})
	if rpcErr == nil {
		t.Fatal("expected RPC error for wrong param count")
	}
	if rpcErr.Code != InvalidParamsCode {
		t.Fatalf("expected code %d, got %d", InvalidParamsCode, rpcErr.Code)
	}
}

func TestValidateExecutionRequests(t *testing.T) {
	tests := []struct {
		name    string
		reqs    [][]byte
		wantErr bool
	}{
		{name: "empty", reqs: [][]byte{}, wantErr: false},
		{name: "single valid", reqs: [][]byte{{0x01, 0xaa}}, wantErr: false},
		{name: "multiple ascending", reqs: [][]byte{{0x01, 0xaa}, {0x02, 0xbb}, {0x05, 0xcc}}, wantErr: false},
		{name: "too short", reqs: [][]byte{{0x01}}, wantErr: true},
		{name: "empty entry", reqs: [][]byte{{}}, wantErr: true},
		{name: "not ascending", reqs: [][]byte{{0x02, 0xaa}, {0x01, 0xbb}}, wantErr: true},
		{name: "duplicate type", reqs: [][]byte{{0x01, 0xaa}, {0x01, 0xbb}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExecutionRequests(tt.reqs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateExecutionRequests() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGlamsterdam_ErrorMapping(t *testing.T) {
	tests := []struct {
		err      error
		wantCode int
	}{
		{ErrUnknownPayload, UnknownPayloadCode},
		{ErrInvalidForkchoiceState, InvalidForkchoiceStateCode},
		{ErrInvalidPayloadAttributes, InvalidPayloadAttributeCode},
		{ErrInvalidParams, InvalidParamsCode},
		{ErrTooLargeRequest, TooLargeRequestCode},
		{ErrUnsupportedFork, UnsupportedForkCode},
	}

	for _, tt := range tests {
		rpcErr := glamsterdamErrorToRPC(tt.err)
		if rpcErr.Code != tt.wantCode {
			t.Errorf("error %v: expected code %d, got %d", tt.err, tt.wantCode, rpcErr.Code)
		}
	}
}
