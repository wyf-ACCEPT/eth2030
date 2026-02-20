package p2p

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProtocolManagerRegister(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})

	handler := func(peerID string, code uint64, payload []byte) error {
		return nil
	}

	// Register a protocol.
	if err := pm.RegisterProtocol("eth", 68, handler); err != nil {
		t.Fatalf("RegisterProtocol: %v", err)
	}

	// Duplicate registration should fail.
	if err := pm.RegisterProtocol("eth", 68, handler); err != ErrProtocolExists {
		t.Errorf("RegisterProtocol(dup) = %v, want ErrProtocolExists", err)
	}

	// Same name, different version should succeed.
	if err := pm.RegisterProtocol("eth", 70, handler); err != nil {
		t.Errorf("RegisterProtocol(v70): %v", err)
	}

	caps := pm.Protocols()
	if len(caps) != 2 {
		t.Fatalf("Protocols() len = %d, want 2", len(caps))
	}
}

func TestMatchCapabilities(t *testing.T) {
	local := []Capability{
		{Name: "eth", Version: 68},
		{Name: "eth", Version: 70},
		{Name: "snap", Version: 1},
	}
	remote := []Capability{
		{Name: "eth", Version: 67},
		{Name: "eth", Version: 68},
		{Name: "les", Version: 4},
	}

	matched := MatchCapabilities(local, remote)
	if len(matched) != 1 {
		t.Fatalf("MatchCapabilities returned %d caps, want 1", len(matched))
	}
	if matched[0].Name != "eth" || matched[0].Version != 68 {
		t.Errorf("matched[0] = %v, want eth/68", matched[0])
	}
}

func TestMatchCapabilitiesHighestVersion(t *testing.T) {
	local := []Capability{
		{Name: "eth", Version: 68},
		{Name: "eth", Version: 70},
	}
	remote := []Capability{
		{Name: "eth", Version: 70},
		{Name: "eth", Version: 71},
	}

	matched := MatchCapabilities(local, remote)
	if len(matched) != 1 {
		t.Fatalf("len = %d, want 1", len(matched))
	}
	// local max is 70, remote max is 71. min(70,71) = 70.
	if matched[0].Version != 70 {
		t.Errorf("version = %d, want 70", matched[0].Version)
	}
}

func TestMatchCapabilitiesNoOverlap(t *testing.T) {
	local := []Capability{{Name: "eth", Version: 68}}
	remote := []Capability{{Name: "snap", Version: 1}}

	matched := MatchCapabilities(local, remote)
	if len(matched) != 0 {
		t.Errorf("len = %d, want 0", len(matched))
	}
}

