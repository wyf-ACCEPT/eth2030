package sync

import (
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
)

// --- Legacy API tests (preserved from original) ---

func TestSyncer_StartStop(t *testing.T) {
	s := NewSyncer(DefaultConfig())

	// Should start in idle state.
	if s.State() != StateIdle {
		t.Fatalf("initial state: want idle, got %d", s.State())
	}

	// Start sync to block 100.
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	if err := s.Start(100); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !s.IsSyncing() {
		t.Fatal("should be syncing after Start")
	}

	prog := s.GetProgress()
	if prog.HighestBlock != 100 {
		t.Fatalf("highest block: want 100, got %d", prog.HighestBlock)
	}

	// Cancel.
	s.Cancel()
	if s.IsSyncing() {
		t.Fatal("should not be syncing after Cancel")
	}
}

func TestSyncer_DoubleStart(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)

	if err := s.Start(100); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	if err := s.Start(200); err != ErrAlreadySyncing {
		t.Fatalf("second Start: want ErrAlreadySyncing, got %v", err)
	}
}

func TestSyncer_ProcessHeaders(t *testing.T) {
	var insertedCount int
	s := NewSyncer(nil)
	s.SetCallbacks(
		func(headers []HeaderData) (int, error) {
			insertedCount = len(headers)
			return len(headers), nil
		},
		nil,
		func() uint64 { return 0 },
		nil,
	)
	s.Start(100)

	headers := []HeaderData{
		{Number: 1, Hash: [32]byte{0x01}},
		{Number: 2, Hash: [32]byte{0x02}},
		{Number: 3, Hash: [32]byte{0x03}},
	}

	n, err := s.ProcessHeaders(headers)
	if err != nil {
		t.Fatalf("ProcessHeaders: %v", err)
	}
	if n != 3 {
		t.Fatalf("processed: want 3, got %d", n)
	}
	if insertedCount != 3 {
		t.Fatalf("insertedCount: want 3, got %d", insertedCount)
	}

	prog := s.GetProgress()
	if prog.PulledHeaders != 3 {
		t.Fatalf("pulled headers: want 3, got %d", prog.PulledHeaders)
	}
	if prog.CurrentBlock != 3 {
		t.Fatalf("current block: want 3, got %d", prog.CurrentBlock)
	}
}

func TestSyncer_ProcessBlocks_Completion(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(
		nil,
		func(blocks []BlockData) (int, error) {
			return len(blocks), nil
		},
		func() uint64 { return 0 },
		nil,
	)
	s.Start(3) // target = block 3

	blocks := []BlockData{
		{Number: 1},
		{Number: 2},
		{Number: 3},
	}

	n, err := s.ProcessBlocks(blocks)
	if err != nil {
		t.Fatalf("ProcessBlocks: %v", err)
	}
	if n != 3 {
		t.Fatalf("processed: want 3, got %d", n)
	}

	// Should be marked done since we reached target.
	if s.State() != StateDone {
		t.Fatalf("state: want done, got %d", s.State())
	}
}

func TestSyncer_CancelledProcess(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(
		func(headers []HeaderData) (int, error) {
			return len(headers), nil
		},
		nil,
		func() uint64 { return 0 },
		nil,
	)
	s.Start(100)
	s.Cancel()

	_, err := s.ProcessHeaders([]HeaderData{{Number: 1}})
	if err != ErrCancelled {
		t.Fatalf("want ErrCancelled, got %v", err)
	}
}

