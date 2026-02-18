package p2p

import (
	"sync"
	"testing"
	"time"
)

// --- Server with handshake tests ---

// TestServer_HandshakeConnect verifies the full lifecycle:
// connect -> handshake -> register with peer manager -> protocol run -> disconnect.
func TestServer_HandshakeConnect(t *testing.T) {
	var mu sync.Mutex
	var peerIDs []string
	protoDone := make(chan struct{}, 2)

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			mu.Lock()
			peerIDs = append(peerIDs, peer.ID())
			mu.Unlock()
			protoDone <- struct{}{}

			// Exchange a message over the transport to verify it works post-handshake.
			if err := tr.WriteMsg(Msg{Code: 0x00, Size: 4, Payload: []byte("ping")}); err != nil {
				return err
			}
			msg, err := tr.ReadMsg()
			if err != nil {
				return err
			}
			_ = msg
			return nil
		},
	}

	srv1 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		Name:       "srv1",
		NodeID:     "node-alpha",
	})
	srv2 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		Name:       "srv2",
		NodeID:     "node-beta",
	})

	if err := srv1.Start(); err != nil {
		t.Fatalf("srv1 start: %v", err)
	}
	defer srv1.Stop()

	if err := srv2.Start(); err != nil {
		t.Fatalf("srv2 start: %v", err)
	}
	defer srv2.Stop()

	// srv2 dials srv1.
	if err := srv2.AddPeer(srv1.ListenAddr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Wait for both protocol handlers.
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-protoDone:
		case <-timeout:
			t.Fatal("timeout waiting for protocol handler")
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(peerIDs) != 2 {
		t.Fatalf("expected 2 protocol runs, got %d", len(peerIDs))
	}

	// Verify that the peer IDs are the remote node IDs (from the handshake).
	// srv1 should see "node-beta" as its peer, and srv2 should see "node-alpha".
	idSet := make(map[string]bool)
	for _, id := range peerIDs {
		idSet[id] = true
	}
	if !idSet["node-alpha"] {
		t.Errorf("expected node-alpha in peer IDs, got %v", peerIDs)
	}
	if !idSet["node-beta"] {
		t.Errorf("expected node-beta in peer IDs, got %v", peerIDs)
	}
}

// TestServer_HandshakePeerCaps verifies that after handshake, the peer's
// capabilities are populated from the remote hello message.
func TestServer_HandshakePeerCaps(t *testing.T) {
	peerReady := make(chan *Peer, 2)

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			peerReady <- peer
			// Return immediately so the connection cleans up.
			return nil
		},
	}

	srv1 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		NodeID:     "caps-test-1",
	})
	srv2 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		NodeID:     "caps-test-2",
	})

	if err := srv1.Start(); err != nil {
		t.Fatalf("srv1 start: %v", err)
	}
	defer srv1.Stop()

	if err := srv2.Start(); err != nil {
		t.Fatalf("srv2 start: %v", err)
	}
	defer srv2.Stop()

	if err := srv2.AddPeer(srv1.ListenAddr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Collect both peers from the protocol handlers.
	var peers []*Peer
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case p := <-peerReady:
			peers = append(peers, p)
		case <-timeout:
			t.Fatal("timeout waiting for protocol handler")
		}
	}

	// Both peers should have "eth/68" in their capabilities.
	for _, p := range peers {
		caps := p.Caps()
		if len(caps) == 0 {
			t.Errorf("peer %s has no caps after handshake", p.ID())
			continue
		}
		found := false
		for _, c := range caps {
			if c.Name == "eth" && c.Version == 68 {
				found = true
			}
		}
		if !found {
			t.Errorf("peer %s missing eth/68 cap, got %v", p.ID(), caps)
		}
	}
}

