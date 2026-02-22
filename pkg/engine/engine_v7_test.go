package engine

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// mockV7Backend implements EngineV7Backend for testing.
type mockV7Backend struct {
	mu             sync.Mutex
	newPayloadResp *PayloadStatusV1
	newPayloadErr  error
	fcuResult      *ForkchoiceUpdatedResult
	fcuErr         error
	getPayloadResp *ExecutionPayloadV7
	getPayloadErr  error

	// Track calls for assertions.
	lastPayload *ExecutionPayloadV7
	lastFCState *ForkchoiceStateV1
	lastAttrs   *PayloadAttributesV7
	lastID      PayloadID
}

func (m *mockV7Backend) NewPayloadV7(payload *ExecutionPayloadV7) (*PayloadStatusV1, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastPayload = payload
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

func (m *mockV7Backend) ForkchoiceUpdatedV7(state *ForkchoiceStateV1, attrs *PayloadAttributesV7) (*ForkchoiceUpdatedResult, error) {
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

func (m *mockV7Backend) GetPayloadV7(id PayloadID) (*ExecutionPayloadV7, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastID = id
	if m.getPayloadErr != nil {
		return nil, m.getPayloadErr
	}
	if m.getPayloadResp != nil {
		return m.getPayloadResp, nil
	}
	return nil, ErrUnknownPayload
}

func makeTestPayloadV7() *ExecutionPayloadV7 {
	return &ExecutionPayloadV7{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:    types.HexToHash("0x01"),
					FeeRecipient:  types.HexToAddress("0xdead"),
					StateRoot:     types.HexToHash("0x02"),
					ReceiptsRoot:  types.HexToHash("0x03"),
					BlockNumber:   100,
					GasLimit:      30_000_000,
					GasUsed:       21_000,
					Timestamp:     1700000000,
					BaseFeePerGas: big.NewInt(1_000_000_000),
					BlockHash:     types.HexToHash("0xaa"),
					Transactions:  [][]byte{},
				},
				Withdrawals: []*Withdrawal{},
			},
			BlobGasUsed:   131072,
			ExcessBlobGas: 0,
		},
		BlobCommitments:  []types.Hash{types.HexToHash("0xc1")},
		ProofSubmissions: [][]byte{{0x01, 0x02}, {0x03, 0x04}, {0x05, 0x06}},
		ShieldedResults:  []types.Hash{types.HexToHash("0xdd")},
	}
}

func TestNewEngineV7(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)
	if e == nil {
		t.Fatal("NewEngineV7 returned nil")
	}
	if e.backend != backend {
		t.Fatal("backend not set correctly")
	}
}

func TestHandleNewPayloadV7_Valid(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	status, err := e.HandleNewPayloadV7(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", status.Status)
	}
	if status.LatestValidHash == nil {
		t.Fatal("expected LatestValidHash to be set")
	}
	if *status.LatestValidHash != payload.BlockHash {
		t.Fatalf("hash mismatch: %s != %s", status.LatestValidHash, payload.BlockHash)
	}
}

func TestHandleNewPayloadV7_NilPayload(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	_, err := e.HandleNewPayloadV7(nil)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestHandleNewPayloadV7_NilProofSubmissions(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	payload.ProofSubmissions = nil
	_, err := e.HandleNewPayloadV7(payload)
	if err != ErrInvalidParams {
		t.Fatalf("expected ErrInvalidParams, got %v", err)
	}
}

func TestHandleNewPayloadV7_EmptyProofEntry(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	payload.ProofSubmissions = [][]byte{{0x01}, {}} // second entry is empty
	status, err := e.HandleNewPayloadV7(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Fatalf("expected INVALID, got %s", status.Status)
	}
	if status.ValidationError == nil {
		t.Fatal("expected validation error message")
	}
}

func TestHandleNewPayloadV7_BlobGasWithoutCommitments(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	payload.BlobGasUsed = 131072
	payload.BlobCommitments = nil // no commitments
	status, err := e.HandleNewPayloadV7(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Fatalf("expected INVALID, got %s", status.Status)
	}
}

func TestHandleNewPayloadV7_NoBlobGasNoCommitmentsOK(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	payload.BlobGasUsed = 0
	payload.BlobCommitments = nil
	payload.ProofSubmissions = [][]byte{{0x01}}
	status, err := e.HandleNewPayloadV7(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", status.Status)
	}
}

func TestHandleNewPayloadV7_BackendError(t *testing.T) {
	backend := &mockV7Backend{
		newPayloadErr: ErrUnsupportedFork,
	}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	_, err := e.HandleNewPayloadV7(payload)
	if err != ErrUnsupportedFork {
		t.Fatalf("expected ErrUnsupportedFork, got %v", err)
	}
}

func TestHandleForkchoiceUpdatedV7_Valid(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0xaa"),
		SafeBlockHash:      types.HexToHash("0xbb"),
		FinalizedBlockHash: types.HexToHash("0xcc"),
	}
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             1700000000,
					PrevRandao:            types.HexToHash("0xdd"),
					SuggestedFeeRecipient: types.HexToAddress("0xdead"),
				},
			},
			ParentBeaconBlockRoot: types.HexToHash("0xee"),
		},
	}

	result, err := e.HandleForkchoiceUpdatedV7(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}

