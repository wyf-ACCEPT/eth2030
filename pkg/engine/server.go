package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Backend defines the interface that the execution layer must implement
// for the Engine API to interact with it.
type Backend interface {
	// ProcessBlock validates and executes a new payload from the consensus layer.
	// It returns the payload status indicating whether the block is valid, invalid,
	// or the node is syncing.
	ProcessBlock(payload *ExecutionPayloadV3, expectedBlobVersionedHashes []types.Hash, parentBeaconBlockRoot types.Hash) (PayloadStatusV1, error)

	// ProcessBlockV4 validates and executes a Prague payload with execution requests.
	ProcessBlockV4(payload *ExecutionPayloadV3, expectedBlobVersionedHashes []types.Hash, parentBeaconBlockRoot types.Hash, executionRequests [][]byte) (PayloadStatusV1, error)

	// ProcessBlockV5 validates and executes a new Amsterdam payload with BAL.
	ProcessBlockV5(payload *ExecutionPayloadV5, expectedBlobVersionedHashes []types.Hash, parentBeaconBlockRoot types.Hash, executionRequests [][]byte) (PayloadStatusV1, error)

	// ForkchoiceUpdated processes a forkchoice state update from the consensus layer.
	// If payloadAttributes is non-nil, it begins building a new payload.
	// It returns the payload status and an optional payload ID if building was started.
	ForkchoiceUpdated(state ForkchoiceStateV1, payloadAttributes *PayloadAttributesV3) (ForkchoiceUpdatedResult, error)

	// ForkchoiceUpdatedV4 processes a forkchoice update with V4 payload attributes (Amsterdam).
	ForkchoiceUpdatedV4(state ForkchoiceStateV1, payloadAttributes *PayloadAttributesV4) (ForkchoiceUpdatedResult, error)

	// GetPayloadByID retrieves a previously requested payload by its ID.
	GetPayloadByID(id PayloadID) (*GetPayloadResponse, error)

	// GetPayloadV4ByID retrieves a previously built payload for getPayloadV4 (Prague).
	GetPayloadV4ByID(id PayloadID) (*GetPayloadV4Response, error)

	// GetPayloadV6ByID retrieves a previously built payload for getPayloadV6 (Amsterdam).
	GetPayloadV6ByID(id PayloadID) (*GetPayloadV6Response, error)

	// GetHeadTimestamp returns the timestamp of the current head block.
	// Used to validate timestamp progression in payload attributes.
	GetHeadTimestamp() uint64

	// IsCancun returns true if the given timestamp falls within the Cancun fork.
	IsCancun(timestamp uint64) bool

	// IsPrague returns true if the given timestamp falls within the Prague fork.
	IsPrague(timestamp uint64) bool

	// IsAmsterdam returns true if the given timestamp falls within the Amsterdam fork.
	IsAmsterdam(timestamp uint64) bool
}

// EngineAPI implements the Engine API JSON-RPC methods.
type EngineAPI struct {
	backend         Backend
	builderRegistry *BuilderRegistry
	server          *http.Server
	listener        net.Listener
	mu              sync.Mutex
}

// NewEngineAPI creates a new Engine API instance with the given backend.
func NewEngineAPI(backend Backend) *EngineAPI {
	return &EngineAPI{
		backend:         backend,
		builderRegistry: NewBuilderRegistry(),
	}
}

