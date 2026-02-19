package rpc

import (
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewLogFilter(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	addr := types.HexToAddress("0xaaaa")
	topic := types.HexToHash("0x1111")

	id, err := fs.NewFSLogFilter(10, 100, []types.Address{addr}, [][]types.Hash{{topic}})
	if err != nil {
		t.Fatalf("NewFSLogFilter failed: %v", err)
	}
	if id.IsZero() {
		t.Fatal("expected non-zero filter ID")
	}
	if fs.FilterCount() != 1 {
		t.Fatalf("want 1 filter, got %d", fs.FilterCount())
	}
}

func TestNewBlockFilter(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	id, err := fs.NewFSBlockFilter()
	if err != nil {
		t.Fatalf("NewFSBlockFilter failed: %v", err)
	}
	if id.IsZero() {
		t.Fatal("expected non-zero filter ID")
	}
	if fs.FilterCount() != 1 {
		t.Fatalf("want 1 filter, got %d", fs.FilterCount())
	}
}

func TestAddLogMatching(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	addr := types.HexToAddress("0xaaaa")
	topic := types.HexToHash("0x1111")

	id, _ := fs.NewFSLogFilter(0, 100, []types.Address{addr}, [][]types.Hash{{topic}})

	log := &types.Log{
		Address:     addr,
		Topics:      []types.Hash{topic},
		BlockNumber: 50,
	}
	fs.AddLog(log)

	logs, err := fs.GetFilterLogs(id)
	if err != nil {
		t.Fatalf("GetFilterLogs failed: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log, got %d", len(logs))
	}
	if logs[0].Address != addr {
		t.Fatalf("want address %v, got %v", addr, logs[0].Address)
	}
}

func TestAddLogNoMatch(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	addr := types.HexToAddress("0xaaaa")
	topic := types.HexToHash("0x1111")
	otherAddr := types.HexToAddress("0xbbbb")

	id, _ := fs.NewFSLogFilter(0, 100, []types.Address{addr}, [][]types.Hash{{topic}})

	// Log from a different address.
	log := &types.Log{
		Address:     otherAddr,
		Topics:      []types.Hash{topic},
		BlockNumber: 50,
	}
	fs.AddLog(log)

	logs, err := fs.GetFilterLogs(id)
	if err != nil {
		t.Fatalf("GetFilterLogs failed: %v", err)
	}
	if len(logs) != 0 {
		t.Fatalf("want 0 logs, got %d", len(logs))
	}
}

func TestAddBlockHash(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	id, _ := fs.NewFSBlockFilter()

	h1 := types.HexToHash("0xdead")
	h2 := types.HexToHash("0xbeef")
	fs.AddBlockHash(h1)
	fs.AddBlockHash(h2)

	hashes, err := fs.GetFilterBlockHashes(id)
	if err != nil {
		t.Fatalf("GetFilterBlockHashes failed: %v", err)
	}
	if len(hashes) != 2 {
		t.Fatalf("want 2 hashes, got %d", len(hashes))
	}
	if hashes[0] != h1 || hashes[1] != h2 {
		t.Fatalf("hash mismatch: got %v, %v", hashes[0], hashes[1])
	}

	// Second poll should return empty since hashes were drained.
	hashes2, _ := fs.GetFilterBlockHashes(id)
	if len(hashes2) != 0 {
		t.Fatalf("want 0 hashes after drain, got %d", len(hashes2))
	}
}

func TestGetFilterLogs(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	// Create a log filter that matches any address.
	id, _ := fs.NewFSLogFilter(0, 100, nil, nil)

	log1 := &types.Log{Address: types.HexToAddress("0xaaaa"), BlockNumber: 10}
	log2 := &types.Log{Address: types.HexToAddress("0xbbbb"), BlockNumber: 20}
	fs.AddLog(log1)
	fs.AddLog(log2)

	logs, err := fs.GetFilterLogs(id)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("want 2 logs, got %d", len(logs))
	}

	// Logs should be drained after retrieval.
	logs2, _ := fs.GetFilterLogs(id)
	if len(logs2) != 0 {
		t.Fatalf("want 0 after drain, got %d", len(logs2))
	}
}

func TestGetFilterLogs_BlockFilter(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	id, _ := fs.NewFSBlockFilter()

	_, err := fs.GetFilterLogs(id)
	if err == nil {
		t.Fatal("expected error when calling GetFilterLogs on a block filter")
	}
}

func TestGetFilterBlockHashes_LogFilter(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	id, _ := fs.NewFSLogFilter(0, 100, nil, nil)

	_, err := fs.GetFilterBlockHashes(id)
	if err == nil {
		t.Fatal("expected error when calling GetFilterBlockHashes on a log filter")
	}
}

func TestFSUninstallFilter(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	id, _ := fs.NewFSLogFilter(0, 100, nil, nil)

	ok := fs.UninstallFilter(id)
	if !ok {
		t.Fatal("expected true for existing filter")
	}
	if fs.FilterCount() != 0 {
		t.Fatalf("want 0 filters, got %d", fs.FilterCount())
	}

	// Uninstalling non-existent filter should return false.
	ok = fs.UninstallFilter(types.Hash{0xff})
	if ok {
		t.Fatal("expected false for non-existent filter")
	}
}

