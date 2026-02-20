package p2p

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func defaultTestConfig() PeerManagerConfig {
	return PeerManagerConfig{
		MaxInbound:    5,
		MaxOutbound:   5,
		MinPeers:      2,
		PruneInterval: 60,
		BanDuration:   300,
	}
}

func TestAdvPeerManager_AddAndGet(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	info := AdvPeerInfo{
		ID:          "peer1",
		RemoteAddr:  "10.0.0.1:30303",
		Protocols:   []string{"eth/68", "snap/1"},
		Inbound:     false,
		ConnectedAt: time.Now().Unix(),
		Reputation:  100,
	}

	if err := m.AddPeer(info); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	got := m.GetPeer("peer1")
	if got == nil {
		t.Fatal("GetPeer returned nil")
	}
	if got.ID != "peer1" {
		t.Errorf("ID = %q, want %q", got.ID, "peer1")
	}
	if got.RemoteAddr != "10.0.0.1:30303" {
		t.Errorf("RemoteAddr = %q, want %q", got.RemoteAddr, "10.0.0.1:30303")
	}
	if len(got.Protocols) != 2 || got.Protocols[0] != "eth/68" {
		t.Errorf("Protocols = %v, want [eth/68 snap/1]", got.Protocols)
	}
	if got.Reputation != 100 {
		t.Errorf("Reputation = %d, want 100", got.Reputation)
	}
}

func TestAdvPeerManager_GetUnknown(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())
	if got := m.GetPeer("unknown"); got != nil {
		t.Errorf("GetPeer(unknown) = %v, want nil", got)
	}
}

func TestAdvPeerManager_DuplicateAdd(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	info := AdvPeerInfo{ID: "peer1", Protocols: []string{"eth/68"}}
	if err := m.AddPeer(info); err != nil {
		t.Fatalf("first AddPeer: %v", err)
	}
	if err := m.AddPeer(info); err != ErrPeerExists {
		t.Errorf("duplicate AddPeer: got %v, want ErrPeerExists", err)
	}
}

func TestAdvPeerManager_InboundLimit(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.MaxInbound = 2
	m := NewAdvPeerManager(cfg)

	for i := 0; i < 2; i++ {
		err := m.AddPeer(AdvPeerInfo{
			ID:      fmt.Sprintf("in%d", i),
			Inbound: true,
		})
		if err != nil {
			t.Fatalf("AddPeer(in%d): %v", i, err)
		}
	}

	// Third inbound should fail.
	err := m.AddPeer(AdvPeerInfo{ID: "in2", Inbound: true})
	if err != ErrTooManyInbound {
		t.Errorf("excess inbound: got %v, want ErrTooManyInbound", err)
	}

	// Outbound should still work.
	err = m.AddPeer(AdvPeerInfo{ID: "out0", Inbound: false})
	if err != nil {
		t.Errorf("outbound after inbound full: %v", err)
	}
}

func TestAdvPeerManager_OutboundLimit(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.MaxOutbound = 2
	m := NewAdvPeerManager(cfg)

	for i := 0; i < 2; i++ {
		err := m.AddPeer(AdvPeerInfo{
			ID:      fmt.Sprintf("out%d", i),
			Inbound: false,
		})
		if err != nil {
			t.Fatalf("AddPeer(out%d): %v", i, err)
		}
	}

	err := m.AddPeer(AdvPeerInfo{ID: "out2", Inbound: false})
	if err != ErrTooManyOutbound {
		t.Errorf("excess outbound: got %v, want ErrTooManyOutbound", err)
	}
}

func TestAdvPeerManager_RemovePeer(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{ID: "peer1"})
	if m.PeerCount() != 1 {
		t.Fatalf("PeerCount = %d, want 1", m.PeerCount())
	}

	m.RemovePeer("peer1")
	if m.PeerCount() != 0 {
		t.Errorf("PeerCount after remove = %d, want 0", m.PeerCount())
	}

	// Removing unknown peer should not panic.
	m.RemovePeer("unknown")
}

