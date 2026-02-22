package sync

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// mockChainHeaderSource serves a pre-built chain of headers.
type mockChainHeaderSource struct {
	headers map[uint64]*types.Header
}

func (m *mockChainHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	var result []*types.Header
	for i := 0; i < count; i++ {
		h, ok := m.headers[from+uint64(i)]
		if !ok {
			break
		}
		result = append(result, h)
	}
	if len(result) == 0 {
		return nil, errors.New("no headers found")
	}
	return result, nil
}

// mockChainBodySource serves empty bodies for any hash.
type mockChainBodySource struct{}

func (m *mockChainBodySource) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	bodies := make([]*types.Body, len(hashes))
	for i := range bodies {
		bodies[i] = &types.Body{}
	}
	return bodies, nil
}

// buildChainHeaders creates a chain of n linked headers starting at block 1.
func buildChainHeaders(n int) map[uint64]*types.Header {
	headers := make(map[uint64]*types.Header, n)

	// Genesis-like header for parent hash reference.
	genesis := &types.Header{
		Number: big.NewInt(0),
		Time:   1000,
	}

	prev := genesis
	for i := 1; i <= n; i++ {
		h := &types.Header{
			ParentHash: prev.Hash(),
			Number:     big.NewInt(int64(i)),
			Time:       prev.Time + 12,
		}
		headers[uint64(i)] = h
		prev = h
	}
	return headers
}

func TestChainDownloaderPeerManagement(t *testing.T) {
	cd := NewChainDownloader(&DownloadConfig{MaxPeers: 3})

	cd.AddPeer(PeerInfo{ID: "p1", TotalDifficulty: 100})
	cd.AddPeer(PeerInfo{ID: "p2", TotalDifficulty: 200})
	cd.AddPeer(PeerInfo{ID: "p3", TotalDifficulty: 300})

	if cd.PeerCount() != 3 {
		t.Errorf("PeerCount = %d, want 3", cd.PeerCount())
	}

	// Adding a 4th peer with higher TD should evict the lowest.
	cd.AddPeer(PeerInfo{ID: "p4", TotalDifficulty: 400})
	if cd.PeerCount() != 3 {
		t.Errorf("PeerCount = %d, want 3 after eviction", cd.PeerCount())
	}
	if cd.GetPeer("p1") != nil {
		t.Error("p1 should have been evicted (lowest TD)")
	}

	// Adding a peer with lower TD than all should be rejected.
	cd.AddPeer(PeerInfo{ID: "p5", TotalDifficulty: 50})
	if cd.GetPeer("p5") != nil {
		t.Error("p5 should not have been added (TD too low)")
	}
}

func TestChainDownloaderSelectBestPeer(t *testing.T) {
	cd := NewChainDownloader(nil)

	_, err := cd.SelectBestPeer()
	if !errors.Is(err, ErrChainNoPeers) {
		t.Errorf("expected ErrChainNoPeers, got %v", err)
	}

	cd.AddPeer(PeerInfo{ID: "low", TotalDifficulty: 10})
	cd.AddPeer(PeerInfo{ID: "high", TotalDifficulty: 999})
	cd.AddPeer(PeerInfo{ID: "mid", TotalDifficulty: 500})

	best, err := cd.SelectBestPeer()
	if err != nil {
		t.Fatalf("SelectBestPeer: %v", err)
	}
	if best != "high" {
		t.Errorf("best peer = %s, want high", best)
	}
}

func TestChainDownloaderRemovePeer(t *testing.T) {
	cd := NewChainDownloader(nil)
	cd.AddPeer(PeerInfo{ID: "p1"})
	cd.RemovePeer("p1")
	if cd.PeerCount() != 0 {
		t.Errorf("PeerCount = %d, want 0", cd.PeerCount())
	}
}

func TestChainDownloaderHandleAnnouncement(t *testing.T) {
	cd := NewChainDownloader(nil)
	cd.AddPeer(PeerInfo{ID: "p1", HeadNumber: 5})

	var hash [32]byte
	hash[0] = 0xab
	cd.HandleAnnouncement("p1", hash, 10)

	p := cd.GetPeer("p1")
	if p == nil {
		t.Fatal("peer p1 not found")
	}
	if p.HeadNumber != 10 {
		t.Errorf("HeadNumber = %d, want 10", p.HeadNumber)
	}
	if p.HeadHash != hash {
		t.Errorf("HeadHash mismatch")
	}

	anns := cd.Announcements()
	if len(anns) != 1 {
		t.Errorf("announcement count = %d, want 1", len(anns))
	}
}

