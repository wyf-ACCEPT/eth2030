package p2p

import (
	"testing"
	"time"
)

func TestPeerRep_AdjustScore(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	composite, err := pr.RepAdjustScore("peer1", RepCatProtocol, 10.0)
	if err != nil {
		t.Fatalf("RepAdjustScore failed: %v", err)
	}
	if composite == 0 {
		t.Fatal("expected non-zero composite score")
	}

	entry, err := pr.RepGetEntry("peer1")
	if err != nil {
		t.Fatalf("RepGetEntry failed: %v", err)
	}
	if entry.EventCount != 1 {
		t.Fatalf("expected 1 event, got %d", entry.EventCount)
	}
	if entry.PeerID != "peer1" {
		t.Fatalf("expected peer1, got %s", entry.PeerID)
	}
}

func TestPeerRep_AdjustScoreInvalidCategory(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	_, err := pr.RepAdjustScore("peer1", RepCategory(-1), 10.0)
	if err != ErrRepInvalidCategory {
		t.Fatalf("expected ErrRepInvalidCategory, got %v", err)
	}

	_, err = pr.RepAdjustScore("peer1", repCatCount, 10.0)
	if err != ErrRepInvalidCategory {
		t.Fatalf("expected ErrRepInvalidCategory for sentinel, got %v", err)
	}
}

func TestPeerRep_GetComposite(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	// Adjust to create the peer.
	pr.RepAdjustScore("peer1", RepCatLatency, 5.0)

	composite, err := pr.RepGetComposite("peer1")
	if err != nil {
		t.Fatalf("RepGetComposite failed: %v", err)
	}
	if composite == 0 {
		t.Fatal("expected non-zero composite")
	}

	// Untracked peer.
	_, err = pr.RepGetComposite("nonexistent")
	if err != ErrRepPeerNotTracked {
		t.Fatalf("expected ErrRepPeerNotTracked, got %v", err)
	}
}

func TestPeerRep_GetCategoryScore(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepAdjustScore("peer1", RepCatBandwidth, 20.0)

	score, err := pr.RepGetCategoryScore("peer1", RepCatBandwidth)
	if err != nil {
		t.Fatalf("RepGetCategoryScore failed: %v", err)
	}
	// Should be higher than initial due to positive adjustment.
	if score <= cfg.InitialScore {
		t.Fatalf("expected score > %f, got %f", cfg.InitialScore, score)
	}
}

func TestPeerRep_BanTemp(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepBanTemp("peer1", BanReasonSpam, 10*time.Minute)

	if !pr.RepIsBanned("peer1") {
		t.Fatal("expected peer1 to be banned")
	}

	entry, err := pr.RepGetEntry("peer1")
	if err != nil {
		t.Fatalf("RepGetEntry failed: %v", err)
	}
	if !entry.Banned {
		t.Fatal("expected entry to be banned")
	}
	if entry.Permanent {
		t.Fatal("expected non-permanent ban")
	}
	if entry.BanRsn != BanReasonSpam {
		t.Fatalf("expected BanReasonSpam, got %v", entry.BanRsn)
	}
}

func TestPeerRep_BanPermanent(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepBanPermanent("peer1", BanReasonDoS)

	if !pr.RepIsBanned("peer1") {
		t.Fatal("expected peer1 to be permanently banned")
	}

	entry, _ := pr.RepGetEntry("peer1")
	if !entry.Permanent {
		t.Fatal("expected permanent ban")
	}
}

func TestPeerRep_TempBanExpires(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	// Ban with already-expired duration.
	pr.RepBanTemp("peer1", BanReasonSpam, -1*time.Second)

	// RepIsBanned should auto-lift expired ban.
	if pr.RepIsBanned("peer1") {
		t.Fatal("expected expired ban to be lifted")
	}
}

func TestPeerRep_UnbanPeerRep(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepBanPermanent("peer1", BanReasonInvalidBlocks)
	if !pr.RepIsBanned("peer1") {
		t.Fatal("expected peer1 to be banned")
	}

	err := pr.RepUnban("peer1")
	if err != nil {
		t.Fatalf("RepUnban failed: %v", err)
	}
	if pr.RepIsBanned("peer1") {
		t.Fatal("expected peer1 to be unbanned")
	}
}

func TestPeerRep_UnbanPeerRepNotTracked(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	err := pr.RepUnban("nonexistent")
	if err != ErrRepPeerNotTracked {
		t.Fatalf("expected ErrRepPeerNotTracked, got %v", err)
	}
}

