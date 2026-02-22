package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// --- mock state writer for trie healer tests ---

type thMockWriter struct {
	trieNodes  map[string][]byte
	missingFn  func(root types.Hash, limit int) [][]byte
	accounts   map[types.Hash]AccountData
	storage    map[string][]byte
	bytecodes  map[types.Hash][]byte
}

func newTHMockWriter() *thMockWriter {
	return &thMockWriter{
		trieNodes: make(map[string][]byte),
		accounts:  make(map[types.Hash]AccountData),
		storage:   make(map[string][]byte),
		bytecodes: make(map[types.Hash][]byte),
	}
}

func (w *thMockWriter) WriteAccount(hash types.Hash, data AccountData) error {
	w.accounts[hash] = data
	return nil
}

func (w *thMockWriter) WriteStorage(accountHash, slotHash types.Hash, value []byte) error {
	key := string(accountHash[:]) + string(slotHash[:])
	w.storage[key] = append([]byte{}, value...)
	return nil
}

func (w *thMockWriter) WriteBytecode(hash types.Hash, code []byte) error {
	w.bytecodes[hash] = append([]byte{}, code...)
	return nil
}

func (w *thMockWriter) WriteTrieNode(path []byte, data []byte) error {
	w.trieNodes[string(path)] = append([]byte{}, data...)
	return nil
}

func (w *thMockWriter) HasBytecode(hash types.Hash) bool {
	_, ok := w.bytecodes[hash]
	return ok
}

func (w *thMockWriter) HasTrieNode(path []byte) bool {
	_, ok := w.trieNodes[string(path)]
	return ok
}

func (w *thMockWriter) MissingTrieNodes(root types.Hash, limit int) [][]byte {
	if w.missingFn != nil {
		return w.missingFn(root, limit)
	}
	return nil
}

// --- mock snap peer for trie healer tests ---

type thMockPeer struct {
	trieNodesFn func(root types.Hash, paths [][]byte) ([][]byte, error)
}

func (p *thMockPeer) ID() string { return "th-test-peer" }

func (p *thMockPeer) RequestAccountRange(req AccountRangeRequest) (*AccountRangeResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *thMockPeer) RequestStorageRange(req StorageRangeRequest) (*StorageRangeResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *thMockPeer) RequestBytecodes(req BytecodeRequest) (*BytecodeResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *thMockPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	if p.trieNodesFn != nil {
		return p.trieNodesFn(root, paths)
	}
	results := make([][]byte, len(paths))
	for i, path := range paths {
		results[i] = append([]byte("node-"), path...)
	}
	return results, nil
}

// --- tests ---

func TestTrieHealerNew(t *testing.T) {
	config := DefaultTrieHealConfig()
	root := types.Hash{1, 2, 3}
	writer := newTHMockWriter()

	th := NewTrieHealer(config, root, writer)
	if th == nil {
		t.Fatal("NewTrieHealer returned nil")
	}
	if th.QueueLen() != 0 {
		t.Errorf("QueueLen = %d, want 0", th.QueueLen())
	}
	if th.FailedCount() != 0 {
		t.Errorf("FailedCount = %d, want 0", th.FailedCount())
	}
}

func TestTrieHealPriorityQueue(t *testing.T) {
	var pq trieHealPriorityQueue

	// Push nodes at different depths.
	pq.Push(&TrieHealNode{Path: []byte{1, 2, 3}, Depth: 3})
	pq.Push(&TrieHealNode{Path: []byte{1}, Depth: 1})
	pq.Push(&TrieHealNode{Path: []byte{1, 2}, Depth: 2})
	pq.Push(&TrieHealNode{Path: []byte{1, 2, 3, 4, 5}, Depth: 5})

	if pq.Len() != 4 {
		t.Fatalf("pq.Len() = %d, want 4", pq.Len())
	}

	// Pop should return shallowest first.
	n1 := pq.Pop()
	if n1.Depth != 1 {
		t.Errorf("first pop depth = %d, want 1", n1.Depth)
	}

	n2 := pq.Pop()
	if n2.Depth != 2 {
		t.Errorf("second pop depth = %d, want 2", n2.Depth)
	}

	n3 := pq.Pop()
	if n3.Depth != 3 {
		t.Errorf("third pop depth = %d, want 3", n3.Depth)
	}

	n4 := pq.Pop()
	if n4.Depth != 5 {
		t.Errorf("fourth pop depth = %d, want 5", n4.Depth)
	}

	// Empty pop should return nil.
	if pq.Pop() != nil {
		t.Error("Pop on empty queue should return nil")
	}
}