// TestServer_HandshakeScoring verifies that handshake completion updates
// the peer's score. The score is checked from within the protocol handler
// to avoid a race with the cleanup defer.
func TestServer_HandshakeScoring(t *testing.T) {
	type scoreResult struct {
		peerID string
		score  float64
	}
	scoreCh := make(chan scoreResult, 2)

	// We capture the server references via closures after creation.
	var srv1, srv2 *Server

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			// Check the score from within the protocol handler. At this
			// point, HandshakeOK has been called but the cleanup defer
			// has not yet run.
			var s float64
			if srv1 != nil {
				sc := srv1.Scores().Get(peer.ID())
				s = sc.Value()
			}
			scoreCh <- scoreResult{peerID: peer.ID(), score: s}
			return nil
		},
	}

	srv1 = NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		NodeID:     "score-test-1",
	})
	srv2 = NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		NodeID:     "score-test-2",
	})

	if err := srv1.Start(); err != nil {
		t.Fatalf("srv1 start: %v", err)
	}
	defer srv1.Stop()

	if err := srv2.Start(); err != nil {
		t.Fatalf("srv2 start: %v", err)
	}
	defer srv2.Stop()

	srv2.AddPeer(srv1.ListenAddr().String())

	// Wait for both protocol handlers.
	timeout := time.After(3 * time.Second)
	gotPositive := false
	for i := 0; i < 2; i++ {
		select {
		case res := <-scoreCh:
			// The protocol handler on srv1 sees peer "score-test-2".
			// The HandshakeOK credit (5.0) should be reflected.
			if res.peerID == "score-test-2" && res.score >= scoreHandshakeOK {
				gotPositive = true
			}
		case <-timeout:
			t.Fatal("timeout waiting for protocol handler")
		}
	}
	if !gotPositive {
		t.Error("expected positive score for peer after handshake on srv1")
	}
}

// TestServer_MockTransports verifies that the server works with mock
// transport implementations (no real network).
func TestServer_MockTransports(t *testing.T) {
	ml := newMockListener()
	protoDone := make(chan string, 1)

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			protoDone <- peer.ID()
			return nil
		},
	}

	srv := NewServer(Config{
		MaxPeers:  5,
		Protocols: []Protocol{proto},
		Listener:  ml,
		NodeID:    "mock-server",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Create a mock connection pair.
	clientSide, serverSide := mockConnTransportPair()

	// Inject the server side into the listener.
	ml.inject(serverSide)

	// On the client side, perform a handshake concurrently.
	go func() {
		localHello := &HelloPacket{
			Version: 5,
			Name:    "mock-client",
			Caps:    []Cap{{Name: "eth", Version: 68}},
			ID:      "mock-peer",
		}
		PerformHandshake(clientSide, localHello)
	}()

	// Wait for the protocol handler.
	select {
	case peerID := <-protoDone:
		if peerID != "mock-peer" {
			t.Errorf("peer ID: got %q, want %q", peerID, "mock-peer")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for protocol handler with mock transport")
	}
}

// TestServer_PeerDisconnectCleanup verifies that after a peer disconnects,
// it is removed from the peer set and scores are cleaned up.
func TestServer_PeerDisconnectCleanup(t *testing.T) {
	ml := newMockListener()
	protoDone := make(chan struct{}, 1)

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			protoDone <- struct{}{}
			// Return immediately to simulate disconnect.
			return nil
		},
	}

	srv := NewServer(Config{
		MaxPeers:  5,
		Protocols: []Protocol{proto},
		Listener:  ml,
		NodeID:    "cleanup-server",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	clientSide, serverSide := mockConnTransportPair()
	ml.inject(serverSide)

	// Client performs handshake.
	go func() {
		localHello := &HelloPacket{
			Version: 5,
			Name:    "temp-client",
			Caps:    []Cap{{Name: "eth", Version: 68}},
			ID:      "temp-peer",
		}
		PerformHandshake(clientSide, localHello)
	}()

	// Wait for protocol to complete.
	select {
	case <-protoDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for protocol handler")
	}

	// Give the server time to clean up.
	time.Sleep(50 * time.Millisecond)

	// Peer should be removed from the set.
	if srv.PeerCount() != 0 {
		t.Errorf("PeerCount after disconnect: got %d, want 0", srv.PeerCount())
	}
}

// TestServer_RunningState verifies the Running() method.
func TestServer_RunningState(t *testing.T) {
	srv := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
	})

	if srv.Running() {
		t.Error("Running() should be false before Start")
	}

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !srv.Running() {
		t.Error("Running() should be true after Start")
	}

	srv.Stop()
	if srv.Running() {
		t.Error("Running() should be false after Stop")
	}
}

// TestServer_DoubleStart verifies that starting a running server returns an error.
func TestServer_DoubleStart(t *testing.T) {
	srv := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	err := srv.Start()
	if err == nil {
		t.Error("expected error on double Start")
	}
}

