package types

import (
	"testing"
)

func makeTestLogs(addr Address, topics []Hash, data []byte) []*Log {
	return []*Log{{
		Address: addr,
		Topics:  topics,
		Data:    data,
	}}
}

func makeTestReceipts() []*Receipt {
	addr1 := HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := HexToAddress("0x2222222222222222222222222222222222222222")
	topic1 := HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	topic2 := HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	topic3 := HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")

	return []*Receipt{
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 21000,
			Logs: []*Log{
				{Address: addr1, Topics: []Hash{topic1, topic2}, Data: []byte{0x01}},
				{Address: addr1, Topics: []Hash{topic1}, Data: []byte{0x02}},
			},
		},
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 42000,
			Logs: []*Log{
				{Address: addr2, Topics: []Hash{topic2, topic3}, Data: []byte{0x03}},
			},
		},
		{
			Status:            ReceiptStatusSuccessful,
			CumulativeGasUsed: 63000,
			Logs:              []*Log{},
		},
	}
}

func TestBuildLogIndex(t *testing.T) {
	receipts := makeTestReceipts()
	idx := BuildLogIndex(receipts, 100, 50)

	if idx.BlockNumber != 100 {
		t.Fatalf("expected block number 100, got %d", idx.BlockNumber)
	}
	if idx.FirstGlobalIndex != 50 {
		t.Fatalf("expected first global index 50, got %d", idx.FirstGlobalIndex)
	}
	if len(idx.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(idx.Entries))
	}

	// Verify entry 0: tx 0, log 0.
	e := idx.Entries[0]
	if e.BlockNumber != 100 || e.TxIndex != 0 || e.LogIndex != 0 || e.GlobalIndex != 50 {
		t.Errorf("entry 0 mismatch: %+v", e)
	}

	// Verify entry 1: tx 0, log 1.
	e = idx.Entries[1]
	if e.BlockNumber != 100 || e.TxIndex != 0 || e.LogIndex != 1 || e.GlobalIndex != 51 {
		t.Errorf("entry 1 mismatch: %+v", e)
	}

	// Verify entry 2: tx 1, log 0.
	e = idx.Entries[2]
	if e.BlockNumber != 100 || e.TxIndex != 1 || e.LogIndex != 0 || e.GlobalIndex != 52 {
		t.Errorf("entry 2 mismatch: %+v", e)
	}
}

func TestBuildLogIndexEmpty(t *testing.T) {
	idx := BuildLogIndex(nil, 0, 0)
	if len(idx.Entries) != 0 {
		t.Fatalf("expected 0 entries for nil receipts, got %d", len(idx.Entries))
	}

	idx = BuildLogIndex([]*Receipt{{Logs: []*Log{}}}, 5, 10)
	if len(idx.Entries) != 0 {
		t.Fatalf("expected 0 entries for empty logs, got %d", len(idx.Entries))
	}
}

func TestQueryLogsByTopics(t *testing.T) {
	receipts := makeTestReceipts()
	idx := BuildLogIndex(receipts, 100, 0)

	topic1 := HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	topic2 := HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Match topic[0] == topic1.
	results := QueryLogsByTopics(idx, receipts, [][]Hash{{topic1}})
	if len(results) != 2 {
		t.Fatalf("expected 2 results matching topic1 at position 0, got %d", len(results))
	}

	// Match topic[0] == topic1 AND topic[1] == topic2.
	results = QueryLogsByTopics(idx, receipts, [][]Hash{{topic1}, {topic2}})
	if len(results) != 1 {
		t.Fatalf("expected 1 result matching topic1+topic2, got %d", len(results))
	}

	// Match topic[0] == topic2 (only in receipt 1).
	results = QueryLogsByTopics(idx, receipts, [][]Hash{{topic2}})
	if len(results) != 1 {
		t.Fatalf("expected 1 result matching topic2 at position 0, got %d", len(results))
	}

	// Match with OR in position 0.
	results = QueryLogsByTopics(idx, receipts, [][]Hash{{topic1, topic2}})
	if len(results) != 3 {
		t.Fatalf("expected 3 results matching topic1 OR topic2, got %d", len(results))
	}

	// Empty topic set at position 0 matches everything.
	results = QueryLogsByTopics(idx, receipts, [][]Hash{{}})
	if len(results) != 3 {
		t.Fatalf("expected 3 results for empty topic set, got %d", len(results))
	}

	// Nil topics returns nil.
	results = QueryLogsByTopics(idx, receipts, nil)
	if results != nil {
		t.Fatalf("expected nil for nil topics, got %v", results)
	}
}