func TestConnectAndDisconnect(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{MaxTotal: 10})
	pm.ConnectFn = func(nodeID string) ([]Capability, error) {
		return []Capability{{Name: "eth", Version: 68}}, nil
	}

	pm.RegisterProtocol("eth", 68, func(string, uint64, []byte) error { return nil })

	// Track connect/disconnect events.
	var connectedID string
	var disconnectedID string
	var disconnectReason string

	pm.OnConnect(func(info *PeerMgrInfo) {
		connectedID = info.NodeID
	})
	pm.OnDisconnect(func(nodeID, reason string) {
		disconnectedID = nodeID
		disconnectReason = reason
	})

	// Connect.
	if err := pm.Connect("peer1"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if pm.PeerCount() != 1 {
		t.Errorf("PeerCount = %d, want 1", pm.PeerCount())
	}
	if connectedID != "peer1" {
		t.Errorf("connectedID = %q, want peer1", connectedID)
	}

	// Verify peer info.
	info := pm.PeerInfo("peer1")
	if info == nil {
		t.Fatal("PeerInfo returned nil")
	}
	if info.Inbound {
		t.Error("peer should be outbound")
	}
	if len(info.Capabilities) != 1 || info.Capabilities[0].Name != "eth" {
		t.Errorf("caps = %v, want [eth/68]", info.Capabilities)
	}

	// Disconnect.
	if err := pm.Disconnect("peer1", "test"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if pm.PeerCount() != 0 {
		t.Errorf("PeerCount = %d after disconnect, want 0", pm.PeerCount())
	}
	if disconnectedID != "peer1" {
		t.Errorf("disconnectedID = %q, want peer1", disconnectedID)
	}
	if disconnectReason != "test" {
		t.Errorf("disconnectReason = %q, want test", disconnectReason)
	}
}

func TestConnectDuplicate(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.ConnectFn = func(string) ([]Capability, error) {
		return nil, nil
	}

	if err := pm.Connect("peer1"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := pm.Connect("peer1"); err != ErrPeerAlreadyConnected {
		t.Errorf("Connect(dup) = %v, want ErrPeerAlreadyConnected", err)
	}
}

func TestDisconnectUnknown(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	if err := pm.Disconnect("nobody", "test"); err != ErrPeerNotConnected {
		t.Errorf("Disconnect(unknown) = %v, want ErrPeerNotConnected", err)
	}
}

func TestPeerLimits(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{
		MaxTotal:    2,
		MaxInbound:  1,
		MaxOutbound: 1,
	})
	pm.ConnectFn = func(string) ([]Capability, error) {
		return nil, nil
	}

	// Connect one outbound.
	if err := pm.Connect("out1"); err != nil {
		t.Fatalf("Connect(out1): %v", err)
	}
	if pm.OutboundCount() != 1 {
		t.Errorf("OutboundCount = %d, want 1", pm.OutboundCount())
	}

	// Second outbound should fail.
	if err := pm.Connect("out2"); err != ErrTooManyOutbound {
		t.Errorf("Connect(out2) = %v, want ErrTooManyOutbound", err)
	}

	// Accept one inbound.
	if err := pm.AcceptPeer("in1", nil); err != nil {
		t.Fatalf("AcceptPeer(in1): %v", err)
	}
	if pm.InboundCount() != 1 {
		t.Errorf("InboundCount = %d, want 1", pm.InboundCount())
	}

	// Total is now 2. Another inbound should fail (total limit).
	if err := pm.AcceptPeer("in2", nil); err != ErrTooManyPeers {
		t.Errorf("AcceptPeer(in2) = %v, want ErrTooManyPeers", err)
	}
}

func TestAcceptPeer(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{MaxTotal: 10, MaxInbound: 5})
	pm.RegisterProtocol("eth", 68, func(string, uint64, []byte) error { return nil })

	remoteCaps := []Capability{
		{Name: "eth", Version: 68},
		{Name: "snap", Version: 1},
	}

	if err := pm.AcceptPeer("inbound1", remoteCaps); err != nil {
		t.Fatalf("AcceptPeer: %v", err)
	}

	info := pm.PeerInfo("inbound1")
	if info == nil {
		t.Fatal("PeerInfo returned nil")
	}
	if !info.Inbound {
		t.Error("peer should be inbound")
	}
	// Only "eth" is shared.
	if len(info.Capabilities) != 1 || info.Capabilities[0].Name != "eth" {
		t.Errorf("caps = %v, want [eth/68]", info.Capabilities)
	}
}

func TestRouteMessage(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.ConnectFn = func(string) ([]Capability, error) {
		return []Capability{{Name: "eth", Version: 68}}, nil
	}

	var handledPeer string
	var handledCode uint64
	var handledPayload []byte

	pm.RegisterProtocol("eth", 68, func(peerID string, code uint64, payload []byte) error {
		handledPeer = peerID
		handledCode = code
		handledPayload = payload
		return nil
	})

	pm.Connect("peer1")

	payload := []byte{1, 2, 3}
	if err := pm.RouteMessage("peer1", "eth", 0x03, payload); err != nil {
		t.Fatalf("RouteMessage: %v", err)
	}
	if handledPeer != "peer1" {
		t.Errorf("handledPeer = %q, want peer1", handledPeer)
	}
	if handledCode != 0x03 {
		t.Errorf("handledCode = %d, want 3", handledCode)
	}
	if len(handledPayload) != 3 {
		t.Errorf("handledPayload len = %d, want 3", len(handledPayload))
	}
}

func TestRouteMessageUnknownPeer(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.RegisterProtocol("eth", 68, func(string, uint64, []byte) error { return nil })

	if err := pm.RouteMessage("nobody", "eth", 0, nil); err != ErrPeerNotConnected {
		t.Errorf("RouteMessage(unknown) = %v, want ErrPeerNotConnected", err)
	}
}

func TestRouteMessageUnknownProtocol(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.ConnectFn = func(string) ([]Capability, error) { return nil, nil }
	pm.Connect("peer1")

	if err := pm.RouteMessage("peer1", "unknown", 0, nil); err != ErrProtocolNotFound {
		t.Errorf("RouteMessage(unknown proto) = %v, want ErrProtocolNotFound", err)
	}
}

func TestUpdateLatency(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.ConnectFn = func(string) ([]Capability, error) { return nil, nil }
	pm.Connect("peer1")

	pm.UpdateLatency("peer1", 50*time.Millisecond)

	info := pm.PeerInfo("peer1")
	if info.Latency != 50*time.Millisecond {
		t.Errorf("Latency = %v, want 50ms", info.Latency)
	}
}