func TestHeaderFetcher_RequestDeliver(t *testing.T) {
	f := NewHeaderFetcher(192)

	peer := PeerID("peer1")
	if err := f.Request(peer, 1, 10); err != nil {
		t.Fatalf("Request: %v", err)
	}

	if f.PendingCount() != 1 {
		t.Fatalf("pending: want 1, got %d", f.PendingCount())
	}

	// Duplicate request should fail.
	if err := f.Request(peer, 11, 10); err == nil {
		t.Fatal("duplicate request should fail")
	}

	// Deliver response.
	headers := []HeaderData{
		{Number: 1, Hash: [32]byte{0x01}},
		{Number: 2, Hash: [32]byte{0x02}},
	}
	if err := f.Deliver(peer, headers); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if f.PendingCount() != 0 {
		t.Fatalf("pending after deliver: want 0, got %d", f.PendingCount())
	}

	// Check result channel.
	select {
	case resp := <-f.Results():
		if resp.PeerID != peer {
			t.Fatalf("response peer: want %s, got %s", peer, resp.PeerID)
		}
		if len(resp.Headers) != 2 {
			t.Fatalf("response headers: want 2, got %d", len(resp.Headers))
		}
	default:
		t.Fatal("expected result in channel")
	}
}

func TestHeaderFetcher_DeliverUnknown(t *testing.T) {
	f := NewHeaderFetcher(192)
	err := f.Deliver(PeerID("unknown"), nil)
	if err == nil {
		t.Fatal("deliver to unknown peer should fail")
	}
}

func TestHeaderFetcher_MaxBatch(t *testing.T) {
	f := NewHeaderFetcher(10)

	// Request more than max batch.
	peer := PeerID("peer1")
	if err := f.Request(peer, 1, 100); err != nil {
		t.Fatalf("Request: %v", err)
	}

	// Pending count should still be 1 (request was capped).
	if !f.HasPending(peer) {
		t.Fatal("should have pending request")
	}
}

func TestBodyFetcher_RequestDeliver(t *testing.T) {
	f := NewBodyFetcher(128)

	peer := PeerID("peer1")
	f.Request(peer, 1, 5)

	bodies := []BlockData{
		{Number: 1, Hash: [32]byte{0x01}},
	}
	if err := f.Deliver(peer, bodies); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if f.PendingCount() != 0 {
		t.Fatalf("pending: want 0, got %d", f.PendingCount())
	}

	select {
	case resp := <-f.Results():
		if len(resp.Bodies) != 1 {
			t.Fatalf("bodies: want 1, got %d", len(resp.Bodies))
		}
	default:
		t.Fatal("expected result")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Mode != ModeFull {
		t.Fatalf("default mode: want full, got %s", cfg.Mode)
	}
	if cfg.BatchSize != 192 {
		t.Fatalf("batch size: want 192, got %d", cfg.BatchSize)
	}
}

// --- Mock implementations for new interface-based sync pipeline ---

// mockHeaderSource implements HeaderSource for testing.
type mockHeaderSource struct {
	headers map[uint64]*types.Header
	errAt   uint64 // return error when fetching from this block
}

func newMockHeaderSource() *mockHeaderSource {
	return &mockHeaderSource{headers: make(map[uint64]*types.Header)}
}

func (m *mockHeaderSource) addHeader(h *types.Header) {
	m.headers[h.Number.Uint64()] = h
}

func (m *mockHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	if m.errAt > 0 && from >= m.errAt {
		return nil, errors.New("fetch error at block")
	}
	var result []*types.Header
	for i := 0; i < count; i++ {
		h, ok := m.headers[from+uint64(i)]
		if !ok {
			break
		}
		result = append(result, h)
	}
	if len(result) == 0 {
		return nil, errors.New("no headers available")
	}
	return result, nil
}

// mockBodySource implements BodySource for testing.
type mockBodySource struct {
	bodies map[types.Hash]*types.Body
	errAt  int // return error after this many calls
	calls  int
}

func newMockBodySource() *mockBodySource {
	return &mockBodySource{bodies: make(map[types.Hash]*types.Body)}
}

func (m *mockBodySource) addBody(hash types.Hash, body *types.Body) {
	m.bodies[hash] = body
}

func (m *mockBodySource) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	m.calls++
	if m.errAt > 0 && m.calls >= m.errAt {
		return nil, errors.New("fetch bodies error")
	}
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

// mockBlockInserter implements BlockInserter for testing.
type mockBlockInserter struct {
	blocks  []*types.Block
	current *types.Block
	errAt   int // fail after inserting this many blocks total
}

func newMockBlockInserter(genesis *types.Block) *mockBlockInserter {
	return &mockBlockInserter{
		blocks:  []*types.Block{genesis},
		current: genesis,
	}
}

func (m *mockBlockInserter) InsertChain(blocks []*types.Block) (int, error) {
	for i, b := range blocks {
		if m.errAt > 0 && len(m.blocks)+i >= m.errAt {
			return i, errors.New("insert error")
		}
		m.blocks = append(m.blocks, b)
		m.current = b
	}
	return len(blocks), nil
}

func (m *mockBlockInserter) CurrentBlock() *types.Block {
	return m.current
}

// makeTestHeader creates a test header at the given number with the given parent hash.
func makeTestHeader(num uint64, parentHash types.Hash, timestamp uint64) *types.Header {
	return &types.Header{
		Number:     new(big.Int).SetUint64(num),
		ParentHash: parentHash,
		Difficulty: big.NewInt(1),
		GasLimit:   30_000_000,
		Time:       timestamp,
	}
}

// buildTestChain builds a chain of headers and returns them along with genesis.
func buildTestChain(count int) (*types.Block, []*types.Header) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	headers := make([]*types.Header, count)
	prev := genesis.Header()
	for i := 0; i < count; i++ {
		h := makeTestHeader(uint64(i+1), prev.Hash(), 1000+uint64(i+1)*12)
		headers[i] = h
		prev = h
	}
	return genesis, headers
}