func TestAdvPeerManager_BanAndUnban(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.BanDuration = 3600 // 1 hour
	m := NewAdvPeerManager(cfg)

	m.AddPeer(AdvPeerInfo{ID: "peer1"})
	if m.IsBanned("peer1") {
		t.Error("peer1 should not be banned initially")
	}

	m.BanPeer("peer1", "bad behavior")

	if !m.IsBanned("peer1") {
		t.Error("peer1 should be banned after BanPeer")
	}
	// Banned peer should be removed from active peers.
	if m.GetPeer("peer1") != nil {
		t.Error("banned peer should be removed from active set")
	}
	if m.PeerCount() != 0 {
		t.Errorf("PeerCount after ban = %d, want 0", m.PeerCount())
	}

	// Adding a banned peer should fail.
	err := m.AddPeer(AdvPeerInfo{ID: "peer1"})
	if err != ErrPeerBanned {
		t.Errorf("adding banned peer: got %v, want ErrPeerBanned", err)
	}
}

func TestAdvPeerManager_BanExpiry(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.BanDuration = 0 // Immediate expiry.
	m := NewAdvPeerManager(cfg)

	m.BanPeer("peer1", "test")
	// With 0 duration, the ban should already be expired.
	time.Sleep(time.Millisecond)

	if m.IsBanned("peer1") {
		t.Error("ban should have expired with BanDuration=0")
	}

	// Should be able to add the peer again.
	if err := m.AddPeer(AdvPeerInfo{ID: "peer1"}); err != nil {
		t.Errorf("AddPeer after ban expiry: %v", err)
	}
}

func TestAdvPeerManager_UpdateReputation(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{ID: "peer1", Reputation: 50})
	m.UpdateReputation("peer1", 10)

	got := m.GetPeer("peer1")
	if got.Reputation != 60 {
		t.Errorf("Reputation = %d, want 60", got.Reputation)
	}

	m.UpdateReputation("peer1", -30)
	got = m.GetPeer("peer1")
	if got.Reputation != 30 {
		t.Errorf("Reputation = %d, want 30", got.Reputation)
	}

	// No-op for unknown peer.
	m.UpdateReputation("unknown", 100)
}

func TestAdvPeerManager_BestPeers(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{ID: "low", Reputation: 10})
	m.AddPeer(AdvPeerInfo{ID: "mid", Reputation: 50})
	m.AddPeer(AdvPeerInfo{ID: "high", Reputation: 100})

	best := m.BestPeers(2)
	if len(best) != 2 {
		t.Fatalf("BestPeers(2) returned %d, want 2", len(best))
	}
	if best[0].ID != "high" {
		t.Errorf("BestPeers[0].ID = %q, want %q", best[0].ID, "high")
	}
	if best[1].ID != "mid" {
		t.Errorf("BestPeers[1].ID = %q, want %q", best[1].ID, "mid")
	}

	// Request more than available.
	all := m.BestPeers(100)
	if len(all) != 3 {
		t.Errorf("BestPeers(100) returned %d, want 3", len(all))
	}

	// Empty manager.
	empty := NewAdvPeerManager(defaultTestConfig())
	if got := empty.BestPeers(5); len(got) != 0 {
		t.Errorf("BestPeers on empty = %d, want 0", len(got))
	}
}

func TestAdvPeerManager_PeersByProtocol(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{ID: "a", Protocols: []string{"eth/68", "snap/1"}})
	m.AddPeer(AdvPeerInfo{ID: "b", Protocols: []string{"eth/68"}})
	m.AddPeer(AdvPeerInfo{ID: "c", Protocols: []string{"snap/1"}})

	eth := m.PeersByProtocol("eth/68")
	if len(eth) != 2 {
		t.Errorf("PeersByProtocol(eth/68) = %d, want 2", len(eth))
	}

	snap := m.PeersByProtocol("snap/1")
	if len(snap) != 2 {
		t.Errorf("PeersByProtocol(snap/1) = %d, want 2", len(snap))
	}

	none := m.PeersByProtocol("les/4")
	if len(none) != 0 {
		t.Errorf("PeersByProtocol(les/4) = %d, want 0", len(none))
	}
}

func TestAdvPeerManager_InboundOutboundCount(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{ID: "in1", Inbound: true})
	m.AddPeer(AdvPeerInfo{ID: "in2", Inbound: true})
	m.AddPeer(AdvPeerInfo{ID: "out1", Inbound: false})

	if m.InboundCount() != 2 {
		t.Errorf("InboundCount = %d, want 2", m.InboundCount())
	}
	if m.OutboundCount() != 1 {
		t.Errorf("OutboundCount = %d, want 1", m.OutboundCount())
	}
	if m.PeerCount() != 3 {
		t.Errorf("PeerCount = %d, want 3", m.PeerCount())
	}
}