func TestPeerRep_AdjustBannedPeerReturnsError(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepBanPermanent("peer1", BanReasonDoS)

	_, err := pr.RepAdjustScore("peer1", RepCatProtocol, 10.0)
	if err != ErrRepPeerBanned {
		t.Fatalf("expected ErrRepPeerBanned, got %v", err)
	}
}

func TestPeerRep_AutoBanOnLowScore(t *testing.T) {
	cfg := DefaultRepConfig()
	cfg.BanThreshold = -20.0
	pr := NewPeerRep(cfg)

	// Drive score very low with large negative deltas across ALL categories
	// so the composite (weighted sum) can reach the ban threshold.
	// Adjusting only one category cannot bring composite below threshold
	// because other categories remain at their initial value.
	for i := 0; i < 100; i++ {
		var banned bool
		for _, cat := range []RepCategory{RepCatProtocol, RepCatLatency, RepCatBandwidth, RepCatAvailability} {
			_, err := pr.RepAdjustScore("peer1", cat, -100.0)
			if err == ErrRepPeerBanned {
				banned = true
				break
			}
		}
		if banned {
			break
		}
	}

	if !pr.RepIsBanned("peer1") {
		t.Fatal("expected peer1 to be auto-banned after large negative adjustments")
	}
}

func TestPeerRep_DecayAll(t *testing.T) {
	cfg := DefaultRepConfig()
	cfg.DecayAlpha = 0.5
	pr := NewPeerRep(cfg)

	// Create peer with above-initial scores via positive adjustments.
	pr.RepAdjustScore("peer1", RepCatProtocol, 50.0)

	scoreBefore, _ := pr.RepGetCategoryScore("peer1", RepCatProtocol)
	pr.RepDecayAll()
	scoreAfter, _ := pr.RepGetCategoryScore("peer1", RepCatProtocol)

	// After decay toward initial (100.0), the score should move closer to 100.
	// new = 0.5 * scoreBefore + 0.5 * 100
	expected := 0.5*scoreBefore + 0.5*cfg.InitialScore
	diff := scoreAfter - expected
	if diff < -0.01 || diff > 0.01 {
		t.Fatalf("expected score ~%f, got %f", expected, scoreAfter)
	}
}

func TestPeerRep_HighRepPeers(t *testing.T) {
	cfg := DefaultRepConfig()
	cfg.HighRepThreshold = 110.0
	pr := NewPeerRep(cfg)

	// Boost peer1 above threshold.
	for i := 0; i < 10; i++ {
		pr.RepAdjustScore("peer1", RepCatProtocol, 50.0)
		pr.RepAdjustScore("peer1", RepCatLatency, 50.0)
		pr.RepAdjustScore("peer1", RepCatBandwidth, 50.0)
		pr.RepAdjustScore("peer1", RepCatAvailability, 50.0)
	}

	// peer2 stays at initial.
	pr.RepAdjustScore("peer2", RepCatProtocol, 0.0)

	high := pr.RepHighPeers()
	// peer1 should be in high rep list (if score exceeds threshold).
	found := false
	for _, e := range high {
		if e.PeerID == "peer1" {
			found = true
		}
	}
	if !found {
		comp, _ := pr.RepGetComposite("peer1")
		t.Fatalf("expected peer1 in high rep peers (composite=%f, threshold=%f)", comp, cfg.HighRepThreshold)
	}
}

func TestPeerRep_TrackedAndBannedCount(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepAdjustScore("a", RepCatProtocol, 1.0)
	pr.RepAdjustScore("b", RepCatProtocol, 1.0)
	pr.RepBanPermanent("c", BanReasonDoS)

	if pr.RepTrackedCount() != 3 {
		t.Fatalf("expected 3 tracked, got %d", pr.RepTrackedCount())
	}
	if pr.RepBannedCount() != 1 {
		t.Fatalf("expected 1 banned, got %d", pr.RepBannedCount())
	}
}

func TestPeerRep_BanLog(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepBanTemp("peer1", BanReasonSpam, time.Hour)
	pr.RepBanPermanent("peer2", BanReasonDoS)

	log := pr.RepBanLog()
	if len(log) != 2 {
		t.Fatalf("expected 2 ban records, got %d", len(log))
	}
	if log[0].PeerID != "peer1" || log[0].Reason != BanReasonSpam {
		t.Fatalf("unexpected first ban record: %+v", log[0])
	}
	if log[1].PeerID != "peer2" || log[1].Reason != BanReasonDoS || !log[1].Permanent {
		t.Fatalf("unexpected second ban record: %+v", log[1])
	}
}

