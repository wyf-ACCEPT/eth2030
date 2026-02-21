package eth

import (
	"sync"
	"testing"
)

// TestProtocolMetricsNewMetrics verifies constructor behavior.
func TestProtocolMetricsNewMetrics(t *testing.T) {
	pm := NewProtocolMetrics()
	if pm == nil {
		t.Fatal("NewProtocolMetrics returned nil")
	}
	gm := pm.GlobalMetrics()
	if gm.TotalMessages != 0 {
		t.Fatalf("expected 0 messages, got %d", gm.TotalMessages)
	}
	if gm.ActivePeers != 0 {
		t.Fatalf("expected 0 active peers, got %d", gm.ActivePeers)
	}
	if gm.StartedAt == 0 {
		t.Fatal("StartedAt should be set")
	}
}

// TestProtocolMetricsRecordMessage verifies basic message recording.
func TestProtocolMetricsRecordMessage(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgBlockHeaders, 1024, 50)

	gm := pm.GlobalMetrics()
	if gm.TotalMessages != 1 {
		t.Fatalf("expected 1 message, got %d", gm.TotalMessages)
	}
	if gm.TotalBytes != 1024 {
		t.Fatalf("expected 1024 bytes, got %d", gm.TotalBytes)
	}
}

// TestProtocolMetricsPeerMetrics verifies per-peer metric retrieval.
func TestProtocolMetricsPeerMetrics(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgBlockHeaders, 500, 10)
	pm.RecordMessage("peer1", MsgBlockBodies, 1500, 30)

	metrics := pm.PeerMetrics("peer1")
	if metrics == nil {
		t.Fatal("expected non-nil peer metrics")
	}
	if metrics.PeerID != "peer1" {
		t.Fatalf("expected peer1, got %s", metrics.PeerID)
	}
	if metrics.TotalMessages != 2 {
		t.Fatalf("expected 2 messages, got %d", metrics.TotalMessages)
	}
	if metrics.TotalBytes != 2000 {
		t.Fatalf("expected 2000 bytes, got %d", metrics.TotalBytes)
	}
	// Average latency: (10+30)/2 = 20ms
	if metrics.AvgLatencyMs != 20.0 {
		t.Fatalf("expected avg latency 20.0, got %f", metrics.AvgLatencyMs)
	}
	if metrics.MessagesByType[MsgBlockHeaders] != 1 {
		t.Fatal("expected 1 BlockHeaders message")
	}
	if metrics.MessagesByType[MsgBlockBodies] != 1 {
		t.Fatal("expected 1 BlockBodies message")
	}
}

// TestProtocolMetricsMessageTypeMetrics verifies per-message-type stats.
func TestProtocolMetricsMessageTypeMetrics(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgBlockHeaders, 400, 10)
	pm.RecordMessage("peer2", MsgBlockHeaders, 600, 20)
	pm.RecordMessage("peer1", MsgBlockBodies, 1000, 30)

	stats := pm.MessageTypeMetrics(MsgBlockHeaders)
	if stats == nil {
		t.Fatal("expected non-nil message type stats")
	}
	if stats.Count != 2 {
		t.Fatalf("expected 2 BlockHeaders, got %d", stats.Count)
	}
	if stats.TotalBytes != 1000 {
		t.Fatalf("expected 1000 bytes, got %d", stats.TotalBytes)
	}
	// AvgSize: 1000/2 = 500
	if stats.AvgSize != 500.0 {
		t.Fatalf("expected avg size 500.0, got %f", stats.AvgSize)
	}
	// AvgLatency: (10+20)/2 = 15
	if stats.AvgLatencyMs != 15.0 {
		t.Fatalf("expected avg latency 15.0, got %f", stats.AvgLatencyMs)
	}
}

// TestProtocolMetricsActivePeers verifies active peer listing.
func TestProtocolMetricsActivePeers(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer_b", MsgStatus, 100, 5)
	pm.RecordMessage("peer_a", MsgStatus, 100, 5)
	pm.RecordMessage("peer_c", MsgStatus, 100, 5)

	peers := pm.ActivePeers()
	if len(peers) != 3 {
		t.Fatalf("expected 3 active peers, got %d", len(peers))
	}
	// Should be sorted.
	if peers[0] != "peer_a" || peers[1] != "peer_b" || peers[2] != "peer_c" {
		t.Fatalf("unexpected peer order: %v", peers)
	}
}

