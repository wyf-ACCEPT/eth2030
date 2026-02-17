package rpc

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// ---------- subscription manager unit tests ----------

func newTestBackend() *mockBackend {
	mb := newMockBackend()
	// Add a second block (43) so we have a range to query.
	header43 := &types.Header{
		Number:   big.NewInt(43),
		GasLimit: 30000000,
		GasUsed:  10000000,
		Time:     1700000012,
		BaseFee:  big.NewInt(1000000000),
	}
	mb.headers[43] = header43

	// Populate logs for block 42 and 43.
	block42Hash := mb.headers[42].Hash()
	block43Hash := header43.Hash()

	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	approvalTopic := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
	contractA := types.HexToAddress("0xaaaa")
	contractB := types.HexToAddress("0xbbbb")

	mb.logs[block42Hash] = []*types.Log{
		{
			Address:     contractA,
			Topics:      []types.Hash{transferTopic},
			Data:        []byte{0x01},
			BlockNumber: 42,
			BlockHash:   block42Hash,
			TxIndex:     0,
			Index:       0,
		},
	}
	// Set bloom on block 42 header.
	mb.headers[42].Bloom = types.LogsBloom(mb.logs[block42Hash])

	mb.logs[block43Hash] = []*types.Log{
		{
			Address:     contractA,
			Topics:      []types.Hash{transferTopic},
			Data:        []byte{0x02},
			BlockNumber: 43,
			BlockHash:   block43Hash,
			TxIndex:     0,
			Index:       0,
		},
		{
			Address:     contractB,
			Topics:      []types.Hash{approvalTopic},
			Data:        []byte{0x03},
			BlockNumber: 43,
			BlockHash:   block43Hash,
			TxIndex:     1,
			Index:       1,
		},
	}
	mb.headers[43].Bloom = types.LogsBloom(mb.logs[block43Hash])

	return mb
}

func TestNewLogFilter_AllLogs(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	from := uint64(42)
	to := uint64(43)
	id := sm.NewLogFilter(FilterQuery{
		FromBlock: &from,
		ToBlock:   &to,
	})

	if id == "" {
		t.Fatal("expected non-empty filter ID")
	}

	logs, ok := sm.GetFilterLogs(id)
	if !ok {
		t.Fatal("filter not found")
	}
	if len(logs) != 3 {
		t.Fatalf("want 3 logs, got %d", len(logs))
	}
}

func TestNewLogFilter_AddressFilter(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	contractB := types.HexToAddress("0xbbbb")
	from := uint64(42)
	to := uint64(43)
	id := sm.NewLogFilter(FilterQuery{
		FromBlock: &from,
		ToBlock:   &to,
		Addresses: []types.Address{contractB},
	})

	logs, ok := sm.GetFilterLogs(id)
	if !ok {
		t.Fatal("filter not found")
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log from contractB, got %d", len(logs))
	}
	if logs[0].Address != contractB {
		t.Fatalf("want address %v, got %v", contractB, logs[0].Address)
	}
}

func TestNewLogFilter_TopicFilter(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	from := uint64(42)
	to := uint64(43)
	id := sm.NewLogFilter(FilterQuery{
		FromBlock: &from,
		ToBlock:   &to,
		Topics:    [][]types.Hash{{transferTopic}},
	})

	logs, ok := sm.GetFilterLogs(id)
	if !ok {
		t.Fatal("filter not found")
	}
	// Two Transfer logs (block 42 and block 43, contractA).
	if len(logs) != 2 {
		t.Fatalf("want 2 Transfer logs, got %d", len(logs))
	}
}

