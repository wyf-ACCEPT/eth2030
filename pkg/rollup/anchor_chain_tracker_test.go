package rollup

import (
	"sync"
	"testing"
)

func testAnchorConfig(chainID uint64) *AnchorChainConfig {
	var addr [20]byte
	addr[0] = byte(chainID)
	var genesis [32]byte
	genesis[0] = 0xAA
	genesis[31] = byte(chainID)
	return &AnchorChainConfig{
		ChainID:            chainID,
		AnchorAddress:      addr,
		GenesisRoot:        genesis,
		ConfirmationDepth:  10,
		MaxGasPerExecution: 30_000_000,
	}
}

func TestAnchorChainTrackerNewManager(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.ChainCount() != 0 {
		t.Errorf("expected 0 chains, got %d", tracker.ChainCount())
	}
	// Also test default max.
	if t2 := NewAnchorChainTracker(0); t2 == nil {
		t.Fatal("expected non-nil tracker with default max")
	}
}

func TestAnchorChainTrackerRegisterChain(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := testAnchorConfig(100)

	err := tracker.RegisterChain(100, cfg)
	if err != nil {
		t.Fatalf("RegisterChain: %v", err)
	}
	if tracker.ChainCount() != 1 {
		t.Errorf("expected 1 chain, got %d", tracker.ChainCount())
	}
}

func TestAnchorChainTrackerRegisterChainZeroID(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := testAnchorConfig(0)
	err := tracker.RegisterChain(0, cfg)
	if err != ErrChainIDZero {
		t.Errorf("expected ErrChainIDZero, got %v", err)
	}
}

func TestAnchorChainTrackerUpdateAnchor(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(100, testAnchorConfig(100))
	var root [32]byte
	root[0] = 0xBB
	if err := tracker.UpdateAnchor(100, 1, root); err != nil {
		t.Fatalf("UpdateAnchor: %v", err)
	}
	anchor, err := tracker.GetLatestAnchor(100)
	if err != nil {
		t.Fatalf("GetLatestAnchor: %v", err)
	}
	if anchor.L1BlockNumber != 1 || anchor.L2StateRoot != root || anchor.ChainID != 100 {
		t.Errorf("unexpected anchor: block=%d root=%x chain=%d", anchor.L1BlockNumber, anchor.L2StateRoot, anchor.ChainID)
	}
}

func TestAnchorChainTrackerUpdateAnchorUnknownChain(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	var root [32]byte
	err := tracker.UpdateAnchor(999, 1, root)
	if err != ErrChainNotRegistered {
		t.Errorf("expected ErrChainNotRegistered, got %v", err)
	}
}

func TestAnchorChainTrackerUpdateAnchorRegression(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := testAnchorConfig(100)
	tracker.RegisterChain(100, cfg)

	var root [32]byte
	tracker.UpdateAnchor(100, 10, root)
	err := tracker.UpdateAnchor(100, 5, root)
	if err != ErrAnchorBlockRegression {
		t.Errorf("expected ErrAnchorBlockRegression, got %v", err)
	}
}

func TestAnchorChainTrackerGetLatest(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(42, testAnchorConfig(42))
	if _, err := tracker.GetLatestAnchor(42); err != ErrAnchorBlockNotFound {
		t.Errorf("expected ErrAnchorBlockNotFound, got %v", err)
	}
	var root1, root2 [32]byte
	root1[0] = 0x11
	root2[0] = 0x22
	tracker.UpdateAnchor(42, 100, root1)
	tracker.UpdateAnchor(42, 200, root2)
	anchor, err := tracker.GetLatestAnchor(42)
	if err != nil {
		t.Fatalf("GetLatestAnchor: %v", err)
	}
	if anchor.L1BlockNumber != 200 || anchor.L2StateRoot != root2 {
		t.Error("latest anchor mismatch")
	}
	// Unknown chain.
	if _, err := tracker.GetLatestAnchor(999); err != ErrChainNotRegistered {
		t.Errorf("expected ErrChainNotRegistered, got %v", err)
	}
}

func TestAnchorChainTrackerGetHistory(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(1, testAnchorConfig(1))
	for i := uint64(1); i <= 5; i++ {
		var root [32]byte
		root[0] = byte(i)
		tracker.UpdateAnchor(1, i*10, root)
	}
	history, err := tracker.GetAnchorHistory(1, 3)
	if err != nil {
		t.Fatalf("GetAnchorHistory: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 anchors, got %d", len(history))
	}
	if history[0].L1BlockNumber != 50 || history[2].L1BlockNumber != 30 {
		t.Error("history ordering wrong: expected newest first")
	}
	// Request more than available.
	if h, _ := tracker.GetAnchorHistory(1, 100); len(h) != 5 {
		t.Errorf("expected 5 anchors, got %d", len(h))
	}
	// Unknown chain.
	if _, err := tracker.GetAnchorHistory(999, 5); err != ErrChainNotRegistered {
		t.Errorf("expected ErrChainNotRegistered, got %v", err)
	}
}