func TestTrieHealerDetectStateGaps(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	callCount := 0
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		callCount++
		if callCount <= 1 {
			return [][]byte{{0x01}, {0x01, 0x02}, {0x01, 0x02, 0x03}}
		}
		return nil
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	added := th.DetectStateGaps()
	if added != 3 {
		t.Errorf("DetectStateGaps = %d, want 3", added)
	}
	if th.QueueLen() != 3 {
		t.Errorf("QueueLen = %d, want 3", th.QueueLen())
	}

	// Detect again should not add duplicates.
	added2 := th.DetectStateGaps()
	if added2 != 0 {
		t.Errorf("DetectStateGaps second call = %d, want 0", added2)
	}
}

func TestTrieHealerDetectStorageGaps(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	acctHash := types.Hash{0xaa}
	storageRoot := types.Hash{0xbb}

	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		if r == storageRoot {
			return [][]byte{{0x10}, {0x10, 0x20}}
		}
		return nil
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)
	th.AddStorageTrie(acctHash, storageRoot)

	added := th.DetectStorageGaps()
	if added != 2 {
		t.Errorf("DetectStorageGaps = %d, want 2", added)
	}
}

func TestTrieHealerAddStorageTrieSkipsEmpty(t *testing.T) {
	th := NewTrieHealer(DefaultTrieHealConfig(), types.Hash{1}, newTHMockWriter())

	th.AddStorageTrie(types.Hash{0xaa}, types.EmptyRootHash)
	th.AddStorageTrie(types.Hash{0xbb}, types.Hash{})

	th.mu.Lock()
	count := len(th.storageRoots)
	th.mu.Unlock()
	if count != 0 {
		t.Errorf("storageRoots count = %d, want 0 (empty roots skipped)", count)
	}
}

func TestTrieHealerScheduleBatch(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}, {0x01, 0x02}, {0x01, 0x02, 0x03}}
	}

	config := TrieHealConfig{BatchSize: 2, MaxRetries: 3, CheckpointInterval: 100}
	th := NewTrieHealer(config, root, writer)

	th.DetectStateGaps()

	batch := th.ScheduleBatch()
	if len(batch) != 2 {
		t.Errorf("batch len = %d, want 2 (batch size)", len(batch))
	}

	// First batch item should be shallowest.
	if batch[0].Depth > batch[1].Depth {
		t.Error("batch not sorted by depth (shallowest first)")
	}

	// One item remaining.
	if th.QueueLen() != 1 {
		t.Errorf("QueueLen = %d, want 1", th.QueueLen())
	}
}

func TestTrieHealerProcessResults(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	batch := []*TrieHealNode{
		{Path: []byte{0x01}, Depth: 1, Root: root},
		{Path: []byte{0x02}, Depth: 1, Root: root},
	}

	results := [][]byte{
		[]byte("valid-node-data"),
		nil, // empty -> retry
	}

	err := th.ProcessResults(batch, results)
	if err != nil {
		t.Fatalf("ProcessResults: %v", err)
	}

	prog := th.Progress()
	if prog.StateNodesHealed != 1 {
		t.Errorf("StateNodesHealed = %d, want 1", prog.StateNodesHealed)
	}

	// The second node should be re-queued.
	if th.QueueLen() != 1 {
		t.Errorf("QueueLen = %d, want 1 (re-queued failed node)", th.QueueLen())
	}

	// Verify the node was written.
	if _, ok := writer.trieNodes[string([]byte{0x01})]; !ok {
		t.Error("node 0x01 not written to state writer")
	}
}

