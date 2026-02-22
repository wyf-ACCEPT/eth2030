package sync

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// healerStateWriter implements StateWriter for testing the state healer.
type healerStateWriter struct {
	accounts   map[types.Hash]AccountData
	storage    map[string][]byte
	bytecodes  map[types.Hash][]byte
	trieNodes  map[string][]byte
	missingFn  func(root types.Hash, limit int) [][]byte
}

func newHealerStateWriter() *healerStateWriter {
	return &healerStateWriter{
		accounts:  make(map[types.Hash]AccountData),
		storage:   make(map[string][]byte),
		bytecodes: make(map[types.Hash][]byte),
		trieNodes: make(map[string][]byte),
	}
}

func (w *healerStateWriter) WriteAccount(hash types.Hash, data AccountData) error {
	w.accounts[hash] = data
	return nil
}

func (w *healerStateWriter) WriteStorage(accountHash, slotHash types.Hash, value []byte) error {
	key := string(accountHash[:]) + string(slotHash[:])
	w.storage[key] = append([]byte{}, value...)
	return nil
}

func (w *healerStateWriter) WriteBytecode(hash types.Hash, code []byte) error {
	w.bytecodes[hash] = append([]byte{}, code...)
	return nil
}

func (w *healerStateWriter) WriteTrieNode(path []byte, data []byte) error {
	w.trieNodes[string(path)] = append([]byte{}, data...)
	return nil
}

func (w *healerStateWriter) HasBytecode(hash types.Hash) bool {
	_, ok := w.bytecodes[hash]
	return ok
}

func (w *healerStateWriter) HasTrieNode(path []byte) bool {
	_, ok := w.trieNodes[string(path)]
	return ok
}

func (w *healerStateWriter) MissingTrieNodes(root types.Hash, limit int) [][]byte {
	if w.missingFn != nil {
		return w.missingFn(root, limit)
	}
	return nil
}

// healerPeer implements SnapPeer for testing the state healer.
type healerPeer struct {
	trieNodesFn func(root types.Hash, paths [][]byte) ([][]byte, error)
}

func (p *healerPeer) ID() string { return "healer-test-peer" }

func (p *healerPeer) RequestAccountRange(req AccountRangeRequest) (*AccountRangeResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *healerPeer) RequestStorageRange(req StorageRangeRequest) (*StorageRangeResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *healerPeer) RequestBytecodes(req BytecodeRequest) (*BytecodeResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *healerPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	if p.trieNodesFn != nil {
		return p.trieNodesFn(root, paths)
	}
	return nil, nil
}

// --- Tests ---

func TestNewStateHealer(t *testing.T) {
	root := crypto.Keccak256Hash([]byte("root"))
	w := newHealerStateWriter()
	h := NewStateHealer(root, w)

	if h.Root() != root {
		t.Errorf("root mismatch: got %s, want %s", h.Root().Hex(), root.Hex())
	}
	if h.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", h.PendingCount())
	}
	if h.IsComplete() {
		t.Error("healer should not be complete initially")
	}
}

func TestDetectGaps_NoGaps(t *testing.T) {
	w := newHealerStateWriter()
	h := NewStateHealer(types.Hash{}, w)

	n, err := h.DetectGaps()
	if err != nil {
		t.Fatalf("DetectGaps: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 gaps, got %d", n)
	}
}

func TestDetectGaps_FindsMissing(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return [][]byte{
			{0x01, 0x02},
			{0x03, 0x04},
			{0x05, 0x06},
		}
	}

	h := NewStateHealer(types.Hash{}, w)
	n, err := h.DetectGaps()
	if err != nil {
		t.Fatalf("DetectGaps: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 gaps, got %d", n)
	}
	if h.PendingCount() != 3 {
		t.Errorf("expected 3 pending, got %d", h.PendingCount())
	}
}

func TestDetectGaps_DeduplicatesExisting(t *testing.T) {
	w := newHealerStateWriter()
	paths := [][]byte{{0x01}, {0x02}}
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return paths
	}

	h := NewStateHealer(types.Hash{}, w)
	h.DetectGaps()
	n, _ := h.DetectGaps() // second call with same paths
	if n != 0 {
		t.Errorf("expected 0 new gaps on second scan, got %d", n)
	}
}

func TestDetectGaps_Closed(t *testing.T) {
	h := NewStateHealer(types.Hash{}, newHealerStateWriter())
	h.Close()
	_, err := h.DetectGaps()
	if err != ErrHealerClosed {
		t.Errorf("expected ErrHealerClosed, got %v", err)
	}
}

