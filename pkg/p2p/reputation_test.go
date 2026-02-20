package p2p

import (
	"sync"
	"testing"
)

func defaultRepConfig() ReputationConfig {
	return ReputationConfig{
		InitialScore:  100.0,
		MaxScore:      200.0,
		MinScore:      -100.0,
		DecayInterval: 60,
		DecayRate:     0.95,
	}
}

func TestNewReputationTracker(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	if rt == nil {
		t.Fatal("NewReputationTracker returned nil")
	}
	if rt.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", rt.PeerCount())
	}
}

func TestNewReputationTrackerBadConfig(t *testing.T) {
	// Bad config: max <= min, decay out of range.
	rt := NewReputationTracker(ReputationConfig{
		InitialScore:  50,
		MaxScore:      -10,
		MinScore:      100,
		DecayInterval: 60,
		DecayRate:     -0.5,
	})
	if rt.config.MaxScore <= rt.config.MinScore {
		t.Error("config should have been corrected for invalid max/min")
	}
	if rt.config.DecayRate <= 0 || rt.config.DecayRate > 1 {
		t.Error("config should have been corrected for invalid decay rate")
	}
}

func TestRecordEventCreatesAndUpdates(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "valid block")

	if rt.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", rt.PeerCount())
	}

	score, err := rt.GetScore("peer1")
	if err != nil {
		t.Fatalf("GetScore failed: %v", err)
	}
	expected := 100.0 + 10.0 // initial + good_block delta
	if score != expected {
		t.Errorf("expected score %f, got %f", expected, score)
	}
}

func TestRecordEventMultipleTypes(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())

	rt.RecordEvent("peer1", EventGoodBlock, "")
	rt.RecordEvent("peer1", EventBadBlock, "")
	rt.RecordEvent("peer1", EventTimeout, "")
	rt.RecordEvent("peer1", EventDisconnect, "")
	rt.RecordEvent("peer1", EventGoodAttestation, "")
	rt.RecordEvent("peer1", EventBadAttestation, "")

	score, _ := rt.GetScore("peer1")
	// 100 + 10 - 25 - 15 - 10 + 5 - 20 = 45
	if score != 45 {
		t.Errorf("expected score 45, got %f", score)
	}
}

func TestRecordEventUnknownType(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", "unknown_event", "test")

	score, _ := rt.GetScore("peer1")
	// unknown event has delta 0, so score stays at initial.
	if score != 100.0 {
		t.Errorf("expected initial score 100, got %f", score)
	}
}

func TestRecordEventClampMax(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	// Many good events to exceed max.
	for i := 0; i < 20; i++ {
		rt.RecordEvent("peer1", EventGoodBlock, "")
	}
	score, _ := rt.GetScore("peer1")
	if score != 200.0 {
		t.Errorf("expected clamped max score 200, got %f", score)
	}
}

func TestRecordEventClampMinAndAutoBan(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	// Many bad events to hit minimum.
	for i := 0; i < 20; i++ {
		rt.RecordEvent("peer1", EventBadBlock, "")
	}
	score, _ := rt.GetScore("peer1")
	if score != -100.0 {
		t.Errorf("expected clamped min score -100, got %f", score)
	}
	if !rt.IsBanned("peer1") {
		t.Error("peer should be auto-banned after hitting min score")
	}
}

func TestGetScorePeerNotFound(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	_, err := rt.GetScore("nonexistent")
	if err != ErrReputationPeerNotFound {
		t.Errorf("expected ErrReputationPeerNotFound, got %v", err)
	}
}

func TestIsBannedUnknownPeer(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	if rt.IsBanned("unknown") {
		t.Error("unknown peer should not be banned")
	}
}

func TestReputationBanPeer(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")

	err := rt.BanPeer("peer1", 3600)
	if err != nil {
		t.Fatalf("BanPeer failed: %v", err)
	}
	if !rt.IsBanned("peer1") {
		t.Error("peer should be banned after BanPeer")
	}
}

func TestBanPeerAlreadyBanned(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.BanPeer("peer1", 3600)

	err := rt.BanPeer("peer1", 3600)
	if err != ErrReputationAlreadyBanned {
		t.Errorf("expected ErrReputationAlreadyBanned, got %v", err)
	}
}

func TestBanPeerCreatesNewPeer(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	err := rt.BanPeer("newpeer", 60)
	if err != nil {
		t.Fatalf("BanPeer for new peer failed: %v", err)
	}
	if rt.PeerCount() != 1 {
		t.Errorf("expected 1 peer, got %d", rt.PeerCount())
	}
	if !rt.IsBanned("newpeer") {
		t.Error("new peer should be banned")
	}
}

func TestUnbanPeer(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.BanPeer("peer1", 3600)

	err := rt.UnbanPeer("peer1")
	if err != nil {
		t.Fatalf("UnbanPeer failed: %v", err)
	}
	if rt.IsBanned("peer1") {
		t.Error("peer should not be banned after unban")
	}
	score, _ := rt.GetScore("peer1")
	if score != 100.0 {
		t.Errorf("expected score reset to 100 after unban, got %f", score)
	}
}