// NewPayloadV3 validates and executes a new Cancun/Deneb payload.
// Per EIP-4844: blob versioned hashes must match commitments in transactions.
// Per EIP-4788: parentBeaconBlockRoot must be provided (non-zero).
func (api *EngineAPI) NewPayloadV3(
	payload ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (PayloadStatusV1, error) {
	// Validate that the payload timestamp falls within the Cancun fork.
	if !api.backend.IsCancun(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: parentBeaconBlockRoot must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-4844: validate blob versioned hashes match transaction contents.
	if err := validateBlobHashes(&payload, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	return api.backend.ProcessBlock(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

// ForkchoiceUpdatedV3 processes a forkchoice update with optional V3 payload attributes.
// Validates withdrawals and parentBeaconBlockRoot in payload attributes.
// Validates timestamp progression when attributes are provided.
func (api *EngineAPI) ForkchoiceUpdatedV3(
	state ForkchoiceStateV1,
	payloadAttributes *PayloadAttributesV3,
) (ForkchoiceUpdatedResult, error) {
	if payloadAttributes != nil {
		// Validate timestamp is non-zero.
		if payloadAttributes.Timestamp == 0 {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}
		// Validate parentBeaconBlockRoot is provided (V3 requires it).
		if payloadAttributes.ParentBeaconBlockRoot == (types.Hash{}) {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}
		// Validate timestamp progression: must be greater than head block timestamp.
		headTimestamp := api.backend.GetHeadTimestamp()
		if headTimestamp > 0 && payloadAttributes.Timestamp <= headTimestamp {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}
	}
	return api.backend.ForkchoiceUpdated(state, payloadAttributes)
}

// GetPayloadV3 retrieves a previously built payload by ID.
// Returns ExecutionPayloadV3 + blockValue + blobsBundle (no executionRequests).
func (api *EngineAPI) GetPayloadV3(payloadID PayloadID) (*GetPayloadV3Response, error) {
	resp, err := api.backend.GetPayloadByID(payloadID)
	if err != nil {
		return nil, err
	}
	// Build the V3 response (no executionRequests in V3).
	v3Resp := &GetPayloadV3Response{
		ExecutionPayload: &resp.ExecutionPayload.ExecutionPayloadV3,
		BlockValue:       resp.BlockValue,
		BlobsBundle:      resp.BlobsBundle,
		Override:         resp.Override,
	}
	return v3Resp, nil
}

// NewPayloadV4 validates and executes a Prague/Electra payload with execution requests.
// Per EIP-7685: executionRequests must be provided.
func (api *EngineAPI) NewPayloadV4(
	payload ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// Validate that the payload timestamp falls within the Prague fork.
	if !api.backend.IsPrague(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: parentBeaconBlockRoot must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-7685: executionRequests must be provided (can be empty list, not nil).
	if executionRequests == nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-4844: validate blob versioned hashes match transaction contents.
	if err := validateBlobHashes(&payload, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	return api.backend.ProcessBlockV4(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests)
}

// GetPayloadV4 retrieves a previously built payload for Prague.
// Returns ExecutionPayloadV3 + blockValue + blobsBundle + executionRequests.
func (api *EngineAPI) GetPayloadV4(payloadID PayloadID) (*GetPayloadV4Response, error) {
	resp, err := api.backend.GetPayloadV4ByID(payloadID)
	if err != nil {
		return nil, err
	}
	// Validate that the payload timestamp falls within the Prague fork.
	if resp.ExecutionPayload != nil && !api.backend.IsPrague(resp.ExecutionPayload.Timestamp) {
		return nil, ErrUnsupportedFork
	}
	return resp, nil
}

// NewPayloadV5 validates and executes an Amsterdam payload with BAL.
func (api *EngineAPI) NewPayloadV5(
	payload ExecutionPayloadV5,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// Check that the timestamp falls within the Amsterdam fork.
	if !api.backend.IsAmsterdam(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: parentBeaconBlockRoot must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-7685: executionRequests must be provided.
	if executionRequests == nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// The blockAccessList field must be present.
	if payload.BlockAccessList == nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-4844: validate blob versioned hashes match transaction contents.
	if err := validateBlobHashes(&payload.ExecutionPayloadV3, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	return api.backend.ProcessBlockV5(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests)
}

// GetPayloadV6 retrieves a previously built payload for Amsterdam.
// Returns ExecutionPayloadV4 (which includes BAL) with execution requests.
func (api *EngineAPI) GetPayloadV6(payloadID PayloadID) (*GetPayloadV6Response, error) {
	resp, err := api.backend.GetPayloadV6ByID(payloadID)
	if err != nil {
		return nil, err
	}
	// Validate that the payload timestamp falls within the Amsterdam fork.
	if resp.ExecutionPayload != nil && !api.backend.IsAmsterdam(resp.ExecutionPayload.Timestamp) {
		return nil, ErrUnsupportedFork
	}
	return resp, nil
}

// ForkchoiceUpdatedV4 processes a forkchoice update with V4 payload attributes (Amsterdam).
func (api *EngineAPI) ForkchoiceUpdatedV4(
	state ForkchoiceStateV1,
	payloadAttributes *PayloadAttributesV4,
) (ForkchoiceUpdatedResult, error) {
	// If payload attributes are provided, validate the timestamp.
	if payloadAttributes != nil {
		if !api.backend.IsAmsterdam(payloadAttributes.Timestamp) {
			// Per spec: forkchoice state update must NOT be rolled back,
			// but we still return the error. The backend handles the state update
			// before attribute validation in the full implementation.
			// For now, delegate entirely to the backend which handles ordering.
			return ForkchoiceUpdatedResult{}, ErrUnsupportedFork
		}
	}
	return api.backend.ForkchoiceUpdatedV4(state, payloadAttributes)
}

// ExchangeCapabilities returns the list of Engine API methods this node supports.
func (api *EngineAPI) ExchangeCapabilities(requested []string) []string {
	supported := []string{
		"engine_newPayloadV3",
		"engine_newPayloadV4",
		"engine_newPayloadV5",
		"engine_forkchoiceUpdatedV3",
		"engine_forkchoiceUpdatedV4",
		"engine_getPayloadV3",
		"engine_getPayloadV4",
		"engine_getPayloadV6",
		"engine_exchangeCapabilities",
		"engine_getClientVersionV1",
		"engine_submitBuilderBidV1",
		"engine_getBuilderBidsV1",
		"engine_newInclusionListV1",
		"engine_getInclusionListV1",
		"engine_getPayloadHeaderV1",
		"engine_submitBlindedBlockV1",
	}
	return supported
}

// ClientVersionV1 represents the client version information.
type ClientVersionV1 struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// GetClientVersionV1 returns the client version information.
func (api *EngineAPI) GetClientVersionV1(peerVersion ClientVersionV1) []ClientVersionV1 {
	return []ClientVersionV1{
		{
			Code:    "ET",
			Name:    "eth2030",
			Version: "v0.1.0",
			Commit:  "000000",
		},
	}
}

// Start starts the HTTP JSON-RPC server on the given address.
func (api *EngineAPI) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", api.httpHandler)

	api.mu.Lock()
	api.listener = ln
	api.server = &http.Server{Handler: mux}
	api.mu.Unlock()

	log.Printf("Engine API server starting on %s", ln.Addr())
	if err := api.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("engine API server error: %w", err)
	}
	return nil
}

// Addr returns the listener address, useful when started on port 0.
func (api *EngineAPI) Addr() net.Addr {
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.listener == nil {
		return nil
	}
	return api.listener.Addr()
}

// Stop gracefully shuts down the HTTP server.
func (api *EngineAPI) Stop() error {
	api.mu.Lock()
	srv := api.server
	api.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(context.Background())
}

// httpHandler handles incoming HTTP requests and dispatches them as JSON-RPC.
func (api *EngineAPI) httpHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	resp := api.HandleRequest(body)

	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

// validateBlobHashes checks that the blob versioned hashes from the CL
// match the versioned hashes found in blob transactions within the payload.
// Per EIP-4844, each blob transaction carries versioned hashes that must
// appear in the same order in the expectedBlobVersionedHashes list.
func validateBlobHashes(payload *ExecutionPayloadV3, expected []types.Hash) error {
	// Collect all blob versioned hashes from transactions in the payload.
	var actual []types.Hash
	for _, txBytes := range payload.Transactions {
		tx, err := types.DecodeTxRLP(txBytes)
		if err != nil {
			// Invalid transaction encoding will be caught later during block processing.
			continue
		}
		hashes := tx.BlobHashes()
		if len(hashes) > 0 {
			actual = append(actual, hashes...)
		}
	}

	// The expected list must match the actual list exactly (same length, same order).
	if len(expected) != len(actual) {
		return fmt.Errorf("%w: expected %d blob hashes, got %d from transactions",
			ErrInvalidBlobHashes, len(expected), len(actual))
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return fmt.Errorf("%w: hash mismatch at index %d", ErrInvalidBlobHashes, i)
		}
	}
	return nil
}
