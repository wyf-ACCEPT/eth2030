package p2p

import (
	"testing"
	"time"
)

func TestGossipMeshScore_RecordMeshDelivery(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	// Graft peer first, then record delivery.
	mgr.GraftPeer("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")

	score := mgr.TopicScore("peer1", "blocks")
	if score <= 0 {
		t.Fatalf("expected positive score for mesh deliveries, got %f", score)
	}
}

func TestGossipMeshScore_FirstMessageReward(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	mgr.GraftPeer("peer1", "blocks")
	mgr.RecordFirstMessage("peer1", "blocks")

	score := mgr.TopicScore("peer1", "blocks")
	params := DefaultMeshScoreParams()
	if score < params.FirstMessageWeight {
		t.Fatalf("expected score >= %f for first message, got %f", params.FirstMessageWeight, score)
	}
}

func TestGossipMeshScore_InvalidMessagePenalty(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	mgr.GraftPeer("peer1", "blocks")
	mgr.RecordInvalidMessage("peer1", "blocks")

	score := mgr.TopicScore("peer1", "blocks")
	if score >= 0 {
		t.Fatalf("expected negative score for invalid message, got %f", score)
	}
}

func TestGossipMeshScore_GraftAndPrune(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	ok := mgr.GraftPeer("peer1", "blocks")
	if !ok {
		t.Fatal("expected graft to succeed")
	}

	peers := mgr.MeshPeers("blocks")
	if len(peers) != 1 || peers[0] != "peer1" {
		t.Fatalf("expected [peer1] in mesh, got %v", peers)
	}

	mgr.PrunePeer("peer1", "blocks")
	peers = mgr.MeshPeers("blocks")
	if len(peers) != 0 {
		t.Fatalf("expected empty mesh after prune, got %v", peers)
	}
}

func TestGossipMeshScore_BannedPeerCannotGraft(t *testing.T) {
	ban := DefaultMeshBanConfig()
	ban.BanThreshold = -5.0
	ban.BanCooldown = time.Hour

	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), ban)

	// Graft and then generate enough invalid messages to trigger a ban.
	mgr.GraftPeer("peer1", "blocks")
	for i := 0; i < 5; i++ {
		mgr.RecordInvalidMessage("peer1", "blocks")
	}
	banned := mgr.CheckBans()
	if len(banned) == 0 {
		t.Fatal("expected peer1 to be banned")
	}

	// Attempting to graft a banned peer should fail.
	ok := mgr.GraftPeer("peer1", "blocks")
	if ok {
		t.Fatal("expected graft to fail for banned peer")
	}
}

func TestGossipMeshScore_BanCooldown(t *testing.T) {
	ban := DefaultMeshBanConfig()
	ban.BanThreshold = -5.0
	ban.BanCooldown = 100 * time.Millisecond

	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), ban)

	mgr.GraftPeer("peer1", "blocks")
	for i := 0; i < 5; i++ {
		mgr.RecordInvalidMessage("peer1", "blocks")
	}
	mgr.CheckBans()

	if !mgr.IsBanned("peer1") {
		t.Fatal("expected peer1 to be banned")
	}

	remaining := mgr.BanCooldownRemaining("peer1")
	if remaining <= 0 {
		t.Fatal("expected positive cooldown remaining")
	}

	// Wait for cooldown to expire.
	time.Sleep(150 * time.Millisecond)

	if mgr.IsBanned("peer1") {
		t.Fatal("expected ban to have expired")
	}

	// Should be able to graft again after cooldown.
	ok := mgr.GraftPeer("peer1", "blocks")
	if !ok {
		t.Fatal("expected graft to succeed after cooldown")
	}
}

func TestGossipMeshScore_ExponentialDecay(t *testing.T) {
	decay := DefaultMeshDecayConfig()
	decay.DecayInterval = 10 * time.Millisecond
	decay.DecayFactor = 0.5

	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), decay, DefaultMeshBanConfig())

	mgr.GraftPeer("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")

	scoreBefore := mgr.TopicScore("peer1", "blocks")

	// Wait for at least one decay interval.
	time.Sleep(20 * time.Millisecond)
	mgr.ApplyDecay()

	scoreAfter := mgr.TopicScore("peer1", "blocks")

	if scoreAfter >= scoreBefore {
		t.Fatalf("expected score to decrease after decay: before=%f, after=%f", scoreBefore, scoreAfter)
	}
}

