package sync

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// --- mock header source for header downloader tests ---

type hdlMockSource struct {
	headers map[uint64]*types.Header
	failAt  uint64 // if > 0, fail when requesting from this number
}

func newHDLMockSource() *hdlMockSource {
	return &hdlMockSource{headers: make(map[uint64]*types.Header)}
}

func (s *hdlMockSource) addChain(start, end uint64) {
	var parentHash types.Hash
	for i := start; i <= end; i++ {
		h := &types.Header{
			Number:     new(big.Int).SetUint64(i),
			ParentHash: parentHash,
			Difficulty: new(big.Int),
			GasLimit:   30_000_000,
			Time:       1000 + i*12,
		}
		s.headers[i] = h
		parentHash = h.Hash()
	}
}

func (s *hdlMockSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	if s.failAt > 0 && from == s.failAt {
		return nil, errors.New("mock fetch error")
	}
	var result []*types.Header
	for i := from; i < from+uint64(count); i++ {
		h, ok := s.headers[i]
		if !ok {
			break
		}
		result = append(result, h)
	}
	return result, nil
}

// --- tests ---

func TestHDLNewHeaderDownloader(t *testing.T) {
	config := DefaultHDLConfig()
	source := newHDLMockSource()
	hd := NewHeaderDownloader(config, source)

	if hd == nil {
		t.Fatal("NewHeaderDownloader returned nil")
	}
	if hd.config.BatchSize != DefaultHDLBatchSize {
		t.Errorf("BatchSize = %d, want %d", hd.config.BatchSize, DefaultHDLBatchSize)
	}
	if hd.HeaderCount() != 0 {
		t.Errorf("HeaderCount = %d, want 0", hd.HeaderCount())
	}
}

func TestHDLAddRemovePeer(t *testing.T) {
	hd := NewHeaderDownloader(DefaultHDLConfig(), newHDLMockSource())

	hd.AddPeer("peer1", types.Hash{1}, 100)
	hd.AddPeer("peer2", types.Hash{2}, 200)

	hd.mu.Lock()
	if len(hd.peers) != 2 {
		t.Errorf("peer count = %d, want 2", len(hd.peers))
	}
	hd.mu.Unlock()

	hd.RemovePeer("peer1")
	hd.mu.Lock()
	if len(hd.peers) != 1 {
		t.Errorf("peer count after remove = %d, want 1", len(hd.peers))
	}
	if _, ok := hd.peers["peer2"]; !ok {
		t.Error("peer2 should still exist")
	}
	hd.mu.Unlock()
}

func TestHDLSelectPeer(t *testing.T) {
	hd := NewHeaderDownloader(DefaultHDLConfig(), newHDLMockSource())

	// No peers.
	hd.mu.Lock()
	p := hd.selectPeer()
	hd.mu.Unlock()
	if p != nil {
		t.Error("selectPeer should return nil when no peers")
	}

	// Add peers with different failure counts.
	hd.AddPeer("peer1", types.Hash{1}, 100)
	hd.AddPeer("peer2", types.Hash{2}, 200)

	hd.mu.Lock()
	hd.peers["peer1"].Failures = 5
	hd.peers["peer2"].Failures = 1
	p = hd.selectPeer()
	hd.mu.Unlock()

	if p == nil {
		t.Fatal("selectPeer should return a peer")
	}
	if p.ID != "peer2" {
		t.Errorf("selectPeer = %s, want peer2 (fewer failures)", p.ID)
	}
}

func TestHDLDownloadHeaders(t *testing.T) {
	source := newHDLMockSource()
	source.addChain(1, 100)

	config := HDLConfig{
		BatchSize:    32,
		RetryLimit:   3,
		Timeout:      5 * time.Second,
		PivotMargin:  10,
		AnchorStride: 20,
	}
	hd := NewHeaderDownloader(config, source)
	hd.AddPeer("peer1", types.Hash{1}, 100)

	// Build a local head for reorg detection.
	localHead := source.headers[1]

	err := hd.DownloadHeaders(1, 100, localHead)
	if err != nil {
		t.Fatalf("DownloadHeaders failed: %v", err)
	}

	// Should have downloaded headers for the range.
	if hd.HeaderCount() == 0 {
		t.Error("no headers downloaded")
	}

	// Check progress.
	prog := hd.Progress()
	if prog.Target != 100 {
		t.Errorf("Target = %d, want 100", prog.Target)
	}
	if prog.Pivot != 90 {
		t.Errorf("Pivot = %d, want 90 (target - PivotMargin)", prog.Pivot)
	}
}

func TestHDLDownloadHeadersNoPeers(t *testing.T) {
	source := newHDLMockSource()
	source.addChain(1, 10)
	hd := NewHeaderDownloader(DefaultHDLConfig(), source)

	err := hd.DownloadHeaders(1, 10, nil)
	if !errors.Is(err, ErrHDLNoPeers) {
		t.Errorf("expected ErrHDLNoPeers, got %v", err)
	}
}

func TestHDLDownloadHeadersInvalidRange(t *testing.T) {
	source := newHDLMockSource()
	hd := NewHeaderDownloader(DefaultHDLConfig(), source)
	hd.AddPeer("peer1", types.Hash{1}, 100)

	err := hd.DownloadHeaders(50, 10, nil)
	if !errors.Is(err, ErrInvalidRange) {
		t.Errorf("expected ErrInvalidRange, got %v", err)
	}
}

