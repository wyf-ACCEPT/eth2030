package p2p

import (
	"fmt"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2028/eth2028/core/types"
)

func TestNewBlockGossipHandler(t *testing.T) {
	cfg := DefaultBlockGossipConfig()
	h := NewBlockGossipHandler(cfg)

	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", h.PeerCount())
	}
	stats := h.Stats()
	if stats.Announced != 0 || stats.Propagated != 0 || stats.Duplicates != 0 {
		t.Errorf("expected zero stats, got %+v", stats)
	}
}

func TestBlockGossipAddRemovePeer(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	h.AddPeer("peer1")
	h.AddPeer("peer2")
	h.AddPeer("peer3")

	if h.PeerCount() != 3 {
		t.Fatalf("expected 3 peers, got %d", h.PeerCount())
	}

	h.RemovePeer("peer2")
	if h.PeerCount() != 2 {
		t.Fatalf("expected 2 peers after remove, got %d", h.PeerCount())
	}

	// Remove non-existent peer is no-op.
	h.RemovePeer("nonexistent")
	if h.PeerCount() != 2 {
		t.Fatalf("expected 2 peers, got %d", h.PeerCount())
	}

	// Empty peer ID is ignored.
	h.AddPeer("")
	if h.PeerCount() != 2 {
		t.Fatalf("expected 2 peers after empty add, got %d", h.PeerCount())
	}
}

func TestBlockGossipMaxPeers(t *testing.T) {
	cfg := DefaultBlockGossipConfig()
	cfg.MaxPeers = 3
	h := NewBlockGossipHandler(cfg)

	h.AddPeer("peer1")
	h.AddPeer("peer2")
	h.AddPeer("peer3")
	h.AddPeer("peer4") // should be rejected

	if h.PeerCount() != 3 {
		t.Fatalf("expected max 3 peers, got %d", h.PeerCount())
	}
}

func TestBlockGossipHandleAnnouncement(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	hash := types.HexToHash("0x1234")
	ann := BlockAnnouncement{
		Hash:   hash,
		Number: 100,
		TD:     big.NewInt(50000),
		PeerID: "peer1",
	}

	if err := h.HandleAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats := h.Stats()
	if stats.Announced != 1 {
		t.Errorf("expected 1 announcement, got %d", stats.Announced)
	}

	if !h.SeenBlock(hash) {
		t.Error("expected block to be seen")
	}
}

func TestBlockGossipDuplicateFiltering(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	hash := types.HexToHash("0xabcd")
	ann := BlockAnnouncement{
		Hash:   hash,
		Number: 200,
		TD:     big.NewInt(100000),
		PeerID: "peer1",
	}

	// First announcement succeeds.
	if err := h.HandleAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error on first: %v", err)
	}

	// Duplicate is rejected.
	ann.PeerID = "peer2"
	if err := h.HandleAnnouncement(ann); err != ErrBlockGossipDuplicate {
		t.Fatalf("expected ErrBlockGossipDuplicate, got %v", err)
	}

	stats := h.Stats()
	if stats.Duplicates != 1 {
		t.Errorf("expected 1 duplicate, got %d", stats.Duplicates)
	}
}

func TestBlockGossipDuplicateFilteringDisabled(t *testing.T) {
	cfg := DefaultBlockGossipConfig()
	cfg.FilterDuplicates = false
	h := NewBlockGossipHandler(cfg)

	hash := types.HexToHash("0xabcd")
	ann := BlockAnnouncement{
		Hash:   hash,
		Number: 200,
		TD:     big.NewInt(100000),
		PeerID: "peer1",
	}

	if err := h.HandleAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second announcement also succeeds when filtering is disabled.
	ann.PeerID = "peer2"
	if err := h.HandleAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error with filtering disabled: %v", err)
	}

	stats := h.Stats()
	if stats.Announced != 2 {
		t.Errorf("expected 2 announcements, got %d", stats.Announced)
	}
}

