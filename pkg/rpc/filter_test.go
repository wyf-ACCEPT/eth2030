package rpc

import (
	"bytes"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// ---------- MatchFilter edge cases ----------

func TestMatchFilter_NilLog(t *testing.T) {
	// A nil topics slice log should match an empty query.
	log := &types.Log{
		Address: types.HexToAddress("0xaaaa"),
	}
	q := FilterQuery{}
	if !MatchFilter(log, q) {
		t.Fatal("empty query should match log with nil topics")
	}
}

func TestMatchFilter_EmptyTopicSlice(t *testing.T) {
	// Log has empty topics, query has empty topics -- should match.
	log := &types.Log{
		Address: types.HexToAddress("0xaaaa"),
		Topics:  []types.Hash{},
	}
	q := FilterQuery{Topics: [][]types.Hash{}}
	if !MatchFilter(log, q) {
		t.Fatal("empty topic slices should match")
	}
}

func TestMatchFilter_WildcardInMiddlePosition(t *testing.T) {
	topic0 := types.HexToHash("0x1111")
	topic2 := types.HexToHash("0x3333")
	log := &types.Log{
		Topics: []types.Hash{topic0, types.HexToHash("0x2222"), topic2},
	}
	// Wildcard at position 1, match at positions 0 and 2.
	q := FilterQuery{
		Topics: [][]types.Hash{
			{topic0},
			{}, // wildcard
			{topic2},
		},
	}
	if !MatchFilter(log, q) {
		t.Fatal("should match with wildcard in middle position")
	}
}

func TestMatchFilter_AllWildcards(t *testing.T) {
	log := &types.Log{
		Address: types.HexToAddress("0xaaaa"),
		Topics:  []types.Hash{types.HexToHash("0x1111"), types.HexToHash("0x2222")},
	}
	// All positions are wildcards.
	q := FilterQuery{
		Topics: [][]types.Hash{{}, {}, {}},
	}
	// Third position is wildcard but log only has 2 topics -- wildcard should still pass.
	if !MatchFilter(log, q) {
		t.Fatal("all-wildcard query should match any log")
	}
}

func TestMatchFilter_MultipleAddresses_NoMatch(t *testing.T) {
	log := &types.Log{
		Address: types.HexToAddress("0xcccc"),
	}
	q := FilterQuery{
		Addresses: []types.Address{
			types.HexToAddress("0xaaaa"),
			types.HexToAddress("0xbbbb"),
		},
	}
	if MatchFilter(log, q) {
		t.Fatal("should not match when log address is not in address list")
	}
}

func TestMatchFilter_AddressAndTopicCombined(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	topic := types.HexToHash("0x1111")

	// Matching address, wrong topic.
	log := &types.Log{
		Address: addr,
		Topics:  []types.Hash{types.HexToHash("0x9999")},
	}
	q := FilterQuery{
		Addresses: []types.Address{addr},
		Topics:    [][]types.Hash{{topic}},
	}
	if MatchFilter(log, q) {
		t.Fatal("should not match when topic doesn't match")
	}

	// Wrong address, matching topic.
	log2 := &types.Log{
		Address: types.HexToAddress("0xbbbb"),
		Topics:  []types.Hash{topic},
	}
	if MatchFilter(log2, q) {
		t.Fatal("should not match when address doesn't match")
	}
}

// ---------- FilterLogs edge cases ----------

func TestFilterLogs_EmptyInput(t *testing.T) {
	result := FilterLogs(nil, FilterQuery{})
	if result != nil {
		t.Fatalf("want nil for empty input, got %d logs", len(result))
	}

	result2 := FilterLogs([]*types.Log{}, FilterQuery{})
	if result2 != nil {
		t.Fatalf("want nil for empty slice, got %d logs", len(result2))
	}
}

func TestFilterLogs_AllMatch(t *testing.T) {
	logs := []*types.Log{
		{Address: types.HexToAddress("0xaaaa")},
		{Address: types.HexToAddress("0xbbbb")},
		{Address: types.HexToAddress("0xcccc")},
	}
	result := FilterLogs(logs, FilterQuery{})
	if len(result) != 3 {
		t.Fatalf("want 3 logs, got %d", len(result))
	}
}

func TestFilterLogs_NoneMatch(t *testing.T) {
	logs := []*types.Log{
		{Address: types.HexToAddress("0xaaaa")},
		{Address: types.HexToAddress("0xbbbb")},
	}
	q := FilterQuery{Addresses: []types.Address{types.HexToAddress("0xcccc")}}
	result := FilterLogs(logs, q)
	if result != nil {
		t.Fatalf("want nil when none match, got %d logs", len(result))
	}
}

func TestFilterLogs_TopicFilterOnly(t *testing.T) {
	topic1 := types.HexToHash("0x1111")
	topic2 := types.HexToHash("0x2222")
	logs := []*types.Log{
		{Topics: []types.Hash{topic1}},
		{Topics: []types.Hash{topic2}},
		{Topics: []types.Hash{topic1, topic2}},
	}
	// Topic filter at position 0: match topic2. Only the second log has topic2
	// at position 0 (the third has topic1 at position 0, topic2 at position 1).
	q := FilterQuery{Topics: [][]types.Hash{{topic2}}}
	result := FilterLogs(logs, q)
	if len(result) != 1 {
		t.Fatalf("want 1 log matching topic2 at pos 0, got %d", len(result))
	}

	// Match either topic1 or topic2 at position 0 -> all 3 logs match.
	q2 := FilterQuery{Topics: [][]types.Hash{{topic1, topic2}}}
	result2 := FilterLogs(logs, q2)
	if len(result2) != 3 {
		t.Fatalf("want 3 logs matching topic1 OR topic2 at pos 0, got %d", len(result2))
	}
}

// ---------- FilterLogsWithBloom edge cases ----------

func TestFilterLogsWithBloom_NoMatch(t *testing.T) {
	// Bloom that doesn't contain the queried address.
	addr := types.HexToAddress("0xaaaa")
	otherAddr := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	log := &types.Log{Address: addr}
	bloom := types.LogsBloom([]*types.Log{log})

	// Query for a completely different address -- bloom should reject.
	// Note: bloom filters can have false positives, so we use a distinctive address.
	q := FilterQuery{Addresses: []types.Address{otherAddr}}
	result := FilterLogsWithBloom(bloom, []*types.Log{log}, q)
	// The bloom pre-screen may or may not reject; if it passes bloom check,
	// exact matching will filter it out. Either way, result should be empty.
	if len(result) != 0 {
		t.Fatalf("want 0 logs (address doesn't match), got %d", len(result))
	}
}

func TestFilterLogsWithBloom_EmptyBloom(t *testing.T) {
	// Empty bloom with wildcard query should return all logs.
	bloom := types.Bloom{}
	logs := []*types.Log{
		{Address: types.HexToAddress("0xaaaa")},
	}
	result := FilterLogsWithBloom(bloom, logs, FilterQuery{})
	if len(result) != 1 {
		t.Fatalf("want 1 log with wildcard query, got %d", len(result))
	}
}

func TestFilterLogsWithBloom_TopicMatch(t *testing.T) {
	topic := crypto.Keccak256Hash([]byte("Event(uint256)"))
	addr := types.HexToAddress("0xaaaa")
	log := &types.Log{
		Address: addr,
		Topics:  []types.Hash{topic},
	}
	bloom := types.LogsBloom([]*types.Log{log})

	q := FilterQuery{
		Addresses: []types.Address{addr},
		Topics:    [][]types.Hash{{topic}},
	}
	result := FilterLogsWithBloom(bloom, []*types.Log{log}, q)
	if len(result) != 1 {
		t.Fatalf("want 1 log, got %d", len(result))
	}
}

// ---------- bloomMatchesQuery edge cases ----------

func TestBloomMatchesQuery_MultipleAddresses(t *testing.T) {
	addr1 := types.HexToAddress("0xaaaa")
	addr2 := types.HexToAddress("0xbbbb")
	log := &types.Log{Address: addr1}
	bloom := types.LogsBloom([]*types.Log{log})

	// Query with both addresses -- at least one is in bloom.
	q := FilterQuery{Addresses: []types.Address{addr1, addr2}}
	if !bloomMatchesQuery(bloom, q) {
		t.Fatal("bloom should match when at least one address is present")
	}
}

func TestBloomMatchesQuery_MultipleTopicPositions(t *testing.T) {
	topic0 := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	topic1 := crypto.Keccak256Hash([]byte("sender"))
	log := &types.Log{
		Topics: []types.Hash{topic0, topic1},
	}
	bloom := types.LogsBloom([]*types.Log{log})

	// Both topic positions present in bloom.
	q := FilterQuery{Topics: [][]types.Hash{{topic0}, {topic1}}}
	if !bloomMatchesQuery(bloom, q) {
		t.Fatal("bloom should match when both topic positions are present")
	}
}

func TestBloomMatchesQuery_WildcardTopicPosition(t *testing.T) {
	topic0 := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	log := &types.Log{Topics: []types.Hash{topic0}}
	bloom := types.LogsBloom([]*types.Log{log})

	// Wildcard at position 0, specific at position 1 (not in bloom).
	otherTopic := crypto.Keccak256Hash([]byte("Nonexistent()"))
	q := FilterQuery{Topics: [][]types.Hash{{}, {otherTopic}}}
	// Position 1 topic likely not in bloom -> should reject.
	// (Unless false positive in bloom.)
	_ = bloomMatchesQuery(bloom, q) // Just ensure no panic.
}

func TestBloomMatchesAddress(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	log := &types.Log{Address: addr}
	bloom := types.LogsBloom([]*types.Log{log})

	if !bloomMatchesAddress(bloom, addr) {
		t.Fatal("bloom should contain the address that was added")
	}
}

func TestBloomMatchesTopic(t *testing.T) {
	topic := crypto.Keccak256Hash([]byte("Event()"))
	log := &types.Log{Topics: []types.Hash{topic}}
	bloom := types.LogsBloom([]*types.Log{log})

	if !bloomMatchesTopic(bloom, topic) {
		t.Fatal("bloom should contain the topic that was added")
	}
}

// ---------- SubscriptionManager filter lifecycle ----------

func TestNewLogFilter_DefaultFromBlock(t *testing.T) {
	// When FromBlock is nil, the filter should start from the current block.
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id := sm.NewLogFilter(FilterQuery{})
	if id == "" {
		t.Fatal("expected non-empty filter ID")
	}

	// The filter's lastBlock should be set to current header number (42).
	sm.mu.Lock()
	f := sm.filters[id]
	sm.mu.Unlock()

	if f == nil {
		t.Fatal("filter not found in map")
	}
	if f.lastBlock != 42 {
		t.Fatalf("want lastBlock=42 (current header), got %d", f.lastBlock)
	}
}

func TestNewLogFilter_FromBlockZero(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	from := uint64(0)
	id := sm.NewLogFilter(FilterQuery{FromBlock: &from})

	sm.mu.Lock()
	f := sm.filters[id]
	sm.mu.Unlock()

	// FromBlock=0, so lastBlock should be 0 (capped at 0, not -1).
	if f.lastBlock != 0 {
		t.Fatalf("want lastBlock=0, got %d", f.lastBlock)
	}
}

func TestNotifyNewBlock_MultipleFilters(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id1 := sm.NewBlockFilter()
	id2 := sm.NewBlockFilter()
	id3 := sm.NewPendingTxFilter() // should not receive block hashes

	hash := types.HexToHash("0xdeadbeef")
	sm.NotifyNewBlock(hash)

	// Block filters should both get the hash.
	result1, ok := sm.GetFilterChanges(id1)
	if !ok {
		t.Fatal("filter 1 not found")
	}
	hashes1 := result1.([]types.Hash)
	if len(hashes1) != 1 || hashes1[0] != hash {
		t.Fatalf("filter 1: want [%v], got %v", hash, hashes1)
	}

	result2, ok := sm.GetFilterChanges(id2)
	if !ok {
		t.Fatal("filter 2 not found")
	}
	hashes2 := result2.([]types.Hash)
	if len(hashes2) != 1 || hashes2[0] != hash {
		t.Fatalf("filter 2: want [%v], got %v", hash, hashes2)
	}

	// Pending tx filter should have no hashes.
	result3, ok := sm.GetFilterChanges(id3)
	if !ok {
		t.Fatal("filter 3 not found")
	}
	hashes3 := result3.([]types.Hash)
	if len(hashes3) != 0 {
		t.Fatalf("pending tx filter should have 0 hashes, got %d", len(hashes3))
	}
}

func TestNotifyPendingTx_MultipleFilters(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id1 := sm.NewPendingTxFilter()
	id2 := sm.NewPendingTxFilter()
	id3 := sm.NewBlockFilter() // should not receive tx hashes

	txHash := types.HexToHash("0x1234")
	sm.NotifyPendingTx(txHash)

	result1, _ := sm.GetFilterChanges(id1)
	hashes1 := result1.([]types.Hash)
	if len(hashes1) != 1 {
		t.Fatalf("pending filter 1: want 1 hash, got %d", len(hashes1))
	}

	result2, _ := sm.GetFilterChanges(id2)
	hashes2 := result2.([]types.Hash)
	if len(hashes2) != 1 {
		t.Fatalf("pending filter 2: want 1 hash, got %d", len(hashes2))
	}

	result3, _ := sm.GetFilterChanges(id3)
	hashes3 := result3.([]types.Hash)
	if len(hashes3) != 0 {
		t.Fatalf("block filter should have 0 tx hashes, got %d", len(hashes3))
	}
}

func TestNotifyNewBlock_MultipleHashes(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id := sm.NewBlockFilter()

	h1 := types.HexToHash("0x0001")
	h2 := types.HexToHash("0x0002")
	h3 := types.HexToHash("0x0003")
	sm.NotifyNewBlock(h1)
	sm.NotifyNewBlock(h2)
	sm.NotifyNewBlock(h3)

	result, _ := sm.GetFilterChanges(id)
	hashes := result.([]types.Hash)
	if len(hashes) != 3 {
		t.Fatalf("want 3 hashes, got %d", len(hashes))
	}
	if hashes[0] != h1 || hashes[1] != h2 || hashes[2] != h3 {
		t.Fatalf("hashes out of order: %v", hashes)
	}
}

func TestCleanupStale_KeepsFreshFilters(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	sm.NewBlockFilter() // stale
	freshID := sm.NewBlockFilter()

	// Make the first filter stale, keep the second fresh.
	sm.mu.Lock()
	for id, f := range sm.filters {
		if id != freshID {
			f.lastPoll = time.Now().Add(-filterTimeout * 2)
		}
	}
	sm.mu.Unlock()

	removed := sm.CleanupStale()
	if removed != 1 {
		t.Fatalf("want 1 removed, got %d", removed)
	}
	if sm.FilterCount() != 1 {
		t.Fatalf("want 1 remaining filter, got %d", sm.FilterCount())
	}
}

func TestCleanupStale_NoStaleFilters(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	sm.NewBlockFilter()
	sm.NewPendingTxFilter()

	removed := sm.CleanupStale()
	if removed != 0 {
		t.Fatalf("want 0 removed (all fresh), got %d", removed)
	}
	if sm.FilterCount() != 2 {
		t.Fatalf("want 2 filters, got %d", sm.FilterCount())
	}
}

func TestFilterIDs_Unique(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	ids := make(map[string]bool)
	for i := 0; i < 50; i++ {
		id := sm.NewBlockFilter()
		if ids[id] {
			t.Fatalf("duplicate filter ID: %s", id)
		}
		ids[id] = true
	}
}

// ---------- Concurrent filter operations ----------

func TestConcurrentFilterOperations(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	var wg sync.WaitGroup
	filterIDs := make(chan string, 100)

	// Concurrently create filters.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := sm.NewBlockFilter()
			filterIDs <- id
		}()
	}

	// Concurrently notify.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			hash := types.HexToHash("0x" + string(rune('a'+n)))
			sm.NotifyNewBlock(hash)
		}(i)
	}

	wg.Wait()
	close(filterIDs)

	// All filters should exist.
	count := sm.FilterCount()
	if count != 20 {
		t.Fatalf("want 20 filters, got %d", count)
	}

	// Concurrently poll and uninstall.
	var wg2 sync.WaitGroup
	for id := range filterIDs {
		wg2.Add(1)
		go func(fid string) {
			defer wg2.Done()
			sm.GetFilterChanges(fid)
			sm.Uninstall(fid)
		}(id)
	}
	wg2.Wait()

	if sm.FilterCount() != 0 {
		t.Fatalf("want 0 filters after uninstall, got %d", sm.FilterCount())
	}
}

