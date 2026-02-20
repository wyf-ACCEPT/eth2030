package sync

import (
	"errors"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// dlMockHeaderSource is a HeaderSource mock for downloader-specific tests,
// distinct from mockHeaderSource in sync_test.go.
type dlMockHeaderSource struct {
	headers []*types.Header
}

func (m *dlMockHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	var result []*types.Header
	for _, h := range m.headers {
		if h.Number.Uint64() >= from && h.Number.Uint64() < from+uint64(count) {
			result = append(result, h)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("dl: no headers")
	}
	return result, nil
}

// dlMockBodySource is a BodySource mock for downloader-specific tests,
// distinct from mockBodySource in sync_test.go.
type dlMockBodySource struct {
	bodies map[types.Hash]*types.Body
}

func (m *dlMockBodySource) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	result := make([]*types.Body, len(hashes))
	for i, h := range hashes {
		if b, ok := m.bodies[h]; ok {
			result[i] = b
		} else {
			result[i] = &types.Body{}
		}
	}
	return result, nil
}

// --- Downloader configuration tests ---

func TestDownloader_DefaultConfig(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	if cfg == nil {
		t.Fatal("DefaultDownloaderConfig returned nil")
	}
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("timeout: want %v, got %v", DefaultRequestTimeout, cfg.RequestTimeout)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Errorf("max retries: want %d, got %d", DefaultMaxRetries, cfg.MaxRetries)
	}
	if cfg.BanThreshold != DefaultBanThreshold {
		t.Errorf("ban threshold: want %d, got %d", DefaultBanThreshold, cfg.BanThreshold)
	}
	if cfg.SyncConfig == nil {
		t.Fatal("SyncConfig should not be nil in default config")
	}
}

func TestDownloader_NilConfig(t *testing.T) {
	dl := NewDownloader(nil)
	if dl == nil {
		t.Fatal("NewDownloader(nil) returned nil")
	}
	if dl.config == nil {
		t.Fatal("downloader config should default when nil passed")
	}
	if dl.config.MaxRetries != DefaultMaxRetries {
		t.Errorf("max retries: want %d, got %d", DefaultMaxRetries, dl.config.MaxRetries)
	}
}

func TestDownloader_InitialState(t *testing.T) {
	dl := NewDownloader(nil)
	if dl.IsSyncing() {
		t.Fatal("new downloader should not be syncing")
	}
	prog := dl.Progress()
	if prog.Syncing {
		t.Fatal("progress should not show syncing")
	}
}

// --- Peer failure tracking tests ---

func TestDownloader_RecordPeerFailureBelowThreshold(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 5
	dl := NewDownloader(cfg)

	for i := 0; i < 4; i++ {
		dl.RecordPeerFailure("peer-alpha")
	}
	if dl.IsPeerBanned("peer-alpha") {
		t.Fatal("peer should not be banned below threshold")
	}
}

func TestDownloader_RecordPeerFailureExactThreshold(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 3
	dl := NewDownloader(cfg)

	dl.RecordPeerFailure("peer-beta")
	dl.RecordPeerFailure("peer-beta")
	dl.RecordPeerFailure("peer-beta")

	if !dl.IsPeerBanned("peer-beta") {
		t.Fatal("peer should be banned at exact threshold")
	}
}

func TestDownloader_MultiplePeersIndependent(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 2
	dl := NewDownloader(cfg)

	dl.RecordPeerFailure("peer-a")
	dl.RecordPeerFailure("peer-b")

	if dl.IsPeerBanned("peer-a") {
		t.Fatal("peer-a should not be banned after 1 failure")
	}
	if dl.IsPeerBanned("peer-b") {
		t.Fatal("peer-b should not be banned after 1 failure")
	}

	dl.RecordPeerFailure("peer-a")
	if !dl.IsPeerBanned("peer-a") {
		t.Fatal("peer-a should be banned after 2 failures")
	}
	if dl.IsPeerBanned("peer-b") {
		t.Fatal("peer-b should still not be banned")
	}
}