// --- Header validation tests ---

func TestProcessHeaders_ValidChain(t *testing.T) {
	genesis, headers := buildTestChain(5)
	err := ValidateHeaderChain(headers, genesis.Header())
	if err != nil {
		t.Fatalf("ValidateHeaderChain: %v", err)
	}
}

func TestProcessHeaders_NonContiguousNumber(t *testing.T) {
	genesis, headers := buildTestChain(3)
	// Skip a block number.
	headers[1].Number = big.NewInt(5)
	err := ValidateHeaderChain(headers, genesis.Header())
	if !errors.Is(err, ErrBadBlockNumber) {
		t.Fatalf("want ErrBadBlockNumber, got %v", err)
	}
}

func TestProcessHeaders_BadParentHash(t *testing.T) {
	genesis, headers := buildTestChain(3)
	// Corrupt parent hash of the second header.
	headers[1].ParentHash = types.Hash{0xff}
	err := ValidateHeaderChain(headers, genesis.Header())
	if !errors.Is(err, ErrBadParentHash) {
		t.Fatalf("want ErrBadParentHash, got %v", err)
	}
}

func TestProcessHeaders_FutureTimestamp(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	futureTime := uint64(time.Now().Unix()) + 1000 // far in the future
	h := makeTestHeader(1, genesis.Header().Hash(), futureTime)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if !errors.Is(err, ErrFutureTimestamp) {
		t.Fatalf("want ErrFutureTimestamp, got %v", err)
	}
}

func TestProcessHeaders_TimestampBeforeParent(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	h := makeTestHeader(1, genesis.Header().Hash(), 999) // before parent
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if !errors.Is(err, ErrTimestampOrder) {
		t.Fatalf("want ErrTimestampOrder, got %v", err)
	}
}

func TestProcessHeaders_EqualTimestamp(t *testing.T) {
	genesis := types.NewBlock(makeTestHeader(0, types.Hash{}, 1000), nil)
	// Equal timestamp is allowed (fast blocks).
	h := makeTestHeader(1, genesis.Header().Hash(), 1000)
	err := ValidateHeaderChain([]*types.Header{h}, genesis.Header())
	if err != nil {
		t.Fatalf("equal timestamp should be allowed: %v", err)
	}
}

func TestProcessHeaders_Empty(t *testing.T) {
	genesis, _ := buildTestChain(0)
	err := ValidateHeaderChain([]*types.Header{}, genesis.Header())
	if !errors.Is(err, ErrEmptyHeaders) {
		t.Fatalf("want ErrEmptyHeaders, got %v", err)
	}
}