func TestHandleForkchoiceUpdatedV7_NilState(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	_, err := e.HandleForkchoiceUpdatedV7(nil, nil)
	if err != ErrInvalidForkchoiceState {
		t.Fatalf("expected ErrInvalidForkchoiceState, got %v", err)
	}
}

func TestHandleForkchoiceUpdatedV7_ZeroHead(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{}
	_, err := e.HandleForkchoiceUpdatedV7(state, nil)
	if err != ErrInvalidForkchoiceState {
		t.Fatalf("expected ErrInvalidForkchoiceState, got %v", err)
	}
}

func TestHandleForkchoiceUpdatedV7_NilAttrs(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	result, err := e.HandleForkchoiceUpdatedV7(state, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}

func TestHandleForkchoiceUpdatedV7_ZeroTimestamp(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp: 0,
				},
			},
		},
	}
	_, err := e.HandleForkchoiceUpdatedV7(state, attrs)
	if err != ErrInvalidPayloadAttributes {
		t.Fatalf("expected ErrInvalidPayloadAttributes, got %v", err)
	}
}

func TestHandleForkchoiceUpdatedV7_InvalidProofRequirements(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}

	tests := []struct {
		name string
		pr   *ProofRequirements
	}{
		{
			name: "zero total",
			pr:   &ProofRequirements{MinProofs: 3, TotalProofs: 0},
		},
		{
			name: "zero min",
			pr:   &ProofRequirements{MinProofs: 0, TotalProofs: 5},
		},
		{
			name: "min > total",
			pr:   &ProofRequirements{MinProofs: 6, TotalProofs: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := &PayloadAttributesV7{
				PayloadAttributesV3: PayloadAttributesV3{
					PayloadAttributesV2: PayloadAttributesV2{
						PayloadAttributesV1: PayloadAttributesV1{
							Timestamp: 1700000000,
						},
					},
				},
				ProofRequirements: tt.pr,
			}
			_, err := e.HandleForkchoiceUpdatedV7(state, attrs)
			if err != ErrInvalidPayloadAttributes {
				t.Fatalf("expected ErrInvalidPayloadAttributes, got %v", err)
			}
		})
	}
}

func TestHandleForkchoiceUpdatedV7_ValidProofRequirements(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp: 1700000000,
				},
			},
		},
		ProofRequirements: &ProofRequirements{
			MinProofs:    3,
			TotalProofs:  5,
			AllowedTypes: []string{"snark", "stark", "groth16"},
		},
	}

	result, err := e.HandleForkchoiceUpdatedV7(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}

func TestHandleForkchoiceUpdatedV7_InvalidDAConfig(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}

	tests := []struct {
		name string
		da   *DALayerConfig
	}{
		{
			name: "zero sample count",
			da:   &DALayerConfig{SampleCount: 0, ColumnCount: 128},
		},
		{
			name: "zero column count",
			da:   &DALayerConfig{SampleCount: 64, ColumnCount: 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := &PayloadAttributesV7{
				PayloadAttributesV3: PayloadAttributesV3{
					PayloadAttributesV2: PayloadAttributesV2{
						PayloadAttributesV1: PayloadAttributesV1{
							Timestamp: 1700000000,
						},
					},
				},
				DALayerConfig: tt.da,
			}
			_, err := e.HandleForkchoiceUpdatedV7(state, attrs)
			if err != ErrInvalidPayloadAttributes {
				t.Fatalf("expected ErrInvalidPayloadAttributes, got %v", err)
			}
		})
	}
}