func TestQueryLogsByAddress(t *testing.T) {
	receipts := makeTestReceipts()
	idx := BuildLogIndex(receipts, 100, 0)

	addr1 := HexToAddress("0x1111111111111111111111111111111111111111")
	addr2 := HexToAddress("0x2222222222222222222222222222222222222222")
	addr3 := HexToAddress("0x3333333333333333333333333333333333333333")

	results := QueryLogsByAddress(idx, receipts, []Address{addr1})
	if len(results) != 2 {
		t.Fatalf("expected 2 logs from addr1, got %d", len(results))
	}

	results = QueryLogsByAddress(idx, receipts, []Address{addr2})
	if len(results) != 1 {
		t.Fatalf("expected 1 log from addr2, got %d", len(results))
	}

	results = QueryLogsByAddress(idx, receipts, []Address{addr1, addr2})
	if len(results) != 3 {
		t.Fatalf("expected 3 logs from addr1+addr2, got %d", len(results))
	}

	results = QueryLogsByAddress(idx, receipts, []Address{addr3})
	if len(results) != 0 {
		t.Fatalf("expected 0 logs from addr3, got %d", len(results))
	}

	results = QueryLogsByAddress(idx, receipts, nil)
	if results != nil {
		t.Fatalf("expected nil for nil addrs, got %v", results)
	}
}

func TestComputeLogIndexRoot(t *testing.T) {
	receipts := makeTestReceipts()
	idx := BuildLogIndex(receipts, 100, 0)

	root := ComputeLogIndexRoot(idx)
	if root == (Hash{}) {
		t.Fatal("expected non-zero root")
	}

	// Same index should produce the same root.
	root2 := ComputeLogIndexRoot(idx)
	if root != root2 {
		t.Fatal("root mismatch for same index")
	}

	// Different index should produce a different root.
	idx2 := BuildLogIndex(receipts, 101, 0)
	root3 := ComputeLogIndexRoot(idx2)
	if root == root3 {
		t.Fatal("expected different root for different block number")
	}

	// Empty index.
	emptyIdx := BuildLogIndex(nil, 0, 0)
	emptyRoot := ComputeLogIndexRoot(emptyIdx)
	if emptyRoot != (Hash{}) {
		t.Fatal("expected zero root for empty index")
	}
}

func TestComputeLogIndexRootSingleEntry(t *testing.T) {
	receipts := []*Receipt{{
		Logs: []*Log{{Address: HexToAddress("0x01")}},
	}}
	idx := BuildLogIndex(receipts, 1, 0)
	root := ComputeLogIndexRoot(idx)
	if root == (Hash{}) {
		t.Fatal("expected non-zero root for single entry")
	}
}

func TestFilterMap(t *testing.T) {
	fm := NewFilterMap(100, 200)

	fm.Set(0x1234, 5)
	if !fm.Test(0x1234, 5) {
		t.Fatal("expected bit 5 to be set for key 0x1234")
	}
	if fm.Test(0x1234, 6) {
		t.Fatal("expected bit 6 to not be set for key 0x1234")
	}
	if fm.Test(0x5678, 5) {
		t.Fatal("expected bit 5 to not be set for key 0x5678")
	}

	// Out of range.
	fm.Set(0xaaaa, 300)
	if fm.Test(0xaaaa, 300) {
		t.Fatal("expected out-of-range bit to not be set")
	}
}

func TestFilterMapMultipleBits(t *testing.T) {
	fm := NewFilterMap(0, 255)

	fm.Set(42, 0)
	fm.Set(42, 7)
	fm.Set(42, 255)

	if !fm.Test(42, 0) {
		t.Fatal("bit 0 should be set")
	}
	if !fm.Test(42, 7) {
		t.Fatal("bit 7 should be set")
	}
	if !fm.Test(42, 255) {
		t.Fatal("bit 255 should be set")
	}
	if fm.Test(42, 128) {
		t.Fatal("bit 128 should not be set")
	}
}
