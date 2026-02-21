package p2p

import (
	"sync"
	"testing"
	"time"
)

func newTestBandwidthTracker() *BandwidthTracker {
	return NewBandwidthTracker(DefaultBandwidthTrackerConfig())
}

func TestBandwidthTracker_DefaultConfig(t *testing.T) {
	cfg := DefaultBandwidthTrackerConfig()
	if cfg.GlobalUploadLimit != DefaultGlobalUploadLimit {
		t.Errorf("GlobalUploadLimit = %d, want %d", cfg.GlobalUploadLimit, DefaultGlobalUploadLimit)
	}
	if cfg.GlobalDownloadLimit != DefaultGlobalDownloadLimit {
		t.Errorf("GlobalDownloadLimit = %d, want %d", cfg.GlobalDownloadLimit, DefaultGlobalDownloadLimit)
	}
	if cfg.PerPeerUploadLimit != DefaultPerPeerUploadLimit {
		t.Errorf("PerPeerUploadLimit = %d, want %d", cfg.PerPeerUploadLimit, DefaultPerPeerUploadLimit)
	}
	if cfg.PerPeerDownloadLimit != DefaultPerPeerDownloadLimit {
		t.Errorf("PerPeerDownloadLimit = %d, want %d", cfg.PerPeerDownloadLimit, DefaultPerPeerDownloadLimit)
	}
	if cfg.WindowSize != DefaultWindowSize {
		t.Errorf("WindowSize = %v, want %v", cfg.WindowSize, DefaultWindowSize)
	}
	if cfg.BucketCount != DefaultBucketCount {
		t.Errorf("BucketCount = %d, want %d", cfg.BucketCount, DefaultBucketCount)
	}
}

func TestBandwidthTracker_RegisterRemovePeer(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")
	bt.RegisterPeer("p2")

	if bt.PeerCount() != 2 {
		t.Errorf("PeerCount = %d, want 2", bt.PeerCount())
	}

	bt.RemovePeer("p1")
	if bt.PeerCount() != 1 {
		t.Errorf("PeerCount after remove = %d, want 1", bt.PeerCount())
	}

	// Re-register should not panic.
	bt.RegisterPeer("p2")
	if bt.PeerCount() != 1 {
		t.Errorf("PeerCount after re-register = %d, want 1", bt.PeerCount())
	}
}

func TestBandwidthTracker_RecordUpload(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")

	err := bt.RecordUpload("p1", 1024, BandwidthPriorityBlocks)
	if err != nil {
		t.Fatalf("RecordUpload: %v", err)
	}

	stats, err := bt.PeerStats("p1")
	if err != nil {
		t.Fatalf("PeerStats: %v", err)
	}
	if stats.TotalUp != 1024 {
		t.Errorf("TotalUp = %d, want 1024", stats.TotalUp)
	}
}

func TestBandwidthTracker_RecordDownload(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")

	err := bt.RecordDownload("p1", 2048, BandwidthPriorityTxs)
	if err != nil {
		t.Fatalf("RecordDownload: %v", err)
	}

	stats, err := bt.PeerStats("p1")
	if err != nil {
		t.Fatalf("PeerStats: %v", err)
	}
	if stats.TotalDown != 2048 {
		t.Errorf("TotalDown = %d, want 2048", stats.TotalDown)
	}
}

func TestBandwidthTracker_UnknownPeer(t *testing.T) {
	bt := newTestBandwidthTracker()

	err := bt.RecordUpload("ghost", 100, BandwidthPriorityConsensus)
	if err != ErrBWUnknownPeer {
		t.Errorf("RecordUpload(unknown) = %v, want ErrBWUnknownPeer", err)
	}

	err = bt.RecordDownload("ghost", 100, BandwidthPriorityConsensus)
	if err != ErrBWUnknownPeer {
		t.Errorf("RecordDownload(unknown) = %v, want ErrBWUnknownPeer", err)
	}

	_, err = bt.PeerStats("ghost")
	if err != ErrBWUnknownPeer {
		t.Errorf("PeerStats(unknown) = %v, want ErrBWUnknownPeer", err)
	}
}

