package rpc

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
)

// Batch processing extended constants.
const (
	// MinBatchSize is the minimum number of requests for a valid batch.
	MinBatchSize = 1

	// MaxNotificationBatchSize is the max number of notifications batched together.
	MaxNotificationBatchSize = 50

	// DefaultBatchTimeout is the default timeout per batch item in milliseconds.
	DefaultBatchTimeout = 5000
)

// BatchStats tracks statistics about batch processing for diagnostics.
type BatchStats struct {
	TotalBatches    atomic.Uint64
	TotalRequests   atomic.Uint64
	TotalErrors     atomic.Uint64
	LargestBatch    atomic.Uint64
	ParallelBatches atomic.Uint64
}

// Snapshot returns a point-in-time snapshot of the batch statistics.
func (s *BatchStats) Snapshot() BatchStatsSnapshot {
	return BatchStatsSnapshot{
		TotalBatches:    s.TotalBatches.Load(),
		TotalRequests:   s.TotalRequests.Load(),
		TotalErrors:     s.TotalErrors.Load(),
		LargestBatch:    s.LargestBatch.Load(),
		ParallelBatches: s.ParallelBatches.Load(),
	}
}

// BatchStatsSnapshot is a non-atomic copy of BatchStats for serialization.
type BatchStatsSnapshot struct {
	TotalBatches    uint64 `json:"totalBatches"`
	TotalRequests   uint64 `json:"totalRequests"`
	TotalErrors     uint64 `json:"totalErrors"`
	LargestBatch    uint64 `json:"largestBatch"`
	ParallelBatches uint64 `json:"parallelBatches"`
}

// BatchItemResult contains the result of executing a single batch item,
// including timing information.
type BatchItemResult struct {
	Response BatchResponse
	Index    int
	Error    bool
}

// BatchValidator checks the structural validity of a batch request before
// processing. It returns detailed error information for each invalid item.
type BatchValidator struct {
	maxSize int
}

// NewBatchValidator creates a validator with the given maximum batch size.
func NewBatchValidator(maxSize int) *BatchValidator {
	if maxSize <= 0 {
		maxSize = MaxBatchSize
	}
	return &BatchValidator{maxSize: maxSize}
}

// Validate checks the structural validity of a parsed batch. Returns a
// slice of per-item validation errors (nil entries mean the item is valid).
func (v *BatchValidator) Validate(requests []BatchRequest) []error {
	errors := make([]error, len(requests))
	for i, req := range requests {
		if req.JSONRPC != "2.0" {
			errors[i] = fmt.Errorf("invalid jsonrpc version: %q", req.JSONRPC)
			continue
		}
		if req.Method == "" {
			errors[i] = fmt.Errorf("method is required")
			continue
		}
	}
	return errors
}

// ValidateBatchSize checks the batch size constraints.
func (v *BatchValidator) ValidateBatchSize(count int) error {
	if count < MinBatchSize {
		return ErrBatchEmpty
	}
	if count > v.maxSize {
		return fmt.Errorf("rpc: batch exceeds maximum size of %d", v.maxSize)
	}
	return nil
}

// ExtendedBatchHandler extends BatchHandler with validation, statistics,
// and notification batching support.
type ExtendedBatchHandler struct {
	api         *EthAPI
	parallelism int
	stats       BatchStats
	validator   *BatchValidator
}

// NewExtendedBatchHandler creates an extended batch handler with the given API.
func NewExtendedBatchHandler(api *EthAPI) *ExtendedBatchHandler {
	return &ExtendedBatchHandler{
		api:         api,
		parallelism: DefaultParallelism,
		validator:   NewBatchValidator(MaxBatchSize),
	}
}

// SetParallelism sets the concurrency limit for parallel execution.
func (bh *ExtendedBatchHandler) SetParallelism(n int) {
	if n < 1 {
		n = 1
	}
	bh.parallelism = n
}

// HandleBatchValidated parses, validates, and executes a batch request.
// Invalid items receive per-item error responses rather than failing
// the entire batch.
func (bh *ExtendedBatchHandler) HandleBatchValidated(body []byte) ([]BatchResponse, error) {
	requests, err := parseBatchRequests(body)
	if err != nil {
		return nil, err
	}
	if err := bh.validator.ValidateBatchSize(len(requests)); err != nil {
		return nil, err
	}

	// Validate each item.
	itemErrors := bh.validator.Validate(requests)

	// Update stats.
	bh.stats.TotalBatches.Add(1)
	bh.stats.TotalRequests.Add(uint64(len(requests)))
	current := uint64(len(requests))
	for {
		largest := bh.stats.LargestBatch.Load()
		if current <= largest {
			break
		}
		if bh.stats.LargestBatch.CompareAndSwap(largest, current) {
			break
		}
	}

	// Execute valid items in parallel, skip invalid ones.
	return bh.executeWithValidation(requests, itemErrors), nil
}

