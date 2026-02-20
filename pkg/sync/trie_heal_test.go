package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// mockStateWriterForHeal implements StateWriter for testing the trie healer.
type mockStateWriterForHeal struct {
	nodes   map[string][]byte // path -> node data
	missing [][]byte          // paths to report as missing
}

func newMockStateWriterForHeal() *mockStateWriterForHeal {
	return &mockStateWriterForHeal{
		nodes: make(map[string][]byte),
	}
}

func (m *mockStateWriterForHeal) WriteAccount(hash types.Hash, data AccountData) error { return nil }
func (m *mockStateWriterForHeal) WriteStorage(accountHash, slotHash types.Hash, value []byte) error {
	return nil
}
func (m *mockStateWriterForHeal) WriteBytecode(hash types.Hash, code []byte) error { return nil }
func (m *mockStateWriterForHeal) HasBytecode(hash types.Hash) bool                 { return false }
func (m *mockStateWriterForHeal) HasTrieNode(path []byte) bool {
	_, ok := m.nodes[string(path)]
	return ok
}

func (m *mockStateWriterForHeal) WriteTrieNode(path []byte, data []byte) error {
	m.nodes[string(path)] = data
	// Remove from missing list.
	var remaining [][]byte
	for _, p := range m.missing {
		if string(p) != string(path) {
			remaining = append(remaining, p)
		}
	}
	m.missing = remaining
	return nil
}

func (m *mockStateWriterForHeal) MissingTrieNodes(root types.Hash, limit int) [][]byte {
	if limit > 0 && limit < len(m.missing) {
		return m.missing[:limit]
	}
	return m.missing
}

// mockSnapPeerForHeal implements SnapPeer for testing.
type mockSnapPeerForHeal struct {
	id       string
	nodeData map[string][]byte
	err      error
}

func newMockSnapPeerForHeal(id string) *mockSnapPeerForHeal {
	return &mockSnapPeerForHeal{
		id:       id,
		nodeData: make(map[string][]byte),
	}
}

func (m *mockSnapPeerForHeal) ID() string { return m.id }
func (m *mockSnapPeerForHeal) RequestAccountRange(req AccountRangeRequest) (*AccountRangeResponse, error) {
	return nil, nil
}
func (m *mockSnapPeerForHeal) RequestStorageRange(req StorageRangeRequest) (*StorageRangeResponse, error) {
	return nil, nil
}
func (m *mockSnapPeerForHeal) RequestBytecodes(req BytecodeRequest) (*BytecodeResponse, error) {
	return nil, nil
}

func (m *mockSnapPeerForHeal) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	results := make([][]byte, len(paths))
	for i, path := range paths {
		if data, ok := m.nodeData[string(path)]; ok {
			results[i] = data
		}
	}
	return results, nil
}

func TestTrieHeal_GapFinder(t *testing.T) {
	writer := newMockStateWriterForHeal()
	writer.missing = [][]byte{{0x01}, {0x02}, {0x03}}

	gf := NewGapFinder(writer)
	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")

	result, err := gf.FindGaps(root, 0)
	if err != nil {
		t.Fatalf("FindGaps: %v", err)
	}
	if result.Scanned != 3 {
		t.Fatalf("expected 3 gaps, got %d", result.Scanned)
	}
}

func TestTrieHeal_GapFinderNoRoot(t *testing.T) {
	writer := newMockStateWriterForHeal()
	gf := NewGapFinder(writer)

	_, err := gf.FindGaps(types.Hash{}, 0)
	if !errors.Is(err, ErrGapFinderNoRoot) {
		t.Fatalf("expected ErrGapFinderNoRoot, got: %v", err)
	}
}

func TestTrieHeal_NodeVerifier(t *testing.T) {
	nv := NewNodeVerifier()

	data := []byte("test node data")
	hash := crypto.Keccak256Hash(data)

	// Valid verification.
	err := nv.VerifyNode(data, hash)
	if err != nil {
		t.Fatalf("expected valid node, got: %v", err)
	}

	// Invalid verification.
	wrongHash := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	err = nv.VerifyNode(data, wrongHash)
	if !errors.Is(err, ErrNodeVerifyFailed) {
		t.Fatalf("expected ErrNodeVerifyFailed, got: %v", err)
	}
}

func TestTrieHeal_NodeVerifierEmpty(t *testing.T) {
	nv := NewNodeVerifier()
	err := nv.VerifyNode(nil, types.Hash{})
	if !errors.Is(err, ErrNodeVerifyFailed) {
		t.Fatalf("expected ErrNodeVerifyFailed for empty data, got: %v", err)
	}
}