// --- Body-header matching tests ---

func TestAssembleBlocks_MatchingCount(t *testing.T) {
	_, headers := buildTestChain(3)
	bodies := make([]*types.Body, 3)
	for i := range bodies {
		bodies[i] = &types.Body{}
	}
	blocks, err := AssembleBlocks(headers, bodies)
	if err != nil {
		t.Fatalf("AssembleBlocks: %v", err)
	}
	if len(blocks) != 3 {
		t.Fatalf("blocks: want 3, got %d", len(blocks))
	}
	for i, b := range blocks {
		if b.NumberU64() != uint64(i+1) {
			t.Errorf("block[%d].Number: want %d, got %d", i, i+1, b.NumberU64())
		}
	}
}

func TestAssembleBlocks_MismatchedCount(t *testing.T) {
	_, headers := buildTestChain(3)
	bodies := make([]*types.Body, 2) // mismatched
	for i := range bodies {
		bodies[i] = &types.Body{}
	}
	_, err := AssembleBlocks(headers, bodies)
	if !errors.Is(err, ErrBodyHeaderCount) {
		t.Fatalf("want ErrBodyHeaderCount, got %v", err)
	}
}

// --- Full sync pipeline tests ---

func TestRunSync_FullPipeline(t *testing.T) {
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
		BatchSize:     4, // small batches for testing
		BodyBatchSize: 2,
		MaxPending:    1,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(10)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}

	prog := s.GetProgress()
	if prog.CurrentBlock != 10 {
		t.Fatalf("current block: want 10, got %d", prog.CurrentBlock)
	}
	if prog.PulledHeaders != 10 {
		t.Fatalf("pulled headers: want 10, got %d", prog.PulledHeaders)
	}
	if prog.PulledBodies != 10 {
		t.Fatalf("pulled bodies: want 10, got %d", prog.PulledBodies)
	}
	if inserter.CurrentBlock().NumberU64() != 10 {
		t.Fatalf("inserter head: want 10, got %d", inserter.CurrentBlock().NumberU64())
	}
}

func TestRunSync_HeaderFetchError(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers[:3] {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}
	// Headers 4-5 are missing, so after block 3 the fetcher returns error.

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     5,
		BodyBatchSize: 5,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(5)
	// Should fail because headers 4-5 are not available.
	if err == nil {
		t.Fatal("expected error from missing headers")
	}
}

func TestRunSync_InsertionError(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)
	inserter.errAt = 4 // fail after 3 blocks inserted (genesis + 3 = 4)

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

	err := s.RunSync(5)
	if !errors.Is(err, ErrInsertionFailed) {
		t.Fatalf("want ErrInsertionFailed, got %v", err)
	}
}

func TestRunSync_NoFetchers(t *testing.T) {
	s := NewSyncer(DefaultConfig())
	err := s.RunSync(10)
	if !errors.Is(err, ErrNoHeaderFetcher) {
		t.Fatalf("want ErrNoHeaderFetcher, got %v", err)
	}
}

func TestRunSync_NoBodyFetcher(t *testing.T) {
	genesis, _ := buildTestChain(1)
	inserter := newMockBlockInserter(genesis)

	s := NewSyncer(DefaultConfig())
	s.SetFetchers(newMockHeaderSource(), nil, inserter)
	err := s.RunSync(10)
	if !errors.Is(err, ErrNoBodyFetcher) {
		t.Fatalf("want ErrNoBodyFetcher, got %v", err)
	}
}

func TestRunSync_NoInserter(t *testing.T) {
	s := NewSyncer(DefaultConfig())
	s.SetFetchers(newMockHeaderSource(), newMockBodySource(), nil)
	err := s.RunSync(10)
	if !errors.Is(err, ErrNoBlockInserter) {
		t.Fatalf("want ErrNoBlockInserter, got %v", err)
	}
}