func TestAnchorChainTrackerGetHistoryEmpty(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(1, testAnchorConfig(1))
	history, _ := tracker.GetAnchorHistory(1, 5)
	if len(history) != 0 {
		t.Errorf("expected 0 anchors, got %d", len(history))
	}
}

func TestAnchorChainTrackerConfirmAnchor(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(1, testAnchorConfig(1))
	var root [32]byte
	tracker.UpdateAnchor(1, 100, root)
	if err := tracker.ConfirmAnchor(1, 100); err != nil {
		t.Fatalf("ConfirmAnchor: %v", err)
	}
	anchor, _ := tracker.GetLatestAnchor(1)
	if !anchor.Confirmed {
		t.Error("expected anchor to be confirmed")
	}
	// Already confirmed.
	if err := tracker.ConfirmAnchor(1, 100); err != ErrAnchorAlreadyConfirmed {
		t.Errorf("expected ErrAnchorAlreadyConfirmed, got %v", err)
	}
	// Not found.
	if err := tracker.ConfirmAnchor(1, 999); err != ErrAnchorBlockNotFound {
		t.Errorf("expected ErrAnchorBlockNotFound, got %v", err)
	}
	// Unknown chain.
	if err := tracker.ConfirmAnchor(999, 100); err != ErrChainNotRegistered {
		t.Errorf("expected ErrChainNotRegistered, got %v", err)
	}
}

func TestAnchorChainTrackerPruneAnchors(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(1, testAnchorConfig(1))
	for i := uint64(1); i <= 10; i++ {
		var root [32]byte
		root[0] = byte(i)
		tracker.UpdateAnchor(1, i*10, root)
	}
	if pruned := tracker.PruneAnchors(1, 50); pruned != 4 {
		t.Errorf("expected 4 pruned, got %d", pruned)
	}
	if h, _ := tracker.GetAnchorHistory(1, 100); len(h) != 6 {
		t.Errorf("expected 6 remaining, got %d", len(h))
	}
	// Unknown chain.
	if pruned := tracker.PruneAnchors(999, 100); pruned != 0 {
		t.Errorf("expected 0 pruned for unknown, got %d", pruned)
	}
}

func TestAnchorChainTrackerActiveChains(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(30, testAnchorConfig(30))
	tracker.RegisterChain(10, testAnchorConfig(10))
	tracker.RegisterChain(20, testAnchorConfig(20))

	chains := tracker.ActiveChains()
	if len(chains) != 3 {
		t.Fatalf("expected 3 chains, got %d", len(chains))
	}
	// Should be sorted.
	if chains[0] != 10 || chains[1] != 20 || chains[2] != 30 {
		t.Errorf("expected [10, 20, 30], got %v", chains)
	}
}

func TestAnchorChainTrackerChainMetrics(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(1, testAnchorConfig(1))
	for i := uint64(1); i <= 5; i++ {
		var root [32]byte
		root[0] = byte(i)
		tracker.UpdateAnchor(1, i*10, root)
	}
	tracker.ConfirmAnchor(1, 10)
	tracker.ConfirmAnchor(1, 20)
	metrics := tracker.ChainMetrics(1)
	if metrics == nil {
		t.Fatal("expected non-nil metrics")
	}
	if metrics.TotalAnchors != 5 || metrics.ConfirmedAnchors != 2 {
		t.Errorf("expected 5 total / 2 confirmed, got %d / %d", metrics.TotalAnchors, metrics.ConfirmedAnchors)
	}
	// Unknown chain.
	if tracker.ChainMetrics(999) != nil {
		t.Error("expected nil metrics for unknown chain")
	}
}

func TestAnchorChainTrackerDuplicateRegister(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := testAnchorConfig(1)
	err := tracker.RegisterChain(1, cfg)
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	err = tracker.RegisterChain(1, cfg)
	if err != ErrChainAlreadyRegistered {
		t.Errorf("expected ErrChainAlreadyRegistered, got %v", err)
	}
}

func TestAnchorChainTrackerMaxChains(t *testing.T) {
	tracker := NewAnchorChainTracker(2)
	tracker.RegisterChain(1, testAnchorConfig(1))
	tracker.RegisterChain(2, testAnchorConfig(2))

	err := tracker.RegisterChain(3, testAnchorConfig(3))
	if err != ErrChainMaxReached {
		t.Errorf("expected ErrChainMaxReached, got %v", err)
	}
}