func TestDownloader_ResetPeerClearsState(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 2
	dl := NewDownloader(cfg)

	dl.RecordPeerFailure("peer-c")
	dl.RecordPeerFailure("peer-c")
	if !dl.IsPeerBanned("peer-c") {
		t.Fatal("peer-c should be banned")
	}

	dl.ResetPeer("peer-c")
	if dl.IsPeerBanned("peer-c") {
		t.Fatal("peer-c should not be banned after reset")
	}

	// After reset, failures should start fresh.
	dl.RecordPeerFailure("peer-c")
	if dl.IsPeerBanned("peer-c") {
		t.Fatal("peer-c should not be banned after single failure post-reset")
	}
}

func TestDownloader_ResetNonexistentPeer(t *testing.T) {
	dl := NewDownloader(nil)
	// Should not panic.
	dl.ResetPeer("nonexistent")
	if dl.IsPeerBanned("nonexistent") {
		t.Fatal("nonexistent peer should not be banned")
	}
}

// --- Cancel tests ---

func TestDownloader_CancelSafety(t *testing.T) {
	dl := NewDownloader(nil)
	// Cancel on a non-started downloader should not panic.
	dl.Cancel()
	dl.Cancel() // double cancel
}

// --- Syncer access tests ---

func TestDownloader_SyncerAccessor(t *testing.T) {
	dl := NewDownloader(nil)
	if dl.Syncer() == nil {
		t.Fatal("Syncer() should not be nil")
	}
}

// --- Timeout fetcher tests ---

func TestDownloader_TimeoutHeaderFetcherSuccess(t *testing.T) {
	genesis, headers := buildTestChain(3)
	_ = genesis

	inner := newMockHeaderSource()
	for _, h := range headers {
		inner.addHeader(h)
	}
	tf := NewTimeoutHeaderFetcher(inner, 5*time.Second)

	result, err := tf.FetchHeaders(1, 3)
	if err != nil {
		t.Fatalf("FetchHeaders: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("want 3 headers, got %d", len(result))
	}
}

func TestDownloader_TimeoutBodyFetcherSuccess(t *testing.T) {
	genesis, headers := buildTestChain(2)
	_ = genesis

	inner := newMockBodySource()
	hashes := make([]types.Hash, len(headers))
	for i, h := range headers {
		inner.addBody(h.Hash(), &types.Body{})
		hashes[i] = h.Hash()
	}

	tf := NewTimeoutBodyFetcher(inner, 5*time.Second)
	result, err := tf.FetchBodies(hashes)
	if err != nil {
		t.Fatalf("FetchBodies: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("want 2 bodies, got %d", len(result))
	}
}

// --- Error constants ---

func TestDownloader_ErrorConstants(t *testing.T) {
	if ErrDownloaderRunning == nil {
		t.Fatal("ErrDownloaderRunning should not be nil")
	}
	if ErrPeerBanned == nil {
		t.Fatal("ErrPeerBanned should not be nil")
	}
	if ErrAllPeersBanned == nil {
		t.Fatal("ErrAllPeersBanned should not be nil")
	}
	if ErrMaxRetries == nil {
		t.Fatal("ErrMaxRetries should not be nil")
	}
}

// --- SyncProgress fields ---

func TestDownloader_ProgressFields(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := DefaultDownloaderConfig()
	cfg.SyncConfig.BatchSize = 5
	cfg.SyncConfig.BodyBatchSize = 5

	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)

	err := dl.Start(5)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	prog := dl.Progress()
	if prog.CurrentBlock != 5 {
		t.Errorf("current block: want 5, got %d", prog.CurrentBlock)
	}
	if prog.HighestBlock != 5 {
		t.Errorf("highest block: want 5, got %d", prog.HighestBlock)
	}
	if prog.PulledHeaders != 5 {
		t.Errorf("pulled headers: want 5, got %d", prog.PulledHeaders)
	}
	if prog.PulledBodies != 5 {
		t.Errorf("pulled bodies: want 5, got %d", prog.PulledBodies)
	}
}