func TestAdvPeerManager_RecordBytes(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{ID: "peer1"})

	m.RecordBytes("peer1", 1024, 512)
	m.RecordBytes("peer1", 2048, 256)

	got := m.GetPeer("peer1")
	if got.BytesIn != 3072 {
		t.Errorf("BytesIn = %d, want 3072", got.BytesIn)
	}
	if got.BytesOut != 768 {
		t.Errorf("BytesOut = %d, want 768", got.BytesOut)
	}

	// No-op for unknown peer.
	m.RecordBytes("unknown", 100, 200)
}

func TestAdvPeerManager_GetPeerReturnsDefensiveCopy(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())

	m.AddPeer(AdvPeerInfo{
		ID:        "peer1",
		Protocols: []string{"eth/68"},
	})

	got := m.GetPeer("peer1")
	// Mutating the copy should not affect the original.
	got.Reputation = 9999
	got.Protocols[0] = "MODIFIED"

	original := m.GetPeer("peer1")
	if original.Reputation == 9999 {
		t.Error("GetPeer should return a defensive copy (Reputation leaked)")
	}
	if original.Protocols[0] == "MODIFIED" {
		t.Error("GetPeer should return a defensive copy (Protocols leaked)")
	}
}

func TestAdvPeerManager_Concurrency(t *testing.T) {
	cfg := PeerManagerConfig{
		MaxInbound:  100,
		MaxOutbound: 100,
		MinPeers:    1,
		BanDuration: 3600,
	}
	m := NewAdvPeerManager(cfg)
	const n = 50

	var wg sync.WaitGroup

	// Concurrent adds.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			m.AddPeer(AdvPeerInfo{
				ID:         fmt.Sprintf("peer%d", i),
				Inbound:    i%2 == 0,
				Protocols:  []string{"eth/68"},
				Reputation: i,
			})
		}(i)
	}
	wg.Wait()

	if m.PeerCount() != n {
		t.Errorf("PeerCount = %d, want %d", m.PeerCount(), n)
	}

	// Concurrent reads and mutations.
	wg.Add(n * 4)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			m.GetPeer(fmt.Sprintf("peer%d", i))
		}(i)
		go func(i int) {
			defer wg.Done()
			m.UpdateReputation(fmt.Sprintf("peer%d", i), 1)
		}(i)
		go func() {
			defer wg.Done()
			m.BestPeers(5)
		}()
		go func() {
			defer wg.Done()
			m.PeersByProtocol("eth/68")
		}()
	}
	wg.Wait()

	// Concurrent bans and removes.
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("peer%d", i)
			if i%3 == 0 {
				m.BanPeer(id, "test")
			} else {
				m.RemovePeer(id)
			}
		}(i)
	}
	wg.Wait()

	if m.PeerCount() != 0 {
		t.Errorf("PeerCount after cleanup = %d, want 0", m.PeerCount())
	}
}

func TestAdvPeerManager_BanUnknownPeer(t *testing.T) {
	m := NewAdvPeerManager(defaultTestConfig())
	// Banning an unknown peer should not panic and should mark as banned.
	m.BanPeer("ghost", "never connected")
	if !m.IsBanned("ghost") {
		t.Error("ghost should be banned")
	}
}

func TestAdvPeerManager_RemoveFreesSlot(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.MaxInbound = 1
	m := NewAdvPeerManager(cfg)

	m.AddPeer(AdvPeerInfo{ID: "in1", Inbound: true})

	// Slot is full.
	err := m.AddPeer(AdvPeerInfo{ID: "in2", Inbound: true})
	if err != ErrTooManyInbound {
		t.Fatalf("expected ErrTooManyInbound, got %v", err)
	}

	// Remove frees the slot.
	m.RemovePeer("in1")
	if err := m.AddPeer(AdvPeerInfo{ID: "in2", Inbound: true}); err != nil {
		t.Errorf("AddPeer after remove: %v", err)
	}
}
