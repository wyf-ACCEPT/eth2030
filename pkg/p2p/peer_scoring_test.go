package p2p

import (
	"fmt"
	"sync"
	"testing"
)

func defaultScorer() *PeerScorer {
	return NewPeerScorer(DefaultPeerScoreConfig())
}

func TestPeerScorer_InitialScore(t *testing.T) {
	ps := defaultScorer()

	// Unknown peer returns initial score without registering.
	if s := ps.GetScore("unknown"); s != 100.0 {
		t.Errorf("GetScore(unknown) = %f, want 100.0", s)
	}
	if ps.PeerCount() != 0 {
		t.Errorf("PeerCount = %d, want 0 (unknown peer not registered)", ps.PeerCount())
	}

	// After recording an event the peer is registered.
	ps.RecordEvent("peer1", ScoreValidTx) // +1
	if s := ps.GetScore("peer1"); s != 101.0 {
		t.Errorf("GetScore(peer1) = %f, want 101.0", s)
	}
	if ps.PeerCount() != 1 {
		t.Errorf("PeerCount = %d, want 1", ps.PeerCount())
	}
}

func TestPeerScorer_RecordEvents(t *testing.T) {
	ps := defaultScorer()

	ps.RecordEvent("p1", ScoreValidBlock)   // 100 + 5 = 105
	ps.RecordEvent("p1", ScoreValidTx)      // 105 + 1 = 106
	ps.RecordEvent("p1", ScoreUsefulData)   // 106 + 3 = 109
	ps.RecordEvent("p1", ScoreDuplicateMsg) // 109 - 1 = 108

	info := ps.GetPeerInfo("p1")
	if info.Score != 108.0 {
		t.Errorf("Score = %f, want 108.0", info.Score)
	}
	if info.Events != 4 {
		t.Errorf("Events = %d, want 4", info.Events)
	}
	if info.Banned {
		t.Error("peer should not be banned")
	}
}

func TestPeerScorer_ScoreClamping(t *testing.T) {
	cfg := DefaultPeerScoreConfig()
	ps := NewPeerScorer(cfg)

	// Push above max.
	for i := 0; i < 50; i++ {
		ps.RecordEvent("high", ScoreValidBlock) // +5 each
	}
	if s := ps.GetScore("high"); s != cfg.MaxScore {
		t.Errorf("Score = %f, want MaxScore %f", s, cfg.MaxScore)
	}

	// Push below min.
	for i := 0; i < 30; i++ {
		ps.RecordEvent("low", ScoreInvalidBlock) // -20 each
	}
	if s := ps.GetScore("low"); s != cfg.MinScore {
		t.Errorf("Score = %f, want MinScore %f", s, cfg.MinScore)
	}
}

func TestPeerScorer_BanThreshold(t *testing.T) {
	ps := defaultScorer()

	// Initial score 100. Need to lose >150 to reach -50.
	// ScoreInvalidBlock = -20; 8 events: 100 - 160 = -60, clamped to -100.
	for i := 0; i < 8; i++ {
		ps.RecordEvent("bad", ScoreInvalidBlock)
	}

	if !ps.IsBanned("bad") {
		t.Error("peer should be banned after hitting threshold")
	}

	info := ps.GetPeerInfo("bad")
	if info.BanCount != 1 {
		t.Errorf("BanCount = %d, want 1", info.BanCount)
	}
}

func TestPeerScorer_ForceBanUnban(t *testing.T) {
	ps := defaultScorer()
	ps.RecordEvent("p1", ScoreValidTx)

	ps.BanPeer("p1")
	if !ps.IsBanned("p1") {
		t.Error("peer should be banned after BanPeer")
	}

	ps.UnbanPeer("p1")
	if ps.IsBanned("p1") {
		t.Error("peer should not be banned after UnbanPeer")
	}
	// Unban resets score to initial.
	if s := ps.GetScore("p1"); s != 100.0 {
		t.Errorf("Score after unban = %f, want 100.0", s)
	}
}

func TestPeerScorer_BanPeerUnknown(t *testing.T) {
	ps := defaultScorer()
	ps.BanPeer("new")
	if !ps.IsBanned("new") {
		t.Error("BanPeer on unknown peer should create and ban it")
	}
	if ps.PeerCount() != 1 {
		t.Errorf("PeerCount = %d, want 1", ps.PeerCount())
	}
}

func TestPeerScorer_UnbanUnknown(t *testing.T) {
	ps := defaultScorer()
	// UnbanPeer on unknown peer should be a no-op.
	ps.UnbanPeer("ghost")
	if ps.PeerCount() != 0 {
		t.Errorf("PeerCount = %d, want 0", ps.PeerCount())
	}
}

func TestPeerScorer_DecayScores(t *testing.T) {
	cfg := DefaultPeerScoreConfig()
	ps := NewPeerScorer(cfg)

	ps.RecordEvent("p1", ScoreValidBlock) // 100 + 5 = 105
	ps.DecayScores()

	expected := 105.0 * cfg.DecayFactor // 99.75
	got := ps.GetScore("p1")
	if got != expected {
		t.Errorf("Score after decay = %f, want %f", got, expected)
	}

	// Multiple decays.
	ps.DecayScores()
	expected *= cfg.DecayFactor
	got = ps.GetScore("p1")
	if got != expected {
		t.Errorf("Score after 2nd decay = %f, want %f", got, expected)
	}
}

