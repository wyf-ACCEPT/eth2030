package trie

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// makeNodeData returns deterministic node data and its keccak256 hash.
func makeNodeData(seed byte) (types.Hash, []byte) {
	data := []byte{seed, seed + 1, seed + 2, seed + 3}
	hash := types.BytesToHash(crypto.Keccak256(data))
	return hash, data
}

func newTestScheduler() *SyncScheduler {
	db := NewNodeDatabase(nil)
	return NewSyncScheduler(db)
}

func TestSyncScheduler_NewScheduler(t *testing.T) {
	s := newTestScheduler()
	if s.Pending() != 0 {
		t.Fatalf("expected 0 pending, got %d", s.Pending())
	}
	if !s.IsDone() {
		t.Fatal("new scheduler should be done")
	}
}

func TestSyncScheduler_AddRoot(t *testing.T) {
	s := newTestScheduler()
	root := types.BytesToHash([]byte{0x01, 0x02, 0x03})
	s.AddRoot(root)

	if s.Pending() != 1 {
		t.Fatalf("expected 1 pending after AddRoot, got %d", s.Pending())
	}
	if s.IsDone() {
		t.Fatal("should not be done with pending requests")
	}

	stats := s.Stats()
	if stats.TotalRequested != 1 {
		t.Fatalf("expected TotalRequested=1, got %d", stats.TotalRequested)
	}
}

func TestSyncScheduler_AddHash_ZeroHash(t *testing.T) {
	s := newTestScheduler()
	s.AddHash(types.Hash{}, nil, 0, false)
	if s.Pending() != 0 {
		t.Fatal("zero hash should be rejected")
	}
}

func TestSyncScheduler_Deduplication_Pending(t *testing.T) {
	s := newTestScheduler()
	hash := types.BytesToHash([]byte{0xAA})

	s.AddHash(hash, nil, 0, false)
	s.AddHash(hash, nil, 0, false)

	if s.Pending() != 1 {
		t.Fatalf("expected 1 pending (dedup), got %d", s.Pending())
	}
	stats := s.Stats()
	if stats.TotalDuplicate != 1 {
		t.Fatalf("expected 1 duplicate, got %d", stats.TotalDuplicate)
	}
}

func TestSyncScheduler_Deduplication_Done(t *testing.T) {
	s := newTestScheduler()
	hash, data := makeNodeData(0x10)

	s.AddHash(hash, nil, 0, false)
	reqs := s.PopRequests(10)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if err := s.NodeArrived(hash, data); err != nil {
		t.Fatalf("NodeArrived failed: %v", err)
	}

	// Adding a done hash should be deduplicated.
	s.AddHash(hash, nil, 0, false)
	if s.Pending() != 0 {
		t.Fatal("expected 0 pending after adding done hash")
	}
	stats := s.Stats()
	if stats.TotalDuplicate != 1 {
		t.Fatalf("expected 1 duplicate, got %d", stats.TotalDuplicate)
	}
}

func TestSyncScheduler_Deduplication_Inflight(t *testing.T) {
	s := newTestScheduler()
	hash := types.BytesToHash([]byte{0xBB})

	s.AddHash(hash, nil, 0, false)
	s.PopRequests(10) // marks inflight

	// Adding again while inflight should be deduplicated.
	s.AddHash(hash, nil, 0, false)
	stats := s.Stats()
	if stats.TotalDuplicate != 1 {
		t.Fatalf("expected 1 duplicate for inflight, got %d", stats.TotalDuplicate)
	}
}

func TestSyncScheduler_Deduplication_AlreadyInDB(t *testing.T) {
	db := NewNodeDatabase(nil)
	hash, data := makeNodeData(0x20)
	db.InsertNode(hash, data)

	s := NewSyncScheduler(db)
	s.AddHash(hash, nil, 0, false)

	if s.Pending() != 0 {
		t.Fatal("node already in DB should not become pending")
	}
	stats := s.Stats()
	if stats.TotalDuplicate != 1 {
		t.Fatalf("expected 1 duplicate, got %d", stats.TotalDuplicate)
	}
}

