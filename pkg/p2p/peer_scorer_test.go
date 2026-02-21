package p2p

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"
)

func newBehaviorScorer() *BehaviorScorer {
	return NewBehaviorScorer(DefaultBehaviorScorerConfig())
}

func TestBehaviorScorer_InitialScore(t *testing.T) {
	bs := newBehaviorScorer()
	score := bs.CompositeScore("unknown")
	if score != 50.0 {
		t.Errorf("CompositeScore(unknown) = %f, want 50.0", score)
	}
	if bs.PeerCount() != 0 {
		t.Errorf("PeerCount = %d, want 0", bs.PeerCount())
	}
}

func TestBehaviorScorer_RecordValidBlock(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1")
	m := bs.GetMetrics("p1")
	if m.ValidBlocks != 1 {
		t.Errorf("ValidBlocks = %d, want 1", m.ValidBlocks)
	}
	if m.Score != 55.0 {
		t.Errorf("Score = %f, want 55.0", m.Score)
	}
}

func TestBehaviorScorer_RecordInvalidMessage(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordInvalidMessage("p1")
	m := bs.GetMetrics("p1")
	if m.InvalidMsgs != 1 {
		t.Errorf("InvalidMsgs = %d, want 1", m.InvalidMsgs)
	}
	// 50 - 15 = 35
	if m.Score != 35.0 {
		t.Errorf("Score = %f, want 35.0", m.Score)
	}
}

func TestBehaviorScorer_RecordTimeout(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordTimeout("p1")
	m := bs.GetMetrics("p1")
	if m.TimedOut != 1 {
		t.Errorf("TimedOut = %d, want 1", m.TimedOut)
	}
	// 50 - 8 = 42
	if m.Score != 42.0 {
		t.Errorf("Score = %f, want 42.0", m.Score)
	}
}

func TestBehaviorScorer_AutoBan(t *testing.T) {
	bs := newBehaviorScorer()
	// Initial 50, each invalid -15. After 7: 50 - 105 = -55, clamped to -55, banned at threshold -50.
	for i := 0; i < 7; i++ {
		bs.RecordInvalidMessage("bad")
	}
	if !bs.IsBanned("bad") {
		t.Error("peer should be banned after threshold breach")
	}
	m := bs.GetMetrics("bad")
	if m.BanCount != 1 {
		t.Errorf("BanCount = %d, want 1", m.BanCount)
	}
}

func TestBehaviorScorer_ScoreClamping(t *testing.T) {
	cfg := DefaultBehaviorScorerConfig()
	bs := NewBehaviorScorer(cfg)
	// Push above max.
	for i := 0; i < 50; i++ {
		bs.RecordValidBlock("high")
	}
	m := bs.GetMetrics("high")
	if m.Score != cfg.MaxScore {
		t.Errorf("Score = %f, want max %f", m.Score, cfg.MaxScore)
	}
	// Push below min.
	for i := 0; i < 30; i++ {
		bs.RecordInvalidMessage("low")
	}
	mLow := bs.GetMetrics("low")
	if mLow.Score != cfg.MinScore {
		t.Errorf("Score = %f, want min %f", mLow.Score, cfg.MinScore)
	}
}

func TestBehaviorScorer_BanUnban(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1")
	bs.BanPeer("p1")
	if !bs.IsBanned("p1") {
		t.Error("peer should be banned after BanPeer")
	}
	bs.UnbanPeer("p1")
	if bs.IsBanned("p1") {
		t.Error("peer should not be banned after UnbanPeer")
	}
	// Score should be reset to initial.
	m := bs.GetMetrics("p1")
	if m.Score != 50.0 {
		t.Errorf("Score after unban = %f, want 50.0", m.Score)
	}
}

func TestBehaviorScorer_UnbanUnknown(t *testing.T) {
	bs := newBehaviorScorer()
	bs.UnbanPeer("ghost")
	if bs.PeerCount() != 0 {
		t.Errorf("PeerCount = %d, want 0", bs.PeerCount())
	}
}

func TestBehaviorScorer_RecordLatency(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordLatency("p1", 50*time.Millisecond)
	m := bs.GetMetrics("p1")
	if m.LatencySamples != 1 {
		t.Errorf("LatencySamples = %d, want 1", m.LatencySamples)
	}
	if m.AvgLatencyMs != 50.0 {
		t.Errorf("AvgLatencyMs = %f, want 50.0", m.AvgLatencyMs)
	}
	// Record a second sample; EWMA alpha=0.3: 0.3*200 + 0.7*50 = 60+35 = 95.
	bs.RecordLatency("p1", 200*time.Millisecond)
	m = bs.GetMetrics("p1")
	if m.LatencySamples != 2 {
		t.Errorf("LatencySamples = %d, want 2", m.LatencySamples)
	}
	expected := 0.3*200.0 + 0.7*50.0
	if math.Abs(m.AvgLatencyMs-expected) > 0.01 {
		t.Errorf("AvgLatencyMs = %f, want %f", m.AvgLatencyMs, expected)
	}
}