func TestScheduleHealing_Empty(t *testing.T) {
	h := NewStateHealer(types.Hash{}, newHealerStateWriter())
	tasks, err := h.ScheduleHealing()
	if err != nil {
		t.Fatalf("ScheduleHealing: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

func TestScheduleHealing_ReturnsBatch(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		var paths [][]byte
		for i := 0; i < 5; i++ {
			paths = append(paths, []byte{byte(i)})
		}
		return paths
	}

	h := NewStateHealer(types.Hash{}, w)
	h.SetBatchSize(3)
	h.DetectGaps()

	tasks, err := h.ScheduleHealing()
	if err != nil {
		t.Fatalf("ScheduleHealing: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected batch of 3, got %d", len(tasks))
	}
	// 2 should remain pending.
	if h.PendingCount() != 2 {
		t.Errorf("expected 2 remaining, got %d", h.PendingCount())
	}
}

func TestScheduleHealing_Closed(t *testing.T) {
	h := NewStateHealer(types.Hash{}, newHealerStateWriter())
	h.Close()
	_, err := h.ScheduleHealing()
	if err != ErrHealerClosed {
		t.Errorf("expected ErrHealerClosed, got %v", err)
	}
}

func TestProcessHealingBatch_SuccessfulNodes(t *testing.T) {
	w := newHealerStateWriter()
	// After processing, report no more missing.
	callCount := 0
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		callCount++
		if callCount <= 1 {
			return [][]byte{{0x01}, {0x02}}
		}
		return nil
	}

	h := NewStateHealer(types.Hash{}, w)
	h.DetectGaps()

	tasks, _ := h.ScheduleHealing()

	results := make([][]byte, len(tasks))
	for i := range tasks {
		results[i] = []byte{0xaa, 0xbb, 0xcc} // fake node data
	}

	err := h.ProcessHealingBatch(tasks, results)
	if err != nil {
		t.Fatalf("ProcessHealingBatch: %v", err)
	}

	p := h.Progress()
	if p.NodesHealed != 2 {
		t.Errorf("expected 2 nodes healed, got %d", p.NodesHealed)
	}
	if p.BytesDownloaded != 6 {
		t.Errorf("expected 6 bytes downloaded, got %d", p.BytesDownloaded)
	}
}

func TestProcessHealingBatch_EmptyData_Retry(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}}
	}

	h := NewStateHealer(types.Hash{}, w)
	h.DetectGaps()
	tasks, _ := h.ScheduleHealing()

	// Provide empty data to trigger retry.
	err := h.ProcessHealingBatch(tasks, [][]byte{nil})
	if err != nil {
		t.Fatalf("ProcessHealingBatch: %v", err)
	}

	// Task should be re-queued.
	if h.PendingCount() != 1 {
		t.Errorf("expected 1 pending (retried), got %d", h.PendingCount())
	}
}

func TestProcessHealingBatch_MaxRetries(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}}
	}

	h := NewStateHealer(types.Hash{}, w)
	h.DetectGaps()

	// Exhaust retries.
	for i := 0; i < MaxHealRetries; i++ {
		tasks, _ := h.ScheduleHealing()
		if len(tasks) == 0 {
			break
		}
		h.ProcessHealingBatch(tasks, [][]byte{nil})
	}

	if h.FailedCount() != 1 {
		t.Errorf("expected 1 failed, got %d", h.FailedCount())
	}
	if h.PendingCount() != 0 {
		t.Errorf("expected 0 pending, got %d", h.PendingCount())
	}
}

func TestProcessHealingBatch_EmptyTasks(t *testing.T) {
	h := NewStateHealer(types.Hash{}, newHealerStateWriter())
	err := h.ProcessHealingBatch(nil, nil)
	if err != ErrHealBatchEmpty {
		t.Errorf("expected ErrHealBatchEmpty, got %v", err)
	}
}

func TestProcessHealingBatch_WritesToDB(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return [][]byte{{0xab}}
	}

	h := NewStateHealer(types.Hash{}, w)
	h.DetectGaps()
	tasks, _ := h.ScheduleHealing()

	nodeData := []byte{0x01, 0x02, 0x03, 0x04}
	h.ProcessHealingBatch(tasks, [][]byte{nodeData})

	stored := w.trieNodes[string([]byte{0xab})]
	if !bytes.Equal(stored, nodeData) {
		t.Errorf("stored data mismatch: got %x, want %x", stored, nodeData)
	}
}