func TestBlockGossipAnnouncementValidation(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	// Zero hash should fail.
	err := h.HandleAnnouncement(BlockAnnouncement{
		Hash:   types.Hash{},
		Number: 1,
		PeerID: "peer1",
	})
	if err != ErrBlockGossipNilHash {
		t.Errorf("expected ErrBlockGossipNilHash, got %v", err)
	}

	// Empty peer ID should fail.
	err = h.HandleAnnouncement(BlockAnnouncement{
		Hash:   types.HexToHash("0x1"),
		Number: 1,
		PeerID: "",
	})
	if err != ErrBlockGossipEmptyPeer {
		t.Errorf("expected ErrBlockGossipEmptyPeer, got %v", err)
	}
}

func TestBlockGossipPropagateBlock(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	// No peers, no propagation.
	hash := types.HexToHash("0x9999")
	result := h.PropagateBlock(hash, 500)
	if len(result) != 0 {
		t.Fatalf("expected no peers selected, got %d", len(result))
	}

	// Add 9 peers -> sqrt(9) = 3 peers selected.
	for i := 0; i < 9; i++ {
		h.AddPeer(fmt.Sprintf("peer%d", i))
	}

	result = h.PropagateBlock(hash, 500)
	if len(result) != 3 {
		t.Fatalf("expected 3 peers (sqrt of 9), got %d", len(result))
	}

	// Block should be marked as seen after propagation.
	if !h.SeenBlock(hash) {
		t.Error("expected block to be seen after propagation")
	}

	stats := h.Stats()
	if stats.Propagated != 3 {
		t.Errorf("expected 3 propagated, got %d", stats.Propagated)
	}
}

func TestBlockGossipPropagateZeroHash(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())
	h.AddPeer("peer1")

	result := h.PropagateBlock(types.Hash{}, 1)
	if result != nil {
		t.Error("expected nil for zero hash propagation")
	}
}

func TestBlockGossipPropagateSinglePeer(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())
	h.AddPeer("peer1")

	hash := types.HexToHash("0xaaa")
	result := h.PropagateBlock(hash, 1)
	if len(result) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(result))
	}
	if result[0] != "peer1" {
		t.Errorf("expected peer1, got %s", result[0])
	}
}

func TestBlockGossipRecentAnnouncements(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	// No announcements.
	recent := h.RecentAnnouncements(5)
	if len(recent) != 0 {
		t.Fatalf("expected 0 recent, got %d", len(recent))
	}

	// Add several announcements.
	for i := 1; i <= 10; i++ {
		hash := types.HexToHash(fmt.Sprintf("0x%x", i))
		ann := BlockAnnouncement{
			Hash:   hash,
			Number: uint64(i),
			TD:     big.NewInt(int64(i * 1000)),
			PeerID: fmt.Sprintf("peer%d", i),
		}
		if err := h.HandleAnnouncement(ann); err != nil {
			t.Fatalf("announcement %d failed: %v", i, err)
		}
	}

	// Get last 3.
	recent = h.RecentAnnouncements(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent, got %d", len(recent))
	}
	// Should be blocks 8, 9, 10 (most recent).
	if recent[0].Number != 8 {
		t.Errorf("expected oldest in limit to be block 8, got %d", recent[0].Number)
	}
	if recent[2].Number != 10 {
		t.Errorf("expected newest in limit to be block 10, got %d", recent[2].Number)
	}

	// Limit larger than total returns all.
	recent = h.RecentAnnouncements(100)
	if len(recent) != 10 {
		t.Fatalf("expected 10, got %d", len(recent))
	}

	// Zero or negative limit returns nil.
	recent = h.RecentAnnouncements(0)
	if recent != nil {
		t.Error("expected nil for limit 0")
	}
	recent = h.RecentAnnouncements(-1)
	if recent != nil {
		t.Error("expected nil for negative limit")
	}
}

func TestBlockGossipSeenBlock(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	hash := types.HexToHash("0xbeef")
	if h.SeenBlock(hash) {
		t.Error("block should not be seen initially")
	}

	ann := BlockAnnouncement{
		Hash:   hash,
		Number: 1,
		PeerID: "peer1",
	}
	_ = h.HandleAnnouncement(ann)

	if !h.SeenBlock(hash) {
		t.Error("block should be seen after announcement")
	}
}

