package sync

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/eth2028/eth2028/core/types"
	"github.com/eth2028/eth2028/crypto"
)

// --- Snap sync integration tests ---
// These tests verify that snap sync is properly wired into the main sync loop.

func TestRunSync_SnapMode_FullPipeline(t *testing.T) {
	// Build a chain long enough for snap sync (needs > MinPivotBlock).
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	// Create snap sync components.
	accounts := makeTestAccounts(5)
	peer := newMockSnapPeer("snap-peer1")
	peer.accounts = accounts
	writer := newMockStateWriter()

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     20,
		BodyBatchSize: 10,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(peer, writer)

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync snap mode: %v", err)
	}

	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}

	prog := s.GetProgress()
	if prog.CurrentBlock != uint64(chainLen) {
		t.Fatalf("current block: want %d, got %d", chainLen, prog.CurrentBlock)
	}

	// Snap progress should be recorded.
	if prog.SnapProgress == nil {
		t.Fatal("snap progress should not be nil after snap sync")
	}
	if prog.SnapProgress.AccountsDone != 5 {
		t.Fatalf("snap accounts done: want 5, got %d", prog.SnapProgress.AccountsDone)
	}
	if prog.SnapProgress.Phase != PhaseComplete {
		t.Fatalf("snap phase: want complete, got %s", PhaseName(prog.SnapProgress.Phase))
	}

	// Accounts should have been written to the state writer.
	if len(writer.accounts) != 5 {
		t.Fatalf("written accounts: want 5, got %d", len(writer.accounts))
	}
}

func TestRunSync_SnapMode_WithStorageAndBytecodes(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	// Create accounts with storage and bytecodes.
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	codeHash := types.BytesToHash(crypto.Keccak256(code))
	storageRoot := types.Hash{0xbb, 0xcc}

	accounts := []AccountData{
		{
			Hash:     types.Hash{0x10},
			Nonce:    1,
			Balance:  big.NewInt(1000),
			Root:     types.EmptyRootHash,
			CodeHash: types.EmptyCodeHash,
		},
		{
			Hash:     types.Hash{0x20},
			Nonce:    5,
			Balance:  big.NewInt(5000),
			Root:     storageRoot,
			CodeHash: codeHash,
		},
	}

	peer := newMockSnapPeer("snap-peer1")
	peer.accounts = accounts
	peer.codes[codeHash] = code
	peer.storage[types.Hash{0x20}] = []StorageData{
		{AccountHash: types.Hash{0x20}, SlotHash: types.Hash{0x01}, Value: []byte{0x42}},
	}

	writer := newMockStateWriter()
	writer.missingPaths = [][]byte{{0xab}}
	writer.healRounds = 1
	peer.healData[string([]byte{0xab})] = []byte{0x01, 0x02, 0x03}

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(peer, writer)

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	// Verify snap sync downloaded everything.
	if len(writer.accounts) != 2 {
		t.Fatalf("accounts: want 2, got %d", len(writer.accounts))
	}
	if len(writer.storage) != 1 {
		t.Fatalf("storage slots: want 1, got %d", len(writer.storage))
	}
	if len(writer.codes) != 1 {
		t.Fatalf("bytecodes: want 1, got %d", len(writer.codes))
	}
	if len(writer.nodes) != 1 {
		t.Fatalf("healed nodes: want 1, got %d", len(writer.nodes))
	}

	// Verify full sync completed all blocks.
	if inserter.CurrentBlock().NumberU64() != uint64(chainLen) {
		t.Fatalf("inserter head: want %d, got %d", chainLen, inserter.CurrentBlock().NumberU64())
	}
}

func TestRunSync_SnapMode_FallbackToFullOnError(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	// Create a snap peer that fails during account download.
	peer := newMockSnapPeer("failing-peer")
	peer.accountErr = errors.New("network failure")
	writer := newMockStateWriter()

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(peer, writer)

	// Should fall back to full sync and succeed.
	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync should succeed via fallback: %v", err)
	}

	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}

	// Mode should have switched to full after fallback.
	if s.Mode() != ModeFull {
		t.Fatalf("mode after fallback: want full, got %s", s.Mode())
	}

	if inserter.CurrentBlock().NumberU64() != uint64(chainLen) {
		t.Fatalf("inserter head: want %d, got %d", chainLen, inserter.CurrentBlock().NumberU64())
	}
}

func TestRunSync_SnapMode_FallbackNoSnapPeer(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	// No snap peer set -- should fall back to full sync.
	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	// Deliberately not calling SetSnapSync.

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync should fall back to full sync: %v", err)
	}

	if s.Mode() != ModeFull {
		t.Fatalf("mode: want full (after fallback), got %s", s.Mode())
	}
	if inserter.CurrentBlock().NumberU64() != uint64(chainLen) {
		t.Fatalf("head: want %d, got %d", chainLen, inserter.CurrentBlock().NumberU64())
	}
}