func TestStateHealerReset(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}, {0x02}}
	}

	h := NewStateHealer(types.Hash{}, w)
	h.DetectGaps()
	if h.PendingCount() != 2 {
		t.Fatalf("expected 2 pending")
	}

	h.Reset()
	if h.PendingCount() != 0 {
		t.Errorf("expected 0 pending after reset, got %d", h.PendingCount())
	}
	p := h.Progress()
	if p.NodesDetected != 0 {
		t.Errorf("expected 0 detected after reset, got %d", p.NodesDetected)
	}
}

func TestHealerRun_CompletesWithNoGaps(t *testing.T) {
	w := newHealerStateWriter()
	// No missing nodes.
	w.missingFn = func(root types.Hash, limit int) [][]byte { return nil }

	h := NewStateHealer(types.Hash{}, w)
	peer := &healerPeer{}

	err := h.Run(peer)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !h.IsComplete() {
		t.Error("expected healer to be complete")
	}
}

func TestHealerRun_HealsGaps(t *testing.T) {
	w := newHealerStateWriter()
	callCount := 0
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		callCount++
		if callCount == 1 {
			return [][]byte{{0x01}, {0x02}}
		}
		return nil
	}

	peer := &healerPeer{
		trieNodesFn: func(root types.Hash, paths [][]byte) ([][]byte, error) {
			results := make([][]byte, len(paths))
			for i := range paths {
				results[i] = []byte{0xde, 0xad, 0xbe, 0xef}
			}
			return results, nil
		},
	}

	h := NewStateHealer(types.Hash{}, w)
	err := h.Run(peer)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	p := h.Progress()
	if p.NodesHealed != 2 {
		t.Errorf("expected 2 healed, got %d", p.NodesHealed)
	}
	if !p.Complete {
		t.Error("expected complete")
	}
}

func TestHealerRun_PeerError(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		return [][]byte{{0x01}}
	}

	peer := &healerPeer{
		trieNodesFn: func(root types.Hash, paths [][]byte) ([][]byte, error) {
			return nil, errors.New("network error")
		},
	}

	h := NewStateHealer(types.Hash{}, w)
	err := h.Run(peer)
	if err == nil {
		t.Error("expected error from peer failure")
	}
}

func TestHealerRun_AlreadyRunning(t *testing.T) {
	w := newHealerStateWriter()
	// Block the first run with a slow missing function.
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	h := NewStateHealer(types.Hash{}, w)
	peer := &healerPeer{}

	go h.Run(peer)
	time.Sleep(10 * time.Millisecond)

	err := h.Run(peer)
	if err != ErrHealerRunning {
		t.Errorf("expected ErrHealerRunning, got %v", err)
	}
}

func TestHealingProgress_Remaining(t *testing.T) {
	p := HealingProgress{
		NodesDetected: 100,
		NodesHealed:   60,
		NodesFailed:   10,
	}
	if p.Remaining() != 30 {
		t.Errorf("expected 30 remaining, got %d", p.Remaining())
	}
}

func TestHealingProgress_Elapsed(t *testing.T) {
	p := HealingProgress{}
	if p.Elapsed() != 0 {
		t.Error("expected 0 elapsed for zero start time")
	}
	p.StartTime = time.Now().Add(-5 * time.Second)
	if p.Elapsed() < 4*time.Second {
		t.Error("elapsed should be around 5s")
	}
}

func TestHealerSetBatchSize(t *testing.T) {
	w := newHealerStateWriter()
	w.missingFn = func(root types.Hash, limit int) [][]byte {
		var paths [][]byte
		for i := 0; i < 10; i++ {
			paths = append(paths, []byte{byte(i)})
		}
		return paths
	}

	h := NewStateHealer(types.Hash{}, w)
	h.SetBatchSize(2)
	h.DetectGaps()

	tasks, _ := h.ScheduleHealing()
	if len(tasks) != 2 {
		t.Errorf("expected batch of 2, got %d", len(tasks))
	}

	// Zero or negative batch size should not change.
	h.SetBatchSize(0)
	h.SetBatchSize(-1)
	tasks2, _ := h.ScheduleHealing()
	if len(tasks2) != 2 {
		t.Errorf("expected batch of 2 after no-op set, got %d", len(tasks2))
	}
}