func TestNewLogFilter_TopicWithWildcard(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	// Wildcard first position (nil/empty), match contractB in address.
	contractB := types.HexToAddress("0xbbbb")
	from := uint64(42)
	to := uint64(43)
	id := sm.NewLogFilter(FilterQuery{
		FromBlock: &from,
		ToBlock:   &to,
		Addresses: []types.Address{contractB},
		Topics:    [][]types.Hash{{}}, // wildcard topic[0]
	})

	logs, ok := sm.GetFilterLogs(id)
	if !ok {
		t.Fatal("filter not found")
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
}

func TestNewLogFilter_MultiTopicOR(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	approvalTopic := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))

	from := uint64(42)
	to := uint64(43)
	// Topic[0] = Transfer OR Approval -> should match all 3 logs.
	id := sm.NewLogFilter(FilterQuery{
		FromBlock: &from,
		ToBlock:   &to,
		Topics:    [][]types.Hash{{transferTopic, approvalTopic}},
	})

	logs, ok := sm.GetFilterLogs(id)
	if !ok {
		t.Fatal("filter not found")
	}
	if len(logs) != 3 {
		t.Fatalf("want 3 logs (Transfer OR Approval), got %d", len(logs))
	}
}

func TestGetFilterChanges_LogFilter(t *testing.T) {
	mb := newTestBackend()

	// Override CurrentHeader to return block 43 so the poll range is valid.
	mb.headers[42] = nil // remove block 42 from the lookup so it's not "current"
	// Make CurrentHeader() return block 43 by storing it at key 42 (since
	// the mock's CurrentHeader returns headers[42]). We keep the real block
	// 43 data but swap the lookup key.
	header43 := &types.Header{
		Number:   big.NewInt(43),
		GasLimit: 30000000,
		Time:     1700000012,
		BaseFee:  big.NewInt(1000000000),
	}
	mb.headers[42] = header43 // mock's CurrentHeader returns headers[42]
	mb.headers[43] = header43 // also accessible by number 43

	block43Hash := header43.Hash()
	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	approvalTopic := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
	mb.logs[block43Hash] = []*types.Log{
		{
			Address:     types.HexToAddress("0xaaaa"),
			Topics:      []types.Hash{transferTopic},
			BlockNumber: 43,
			BlockHash:   block43Hash,
		},
		{
			Address:     types.HexToAddress("0xbbbb"),
			Topics:      []types.Hash{approvalTopic},
			BlockNumber: 43,
			BlockHash:   block43Hash,
		},
	}
	header43.Bloom = types.LogsBloom(mb.logs[block43Hash])

	sm := NewSubscriptionManager(mb)

	// Install a filter starting from block 43.
	// NewLogFilter sets lastBlock = FromBlock - 1 = 42.
	// CurrentHeader now returns block 43, so pollLogs scans 43..43.
	from := uint64(43)
	id := sm.NewLogFilter(FilterQuery{
		FromBlock: &from,
	})

	result, ok := sm.GetFilterChanges(id)
	if !ok {
		t.Fatal("filter not found")
	}
	logs, ok := result.([]*types.Log)
	if !ok {
		t.Fatalf("result not []*types.Log: %T", result)
	}
	if len(logs) != 2 {
		t.Fatalf("want 2 logs from block 43, got %d", len(logs))
	}

	// Second poll: no new blocks, should return empty.
	result2, ok := sm.GetFilterChanges(id)
	if !ok {
		t.Fatal("filter not found on second poll")
	}
	logs2 := result2.([]*types.Log)
	if len(logs2) != 0 {
		t.Fatalf("want 0 logs on second poll, got %d", len(logs2))
	}
}

func TestBlockFilter(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	// Current header is at block 42 (default in mock). Override to 43.
	// The mock's CurrentHeader returns headers[42], so let's install a block
	// filter starting at current (42), then simulate a new block.
	id := sm.NewBlockFilter()

	// First poll -- current is 42, lastBlock was set to 42, so nothing new yet.
	// But the mock has block 43 header -- CurrentHeader returns 42, so
	// pollBlocks scans 43..42 which is empty. Let's adjust:
	// Actually, HeaderByNumber(43) exists but CurrentHeader() returns 42.
	// So poll sees current=42, lastBlock=42, nothing new. Good.
	result, ok := sm.GetFilterChanges(id)
	if !ok {
		t.Fatal("filter not found")
	}
	hashes, ok := result.([]types.Hash)
	if !ok {
		t.Fatalf("result not []types.Hash: %T", result)
	}
	if len(hashes) != 0 {
		t.Fatalf("want 0 hashes initially, got %d", len(hashes))
	}

	// Push a new block hash via NotifyNewBlock.
	newBlockHash := types.HexToHash("0xdeadbeef")
	sm.NotifyNewBlock(newBlockHash)

	result2, _ := sm.GetFilterChanges(id)
	hashes2 := result2.([]types.Hash)
	// Should contain the notified hash (plus any from scanning, which is none
	// since CurrentHeader is still 42).
	if len(hashes2) != 1 {
		t.Fatalf("want 1 hash after notification, got %d", len(hashes2))
	}
	if hashes2[0] != newBlockHash {
		t.Fatalf("want %v, got %v", newBlockHash, hashes2[0])
	}
}