// TestProtocolMetricsTopPeers verifies top-N peer selection by volume.
func TestProtocolMetricsTopPeers(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("low", MsgStatus, 100, 5)
	pm.RecordMessage("high", MsgBlockBodies, 5000, 10)
	pm.RecordMessage("mid", MsgBlockHeaders, 2000, 8)

	top := pm.TopPeersByVolume(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 top peers, got %d", len(top))
	}
	if top[0].PeerID != "high" {
		t.Fatalf("expected highest volume peer first, got %s", top[0].PeerID)
	}
	if top[1].PeerID != "mid" {
		t.Fatalf("expected second highest peer second, got %s", top[1].PeerID)
	}
}

// TestProtocolMetricsTopPeersMoreThanAvailable verifies requesting more peers
// than exist returns all of them.
func TestProtocolMetricsTopPeersMoreThanAvailable(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgStatus, 100, 5)

	top := pm.TopPeersByVolume(10)
	if len(top) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(top))
	}
}

// TestProtocolMetricsGlobalMetrics verifies aggregate statistics.
func TestProtocolMetricsGlobalMetrics(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("p1", MsgStatus, 200, 10)
	pm.RecordMessage("p2", MsgBlockHeaders, 800, 30)

	gm := pm.GlobalMetrics()
	if gm.TotalMessages != 2 {
		t.Fatalf("expected 2 total messages, got %d", gm.TotalMessages)
	}
	if gm.TotalBytes != 1000 {
		t.Fatalf("expected 1000 total bytes, got %d", gm.TotalBytes)
	}
	if gm.ActivePeers != 2 {
		t.Fatalf("expected 2 active peers, got %d", gm.ActivePeers)
	}
	// AvgLatency: (10+30)/2 = 20
	if gm.AvgLatencyMs != 20.0 {
		t.Fatalf("expected avg latency 20.0, got %f", gm.AvgLatencyMs)
	}
}

// TestProtocolMetricsPrunePeer verifies peer data removal.
func TestProtocolMetricsPrunePeer(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgStatus, 100, 5)
	pm.RecordMessage("peer2", MsgStatus, 200, 10)

	pm.PrunePeer("peer1")

	if pm.PeerMetrics("peer1") != nil {
		t.Fatal("pruned peer should return nil metrics")
	}
	if pm.PeerMetrics("peer2") == nil {
		t.Fatal("peer2 should still exist")
	}

	peers := pm.ActivePeers()
	if len(peers) != 1 || peers[0] != "peer2" {
		t.Fatalf("expected only peer2 active, got %v", peers)
	}
}

// TestProtocolMetricsReset verifies clearing all data.
func TestProtocolMetricsReset(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgStatus, 100, 5)
	pm.RecordMessage("peer2", MsgBlockHeaders, 500, 10)

	pm.Reset()

	gm := pm.GlobalMetrics()
	if gm.TotalMessages != 0 {
		t.Fatalf("expected 0 messages after reset, got %d", gm.TotalMessages)
	}
	if gm.TotalBytes != 0 {
		t.Fatalf("expected 0 bytes after reset, got %d", gm.TotalBytes)
	}
	if gm.ActivePeers != 0 {
		t.Fatalf("expected 0 peers after reset, got %d", gm.ActivePeers)
	}
	if len(pm.ActivePeers()) != 0 {
		t.Fatal("expected no active peers after reset")
	}
}

// TestProtocolMetricsConcurrentAccess verifies thread safety under
// concurrent reads and writes.
func TestProtocolMetricsConcurrentAccess(t *testing.T) {
	pm := NewProtocolMetrics()
	var wg sync.WaitGroup

	// Writers: record messages from many peers.
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			peerID := "peer" + string(rune('A'+i%26))
			pm.RecordMessage(peerID, uint64(i%5), 100+i, int64(i))
		}(i)
	}

	// Readers: read metrics concurrently.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pm.GlobalMetrics()
			pm.ActivePeers()
			peerID := "peer" + string(rune('A'+i%26))
			pm.PeerMetrics(peerID)
			pm.MessageTypeMetrics(uint64(i % 5))
		}(i)
	}

	wg.Wait()

	gm := pm.GlobalMetrics()
	if gm.TotalMessages != 100 {
		t.Fatalf("expected 100 total messages, got %d", gm.TotalMessages)
	}
}

// TestProtocolMetricsUnknownPeer verifies that requesting metrics for an
// unknown peer returns nil.
func TestProtocolMetricsUnknownPeer(t *testing.T) {
	pm := NewProtocolMetrics()
	if pm.PeerMetrics("nonexistent") != nil {
		t.Fatal("expected nil for unknown peer")
	}
}

