package sync

import (
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// --- Header chain validation edge cases ---

func TestProcessHeaders_LongChain(t *testing.T) {
	genesis, headers := buildTestChain(500)
	err := ValidateHeaderChain(headers, genesis.Header())
	if err != nil {
		t.Fatalf("ValidateHeaderChain for 500 headers: %v", err)
	}
}

func TestProcessHeaders_SingleHeader(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	h := makeTestHeader(1, genesis.Header().Hash(), 1012)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if err != nil {
		t.Fatalf("single header should be valid: %v", err)
	}
}

func TestProcessHeaders_NonContiguousAtFirst(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	// Header with number 2 instead of 1 (skips block 1).
	h := makeTestHeader(2, genesis.Header().Hash(), 1012)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if !errors.Is(err, ErrBadBlockNumber) {
		t.Fatalf("want ErrBadBlockNumber for non-contiguous first header, got %v", err)
	}
}

func TestProcessHeaders_BadParentHashAtFirst(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	h := makeTestHeader(1, types.Hash{0xde, 0xad}, 1012)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if !errors.Is(err, ErrBadParentHash) {
		t.Fatalf("want ErrBadParentHash for first header, got %v", err)
	}
}

func TestProcessHeaders_TimestampExactlyAtLimit(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	// Timestamp exactly at the future limit (now + maxFutureTimestamp).
	now := uint64(time.Now().Unix())
	h := makeTestHeader(1, genesis.Header().Hash(), now+maxFutureTimestamp)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if err != nil {
		t.Fatalf("timestamp at exact limit should pass: %v", err)
	}
}

func TestProcessHeaders_TimestampOneOverLimit(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	now := uint64(time.Now().Unix())
	h := makeTestHeader(1, genesis.Header().Hash(), now+maxFutureTimestamp+1)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if !errors.Is(err, ErrFutureTimestamp) {
		t.Fatalf("want ErrFutureTimestamp for timestamp one over limit, got %v", err)
	}
}

func TestProcessHeaders_ErrorAtMiddleOfChain(t *testing.T) {
	genesis, headers := buildTestChain(5)
	// Corrupt the third header's parent hash.
	headers[2] = makeTestHeader(3, types.Hash{0xba, 0xad}, headers[2].Time)
	err := ValidateHeaderChain(headers, genesis.Header())
	if !errors.Is(err, ErrBadParentHash) {
		t.Fatalf("want ErrBadParentHash at header[2], got %v", err)
	}
}

func TestProcessHeaders_DecreasingTimestamp(t *testing.T) {
	genesis, headers := buildTestChain(3)
	// Make header[2] have a timestamp earlier than header[1].
	headers[2] = makeTestHeader(3, headers[1].Hash(), headers[1].Time-1)
	err := ValidateHeaderChain(headers, genesis.Header())
	if !errors.Is(err, ErrTimestampOrder) {
		t.Fatalf("want ErrTimestampOrder for decreasing timestamp, got %v", err)
	}
}

func TestProcessHeaders_AllSameTimestamp(t *testing.T) {
	// Equal timestamps are allowed (fast blocks).
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	h1 := makeTestHeader(1, genesis.Header().Hash(), 1000)
	h2 := makeTestHeader(2, h1.Hash(), 1000)
	h3 := makeTestHeader(3, h2.Hash(), 1000)
	err := ValidateHeaderChain([]*types.Header{h1, h2, h3}, genesis.Header())
	if err != nil {
		t.Fatalf("equal timestamps should be valid: %v", err)
	}
}

// --- Body-header matching edge cases ---

func TestAssembleBlocks_SingleBlock(t *testing.T) {
	_, headers := buildTestChain(1)
	bodies := []*types.Body{{}}
	blocks, err := AssembleBlocks(headers, bodies)
	if err != nil {
		t.Fatalf("AssembleBlocks single: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks: want 1, got %d", len(blocks))
	}
	if blocks[0].NumberU64() != 1 {
		t.Fatalf("block number: want 1, got %d", blocks[0].NumberU64())
	}
}

func TestAssembleBlocks_Empty(t *testing.T) {
	blocks, err := AssembleBlocks([]*types.Header{}, []*types.Body{})
	if err != nil {
		t.Fatalf("AssembleBlocks empty: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("blocks: want 0, got %d", len(blocks))
	}
}

func TestAssembleBlocks_MoreBodiesThanHeaders(t *testing.T) {
	_, headers := buildTestChain(2)
	bodies := make([]*types.Body, 3) // one extra
	for i := range bodies {
		bodies[i] = &types.Body{}
	}
	_, err := AssembleBlocks(headers, bodies)
	if !errors.Is(err, ErrBodyHeaderCount) {
		t.Fatalf("want ErrBodyHeaderCount, got %v", err)
	}
}

// --- HeaderFetcher extended tests ---

func TestHeaderFetcher_DeliverErrorReported(t *testing.T) {
	f := NewHeaderFetcher(192)
	peer := PeerID("peer1")
	f.Request(peer, 1, 10)

	// Deliver an error.
	testErr := errors.New("connection reset")
	f.DeliverError(peer, testErr)

	if f.PendingCount() != 0 {
		t.Fatalf("pending after error: want 0, got %d", f.PendingCount())
	}

	select {
	case resp := <-f.Results():
		if resp.Err == nil {
			t.Fatal("expected error in response")
		}
		if resp.Err.Error() != "connection reset" {
			t.Fatalf("error message: want 'connection reset', got %q", resp.Err.Error())
		}
		if resp.PeerID != peer {
			t.Fatalf("peer: want %s, got %s", peer, resp.PeerID)
		}
	default:
		t.Fatal("expected result in channel")
	}
}

func TestHeaderFetcher_DeliverErrorUnknownPeer(t *testing.T) {
	f := NewHeaderFetcher(192)
	// Delivering error for unknown peer should not panic.
	f.DeliverError(PeerID("ghost"), errors.New("some error"))

	// Should still produce a response (the implementation always sends to results).
	select {
	case resp := <-f.Results():
		if resp.PeerID != "ghost" {
			t.Fatalf("peer: want ghost, got %s", resp.PeerID)
		}
	default:
		t.Fatal("expected result")
	}
}

func TestHeaderFetcher_HasPending(t *testing.T) {
	f := NewHeaderFetcher(192)
	peer1 := PeerID("peer1")
	peer2 := PeerID("peer2")

	if f.HasPending(peer1) {
		t.Fatal("should not have pending for peer1")
	}

	f.Request(peer1, 1, 10)
	if !f.HasPending(peer1) {
		t.Fatal("should have pending for peer1")
	}
	if f.HasPending(peer2) {
		t.Fatal("should not have pending for peer2")
	}
}

func TestHeaderFetcher_MaxBatchCapping(t *testing.T) {
	f := NewHeaderFetcher(10)
	peer := PeerID("peer1")
	f.Request(peer, 1, 1000)

	// The request should be accepted but internally capped.
	if f.PendingCount() != 1 {
		t.Fatalf("pending: want 1, got %d", f.PendingCount())
	}
}

func TestHeaderFetcher_MultiplePeers(t *testing.T) {
	f := NewHeaderFetcher(192)
	peers := []PeerID{"peer1", "peer2", "peer3"}

	for i, p := range peers {
		if err := f.Request(p, uint64(i*10+1), 10); err != nil {
			t.Fatalf("Request(%s): %v", p, err)
		}
	}

	if f.PendingCount() != 3 {
		t.Fatalf("pending: want 3, got %d", f.PendingCount())
	}

	// Deliver in reverse order.
	for i := len(peers) - 1; i >= 0; i-- {
		f.Deliver(peers[i], []HeaderData{{Number: uint64(i)}})
	}

	if f.PendingCount() != 0 {
		t.Fatalf("pending after all delivers: want 0, got %d", f.PendingCount())
	}
}

// --- BodyFetcher extended tests ---

func TestBodyFetcher_DuplicateRequest(t *testing.T) {
	f := NewBodyFetcher(128)
	peer := PeerID("peer1")

	if err := f.Request(peer, 1, 5); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if err := f.Request(peer, 6, 5); err == nil {
		t.Fatal("duplicate request should fail")
	}
}

func TestBodyFetcher_MaxBatchCapping(t *testing.T) {
	f := NewBodyFetcher(10)
	peer := PeerID("peer1")
	if err := f.Request(peer, 1, 1000); err != nil {
		t.Fatalf("Request: %v", err)
	}
	if f.PendingCount() != 1 {
		t.Fatalf("pending: want 1, got %d", f.PendingCount())
	}
}

func TestBodyFetcher_DeliverUnknown(t *testing.T) {
	f := NewBodyFetcher(128)
	err := f.Deliver(PeerID("ghost"), nil)
	if err == nil {
		t.Fatal("deliver to unknown peer should fail")
	}
}

func TestBodyFetcher_MultiplePeers(t *testing.T) {
	f := NewBodyFetcher(128)
	peers := []PeerID{"a", "b", "c"}

	for _, p := range peers {
		f.Request(p, 1, 5)
	}
	if f.PendingCount() != 3 {
		t.Fatalf("pending: want 3, got %d", f.PendingCount())
	}

	// Deliver for each.
	for _, p := range peers {
		f.Deliver(p, []BlockData{{Number: 1}})
	}
	if f.PendingCount() != 0 {
		t.Fatalf("pending: want 0, got %d", f.PendingCount())
	}
}

// --- Syncer ProcessHeaders/ProcessBlocks error handling ---

func TestSyncer_ProcessHeaders_NoCallback(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	s.Start(100)

	// insertHeaders is nil.
	_, err := s.ProcessHeaders([]HeaderData{{Number: 1}})
	if err == nil {
		t.Fatal("ProcessHeaders with nil callback should error")
	}
}

func TestSyncer_ProcessBlocks_NoCallback(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	s.Start(100)

	// insertBlocks is nil.
	_, err := s.ProcessBlocks([]BlockData{{Number: 1}})
	if err == nil {
		t.Fatal("ProcessBlocks with nil callback should error")
	}
}

func TestSyncer_ProcessBlocks_PartialInsert(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(
		nil,
		func(blocks []BlockData) (int, error) {
			// Only insert 2 of 3 blocks.
			return 2, errors.New("insert partial")
		},
		func() uint64 { return 0 },
		nil,
	)
	s.Start(100)

	blocks := []BlockData{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	}
	n, err := s.ProcessBlocks(blocks)
	if n != 2 {
		t.Fatalf("processed: want 2, got %d", n)
	}
	if err == nil {
		t.Fatal("expected error from partial insert")
	}

	prog := s.GetProgress()
	if prog.PulledBodies != 2 {
		t.Fatalf("pulled bodies: want 2, got %d", prog.PulledBodies)
	}
	if prog.CurrentBlock != 2 {
		t.Fatalf("current block: want 2, got %d", prog.CurrentBlock)
	}
}

func TestSyncer_ProcessHeaders_PartialInsert(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(
		func(headers []HeaderData) (int, error) {
			return 1, errors.New("failed after 1")
		},
		nil,
		func() uint64 { return 0 },
		nil,
	)
	s.Start(100)

	headers := []HeaderData{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	}
	n, err := s.ProcessHeaders(headers)
	if n != 1 {
		t.Fatalf("processed: want 1, got %d", n)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	prog := s.GetProgress()
	if prog.PulledHeaders != 1 {
		t.Fatalf("pulled headers: want 1, got %d", prog.PulledHeaders)
	}
	if prog.CurrentBlock != 1 {
		t.Fatalf("current block: want 1, got %d", prog.CurrentBlock)
	}
}

// --- Full sync: body fetch error mid-sync ---

func TestRunSync_BodyFetchError(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	bs.errAt = 1 // fail on first body fetch

	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     5,
		BodyBatchSize: 5,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(5)
	if err == nil {
		t.Fatal("expected error from body fetch failure")
	}
}

// --- Full sync with zero batch sizes (should use defaults) ---

func TestRunSync_ZeroBatchSizes(t *testing.T) {
	genesis, headers := buildTestChain(10)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     0, // should use DefaultHeaderBatch
		BodyBatchSize: 0, // should use DefaultBodyBatch
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(10)
	if err != nil {
		t.Fatalf("RunSync with zero batch sizes: %v", err)
	}
	if inserter.CurrentBlock().NumberU64() != 10 {
		t.Fatalf("head: want 10, got %d", inserter.CurrentBlock().NumberU64())
	}
}

// --- Downloader: concurrent progress reads ---

func TestDownloader_ConcurrentProgress(t *testing.T) {
	genesis, headers := buildTestChain(10)
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

	// Read progress concurrently while syncing.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dl.Start(10)
	}()

	// Poll progress a few times.
	for i := 0; i < 10; i++ {
		_ = dl.Progress()
		time.Sleep(1 * time.Millisecond)
	}
	wg.Wait()

	prog := dl.Progress()
	if prog.CurrentBlock != 10 {
		t.Fatalf("final current: want 10, got %d", prog.CurrentBlock)
	}
}

// --- Downloader: peer failure tracking edge cases ---

func TestDownloader_PeerBan_MultiplePeers(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 2
	dl := NewDownloader(cfg)

	// Ban peer1 but not peer2.
	dl.RecordPeerFailure("peer1")
	dl.RecordPeerFailure("peer1")
	dl.RecordPeerFailure("peer2")

	if !dl.IsPeerBanned("peer1") {
		t.Fatal("peer1 should be banned")
	}
	if dl.IsPeerBanned("peer2") {
		t.Fatal("peer2 should not be banned (only 1 failure)")
	}
}

func TestDownloader_ResetPeer_NonExistent(t *testing.T) {
	dl := NewDownloader(DefaultDownloaderConfig())
	// Should not panic.
	dl.ResetPeer("nonexistent")
	if dl.IsPeerBanned("nonexistent") {
		t.Fatal("nonexistent peer should not be banned")
	}
}

func TestDownloader_PeerBan_ResetAndReBan(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 2
	dl := NewDownloader(cfg)

	dl.RecordPeerFailure("peer1")
	dl.RecordPeerFailure("peer1")
	if !dl.IsPeerBanned("peer1") {
		t.Fatal("peer1 should be banned after 2 failures")
	}

	dl.ResetPeer("peer1")
	if dl.IsPeerBanned("peer1") {
		t.Fatal("peer1 should not be banned after reset")
	}

	// Need threshold again to ban.
	dl.RecordPeerFailure("peer1")
	if dl.IsPeerBanned("peer1") {
		t.Fatal("peer1 should not be banned after 1 failure post-reset")
	}
	dl.RecordPeerFailure("peer1")
	if !dl.IsPeerBanned("peer1") {
		t.Fatal("peer1 should be re-banned after 2 failures")
	}
}

// --- Downloader: IsSyncing state ---

func TestDownloader_IsSyncing_WhenNotStarted(t *testing.T) {
	dl := NewDownloader(DefaultDownloaderConfig())
	if dl.IsSyncing() {
		t.Fatal("should not be syncing when not started")
	}
}

// --- DefaultDownloaderConfig values ---

func TestDefaultDownloaderConfig_Values(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	if cfg.RequestTimeout != DefaultRequestTimeout {
		t.Fatalf("RequestTimeout: want %v, got %v", DefaultRequestTimeout, cfg.RequestTimeout)
	}
	if cfg.MaxRetries != DefaultMaxRetries {
		t.Fatalf("MaxRetries: want %d, got %d", DefaultMaxRetries, cfg.MaxRetries)
	}
	if cfg.BanThreshold != DefaultBanThreshold {
		t.Fatalf("BanThreshold: want %d, got %d", DefaultBanThreshold, cfg.BanThreshold)
	}
	if cfg.SyncConfig == nil {
		t.Fatal("SyncConfig should not be nil")
	}
	if cfg.SyncConfig.Mode != ModeSnap {
		t.Fatalf("SyncConfig.Mode: want snap, got %s", cfg.SyncConfig.Mode)
	}
}

// --- TimeoutBodyFetcher success case ---

func TestTimeoutBodyFetcher_Success(t *testing.T) {
	inner := newMockBodySource()
	genesis, headers := buildTestChain(2)
	_ = genesis
	for _, h := range headers {
		inner.addBody(h.Hash(), &types.Body{})
	}

	hashes := []types.Hash{headers[0].Hash(), headers[1].Hash()}
	tf := NewTimeoutBodyFetcher(inner, 1*time.Second)
	bodies, err := tf.FetchBodies(hashes)
	if err != nil {
		t.Fatalf("FetchBodies: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("bodies: want 2, got %d", len(bodies))
	}
}

// --- Progress edge cases ---

func TestProgress_Percentage_HighNumbers(t *testing.T) {
	p := Progress{
		StartingBlock: 18_000_000,
		CurrentBlock:  19_000_000,
		HighestBlock:  20_000_000,
	}
	got := p.Percentage()
	if got != 50.0 {
		t.Fatalf("percentage: want 50.0, got %.1f", got)
	}
}

func TestProgress_Percentage_AlreadySynced(t *testing.T) {
	p := Progress{
		StartingBlock: 100,
		CurrentBlock:  100,
		HighestBlock:  100,
	}
	got := p.Percentage()
	if got != 100.0 {
		t.Fatalf("percentage when at target: want 100.0, got %.1f", got)
	}
}

// --- Syncer: stage tracking helpers ---

func TestSyncer_StageInitiallyNone(t *testing.T) {
	s := NewSyncer(nil)
	if s.Stage() != StageNone {
		t.Fatalf("initial stage: want StageNone, got %d", s.Stage())
	}
}

// --- Syncer cancel idempotent ---

func TestSyncer_CancelIdempotent(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	s.Start(100)

	// Multiple cancels should not panic.
	s.Cancel()
	s.Cancel()
	s.Cancel()

	if s.IsSyncing() {
		t.Fatal("should not be syncing after multiple cancels")
	}
}

// --- Downloader cancel without start ---

func TestDownloader_CancelWithoutStart(t *testing.T) {
	dl := NewDownloader(DefaultDownloaderConfig())
	// Should not panic.
	dl.Cancel()
}

// --- RunSync: header error at specific block ---

func TestRunSync_HeaderErrorAtSpecificBlock(t *testing.T) {
	genesis, headers := buildTestChain(10)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	hs.errAt = 6 // fail when fetching from block 6+
	bs := newMockBodySource()

	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     5,
		BodyBatchSize: 5,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(10)
	if err == nil {
		t.Fatal("expected error from header fetch at block 6")
	}
	// First batch (1-5) should have succeeded.
	if inserter.CurrentBlock().NumberU64() != 5 {
		t.Fatalf("head: want 5, got %d", inserter.CurrentBlock().NumberU64())
	}
}

// --- RunSync: non-aligned batch across boundary ---

func TestRunSync_NonAlignedBatchLastBatch(t *testing.T) {
	// Chain of 7 blocks with batch size 3 means: 3 + 3 + 1
	genesis, headers := buildTestChain(7)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     3,
		BodyBatchSize: 2,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(7)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if inserter.CurrentBlock().NumberU64() != 7 {
		t.Fatalf("head: want 7, got %d", inserter.CurrentBlock().NumberU64())
	}
}

// --- Syncer.GetProgress returns snap progress when snap syncer running ---

func TestSyncer_GetProgress_WithSnapProgress(t *testing.T) {
	s := NewSyncer(&Config{Mode: ModeSnap})
	peer := newMockSnapPeer("peer1")
	writer := newMockStateWriter()
	s.SetSnapSync(peer, writer)

	// Manually set snap syncer state.
	s.snapSyncer.running.Store(true)
	s.snapSyncer.mu.Lock()
	s.snapSyncer.progress.AccountsDone = 42
	s.snapSyncer.progress.Phase = PhaseAccounts
	s.snapSyncer.mu.Unlock()

	prog := s.GetProgress()
	if prog.SnapProgress == nil {
		t.Fatal("snap progress should not be nil when snap syncer is running")
	}
	if prog.SnapProgress.AccountsDone != 42 {
		t.Fatalf("snap accounts done: want 42, got %d", prog.SnapProgress.AccountsDone)
	}

	// Clean up.
	s.snapSyncer.running.Store(false)
}

func TestSyncer_GetProgress_WithoutSnapSyncer(t *testing.T) {
	s := NewSyncer(&Config{Mode: ModeFull})
	prog := s.GetProgress()
	if prog.SnapProgress != nil {
		t.Fatal("snap progress should be nil for full sync")
	}
}

// --- SyncProgress from Downloader reflects stage name ---

func TestDownloader_Progress_StageNameReflected(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := DefaultDownloaderConfig()
	cfg.SyncConfig.Mode = ModeFull
	cfg.SyncConfig.BatchSize = 5
	cfg.SyncConfig.BodyBatchSize = 5

	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)

	err := dl.Start(5)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	prog := dl.Progress()
	// After full sync completes, stage should be "caught up".
	if prog.Stage != "caught up" {
		t.Fatalf("stage: want 'caught up', got %q", prog.Stage)
	}
}

// --- Syncer.Mode reflects config ---

func TestNewSyncer_NilConfig(t *testing.T) {
	s := NewSyncer(nil)
	// Should use defaults.
	if s.Mode() != ModeSnap {
		t.Fatalf("nil config mode: want snap, got %s", s.Mode())
	}
}

// --- Multiple sync runs (reset state between) ---

func TestRunSync_SequentialRuns(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     5,
		BodyBatchSize: 5,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	// First run.
	err := s.RunSync(5)
	if err != nil {
		t.Fatalf("first RunSync: %v", err)
	}
	if s.State() != StateDone {
		t.Fatalf("after first run: want StateDone, got %d", s.State())
	}

	// Reset state and run again (simulating re-sync).
	s.state.Store(StateIdle)
	// Already at target, so this should complete immediately.
	err = s.RunSync(5)
	if err != nil {
		t.Fatalf("second RunSync: %v", err)
	}
}

// --- Syncer Start with inserter returning current block ---

func TestSyncer_Start_WithInserter(t *testing.T) {
	genesis, _ := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)
	// Simulate inserter already at block 5.
	h5 := makeTestHeader(5, types.Hash{}, 1060)
	b5 := types.NewBlock(h5, nil)
	inserter.current = b5

	s := NewSyncer(DefaultConfig())
	s.SetFetchers(newMockHeaderSource(), newMockBodySource(), inserter)

	if err := s.Start(100); err != nil {
		t.Fatalf("Start: %v", err)
	}

	prog := s.GetProgress()
	if prog.StartingBlock != 5 {
		t.Fatalf("starting block: want 5, got %d", prog.StartingBlock)
	}
	if prog.CurrentBlock != 5 {
		t.Fatalf("current block: want 5, got %d", prog.CurrentBlock)
	}
}

// --- Downloader: nil config uses defaults ---

func TestNewDownloader_NilConfig(t *testing.T) {
	dl := NewDownloader(nil)
	if dl.syncer == nil {
		t.Fatal("syncer should not be nil")
	}
	if dl.config == nil {
		t.Fatal("config should not be nil")
	}
	if dl.config.MaxRetries != DefaultMaxRetries {
		t.Fatalf("max retries: want %d, got %d", DefaultMaxRetries, dl.config.MaxRetries)
	}
}

// --- Verify insertion error mid-batch wraps correctly ---

func TestRunSync_InsertionError_WrapsCorrectly(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)
	inserter.errAt = 3 // fail at 3rd block total (genesis=1 + block1=2 + block2=3)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     10,
		BodyBatchSize: 10,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(5)
	if !errors.Is(err, ErrInsertionFailed) {
		t.Fatalf("want ErrInsertionFailed, got %v", err)
	}
}