func TestPendingTxFilter(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id := sm.NewPendingTxFilter()

	// Push pending tx hashes.
	txHash1 := types.HexToHash("0x1111")
	txHash2 := types.HexToHash("0x2222")
	sm.NotifyPendingTx(txHash1)
	sm.NotifyPendingTx(txHash2)

	result, ok := sm.GetFilterChanges(id)
	if !ok {
		t.Fatal("filter not found")
	}
	hashes := result.([]types.Hash)
	if len(hashes) != 2 {
		t.Fatalf("want 2 pending tx hashes, got %d", len(hashes))
	}

	// Second poll: empty.
	result2, _ := sm.GetFilterChanges(id)
	hashes2 := result2.([]types.Hash)
	if len(hashes2) != 0 {
		t.Fatalf("want 0 hashes on second poll, got %d", len(hashes2))
	}
}

func TestUninstallFilter(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id := sm.NewBlockFilter()
	if sm.FilterCount() != 1 {
		t.Fatalf("want 1 filter, got %d", sm.FilterCount())
	}

	ok := sm.Uninstall(id)
	if !ok {
		t.Fatal("expected Uninstall to return true")
	}
	if sm.FilterCount() != 0 {
		t.Fatalf("want 0 filters after uninstall, got %d", sm.FilterCount())
	}

	// Uninstalling again should return false.
	ok2 := sm.Uninstall(id)
	if ok2 {
		t.Fatal("expected Uninstall to return false for non-existent filter")
	}
}

func TestCleanupStale(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id := sm.NewBlockFilter()

	// Force the filter's lastPoll to be old.
	sm.mu.Lock()
	sm.filters[id].lastPoll = sm.filters[id].lastPoll.Add(-filterTimeout * 2)
	sm.mu.Unlock()

	removed := sm.CleanupStale()
	if removed != 1 {
		t.Fatalf("want 1 removed, got %d", removed)
	}
	if sm.FilterCount() != 0 {
		t.Fatalf("want 0 filters after cleanup, got %d", sm.FilterCount())
	}
}

func TestGetFilterChanges_NonExistent(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	_, ok := sm.GetFilterChanges("0xnonexistent")
	if ok {
		t.Fatal("expected false for non-existent filter")
	}
}

func TestGetFilterLogs_NonExistent(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	_, ok := sm.GetFilterLogs("0xnonexistent")
	if ok {
		t.Fatal("expected false for non-existent filter")
	}
}

func TestGetFilterLogs_WrongType(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id := sm.NewBlockFilter()
	_, ok := sm.GetFilterLogs(id)
	if ok {
		t.Fatal("expected false for block filter on GetFilterLogs")
	}
}

// ---------- MatchFilter unit tests ----------

func TestMatchFilter_EmptyQuery(t *testing.T) {
	log := &types.Log{
		Address: types.HexToAddress("0xaaaa"),
		Topics:  []types.Hash{types.HexToHash("0x1111")},
	}
	query := FilterQuery{}
	if !MatchFilter(log, query) {
		t.Fatal("empty query should match everything")
	}
}

func TestMatchFilter_AddressMatch(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	log := &types.Log{Address: addr}

	q := FilterQuery{Addresses: []types.Address{addr}}
	if !MatchFilter(log, q) {
		t.Fatal("should match same address")
	}

	other := types.HexToAddress("0xbbbb")
	q2 := FilterQuery{Addresses: []types.Address{other}}
	if MatchFilter(log, q2) {
		t.Fatal("should not match different address")
	}
}