func TestRunSync_SnapMode_FallbackChainTooShort(t *testing.T) {
	// Chain shorter than MinPivotBlock -- snap sync should fail and
	// fall back to full sync.
	chainLen := 50
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	peer := newMockSnapPeer("peer1")
	writer := newMockStateWriter()

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     20,
		BodyBatchSize: 10,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(peer, writer)

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync should fall back to full sync: %v", err)
	}

	if s.Mode() != ModeFull {
		t.Fatalf("mode: want full (chain too short for snap), got %s", s.Mode())
	}
	if inserter.CurrentBlock().NumberU64() != uint64(chainLen) {
		t.Fatalf("head: want %d, got %d", chainLen, inserter.CurrentBlock().NumberU64())
	}
}

func TestRunSync_FullMode_Explicit(t *testing.T) {
	// Explicitly set full mode -- snap sync should not run.
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	cfg := &Config{
		Mode:          ModeFull,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	if s.Mode() != ModeFull {
		t.Fatalf("mode: want full, got %s", s.Mode())
	}
	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}
	prog := s.GetProgress()
	if prog.Mode != ModeFull {
		t.Fatalf("progress mode: want full, got %s", prog.Mode)
	}
}

// --- Stage tracking tests ---

func TestStageTracking_FullSync(t *testing.T) {
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

	err := s.RunSync(10)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	if s.Stage() != StageCaughtUp {
		t.Fatalf("stage: want StageCaughtUp, got %s", StageName(s.Stage()))
	}

	prog := s.GetProgress()
	if prog.Stage != StageCaughtUp {
		t.Fatalf("progress stage: want StageCaughtUp, got %s", StageName(prog.Stage))
	}
}

func TestStageTracking_SnapSync(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = makeTestAccounts(3)
	writer := newMockStateWriter()

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(peer, writer)

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	if s.Stage() != StageCaughtUp {
		t.Fatalf("stage: want StageCaughtUp, got %s", StageName(s.Stage()))
	}
}

func TestStageName_AllStages(t *testing.T) {
	tests := []struct {
		stage uint32
		want  string
	}{
		{StageNone, "none"},
		{StageHeaders, "downloading headers"},
		{StageSnapAccounts, "downloading accounts"},
		{StageSnapStorage, "downloading storage"},
		{StageSnapBytecodes, "downloading bytecodes"},
		{StageSnapHealing, "healing trie"},
		{StageBlocks, "downloading blocks"},
		{StageCaughtUp, "caught up"},
		{99, "unknown(99)"},
	}

	for _, tt := range tests {
		got := StageName(tt.stage)
		if got != tt.want {
			t.Errorf("StageName(%d): want %q, got %q", tt.stage, tt.want, got)
		}
	}
}

// --- Syncer.SetSnapSync and Mode tests ---

func TestSyncer_SetSnapSync(t *testing.T) {
	s := NewSyncer(&Config{Mode: ModeSnap})
	if s.SnapSyncer() != nil {
		t.Fatal("snap syncer should be nil before SetSnapSync")
	}

	peer := newMockSnapPeer("peer1")
	writer := newMockStateWriter()
	s.SetSnapSync(peer, writer)

	if s.SnapSyncer() == nil {
		t.Fatal("snap syncer should be set after SetSnapSync")
	}
}

func TestSyncer_Mode(t *testing.T) {
	s := NewSyncer(&Config{Mode: ModeSnap})
	if s.Mode() != ModeSnap {
		t.Fatalf("mode: want snap, got %s", s.Mode())
	}

	s2 := NewSyncer(&Config{Mode: ModeFull})
	if s2.Mode() != ModeFull {
		t.Fatalf("mode: want full, got %s", s2.Mode())
	}
}

// --- Downloader snap sync tests ---

func TestDownloader_SnapSync(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	peer := newMockSnapPeer("snap-peer")
	peer.accounts = makeTestAccounts(3)
	writer := newMockStateWriter()

	cfg := DefaultDownloaderConfig()
	cfg.SyncConfig.Mode = ModeSnap
	cfg.SyncConfig.BatchSize = 50
	cfg.SyncConfig.BodyBatchSize = 25

	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)
	dl.SetSnapSync(peer, writer)

	err := dl.Start(uint64(chainLen))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	prog := dl.Progress()
	if prog.CurrentBlock != uint64(chainLen) {
		t.Fatalf("current: want %d, got %d", chainLen, prog.CurrentBlock)
	}
	if prog.SnapProgress == nil {
		t.Fatal("snap progress should not be nil")
	}
	if prog.SnapProgress.AccountsDone != 3 {
		t.Fatalf("snap accounts: want 3, got %d", prog.SnapProgress.AccountsDone)
	}
}

func TestDownloader_SnapSync_FallbackOnError(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	// Failing snap peer.
	peer := newMockSnapPeer("failing-peer")
	peer.accountErr = errors.New("snap protocol error")
	writer := newMockStateWriter()

	cfg := DefaultDownloaderConfig()
	cfg.SyncConfig.Mode = ModeSnap
	cfg.SyncConfig.BatchSize = 50
	cfg.SyncConfig.BodyBatchSize = 25

	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)
	dl.SetSnapSync(peer, writer)

	err := dl.Start(uint64(chainLen))
	if err != nil {
		t.Fatalf("Start should succeed via fallback: %v", err)
	}

	if inserter.CurrentBlock().NumberU64() != uint64(chainLen) {
		t.Fatalf("head: want %d, got %d", chainLen, inserter.CurrentBlock().NumberU64())
	}
}