// --- buildTestChain verifier ---

func TestBuildTestChain_Integrity(t *testing.T) {
	genesis, headers := buildTestChain(10)

	// Genesis should be block 0.
	if genesis.NumberU64() != 0 {
		t.Fatalf("genesis number: want 0, got %d", genesis.NumberU64())
	}

	// First header links to genesis.
	if headers[0].ParentHash != genesis.Header().Hash() {
		t.Fatal("first header parent should be genesis hash")
	}
	if headers[0].Number.Uint64() != 1 {
		t.Fatalf("first header number: want 1, got %d", headers[0].Number.Uint64())
	}

	// Each subsequent header links to previous.
	for i := 1; i < len(headers); i++ {
		if headers[i].ParentHash != headers[i-1].Hash() {
			t.Errorf("header[%d] parent hash mismatch", i)
		}
		if headers[i].Number.Uint64() != uint64(i+1) {
			t.Errorf("header[%d] number: want %d, got %d", i, i+1, headers[i].Number.Uint64())
		}
	}

	// Timestamps should be increasing.
	prevTime := genesis.Header().Time
	for i, h := range headers {
		if h.Time <= prevTime {
			t.Errorf("header[%d] timestamp %d not after previous %d", i, h.Time, prevTime)
		}
		prevTime = h.Time
	}
}