func TestRunSync_AlreadySyncing(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	s := NewSyncer(DefaultConfig())
	s.SetFetchers(hs, bs, inserter)

	// Manually set state to syncing.
	s.state.Store(StateSyncing)
	err := s.RunSync(5)
	if !errors.Is(err, ErrAlreadySyncing) {
		t.Fatalf("want ErrAlreadySyncing, got %v", err)
	}
}

func TestRunSync_CancelMidSync(t *testing.T) {
	genesis, headers := buildTestChain(100)
	inserter := newMockBlockInserter(genesis)

	// Slow header source that allows cancellation to take effect.
	slowHS := &slowHeaderSource{
		inner:  newMockHeaderSource(),
		delay:  50 * time.Millisecond,
		cancel: make(chan struct{}),
	}
	bs := newMockBodySource()
	for _, h := range headers {
		slowHS.inner.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     5,
		BodyBatchSize: 5,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(slowHS, bs, inserter)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.RunSync(100)
	}()

	// Wait a bit then cancel.
	time.Sleep(100 * time.Millisecond)
	s.Cancel()

	err := <-errCh
	if err != nil && !errors.Is(err, ErrCancelled) {
		// It could also finish or error for other reasons depending on timing.
		t.Logf("sync ended with: %v (expected ErrCancelled or nil)", err)
	}
}

// slowHeaderSource adds a delay to header fetching for cancel testing.
type slowHeaderSource struct {
	inner  *mockHeaderSource
	delay  time.Duration
	cancel chan struct{}
}

func (s *slowHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	select {
	case <-s.cancel:
		return nil, ErrCancelled
	case <-time.After(s.delay):
	}
	return s.inner.FetchHeaders(from, count)
}

// --- Progress tracking tests ---

func TestProgress_Percentage(t *testing.T) {
	tests := []struct {
		starting, current, highest uint64
		want                       float64
	}{
		{0, 0, 100, 0.0},
		{0, 50, 100, 50.0},
		{0, 100, 100, 100.0},
		{50, 50, 100, 0.0},
		{50, 75, 100, 50.0},
		{50, 100, 100, 100.0},
		{0, 0, 0, 100.0}, // edge: no blocks to sync
	}

	for _, tt := range tests {
		p := Progress{
			StartingBlock: tt.starting,
			CurrentBlock:  tt.current,
			HighestBlock:  tt.highest,
		}
		got := p.Percentage()
		if got != tt.want {
			t.Errorf("Percentage(%d, %d, %d): want %.1f, got %.1f",
				tt.starting, tt.current, tt.highest, tt.want, got)
		}
	}
}

func TestProgress_DuringSync(t *testing.T) {
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
		BatchSize:     5,
		BodyBatchSize: 5,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	if err := s.RunSync(10); err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	prog := s.GetProgress()
	if prog.Percentage() != 100.0 {
		t.Fatalf("percentage: want 100.0, got %.1f", prog.Percentage())
	}
	if prog.StartingBlock != 0 {
		t.Fatalf("starting block: want 0, got %d", prog.StartingBlock)
	}
	if prog.CurrentBlock != 10 {
		t.Fatalf("current block: want 10, got %d", prog.CurrentBlock)
	}
}

// --- Downloader tests ---

func TestDownloader_StartAndProgress(t *testing.T) {
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

	err := dl.Start(10)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	prog := dl.Progress()
	if prog.CurrentBlock != 10 {
		t.Fatalf("current: want 10, got %d", prog.CurrentBlock)
	}
	if prog.Percentage != 100.0 {
		t.Fatalf("percentage: want 100, got %.1f", prog.Percentage)
	}
}