// ---------- QueryLogs ----------

func TestQueryLogs_WithRange(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	// Query only block 42.
	from := uint64(42)
	to := uint64(42)
	logs := sm.QueryLogs(FilterQuery{FromBlock: &from, ToBlock: &to})

	// newTestBackend puts 1 log in block 42.
	if len(logs) != 1 {
		t.Fatalf("want 1 log from block 42, got %d", len(logs))
	}
}

func TestQueryLogs_FromBlockOnly(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	// Only set FromBlock, ToBlock defaults to current (42).
	from := uint64(42)
	logs := sm.QueryLogs(FilterQuery{FromBlock: &from})
	// Blocks 42..42 (current is 42).
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
}

func TestQueryLogs_AddressFilter(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	contractB := types.HexToAddress("0xbbbb")
	from := uint64(42)
	to := uint64(43)
	logs := sm.QueryLogs(FilterQuery{
		FromBlock: &from,
		ToBlock:   &to,
		Addresses: []types.Address{contractB},
	})
	// Only block 43 has a log from contractB.
	if len(logs) != 1 {
		t.Fatalf("want 1 log from contractB, got %d", len(logs))
	}
}

func TestQueryLogs_OutOfRange(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	from := uint64(100)
	to := uint64(200)
	logs := sm.QueryLogs(FilterQuery{FromBlock: &from, ToBlock: &to})
	if len(logs) != 0 {
		t.Fatalf("want 0 logs for out-of-range query, got %d", len(logs))
	}
}

