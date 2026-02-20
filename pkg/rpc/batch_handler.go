package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Batch processing errors.
var (
	ErrBatchEmpty    = errors.New("rpc: empty batch")
	ErrBatchTooLarge = fmt.Errorf("rpc: batch exceeds maximum size of %d", MaxBatchSize)
	ErrNotBatch      = errors.New("rpc: request is not a JSON array")
)

// Batch processing constants.
const (
	// MaxBatchSize is the maximum number of requests in a single batch.
	MaxBatchSize = 100

	// DefaultParallelism is the default number of goroutines for parallel execution.
	DefaultParallelism = 16
)

// BatchRequest represents a single request within a JSON-RPC batch.
type BatchRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
	ID      json.RawMessage   `json:"id"`
}

// BatchResponse represents a single response within a JSON-RPC batch response.
type BatchResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// BatchHandler processes JSON-RPC batch requests. It parses a batch array,
// executes each request (optionally in parallel), and assembles the results
// in the original request order.
type BatchHandler struct {
	api         *EthAPI
	parallelism int
}

// NewBatchHandler creates a new batch handler that dispatches to the given API.
func NewBatchHandler(api *EthAPI) *BatchHandler {
	return &BatchHandler{
		api:         api,
		parallelism: DefaultParallelism,
	}
}

// SetParallelism sets the maximum number of goroutines used for parallel
// batch execution. Must be at least 1.
func (bh *BatchHandler) SetParallelism(n int) {
	if n < 1 {
		n = 1
	}
	bh.parallelism = n
}

// HandleBatch parses a raw JSON body as a batch request and returns the
// batch response. If the body is not a JSON array, it returns a single
// error response indicating an invalid request.
func (bh *BatchHandler) HandleBatch(body []byte) ([]BatchResponse, error) {
	requests, err := parseBatchRequests(body)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return nil, ErrBatchEmpty
	}
	if len(requests) > MaxBatchSize {
		return nil, ErrBatchTooLarge
	}
	return bh.ExecuteParallel(requests), nil
}

// ExecuteParallel executes a slice of batch requests in parallel with bounded
// concurrency. Results are returned in the same order as the input requests.
func (bh *BatchHandler) ExecuteParallel(requests []BatchRequest) []BatchResponse {
	n := len(requests)
	responses := make([]BatchResponse, n)

	sem := make(chan struct{}, bh.parallelism)
	var wg sync.WaitGroup

	for i, req := range requests {
		wg.Add(1)
		sem <- struct{}{} // acquire
		go func(idx int, r BatchRequest) {
			defer wg.Done()
			defer func() { <-sem }() // release
			responses[idx] = bh.executeOne(r)
		}(i, req)
	}
	wg.Wait()
	return responses
}

// executeOne dispatches a single BatchRequest to the API and wraps the
// result into a BatchResponse.
func (bh *BatchHandler) executeOne(req BatchRequest) BatchResponse {
	// Validate jsonrpc version.
	if req.JSONRPC != "2.0" {
		return BatchResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeInvalidRequest, Message: "invalid jsonrpc version"},
			ID:      req.ID,
		}
	}
	// Validate method is not empty.
	if req.Method == "" {
		return BatchResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: ErrCodeInvalidRequest, Message: "method is required"},
			ID:      req.ID,
		}
	}

	// Convert to the standard Request type and dispatch.
	apiReq := &Request{
		JSONRPC: req.JSONRPC,
		Method:  req.Method,
		Params:  req.Params,
		ID:      req.ID,
	}
	resp := bh.api.HandleRequest(apiReq)

	return BatchResponse{
		JSONRPC: resp.JSONRPC,
		Result:  resp.Result,
		Error:   resp.Error,
		ID:      resp.ID,
	}
}

// MarshalBatchResponse serializes a batch of responses to JSON.
func MarshalBatchResponse(responses []BatchResponse) ([]byte, error) {
	return json.Marshal(responses)
}

// parseBatchRequests parses a JSON byte slice as an array of BatchRequest.
func parseBatchRequests(body []byte) ([]BatchRequest, error) {
	// Quick check: must start with '['.
	trimmed := trimWhitespace(body)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, ErrNotBatch
	}

	var requests []BatchRequest
	if err := json.Unmarshal(body, &requests); err != nil {
		return nil, fmt.Errorf("rpc: invalid JSON in batch: %w", err)
	}
	return requests, nil
}

// trimWhitespace returns body with leading whitespace removed.
func trimWhitespace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	return b
}

// IsBatchRequest checks whether a JSON body is a batch request (starts with '[').
func IsBatchRequest(body []byte) bool {
	trimmed := trimWhitespace(body)
	return len(trimmed) > 0 && trimmed[0] == '['
}