func TestTrieHeal_NodeVerifierBatch(t *testing.T) {
	nv := NewNodeVerifier()
	data1 := []byte("node1")
	data2 := []byte("node2")
	hash1 := crypto.Keccak256Hash(data1)
	hash2 := crypto.Keccak256Hash(data2)

	// All valid.
	idx := nv.VerifyBatch([][]byte{data1, data2}, []types.Hash{hash1, hash2})
	if idx != -1 {
		t.Fatalf("expected all valid, got invalid at index %d", idx)
	}

	// Second is invalid.
	wrongHash := types.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001")
	idx = nv.VerifyBatch([][]byte{data1, data2}, []types.Hash{hash1, wrongHash})
	if idx != 1 {
		t.Fatalf("expected invalid at index 1, got %d", idx)
	}
}

func TestTrieHeal_Scheduler_AddAndSchedule(t *testing.T) {
	sched := NewHealScheduler()

	task1 := &HealTask{Path: []byte{0x01}, Root: types.Hash{}, Priority: PriorityLow, Depth: 3}
	task2 := &HealTask{Path: []byte{0x02}, Root: types.Hash{}, Priority: PriorityHigh, Depth: 5}
	task3 := &HealTask{Path: []byte{0x03}, Root: types.Hash{}, Priority: PriorityMedium, Depth: 1}

	sched.AddTask(task1)
	sched.AddTask(task2)
	sched.AddTask(task3)

	if sched.PendingCount() != 3 {
		t.Fatalf("expected 3 pending, got %d", sched.PendingCount())
	}

	batch := sched.ScheduleBatch(3)
	if len(batch) != 3 {
		t.Fatalf("expected 3 in batch, got %d", len(batch))
	}

	// First should be highest priority.
	if batch[0].Priority != PriorityHigh {
		t.Fatalf("expected first batch item to be PriorityHigh, got %d", batch[0].Priority)
	}
}

func TestTrieHeal_Scheduler_DuplicatePaths(t *testing.T) {
	sched := NewHealScheduler()

	task1 := &HealTask{Path: []byte{0x01}, Priority: PriorityLow}
	task2 := &HealTask{Path: []byte{0x01}, Priority: PriorityLow}

	added1 := sched.AddTask(task1)
	added2 := sched.AddTask(task2)

	if !added1 {
		t.Fatal("expected first add to succeed")
	}
	if added2 {
		t.Fatal("expected second add to fail (duplicate)")
	}
	if sched.PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", sched.PendingCount())
	}
}

func TestTrieHeal_Scheduler_AccessPatternUpgrade(t *testing.T) {
	sched := NewHealScheduler()

	path := []byte{0x01}
	// Record enough accesses to trigger priority upgrade.
	for i := 0; i < 6; i++ {
		sched.RecordAccess(path)
	}

	task := &HealTask{Path: path, Priority: PriorityLow, Depth: 5}
	sched.AddTask(task)

	batch := sched.ScheduleBatch(1)
	if len(batch) != 1 {
		t.Fatalf("expected 1 in batch, got %d", len(batch))
	}
	if batch[0].Priority != PriorityHigh {
		t.Fatalf("expected PriorityHigh after frequent access, got %d", batch[0].Priority)
	}
}

func TestTrieHeal_ConcurrentHealer_DetectGaps(t *testing.T) {
	writer := newMockStateWriterForHeal()
	writer.missing = [][]byte{{0x01}, {0x02}, {0x03}}

	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")
	ch := NewConcurrentTrieHealer(DefaultConcurrentHealConfig(), root, writer)

	added, err := ch.DetectGaps()
	if err != nil {
		t.Fatalf("DetectGaps: %v", err)
	}
	if added != 3 {
		t.Fatalf("expected 3 gaps detected, got %d", added)
	}

	progress := ch.Progress()
	if progress.NodesDetected != 3 {
		t.Fatalf("expected 3 detected in progress, got %d", progress.NodesDetected)
	}
}

func TestTrieHeal_ConcurrentHealer_ProcessBatch(t *testing.T) {
	writer := newMockStateWriterForHeal()
	writer.missing = [][]byte{{0x01}, {0x02}}

	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")
	config := DefaultConcurrentHealConfig()
	config.BatchSize = 10
	ch := NewConcurrentTrieHealer(config, root, writer)

	// Detect gaps first.
	ch.DetectGaps()

	// Set up peer with node data.
	peer := newMockSnapPeerForHeal("peer1")
	peer.nodeData[string([]byte{0x01})] = []byte("node_data_1")
	peer.nodeData[string([]byte{0x02})] = []byte("node_data_2")

	healed, err := ch.ProcessBatch(peer)
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if healed != 2 {
		t.Fatalf("expected 2 healed, got %d", healed)
	}

	progress := ch.Progress()
	if progress.NodesHealed != 2 {
		t.Fatalf("expected 2 healed in progress, got %d", progress.NodesHealed)
	}
}

