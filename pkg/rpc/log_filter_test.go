package rpc

import (
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewLogFilterEngine(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if engine.ActiveFilters() != 0 {
		t.Fatalf("expected 0 filters, got %d", engine.ActiveFilters())
	}
	if engine.LogCount() != 0 {
		t.Fatalf("expected 0 logs, got %d", engine.LogCount())
	}
}

func TestCreateAndDeleteFilter(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	filter := LogFilterSpec{
		FromBlock: 100,
		ToBlock:   200,
		Addresses: []types.Address{types.HexToAddress("0xaaaa")},
	}

	id, err := engine.CreateFilter(filter)
	if err != nil {
		t.Fatalf("create filter failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty filter ID")
	}
	if engine.ActiveFilters() != 1 {
		t.Fatalf("expected 1 filter, got %d", engine.ActiveFilters())
	}

	err = engine.DeleteFilter(id)
	if err != nil {
		t.Fatalf("delete filter failed: %v", err)
	}
	if engine.ActiveFilters() != 0 {
		t.Fatalf("expected 0 filters after delete, got %d", engine.ActiveFilters())
	}
}

func TestDeleteNonExistent(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())
	err := engine.DeleteFilter("nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting non-existent filter")
	}
}

func TestCreateFilterTooManyTopics(t *testing.T) {
	config := LogFilterConfig{MaxTopics: 2, MaxBlocks: 10000, MaxLogs: 100000}
	engine := NewLogFilterEngine(config)

	filter := LogFilterSpec{
		Topics: [][]types.Hash{{}, {}, {}}, // 3 positions, max is 2
	}

	_, err := engine.CreateFilter(filter)
	if err == nil {
		t.Fatal("expected error for too many topics")
	}
}

func TestCreateFilterBlockRangeExceeded(t *testing.T) {
	config := LogFilterConfig{MaxTopics: 4, MaxBlocks: 100, MaxLogs: 100000}
	engine := NewLogFilterEngine(config)

	filter := LogFilterSpec{
		FromBlock: 0,
		ToBlock:   200, // range of 200 exceeds max of 100
	}

	_, err := engine.CreateFilter(filter)
	if err == nil {
		t.Fatal("expected error for block range exceeding maximum")
	}
}

func TestCreateFilterInvalidRange(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	filter := LogFilterSpec{
		FromBlock: 500,
		ToBlock:   100, // from > to
	}

	_, err := engine.CreateFilter(filter)
	if err == nil {
		t.Fatal("expected error for fromBlock > toBlock")
	}
}

func TestAddLogAndCount(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	log := FilteredLog{
		Address:     types.HexToAddress("0x1111"),
		BlockNumber: 42,
		Data:        []byte{0x01, 0x02},
	}

	engine.AddLog(log)
	if engine.LogCount() != 1 {
		t.Fatalf("expected 1 log, got %d", engine.LogCount())
	}

	engine.AddLog(log)
	if engine.LogCount() != 2 {
		t.Fatalf("expected 2 logs, got %d", engine.LogCount())
	}
}

func TestAddLogMaxCapacity(t *testing.T) {
	config := LogFilterConfig{MaxTopics: 4, MaxBlocks: 10000, MaxLogs: 3}
	engine := NewLogFilterEngine(config)

	for i := 0; i < 5; i++ {
		engine.AddLog(FilteredLog{BlockNumber: uint64(i)})
	}

	if engine.LogCount() != 3 {
		t.Fatalf("expected 3 logs (capped), got %d", engine.LogCount())
	}
}

func TestGetFilterLogsEngine(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	addr := types.HexToAddress("0xaaaa")
	other := types.HexToAddress("0xbbbb")

	engine.AddLog(FilteredLog{Address: addr, BlockNumber: 10})
	engine.AddLog(FilteredLog{Address: other, BlockNumber: 11})
	engine.AddLog(FilteredLog{Address: addr, BlockNumber: 12})

	filter := LogFilterSpec{
		FromBlock: 0,
		ToBlock:   100,
		Addresses: []types.Address{addr},
	}

	id, err := engine.CreateFilter(filter)
	if err != nil {
		t.Fatalf("create filter failed: %v", err)
	}

	logs := engine.GetFilterLogs(id)
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs, got %d", len(logs))
	}
	for _, log := range logs {
		if log.Address != addr {
			t.Fatalf("expected address %s, got %s", addr, log.Address)
		}
	}
}