func TestChainDownloaderValidateChain(t *testing.T) {
	cd := NewChainDownloader(nil)
	headers := buildChainHeaders(5)

	// Build sorted header slice.
	sorted := make([]*types.Header, 5)
	for i := 0; i < 5; i++ {
		sorted[i] = headers[uint64(i+1)]
	}

	err := cd.ValidateChain(sorted)
	if err != nil {
		t.Errorf("ValidateChain: %v", err)
	}

	// Empty should error.
	if err := cd.ValidateChain(nil); err == nil {
		t.Error("expected error for empty chain")
	}
}

func TestChainDownloaderValidateChainBadSequence(t *testing.T) {
	cd := NewChainDownloader(nil)

	// Create two headers with non-sequential numbers.
	h1 := &types.Header{Number: big.NewInt(1), Time: 1000}
	h3 := &types.Header{Number: big.NewInt(3), Time: 1024, ParentHash: h1.Hash()}

	err := cd.ValidateChain([]*types.Header{h1, h3})
	if err == nil {
		t.Error("expected error for non-sequential block numbers")
	}
}

func TestChainDownloaderDownload(t *testing.T) {
	headers := buildChainHeaders(10)
	hs := &mockChainHeaderSource{headers: headers}
	bs := &mockChainBodySource{}

	cd := NewChainDownloader(&DownloadConfig{
		BatchSize: 5,
		Timeout:   5 * time.Second,
	})
	cd.SetSources(hs, bs)

	ctx := context.Background()
	err := cd.Download(ctx, 1, 10)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	p := cd.Progress()
	if p.CurrentBlock != 10 {
		t.Errorf("CurrentBlock = %d, want 10", p.CurrentBlock)
	}
}

func TestChainDownloaderDownloadBadRange(t *testing.T) {
	cd := NewChainDownloader(nil)
	cd.SetSources(&mockChainHeaderSource{}, &mockChainBodySource{})

	err := cd.Download(context.Background(), 10, 5)
	if !errors.Is(err, ErrChainBadRange) {
		t.Errorf("expected ErrChainBadRange, got %v", err)
	}
}

func TestChainDownloaderDownloadCancellation(t *testing.T) {
	// Use a slow header source to test cancellation.
	slow := &cdSlowHeaderSource{delay: 500 * time.Millisecond}
	cd := NewChainDownloader(&DownloadConfig{
		BatchSize: 5,
		Timeout:   2 * time.Second,
	})
	cd.SetSources(slow, &mockChainBodySource{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := cd.Download(ctx, 1, 100)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// cdSlowHeaderSource adds a delay to header fetching for cancel testing.
// Named differently from slowHeaderSource in sync_test.go to avoid conflicts.
type cdSlowHeaderSource struct {
	delay time.Duration
}

func (s *cdSlowHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	time.Sleep(s.delay)
	return nil, errors.New("slow source")
}

func TestChainDownloaderHighestPeerBlock(t *testing.T) {
	cd := NewChainDownloader(nil)
	if h := cd.HighestPeerBlock(); h != 0 {
		t.Errorf("HighestPeerBlock = %d, want 0 with no peers", h)
	}

	cd.AddPeer(PeerInfo{ID: "p1", HeadNumber: 50})
	cd.AddPeer(PeerInfo{ID: "p2", HeadNumber: 200})
	cd.AddPeer(PeerInfo{ID: "p3", HeadNumber: 100})

	if h := cd.HighestPeerBlock(); h != 200 {
		t.Errorf("HighestPeerBlock = %d, want 200", h)
	}
}

func TestChainDownloaderProcessBatch(t *testing.T) {
	cd := NewChainDownloader(nil)

	h := &types.Header{Number: big.NewInt(1)}
	body := &types.Body{}

	// Matching counts: OK.
	err := cd.ProcessBatch([]*types.Header{h}, []*types.Body{body})
	if err != nil {
		t.Errorf("ProcessBatch: %v", err)
	}

	// Mismatched counts: error.
	err = cd.ProcessBatch([]*types.Header{h}, []*types.Body{body, body})
	if err == nil {
		t.Error("expected error for mismatched counts")
	}

	// Empty: error.
	err = cd.ProcessBatch(nil, nil)
	if err == nil {
		t.Error("expected error for empty batch")
	}
}