// ---------- RPC-level filter tests ----------

func TestRPC_EthNewFilter_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_newFilter")

	if resp.Error == nil {
		t.Fatal("expected error for missing filter criteria")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestRPC_EthGetFilterChanges_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getFilterChanges")

	if resp.Error == nil {
		t.Fatal("expected error for missing filter ID")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

func TestRPC_EthGetFilterLogs_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getFilterLogs")

	if resp.Error == nil {
		t.Fatal("expected error for missing filter ID")
	}
}

func TestRPC_EthUninstallFilter_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_uninstallFilter")

	if resp.Error == nil {
		t.Fatal("expected error for missing filter ID")
	}
}

func TestRPC_EthGetFilterChanges_NonExistentFilter(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getFilterChanges", "0xdeadbeef")

	if resp.Error == nil {
		t.Fatal("expected error for non-existent filter")
	}
}

func TestRPC_EthGetFilterLogs_BlockFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	// Create a block filter, then try eth_getFilterLogs (should fail since
	// getFilterLogs only works with log filters).
	resp := callRPC(t, api, "eth_newBlockFilter")
	filterID := resp.Result.(string)

	logsResp := callRPC(t, api, "eth_getFilterLogs", filterID)
	if logsResp.Error == nil {
		t.Fatal("expected error when calling getFilterLogs on a block filter")
	}
}