func TestHasCapability(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.RegisterProtocol("eth", 68, func(string, uint64, []byte) error { return nil })
	pm.ConnectFn = func(string) ([]Capability, error) {
		return []Capability{{Name: "eth", Version: 68}}, nil
	}
	pm.Connect("peer1")

	if !pm.HasCapability("peer1", "eth") {
		t.Error("HasCapability(eth) = false, want true")
	}
	if pm.HasCapability("peer1", "snap") {
		t.Error("HasCapability(snap) = true, want false")
	}
	if pm.HasCapability("nobody", "eth") {
		t.Error("HasCapability(unknown peer) = true, want false")
	}
}

func TestAllPeers(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{MaxTotal: 10})
	pm.ConnectFn = func(string) ([]Capability, error) { return nil, nil }

	pm.Connect("peer1")
	pm.Connect("peer2")
	pm.Connect("peer3")

	all := pm.AllPeers()
	if len(all) != 3 {
		t.Errorf("AllPeers len = %d, want 3", len(all))
	}
}

func TestInboundOutboundCounting(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{MaxTotal: 10})
	pm.ConnectFn = func(string) ([]Capability, error) { return nil, nil }

	pm.Connect("out1")
	pm.AcceptPeer("in1", nil)

	if pm.InboundCount() != 1 {
		t.Errorf("InboundCount = %d, want 1", pm.InboundCount())
	}
	if pm.OutboundCount() != 1 {
		t.Errorf("OutboundCount = %d, want 1", pm.OutboundCount())
	}

	// Disconnect inbound.
	pm.Disconnect("in1", "done")
	if pm.InboundCount() != 0 {
		t.Errorf("InboundCount after disconnect = %d, want 0", pm.InboundCount())
	}

	// Disconnect outbound.
	pm.Disconnect("out1", "done")
	if pm.OutboundCount() != 0 {
		t.Errorf("OutboundCount after disconnect = %d, want 0", pm.OutboundCount())
	}
}

func TestConcurrentProtocolManager(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{MaxTotal: 200})
	pm.ConnectFn = func(string) ([]Capability, error) { return nil, nil }
	pm.RegisterProtocol("eth", 68, func(string, uint64, []byte) error { return nil })

	var connectCount atomic.Int32
	pm.OnConnect(func(_ *PeerMgrInfo) {
		connectCount.Add(1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			nodeID := "peer" + string(rune('A'+i))
			pm.Connect(nodeID)
			pm.PeerInfo(nodeID)
			pm.PeerCount()
			pm.HasCapability(nodeID, "eth")
			pm.AllPeers()
			pm.Disconnect(nodeID, "test")
		}(i)
	}
	wg.Wait()

	if pm.PeerCount() != 0 {
		t.Errorf("PeerCount after all disconnects = %d, want 0", pm.PeerCount())
	}
}

func TestCapabilityString(t *testing.T) {
	c := Capability{Name: "eth", Version: 68}
	if s := c.String(); s != "eth/68" {
		t.Errorf("String() = %q, want eth/68", s)
	}
}

func TestProtocolManagerConfigDefaults(t *testing.T) {
	cfg := ProtocolManagerConfig{}
	cfg.defaults()
	if cfg.MaxTotal != 50 {
		t.Errorf("MaxTotal = %d, want 50", cfg.MaxTotal)
	}
	if cfg.MaxInbound != 50 {
		t.Errorf("MaxInbound = %d, want 50", cfg.MaxInbound)
	}
	if cfg.MaxOutbound != 50 {
		t.Errorf("MaxOutbound = %d, want 50", cfg.MaxOutbound)
	}
}

func TestPeerInfoReturnsNilForUnknown(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	if info := pm.PeerInfo("nonexistent"); info != nil {
		t.Error("PeerInfo for unknown peer should be nil")
	}
}

func TestRouteMessageMultipleVersions(t *testing.T) {
	pm := NewProtocolManager(ProtocolManagerConfig{})
	pm.ConnectFn = func(string) ([]Capability, error) { return nil, nil }

	var calledVersion uint64

	// Register v68 and v70. The router should pick the highest version.
	pm.RegisterProtocol("eth", 68, func(_ string, code uint64, _ []byte) error {
		calledVersion = 68
		return nil
	})
	pm.RegisterProtocol("eth", 70, func(_ string, code uint64, _ []byte) error {
		calledVersion = 70
		return nil
	})

	pm.Connect("peer1")

	if err := pm.RouteMessage("peer1", "eth", 0, nil); err != nil {
		t.Fatalf("RouteMessage: %v", err)
	}
	if calledVersion != 70 {
		t.Errorf("calledVersion = %d, want 70 (highest)", calledVersion)
	}
}
