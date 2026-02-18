package p2p

import (
	"testing"
)

func TestPeerScore_Default(t *testing.T) {
	ps := NewPeerScore()
	if ps.Value() != DefaultScore {
		t.Errorf("initial Value = %f, want %f", ps.Value(), DefaultScore)
	}
	if ps.ShouldDisconnect() {
		t.Error("new peer should not be disconnected")
	}
}

func TestPeerScore_GoodResponse(t *testing.T) {
	ps := NewPeerScore()
	ps.GoodResponse()
	ps.GoodResponse()
	ps.GoodResponse()

	if ps.Value() != 3*scoreGoodResponse {
		t.Errorf("Value after 3 good responses = %f, want %f", ps.Value(), 3*scoreGoodResponse)
	}

	stats := ps.Stats()
	if stats.GoodResponses != 3 {
		t.Errorf("GoodResponses = %d, want 3", stats.GoodResponses)
	}
}

func TestPeerScore_BadResponse(t *testing.T) {
	ps := NewPeerScore()
	ps.BadResponse()

	if ps.Value() != scoreBadResponse {
		t.Errorf("Value after bad response = %f, want %f", ps.Value(), scoreBadResponse)
	}

	stats := ps.Stats()
	if stats.BadResponses != 1 {
		t.Errorf("BadResponses = %d, want 1", stats.BadResponses)
	}
}

func TestPeerScore_Timeout(t *testing.T) {
	ps := NewPeerScore()
	ps.Timeout()

	if ps.Value() != scoreTimeout {
		t.Errorf("Value after timeout = %f, want %f", ps.Value(), scoreTimeout)
	}

	stats := ps.Stats()
	if stats.Timeouts != 1 {
		t.Errorf("Timeouts = %d, want 1", stats.Timeouts)
	}
}

func TestPeerScore_UsefulBlock(t *testing.T) {
	ps := NewPeerScore()
	ps.UsefulBlock()

	if ps.Value() != scoreUsefulBlock {
		t.Errorf("Value = %f, want %f", ps.Value(), scoreUsefulBlock)
	}

	stats := ps.Stats()
	if stats.UsefulBlocks != 1 {
		t.Errorf("UsefulBlocks = %d, want 1", stats.UsefulBlocks)
	}
}

func TestPeerScore_UselessBlock(t *testing.T) {
	ps := NewPeerScore()
	ps.UselessBlock()

	if ps.Value() != scoreUselessBlock {
		t.Errorf("Value = %f, want %f", ps.Value(), scoreUselessBlock)
	}
}

func TestPeerScore_Handshake(t *testing.T) {
	ps := NewPeerScore()
	ps.HandshakeOK()
	if ps.Value() != scoreHandshakeOK {
		t.Errorf("Value after HandshakeOK = %f, want %f", ps.Value(), scoreHandshakeOK)
	}

	ps2 := NewPeerScore()
	ps2.HandshakeFail()
	if ps2.Value() != scoreHandshakeFail {
		t.Errorf("Value after HandshakeFail = %f, want %f", ps2.Value(), scoreHandshakeFail)
	}
}

func TestPeerScore_ShouldDisconnect(t *testing.T) {
	ps := NewPeerScore()

	// Drive score below disconnect threshold with timeouts.
	for i := 0; i < 6; i++ {
		ps.Timeout() // -10 each
	}

	// Score should be -60, below -50 threshold.
	if !ps.ShouldDisconnect() {
		t.Errorf("should disconnect at score %f", ps.Value())
	}
}

func TestPeerScore_ClampMax(t *testing.T) {
	ps := NewPeerScore()

	// Add many good responses to try exceeding MaxScore.
	for i := 0; i < 200; i++ {
		ps.GoodResponse()
	}

	if ps.Value() > MaxScore {
		t.Errorf("Value %f exceeds MaxScore %f", ps.Value(), MaxScore)
	}
	if ps.Value() != MaxScore {
		t.Errorf("Value = %f, want MaxScore %f", ps.Value(), MaxScore)
	}
}

func TestPeerScore_ClampMin(t *testing.T) {
	ps := NewPeerScore()

	// Add many timeouts to try going below MinScore.
	for i := 0; i < 20; i++ {
		ps.Timeout()
	}

	if ps.Value() < MinScore {
		t.Errorf("Value %f below MinScore %f", ps.Value(), MinScore)
	}
	if ps.Value() != MinScore {
		t.Errorf("Value = %f, want MinScore %f", ps.Value(), MinScore)
	}
}

func TestPeerScore_Stats(t *testing.T) {
	ps := NewPeerScore()
	ps.GoodResponse()
	ps.GoodResponse()
	ps.BadResponse()
	ps.Timeout()
	ps.UsefulBlock()

	stats := ps.Stats()

	if stats.GoodResponses != 2 {
		t.Errorf("GoodResponses = %d, want 2", stats.GoodResponses)
	}
	if stats.BadResponses != 1 {
		t.Errorf("BadResponses = %d, want 1", stats.BadResponses)
	}
	if stats.Timeouts != 1 {
		t.Errorf("Timeouts = %d, want 1", stats.Timeouts)
	}
	if stats.UsefulBlocks != 1 {
		t.Errorf("UsefulBlocks = %d, want 1", stats.UsefulBlocks)
	}
	if stats.LastUpdated.IsZero() {
		t.Error("LastUpdated is zero")
	}

	// Value should be: 2*1 + (-5) + (-10) + 2 = -11
	expected := 2*scoreGoodResponse + scoreBadResponse + scoreTimeout + scoreUsefulBlock
	if stats.Value != expected {
		t.Errorf("Value = %f, want %f", stats.Value, expected)
	}
}

func TestScoreMap(t *testing.T) {
	sm := NewScoreMap()

	// Get creates a new score.
	s := sm.Get("peer1")
	if s == nil {
		t.Fatal("Get returned nil")
	}
	if s.Value() != DefaultScore {
		t.Errorf("initial Value = %f, want %f", s.Value(), DefaultScore)
	}

	// Same score on second call.
	s2 := sm.Get("peer1")
	if s2 != s {
		t.Error("Get returned different pointer for same ID")
	}

	// Modify and check.
	s.GoodResponse()
	sm.Get("peer2").BadResponse()

	all := sm.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d entries, want 2", len(all))
	}
	if all["peer1"] != scoreGoodResponse {
		t.Errorf("peer1 score = %f, want %f", all["peer1"], scoreGoodResponse)
	}

	// Remove.
	sm.Remove("peer1")
	all = sm.All()
	if len(all) != 1 {
		t.Errorf("All() after remove = %d, want 1", len(all))
	}
}