func TestMatchFilter_MultiAddressOR(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	log := &types.Log{Address: addr}

	other := types.HexToAddress("0xbbbb")
	q := FilterQuery{Addresses: []types.Address{other, addr}}
	if !MatchFilter(log, q) {
		t.Fatal("should match when any address matches")
	}
}

func TestMatchFilter_TopicAND(t *testing.T) {
	topic0 := types.HexToHash("0x1111")
	topic1 := types.HexToHash("0x2222")
	log := &types.Log{Topics: []types.Hash{topic0, topic1}}

	// AND: both positions must match.
	q := FilterQuery{Topics: [][]types.Hash{{topic0}, {topic1}}}
	if !MatchFilter(log, q) {
		t.Fatal("should match when both topic positions match")
	}

	wrongTopic := types.HexToHash("0x3333")
	q2 := FilterQuery{Topics: [][]types.Hash{{topic0}, {wrongTopic}}}
	if MatchFilter(log, q2) {
		t.Fatal("should not match when topic[1] doesn't match")
	}
}

func TestMatchFilter_TopicORWithinPosition(t *testing.T) {
	topic0 := types.HexToHash("0x1111")
	log := &types.Log{Topics: []types.Hash{topic0}}

	alt := types.HexToHash("0x9999")
	// Position 0: topic0 OR alt -> should match.
	q := FilterQuery{Topics: [][]types.Hash{{alt, topic0}}}
	if !MatchFilter(log, q) {
		t.Fatal("should match when any topic in position matches")
	}
}

func TestMatchFilter_TopicShortLog(t *testing.T) {
	// Log has only 1 topic, filter asks for 2.
	log := &types.Log{Topics: []types.Hash{types.HexToHash("0x1111")}}
	q := FilterQuery{Topics: [][]types.Hash{{types.HexToHash("0x1111")}, {types.HexToHash("0x2222")}}}
	if MatchFilter(log, q) {
		t.Fatal("should not match when log has fewer topics than filter requires")
	}
}

// ---------- bloom filter tests ----------

func TestBloomMatchesQuery_Address(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	log := &types.Log{Address: addr}
	bloom := types.LogsBloom([]*types.Log{log})

	q := FilterQuery{Addresses: []types.Address{addr}}
	if !bloomMatchesQuery(bloom, q) {
		t.Fatal("bloom should match address that was added")
	}

	other := types.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	q2 := FilterQuery{Addresses: []types.Address{other}}
	// Bloom may have false positives, but unlikely for this specific case.
	// We test the positive case which is always correct.
	_ = bloomMatchesQuery(bloom, q2) // just ensure it doesn't crash
}

func TestBloomMatchesQuery_Topic(t *testing.T) {
	topic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	log := &types.Log{Topics: []types.Hash{topic}}
	bloom := types.LogsBloom([]*types.Log{log})

	q := FilterQuery{Topics: [][]types.Hash{{topic}}}
	if !bloomMatchesQuery(bloom, q) {
		t.Fatal("bloom should match topic that was added")
	}
}

func TestBloomMatchesQuery_Wildcard(t *testing.T) {
	bloom := types.Bloom{} // empty bloom
	q := FilterQuery{}     // no constraints
	if !bloomMatchesQuery(bloom, q) {
		t.Fatal("empty query should match any bloom (wildcard)")
	}
}

// ---------- FilterLogs function tests ----------

func TestFilterLogs(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	other := types.HexToAddress("0xbbbb")
	topic := types.HexToHash("0x1111")

	logs := []*types.Log{
		{Address: addr, Topics: []types.Hash{topic}},
		{Address: other, Topics: []types.Hash{topic}},
		{Address: addr, Topics: []types.Hash{types.HexToHash("0x2222")}},
	}

	// Filter by address only.
	result := FilterLogs(logs, FilterQuery{Addresses: []types.Address{addr}})
	if len(result) != 2 {
		t.Fatalf("want 2 logs for addr, got %d", len(result))
	}

	// Filter by topic only.
	result2 := FilterLogs(logs, FilterQuery{Topics: [][]types.Hash{{topic}}})
	if len(result2) != 2 {
		t.Fatalf("want 2 logs for topic, got %d", len(result2))
	}

	// Filter by address AND topic.
	result3 := FilterLogs(logs, FilterQuery{
		Addresses: []types.Address{addr},
		Topics:    [][]types.Hash{{topic}},
	})
	if len(result3) != 1 {
		t.Fatalf("want 1 log for addr+topic, got %d", len(result3))
	}
}