func TestRPC_EthNewFilter_WithAddressAndTopics(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	resp := callRPC(t, api, "eth_newFilter", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2b",
		"address":   []string{"0x000000000000000000000000000000000000aaaa"},
		"topics":    [][]string{{encodeHash(transferTopic)}},
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	filterID := resp.Result.(string)

	// Use eth_getFilterLogs to retrieve.
	logsResp := callRPC(t, api, "eth_getFilterLogs", filterID)
	if logsResp.Error != nil {
		t.Fatalf("error: %v", logsResp.Error.Message)
	}
	rpcLogs := logsResp.Result.([]*RPCLog)
	// Block 42 has a Transfer log from contractA (0xaaaa), block 43 also.
	if len(rpcLogs) != 2 {
		t.Fatalf("want 2 Transfer logs from 0xaaaa, got %d", len(rpcLogs))
	}
}

func TestRPC_EthGetLogs_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "eth_getLogs")

	if resp.Error == nil {
		t.Fatal("expected error for missing filter criteria")
	}
}

func TestRPC_EthGetLogs_TopicFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	approvalTopic := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2b",
		"topics":    [][]string{{encodeHash(approvalTopic)}},
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	logs := resp.Result.([]*RPCLog)
	// Only block 43 has an Approval log.
	if len(logs) != 1 {
		t.Fatalf("want 1 Approval log, got %d", len(logs))
	}
}