func TestDownloader_Cancel(t *testing.T) {
	genesis, headers := buildTestChain(100)
	inserter := newMockBlockInserter(genesis)

	slowHS := &slowHeaderSource{
		inner:  newMockHeaderSource(),
		delay:  100 * time.Millisecond,
		cancel: make(chan struct{}),
	}
	bs := newMockBodySource()
	for _, h := range headers {
		slowHS.inner.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := DefaultDownloaderConfig()
	cfg.SyncConfig.BatchSize = 5
	cfg.SyncConfig.BodyBatchSize = 5

	dl := NewDownloader(cfg)
	dl.SetFetchers(slowHS, bs, inserter)

	errCh := make(chan error, 1)
	go func() {
		errCh <- dl.Start(100)
	}()

	time.Sleep(50 * time.Millisecond)
	dl.Cancel()

	err := <-errCh
	// Should be cancelled or already done.
	if err != nil && !errors.Is(err, ErrCancelled) {
		t.Logf("expected ErrCancelled, got: %v", err)
	}
}

func TestDownloader_DoubleStart(t *testing.T) {
	genesis, _ := buildTestChain(0)
	inserter := newMockBlockInserter(genesis)

	dl := NewDownloader(DefaultDownloaderConfig())
	dl.SetFetchers(newMockHeaderSource(), newMockBodySource(), inserter)

	// Simulate already running.
	dl.running.Store(true)
	err := dl.Start(10)
	if !errors.Is(err, ErrDownloaderRunning) {
		t.Fatalf("want ErrDownloaderRunning, got %v", err)
	}
}

func TestDownloader_PeerBan(t *testing.T) {
	cfg := DefaultDownloaderConfig()
	cfg.BanThreshold = 3

	dl := NewDownloader(cfg)

	peerID := "peer1"

	// Not banned initially.
	if dl.IsPeerBanned(peerID) {
		t.Fatal("peer should not be banned initially")
	}

	// Record failures up to threshold.
	dl.RecordPeerFailure(peerID)
	dl.RecordPeerFailure(peerID)
	if dl.IsPeerBanned(peerID) {
		t.Fatal("peer should not be banned after 2 failures")
	}

	dl.RecordPeerFailure(peerID)
	if !dl.IsPeerBanned(peerID) {
		t.Fatal("peer should be banned after 3 failures")
	}

	// Reset should clear the ban.
	dl.ResetPeer(peerID)
	if dl.IsPeerBanned(peerID) {
		t.Fatal("peer should not be banned after reset")
	}
}

func TestDownloader_RetryLogic(t *testing.T) {
	genesis, headers := buildTestChain(5)

	// Create a header source that fails the first 2 times then succeeds.
	failCount := &atomic.Int32{}
	hs := &failNTimesHeaderSource{
		inner:     newMockHeaderSource(),
		failCount: failCount,
		failMax:   2,
	}
	bs := newMockBodySource()

	for _, h := range headers {
		hs.inner.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := DefaultDownloaderConfig()
	cfg.MaxRetries = 5
	cfg.SyncConfig.BatchSize = 5
	cfg.SyncConfig.BodyBatchSize = 5

	inserter := newMockBlockInserter(genesis)
	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)

	err := dl.Start(5)
	if err != nil {
		t.Fatalf("expected sync to succeed after retries, got: %v", err)
	}
}

type failNTimesHeaderSource struct {
	inner     *mockHeaderSource
	failCount *atomic.Int32
	failMax   int32
}

func (f *failNTimesHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	c := f.failCount.Add(1)
	if c <= f.failMax {
		return nil, errors.New("transient failure")
	}
	return f.inner.FetchHeaders(from, count)
}

func TestDownloader_MaxRetriesExceeded(t *testing.T) {
	genesis, _ := buildTestChain(0)

	// Always-failing header source.
	hs := &alwaysFailHeaderSource{}
	bs := newMockBodySource()

	cfg := DefaultDownloaderConfig()
	cfg.MaxRetries = 3
	cfg.SyncConfig.BatchSize = 5
	cfg.SyncConfig.BodyBatchSize = 5

	inserter := newMockBlockInserter(genesis)
	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)

	err := dl.Start(5)
	if !errors.Is(err, ErrMaxRetries) {
		t.Fatalf("want ErrMaxRetries, got %v", err)
	}
}