func TestBandwidthTracker_PerPeerUploadLimit(t *testing.T) {
	cfg := BandwidthTrackerConfig{
		GlobalUploadLimit:    1000000,
		GlobalDownloadLimit:  1000000,
		PerPeerUploadLimit:   1000, // 1000 bytes/sec per peer.
		PerPeerDownloadLimit: 1000000,
		WindowSize:           time.Second,
		BucketCount:          1,
	}
	bt := NewBandwidthTracker(cfg)
	bt.RegisterPeer("p1")

	// First upload within limit.
	err := bt.RecordUpload("p1", 500, BandwidthPriorityBlocks)
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}

	// Second upload exceeds per-peer limit.
	err = bt.RecordUpload("p1", 600, BandwidthPriorityBlocks)
	if err != ErrBWPeerUploadLimit {
		t.Errorf("over-limit upload = %v, want ErrBWPeerUploadLimit", err)
	}
}

func TestBandwidthTracker_PerPeerDownloadLimit(t *testing.T) {
	cfg := BandwidthTrackerConfig{
		GlobalUploadLimit:    1000000,
		GlobalDownloadLimit:  1000000,
		PerPeerUploadLimit:   1000000,
		PerPeerDownloadLimit: 1000, // 1000 bytes/sec per peer.
		WindowSize:           time.Second,
		BucketCount:          1,
	}
	bt := NewBandwidthTracker(cfg)
	bt.RegisterPeer("p1")

	err := bt.RecordDownload("p1", 500, BandwidthPriorityBlocks)
	if err != nil {
		t.Fatalf("first download: %v", err)
	}

	err = bt.RecordDownload("p1", 600, BandwidthPriorityBlocks)
	if err != ErrBWPeerDownloadLimit {
		t.Errorf("over-limit download = %v, want ErrBWPeerDownloadLimit", err)
	}
}

func TestBandwidthTracker_GlobalUploadLimit(t *testing.T) {
	cfg := BandwidthTrackerConfig{
		GlobalUploadLimit:    1000, // 1000 bytes/sec global.
		GlobalDownloadLimit:  1000000,
		PerPeerUploadLimit:   1000000,
		PerPeerDownloadLimit: 1000000,
		WindowSize:           time.Second,
		BucketCount:          1,
	}
	bt := NewBandwidthTracker(cfg)
	bt.RegisterPeer("p1")
	bt.RegisterPeer("p2")

	// Use priority -1 to skip priority checks and test only global limit.
	err := bt.RecordUpload("p1", 600, -1)
	if err != nil {
		t.Fatalf("p1 upload: %v", err)
	}

	// Second peer's upload exceeds global limit.
	err = bt.RecordUpload("p2", 500, -1)
	if err != ErrBWGlobalUploadLimit {
		t.Errorf("global over-limit = %v, want ErrBWGlobalUploadLimit", err)
	}
}

func TestBandwidthTracker_GlobalDownloadLimit(t *testing.T) {
	cfg := BandwidthTrackerConfig{
		GlobalUploadLimit:    1000000,
		GlobalDownloadLimit:  1000, // 1000 bytes/sec global.
		PerPeerUploadLimit:   1000000,
		PerPeerDownloadLimit: 1000000,
		WindowSize:           time.Second,
		BucketCount:          1,
	}
	bt := NewBandwidthTracker(cfg)
	bt.RegisterPeer("p1")
	bt.RegisterPeer("p2")

	// Priority is not enforced for downloads, so use any valid priority.
	err := bt.RecordDownload("p1", 600, -1)
	if err != nil {
		t.Fatalf("p1 download: %v", err)
	}

	err = bt.RecordDownload("p2", 500, -1)
	if err != ErrBWGlobalDownloadLimit {
		t.Errorf("global over-limit = %v, want ErrBWGlobalDownloadLimit", err)
	}
}

func TestBandwidthTracker_PriorityAllocation(t *testing.T) {
	cfg := BandwidthTrackerConfig{
		GlobalUploadLimit:    10000,
		GlobalDownloadLimit:  10000,
		PerPeerUploadLimit:   10000,
		PerPeerDownloadLimit: 10000,
		WindowSize:           time.Second,
		BucketCount:          1,
	}
	bt := NewBandwidthTracker(cfg)
	bt.RegisterPeer("p1")

	// Consensus has 40% share = 4000 bytes/sec.
	err := bt.RecordUpload("p1", 3500, BandwidthPriorityConsensus)
	if err != nil {
		t.Fatalf("consensus within limit: %v", err)
	}

	// Exceeding consensus allocation.
	err = bt.RecordUpload("p1", 1000, BandwidthPriorityConsensus)
	if err != ErrBWPriorityExhausted {
		t.Errorf("consensus over-limit = %v, want ErrBWPriorityExhausted", err)
	}

	// Blobs have 10% share = 1000 bytes/sec.
	err = bt.RecordUpload("p1", 900, BandwidthPriorityBlobs)
	if err != nil {
		t.Fatalf("blobs within limit: %v", err)
	}

	err = bt.RecordUpload("p1", 200, BandwidthPriorityBlobs)
	if err != ErrBWPriorityExhausted {
		t.Errorf("blobs over-limit = %v, want ErrBWPriorityExhausted", err)
	}
}