func TestDownloader_Progress_SnapFields(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = makeTestAccounts(2)
	writer := newMockStateWriter()

	cfg := DefaultDownloaderConfig()
	cfg.SyncConfig.Mode = ModeSnap
	cfg.SyncConfig.BatchSize = 50
	cfg.SyncConfig.BodyBatchSize = 25

	dl := NewDownloader(cfg)
	dl.SetFetchers(hs, bs, inserter)
	dl.SetSnapSync(peer, writer)

	err := dl.Start(uint64(chainLen))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	prog := dl.Progress()
	if prog.Stage == "" {
		t.Fatal("stage should not be empty")
	}
	// After completion stage should be "caught up".
	if prog.Stage != "caught up" {
		t.Fatalf("stage: want 'caught up', got %q", prog.Stage)
	}
}

// --- Cancel during snap sync tests ---

func TestRunSync_SnapMode_CancelDuringSnap(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	// Use a blocking snap peer to simulate long-running snap sync.
	innerPeer := newMockSnapPeer("peer1")
	blockingPeer := &blockingSnapPeer{
		inner:   innerPeer,
		blockCh: make(chan struct{}),
	}
	writer := newMockStateWriter()

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(blockingPeer, writer)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.RunSync(uint64(chainLen))
	}()

	// Wait for sync to start then cancel.
	time.Sleep(50 * time.Millisecond)
	s.Cancel()

	// Unblock the peer so the goroutine can finish.
	close(blockingPeer.blockCh)

	err := <-errCh
	if err != nil && !errors.Is(err, ErrCancelled) {
		t.Logf("expected ErrCancelled or nil, got: %v", err)
	}
}

// --- Snap sync mode is default test ---

func TestDefaultMode_IsSnap(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Mode != ModeSnap {
		t.Fatalf("default mode should be snap, got %s", cfg.Mode)
	}
}

func TestDefaultSyncer_FallsBackToFull(t *testing.T) {
	// Using default config (snap mode) without snap components
	// should fall back to full sync automatically.
	genesis, headers := buildTestChain(10)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	s := NewSyncer(DefaultConfig())
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(10)
	if err != nil {
		t.Fatalf("RunSync with default config should fallback: %v", err)
	}

	if s.State() != StateDone {
		t.Fatalf("state: want StateDone, got %d", s.State())
	}
	if inserter.CurrentBlock().NumberU64() != 10 {
		t.Fatalf("head: want 10, got %d", inserter.CurrentBlock().NumberU64())
	}
}

// --- updateSnapStage tests ---

func TestUpdateSnapStage(t *testing.T) {
	writer := newMockStateWriter()
	s := NewSyncer(&Config{Mode: ModeSnap})
	s.SetSnapSync(newMockSnapPeer("peer1"), writer)

	// Before snap sync starts, updateSnapStage should not crash.
	s.updateSnapStage()
	if s.Stage() != StageNone {
		t.Fatalf("stage should be none before sync, got %s", StageName(s.Stage()))
	}
}

// --- Error type tests ---

func TestSnapSyncErrorTypes(t *testing.T) {
	if ErrSnapSyncFailed == nil {
		t.Fatal("ErrSnapSyncFailed should not be nil")
	}
	if ErrNoSnapPeerSet == nil {
		t.Fatal("ErrNoSnapPeerSet should not be nil")
	}
	if ErrNoStateWriter == nil {
		t.Fatal("ErrNoStateWriter should not be nil")
	}
}

// --- Progress Mode field test ---

func TestProgress_ModeField(t *testing.T) {
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
		BatchSize:     10,
		BodyBatchSize: 10,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)

	err := s.RunSync(10)
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	prog := s.GetProgress()
	if prog.Mode != ModeFull {
		t.Fatalf("progress mode: want full, got %s", prog.Mode)
	}
}

func TestProgress_ModeField_Snap(t *testing.T) {
	chainLen := 200
	genesis, headers := buildTestChain(chainLen)
	inserter := newMockBlockInserter(genesis)

	hs := newMockHeaderSource()
	bs := newMockBodySource()
	for _, h := range headers {
		hs.addHeader(h)
		bs.addBody(h.Hash(), &types.Body{})
	}

	peer := newMockSnapPeer("peer1")
	peer.accounts = makeTestAccounts(2)
	writer := newMockStateWriter()

	cfg := &Config{
		Mode:          ModeSnap,
		BatchSize:     50,
		BodyBatchSize: 25,
	}
	s := NewSyncer(cfg)
	s.SetFetchers(hs, bs, inserter)
	s.SetSnapSync(peer, writer)

	err := s.RunSync(uint64(chainLen))
	if err != nil {
		t.Fatalf("RunSync: %v", err)
	}

	prog := s.GetProgress()
	// After successful snap sync, mode should still be snap.
	if prog.Mode != ModeSnap {
		t.Fatalf("progress mode: want snap, got %s", prog.Mode)
	}
}