func TestBehaviorScorer_LatencyBonusLow(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordLatency("fast", 50*time.Millisecond) // Below target (100ms).
	score := bs.CompositeScore("fast")
	// Base 50 + latency bonus 10 = 60.
	if score != 60.0 {
		t.Errorf("CompositeScore = %f, want 60.0", score)
	}
}

func TestBehaviorScorer_LatencyPenaltyHigh(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordLatency("slow", 6*time.Second) // Above penalty (5000ms).
	score := bs.CompositeScore("slow")
	// Base 50 + latency penalty -10 = 40.
	if score != 40.0 {
		t.Errorf("CompositeScore = %f, want 40.0", score)
	}
}

func TestBehaviorScorer_ColocationPenalty(t *testing.T) {
	bs := newBehaviorScorer()
	// Register 4 peers on the same /24.
	bs.RegisterPeer("p1", "192.168.1.10")
	bs.RegisterPeer("p2", "192.168.1.20")
	bs.RegisterPeer("p3", "192.168.1.30")
	bs.RegisterPeer("p4", "192.168.1.40")

	// MaxColocationPeers=2. p1 has 3 co-located peers.
	// excess = 3 - 2 = 1, penalty = 1 * -5 = -5.
	score := bs.CompositeScore("p1")
	// Base 50 + colocation -5 = 45.
	if score != 45.0 {
		t.Errorf("CompositeScore(p1) = %f, want 45.0", score)
	}
}

func TestBehaviorScorer_NoColocationPenaltyDifferentSubnets(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RegisterPeer("p1", "192.168.1.10")
	bs.RegisterPeer("p2", "10.0.0.20")
	score := bs.CompositeScore("p1")
	// No colocation penalty (different subnets).
	if score != 50.0 {
		t.Errorf("CompositeScore(p1) = %f, want 50.0", score)
	}
}

func TestBehaviorScorer_DecayScores(t *testing.T) {
	cfg := DefaultBehaviorScorerConfig()
	bs := NewBehaviorScorer(cfg)
	bs.RecordValidBlock("p1") // Score = 55
	bs.DecayScores()
	m := bs.GetMetrics("p1")
	expected := 55.0 * cfg.DecayFactor
	if math.Abs(m.Score-expected) > 0.001 {
		t.Errorf("Score after decay = %f, want %f", m.Score, expected)
	}
}

func TestBehaviorScorer_RankedPeers(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1")   // 55
	bs.RecordValidBlock("p1")   // 60
	bs.RecordValidBlock("p2")   // 55
	bs.RecordTimeout("p3")      // 42
	bs.RecordInvalidMessage("p4") // 35

	ranked := bs.RankedPeers(3)
	if len(ranked) != 3 {
		t.Fatalf("RankedPeers(3) = %d, want 3", len(ranked))
	}
	if ranked[0].PeerID != "p1" {
		t.Errorf("top ranked = %s, want p1", ranked[0].PeerID)
	}
}

func TestBehaviorScorer_RankedPeersExcludesBanned(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1")
	bs.RecordValidBlock("p2")
	bs.BanPeer("p1")
	ranked := bs.RankedPeers(10)
	if len(ranked) != 1 {
		t.Fatalf("RankedPeers = %d, want 1", len(ranked))
	}
	if ranked[0].PeerID != "p2" {
		t.Errorf("ranked peer = %s, want p2", ranked[0].PeerID)
	}
}

func TestBehaviorScorer_RemovePeer(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1")
	bs.RemovePeer("p1")
	if bs.PeerCount() != 0 {
		t.Errorf("PeerCount = %d, want 0", bs.PeerCount())
	}
}

func TestBehaviorScorer_BannedPeers(t *testing.T) {
	bs := newBehaviorScorer()
	bs.BanPeer("bad1")
	bs.BanPeer("bad2")
	bs.RecordValidBlock("good")
	banned := bs.BannedPeers()
	if len(banned) != 2 {
		t.Fatalf("BannedPeers = %d, want 2", len(banned))
	}
}