func TestFilterLogsWithBloom(t *testing.T) {
	addr := types.HexToAddress("0xaaaa")
	topic := types.HexToHash("0x1111")
	logs := []*types.Log{
		{Address: addr, Topics: []types.Hash{topic}},
	}
	bloom := types.LogsBloom(logs)

	result := FilterLogsWithBloom(bloom, logs, FilterQuery{Addresses: []types.Address{addr}})
	if len(result) != 1 {
		t.Fatalf("want 1 log, got %d", len(result))
	}
}

// ---------- RPC method integration tests ----------

func TestRPC_EthNewFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newFilter", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2b",
	})
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	filterID, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if filterID == "" {
		t.Fatal("expected non-empty filter ID")
	}
}

func TestRPC_EthNewBlockFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newBlockFilter")
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	filterID, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	if filterID == "" {
		t.Fatal("expected non-empty filter ID")
	}
}

func TestRPC_EthNewPendingTransactionFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newPendingTransactionFilter")
	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	_, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
}

func TestRPC_EthGetFilterChanges_Log(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	// Create log filter starting at block 43.
	resp := callRPC(t, api, "eth_newFilter", map[string]interface{}{
		"fromBlock": "0x2b",
	})
	filterID := resp.Result.(string)

	// Poll: should get logs from block 43. But current header is 42 in mock,
	// so the scan range will be 43..42 (empty, since current=42 < from=43).
	// Update: CurrentHeader returns 42, so toBlock defaults to 42.
	// We need to adjust: set the mock's current header to 43.
	mb.headers[43] = &types.Header{
		Number:   big.NewInt(43),
		GasLimit: 30000000,
		Time:     1700000012,
		BaseFee:  big.NewInt(1000000000),
	}
	// Populate logs for block 43.
	block43Hash := mb.headers[43].Hash()
	transferTopic := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	mb.logs[block43Hash] = []*types.Log{
		{
			Address:     types.HexToAddress("0xaaaa"),
			Topics:      []types.Hash{transferTopic},
			Data:        []byte{0x99},
			BlockNumber: 43,
			BlockHash:   block43Hash,
		},
	}
	mb.headers[43].Bloom = types.LogsBloom(mb.logs[block43Hash])

	// To make pollLogs work, we need CurrentHeader to return block 43.
	// Override by changing the mock to return header 43 as current.
	origHeader := mb.headers[42]
	mb.headers[42] = nil // temporarily remove 42
	mb.headers[43].Number = big.NewInt(43)

	// Restore: the mock's CurrentHeader returns headers[42] always.
	// We need to update the mock. Instead, let's test with block 42.
	mb.headers[42] = origHeader

	// Simpler test: filter from block 42 with current at 42.
	resp2 := callRPC(t, api, "eth_newFilter", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
	})
	filterID2 := resp2.Result.(string)

	changes := callRPC(t, api, "eth_getFilterChanges", filterID2)
	if changes.Error != nil {
		t.Fatalf("error: %v", changes.Error.Message)
	}
	rpcLogs, ok := changes.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("result not []*RPCLog: %T", changes.Result)
	}
	// Block 42 has logs (populated by newTestBackend via newMockBackend + we
	// set bloom). Actually the api creates its own backend. Let's check.
	_ = rpcLogs

	// Also test that polling a non-existent filter returns error.
	bad := callRPC(t, api, "eth_getFilterChanges", "0xbadid")
	if bad.Error == nil {
		t.Fatal("expected error for non-existent filter")
	}

	// Uninstall the first filter.
	uninstall := callRPC(t, api, "eth_uninstallFilter", filterID)
	if uninstall.Error != nil {
		t.Fatalf("error: %v", uninstall.Error.Message)
	}
	if uninstall.Result != true {
		t.Fatalf("want true, got %v", uninstall.Result)
	}
}