func TestGossipMeshScore_PeerScore_AcrossTopics(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	mgr.GraftPeer("peer1", "blocks")
	mgr.GraftPeer("peer1", "attestations")
	mgr.RecordFirstMessage("peer1", "blocks")
	mgr.RecordFirstMessage("peer1", "attestations")

	total := mgr.PeerScore("peer1")
	singleTopic := mgr.TopicScore("peer1", "blocks")

	if total <= singleTopic {
		t.Fatalf("expected total score > single topic score: total=%f, single=%f", total, singleTopic)
	}
}

func TestGossipMeshScore_PruneByScore(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	// Graft 5 peers.
	for i := 0; i < 5; i++ {
		peerID := "peer" + string(rune('A'+i))
		mgr.GraftPeer(peerID, "blocks")
	}

	// Give peer A some invalid messages (low score).
	mgr.RecordInvalidMessage("peerA", "blocks")
	mgr.RecordInvalidMessage("peerA", "blocks")

	// Prune to target size 3.
	pruned := mgr.PruneByScore("blocks", 3)
	if len(pruned) != 2 {
		t.Fatalf("expected 2 pruned peers, got %d: %v", len(pruned), pruned)
	}

	remaining := mgr.MeshPeers("blocks")
	if len(remaining) != 3 {
		t.Fatalf("expected 3 remaining mesh peers, got %d", len(remaining))
	}
}

func TestGossipMeshScore_Graylist(t *testing.T) {
	ban := DefaultMeshBanConfig()
	ban.GraylistThreshold = -5.0
	ban.BanThreshold = -50.0

	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), ban)

	mgr.GraftPeer("peer1", "blocks")
	// Generate enough invalid messages to cross graylist but not ban threshold.
	mgr.RecordInvalidMessage("peer1", "blocks")

	mgr.CheckBans()

	if mgr.IsBanned("peer1") {
		t.Fatal("expected peer1 not to be banned (only graylisted)")
	}
	if !mgr.IsGraylisted("peer1") {
		t.Fatal("expected peer1 to be graylisted")
	}
}

func TestGossipMeshScore_RemovePeer(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	mgr.GraftPeer("peer1", "blocks")
	mgr.RecordMeshDelivery("peer1", "blocks")

	if mgr.PeerCount() != 1 {
		t.Fatalf("expected 1 peer, got %d", mgr.PeerCount())
	}

	mgr.RemovePeer("peer1")
	if mgr.PeerCount() != 0 {
		t.Fatalf("expected 0 peers after removal, got %d", mgr.PeerCount())
	}

	peers := mgr.MeshPeers("blocks")
	if len(peers) != 0 {
		t.Fatalf("expected empty mesh after peer removal, got %v", peers)
	}
}

func TestGossipMeshScore_MeshTimeContribution(t *testing.T) {
	params := DefaultMeshScoreParams()
	params.MeshTimeActivation = 10 * time.Millisecond
	params.MeshTimeWeight = 1.0

	mgr := NewGossipMeshScoreManager(params, DefaultMeshDecayConfig(), DefaultMeshBanConfig())
	mgr.GraftPeer("peer1", "blocks")

	// Score immediately after graft (before activation).
	scoreBefore := mgr.TopicScore("peer1", "blocks")

	// Wait for mesh time activation.
	time.Sleep(30 * time.Millisecond)

	scoreAfter := mgr.TopicScore("peer1", "blocks")
	if scoreAfter <= scoreBefore {
		t.Fatalf("expected score to increase with mesh time: before=%f, after=%f", scoreBefore, scoreAfter)
	}
}

func TestGossipMeshScore_UnknownPeerScore(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	score := mgr.PeerScore("nonexistent")
	if score != 0 {
		t.Fatalf("expected 0 score for unknown peer, got %f", score)
	}

	topicScore := mgr.TopicScore("nonexistent", "blocks")
	if topicScore != 0 {
		t.Fatalf("expected 0 topic score for unknown peer, got %f", topicScore)
	}
}

func TestGossipMeshScore_DeliveryOutsideMeshIgnored(t *testing.T) {
	mgr := NewGossipMeshScoreManager(DefaultMeshScoreParams(), DefaultMeshDecayConfig(), DefaultMeshBanConfig())

	// Record delivery without grafting (not in mesh).
	mgr.RecordMeshDelivery("peer1", "blocks")

	// Mesh delivery should not count since peer is not in mesh.
	mgr.mu.RLock()
	ps := mgr.peers["peer1"]
	ts := ps.topics["blocks"]
	deliveries := ts.MeshDeliveries
	mgr.mu.RUnlock()

	if deliveries != 0 {
		t.Fatalf("expected 0 mesh deliveries for non-mesh peer, got %d", deliveries)
	}
}