func TestBehaviorScorer_SubnetPeerCount(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RegisterPeer("p1", "10.0.1.1")
	bs.RegisterPeer("p2", "10.0.1.2")
	bs.RegisterPeer("p3", "10.0.2.1") // Different subnet.
	count := bs.SubnetPeerCount("10.0.1.5")
	if count != 2 {
		t.Errorf("SubnetPeerCount = %d, want 2", count)
	}
}

func TestBehaviorScorer_ConcurrentAccess(t *testing.T) {
	bs := newBehaviorScorer()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("peer%d", n%10)
			bs.RecordValidBlock(id)
			bs.RecordLatency(id, time.Duration(n)*time.Millisecond)
			bs.CompositeScore(id)
			bs.GetMetrics(id)
			bs.IsBanned(id)
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		bs.DecayScores()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		bs.RankedPeers(5)
	}()
	wg.Wait()
	if bs.PeerCount() != 10 {
		t.Errorf("PeerCount = %d, want 10", bs.PeerCount())
	}
}

func TestBehaviorScorer_StaleInactivePeers(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1")
	// No stale peers immediately.
	stale := bs.StaleInactivePeers(time.Minute)
	if len(stale) != 0 {
		t.Errorf("StaleInactivePeers = %d, want 0", len(stale))
	}
	// With zero threshold, all peers are stale.
	stale = bs.StaleInactivePeers(0)
	if len(stale) != 1 {
		t.Errorf("StaleInactivePeers(0) = %d, want 1", len(stale))
	}
}

func TestBehaviorScorer_ScoreDistribution(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RecordValidBlock("p1") // 55
	bs.RecordValidBlock("p2") // 55
	bs.RecordTimeout("p3")    // 42

	min, max, mean, median := bs.ScoreDistribution()
	if min != 42.0 {
		t.Errorf("min = %f, want 42.0", min)
	}
	if max != 55.0 {
		t.Errorf("max = %f, want 55.0", max)
	}
	expectedMean := (55.0 + 55.0 + 42.0) / 3.0
	expectedMean = math.Round(expectedMean*100) / 100
	if mean != expectedMean {
		t.Errorf("mean = %f, want %f", mean, expectedMean)
	}
	if median != 55.0 {
		t.Errorf("median = %f, want 55.0", median)
	}
}

func TestBehaviorScorer_NormalizeIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1:30303", "192.168.1.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"[::1]:8080", "::1"},
	}
	for _, tt := range tests {
		got := normalizeIP(tt.input)
		if got != tt.want {
			t.Errorf("normalizeIP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBehaviorScorer_SubnetOf(t *testing.T) {
	tests := []struct {
		ip    string
		empty bool
	}{
		{"192.168.1.5", false},
		{"10.0.0.1", false},
		{"invalid", true},
		{"", true},
	}
	for _, tt := range tests {
		got := subnetOf(tt.ip)
		if tt.empty && got != "" {
			t.Errorf("subnetOf(%q) = %q, want empty", tt.ip, got)
		}
		if !tt.empty && got == "" {
			t.Errorf("subnetOf(%q) = empty, want non-empty", tt.ip)
		}
	}
}

func TestBehaviorScorer_MultipleBanCycles(t *testing.T) {
	bs := newBehaviorScorer()
	// Ban via threshold.
	for i := 0; i < 7; i++ {
		bs.RecordInvalidMessage("p1")
	}
	if !bs.IsBanned("p1") {
		t.Fatal("should be banned")
	}
	bs.UnbanPeer("p1")
	for i := 0; i < 7; i++ {
		bs.RecordInvalidMessage("p1")
	}
	m := bs.GetMetrics("p1")
	if m.BanCount != 2 {
		t.Errorf("BanCount = %d, want 2", m.BanCount)
	}
}

func TestBehaviorScorer_RegisterPeerUpdatesIP(t *testing.T) {
	bs := newBehaviorScorer()
	bs.RegisterPeer("p1", "192.168.1.1:30303")
	m := bs.GetMetrics("p1")
	if m.IP != "192.168.1.1" {
		t.Errorf("IP = %q, want 192.168.1.1", m.IP)
	}
	bs.RegisterPeer("p1", "10.0.0.1")
	m = bs.GetMetrics("p1")
	if m.IP != "10.0.0.1" {
		t.Errorf("IP after update = %q, want 10.0.0.1", m.IP)
	}
}

func TestBehaviorScorer_EmptyScoreDistribution(t *testing.T) {
	bs := newBehaviorScorer()
	min, max, mean, median := bs.ScoreDistribution()
	if min != 0 || max != 0 || mean != 0 || median != 0 {
		t.Errorf("empty distribution: min=%f max=%f mean=%f median=%f", min, max, mean, median)
	}
}