func TestBandwidthTracker_PriorityShares(t *testing.T) {
	bt := newTestBandwidthTracker()

	if share := bt.PriorityShare(BandwidthPriorityConsensus); share != priorityShareConsensus {
		t.Errorf("consensus share = %d, want %d", share, priorityShareConsensus)
	}
	if share := bt.PriorityShare(BandwidthPriorityBlocks); share != priorityShareBlocks {
		t.Errorf("blocks share = %d, want %d", share, priorityShareBlocks)
	}
	if share := bt.PriorityShare(BandwidthPriorityTxs); share != priorityShareTxs {
		t.Errorf("txs share = %d, want %d", share, priorityShareTxs)
	}
	if share := bt.PriorityShare(BandwidthPriorityBlobs); share != priorityShareBlobs {
		t.Errorf("blobs share = %d, want %d", share, priorityShareBlobs)
	}

	// Invalid priority returns 0.
	if share := bt.PriorityShare(-1); share != 0 {
		t.Errorf("invalid priority share = %d, want 0", share)
	}
	if share := bt.PriorityShare(99); share != 0 {
		t.Errorf("out-of-range priority share = %d, want 0", share)
	}
}

func TestBandwidthTracker_PriorityName(t *testing.T) {
	tests := []struct {
		priority int
		want     string
	}{
		{BandwidthPriorityConsensus, "consensus"},
		{BandwidthPriorityBlocks, "blocks"},
		{BandwidthPriorityTxs, "transactions"},
		{BandwidthPriorityBlobs, "blobs"},
		{-1, "unknown"},
		{99, "unknown"},
	}
	for _, tt := range tests {
		got := PriorityName(tt.priority)
		if got != tt.want {
			t.Errorf("PriorityName(%d) = %q, want %q", tt.priority, got, tt.want)
		}
	}
}

func TestBandwidthTracker_GlobalStats(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")
	bt.RegisterPeer("p2")

	_ = bt.RecordUpload("p1", 1000, BandwidthPriorityConsensus)
	_ = bt.RecordDownload("p2", 2000, BandwidthPriorityBlocks)

	stats := bt.GlobalStats()
	if stats.PeerCount != 2 {
		t.Errorf("PeerCount = %d, want 2", stats.PeerCount)
	}
	if stats.TotalUp != 1000 {
		t.Errorf("TotalUp = %d, want 1000", stats.TotalUp)
	}
	if stats.TotalDown != 2000 {
		t.Errorf("TotalDown = %d, want 2000", stats.TotalDown)
	}
}

func TestBandwidthTracker_AllPeerStats(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")
	bt.RegisterPeer("p2")

	_ = bt.RecordUpload("p1", 100, BandwidthPriorityTxs)
	_ = bt.RecordDownload("p2", 200, BandwidthPriorityBlobs)

	all := bt.AllPeerStats()
	if len(all) != 2 {
		t.Fatalf("AllPeerStats len = %d, want 2", len(all))
	}

	// Find p1 and p2 in results.
	found := map[string]bool{}
	for _, s := range all {
		found[s.PeerID] = true
	}
	if !found["p1"] || !found["p2"] {
		t.Errorf("missing peers in AllPeerStats: %v", found)
	}
}

func TestBandwidthTracker_RateCalculation(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")

	_ = bt.RecordUpload("p1", 5000, BandwidthPriorityBlocks)
	_ = bt.RecordDownload("p1", 3000, BandwidthPriorityBlocks)

	upRate := bt.UploadRate()
	if upRate <= 0 {
		t.Errorf("UploadRate = %f, want > 0", upRate)
	}

	dnRate := bt.DownloadRate()
	if dnRate <= 0 {
		t.Errorf("DownloadRate = %f, want > 0", dnRate)
	}
}

