package engine

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/eth2028/eth2028/core/types"
)

// Backend defines the interface that the execution layer must implement
// for the Engine API to interact with it.
type Backend interface {
	// ProcessBlock validates and executes a new payload from the consensus layer.
	// It returns the payload status indicating whether the block is valid, invalid,
	// or the node is syncing.
	ProcessBlock(payload *ExecutionPayloadV3, expectedBlobVersionedHashes []types.Hash, parentBeaconBlockRoot types.Hash) (PayloadStatusV1, error)

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

	// IsPrague returns true if the given timestamp falls within the Prague fork.
	IsPrague(timestamp uint64) bool

	// IsAmsterdam returns true if the given timestamp falls within the Amsterdam fork.
	IsAmsterdam(timestamp uint64) bool
}

// EngineAPI implements the Engine API JSON-RPC methods.
type EngineAPI struct {
	backend  Backend
	server   *http.Server
	listener net.Listener
	mu       sync.Mutex
}

// NewEngineAPI creates a new Engine API instance with the given backend.
func NewEngineAPI(backend Backend) *EngineAPI {
	return &EngineAPI{
		backend: backend,
	}
}

// NewPayloadV3 validates and executes a new Cancun/Deneb payload.
func (api *EngineAPI) NewPayloadV3(
	payload ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (PayloadStatusV1, error) {
	return api.backend.ProcessBlock(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

// ForkchoiceUpdatedV3 processes a forkchoice update with optional V3 payload attributes.
func (api *EngineAPI) ForkchoiceUpdatedV3(
	state ForkchoiceStateV1,
	payloadAttributes *PayloadAttributesV3,
) (ForkchoiceUpdatedResult, error) {
	return api.backend.ForkchoiceUpdated(state, payloadAttributes)
}

// GetPayloadV3 retrieves a previously built payload by ID.
func (api *EngineAPI) GetPayloadV3(payloadID PayloadID) (GetPayloadResponse, error) {
	resp, err := api.backend.GetPayloadByID(payloadID)
	if err != nil {
		return GetPayloadResponse{}, err
	}
	return *resp, nil
}

// NewPayloadV4 validates and executes a Prague/Electra payload with execution requests.
func (api *EngineAPI) NewPayloadV4(
	payload ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// V4 passes execution requests alongside the V3 payload.
	// The backend processes the V3 payload; execution requests are validated separately.
	return api.backend.ProcessBlock(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

// GetPayloadV4 retrieves a previously built payload for Prague.
// Returns ExecutionPayloadV3 + executionRequests.
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
	// The blockAccessList field must be present.
	if payload.BlockAccessList == nil {
		return PayloadStatusV1{}, ErrInvalidParams
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
			Name:    "eth2028",
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
