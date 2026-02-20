package core

import (
	"fmt"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewChainReorgHandler(t *testing.T) {
	cfg := DefaultReorgConfig()
	h := NewChainReorgHandler(cfg)

	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.ReorgCount() != 0 {
		t.Errorf("expected 0 reorgs, got %d", h.ReorgCount())
	}
	if h.MaxReorgDepthSeen() != 0 {
		t.Errorf("expected 0 max depth, got %d", h.MaxReorgDepthSeen())
	}

	number, hash := h.ChainHead()
	if number != 0 || !hash.IsZero() {
		t.Errorf("expected zero head, got %d %s", number, hash)
	}
}

func TestChainReorgNormalExtension(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	// Build a chain: genesis -> 1 -> 2 -> 3.
	hashes := make([]types.Hash, 4)
	for i := 0; i < 4; i++ {
		hashes[i] = types.HexToHash(fmt.Sprintf("0x%x", i+1))
	}

	// Block 0 (genesis).
	ev, err := h.ProcessNewHead(0, hashes[0], types.Hash{})
	if err != nil {
		t.Fatalf("block 0: %v", err)
	}
	if ev != nil {
		t.Error("expected no reorg event for genesis")
	}

	// Block 1 extends genesis.
	ev, err = h.ProcessNewHead(1, hashes[1], hashes[0])
	if err != nil {
		t.Fatalf("block 1: %v", err)
	}
	if ev != nil {
		t.Error("expected no reorg for normal extension")
	}

	// Block 2 extends block 1.
	ev, err = h.ProcessNewHead(2, hashes[2], hashes[1])
	if err != nil {
		t.Fatalf("block 2: %v", err)
	}
	if ev != nil {
		t.Error("expected no reorg for normal extension")
	}

	// Verify chain head.
	number, hash := h.ChainHead()
	if number != 2 || hash != hashes[2] {
		t.Errorf("expected head (2, %s), got (%d, %s)", hashes[2], number, hash)
	}

	// Verify canonical chain.
	for i := 0; i <= 2; i++ {
		if !h.IsCanonical(uint64(i), hashes[i]) {
			t.Errorf("block %d should be canonical", i)
		}
	}
}

func TestChainReorgDetection(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	// Build canonical chain: 0 -> 1 -> 2 -> 3.
	canon := []types.Hash{
		types.HexToHash("0xa0"),
		types.HexToHash("0xa1"),
		types.HexToHash("0xa2"),
		types.HexToHash("0xa3"),
	}

	h.ProcessNewHead(0, canon[0], types.Hash{})
	h.ProcessNewHead(1, canon[1], canon[0])
	h.ProcessNewHead(2, canon[2], canon[1])
	h.ProcessNewHead(3, canon[3], canon[2])

	// Now introduce a fork at block 2: new block 2' with parent = block 1.
	forkHash := types.HexToHash("0xb2")
	ev, err := h.ProcessNewHead(2, forkHash, canon[1])
	if err != nil {
		t.Fatalf("reorg: %v", err)
	}
	if ev == nil {
		t.Fatal("expected reorg event")
	}

	// Check reorg event.
	if ev.OldHead != canon[3] {
		t.Errorf("OldHead = %s, want %s", ev.OldHead, canon[3])
	}
	if ev.NewHead != forkHash {
		t.Errorf("NewHead = %s, want %s", ev.NewHead, forkHash)
	}
	if ev.Depth == 0 {
		t.Error("expected non-zero depth")
	}

	// Old blocks should include 2, 3 (removed from canonical).
	if len(ev.OldBlocks) < 1 {
		t.Error("expected at least 1 old block")
	}

	// New head should be the fork.
	number, hash := h.ChainHead()
	if number != 2 || hash != forkHash {
		t.Errorf("expected head (2, %s), got (%d, %s)", forkHash, number, hash)
	}
}

func TestChainReorgMaxDepth(t *testing.T) {
	cfg := DefaultReorgConfig()
	cfg.MaxReorgDepth = 2
	h := NewChainReorgHandler(cfg)

	// Build canonical chain: 0 -> 1 -> 2 -> 3 -> 4.
	hashes := make([]types.Hash, 5)
	for i := 0; i < 5; i++ {
		hashes[i] = types.HexToHash(fmt.Sprintf("0xc%d", i))
	}

	h.ProcessNewHead(0, hashes[0], types.Hash{})
	h.ProcessNewHead(1, hashes[1], hashes[0])
	h.ProcessNewHead(2, hashes[2], hashes[1])
	h.ProcessNewHead(3, hashes[3], hashes[2])
	h.ProcessNewHead(4, hashes[4], hashes[3])

	// Try a reorg deeper than MaxReorgDepth=2.
	// Fork at block 1: new block 2' with parent = 1.
	// This would reorg blocks 2, 3, 4 -> depth 3, exceeds limit of 2.
	forkHash := types.HexToHash("0xd1")
	_, err := h.ProcessNewHead(2, forkHash, hashes[1])
	if err != ErrReorgTooDeep {
		t.Fatalf("expected ErrReorgTooDeep, got %v", err)
	}

	// Head should be unchanged.
	number, hash := h.ChainHead()
	if number != 4 || hash != hashes[4] {
		t.Errorf("head should be unchanged, got (%d, %s)", number, hash)
	}
}

func TestChainReorgZeroHash(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	_, err := h.ProcessNewHead(1, types.Hash{}, types.Hash{})
	if err != ErrReorgZeroHash {
		t.Fatalf("expected ErrReorgZeroHash, got %v", err)
	}
}

func TestChainReorgGetSetCanonicalHash(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	hash := types.HexToHash("0xfeed")
	h.SetCanonicalHash(42, hash)

	got := h.GetCanonicalHash(42)
	if got != hash {
		t.Errorf("GetCanonicalHash(42) = %s, want %s", got, hash)
	}

	// Unknown height returns zero hash.
	got = h.GetCanonicalHash(999)
	if !got.IsZero() {
		t.Errorf("expected zero hash for unknown height, got %s", got)
	}
}

func TestChainReorgIsCanonical(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	hash := types.HexToHash("0xbabe")
	h.SetCanonicalHash(10, hash)

	if !h.IsCanonical(10, hash) {
		t.Error("expected block to be canonical")
	}

	// Wrong hash.
	if h.IsCanonical(10, types.HexToHash("0xdead")) {
		t.Error("expected non-canonical with wrong hash")
	}

	// Unknown number.
	if h.IsCanonical(999, hash) {
		t.Error("expected non-canonical for unknown number")
	}
}

func TestChainReorgHistory(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	// No history initially.
	history := h.ReorgHistory(10)
	if len(history) != 0 {
		t.Fatalf("expected 0 events, got %d", len(history))
	}

	// Build chain and trigger reorgs.
	h.ProcessNewHead(0, types.HexToHash("0xe0"), types.Hash{})
	h.ProcessNewHead(1, types.HexToHash("0xe1"), types.HexToHash("0xe0"))
	h.ProcessNewHead(2, types.HexToHash("0xe2"), types.HexToHash("0xe1"))

	// Reorg 1: fork at 1.
	h.ProcessNewHead(2, types.HexToHash("0xf2"), types.HexToHash("0xe1"))

	// Extend the new fork.
	h.ProcessNewHead(3, types.HexToHash("0xf3"), types.HexToHash("0xf2"))

	// Reorg 2: fork at 2 on the new chain.
	h.ProcessNewHead(3, types.HexToHash("0xa3"), types.HexToHash("0xf2"))

	if h.ReorgCount() != 2 {
		t.Fatalf("expected 2 reorgs, got %d", h.ReorgCount())
	}

	history = h.ReorgHistory(10)
	if len(history) != 2 {
		t.Fatalf("expected 2 events, got %d", len(history))
	}

	// Test limit.
	history = h.ReorgHistory(1)
	if len(history) != 1 {
		t.Fatalf("expected 1 event with limit=1, got %d", len(history))
	}

	// Zero or negative limit.
	if h.ReorgHistory(0) != nil {
		t.Error("expected nil for limit 0")
	}
	if h.ReorgHistory(-1) != nil {
		t.Error("expected nil for negative limit")
	}
}

func TestChainReorgMaxDepthSeen(t *testing.T) {
	cfg := DefaultReorgConfig()
	cfg.MaxReorgDepth = 100 // high enough to not block
	h := NewChainReorgHandler(cfg)

	// Build chain: 0 -> 1 -> 2 -> 3 -> 4.
	hashes := []types.Hash{
		types.HexToHash("0x10"),
		types.HexToHash("0x11"),
		types.HexToHash("0x12"),
		types.HexToHash("0x13"),
		types.HexToHash("0x14"),
	}

	h.ProcessNewHead(0, hashes[0], types.Hash{})
	h.ProcessNewHead(1, hashes[1], hashes[0])
	h.ProcessNewHead(2, hashes[2], hashes[1])
	h.ProcessNewHead(3, hashes[3], hashes[2])
	h.ProcessNewHead(4, hashes[4], hashes[3])

	// Reorg from block 3: fork at 2 -> depth = 2.
	h.ProcessNewHead(3, types.HexToHash("0x23"), hashes[2])

	if h.MaxReorgDepthSeen() == 0 {
		t.Error("expected non-zero max depth after reorg")
	}

	firstDepth := h.MaxReorgDepthSeen()

	// Build further and do a deeper reorg.
	h.ProcessNewHead(4, types.HexToHash("0x24"), types.HexToHash("0x23"))
	h.ProcessNewHead(5, types.HexToHash("0x25"), types.HexToHash("0x24"))

	// Reorg from block 3: fork at 2 -> depth = 3.
	h.ProcessNewHead(3, types.HexToHash("0x33"), hashes[2])

	secondDepth := h.MaxReorgDepthSeen()
	if secondDepth < firstDepth {
		t.Errorf("max depth should be non-decreasing: %d < %d", secondDepth, firstDepth)
	}
}

func TestChainReorgMetricsDisabled(t *testing.T) {
	cfg := DefaultReorgConfig()
	cfg.TrackMetrics = false
	cfg.NotifyOnReorg = false
	h := NewChainReorgHandler(cfg)

	// Build and reorg.
	h.ProcessNewHead(0, types.HexToHash("0x50"), types.Hash{})
	h.ProcessNewHead(1, types.HexToHash("0x51"), types.HexToHash("0x50"))
	h.ProcessNewHead(2, types.HexToHash("0x52"), types.HexToHash("0x51"))

	// Reorg at 1.
	ev, err := h.ProcessNewHead(2, types.HexToHash("0x62"), types.HexToHash("0x51"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Event is still returned even without tracking.
	if ev == nil {
		t.Fatal("expected reorg event")
	}

	// But metrics and history should not be tracked.
	if h.ReorgCount() != 0 {
		t.Errorf("expected 0 reorg count with metrics disabled, got %d", h.ReorgCount())
	}
	if h.MaxReorgDepthSeen() != 0 {
		t.Errorf("expected 0 max depth with metrics disabled, got %d", h.MaxReorgDepthSeen())
	}
	if len(h.ReorgHistory(10)) != 0 {
		t.Error("expected empty history with notifications disabled")
	}
}

func TestChainReorgConcurrency(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	// Initialize chain.
	h.ProcessNewHead(0, types.HexToHash("0x70"), types.Hash{})
	h.ProcessNewHead(1, types.HexToHash("0x71"), types.HexToHash("0x70"))

	var wg sync.WaitGroup

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ChainHead()
			h.ReorgCount()
			h.MaxReorgDepthSeen()
			h.ReorgHistory(5)
			h.GetCanonicalHash(0)
			h.IsCanonical(0, types.HexToHash("0x70"))
		}()
	}

	// Concurrent writes.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hash := types.HexToHash(fmt.Sprintf("0x8%x", idx))
			h.SetCanonicalHash(uint64(100+idx), hash)
		}(i)
	}

	wg.Wait()

	// Verify no panics occurred.
	_, hash := h.ChainHead()
	if hash.IsZero() {
		t.Error("chain head should not be zero after operations")
	}
}