func TestGetFilterLogsNonExistent(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())
	logs := engine.GetFilterLogs("nonexistent")
	if logs != nil {
		t.Fatal("expected nil for non-existent filter")
	}
}

func TestMatchesFilterNoConstraints(t *testing.T) {
	log := FilteredLog{
		Address:     types.HexToAddress("0x1111"),
		BlockNumber: 50,
	}
	filter := LogFilterSpec{} // no constraints

	if !MatchesFilter(log, filter) {
		t.Fatal("expected match with no constraints")
	}
}

func TestMatchesFilterBlockRange(t *testing.T) {
	log := FilteredLog{BlockNumber: 50}

	// In range.
	if !MatchesFilter(log, LogFilterSpec{FromBlock: 10, ToBlock: 100}) {
		t.Fatal("expected match within range")
	}

	// Below range.
	if MatchesFilter(log, LogFilterSpec{FromBlock: 60, ToBlock: 100}) {
		t.Fatal("expected no match below range")
	}

	// Above range.
	if MatchesFilter(log, LogFilterSpec{FromBlock: 10, ToBlock: 40}) {
		t.Fatal("expected no match above range")
	}

	// ToBlock=0 means unbounded.
	if !MatchesFilter(log, LogFilterSpec{FromBlock: 10, ToBlock: 0}) {
		t.Fatal("expected match with unbounded toBlock")
	}
}

func TestMatchesFilterAddresses(t *testing.T) {
	addr1 := types.HexToAddress("0x1111")
	addr2 := types.HexToAddress("0x2222")
	addr3 := types.HexToAddress("0x3333")

	log := FilteredLog{Address: addr1}

	// Match single address.
	if !MatchesFilter(log, LogFilterSpec{Addresses: []types.Address{addr1}}) {
		t.Fatal("expected match on matching address")
	}

	// No match.
	if MatchesFilter(log, LogFilterSpec{Addresses: []types.Address{addr2}}) {
		t.Fatal("expected no match on different address")
	}

	// Match one of multiple.
	if !MatchesFilter(log, LogFilterSpec{Addresses: []types.Address{addr2, addr1, addr3}}) {
		t.Fatal("expected match on one of multiple addresses")
	}
}

func TestMatchesFilterTopics(t *testing.T) {
	topic0 := types.HexToHash("0xaaaa")
	topic1 := types.HexToHash("0xbbbb")
	other := types.HexToHash("0xcccc")

	log := FilteredLog{
		Topics: []types.Hash{topic0, topic1},
	}

	// Match exact topics.
	filter := LogFilterSpec{
		Topics: [][]types.Hash{{topic0}, {topic1}},
	}
	if !MatchesFilter(log, filter) {
		t.Fatal("expected match on exact topics")
	}

	// Mismatch on first position.
	filter2 := LogFilterSpec{
		Topics: [][]types.Hash{{other}, {topic1}},
	}
	if MatchesFilter(log, filter2) {
		t.Fatal("expected no match on wrong first topic")
	}

	// Wildcard first position.
	filter3 := LogFilterSpec{
		Topics: [][]types.Hash{{}, {topic1}},
	}
	if !MatchesFilter(log, filter3) {
		t.Fatal("expected match with wildcard first position")
	}

	// OR within position.
	filter4 := LogFilterSpec{
		Topics: [][]types.Hash{{other, topic0}},
	}
	if !MatchesFilter(log, filter4) {
		t.Fatal("expected match with OR within position")
	}
}

func TestMatchesFilterTopicTooFew(t *testing.T) {
	log := FilteredLog{
		Topics: []types.Hash{types.HexToHash("0x01")},
	}

	// Filter expects 2 topic positions but log only has 1.
	filter := LogFilterSpec{
		Topics: [][]types.Hash{{types.HexToHash("0x01")}, {types.HexToHash("0x02")}},
	}
	if MatchesFilter(log, filter) {
		t.Fatal("expected no match when log has fewer topics than filter requires")
	}
}