func TestTrieHealerProcessResultsRetryExhausted(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	config := TrieHealConfig{BatchSize: 10, MaxRetries: 1, CheckpointInterval: 1000}
	th := NewTrieHealer(config, root, writer)

	node := &TrieHealNode{Path: []byte{0x01}, Depth: 1, Root: root, Retries: 0}
	batch := []*TrieHealNode{node}
	results := [][]byte{nil} // empty -> retry, but retries = 0 + 1 >= MaxRetries (1)

	err := th.ProcessResults(batch, results)
	if err != nil {
		t.Fatalf("ProcessResults: %v", err)
	}

	if th.FailedCount() != 1 {
		t.Errorf("FailedCount = %d, want 1", th.FailedCount())
	}
	if th.QueueLen() != 0 {
		t.Errorf("QueueLen = %d, want 0", th.QueueLen())
	}
}

func TestTrieHealerProcessResultsStorageNode(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	batch := []*TrieHealNode{
		{
			Path:        []byte{0x01},
			Depth:       1,
			Root:        types.Hash{0xbb},
			AccountHash: types.Hash{0xaa}, // storage trie node
		},
	}

	results := [][]byte{[]byte("storage-node-data")}

	err := th.ProcessResults(batch, results)
	if err != nil {
		t.Fatalf("ProcessResults: %v", err)
	}

	prog := th.Progress()
	if prog.StorageNodesHealed != 1 {
		t.Errorf("StorageNodesHealed = %d, want 1", prog.StorageNodesHealed)
	}
	if prog.StateNodesHealed != 0 {
		t.Errorf("StateNodesHealed = %d, want 0", prog.StateNodesHealed)
	}
}

func TestTrieHealerCheckCompletion(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		return nil // no missing nodes
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	if !th.CheckCompletion() {
		t.Error("CheckCompletion should return true when no gaps")
	}

	prog := th.Progress()
	if !prog.Complete {
		t.Error("progress.Complete should be true")
	}
}

func TestTrieHealerCheckCompletionWithGaps(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}}
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	if th.CheckCompletion() {
		t.Error("CheckCompletion should return false when gaps exist")
	}
}

func TestTrieHealerCheckpoint(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	config := TrieHealConfig{BatchSize: 10, MaxRetries: 3, CheckpointInterval: 2}
	th := NewTrieHealer(config, root, writer)

	var savedCP *TrieHealCheckpoint
	th.SetCheckpointCallback(func(cp TrieHealCheckpoint) error {
		saved := cp
		savedCP = &saved
		return nil
	})

	// Process enough results to trigger a checkpoint.
	batch := []*TrieHealNode{
		{Path: []byte{0x01}, Depth: 1, Root: root},
		{Path: []byte{0x02}, Depth: 1, Root: root},
		{Path: []byte{0x03}, Depth: 1, Root: root},
	}
	results := [][]byte{
		[]byte("data1"),
		[]byte("data2"),
		[]byte("data3"),
	}

	err := th.ProcessResults(batch, results)
	if err != nil {
		t.Fatalf("ProcessResults: %v", err)
	}

	if savedCP == nil {
		t.Fatal("checkpoint callback was not invoked")
	}
	if savedCP.NodesHealed != 3 {
		t.Errorf("checkpoint NodesHealed = %d, want 3", savedCP.NodesHealed)
	}

	prog := th.Progress()
	if prog.CheckpointsWritten != 1 {
		t.Errorf("CheckpointsWritten = %d, want 1", prog.CheckpointsWritten)
	}
}

