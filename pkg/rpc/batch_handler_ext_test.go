package rpc

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNewBatchValidator(t *testing.T) {
	v := NewBatchValidator(50)
	if v.maxSize != 50 {
		t.Fatalf("want maxSize 50, got %d", v.maxSize)
	}
}

func TestNewBatchValidator_DefaultSize(t *testing.T) {
	v := NewBatchValidator(0)
	if v.maxSize != MaxBatchSize {
		t.Fatalf("want default maxSize %d, got %d", MaxBatchSize, v.maxSize)
	}
}

func TestBatchValidator_ValidateBatchSize_Empty(t *testing.T) {
	v := NewBatchValidator(100)
	err := v.ValidateBatchSize(0)
	if err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestBatchValidator_ValidateBatchSize_TooLarge(t *testing.T) {
	v := NewBatchValidator(10)
	err := v.ValidateBatchSize(11)
	if err == nil {
		t.Fatal("expected error for oversized batch")
	}
}

func TestBatchValidator_ValidateBatchSize_Valid(t *testing.T) {
	v := NewBatchValidator(100)
	err := v.ValidateBatchSize(50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBatchValidator_Validate_AllValid(t *testing.T) {
	v := NewBatchValidator(100)
	requests := []BatchRequest{
		{JSONRPC: "2.0", Method: "eth_chainId"},
		{JSONRPC: "2.0", Method: "eth_blockNumber"},
	}
	errors := v.Validate(requests)
	for i, err := range errors {
		if err != nil {
			t.Fatalf("item %d: unexpected error: %v", i, err)
		}
	}
}

func TestBatchValidator_Validate_InvalidVersion(t *testing.T) {
	v := NewBatchValidator(100)
	requests := []BatchRequest{
		{JSONRPC: "1.0", Method: "eth_chainId"},
	}
	errors := v.Validate(requests)
	if errors[0] == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestBatchValidator_Validate_EmptyMethod(t *testing.T) {
	v := NewBatchValidator(100)
	requests := []BatchRequest{
		{JSONRPC: "2.0", Method: ""},
	}
	errors := v.Validate(requests)
	if errors[0] == nil {
		t.Fatal("expected error for empty method")
	}
}

func TestBatchValidator_Validate_Mixed(t *testing.T) {
	v := NewBatchValidator(100)
	requests := []BatchRequest{
		{JSONRPC: "2.0", Method: "eth_chainId"},    // valid
		{JSONRPC: "1.0", Method: "eth_blockNumber"}, // invalid version
		{JSONRPC: "2.0", Method: ""},                // empty method
		{JSONRPC: "2.0", Method: "eth_gasPrice"},    // valid
	}
	errors := v.Validate(requests)
	if errors[0] != nil {
		t.Fatal("item 0 should be valid")
	}
	if errors[1] == nil {
		t.Fatal("item 1 should have version error")
	}
	if errors[2] == nil {
		t.Fatal("item 2 should have method error")
	}
	if errors[3] != nil {
		t.Fatal("item 3 should be valid")
	}
}

func TestExtendedBatchHandler_HandleBatchValidated(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	body := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":2}
	]`
	responses, err := bh.HandleBatchValidated([]byte(body))
	if err != nil {
		t.Fatalf("HandleBatchValidated: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("want 2 responses, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("response 0 error: %v", responses[0].Error.Message)
	}
	if responses[1].Error != nil {
		t.Fatalf("response 1 error: %v", responses[1].Error.Message)
	}
}

func TestExtendedBatchHandler_HandleBatchValidated_MixedValidity(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	body := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"1.0","method":"eth_blockNumber","params":[],"id":2},
		{"jsonrpc":"2.0","method":"eth_gasPrice","params":[],"id":3}
	]`
	responses, err := bh.HandleBatchValidated([]byte(body))
	if err != nil {
		t.Fatalf("HandleBatchValidated: %v", err)
	}
	if len(responses) != 3 {
		t.Fatalf("want 3 responses, got %d", len(responses))
	}
	// Item 0: valid.
	if responses[0].Error != nil {
		t.Fatalf("response 0 should succeed: %v", responses[0].Error.Message)
	}
	// Item 1: invalid version.
	if responses[1].Error == nil {
		t.Fatal("response 1 should have error")
	}
	if responses[1].Error.Code != ErrCodeInvalidRequest {
		t.Fatalf("want code %d, got %d", ErrCodeInvalidRequest, responses[1].Error.Code)
	}
	// Item 2: valid.
	if responses[2].Error != nil {
		t.Fatalf("response 2 should succeed: %v", responses[2].Error.Message)
	}
}

func TestExtendedBatchHandler_HandleBatchValidated_Empty(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	_, err := bh.HandleBatchValidated([]byte(`[]`))
	if err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestExtendedBatchHandler_HandleBatchValidated_TooLarge(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	var reqs []string
	for i := 0; i <= MaxBatchSize; i++ {
		reqs = append(reqs, `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`)
	}
	body := "[" + strings.Join(reqs, ",") + "]"

	_, err := bh.HandleBatchValidated([]byte(body))
	if err == nil {
		t.Fatal("expected error for oversized batch")
	}
}

func TestExtendedBatchHandler_Stats(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	// Initial stats.
	stats := bh.Stats()
	if stats.TotalBatches != 0 {
		t.Fatalf("want 0 total batches, got %d", stats.TotalBatches)
	}

	// Execute a batch.
	body := `[{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}]`
	bh.HandleBatchValidated([]byte(body))

	stats = bh.Stats()
	if stats.TotalBatches != 1 {
		t.Fatalf("want 1 total batch, got %d", stats.TotalBatches)
	}
	if stats.TotalRequests != 1 {
		t.Fatalf("want 1 total request, got %d", stats.TotalRequests)
	}
	if stats.LargestBatch != 1 {
		t.Fatalf("want largest batch 1, got %d", stats.LargestBatch)
	}
}

func TestExtendedBatchHandler_Stats_ErrorTracking(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	body := `[
		{"jsonrpc":"1.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"nonexistent","params":[],"id":2}
	]`
	bh.HandleBatchValidated([]byte(body))

	stats := bh.Stats()
	// Item 0 has invalid version (validation error) + item 1 method not found (runtime error).
	if stats.TotalErrors != 2 {
		t.Fatalf("want 2 total errors, got %d", stats.TotalErrors)
	}
}

func TestExtendedBatchHandler_SetParallelism(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	bh.SetParallelism(4)
	if bh.parallelism != 4 {
		t.Fatalf("want parallelism 4, got %d", bh.parallelism)
	}
	bh.SetParallelism(0)
	if bh.parallelism != 1 {
		t.Fatalf("want minimum parallelism 1, got %d", bh.parallelism)
	}
}

func TestExtendedBatchHandler_OrderPreserved(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)
	bh.SetParallelism(8)

	var reqs []string
	n := 20
	for i := 0; i < n; i++ {
		reqs = append(reqs, fmt.Sprintf(
			`{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":%d}`, i))
	}
	body := "[" + strings.Join(reqs, ",") + "]"

	responses, err := bh.HandleBatchValidated([]byte(body))
	if err != nil {
		t.Fatalf("HandleBatchValidated: %v", err)
	}
	for i := 0; i < n; i++ {
		expectedID := fmt.Sprintf("%d", i)
		if string(responses[i].ID) != expectedID {
			t.Fatalf("response %d: ID %s, want %s", i, responses[i].ID, expectedID)
		}
	}
}

func TestNotificationBatch_Basic(t *testing.T) {
	nb := NewNotificationBatch(3)
	if nb.Len() != 0 {
		t.Fatalf("want 0 items, got %d", nb.Len())
	}

	// First two should not trigger flush.
	r1 := nb.Add("event1")
	if r1 != nil {
		t.Fatal("expected no flush after first add")
	}
	r2 := nb.Add("event2")
	if r2 != nil {
		t.Fatal("expected no flush after second add")
	}
	if nb.Len() != 2 {
		t.Fatalf("want 2 items, got %d", nb.Len())
	}

	// Third should trigger flush.
	r3 := nb.Add("event3")
	if r3 == nil {
		t.Fatal("expected flush after third add")
	}
	// Verify the flushed data is valid JSON.
	var items []json.RawMessage
	if err := json.Unmarshal(r3, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("want 3 items in flush, got %d", len(items))
	}
	// After flush, batch should be empty.
	if nb.Len() != 0 {
		t.Fatalf("want 0 items after flush, got %d", nb.Len())
	}
}

func TestNotificationBatch_ManualFlush(t *testing.T) {
	nb := NewNotificationBatch(10)
	nb.Add("event1")
	nb.Add("event2")

	data := nb.Flush()
	if data == nil {
		t.Fatal("expected data from flush")
	}
	var items []json.RawMessage
	json.Unmarshal(data, &items)
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if nb.Len() != 0 {
		t.Fatalf("want 0 after manual flush, got %d", nb.Len())
	}
}

func TestNotificationBatch_FlushEmpty(t *testing.T) {
	nb := NewNotificationBatch(10)
	data := nb.Flush()
	if data != nil {
		t.Fatal("expected nil for empty flush")
	}
}

func TestNotificationBatch_DefaultLimit(t *testing.T) {
	nb := NewNotificationBatch(0)
	if nb.limit != MaxNotificationBatchSize {
		t.Fatalf("want default limit %d, got %d", MaxNotificationBatchSize, nb.limit)
	}
}

func TestSummarizeBatch(t *testing.T) {
	requests := []BatchRequest{
		{Method: "eth_chainId"},
		{Method: "eth_blockNumber"},
		{Method: "eth_gasPrice"},
	}
	summary := SummarizeBatch(requests)
	if summary.Count != 3 {
		t.Fatalf("want count 3, got %d", summary.Count)
	}
	if len(summary.Methods) != 3 {
		t.Fatalf("want 3 methods, got %d", len(summary.Methods))
	}
	if summary.Methods[0] != "eth_chainId" {
		t.Fatalf("want eth_chainId, got %s", summary.Methods[0])
	}
}

func TestSplitBatch_EvenSplit(t *testing.T) {
	requests := make([]BatchRequest, 10)
	for i := range requests {
		requests[i] = BatchRequest{Method: fmt.Sprintf("m%d", i)}
	}
	chunks := SplitBatch(requests, 5)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 5 || len(chunks[1]) != 5 {
		t.Fatal("expected even 5-5 split")
	}
}

func TestSplitBatch_UnevenSplit(t *testing.T) {
	requests := make([]BatchRequest, 7)
	chunks := SplitBatch(requests, 3)
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks, got %d", len(chunks))
	}
	if len(chunks[0]) != 3 || len(chunks[1]) != 3 || len(chunks[2]) != 1 {
		t.Fatalf("expected 3-3-1 split, got %d-%d-%d", len(chunks[0]), len(chunks[1]), len(chunks[2]))
	}
}

func TestSplitBatch_SingleChunk(t *testing.T) {
	requests := make([]BatchRequest, 3)
	chunks := SplitBatch(requests, 10)
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
}

func TestSplitBatch_DefaultChunkSize(t *testing.T) {
	requests := make([]BatchRequest, 5)
	chunks := SplitBatch(requests, 0)
	// With default MaxBatchSize (100), should be a single chunk.
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk with default size, got %d", len(chunks))
	}
}

func TestBatchStats_Snapshot(t *testing.T) {
	var stats BatchStats
	stats.TotalBatches.Add(5)
	stats.TotalRequests.Add(20)
	stats.TotalErrors.Add(3)
	stats.LargestBatch.Store(10)
	stats.ParallelBatches.Add(4)

	snap := stats.Snapshot()
	if snap.TotalBatches != 5 {
		t.Fatalf("want 5, got %d", snap.TotalBatches)
	}
	if snap.TotalRequests != 20 {
		t.Fatalf("want 20, got %d", snap.TotalRequests)
	}
	if snap.TotalErrors != 3 {
		t.Fatalf("want 3, got %d", snap.TotalErrors)
	}
	if snap.LargestBatch != 10 {
		t.Fatalf("want 10, got %d", snap.LargestBatch)
	}
	if snap.ParallelBatches != 4 {
		t.Fatalf("want 4, got %d", snap.ParallelBatches)
	}
}

func TestBatchStatsSnapshot_JSON(t *testing.T) {
	snap := BatchStatsSnapshot{
		TotalBatches:    5,
		TotalRequests:   20,
		TotalErrors:     3,
		LargestBatch:    10,
		ParallelBatches: 4,
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed BatchStatsSnapshot
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.TotalBatches != 5 {
		t.Fatalf("roundtrip: want 5, got %d", parsed.TotalBatches)
	}
}

func TestExtendedBatchHandler_LargestBatchTracking(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	bh := NewExtendedBatchHandler(api)

	// First batch: 2 items.
	body1 := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":2}
	]`
	bh.HandleBatchValidated([]byte(body1))
	if bh.Stats().LargestBatch != 2 {
		t.Fatalf("want largest 2, got %d", bh.Stats().LargestBatch)
	}

	// Second batch: 5 items.
	var reqs []string
	for i := 0; i < 5; i++ {
		reqs = append(reqs, fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":%d}`, i))
	}
	body2 := "[" + strings.Join(reqs, ",") + "]"
	bh.HandleBatchValidated([]byte(body2))
	if bh.Stats().LargestBatch != 5 {
		t.Fatalf("want largest 5, got %d", bh.Stats().LargestBatch)
	}

	// Third batch: 3 items (should not update largest).
	body3 := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1},
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":2},
		{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":3}
	]`
	bh.HandleBatchValidated([]byte(body3))
	if bh.Stats().LargestBatch != 5 {
		t.Fatalf("want largest still 5, got %d", bh.Stats().LargestBatch)
	}
}

func TestBatchConstants(t *testing.T) {
	if MinBatchSize != 1 {
		t.Fatalf("want MinBatchSize 1, got %d", MinBatchSize)
	}
	if MaxNotificationBatchSize != 50 {
		t.Fatalf("want MaxNotificationBatchSize 50, got %d", MaxNotificationBatchSize)
	}
	if DefaultBatchTimeout != 5000 {
		t.Fatalf("want DefaultBatchTimeout 5000, got %d", DefaultBatchTimeout)
	}
}