func TestHDLDownloadHeadersClosed(t *testing.T) {
	source := newHDLMockSource()
	source.addChain(1, 10)
	hd := NewHeaderDownloader(DefaultHDLConfig(), source)
	hd.AddPeer("peer1", types.Hash{1}, 10)
	hd.Close()

	err := hd.DownloadHeaders(1, 10, nil)
	if !errors.Is(err, ErrHDLClosed) {
		t.Errorf("expected ErrHDLClosed, got %v", err)
	}
}

func TestHDLDoubleRun(t *testing.T) {
	source := newHDLMockSource()
	source.addChain(1, 10)

	config := HDLConfig{
		BatchSize:    10,
		RetryLimit:   1,
		Timeout:      time.Second,
		PivotMargin:  2,
		AnchorStride: 5,
	}
	hd := NewHeaderDownloader(config, source)
	hd.AddPeer("peer1", types.Hash{1}, 10)

	// Manually set running to simulate concurrent call.
	hd.running.Store(true)
	err := hd.DownloadHeaders(1, 10, nil)
	if !errors.Is(err, ErrHDLRunning) {
		t.Errorf("expected ErrHDLRunning, got %v", err)
	}
	hd.running.Store(false)
}

func TestHDLVerifyBatchBadNumber(t *testing.T) {
	hd := NewHeaderDownloader(DefaultHDLConfig(), newHDLMockSource())

	h1 := &types.Header{
		Number: big.NewInt(5), Difficulty: new(big.Int), Time: 1000,
	}
	h2 := &types.Header{
		Number: big.NewInt(10), Difficulty: new(big.Int), Time: 1100,
		ParentHash: h1.Hash(),
	}
	err := hd.verifyBatch([]*types.Header{h1, h2}, nil)
	if err == nil {
		t.Error("expected error for non-contiguous block numbers")
	}
}

func TestHDLVerifyBatchBadParent(t *testing.T) {
	hd := NewHeaderDownloader(DefaultHDLConfig(), newHDLMockSource())

	h1 := &types.Header{
		Number: big.NewInt(5), Difficulty: new(big.Int), Time: 1000,
	}
	h2 := &types.Header{
		Number: big.NewInt(6), Difficulty: new(big.Int), Time: 1012,
		ParentHash: types.Hash{0xff}, // wrong parent
	}
	err := hd.verifyBatch([]*types.Header{h1, h2}, nil)
	if err == nil {
		t.Error("expected error for bad parent hash")
	}
}

func TestHDLVerifyBatchBadTimestamp(t *testing.T) {
	hd := NewHeaderDownloader(DefaultHDLConfig(), newHDLMockSource())

	h1 := &types.Header{
		Number: big.NewInt(5), Difficulty: new(big.Int), Time: 2000,
	}
	h2 := &types.Header{
		Number:     big.NewInt(6),
		Difficulty: new(big.Int),
		Time:       1000, // before parent
		ParentHash: h1.Hash(),
	}
	err := hd.verifyBatch([]*types.Header{h1, h2}, nil)
	if err == nil {
		t.Error("expected error for timestamp ordering violation")
	}
}

func TestHDLFetchWithRetryFails(t *testing.T) {
	source := newHDLMockSource()
	source.failAt = 5

	config := HDLConfig{
		BatchSize:    10,
		RetryLimit:   2,
		Timeout:      time.Second,
		PivotMargin:  2,
		AnchorStride: 5,
	}
	hd := NewHeaderDownloader(config, source)
	hd.AddPeer("peer1", types.Hash{1}, 10)

	_, err := hd.fetchWithRetry(5, 1)
	if err == nil {
		t.Error("expected error when all retries fail")
	}
}

func TestHDLProgressPercentage(t *testing.T) {
	p := &HDLProgress{Anchor: 0, Target: 100, Downloaded: 50}
	pct := p.Percentage()
	if pct != 50.0 {
		t.Errorf("Percentage = %f, want 50.0", pct)
	}

	p2 := &HDLProgress{Anchor: 0, Target: 0}
	if p2.Percentage() != 100.0 {
		t.Errorf("Percentage for zero target = %f, want 100.0", p2.Percentage())
	}

	p3 := &HDLProgress{Anchor: 0, Target: 100, Downloaded: 200}
	if p3.Percentage() != 100.0 {
		t.Errorf("Percentage for over-downloaded = %f, want 100.0", p3.Percentage())
	}
}

func TestHDLReset(t *testing.T) {
	source := newHDLMockSource()
	source.addChain(1, 10)

	config := HDLConfig{
		BatchSize: 10, RetryLimit: 1, Timeout: time.Second,
		PivotMargin: 2, AnchorStride: 5,
	}
	hd := NewHeaderDownloader(config, source)
	hd.AddPeer("peer1", types.Hash{1}, 10)

	_ = hd.DownloadHeaders(1, 10, nil)
	if hd.HeaderCount() == 0 {
		t.Fatal("expected some headers after download")
	}

	hd.Reset()
	if hd.HeaderCount() != 0 {
		t.Errorf("HeaderCount after reset = %d, want 0", hd.HeaderCount())
	}
}