func TestHandleForkchoiceUpdatedV7_WithShieldedTxs(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp: 1700000000,
				},
			},
		},
		ShieldedTxs: [][]byte{
			{0x01, 0x02, 0x03},
			{0x04, 0x05, 0x06},
		},
	}

	result, err := e.HandleForkchoiceUpdatedV7(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}

	// Verify the backend received the shielded txs.
	backend.mu.Lock()
	if backend.lastAttrs == nil {
		t.Fatal("backend did not receive attrs")
	}
	if len(backend.lastAttrs.ShieldedTxs) != 2 {
		t.Fatalf("expected 2 shielded txs, got %d", len(backend.lastAttrs.ShieldedTxs))
	}
	backend.mu.Unlock()
}

func TestHandleGetPayloadV7_Valid(t *testing.T) {
	expected := makeTestPayloadV7()
	backend := &mockV7Backend{
		getPayloadResp: expected,
	}
	e := NewEngineV7(backend)

	id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	result, err := e.HandleGetPayloadV7(id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BlockHash != expected.BlockHash {
		t.Fatalf("payload mismatch: got %s, want %s", result.BlockHash, expected.BlockHash)
	}
}

func TestHandleGetPayloadV7_ZeroID(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	_, err := e.HandleGetPayloadV7(PayloadID{})
	if err != ErrUnknownPayload {
		t.Fatalf("expected ErrUnknownPayload, got %v", err)
	}
}

func TestHandleGetPayloadV7_NotFound(t *testing.T) {
	backend := &mockV7Backend{} // no getPayloadResp set, returns ErrUnknownPayload
	e := NewEngineV7(backend)

	id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	_, err := e.HandleGetPayloadV7(id)
	if err != ErrUnknownPayload {
		t.Fatalf("expected ErrUnknownPayload, got %v", err)
	}
}

func TestEngineV7_Concurrency(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	var wg sync.WaitGroup
	errCh := make(chan error, 30)

	// Run HandleNewPayloadV7 concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := makeTestPayloadV7()
			_, err := e.HandleNewPayloadV7(payload)
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Run HandleForkchoiceUpdatedV7 concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := &ForkchoiceStateV1{
				HeadBlockHash: types.HexToHash("0xaa"),
			}
			_, err := e.HandleForkchoiceUpdatedV7(state, nil)
			if err != nil {
				errCh <- err
			}
		}()
	}

	// Run HandleGetPayloadV7 concurrently.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
			// This will return ErrUnknownPayload, which is expected.
			e.HandleGetPayloadV7(id)
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent error: %v", err)
	}
}