func TestPeerScorer_TopPeers(t *testing.T) {
	ps := defaultScorer()

	// Create peers with different scores.
	ps.RecordEvent("p1", ScoreValidBlock)   // 105
	ps.RecordEvent("p2", ScoreInvalidBlock) // 80
	ps.RecordEvent("p3", ScoreUsefulData)   // 103
	ps.RecordEvent("p4", ScoreValidTx)      // 101

	top := ps.TopPeers(3)
	if len(top) != 3 {
		t.Fatalf("TopPeers(3) returned %d, want 3", len(top))
	}

	// Verify descending order.
	for i := 1; i < len(top); i++ {
		if top[i].Score > top[i-1].Score {
			t.Errorf("TopPeers not sorted: [%d].Score=%f > [%d].Score=%f",
				i, top[i].Score, i-1, top[i-1].Score)
		}
	}

	if top[0].PeerID != "p1" {
		t.Errorf("top peer = %s, want p1", top[0].PeerID)
	}
}

func TestPeerScorer_TopPeersExcludesBanned(t *testing.T) {
	ps := defaultScorer()

	ps.RecordEvent("p1", ScoreValidBlock) // 105
	ps.RecordEvent("p2", ScoreValidBlock) // 105
	ps.BanPeer("p1")

	top := ps.TopPeers(5)
	if len(top) != 1 {
		t.Fatalf("TopPeers = %d, want 1 (banned excluded)", len(top))
	}
	if top[0].PeerID != "p2" {
		t.Errorf("top peer = %s, want p2", top[0].PeerID)
	}
}

func TestPeerScorer_TopPeersMoreThanAvailable(t *testing.T) {
	ps := defaultScorer()
	ps.RecordEvent("p1", ScoreValidTx)

	top := ps.TopPeers(100)
	if len(top) != 1 {
		t.Errorf("TopPeers(100) = %d, want 1", len(top))
	}
}

func TestPeerScorer_Prune(t *testing.T) {
	ps := defaultScorer()

	ps.RecordEvent("good", ScoreValidBlock) // 105
	ps.RecordEvent("ok", ScoreValidTx)      // 101
	ps.RecordEvent("bad", ScoreInvalidBlock) // 80

	pruned := ps.PruneLowScoring(90.0)
	if len(pruned) != 1 {
		t.Fatalf("PruneLowScoring returned %d, want 1", len(pruned))
	}
	if pruned[0] != "bad" {
		t.Errorf("pruned peer = %s, want bad", pruned[0])
	}
	if ps.PeerCount() != 2 {
		t.Errorf("PeerCount after prune = %d, want 2", ps.PeerCount())
	}
}

func TestPeerScorer_PruneBannedAlways(t *testing.T) {
	ps := defaultScorer()

	ps.RecordEvent("p1", ScoreValidBlock) // 105
	ps.BanPeer("p1")

	// Even though score is above threshold, banned peers are pruned.
	pruned := ps.PruneLowScoring(0.0)
	if len(pruned) != 1 {
		t.Fatalf("pruned count = %d, want 1", len(pruned))
	}
}

func TestPeerScorer_ConcurrentAccess(t *testing.T) {
	ps := defaultScorer()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("peer%d", n%10)
			ps.RecordEvent(id, ScoreValidTx)
			ps.GetScore(id)
			ps.IsBanned(id)
			ps.GetPeerInfo(id)
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ps.DecayScores()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ps.TopPeers(5)
	}()

	wg.Wait()

	// Just verify no panic or deadlock; count should be 10 unique peers.
	if ps.PeerCount() != 10 {
		t.Errorf("PeerCount = %d, want 10", ps.PeerCount())
	}
}

func TestPeerScorer_MultipleBanIncrements(t *testing.T) {
	ps := defaultScorer()

	// Drive to ban.
	for i := 0; i < 8; i++ {
		ps.RecordEvent("p1", ScoreInvalidBlock)
	}
	if !ps.IsBanned("p1") {
		t.Fatal("should be banned")
	}

	info := ps.GetPeerInfo("p1")
	if info.BanCount != 1 {
		t.Errorf("BanCount = %d, want 1", info.BanCount)
	}

	// Unban, then re-ban.
	ps.UnbanPeer("p1")
	for i := 0; i < 8; i++ {
		ps.RecordEvent("p1", ScoreInvalidBlock)
	}
	info = ps.GetPeerInfo("p1")
	if info.BanCount != 2 {
		t.Errorf("BanCount after re-ban = %d, want 2", info.BanCount)
	}
}

func TestScoreEvent_Values(t *testing.T) {
	// Verify the expected deltas for documentation correctness.
	tests := []struct {
		event ScoreEvent
		want  float64
	}{
		{ScoreValidBlock, 5},
		{ScoreInvalidBlock, -20},
		{ScoreValidTx, 1},
		{ScoreInvalidTx, -5},
		{ScoreTimedOut, -10},
		{ScoreReconnect, 2},
		{ScoreDuplicateMsg, -1},
		{ScoreUsefulData, 3},
	}
	for _, tt := range tests {
		if float64(tt.event) != tt.want {
			t.Errorf("ScoreEvent %f != %f", float64(tt.event), tt.want)
		}
	}
}