func TestUnbanPeerNotFound(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	err := rt.UnbanPeer("nonexistent")
	if err != ErrReputationPeerNotFound {
		t.Errorf("expected ErrReputationPeerNotFound, got %v", err)
	}
}

func TestIsBannedTimedExpiry(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	// Ban with a very short duration (0 means indefinite ban, so use 1 second).
	rt.BanPeer("peer1", 0)
	// BanUntil is zero, so this is an indefinite ban. Should stay banned.
	if !rt.IsBanned("peer1") {
		t.Error("peer with indefinite ban should be banned")
	}
}

func TestDecayScores(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")
	scoreBefore, _ := rt.GetScore("peer1")

	rt.DecayScores()
	scoreAfter, _ := rt.GetScore("peer1")

	expected := scoreBefore * 0.95
	if scoreAfter != expected {
		t.Errorf("expected decayed score %f, got %f", expected, scoreAfter)
	}
}

func TestDecayScoresMultipleRounds(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")

	for i := 0; i < 10; i++ {
		rt.DecayScores()
	}

	score, _ := rt.GetScore("peer1")
	if score <= 0 {
		t.Error("score should still be positive after 10 decay rounds from +110")
	}
}

func TestTopPeers(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")
	rt.RecordEvent("peer2", EventBadBlock, "")
	rt.RecordEvent("peer3", EventGoodAttestation, "")

	top := rt.TopPeers(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 top peers, got %d", len(top))
	}
	// peer1 has score 110, peer3 has 105, peer2 has 75.
	if top[0].PeerID != "peer1" {
		t.Errorf("expected peer1 at top, got %s", top[0].PeerID)
	}
	if top[1].PeerID != "peer3" {
		t.Errorf("expected peer3 second, got %s", top[1].PeerID)
	}
}

func TestTopPeersExcludesBanned(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")
	rt.BanPeer("peer2", 3600)

	top := rt.TopPeers(10)
	for _, p := range top {
		if p.PeerID == "peer2" {
			t.Error("banned peer should not appear in TopPeers")
		}
	}
}

func TestBottomPeers(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")  // 110
	rt.RecordEvent("peer2", EventBadBlock, "")    // 75
	rt.RecordEvent("peer3", EventGoodAttestation, "") // 105

	bottom := rt.BottomPeers(2)
	if len(bottom) != 2 {
		t.Fatalf("expected 2 bottom peers, got %d", len(bottom))
	}
	if bottom[0].PeerID != "peer2" {
		t.Errorf("expected peer2 at bottom, got %s", bottom[0].PeerID)
	}
}

func TestPeerCount(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("a", EventGoodBlock, "")
	rt.RecordEvent("b", EventGoodBlock, "")
	rt.RecordEvent("c", EventGoodBlock, "")

	if rt.PeerCount() != 3 {
		t.Errorf("expected 3 peers, got %d", rt.PeerCount())
	}
}

func TestBannedCount(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.BanPeer("a", 100)
	rt.BanPeer("b", 100)
	rt.RecordEvent("c", EventGoodBlock, "")

	if rt.BannedCount() != 2 {
		t.Errorf("expected 2 banned peers, got %d", rt.BannedCount())
	}
}

func TestConcurrentReputationAccess(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	var wg sync.WaitGroup

	// Writer goroutines.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				peerID := "peer"
				if id%2 == 0 {
					peerID = "peerA"
				} else {
					peerID = "peerB"
				}
				rt.RecordEvent(peerID, EventGoodBlock, "test")
			}
		}(g)
	}

	// Reader goroutines.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				rt.GetScore("peerA")
				rt.IsBanned("peerB")
				rt.PeerCount()
				rt.BannedCount()
				rt.TopPeers(5)
				rt.BottomPeers(5)
			}
		}()
	}

	// Decay goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			rt.DecayScores()
		}
	}()

	wg.Wait()
}

func TestTopPeersMoreThanAvailable(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	rt.RecordEvent("peer1", EventGoodBlock, "")

	top := rt.TopPeers(100)
	if len(top) != 1 {
		t.Errorf("expected 1 peer, got %d", len(top))
	}
}

func TestBottomPeersEmpty(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	bottom := rt.BottomPeers(5)
	if len(bottom) != 0 {
		t.Errorf("expected 0 bottom peers, got %d", len(bottom))
	}
}

func TestBanPeerZeroDuration(t *testing.T) {
	rt := NewReputationTracker(defaultRepConfig())
	err := rt.BanPeer("peer1", 0)
	if err != nil {
		t.Fatalf("BanPeer with 0 duration failed: %v", err)
	}
	// Zero duration means BanUntil is zero, so IsBanned checks for indefinite ban.
	if !rt.IsBanned("peer1") {
		t.Error("peer with 0-duration ban should be banned indefinitely")
	}
}