func TestProofRequirements_Validate(t *testing.T) {
	tests := []struct {
		name    string
		pr      ProofRequirements
		wantErr bool
	}{
		{
			name:    "valid 3-of-5",
			pr:      ProofRequirements{MinProofs: 3, TotalProofs: 5},
			wantErr: false,
		},
		{
			name:    "valid 1-of-1",
			pr:      ProofRequirements{MinProofs: 1, TotalProofs: 1},
			wantErr: false,
		},
		{
			name:    "valid equal",
			pr:      ProofRequirements{MinProofs: 5, TotalProofs: 5},
			wantErr: false,
		},
		{
			name:    "zero total",
			pr:      ProofRequirements{MinProofs: 3, TotalProofs: 0},
			wantErr: true,
		},
		{
			name:    "zero min",
			pr:      ProofRequirements{MinProofs: 0, TotalProofs: 5},
			wantErr: true,
		},
		{
			name:    "min greater than total",
			pr:      ProofRequirements{MinProofs: 6, TotalProofs: 5},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.pr.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateV7PayloadID(t *testing.T) {
	parentHash := types.HexToHash("0xabcdef")
	ts := uint64(1700000000)

	id1 := generateV7PayloadID(parentHash, ts)
	id2 := generateV7PayloadID(parentHash, ts)

	// Same inputs produce the same ID.
	if id1 != id2 {
		t.Fatalf("deterministic: expected %v == %v", id1, id2)
	}

	// Different timestamp produces a different ID.
	id3 := generateV7PayloadID(parentHash, ts+1)
	if id1 == id3 {
		t.Fatal("expected different IDs for different timestamps")
	}

	// Different parent hash produces a different ID.
	id4 := generateV7PayloadID(types.HexToHash("0x123456"), ts)
	if id1 == id4 {
		t.Fatal("expected different IDs for different parent hashes")
	}
}

func TestExecutionPayloadV7_FieldAccess(t *testing.T) {
	p := makeTestPayloadV7()

	// Verify V3 fields are accessible through embedding.
	if p.BlockNumber != 100 {
		t.Fatalf("expected block 100, got %d", p.BlockNumber)
	}
	if p.GasLimit != 30_000_000 {
		t.Fatalf("expected gas limit 30M, got %d", p.GasLimit)
	}
	if p.BlobGasUsed != 131072 {
		t.Fatalf("expected blob gas 131072, got %d", p.BlobGasUsed)
	}

	// Verify V7 fields.
	if len(p.BlobCommitments) != 1 {
		t.Fatalf("expected 1 blob commitment, got %d", len(p.BlobCommitments))
	}
	if len(p.ProofSubmissions) != 3 {
		t.Fatalf("expected 3 proof submissions, got %d", len(p.ProofSubmissions))
	}
	if len(p.ShieldedResults) != 1 {
		t.Fatalf("expected 1 shielded result, got %d", len(p.ShieldedResults))
	}
}

func TestPayloadAttributesV7_FieldAccess(t *testing.T) {
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             1700000000,
					PrevRandao:            types.HexToHash("0xdd"),
					SuggestedFeeRecipient: types.HexToAddress("0xdead"),
				},
				Withdrawals: []*Withdrawal{
					{Index: 1, ValidatorIndex: 100, Amount: 1000},
				},
			},
			ParentBeaconBlockRoot: types.HexToHash("0xee"),
		},
		DALayerConfig: &DALayerConfig{
			SampleCount:       64,
			ColumnCount:       128,
			RecoveryThreshold: 5000,
		},
		ProofRequirements: &ProofRequirements{
			MinProofs:    3,
			TotalProofs:  5,
			AllowedTypes: []string{"snark", "stark"},
		},
		ShieldedTxs: [][]byte{{0x01}},
	}

	// Verify V3 fields.
	if attrs.Timestamp != 1700000000 {
		t.Fatalf("expected timestamp 1700000000, got %d", attrs.Timestamp)
	}
	if len(attrs.Withdrawals) != 1 {
		t.Fatalf("expected 1 withdrawal, got %d", len(attrs.Withdrawals))
	}

	// Verify V7 fields.
	if attrs.DALayerConfig.SampleCount != 64 {
		t.Fatalf("expected 64 samples, got %d", attrs.DALayerConfig.SampleCount)
	}
	if attrs.ProofRequirements.MinProofs != 3 {
		t.Fatalf("expected minProofs 3, got %d", attrs.ProofRequirements.MinProofs)
	}
	if len(attrs.ShieldedTxs) != 1 {
		t.Fatalf("expected 1 shielded tx, got %d", len(attrs.ShieldedTxs))
	}
}

func TestHandleNewPayloadV7_EmptyProofSubmissionsOK(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	payload := makeTestPayloadV7()
	payload.BlobGasUsed = 0
	payload.BlobCommitments = nil
	payload.ProofSubmissions = [][]byte{} // empty but non-nil
	status, err := e.HandleNewPayloadV7(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", status.Status)
	}
}

func TestHandleForkchoiceUpdatedV7_BackendError(t *testing.T) {
	backend := &mockV7Backend{
		fcuErr: ErrInvalidForkchoiceState,
	}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	_, err := e.HandleForkchoiceUpdatedV7(state, nil)
	if err != ErrInvalidForkchoiceState {
		t.Fatalf("expected ErrInvalidForkchoiceState, got %v", err)
	}
}

func TestHandleForkchoiceUpdatedV7_WithPayloadID(t *testing.T) {
	pid := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockV7Backend{
		fcuResult: &ForkchoiceUpdatedResult{
			PayloadStatus: PayloadStatusV1{Status: StatusValid},
			PayloadID:     &pid,
		},
	}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp: 1700000000,
				},
			},
		},
	}

	result, err := e.HandleForkchoiceUpdatedV7(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadID == nil {
		t.Fatal("expected PayloadID to be set")
	}
	if *result.PayloadID != pid {
		t.Fatalf("payload ID mismatch: got %v, want %v", *result.PayloadID, pid)
	}
}

func TestHandleForkchoiceUpdatedV7_ValidDAConfig(t *testing.T) {
	backend := &mockV7Backend{}
	e := NewEngineV7(backend)

	state := &ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaa"),
	}
	attrs := &PayloadAttributesV7{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp: 1700000000,
				},
			},
		},
		DALayerConfig: &DALayerConfig{
			SampleCount:       64,
			ColumnCount:       128,
			RecoveryThreshold: 5000,
		},
	}

	result, err := e.HandleForkchoiceUpdatedV7(state, attrs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}