func TestTrieHeal_ConcurrentHealer_FailedRetries(t *testing.T) {
	writer := newMockStateWriterForHeal()
	writer.missing = [][]byte{{0x01}}

	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")
	config := DefaultConcurrentHealConfig()
	config.MaxRetries = 2
	config.BatchSize = 10
	ch := NewConcurrentTrieHealer(config, root, writer)

	ch.DetectGaps()

	// Peer returns empty data (node not available).
	peer := newMockSnapPeerForHeal("peer1")

	// Process multiple times to exhaust retries.
	for i := 0; i < 3; i++ {
		ch.ProcessBatch(peer)
	}

	progress := ch.Progress()
	if progress.NodesFailed != 1 {
		t.Fatalf("expected 1 failed, got %d", progress.NodesFailed)
	}
}

func TestTrieHeal_ConcurrentHealer_Run(t *testing.T) {
	writer := newMockStateWriterForHeal()
	writer.missing = [][]byte{{0x01}, {0x02}}

	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")
	config := DefaultConcurrentHealConfig()
	config.Workers = 2
	ch := NewConcurrentTrieHealer(config, root, writer)

	peer1 := newMockSnapPeerForHeal("peer1")
	peer1.nodeData[string([]byte{0x01})] = []byte("data1")
	peer1.nodeData[string([]byte{0x02})] = []byte("data2")

	peer2 := newMockSnapPeerForHeal("peer2")
	peer2.nodeData[string([]byte{0x01})] = []byte("data1")
	peer2.nodeData[string([]byte{0x02})] = []byte("data2")

	err := ch.Run([]SnapPeer{peer1, peer2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	progress := ch.Progress()
	if !progress.Complete {
		t.Fatal("expected healing to be complete")
	}
}

func TestTrieHeal_ConcurrentHealer_NoPeers(t *testing.T) {
	writer := newMockStateWriterForHeal()
	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")
	ch := NewConcurrentTrieHealer(DefaultConcurrentHealConfig(), root, writer)

	err := ch.Run(nil)
	if !errors.Is(err, ErrTrieHealNoPeer) {
		t.Fatalf("expected ErrTrieHealNoPeer, got: %v", err)
	}
}

func TestTrieHeal_Progress_ETA(t *testing.T) {
	p := &ConcurrentHealProgress{
		NodesDetected: 1000,
		NodesHealed:   500,
		NodesFailed:   0,
		StartTime:     time.Now().Add(-10 * time.Second),
	}

	pct := p.Percentage()
	if pct < 49.9 || pct > 50.1 {
		t.Fatalf("expected ~50%%, got %f%%", pct)
	}

	eta := p.ETA()
	if eta <= 0 {
		t.Fatal("expected positive ETA")
	}

	nps := p.NodesPerSecond()
	if nps <= 0 {
		t.Fatal("expected positive nodes/sec")
	}
}

func TestTrieHeal_Progress_Complete(t *testing.T) {
	p := &ConcurrentHealProgress{
		NodesDetected: 100,
		NodesHealed:   100,
		NodesFailed:   0,
	}
	if p.Percentage() != 100.0 {
		t.Fatalf("expected 100%%, got %f%%", p.Percentage())
	}
}

func TestTrieHeal_FormatETA(t *testing.T) {
	tests := []struct {
		dur    time.Duration
		expect string
	}{
		{0, "unknown"},
		{-1 * time.Second, "unknown"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{3661 * time.Second, "1h1m1s"},
	}
	for _, tc := range tests {
		got := FormatETA(tc.dur)
		if got != tc.expect {
			t.Errorf("FormatETA(%v): want %q, got %q", tc.dur, tc.expect, got)
		}
	}
}

func TestTrieHeal_ConcurrentHealer_Reset(t *testing.T) {
	writer := newMockStateWriterForHeal()
	writer.missing = [][]byte{{0x01}}
	root := types.HexToHash("0xaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd")
	ch := NewConcurrentTrieHealer(DefaultConcurrentHealConfig(), root, writer)

	ch.DetectGaps()
	if ch.Scheduler().PendingCount() != 1 {
		t.Fatalf("expected 1 pending, got %d", ch.Scheduler().PendingCount())
	}

	ch.Reset()
	if ch.Scheduler().PendingCount() != 0 {
		t.Fatalf("expected 0 pending after reset, got %d", ch.Scheduler().PendingCount())
	}

	progress := ch.Progress()
	if progress.NodesDetected != 0 {
		t.Fatalf("expected 0 detected after reset, got %d", progress.NodesDetected)
	}
}