func TestPruneExpired(t *testing.T) {
	config := DefaultFilterConfig()
	config.FilterTimeout = 50 * time.Millisecond
	fs := NewFilterSystem(config)

	id1, _ := fs.NewFSLogFilter(0, 100, nil, nil)
	id2, _ := fs.NewFSBlockFilter()

	// Manually set the first filter's lastPoll to be expired.
	fs.mu.Lock()
	fs.filters[id1].lastPoll = time.Now().Add(-100 * time.Millisecond)
	fs.mu.Unlock()

	fs.PruneExpired()

	if fs.FilterCount() != 1 {
		t.Fatalf("want 1 filter after prune, got %d", fs.FilterCount())
	}

	// The expired filter should be gone.
	_, err := fs.GetFilterLogs(id1)
	if err == nil {
		t.Fatal("expired filter should have been pruned")
	}

	// The fresh filter should remain.
	_, err = fs.GetFilterBlockHashes(id2)
	if err != nil {
		t.Fatalf("fresh filter should still exist: %v", err)
	}
}

func TestMaxFilters(t *testing.T) {
	config := DefaultFilterConfig()
	config.MaxFilters = 3
	fs := NewFilterSystem(config)

	fs.NewFSLogFilter(0, 100, nil, nil)
	fs.NewFSBlockFilter()
	fs.NewFSLogFilter(0, 200, nil, nil)

	_, err := fs.NewFSLogFilter(0, 300, nil, nil)
	if err == nil {
		t.Fatal("expected error when max filters reached")
	}

	_, err = fs.NewFSBlockFilter()
	if err == nil {
		t.Fatal("expected error when max filters reached")
	}
}

func TestFilterTopicMatching(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	topic0 := types.HexToHash("0x1111")
	topic1a := types.HexToHash("0x2222")
	topic1b := types.HexToHash("0x3333")

	// Filter: topic0 at position 0, (topic1a OR topic1b) at position 1.
	id, _ := fs.NewFSLogFilter(0, 100, nil, [][]types.Hash{
		{topic0},
		{topic1a, topic1b},
	})

	// Matching log: has topic0 at 0 and topic1a at 1.
	log1 := &types.Log{
		Topics:      []types.Hash{topic0, topic1a},
		BlockNumber: 10,
	}
	// Matching log: has topic0 at 0 and topic1b at 1.
	log2 := &types.Log{
		Topics:      []types.Hash{topic0, topic1b},
		BlockNumber: 20,
	}
	// Non-matching: wrong topic at position 0.
	log3 := &types.Log{
		Topics:      []types.Hash{topic1a, topic1b},
		BlockNumber: 30,
	}
	// Non-matching: only 1 topic (filter requires 2 positions).
	log4 := &types.Log{
		Topics:      []types.Hash{topic0},
		BlockNumber: 40,
	}

	fs.AddLog(log1)
	fs.AddLog(log2)
	fs.AddLog(log3)
	fs.AddLog(log4)

	logs, err := fs.GetFilterLogs(id)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("want 2 matching logs, got %d", len(logs))
	}
}

func TestFilterTopicWildcard(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	topic0 := types.HexToHash("0x1111")
	topic1 := types.HexToHash("0x2222")

	// Wildcard at position 0, specific topic at position 1.
	id, _ := fs.NewFSLogFilter(0, 100, nil, [][]types.Hash{
		{},       // wildcard
		{topic1}, // must match
	})

	log := &types.Log{
		Topics:      []types.Hash{topic0, topic1},
		BlockNumber: 10,
	}
	fs.AddLog(log)

	logs, err := fs.GetFilterLogs(id)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("want 1 log with wildcard+specific match, got %d", len(logs))
	}
}

func TestFilterBlockRange(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	// Filter only blocks 50-60.
	id, _ := fs.NewFSLogFilter(50, 60, nil, nil)

	logInRange := &types.Log{Address: types.HexToAddress("0xaaaa"), BlockNumber: 55}
	logBelow := &types.Log{Address: types.HexToAddress("0xaaaa"), BlockNumber: 10}
	logAbove := &types.Log{Address: types.HexToAddress("0xaaaa"), BlockNumber: 100}

	fs.AddLog(logInRange)
	fs.AddLog(logBelow)
	fs.AddLog(logAbove)

	logs, _ := fs.GetFilterLogs(id)
	if len(logs) != 1 {
		t.Fatalf("want 1 log in range, got %d", len(logs))
	}
}

func TestAddLogNil(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())
	// Should not panic.
	fs.AddLog(nil)
}

func TestGetFilterLogs_NotFound(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	_, err := fs.GetFilterLogs(types.Hash{0xff})
	if err == nil {
		t.Fatal("expected error for non-existent filter")
	}
}

func TestGetFilterBlockHashes_NotFound(t *testing.T) {
	fs := NewFilterSystem(DefaultFilterConfig())

	_, err := fs.GetFilterBlockHashes(types.Hash{0xff})
	if err == nil {
		t.Fatal("expected error for non-existent filter")
	}
}