func TestPeerRep_RemoveRepPeer(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepAdjustScore("peer1", RepCatProtocol, 5.0)
	pr.RepRemovePeer("peer1")

	if pr.RepTrackedCount() != 0 {
		t.Fatalf("expected 0 tracked after removal, got %d", pr.RepTrackedCount())
	}
}

func TestPeerRep_IsPeerBannedUntracked(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	if pr.RepIsBanned("unknown") {
		t.Fatal("expected untracked peer to not be banned")
	}
}

func TestPeerRep_CategoryScoreClamping(t *testing.T) {
	cfg := DefaultRepConfig()
	cfg.MaxCategoryScore = 200.0
	pr := NewPeerRep(cfg)

	// Apply huge positive delta.
	for i := 0; i < 100; i++ {
		pr.RepAdjustScore("peer1", RepCatProtocol, 1000.0)
	}

	score, _ := pr.RepGetCategoryScore("peer1", RepCatProtocol)
	if score > cfg.MaxCategoryScore {
		t.Fatalf("expected score <= %f, got %f", cfg.MaxCategoryScore, score)
	}
}

func TestPeerRep_SelectPeersWeighted(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	// Create some peers.
	pr.RepAdjustScore("a", RepCatProtocol, 10.0)
	pr.RepAdjustScore("b", RepCatProtocol, 20.0)
	pr.RepAdjustScore("c", RepCatProtocol, 5.0)

	selected := pr.RepSelectWeighted(2)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected peers, got %d", len(selected))
	}

	// Verify uniqueness.
	seen := make(map[string]bool)
	for _, id := range selected {
		if seen[id] {
			t.Fatalf("duplicate peer selected: %s", id)
		}
		seen[id] = true
	}
}

func TestPeerRep_SelectPeersWeightedExcludesBanned(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	pr.RepAdjustScore("a", RepCatProtocol, 10.0)
	pr.RepBanPermanent("b", BanReasonDoS)

	selected := pr.RepSelectWeighted(10)
	for _, id := range selected {
		if id == "b" {
			t.Fatal("banned peer should not be selected")
		}
	}
}

func TestPeerRep_SelectPeersWeightedEmpty(t *testing.T) {
	cfg := DefaultRepConfig()
	pr := NewPeerRep(cfg)

	selected := pr.RepSelectWeighted(5)
	if selected != nil {
		t.Fatalf("expected nil for empty peer set, got %v", selected)
	}
}

func TestPeerRep_DefaultRepConfig(t *testing.T) {
	cfg := DefaultRepConfig()
	if cfg.InitialScore != 100.0 {
		t.Fatalf("expected initial 100, got %f", cfg.InitialScore)
	}
	if cfg.DecayAlpha != 0.95 {
		t.Fatalf("expected alpha 0.95, got %f", cfg.DecayAlpha)
	}
	if cfg.DefaultBanDuration != time.Hour {
		t.Fatalf("expected 1h ban duration, got %v", cfg.DefaultBanDuration)
	}
}

func TestPeerRep_InvalidConfig(t *testing.T) {
	cfg := RepConfig{
		MaxCategoryScore: -10,
		MinCategoryScore: 10,
		DecayAlpha:       2.0,
	}
	pr := NewPeerRep(cfg)

	// Should have been corrected.
	if pr.config.MaxCategoryScore != 200.0 {
		t.Fatalf("expected corrected max 200, got %f", pr.config.MaxCategoryScore)
	}
	if pr.config.DecayAlpha != 0.95 {
		t.Fatalf("expected corrected alpha 0.95, got %f", pr.config.DecayAlpha)
	}
}

func TestBanReasonString(t *testing.T) {
	tests := []struct {
		reason BanReason
		want   string
	}{
		{BanReasonNone, "none"},
		{BanReasonProtocolViolation, "protocol_violation"},
		{BanReasonSpam, "spam"},
		{BanReasonDoS, "dos"},
		{BanReasonInvalidBlocks, "invalid_blocks"},
	}
	for _, tc := range tests {
		if got := tc.reason.String(); got != tc.want {
			t.Errorf("BanReason(%d).String() = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

func TestRepCategoryString(t *testing.T) {
	tests := []struct {
		cat  RepCategory
		want string
	}{
		{RepCatProtocol, "protocol"},
		{RepCatLatency, "latency"},
		{RepCatBandwidth, "bandwidth"},
		{RepCatAvailability, "availability"},
		{RepCategory(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.cat.String(); got != tc.want {
			t.Errorf("RepCategory(%d).String() = %q, want %q", tc.cat, got, tc.want)
		}
	}
}
