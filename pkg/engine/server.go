package engine

import (
	"context"
	"fmt"
	"io"
	"log"
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

	// ForkchoiceUpdated processes a forkchoice state update from the consensus layer.
	// If payloadAttributes is non-nil, it begins building a new payload.
	// It returns the payload status and an optional payload ID if building was started.
	ForkchoiceUpdated(state ForkchoiceStateV1, payloadAttributes *PayloadAttributesV3) (ForkchoiceUpdatedResult, error)

	// GetPayloadByID retrieves a previously requested payload by its ID.
	GetPayloadByID(id PayloadID) (*GetPayloadResponse, error)
}

// EngineAPI implements the Engine API JSON-RPC methods.
type EngineAPI struct {
	backend Backend
	server  *http.Server
	mu      sync.Mutex
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

// Start starts the HTTP JSON-RPC server on the given address.
func (api *EngineAPI) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", api.httpHandler)

	api.mu.Lock()
	api.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	api.mu.Unlock()

	log.Printf("Engine API server starting on %s", addr)
	if err := api.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("engine API server error: %w", err)
	}
	return nil
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