// --- encodeAccountForProof with nil balance ---

func TestEncodeAccountForProof_NilBalance(t *testing.T) {
	a := AccountData{
		Hash:     types.Hash{0x01},
		Nonce:    5,
		Balance:  nil,
		Root:     types.Hash{0xaa},
		CodeHash: types.Hash{0xbb},
	}
	encoded := encodeAccountForProof(a)
	// Should not panic and should produce valid output.
	// 8 (nonce) + 32 (balance) + 32 (root) + 32 (codehash) = 104 bytes.
	if len(encoded) != 104 {
		t.Fatalf("encoded length: want 104, got %d", len(encoded))
	}
}

func TestEncodeAccountForProof_WithBalance(t *testing.T) {
	a := AccountData{
		Hash:     types.Hash{0x01},
		Nonce:    1,
		Balance:  big.NewInt(1000),
		Root:     types.Hash{0xcc},
		CodeHash: types.Hash{0xdd},
	}
	encoded := encodeAccountForProof(a)
	if len(encoded) != 104 {
		t.Fatalf("encoded length: want 104, got %d", len(encoded))
	}
}

// --- verifyRangeProof edge cases ---

func TestVerifyRangeProof_EmptyProof(t *testing.T) {
	err := verifyRangeProof(types.Hash{0x01}, []byte{0x01}, []byte{0x02}, nil)
	if err != nil {
		t.Fatalf("empty proof should pass: %v", err)
	}
}

func TestVerifyRangeProof_BadRootHash(t *testing.T) {
	// Proof root hash won't match.
	proof := [][]byte{{0x01, 0x02, 0x03}}
	err := verifyRangeProof(types.Hash{0xff}, []byte{0x01}, []byte{0x02}, proof)
	if err == nil {
		t.Fatal("expected error for mismatched proof root")
	}
}

// --- estimateAccountSize ---

func TestEstimateAccountSize(t *testing.T) {
	a := AccountData{
		Hash:     types.Hash{0x01},
		Nonce:    1,
		Balance:  big.NewInt(100),
		Root:     types.EmptyRootHash,
		CodeHash: types.EmptyCodeHash,
	}
	size := estimateAccountSize(a)
	// 32 + 20 + 8 + 32 + 32 + 32 = 156
	if size != 156 {
		t.Fatalf("estimateAccountSize: want 156, got %d", size)
	}
}