func TestLogFilterEngineMultipleFilters(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	addr1 := types.HexToAddress("0x1111")
	addr2 := types.HexToAddress("0x2222")

	engine.AddLog(FilteredLog{Address: addr1, BlockNumber: 10})
	engine.AddLog(FilteredLog{Address: addr2, BlockNumber: 11})
	engine.AddLog(FilteredLog{Address: addr1, BlockNumber: 12})

	id1, _ := engine.CreateFilter(LogFilterSpec{Addresses: []types.Address{addr1}})
	id2, _ := engine.CreateFilter(LogFilterSpec{Addresses: []types.Address{addr2}})

	logs1 := engine.GetFilterLogs(id1)
	logs2 := engine.GetFilterLogs(id2)

	if len(logs1) != 2 {
		t.Fatalf("expected 2 logs for filter 1, got %d", len(logs1))
	}
	if len(logs2) != 1 {
		t.Fatalf("expected 1 log for filter 2, got %d", len(logs2))
	}
}

func TestLogFilterEngineConcurrency(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	var wg sync.WaitGroup

	// Concurrent filter creation.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			var addr types.Address
			addr[0] = byte(n)
			engine.CreateFilter(LogFilterSpec{
				Addresses: []types.Address{addr},
			})
		}(i)
	}

	// Concurrent log addition.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			engine.AddLog(FilteredLog{BlockNumber: uint64(n)})
		}(i)
	}

	wg.Wait()

	if engine.ActiveFilters() != 20 {
		t.Fatalf("expected 20 filters, got %d", engine.ActiveFilters())
	}
	if engine.LogCount() != 50 {
		t.Fatalf("expected 50 logs, got %d", engine.LogCount())
	}
}

func TestDefaultLogFilterConfig(t *testing.T) {
	config := DefaultLogFilterConfig()
	if config.MaxTopics != 4 {
		t.Fatalf("expected MaxTopics 4, got %d", config.MaxTopics)
	}
	if config.MaxBlocks != 10000 {
		t.Fatalf("expected MaxBlocks 10000, got %d", config.MaxBlocks)
	}
	if config.MaxLogs != 100000 {
		t.Fatalf("expected MaxLogs 100000, got %d", config.MaxLogs)
	}
}

func TestFilterWithTopicAndAddress(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())
	addr := types.HexToAddress("0x5555")
	topic := types.HexToHash("0xdead")

	engine.AddLog(FilteredLog{Address: addr, Topics: []types.Hash{topic}, BlockNumber: 1})
	engine.AddLog(FilteredLog{Address: addr, Topics: []types.Hash{types.HexToHash("0xbeef")}, BlockNumber: 2})
	engine.AddLog(FilteredLog{Address: types.HexToAddress("0x6666"), Topics: []types.Hash{topic}, BlockNumber: 3})

	id, _ := engine.CreateFilter(LogFilterSpec{
		Addresses: []types.Address{addr},
		Topics:    [][]types.Hash{{topic}},
	})

	logs := engine.GetFilterLogs(id)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log matching both address and topic, got %d", len(logs))
	}
	if logs[0].BlockNumber != 1 {
		t.Fatalf("expected block 1, got %d", logs[0].BlockNumber)
	}
}

func TestFilterBlockRangeZeroToBlock(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())

	engine.AddLog(FilteredLog{BlockNumber: 100})
	engine.AddLog(FilteredLog{BlockNumber: 200})
	engine.AddLog(FilteredLog{BlockNumber: 300})

	// ToBlock=0 means no upper bound.
	id, _ := engine.CreateFilter(LogFilterSpec{FromBlock: 150})

	logs := engine.GetFilterLogs(id)
	if len(logs) != 2 {
		t.Fatalf("expected 2 logs (200, 300), got %d", len(logs))
	}
}

func TestCreateMultipleFiltersUniqueIDs(t *testing.T) {
	engine := NewLogFilterEngine(DefaultLogFilterConfig())
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := engine.CreateFilter(LogFilterSpec{})
		if err != nil {
			t.Fatalf("create filter failed: %v", err)
		}
		if ids[id] {
			t.Fatalf("duplicate filter ID: %s", id)
		}
		ids[id] = true
	}
}