func TestChainReorgEventFields(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	// Build chain: 0 -> 1 -> 2.
	h.ProcessNewHead(0, types.HexToHash("0x90"), types.Hash{})
	h.ProcessNewHead(1, types.HexToHash("0x91"), types.HexToHash("0x90"))
	h.ProcessNewHead(2, types.HexToHash("0x92"), types.HexToHash("0x91"))

	// Fork at 0: new block 1'.
	forkHash := types.HexToHash("0xa1")
	ev, err := h.ProcessNewHead(1, forkHash, types.HexToHash("0x90"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev == nil {
		t.Fatal("expected reorg event")
	}

	if ev.OldHead != types.HexToHash("0x92") {
		t.Errorf("OldHead = %s, want 0x92", ev.OldHead)
	}
	if ev.NewHead != forkHash {
		t.Errorf("NewHead = %s, want %s", ev.NewHead, forkHash)
	}
	if ev.Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
	if len(ev.NewBlocks) == 0 {
		t.Error("expected at least one new block")
	}
}

func TestChainReorgFirstBlock(t *testing.T) {
	h := NewChainReorgHandler(DefaultReorgConfig())

	// Processing the very first block should always succeed without reorg.
	hash := types.HexToHash("0xae")
	ev, err := h.ProcessNewHead(0, hash, types.Hash{})
	if err != nil {
		t.Fatalf("first block failed: %v", err)
	}
	if ev != nil {
		t.Error("expected no reorg for first block")
	}

	number, got := h.ChainHead()
	if number != 0 || got != hash {
		t.Errorf("expected head (0, %s), got (%d, %s)", hash, number, got)
	}
}