func TestAnchorChainTrackerMultipleChains(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	tracker.RegisterChain(100, testAnchorConfig(100))
	tracker.RegisterChain(200, testAnchorConfig(200))
	var root1, root2 [32]byte
	root1[0] = 0x11
	root2[0] = 0x22
	tracker.UpdateAnchor(100, 10, root1)
	tracker.UpdateAnchor(200, 20, root2)
	a1, _ := tracker.GetLatestAnchor(100)
	a2, _ := tracker.GetLatestAnchor(200)
	if a1.L1BlockNumber != 10 || a1.L2StateRoot != root1 {
		t.Error("chain 100 mismatch")
	}
	if a2.L1BlockNumber != 20 || a2.L2StateRoot != root2 {
		t.Error("chain 200 mismatch")
	}
}

func TestAnchorChainTrackerConcurrentAccess(t *testing.T) {
	tracker := NewAnchorChainTracker(64)
	for i := uint64(1); i <= 10; i++ {
		tracker.RegisterChain(i, testAnchorConfig(i))
	}

	var wg sync.WaitGroup

	// Concurrent writes.
	for chainID := uint64(1); chainID <= 10; chainID++ {
		wg.Add(1)
		go func(cid uint64) {
			defer wg.Done()
			for block := uint64(1); block <= 20; block++ {
				var root [32]byte
				root[0] = byte(cid)
				root[1] = byte(block)
				tracker.UpdateAnchor(cid, block, root)
			}
		}(chainID)
	}

	// Concurrent reads.
	for chainID := uint64(1); chainID <= 10; chainID++ {
		wg.Add(1)
		go func(cid uint64) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				tracker.GetLatestAnchor(cid)
				tracker.GetAnchorHistory(cid, 5)
				tracker.ChainMetrics(cid)
				tracker.ActiveChains()
			}
		}(chainID)
	}

	wg.Wait()

	// Verify all chains have exactly 20 anchors.
	for chainID := uint64(1); chainID <= 10; chainID++ {
		metrics := tracker.ChainMetrics(chainID)
		if metrics.TotalAnchors != 20 {
			t.Errorf("chain %d: expected 20 anchors, got %d", chainID, metrics.TotalAnchors)
		}
	}
}

func TestAnchorChainTrackerAnchorConfig(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := &AnchorChainConfig{ChainID: 42, ConfirmationDepth: 32, MaxGasPerExecution: 50_000_000}
	cfg.AnchorAddress[0] = 0xDE
	cfg.GenesisRoot[0] = 0xAD
	tracker.RegisterChain(42, cfg)
	got, err := tracker.GetChainConfig(42)
	if err != nil {
		t.Fatalf("GetChainConfig: %v", err)
	}
	if got.ConfirmationDepth != 32 || got.MaxGasPerExecution != 50_000_000 {
		t.Error("config mismatch")
	}
	if got.AnchorAddress[0] != 0xDE || got.GenesisRoot[0] != 0xAD {
		t.Error("address/genesis mismatch")
	}
	// Unknown chain.
	if _, err := tracker.GetChainConfig(999); err != ErrChainNotRegistered {
		t.Errorf("expected ErrChainNotRegistered, got %v", err)
	}
}

func TestAnchorChainTrackerConfirmationDepth(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := testAnchorConfig(1)
	cfg.ConfirmationDepth = 5
	tracker.RegisterChain(1, cfg)

	// Add anchors and confirm some.
	for i := uint64(1); i <= 10; i++ {
		var root [32]byte
		root[0] = byte(i)
		tracker.UpdateAnchor(1, i, root)
	}

	// Confirm anchor at block 3.
	tracker.ConfirmAnchor(1, 3)

	metrics := tracker.ChainMetrics(1)
	if metrics.ConfirmedAnchors != 1 {
		t.Errorf("expected 1 confirmed, got %d", metrics.ConfirmedAnchors)
	}
	// Avg depth: latest block (10) - confirmed block (3) = 7.
	if metrics.AvgConfirmationDepth != 7 {
		t.Errorf("expected avg depth 7, got %d", metrics.AvgConfirmationDepth)
	}
}

func TestAnchorChainTrackerDefaultConfig(t *testing.T) {
	tracker := NewAnchorChainTracker(16)
	cfg := &AnchorChainConfig{} // zero values for optional fields
	tracker.RegisterChain(1, cfg)

	got, _ := tracker.GetChainConfig(1)
	if got.ConfirmationDepth != 64 {
		t.Errorf("expected default depth 64, got %d", got.ConfirmationDepth)
	}
	if got.MaxGasPerExecution != 30_000_000 {
		t.Errorf("expected default gas 30M, got %d", got.MaxGasPerExecution)
	}
}