func TestBlockGossipStatsSnapshot(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	h.AddPeer("peer1")
	h.AddPeer("peer2")

	hash1 := types.HexToHash("0x01")
	hash2 := types.HexToHash("0x02")

	_ = h.HandleAnnouncement(BlockAnnouncement{Hash: hash1, Number: 1, PeerID: "p1"})
	_ = h.HandleAnnouncement(BlockAnnouncement{Hash: hash2, Number: 2, PeerID: "p2"})
	_ = h.HandleAnnouncement(BlockAnnouncement{Hash: hash1, Number: 1, PeerID: "p3"}) // duplicate

	h.PropagateBlock(types.HexToHash("0x03"), 3)

	stats := h.Stats()
	if stats.Announced != 2 {
		t.Errorf("Announced = %d, want 2", stats.Announced)
	}
	if stats.Duplicates != 1 {
		t.Errorf("Duplicates = %d, want 1", stats.Duplicates)
	}
	if stats.Peers != 2 {
		t.Errorf("Peers = %d, want 2", stats.Peers)
	}
}

func TestBlockGossipConcurrency(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	// Add initial peers.
	for i := 0; i < 10; i++ {
		h.AddPeer(fmt.Sprintf("peer%d", i))
	}

	var wg sync.WaitGroup

	// Concurrent announcements.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hash := types.HexToHash(fmt.Sprintf("0x%x", idx))
			ann := BlockAnnouncement{
				Hash:   hash,
				Number: uint64(idx),
				TD:     big.NewInt(int64(idx)),
				PeerID: fmt.Sprintf("sender%d", idx),
			}
			_ = h.HandleAnnouncement(ann)
		}(i)
	}

	// Concurrent propagations.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hash := types.HexToHash(fmt.Sprintf("0x%x", idx+1000))
			h.PropagateBlock(hash, uint64(idx))
		}(i)
	}

	// Concurrent peer management.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h.AddPeer(fmt.Sprintf("dynamic%d", idx))
			h.PeerCount()
			h.Stats()
			h.RemovePeer(fmt.Sprintf("dynamic%d", idx))
		}(i)
	}

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.SeenBlock(types.HexToHash("0x1"))
			h.PeerCount()
			h.Stats()
			h.RecentAnnouncements(5)
		}()
	}

	wg.Wait()

	// Basic sanity: no panics, stats are consistent.
	// Some announcements may be filtered as duplicates if propagation
	// marked the same hash as seen first.
	stats := h.Stats()
	total := stats.Announced + stats.Duplicates
	if total == 0 {
		t.Error("expected some announcements to be processed")
	}
}

func TestBlockGossipPropagateFanout(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	// Test various peer counts and expected fanout (sqrt(n)).
	tests := []struct {
		peers    int
		expected int
	}{
		{1, 1},
		{4, 2},
		{9, 3},
		{16, 4},
		{25, 5},
		{100, 10},
	}

	for _, tt := range tests {
		// Reset handler with enough capacity for all peers.
		cfg := DefaultBlockGossipConfig()
		cfg.MaxPeers = tt.peers + 10
		h = NewBlockGossipHandler(cfg)
		for i := 0; i < tt.peers; i++ {
			h.AddPeer(fmt.Sprintf("p%d", i))
		}

		hash := types.HexToHash(fmt.Sprintf("0x%x", tt.peers))
		selected := h.PropagateBlock(hash, 1)
		if len(selected) != tt.expected {
			t.Errorf("peers=%d: expected fanout %d, got %d", tt.peers, tt.expected, len(selected))
		}
	}
}

func TestBlockGossipNilTD(t *testing.T) {
	h := NewBlockGossipHandler(DefaultBlockGossipConfig())

	// TD can be nil; should still work.
	ann := BlockAnnouncement{
		Hash:   types.HexToHash("0xdead"),
		Number: 42,
		TD:     nil,
		PeerID: "peer1",
	}

	if err := h.HandleAnnouncement(ann); err != nil {
		t.Fatalf("unexpected error with nil TD: %v", err)
	}

	recent := h.RecentAnnouncements(1)
	if len(recent) != 1 {
		t.Fatal("expected 1 announcement")
	}
	if recent[0].TD != nil {
		t.Error("expected nil TD in stored announcement")
	}
}