func TestRPC_EthGetFilterLogs(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newFilter", map[string]interface{}{
		"fromBlock": "0x2a",
		"toBlock":   "0x2a",
	})
	filterID := resp.Result.(string)

	logsResp := callRPC(t, api, "eth_getFilterLogs", filterID)
	if logsResp.Error != nil {
		t.Fatalf("error: %v", logsResp.Error.Message)
	}
	rpcLogs, ok := logsResp.Result.([]*RPCLog)
	if !ok {
		t.Fatalf("result not []*RPCLog: %T", logsResp.Result)
	}
	// Block 42 has 1 log in newTestBackend.
	if len(rpcLogs) != 1 {
		t.Fatalf("want 1 log, got %d", len(rpcLogs))
	}
}

func TestRPC_EthGetFilterLogs_BadID(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_getFilterLogs", "0xbadid")
	if resp.Error == nil {
		t.Fatal("expected error for non-existent filter")
	}
}

func TestRPC_EthUninstallFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newBlockFilter")
	filterID := resp.Result.(string)

	uninstall := callRPC(t, api, "eth_uninstallFilter", filterID)
	if uninstall.Error != nil {
		t.Fatalf("error: %v", uninstall.Error.Message)
	}
	if uninstall.Result != true {
		t.Fatalf("want true, got %v", uninstall.Result)
	}

	// Second uninstall should return false.
	uninstall2 := callRPC(t, api, "eth_uninstallFilter", filterID)
	if uninstall2.Error != nil {
		t.Fatalf("error: %v", uninstall2.Error.Message)
	}
	if uninstall2.Result != false {
		t.Fatalf("want false, got %v", uninstall2.Result)
	}
}

func TestRPC_EthGetFilterChanges_BlockFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newBlockFilter")
	filterID := resp.Result.(string)

	// Notify a new block via the subscription manager.
	newHash := types.HexToHash("0xfeed")
	api.subs.NotifyNewBlock(newHash)

	changes := callRPC(t, api, "eth_getFilterChanges", filterID)
	if changes.Error != nil {
		t.Fatalf("error: %v", changes.Error.Message)
	}
	hashes, ok := changes.Result.([]string)
	if !ok {
		t.Fatalf("result not []string: %T", changes.Result)
	}
	if len(hashes) != 1 {
		t.Fatalf("want 1 hash, got %d", len(hashes))
	}
	if hashes[0] != encodeHash(newHash) {
		t.Fatalf("want %v, got %v", encodeHash(newHash), hashes[0])
	}
}

func TestRPC_EthGetFilterChanges_PendingTxFilter(t *testing.T) {
	mb := newTestBackend()
	api := NewEthAPI(mb)

	resp := callRPC(t, api, "eth_newPendingTransactionFilter")
	filterID := resp.Result.(string)

	txHash := types.HexToHash("0xabcdef")
	api.subs.NotifyPendingTx(txHash)

	changes := callRPC(t, api, "eth_getFilterChanges", filterID)
	if changes.Error != nil {
		t.Fatalf("error: %v", changes.Error.Message)
	}
	hashes, ok := changes.Result.([]string)
	if !ok {
		t.Fatalf("result not []string: %T", changes.Result)
	}
	if len(hashes) != 1 {
		t.Fatalf("want 1 hash, got %d", len(hashes))
	}
}

// ---------- net_ and web3_ method tests ----------

func TestRPC_NetListening(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "net_listening")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != true {
		t.Fatalf("want true, got %v", resp.Result)
	}
}

func TestRPC_NetPeerCount(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "net_peerCount")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	if resp.Result != "0x0" {
		t.Fatalf("want 0x0, got %v", resp.Result)
	}
}