func TestSyncScheduler_PriorityOrdering(t *testing.T) {
	s := newTestScheduler()

	// Add nodes at various depths so they get different priorities.
	deepHash := types.BytesToHash([]byte{0x01})
	medHash := types.BytesToHash([]byte{0x02})
	shallowHash := types.BytesToHash([]byte{0x03})
	rootHash := types.BytesToHash([]byte{0x04})

	s.AddHash(deepHash, nil, 20, false)    // PriorityDeep (3)
	s.AddHash(medHash, nil, 10, false)      // PriorityMedium (2)
	s.AddHash(shallowHash, nil, 2, false)   // PriorityShallow (1)
	s.AddHash(rootHash, nil, 0, false)      // PriorityRoot (0)

	reqs := s.PopRequests(10)
	if len(reqs) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(reqs))
	}

	// Verify priority ordering: root (0) -> shallow (1) -> medium (2) -> deep (3).
	expectedPriorities := []SyncPriority{PriorityRoot, PriorityShallow, PriorityMedium, PriorityDeep}
	for i, req := range reqs {
		if req.Priority != expectedPriorities[i] {
			t.Fatalf("request %d: expected priority %d, got %d", i, expectedPriorities[i], req.Priority)
		}
	}
}

func TestSyncScheduler_PopRequests_MaxCount(t *testing.T) {
	s := newTestScheduler()

	for i := byte(1); i <= 10; i++ {
		s.AddHash(types.BytesToHash([]byte{i}), nil, 5, false)
	}

	reqs := s.PopRequests(3)
	if len(reqs) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(reqs))
	}
	if s.Pending() != 7 {
		t.Fatalf("expected 7 remaining pending, got %d", s.Pending())
	}

	stats := s.Stats()
	if stats.Inflight != 3 {
		t.Fatalf("expected 3 inflight, got %d", stats.Inflight)
	}
}

func TestSyncScheduler_NodeArrived(t *testing.T) {
	s := newTestScheduler()
	hash, data := makeNodeData(0x30)

	s.AddHash(hash, nil, 0, false)
	s.PopRequests(10) // mark inflight

	if err := s.NodeArrived(hash, data); err != nil {
		t.Fatalf("NodeArrived failed: %v", err)
	}

	stats := s.Stats()
	if stats.Done != 1 {
		t.Fatalf("expected 1 done, got %d", stats.Done)
	}
	if stats.Inflight != 0 {
		t.Fatalf("expected 0 inflight, got %d", stats.Inflight)
	}
	if stats.TotalReceived != 1 {
		t.Fatalf("expected TotalReceived=1, got %d", stats.TotalReceived)
	}
	if !s.IsDone() {
		t.Fatal("expected IsDone after all nodes received")
	}
}

func TestSyncScheduler_NodeArrived_HashMismatch(t *testing.T) {
	s := newTestScheduler()
	hash := types.BytesToHash([]byte{0xAA})

	s.AddHash(hash, nil, 0, false)
	s.PopRequests(10)

	// Send wrong data.
	err := s.NodeArrived(hash, []byte{0xFF, 0xFF})
	if err == nil {
		t.Fatal("expected error for hash mismatch")
	}
}

func TestSyncScheduler_NodeFailed_Retry(t *testing.T) {
	s := newTestScheduler()
	hash := types.BytesToHash([]byte{0xCC})

	s.AddHash(hash, nil, 0, false)
	s.PopRequests(10) // mark inflight

	s.NodeFailed(hash)

	stats := s.Stats()
	if stats.Inflight != 0 {
		t.Fatalf("expected 0 inflight after failure, got %d", stats.Inflight)
	}
	if stats.Pending != 1 {
		t.Fatalf("expected 1 pending after retry, got %d", stats.Pending)
	}

	// Should be able to pop it again.
	reqs := s.PopRequests(10)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 retried request, got %d", len(reqs))
	}
}

func TestSyncScheduler_NodeFailed_NotInflight(t *testing.T) {
	s := newTestScheduler()
	// Failing a hash that is not inflight should be a no-op.
	s.NodeFailed(types.BytesToHash([]byte{0xDD}))
	if s.Pending() != 0 {
		t.Fatal("expected no pending for non-inflight failure")
	}
}

func TestSyncScheduler_HealRequests(t *testing.T) {
	s := newTestScheduler()
	hash := types.BytesToHash([]byte{0xEE})

	s.AddHealHash(hash, []byte{0x01, 0x02}, 10)

	stats := s.Stats()
	if stats.HealRequested != 1 {
		t.Fatalf("expected 1 heal requested, got %d", stats.HealRequested)
	}

	reqs := s.PopRequests(10)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Priority != PriorityHeal {
		t.Fatalf("expected PriorityHeal, got %d", reqs[0].Priority)
	}
	if !reqs[0].IsHeal {
		t.Fatal("expected IsHeal=true")
	}
}