func TestRPC_EthGetLogs_EmptyResult(t *testing.T) {
	mb := newMockBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getLogs", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
		"address":   []string{"0x0000000000000000000000000000000000009999"},
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	logs := resp.Result.([]*RPCLog)
	if len(logs) != 0 {
		t.Fatalf("want 0 logs for non-existent address, got %d", len(logs))
	}
}

// ---------- Server-level filter tests ----------

func TestServer_NewFilterAndPoll(t *testing.T) {
	mb := newTestBackend()
	srv := NewServer(mb)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Create a block filter.
	createReq := `{"jsonrpc":"2.0","method":"eth_newBlockFilter","params":[],"id":1}`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(createReq))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var createResp Response
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if createResp.Error != nil {
		t.Fatalf("RPC error: %v", createResp.Error.Message)
	}

	filterID, ok := createResp.Result.(string)
	if !ok || filterID == "" {
		t.Fatal("expected non-empty filter ID")
	}

	// Poll for changes (should be empty).
	pollBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_getFilterChanges",
		"params":  []string{filterID},
		"id":      2,
	})
	resp2, err := http.Post(ts.URL, "application/json", bytes.NewBuffer(pollBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp2.Body.Close()

	var pollResp Response
	if err := json.NewDecoder(resp2.Body).Decode(&pollResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if pollResp.Error != nil {
		t.Fatalf("poll error: %v", pollResp.Error.Message)
	}
}

func TestServer_InvalidJSON(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString("not-json{"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	if rpcResp.Error.Code != ErrCodeParse {
		t.Fatalf("want error code %d, got %d", ErrCodeParse, rpcResp.Error.Code)
	}
}

func TestServer_BatchRequest(t *testing.T) {
	srv := NewServer(newMockBackend())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// A single request (not a batch) should work.
	body := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`
	resp, err := http.Post(ts.URL, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	var rpcResp Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error != nil {
		t.Fatalf("error: %v", rpcResp.Error.Message)
	}
	if rpcResp.Result != "0x539" {
		t.Fatalf("want 0x539, got %v", rpcResp.Result)
	}
}

// ---------- matchLog helper (used by getLogs) ----------

func TestMatchLog_EmptyFilters(t *testing.T) {
	log := &types.Log{
		Address: types.HexToAddress("0xaaaa"),
		Topics:  []types.Hash{types.HexToHash("0x1111")},
	}
	if !matchLog(log, nil, nil) {
		t.Fatal("empty filters should match any log")
	}
}

func TestMatchLog_AddressFilter(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	log := &types.Log{Address: addr}

	filter := map[types.Address]bool{addr: true}
	if !matchLog(log, filter, nil) {
		t.Fatal("should match when address is in filter")
	}

	other := types.HexToAddress("0xbbbb")
	filter2 := map[types.Address]bool{other: true}
	if matchLog(log, filter2, nil) {
		t.Fatal("should not match when address is not in filter")
	}
}

func TestMatchLog_TopicFilter(t *testing.T) {
	topic := types.HexToHash("0x1111")
	log := &types.Log{Topics: []types.Hash{topic}}

	// Matching topic.
	tFilter := [][]types.Hash{{topic}}
	if !matchLog(log, nil, tFilter) {
		t.Fatal("should match when topic matches")
	}

	// Non-matching topic.
	other := types.HexToHash("0x2222")
	tFilter2 := [][]types.Hash{{other}}
	if matchLog(log, nil, tFilter2) {
		t.Fatal("should not match when topic doesn't match")
	}
}

func TestMatchLog_TopicWildcard(t *testing.T) {
	log := &types.Log{Topics: []types.Hash{types.HexToHash("0x1111")}}

	// Wildcard at position 0.
	tFilter := [][]types.Hash{{}}
	if !matchLog(log, nil, tFilter) {
		t.Fatal("empty topic set should be wildcard")
	}
}

func TestMatchLog_TopicShortLog(t *testing.T) {
	log := &types.Log{Topics: []types.Hash{types.HexToHash("0x1111")}}

	// Filter requires 2 topic positions but log only has 1.
	tFilter := [][]types.Hash{
		{types.HexToHash("0x1111")},
		{types.HexToHash("0x2222")},
	}
	if matchLog(log, nil, tFilter) {
		t.Fatal("should not match when log has fewer topics than filter")
	}
}

// ---------- criteriaToQuery tests ----------

func TestCriteriaToQuery_LatestFromBlock(t *testing.T) {
	mb := newMockBackend()
	latest := LatestBlockNumber
	criteria := FilterCriteria{
		FromBlock: &latest,
	}
	q := criteriaToQuery(criteria, mb)
	if q.FromBlock == nil {
		t.Fatal("expected non-nil FromBlock")
	}
	if *q.FromBlock != 42 {
		t.Fatalf("want FromBlock=42 (current), got %d", *q.FromBlock)
	}
}

func TestCriteriaToQuery_LatestToBlock(t *testing.T) {
	mb := newMockBackend()
	latest := LatestBlockNumber
	criteria := FilterCriteria{
		ToBlock: &latest,
	}
	q := criteriaToQuery(criteria, mb)
	if q.ToBlock == nil {
		t.Fatal("expected non-nil ToBlock")
	}
	if *q.ToBlock != 42 {
		t.Fatalf("want ToBlock=42 (current), got %d", *q.ToBlock)
	}
}

func TestCriteriaToQuery_Addresses(t *testing.T) {
	mb := newMockBackend()
	criteria := FilterCriteria{
		Addresses: []string{"0xaaaa", "0xbbbb"},
	}
	q := criteriaToQuery(criteria, mb)
	if len(q.Addresses) != 2 {
		t.Fatalf("want 2 addresses, got %d", len(q.Addresses))
	}
}

func TestCriteriaToQuery_Topics(t *testing.T) {
	mb := newMockBackend()
	criteria := FilterCriteria{
		Topics: [][]string{
			{"0x1111", "0x2222"},
			{},
			{"0x3333"},
		},
	}
	q := criteriaToQuery(criteria, mb)
	if len(q.Topics) != 3 {
		t.Fatalf("want 3 topic positions, got %d", len(q.Topics))
	}
	if len(q.Topics[0]) != 2 {
		t.Fatalf("want 2 topics at pos 0, got %d", len(q.Topics[0]))
	}
	if len(q.Topics[1]) != 0 {
		t.Fatalf("want 0 topics at pos 1 (wildcard), got %d", len(q.Topics[1]))
	}
	if len(q.Topics[2]) != 1 {
		t.Fatalf("want 1 topic at pos 2, got %d", len(q.Topics[2]))
	}
}

func TestCriteriaToQuery_HexFromBlock(t *testing.T) {
	mb := newMockBackend()
	bn := BlockNumber(10)
	criteria := FilterCriteria{
		FromBlock: &bn,
	}
	q := criteriaToQuery(criteria, mb)
	if q.FromBlock == nil {
		t.Fatal("expected non-nil FromBlock")
	}
	if *q.FromBlock != 10 {
		t.Fatalf("want FromBlock=10, got %d", *q.FromBlock)
	}
}

// ---------- parseCallArgs tests ----------

func TestParseCallArgs_Defaults(t *testing.T) {
	args := &CallArgs{}
	from, to, gas, value, data := parseCallArgs(args)

	if from != (types.Address{}) {
		t.Fatalf("want zero address for from, got %v", from)
	}
	if to != nil {
		t.Fatal("want nil for to")
	}
	if gas != 50_000_000 {
		t.Fatalf("want default gas 50000000, got %d", gas)
	}
	if value.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("want zero value, got %v", value)
	}
	if data != nil {
		t.Fatalf("want nil data, got %v", data)
	}
}

func TestParseCallArgs_AllFields(t *testing.T) {
	fromStr := "0x000000000000000000000000000000000000aaaa"
	toStr := "0x000000000000000000000000000000000000bbbb"
	gasStr := "0x5208"
	valueStr := "0xde0b6b3a7640000"
	dataStr := "0x12345678"

	args := &CallArgs{
		From:  &fromStr,
		To:    &toStr,
		Gas:   &gasStr,
		Value: &valueStr,
		Data:  &dataStr,
	}
	from, to, gas, value, data := parseCallArgs(args)

	if from != types.HexToAddress("0xaaaa") {
		t.Fatalf("from mismatch: %v", from)
	}
	if to == nil {
		t.Fatal("to should not be nil")
	}
	if *to != types.HexToAddress("0xbbbb") {
		t.Fatalf("to mismatch: %v", *to)
	}
	if gas != 21000 {
		t.Fatalf("want gas 21000, got %d", gas)
	}
	expected := new(big.Int)
	expected.SetString("de0b6b3a7640000", 16)
	if value.Cmp(expected) != 0 {
		t.Fatalf("value mismatch: want %v, got %v", expected, value)
	}
	if len(data) != 4 {
		t.Fatalf("want 4 bytes data, got %d", len(data))
	}
}

func TestParseCallArgs_InputOverData(t *testing.T) {
	dataStr := "0xaaaa"
	inputStr := "0xbbbb"
	args := &CallArgs{
		Data:  &dataStr,
		Input: &inputStr,
	}
	_, _, _, _, data := parseCallArgs(args)
	// Input should take precedence over data.
	if len(data) != 2 || data[0] != 0xbb || data[1] != 0xbb {
		t.Fatalf("expected input to override data, got %x", data)
	}
}

// ---------- Utility function tests ----------

func TestFromHexBytes_Edge(t *testing.T) {
	// Empty string.
	if b := fromHexBytes(""); b != nil {
		t.Fatalf("want nil for empty string, got %v", b)
	}
	// Only prefix.
	if b := fromHexBytes("0x"); b != nil {
		t.Fatalf("want nil for 0x, got %v", b)
	}
	// Odd-length hex.
	b := fromHexBytes("0xf")
	if len(b) != 1 || b[0] != 0x0f {
		t.Fatalf("want [0x0f] for 0xf, got %v", b)
	}
	// Capital prefix.
	b2 := fromHexBytes("0Xff")
	if len(b2) != 1 || b2[0] != 0xff {
		t.Fatalf("want [0xff] for 0Xff, got %v", b2)
	}
}

func TestParseHexUint64(t *testing.T) {
	tests := []struct {
		input string
		want  uint64
	}{
		{"0x0", 0},
		{"0x1", 1},
		{"0x2a", 42},
		{"0xff", 255},
		{"0x5208", 21000},
		{"ff", 255},
	}
	for _, tt := range tests {
		got := parseHexUint64(tt.input)
		if got != tt.want {
			t.Errorf("parseHexUint64(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseHexBigInt(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0x0", 0},
		{"0x1", 1},
		{"0x3b9aca00", 1000000000},
	}
	for _, tt := range tests {
		got := parseHexBigInt(tt.input)
		if got.Int64() != tt.want {
			t.Errorf("parseHexBigInt(%q) = %d, want %d", tt.input, got.Int64(), tt.want)
		}
	}
}

func TestEncodeUint64(t *testing.T) {
	if s := encodeUint64(0); s != "0x0" {
		t.Fatalf("want 0x0, got %v", s)
	}
	if s := encodeUint64(42); s != "0x2a" {
		t.Fatalf("want 0x2a, got %v", s)
	}
	if s := encodeUint64(21000); s != "0x5208" {
		t.Fatalf("want 0x5208, got %v", s)
	}
}

func TestEncodeBigInt_Nil(t *testing.T) {
	if s := encodeBigInt(nil); s != "0x0" {
		t.Fatalf("want 0x0 for nil, got %v", s)
	}
}

func TestEncodeBytes_Empty(t *testing.T) {
	if s := encodeBytes(nil); s != "0x" {
		t.Fatalf("want 0x for nil, got %v", s)
	}
	if s := encodeBytes([]byte{}); s != "0x" {
		t.Fatalf("want 0x for empty, got %v", s)
	}
}