// TestServer_HandshakeFullLifecycle tests the complete peer connection lifecycle:
// connect -> handshake -> register -> protocol message exchange -> disconnect -> cleanup.
func TestServer_HandshakeFullLifecycle(t *testing.T) {
	var mu sync.Mutex
	lifecycle := make([]string, 0)

	// Use a gate channel so we can control when the protocol finishes.
	// Each handler signals it started, then waits for gate to be closed
	// before exchanging messages and returning.
	gate := make(chan struct{})
	protoStarted := make(chan struct{}, 2)
	protoDone := make(chan struct{}, 2)

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			mu.Lock()
			lifecycle = append(lifecycle, "proto-start:"+peer.ID())
			mu.Unlock()
			protoStarted <- struct{}{}

			// Wait until the test says to proceed.
			<-gate

			// Exchange messages.
			if err := tr.WriteMsg(Msg{Code: 0x01, Payload: []byte("data")}); err != nil {
				return err
			}
			msg, err := tr.ReadMsg()
			if err != nil {
				return err
			}

			mu.Lock()
			lifecycle = append(lifecycle, "proto-done:"+peer.ID()+":"+string(msg.Payload))
			mu.Unlock()
			protoDone <- struct{}{}
			return nil
		},
	}

	srv1 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		NodeID:     "lifecycle-1",
		Name:       "lifecycle-srv1",
	})
	srv2 := NewServer(Config{
		ListenAddr: "127.0.0.1:0",
		MaxPeers:   5,
		Protocols:  []Protocol{proto},
		NodeID:     "lifecycle-2",
		Name:       "lifecycle-srv2",
	})

	if err := srv1.Start(); err != nil {
		t.Fatalf("srv1 start: %v", err)
	}
	defer srv1.Stop()

	if err := srv2.Start(); err != nil {
		t.Fatalf("srv2 start: %v", err)
	}
	defer srv2.Stop()

	// Connect.
	if err := srv2.AddPeer(srv1.ListenAddr().String()); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Wait for both protocols to start (handshake completed, registered).
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-protoStarted:
		case <-timeout:
			t.Fatal("timeout waiting for protocol start")
		}
	}

	// Verify both peers are registered while the protocol handlers are held.
	if srv1.PeerCount() != 1 {
		t.Errorf("srv1 PeerCount: got %d, want 1", srv1.PeerCount())
	}
	if srv2.PeerCount() != 1 {
		t.Errorf("srv2 PeerCount: got %d, want 1", srv2.PeerCount())
	}

	// Release the handlers to exchange messages and finish.
	close(gate)

	// Wait for both protocols to complete.
	for i := 0; i < 2; i++ {
		select {
		case <-protoDone:
		case <-timeout:
			t.Fatal("timeout waiting for protocol done")
		}
	}

	// Give cleanup time.
	time.Sleep(50 * time.Millisecond)

	// After protocol completes, peers should be cleaned up.
	if srv1.PeerCount() != 0 {
		t.Errorf("srv1 PeerCount after disconnect: got %d, want 0", srv1.PeerCount())
	}
	if srv2.PeerCount() != 0 {
		t.Errorf("srv2 PeerCount after disconnect: got %d, want 0", srv2.PeerCount())
	}

	// Verify lifecycle events occurred.
	mu.Lock()
	defer mu.Unlock()
	if len(lifecycle) < 4 {
		t.Errorf("expected at least 4 lifecycle events, got %d: %v", len(lifecycle), lifecycle)
	}
}

// TestServer_AddPeerWithMockDialer verifies AddPeer using a mock dialer.
func TestServer_AddPeerWithMockDialer(t *testing.T) {
	md := newMockDialer()
	ml := newMockListener()
	protoDone := make(chan string, 1)

	proto := Protocol{
		Name:    "eth",
		Version: 68,
		Length:  17,
		Run: func(peer *Peer, tr Transport) error {
			protoDone <- peer.ID()
			return nil
		},
	}

	srv := NewServer(Config{
		MaxPeers:  5,
		Protocols: []Protocol{proto},
		Dialer:    md,
		Listener:  ml,
		NodeID:    "dialer-server",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Create the connection pair: dialer gets one end, the "remote" gets the other.
	dialerSide, remoteSide := mockConnTransportPair()
	md.prepare(dialerSide)

	// The remote side performs the handshake.
	go func() {
		remoteHello := &HelloPacket{
			Version: 5,
			Name:    "remote",
			Caps:    []Cap{{Name: "eth", Version: 68}},
			ID:      "remote-node",
		}
		PerformHandshake(remoteSide, remoteHello)
	}()

	if err := srv.AddPeer("fake-addr:30303"); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	select {
	case peerID := <-protoDone:
		if peerID != "remote-node" {
			t.Errorf("peer ID: got %q, want %q", peerID, "remote-node")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for protocol handler")
	}
}