func TestTrieHealerResumeFromCheckpoint(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	cp := TrieHealCheckpoint{
		StateRoot:       root,
		NodesHealed:     50,
		NodesFailed:     2,
		BytesDownloaded: 10000,
		PendingPaths:    [][]byte{{0x01}, {0x02}},
		AccountRoots:    []types.Hash{{0xaa}},
		Timestamp:       time.Now(),
	}

	th.ResumeFromCheckpoint(cp)

	prog := th.Progress()
	if prog.StateNodesHealed != 50 {
		t.Errorf("StateNodesHealed = %d, want 50", prog.StateNodesHealed)
	}
	if prog.BytesDownloaded != 10000 {
		t.Errorf("BytesDownloaded = %d, want 10000", prog.BytesDownloaded)
	}
	if th.QueueLen() != 2 {
		t.Errorf("QueueLen = %d, want 2", th.QueueLen())
	}
}

func TestTrieHealerRun(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()

	callCount := 0
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		callCount++
		if callCount == 1 {
			return [][]byte{{0x01}, {0x02}}
		}
		return nil
	}

	peer := &thMockPeer{
		trieNodesFn: func(r types.Hash, paths [][]byte) ([][]byte, error) {
			results := make([][]byte, len(paths))
			for i, p := range paths {
				results[i] = append([]byte("node-"), p...)
			}
			return results, nil
		},
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)

	err := th.Run(peer)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	prog := th.Progress()
	if !prog.Complete {
		t.Error("healing should be complete")
	}
	if prog.StateNodesHealed != 2 {
		t.Errorf("StateNodesHealed = %d, want 2", prog.StateNodesHealed)
	}
}

func TestTrieHealerRunClosed(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}}
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)
	th.Close()

	peer := &thMockPeer{}
	err := th.Run(peer)
	// Should return quickly since closed.
	if err != nil && !errors.Is(err, ErrTrieHealerClosed) {
		t.Errorf("expected ErrTrieHealerClosed or nil, got %v", err)
	}
}

func TestTrieHealerRunDoubleStart(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	writer.missingFn = func(r types.Hash, limit int) [][]byte { return nil }

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)
	th.running.Store(true)

	peer := &thMockPeer{}
	err := th.Run(peer)
	if !errors.Is(err, ErrTrieHealerRunning) {
		t.Errorf("expected ErrTrieHealerRunning, got %v", err)
	}
	th.running.Store(false)
}

func TestTrieHealProgressPercentage(t *testing.T) {
	p := TrieHealProgress{
		StateNodesDetected:   80,
		StorageNodesDetected: 20,
		StateNodesHealed:     40,
		StorageNodesHealed:   10,
	}
	pct := p.Percentage()
	if pct != 50.0 {
		t.Errorf("Percentage = %f, want 50.0", pct)
	}

	p2 := TrieHealProgress{} // zero detected
	if p2.Percentage() != 100.0 {
		t.Errorf("Percentage for zero detected = %f, want 100.0", p2.Percentage())
	}
}

func TestTrieHealerReset(t *testing.T) {
	root := types.Hash{1}
	writer := newTHMockWriter()
	writer.missingFn = func(r types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}}
	}

	th := NewTrieHealer(DefaultTrieHealConfig(), root, writer)
	th.DetectStateGaps()
	th.AddStorageTrie(types.Hash{0xaa}, types.Hash{0xbb})

	th.Reset()

	if th.QueueLen() != 0 {
		t.Errorf("QueueLen after reset = %d, want 0", th.QueueLen())
	}
	if th.FailedCount() != 0 {
		t.Errorf("FailedCount after reset = %d, want 0", th.FailedCount())
	}
	prog := th.Progress()
	if prog.StateNodesDetected != 0 {
		t.Errorf("StateNodesDetected after reset = %d, want 0", prog.StateNodesDetected)
	}
}

func TestTrieHealNodeIsStorage(t *testing.T) {
	n1 := &TrieHealNode{AccountHash: types.Hash{0xaa}}
	if !n1.isStorageTrie() {
		t.Error("node with non-zero AccountHash should be storage trie")
	}

	n2 := &TrieHealNode{AccountHash: types.Hash{}}
	if n2.isStorageTrie() {
		t.Error("node with zero AccountHash should not be storage trie")
	}
}

// Silence unused import for crypto.
var _ = crypto.Keccak256Hash