// TestProtocolMetricsMultiplePeers verifies tracking multiple peers independently.
func TestProtocolMetricsMultiplePeers(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("alice", MsgBlockHeaders, 300, 10)
	pm.RecordMessage("bob", MsgBlockBodies, 700, 20)
	pm.RecordMessage("alice", MsgBlockHeaders, 500, 30)

	alice := pm.PeerMetrics("alice")
	if alice.TotalMessages != 2 {
		t.Fatalf("alice expected 2 messages, got %d", alice.TotalMessages)
	}
	if alice.TotalBytes != 800 {
		t.Fatalf("alice expected 800 bytes, got %d", alice.TotalBytes)
	}

	bob := pm.PeerMetrics("bob")
	if bob.TotalMessages != 1 {
		t.Fatalf("bob expected 1 message, got %d", bob.TotalMessages)
	}
}

// TestProtocolMetricsLargeVolume verifies metrics under high message counts.
func TestProtocolMetricsLargeVolume(t *testing.T) {
	pm := NewProtocolMetrics()
	for i := 0; i < 10000; i++ {
		pm.RecordMessage("bulk_peer", MsgTransactions, 256, 5)
	}

	metrics := pm.PeerMetrics("bulk_peer")
	if metrics.TotalMessages != 10000 {
		t.Fatalf("expected 10000 messages, got %d", metrics.TotalMessages)
	}
	if metrics.TotalBytes != 2560000 {
		t.Fatalf("expected 2560000 bytes, got %d", metrics.TotalBytes)
	}
	if metrics.AvgLatencyMs != 5.0 {
		t.Fatalf("expected avg latency 5.0, got %f", metrics.AvgLatencyMs)
	}
}

// TestProtocolMetricsLatencyTracking verifies latency averaging works correctly.
func TestProtocolMetricsLatencyTracking(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgStatus, 100, 0)
	pm.RecordMessage("peer1", MsgStatus, 100, 100)

	metrics := pm.PeerMetrics("peer1")
	// Average: (0+100)/2 = 50
	if metrics.AvgLatencyMs != 50.0 {
		t.Fatalf("expected avg latency 50.0, got %f", metrics.AvgLatencyMs)
	}
}

// TestProtocolMetricsMessageTypes verifies that different message types are
// tracked independently.
func TestProtocolMetricsMessageTypes(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("p1", MsgStatus, 100, 5)
	pm.RecordMessage("p1", MsgStatus, 200, 10)
	pm.RecordMessage("p1", MsgBlockHeaders, 500, 20)

	statusStats := pm.MessageTypeMetrics(MsgStatus)
	if statusStats.Count != 2 {
		t.Fatalf("expected 2 Status messages, got %d", statusStats.Count)
	}

	headerStats := pm.MessageTypeMetrics(MsgBlockHeaders)
	if headerStats.Count != 1 {
		t.Fatalf("expected 1 BlockHeaders message, got %d", headerStats.Count)
	}

	// Unknown type should return nil.
	unknown := pm.MessageTypeMetrics(0xff)
	if unknown != nil {
		t.Fatal("expected nil for unrecorded message type")
	}
}

// TestProtocolMetricsPrunePeerTwice verifies double pruning is safe.
func TestProtocolMetricsPrunePeerTwice(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgStatus, 100, 5)
	pm.PrunePeer("peer1")
	pm.PrunePeer("peer1") // should not panic

	if pm.PeerMetrics("peer1") != nil {
		t.Fatal("expected nil after double prune")
	}
}

// TestProtocolMetricsLastSeen verifies the LastSeen timestamp is set.
func TestProtocolMetricsLastSeen(t *testing.T) {
	pm := NewProtocolMetrics()
	pm.RecordMessage("peer1", MsgStatus, 100, 5)

	metrics := pm.PeerMetrics("peer1")
	if metrics.LastSeen == 0 {
		t.Fatal("LastSeen should be set")
	}
}

// TestProtocolMetricsGlobalLatencyZero verifies zero latency when no messages.
func TestProtocolMetricsGlobalLatencyZero(t *testing.T) {
	pm := NewProtocolMetrics()
	gm := pm.GlobalMetrics()
	if gm.AvgLatencyMs != 0 {
		t.Fatalf("expected 0 avg latency with no messages, got %f", gm.AvgLatencyMs)
	}
}