// executeWithValidation runs valid batch items in parallel and returns
// pre-built error responses for invalid ones.
func (bh *ExtendedBatchHandler) executeWithValidation(
	requests []BatchRequest,
	itemErrors []error,
) []BatchResponse {
	n := len(requests)
	responses := make([]BatchResponse, n)

	sem := make(chan struct{}, bh.parallelism)
	var wg sync.WaitGroup

	for i, req := range requests {
		if itemErrors[i] != nil {
			// Return a validation error for this item.
			responses[i] = BatchResponse{
				JSONRPC: "2.0",
				Error:   &RPCError{Code: ErrCodeInvalidRequest, Message: itemErrors[i].Error()},
				ID:      req.ID,
			}
			bh.stats.TotalErrors.Add(1)
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, r BatchRequest) {
			defer wg.Done()
			defer func() { <-sem }()

			apiReq := &Request{
				JSONRPC: r.JSONRPC,
				Method:  r.Method,
				Params:  r.Params,
				ID:      r.ID,
			}
			resp := bh.api.HandleRequest(apiReq)
			responses[idx] = BatchResponse{
				JSONRPC: resp.JSONRPC,
				Result:  resp.Result,
				Error:   resp.Error,
				ID:      resp.ID,
			}
			if resp.Error != nil {
				bh.stats.TotalErrors.Add(1)
			}
		}(i, req)
	}

	wg.Wait()
	bh.stats.ParallelBatches.Add(1)
	return responses
}

// Stats returns the current batch processing statistics.
func (bh *ExtendedBatchHandler) Stats() BatchStatsSnapshot {
	return bh.stats.Snapshot()
}

// NotificationBatch accumulates subscription notifications and flushes
// them as a batch when the batch is full or a flush interval is reached.
type NotificationBatch struct {
	mu    sync.Mutex
	items []json.RawMessage
	limit int
}

// NewNotificationBatch creates a new batch with the given flush limit.
func NewNotificationBatch(limit int) *NotificationBatch {
	if limit <= 0 {
		limit = MaxNotificationBatchSize
	}
	return &NotificationBatch{
		items: make([]json.RawMessage, 0, limit),
		limit: limit,
	}
}

// Add appends a notification to the batch. Returns the serialized batch
// if the limit is reached, or nil if more items can be added.
func (nb *NotificationBatch) Add(notification interface{}) []byte {
	data, err := json.Marshal(notification)
	if err != nil {
		return nil
	}

	nb.mu.Lock()
	defer nb.mu.Unlock()

	nb.items = append(nb.items, json.RawMessage(data))
	if len(nb.items) >= nb.limit {
		return nb.flushLocked()
	}
	return nil
}

// Flush forces the batch to be serialized and returned, even if not full.
// Returns nil if the batch is empty.
func (nb *NotificationBatch) Flush() []byte {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	return nb.flushLocked()
}

// flushLocked serializes all accumulated items as a JSON array and resets.
// Caller must hold nb.mu.
func (nb *NotificationBatch) flushLocked() []byte {
	if len(nb.items) == 0 {
		return nil
	}
	data, _ := json.Marshal(nb.items)
	nb.items = nb.items[:0]
	return data
}

// Len returns the number of buffered notifications.
func (nb *NotificationBatch) Len() int {
	nb.mu.Lock()
	defer nb.mu.Unlock()
	return len(nb.items)
}

// BatchRequestSummary provides a human-readable summary of a batch request
// for logging and debugging purposes.
type BatchRequestSummary struct {
	Count   int      `json:"count"`
	Methods []string `json:"methods"`
}

// SummarizeBatch extracts method names from a batch for diagnostic logging.
func SummarizeBatch(requests []BatchRequest) BatchRequestSummary {
	methods := make([]string, len(requests))
	for i, req := range requests {
		methods[i] = req.Method
	}
	return BatchRequestSummary{
		Count:   len(requests),
		Methods: methods,
	}
}

// SplitBatch splits a large batch into smaller sub-batches of at most chunkSize.
// This is useful when batch size limits need to be imposed at a higher level.
func SplitBatch(requests []BatchRequest, chunkSize int) [][]BatchRequest {
	if chunkSize <= 0 {
		chunkSize = MaxBatchSize
	}
	var chunks [][]BatchRequest
	for i := 0; i < len(requests); i += chunkSize {
		end := i + chunkSize
		if end > len(requests) {
			end = len(requests)
		}
		chunks = append(chunks, requests[i:end])
	}
	return chunks
}