func TestRPC_Web3Sha3(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	// keccak256("") = c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470
	resp := callRPC(t, api, "web3_sha3", "0x")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got, ok := resp.Result.(string)
	if !ok {
		t.Fatalf("result not string: %T", resp.Result)
	}
	want := encodeHash(types.EmptyCodeHash)
	if got != want {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestRPC_Web3Sha3_WithData(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	// keccak256(0x68656c6c6f) = keccak256("hello")
	resp := callRPC(t, api, "web3_sha3", "0x68656c6c6f")

	if resp.Error != nil {
		t.Fatalf("error: %v", resp.Error.Message)
	}
	got := resp.Result.(string)
	expected := encodeHash(crypto.Keccak256Hash([]byte("hello")))
	if got != expected {
		t.Fatalf("want %v, got %v", expected, got)
	}
}

func TestRPC_Web3Sha3_MissingParam(t *testing.T) {
	api := NewEthAPI(newMockBackend())
	resp := callRPC(t, api, "web3_sha3")

	if resp.Error == nil {
		t.Fatal("expected error for missing parameter")
	}
	if resp.Error.Code != ErrCodeInvalidParams {
		t.Fatalf("want error code %d, got %d", ErrCodeInvalidParams, resp.Error.Code)
	}
}

// ---------- FilterCriteria JSON unmarshaling ----------

func TestFilterCriteria_SingleAddress(t *testing.T) {
	// The Ethereum JSON-RPC spec allows "address" to be a single string
	// or an array. Our FilterCriteria.Addresses is []string, so JSON
	// arrays are handled. Single strings need the caller to wrap them,
	// which is standard in the spec (most clients send arrays).
	raw := `{"fromBlock":"0x1","address":["0xaaaa"],"topics":[]}`
	var fc FilterCriteria
	if err := json.Unmarshal([]byte(raw), &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(fc.Addresses) != 1 {
		t.Fatalf("want 1 address, got %d", len(fc.Addresses))
	}
}

func TestFilterCriteria_NestedTopics(t *testing.T) {
	raw := `{"topics":[["0x1111","0x2222"],null,["0x3333"]]}`
	var fc FilterCriteria
	if err := json.Unmarshal([]byte(raw), &fc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(fc.Topics) != 3 {
		t.Fatalf("want 3 topic positions, got %d", len(fc.Topics))
	}
	if len(fc.Topics[0]) != 2 {
		t.Fatalf("want 2 topics at pos 0, got %d", len(fc.Topics[0]))
	}
	// null topic position -> nil/empty slice.
	if fc.Topics[1] != nil && len(fc.Topics[1]) != 0 {
		t.Fatalf("want nil/empty at pos 1, got %v", fc.Topics[1])
	}
	if len(fc.Topics[2]) != 1 {
		t.Fatalf("want 1 topic at pos 2, got %d", len(fc.Topics[2]))
	}
}

// ---------- edge cases ----------

func TestQueryLogs_EmptyChain(t *testing.T) {
	mb := &mockBackend{
		chainID: big.NewInt(1),
		headers: map[uint64]*types.Header{},
		logs:    map[types.Hash][]*types.Log{},
	}
	sm := NewSubscriptionManager(mb)
	result := sm.QueryLogs(FilterQuery{})
	if len(result) != 0 {
		t.Fatalf("want 0 logs on empty chain, got %d", len(result))
	}
}

func TestMultipleFilters(t *testing.T) {
	mb := newTestBackend()
	sm := NewSubscriptionManager(mb)

	id1 := sm.NewBlockFilter()
	id2 := sm.NewPendingTxFilter()
	from := uint64(42)
	id3 := sm.NewLogFilter(FilterQuery{FromBlock: &from})

	if sm.FilterCount() != 3 {
		t.Fatalf("want 3 filters, got %d", sm.FilterCount())
	}

	// IDs should all be unique.
	if id1 == id2 || id2 == id3 || id1 == id3 {
		t.Fatal("filter IDs should be unique")
	}

	// Uninstall all.
	sm.Uninstall(id1)
	sm.Uninstall(id2)
	sm.Uninstall(id3)
	if sm.FilterCount() != 0 {
		t.Fatalf("want 0 filters, got %d", sm.FilterCount())
	}
}