func TestBandwidthTracker_ConcurrentAccess(t *testing.T) {
	bt := newTestBandwidthTracker()

	// Register several peers.
	for i := 0; i < 10; i++ {
		bt.RegisterPeer(peerName(i))
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent uploads from multiple goroutines.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := peerName(idx)
			for j := 0; j < 20; j++ {
				if err := bt.RecordUpload(name, 100, idx%bandwidthPriorityCount); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}

	// Concurrent downloads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := peerName(idx)
			for j := 0; j < 20; j++ {
				if err := bt.RecordDownload(name, 100, idx%bandwidthPriorityCount); err != nil {
					errCh <- err
					return
				}
			}
		}(i)
	}

	// Concurrent stats reads.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = bt.GlobalStats()
			_ = bt.UploadRate()
			_ = bt.DownloadRate()
			_ = bt.PeerCount()
			_ = bt.AllPeerStats()
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

func peerName(i int) string {
	names := []string{"peer0", "peer1", "peer2", "peer3", "peer4",
		"peer5", "peer6", "peer7", "peer8", "peer9"}
	if i >= 0 && i < len(names) {
		return names[i]
	}
	return "peerX"
}

func TestBandwidthTracker_ConfigDefaults(t *testing.T) {
	// Zero/negative values should be replaced with defaults.
	bt := NewBandwidthTracker(BandwidthTrackerConfig{
		GlobalUploadLimit: -1, GlobalDownloadLimit: 0,
		PerPeerUploadLimit: 0, PerPeerDownloadLimit: -5,
		WindowSize: 0, BucketCount: -1,
	})
	if bt.config.GlobalUploadLimit != DefaultGlobalUploadLimit {
		t.Errorf("default GlobalUploadLimit = %d, want %d", bt.config.GlobalUploadLimit, DefaultGlobalUploadLimit)
	}
	if bt.config.PerPeerDownloadLimit != DefaultPerPeerDownloadLimit {
		t.Errorf("default PerPeerDownloadLimit = %d, want %d", bt.config.PerPeerDownloadLimit, DefaultPerPeerDownloadLimit)
	}
}

func TestSlidingWindow_Basic(t *testing.T) {
	sw := newSlidingWindow(time.Second, 1)
	now := time.Now()

	sw.add(500, now)
	if total := sw.totalBytes(now); total != 500 {
		t.Errorf("totalBytes = %d, want 500", total)
	}

	sw.add(300, now)
	if total := sw.totalBytes(now); total != 800 {
		t.Errorf("totalBytes = %d, want 800", total)
	}

	r := sw.rate(now)
	if r <= 0 {
		t.Errorf("rate = %f, want > 0", r)
	}
}

func TestSlidingWindow_DefaultBuckets(t *testing.T) {
	sw := newSlidingWindow(time.Second, 0)
	if len(sw.buckets) != DefaultBucketCount {
		t.Errorf("default buckets = %d, want %d", len(sw.buckets), DefaultBucketCount)
	}
}

func TestBandwidthTracker_PriorityRate(t *testing.T) {
	bt := newTestBandwidthTracker()
	bt.RegisterPeer("p1")

	_ = bt.RecordUpload("p1", 500, BandwidthPriorityConsensus)

	rate := bt.PriorityRate(BandwidthPriorityConsensus)
	if rate <= 0 {
		t.Errorf("PriorityRate(consensus) = %f, want > 0", rate)
	}

	// Invalid priority.
	if r := bt.PriorityRate(-1); r != 0 {
		t.Errorf("PriorityRate(-1) = %f, want 0", r)
	}
	if r := bt.PriorityRate(99); r != 0 {
		t.Errorf("PriorityRate(99) = %f, want 0", r)
	}
}

func TestBandwidthTracker_PriorityConsensusHigherThanBlobs(t *testing.T) {
	// Verify that consensus has a larger allocation than blobs.
	bt := newTestBandwidthTracker()
	cShare := bt.PriorityShare(BandwidthPriorityConsensus)
	bShare := bt.PriorityShare(BandwidthPriorityBlobs)
	if cShare <= bShare {
		t.Errorf("consensus share %d should be > blob share %d", cShare, bShare)
	}

	// Priority ordering: consensus > blocks > txs > blobs.
	blkShare := bt.PriorityShare(BandwidthPriorityBlocks)
	txShare := bt.PriorityShare(BandwidthPriorityTxs)
	if cShare <= blkShare {
		t.Errorf("consensus %d should be > blocks %d", cShare, blkShare)
	}
	if blkShare <= txShare {
		t.Errorf("blocks %d should be > txs %d", blkShare, txShare)
	}
	if txShare <= bShare {
		t.Errorf("txs %d should be > blobs %d", txShare, bShare)
	}
}