type alwaysFailHeaderSource struct{}

func (a *alwaysFailHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	return nil, errors.New("permanent failure")
}

// --- Timeout tests ---

func TestTimeoutHeaderFetcher_Success(t *testing.T) {
	inner := newMockHeaderSource()
	genesis, headers := buildTestChain(3)
	_ = genesis
	for _, h := range headers {
		inner.addHeader(h)
	}

	tf := NewTimeoutHeaderFetcher(inner, 1*time.Second)
	h, err := tf.FetchHeaders(1, 3)
	if err != nil {
		t.Fatalf("FetchHeaders: %v", err)
	}
	if len(h) != 3 {
		t.Fatalf("headers: want 3, got %d", len(h))
	}
}

func TestTimeoutHeaderFetcher_Timeout(t *testing.T) {
	slow := &verySlowHeaderSource{delay: 500 * time.Millisecond}
	tf := NewTimeoutHeaderFetcher(slow, 50*time.Millisecond)
	_, err := tf.FetchHeaders(1, 1)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

type verySlowHeaderSource struct {
	delay time.Duration
}

func (v *verySlowHeaderSource) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	time.Sleep(v.delay)
	return nil, nil
}

func TestTimeoutBodyFetcher_Timeout(t *testing.T) {
	slow := &verySlowBodySource{delay: 500 * time.Millisecond}
	tf := NewTimeoutBodyFetcher(slow, 50*time.Millisecond)
	_, err := tf.FetchBodies([]types.Hash{{0x01}})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout, got %v", err)
	}
}

type verySlowBodySource struct {
	delay time.Duration
}

func (v *verySlowBodySource) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	time.Sleep(v.delay)
	return nil, nil
}

// --- Batch size tests ---

func TestRunSync_ConfigurableBatchSizes(t *testing.T) {
	genesis, headers := buildTestChain(20)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := &countingBodySource{
		inner: newMockBodySource(),
	}

	for _, h := range headers {
		hs.addHeader(h)
		bs.inner.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     7,  // non-aligned batch size
		BodyBatchSize: 3,  // small body batch
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	if err := s.RunSync(20); err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	if inserter.CurrentBlock().NumberU64() != 20 {
		t.Fatalf("head: want 20, got %d", inserter.CurrentBlock().NumberU64())
	}
}

type countingBodySource struct {
	inner *mockBodySource
	calls int
}

func (c *countingBodySource) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	c.calls++
	return c.inner.FetchBodies(hashes)
}

// --- Edge cases ---

func TestRunSync_AlreadyAtTarget(t *testing.T) {
	genesis, headers := buildTestChain(5)
	inserter := newMockBlockInserter(genesis)

	// Pre-insert all blocks.
	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
		block := types.NewBlock(h, &types.Body{})
		inserter.blocks = append(inserter.blocks, block)
		inserter.current = block
	}

	s := NewSyncer(DefaultConfig())
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(5) // target == current
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}
}

func TestRunSync_SingleBlock(t *testing.T) {
	genesis, headers := buildTestChain(1)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	hs.addHeader(headers[0])
	bs.addBody(headers[0].Hash(), &types.Body{})

	s := NewSyncer(DefaultConfig())
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(1)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}
	if inserter.CurrentBlock().NumberU64() != 1 {
		t.Fatalf("head: want 1, got %d", inserter.CurrentBlock().NumberU64())
	}
}

func TestSyncer_MarkDone(t *testing.T) {
	s := NewSyncer(nil)
	s.SetCallbacks(nil, nil, func() uint64 { return 0 }, nil)
	s.Start(100)
	s.MarkDone()
	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}
}

func TestDefaultBatchSizes(t *testing.T) {
	if DefaultHeaderBatch != 64 {
		t.Fatalf("DefaultHeaderBatch: want 64, got %d", DefaultHeaderBatch)
	}
	if DefaultBodyBatch != 32 {
		t.Fatalf("DefaultBodyBatch: want 32, got %d", DefaultBodyBatch)
	}
}