func TestSyncScheduler_Reset(t *testing.T) {
	s := newTestScheduler()

	for i := byte(1); i <= 5; i++ {
		s.AddHash(types.BytesToHash([]byte{i}), nil, 5, false)
	}
	s.PopRequests(2) // 2 inflight, 3 pending

	s.Reset()

	stats := s.Stats()
	if stats.Pending != 0 || stats.Inflight != 0 || stats.Done != 0 {
		t.Fatalf("expected all zeros after reset, got %+v", stats)
	}
	if stats.TotalRequested != 0 || stats.TotalReceived != 0 || stats.HealRequested != 0 {
		t.Fatalf("expected counters reset, got %+v", stats)
	}
	if !s.IsDone() {
		t.Fatal("expected IsDone after reset")
	}
}

func TestSyncScheduler_FullWorkflow(t *testing.T) {
	s := newTestScheduler()

	// Create 3 nodes with real hash-matching data.
	nodes := make([]nodeInfo, 3)
	for i := range nodes {
		h, d := makeNodeData(byte(i * 10))
		nodes[i] = nodeInfo{h, d}
	}

	// Schedule all 3.
	for _, n := range nodes {
		s.AddHash(n.hash, nil, 5, false)
	}
	if s.Pending() != 3 {
		t.Fatalf("expected 3 pending, got %d", s.Pending())
	}

	// Pop 2.
	reqs := s.PopRequests(2)
	if len(reqs) != 2 {
		t.Fatalf("expected 2 popped, got %d", len(reqs))
	}

	// Complete 1, fail 1.
	if err := s.NodeArrived(reqs[0].Hash, findData(nodes, reqs[0].Hash)); err != nil {
		t.Fatalf("NodeArrived failed: %v", err)
	}
	s.NodeFailed(reqs[1].Hash)

	stats := s.Stats()
	if stats.Done != 1 {
		t.Fatalf("expected 1 done, got %d", stats.Done)
	}
	// 1 pending original + 1 retried = 2 pending.
	if stats.Pending != 2 {
		t.Fatalf("expected 2 pending, got %d", stats.Pending)
	}

	// Pop remaining and complete all.
	reqs = s.PopRequests(10)
	for _, req := range reqs {
		if err := s.NodeArrived(req.Hash, findData(nodes, req.Hash)); err != nil {
			t.Fatalf("NodeArrived failed: %v", err)
		}
	}

	if !s.IsDone() {
		t.Fatal("expected IsDone after completing all nodes")
	}
}

type nodeInfo struct {
	hash types.Hash
	data []byte
}

func findData(nodes []nodeInfo, hash types.Hash) []byte {
	for _, n := range nodes {
		if n.hash == hash {
			return n.data
		}
	}
	return nil
}

func TestPriorityForDepth(t *testing.T) {
	tests := []struct {
		depth    int
		expected SyncPriority
	}{
		{0, PriorityRoot},
		{1, PriorityShallow},
		{4, PriorityShallow},
		{5, PriorityMedium},
		{16, PriorityMedium},
		{17, PriorityDeep},
		{100, PriorityDeep},
	}
	for _, tt := range tests {
		got := priorityForDepth(tt.depth)
		if got != tt.expected {
			t.Errorf("priorityForDepth(%d) = %d, want %d", tt.depth, got, tt.expected)
		}
	}
}

func TestSyncScheduler_PopRequests_FiltersDone(t *testing.T) {
	s := newTestScheduler()
	hash1, data1 := makeNodeData(0x40)
	hash2 := types.BytesToHash([]byte{0x50})

	// Add both to pending at same priority.
	s.AddHash(hash1, nil, 5, false)
	s.AddHash(hash2, nil, 5, false)

	// Directly insert hash1 into the DB to simulate it arriving out of band.
	s.nodeDB.InsertNode(hash1, data1)
	s.mu.Lock()
	s.done[hash1] = struct{}{}
	s.mu.Unlock()

	reqs := s.PopRequests(10)
	// Only hash2 should be returned; hash1 should be filtered.
	for _, req := range reqs {
		if req.Hash == hash1 {
			t.Fatal("expected hash1 to be filtered out of PopRequests")
		}
	}
}
